package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/notfoundsam/sms-mock-server/app/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// smokeTestConfig returns a config for the smoke test: in-memory DB, no
// callback delays, allowed/success/failure numbers in the test data set.
func smokeTestConfig(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{
		Server:   config.Server{Host: "127.0.0.1", Port: 0, Timezone: "UTC"},
		Provider: "twilio",
		Database: config.Database{Path: ":memory:"},
		Twilio: config.Twilio{
			AccountSid: "ACtest",
			AuthToken:  "ttoken",
			Validation: config.Validation{
				RequireAuth:         true,
				ValidatePhoneFormat: true,
				CheckFromNumbers:    true,
				RequireParameters:   true,
			},
			SuccessNumbers:     []string{"+12025550100"},
			AllowedFromNumbers: []string{"+12025551234"},
			FailureNumbers:     []string{"+12025550199"},
			Callbacks: config.Callbacks{
				DelaySeconds:      0,
				RetryAttempts:     1,
				RetryDelaySeconds: 0,
			},
		},
	}
}

// startServer is a test helper that runs the server's wiring (run()) against
// a temporary config and returns an httptest.Server-style handler. We can't
// directly use run() because it binds to a real port; instead this test
// exercises the same wiring through the embedded resources, in-memory DB,
// and a real http.Handler.
//
// For now this is implemented as a manual mini-replica of run() so we don't
// need to refactor run() to take an injectable port. If we add such a hook
// later, this can call run() directly.
//
// The smoke test verifies: end-to-end POST→render→persist works, /health
// reflects the new row, embedded templates and static manifest both load.

func TestSmoke_EndToEnd(t *testing.T) {
	srv := newSmokeServer(t)
	defer srv.Close()

	// 1. /health reports zero counts initially
	checkHealth(t, srv.URL, 0)

	// 2. POST a message
	form := url.Values{
		"From": []string{"+12025551234"},
		"To":   []string{"+12025550100"},
		"Body": []string{"hello"},
	}
	req, err := http.NewRequest("POST",
		srv.URL+"/2010-04-01/Accounts/ACtest/Messages.json",
		strings.NewReader(form.Encode()))
	require.NoError(t, err, "NewRequest")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("ACtest", "ttoken")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "POST")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode, "body=%s", body)

	// 3. /health now reports 1 message
	checkHealth(t, srv.URL, 1)

	// 4. Inbox renders with the message visible
	resp, err = http.Get(srv.URL + "/")
	require.NoError(t, err, "GET /")
	dashBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	assert.Contains(t, string(dashBody), "Messages · SMS Mock", "inbox title missing")
	assert.Contains(t, string(dashBody), "&#43;12025551234", "inbox missing message phone (HTML-escaped)")

	// 5. Static asset is served from embedded FS
	resp, err = http.Get(srv.URL + "/static/css/style.css")
	require.NoError(t, err, "GET /static/css/style.css")
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode, "static asset")

	// 6. /clear/all wipes data
	resp, err = http.Post(srv.URL+"/clear/all", "application/json", nil)
	require.NoError(t, err, "POST /clear/all")
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode, "clear/all")
	checkHealth(t, srv.URL, 0)
}

func checkHealth(t *testing.T, baseURL string, wantMessages int) {
	t.Helper()
	resp, err := http.Get(baseURL + "/health")
	require.NoError(t, err, "GET /health")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "/health")
	var got map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got), "decode /health")
	assert.Equal(t, "healthy", got["status"])
	stats, ok := got["statistics"].(map[string]any)
	require.True(t, ok, "statistics not a map: %v", got["statistics"])
	assert.EqualValues(t, wantMessages, stats["messages"])
}

// newSmokeServer assembles the full stack (embedded templates, in-memory DB,
// dispatcher, all routes) wrapped in an httptest.Server. End-to-end validation
// without binding a real port.
func newSmokeServer(t *testing.T) *httptest.Server {
	t.Helper()
	h, cleanup, err := buildTestHandler(t)
	require.NoError(t, err, "buildTestHandler")
	srv := httptest.NewServer(h)
	t.Cleanup(func() {
		srv.Close()
		cleanup()
	})
	return srv
}

func buildTestHandler(t *testing.T) (http.Handler, func(), error) {
	t.Helper()
	cfg := smokeTestConfig(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	ctx, cancel := context.WithCancel(context.Background())
	st, err := buildStack(ctx, cfg, logger)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	cleanup := func() {
		shutdownCtx, sCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer sCancel()
		_ = st.dispatcher.Close(shutdownCtx)
		_ = st.store.Close()
		cancel()
	}
	return st.handler, cleanup, nil
}
