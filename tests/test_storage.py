"""Tests for storage module."""
import sqlite3
from pathlib import Path

import pytest

from app.storage import Storage


class TestStorageInitialization:
    """Tests for Storage initialization and database setup."""

    def test_init_creates_database_file(self, tmp_path):
        """Test that Storage creates database file."""
        db_path = tmp_path / "test.db"
        storage = Storage(str(db_path))
        assert db_path.exists()

    def test_init_creates_parent_directories(self, tmp_path):
        """Test that Storage creates parent directories if they don't exist."""
        db_path = tmp_path / "nested" / "dir" / "test.db"
        storage = Storage(str(db_path))
        assert db_path.exists()
        assert db_path.parent.exists()

    def test_init_creates_messages_table(self, tmp_path):
        """Test that Storage creates messages table."""
        db_path = tmp_path / "test.db"
        storage = Storage(str(db_path))
        conn = storage._get_connection()
        cursor = conn.cursor()

        cursor.execute("SELECT name FROM sqlite_master WHERE type='table' AND name='messages'")
        assert cursor.fetchone() is not None
        conn.close()

    def test_init_creates_calls_table(self, tmp_path):
        """Test that Storage creates calls table."""
        db_path = tmp_path / "test.db"
        storage = Storage(str(db_path))
        conn = storage._get_connection()
        cursor = conn.cursor()

        cursor.execute("SELECT name FROM sqlite_master WHERE type='table' AND name='calls'")
        assert cursor.fetchone() is not None
        conn.close()

    def test_init_creates_delivery_events_table(self, tmp_path):
        """Test that Storage creates delivery_events table."""
        db_path = tmp_path / "test.db"
        storage = Storage(str(db_path))
        conn = storage._get_connection()
        cursor = conn.cursor()

        cursor.execute("SELECT name FROM sqlite_master WHERE type='table' AND name='delivery_events'")
        assert cursor.fetchone() is not None
        conn.close()

    def test_init_creates_callback_logs_table(self, tmp_path):
        """Test that Storage creates callback_logs table."""
        db_path = tmp_path / "test.db"
        storage = Storage(str(db_path))
        conn = storage._get_connection()
        cursor = conn.cursor()

        cursor.execute("SELECT name FROM sqlite_master WHERE type='table' AND name='callback_logs'")
        assert cursor.fetchone() is not None
        conn.close()


class TestMessageOperations:
    """Tests for message CRUD operations."""

    @pytest.fixture
    def storage(self, tmp_path):
        """Create temporary file storage for testing."""
        db_path = tmp_path / "test.db"
        return Storage(str(db_path))

    def test_create_message(self, storage):
        """Test creating a message."""
        message_id = storage.create_message(
            message_sid="SM123",
            provider="twilio",
            from_number="+1234567890",
            to_number="+0987654321",
            body="Test message",
            status="queued",
            callback_url="http://example.com/callback",
        )

        assert message_id > 0

    def test_create_message_without_callback(self, storage):
        """Test creating a message without callback URL."""
        message_id = storage.create_message(
            message_sid="SM124",
            provider="twilio",
            from_number="+1234567890",
            to_number="+0987654321",
            body="Test message",
            status="queued",
        )

        assert message_id > 0

    def test_get_message(self, storage):
        """Test getting a message by SID."""
        storage.create_message(
            message_sid="SM125",
            provider="twilio",
            from_number="+1234567890",
            to_number="+0987654321",
            body="Test message",
            status="queued",
        )

        message = storage.get_message("SM125")
        assert message is not None
        assert message["message_sid"] == "SM125"
        assert message["provider"] == "twilio"
        assert message["from_number"] == "+1234567890"
        assert message["to_number"] == "+0987654321"
        assert message["body"] == "Test message"
        assert message["status"] == "queued"

    def test_get_nonexistent_message(self, storage):
        """Test getting a message that doesn't exist."""
        message = storage.get_message("SMNONEXISTENT")
        assert message is None

    def test_update_message_status(self, storage):
        """Test updating message status."""
        storage.create_message(
            message_sid="SM126",
            provider="twilio",
            from_number="+1234567890",
            to_number="+0987654321",
            body="Test message",
            status="queued",
        )

        storage.update_message_status("SM126", "sent")

        message = storage.get_message("SM126")
        assert message["status"] == "sent"

    def test_get_all_messages(self, storage):
        """Test getting all messages."""
        storage.create_message("SM127", "twilio", "+1111111111", "+2222222222", "Msg 1", "sent")
        storage.create_message("SM128", "twilio", "+1111111111", "+3333333333", "Msg 2", "sent")
        storage.create_message("SM129", "twilio", "+1111111111", "+4444444444", "Msg 3", "sent")

        messages = storage.get_all_messages()
        assert len(messages) == 3
        # Verify all expected messages are present
        sids = {msg["message_sid"] for msg in messages}
        assert sids == {"SM127", "SM128", "SM129"}

    def test_get_all_messages_with_limit(self, storage):
        """Test getting messages with limit."""
        for i in range(10):
            storage.create_message(f"SM{i}", "twilio", "+1111111111", "+2222222222", f"Msg {i}", "sent")

        messages = storage.get_all_messages(limit=5)
        assert len(messages) == 5

    def test_get_all_messages_with_offset(self, storage):
        """Test getting messages with offset."""
        storage.create_message("SM130", "twilio", "+1111111111", "+2222222222", "Msg 1", "sent")
        storage.create_message("SM131", "twilio", "+1111111111", "+2222222222", "Msg 2", "sent")
        storage.create_message("SM132", "twilio", "+1111111111", "+2222222222", "Msg 3", "sent")

        messages = storage.get_all_messages(limit=10, offset=1)
        assert len(messages) == 2
        assert messages[0]["message_sid"] == "SM131"


class TestCallOperations:
    """Tests for call CRUD operations."""

    @pytest.fixture
    def storage(self, tmp_path):
        """Create temporary file storage for testing."""
        db_path = tmp_path / "test.db"
        return Storage(str(db_path))

    def test_create_call(self, storage):
        """Test creating a call."""
        call_id = storage.create_call(
            call_sid="CA123",
            provider="twilio",
            from_number="+1234567890",
            to_number="+0987654321",
            status="initiated",
            callback_url="http://example.com/callback",
            twiml_url="http://example.com/twiml",
        )

        assert call_id > 0

    def test_create_call_minimal(self, storage):
        """Test creating a call with minimal parameters."""
        call_id = storage.create_call(
            call_sid="CA124",
            provider="twilio",
            from_number="+1234567890",
            to_number="+0987654321",
            status="initiated",
        )

        assert call_id > 0

    def test_get_call(self, storage):
        """Test getting a call by SID."""
        storage.create_call(
            call_sid="CA125",
            provider="twilio",
            from_number="+1234567890",
            to_number="+0987654321",
            status="initiated",
        )

        call = storage.get_call("CA125")
        assert call is not None
        assert call["call_sid"] == "CA125"
        assert call["provider"] == "twilio"
        assert call["from_number"] == "+1234567890"
        assert call["to_number"] == "+0987654321"
        assert call["status"] == "initiated"

    def test_get_nonexistent_call(self, storage):
        """Test getting a call that doesn't exist."""
        call = storage.get_call("CANONEXISTENT")
        assert call is None

    def test_update_call_status(self, storage):
        """Test updating call status."""
        storage.create_call(
            call_sid="CA126",
            provider="twilio",
            from_number="+1234567890",
            to_number="+0987654321",
            status="initiated",
        )

        storage.update_call_status("CA126", "in-progress")

        call = storage.get_call("CA126")
        assert call["status"] == "in-progress"

    def test_get_all_calls(self, storage):
        """Test getting all calls."""
        storage.create_call("CA127", "twilio", "+1111111111", "+2222222222", "initiated")
        storage.create_call("CA128", "twilio", "+1111111111", "+3333333333", "in-progress")
        storage.create_call("CA129", "twilio", "+1111111111", "+4444444444", "completed")

        calls = storage.get_all_calls()
        assert len(calls) == 3
        # Verify all expected calls are present
        sids = {call["call_sid"] for call in calls}
        assert sids == {"CA127", "CA128", "CA129"}

    def test_get_all_calls_with_pagination(self, storage):
        """Test getting calls with pagination."""
        for i in range(10):
            storage.create_call(f"CA{i}", "twilio", "+1111111111", "+2222222222", "completed")

        calls = storage.get_all_calls(limit=3, offset=2)
        assert len(calls) == 3


class TestDeliveryEventOperations:
    """Tests for delivery event operations."""

    @pytest.fixture
    def storage(self, tmp_path):
        """Create temporary file storage for testing."""
        db_path = tmp_path / "test.db"
        return Storage(str(db_path))

    def test_create_delivery_event_for_message(self, storage):
        """Test creating a delivery event for a message."""
        event_id = storage.create_delivery_event(
            message_sid="SM123",
            call_sid=None,
            event_type="delivered",
            status="success",
        )

        assert event_id > 0

    def test_create_delivery_event_for_call(self, storage):
        """Test creating a delivery event for a call."""
        event_id = storage.create_delivery_event(
            message_sid=None,
            call_sid="CA123",
            event_type="answered",
            status="success",
        )

        assert event_id > 0

    def test_update_delivery_event_callback(self, storage):
        """Test updating delivery event callback status."""
        event_id = storage.create_delivery_event(
            message_sid="SM124",
            call_sid=None,
            event_type="delivered",
            status="success",
        )

        storage.update_delivery_event_callback(
            event_id=event_id,
            callback_sent=True,
            callback_response="OK",
        )

        conn = storage._get_connection()
        cursor = conn.cursor()
        cursor.execute("SELECT * FROM delivery_events WHERE id = ?", (event_id,))
        event = dict(cursor.fetchone())
        conn.close()

        assert event["callback_sent"] == 1
        assert event["callback_response"] == "OK"


class TestCallbackLogOperations:
    """Tests for callback log operations."""

    @pytest.fixture
    def storage(self, tmp_path):
        """Create temporary file storage for testing."""
        db_path = tmp_path / "test.db"
        return Storage(str(db_path))

    def test_create_callback_log(self, storage):
        """Test creating a callback log."""
        log_id = storage.create_callback_log(
            target_url="http://example.com/callback",
            payload='{"status": "delivered"}',
            status_code=200,
            response_body="OK",
            attempt_number=1,
        )

        assert log_id > 0

    def test_create_callback_log_minimal(self, storage):
        """Test creating a callback log with minimal parameters."""
        log_id = storage.create_callback_log(
            target_url="http://example.com/callback",
            payload='{"status": "delivered"}',
        )

        assert log_id > 0

    def test_get_all_callback_logs(self, storage):
        """Test getting all callback logs."""
        storage.create_callback_log("http://example.com/1", '{"status": "delivered"}', 200, "OK", 1)
        storage.create_callback_log("http://example.com/2", '{"status": "failed"}', 500, "Error", 1)
        storage.create_callback_log("http://example.com/3", '{"status": "delivered"}', 200, "OK", 2)

        logs = storage.get_all_callback_logs()
        assert len(logs) == 3

    def test_get_all_callback_logs_with_pagination(self, storage):
        """Test getting callback logs with pagination."""
        for i in range(10):
            storage.create_callback_log(f"http://example.com/{i}", '{"data": "test"}')

        logs = storage.get_all_callback_logs(limit=5, offset=3)
        assert len(logs) == 5


class TestStatistics:
    """Tests for statistics operations."""

    @pytest.fixture
    def storage(self, tmp_path):
        """Create temporary file storage for testing."""
        db_path = tmp_path / "test.db"
        return Storage(str(db_path))

    def test_get_statistics_empty_database(self, storage):
        """Test getting statistics from empty database."""
        stats = storage.get_statistics()
        assert stats["messages"] == 0
        assert stats["calls"] == 0
        assert stats["callbacks"] == 0

    def test_get_statistics_with_data(self, storage):
        """Test getting statistics with data."""
        storage.create_message("SM1", "twilio", "+1", "+2", "Test", "sent")
        storage.create_message("SM2", "twilio", "+1", "+2", "Test", "sent")
        storage.create_call("CA1", "twilio", "+1", "+2", "completed")
        storage.create_callback_log("http://example.com", '{"data": "test"}')
        storage.create_callback_log("http://example.com", '{"data": "test"}')
        storage.create_callback_log("http://example.com", '{"data": "test"}')

        stats = storage.get_statistics()
        assert stats["messages"] == 2
        assert stats["calls"] == 1
        assert stats["callbacks"] == 3


class TestClearOperations:
    """Tests for clear/reset operations."""

    @pytest.fixture
    def storage(self, tmp_path):
        """Create temporary file storage for testing."""
        db_path = tmp_path / "test.db"
        return Storage(str(db_path))

    def test_clear_messages(self, storage):
        """Test clearing messages."""
        storage.create_message("SM1", "twilio", "+1", "+2", "Test", "sent")
        storage.create_message("SM2", "twilio", "+1", "+2", "Test", "sent")
        storage.create_delivery_event("SM1", None, "delivered", "success")

        count = storage.clear_messages()

        assert count == 2
        messages = storage.get_all_messages()
        assert len(messages) == 0

        conn = storage._get_connection()
        cursor = conn.cursor()
        cursor.execute("SELECT COUNT(*) as count FROM delivery_events WHERE message_sid IS NOT NULL")
        event_count = cursor.fetchone()["count"]
        conn.close()

        assert event_count == 0

    def test_clear_calls(self, storage):
        """Test clearing calls."""
        storage.create_call("CA1", "twilio", "+1", "+2", "completed")
        storage.create_call("CA2", "twilio", "+1", "+2", "completed")
        storage.create_delivery_event(None, "CA1", "answered", "success")

        count = storage.clear_calls()

        assert count == 2
        calls = storage.get_all_calls()
        assert len(calls) == 0

        conn = storage._get_connection()
        cursor = conn.cursor()
        cursor.execute("SELECT COUNT(*) as count FROM delivery_events WHERE call_sid IS NOT NULL")
        event_count = cursor.fetchone()["count"]
        conn.close()

        assert event_count == 0

    def test_clear_callbacks(self, storage):
        """Test clearing callback logs."""
        storage.create_callback_log("http://example.com/1", '{"data": "test"}')
        storage.create_callback_log("http://example.com/2", '{"data": "test"}')
        storage.create_callback_log("http://example.com/3", '{"data": "test"}')

        count = storage.clear_callbacks()

        assert count == 3
        logs = storage.get_all_callback_logs()
        assert len(logs) == 0

    def test_clear_all(self, storage):
        """Test clearing all data."""
        storage.create_message("SM1", "twilio", "+1", "+2", "Test", "sent")
        storage.create_message("SM2", "twilio", "+1", "+2", "Test", "sent")
        storage.create_call("CA1", "twilio", "+1", "+2", "completed")
        storage.create_callback_log("http://example.com", '{"data": "test"}')
        storage.create_callback_log("http://example.com", '{"data": "test"}')
        storage.create_delivery_event("SM1", None, "delivered", "success")
        storage.create_delivery_event(None, "CA1", "answered", "success")

        result = storage.clear_all()

        assert result["messages"] == 2
        assert result["calls"] == 1
        assert result["callbacks"] == 2

        assert len(storage.get_all_messages()) == 0
        assert len(storage.get_all_calls()) == 0
        assert len(storage.get_all_callback_logs()) == 0

        conn = storage._get_connection()
        cursor = conn.cursor()
        cursor.execute("SELECT COUNT(*) as count FROM delivery_events")
        event_count = cursor.fetchone()["count"]
        conn.close()

        assert event_count == 0
