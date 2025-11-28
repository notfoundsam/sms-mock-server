# SMS Mock Server

A mock server for Twilio SMS and Call APIs, perfect for development and testing without sending real messages or making real calls.

## Features

- **Twilio-compatible API** - Drop-in replacement for Twilio SMS/Call APIs
- **Configurable behavior** - Control success/failure scenarios via configuration
- **Callback simulation** - Automatic delivery status callbacks with configurable delays
- **Validation** - Toggleable authentication, phone format, and parameter validation
- **Web UI** - Simple dashboard to monitor messages, calls, and callbacks
- **Docker support** - Easy deployment with Docker and Docker Compose
- **SDK compatible** - Works with official Twilio SDKs (Python, Node.js, PHP, Ruby, Java, C#)

## Quick Start

### Option 1: Docker Hub (Easiest)

```bash
# Pull and run from Docker Hub
docker run -d \
  -p 8080:8080 \
  -v $(pwd)/config.yaml:/app/config.yaml \
  -v $(pwd)/data:/app/data \
  --name sms-mock-server \
  notfoundsam/sms-mock-server:latest

# Access the UI
open http://localhost:8080
```

### Option 2: Docker Compose

```bash
# Using the provided docker-compose.yml
docker-compose up -d

# Or create your own docker-compose.yml:
version: '3.8'
services:
  sms-mock-server:
    image: notfoundsam/sms-mock-server:latest
    ports:
      - "8080:8080"
    volumes:
      - ./config.yaml:/app/config.yaml
      - ./data:/app/data
```

### Option 3: Build from Source

```bash
# Clone the repository
git clone <repository-url>
cd sms-mock-server

# Build and run
docker-compose up -d
```

### Option 4: Local Python

```bash
# Install dependencies
pip install -r requirements.txt

# Run the server
python -m app.main
```

## Configuration

Edit `config.yaml` to customize the mock server behavior:

```yaml
server:
  host: 0.0.0.0
  port: 8080

provider: twilio

twilio:
  account_sid: ACXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
  auth_token: your_auth_token_here

  # Validation settings (toggle on/off)
  validation:
    require_auth: true              # Validate credentials
    validate_phone_format: true     # Check E.164 format
    check_from_numbers: true        # Require From in allowed list
    require_parameters: true        # Validate required params

  # Number behavior
  default_behavior: success  # "success" or "failure"

  registered_numbers:
    - "+15551234567"  # These always succeed
    - "+15559876543"

  allowed_from_numbers:
    - "+15550000001"  # Valid From numbers
    - "+15550000002"

  failure_numbers:
    - "+15559999999"  # These always fail

  # Callback settings
  callbacks:
    enabled: true
    delay_seconds: 2
    retry_attempts: 3
    retry_delay_seconds: 5
```

### Number Behavior Logic

1. **In `failure_numbers`** → Always fails
2. **In `registered_numbers`** → Always succeeds
3. **Not in either** → Uses `default_behavior` setting

## SDK Integration

### Python

```python
from twilio.rest import Client
from twilio.http.http_client import TwilioHttpClient

account_sid = 'ACXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX'
auth_token = 'your_auth_token_here'

# Point to mock server
http_client = TwilioHttpClient()
http_client.api_base_url = 'http://localhost:8080'

client = Client(account_sid, auth_token, http_client=http_client)

# Send SMS
message = client.messages.create(
    to='+15551234567',
    from_='+15550000001',
    body='Hello from mock server!',
    status_callback='http://your-app.com/callback'
)

print(f"Message SID: {message.sid}")
```

### Node.js

```javascript
const twilio = require('twilio');

const accountSid = 'ACXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX';
const authToken = 'your_auth_token_here';

const client = twilio(accountSid, authToken, {
    lazyLoading: true,
    accountSid: accountSid,
    apiBaseUrl: 'http://localhost:8080'
});

// Send SMS
const message = await client.messages.create({
    to: '+15551234567',
    from: '+15550000001',
    body: 'Hello from mock server!',
    statusCallback: 'http://your-app.com/callback'
});

console.log(`Message SID: ${message.sid}`);
```

### PHP

```php
<?php
require_once 'vendor/autoload.php';
use Twilio\Rest\Client;

$accountSid = 'ACXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX';
$authToken = 'your_auth_token_here';
$mockServerUrl = 'http://localhost:8080';

$client = new Client($accountSid, $authToken, $accountSid, null, $mockServerUrl);

$message = $client->messages->create(
    '+15551234567',
    [
        'from' => '+15550000001',
        'body' => 'Hello from mock server!',
        'statusCallback' => 'http://your-app.com/callback'
    ]
);

echo "Message SID: " . $message->sid . "\n";
?>
```

## API Endpoints

### Send SMS

```
POST /2010-04-01/Accounts/{AccountSid}/Messages.json
```

**Parameters:**
- `From` (required) - Sender phone number
- `To` (required) - Recipient phone number
- `Body` (required) - Message text
- `StatusCallback` (optional) - Callback URL for delivery status

**Response:** Standard Twilio message resource JSON

### Make Call

```
POST /2010-04-01/Accounts/{AccountSid}/Calls.json
```

**Parameters:**
- `From` (required) - Caller phone number
- `To` (required) - Callee phone number
- `Url` (required) - TwiML URL
- `StatusCallback` (optional) - Callback URL for call status

**Response:** Standard Twilio call resource JSON

### Health Check

```
GET /health
```

**Response:**
```json
{
  "status": "healthy",
  "version": "1.0.0",
  "provider": "twilio",
  "timestamp": "2024-01-15T10:30:00Z",
  "statistics": {
    "messages": 42,
    "calls": 15,
    "callbacks": 84
  }
}
```

### Callback Test Endpoint

```
POST /callback-test
```

A test endpoint that accepts POST requests (used for testing callbacks locally without external URLs).

**Response:** `{"status": "received", "data": {...}}`

### Clear Data

```
POST /clear/messages    # Clear all messages
POST /clear/calls       # Clear all calls
POST /clear/callbacks   # Clear all callback logs
POST /clear/all         # Clear all data
```

## Web UI

Access the web UI at `http://localhost:8080`:

- **Dashboard** - Overview with statistics and recent activity (auto-refreshes every 3 seconds)
- **Messages** - Paginated list of all SMS messages (50 per page) with click-to-view details modal
- **Calls** - Paginated list of all calls (50 per page) with click-to-view details modal
- **Callbacks** - Paginated log of all callback attempts (50 per page) with responses

All pages auto-refresh every 3 seconds to show new data without manual reload.

## Callback Flow

When you send an SMS/call with a `StatusCallback` URL, the mock server will:

1. Accept the request and return immediately (status: `queued`)
2. Wait for `delay_seconds` (default: 2s)
3. Send status update callbacks:
   - **SMS Success**: queued → sent → delivered
   - **SMS Failure**: queued → failed
   - **Call Success**: queued → ringing → in-progress → completed
   - **Call Failure**: queued → failed
4. Retry failed callbacks up to `retry_attempts` times

## Docker Compose with Your App

```yaml
version: '3.8'

services:
  sms-mock-server:
    build: ./sms-mock-server
    ports:
      - "8080:8080"
    networks:
      - app-network

  your-app:
    build: ./your-app
    depends_on:
      - sms-mock-server
    environment:
      - TWILIO_API_BASE_URL=http://sms-mock-server:8080
      - TWILIO_ACCOUNT_SID=ACXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
      - TWILIO_AUTH_TOKEN=your_auth_token_here
    networks:
      - app-network

networks:
  app-network:
    driver: bridge
```

## Error Scenarios

The mock server emulates common Twilio errors:

| Error | HTTP Status | When |
|-------|-------------|------|
| Authentication Failed | 401 | Invalid credentials |
| Missing Parameter | 400 | Required param missing |
| Invalid Phone Number | 400 | Invalid E.164 format |
| Invalid From Number | 400 | From not in allowed list |

## Development

```bash
# Install dependencies
pip install -r requirements.txt

# Run tests
pytest

# Run server with auto-reload
uvicorn app.main:app --reload --port 8080
```

## Project Structure

```
sms-mock-server/
├── app/
│   ├── main.py              # FastAPI application
│   ├── config.py            # Configuration loader
│   ├── storage.py           # SQLite storage
│   ├── template_engine.py   # Jinja2 template rendering
│   ├── callbacks.py         # Async callback handler
│   ├── ui.py                # Web UI routes
│   └── providers/
│       ├── base.py          # Base provider interface
│       └── twilio.py        # Twilio provider implementation
├── templates/
│   ├── responses/twilio/    # JSON response templates
│   ├── errors/twilio/       # JSON error templates
│   └── ui/                  # HTML templates
├── data/                    # SQLite database
├── config.yaml              # Server configuration
├── Dockerfile
├── docker-compose.yml
└── requirements.txt
```

## Troubleshooting

**Authentication errors even with correct credentials:**
- Make sure you updated `account_sid` and `auth_token` in `config.yaml`
- Or set `validation.require_auth: false` for quick testing

**Callbacks not being received:**
- Check that `callbacks.enabled: true` in config
- Verify the `To` number is in `registered_numbers` list (callbacks only fire for registered numbers)
- Verify your callback URL is accessible from the mock server
- For local testing, use the built-in `/callback-test` endpoint: `http://localhost:8080/callback-test`
- Check callback logs in the UI at `/ui/callbacks`

**Phone number validation errors:**
- Use E.164 format: `+15551234567` (with + and country code)
- Or set `validation.validate_phone_format: false`

## License

MIT License - See DESIGN.md for architecture details

## Contributing

Contributions welcome! This is a development tool, so focus on:
- Simplicity over features
- Compatibility with Twilio SDKs
- Easy configuration and debugging
