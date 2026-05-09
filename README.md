# SMS Mock Server

A mock server for Twilio SMS and Call APIs, perfect for development and testing without sending real messages or making real calls.

## Features

- **Twilio-compatible API** - Drop-in replacement for Twilio SMS/Call APIs
- **Configurable behavior** - Control success/failure scenarios via configuration
- **Callback simulation** - Automatic delivery status callbacks with configurable delays
- **Validation** - Toggleable authentication, phone format, and parameter validation
- **Web UI** - Mailbox-style interface to inspect received messages and calls, with user-defined tags supplied via the `X-Tags` HTTP header
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
docker compose up -d

# Or create your own docker-compose.yml:
services:
  sms-mock-server:
    image: notfoundsam/sms-mock-server:latest
    ports:
      - "8080:8080"
    volumes:
      - ./config.yaml:/app/config.yaml
```

### Persistence

By default the SQLite DB lives at `/tmp/mock_server.db` inside the container and is wiped on container restart — fine for the typical "fresh state per test run" use case. To persist data across restarts, set `SMS_MOCK_DB_PATH` to a path inside a mounted volume:

```yaml
services:
  sms-mock-server:
    image: notfoundsam/sms-mock-server:latest
    ports:
      - "8080:8080"
    volumes:
      - ./config.yaml:/app/config.yaml
      - ./data:/data
    environment:
      - SMS_MOCK_DB_PATH=/data/mock_server.db
```

On Linux you must chown the host directory to UID 65532 first (the distroless `nonroot` user the container runs as): `mkdir -p ./data && sudo chown -R 65532:65532 ./data`. Docker Desktop on macOS/Windows handles UID translation automatically.

### Option 3: Local Go build

```bash
# Build a static binary into ./bin/sms-mock-server
make build

# Run it against the local config.yaml
./bin/sms-mock-server -config config.yaml

# Or skip the build step and use `go run`:
make run
```

Requirements: Go 1.25+ (matches `go.mod`'s declared toolchain). The binary is fully static (`CGO_ENABLED=0`) and embeds all templates and static assets, so the running binary needs nothing besides `config.yaml` and a writable directory for the SQLite DB.

## Configuration

Edit `config.yaml` to customize the mock server behavior:

```yaml
server:
  host: 0.0.0.0
  port: 8080
  timezone: UTC  # Timezone for UI date display (e.g., America/New_York, Asia/Tokyo)

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

Access the web UI at `http://localhost:8080`. The layout is a mailbox-style
inbox: top bar with search, left sidebar with type nav and tags, list pane
that opens records as full pages.

- **Messages** (`/`) — list of received SMS, newest first. Unread rows are bold
  with a leading dot.
- **Calls** (`/calls`) — list of calls in the same shape (no message body).
- **Tags** — sidebar section that appears only when at least one record of the
  active type carries a tag. Tags are user-defined and per-type (the messages
  sidebar shows only tags attached to messages; same for calls). Click a tag
  to filter; click again or click another tag to switch (single-select).
  See [Tagging Messages](#tagging-messages) below.
- **Search** — top-bar input. Free text filters across `From`, `To`, and
  `Body` (calls: `From`/`To` only). Inline `tag:foo` operators are supported
  in the same box (e.g. `verify tag:auth` finds messages containing "verify"
  AND tagged `auth`). Live as you type; press Enter for a shareable URL.
- **Detail view** — clicking a row opens `/view/messages/{sid}` (or
  `/view/calls/{sid}`). Marks the record read on open. Shows a one-line
  callback delivery summary if any webhook was sent and the record's tags as
  pill chips. Filter state is preserved through to the Back button.
- **Delete** — per-row delete on hover; "Delete all" button at the bottom of
  the sidebar (calls `POST /clear/messages` or `/clear/calls`).
- **Real-time** — list and sidebar poll every 3 seconds via HTMX, so new
  records appear without a manual reload.

The dashboard, the Callbacks page, and modal-style detail views are gone.
Callback delivery info now lives inline on the relevant message/call detail
page; raw callback log rows remain queryable via
`SELECT * FROM callback_logs;` against the SQLite DB.

### Tagging Messages

Clients can attach arbitrary tags to a message or call by sending an `X-Tags`
HTTP header on the Twilio-shaped POST. The Twilio request body is unchanged —
tags are out-of-band metadata for the mock UI's benefit only.

```bash
curl -X POST http://localhost:8080/2010-04-01/Accounts/AC1/Messages.json \
  -H 'X-Tags: verification, auth' \
  -d 'From=%2B15550000001&To=%2B15551234567&Body=Your+code+is+1234'
```

- **Format**: comma-separated names. Whitespace around each name is trimmed,
  empty entries are dropped, names are lowercased on the server, duplicates
  are folded.
- **Multiple tags**: a record can carry zero or more tags. The query
  `tag:verification tag:auth` finds records that have both (AND-semantics).
- **Per-type isolation**: a tag attached only to a call appears in the Calls
  sidebar, not in the Messages sidebar.

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
services:
  sms-mock-server:
    image: notfoundsam/sms-mock-server:latest
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

### Makefile Commands

```bash
# Go build / test
make build         # Build static binary into ./bin/sms-mock-server
make run           # Run the server directly (go run)
make test          # Go unit tests
make test-race     # Go unit tests with -race
make lint          # golangci-lint run ./... (41 linters, see .golangci.yml)
make tidy          # go mod tidy
make version       # Print the build version that `make build` would stamp

# Docker / docker compose (uses published image from Docker Hub)
make up               # Pull latest image + start container
make stop             # Stop container
make restart          # Restart container
make clean            # Stop + remove volumes
make logs             # Tail container logs
make docker-snapshot  # Build a local image via goreleaser (no push)

# Data helpers
make seed          # Seed sample messages/calls (against running server on :8080)

make help          # Show this list
```

### Local Development

The server is a single Go binary; templates, static assets, and migrations are all embedded at build time. No external runtime tooling is required.

```bash
# Run unit tests
go test ./...
go test -race ./...        # with the race detector

# Lint (requires golangci-lint installed)
make lint

# Run the server (auto-reloading is not built in; rebuild + restart on changes)
make run
```

Test coverage:
- **Per-package unit tests** (`app/<pkg>/*_test.go`) using stdlib `testing` + `testify`. Includes table-driven validation matrices, fakes for storage / HTTP / clock.
- **End-to-end smoke test** (`app/main_test.go`) builds the full stack via `httptest.NewServer` and exercises POST Messages → persistence → `/health` → messages page → static asset → `/clear/all`.
- All HTTP endpoints (Twilio Messages/Calls, `/health`, `/clear/*`, `/callback-test`, `/favicon.ico`, mailbox/detail pages, UI fragments) are covered by handler-level tests in `app/httpapi/` and `app/ui/`.

## Project Structure

```
sms-mock-server/
├── app/                       # all Go source (flat layout, single binary)
│   ├── main.go                # entrypoint
│   ├── main_test.go           # end-to-end smoke test (httptest.NewServer)
│   ├── embedded.go            # //go:embed templates + static
│   ├── config/                # YAML config loader + validation
│   ├── storage/               # SQLite store, embedded migrations
│   ├── provider/              # Provider interface + types (ValidationError, etc.)
│   │   └── twilio/            # Twilio adapter (auth, validation, outcome)
│   ├── template/              # text/template + html/template engine
│   ├── callback/              # Async dispatcher: worker pool + Clock-driven retries
│   ├── httpapi/               # Twilio API routes, /health, /clear/*, middleware
│   ├── ui/                    # Mailbox pages, detail views, HTMX fragment handlers
│   ├── clock/                 # Clock interface (real + fake for tests)
│   ├── testutil/              # Shared fakes for unit tests
│   ├── templates/             # JSON response/error + HTML UI templates (embedded)
│   │   ├── responses/twilio/
│   │   ├── errors/twilio/
│   │   └── ui/                # base.html + page templates + fragments/
│   └── static/                # CSS, JS, favicon (embedded)
├── scripts/
│   └── seed_data.sh           # Seed sample messages/calls via curl
├── docs/
│   ├── DESIGN.md              # Architecture documentation
│   └── plans/                 # Implementation plans (history)
├── .github/workflows/         # CI (test + lint + shellcheck) + release (goreleaser)
├── config.yaml                # Server configuration
├── Makefile                   # Build / test / docker targets
├── Dockerfile                 # Multi-stage; static binary on distroless/static
├── docker-compose.yml
├── .golangci.yml              # Linter config (41 linters)
├── .goreleaser.yml            # Release automation (binaries + Docker Hub image)
└── go.mod / go.sum
```

## Troubleshooting

**Authentication errors even with correct credentials:**
- Make sure you updated `account_sid` and `auth_token` in `config.yaml`
- Or set `validation.require_auth: false` for quick testing

**Callbacks not being received:**
- Check that `callbacks.enabled: true` in config
- Verify the `To` number is in `registered_numbers` (success flow) or `failure_numbers` (failure flow). Numbers in *neither* list stay queued forever and produce no callbacks — this is intentional, mirroring the original Python behavior.
- Verify your callback URL is accessible from the mock server
- For local testing, use the built-in `/callback-test` endpoint: `http://localhost:8080/callback-test`
- Inspect callback delivery on the message/call detail page (`/view/messages/{sid}` shows a "Callback delivery" summary if any webhooks were sent for that record). For raw rows, query the SQLite DB directly: `sqlite3 /tmp/sms-mock.db 'SELECT * FROM callback_logs;'`

**Phone number validation errors:**
- Use E.164 format: `+15551234567` (with `+` and country code)
- Or set `validation.validate_phone_format: false`
- Note: `+1555...` numbers (NANP fictional-use) are **rejected by libphonenumber** when `validate_phone_format: true`. Use real-looking numbers like `+12025550100` (DC area code) for testing with strict validation, or disable the format check for permissive testing.

## License

MIT License - See [docs/DESIGN.md](docs/DESIGN.md) for architecture details

## Contributing

Contributions welcome! This is a development tool, so focus on:
- Simplicity over features
- Compatibility with Twilio SDKs
- Easy configuration and debugging
