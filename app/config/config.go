// Package config loads and validates the server configuration from YAML.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   Server   `yaml:"server"`
	Provider string   `yaml:"provider"`
	Twilio   Twilio   `yaml:"twilio"`
	Database Database `yaml:"database"`
}

type Server struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Timezone string `yaml:"timezone"`
}

type Twilio struct {
	AccountSid         string     `yaml:"account_sid"`
	AuthToken          string     `yaml:"auth_token"`
	Validation         Validation `yaml:"validation"`
	DefaultBehavior    string     `yaml:"default_behavior"`
	RegisteredNumbers  []string   `yaml:"registered_numbers"`
	AllowedFromNumbers []string   `yaml:"allowed_from_numbers"`
	FailureNumbers     []string   `yaml:"failure_numbers"`
	Callbacks          Callbacks  `yaml:"callbacks"`
}

type Validation struct {
	RequireAuth         bool `yaml:"require_auth"`
	ValidatePhoneFormat bool `yaml:"validate_phone_format"`
	CheckFromNumbers    bool `yaml:"check_from_numbers"`
	RequireParameters   bool `yaml:"require_parameters"`
}

type Callbacks struct {
	Enabled           bool `yaml:"enabled"`
	DelaySeconds      int  `yaml:"delay_seconds"`
	RetryAttempts     int  `yaml:"retry_attempts"`
	RetryDelaySeconds int  `yaml:"retry_delay_seconds"`
}

type Database struct {
	Path string `yaml:"path"`
}

// Load reads the YAML config at path, applies defaults, and validates it.
// If path is empty, the CONFIG_PATH env var is consulted, then "./config.yaml".
func Load(path string) (*Config, error) {
	if path == "" {
		path = os.Getenv("CONFIG_PATH")
	}
	if path == "" {
		path = "./config.yaml"
	}

	data, err := os.ReadFile(path) //nolint:gosec // G304: path is the operator-supplied config location, this function exists to read it
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("config file not found: %s", path)
		}
		return nil, fmt.Errorf("read config: %w", err)
	}
	if len(data) == 0 {
		return nil, errors.New("config file is empty")
	}

	cfg := defaults()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// SMS_MOCK_DB_PATH overrides database.path from config.yaml. This lets
	// docker-compose set a custom DB location without templating the config.
	if v := os.Getenv("SMS_MOCK_DB_PATH"); v != "" {
		cfg.Database.Path = v
	}

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
		Server: Server{Host: "0.0.0.0", Port: 8080, Timezone: "UTC"},
		Twilio: Twilio{
			DefaultBehavior: "success",
			Validation: Validation{
				RequireAuth:         true,
				ValidatePhoneFormat: true,
				CheckFromNumbers:    true,
				RequireParameters:   true,
			},
			Callbacks: Callbacks{
				Enabled:           true,
				DelaySeconds:      2,
				RetryAttempts:     3,
				RetryDelaySeconds: 5,
			},
		},
		Database: Database{Path: "/tmp/mock_server.db"},
	}
}

func (c *Config) validate() error {
	if c.Provider == "" {
		c.Provider = "twilio"
	}
	if c.Provider != "twilio" {
		return fmt.Errorf("unsupported provider: %q (only 'twilio' is supported)", c.Provider)
	}

	if c.Twilio.DefaultBehavior != "success" && c.Twilio.DefaultBehavior != "failure" {
		return fmt.Errorf("default_behavior must be 'success' or 'failure', got: %q", c.Twilio.DefaultBehavior)
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
