"""Base provider interface for SMS Mock Server."""
from abc import ABC, abstractmethod
from typing import Dict, Any, Tuple, Optional


class BaseProvider(ABC):
    """Abstract base class for provider implementations."""

    @abstractmethod
    def send_sms(self, request_data: Dict[str, Any]) -> Dict[str, Any]:
        """Process SMS sending request.

        Args:
            request_data: Request parameters

        Returns:
            Response data dict
        """
        pass

    @abstractmethod
    def make_call(self, request_data: Dict[str, Any]) -> Dict[str, Any]:
        """Process call making request.

        Args:
            request_data: Request parameters

        Returns:
            Response data dict
        """
        pass

    @abstractmethod
    def validate_auth(
        self, username: Optional[str], password: Optional[str]
    ) -> Tuple[bool, Optional[Dict[str, Any]]]:
        """Validate authentication credentials.

        Args:
            username: Username from Basic Auth (account SID)
            password: Password from Basic Auth (auth token)

        Returns:
            Tuple of (is_valid, error_response)
            If valid, error_response is None
            If invalid, error_response contains error details
        """
        pass

    @abstractmethod
    def validate_parameters(
        self, request_data: Dict[str, Any], required_params: list
    ) -> Tuple[bool, Optional[Dict[str, Any]]]:
        """Validate required parameters are present.

        Args:
            request_data: Request parameters
            required_params: List of required parameter names

        Returns:
            Tuple of (is_valid, error_response)
        """
        pass

    @abstractmethod
    def validate_phone_number(
        self, number: str, field_name: str
    ) -> Tuple[bool, Optional[Dict[str, Any]]]:
        """Validate phone number format (E.164).

        Args:
            number: Phone number to validate
            field_name: Field name ('To', 'From', etc.)

        Returns:
            Tuple of (is_valid, error_response)
        """
        pass

    @abstractmethod
    def validate_from_number(
        self, number: str
    ) -> Tuple[bool, Optional[Dict[str, Any]]]:
        """Validate From number is in allowed list.

        Args:
            number: From phone number

        Returns:
            Tuple of (is_valid, error_response)
        """
        pass

    @abstractmethod
    def should_succeed(self, to_number: str) -> bool:
        """Determine if message/call should succeed based on To number.

        Checks registered_numbers, failure_numbers, and default_behavior.

        Args:
            to_number: Destination phone number

        Returns:
            True if should succeed, False otherwise
        """
        pass

    @abstractmethod
    def get_response_template(self, action: str, success: bool) -> str:
        """Get template name for response.

        Args:
            action: Action type ('send_sms', 'make_call', etc.)
            success: Whether operation succeeded

        Returns:
            Template filename
        """
        pass

    @abstractmethod
    def get_error_template(self, error_type: str) -> str:
        """Get template name for error response.

        Args:
            error_type: Error type

        Returns:
            Template filename
        """
        pass
