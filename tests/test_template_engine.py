"""Tests for template engine module."""

import json
import string

import pytest
from freezegun import freeze_time

from app.template_engine import TemplateEngine


class TestTemplateEngineInitialization:
    """Tests for TemplateEngine initialization."""

    def test_init_with_default_provider(self, tmp_path):
        """Test TemplateEngine initialization with default provider."""
        templates_path = tmp_path / "responses"
        templates_path.mkdir()
        errors_path = tmp_path / "errors"
        errors_path.mkdir()

        engine = TemplateEngine(str(templates_path))
        assert engine.provider == "twilio"
        assert engine.templates_path == templates_path

    def test_init_with_custom_provider(self, tmp_path):
        """Test TemplateEngine initialization with custom provider."""
        templates_path = tmp_path / "responses"
        templates_path.mkdir()
        errors_path = tmp_path / "errors"
        errors_path.mkdir()

        engine = TemplateEngine(str(templates_path), provider="custom")
        assert engine.provider == "custom"


class TestGenerateSid:
    """Tests for generate_sid method."""

    @pytest.fixture
    def engine(self, tmp_path):
        """Create TemplateEngine for testing."""
        templates_path = tmp_path / "responses"
        templates_path.mkdir()
        errors_path = tmp_path / "errors"
        errors_path.mkdir()
        return TemplateEngine(str(templates_path))

    def test_generate_sid_default_prefix(self, engine):
        """Test generating SID with default prefix."""
        sid = engine.generate_sid()
        assert sid.startswith("SM")
        assert len(sid) == 34

    def test_generate_sid_custom_prefix(self, engine):
        """Test generating SID with custom prefix."""
        sid = engine.generate_sid(prefix="CA")
        assert sid.startswith("CA")
        assert len(sid) == 34

    def test_generate_sid_uniqueness(self, engine):
        """Test that generate_sid creates unique SIDs."""
        sids = {engine.generate_sid() for _ in range(100)}
        assert len(sids) == 100

    def test_generate_sid_valid_characters(self, engine):
        """Test that generated SID contains only valid characters."""
        sid = engine.generate_sid()
        sid_body = sid[2:]
        valid_chars = string.ascii_lowercase + string.digits
        assert all(char in valid_chars for char in sid_body)


class TestTimestampMethods:
    """Tests for timestamp generation methods."""

    @pytest.fixture
    def engine(self, tmp_path):
        """Create TemplateEngine for testing."""
        templates_path = tmp_path / "responses"
        templates_path.mkdir()
        errors_path = tmp_path / "errors"
        errors_path.mkdir()
        return TemplateEngine(str(templates_path))

    @freeze_time("2024-01-15 10:30:00")
    def test_get_timestamp(self, engine):
        """Test RFC 2822 timestamp generation."""
        timestamp = engine.get_timestamp()
        assert timestamp == "Mon, 15 Jan 2024 10:30:00 +0000"

    @freeze_time("2024-12-25 23:59:59")
    def test_get_timestamp_specific_date(self, engine):
        """Test RFC 2822 timestamp for specific date."""
        timestamp = engine.get_timestamp()
        assert timestamp == "Wed, 25 Dec 2024 23:59:59 +0000"

    @freeze_time("2024-01-15 10:30:00")
    def test_get_iso_timestamp(self, engine):
        """Test ISO 8601 timestamp generation."""
        timestamp = engine.get_iso_timestamp()
        assert timestamp == "2024-01-15T10:30:00Z"

    @freeze_time("2024-12-25 23:59:59")
    def test_get_iso_timestamp_specific_date(self, engine):
        """Test ISO 8601 timestamp for specific date."""
        timestamp = engine.get_iso_timestamp()
        assert timestamp == "2024-12-25T23:59:59Z"


class TestCalculateSmsSegments:
    """Tests for calculate_sms_segments static method."""

    def test_empty_message(self):
        """Test calculation for empty message."""
        segments = TemplateEngine.calculate_sms_segments("")
        assert segments == 1

    def test_single_segment_ascii_short(self):
        """Test single segment for short ASCII message."""
        message = "Hello World!"
        segments = TemplateEngine.calculate_sms_segments(message)
        assert segments == 1

    def test_single_segment_ascii_max(self):
        """Test single segment for max length ASCII message (160 chars)."""
        message = "a" * 160
        segments = TemplateEngine.calculate_sms_segments(message)
        assert segments == 1

    def test_multi_segment_ascii(self):
        """Test multiple segments for long ASCII message."""
        message = "a" * 161
        segments = TemplateEngine.calculate_sms_segments(message)
        assert segments == 2

    def test_multi_segment_ascii_exact_fit(self):
        """Test multiple segments for ASCII message exactly 306 chars (2 segments)."""
        message = "a" * 306
        segments = TemplateEngine.calculate_sms_segments(message)
        assert segments == 2

    def test_multi_segment_ascii_three_segments(self):
        """Test three segments for long ASCII message."""
        message = "a" * 307
        segments = TemplateEngine.calculate_sms_segments(message)
        assert segments == 3

    def test_single_segment_unicode_short(self):
        """Test single segment for short Unicode message."""
        message = "Hello 世界"
        segments = TemplateEngine.calculate_sms_segments(message)
        assert segments == 1

    def test_single_segment_unicode_max(self):
        """Test single segment for max length Unicode message (70 chars)."""
        message = "世" * 70
        segments = TemplateEngine.calculate_sms_segments(message)
        assert segments == 1

    def test_multi_segment_unicode(self):
        """Test multiple segments for long Unicode message."""
        message = "世" * 71
        segments = TemplateEngine.calculate_sms_segments(message)
        assert segments == 2

    def test_multi_segment_unicode_exact_fit(self):
        """Test multiple segments for Unicode message exactly 134 chars (2 segments)."""
        message = "世" * 134
        segments = TemplateEngine.calculate_sms_segments(message)
        assert segments == 2

    def test_multi_segment_unicode_three_segments(self):
        """Test three segments for long Unicode message."""
        message = "世" * 135
        segments = TemplateEngine.calculate_sms_segments(message)
        assert segments == 3


class TestRenderMethods:
    """Tests for template rendering methods."""

    @pytest.fixture
    def engine_with_templates(self, tmp_path):
        """Create TemplateEngine with test templates."""
        # Create response templates
        responses_path = tmp_path / "responses" / "twilio"
        responses_path.mkdir(parents=True)

        message_template = responses_path / "message.json"
        message_template.write_text(
            json.dumps(
                {
                    "sid": "{{ message_sid }}",
                    "account_sid": "{{ account_sid }}",
                    "from": "{{ request.From }}",
                    "to": "{{ request.To }}",
                    "body": "{{ request.Body }}",
                    "status": "{{ status }}",
                    "num_segments": "{{ num_segments }}",
                    "date_created": "{{ date_created }}",
                    "date_updated": "{{ date_updated }}",
                }
            )
        )

        # Create error templates
        errors_path = tmp_path / "errors" / "twilio"
        errors_path.mkdir(parents=True)

        error_template = errors_path / "error.json"
        error_template.write_text(
            json.dumps(
                {
                    "code": "{{ code }}",
                    "message": "{{ message }}",
                    "status": "{{ status }}",
                }
            )
        )

        return TemplateEngine(str(tmp_path / "responses"))

    @freeze_time("2024-01-15 10:30:00")
    def test_render_response(self, engine_with_templates):
        """Test rendering response template."""
        context = {
            "message_sid": "SM123",
            "account_sid": "AC456",
            "request": {
                "From": "+1234567890",
                "To": "+0987654321",
                "Body": "Test message",
            },
            "status": "queued",
            "num_segments": 1,
        }

        result = engine_with_templates.render_response("message.json", context)

        assert result["sid"] == "SM123"
        assert result["account_sid"] == "AC456"
        assert result["from"] == "+1234567890"
        assert result["to"] == "+0987654321"
        assert result["body"] == "Test message"
        assert result["status"] == "queued"
        assert result["num_segments"] == "1"
        assert result["date_created"] == "Mon, 15 Jan 2024 10:30:00 +0000"
        assert result["date_updated"] == "Mon, 15 Jan 2024 10:30:00 +0000"

    @freeze_time("2024-01-15 10:30:00")
    def test_render_error(self, engine_with_templates):
        """Test rendering error template."""
        context = {"code": 21211, "message": "Invalid phone number", "status": 400}

        result = engine_with_templates.render_error("error.json", context)

        # JSON renders numbers as strings when using Jinja templates
        assert result["code"] == "21211"
        assert result["message"] == "Invalid phone number"
        assert result["status"] == "400"


class TestCreateMessageContext:
    """Tests for create_message_context method."""

    @pytest.fixture
    def engine(self, tmp_path):
        """Create TemplateEngine for testing."""
        templates_path = tmp_path / "responses"
        templates_path.mkdir()
        errors_path = tmp_path / "errors"
        errors_path.mkdir()
        return TemplateEngine(str(templates_path))

    @freeze_time("2024-01-15 10:30:00")
    def test_create_message_context_basic(self, engine):
        """Test creating basic message context."""
        request_data = {"From": "+1234567890", "To": "+0987654321", "Body": "Hello"}

        context = engine.create_message_context(
            message_sid="SM123",
            account_sid="AC456",
            request_data=request_data,
            status="queued",
        )

        assert context["message_sid"] == "SM123"
        assert context["account_sid"] == "AC456"
        assert context["request"] == request_data
        assert context["status"] == "queued"
        assert context["num_segments"] == 1
        assert context["date_created"] == "Mon, 15 Jan 2024 10:30:00 +0000"

    def test_create_message_context_multi_segment(self, engine):
        """Test creating message context with multi-segment message."""
        request_data = {"From": "+1234567890", "To": "+0987654321", "Body": "a" * 200}

        context = engine.create_message_context(
            message_sid="SM123", account_sid="AC456", request_data=request_data
        )

        assert context["num_segments"] == 2


class TestCreateCallContext:
    """Tests for create_call_context method."""

    @pytest.fixture
    def engine(self, tmp_path):
        """Create TemplateEngine for testing."""
        templates_path = tmp_path / "responses"
        templates_path.mkdir()
        errors_path = tmp_path / "errors"
        errors_path.mkdir()
        return TemplateEngine(str(templates_path))

    @freeze_time("2024-01-15 10:30:00")
    def test_create_call_context(self, engine):
        """Test creating call context."""
        request_data = {
            "From": "+1234567890",
            "To": "+0987654321",
            "Url": "http://example.com/twiml",
        }

        context = engine.create_call_context(
            call_sid="CA123",
            account_sid="AC456",
            request_data=request_data,
            status="queued",
        )

        assert context["call_sid"] == "CA123"
        assert context["account_sid"] == "AC456"
        assert context["request"] == request_data
        assert context["status"] == "queued"
        assert context["date_created"] == "Mon, 15 Jan 2024 10:30:00 +0000"


class TestCreateDeliveryStatusContext:
    """Tests for create_delivery_status_context method."""

    @pytest.fixture
    def engine(self, tmp_path):
        """Create TemplateEngine for testing."""
        templates_path = tmp_path / "responses"
        templates_path.mkdir()
        errors_path = tmp_path / "errors"
        errors_path.mkdir()
        return TemplateEngine(str(templates_path))

    @freeze_time("2024-01-15 10:30:00")
    def test_create_delivery_status_context(self, engine):
        """Test creating delivery status context."""
        context = engine.create_delivery_status_context(
            message_sid="SM123",
            account_sid="AC456",
            from_number="+1234567890",
            to_number="+0987654321",
            status="delivered",
        )

        assert context["MessageSid"] == "SM123"
        assert context["AccountSid"] == "AC456"
        assert context["From"] == "+1234567890"
        assert context["To"] == "+0987654321"
        assert context["MessageStatus"] == "delivered"
        assert context["timestamp"] == "2024-01-15T10:30:00Z"
