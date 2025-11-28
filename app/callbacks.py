"""Callback handler for SMS Mock Server."""
import asyncio
import json
import logging
from typing import Dict, Any

import httpx

from app.config import Config
from app.storage import Storage
from app.template_engine import TemplateEngine

logger = logging.getLogger(__name__)


class CallbackHandler:
    """Handles asynchronous callback delivery for status updates."""

    def __init__(self, config: Config, storage: Storage, template_engine: TemplateEngine):
        """Initialize callback handler.

        Args:
            config: Server configuration
            storage: Storage instance
            template_engine: Template engine instance
        """
        self.config = config
        self.storage = storage
        self.template_engine = template_engine

    async def send_callback(
        self,
        url: str,
        payload: Dict[str, Any],
        attempt: int = 1,
    ) -> tuple[bool, int, str]:
        """Send callback to URL with retry logic.

        Args:
            url: Callback URL
            payload: Callback payload data
            attempt: Current attempt number

        Returns:
            Tuple of (success, status_code, response_body)
        """
        try:
            async with httpx.AsyncClient(timeout=10.0) as client:
                response = await client.post(
                    url,
                    data=payload,
                    headers={"Content-Type": "application/x-www-form-urlencoded"},
                )

                logger.info(
                    f"Callback sent to {url} (attempt {attempt}): "
                    f"status={response.status_code}"
                )

                # Log callback attempt
                self.storage.create_callback_log(
                    target_url=url,
                    payload=json.dumps(payload),
                    status_code=response.status_code,
                    response_body=response.text[:500],  # Limit response body size
                    attempt_number=attempt,
                )

                # Consider 2xx status codes as success
                return (200 <= response.status_code < 300), response.status_code, response.text

        except Exception as e:
            logger.error(f"Callback failed to {url} (attempt {attempt}): {str(e)}")

            # Log failed callback attempt
            self.storage.create_callback_log(
                target_url=url,
                payload=json.dumps(payload),
                status_code=None,
                response_body=f"Error: {str(e)}",
                attempt_number=attempt,
            )

            return False, 0, str(e)

    async def send_callback_with_retry(
        self,
        url: str,
        payload: Dict[str, Any],
    ) -> bool:
        """Send callback with retry logic.

        Args:
            url: Callback URL
            payload: Callback payload

        Returns:
            True if successful, False otherwise
        """
        max_attempts = self.config.twilio.callbacks.retry_attempts
        retry_delay = self.config.twilio.callbacks.retry_delay_seconds

        for attempt in range(1, max_attempts + 1):
            success, status_code, response_body = await self.send_callback(
                url, payload, attempt
            )

            if success:
                return True

            # Wait before retry (except on last attempt)
            if attempt < max_attempts:
                await asyncio.sleep(retry_delay)

        logger.warning(f"All callback attempts failed for {url}")
        return False

    async def process_message_callbacks(
        self,
        message_sid: str,
        from_number: str,
        to_number: str,
        callback_url: str | None,
        will_succeed: bool,
    ) -> None:
        """Process message status updates and callbacks.

        Simulates the status progression:
        - Registered numbers: queued → sent → delivered
        - Failure numbers: queued → failed
        - Unknown numbers: stays queued (no progression)

        Args:
            message_sid: Message SID
            from_number: From number
            to_number: To number
            callback_url: Callback URL (None = skip HTTP callbacks)
            will_succeed: Whether message should succeed (None = no progression)
        """
        # Check if this number should progress or stay queued
        to_in_registered = to_number in self.config.twilio.registered_numbers
        to_in_failures = to_number in self.config.twilio.failure_numbers

        # If number is not explicitly configured, keep it queued
        if not to_in_registered and not to_in_failures:
            logger.info(f"Message {message_sid} to {to_number} - unknown number, staying queued")
            return

        # Initial delay before first status update
        await asyncio.sleep(self.config.twilio.callbacks.delay_seconds)

        account_sid = self.config.twilio.account_sid

        if will_succeed:
            # Success flow: queued → sent → delivered
            statuses = ["sent", "delivered"]
        else:
            # Failure flow: queued → failed
            statuses = ["failed"]

        for status in statuses:
            # Update message status in database
            self.storage.update_message_status(message_sid, status)
            logger.info(f"Message {message_sid} status updated to: {status}")

            # Send HTTP callback only if URL provided
            if callback_url:
                # Create callback payload
                payload = {
                    "MessageSid": message_sid,
                    "AccountSid": account_sid,
                    "From": from_number,
                    "To": to_number,
                    "MessageStatus": status,
                    "ApiVersion": "2010-04-01",
                }

                # Send callback
                logger.info(f"Sending {status} callback for message {message_sid} to {callback_url}")
                await self.send_callback_with_retry(callback_url, payload)

            # Create delivery event
            self.storage.create_delivery_event(
                message_sid=message_sid,
                call_sid=None,
                event_type="status_update",
                status=status,
            )

            # Delay between status updates (except for last one)
            if status != statuses[-1]:
                await asyncio.sleep(self.config.twilio.callbacks.delay_seconds)

        logger.info(f"Message callbacks completed for {message_sid} (final status: {statuses[-1]})")

    async def process_call_callbacks(
        self,
        call_sid: str,
        from_number: str,
        to_number: str,
        callback_url: str | None,
        will_succeed: bool,
    ) -> None:
        """Process call status updates and callbacks.

        Simulates the status progression:
        - Registered numbers: queued → ringing → in-progress → completed
        - Failure numbers: queued → failed
        - Unknown numbers: stays queued (no progression)

        Args:
            call_sid: Call SID
            from_number: From number
            to_number: To number
            callback_url: Callback URL (None = skip HTTP callbacks)
            will_succeed: Whether call should succeed
        """
        # Check if this number should progress or stay queued
        to_in_registered = to_number in self.config.twilio.registered_numbers
        to_in_failures = to_number in self.config.twilio.failure_numbers

        # If number is not explicitly configured, keep it queued
        if not to_in_registered and not to_in_failures:
            logger.info(f"Call {call_sid} to {to_number} - unknown number, staying queued")
            return

        # Initial delay before first status update
        await asyncio.sleep(self.config.twilio.callbacks.delay_seconds)

        account_sid = self.config.twilio.account_sid

        if will_succeed:
            # Success flow: queued → ringing → in-progress → completed
            statuses = ["ringing", "in-progress", "completed"]
        else:
            # Failure flow: queued → failed
            statuses = ["failed"]

        for status in statuses:
            # Update call status in database
            self.storage.update_call_status(call_sid, status)
            logger.info(f"Call {call_sid} status updated to: {status}")

            # Send HTTP callback only if URL provided
            if callback_url:
                # Create callback payload
                payload = {
                    "CallSid": call_sid,
                    "AccountSid": account_sid,
                    "From": from_number,
                    "To": to_number,
                    "CallStatus": status,
                    "ApiVersion": "2010-04-01",
                    "Direction": "outbound-api",
                }

                # Send callback
                logger.info(f"Sending {status} callback for call {call_sid} to {callback_url}")
                await self.send_callback_with_retry(callback_url, payload)

            # Create delivery event
            self.storage.create_delivery_event(
                message_sid=None,
                call_sid=call_sid,
                event_type="status_update",
                status=status,
            )

            # Delay between status updates (except for last one)
            if status != statuses[-1]:
                await asyncio.sleep(self.config.twilio.callbacks.delay_seconds)

        logger.info(f"Call callbacks completed for {call_sid} (final status: {statuses[-1]})")
