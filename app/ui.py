"""Web UI routes for SMS Mock Server."""
import json
import logging
from datetime import datetime
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
        client_host = request.client.host if request.client else "unknown"
        user_agent = request.headers.get("user-agent", "unknown")
        logger.info(f"Dashboard accessed from {client_host} - UA: {user_agent}")
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
        client_host = request.client.host if request.client else "unknown"
        user_agent = request.headers.get("user-agent", "unknown")
        logger.info(f"Messages page accessed from {client_host} - UA: {user_agent}")

        # Pagination
        per_page = 50
        offset = (page - 1) * per_page
        messages = storage.get_all_messages(limit=per_page, offset=offset)
        stats = storage.get_statistics()
        total_messages = stats.get("messages", 0)
        total_pages = (total_messages + per_page - 1) // per_page

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
        client_host = request.client.host if request.client else "unknown"
        user_agent = request.headers.get("user-agent", "unknown")
        logger.info(f"Calls page accessed from {client_host} - UA: {user_agent}")

        # Pagination
        per_page = 50
        offset = (page - 1) * per_page
        calls = storage.get_all_calls(limit=per_page, offset=offset)
        stats = storage.get_statistics()
        total_calls = stats.get("calls", 0)
        total_pages = (total_calls + per_page - 1) // per_page

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
        client_host = request.client.host if request.client else "unknown"
        user_agent = request.headers.get("user-agent", "unknown")
        logger.info(f"Callbacks page accessed from {client_host} - UA: {user_agent}")

        # Pagination
        per_page = 50
        offset = (page - 1) * per_page
        callbacks = storage.get_all_callback_logs(limit=per_page, offset=offset)
        stats = storage.get_statistics()
        total_callbacks = stats.get("callbacks", 0)
        total_pages = (total_callbacks + per_page - 1) // per_page

        # Parse JSON payload to extract status and SID for each callback
        for callback in callbacks:
            try:
                payload = json.loads(callback["payload"])
                callback["message_status"] = payload.get("MessageStatus")
                callback["call_status"] = payload.get("CallStatus")
                callback["message_sid"] = payload.get("MessageSid")
                callback["call_sid"] = payload.get("CallSid")
            except (json.JSONDecodeError, KeyError):
                callback["message_status"] = None
                callback["call_status"] = None
                callback["message_sid"] = None
                callback["call_sid"] = None

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
        client_host = request.client.host if request.client else "unknown"
        logger.debug(f"Stats fragment requested from {client_host}")
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
        client_host = request.client.host if request.client else "unknown"
        logger.debug(f"Recent messages fragment requested from {client_host}")
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
        client_host = request.client.host if request.client else "unknown"
        logger.debug(f"Recent calls fragment requested from {client_host}")
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
        per_page = 50
        offset = (page - 1) * per_page
        messages = storage.get_all_messages(limit=per_page, offset=offset)
        stats = storage.get_statistics()
        total_messages = stats.get("messages", 0)
        total_pages = (total_messages + per_page - 1) // per_page

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
        per_page = 50
        offset = (page - 1) * per_page
        calls = storage.get_all_calls(limit=per_page, offset=offset)
        stats = storage.get_statistics()
        total_calls = stats.get("calls", 0)
        total_pages = (total_calls + per_page - 1) // per_page

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
        per_page = 50
        offset = (page - 1) * per_page
        callbacks = storage.get_all_callback_logs(limit=per_page, offset=offset)
        stats = storage.get_statistics()
        total_callbacks = stats.get("callbacks", 0)
        total_pages = (total_callbacks + per_page - 1) // per_page

        # Parse JSON payload to extract status and SID for each callback
        for callback in callbacks:
            try:
                payload = json.loads(callback["payload"])
                callback["message_status"] = payload.get("MessageStatus")
                callback["call_status"] = payload.get("CallStatus")
                callback["message_sid"] = payload.get("MessageSid")
                callback["call_sid"] = payload.get("CallSid")
            except (json.JSONDecodeError, KeyError):
                callback["message_status"] = None
                callback["call_status"] = None
                callback["message_sid"] = None
                callback["call_sid"] = None

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
        user_agent = request.headers.get("user-agent", "unknown")
        logger.info(f"Request for message details: {message_sid} - UA: {user_agent}")
        message = storage.get_message(message_sid)
        if not message:
            logger.warning(f"Message not found: {message_sid}")
            return HTMLResponse(
                content="<div class='modal-body'><p>Message not found</p></div>",
                status_code=404,
            )

        logger.info(f"Returning message details for {message_sid}")
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
        user_agent = request.headers.get("user-agent", "unknown")
        logger.info(f"Request for call details: {call_sid} - UA: {user_agent}")
        call = storage.get_call(call_sid)
        if not call:
            logger.warning(f"Call not found: {call_sid}")
            return HTMLResponse(
                content="<div class='modal-body'><p>Call not found</p></div>",
                status_code=404,
            )

        logger.info(f"Returning call details for {call_sid}")
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
        user_agent = request.headers.get("user-agent", "unknown")
        logger.info(f"Request for callback details: {callback_id} - UA: {user_agent}")
        callback = storage.get_callback(callback_id)
        if not callback:
            logger.warning(f"Callback not found: {callback_id}")
            return HTMLResponse(
                content="<div class='modal-body'><p>Callback not found</p></div>",
                status_code=404,
            )

        # Parse JSON payload to extract status and SID
        try:
            payload = json.loads(callback["payload"])
            callback["message_status"] = payload.get("MessageStatus")
            callback["call_status"] = payload.get("CallStatus")
            callback["message_sid"] = payload.get("MessageSid")
            callback["call_sid"] = payload.get("CallSid")
        except (json.JSONDecodeError, KeyError):
            callback["message_status"] = None
            callback["call_status"] = None
            callback["message_sid"] = None
            callback["call_sid"] = None

        logger.info(f"Returning callback details for {callback_id}")
        return templates.TemplateResponse(
            "fragments/callback_detail.html",
            {
                "request": request,
                "callback": callback,
            },
        )
