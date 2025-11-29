"""Twilio provider implementation for SMS Mock Server."""
from typing import Any

import phonenumbers

from app.config import TwilioConfig
from app.providers.base import BaseProvider


class TwilioProvider(BaseProvider):
    """Twilio provider implementation."""

    def __init__(self, config: TwilioConfig):
        """Initialize Twilio provider.

        Args:
            config: Twilio configuration
        """
        self.config = config

    def send_sms(self, request_data: dict[str, Any]) -> dict[str, Any]:
        """Process SMS sending request (implementation in main app)."""
        # This is handled by the API layer
        pass

    def make_call(self, request_data: dict[str, Any]) -> dict[str, Any]:
        """Process call making request (implementation in main app)."""
        # This is handled by the API layer
        pass

    def validate_auth(
        self, username: str | None, password: str | None
    ) -> tuple[bool, dict[str, Any] | None]:
        """Validate authentication credentials.

        Args:
            username: Account SID from Basic Auth
            password: Auth token from Basic Auth

        Returns:
            Tuple of (is_valid, error_response)
        """
        if not self.config.validation.require_auth:
            return True, None

        if not username or not password:
            return False, {
                "error_type": "auth_failed",
                "http_status": 401,
            }

        if username != self.config.account_sid or password != self.config.auth_token:
            return False, {
                "error_type": "auth_failed",
                "http_status": 401,
            }

        return True, None

    def validate_parameters(
        self, request_data: dict[str, Any], required_params: list
    ) -> tuple[bool, dict[str, Any] | None]:
        """Validate required parameters are present.

        Args:
            request_data: Request parameters
            required_params: List of required parameter names

        Returns:
            Tuple of (is_valid, error_response)
        """
        if not self.config.validation.require_parameters:
            return True, None

        for param in required_params:
            if param not in request_data or not request_data[param]:
                return False, {
                    "error_type": "missing_parameter",
                    "http_status": 400,
                    "parameter": param,
                }

        return True, None

    def validate_phone_number(
        self, number: str, field_name: str
    ) -> tuple[bool, dict[str, Any] | None]:
        """Validate phone number format (E.164).

        Args:
            number: Phone number to validate
            field_name: Field name ('To', 'From', etc.)

        Returns:
            Tuple of (is_valid, error_response)
        """
        if not self.config.validation.validate_phone_format:
            return True, None

        try:
            parsed = phonenumbers.parse(number, None)
            if not phonenumbers.is_valid_number(parsed):
                return False, {
                    "error_type": "invalid_phone_number",
                    "http_status": 400,
                    "field": field_name,
                    "number": number,
                }
        except phonenumbers.NumberParseException:
            return False, {
                "error_type": "invalid_phone_number",
                "http_status": 400,
                "field": field_name,
                "number": number,
            }

        return True, None

    def validate_from_number(
        self, number: str
    ) -> tuple[bool, dict[str, Any] | None]:
        """Validate From number is in allowed list.

        Args:
            number: From phone number

        Returns:
            Tuple of (is_valid, error_response)
        """
        if not self.config.validation.check_from_numbers:
            return True, None

        if number not in self.config.allowed_from_numbers:
            return False, {
                "error_type": "invalid_from_number",
                "http_status": 400,
                "from_number": number,
            }

        return True, None

    def should_succeed(self, to_number: str) -> bool:
        """Determine if message/call should succeed based on To number.

        Priority:
        1. If in failure_numbers -> False
        2. If in registered_numbers -> True
        3. Otherwise -> use default_behavior

        Args:
            to_number: Destination phone number

        Returns:
            True if should succeed, False otherwise
        """
        # Check failure list first (highest priority)
        if to_number in self.config.failure_numbers:
            return False

        # Check registered list second
        if to_number in self.config.registered_numbers:
            return True

        # Fall back to default behavior
        return self.config.default_behavior == "success"

    def get_response_template(self, action: str, success: bool) -> str:
        """Get template name for response.

        Args:
            action: Action type ('send_sms', 'make_call', etc.)
            success: Whether operation succeeded

        Returns:
            Template filename
        """
        status = "success" if success else "failure"
        return f"{action}_{status}.json"

    def get_error_template(self, error_type: str) -> str:
        """Get template name for error response.

        Args:
            error_type: Error type

        Returns:
            Template filename
        """
        return f"{error_type}.json"
