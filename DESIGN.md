# SMS Mock Server - Design Document

## 1. Overview

### Purpose
A mock server that simulates SMS carrier APIs (starting with Twilio) for development and testing purposes. The server provides configurable responses, callback functionality, and activity monitoring through a simple web UI.

### Key Features
- Mock SMS sending and phone call APIs
- Configurable responses via JSON templates with variable substitution
- Callback simulation for delivery status and call events
- Simple web UI for browsing activity
- Docker containerized deployment
- Extensible architecture for multiple providers

## 2. High-Level Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      Client Application                      │
└──────────────────┬──────────────────────────────────────────┘
                   │ HTTP Requests (SMS/Call API)
                   ▼
┌─────────────────────────────────────────────────────────────┐
│                    SMS Mock Server                           │
│  ┌──────────────────────────────────────────────────────┐   │
│  │              FastAPI Application                      │   │
│  │  ┌─────────────────┐      ┌─────────────────────┐   │   │
│  │  │  Provider API   │      │    UI Routes        │   │   │
│  │  │  Routes         │      │    (HTML/HTMX)      │   │   │
│  │  └────────┬────────┘      └──────────┬──────────┘   │   │
│  │           │                           │              │   │
│  │           ▼                           ▼              │   │
│  │  ┌─────────────────────────────────────────────┐   │   │
│  │  │         Provider Abstraction Layer          │   │   │
│  │  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  │   │   │
│  │  │  │  Twilio  │  │ Provider │  │ Provider │  │   │   │
│  │  │  │ Adapter  │  │    2     │  │    3     │  │   │   │
│  │  │  └──────────┘  └──────────┘  └──────────┘  │   │   │
│  │  └────────┬────────────────────────────────────┘   │   │
│  │           │                                         │   │
│  │           ▼                                         │   │
│  │  ┌─────────────────────────────────────────────┐   │   │
│  │  │          Core Services                      │   │   │
│  │  │  ┌──────────────┐  ┌──────────────────┐    │   │   │
│  │  │  │  Template    │  │   Callback       │    │   │   │
│  │  │  │  Engine      │  │   Handler        │    │   │   │
│  │  │  └──────────────┘  └────────┬─────────┘    │   │   │
│  │  └─────────────────────────────┼──────────────┘   │   │
│  │                                 │                  │   │
│  │           ┌─────────────────────┴─────────┐        │   │
│  │           ▼                               ▼        │   │
│  │  ┌─────────────────┐            ┌──────────────┐  │   │
│  │  │  Storage Layer  │            │   Config     │  │   │
│  │  │   (SQLite)      │            │   Loader     │  │   │
│  │  └─────────────────┘            └──────────────┘  │   │
│  └──────────────────────────────────────────────────┘   │
└─────────────────────┬───────────────────────────────────┘
                      │ HTTP Callbacks
                      ▼
┌─────────────────────────────────────────────────────────────┐
│                 Client Callback Endpoint                     │
└─────────────────────────────────────────────────────────────┘
```

## 3. Component Details

### 3.1 Provider API Routes
**Responsibility**: Handle incoming API requests matching provider specifications

**Twilio Endpoints**:
- `POST /2010-04-01/Accounts/{AccountSid}/Messages.json` - Send SMS
- `POST /2010-04-01/Accounts/{AccountSid}/Calls.json` - Make call
- Additional endpoints as needed

**Functions**:
- Request validation (auth, parameters)
- Route to appropriate provider adapter
- Return templated responses

### 3.2 Provider Abstraction Layer
**Responsibility**: Define interface for different providers

**Base Provider Interface**:
```python
class BaseProvider:
    def send_sms(request_data) -> response
    def make_call(request_data) -> response

    # Validation methods
    def validate_auth(credentials) -> tuple[bool, error_response | None]
    def validate_parameters(request_data, required_params) -> tuple[bool, error_response | None]
    def validate_phone_number(number, field_name) -> tuple[bool, error_response | None]
    def validate_from_number(number) -> tuple[bool, error_response | None]

    # Behavior determination
    def should_succeed(to_number) -> bool  # Checks registered/failure lists + default_behavior

    # Template methods
    def get_response_template(action, success: bool) -> template
    def get_error_template(error_type) -> template
```

**Twilio Adapter**: Implements Twilio-specific logic and validation

### 3.3 Template Engine
**Responsibility**: Process JSON response templates with variable substitution

**Features**:
- Load templates from `templates/responses/{provider}/`
- Support Jinja2 syntax for variables
- Access to request data, config, and generated values (IDs, timestamps)

**Example Template Variables**:
```json
{
  "sid": "{{ message_sid }}",
  "from": "{{ request.From }}",
  "to": "{{ request.To }}",
  "body": "{{ request.Body }}",
  "status": "{{ status }}",
  "date_created": "{{ timestamp }}"
}
```

### 3.4 Callback Handler
**Responsibility**: Asynchronous callback delivery to client URLs

**Features**:
- Queue callback requests
- Configurable delay simulation
- Retry logic with exponential backoff
- Support for different callback types (delivery status, call events)
- Log callback attempts and responses

### 3.5 Storage Layer
**Responsibility**: Persist messages, calls, and delivery events

**SQLite Schema**:

```sql
-- Messages table
CREATE TABLE messages (
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
);

-- Calls table
CREATE TABLE calls (
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
);

-- Delivery events table
CREATE TABLE delivery_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    message_sid TEXT,
    call_sid TEXT,
    event_type TEXT NOT NULL,
    status TEXT NOT NULL,
    callback_sent BOOLEAN DEFAULT FALSE,
    callback_response TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Callback logs table
CREATE TABLE callback_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    target_url TEXT NOT NULL,
    payload TEXT NOT NULL,
    status_code INTEGER,
    response_body TEXT,
    attempt_number INTEGER DEFAULT 1,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

### 3.6 Config Loader
**Responsibility**: Load and validate server configuration

**YAML Structure**:
```yaml
server:
  host: 0.0.0.0
  port: 8080

provider: twilio  # Current active provider

twilio:
  account_sid: ACXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
  auth_token: your_auth_token_here

  # Validation settings
  validation:
    require_auth: true              # Validate auth token
    validate_phone_format: true     # Check E.164 phone number format
    check_from_numbers: true        # Require From number in allowed list
    require_parameters: true        # Validate required parameters

  # Behavior for numbers not in registered_numbers or failure_numbers
  # Options: "success" or "failure"
  default_behavior: success

  registered_numbers:
    # Numbers that will successfully deliver (queued -> sent -> delivered)
    - "+15551234567"
    - "+15559876543"

  allowed_from_numbers:
    - "+15550000001"
    - "+15550000002"

  # Numbers that simulate failures
  failure_numbers:
    - "+15559999999"  # Always fails

  callbacks:
    enabled: true
    delay_seconds: 2  # Simulate delivery delay
    retry_attempts: 3
    retry_delay_seconds: 5

database:
  path: "./data/mock_server.db"

templates:
  path: "./templates/responses"
```

#### Number Validation Behavior

The server determines message/call success based on the destination number (`To` parameter) according to the following logic:

| Number Location | Behavior | Status Flow | Use Case |
|----------------|----------|-------------|----------|
| In `failure_numbers` | Always fails | queued → failed | Test error handling |
| In `registered_numbers` | Always succeeds | queued → sent → delivered | Test success path |
| Not in either list | Uses `default_behavior` config | Depends on config | Flexible testing |

**When `default_behavior: success`** (recommended for development):
- Any number not explicitly in `failure_numbers` will succeed
- Allows testing with arbitrary phone numbers
- Only need to list numbers that should fail

**When `default_behavior: failure`**:
- Only numbers in `registered_numbers` will succeed
- Stricter validation, mimics production constraints
- Useful for testing registration flows

**Examples**:

```yaml
# Permissive mode (default_behavior: success)
# - "+15551111111" → Success (not in either list, defaults to success)
# - "+15551234567" → Success (in registered_numbers)
# - "+15559999999" → Failure (in failure_numbers)

# Strict mode (default_behavior: failure)
# - "+15551111111" → Failure (not in either list, defaults to failure)
# - "+15551234567" → Success (in registered_numbers)
# - "+15559999999" → Failure (in failure_numbers)
```

#### Error Handling & Validation

The server emulates Twilio's error responses to help developers test error handling in their applications. Error validation is configurable via the `validation` settings.

**Supported Error Scenarios:**

| Error Type | HTTP Status | Twilio Error Code | Trigger | Configurable |
|-----------|-------------|-------------------|---------|--------------|
| Authentication Failed | 401 | 20003 | Invalid/missing auth token | `require_auth` |
| Invalid Account SID | 401 | 20003 | Wrong account SID in URL | `require_auth` |
| Missing Required Parameter | 400 | 21604 | Missing `From`, `To`, or `Body` | `require_parameters` |
| Invalid Phone Number | 400 | 21211 | Invalid E.164 format | `validate_phone_format` |
| Invalid From Number | 400 | 21606 | `From` not in allowed list | `check_from_numbers` |

**Error Response Format:**

Error responses match Twilio's standard error format:

```json
{
  "code": 21211,
  "message": "The 'To' number +1234 is not a valid phone number.",
  "more_info": "https://www.twilio.com/docs/errors/21211",
  "status": 400
}
```

**Validation Order:**

1. Authentication (if `require_auth: true`)
2. Required parameters (if `require_parameters: true`)
3. Phone number format (if `validate_phone_format: true`)
4. From number allowed list (if `check_from_numbers: true`)
5. Determine success/failure based on To number

**Flexible Validation:**

Each validation can be toggled on/off in config for different testing scenarios:

```yaml
# Strict validation (production-like)
validation:
  require_auth: true
  validate_phone_format: true
  check_from_numbers: true
  require_parameters: true

# Permissive (quick testing)
validation:
  require_auth: false
  validate_phone_format: false
  check_from_numbers: false
  require_parameters: true  # Keep minimal validation
```

### 3.7 Web UI
**Responsibility**: Provide simple interface for browsing activity

**Pages**:
1. **Dashboard** (`/`)
   - Recent activity summary (last 10 messages/calls)
   - Statistics (total messages, calls, callbacks)
   - Auto-refresh every 3 seconds via HTMX
   - Clickable rows to view message/call details in modal

2. **Messages** (`/ui/messages`)
   - Paginated table of all SMS messages (50 per page)
   - Auto-refresh every 3 seconds via HTMX
   - Click-to-view modal with full message details
   - Previous/Next pagination controls

3. **Calls** (`/ui/calls`)
   - Paginated table of all calls (50 per page)
   - Auto-refresh every 3 seconds via HTMX
   - Click-to-view modal with full call details
   - Previous/Next pagination controls

4. **Callbacks** (`/ui/callbacks`)
   - Paginated log of all callback attempts (50 per page)
   - Auto-refresh every 3 seconds via HTMX
   - Shows status code, attempt number, response body
   - Previous/Next pagination controls

**Technology**: Server-side rendered HTML with Jinja2 + HTMX for auto-refresh and dynamic updates

**HTMX Features**:
- Automatic polling (`hx-trigger="every 3s"`) for real-time updates
- Fragment updates without full page reload
- Modal loading for detail views
- Clear data actions with confirmation dialogs

## 4. Data Flow

### 4.1 SMS Sending Flow
```
1. Client → POST /2010-04-01/Accounts/{sid}/Messages.json

2. API Route → Validation (configurable):
   a. Check authentication (if require_auth: true)
      → Return 401 if invalid
   b. Validate required parameters (if require_parameters: true)
      → Return 400 if From/To/Body missing
   c. Validate phone number format (if validate_phone_format: true)
      → Return 400 if invalid E.164 format
   d. Check From number (if check_from_numbers: true)
      → Return 400 if not in allowed_from_numbers

3. API Route → Determine to_number behavior:
   - If in failure_numbers → Mark for failure
   - If in registered_numbers → Mark for success
   - Otherwise → Use default_behavior setting

4. Provider Adapter → Generate message_sid

5. Template Engine → Render response JSON (initial status: "queued")

6. Storage → Save message record

7. Response → Return to client

8. Callback Handler → Queue delivery status updates (if callback URL provided):
   - Success flow: queued → sent → delivered
   - Failure flow: queued → failed

9. Background Task → Send status callbacks after configured delay

10. Storage → Update message status and log callback attempts
```

### 4.2 Call Making Flow
```
1. Client → POST /2010-04-01/Accounts/{sid}/Calls.json

2. API Route → Validation (same as SMS):
   a. Check authentication
   b. Validate required parameters (From/To/Url)
   c. Validate phone number format
   d. Check From number

3. API Route → Determine to_number behavior (same as SMS)

4. Provider Adapter → Generate call_sid

5. Template Engine → Render response JSON (initial status: "queued")

6. Storage → Save call record

7. Response → Return to client

8. Callback Handler → Fetch TwiML from URL (if provided)

9. Callback Handler → Queue call status callbacks:
   - Success flow: queued → ringing → in-progress → completed
   - Failure flow: queued → failed

10. Background Task → Send callbacks for call events
```

## 5. Configuration & Templates

### 5.1 Directory Structure
```
/
├── config.yaml
├── templates/
│   ├── responses/
│   │   └── twilio/
│   │       ├── send_sms_success.json
│   │       ├── send_sms_failure.json
│   │       ├── make_call_success.json
│   │       ├── make_call_failure.json
│   │       ├── delivery_status.json
│   │       └── call_status.json
│   ├── errors/
│   │   └── twilio/
│   │       ├── auth_failed.json           # 401 - Invalid credentials
│   │       ├── missing_parameter.json     # 400 - Missing required param
│   │       ├── invalid_phone_number.json  # 400 - Invalid format
│   │       └── invalid_from_number.json   # 400 - From not allowed
│   └── ui/
│       ├── base.html           # Base template with navbar, modal, styles
│       ├── dashboard.html      # Dashboard page
│       ├── messages.html       # Messages list page
│       ├── calls.html          # Calls list page
│       ├── callbacks.html      # Callbacks list page
│       └── fragments/          # HTMX fragments for auto-refresh
│           ├── stats.html
│           ├── recent_messages.html
│           ├── recent_calls.html
│           ├── messages_table.html
│           ├── calls_table.html
│           ├── callbacks_table.html
│           ├── message_detail.html    # Modal content
│           ├── call_detail.html       # Modal content
│           └── pagination.html        # Reusable pagination controls
└── data/
    └── mock_server.db
```

### 5.2 Response Template Example
**File**: `templates/responses/twilio/send_sms_success.json`
```json
{
  "sid": "{{ message_sid }}",
  "date_created": "{{ date_created }}",
  "date_updated": "{{ date_updated }}",
  "date_sent": null,
  "account_sid": "{{ account_sid }}",
  "to": "{{ request.To }}",
  "from": "{{ request.From }}",
  "messaging_service_sid": null,
  "body": "{{ request.Body }}",
  "status": "queued",
  "num_segments": "1",
  "num_media": "0",
  "direction": "outbound-api",
  "api_version": "2010-04-01",
  "price": null,
  "price_unit": "USD",
  "error_code": null,
  "error_message": null,
  "uri": "/2010-04-01/Accounts/{{ account_sid }}/Messages/{{ message_sid }}.json",
  "subresource_uris": {
    "media": "/2010-04-01/Accounts/{{ account_sid }}/Messages/{{ message_sid }}/Media.json"
  }
}
```

### 5.3 Error Template Examples

**File**: `templates/errors/twilio/auth_failed.json`
```json
{
  "code": 20003,
  "message": "Authenticate",
  "more_info": "https://www.twilio.com/docs/errors/20003",
  "status": 401
}
```

**File**: `templates/errors/twilio/missing_parameter.json`
```json
{
  "code": 21604,
  "message": "The required parameter '{{ parameter }}' is missing.",
  "more_info": "https://www.twilio.com/docs/errors/21604",
  "status": 400
}
```

**File**: `templates/errors/twilio/invalid_phone_number.json`
```json
{
  "code": 21211,
  "message": "The '{{ field }}' number {{ number }} is not a valid phone number.",
  "more_info": "https://www.twilio.com/docs/errors/21211",
  "status": 400
}
```

**File**: `templates/errors/twilio/invalid_from_number.json`
```json
{
  "code": 21606,
  "message": "The 'From' phone number {{ from_number }} is not a valid, message-capable Twilio phone number.",
  "more_info": "https://www.twilio.com/docs/errors/21606",
  "status": 400
}
```

## 6. API Design

### 6.1 Twilio SMS API
**Endpoint**: `POST /2010-04-01/Accounts/{AccountSid}/Messages.json`

**Request Parameters**:
- `From` (required): Sending phone number
- `To` (required): Destination phone number
- `Body` (required): Message text
- `StatusCallback` (optional): URL for delivery status callbacks

**Response**: JSON matching Twilio's message resource

### 6.2 Twilio Call API
**Endpoint**: `POST /2010-04-01/Accounts/{AccountSid}/Calls.json`

**Request Parameters**:
- `From` (required): Calling phone number
- `To` (required): Destination phone number
- `Url` (required): TwiML URL
- `StatusCallback` (optional): URL for call status callbacks

**Response**: JSON matching Twilio's call resource

### 6.3 Health Check Endpoint

**Endpoint**: `GET /health`

**Purpose**: Docker health checks and monitoring

**Response**:
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

**HTTP Status**: 200 OK

### 6.4 Callback Test Endpoint

**Endpoint**: `POST /callback-test`

**Purpose**: Local endpoint for testing callbacks without external URLs

**Request**: Accepts any form data (simulates receiving callback POST)

**Response**:
```json
{
  "status": "received",
  "data": {
    "MessageSid": "SM...",
    "MessageStatus": "delivered",
    ...
  }
}
```

**HTTP Status**: 200 OK

**Use Case**: When running in Docker, external callback URLs may not be accessible. This endpoint provides a working target for testing callback functionality:
```
StatusCallback=http://localhost:8080/callback-test
```

### 6.5 Clear Data Endpoints

**Endpoints**:
- `POST /clear/messages` - Clear all messages
- `POST /clear/calls` - Clear all calls
- `POST /clear/callbacks` - Clear all callback logs
- `POST /clear/all` - Clear all data (messages + calls + callbacks)

**Response**:
```json
{
  "deleted": 42,
  "type": "messages"
}
```

**Purpose**: Reset mock server data during testing without restarting container

### 6.6 Favicon Endpoint

**Endpoint**: `GET /favicon.ico`

**Purpose**: Serve favicon for web UI

**Response**: SVG image (SMS message bubble icon)

**HTTP Status**: 200 OK

### 6.7 SDK Compatibility

The mock server is designed to work seamlessly with official Twilio SDKs by using the same URL structure and authentication mechanism.

#### Authentication Method

All Twilio SDKs use HTTP Basic Authentication:
- **Username**: Account SID (configured in `config.yaml`)
- **Password**: Auth Token (configured in `config.yaml`)
- **Header**: `Authorization: Basic <base64(account_sid:auth_token)>`

The mock server validates this if `validation.require_auth: true`.

#### URL Structure Compatibility

The mock server uses Twilio's exact URL patterns:
```
POST http://localhost:8080/2010-04-01/Accounts/{AccountSid}/Messages.json
POST http://localhost:8080/2010-04-01/Accounts/{AccountSid}/Calls.json
```

This allows SDKs to work without modification, only requiring a base URL override.

#### SDK Configuration Examples

**PHP SDK:**
```php
<?php
require_once 'vendor/autoload.php';
use Twilio\Rest\Client;

// Point SDK to mock server
$accountSid = 'ACXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX';
$authToken = 'your_auth_token_here';
$mockServerUrl = 'http://localhost:8080';

$client = new Client($accountSid, $authToken, $accountSid, null, $mockServerUrl);

// Use SDK normally
$message = $client->messages->create(
    '+15551234567',  // To
    [
        'from' => '+15550000001',
        'body' => 'Hello from mock server!',
        'statusCallback' => 'http://your-app.com/status-callback'
    ]
);

echo "Message SID: " . $message->sid . "\n";
```

**Python SDK:**
```python
from twilio.rest import Client
from twilio.http.http_client import TwilioHttpClient

# Configure HTTP client for mock server
account_sid = 'ACXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX'
auth_token = 'your_auth_token_here'

http_client = TwilioHttpClient()
http_client.api_base_url = 'http://localhost:8080'

client = Client(account_sid, auth_token, http_client=http_client)

# Use SDK normally
message = client.messages.create(
    to='+15551234567',
    from_='+15550000001',
    body='Hello from mock server!',
    status_callback='http://your-app.com/status-callback'
)

print(f"Message SID: {message.sid}")
```

**Node.js SDK:**
```javascript
const twilio = require('twilio');

// Configure client for mock server
const accountSid = 'ACXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX';
const authToken = 'your_auth_token_here';

const client = twilio(accountSid, authToken, {
    lazyLoading: true,
    accountSid: accountSid,
    // Override base URL
    apiBaseUrl: 'http://localhost:8080'
});

// Use SDK normally
async function sendMessage() {
    const message = await client.messages.create({
        to: '+15551234567',
        from: '+15550000001',
        body: 'Hello from mock server!',
        statusCallback: 'http://your-app.com/status-callback'
    });

    console.log(`Message SID: ${message.sid}`);
}

sendMessage();
```

**Ruby SDK:**
```ruby
require 'twilio-ruby'

# Configure client for mock server
account_sid = 'ACXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX'
auth_token = 'your_auth_token_here'

@client = Twilio::REST::Client.new(account_sid, auth_token)
@client.http_client.base_url = 'http://localhost:8080'

# Use SDK normally
message = @client.messages.create(
  from: '+15550000001',
  to: '+15551234567',
  body: 'Hello from mock server!',
  status_callback: 'http://your-app.com/status-callback'
)

puts "Message SID: #{message.sid}"
```

**Java SDK:**
```java
import com.twilio.Twilio;
import com.twilio.rest.api.v2010.account.Message;
import com.twilio.type.PhoneNumber;

public class MockServerExample {
    public static final String ACCOUNT_SID = "ACXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX";
    public static final String AUTH_TOKEN = "your_auth_token_here";

    public static void main(String[] args) {
        // Initialize with mock server
        Twilio.init(ACCOUNT_SID, AUTH_TOKEN);
        Twilio.setRestClient(
            new com.twilio.http.TwilioRestClient.Builder(ACCOUNT_SID, AUTH_TOKEN)
                .baseUrl("http://localhost:8080")
                .build()
        );

        // Use SDK normally
        Message message = Message.creator(
            new PhoneNumber("+15551234567"),
            new PhoneNumber("+15550000001"),
            "Hello from mock server!"
        ).setStatusCallback("http://your-app.com/status-callback")
         .create();

        System.out.println("Message SID: " + message.getSid());
    }
}
```

**C# / .NET SDK:**
```csharp
using System;
using Twilio;
using Twilio.Rest.Api.V2010.Account;
using Twilio.Http;

class Program
{
    static void Main(string[] args)
    {
        const string accountSid = "ACXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX";
        const string authToken = "your_auth_token_here";

        // Configure with mock server
        TwilioClient.Init(accountSid, authToken,
            new SystemNetHttpClient(new HttpClient
            {
                BaseAddress = new Uri("http://localhost:8080")
            })
        );

        // Use SDK normally
        var message = MessageResource.Create(
            to: new Twilio.Types.PhoneNumber("+15551234567"),
            from: new Twilio.Types.PhoneNumber("+15550000001"),
            body: "Hello from mock server!",
            statusCallback: new Uri("http://your-app.com/status-callback")
        );

        Console.WriteLine($"Message SID: {message.Sid}");
    }
}
```

#### Docker Networking

When running the mock server in Docker and your application in another container:

**Docker Compose Example:**
```yaml
version: '3.8'

services:
  sms-mock-server:
    build: .
    container_name: sms-mock-server
    ports:
      - "8080:8080"
    volumes:
      - ./config.yaml:/app/config.yaml
      - ./templates:/app/templates
      - ./data:/app/data
    networks:
      - app-network

  your-application:
    image: your-app:latest
    depends_on:
      - sms-mock-server
    environment:
      # Point to mock server by container name
      - TWILIO_API_BASE_URL=http://sms-mock-server:8080
      - TWILIO_ACCOUNT_SID=ACXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
      - TWILIO_AUTH_TOKEN=your_auth_token_here
    networks:
      - app-network

networks:
  app-network:
    driver: bridge
```

**SDK Configuration in Docker:**
```python
# In your application container
import os
from twilio.rest import Client
from twilio.http.http_client import TwilioHttpClient

account_sid = os.getenv('TWILIO_ACCOUNT_SID')
auth_token = os.getenv('TWILIO_AUTH_TOKEN')
mock_url = os.getenv('TWILIO_API_BASE_URL', 'http://sms-mock-server:8080')

http_client = TwilioHttpClient()
http_client.api_base_url = mock_url

client = Client(account_sid, auth_token, http_client=http_client)
```

#### Environment Variables Support

For easier configuration across environments:

```bash
# .env file
TWILIO_ACCOUNT_SID=ACXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
TWILIO_AUTH_TOKEN=your_auth_token_here
TWILIO_API_BASE_URL=http://localhost:8080

# Production
# TWILIO_API_BASE_URL=https://api.twilio.com

# Staging with mock
# TWILIO_API_BASE_URL=http://sms-mock-server:8080
```

## 7. Deployment

### 7.1 Docker Container
**Dockerfile Strategy**: Multi-stage build
- Stage 1: Install dependencies
- Stage 2: Copy application code
- Stage 3: Slim runtime image

**Exposed Ports**:
- `8080` - HTTP

**Volumes**:
- `/app/config.yaml` - Configuration file
- `/app/templates` - Response templates (JSON templates for responses/errors)
- `/app/data` - SQLite database

**Environment Variables**:
- `CONFIG_PATH` - Override config file location
- `LOG_LEVEL` - Logging verbosity (DEBUG, INFO, WARNING, ERROR)

### 7.2 Docker Compose Example

**Basic Setup:**
```yaml
version: '3.8'
services:
  sms-mock-server:
    build: .
    container_name: sms-mock-server
    ports:
      - "8080:8080"
    volumes:
      - ./config.yaml:/app/config.yaml
      - ./templates:/app/templates
      - ./data:/app/data
    environment:
      - LOG_LEVEL=INFO
```

**With Application Stack:**
```yaml
version: '3.8'

services:
  sms-mock-server:
    build: ./sms-mock-server
    container_name: sms-mock-server
    ports:
      - "8080:8080"
    volumes:
      - ./sms-mock-server/config.yaml:/app/config.yaml
      - ./sms-mock-server/templates:/app/templates
      - ./sms-mock-server/data:/app/data
    environment:
      - LOG_LEVEL=INFO
    networks:
      - app-network
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
      interval: 10s
      timeout: 5s
      retries: 3

  your-application:
    build: ./your-app
    depends_on:
      sms-mock-server:
        condition: service_healthy
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

## 8. Extensibility

### 8.1 Adding New Providers
To add a new provider (e.g., MessageBird, Vonage):

1. Create new adapter in `app/providers/{provider_name}.py`
2. Implement `BaseProvider` interface
3. Add provider-specific templates in `templates/responses/{provider_name}/`
4. Update `config.yaml` with provider configuration
5. Register provider in provider factory

### 8.2 Custom Response Behavior
- Edit JSON templates to modify response structure
- Add conditional logic in templates based on request parameters
- Configure number-based routing (success/failure) in `config.yaml`

### 8.3 Future Enhancements
- **HTTPS support** with self-signed certificates (optional for production-like testing)
- API authentication/authorization (beyond basic auth)
- Multiple provider instances simultaneously
- Advanced callback scheduling (custom delays per number)
- Webhook verification (signature validation like Twilio's X-Twilio-Signature)
- MMS support with file handling
- Call recording simulation
- Conference call simulation
- REST API for managing configuration dynamically
- Web UI for modifying templates without file edits
- Metrics and analytics dashboard

## 9. Technology Stack Summary

| Component | Technology | Rationale |
|-----------|-----------|-----------|
| Language | Python 3.11+ | Rapid development, excellent libraries |
| Web Framework | FastAPI | Modern, async, auto-documentation |
| Template Engine | Jinja2 | Variable substitution, server-side rendering |
| Database | SQLite | Lightweight, file-based, no external dependencies |
| UI | HTML + HTMX | Simple, no heavy frontend framework |
| Containerization | Docker | Portable, isolated environment |
| Config Format | YAML | Human-readable, easy to edit |
| Response Format | JSON | Standard API format |

## 10. Development Approach

### 10.1 Phase 1: Core Functionality
- Basic FastAPI application setup (HTTP only)
- Config loader (including validation settings)
- Health check endpoint (`/health`)
- Twilio SMS endpoint
- Template engine (responses + errors)
- Request validation and error handling
- SQLite storage
- Simple response handling

### 10.2 Phase 2: Callbacks
- Async callback handler
- Background task queue
- Delivery status simulation
- Callback logging

### 10.3 Phase 3: Calls Support
- Twilio call endpoint
- TwiML fetching
- Call status simulation

### 10.4 Phase 4: UI
- Dashboard
- Messages list
- Calls list
- Callback logs

### 10.5 Phase 5: Dockerization
- Dockerfile
- Docker Compose
- Documentation

## 11. Non-Goals (Keeping it Simple)

- No user authentication (single-tenant mock server)
- No complex queue systems (simple async tasks)
- No microservices architecture
- No external cache (Redis, etc.)
- No ORM (use simple SQL)
- No GraphQL or gRPC
- No complex frontend framework (React, Vue)
- No real message delivery
- No production-grade monitoring/observability
