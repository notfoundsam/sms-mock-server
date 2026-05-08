package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_valid(t *testing.T) {
	cfg, err := Load("testdata/valid.yaml")
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1", cfg.Server.Host)
	assert.Equal(t, 9090, cfg.Server.Port)
	assert.Equal(t, "Asia/Tokyo", cfg.Server.Timezone)
	assert.Equal(t, "twilio", cfg.Provider)
	assert.Equal(t, "ACtest_account_sid", cfg.Twilio.AccountSid)
	assert.Equal(t, "success", cfg.Twilio.DefaultBehavior)
	assert.Len(t, cfg.Twilio.RegisteredNumbers, 1)
	assert.Equal(t, 1, cfg.Twilio.Callbacks.DelaySeconds)
}

func TestLoad_appliesDefaults(t *testing.T) {
	cfg, err := Load("testdata/minimal.yaml")
	require.NoError(t, err)

	assert.Equal(t, "0.0.0.0", cfg.Server.Host)
	assert.Equal(t, 8080, cfg.Server.Port)
	assert.Equal(t, "UTC", cfg.Server.Timezone)
	assert.Equal(t, "success", cfg.Twilio.DefaultBehavior)
	assert.True(t, cfg.Twilio.Validation.RequireAuth, "RequireAuth should default to true")
	assert.Equal(t, 3, cfg.Twilio.Callbacks.RetryAttempts)
}

func TestLoad_authDisabledAllowsPlaceholders(t *testing.T) {
	cfg, err := Load("testdata/auth_disabled.yaml")
	require.NoError(t, err)
	assert.False(t, cfg.Twilio.Validation.RequireAuth)
}

func TestLoad_errors(t *testing.T) {
	cases := []struct {
		name     string
		path     string
		contains string
	}{
		{"missing file", "testdata/does_not_exist.yaml", "config file not found"},
		{"bad provider", "testdata/bad_provider.yaml", "unsupported provider"},
		{"bad default_behavior", "testdata/bad_default_behavior.yaml", "default_behavior must be"},
		{"bad timezone", "testdata/bad_timezone.yaml", "invalid timezone"},
		{"placeholder credentials with auth required", "testdata/auth_required_placeholder.yaml", "account_sid must be set"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(tc.path)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.contains)
		})
	}
}

func TestLoad_emptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.yaml")
	require.NoError(t, os.WriteFile(path, []byte{}, 0o644))

	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestLoad_envVarFallback(t *testing.T) {
	t.Setenv("CONFIG_PATH", "testdata/valid.yaml")
	cfg, err := Load("")
	require.NoError(t, err)
	assert.Equal(t, "ACtest_account_sid", cfg.Twilio.AccountSid)
}
