package config

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// clearEnv unsets every config-relevant env var so each test starts from a
// clean slate. t.Setenv on a non-existent var is fine; we use it for both
// setting and unsetting.
func clearEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"SMS_MOCK_PORT",
		"SMS_MOCK_TIMEZONE",
		"SMS_MOCK_DB_PATH",
		"SMS_MOCK_PROVIDER",
		"SMS_MOCK_TWILIO_ACCOUNT_SID",
		"SMS_MOCK_TWILIO_AUTH_TOKEN",
		"SMS_MOCK_TWILIO_ALLOWED_FROM_NUMBERS",
		"SMS_MOCK_TWILIO_SUCCESS_NUMBERS",
		"SMS_MOCK_TWILIO_FAILURE_NUMBERS",
		"SMS_MOCK_TWILIO_REQUIRE_AUTH",
		"SMS_MOCK_TWILIO_VALIDATE_PHONE_FORMAT",
		"SMS_MOCK_TWILIO_CHECK_FROM_NUMBERS",
		"SMS_MOCK_TWILIO_REQUIRE_PARAMETERS",
		"SMS_MOCK_TWILIO_CALLBACK_DELAY_SECONDS",
		"SMS_MOCK_TWILIO_CALLBACK_RETRY_ATTEMPTS",
		"SMS_MOCK_TWILIO_CALLBACK_RETRY_DELAY_SECONDS",
		"SMS_MOCK_MAX_MESSAGES",
		"SMS_MOCK_MAX_CALLS",
		"SMS_MOCK_MAX_AGE",
		"SMS_MOCK_HIDE_DELETE_ALL_BUTTON",
	}
	for _, k := range keys {
		t.Setenv(k, "")
	}
}

// useTempDB points SMS_MOCK_DB_PATH at a per-test temp file so Load()'s
// MkdirAll step doesn't try to create /tmp/mock_server.db on a system that
// might not have a writable /tmp.
func useTempDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	t.Setenv("SMS_MOCK_DB_PATH", path)
	return path
}

func TestLoad_appliesDefaults(t *testing.T) {
	clearEnv(t)
	dbPath := useTempDB(t)
	// require_auth defaults to true, so we need credentials to pass validate()
	t.Setenv("SMS_MOCK_TWILIO_ACCOUNT_SID", "ACtest")
	t.Setenv("SMS_MOCK_TWILIO_AUTH_TOKEN", "ttoken")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, 8080, cfg.Server.Port)
	assert.Equal(t, "UTC", cfg.Server.Timezone)
	assert.Equal(t, "twilio", cfg.Provider)
	assert.Equal(t, dbPath, cfg.Database.Path)
	assert.True(t, cfg.Twilio.Validation.RequireAuth)
	assert.True(t, cfg.Twilio.Validation.ValidatePhoneFormat)
	assert.True(t, cfg.Twilio.Validation.CheckFromNumbers)
	assert.True(t, cfg.Twilio.Validation.RequireParameters)
	assert.Equal(t, 2, cfg.Twilio.Callbacks.DelaySeconds)
	assert.Equal(t, 3, cfg.Twilio.Callbacks.RetryAttempts)
	assert.Equal(t, 5, cfg.Twilio.Callbacks.RetryDelaySeconds)
	assert.Empty(t, cfg.Twilio.SuccessNumbers)
	assert.Empty(t, cfg.Twilio.FailureNumbers)
	assert.Empty(t, cfg.Twilio.AllowedFromNumbers)
	assert.Equal(t, 500, cfg.Limits.MaxMessages)
	assert.Equal(t, 500, cfg.Limits.MaxCalls)
	assert.Equal(t, time.Duration(0), cfg.Limits.MaxAge)
	assert.False(t, cfg.Limits.HideDeleteAllButton)
}

func TestLoad_envOverridesDefaults(t *testing.T) {
	clearEnv(t)
	dbPath := useTempDB(t)
	t.Setenv("SMS_MOCK_PORT", "9090")
	t.Setenv("SMS_MOCK_TIMEZONE", "Asia/Tokyo")
	t.Setenv("SMS_MOCK_PROVIDER", "twilio")
	t.Setenv("SMS_MOCK_TWILIO_ACCOUNT_SID", "ACtest_account_sid")
	t.Setenv("SMS_MOCK_TWILIO_AUTH_TOKEN", "secret_token")
	t.Setenv("SMS_MOCK_TWILIO_SUCCESS_NUMBERS", "+15551234567,+15559876543")
	t.Setenv("SMS_MOCK_TWILIO_FAILURE_NUMBERS", "+15559999999")
	t.Setenv("SMS_MOCK_TWILIO_ALLOWED_FROM_NUMBERS", "+15550000001,+15550000002")
	t.Setenv("SMS_MOCK_TWILIO_REQUIRE_AUTH", "false")
	t.Setenv("SMS_MOCK_TWILIO_VALIDATE_PHONE_FORMAT", "false")
	t.Setenv("SMS_MOCK_TWILIO_CHECK_FROM_NUMBERS", "false")
	t.Setenv("SMS_MOCK_TWILIO_REQUIRE_PARAMETERS", "false")
	t.Setenv("SMS_MOCK_TWILIO_CALLBACK_DELAY_SECONDS", "1")
	t.Setenv("SMS_MOCK_TWILIO_CALLBACK_RETRY_ATTEMPTS", "5")
	t.Setenv("SMS_MOCK_TWILIO_CALLBACK_RETRY_DELAY_SECONDS", "10")
	t.Setenv("SMS_MOCK_MAX_MESSAGES", "100")
	t.Setenv("SMS_MOCK_MAX_CALLS", "200")
	t.Setenv("SMS_MOCK_MAX_AGE", "72h")
	t.Setenv("SMS_MOCK_HIDE_DELETE_ALL_BUTTON", "true")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, 9090, cfg.Server.Port)
	assert.Equal(t, "Asia/Tokyo", cfg.Server.Timezone)
	assert.Equal(t, dbPath, cfg.Database.Path)
	assert.Equal(t, "ACtest_account_sid", cfg.Twilio.AccountSid)
	assert.Equal(t, "secret_token", cfg.Twilio.AuthToken)
	assert.Equal(t, []string{"+15551234567", "+15559876543"}, cfg.Twilio.SuccessNumbers)
	assert.Equal(t, []string{"+15559999999"}, cfg.Twilio.FailureNumbers)
	assert.Equal(t, []string{"+15550000001", "+15550000002"}, cfg.Twilio.AllowedFromNumbers)
	assert.False(t, cfg.Twilio.Validation.RequireAuth)
	assert.False(t, cfg.Twilio.Validation.ValidatePhoneFormat)
	assert.False(t, cfg.Twilio.Validation.CheckFromNumbers)
	assert.False(t, cfg.Twilio.Validation.RequireParameters)
	assert.Equal(t, 1, cfg.Twilio.Callbacks.DelaySeconds)
	assert.Equal(t, 5, cfg.Twilio.Callbacks.RetryAttempts)
	assert.Equal(t, 10, cfg.Twilio.Callbacks.RetryDelaySeconds)
	assert.Equal(t, 100, cfg.Limits.MaxMessages)
	assert.Equal(t, 200, cfg.Limits.MaxCalls)
	assert.Equal(t, 72*time.Hour, cfg.Limits.MaxAge)
	assert.True(t, cfg.Limits.HideDeleteAllButton)
}

func TestLoad_authDisabledAllowsEmptyCredentials(t *testing.T) {
	clearEnv(t)
	useTempDB(t)
	t.Setenv("SMS_MOCK_TWILIO_REQUIRE_AUTH", "false")

	cfg, err := Load()
	require.NoError(t, err)
	assert.False(t, cfg.Twilio.Validation.RequireAuth)
	assert.Empty(t, cfg.Twilio.AccountSid)
	assert.Empty(t, cfg.Twilio.AuthToken)
}

func TestLoad_errors(t *testing.T) {
	cases := []struct {
		name     string
		setup    func(t *testing.T)
		contains string
	}{
		{
			"unsupported provider",
			func(t *testing.T) {
				t.Setenv("SMS_MOCK_PROVIDER", "vonage")
			},
			"unsupported provider",
		},
		{
			"invalid timezone",
			func(t *testing.T) {
				t.Setenv("SMS_MOCK_TIMEZONE", "Mars/Olympus_Mons")
				t.Setenv("SMS_MOCK_TWILIO_ACCOUNT_SID", "ACtest")
				t.Setenv("SMS_MOCK_TWILIO_AUTH_TOKEN", "ttoken")
			},
			"invalid timezone",
		},
		{
			"require_auth without account_sid",
			func(t *testing.T) {
				// auth required (default), no creds set
			},
			"account_sid must be set",
		},
		{
			"require_auth with placeholder account_sid",
			func(t *testing.T) {
				t.Setenv("SMS_MOCK_TWILIO_ACCOUNT_SID", "ACXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX")
				t.Setenv("SMS_MOCK_TWILIO_AUTH_TOKEN", "ttoken")
			},
			"account_sid must be set",
		},
		{
			"require_auth with placeholder auth_token",
			func(t *testing.T) {
				t.Setenv("SMS_MOCK_TWILIO_ACCOUNT_SID", "ACtest")
				t.Setenv("SMS_MOCK_TWILIO_AUTH_TOKEN", "your_auth_token_here")
			},
			"auth_token must be set",
		},
		{
			"negative max_messages",
			func(t *testing.T) {
				t.Setenv("SMS_MOCK_TWILIO_REQUIRE_AUTH", "false")
				t.Setenv("SMS_MOCK_MAX_MESSAGES", "-1")
			},
			"max_messages must not be negative",
		},
		{
			"negative max_calls",
			func(t *testing.T) {
				t.Setenv("SMS_MOCK_TWILIO_REQUIRE_AUTH", "false")
				t.Setenv("SMS_MOCK_MAX_CALLS", "-5")
			},
			"max_calls must not be negative",
		},
		{
			"max_age with minutes suffix",
			func(t *testing.T) {
				t.Setenv("SMS_MOCK_TWILIO_REQUIRE_AUTH", "false")
				t.Setenv("SMS_MOCK_MAX_AGE", "30m")
			},
			"max_age must match",
		},
		{
			"max_age garbage",
			func(t *testing.T) {
				t.Setenv("SMS_MOCK_TWILIO_REQUIRE_AUTH", "false")
				t.Setenv("SMS_MOCK_MAX_AGE", "garbage")
			},
			"max_age must match",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			useTempDB(t)
			tc.setup(t)
			_, err := Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.contains)
		})
	}
}

// TestGetList covers the list parsing edge cases without going through Load().
// Direct unit test on getList keeps the assertions tight.
func TestGetList(t *testing.T) {
	const key = "SMS_MOCK_TEST_LIST"
	cases := []struct {
		name string
		val  string
		want []string
	}{
		{"unset", "", nil},
		{"single", "+15551234567", []string{"+15551234567"}},
		{"multiple", "+15551234567,+15559876543", []string{"+15551234567", "+15559876543"}},
		{"whitespace", " +15551234567 , +15559876543 ", []string{"+15551234567", "+15559876543"}},
		{"trailing comma", "+15551234567,", []string{"+15551234567"}},
		{"leading comma", ",+15551234567", []string{"+15551234567"}},
		{"empty entries", "+15551234567,,+15559876543", []string{"+15551234567", "+15559876543"}},
		{"only commas", ",,,", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(key, tc.val)
			assert.Equal(t, tc.want, getList(key))
		})
	}
}

func TestGetBool_invalidFallsBackToDefault(t *testing.T) {
	const key = "SMS_MOCK_TEST_BOOL"
	t.Setenv(key, "ture") // typo
	assert.True(t, getBool(key, true), "invalid bool should fall back to default")
	assert.False(t, getBool(key, false), "invalid bool should fall back to default")
}

func TestGetInt_invalidFallsBackToDefault(t *testing.T) {
	const key = "SMS_MOCK_TEST_INT"
	t.Setenv(key, "not-a-number")
	assert.Equal(t, 42, getInt(key, 42))
}

func TestParseMaxAge(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"", 0, false},
		{"72h", 72 * time.Hour, false},
		{"1h", time.Hour, false},
		{"3d", 72 * time.Hour, false},
		{"1d", 24 * time.Hour, false},
		{"30m", 0, true},
		{"72", 0, true},
		{"d", 0, true},
		{"1.5h", 0, true},
		{"-1h", 0, true}, // regex requires unsigned digits
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseMaxAge(tc.in)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
