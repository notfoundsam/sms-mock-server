// Package testutil provides shared fakes for unit tests across packages.
package testutil

import (
	"context"
	"sort"
	"sync"

	"github.com/notfoundsam/sms-mock-server/app/storage"
)

// FakeStore is an in-memory storage.Store for unit tests. It does not aim
// to perfectly mimic SQLite semantics — it preserves insertion order via
// monotonic IDs and supports the operations the dispatcher and handlers use.
type FakeStore struct {
	mu             sync.Mutex
	nextMessageID  int64
	nextCallID     int64
	nextEventID    int64
	nextCallbackID int64
	messages       []storage.Message
	calls          []storage.Call
	deliveryEvents []storage.DeliveryEvent
	callbackLogs   []storage.CallbackLog
	closed         bool
}

func NewFakeStore() *FakeStore { return &FakeStore{} }

func (s *FakeStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

// Closed reports whether Close has been called. Useful in tests verifying
// late-firing-timer safety in the dispatcher.
func (s *FakeStore) Closed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// --- messages ---

func (s *FakeStore) SaveMessage(_ context.Context, m *storage.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.messages {
		if existing.SID == m.SID {
			return errDuplicate("message_sid")
		}
	}
	s.nextMessageID++
	m.ID = s.nextMessageID
	s.messages = append(s.messages, *m)
	return nil
}

func (s *FakeStore) GetMessage(_ context.Context, sid string) (*storage.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.messages {
		if s.messages[i].SID == sid {
			cp := s.messages[i]
			return &cp, nil
		}
	}
	return nil, storage.ErrNotFound
}

func (s *FakeStore) UpdateMessageStatus(_ context.Context, sid, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.messages {
		if s.messages[i].SID == sid {
			s.messages[i].Status = status
			return nil
		}
	}
	return storage.ErrNotFound
}

func (s *FakeStore) ListMessages(_ context.Context, limit, offset int) ([]storage.Message, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := len(s.messages)
	// newest first: reverse insertion order
	out := make([]storage.Message, 0, total)
	for i := len(s.messages) - 1; i >= 0; i-- {
		out = append(out, s.messages[i])
	}
	if offset >= len(out) {
		return nil, total, nil
	}
	end := offset + limit
	if end > len(out) {
		end = len(out)
	}
	return out[offset:end], total, nil
}

// --- calls ---

func (s *FakeStore) SaveCall(_ context.Context, c *storage.Call) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.calls {
		if existing.SID == c.SID {
			return errDuplicate("call_sid")
		}
	}
	s.nextCallID++
	c.ID = s.nextCallID
	s.calls = append(s.calls, *c)
	return nil
}

func (s *FakeStore) GetCall(_ context.Context, sid string) (*storage.Call, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.calls {
		if s.calls[i].SID == sid {
			cp := s.calls[i]
			return &cp, nil
		}
	}
	return nil, storage.ErrNotFound
}

func (s *FakeStore) UpdateCallStatus(_ context.Context, sid, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.calls {
		if s.calls[i].SID == sid {
			s.calls[i].Status = status
			return nil
		}
	}
	return storage.ErrNotFound
}

func (s *FakeStore) ListCalls(_ context.Context, limit, offset int) ([]storage.Call, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := len(s.calls)
	out := make([]storage.Call, 0, total)
	for i := len(s.calls) - 1; i >= 0; i-- {
		out = append(out, s.calls[i])
	}
	if offset >= len(out) {
		return nil, total, nil
	}
	end := offset + limit
	if end > len(out) {
		end = len(out)
	}
	return out[offset:end], total, nil
}

// --- delivery events ---

func (s *FakeStore) SaveDeliveryEvent(_ context.Context, e *storage.DeliveryEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextEventID++
	e.ID = s.nextEventID
	s.deliveryEvents = append(s.deliveryEvents, *e)
	return nil
}

// DeliveryEvents returns a copy of all stored events for test assertions.
func (s *FakeStore) DeliveryEvents() []storage.DeliveryEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]storage.DeliveryEvent, len(s.deliveryEvents))
	copy(out, s.deliveryEvents)
	return out
}

// --- callback logs ---

func (s *FakeStore) SaveCallbackLog(_ context.Context, l *storage.CallbackLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l.AttemptNumber == 0 {
		l.AttemptNumber = 1
	}
	s.nextCallbackID++
	l.ID = s.nextCallbackID
	s.callbackLogs = append(s.callbackLogs, *l)
	return nil
}

func (s *FakeStore) GetCallbackLog(_ context.Context, id int64) (*storage.CallbackLog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.callbackLogs {
		if s.callbackLogs[i].ID == id {
			cp := s.callbackLogs[i]
			return &cp, nil
		}
	}
	return nil, storage.ErrNotFound
}

func (s *FakeStore) ListCallbackLogs(_ context.Context, limit, offset int) ([]storage.CallbackLog, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := len(s.callbackLogs)
	// newest first
	out := make([]storage.CallbackLog, 0, total)
	for i := len(s.callbackLogs) - 1; i >= 0; i-- {
		out = append(out, s.callbackLogs[i])
	}
	if offset >= len(out) {
		return nil, total, nil
	}
	end := offset + limit
	if end > len(out) {
		end = len(out)
	}
	return out[offset:end], total, nil
}

// CallbackLogsByURL returns all logs targeting the given URL, in insertion
// order. Useful for verifying retry sequences in dispatcher tests.
func (s *FakeStore) CallbackLogsByURL(url string) []storage.CallbackLog {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []storage.CallbackLog
	for _, l := range s.callbackLogs {
		if l.TargetURL == url {
			out = append(out, l)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// --- stats / clear ---

func (s *FakeStore) Stats(_ context.Context) (storage.Stats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return storage.Stats{
		Messages:  len(s.messages),
		Calls:     len(s.calls),
		Callbacks: len(s.callbackLogs),
	}, nil
}

func (s *FakeStore) ClearMessages(_ context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.messages)
	s.messages = nil
	// remove message-keyed delivery events; keep call-keyed
	kept := s.deliveryEvents[:0]
	for _, e := range s.deliveryEvents {
		if e.MessageSID == "" {
			kept = append(kept, e)
		}
	}
	s.deliveryEvents = kept
	return n, nil
}

func (s *FakeStore) ClearCalls(_ context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.calls)
	s.calls = nil
	kept := s.deliveryEvents[:0]
	for _, e := range s.deliveryEvents {
		if e.CallSID == "" {
			kept = append(kept, e)
		}
	}
	s.deliveryEvents = kept
	return n, nil
}

func (s *FakeStore) ClearCallbacks(_ context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.callbackLogs)
	s.callbackLogs = nil
	return n, nil
}

func (s *FakeStore) ClearAll(ctx context.Context) (storage.ClearCounts, error) {
	m, err := s.ClearMessages(ctx)
	if err != nil {
		return storage.ClearCounts{}, err
	}
	c, err := s.ClearCalls(ctx)
	if err != nil {
		return storage.ClearCounts{}, err
	}
	cb, err := s.ClearCallbacks(ctx)
	if err != nil {
		return storage.ClearCounts{}, err
	}
	return storage.ClearCounts{Messages: m, Calls: c, Callbacks: cb}, nil
}

// errDuplicate models the SQLite uniqueness violation without dragging in
// the driver's concrete error type. Tests that care about this case can
// match on its message; most don't.
type duplicateError struct{ field string }

func (e duplicateError) Error() string { return "duplicate " + e.field }

func errDuplicate(field string) error { return duplicateError{field: field} }
