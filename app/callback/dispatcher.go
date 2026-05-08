// Package callback delivers status callbacks for messages and calls.
//
// Architecture:
//
//   - ScheduleSMSStatusFlow / ScheduleCallStatusFlow are called from HTTP
//     handlers right after the request response is written. They schedule a
//     sequence of "ticks" via clock.AfterFunc at multiples of cfg.Delay.
//
//   - Each tick (a) updates the row's status in the store, (b) writes a
//     delivery_event, and (c) if a callback URL is set, builds the form-encoded
//     payload and enqueues an HTTP POST job onto a buffered channel.
//
//   - A pool of workers drains the job channel, sending each POST with retries
//     (fixed delay, 2xx-only success). Each attempt logs a callback_logs row.
//
//   - Late-firing timers (post-Close) drop silently. See Close().
//
// Differences from the Python implementation:
//
//   - Python awaits each callback's retries serially before moving to the
//     next status. We schedule status updates by wall-clock and POST jobs
//     concurrently. The DB-side ordering of status updates and delivery
//     events still follows the timer order (matching Python). The order in
//     which the receiver observes HTTP callbacks may differ if a callback
//     hits transport errors and retries past the next status's scheduled time.
package callback

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/notfoundsam/sms-mock-server/app/clock"
	"github.com/notfoundsam/sms-mock-server/app/storage"
)

const (
	// jobBufferSize gives some headroom for back-pressure between schedulers and workers.
	// Beyond this, ScheduleSMS/CallStatusFlow's tick fn would block, which is fine
	// because ticks run on the clock's goroutines (or test harness).
	jobBufferSize = 256
)

// Config configures retry/delay behavior. Fields mirror config.Twilio.Callbacks.
type Config struct {
	Delay         time.Duration // delay between status transitions
	RetryAttempts int           // total attempts including the first; min 1
	RetryDelay    time.Duration // fixed delay between attempts
	Workers       int           // worker goroutine count; default 4
}

// HTTPDoer is the subset of *http.Client that Dispatcher uses. Tests inject
// a fake; production passes (&http.Client{Timeout: ...}).Do.
type HTTPDoer func(*http.Request) (*http.Response, error)

// Dispatcher schedules and delivers status callbacks.
type Dispatcher struct {
	logger     *slog.Logger
	store      storage.Store
	clk        clock.Clock
	httpDo     HTTPDoer
	cfg        Config
	accountSid string

	jobs chan job
	wg   sync.WaitGroup

	shuttingDown atomic.Bool
}

// New constructs a Dispatcher and starts cfg.Workers goroutines processing
// HTTP callback jobs. Caller must call Close to stop them cleanly.
func New(logger *slog.Logger, store storage.Store, clk clock.Clock, httpDo HTTPDoer, cfg Config, accountSid string) *Dispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	if cfg.RetryAttempts <= 0 {
		cfg.RetryAttempts = 1
	}

	d := &Dispatcher{
		logger:     logger,
		store:      store,
		clk:        clk,
		httpDo:     httpDo,
		cfg:        cfg,
		accountSid: accountSid,
		jobs:       make(chan job, jobBufferSize),
	}
	for range cfg.Workers {
		d.wg.Add(1)
		go d.workerLoop()
	}
	return d
}

// SMS status flow:
//   - WillSucceed: queued → sent → delivered  (two timer fires)
//   - !WillSucceed: queued → failed           (one timer fire)
//   - !IsKnown: nothing scheduled
func (d *Dispatcher) ScheduleSMSStatusFlow(sid, from, to, callbackURL string, isKnown, willSucceed bool) {
	if !isKnown {
		d.logger.Debug("SMS stay-queued: unknown destination", "sid", sid, "to", to)
		return
	}
	statuses := []string{"sent", "delivered"}
	if !willSucceed {
		statuses = []string{"failed"}
	}
	d.scheduleFlow(flowKindSMS, sid, from, to, callbackURL, statuses)
}

// Call status flow:
//   - WillSucceed: queued → ringing → in-progress → completed
//   - !WillSucceed: queued → failed
//   - !IsKnown: nothing scheduled
func (d *Dispatcher) ScheduleCallStatusFlow(sid, from, to, callbackURL string, isKnown, willSucceed bool) {
	if !isKnown {
		d.logger.Debug("Call stay-queued: unknown destination", "sid", sid, "to", to)
		return
	}
	statuses := []string{"ringing", "in-progress", "completed"}
	if !willSucceed {
		statuses = []string{"failed"}
	}
	d.scheduleFlow(flowKindCall, sid, from, to, callbackURL, statuses)
}

type flowKind int

const (
	flowKindSMS flowKind = iota
	flowKindCall
)

func (d *Dispatcher) scheduleFlow(kind flowKind, sid, from, to, callbackURL string, statuses []string) {
	// Go 1.22+ scopes loop variables per-iteration, so no manual capture needed.
	for i, status := range statuses {
		// Initial delay before status[0]; subsequent statuses one cfg.Delay apart.
		delay := time.Duration(i+1) * d.cfg.Delay
		d.clk.AfterFunc(delay, func() {
			d.fire(kind, sid, from, to, callbackURL, status)
		})
	}
}

// fire executes one status tick: update DB → write delivery event → enqueue HTTP job.
// Skips all writes if shutting down.
func (d *Dispatcher) fire(kind flowKind, sid, from, to, callbackURL, status string) {
	if d.shuttingDown.Load() {
		return
	}
	ctx := context.Background()

	switch kind {
	case flowKindSMS:
		if err := d.store.UpdateMessageStatus(ctx, sid, status); err != nil {
			d.logger.Warn("update message status failed", "sid", sid, "status", status, "error", err)
		}
		if err := d.store.SaveDeliveryEvent(ctx, &storage.DeliveryEvent{
			MessageSID: sid, EventType: "status_update", Status: status,
		}); err != nil {
			d.logger.Warn("save delivery event failed", "sid", sid, "error", err)
		}
	case flowKindCall:
		if err := d.store.UpdateCallStatus(ctx, sid, status); err != nil {
			d.logger.Warn("update call status failed", "sid", sid, "status", status, "error", err)
		}
		if err := d.store.SaveDeliveryEvent(ctx, &storage.DeliveryEvent{
			CallSID: sid, EventType: "status_update", Status: status,
		}); err != nil {
			d.logger.Warn("save delivery event failed", "sid", sid, "error", err)
		}
	}

	if callbackURL == "" {
		return
	}

	// Build the form-encoded payload here so each retry sends the same body.
	form := url.Values{}
	form.Set("AccountSid", d.accountSid)
	form.Set("From", from)
	form.Set("To", to)
	form.Set("ApiVersion", "2010-04-01")
	switch kind {
	case flowKindSMS:
		form.Set("MessageSid", sid)
		form.Set("MessageStatus", status)
	case flowKindCall:
		form.Set("CallSid", sid)
		form.Set("CallStatus", status)
		form.Set("Direction", "outbound-api")
	}

	j := job{
		url:    callbackURL,
		body:   form.Encode(),
		status: status,
	}
	if d.shuttingDown.Load() {
		return
	}
	// Best-effort enqueue. If channel is closed (shutdown raced us), recover.
	defer func() {
		if r := recover(); r != nil {
			d.logger.Debug("enqueue after close (dropping job)", "sid", sid, "status", status)
		}
	}()
	d.jobs <- j
}

// job is one HTTP POST request to deliver. Retries are handled inside the worker.
type job struct {
	url    string
	body   string
	status string // for logging only
}

// workerLoop consumes jobs and POSTs them with retries. Exits when jobs is closed.
func (d *Dispatcher) workerLoop() {
	defer d.wg.Done()
	for j := range d.jobs {
		d.deliver(j)
	}
}

// deliver POSTs the job's body, retrying with fixed delay on failure.
// Each attempt writes a callback_logs row. Returns no error — failures
// are logged only.
func (d *Dispatcher) deliver(j job) {
	ctx := context.Background()
	for attempt := 1; attempt <= d.cfg.RetryAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, j.url, strings.NewReader(j.body))
		if err != nil {
			d.logger.Error("build callback request failed", "url", j.url, "error", err)
			return
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err := d.httpDo(req) //nolint:bodyclose // closed by readAndCloseLimited below
		if err != nil {
			d.logCallbackAttempt(ctx, j, attempt, 0, "Error: "+err.Error())
			d.logger.Warn("callback transport error",
				"url", j.url, "status", j.status, "attempt", attempt, "error", err)
			if attempt < d.cfg.RetryAttempts && !d.shuttingDown.Load() {
				d.sleepBetweenAttempts()
			}
			continue
		}

		body := readAndCloseLimited(resp, 500)
		d.logCallbackAttempt(ctx, j, attempt, resp.StatusCode, body)

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			d.logger.Info("callback delivered",
				"url", j.url, "status", j.status, "attempt", attempt, "code", resp.StatusCode)
			return
		}

		d.logger.Warn("callback non-2xx",
			"url", j.url, "status", j.status, "attempt", attempt, "code", resp.StatusCode)
		if attempt < d.cfg.RetryAttempts && !d.shuttingDown.Load() {
			d.sleepBetweenAttempts()
		}
	}
	d.logger.Warn("callback exhausted retries", "url", j.url, "status", j.status, "attempts", d.cfg.RetryAttempts)
}

// sleepBetweenAttempts waits cfg.RetryDelay using the clock. Use AfterFunc
// instead of time.Sleep so the FakeClock can drive retries deterministically
// in tests.
func (d *Dispatcher) sleepBetweenAttempts() {
	if d.cfg.RetryDelay <= 0 {
		return
	}
	done := make(chan struct{})
	d.clk.AfterFunc(d.cfg.RetryDelay, func() { close(done) })
	<-done
}

func (d *Dispatcher) logCallbackAttempt(ctx context.Context, j job, attempt, statusCode int, respBody string) {
	// Persist the payload as JSON to match Python (which json.dumps()'s the dict).
	// We have form values; rebuild the dict shape for the log.
	payloadJSON, _ := encodePayloadAsJSON(j.body)
	if err := d.store.SaveCallbackLog(ctx, &storage.CallbackLog{
		TargetURL:     j.url,
		Payload:       payloadJSON,
		StatusCode:    statusCode,
		ResponseBody:  respBody,
		AttemptNumber: attempt,
	}); err != nil {
		d.logger.Warn("save callback log failed", "error", err)
	}
}

// encodePayloadAsJSON converts a url-encoded form body to a JSON object
// (single string per key, matching Python's behavior of json.dumps(dict)).
func encodePayloadAsJSON(formBody string) (string, error) {
	values, err := url.ParseQuery(formBody)
	if err != nil {
		return formBody, fmt.Errorf("parse form body: %w", err)
	}
	flat := make(map[string]string, len(values))
	for k, v := range values {
		if len(v) > 0 {
			flat[k] = v[0]
		}
	}
	b, err := json.Marshal(flat)
	if err != nil {
		return formBody, fmt.Errorf("marshal payload: %w", err)
	}
	return string(b), nil
}

func readAndCloseLimited(resp *http.Response, maxBytes int) string {
	if resp.Body == nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	s := buf.String()
	if len(s) > maxBytes {
		s = s[:maxBytes]
	}
	return s
}

// Close marks the dispatcher as shutting down, closes the job channel, and
// waits for in-flight workers to finish or for ctx to cancel.
//
// Late-firing timers (already scheduled by AfterFunc) check shuttingDown and
// drop silently — they will not write to the store after this call returns.
//
// After Close returns, ScheduleSMS/CallStatusFlow must not be called.
// (They would panic when sending on the closed channel; the recover() in fire
// turns that into a debug log, but better to not call them at all.)
func (d *Dispatcher) Close(ctx context.Context) error {
	if !d.shuttingDown.CompareAndSwap(false, true) {
		return nil // already closed
	}
	close(d.jobs)

	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("close: %w", ctx.Err())
	}
}
