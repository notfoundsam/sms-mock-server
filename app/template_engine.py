"""Template engine for rendering JSON responses."""
import json
import secrets
import string
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from jinja2 import Environment, FileSystemLoader, select_autoescape


class TemplateEngine:
    """Jinja2-based template engine for JSON responses."""

    def __init__(self, templates_path: str, provider: str = "twilio"):
        """Initialize template engine.

        Args:
            templates_path: Path to templates directory
            provider: Provider name (e.g., 'twilio')
        """
        self.templates_path = Path(templates_path)
        self.provider = provider

        # Set up Jinja2 environment for response templates
        self.response_env = Environment(
            loader=FileSystemLoader(str(self.templates_path)),
            autoescape=select_autoescape(disabled_extensions=("json",)),
        )

        # Set up Jinja2 environment for error templates
        errors_path = self.templates_path.parent / "errors"
        self.error_env = Environment(
            loader=FileSystemLoader(str(errors_path)),
            autoescape=select_autoescape(disabled_extensions=("json",)),
        )

    def generate_sid(self, prefix: str = "SM") -> str:
        """Generate a unique SID (like Twilio's).

        Args:
            prefix: SID prefix (SM for messages, CA for calls)

        Returns:
            Generated SID string
        """
        # Generate 32 character hex string
        chars = string.ascii_lowercase + string.digits
        random_part = "".join(secrets.choice(chars) for _ in range(32))
        return f"{prefix}{random_part}"

    def get_timestamp(self) -> str:
        """Get current timestamp in RFC 2822 format.

        Returns:
            Formatted timestamp string
        """
        # Twilio uses RFC 2822 format like: "Tue, 15 Jan 2024 10:30:00 +0000"
        now = datetime.now(timezone.utc)
        return now.strftime("%a, %d %b %Y %H:%M:%S +0000")

    def get_iso_timestamp(self) -> str:
        """Get current timestamp in ISO 8601 format.

        Returns:
            ISO formatted timestamp string
        """
        now = datetime.now(timezone.utc)
        return now.strftime("%Y-%m-%dT%H:%M:%SZ")

    @staticmethod
    def calculate_sms_segments(body: str) -> int:
        """Calculate number of SMS segments based on message length.

        SMS segmentation rules:
        - GSM-7 encoding (ASCII): 160 chars for single, 153 chars per segment for multi-part
        - UCS-2 encoding (Unicode): 70 chars for single, 67 chars per segment for multi-part

        Args:
            body: Message body text

        Returns:
            Number of segments required
        """
        if not body:
            return 1

        # Check if message contains non-GSM characters (simple check for non-ASCII)
        is_unicode = any(ord(char) > 127 for char in body)

        # Define segment limits
        if is_unicode:
            single_limit = 70
            multi_limit = 67
        else:
            single_limit = 160
            multi_limit = 153

        message_length = len(body)

        # Calculate segments
        if message_length <= single_limit:
            return 1
        else:
            # For multi-part messages, each segment uses the multi_limit
            return (message_length + multi_limit - 1) // multi_limit  # Ceiling division

    def render_response(
        self,
        template_name: str,
        context: dict[str, Any],
    ) -> dict[str, Any]:
        """Render response template with context.

        Args:
            template_name: Template filename
            context: Template context variables

        Returns:
            Rendered response as dict
        """
        # Add helper variables to context
        context.setdefault("date_created", self.get_timestamp())
        context.setdefault("date_updated", self.get_timestamp())
        context.setdefault("timestamp", self.get_iso_timestamp())

        # Load and render template
        template_path = f"{self.provider}/{template_name}"
        template = self.response_env.get_template(template_path)
        rendered = template.render(**context)

        # Parse JSON and return as dict
        return json.loads(rendered)

    def render_error(
        self,
        template_name: str,
        context: dict[str, Any],
    ) -> dict[str, Any]:
        """Render error template with context.

        Args:
            template_name: Error template filename
            context: Template context variables

        Returns:
            Rendered error response as dict
        """
        # Load and render error template
        template_path = f"{self.provider}/{template_name}"
        template = self.error_env.get_template(template_path)
        rendered = template.render(**context)

        # Parse JSON and return as dict
        return json.loads(rendered)

    def create_message_context(
        self,
        message_sid: str,
        account_sid: str,
        request_data: dict[str, Any],
        status: str = "queued",
    ) -> dict[str, Any]:
        """Create context for message response template.

        Args:
            message_sid: Generated message SID
            account_sid: Account SID
            request_data: Request parameters
            status: Message status

        Returns:
            Template context dict
        """
        # Calculate number of segments based on message body
        body = request_data.get("Body", "")
        num_segments = self.calculate_sms_segments(body)

        return {
            "message_sid": message_sid,
            "account_sid": account_sid,
            "request": request_data,
            "status": status,
            "num_segments": num_segments,
            "date_created": self.get_timestamp(),
            "date_updated": self.get_timestamp(),
        }

    def create_call_context(
        self,
        call_sid: str,
        account_sid: str,
        request_data: dict[str, Any],
        status: str = "queued",
    ) -> dict[str, Any]:
        """Create context for call response template.

        Args:
            call_sid: Generated call SID
            account_sid: Account SID
            request_data: Request parameters
            status: Call status

        Returns:
            Template context dict
        """
        return {
            "call_sid": call_sid,
            "account_sid": account_sid,
            "request": request_data,
            "status": status,
            "date_created": self.get_timestamp(),
            "date_updated": self.get_timestamp(),
        }

    def create_delivery_status_context(
        self,
        message_sid: str,
        account_sid: str,
        from_number: str,
        to_number: str,
        status: str,
    ) -> dict[str, Any]:
        """Create context for delivery status callback.

        Args:
            message_sid: Message SID
            account_sid: Account SID
            from_number: From number
            to_number: To number
            status: Delivery status

        Returns:
            Template context dict
        """
        return {
            "MessageSid": message_sid,
            "AccountSid": account_sid,
            "From": from_number,
            "To": to_number,
            "MessageStatus": status,
            "timestamp": self.get_iso_timestamp(),
        }
