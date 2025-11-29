"""Tests for utility functions in app/main.py."""

import base64

from app.main import extract_basic_auth


class TestExtractBasicAuth:
    """Tests for extract_basic_auth function."""

    def test_valid_basic_auth(self):
        """Test extraction with valid Basic Auth header."""
        username = "test_user"
        password = "test_pass"
        credentials = base64.b64encode(f"{username}:{password}".encode()).decode()
        auth_header = f"Basic {credentials}"

        result_user, result_pass = extract_basic_auth(auth_header)

        assert result_user == username
        assert result_pass == password

    def test_missing_authorization_header(self):
        """Test with missing Authorization header returns (None, None)."""
        result_user, result_pass = extract_basic_auth(None)

        assert result_user is None
        assert result_pass is None

    def test_invalid_format_not_basic(self):
        """Test with Authorization header not starting with 'Basic '."""
        auth_header = "Bearer some_token_here"

        result_user, result_pass = extract_basic_auth(auth_header)

        assert result_user is None
        assert result_pass is None

    def test_malformed_base64(self):
        """Test with malformed base64 in header."""
        auth_header = "Basic not_valid_base64!!!"

        result_user, result_pass = extract_basic_auth(auth_header)

        assert result_user is None
        assert result_pass is None

    def test_missing_colon_in_credentials(self):
        """Test with valid base64 but no colon separator."""
        credentials = base64.b64encode(b"usernameonly").decode()
        auth_header = f"Basic {credentials}"

        result_user, result_pass = extract_basic_auth(auth_header)

        # Should handle ValueError from split
        assert result_user is None
        assert result_pass is None

    def test_empty_username(self):
        """Test with empty username but valid format."""
        username = ""
        password = "test_pass"
        credentials = base64.b64encode(f"{username}:{password}".encode()).decode()
        auth_header = f"Basic {credentials}"

        result_user, result_pass = extract_basic_auth(auth_header)

        assert result_user == ""
        assert result_pass == password

    def test_empty_password(self):
        """Test with empty password but valid format."""
        username = "test_user"
        password = ""
        credentials = base64.b64encode(f"{username}:{password}".encode()).decode()
        auth_header = f"Basic {credentials}"

        result_user, result_pass = extract_basic_auth(auth_header)

        assert result_user == username
        assert result_pass == ""

    def test_password_with_colon(self):
        """Test password containing colon character."""
        username = "test_user"
        password = "pass:word:with:colons"
        credentials = base64.b64encode(f"{username}:{password}".encode()).decode()
        auth_header = f"Basic {credentials}"

        result_user, result_pass = extract_basic_auth(auth_header)

        assert result_user == username
        assert result_pass == password
