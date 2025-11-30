"""Web UI routes for SMS Mock Server."""
import json
import logging
import math
from datetime import datetime
from typing import Any
from zoneinfo import ZoneInfo

from fastapi import APIRouter, Request
from fastapi.responses import HTMLResponse
from fastapi.templating import Jinja2Templates

from app.storage import Storage
from app.config import Config

logger = logging.getLogger(__name__)

# Create router
router = APIRouter()

# Setup Jinja2 for HTML templates
templates = Jinja2Templates(directory="templates/ui")

# Pagination constant
ITEMS_PER_PAGE = 50


def calculate_total_pages(total_items: int) -> int:
    """Calculate total pages for pagination.

    Args:
        total_items: Total number of items

    Returns:
        Number of pages needed
    """
    return math.ceil(total_items / ITEMS_PER_PAGE)


def parse_callback_payload(callback: dict[str, Any]) -> None:
    """Parse callback JSON payload and extract status/SID fields.

    Modifies the callback dict in place, adding:
    - message_status: MessageStatus from payload
    - call_status: CallStatus from payload
    - message_sid: MessageSid from payload
    - call_sid: CallSid from payload

    Args:
        callback: Callback dict to modify
    """
    try:
        payload = json.loads(callback.get("payload", "{}"))
        callback["message_status"] = payload.get("MessageStatus")
        callback["call_status"] = payload.get("CallStatus")
        callback["message_sid"] = payload.get("MessageSid")
        callback["call_sid"] = payload.get("CallSid")
    except (json.JSONDecodeError, TypeError):
        callback["message_status"] = None
        callback["call_status"] = None
        callback["message_sid"] = None
        callback["call_sid"] = None


def parse_callback_payloads(callbacks: list[dict[str, Any]]) -> None:
    """Parse JSON payloads for a list of callbacks.

    Args:
        callbacks: List of callback dicts to modify
    """
    for callback in callbacks:
        parse_callback_payload(callback)


def setup_ui_routes(app, storage: Storage, config: Config):
    """Setup UI routes on the FastAPI app.

    Args:
        app: FastAPI application
        storage: Storage instance
        config: Configuration instance
    """
    # Get configured timezone
    tz_name = config.server.timezone
    try:
        target_tz = ZoneInfo(tz_name)
    except Exception:
        logger.warning(f"Invalid timezone '{tz_name}', falling back to UTC")
        target_tz = ZoneInfo("UTC")
        tz_name = "UTC"

    def format_datetime(value):
        """Convert UTC timestamp to configured timezone.

        Args:
            value: Timestamp string from SQLite (UTC)

        Returns:
            Formatted datetime string in configured timezone
        """
        if not value:
            return ""
        try:
            # Parse SQLite timestamp (format: YYYY-MM-DD HH:MM:SS)
            dt = datetime.strptime(str(value), "%Y-%m-%d %H:%M:%S")
            # Assume it's UTC
            dt_utc = dt.replace(tzinfo=ZoneInfo("UTC"))
            # Convert to target timezone
            dt_local = dt_utc.astimezone(target_tz)
            # Format for display
            return dt_local.strftime("%Y-%m-%d %H:%M:%S")
        except (ValueError, TypeError) as e:
            logger.debug(f"Failed to parse datetime '{value}': {e}")
            return str(value)

    # Register the filter with Jinja2
    templates.env.filters["tz"] = format_datetime
    # Also make timezone name available globally
    templates.env.globals["timezone"] = tz_name

    @app.get("/", response_class=HTMLResponse)
    async def dashboard(request: Request):
        """Dashboard showing overview and statistics.

        Args:
            request: FastAPI request object

        Returns:
            HTML response
        """
        stats = storage.get_statistics()
        recent_messages = storage.get_all_messages(limit=10)
        recent_calls = storage.get_all_calls(limit=10)

        return templates.TemplateResponse(
            "dashboard.html",
            {
                "request": request,
                "stats": stats,
                "recent_messages": recent_messages,
                "recent_calls": recent_calls,
                "provider": config.provider,
            },
        )

    @app.get("/ui/messages", response_class=HTMLResponse)
    async def messages_list(request: Request, page: int = 1):
        """Messages list page.

        Args:
            request: FastAPI request object
            page: Page number (default 1)

        Returns:
            HTML response
        """
        # Pagination
        offset = (page - 1) * ITEMS_PER_PAGE
        messages = storage.get_all_messages(limit=ITEMS_PER_PAGE, offset=offset)
        stats = storage.get_statistics()
        total_messages = stats.get("messages", 0)
        total_pages = calculate_total_pages(total_messages)

        return templates.TemplateResponse(
            "messages.html",
            {
                "request": request,
                "messages": messages,
                "provider": config.provider,
                "page": page,
                "total_pages": total_pages,
                "total_messages": total_messages,
            },
        )

    @app.get("/ui/calls", response_class=HTMLResponse)
    async def calls_list(request: Request, page: int = 1):
        """Calls list page.

        Args:
            request: FastAPI request object
            page: Page number (default 1)

        Returns:
            HTML response
        """
        # Pagination
        offset = (page - 1) * ITEMS_PER_PAGE
        calls = storage.get_all_calls(limit=ITEMS_PER_PAGE, offset=offset)
        stats = storage.get_statistics()
        total_calls = stats.get("calls", 0)
        total_pages = calculate_total_pages(total_calls)

        return templates.TemplateResponse(
            "calls.html",
            {
                "request": request,
                "calls": calls,
                "provider": config.provider,
                "page": page,
                "total_pages": total_pages,
                "total_calls": total_calls,
            },
        )

    @app.get("/ui/callbacks", response_class=HTMLResponse)
    async def callbacks_list(request: Request, page: int = 1):
        """Callback logs page.

        Args:
            request: FastAPI request object
            page: Page number (default 1)

        Returns:
            HTML response
        """
        # Pagination
        offset = (page - 1) * ITEMS_PER_PAGE
        callbacks = storage.get_all_callback_logs(limit=ITEMS_PER_PAGE, offset=offset)
        stats = storage.get_statistics()
        total_callbacks = stats.get("callbacks", 0)
        total_pages = calculate_total_pages(total_callbacks)

        # Parse JSON payloads
        parse_callback_payloads(callbacks)

        return templates.TemplateResponse(
            "callbacks.html",
            {
                "request": request,
                "callbacks": callbacks,
                "provider": config.provider,
                "page": page,
                "total_pages": total_pages,
                "total_callbacks": total_callbacks,
            },
        )

    @app.get("/ui/fragments/stats", response_class=HTMLResponse)
    async def stats_fragment(request: Request):
        """Statistics fragment for HTMX polling.

        Args:
            request: FastAPI request object

        Returns:
            HTML fragment
        """
        stats = storage.get_statistics()
        return templates.TemplateResponse(
            "fragments/stats.html",
            {
                "request": request,
                "stats": stats,
            },
        )

    @app.get("/ui/fragments/recent-messages", response_class=HTMLResponse)
    async def recent_messages_fragment(request: Request):
        """Recent messages fragment for HTMX polling.

        Args:
            request: FastAPI request object

        Returns:
            HTML fragment
        """
        recent_messages = storage.get_all_messages(limit=10)
        return templates.TemplateResponse(
            "fragments/recent_messages.html",
            {
                "request": request,
                "recent_messages": recent_messages,
            },
        )

    @app.get("/ui/fragments/recent-calls", response_class=HTMLResponse)
    async def recent_calls_fragment(request: Request):
        """Recent calls fragment for HTMX polling.

        Args:
            request: FastAPI request object

        Returns:
            HTML fragment
        """
        recent_calls = storage.get_all_calls(limit=10)
        return templates.TemplateResponse(
            "fragments/recent_calls.html",
            {
                "request": request,
                "recent_calls": recent_calls,
            },
        )

    @app.get("/ui/fragments/messages-table", response_class=HTMLResponse)
    async def messages_table_fragment(request: Request, page: int = 1):
        """Messages table fragment for HTMX polling.

        Args:
            request: FastAPI request object
            page: Page number (default 1)

        Returns:
            HTML fragment
        """
        offset = (page - 1) * ITEMS_PER_PAGE
        messages = storage.get_all_messages(limit=ITEMS_PER_PAGE, offset=offset)
        stats = storage.get_statistics()
        total_messages = stats.get("messages", 0)
        total_pages = calculate_total_pages(total_messages)

        return templates.TemplateResponse(
            "fragments/messages_table.html",
            {
                "request": request,
                "messages": messages,
                "page": page,
                "total_pages": total_pages,
            },
        )

    @app.get("/ui/fragments/calls-table", response_class=HTMLResponse)
    async def calls_table_fragment(request: Request, page: int = 1):
        """Calls table fragment for HTMX polling.

        Args:
            request: FastAPI request object
            page: Page number (default 1)

        Returns:
            HTML fragment
        """
        offset = (page - 1) * ITEMS_PER_PAGE
        calls = storage.get_all_calls(limit=ITEMS_PER_PAGE, offset=offset)
        stats = storage.get_statistics()
        total_calls = stats.get("calls", 0)
        total_pages = calculate_total_pages(total_calls)

        return templates.TemplateResponse(
            "fragments/calls_table.html",
            {
                "request": request,
                "calls": calls,
                "page": page,
                "total_pages": total_pages,
            },
        )

    @app.get("/ui/fragments/callbacks-table", response_class=HTMLResponse)
    async def callbacks_table_fragment(request: Request, page: int = 1):
        """Callbacks table fragment for HTMX polling.

        Args:
            request: FastAPI request object
            page: Page number (default 1)

        Returns:
            HTML fragment
        """
        offset = (page - 1) * ITEMS_PER_PAGE
        callbacks = storage.get_all_callback_logs(limit=ITEMS_PER_PAGE, offset=offset)
        stats = storage.get_statistics()
        total_callbacks = stats.get("callbacks", 0)
        total_pages = calculate_total_pages(total_callbacks)

        # Parse JSON payloads
        parse_callback_payloads(callbacks)

        return templates.TemplateResponse(
            "fragments/callbacks_table.html",
            {
                "request": request,
                "callbacks": callbacks,
                "page": page,
                "total_pages": total_pages,
            },
        )

    @app.get("/ui/fragments/message/{message_sid}", response_class=HTMLResponse)
    async def message_detail(request: Request, message_sid: str):
        """Message detail modal fragment.

        Args:
            request: FastAPI request object
            message_sid: Message SID

        Returns:
            HTML fragment with message details
        """
        message = storage.get_message(message_sid)
        if not message:
            return HTMLResponse(
                content="<div class='modal-body'><p>Message not found</p></div>",
                status_code=404,
            )

        return templates.TemplateResponse(
            "fragments/message_detail.html",
            {
                "request": request,
                "message": message,
            },
        )

    @app.get("/ui/fragments/call/{call_sid}", response_class=HTMLResponse)
    async def call_detail(request: Request, call_sid: str):
        """Call detail modal fragment.

        Args:
            request: FastAPI request object
            call_sid: Call SID

        Returns:
            HTML fragment with call details
        """
        call = storage.get_call(call_sid)
        if not call:
            return HTMLResponse(
                content="<div class='modal-body'><p>Call not found</p></div>",
                status_code=404,
            )

        return templates.TemplateResponse(
            "fragments/call_detail.html",
            {
                "request": request,
                "call": call,
            },
        )

    @app.get("/ui/fragments/callback-detail/{callback_id}", response_class=HTMLResponse)
    async def callback_detail(request: Request, callback_id: int):
        """Callback detail modal fragment.

        Args:
            request: FastAPI request object
            callback_id: Callback log ID

        Returns:
            HTML fragment with callback details
        """
        callback = storage.get_callback(callback_id)
        if not callback:
            return HTMLResponse(
                content="<div class='modal-body'><p>Callback not found</p></div>",
                status_code=404,
            )

        # Parse JSON payload
        parse_callback_payload(callback)

        return templates.TemplateResponse(
            "fragments/callback_detail.html",
            {
                "request": request,
                "callback": callback,
            },
        )
