package twilio

import (
	"encoding/base64"
	"errors"
	"testing"

	"github.com/notfoundsam/sms-mock-server/app/config"
	"github.com/notfoundsam/sms-mock-server/app/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Numbers used in tests. libphonenumber flags +1555-prefixed numbers as
// invalid (NANP reserves them for fictional use), so we use real-looking
// numbers from valid area codes — these pass libphonenumber's full validation.
const (
	validFrom1     = "+12025551234" // valid From (in allowlist)
	validFrom2     = "+12025556789"
	validRegistered = "+12025550100" // valid To (in registered list)
	validRegistered2 = "+12025550101"
	validFailure   = "+12025550199" // valid To (in failure list)
	validUnknown   = "+12025557777" // valid To, in neither list
	validNotAllowedFrom = "+12025558888" // valid format, not in allowedFrom
)

// baseConfig produces a config.Twilio with strict validation and known
// account credentials. Tests override fields as needed.
func baseConfig() *config.Twilio {
	return &config.Twilio{
		AccountSid: "ACtest",
		AuthToken:  "ttoken",
		Validation: config.Validation{
			RequireAuth:         true,
			ValidatePhoneFormat: true,
			CheckFromNumbers:    true,
			RequireParameters:   true,
		},
		SuccessNumbers:     []string{validRegistered, validRegistered2},
		AllowedFromNumbers: []string{validFrom1, validFrom2},
		FailureNumbers:     []string{validFailure},
	}
}

func basicAuthHeader(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

func mustValidationErr(t *testing.T, err error) *provider.ValidationError {
	t.Helper()
	require.Error(t, err, "expected validation error, got nil")
	v, ok := provider.IsValidationError(err)
	require.True(t, ok, "err %v is not *ValidationError", err)
	return v
}

// --- ValidateAuth ---

func TestValidateAuth_skipsWhenDisabled(t *testing.T) {
	cfg := baseConfig()
	cfg.Validation.RequireAuth = false
	p := New(cfg)
	assert.NoError(t, p.ValidateAuth("", ""), "auth-disabled")
	assert.NoError(t, p.ValidateAuth("Basic garbage", "wrong-sid"), "auth-disabled with garbage")
}

func TestValidateAuth_validCredentials(t *testing.T) {
	p := New(baseConfig())
	assert.NoError(t, p.ValidateAuth(basicAuthHeader("ACtest", "ttoken"), "ACtest"), "valid creds")
}

func TestValidateAuth_failures(t *testing.T) {
	p := New(baseConfig())
	cases := []struct {
		name    string
		header  string
		urlSid  string
	}{
		{"missing header", "", "ACtest"},
		{"wrong scheme", "Bearer abc", "ACtest"},
		{"bad base64", "Basic !!!notbase64!!!", "ACtest"},
		{"no colon in creds", "Basic " + base64.StdEncoding.EncodeToString([]byte("nocolonhere")), "ACtest"},
		{"wrong sid", basicAuthHeader("ACwrong", "ttoken"), "ACwrong"},
		{"wrong token", basicAuthHeader("ACtest", "wrongtoken"), "ACtest"},
		{"empty user", basicAuthHeader("", "ttoken"), "ACtest"},
		{"empty pass", basicAuthHeader("ACtest", ""), "ACtest"},
		{"sid mismatch in URL", basicAuthHeader("ACtest", "ttoken"), "ACdifferent"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := p.ValidateAuth(tc.header, tc.urlSid)
			v := mustValidationErr(t, err)
			assert.Equal(t, provider.ErrCodeAuthFailed, v.Code)
			assert.Equal(t, 401, v.HTTPStatus)
			assert.Equal(t, provider.TemplateAuthFailed, v.TemplateName)
		})
	}
}

// --- ValidateSMS / required parameters ---

func TestValidateSMS_missingParameters(t *testing.T) {
	p := New(baseConfig())
	cases := []struct {
		name string
		req  provider.SMSRequest
		want string
	}{
		{"missing From", provider.SMSRequest{To: "+12025550100", Body: "x"}, "From"},
		{"missing To", provider.SMSRequest{From: "+12025551234", Body: "x"}, "To"},
		{"missing Body", provider.SMSRequest{From: "+12025551234", To: "+12025550100"}, "Body"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := p.ValidateSMS(tc.req)
			v := mustValidationErr(t, err)
			assert.Equal(t, provider.ErrCodeMissingParameter, v.Code)
			assert.Equal(t, 400, v.HTTPStatus)
			assert.Equal(t, tc.want, v.Vars["parameter"])
		})
	}
}

func TestValidateSMS_skipsWhenRequireParametersOff(t *testing.T) {
	cfg := baseConfig()
	cfg.Validation.RequireParameters = false
	cfg.Validation.ValidatePhoneFormat = false
	cfg.Validation.CheckFromNumbers = false
	p := New(cfg)

	assert.NoError(t, p.ValidateSMS(provider.SMSRequest{}), "expected no error with all validations disabled")
}

// --- Phone format ---

func TestValidatePhoneFormat_invalid(t *testing.T) {
	p := New(baseConfig())
	cases := []struct {
		name string
		req  provider.SMSRequest
		field string
		num   string
	}{
		{"To not E.164", provider.SMSRequest{From: "+12025551234", To: "+1234", Body: "x"}, "To", "+1234"},
		{"From not E.164", provider.SMSRequest{From: "12345", To: "+12025550100", Body: "x"}, "From", "12345"},
		{"To unparseable garbage", provider.SMSRequest{From: "+12025551234", To: "not-a-number", Body: "x"}, "To", "not-a-number"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := p.ValidateSMS(tc.req)
			v := mustValidationErr(t, err)
			assert.Equal(t, provider.ErrCodeInvalidPhoneNumber, v.Code)
			assert.Equal(t, tc.field, v.Vars["field"])
			assert.Equal(t, tc.num, v.Vars["number"])
		})
	}
}

func TestValidatePhoneFormat_validCountryCodes(t *testing.T) {
	p := New(baseConfig())
	// Multiple country codes — all must pass libphonenumber
	cases := []provider.SMSRequest{
		{From: "+12025551234", To: "+12025550100", Body: "us"},
		// US numbers are sufficient for round-trip; the goal is to confirm
		// libphonenumber is doing the validation, not the success_numbers list.
	}
	for _, req := range cases {
		// Note: From-allowlist still enforced. Use a number in allowedFrom.
		assert.NoError(t, p.ValidateSMS(req), "ValidateSMS for %+v", req)
	}
}

func TestValidatePhoneFormat_skippedWhenDisabled(t *testing.T) {
	cfg := baseConfig()
	// "+1234" would fail libphonenumber, but with the toggle off it passes.
	// We also need to skip the From-allowlist for the test request.
	cfg.Validation.ValidatePhoneFormat = false
	cfg.Validation.CheckFromNumbers = false
	p := New(cfg)
	assert.NoError(t, p.ValidateSMS(provider.SMSRequest{From: "garbage", To: "alsogarbage", Body: "x"}), "phone-format off")
}

// --- From-allowlist ---

func TestValidateFromAllowlist(t *testing.T) {
	p := New(baseConfig())

	// allowed
	err := p.ValidateSMS(provider.SMSRequest{From: "+12025551234", To: "+12025550100", Body: "x"})
	require.NoError(t, err, "allowed From")

	// disallowed
	err = p.ValidateSMS(provider.SMSRequest{From: "+12025558888", To: "+12025550100", Body: "x"})
	v := mustValidationErr(t, err)
	assert.Equal(t, provider.ErrCodeInvalidFromNumber, v.Code)
	assert.Equal(t, "+12025558888", v.Vars["from_number"])
}

func TestValidateFromAllowlist_skippedWhenDisabled(t *testing.T) {
	cfg := baseConfig()
	cfg.Validation.CheckFromNumbers = false
	p := New(cfg)
	err := p.ValidateSMS(provider.SMSRequest{From: "+12025558888", To: "+12025550100", Body: "x"})
	assert.NoError(t, err, "allowlist off")
}

// --- Validation order ---

// When all toggles are on and multiple things are wrong, error must be
// returned in this order: params → phone format (From) → phone format (To) → From-allowlist.
func TestValidateSMS_returnsFirstFailureInOrder(t *testing.T) {
	p := New(baseConfig())

	// Missing param + bad phone + bad allowlist → missing param wins
	err := p.ValidateSMS(provider.SMSRequest{From: "garbage"})
	v := mustValidationErr(t, err)
	assert.Equal(t, provider.ErrCodeMissingParameter, v.Code, "expected missing_parameter")

	// Valid params but bad From phone format → phone format wins over allowlist
	err = p.ValidateSMS(provider.SMSRequest{From: "garbage", To: "+12025550100", Body: "x"})
	v = mustValidationErr(t, err)
	assert.Equal(t, provider.ErrCodeInvalidPhoneNumber, v.Code, "expected invalid_phone_number")
	assert.Equal(t, "From", v.Vars["field"])

	// Valid From phone but invalid To phone → phone format on To
	err = p.ValidateSMS(provider.SMSRequest{From: "+12025551234", To: "garbage", Body: "x"})
	v = mustValidationErr(t, err)
	assert.Equal(t, provider.ErrCodeInvalidPhoneNumber, v.Code, "expected invalid_phone_number for To")
	assert.Equal(t, "To", v.Vars["field"])

	// All phone formats valid but From not allowlisted → from_number error
	err = p.ValidateSMS(provider.SMSRequest{From: "+12025558888", To: "+12025550100", Body: "x"})
	v = mustValidationErr(t, err)
	assert.Equal(t, provider.ErrCodeInvalidFromNumber, v.Code, "expected invalid_from_number")
}

// --- ValidateCall ---

func TestValidateCall_missingParams(t *testing.T) {
	p := New(baseConfig())
	cases := []struct {
		name string
		req  provider.CallRequest
		want string
	}{
		{"missing From", provider.CallRequest{To: "+12025550100", URL: "http://x"}, "From"},
		{"missing To", provider.CallRequest{From: "+12025551234", URL: "http://x"}, "To"},
		{"missing Url", provider.CallRequest{From: "+12025551234", To: "+12025550100"}, "Url"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := p.ValidateCall(tc.req)
			v := mustValidationErr(t, err)
			assert.Equal(t, provider.ErrCodeMissingParameter, v.Code)
			assert.Equal(t, tc.want, v.Vars["parameter"])
		})
	}
}

// --- IsKnownNumber + ShouldSucceed ---

func TestIsKnownNumber(t *testing.T) {
	p := New(baseConfig())
	cases := []struct {
		num  string
		want bool
	}{
		{validFailure, true},         // failure list
		{validRegistered, true},      // success list
		{validRegistered2, true},     // success list (second)
		{validUnknown, false},        // valid format, in neither list
		{validNotAllowedFrom, false}, // valid format, not in success/failure (it's a "From not in allowlist")
		{"", false},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, p.IsKnownNumber(tc.num), "IsKnownNumber(%q)", tc.num)
	}
}

func TestIsKnownNumber_emptyLists(t *testing.T) {
	cfg := baseConfig()
	cfg.SuccessNumbers = nil
	cfg.FailureNumbers = nil
	p := New(cfg)
	// With both lists empty, every number is unknown — this is the new
	// "callbacks effectively disabled" mode.
	for _, num := range []string{validRegistered, validFailure, validUnknown, ""} {
		assert.False(t, p.IsKnownNumber(num), "IsKnownNumber(%q) with empty lists", num)
	}
}

func TestShouldSucceed_failureListBeatsSuccess(t *testing.T) {
	cfg := baseConfig()
	cfg.SuccessNumbers = []string{"+12025550100"}
	cfg.FailureNumbers = []string{"+12025550100"} // contradictory but failure wins
	p := New(cfg)
	assert.False(t, p.ShouldSucceed("+12025550100"), "failure list must take precedence over success")
}

func TestShouldSucceed_successNumber(t *testing.T) {
	p := New(baseConfig())
	assert.True(t, p.ShouldSucceed(validRegistered), "success-listed number must return true")
	assert.True(t, p.ShouldSucceed(validRegistered2), "success-listed number must return true")
}

func TestShouldSucceed_failureNumber(t *testing.T) {
	p := New(baseConfig())
	assert.False(t, p.ShouldSucceed(validFailure), "failure-listed number must return false")
}

// --- Misc ---

func TestName(t *testing.T) {
	p := New(baseConfig())
	assert.Equal(t, "twilio", p.Name())
}

func TestValidationError_ErrorString(t *testing.T) {
	err := &provider.ValidationError{Code: 21211, Message: "bad number"}
	assert.NotEmpty(t, err.Error(), "Error() should not be empty")

	// errors.As/Is should work via IsValidationError
	v, ok := provider.IsValidationError(err)
	assert.True(t, ok)
	assert.Equal(t, 21211, v.Code)
}

func TestIsValidationError_NilAndNonValidation(t *testing.T) {
	v, ok := provider.IsValidationError(nil)
	assert.False(t, ok, "expected (nil,false) for nil input")
	assert.Nil(t, v)

	v, ok = provider.IsValidationError(errors.New("plain error"))
	assert.False(t, ok, "expected (nil,false) for non-ValidationError")
	assert.Nil(t, v)
}
