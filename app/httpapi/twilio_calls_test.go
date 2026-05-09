package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/notfoundsam/sms-mock-server/app/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// authedCallRequest builds a *http.Request for POST .../Calls.json with valid
// Basic auth and a form body, mirroring authedSMSRequest.
func authedCallRequest(form url.Values) *http.Request {
	req := httptest.NewRequest(
		"POST",
		"/2010-04-01/Accounts/ACtest/Calls.json",
		strings.NewReader(form.Encode()),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization",
		"Basic "+base64.StdEncoding.EncodeToString([]byte("ACtest:ttoken")))
	req.SetPathValue("AccountSid", "ACtest")
	return req
}

func defaultCallForm() url.Values {
	return url.Values{
		"From": []string{validFrom},
		"To":   []string{registeredTo},
		"Url":  []string{"http://example.com/twiml"},
	}
}

// --- success path ---

func TestMakeCall_Success(t *testing.T) {
	ts := newTestServer(t)

	rec := httptest.NewRecorder()
	ts.MakeCall(rec, authedCallRequest(defaultCallForm()))

	require.Equal(t, http.StatusCreated, rec.Code, "body=%s", rec.Body.String())
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var got map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got), "response is not valid JSON; body: %s", rec.Body.String())

	sid, _ := got["sid"].(string)
	assert.True(t, strings.HasPrefix(sid, "CA"))
	assert.Len(t, sid, 34)
	assert.Equal(t, "queued", got["status"])
	assert.Equal(t, registeredTo, got["to"])
	assert.Equal(t, validFrom, got["from"])
	assert.Equal(t, "ACtest", got["account_sid"])
	assert.Equal(t, "outbound-api", got["direction"])
	assert.Equal(t, "2010-04-01", got["api_version"])
}

func TestMakeCall_PersistsAtQueuedWithTwiMLURL(t *testing.T) {
	ts := newTestServer(t)

	rec := httptest.NewRecorder()
	ts.MakeCall(rec, authedCallRequest(defaultCallForm()))

	rows, total, err := ts.store.ListCalls(context.Background(), 100, 0)
	require.NoError(t, err, "ListCalls")
	require.Equal(t, 1, total)
	require.Len(t, rows, 1)
	c := rows[0]
	assert.Equal(t, "queued", c.Status)
	assert.Equal(t, validFrom, c.From)
	assert.Equal(t, registeredTo, c.To)
	assert.Equal(t, "http://example.com/twiml", c.TwiMLURL)
	assert.Equal(t, "twilio", c.Provider)
}

func TestMakeCall_SchedulesCallFlow(t *testing.T) {
	ts := newTestServer(t)

	form := defaultCallForm()
	form.Set("StatusCallback", "http://app/cb")

	rec := httptest.NewRecorder()
	ts.MakeCall(rec, authedCallRequest(form))

	calls := ts.dispatcher.CallCalls()
	require.Len(t, calls, 1)
	c := calls[0]
	assert.Equal(t, validFrom, c.From)
	assert.Equal(t, registeredTo, c.To)
	assert.Equal(t, "http://app/cb", c.CallbackURL)
	assert.True(t, c.IsKnown)
	assert.True(t, c.WillSucceed)

	// Did not schedule SMS flow (sanity)
	assert.Empty(t, ts.dispatcher.SMSCalls(), "no SMS flow should be scheduled by MakeCall")
}

func TestMakeCall_FailureNumber(t *testing.T) {
	ts := newTestServer(t)
	form := defaultCallForm()
	form.Set("To", failureTo)

	rec := httptest.NewRecorder()
	ts.MakeCall(rec, authedCallRequest(form))

	require.Equal(t, http.StatusCreated, rec.Code)
	calls := ts.dispatcher.CallCalls()
	require.Len(t, calls, 1)
	assert.True(t, calls[0].IsKnown)
	assert.False(t, calls[0].WillSucceed)
}

func TestMakeCall_UnknownNumber(t *testing.T) {
	ts := newTestServer(t)
	form := defaultCallForm()
	form.Set("To", unknownTo)

	rec := httptest.NewRecorder()
	ts.MakeCall(rec, authedCallRequest(form))

	require.Equal(t, http.StatusCreated, rec.Code)
	calls := ts.dispatcher.CallCalls()
	require.Len(t, calls, 1)
	assert.False(t, calls[0].IsKnown)
}

// --- error paths ---

func TestMakeCall_AuthFailed(t *testing.T) {
	ts := newTestServer(t)
	req := authedCallRequest(defaultCallForm())
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("ACwrong:bad")))

	rec := httptest.NewRecorder()
	ts.MakeCall(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)

	rows, total, _ := ts.store.ListCalls(context.Background(), 10, 0)
	assert.Equal(t, 0, total, "auth-failed should not persist")
	assert.Empty(t, rows)
	assert.Empty(t, ts.dispatcher.CallCalls(), "auth-failed should not schedule")
}

func TestMakeCall_MissingURL(t *testing.T) {
	ts := newTestServer(t)
	form := defaultCallForm()
	form.Del("Url")

	rec := httptest.NewRecorder()
	ts.MakeCall(rec, authedCallRequest(form))

	require.Equal(t, http.StatusBadRequest, rec.Code, "body=%s", rec.Body.String())

	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	assert.EqualValues(t, provider.ErrCodeMissingParameter, got["code"])
	msg, _ := got["message"].(string)
	assert.Contains(t, msg, "Url")
}

func TestMakeCall_MissingFrom(t *testing.T) {
	ts := newTestServer(t)
	form := defaultCallForm()
	form.Del("From")

	rec := httptest.NewRecorder()
	ts.MakeCall(rec, authedCallRequest(form))

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	assert.EqualValues(t, provider.ErrCodeMissingParameter, got["code"])
}

func TestMakeCall_InvalidPhoneNumber(t *testing.T) {
	ts := newTestServer(t)
	form := defaultCallForm()
	form.Set("To", "+1234")

	rec := httptest.NewRecorder()
	ts.MakeCall(rec, authedCallRequest(form))

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	assert.EqualValues(t, provider.ErrCodeInvalidPhoneNumber, got["code"])
}

func TestMakeCall_InvalidFromNumber(t *testing.T) {
	ts := newTestServer(t)
	form := defaultCallForm()
	form.Set("From", notAllowedFrom)

	rec := httptest.NewRecorder()
	ts.MakeCall(rec, authedCallRequest(form))

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	assert.EqualValues(t, provider.ErrCodeInvalidFromNumber, got["code"])
}

// Router integration: route .../Calls.json to MakeCall (no longer 501).
func TestHandler_RoutesPostCalls(t *testing.T) {
	ts := newTestServer(t)
	h := ts.Handler()

	form := defaultCallForm()
	req := httptest.NewRequest("POST",
		"/2010-04-01/Accounts/ACtest/Calls.json",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization",
		"Basic "+base64.StdEncoding.EncodeToString([]byte("ACtest:ttoken")))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code, "body=%s", rec.Body.String())
}
