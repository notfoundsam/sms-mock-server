"""Configuration loader and validator for SMS Mock Server."""
import os
from pathlib import Path
from typing import Any

import yaml


class ConfigurationError(Exception):
    """Raised when configuration is invalid."""
    pass


class ServerConfig:
    """Server configuration."""

    def __init__(self, data: dict[str, Any]):
        self.host: str = data.get("host", "0.0.0.0")
        self.port: int = data.get("port", 8080)
        self.timezone: str = data.get("timezone", "UTC")


class ValidationConfig:
    """Validation settings configuration."""

    def __init__(self, data: dict[str, Any]):
        self.require_auth: bool = data.get("require_auth", True)
        self.validate_phone_format: bool = data.get("validate_phone_format", True)
        self.check_from_numbers: bool = data.get("check_from_numbers", True)
        self.require_parameters: bool = data.get("require_parameters", True)


class CallbackConfig:
    """Callback settings configuration."""

    def __init__(self, data: dict[str, Any]):
        self.enabled: bool = data.get("enabled", True)
        self.delay_seconds: int = data.get("delay_seconds", 2)
        self.retry_attempts: int = data.get("retry_attempts", 3)
        self.retry_delay_seconds: int = data.get("retry_delay_seconds", 5)


class TwilioConfig:
    """Twilio provider configuration."""

    def __init__(self, data: dict[str, Any]):
        self.account_sid: str = data.get("account_sid", "")
        self.auth_token: str = data.get("auth_token", "")

        # Validation settings
        validation_data = data.get("validation", {})
        self.validation = ValidationConfig(validation_data)

        # Number behavior
        self.default_behavior: str = data.get("default_behavior", "success")
        if self.default_behavior not in ["success", "failure"]:
            raise ConfigurationError(
                f"default_behavior must be 'success' or 'failure', got: {self.default_behavior}"
            )

        # Number lists
        self.registered_numbers: list[str] = data.get("registered_numbers", [])
        self.allowed_from_numbers: list[str] = data.get("allowed_from_numbers", [])
        self.failure_numbers: list[str] = data.get("failure_numbers", [])

        # Callbacks
        callback_data = data.get("callbacks", {})
        self.callbacks = CallbackConfig(callback_data)

    def validate(self) -> None:
        """Validate Twilio configuration."""
        if self.validation.require_auth:
            if not self.account_sid or self.account_sid == "ACXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX":
                raise ConfigurationError(
                    "Twilio account_sid must be set when require_auth is enabled"
                )
            if not self.auth_token or self.auth_token == "your_auth_token_here":
                raise ConfigurationError(
                    "Twilio auth_token must be set when require_auth is enabled"
                )


class DatabaseConfig:
    """Database configuration."""

    def __init__(self, data: dict[str, Any]):
        self.path: str = data.get("path", "./data/mock_server.db")


class TemplatesConfig:
    """Templates configuration."""

    def __init__(self, data: dict[str, Any]):
        self.path: str = data.get("path", "./templates/responses")


class Config:
    """Main configuration class."""

    def __init__(self, config_path: str | None = None):
        """Load configuration from YAML file.

        Args:
            config_path: Path to config file. Defaults to ./config.yaml
        """
        if config_path is None:
            config_path = os.getenv("CONFIG_PATH", "./config.yaml")

        self.config_path = Path(config_path)
        if not self.config_path.exists():
            raise ConfigurationError(f"Config file not found: {config_path}")

        with open(self.config_path, "r") as f:
            data = yaml.safe_load(f)

        if not data:
            raise ConfigurationError("Config file is empty")

        # Load sections
        self.server = ServerConfig(data.get("server", {}))
        self.database = DatabaseConfig(data.get("database", {}))
        self.templates = TemplatesConfig(data.get("templates", {}))

        # Provider
        self.provider: str = data.get("provider", "twilio")
        if self.provider != "twilio":
            raise ConfigurationError(
                f"Unsupported provider: {self.provider}. Only 'twilio' is supported."
            )

        # Provider-specific config
        twilio_data = data.get("twilio", {})
        if not twilio_data:
            raise ConfigurationError("Twilio configuration section is missing")

        self.twilio = TwilioConfig(twilio_data)

        # Validate configuration
        self.validate()

    def validate(self) -> None:
        """Validate entire configuration."""
        self.twilio.validate()

        # Ensure database directory exists
        db_path = Path(self.database.path)
        db_path.parent.mkdir(parents=True, exist_ok=True)

        # Ensure templates directory exists
        templates_path = Path(self.templates.path)
        if not templates_path.exists():
            raise ConfigurationError(
                f"Templates directory not found: {self.templates.path}"
            )


def load_config(config_path: str | None = None) -> Config:
    """Load and return configuration.

    Args:
        config_path: Optional path to config file

    Returns:
        Config object

    Raises:
        ConfigurationError: If configuration is invalid
    """
    return Config(config_path)
