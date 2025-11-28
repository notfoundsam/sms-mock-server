"""Shared pytest fixtures for SMS Mock Server tests."""
import pytest
import tempfile
from pathlib import Path
from unittest.mock import AsyncMock, Mock, patch
import asyncio

from app.config import Config
from app.storage import Storage
from app.template_engine import TemplateEngine


@pytest.fixture
def test_config_dict():
    """Return a test configuration dictionary."""
    return {
        "server": {
            "host": "0.0.0.0",
            "port": 8080
        },
        "provider": "twilio",
        "twilio": {
            "account_sid": "AC" + "x" * 32,
            "auth_token": "test_auth_token_12345",
            "validation": {
                "require_auth": True,
                "validate_phone_format": True,
                "check_from_numbers": True,
                "require_parameters": True
            },
            "default_behavior": "success",
            "registered_numbers": [
                "+1111111111",
                "+15551234567",
                "+15559876543"
            ],
            "allowed_from_numbers": [
                "+15550000001",
                "+15550000002",
                "+1234567890"
            ],
            "failure_numbers": [
                "+2222222222",
                "+15559999999"
            ],
            "callbacks": {
                "enabled": True,
                "delay_seconds": 2,
                "retry_attempts": 3,
                "retry_delay_seconds": 5
            }
        },
        "database": {
            "path": ":memory:"
        },
        "templates": {
            "path": "./templates/responses"
        }
    }


@pytest.fixture
def test_config_file(tmp_path, test_config_dict):
    """Create a temporary test configuration file."""
    import yaml

    # Create templates directory
    templates_dir = tmp_path / "templates" / "responses"
    templates_dir.mkdir(parents=True)

    # Update config dict to use temp templates path
    test_config_dict["templates"]["path"] = str(templates_dir)

    # Create config file
    config_file = tmp_path / "config.yaml"
    with open(config_file, "w") as f:
        yaml.dump(test_config_dict, f)

    return str(config_file)


@pytest.fixture
def test_config(test_config_file):
    """Create a test Config instance."""
    return Config(test_config_file)


@pytest.fixture
def test_storage(tmp_path):
    """Create a test Storage instance with temporary file database."""
    db_file = tmp_path / "test.db"
    storage = Storage(str(db_file))
    yield storage
    # Cleanup: remove the temporary database file
    if db_file.exists():
        db_file.unlink()


@pytest.fixture
def test_template_engine(tmp_path):
    """Create a test TemplateEngine instance."""
    # Create templates directory
    templates_dir = tmp_path / "templates" / "responses"
    templates_dir.mkdir(parents=True, exist_ok=True)

    # Create errors directory
    errors_dir = tmp_path / "templates" / "errors"
    errors_dir.mkdir(parents=True, exist_ok=True)

    return TemplateEngine(
        templates_path=str(templates_dir),
        provider="twilio"
    )


@pytest.fixture
def mock_httpx_client():
    """Mock httpx.AsyncClient for testing callbacks."""
    mock_client = AsyncMock()
    mock_response = AsyncMock()
    mock_response.status_code = 200
    mock_response.text = "OK"
    mock_client.post.return_value = mock_response

    with patch("httpx.AsyncClient", return_value=mock_client):
        yield mock_client


@pytest.fixture
def mock_async_sleep():
    """Mock asyncio.sleep to avoid delays in tests."""
    with patch("asyncio.sleep", new_callable=AsyncMock) as mock_sleep:
        yield mock_sleep


@pytest.fixture
def sample_message_data():
    """Sample message request data."""
    return {
        "From": "+15550000001",
        "To": "+15551234567",
        "Body": "Test message",
        "StatusCallback": "http://localhost:8080/callback-test"
    }


@pytest.fixture
def sample_call_data():
    """Sample call request data."""
    return {
        "From": "+15550000001",
        "To": "+15551234567",
        "Url": "http://example.com/twiml",
        "StatusCallback": "http://localhost:8080/callback-test"
    }


@pytest.fixture
def mock_basic_auth():
    """Mock basic auth header."""
    import base64
    account_sid = "AC" + "x" * 32
    auth_token = "test_auth_token_12345"
    credentials = base64.b64encode(
        f"{account_sid}:{auth_token}".encode()
    ).decode()
    return f"Basic {credentials}"
