// Package config loads and validates the server configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Server   Server
	Provider string
	Twilio   Twilio
	Database Database
}

type Server struct {
	Host     string
	Port     int
	Timezone string
}

type Twilio struct {
	AccountSid         string
	AuthToken          string
	Validation         Validation
	SuccessNumbers     []string
	AllowedFromNumbers []string
	FailureNumbers     []string
	Callbacks          Callbacks
}

type Validation struct {
	RequireAuth         bool
	ValidatePhoneFormat bool
	CheckFromNumbers    bool
	RequireParameters   bool
}

type Callbacks struct {
	DelaySeconds      int
	RetryAttempts     int
	RetryDelaySeconds int
}

type Database struct {
	Path string
}

// Load builds the configuration from environment variables, applying defaults
// for any unset values, and validates the result.
func Load() (*Config, error) {
	cfg := defaults()
	applyEnv(cfg)

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(cfg.Database.Path), 0o750); err != nil {
		return nil, fmt.Errorf("create database dir: %w", err)
	}

	return cfg, nil
}

func defaults() *Config {
	return &Config{
		Server:   Server{Host: "0.0.0.0", Port: 8080, Timezone: "UTC"},
		Provider: "twilio",
		Twilio: Twilio{
			Validation: Validation{
				RequireAuth:         true,
				ValidatePhoneFormat: true,
				CheckFromNumbers:    true,
				RequireParameters:   true,
			},
			Callbacks: Callbacks{
				DelaySeconds:      2,
				RetryAttempts:     3,
				RetryDelaySeconds: 5,
			},
		},
		Database: Database{Path: "/tmp/mock_server.db"},
	}
}

func applyEnv(cfg *Config) {
	cfg.Server.Host = getStr("SMS_MOCK_HOST", cfg.Server.Host)
	cfg.Server.Port = getInt("SMS_MOCK_PORT", cfg.Server.Port)
	cfg.Server.Timezone = getStr("SMS_MOCK_TIMEZONE", cfg.Server.Timezone)
	cfg.Database.Path = getStr("SMS_MOCK_DB_PATH", cfg.Database.Path)
	cfg.Provider = getStr("SMS_MOCK_PROVIDER", cfg.Provider)

	cfg.Twilio.AccountSid = getStr("SMS_MOCK_TWILIO_ACCOUNT_SID", cfg.Twilio.AccountSid)
	cfg.Twilio.AuthToken = getStr("SMS_MOCK_TWILIO_AUTH_TOKEN", cfg.Twilio.AuthToken)
	cfg.Twilio.AllowedFromNumbers = getList("SMS_MOCK_TWILIO_ALLOWED_FROM_NUMBERS")
	cfg.Twilio.SuccessNumbers = getList("SMS_MOCK_TWILIO_SUCCESS_NUMBERS")
	cfg.Twilio.FailureNumbers = getList("SMS_MOCK_TWILIO_FAILURE_NUMBERS")

	cfg.Twilio.Validation.RequireAuth = getBool("SMS_MOCK_TWILIO_REQUIRE_AUTH", cfg.Twilio.Validation.RequireAuth)
	cfg.Twilio.Validation.ValidatePhoneFormat = getBool("SMS_MOCK_TWILIO_VALIDATE_PHONE_FORMAT", cfg.Twilio.Validation.ValidatePhoneFormat)
	cfg.Twilio.Validation.CheckFromNumbers = getBool("SMS_MOCK_TWILIO_CHECK_FROM_NUMBERS", cfg.Twilio.Validation.CheckFromNumbers)
	cfg.Twilio.Validation.RequireParameters = getBool("SMS_MOCK_TWILIO_REQUIRE_PARAMETERS", cfg.Twilio.Validation.RequireParameters)

	cfg.Twilio.Callbacks.DelaySeconds = getInt("SMS_MOCK_TWILIO_CALLBACK_DELAY_SECONDS", cfg.Twilio.Callbacks.DelaySeconds)
	cfg.Twilio.Callbacks.RetryAttempts = getInt("SMS_MOCK_TWILIO_CALLBACK_RETRY_ATTEMPTS", cfg.Twilio.Callbacks.RetryAttempts)
	cfg.Twilio.Callbacks.RetryDelaySeconds = getInt("SMS_MOCK_TWILIO_CALLBACK_RETRY_DELAY_SECONDS", cfg.Twilio.Callbacks.RetryDelaySeconds)
}

func getStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func getBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func getList(key string) []string {
	v := os.Getenv(key)
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (c *Config) validate() error {
	if c.Provider != "twilio" {
		return fmt.Errorf("unsupported provider: %q (only 'twilio' is supported)", c.Provider)
	}

	if c.Twilio.Validation.RequireAuth {
		if c.Twilio.AccountSid == "" || c.Twilio.AccountSid == "ACXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX" {
			return errors.New("twilio account_sid must be set when require_auth is enabled")
		}
		if c.Twilio.AuthToken == "" || c.Twilio.AuthToken == "your_auth_token_here" {
			return errors.New("twilio auth_token must be set when require_auth is enabled")
		}
	}

	if _, err := time.LoadLocation(c.Server.Timezone); err != nil {
		return fmt.Errorf("invalid timezone %q: %w", c.Server.Timezone, err)
	}

	if c.Database.Path == "" {
		return errors.New("database.path must not be empty")
	}

	return nil
}
