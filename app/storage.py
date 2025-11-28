"""Database storage layer for SMS Mock Server."""
import sqlite3
from datetime import datetime
from pathlib import Path
from typing import Dict, List, Optional, Any


class Storage:
    """SQLite storage for messages, calls, and events."""

    def __init__(self, db_path: str):
        """Initialize storage with database path.

        Args:
            db_path: Path to SQLite database file
        """
        self.db_path = Path(db_path)
        self.db_path.parent.mkdir(parents=True, exist_ok=True)
        self._init_database()

    def _get_connection(self) -> sqlite3.Connection:
        """Get database connection with row factory."""
        conn = sqlite3.connect(str(self.db_path))
        conn.row_factory = sqlite3.Row
        return conn

    def _init_database(self) -> None:
        """Initialize database schema."""
        conn = self._get_connection()
        cursor = conn.cursor()

        # Messages table
        cursor.execute("""
            CREATE TABLE IF NOT EXISTS messages (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                message_sid TEXT UNIQUE NOT NULL,
                provider TEXT NOT NULL,
                from_number TEXT NOT NULL,
                to_number TEXT NOT NULL,
                body TEXT,
                status TEXT NOT NULL,
                callback_url TEXT,
                created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
            )
        """)

        # Calls table
        cursor.execute("""
            CREATE TABLE IF NOT EXISTS calls (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                call_sid TEXT UNIQUE NOT NULL,
                provider TEXT NOT NULL,
                from_number TEXT NOT NULL,
                to_number TEXT NOT NULL,
                status TEXT NOT NULL,
                callback_url TEXT,
                twiml_url TEXT,
                created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
            )
        """)

        # Delivery events table
        cursor.execute("""
            CREATE TABLE IF NOT EXISTS delivery_events (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                message_sid TEXT,
                call_sid TEXT,
                event_type TEXT NOT NULL,
                status TEXT NOT NULL,
                callback_sent BOOLEAN DEFAULT FALSE,
                callback_response TEXT,
                created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
            )
        """)

        # Callback logs table
        cursor.execute("""
            CREATE TABLE IF NOT EXISTS callback_logs (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                target_url TEXT NOT NULL,
                payload TEXT NOT NULL,
                status_code INTEGER,
                response_body TEXT,
                attempt_number INTEGER DEFAULT 1,
                created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
            )
        """)

        conn.commit()
        conn.close()

    # Message operations
    def create_message(
        self,
        message_sid: str,
        provider: str,
        from_number: str,
        to_number: str,
        body: str,
        status: str,
        callback_url: Optional[str] = None,
    ) -> int:
        """Create a new message record.

        Args:
            message_sid: Unique message SID
            provider: Provider name (e.g., 'twilio')
            from_number: Sender phone number
            to_number: Recipient phone number
            body: Message body
            status: Message status
            callback_url: Optional callback URL

        Returns:
            Message ID
        """
        conn = self._get_connection()
        cursor = conn.cursor()

        cursor.execute(
            """
            INSERT INTO messages (message_sid, provider, from_number, to_number, body, status, callback_url)
            VALUES (?, ?, ?, ?, ?, ?, ?)
            """,
            (message_sid, provider, from_number, to_number, body, status, callback_url),
        )

        message_id = cursor.lastrowid
        conn.commit()
        conn.close()
        return message_id

    def get_message(self, message_sid: str) -> Optional[Dict[str, Any]]:
        """Get message by SID.

        Args:
            message_sid: Message SID

        Returns:
            Message dict or None if not found
        """
        conn = self._get_connection()
        cursor = conn.cursor()

        cursor.execute("SELECT * FROM messages WHERE message_sid = ?", (message_sid,))
        row = cursor.fetchone()
        conn.close()

        return dict(row) if row else None

    def update_message_status(self, message_sid: str, status: str) -> None:
        """Update message status.

        Args:
            message_sid: Message SID
            status: New status
        """
        conn = self._get_connection()
        cursor = conn.cursor()

        cursor.execute(
            """
            UPDATE messages
            SET status = ?, updated_at = CURRENT_TIMESTAMP
            WHERE message_sid = ?
            """,
            (status, message_sid),
        )

        conn.commit()
        conn.close()

    def get_all_messages(self, limit: int = 100, offset: int = 0) -> List[Dict[str, Any]]:
        """Get all messages with pagination.

        Args:
            limit: Maximum number of messages to return
            offset: Offset for pagination

        Returns:
            List of message dicts
        """
        conn = self._get_connection()
        cursor = conn.cursor()

        cursor.execute(
            "SELECT * FROM messages ORDER BY created_at DESC LIMIT ? OFFSET ?",
            (limit, offset),
        )
        rows = cursor.fetchall()
        conn.close()

        return [dict(row) for row in rows]

    # Call operations
    def create_call(
        self,
        call_sid: str,
        provider: str,
        from_number: str,
        to_number: str,
        status: str,
        callback_url: Optional[str] = None,
        twiml_url: Optional[str] = None,
    ) -> int:
        """Create a new call record.

        Args:
            call_sid: Unique call SID
            provider: Provider name
            from_number: Caller phone number
            to_number: Callee phone number
            status: Call status
            callback_url: Optional callback URL
            twiml_url: Optional TwiML URL

        Returns:
            Call ID
        """
        conn = self._get_connection()
        cursor = conn.cursor()

        cursor.execute(
            """
            INSERT INTO calls (call_sid, provider, from_number, to_number, status, callback_url, twiml_url)
            VALUES (?, ?, ?, ?, ?, ?, ?)
            """,
            (call_sid, provider, from_number, to_number, status, callback_url, twiml_url),
        )

        call_id = cursor.lastrowid
        conn.commit()
        conn.close()
        return call_id

    def get_call(self, call_sid: str) -> Optional[Dict[str, Any]]:
        """Get call by SID.

        Args:
            call_sid: Call SID

        Returns:
            Call dict or None if not found
        """
        conn = self._get_connection()
        cursor = conn.cursor()

        cursor.execute("SELECT * FROM calls WHERE call_sid = ?", (call_sid,))
        row = cursor.fetchone()
        conn.close()

        return dict(row) if row else None

    def update_call_status(self, call_sid: str, status: str) -> None:
        """Update call status.

        Args:
            call_sid: Call SID
            status: New status
        """
        conn = self._get_connection()
        cursor = conn.cursor()

        cursor.execute(
            """
            UPDATE calls
            SET status = ?, updated_at = CURRENT_TIMESTAMP
            WHERE call_sid = ?
            """,
            (status, call_sid),
        )

        conn.commit()
        conn.close()

    def get_all_calls(self, limit: int = 100, offset: int = 0) -> List[Dict[str, Any]]:
        """Get all calls with pagination.

        Args:
            limit: Maximum number of calls to return
            offset: Offset for pagination

        Returns:
            List of call dicts
        """
        conn = self._get_connection()
        cursor = conn.cursor()

        cursor.execute(
            "SELECT * FROM calls ORDER BY created_at DESC LIMIT ? OFFSET ?",
            (limit, offset),
        )
        rows = cursor.fetchall()
        conn.close()

        return [dict(row) for row in rows]

    # Delivery event operations
    def create_delivery_event(
        self,
        message_sid: Optional[str],
        call_sid: Optional[str],
        event_type: str,
        status: str,
    ) -> int:
        """Create a delivery event.

        Args:
            message_sid: Message SID (for SMS events)
            call_sid: Call SID (for call events)
            event_type: Event type
            status: Event status

        Returns:
            Event ID
        """
        conn = self._get_connection()
        cursor = conn.cursor()

        cursor.execute(
            """
            INSERT INTO delivery_events (message_sid, call_sid, event_type, status)
            VALUES (?, ?, ?, ?)
            """,
            (message_sid, call_sid, event_type, status),
        )

        event_id = cursor.lastrowid
        conn.commit()
        conn.close()
        return event_id

    def update_delivery_event_callback(
        self, event_id: int, callback_sent: bool, callback_response: Optional[str] = None
    ) -> None:
        """Update delivery event callback status.

        Args:
            event_id: Event ID
            callback_sent: Whether callback was sent
            callback_response: Optional callback response
        """
        conn = self._get_connection()
        cursor = conn.cursor()

        cursor.execute(
            """
            UPDATE delivery_events
            SET callback_sent = ?, callback_response = ?
            WHERE id = ?
            """,
            (callback_sent, callback_response, event_id),
        )

        conn.commit()
        conn.close()

    # Callback log operations
    def create_callback_log(
        self,
        target_url: str,
        payload: str,
        status_code: Optional[int] = None,
        response_body: Optional[str] = None,
        attempt_number: int = 1,
    ) -> int:
        """Create a callback log entry.

        Args:
            target_url: Callback URL
            payload: Callback payload
            status_code: HTTP status code
            response_body: Response body
            attempt_number: Attempt number

        Returns:
            Log ID
        """
        conn = self._get_connection()
        cursor = conn.cursor()

        cursor.execute(
            """
            INSERT INTO callback_logs (target_url, payload, status_code, response_body, attempt_number)
            VALUES (?, ?, ?, ?, ?)
            """,
            (target_url, payload, status_code, response_body, attempt_number),
        )

        log_id = cursor.lastrowid
        conn.commit()
        conn.close()
        return log_id

    def get_all_callback_logs(self, limit: int = 100, offset: int = 0) -> List[Dict[str, Any]]:
        """Get all callback logs with pagination.

        Args:
            limit: Maximum number of logs to return
            offset: Offset for pagination

        Returns:
            List of callback log dicts
        """
        conn = self._get_connection()
        cursor = conn.cursor()

        cursor.execute(
            "SELECT * FROM callback_logs ORDER BY created_at DESC LIMIT ? OFFSET ?",
            (limit, offset),
        )
        rows = cursor.fetchall()
        conn.close()

        return [dict(row) for row in rows]

    # Statistics
    def get_statistics(self) -> Dict[str, int]:
        """Get database statistics.

        Returns:
            Dict with counts of messages, calls, and callbacks
        """
        conn = self._get_connection()
        cursor = conn.cursor()

        cursor.execute("SELECT COUNT(*) as count FROM messages")
        message_count = cursor.fetchone()["count"]

        cursor.execute("SELECT COUNT(*) as count FROM calls")
        call_count = cursor.fetchone()["count"]

        cursor.execute("SELECT COUNT(*) as count FROM callback_logs")
        callback_count = cursor.fetchone()["count"]

        conn.close()

        return {
            "messages": message_count,
            "calls": call_count,
            "callbacks": callback_count,
        }

    # Clear/Reset operations
    def clear_messages(self) -> int:
        """Clear all messages and related delivery events.

        Returns:
            Number of messages deleted
        """
        conn = self._get_connection()
        cursor = conn.cursor()

        # Get count before deletion
        cursor.execute("SELECT COUNT(*) as count FROM messages")
        count = cursor.fetchone()["count"]

        # Delete delivery events for messages
        cursor.execute("DELETE FROM delivery_events WHERE message_sid IS NOT NULL")

        # Delete messages
        cursor.execute("DELETE FROM messages")

        conn.commit()
        conn.close()
        return count

    def clear_calls(self) -> int:
        """Clear all calls and related delivery events.

        Returns:
            Number of calls deleted
        """
        conn = self._get_connection()
        cursor = conn.cursor()

        # Get count before deletion
        cursor.execute("SELECT COUNT(*) as count FROM calls")
        count = cursor.fetchone()["count"]

        # Delete delivery events for calls
        cursor.execute("DELETE FROM delivery_events WHERE call_sid IS NOT NULL")

        # Delete calls
        cursor.execute("DELETE FROM calls")

        conn.commit()
        conn.close()
        return count

    def clear_callbacks(self) -> int:
        """Clear all callback logs.

        Returns:
            Number of callback logs deleted
        """
        conn = self._get_connection()
        cursor = conn.cursor()

        # Get count before deletion
        cursor.execute("SELECT COUNT(*) as count FROM callback_logs")
        count = cursor.fetchone()["count"]

        # Delete callback logs
        cursor.execute("DELETE FROM callback_logs")

        conn.commit()
        conn.close()
        return count

    def clear_all(self) -> Dict[str, int]:
        """Clear all data from all tables.

        Returns:
            Dict with counts of deleted records
        """
        conn = self._get_connection()
        cursor = conn.cursor()

        # Get counts before deletion
        cursor.execute("SELECT COUNT(*) as count FROM messages")
        message_count = cursor.fetchone()["count"]

        cursor.execute("SELECT COUNT(*) as count FROM calls")
        call_count = cursor.fetchone()["count"]

        cursor.execute("SELECT COUNT(*) as count FROM callback_logs")
        callback_count = cursor.fetchone()["count"]

        # Delete all data
        cursor.execute("DELETE FROM delivery_events")
        cursor.execute("DELETE FROM callback_logs")
        cursor.execute("DELETE FROM messages")
        cursor.execute("DELETE FROM calls")

        conn.commit()
        conn.close()

        return {
            "messages": message_count,
            "calls": call_count,
            "callbacks": callback_count,
        }
