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
│  │              Go HTTP Server (net/http)                │   │
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

**Provider Interface** (Go):
```go
type Provider interface {
    Name() string

    // Validation: auth → SMS/Call params → phone format → From-allowlist.
    // Returns *ValidationError (with Twilio code, HTTPStatus, template name + vars) on failure.
    ValidateAuth(authHeader, accountSidFromURL string) error
    ValidateSMS(req SMSRequest) error
    ValidateCall(req CallRequest) error

    // Behavior determination — used by the dispatcher to pick a status flow.
    IsKnownNumber(toNumber string) bool   // in registered/failure list?
    ShouldSucceed(toNumber string) bool   // failure → false; success → true (only called when IsKnownNumber)
}
```

`ValidationError` carries the Twilio error code, HTTP status, and the name of the
JSON template to render. Handlers convert it to a response in one line via `writeError`.

**Twilio Adapter**: implements `Provider` with auth via HTTP Basic, libphonenumber-based
phone validation (`nyaruka/phonenumbers`), and registered/failure-number lookup.

### 3.3 Template Engine
**Responsibility**: Render JSON response templates and HTML UI templates with variable substitution

**Features**:
- Loads templates from an `embed.FS` (baked into the binary at build time, no runtime filesystem dependency).
- Uses Go's stdlib `text/template` for JSON output (no auto-escaping; `json` funcmap handles safe interpolation of arbitrary strings) and `html/template` for UI pages (auto-escaping).
- Access to request data, config, and generated values (IDs, timestamps) via typed structs.

**Example Response Template**:
```json
{
  "sid": "{{ .MessageSID }}",
  "from": {{ .Request.From | json }},
  "to": {{ .Request.To | json }},
  "body": {{ .Request.Body | json }},
  "status": "{{ .Status }}",
  "date_created": "{{ .DateCreated }}"
}
```
The `| json` pipe escapes user-controlled fields safely (matters for `Body` containing quotes/backslashes/newlines).

### 3.4 Callback Handler
**Responsibility**: Asynchronous callback delivery to client URLs

**Features**:
- Worker pool draining a buffered job channel
- Status flow scheduled via a `Clock` interface (`time.AfterFunc` in production, fake clock in tests)
- Fixed retry delay between attempts (`callbacks.retry_delay_seconds`), max `callbacks.retry_attempts` per status; 2xx response counts as success
- Status update in DB happens regardless of HTTP delivery result; HTTP failure only affects callback delivery, not the persisted status
- Log every attempt to `callback_logs`, including transport errors (status_code = 0)
- Graceful shutdown: `Close(ctx)` drains in-flight jobs; late-firing timers detect the shutdown flag and skip all writes

### 3.5 Storage Layer
**Responsibility**: Persist messages, calls, delivery events, and callback logs

**Implementation**: Pure-Go SQLite via `modernc.org/sqlite` (no CGO required), wrapped in a `Store` interface so handlers and the dispatcher can be unit-tested against the in-memory `testutil.FakeStore`. WAL mode + busy timeout. Migrations live under `app/storage/migrations/` and are embedded into the binary; they're applied at startup, tracked in `schema_migrations`.

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
    is_read BOOLEAN NOT NULL DEFAULT 0,  -- set when the UI detail page is opened
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
    is_read BOOLEAN NOT NULL DEFAULT 0,  -- set when the UI detail page is opened
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

-- Tag tables. tags(name) is a global registry; message_tags / call_tags are
-- the per-record link tables. ON DELETE CASCADE on the link tables removes
-- orphan links automatically when a message or call is deleted (requires
-- foreign_keys=1, set on the connection DSN).
CREATE TABLE tags (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE
);
CREATE TABLE message_tags (
    message_id INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    tag_id     INTEGER NOT NULL REFERENCES tags(id),
    PRIMARY KEY (message_id, tag_id)
);
CREATE TABLE call_tags (
    call_id INTEGER NOT NULL REFERENCES calls(id) ON DELETE CASCADE,
    tag_id  INTEGER NOT NULL REFERENCES tags(id),
    PRIMARY KEY (call_id, tag_id)
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

-- Indexes added in migration 004 to keep the retention pruner off full
-- table scans (see §3.7). created_at is the ordering key for both the
-- count-based and age-based pruning queries.
CREATE INDEX idx_messages_created_at ON messages(created_at);
CREATE INDEX idx_calls_created_at    ON calls(created_at);
```

### 3.6 Config Loader
**Responsibility**: Load and validate server configuration from environment variables

**Environment variables**:

Common (provider-agnostic) settings use the `SMS_MOCK_` prefix; Twilio-specific settings use `SMS_MOCK_TWILIO_`.

| Variable | Default | Notes |
| --- | --- | --- |
| `SMS_MOCK_PORT` | `8080` | server always binds to `0.0.0.0`; restrict externally via Docker port-forwarding |
| `SMS_MOCK_TIMEZONE` | `UTC` | UI date display |
| `SMS_MOCK_DB_PATH` | `/tmp/mock_server.db` | SQLite path |
| `SMS_MOCK_PROVIDER` | `twilio` | only `twilio` is supported today |
| `SMS_MOCK_TWILIO_ACCOUNT_SID` | (empty) | required when REQUIRE_AUTH=true |
| `SMS_MOCK_TWILIO_AUTH_TOKEN` | (empty) | required when REQUIRE_AUTH=true |
| `SMS_MOCK_TWILIO_SUCCESS_NUMBERS` | (empty) | comma-separated; queued → sent → delivered |
| `SMS_MOCK_TWILIO_FAILURE_NUMBERS` | (empty) | comma-separated; queued → failed |
| `SMS_MOCK_TWILIO_ALLOWED_FROM_NUMBERS` | (empty) | comma-separated; From-allowlist |
| `SMS_MOCK_TWILIO_REQUIRE_AUTH` | `true` | |
| `SMS_MOCK_TWILIO_VALIDATE_PHONE_FORMAT` | `true` | E.164 check via libphonenumber |
| `SMS_MOCK_TWILIO_CHECK_FROM_NUMBERS` | `true` | |
| `SMS_MOCK_TWILIO_CALLBACK_DELAY_SECONDS` | `2` | between status transitions |
| `SMS_MOCK_TWILIO_CALLBACK_RETRY_ATTEMPTS` | `3` | total attempts |
| `SMS_MOCK_TWILIO_CALLBACK_RETRY_DELAY_SECONDS` | `5` | between retries |
| `SMS_MOCK_MAX_MESSAGES` | `500` | cap on messages table; `0` disables |
| `SMS_MOCK_MAX_CALLS` | `500` | cap on calls table; `0` disables |
| `SMS_MOCK_MAX_AGE` | (empty) | TTL for both tables; `<int>h` or `<int>d` (e.g. `72h`, `3d`); empty disables |
| `SMS_MOCK_HIDE_DELETE_ALL_BUTTON` | `false` | UI-only; hides the bulk-delete button. Backend endpoints remain functional. |

The loader applies defaults, overlays env vars, then validates: provider must be `twilio`; timezone must parse; if `REQUIRE_AUTH=true`, both `ACCOUNT_SID` and `AUTH_TOKEN` must be set to non-placeholder values. List values are comma-separated; whitespace is trimmed and empty entries are dropped.

> Note: in the Go implementation, templates are embedded into the binary via
> `//go:embed`, so there is no template-path config field. To customize
> templates, edit the files under `templates/` and rebuild the binary.

#### Number Validation Behavior

The server determines message/call success based on the destination number (`To` parameter) according to the following logic:

| Number Location | Behavior | Status Flow | Use Case |
|----------------|----------|-------------|----------|
| In `SMS_MOCK_TWILIO_FAILURE_NUMBERS` | Always fails | queued → failed | Test error handling |
| In `SMS_MOCK_TWILIO_SUCCESS_NUMBERS` | Always succeeds | queued → sent → delivered | Test success path |
| Not in either list | Stays queued forever | queued (no progression) | "Callbacks off" mode |

The failure list takes precedence if a number appears in both. Numbers not in either list are deliberately stuck at `queued` — the dispatcher short-circuits on `!IsKnownNumber` so no status transitions are scheduled and no callbacks fire. This is also how the server runs in "callbacks-effectively-off" mode: leave both lists empty.

> **libphonenumber note:** `+1555...` numbers (NANP fictional-use) are flagged invalid by libphonenumber when `SMS_MOCK_TWILIO_VALIDATE_PHONE_FORMAT=true`. For strict testing, use real-looking numbers (e.g. `+12025550100` — Washington DC area code).

**Examples**:

```sh
SMS_MOCK_TWILIO_SUCCESS_NUMBERS="+15551234567,+15559876543"
SMS_MOCK_TWILIO_FAILURE_NUMBERS="+15559999999"

# - "+15551111111" → Stays queued forever (in neither list)
# - "+15551234567" → Success (in success list)
# - "+15559999999" → Failure (in failure list)
```

#### Error Handling & Validation

The server emulates Twilio's error responses to help developers test error handling in their applications. Each validation step is gated on an env var (see §3.6).

**Supported Error Scenarios:**

| Error Type | HTTP Status | Twilio Error Code | Trigger | Toggle |
|-----------|-------------|-------------------|---------|--------|
| Authentication Failed | 401 | 20003 | Invalid/missing auth token | `SMS_MOCK_TWILIO_REQUIRE_AUTH` |
| Invalid Account SID | 401 | 20003 | Wrong account SID in URL | `SMS_MOCK_TWILIO_REQUIRE_AUTH` |
| Missing Required Parameter | 400 | 21604 | Missing `From`, `To`, or `Body` | always enforced |
| Invalid Phone Number | 400 | 21211 | Invalid E.164 format | `SMS_MOCK_TWILIO_VALIDATE_PHONE_FORMAT` |
| Invalid From Number | 400 | 21606 | `From` not in allowed list | `SMS_MOCK_TWILIO_CHECK_FROM_NUMBERS` |

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

1. Authentication (if `SMS_MOCK_TWILIO_REQUIRE_AUTH=true`)
2. Required parameters (always enforced — `From`/`To`/`Body` for SMS, `From`/`To`/`Url` for calls)
3. Phone number format (if `SMS_MOCK_TWILIO_VALIDATE_PHONE_FORMAT=true`)
4. From number allowed list (if `SMS_MOCK_TWILIO_CHECK_FROM_NUMBERS=true`)
5. Determine success/failure based on To number

**Flexible Validation:**

The three toggle-able checks default to `true`. Setting any to `false` skips that step — useful for quick testing without setting up real credentials or full E.164 numbers:

```sh
# Permissive (quick testing): turn off auth, phone format, From-allowlist
SMS_MOCK_TWILIO_REQUIRE_AUTH=false
SMS_MOCK_TWILIO_VALIDATE_PHONE_FORMAT=false
SMS_MOCK_TWILIO_CHECK_FROM_NUMBERS=false
```

### 3.7 Retention Pruner

**Responsibility**: Bound the SQLite DB by enforcing per-table row caps and an optional age-based TTL on messages and calls.

**Settings** live under `config.Limits` (env vars in §3.6):
- `MaxMessages`, `MaxCalls` — independent caps; oldest rows pruned first when exceeded; `0` disables.
- `MaxAge` — single TTL applied to both tables; rows with `created_at < now - MaxAge` are deleted. Zero disables.
- `HideDeleteAllButton` — UI-only; suppresses the sidebar "Delete all" button (backend endpoints unchanged).

**Mechanism**: A single goroutine in `app/prune` runs every 60 seconds. One immediate pass on startup so an over-cap DB gets trimmed without waiting a minute. The pruner consults `config.Limits` and calls `storage.Store`'s four `PruneXByCount` / `PruneXByAge` methods. Errors are logged at WARN; transient SQLite contention doesn't abort the loop.

**Constructor short-circuit**: `prune.New` returns `nil` when every limit is disabled — `main.go` skips wiring the goroutine entirely, so projects with retention disabled don't pay any cost.

**SQL**: count-based prune uses `SELECT sid FROM <table> ORDER BY created_at DESC, id DESC LIMIT -1 OFFSET <cap>` to find rows beyond the cap, then deletes them along with their `delivery_events` in a single transaction. Age-based prune uses `WHERE created_at < <cutoff>`. Both rely on the `idx_{messages,calls}_created_at` indexes added in migration 004 to avoid full scans. `callback_logs` rows (audit trail) and tag links (FK cascade) are not touched.

### 3.8 Web UI

**Responsibility**: Provide a mailbox-style interface for inspecting received messages and calls.

**Layout**: Three-zone CSS grid — top bar (brand, search, environment meta),
left sidebar (type nav, optional Tags section, delete-all), right main pane
(list, then full-page detail). Sidebar and chrome stay visible on detail
pages so the user can navigate without losing context.

**Pages**:
1. **Messages** (`/`) — default landing. Newest-first list with columns
   `dot · From · To · Body · Status · Created · delete`. Unread rows render
   bold with a leading `●`. Plain `<a href>` row links so middle/cmd-click
   work and don't race with the polling swap. The link inside each cell
   covers the full row height (vertical padding lives on the `<a>`, not the
   `<td>`), so a click anywhere in the row targets the detail page.

2. **Calls** (`/calls`) — same shape as Messages, no Body column.

3. **Detail** (`/view/messages/{sid}`, `/view/calls/{sid}`) — full-page
   replacement (sidebar + topbar still visible). Marks the record read on
   open. Renders From/To/SID/Status/Created card, Body block (messages only),
   the record's tags as pill chips when present, collapsible "Raw record"
   JSON, and a one-line "Callback delivery" summary when any `callback_logs`
   row references the SID. Filter state (`?q=`, `?status=`) is threaded
   through the row link and rebuilt by the Back button.

**Search & filters** — single source of truth, the `?q=` URL param:
- **Free text**: applies LIKE `%q%` to `from_number`/`to_number` plus `body`
  (messages only).
- **Tag operators**: `tag:foo` and `tag:"two words"` tokens inside `q` filter
  by tag (AND-semantics across multiple tag tokens). Mixed with free text:
  `verify tag:auth` finds records with body matching "verify" AND tagged
  `auth`.
- The top-bar input is debounced 300ms; live keystrokes update the list via
  HTMX without polluting the URL bar. Pressing Enter submits a real form GET
  so the URL becomes shareable.
- **`?status=`** still works as a URL parameter for power users / scripts but
  has no UI surface today.

**Tags** — user-defined metadata supplied by the client via the `X-Tags`
HTTP header on the Twilio-shaped POST. The header value is comma-separated
(e.g. `X-Tags: verification, auth`); whitespace is trimmed, names are
lowercased, duplicates are folded. The Twilio payload itself is unchanged —
tags are out-of-band signaling for the mock UI only.

- Stored in `tags` (global registry, unique by name) joined to records via
  `message_tags` / `call_tags` link tables.
- The sidebar Tags section appears only when at least one record of the
  active type has a tag attached. Per-type scope: a tag attached only to a
  call doesn't appear in the Messages sidebar.
- Single-select on click (replaces any prior tag); clicking the active tag
  clears the filter. Free text in `q` is preserved across clicks.
- Detail view shows a record's tags as pill chips next to its other metadata.
- View-only: there's no UI to add or remove tags after the record was
  recorded. To change tags, the client re-sends with a different `X-Tags`
  header (which creates a new record).

**Mutation**:
- Per-row `DELETE /ui/messages/{sid}` / `DELETE /ui/calls/{sid}`, hover-only
  delete button on each row, confirm via `hx-confirm`. Deleting a record
  cascades its tag links via `ON DELETE CASCADE`.
- Bulk "Delete all" button in the sidebar reuses the existing
  `POST /clear/messages` / `POST /clear/calls` API routes, then redirects
  back to the list.

**Real-time**: HTMX polling. The list fragment self-replaces every 3 seconds
via `hx-get="/ui/fragments/list?type=…"` + `hx-trigger="every 3s"` +
`hx-swap="outerHTML"`. The sidebar fragment polls the same way to keep tag
membership current.

**Routes** (all method-prefixed via Go 1.22 `net/http` mux):

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/{$}` | Messages mailbox |
| `GET` | `/calls` | Calls mailbox |
| `GET` | `/view/messages/{sid}` | Message detail (marks read) |
| `GET` | `/view/calls/{sid}` | Call detail (marks read) |
| `GET` | `/ui/fragments/list` | List body (polled, also receives search/filter requests) |
| `GET` | `/ui/fragments/sidebar` | Sidebar body (polled) |
| `DELETE` | `/ui/messages/{sid}` | Delete one message |
| `DELETE` | `/ui/calls/{sid}` | Delete one call |

**Templates** (`app/templates/ui/`): `base.html` (3-zone shell), `messages.html`
and `calls.html` (page shells), `view/message.html` and `view/call.html`
(detail full-pages), `fragments/list.html` and `fragments/sidebar.html`
(polled fragments).

**Search parser**: `app/ui/searchparse.go` splits the raw `q` into free-text
terms and `tag:` tokens. Used by every page and fragment handler that needs
to filter — keeps the SQL layer ignorant of the operator syntax.

**Technology**: stdlib `html/template` server-side rendering + HTMX 1.x for
swaps and confirmations. No client JS framework.

## 4. Data Flow

### 4.1 SMS Sending Flow
```
1. Client → POST /2010-04-01/Accounts/{sid}/Messages.json

2. API Route → Validation (configurable):
   a. Check authentication (if SMS_MOCK_TWILIO_REQUIRE_AUTH=true)
      → Return 401 if invalid
   b. Validate required parameters (always enforced)
      → Return 400 if From/To/Body missing
   c. Validate phone number format (if SMS_MOCK_TWILIO_VALIDATE_PHONE_FORMAT=true)
      → Return 400 if invalid E.164 format
   d. Check From number (if SMS_MOCK_TWILIO_CHECK_FROM_NUMBERS=true)
      → Return 400 if not in SMS_MOCK_TWILIO_ALLOWED_FROM_NUMBERS

3. API Route → Determine to_number behavior:
   - If in FAILURE_NUMBERS → Mark for failure
   - If in SUCCESS_NUMBERS → Mark for success
   - Otherwise → Mark unknown (no progression)

4. Provider Adapter → Generate message_sid

5. Template Engine → Render response JSON (initial status: "queued")

6. Storage → Save message record

7. Response → Return to client

8. Callback Handler → Schedule status flow based on `IsKnownNumber(To)`:
   - To in `SUCCESS_NUMBERS` → queued → sent → delivered (success flow)
   - To in `FAILURE_NUMBERS` → queued → failed
   - To in NEITHER list → no progression, stays queued forever (no callbacks fired)

9. For each scheduled status (after CALLBACK_DELAY_SECONDS increments):
   - Update the message status in DB
   - Persist a `delivery_events` row
   - If `StatusCallback` URL was provided: enqueue an HTTP POST to the URL via the worker pool

10. Worker pool → POST callback (with retries on non-2xx or transport error); each attempt logs a `callback_logs` row.
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

8. TwiML URL is stored on the call record but NOT fetched (mock-only behavior).

9. Callback Handler → Queue call status callbacks (if callback URL provided):
   - Success flow: queued → ringing → in-progress → completed
   - Failure flow: queued → failed

10. Background Task → Send callbacks for call events
```

## 5. Configuration & Templates

### 5.1 Directory Structure
```
/
├── app/                              # all Go source (flat layout)
│   ├── main.go                       # entrypoint
│   ├── embedded.go                   # //go:embed templates + static
│   ├── config/                       # env-var config loader + validation
│   ├── storage/                      # SQLite store (modernc.org/sqlite, no CGO)
│   ├── provider/                     # Provider interface + ValidationError
│   │   └── twilio/                   # Twilio adapter
│   ├── template/                     # text/template + html/template engine
│   ├── callback/                     # async dispatcher (worker pool, Clock interface)
│   ├── prune/                        # background retention sweeper (60s tick)
│   ├── httpapi/                      # Twilio routes, /health, /clear/*, middleware
│   ├── ui/                           # Mailbox pages, detail views, HTMX fragments
│   ├── clock/                        # Clock interface (real + fake for tests)
│   ├── testutil/                     # Shared fakes for unit tests
│   ├── templates/                    # JSON + HTML templates (embedded into binary)
│   │   ├── responses/twilio/         # send_sms / make_call success / failure
│   │   ├── errors/twilio/            # auth_failed, missing_parameter, invalid_*
│   │   └── ui/                       # base.html, messages.html, calls.html, view/, fragments/
│   └── static/                       # CSS, JS, favicon (embedded)
├── scripts/                          # seed_data.sh (curl-based sample data)
├── docs/                             # DESIGN.md + plans
├── .github/workflows/                # CI (test + lint) + release (goreleaser)
├── Makefile                          # Build / test / docker targets
├── Dockerfile                        # Multi-stage source build (used by docker compose)
├── Dockerfile.release                # Single-stage prebuilt-binary copy (used by goreleaser)
├── docker-compose.yml
├── .golangci.yml                     # Linter config (41 linters)
├── .goreleaser.yml                   # Release automation
└── go.mod / go.sum
```

Note: SQLite database is stored in a Docker volume (`sms-mock-data`) for persistence.
Templates and static assets are baked into the binary at build time via `//go:embed`,
so the runtime container needs only env-var configuration and a writable `data/` dir.

### 5.2 Response Template Example
**File**: `app/templates/responses/twilio/send_sms_success.json`
```json
{
  "sid": "{{ .MessageSID }}",
  "date_created": "{{ .DateCreated }}",
  "date_updated": "{{ .DateUpdated }}",
  "date_sent": null,
  "account_sid": "{{ .AccountSid }}",
  "to": {{ .Request.To | json }},
  "from": {{ .Request.From | json }},
  "messaging_service_sid": null,
  "body": {{ .Request.Body | json }},
  "status": "{{ .Status }}",
  "num_segments": "{{ .NumSegments }}",
  "num_media": "0",
  "direction": "outbound-api",
  "api_version": "2010-04-01",
  "price": null,
  "price_unit": "USD",
  "error_code": null,
  "error_message": null,
  "uri": "/2010-04-01/Accounts/{{ .AccountSid }}/Messages/{{ .MessageSID }}.json",
  "subresource_uris": {
    "media": "/2010-04-01/Accounts/{{ .AccountSid }}/Messages/{{ .MessageSID }}/Media.json"
  }
}
```

The `| json` pipe wraps user-controlled fields (To, From, Body) in JSON-safe quoted form, handling escapes for quotes, backslashes, and control characters.

### 5.3 Error Template Examples

**File**: `app/templates/errors/twilio/auth_failed.json`
```json
{
  "code": 20003,
  "message": "Authenticate",
  "more_info": "https://www.twilio.com/docs/errors/20003",
  "status": 401
}
```

**File**: `app/templates/errors/twilio/missing_parameter.json`
```json
{
  "code": 21604,
  "message": "The required parameter '{{ .parameter }}' is missing.",
  "more_info": "https://www.twilio.com/docs/errors/21604",
  "status": 400
}
```

**File**: `app/templates/errors/twilio/invalid_phone_number.json`
```json
{
  "code": 21211,
  "message": "The '{{ .field }}' number {{ .number }} is not a valid phone number.",
  "more_info": "https://www.twilio.com/docs/errors/21211",
  "status": 400
}
```

**File**: `app/templates/errors/twilio/invalid_from_number.json`
```json
{
  "code": 21606,
  "message": "The 'From' phone number {{ .from_number }} is not a valid, message-capable Twilio phone number.",
  "more_info": "https://www.twilio.com/docs/errors/21606",
  "status": 400
}
```

Error templates receive their interpolation vars as a `map[string]string`, accessed via `{{ .keyname }}` — Go template syntax for map field access.

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
  "version": "abc1234-20260508T132045",
  "provider": "twilio",
  "timestamp": "2026-01-15T10:30:00.000000Z",
  "statistics": {
    "messages": 42,
    "calls": 15,
    "callbacks": 84
  }
}
```

`version` is injected at build time via `-ldflags -X .../httpapi.version=...`. The Makefile stamps it as `<short-git-hash><-dirty>-<UTC-timestamp>`; release builds (driven by goreleaser on `v*` tags) stamp it as `<tag>-<short-commit>`.

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
- **Username**: Account SID (configured via `SMS_MOCK_TWILIO_ACCOUNT_SID`)
- **Password**: Auth Token (configured via `SMS_MOCK_TWILIO_AUTH_TOKEN`)
- **Header**: `Authorization: Basic <base64(account_sid:auth_token)>`

The mock server validates this if `SMS_MOCK_TWILIO_REQUIRE_AUTH=true` (default).

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
services:
  sms-mock-server:
    image: notfoundsam/sms-mock-server:latest
    container_name: sms-mock-server
    ports:
      - "8080:8080"
    volumes:
      - sms-mock-data:/data
    environment:
      - SMS_MOCK_DB_PATH=/data/mock_server.db
      - SMS_MOCK_TWILIO_ACCOUNT_SID=ACXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
      - SMS_MOCK_TWILIO_AUTH_TOKEN=your_auth_token_here
      - SMS_MOCK_TWILIO_SUCCESS_NUMBERS=+15551234567,+15559876543
      - SMS_MOCK_TWILIO_FAILURE_NUMBERS=+15559999999
      - SMS_MOCK_TWILIO_ALLOWED_FROM_NUMBERS=+15550000001,+15550000002
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

volumes:
  sms-mock-data:
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
**Two Dockerfiles serve different paths:**
- `Dockerfile` (local dev / `docker compose up --build`): multi-stage. Stage 1 (`golang:1.25-alpine`) compiles a fully static binary with `CGO_ENABLED=0`; stage 2 (`gcr.io/distroless/static:nonroot`) copies the binary in. Templates and static assets are embedded via `//go:embed` at compile time.
- `Dockerfile.release` (used by goreleaser): single-stage. Goreleaser cross-compiles the binary per-arch outside Docker and places it in the build context; the Dockerfile just copies it onto `gcr.io/distroless/static:nonroot`. Selected via `dockers[].dockerfile` in `.goreleaser.yml`.

Final image (both paths) is ~17–20 MB. No shell, no curl, no package manager — only the binary and a non-root user. All configuration is supplied via env vars at runtime.

**Exposed Ports**:
- `8080` - HTTP

**Volumes**:
- `/data` - SQLite database (persisted across restarts when `SMS_MOCK_DB_PATH` points inside this dir)

Templates are embedded into the binary; there is no `/app/templates` mount.

**Environment Variables**:
- All `SMS_MOCK_*` settings (see §3.6 Config Loader)
- `LOG_LEVEL` - Logging verbosity (`DEBUG`, `INFO`, `WARN`/`WARNING`, `ERROR`; default `INFO`)

### 7.2 Docker Compose Example

**Basic Setup:**
```yaml
services:
  sms-mock-server:
    image: notfoundsam/sms-mock-server:latest
    container_name: sms-mock-server
    ports:
      - "8080:8080"
    environment:
      - LOG_LEVEL=INFO
      - SMS_MOCK_TWILIO_REQUIRE_AUTH=false
```

**With Application Stack:**
```yaml
services:
  sms-mock-server:
    image: notfoundsam/sms-mock-server:latest
    container_name: sms-mock-server
    ports:
      - "8080:8080"
    environment:
      - LOG_LEVEL=INFO
      - SMS_MOCK_TWILIO_ACCOUNT_SID=ACXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
      - SMS_MOCK_TWILIO_AUTH_TOKEN=your_auth_token_here
    networks:
      - app-network
    # Note: the distroless/static base image ships no shell or curl, so the
    # standard CMD-based healthcheck is unavailable. Compose's port-binding
    # readiness is sufficient for `depends_on` semantics; if you need a true
    # healthcheck, build a tiny healthcheck binary into the image.

  your-application:
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

## 8. Extensibility

### 8.1 Adding New Providers
To add a new provider (e.g., MessageBird, Vonage):

1. Create new adapter package at `app/provider/{provider_name}/` implementing `provider.Provider`
2. Add provider-specific templates under `app/templates/responses/{provider_name}/` and `app/templates/errors/{provider_name}/`
3. Add provider-specific env vars (e.g. `SMS_MOCK_{PROVIDER}_*`) and a struct in `app/config/config.go` populated by `applyEnv`
4. Register provider in the factory map in `app/main.go`
5. Rebuild the binary (templates are embedded)

### 8.2 Custom Response Behavior
- Edit JSON templates to modify response structure
- Add conditional logic in templates based on request parameters
- Configure number-based routing (success/failure) via `SMS_MOCK_TWILIO_SUCCESS_NUMBERS` / `SMS_MOCK_TWILIO_FAILURE_NUMBERS`

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
| Language | Go 1.25+ | Single static binary, low footprint |
| Web Framework | stdlib `net/http` (Go 1.22+ mux) | Zero dependencies, native path patterns |
| Phone Validation | `nyaruka/phonenumbers` | Go port of Google libphonenumber |
| Template Engine | `text/template` (JSON) + `html/template` (UI) | Stdlib; auto-escaping for HTML, explicit `json` funcmap for JSON safety |
| Database | SQLite via `modernc.org/sqlite` | Pure-Go driver — no CGO, fully static binary |
| Embedded Assets | `embed.FS` | Templates + static files baked into the binary |
| UI | HTML + HTMX | Simple, no heavy frontend framework |
| Containerization | Docker (`gcr.io/distroless/static:nonroot`) | Minimal runtime, ~17–20 MB image |
| Config Format | Environment variables (stdlib `os.Getenv`) | 12-factor; no config file to mount |
| Response Format | JSON | Standard API format |

## 10. Development Approach

The codebase is organized into single-responsibility packages under `app/`,
each with its own unit tests. Tests use stdlib `testing` plus `testify`
(`assert` / `require`) for clearer assertions.

**Test layers:**
- Unit tests per package (`go test ./...`) — fast, deterministic, use fakes (`app/testutil/`) for storage / HTTP / clock.
- Race detector in CI (`go test -race ./...`).
- Smoke test (`app/main_test.go::TestSmoke_EndToEnd`) — builds the full stack via `httptest.NewServer` and exercises the API end-to-end (POST Messages → persistence → /health → mailbox page → static asset → /clear/all).
- Linting (`make lint`) — `golangci-lint` with 41 linters configured in `.golangci.yml`.

**Adding a new feature** typically means:
1. Add or extend the relevant `app/<pkg>/` types and functions.
2. Add unit tests in the same package.
3. If the change adds an HTTP endpoint, add handler tests in `app/httpapi/` or `app/ui/` covering success + error paths.
4. Update relevant templates if the response shape changed.
5. Ensure `make test-race` and `make lint` are clean before committing.

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
