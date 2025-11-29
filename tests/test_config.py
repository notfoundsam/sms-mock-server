"""Tests for configuration module."""
import os
import tempfile
from pathlib import Path

import pytest
import yaml

from app.config import (
    CallbackConfig,
    Config,
    ConfigurationError,
    DatabaseConfig,
    ServerConfig,
    TemplatesConfig,
    TwilioConfig,
    ValidationConfig,
    load_config,
)


class TestServerConfig:
    """Tests for ServerConfig class."""

    def test_default_values(self):
        """Test ServerConfig with empty dict uses defaults."""
        config = ServerConfig({})
        assert config.host == "0.0.0.0"
        assert config.port == 8080
        assert config.timezone == "UTC"

    def test_custom_values(self):
        """Test ServerConfig with custom values."""
        data = {"host": "127.0.0.1", "port": 9000, "timezone": "America/New_York"}
        config = ServerConfig(data)
        assert config.host == "127.0.0.1"
        assert config.port == 9000
        assert config.timezone == "America/New_York"

    def test_partial_values(self):
        """Test ServerConfig with partial custom values."""
        data = {"port": 3000}
        config = ServerConfig(data)
        assert config.host == "0.0.0.0"
        assert config.port == 3000
        assert config.timezone == "UTC"

    def test_custom_timezone_only(self):
        """Test ServerConfig with only timezone set."""
        data = {"timezone": "Asia/Tokyo"}
        config = ServerConfig(data)
        assert config.host == "0.0.0.0"
        assert config.port == 8080
        assert config.timezone == "Asia/Tokyo"


class TestValidationConfig:
    """Tests for ValidationConfig class."""

    def test_default_values(self):
        """Test ValidationConfig with empty dict uses defaults."""
        config = ValidationConfig({})
        assert config.require_auth is True
        assert config.validate_phone_format is True
        assert config.check_from_numbers is True
        assert config.require_parameters is True

    def test_custom_values(self):
        """Test ValidationConfig with all custom values."""
        data = {
            "require_auth": False,
            "validate_phone_format": False,
            "check_from_numbers": False,
            "require_parameters": False,
        }
        config = ValidationConfig(data)
        assert config.require_auth is False
        assert config.validate_phone_format is False
        assert config.check_from_numbers is False
        assert config.require_parameters is False


class TestCallbackConfig:
    """Tests for CallbackConfig class."""

    def test_default_values(self):
        """Test CallbackConfig with empty dict uses defaults."""
        config = CallbackConfig({})
        assert config.enabled is True
        assert config.delay_seconds == 2
        assert config.retry_attempts == 3
        assert config.retry_delay_seconds == 5

    def test_custom_values(self):
        """Test CallbackConfig with custom values."""
        data = {
            "enabled": False,
            "delay_seconds": 10,
            "retry_attempts": 5,
            "retry_delay_seconds": 15,
        }
        config = CallbackConfig(data)
        assert config.enabled is False
        assert config.delay_seconds == 10
        assert config.retry_attempts == 5
        assert config.retry_delay_seconds == 15


class TestDatabaseConfig:
    """Tests for DatabaseConfig class."""

    def test_default_path(self):
        """Test DatabaseConfig with empty dict uses default path."""
        config = DatabaseConfig({})
        assert config.path == "./data/mock_server.db"

    def test_custom_path(self):
        """Test DatabaseConfig with custom path."""
        data = {"path": "/custom/path/db.sqlite"}
        config = DatabaseConfig(data)
        assert config.path == "/custom/path/db.sqlite"


class TestTemplatesConfig:
    """Tests for TemplatesConfig class."""

    def test_default_path(self):
        """Test TemplatesConfig with empty dict uses default path."""
        config = TemplatesConfig({})
        assert config.path == "./templates/responses"

    def test_custom_path(self):
        """Test TemplatesConfig with custom path."""
        data = {"path": "/custom/templates"}
        config = TemplatesConfig(data)
        assert config.path == "/custom/templates"


class TestTwilioConfig:
    """Tests for TwilioConfig class."""

    def test_default_values(self):
        """Test TwilioConfig with minimal data."""
        data = {}
        config = TwilioConfig(data)
        assert config.account_sid == ""
        assert config.auth_token == ""
        assert config.default_behavior == "success"
        assert config.registered_numbers == []
        assert config.allowed_from_numbers == []
        assert config.failure_numbers == []
        assert config.validation.require_auth is True
        assert config.callbacks.enabled is True

    def test_custom_values(self):
        """Test TwilioConfig with custom values."""
        data = {
            "account_sid": "AC123456",
            "auth_token": "token123",
            "default_behavior": "failure",
            "registered_numbers": ["+1234567890"],
            "allowed_from_numbers": ["+0987654321"],
            "failure_numbers": ["+1111111111"],
            "validation": {"require_auth": False},
            "callbacks": {"enabled": False},
        }
        config = TwilioConfig(data)
        assert config.account_sid == "AC123456"
        assert config.auth_token == "token123"
        assert config.default_behavior == "failure"
        assert config.registered_numbers == ["+1234567890"]
        assert config.allowed_from_numbers == ["+0987654321"]
        assert config.failure_numbers == ["+1111111111"]
        assert config.validation.require_auth is False
        assert config.callbacks.enabled is False

    def test_invalid_default_behavior(self):
        """Test TwilioConfig raises error for invalid default_behavior."""
        data = {"default_behavior": "invalid"}
        with pytest.raises(ConfigurationError) as exc:
            TwilioConfig(data)
        assert "default_behavior must be 'success' or 'failure'" in str(exc.value)

    def test_validate_success_with_auth(self):
        """Test TwilioConfig.validate() passes with valid auth credentials."""
        data = {
            "account_sid": "AC" + "x" * 32,
            "auth_token": "valid_token",
            "validation": {"require_auth": True},
        }
        config = TwilioConfig(data)
        config.validate()

    def test_validate_fails_with_missing_account_sid(self):
        """Test TwilioConfig.validate() fails when account_sid is missing."""
        data = {
            "account_sid": "",
            "auth_token": "valid_token",
            "validation": {"require_auth": True},
        }
        config = TwilioConfig(data)
        with pytest.raises(ConfigurationError) as exc:
            config.validate()
        assert "account_sid must be set" in str(exc.value)

    def test_validate_fails_with_placeholder_account_sid(self):
        """Test TwilioConfig.validate() fails when account_sid is placeholder."""
        data = {
            "account_sid": "ACXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
            "auth_token": "valid_token",
            "validation": {"require_auth": True},
        }
        config = TwilioConfig(data)
        with pytest.raises(ConfigurationError) as exc:
            config.validate()
        assert "account_sid must be set" in str(exc.value)

    def test_validate_fails_with_missing_auth_token(self):
        """Test TwilioConfig.validate() fails when auth_token is missing."""
        data = {
            "account_sid": "AC" + "x" * 32,
            "auth_token": "",
            "validation": {"require_auth": True},
        }
        config = TwilioConfig(data)
        with pytest.raises(ConfigurationError) as exc:
            config.validate()
        assert "auth_token must be set" in str(exc.value)

    def test_validate_fails_with_placeholder_auth_token(self):
        """Test TwilioConfig.validate() fails when auth_token is placeholder."""
        data = {
            "account_sid": "AC" + "x" * 32,
            "auth_token": "your_auth_token_here",
            "validation": {"require_auth": True},
        }
        config = TwilioConfig(data)
        with pytest.raises(ConfigurationError) as exc:
            config.validate()
        assert "auth_token must be set" in str(exc.value)

    def test_validate_skips_when_auth_not_required(self):
        """Test TwilioConfig.validate() passes when require_auth is False."""
        data = {
            "account_sid": "",
            "auth_token": "",
            "validation": {"require_auth": False},
        }
        config = TwilioConfig(data)
        config.validate()


class TestConfig:
    """Tests for main Config class."""

    @pytest.fixture
    def valid_config_file(self, tmp_path):
        """Create a valid temporary config file."""
        config_data = {
            "server": {"host": "0.0.0.0", "port": 8080},
            "database": {"path": str(tmp_path / "test.db")},
            "templates": {"path": str(tmp_path / "templates")},
            "provider": "twilio",
            "twilio": {
                "account_sid": "AC" + "x" * 32,
                "auth_token": "test_token",
                "validation": {"require_auth": True},
            },
        }

        # Create templates directory
        templates_dir = tmp_path / "templates"
        templates_dir.mkdir()

        config_file = tmp_path / "config.yaml"
        with open(config_file, "w") as f:
            yaml.dump(config_data, f)

        return str(config_file)

    def test_load_valid_config(self, valid_config_file):
        """Test loading a valid configuration file."""
        config = Config(valid_config_file)
        assert config.server.host == "0.0.0.0"
        assert config.server.port == 8080
        assert config.provider == "twilio"
        assert config.twilio.account_sid == "AC" + "x" * 32

    def test_config_file_not_found(self):
        """Test Config raises error when file doesn't exist."""
        with pytest.raises(ConfigurationError) as exc:
            Config("/nonexistent/config.yaml")
        assert "Config file not found" in str(exc.value)

    def test_empty_config_file(self, tmp_path):
        """Test Config raises error when file is empty."""
        config_file = tmp_path / "empty.yaml"
        config_file.write_text("")

        with pytest.raises(ConfigurationError) as exc:
            Config(str(config_file))
        assert "Config file is empty" in str(exc.value)

    def test_unsupported_provider(self, tmp_path):
        """Test Config raises error for unsupported provider."""
        config_data = {
            "server": {},
            "provider": "nexmo",
            "twilio": {"account_sid": "test", "auth_token": "test"},
        }

        templates_dir = tmp_path / "templates"
        templates_dir.mkdir()

        config_file = tmp_path / "config.yaml"
        with open(config_file, "w") as f:
            yaml.dump(config_data, f)

        with pytest.raises(ConfigurationError) as exc:
            Config(str(config_file))
        assert "Unsupported provider" in str(exc.value)
        assert "Only 'twilio' is supported" in str(exc.value)

    def test_missing_twilio_section(self, tmp_path):
        """Test Config raises error when Twilio section is missing."""
        config_data = {
            "server": {},
            "provider": "twilio",
        }

        config_file = tmp_path / "config.yaml"
        with open(config_file, "w") as f:
            yaml.dump(config_data, f)

        with pytest.raises(ConfigurationError) as exc:
            Config(str(config_file))
        assert "Twilio configuration section is missing" in str(exc.value)

    def test_templates_directory_not_found(self, tmp_path):
        """Test Config raises error when templates directory doesn't exist."""
        config_data = {
            "server": {},
            "database": {"path": str(tmp_path / "test.db")},
            "templates": {"path": "/nonexistent/templates"},
            "provider": "twilio",
            "twilio": {
                "account_sid": "AC" + "x" * 32,
                "auth_token": "test_token",
                "validation": {"require_auth": True},
            },
        }

        config_file = tmp_path / "config.yaml"
        with open(config_file, "w") as f:
            yaml.dump(config_data, f)

        with pytest.raises(ConfigurationError) as exc:
            Config(str(config_file))
        assert "Templates directory not found" in str(exc.value)

    def test_database_directory_created(self, tmp_path):
        """Test Config creates database directory if it doesn't exist."""
        db_dir = tmp_path / "data" / "nested"
        templates_dir = tmp_path / "templates"
        templates_dir.mkdir()

        config_data = {
            "server": {},
            "database": {"path": str(db_dir / "test.db")},
            "templates": {"path": str(templates_dir)},
            "provider": "twilio",
            "twilio": {
                "account_sid": "AC" + "x" * 32,
                "auth_token": "test_token",
                "validation": {"require_auth": True},
            },
        }

        config_file = tmp_path / "config.yaml"
        with open(config_file, "w") as f:
            yaml.dump(config_data, f)

        config = Config(str(config_file))
        assert db_dir.exists()

    def test_config_from_env_variable(self, tmp_path, monkeypatch):
        """Test Config uses CONFIG_PATH environment variable."""
        config_data = {
            "server": {},
            "database": {"path": str(tmp_path / "test.db")},
            "templates": {"path": str(tmp_path / "templates")},
            "provider": "twilio",
            "twilio": {
                "account_sid": "AC" + "x" * 32,
                "auth_token": "test_token",
                "validation": {"require_auth": True},
            },
        }

        templates_dir = tmp_path / "templates"
        templates_dir.mkdir()

        config_file = tmp_path / "env_config.yaml"
        with open(config_file, "w") as f:
            yaml.dump(config_data, f)

        monkeypatch.setenv("CONFIG_PATH", str(config_file))
        config = Config()
        assert config.twilio.account_sid == "AC" + "x" * 32


class TestLoadConfig:
    """Tests for load_config function."""

    def test_load_config_wrapper(self, tmp_path):
        """Test load_config function returns Config object."""
        config_data = {
            "server": {},
            "database": {"path": str(tmp_path / "test.db")},
            "templates": {"path": str(tmp_path / "templates")},
            "provider": "twilio",
            "twilio": {
                "account_sid": "AC" + "x" * 32,
                "auth_token": "test_token",
                "validation": {"require_auth": True},
            },
        }

        templates_dir = tmp_path / "templates"
        templates_dir.mkdir()

        config_file = tmp_path / "config.yaml"
        with open(config_file, "w") as f:
            yaml.dump(config_data, f)

        config = load_config(str(config_file))
        assert isinstance(config, Config)
        assert config.provider == "twilio"
