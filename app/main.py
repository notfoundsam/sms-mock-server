"""Main FastAPI application for SMS Mock Server."""
import base64
import logging
from datetime import datetime
from typing import Optional

from fastapi import FastAPI, Request, HTTPException, BackgroundTasks
from fastapi.responses import JSONResponse, Response
from fastapi.staticfiles import StaticFiles

from app.config import load_config
from app.storage import Storage
from app.template_engine import TemplateEngine
from app.providers.twilio import TwilioProvider
from app.callbacks import CallbackHandler
from app.ui import setup_ui_routes

# Setup logging
logging.basicConfig(
    level=logging.INFO,
    format='%(levelname)s:\t%(name)s - %(message)s'
)
logger = logging.getLogger(__name__)

# Load configuration
config = load_config()

# Initialize components
storage = Storage(config.database.path)
template_engine = TemplateEngine(config.templates.path, config.provider)
provider = TwilioProvider(config.twilio)
callback_handler = CallbackHandler(config, storage, template_engine)

# Create FastAPI app
app = FastAPI(
    title="SMS Mock Server",
    description="Mock server for Twilio SMS/Call APIs",
    version="1.0.0",
)

# Mount static files
app.mount("/static", StaticFiles(directory="static"), name="static")

# Setup UI routes
setup_ui_routes(app, storage, config)


def extract_basic_auth(authorization: Optional[str]) -> tuple[Optional[str], Optional[str]]:
    """Extract username and password from Basic Auth header.

    Args:
        authorization: Authorization header value

    Returns:
        Tuple of (username, password) or (None, None)
    """
    if not authorization:
        return None, None

    if not authorization.startswith("Basic "):
        return None, None

    try:
        credentials = base64.b64decode(authorization[6:]).decode("utf-8")
        username, password = credentials.split(":", 1)
        return username, password
    except Exception:
        return None, None


@app.get("/health")
async def health_check():
    """Health check endpoint for Docker and monitoring.

    Returns:
        Health status information
    """
    stats = storage.get_statistics()

    return {
        "status": "healthy",
        "version": "1.0.0",
        "provider": config.provider,
        "timestamp": datetime.utcnow().isoformat() + "Z",
        "statistics": stats,
    }


@app.get("/favicon.ico")
async def favicon():
    """Serve favicon - SMS message bubble icon."""
    svg_icon = """<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">
        <rect width="100" height="100" fill="#3498db"/>
        <path d="M20 25 h60 a8 8 0 0 1 8 8 v30 a8 8 0 0 1 -8 8 h-40 l-15 15 v-15 h-5 a8 8 0 0 1 -8 -8 v-30 a8 8 0 0 1 8 -8 z"
              fill="#ffffff" stroke="#ffffff" stroke-width="2"/>
        <circle cx="35" cy="45" r="3" fill="#3498db"/>
        <circle cx="50" cy="45" r="3" fill="#3498db"/>
        <circle cx="65" cy="45" r="3" fill="#3498db"/>
    </svg>"""
    return Response(content=svg_icon, media_type="image/svg+xml")


@app.post("/callback-test")
async def callback_test(request: Request):
    """Simple endpoint for testing callbacks that accepts POST requests.

    Returns:
        200 OK with received data
    """
    try:
        form_data = await request.form()
        logger.info(f"Callback test endpoint received: {dict(form_data)}")
        return {"status": "received", "data": dict(form_data)}
    except Exception:
        return {"status": "received"}


@app.post("/clear/messages")
async def clear_messages():
    """Clear all messages from the database.

    Returns:
        Number of messages deleted
    """
    count = storage.clear_messages()
    logger.info(f"Cleared {count} messages")
    return {"deleted": count, "type": "messages"}


@app.post("/clear/calls")
async def clear_calls():
    """Clear all calls from the database.

    Returns:
        Number of calls deleted
    """
    count = storage.clear_calls()
    logger.info(f"Cleared {count} calls")
    return {"deleted": count, "type": "calls"}


@app.post("/clear/callbacks")
async def clear_callbacks():
    """Clear all callback logs from the database.

    Returns:
        Number of callback logs deleted
    """
    count = storage.clear_callbacks()
    logger.info(f"Cleared {count} callback logs")
    return {"deleted": count, "type": "callbacks"}


@app.post("/clear/all")
async def clear_all():
    """Clear all data from the database.

    Returns:
        Counts of deleted records by type
    """
    counts = storage.clear_all()
    logger.info(f"Cleared all data: {counts}")
    return {"deleted": counts, "type": "all"}


@app.post("/2010-04-01/Accounts/{account_sid}/Messages.json")
async def send_message(
    account_sid: str,
    request: Request,
    background_tasks: BackgroundTasks,
):
    """Twilio-compatible SMS sending endpoint.

    Args:
        account_sid: Account SID from URL
        request: FastAPI request object
        background_tasks: Background tasks for callbacks

    Returns:
        JSON response matching Twilio format
    """
    # Extract authentication
    auth_header = request.headers.get("authorization")
    username, password = extract_basic_auth(auth_header)

    # Validate authentication
    is_valid, error = provider.validate_auth(username, password)
    if not is_valid:
        error_response = template_engine.render_error(
            provider.get_error_template(error["error_type"]),
            error,
        )
        return JSONResponse(
            status_code=error["http_status"],
            content=error_response,
        )

    # Parse form data (Twilio sends form-encoded data)
    form_data = await request.form()
    request_data = dict(form_data)

    # Validate required parameters
    required_params = ["From", "To", "Body"]
    is_valid, error = provider.validate_parameters(request_data, required_params)
    if not is_valid:
        error_response = template_engine.render_error(
            provider.get_error_template(error["error_type"]),
            error,
        )
        return JSONResponse(
            status_code=error["http_status"],
            content=error_response,
        )

    # Validate phone number formats
    for field in ["From", "To"]:
        is_valid, error = provider.validate_phone_number(request_data[field], field)
        if not is_valid:
            error_response = template_engine.render_error(
                provider.get_error_template(error["error_type"]),
                error,
            )
            return JSONResponse(
                status_code=error["http_status"],
                content=error_response,
            )

    # Validate From number
    is_valid, error = provider.validate_from_number(request_data["From"])
    if not is_valid:
        error_response = template_engine.render_error(
            provider.get_error_template(error["error_type"]),
            error,
        )
        return JSONResponse(
            status_code=error["http_status"],
            content=error_response,
        )

    # Determine if message should succeed
    will_succeed = provider.should_succeed(request_data["To"])

    # Generate message SID
    message_sid = template_engine.generate_sid("SM")

    # Create response context
    context = template_engine.create_message_context(
        message_sid=message_sid,
        account_sid=config.twilio.account_sid,
        request_data=request_data,
        status="queued",
    )

    # Get template and render response
    template_name = provider.get_response_template("send_sms", True)
    response_data = template_engine.render_response(template_name, context)

    # Store message in database
    callback_url = request_data.get("StatusCallback")
    storage.create_message(
        message_sid=message_sid,
        provider=config.provider,
        from_number=request_data["From"],
        to_number=request_data["To"],
        body=request_data["Body"],
        status="queued",
        callback_url=callback_url,
    )

    logger.info(
        f"Message created: {message_sid} from {request_data['From']} to {request_data['To']} "
        f"(will_succeed={will_succeed})"
    )

    # Always queue status updates (callbacks sent only if URL provided and enabled)
    background_tasks.add_task(
        callback_handler.process_message_callbacks,
        message_sid=message_sid,
        from_number=request_data["From"],
        to_number=request_data["To"],
        callback_url=callback_url if (callback_url and config.twilio.callbacks.enabled) else None,
        will_succeed=will_succeed,
    )

    return JSONResponse(status_code=201, content=response_data)


@app.post("/2010-04-01/Accounts/{account_sid}/Calls.json")
async def make_call(
    account_sid: str,
    request: Request,
    background_tasks: BackgroundTasks,
):
    """Twilio-compatible call making endpoint.

    Args:
        account_sid: Account SID from URL
        request: FastAPI request object
        background_tasks: Background tasks for callbacks

    Returns:
        JSON response matching Twilio format
    """
    # Extract authentication
    auth_header = request.headers.get("authorization")
    username, password = extract_basic_auth(auth_header)

    # Validate authentication
    is_valid, error = provider.validate_auth(username, password)
    if not is_valid:
        error_response = template_engine.render_error(
            provider.get_error_template(error["error_type"]),
            error,
        )
        return JSONResponse(
            status_code=error["http_status"],
            content=error_response,
        )

    # Parse form data
    form_data = await request.form()
    request_data = dict(form_data)

    # Validate required parameters
    required_params = ["From", "To", "Url"]
    is_valid, error = provider.validate_parameters(request_data, required_params)
    if not is_valid:
        error_response = template_engine.render_error(
            provider.get_error_template(error["error_type"]),
            error,
        )
        return JSONResponse(
            status_code=error["http_status"],
            content=error_response,
        )

    # Validate phone number formats
    for field in ["From", "To"]:
        is_valid, error = provider.validate_phone_number(request_data[field], field)
        if not is_valid:
            error_response = template_engine.render_error(
                provider.get_error_template(error["error_type"]),
                error,
            )
            return JSONResponse(
                status_code=error["http_status"],
                content=error_response,
            )

    # Validate From number
    is_valid, error = provider.validate_from_number(request_data["From"])
    if not is_valid:
        error_response = template_engine.render_error(
            provider.get_error_template(error["error_type"]),
            error,
        )
        return JSONResponse(
            status_code=error["http_status"],
            content=error_response,
        )

    # Determine if call should succeed
    will_succeed = provider.should_succeed(request_data["To"])

    # Generate call SID
    call_sid = template_engine.generate_sid("CA")

    # Create response context
    context = template_engine.create_call_context(
        call_sid=call_sid,
        account_sid=config.twilio.account_sid,
        request_data=request_data,
        status="queued",
    )

    # Get template and render response
    template_name = provider.get_response_template("make_call", True)
    response_data = template_engine.render_response(template_name, context)

    # Store call in database
    callback_url = request_data.get("StatusCallback")
    twiml_url = request_data.get("Url")
    storage.create_call(
        call_sid=call_sid,
        provider=config.provider,
        from_number=request_data["From"],
        to_number=request_data["To"],
        status="queued",
        callback_url=callback_url,
        twiml_url=twiml_url,
    )

    logger.info(
        f"Call created: {call_sid} from {request_data['From']} to {request_data['To']} "
        f"(will_succeed={will_succeed})"
    )

    # Always queue status updates (callbacks sent only if URL provided and enabled)
    background_tasks.add_task(
        callback_handler.process_call_callbacks,
        call_sid=call_sid,
        from_number=request_data["From"],
        to_number=request_data["To"],
        callback_url=callback_url if (callback_url and config.twilio.callbacks.enabled) else None,
        will_succeed=will_succeed,
    )

    return JSONResponse(status_code=201, content=response_data)


if __name__ == "__main__":
    import uvicorn

    uvicorn.run(
        app,
        host=config.server.host,
        port=config.server.port,
        log_level="info",
    )
