package testutil

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRequest(t *testing.T, method, url, body string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	require.NoError(t, err, "NewRequest")
	return req
}

func TestFakeHTTPClient_DefaultIs200(t *testing.T) {
	f := NewFakeHTTPClient()
	resp, err := f.Do(newRequest(t, "POST", "http://x/cb", "x=1"))
	require.NoError(t, err, "Do")
	assert.Equal(t, 200, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Empty(t, body)
}

func TestFakeHTTPClient_RecordsRequests(t *testing.T) {
	f := NewFakeHTTPClient()
	req := newRequest(t, "POST", "http://example/cb", "MessageStatus=delivered")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, err := f.Do(req)
	require.NoError(t, err, "Do")

	reqs := f.Requests()
	require.Len(t, reqs, 1)
	assert.Equal(t, "POST", reqs[0].Method)
	assert.Equal(t, "http://example/cb", reqs[0].URL)
	assert.Equal(t, "MessageStatus=delivered", reqs[0].Body)
	assert.Equal(t, "application/x-www-form-urlencoded", reqs[0].Header.Get("Content-Type"))
}

func TestFakeHTTPClient_QueuedResponsesPopFIFO(t *testing.T) {
	f := NewFakeHTTPClient()
	f.QueueResponse(500, "boom")
	f.QueueResponse(200, "ok")
	f.QueueResponse(204, "")

	codes := make([]int, 0, 3)
	for i := 0; i < 3; i++ {
		resp, err := f.Do(newRequest(t, "POST", "http://x", ""))
		require.NoError(t, err, "Do[%d]", i)
		codes = append(codes, resp.StatusCode)
	}
	assert.Equal(t, []int{500, 200, 204}, codes)

	// queue empty → default 200
	resp, _ := f.Do(newRequest(t, "POST", "http://x", ""))
	assert.Equal(t, 200, resp.StatusCode)
}

func TestFakeHTTPClient_QueueError(t *testing.T) {
	f := NewFakeHTTPClient()
	f.QueueError(ErrTransport)

	_, err := f.Do(newRequest(t, "POST", "http://x", ""))
	assert.ErrorIs(t, err, ErrTransport)
}

func TestFakeHTTPClient_AlwaysFailRepeats(t *testing.T) {
	f := NewFakeHTTPClient()
	f.AlwaysFail(ErrTransport)

	for i := 0; i < 3; i++ {
		_, err := f.Do(newRequest(t, "POST", "http://x", ""))
		assert.ErrorIs(t, err, ErrTransport, "call %d", i)
	}
}

func TestFakeHTTPClient_AlwaysReturnRepeats(t *testing.T) {
	f := NewFakeHTTPClient()
	f.AlwaysReturn(503, "down")

	for i := 0; i < 3; i++ {
		resp, err := f.Do(newRequest(t, "POST", "http://x", ""))
		require.NoError(t, err, "Do")
		assert.Equal(t, 503, resp.StatusCode, "call %d", i)
	}
}
