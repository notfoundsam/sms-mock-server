"""
Example: Using Twilio Python SDK with SMS Mock Server

Install dependencies:
    pip install twilio

Usage:
    python examples/python_example.py
"""

from twilio.rest import Client
from twilio.http.http_client import TwilioHttpClient

# Mock server configuration
MOCK_SERVER_URL = 'http://localhost:8080'
ACCOUNT_SID = 'ACXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX'
AUTH_TOKEN = 'your_auth_token_here'

# Configure HTTP client to point to mock server
http_client = TwilioHttpClient()
http_client.api_base_url = MOCK_SERVER_URL

# Create Twilio client
client = Client(ACCOUNT_SID, AUTH_TOKEN, http_client=http_client)


def send_sms_example():
    """Send SMS via mock server."""
    print("Sending SMS...")

    message = client.messages.create(
        to='+15551234567',        # Registered number (will succeed)
        from_='+15550000001',     # Allowed From number
        body='Hello from mock server!',
        status_callback='http://your-app.com/sms-callback'
    )

    print(f"✓ Message sent successfully!")
    print(f"  SID: {message.sid}")
    print(f"  Status: {message.status}")
    print(f"  From: {message.from_}")
    print(f"  To: {message.to}")
    print(f"  Body: {message.body}")
    print()


def send_sms_to_failure_number():
    """Send SMS to a failure number."""
    print("Sending SMS to failure number...")

    message = client.messages.create(
        to='+15559999999',        # Failure number (will fail)
        from_='+15550000001',
        body='This message will fail',
        status_callback='http://your-app.com/sms-callback'
    )

    print(f"✓ Message created (will fail during delivery)")
    print(f"  SID: {message.sid}")
    print(f"  Status: {message.status}")
    print()


def make_call_example():
    """Make call via mock server."""
    print("Making call...")

    call = client.calls.create(
        to='+15551234567',
        from_='+15550000001',
        url='http://your-twiml-server.com/voice',
        status_callback='http://your-app.com/call-callback'
    )

    print(f"✓ Call initiated successfully!")
    print(f"  SID: {call.sid}")
    print(f"  Status: {call.status}")
    print(f"  From: {call.from_}")
    print(f"  To: {call.to}")
    print()


def test_validation_errors():
    """Test various validation errors."""
    print("Testing validation errors...\n")

    # Test 1: Missing From parameter
    print("1. Testing missing From parameter...")
    try:
        message = client.messages.create(
            to='+15551234567',
            body='Missing From parameter'
        )
    except Exception as e:
        print(f"   ✓ Got expected error: {e}\n")

    # Test 2: Invalid phone number format
    print("2. Testing invalid phone number format...")
    try:
        message = client.messages.create(
            to='invalid-number',
            from_='+15550000001',
            body='Invalid number'
        )
    except Exception as e:
        print(f"   ✓ Got expected error: {e}\n")

    # Test 3: Invalid From number (not in allowed list)
    print("3. Testing invalid From number...")
    try:
        message = client.messages.create(
            to='+15551234567',
            from_='+15559999999',  # Not in allowed_from_numbers
            body='Invalid From number'
        )
    except Exception as e:
        print(f"   ✓ Got expected error: {e}\n")


if __name__ == '__main__':
    print("=" * 60)
    print("SMS Mock Server - Python SDK Example")
    print("=" * 60)
    print()

    # Run examples
    send_sms_example()
    send_sms_to_failure_number()
    make_call_example()
    test_validation_errors()

    print("=" * 60)
    print("All examples completed!")
    print("Check the web UI at http://localhost:8080")
    print("=" * 60)
