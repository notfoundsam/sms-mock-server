package testutil

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"sync"
)

// FakeHTTPClient records outgoing requests and returns scripted responses.
// The dispatcher's `httpDo` field accepts any
// `func(*http.Request) (*http.Response, error)`, so call f.Do directly.
type FakeHTTPClient struct {
	mu        sync.Mutex
	requests  []recordedRequest
	responses []scriptedResponse
	// alwaysMode means the single response in `responses` is returned repeatedly
	// instead of popped FIFO. Set by AlwaysFail / AlwaysReturn.
	alwaysMode bool
}

type recordedRequest struct {
	Method string
	URL    string
	Body   []byte
	Header http.Header
}

type scriptedResponse struct {
	statusCode int
	body       string
	err        error
}

// NewFakeHTTPClient returns a client that returns 200 OK for every call by default.
func NewFakeHTTPClient() *FakeHTTPClient { return &FakeHTTPClient{} }

// QueueResponse adds a scripted response to the FIFO queue. If the queue is
// empty when a request arrives, the client returns 200 OK with an empty body.
func (f *FakeHTTPClient) QueueResponse(statusCode int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responses = append(f.responses, scriptedResponse{statusCode: statusCode, body: body})
	f.alwaysMode = false
}

// QueueError adds a scripted transport error.
func (f *FakeHTTPClient) QueueError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responses = append(f.responses, scriptedResponse{err: err})
	f.alwaysMode = false
}

// AlwaysFail makes every call return the given transport error.
func (f *FakeHTTPClient) AlwaysFail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responses = []scriptedResponse{{err: err}}
	f.alwaysMode = true
}

// AlwaysReturn makes every call return the given status/body.
func (f *FakeHTTPClient) AlwaysReturn(statusCode int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responses = []scriptedResponse{{statusCode: statusCode, body: body}}
	f.alwaysMode = true
}

// Do is the function value passed to the dispatcher. It records the request
// (including its body) and returns the next scripted response.
func (f *FakeHTTPClient) Do(req *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	if req.Body != nil {
		_ = req.Body.Close()
	}

	f.mu.Lock()
	rec := recordedRequest{
		Method: req.Method,
		URL:    req.URL.String(),
		Body:   body,
		Header: req.Header.Clone(),
	}
	f.requests = append(f.requests, rec)

	var resp scriptedResponse
	switch {
	case len(f.responses) == 0:
		resp = scriptedResponse{statusCode: 200, body: ""}
	case f.alwaysMode:
		resp = f.responses[0]
	default:
		resp = f.responses[0]
		f.responses = f.responses[1:]
	}
	f.mu.Unlock()

	if resp.err != nil {
		return nil, resp.err
	}
	return &http.Response{
		StatusCode: resp.statusCode,
		Body:       io.NopCloser(bytes.NewReader([]byte(resp.body))),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

// Requests returns a snapshot of all requests recorded so far.
func (f *FakeHTTPClient) Requests() []RecordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]RecordedRequest, len(f.requests))
	for i, r := range f.requests {
		out[i] = RecordedRequest{
			Method: r.Method,
			URL:    r.URL,
			Body:   string(r.Body),
			Header: r.Header.Clone(),
		}
	}
	return out
}

// RecordedRequest is the public, copy-friendly view of a recorded request.
type RecordedRequest struct {
	Method string
	URL    string
	Body   string
	Header http.Header
}

// ErrTransport is a convenience sentinel for tests that need a transport-level error.
var ErrTransport = errors.New("simulated transport error")
