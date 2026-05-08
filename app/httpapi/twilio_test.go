package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/notfoundsam/sms-mock-server/app/config"
	"github.com/notfoundsam/sms-mock-server/app/provider"
	"github.com/notfoundsam/sms-mock-server/app/provider/twilio"
	tmpl "github.com/notfoundsam/sms-mock-server/app/template"
	"github.com/notfoundsam/sms-mock-server/app/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Numbers used in handler tests — same set as the Twilio adapter tests
// so libphonenumber accepts them.
const (
	validFrom        = "+12025551234" // in allowedFrom
	notAllowedFrom   = "+12025558888" // valid format, not in allowedFrom
	registeredTo     = "+12025550100" // in registeredNumbers
	failureTo        = "+12025550199" // in failureNumbers
	unknownTo        = "+12025557777" // valid, in neither list
)

// testServer builds a fully-wired Server with the real Twilio provider,
// real template engine, in-memory store, and a FakeDispatcher.
type testServer struct {
	*Server
	store      *testutil.FakeStore
	dispatcher *testutil.FakeDispatcher
}

func newTestServer(t *testing.T, mutate ...func(*config.Twilio)) *testServer {
	t.Helper()

	cfg := &config.Twilio{
		AccountSid: "ACtest",
		AuthToken:  "ttoken",
		Validation: config.Validation{
			RequireAuth:         true,
			ValidatePhoneFormat: true,
			CheckFromNumbers:    true,
			RequireParameters:   true,
		},
		DefaultBehavior:    "success",
		RegisteredNumbers:  []string{registeredTo},
		AllowedFromNumbers: []string{validFrom},
		FailureNumbers:     []string{failureTo},
		Callbacks:          config.Callbacks{Enabled: true},
	}
	for _, fn := range mutate {
		fn(cfg)
	}

	engine, err := tmpl.New(os.DirFS("../templates"), tmpl.Options{
		Provider: "twilio",
		Now:      func() time.Time { return time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC) },
	})
	require.NoError(t, err, "template engine")

	store := testutil.NewFakeStore()
	disp := testutil.NewFakeDispatcher()

	srv := NewServer(Deps{
		Logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		Provider:         twilio.New(cfg),
		Store:            store,
		Templates:        engine,
		Dispatcher:       disp,
		AccountSid:       cfg.AccountSid,
		CallbacksEnabled: cfg.Callbacks.Enabled,
	})
	return &testServer{Server: srv, store: store, dispatcher: disp}
}

// Handler builds a fully-wrapped http.Handler for end-to-end tests by
// composing the same pieces main.go uses: RegisterRoutes + WrapWithMiddleware.
// Test-only — production wiring lives in cmd-side main.go.
func (ts *testServer) Handler() http.Handler {
	mux := http.NewServeMux()
	ts.RegisterRoutes(mux)
	return WrapWithMiddleware(mux, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// authedSMSRequest builds a *http.Request for POST .../Messages.json with valid
// HTTP Basic auth and a form body. Tests can override fields by passing form values.
func authedSMSRequest(form url.Values) *http.Request {
	req := httptest.NewRequest(
		"POST",
		"/2010-04-01/Accounts/ACtest/Messages.json",
		strings.NewReader(form.Encode()),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization",
		"Basic "+base64.StdEncoding.EncodeToString([]byte("ACtest:ttoken")))
	// http.ServeMux's path patterns rely on r.SetPathValue when called via
	// the mux; httptest.NewRequest doesn't run through mux, so set it manually.
	req.SetPathValue("AccountSid", "ACtest")
	return req
}

func defaultSMSForm() url.Values {
	return url.Values{
		"From": []string{validFrom},
		"To":   []string{registeredTo},
		"Body": []string{"hello"},
	}
}

// --- success path ---

func TestSendMessage_Success(t *testing.T) {
	ts := newTestServer(t)

	rec := httptest.NewRecorder()
	ts.SendMessage(rec, authedSMSRequest(defaultSMSForm()))

	require.Equal(t, http.StatusCreated, rec.Code, "body=%s", rec.Body.String())
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var got map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got), "response is not valid JSON; body: %s", rec.Body.String())

	sid, _ := got["sid"].(string)
	assert.True(t, strings.HasPrefix(sid, "SM"))
	assert.Len(t, sid, 34)
	assert.Equal(t, "queued", got["status"])
	assert.Equal(t, registeredTo, got["to"])
	assert.Equal(t, validFrom, got["from"])
	assert.Equal(t, "hello", got["body"])
	assert.Equal(t, "ACtest", got["account_sid"])
}

func TestSendMessage_PersistsAtQueued(t *testing.T) {
	ts := newTestServer(t)

	rec := httptest.NewRecorder()
	ts.SendMessage(rec, authedSMSRequest(defaultSMSForm()))

	rows, total, err := ts.store.ListMessages(context.Background(), 100, 0)
	require.NoError(t, err, "ListMessages")
	require.Equal(t, 1, total)
	require.Len(t, rows, 1)
	m := rows[0]
	assert.Equal(t, "queued", m.Status)
	assert.Equal(t, validFrom, m.From)
	assert.Equal(t, registeredTo, m.To)
	assert.Equal(t, "hello", m.Body)
	assert.Equal(t, "twilio", m.Provider)
}

func TestSendMessage_SchedulesCallbackFlow(t *testing.T) {
	ts := newTestServer(t)

	form := defaultSMSForm()
	form.Set("StatusCallback", "http://app/cb")
	rec := httptest.NewRecorder()
	ts.SendMessage(rec, authedSMSRequest(form))

	calls := ts.dispatcher.SMSCalls()
	require.Len(t, calls, 1)
	c := calls[0]
	assert.Equal(t, validFrom, c.From)
	assert.Equal(t, registeredTo, c.To)
	assert.Equal(t, "http://app/cb", c.CallbackURL)
	assert.True(t, c.IsKnown, "IsKnown = false; registered number must be known")
	assert.True(t, c.WillSucceed, "WillSucceed = false; registered number must succeed")
}

func TestSendMessage_FailureNumber(t *testing.T) {
	ts := newTestServer(t)
	form := defaultSMSForm()
	form.Set("To", failureTo)
	form.Set("StatusCallback", "http://app/cb")

	rec := httptest.NewRecorder()
	ts.SendMessage(rec, authedSMSRequest(form))

	require.Equal(t, http.StatusCreated, rec.Code)

	calls := ts.dispatcher.SMSCalls()
	require.Len(t, calls, 1)
	assert.True(t, calls[0].IsKnown)
	assert.False(t, calls[0].WillSucceed)
}

func TestSendMessage_UnknownNumberSchedulesStayQueued(t *testing.T) {
	ts := newTestServer(t)
	form := defaultSMSForm()
	form.Set("To", unknownTo)
	form.Set("StatusCallback", "http://app/cb")

	rec := httptest.NewRecorder()
	ts.SendMessage(rec, authedSMSRequest(form))

	require.Equal(t, http.StatusCreated, rec.Code)

	calls := ts.dispatcher.SMSCalls()
	require.Len(t, calls, 1)
	assert.False(t, calls[0].IsKnown, "IsKnown = true; expected false for unknown number")
	// WillSucceed value doesn't matter when IsKnown=false (dispatcher will short-circuit)
}

func TestSendMessage_CallbacksDisabledClearsURL(t *testing.T) {
	ts := newTestServer(t, func(c *config.Twilio) {
		c.Callbacks.Enabled = false
	})
	form := defaultSMSForm()
	form.Set("StatusCallback", "http://app/cb")

	rec := httptest.NewRecorder()
	ts.SendMessage(rec, authedSMSRequest(form))

	calls := ts.dispatcher.SMSCalls()
	require.Len(t, calls, 1)
	assert.Empty(t, calls[0].CallbackURL, "CallbackURL should be empty when callbacks globally disabled")
	// Persisted record still has the original URL — Python behavior
	rows, _, _ := ts.store.ListMessages(context.Background(), 10, 0)
	assert.Equal(t, "http://app/cb", rows[0].CallbackURL, "persisted CallbackURL should retain user-supplied URL")
}

// --- error paths ---

func TestSendMessage_AuthFailed(t *testing.T) {
	ts := newTestServer(t)
	req := authedSMSRequest(defaultSMSForm())
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("ACwrong:bad")))

	rec := httptest.NewRecorder()
	ts.SendMessage(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code, "body=%s", rec.Body.String())

	var got map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got), "invalid JSON: %s", rec.Body.String())
	assert.EqualValues(t, provider.ErrCodeAuthFailed, got["code"])
	assert.EqualValues(t, 401, got["status"])

	// Did not persist or schedule
	rows, total, _ := ts.store.ListMessages(context.Background(), 10, 0)
	assert.Equal(t, 0, total, "auth-failed should not persist")
	assert.Empty(t, rows)
	assert.Empty(t, ts.dispatcher.SMSCalls(), "auth-failed should not schedule")
}

func TestSendMessage_MissingParameter(t *testing.T) {
	ts := newTestServer(t)
	form := defaultSMSForm()
	form.Del("Body")

	rec := httptest.NewRecorder()
	ts.SendMessage(rec, authedSMSRequest(form))

	require.Equal(t, http.StatusBadRequest, rec.Code, "body=%s", rec.Body.String())

	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	assert.EqualValues(t, provider.ErrCodeMissingParameter, got["code"])
	msg, _ := got["message"].(string)
	assert.Contains(t, msg, "Body")
}

func TestSendMessage_InvalidPhoneNumber(t *testing.T) {
	ts := newTestServer(t)
	form := defaultSMSForm()
	form.Set("To", "+1234")

	rec := httptest.NewRecorder()
	ts.SendMessage(rec, authedSMSRequest(form))

	require.Equal(t, http.StatusBadRequest, rec.Code, "body=%s", rec.Body.String())

	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	assert.EqualValues(t, provider.ErrCodeInvalidPhoneNumber, got["code"])
	msg, _ := got["message"].(string)
	assert.Contains(t, msg, "+1234")
}

func TestSendMessage_InvalidFromNumber(t *testing.T) {
	ts := newTestServer(t)
	form := defaultSMSForm()
	form.Set("From", notAllowedFrom)

	rec := httptest.NewRecorder()
	ts.SendMessage(rec, authedSMSRequest(form))

	require.Equal(t, http.StatusBadRequest, rec.Code, "body=%s", rec.Body.String())

	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	assert.EqualValues(t, provider.ErrCodeInvalidFromNumber, got["code"])
}

// Validation order — auth checked first, even if other params are missing.
func TestSendMessage_AuthCheckedBeforeOtherValidation(t *testing.T) {
	ts := newTestServer(t)
	// Missing all params + bad auth → auth_failed wins
	req := httptest.NewRequest("POST",
		"/2010-04-01/Accounts/ACtest/Messages.json",
		strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("AccountSid", "ACtest")

	rec := httptest.NewRecorder()
	ts.SendMessage(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code, "expected 401 (auth before params)")
}

// SID generator: prefix + 32 hex, unique across calls.
func TestGenerateSID(t *testing.T) {
	a := generateSID("SM")
	b := generateSID("SM")
	assert.NotEqual(t, a, b, "two SIDs collided")
	for _, s := range []string{a, b} {
		assert.True(t, strings.HasPrefix(s, "SM"))
		assert.Len(t, s, 34)
		// post-prefix is hex
		hex := s[2:]
		for _, r := range hex {
			if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
				t.Errorf("SID %q has non-hex char %q", s, r)
			}
		}
	}
}

func TestGenerateSID_CallPrefix(t *testing.T) {
	s := generateSID("CA")
	assert.True(t, strings.HasPrefix(s, "CA"))
	assert.Len(t, s, 34)
}

// --- Router integration via real ServeMux (path patterns + middleware) ---

func TestHandler_RoutesPostMessages(t *testing.T) {
	ts := newTestServer(t)
	h := ts.Handler()

	form := defaultSMSForm()
	req := httptest.NewRequest("POST",
		"/2010-04-01/Accounts/ACtest/Messages.json",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization",
		"Basic "+base64.StdEncoding.EncodeToString([]byte("ACtest:ttoken")))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code, "body=%s", rec.Body.String())
}

func TestHandler_GetMessagesIs405(t *testing.T) {
	ts := newTestServer(t)
	h := ts.Handler()

	req := httptest.NewRequest("GET", "/2010-04-01/Accounts/ACtest/Messages.json", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code, "GET on POST-only route")
}

// Recoverer middleware must convert handler panics to 500.
func TestRecoverer_ConvertsPanicTo500(t *testing.T) {
	ts := newTestServer(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/boom", func(w http.ResponseWriter, r *http.Request) {
		panic("kaboom")
	})
	h := chain(recoverer(ts.logger), requestLogger(ts.logger))(mux)

	req := httptest.NewRequest("GET", "/boom", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code, "panicked handler should be 500")
}
