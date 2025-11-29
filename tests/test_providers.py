"""Tests for providers module."""
from app.config import TwilioConfig
from app.providers.twilio import TwilioProvider


class TestTwilioProviderInitialization:
    """Tests for TwilioProvider initialization."""

    def test_init_with_config(self):
        """Test TwilioProvider initialization with config."""
        config = TwilioConfig({
            "account_sid": "AC123",
            "auth_token": "token123",
        })

        provider = TwilioProvider(config)
        assert provider.config == config


class TestValidateAuth:
    """Tests for validate_auth method."""

    def test_validate_auth_success(self):
        """Test successful authentication."""
        config = TwilioConfig({
            "account_sid": "AC123",
            "auth_token": "token123",
            "validation": {"require_auth": True},
        })

        provider = TwilioProvider(config)
        is_valid, error = provider.validate_auth("AC123", "token123")

        assert is_valid is True
        assert error is None

    def test_validate_auth_wrong_credentials(self):
        """Test authentication with wrong credentials."""
        config = TwilioConfig({
            "account_sid": "AC123",
            "auth_token": "token123",
            "validation": {"require_auth": True},
        })

        provider = TwilioProvider(config)
        is_valid, error = provider.validate_auth("WRONG", "wrong")

        assert is_valid is False
        assert error["error_type"] == "auth_failed"
        assert error["http_status"] == 401

    def test_validate_auth_missing_username(self):
        """Test authentication with missing username."""
        config = TwilioConfig({
            "account_sid": "AC123",
            "auth_token": "token123",
            "validation": {"require_auth": True},
        })

        provider = TwilioProvider(config)
        is_valid, error = provider.validate_auth(None, "token123")

        assert is_valid is False
        assert error["error_type"] == "auth_failed"
        assert error["http_status"] == 401

    def test_validate_auth_missing_password(self):
        """Test authentication with missing password."""
        config = TwilioConfig({
            "account_sid": "AC123",
            "auth_token": "token123",
            "validation": {"require_auth": True},
        })

        provider = TwilioProvider(config)
        is_valid, error = provider.validate_auth("AC123", None)

        assert is_valid is False
        assert error["error_type"] == "auth_failed"
        assert error["http_status"] == 401

    def test_validate_auth_disabled(self):
        """Test authentication when auth is disabled."""
        config = TwilioConfig({
            "account_sid": "AC123",
            "auth_token": "token123",
            "validation": {"require_auth": False},
        })

        provider = TwilioProvider(config)
        is_valid, error = provider.validate_auth(None, None)

        assert is_valid is True
        assert error is None


class TestValidateParameters:
    """Tests for validate_parameters method."""

    def test_validate_parameters_success(self):
        """Test successful parameter validation."""
        config = TwilioConfig({"validation": {"require_parameters": True}})
        provider = TwilioProvider(config)

        request_data = {
            "To": "+1234567890",
            "From": "+0987654321",
            "Body": "Test message",
        }

        is_valid, error = provider.validate_parameters(
            request_data, ["To", "From", "Body"]
        )

        assert is_valid is True
        assert error is None

    def test_validate_parameters_missing_param(self):
        """Test parameter validation with missing parameter."""
        config = TwilioConfig({"validation": {"require_parameters": True}})
        provider = TwilioProvider(config)

        request_data = {
            "To": "+1234567890",
            "Body": "Test message",
        }

        is_valid, error = provider.validate_parameters(
            request_data, ["To", "From", "Body"]
        )

        assert is_valid is False
        assert error["error_type"] == "missing_parameter"
        assert error["http_status"] == 400
        assert error["parameter"] == "From"

    def test_validate_parameters_empty_value(self):
        """Test parameter validation with empty value."""
        config = TwilioConfig({"validation": {"require_parameters": True}})
        provider = TwilioProvider(config)

        request_data = {
            "To": "+1234567890",
            "From": "",
            "Body": "Test message",
        }

        is_valid, error = provider.validate_parameters(
            request_data, ["To", "From", "Body"]
        )

        assert is_valid is False
        assert error["error_type"] == "missing_parameter"
        assert error["parameter"] == "From"

    def test_validate_parameters_disabled(self):
        """Test parameter validation when disabled."""
        config = TwilioConfig({"validation": {"require_parameters": False}})
        provider = TwilioProvider(config)

        request_data = {}

        is_valid, error = provider.validate_parameters(
            request_data, ["To", "From", "Body"]
        )

        assert is_valid is True
        assert error is None


class TestValidatePhoneNumber:
    """Tests for validate_phone_number method."""

    def test_validate_phone_number_valid_e164(self):
        """Test validation with valid E.164 phone number."""
        config = TwilioConfig({"validation": {"validate_phone_format": True}})
        provider = TwilioProvider(config)

        is_valid, error = provider.validate_phone_number("+12125551234", "To")

        assert is_valid is True
        assert error is None

    def test_validate_phone_number_invalid_format(self):
        """Test validation with invalid phone number format."""
        config = TwilioConfig({"validation": {"validate_phone_format": True}})
        provider = TwilioProvider(config)

        is_valid, error = provider.validate_phone_number("123", "To")

        assert is_valid is False
        assert error["error_type"] == "invalid_phone_number"
        assert error["http_status"] == 400
        assert error["field"] == "To"
        assert error["number"] == "123"

    def test_validate_phone_number_non_e164(self):
        """Test validation with non-E.164 format."""
        config = TwilioConfig({"validation": {"validate_phone_format": True}})
        provider = TwilioProvider(config)

        is_valid, error = provider.validate_phone_number("(212) 555-1234", "From")

        assert is_valid is False
        assert error["error_type"] == "invalid_phone_number"
        assert error["field"] == "From"

    def test_validate_phone_number_disabled(self):
        """Test validation when phone format validation is disabled."""
        config = TwilioConfig({"validation": {"validate_phone_format": False}})
        provider = TwilioProvider(config)

        is_valid, error = provider.validate_phone_number("invalid", "To")

        assert is_valid is True
        assert error is None


class TestValidateFromNumber:
    """Tests for validate_from_number method."""

    def test_validate_from_number_in_allowed_list(self):
        """Test validation with number in allowed list."""
        config = TwilioConfig({
            "validation": {"check_from_numbers": True},
            "allowed_from_numbers": ["+12125551234", "+12125555678"],
        })
        provider = TwilioProvider(config)

        is_valid, error = provider.validate_from_number("+12125551234")

        assert is_valid is True
        assert error is None

    def test_validate_from_number_not_in_allowed_list(self):
        """Test validation with number not in allowed list."""
        config = TwilioConfig({
            "validation": {"check_from_numbers": True},
            "allowed_from_numbers": ["+12125551234"],
        })
        provider = TwilioProvider(config)

        is_valid, error = provider.validate_from_number("+19995551234")

        assert is_valid is False
        assert error["error_type"] == "invalid_from_number"
        assert error["http_status"] == 400
        assert error["from_number"] == "+19995551234"

    def test_validate_from_number_disabled(self):
        """Test validation when from number check is disabled."""
        config = TwilioConfig({
            "validation": {"check_from_numbers": False},
            "allowed_from_numbers": [],
        })
        provider = TwilioProvider(config)

        is_valid, error = provider.validate_from_number("+19995551234")

        assert is_valid is True
        assert error is None


class TestShouldSucceed:
    """Tests for should_succeed method."""

    def test_should_succeed_in_failure_list(self):
        """Test should_succeed with number in failure list."""
        config = TwilioConfig({
            "default_behavior": "success",
            "registered_numbers": ["+11111111111"],
            "failure_numbers": ["+12222222222"],
        })
        provider = TwilioProvider(config)

        # Failure numbers have highest priority
        assert provider.should_succeed("+12222222222") is False

    def test_should_succeed_in_registered_list(self):
        """Test should_succeed with number in registered list."""
        config = TwilioConfig({
            "default_behavior": "failure",
            "registered_numbers": ["+11111111111"],
            "failure_numbers": [],
        })
        provider = TwilioProvider(config)

        assert provider.should_succeed("+11111111111") is True

    def test_should_succeed_default_success(self):
        """Test should_succeed with unknown number and default_behavior=success."""
        config = TwilioConfig({
            "default_behavior": "success",
            "registered_numbers": [],
            "failure_numbers": [],
        })
        provider = TwilioProvider(config)

        assert provider.should_succeed("+19995551234") is True

    def test_should_succeed_default_failure(self):
        """Test should_succeed with unknown number and default_behavior=failure."""
        config = TwilioConfig({
            "default_behavior": "failure",
            "registered_numbers": [],
            "failure_numbers": [],
        })
        provider = TwilioProvider(config)

        assert provider.should_succeed("+19995551234") is False

    def test_should_succeed_priority_failure_over_registered(self):
        """Test that failure list takes priority over registered list."""
        config = TwilioConfig({
            "default_behavior": "success",
            "registered_numbers": ["+11111111111"],
            "failure_numbers": ["+11111111111"],
        })
        provider = TwilioProvider(config)

        # Failure should win
        assert provider.should_succeed("+11111111111") is False


class TestGetResponseTemplate:
    """Tests for get_response_template method."""

    def test_get_response_template_success(self):
        """Test getting response template for success."""
        config = TwilioConfig({})
        provider = TwilioProvider(config)

        template = provider.get_response_template("send_sms", success=True)
        assert template == "send_sms_success.json"

    def test_get_response_template_failure(self):
        """Test getting response template for failure."""
        config = TwilioConfig({})
        provider = TwilioProvider(config)

        template = provider.get_response_template("make_call", success=False)
        assert template == "make_call_failure.json"


class TestGetErrorTemplate:
    """Tests for get_error_template method."""

    def test_get_error_template(self):
        """Test getting error template."""
        config = TwilioConfig({})
        provider = TwilioProvider(config)

        template = provider.get_error_template("auth_failed")
        assert template == "auth_failed.json"

    def test_get_error_template_missing_parameter(self):
        """Test getting error template for missing parameter."""
        config = TwilioConfig({})
        provider = TwilioProvider(config)

        template = provider.get_error_template("missing_parameter")
        assert template == "missing_parameter.json"
