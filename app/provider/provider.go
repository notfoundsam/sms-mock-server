// Package provider defines the abstraction over SMS/voice carrier APIs.
// The Twilio adapter is the first (and currently only) implementation.
package provider

import (
	"errors"
	"fmt"
)

// Provider is the interface implemented by carrier adapters (Twilio, etc.).
//
// IsKnownNumber and ShouldSucceed work together to preserve Python's effective
// behavior: the dispatcher checks IsKnownNumber first; if false, no progression,
// no callbacks. Only if true does it consult ShouldSucceed. This means the
// `default_behavior` config field — which ShouldSucceed consults — is dead code
// in practice today, matching the Python implementation. See the Task 4 / Task 7
// notes in docs/plans/20260507-go-rewrite.md.
type Provider interface {
	// Name returns the provider identifier ("twilio") for routing template lookups.
	Name() string

	// ValidateAuth checks credentials supplied via HTTP Basic auth.
	// Returns nil on success; *ValidationError on failure.
	ValidateAuth(authHeader, accountSidFromURL string) error

	// ValidateSMS checks an inbound SMS request: required params, phone format, From-allowlist.
	ValidateSMS(req SMSRequest) error

	// ValidateCall is the call-side equivalent of ValidateSMS.
	ValidateCall(req CallRequest) error

	// IsKnownNumber reports whether the destination number is in the
	// failure_numbers or registered_numbers list. Numbers not in either
	// list cause the dispatcher to skip status progression.
	IsKnownNumber(toNumber string) bool

	// ShouldSucceed returns true if the destination should produce a
	// success flow, false for a failure flow. Only meaningful when
	// IsKnownNumber returns true.
	ShouldSucceed(toNumber string) bool
}

// SMSRequest is the parsed form payload from POST .../Messages.json.
type SMSRequest struct {
	AccountSid     string
	From           string
	To             string
	Body           string
	StatusCallback string
}

// CallRequest is the parsed form payload from POST .../Calls.json.
type CallRequest struct {
	AccountSid     string
	From           string
	To             string
	URL            string // TwiML URL
	StatusCallback string
}

// Twilio error codes — kept here so handlers and tests can refer to them by name
// instead of by integer literal.
const (
	ErrCodeAuthFailed         = 20003
	ErrCodeMissingParameter   = 21604
	ErrCodeInvalidPhoneNumber = 21211
	ErrCodeInvalidFromNumber  = 21606
)

// Error template names (filenames in templates/errors/{provider}/).
const (
	TemplateAuthFailed         = "auth_failed.json"
	TemplateMissingParameter   = "missing_parameter.json"
	TemplateInvalidPhoneNumber = "invalid_phone_number.json"
	TemplateInvalidFromNumber  = "invalid_from_number.json"
)

// ValidationError is returned by Provider validation methods. It carries
// everything the HTTP handler needs to render the right error response:
// the Twilio error code, the HTTP status, the template filename, and any
// template variables.
type ValidationError struct {
	Code         int               // Twilio error code (e.g. 21211)
	Message      string            // human-readable, primarily for logs
	HTTPStatus   int               // 400, 401, etc.
	TemplateName string            // e.g. "invalid_phone_number.json"
	Vars         map[string]string // substituted into the error template
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error %d: %s", e.Code, e.Message)
}

// IsValidationError unwraps an error to check whether it is a *ValidationError.
// Provided so handlers can use errors.As without importing the concrete type
// in test wrappers.
func IsValidationError(err error) (*ValidationError, bool) {
	if err == nil {
		return nil, false
	}
	var v *ValidationError
	if errors.As(err, &v) {
		return v, true
	}
	return nil, false
}

// Required-parameter sets per request type. Exposed so the Twilio adapter
// can also expose them (and tests can reuse the same list).
var (
	RequiredSMSParams  = []string{"From", "To", "Body"}
	RequiredCallParams = []string{"From", "To", "Url"}
)

