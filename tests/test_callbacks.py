"""Tests for callbacks module."""
import asyncio
import json

import pytest
import respx
from httpx import Response

from app.callbacks import CallbackHandler


class TestCallbackHandlerInitialization:
    """Tests for CallbackHandler initialization."""

    def test_init(self, test_config, test_storage, test_template_engine):
        """Test CallbackHandler initialization."""
        handler = CallbackHandler(test_config, test_storage, test_template_engine)

        assert handler.config == test_config
        assert handler.storage == test_storage
        assert handler.template_engine == test_template_engine


class TestSendCallback:
    """Tests for send_callback method."""

    @pytest.mark.asyncio
    @respx.mock
    async def test_send_callback_success(
        self, test_config, test_storage, test_template_engine
    ):
        """Test successful callback delivery."""
        handler = CallbackHandler(test_config, test_storage, test_template_engine)

        url = "http://example.com/callback"
        payload = {"MessageSid": "SM123", "MessageStatus": "delivered"}

        # Mock successful HTTP response
        respx.post(url).mock(return_value=Response(200, text="OK"))

        success, status_code, response_body = await handler.send_callback(
            url, payload, attempt=1
        )

        assert success is True
        assert status_code == 200
        assert response_body == "OK"

        # Verify callback was logged
        logs = test_storage.get_all_callback_logs()
        assert len(logs) == 1
        assert logs[0]["target_url"] == url
        assert logs[0]["status_code"] == 200
        assert logs[0]["attempt_number"] == 1

    @pytest.mark.asyncio
    @respx.mock
    async def test_send_callback_failure_4xx(
        self, test_config, test_storage, test_template_engine
    ):
        """Test callback delivery with 4xx error."""
        handler = CallbackHandler(test_config, test_storage, test_template_engine)

        url = "http://example.com/callback"
        payload = {"MessageSid": "SM123"}

        # Mock 404 response
        respx.post(url).mock(return_value=Response(404, text="Not Found"))

        success, status_code, response_body = await handler.send_callback(
            url, payload, attempt=1
        )

        assert success is False
        assert status_code == 404
        assert response_body == "Not Found"

        # Verify callback was logged
        logs = test_storage.get_all_callback_logs()
        assert len(logs) == 1
        assert logs[0]["status_code"] == 404

    @pytest.mark.asyncio
    @respx.mock
    async def test_send_callback_network_error(
        self, test_config, test_storage, test_template_engine
    ):
        """Test callback delivery with network error."""
        handler = CallbackHandler(test_config, test_storage, test_template_engine)

        url = "http://example.com/callback"
        payload = {"MessageSid": "SM123"}

        # Mock network error
        respx.post(url).mock(side_effect=Exception("Connection refused"))

        success, status_code, response_body = await handler.send_callback(
            url, payload, attempt=1
        )

        assert success is False
        assert status_code == 0
        assert "Connection refused" in response_body

        # Verify error was logged
        logs = test_storage.get_all_callback_logs()
        assert len(logs) == 1
        assert logs[0]["status_code"] is None
        assert "Connection refused" in logs[0]["response_body"]

    @pytest.mark.asyncio
    @respx.mock
    async def test_send_callback_2xx_codes(
        self, test_config, test_storage, test_template_engine
    ):
        """Test that all 2xx status codes are considered successful."""
        handler = CallbackHandler(test_config, test_storage, test_template_engine)

        url = "http://example.com/callback"
        payload = {"MessageSid": "SM123"}

        for status_code in [200, 201, 202, 204]:
            respx.post(url).mock(return_value=Response(status_code, text="OK"))

            success, code, _ = await handler.send_callback(url, payload, attempt=1)

            assert success is True
            assert code == status_code


class TestSendCallbackWithRetry:
    """Tests for send_callback_with_retry method."""

    @pytest.mark.asyncio
    @respx.mock
    async def test_send_callback_with_retry_success_first_attempt(
        self, test_config, test_storage, test_template_engine, mock_async_sleep
    ):
        """Test successful callback on first attempt."""
        handler = CallbackHandler(test_config, test_storage, test_template_engine)

        url = "http://example.com/callback"
        payload = {"MessageSid": "SM123"}

        respx.post(url).mock(return_value=Response(200, text="OK"))

        success = await handler.send_callback_with_retry(url, payload)

        assert success is True

        # Should only have one attempt logged
        logs = test_storage.get_all_callback_logs()
        assert len(logs) == 1
        assert logs[0]["attempt_number"] == 1

    @pytest.mark.asyncio
    @respx.mock
    async def test_send_callback_with_retry_success_second_attempt(
        self, test_config, test_storage, test_template_engine, mock_async_sleep
    ):
        """Test successful callback on second attempt after first fails."""
        handler = CallbackHandler(test_config, test_storage, test_template_engine)

        url = "http://example.com/callback"
        payload = {"MessageSid": "SM123"}

        # First request fails, second succeeds
        responses = [
            Response(500, text="Server Error"),
            Response(200, text="OK"),
        ]

        respx.post(url).mock(side_effect=responses)

        success = await handler.send_callback_with_retry(url, payload)

        assert success is True

        # Should have two attempts logged
        logs = test_storage.get_all_callback_logs()
        assert len(logs) == 2
        # Logs are ordered DESC (newest first), so reverse for checking
        sorted_logs = sorted(logs, key=lambda x: x["attempt_number"])
        assert sorted_logs[0]["attempt_number"] == 1
        assert sorted_logs[0]["status_code"] == 500
        assert sorted_logs[1]["attempt_number"] == 2
        assert sorted_logs[1]["status_code"] == 200

    @pytest.mark.asyncio
    @respx.mock
    async def test_send_callback_with_retry_all_attempts_fail(
        self, test_config, test_storage, test_template_engine, mock_async_sleep
    ):
        """Test callback retry when all attempts fail."""
        handler = CallbackHandler(test_config, test_storage, test_template_engine)

        url = "http://example.com/callback"
        payload = {"MessageSid": "SM123"}

        # All requests fail
        respx.post(url).mock(return_value=Response(500, text="Server Error"))

        success = await handler.send_callback_with_retry(url, payload)

        assert success is False

        # Should have max_attempts (3) logged
        logs = test_storage.get_all_callback_logs()
        max_attempts = test_config.twilio.callbacks.retry_attempts
        assert len(logs) == max_attempts
        # Logs are ordered DESC (newest first), so sort by attempt_number
        sorted_logs = sorted(logs, key=lambda x: x["attempt_number"])
        for i in range(max_attempts):
            assert sorted_logs[i]["attempt_number"] == i + 1
            assert sorted_logs[i]["status_code"] == 500


class TestProcessMessageCallbacks:
    """Tests for process_message_callbacks method."""

    @pytest.mark.asyncio
    @respx.mock
    async def test_process_message_callbacks_success_flow(
        self, test_config, test_storage, test_template_engine, mock_async_sleep
    ):
        """Test message callback processing for success flow."""
        handler = CallbackHandler(test_config, test_storage, test_template_engine)

        callback_url = "http://example.com/callback"
        message_sid = "SM123"
        from_number = "+1234567890"
        to_number = "+1111111111"

        # Mock callback responses
        respx.post(callback_url).mock(return_value=Response(200, text="OK"))

        # Create initial message
        test_storage.create_message(
            message_sid, "twilio", from_number, to_number, "Test", "queued"
        )

        await handler.process_message_callbacks(
            message_sid, from_number, to_number, callback_url, will_succeed=True
        )

        # Check message status was updated
        message = test_storage.get_message(message_sid)
        assert message["status"] == "delivered"

        # Check delivery events were created
        events = test_storage.get_all_callback_logs()
        assert len(events) >= 2

    @pytest.mark.asyncio
    @respx.mock
    async def test_process_message_callbacks_failure_flow(
        self, test_config, test_storage, test_template_engine, mock_async_sleep
    ):
        """Test message callback processing for failure flow."""
        handler = CallbackHandler(test_config, test_storage, test_template_engine)

        callback_url = "http://example.com/callback"
        message_sid = "SM124"
        from_number = "+1234567890"
        to_number = "+2222222222"

        # Mock callback responses
        respx.post(callback_url).mock(return_value=Response(200, text="OK"))

        # Create initial message
        test_storage.create_message(
            message_sid, "twilio", from_number, to_number, "Test", "queued"
        )

        await handler.process_message_callbacks(
            message_sid, from_number, to_number, callback_url, will_succeed=False
        )

        # Check message status was updated to failed
        message = test_storage.get_message(message_sid)
        assert message["status"] == "failed"

    @pytest.mark.asyncio
    async def test_process_message_callbacks_unknown_number_stays_queued(
        self, test_config, test_storage, test_template_engine, mock_async_sleep
    ):
        """Test that messages to unknown numbers stay queued."""
        handler = CallbackHandler(test_config, test_storage, test_template_engine)

        message_sid = "SM125"
        from_number = "+1234567890"
        to_number = "+9999999999"

        # Create initial message
        test_storage.create_message(
            message_sid, "twilio", from_number, to_number, "Test", "queued"
        )

        await handler.process_message_callbacks(
            message_sid, from_number, to_number, None, will_succeed=True
        )

        # Check message status is still queued
        message = test_storage.get_message(message_sid)
        assert message["status"] == "queued"

    @pytest.mark.asyncio
    @respx.mock
    async def test_process_message_callbacks_without_callback_url(
        self, test_config, test_storage, test_template_engine, mock_async_sleep
    ):
        """Test message callback processing without callback URL."""
        handler = CallbackHandler(test_config, test_storage, test_template_engine)

        message_sid = "SM126"
        from_number = "+1234567890"
        to_number = "+1111111111"

        # Create initial message
        test_storage.create_message(
            message_sid, "twilio", from_number, to_number, "Test", "queued"
        )

        await handler.process_message_callbacks(
            message_sid, from_number, to_number, None, will_succeed=True
        )

        # Check message status was updated (even without callback URL)
        message = test_storage.get_message(message_sid)
        assert message["status"] == "delivered"

        # Check no HTTP callbacks were made
        logs = test_storage.get_all_callback_logs()
        assert len(logs) == 0


class TestProcessCallCallbacks:
    """Tests for process_call_callbacks method."""

    @pytest.mark.asyncio
    @respx.mock
    async def test_process_call_callbacks_success_flow(
        self, test_config, test_storage, test_template_engine, mock_async_sleep
    ):
        """Test call callback processing for success flow."""
        handler = CallbackHandler(test_config, test_storage, test_template_engine)

        callback_url = "http://example.com/callback"
        call_sid = "CA123"
        from_number = "+1234567890"
        to_number = "+1111111111"

        # Mock callback responses
        respx.post(callback_url).mock(return_value=Response(200, text="OK"))

        # Create initial call
        test_storage.create_call(
            call_sid, "twilio", from_number, to_number, "queued"
        )

        await handler.process_call_callbacks(
            call_sid, from_number, to_number, callback_url, will_succeed=True
        )

        # Check call status was updated
        call = test_storage.get_call(call_sid)
        assert call["status"] == "completed"

        # Check callbacks were made
        logs = test_storage.get_all_callback_logs()
        assert len(logs) >= 3

    @pytest.mark.asyncio
    @respx.mock
    async def test_process_call_callbacks_failure_flow(
        self, test_config, test_storage, test_template_engine, mock_async_sleep
    ):
        """Test call callback processing for failure flow."""
        handler = CallbackHandler(test_config, test_storage, test_template_engine)

        callback_url = "http://example.com/callback"
        call_sid = "CA124"
        from_number = "+1234567890"
        to_number = "+2222222222"

        # Mock callback responses
        respx.post(callback_url).mock(return_value=Response(200, text="OK"))

        # Create initial call
        test_storage.create_call(
            call_sid, "twilio", from_number, to_number, "queued"
        )

        await handler.process_call_callbacks(
            call_sid, from_number, to_number, callback_url, will_succeed=False
        )

        # Check call status was updated to failed
        call = test_storage.get_call(call_sid)
        assert call["status"] == "failed"

    @pytest.mark.asyncio
    async def test_process_call_callbacks_unknown_number_stays_queued(
        self, test_config, test_storage, test_template_engine, mock_async_sleep
    ):
        """Test that calls to unknown numbers stay queued."""
        handler = CallbackHandler(test_config, test_storage, test_template_engine)

        call_sid = "CA125"
        from_number = "+1234567890"
        to_number = "+9999999999"

        # Create initial call
        test_storage.create_call(
            call_sid, "twilio", from_number, to_number, "queued"
        )

        await handler.process_call_callbacks(
            call_sid, from_number, to_number, None, will_succeed=True
        )

        # Check call status is still queued
        call = test_storage.get_call(call_sid)
        assert call["status"] == "queued"

    @pytest.mark.asyncio
    @respx.mock
    async def test_process_call_callbacks_without_callback_url(
        self, test_config, test_storage, test_template_engine, mock_async_sleep
    ):
        """Test call callback processing without callback URL."""
        handler = CallbackHandler(test_config, test_storage, test_template_engine)

        call_sid = "CA126"
        from_number = "+1234567890"
        to_number = "+1111111111"

        # Create initial call
        test_storage.create_call(
            call_sid, "twilio", from_number, to_number, "queued"
        )

        await handler.process_call_callbacks(
            call_sid, from_number, to_number, None, will_succeed=True
        )

        # Check call status was updated (even without callback URL)
        call = test_storage.get_call(call_sid)
        assert call["status"] == "completed"

        # Check no HTTP callbacks were made
        logs = test_storage.get_all_callback_logs()
        assert len(logs) == 0
