package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/notfoundsam/sms-mock-server/app/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- /health ---

func TestHealth_OKEmpty(t *testing.T) {
	ts := newTestServer(t)
	rec := httptest.NewRecorder()
	ts.Health(rec, httptest.NewRequest("GET", "/health", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var got map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got), "invalid JSON: %s", rec.Body.String())
	assert.Equal(t, "healthy", got["status"])
	assert.Equal(t, "twilio", got["provider"])
	assert.NotEmpty(t, got["version"], "version missing")

	// Timestamp parses as RFC3339-with-microseconds + Z (e.g. 2026-01-15T10:30:00.000000Z)
	ts2, err := time.Parse("2006-01-02T15:04:05.000000Z", got["timestamp"].(string))
	require.NoError(t, err, "timestamp not parseable: %v", got["timestamp"])
	assert.LessOrEqual(t, time.Since(ts2), 5*time.Second, "timestamp not recent: %v", ts2)

	stats, ok := got["statistics"].(map[string]any)
	require.True(t, ok, "statistics is not a map: %v", got["statistics"])
	assert.EqualValues(t, 0, stats["messages"])
	assert.EqualValues(t, 0, stats["calls"])
	assert.EqualValues(t, 0, stats["callbacks"])
}

func TestHealth_StatisticsReflectStore(t *testing.T) {
	ts := newTestServer(t)
	ctx := context.Background()
	_ = ts.store.SaveMessage(ctx, &storage.Message{SID: "SM1", Provider: "twilio", From: "+1", To: "+2", Status: "queued"})
	_ = ts.store.SaveCall(ctx, &storage.Call{SID: "CA1", Provider: "twilio", From: "+1", To: "+2", Status: "queued"})

	rec := httptest.NewRecorder()
	ts.Health(rec, httptest.NewRequest("GET", "/health", nil))

	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	stats := got["statistics"].(map[string]any)
	assert.EqualValues(t, 1, stats["messages"])
	assert.EqualValues(t, 1, stats["calls"])
}

func TestHealth_RoutedThroughHandler(t *testing.T) {
	ts := newTestServer(t)
	h := ts.Handler()

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHealth_PostReturns405(t *testing.T) {
	ts := newTestServer(t)
	h := ts.Handler()
	req := httptest.NewRequest("POST", "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code, "POST /health")
}

// --- /clear/* ---

func TestClearMessages(t *testing.T) {
	ts := newTestServer(t)
	ctx := context.Background()
	_ = ts.store.SaveMessage(ctx, &storage.Message{SID: "SM1", Provider: "twilio", From: "+1", To: "+2", Status: "queued"})
	_ = ts.store.SaveMessage(ctx, &storage.Message{SID: "SM2", Provider: "twilio", From: "+1", To: "+2", Status: "queued"})

	rec := httptest.NewRecorder()
	ts.ClearMessages(rec, httptest.NewRequest("POST", "/clear/messages", nil))

	require.Equal(t, http.StatusOK, rec.Code)

	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	assert.EqualValues(t, 2, got["deleted"])
	assert.Equal(t, "messages", got["type"])

	_, total, _ := ts.store.ListMessages(ctx, 10, 0)
	assert.Equal(t, 0, total, "messages remaining")
}

func TestClearCalls(t *testing.T) {
	ts := newTestServer(t)
	ctx := context.Background()
	_ = ts.store.SaveCall(ctx, &storage.Call{SID: "CA1", Provider: "twilio", From: "+1", To: "+2", Status: "queued"})

	rec := httptest.NewRecorder()
	ts.ClearCalls(rec, httptest.NewRequest("POST", "/clear/calls", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	assert.EqualValues(t, 1, got["deleted"])
	assert.Equal(t, "calls", got["type"])
}

func TestClearCallbacks(t *testing.T) {
	ts := newTestServer(t)
	ctx := context.Background()
	_ = ts.store.SaveCallbackLog(ctx, &storage.CallbackLog{TargetURL: "x", Payload: "y", AttemptNumber: 1})
	_ = ts.store.SaveCallbackLog(ctx, &storage.CallbackLog{TargetURL: "x", Payload: "y", AttemptNumber: 2})
	_ = ts.store.SaveCallbackLog(ctx, &storage.CallbackLog{TargetURL: "x", Payload: "y", AttemptNumber: 3})

	rec := httptest.NewRecorder()
	ts.ClearCallbacks(rec, httptest.NewRequest("POST", "/clear/callbacks", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	assert.EqualValues(t, 3, got["deleted"])
	assert.Equal(t, "callbacks", got["type"])
}

func TestClearAll(t *testing.T) {
	ts := newTestServer(t)
	ctx := context.Background()
	_ = ts.store.SaveMessage(ctx, &storage.Message{SID: "SM1", Provider: "twilio", From: "+1", To: "+2", Status: "queued"})
	_ = ts.store.SaveCall(ctx, &storage.Call{SID: "CA1", Provider: "twilio", From: "+1", To: "+2", Status: "queued"})
	_ = ts.store.SaveCallbackLog(ctx, &storage.CallbackLog{TargetURL: "x", Payload: "y", AttemptNumber: 1})

	rec := httptest.NewRecorder()
	ts.ClearAll(rec, httptest.NewRequest("POST", "/clear/all", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	assert.Equal(t, "all", got["type"])
	deleted, ok := got["deleted"].(map[string]any)
	require.True(t, ok, "deleted not an object: %v", got["deleted"])
	assert.EqualValues(t, 1, deleted["messages"])
	assert.EqualValues(t, 1, deleted["calls"])
	assert.EqualValues(t, 1, deleted["callbacks"])

	// All tables empty
	stats, _ := ts.store.Stats(ctx)
	assert.Equal(t, storage.Stats{}, stats, "Stats after ClearAll")
}

func TestClear_RoutedThroughHandler(t *testing.T) {
	ts := newTestServer(t)
	h := ts.Handler()
	for _, p := range []string{"/clear/messages", "/clear/calls", "/clear/callbacks", "/clear/all"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", p, nil))
		assert.Equal(t, http.StatusOK, rec.Code, "POST %s; body=%s", p, rec.Body.String())
	}
}

func TestClear_GetReturns405(t *testing.T) {
	ts := newTestServer(t)
	h := ts.Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/clear/messages", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code, "GET /clear/messages")
}

// --- /callback-test ---

func TestCallbackTest_AcceptsForm(t *testing.T) {
	ts := newTestServer(t)
	form := url.Values{
		"MessageSid":    []string{"SM123"},
		"MessageStatus": []string{"delivered"},
		"From":          []string{"+15550000001"},
		"To":            []string{"+15551234567"},
	}
	req := httptest.NewRequest("POST", "/callback-test", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	ts.CallbackTest(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var got map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got), "invalid JSON")
	assert.Equal(t, "received", got["status"])

	data, ok := got["data"].(map[string]any)
	require.True(t, ok, "data not a map: %v", got["data"])
	assert.Equal(t, "SM123", data["MessageSid"])
	assert.Equal(t, "delivered", data["MessageStatus"])
}

func TestCallbackTest_EmptyBody(t *testing.T) {
	ts := newTestServer(t)
	req := httptest.NewRequest("POST", "/callback-test", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	ts.CallbackTest(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	assert.Equal(t, "received", got["status"])
	// data is an empty object, not missing
	data, _ := got["data"].(map[string]any)
	assert.Empty(t, data, "expected empty data")
}

func TestCallbackTest_RoutedThroughHandler(t *testing.T) {
	ts := newTestServer(t)
	h := ts.Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/callback-test", strings.NewReader("")))
	assert.Equal(t, http.StatusOK, rec.Code)
}

// --- /favicon.ico ---

func TestFavicon(t *testing.T) {
	ts := newTestServer(t)
	rec := httptest.NewRecorder()
	ts.Favicon(rec, httptest.NewRequest("GET", "/favicon.ico", nil))

	require.Equal(t, http.StatusMovedPermanently, rec.Code)
	assert.Equal(t, "/static/img/icon.svg", rec.Header().Get("Location"))
}

func TestFavicon_RoutedThroughHandler(t *testing.T) {
	ts := newTestServer(t)
	h := ts.Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/favicon.ico", nil))
	assert.Equal(t, http.StatusMovedPermanently, rec.Code)
	assert.Equal(t, "/static/img/icon.svg", rec.Header().Get("Location"))
}

// --- ensure middleware doesn't break aux endpoints ---

func TestAuxEndpoints_DontPanic(t *testing.T) {
	ts := newTestServer(t)
	h := ts.Handler()

	cases := []struct {
		method, path string
		body         string
		wantStatus   int
	}{
		{"GET", "/health", "", http.StatusOK},
		{"GET", "/favicon.ico", "", http.StatusMovedPermanently},
		{"POST", "/clear/messages", "", http.StatusOK},
		{"POST", "/clear/calls", "", http.StatusOK},
		{"POST", "/clear/callbacks", "", http.StatusOK},
		{"POST", "/clear/all", "", http.StatusOK},
		{"POST", "/callback-test", "MessageSid=SM1", http.StatusOK},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		if tc.body != "" {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assert.Equal(t, tc.wantStatus, rec.Code, "%s %s", tc.method, tc.path)
	}
}
