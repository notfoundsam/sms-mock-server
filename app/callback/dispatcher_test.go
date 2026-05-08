package callback

import (
	"context"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/notfoundsam/sms-mock-server/app/clock"
	"github.com/notfoundsam/sms-mock-server/app/storage"
	"github.com/notfoundsam/sms-mock-server/app/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testRig bundles the dispatcher with the fakes that drive it.
type testRig struct {
	d     *Dispatcher
	store *testutil.FakeStore
	clk   *clock.Fake
	http  *testutil.FakeHTTPClient
	t     *testing.T
}

const (
	delay      = 1 * time.Second
	retryDelay = 500 * time.Millisecond
)

func newRig(t *testing.T, retryAttempts int) *testRig {
	t.Helper()
	store := testutil.NewFakeStore()
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	http := testutil.NewFakeHTTPClient()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := New(logger, store, clk, http.Do, Config{
		Delay:         delay,
		RetryAttempts: retryAttempts,
		RetryDelay:    retryDelay,
		Workers:       2,
	}, "ACtest")

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = d.Close(ctx)
	})
	return &testRig{d: d, store: store, clk: clk, http: http, t: t}
}

// drain advances the FakeClock past `dur` and waits for any pending HTTP
// jobs to be processed by the worker pool. Because workers run in real
// goroutines, advancing the fake clock doesn't synchronously drain them —
// we poll for `count` callback log rows or fail after timeout.
//
// expectedCallbackLogs == -1 means "don't wait for callback logs" (used for
// stay-queued / no-URL cases where no logs are expected).
func (r *testRig) drainAndWait(advance time.Duration, expectedCallbackLogs int) {
	r.t.Helper()
	r.clk.Advance(advance)
	if expectedCallbackLogs < 0 {
		// Give workers a brief grace period so any spurious enqueues surface.
		time.Sleep(50 * time.Millisecond)
		return
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, total, _ := r.store.ListCallbackLogs(context.Background(), 0, 0)
		if total >= expectedCallbackLogs {
			// Settle for a tick to surface any extra unexpected logs.
			time.Sleep(20 * time.Millisecond)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.Fail(r.t, "timed out waiting for callback logs", "expected %d", expectedCallbackLogs)
}

// --- SMS scheduling ---

func TestSMS_StayQueued_NothingScheduled(t *testing.T) {
	r := newRig(t, 1)

	// Pre-seed the message so we can verify status doesn't change.
	_ = r.store.SaveMessage(context.Background(), &storage.Message{
		SID: "SM1", Provider: "twilio", From: "+1", To: "+2", Status: "queued",
	})

	r.d.ScheduleSMSStatusFlow("SM1", "+1", "+2", "http://cb", false /*isKnown*/, true)

	r.drainAndWait(10*delay, -1)

	assert.Equal(t, 0, r.clk.Pending(), "Pending timers, want 0 (stay-queued must not schedule)")
	assert.Empty(t, r.http.Requests(), "HTTP requests, want 0")
	assert.Empty(t, r.store.DeliveryEvents(), "DeliveryEvents, want 0")
	m, _ := r.store.GetMessage(context.Background(), "SM1")
	assert.Equal(t, "queued", m.Status, "want queued (no progression for stay-queued)")
}

func TestSMS_SuccessFlow_StatusUpdates(t *testing.T) {
	r := newRig(t, 1)

	_ = r.store.SaveMessage(context.Background(), &storage.Message{
		SID: "SM1", Provider: "twilio", From: "+15550000001", To: "+15551234567",
		Status: "queued", CallbackURL: "http://cb",
	})
	r.http.AlwaysReturn(200, "ok")

	r.d.ScheduleSMSStatusFlow("SM1", "+15550000001", "+15551234567", "http://cb", true, true)

	// Advance one tick at a time and wait for each callback to land before the
	// next status fires. With multiple workers + a single advance past both
	// ticks, jobs could be processed concurrently and observed out of order.
	r.clk.Advance(delay + 50*time.Millisecond)
	waitForLogs(t, r, 1)
	r.clk.Advance(delay)
	waitForLogs(t, r, 2)

	// Status updates land in DB
	m, _ := r.store.GetMessage(context.Background(), "SM1")
	assert.Equal(t, "delivered", m.Status, "final status")

	// Delivery events: 2 in order
	events := r.store.DeliveryEvents()
	require.Len(t, events, 2, "delivery events")
	assert.Equal(t, "sent", events[0].Status, "event[0] status")
	assert.Equal(t, "SM1", events[0].MessageSID, "event[0] MessageSID")
	assert.Equal(t, "delivered", events[1].Status, "event[1] status")

	// HTTP callbacks fired with correct payload shape
	reqs := r.http.Requests()
	require.Len(t, reqs, 2, "HTTP requests")
	for i, want := range []string{"sent", "delivered"} {
		f, err := url.ParseQuery(reqs[i].Body)
		require.NoError(t, err, "parse req %d body", i)
		assert.Equal(t, want, f.Get("MessageStatus"), "req[%d] MessageStatus", i)
		assert.Equal(t, "SM1", f.Get("MessageSid"), "req[%d] MessageSid", i)
		assert.Equal(t, "ACtest", f.Get("AccountSid"), "req[%d] AccountSid", i)
		assert.Equal(t, "2010-04-01", f.Get("ApiVersion"), "req[%d] ApiVersion", i)
		assert.Equal(t, "application/x-www-form-urlencoded", reqs[i].Header.Get("Content-Type"), "req[%d] Content-Type", i)
	}
}

func TestSMS_FailureFlow_SingleFailedTransition(t *testing.T) {
	r := newRig(t, 1)

	_ = r.store.SaveMessage(context.Background(), &storage.Message{
		SID: "SM1", Provider: "twilio", From: "+1", To: "+2", Status: "queued",
		CallbackURL: "http://cb",
	})
	r.http.AlwaysReturn(200, "ok")

	r.d.ScheduleSMSStatusFlow("SM1", "+1", "+2", "http://cb", true, false)

	r.drainAndWait(2*delay, 1)

	m, _ := r.store.GetMessage(context.Background(), "SM1")
	assert.Equal(t, "failed", m.Status, "status")

	events := r.store.DeliveryEvents()
	require.Len(t, events, 1, "events: %+v", events)
	assert.Equal(t, "failed", events[0].Status, "events: %+v", events)

	reqs := r.http.Requests()
	require.Len(t, reqs, 1, "HTTP requests")
	f, _ := url.ParseQuery(reqs[0].Body)
	assert.Equal(t, "failed", f.Get("MessageStatus"))
}

func TestSMS_NoCallbackURL_StillUpdatesDB(t *testing.T) {
	r := newRig(t, 1)

	_ = r.store.SaveMessage(context.Background(), &storage.Message{
		SID: "SM1", Provider: "twilio", From: "+1", To: "+2", Status: "queued",
	})

	// callbackURL = "" → no HTTP, but still status updates + delivery events
	r.d.ScheduleSMSStatusFlow("SM1", "+1", "+2", "", true, true)

	r.drainAndWait(2*delay+100*time.Millisecond, -1)

	m, _ := r.store.GetMessage(context.Background(), "SM1")
	assert.Equal(t, "delivered", m.Status, "status")
	assert.Len(t, r.store.DeliveryEvents(), 2, "delivery events (must record even without callback)")
	assert.Empty(t, r.http.Requests(), "HTTP requests, want 0 (no URL)")
}

// --- Calls scheduling ---

func TestCall_SuccessFlow(t *testing.T) {
	r := newRig(t, 1)

	_ = r.store.SaveCall(context.Background(), &storage.Call{
		SID: "CA1", Provider: "twilio", From: "+1", To: "+2", Status: "queued",
		CallbackURL: "http://cb",
	})
	r.http.AlwaysReturn(200, "ok")

	r.d.ScheduleCallStatusFlow("CA1", "+1", "+2", "http://cb", true, true)

	// Advance one tick at a time so each HTTP callback completes before
	// the next one is scheduled to fire (workers can otherwise reorder).
	r.clk.Advance(delay + 50*time.Millisecond)
	waitForLogs(t, r, 1)
	r.clk.Advance(delay)
	waitForLogs(t, r, 2)
	r.clk.Advance(delay)
	waitForLogs(t, r, 3)

	c, _ := r.store.GetCall(context.Background(), "CA1")
	assert.Equal(t, "completed", c.Status, "final status")

	events := r.store.DeliveryEvents()
	require.Len(t, events, 3, "events")
	wantStatuses := []string{"ringing", "in-progress", "completed"}
	for i, want := range wantStatuses {
		assert.Equal(t, want, events[i].Status, "event[%d] status", i)
		assert.Equal(t, "CA1", events[i].CallSID, "event[%d] CallSID", i)
	}

	reqs := r.http.Requests()
	require.Len(t, reqs, 3, "HTTP requests")
	for i, want := range wantStatuses {
		f, _ := url.ParseQuery(reqs[i].Body)
		assert.Equal(t, want, f.Get("CallStatus"), "req[%d] CallStatus", i)
		assert.Equal(t, "CA1", f.Get("CallSid"), "req[%d] CallSid", i)
		assert.Equal(t, "outbound-api", f.Get("Direction"), "req[%d] Direction", i)
	}
}

func TestCall_StayQueued(t *testing.T) {
	r := newRig(t, 1)
	r.d.ScheduleCallStatusFlow("CA1", "+1", "+2", "http://cb", false, true)
	r.drainAndWait(10*delay, -1)

	assert.Empty(t, r.http.Requests(), "HTTP requests, want 0")
}

// --- Retry logic ---

func TestRetry_SucceedsAfterTransientFailure(t *testing.T) {
	r := newRig(t, 3)

	_ = r.store.SaveMessage(context.Background(), &storage.Message{
		SID: "SM1", Provider: "twilio", From: "+1", To: "+2", Status: "queued",
		CallbackURL: "http://cb",
	})
	// 500 then 200 → second attempt succeeds.
	r.http.QueueResponse(500, "boom")
	r.http.QueueResponse(200, "ok")
	// Defaults for any further calls (the "delivered" status).
	r.http.QueueResponse(200, "ok")

	r.d.ScheduleSMSStatusFlow("SM1", "+1", "+2", "http://cb", true, true)

	// Tick 1 fires at 1s; attempt 1 → 500; sleeps retryDelay; attempt 2 → 200; done.
	// Tick 2 fires at 2s; attempt 1 → 200; done.
	//
	// Worker uses clk.AfterFunc for the retry sleep, so we need to advance the
	// fake clock past delay + retryDelay + delay.
	r.clk.Advance(delay + 100*time.Millisecond) // fire tick 1
	// Wait for attempt 1's 500 to be logged
	waitForLogs(t, r, 1)
	r.clk.Advance(retryDelay + 100*time.Millisecond) // unblock retry sleep
	waitForLogs(t, r, 2)
	r.clk.Advance(delay) // fire tick 2
	waitForLogs(t, r, 3)

	logs, _, _ := r.store.ListCallbackLogs(context.Background(), 100, 0)
	require.Len(t, logs, 3, "logs (500, 200, 200)")

	// HTTP requests: 3 total
	assert.Len(t, r.http.Requests(), 3, "HTTP requests")
}

func TestRetry_ExhaustedAfterAllAttemptsFail(t *testing.T) {
	r := newRig(t, 3)

	_ = r.store.SaveMessage(context.Background(), &storage.Message{
		SID: "SM1", Provider: "twilio", From: "+1", To: "+2", Status: "queued",
		CallbackURL: "http://cb",
	})
	r.http.AlwaysReturn(500, "boom")

	r.d.ScheduleSMSStatusFlow("SM1", "+1", "+2", "http://cb", true, false) // single "failed" tick

	r.clk.Advance(delay + 100*time.Millisecond) // fire tick
	waitForLogs(t, r, 1)
	r.clk.Advance(retryDelay)
	waitForLogs(t, r, 2)
	r.clk.Advance(retryDelay)
	waitForLogs(t, r, 3)

	logs, _, _ := r.store.ListCallbackLogs(context.Background(), 100, 0)
	require.Len(t, logs, 3, "logs (RetryAttempts=3)")
	for _, l := range logs {
		// logs are newest-first; reverse via attempt number
		assert.Equal(t, 500, l.StatusCode, "log %d StatusCode", l.AttemptNumber)
	}

	// Status update still happened (DB write doesn't depend on HTTP success)
	m, _ := r.store.GetMessage(context.Background(), "SM1")
	assert.Equal(t, "failed", m.Status, "DB status (status update independent of HTTP retries)")
}

func TestRetry_TransportError(t *testing.T) {
	r := newRig(t, 2)

	_ = r.store.SaveMessage(context.Background(), &storage.Message{
		SID: "SM1", Provider: "twilio", From: "+1", To: "+2", Status: "queued",
		CallbackURL: "http://cb",
	})
	r.http.AlwaysFail(testutil.ErrTransport)

	r.d.ScheduleSMSStatusFlow("SM1", "+1", "+2", "http://cb", true, false)

	r.clk.Advance(delay + 100*time.Millisecond)
	waitForLogs(t, r, 1)
	r.clk.Advance(retryDelay)
	waitForLogs(t, r, 2)

	logs, _, _ := r.store.ListCallbackLogs(context.Background(), 100, 0)
	require.Len(t, logs, 2, "logs")
	for _, l := range logs {
		assert.Equal(t, 0, l.StatusCode, "transport-error log StatusCode")
		assert.Contains(t, l.ResponseBody, "Error:", "transport-error log body; want 'Error: ...'")
	}
}

// --- Payload shape (callback log JSON) ---

func TestCallbackLogPayloadIsJSON(t *testing.T) {
	r := newRig(t, 1)

	_ = r.store.SaveMessage(context.Background(), &storage.Message{
		SID: "SM1", Provider: "twilio", From: "+1", To: "+2", Status: "queued",
		CallbackURL: "http://cb",
	})
	r.http.AlwaysReturn(200, "")

	r.d.ScheduleSMSStatusFlow("SM1", "+1", "+2", "http://cb", true, false)
	r.drainAndWait(delay+100*time.Millisecond, 1)

	logs, _, _ := r.store.ListCallbackLogs(context.Background(), 1, 0)
	require.NotEmpty(t, logs, "no logs")
	assert.True(t, strings.HasPrefix(logs[0].Payload, "{"), "payload not JSON-shaped: %q", logs[0].Payload)
	assert.Contains(t, logs[0].Payload, `"MessageSid":"SM1"`, "payload not JSON-shaped: %q", logs[0].Payload)
}

// --- Close / late-firing-timer safety ---

func TestClose_DrainsInFlight(t *testing.T) {
	r := newRig(t, 1)
	_ = r.store.SaveMessage(context.Background(), &storage.Message{
		SID: "SM1", Provider: "twilio", From: "+1", To: "+2", Status: "queued",
		CallbackURL: "http://cb",
	})
	r.http.AlwaysReturn(200, "ok")

	r.d.ScheduleSMSStatusFlow("SM1", "+1", "+2", "http://cb", true, false)
	r.clk.Advance(delay + 100*time.Millisecond) // fire the tick → enqueue job

	// Wait for job to land
	waitForLogs(t, r, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	require.NoError(t, r.d.Close(ctx), "Close")
	assert.True(t, r.d.shuttingDown.Load(), "shuttingDown not set after Close")
}

func TestClose_LateTimerSkipsAllOperations(t *testing.T) {
	store := testutil.NewFakeStore()
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	http := testutil.NewFakeHTTPClient()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := New(logger, store, clk, http.Do, Config{
		Delay:         delay,
		RetryAttempts: 1,
		RetryDelay:    retryDelay,
		Workers:       2,
	}, "ACtest")

	_ = store.SaveMessage(context.Background(), &storage.Message{
		SID: "SM1", Provider: "twilio", From: "+1", To: "+2", Status: "queued",
		CallbackURL: "http://cb",
	})

	d.ScheduleSMSStatusFlow("SM1", "+1", "+2", "http://cb", true, false)

	// Close BEFORE advancing the clock — the timer is registered but hasn't fired.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	require.NoError(t, d.Close(ctx), "Close")

	// Now advance the clock; the timer fires, but everything must be skipped.
	clk.Advance(2 * delay)
	time.Sleep(50 * time.Millisecond) // grace period

	m, _ := store.GetMessage(context.Background(), "SM1")
	assert.Equal(t, "queued", m.Status, "late timer must NOT update after Close")
	assert.Empty(t, store.DeliveryEvents(), "delivery events; late timer must NOT write events after Close")
	assert.Empty(t, http.Requests(), "HTTP requests; late timer must NOT enqueue jobs after Close")
}

func TestClose_Idempotent(t *testing.T) {
	r := newRig(t, 1)
	ctx := context.Background()
	assert.NoError(t, r.d.Close(ctx), "first Close")
	assert.NoError(t, r.d.Close(ctx), "second Close")
}

// --- Helpers ---

func waitForLogs(t *testing.T, r *testRig, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, total, _ := r.store.ListCallbackLogs(context.Background(), 0, 0)
		if total >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, total, _ := r.store.ListCallbackLogs(context.Background(), 0, 0)
	require.Fail(t, "timed out waiting for callback logs", "got %d, want %d", total, want)
}

// Dispatcher must satisfy httpapi.Dispatcher (tested by importing through a check)
func TestDispatcher_SatisfiesHTTPAPIInterface(t *testing.T) {
	// Compile-time check via interface assignment in a func body.
	var _ interface {
		ScheduleSMSStatusFlow(string, string, string, string, bool, bool)
		ScheduleCallStatusFlow(string, string, string, string, bool, bool)
	} = (*Dispatcher)(nil)
}
