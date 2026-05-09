// Package testutil provides shared fakes for unit tests across packages.
package testutil

import (
	"context"
	"sort"
	"strings"
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
	// tag links: messageID/callID → set of tag names. Stored as map[name]bool
	// so duplicates within a single record are de facto eliminated.
	messageTags map[int64]map[string]bool
	callTags    map[int64]map[string]bool
	closed      bool
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

func (s *FakeStore) ListMessages(ctx context.Context, limit, offset int) ([]storage.Message, int, error) {
	return s.SearchMessages(ctx, "", "", nil, limit, offset)
}

func (s *FakeStore) SearchMessages(_ context.Context, q, status string, tags []string, limit, offset int) ([]storage.Message, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// newest first; apply filter inline
	matched := make([]storage.Message, 0, len(s.messages))
	for i := len(s.messages) - 1; i >= 0; i-- {
		m := s.messages[i]
		if q != "" && !strings.Contains(m.From, q) && !strings.Contains(m.To, q) && !strings.Contains(m.Body, q) {
			continue
		}
		if status != "" && m.Status != status {
			continue
		}
		if !s.recordHasAllTags(s.messageTags, m.ID, tags) {
			continue
		}
		matched = append(matched, m)
	}
	total := len(matched)
	if offset >= total {
		return nil, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return matched[offset:end], total, nil
}

func (s *FakeStore) MarkMessageRead(_ context.Context, sid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.messages {
		if s.messages[i].SID == sid {
			s.messages[i].IsRead = true
			return nil
		}
	}
	return storage.ErrNotFound
}

func (s *FakeStore) DeleteMessage(_ context.Context, sid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.messages {
		if s.messages[i].SID != sid {
			continue
		}
		id := s.messages[i].ID
		s.messages = append(s.messages[:i], s.messages[i+1:]...)
		// Cascade-delete delivery events keyed to this message.
		kept := s.deliveryEvents[:0]
		for _, e := range s.deliveryEvents {
			if e.MessageSID != sid {
				kept = append(kept, e)
			}
		}
		s.deliveryEvents = kept
		// Cascade tag links.
		delete(s.messageTags, id)
		return nil
	}
	return storage.ErrNotFound
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

func (s *FakeStore) ListCalls(ctx context.Context, limit, offset int) ([]storage.Call, int, error) {
	return s.SearchCalls(ctx, "", "", nil, limit, offset)
}

func (s *FakeStore) SearchCalls(_ context.Context, q, status string, tags []string, limit, offset int) ([]storage.Call, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	matched := make([]storage.Call, 0, len(s.calls))
	for i := len(s.calls) - 1; i >= 0; i-- {
		c := s.calls[i]
		if q != "" && !strings.Contains(c.From, q) && !strings.Contains(c.To, q) {
			continue
		}
		if status != "" && c.Status != status {
			continue
		}
		if !s.recordHasAllTags(s.callTags, c.ID, tags) {
			continue
		}
		matched = append(matched, c)
	}
	total := len(matched)
	if offset >= total {
		return nil, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return matched[offset:end], total, nil
}

func (s *FakeStore) MarkCallRead(_ context.Context, sid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.calls {
		if s.calls[i].SID == sid {
			s.calls[i].IsRead = true
			return nil
		}
	}
	return storage.ErrNotFound
}

func (s *FakeStore) DeleteCall(_ context.Context, sid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.calls {
		if s.calls[i].SID != sid {
			continue
		}
		id := s.calls[i].ID
		s.calls = append(s.calls[:i], s.calls[i+1:]...)
		kept := s.deliveryEvents[:0]
		for _, e := range s.deliveryEvents {
			if e.CallSID != sid {
				kept = append(kept, e)
			}
		}
		s.deliveryEvents = kept
		delete(s.callTags, id)
		return nil
	}
	return storage.ErrNotFound
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

func (s *FakeStore) StatusCounts(_ context.Context, recordType string) (map[string]int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]int{}
	switch recordType {
	case "messages":
		for _, m := range s.messages {
			out[m.Status]++
		}
	case "calls":
		for _, c := range s.calls {
			out[c.Status]++
		}
	default:
		return nil, unknownTypeError{recordType}
	}
	return out, nil
}

func (s *FakeStore) CallbackSummary(_ context.Context, sid string) (storage.CallbackSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var sum storage.CallbackSummary
	msgKey := `"MessageSid":"` + sid + `"`
	callKey := `"CallSid":"` + sid + `"`
	var lastIdx = -1
	for i, l := range s.callbackLogs {
		if !strings.Contains(l.Payload, msgKey) && !strings.Contains(l.Payload, callKey) {
			continue
		}
		sum.Count++
		lastIdx = i // SaveCallbackLog appends, so last matching index is most recent
	}
	if lastIdx >= 0 {
		l := s.callbackLogs[lastIdx]
		sum.LastStatus = l.StatusCode
		sum.LastResponseBody = l.ResponseBody
	}
	return sum, nil
}

type unknownTypeError struct{ t string }

func (e unknownTypeError) Error() string { return "unknown record type: " + e.t }

// --- tags ---

func (s *FakeStore) SetMessageTags(_ context.Context, messageID int64, names []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.messageTags == nil {
		s.messageTags = map[int64]map[string]bool{}
	}
	set := make(map[string]bool, len(names))
	for _, n := range names {
		if n == "" {
			continue
		}
		set[n] = true
	}
	if len(set) == 0 {
		delete(s.messageTags, messageID)
		return nil
	}
	s.messageTags[messageID] = set
	return nil
}

func (s *FakeStore) SetCallTags(_ context.Context, callID int64, names []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.callTags == nil {
		s.callTags = map[int64]map[string]bool{}
	}
	set := make(map[string]bool, len(names))
	for _, n := range names {
		if n == "" {
			continue
		}
		set[n] = true
	}
	if len(set) == 0 {
		delete(s.callTags, callID)
		return nil
	}
	s.callTags[callID] = set
	return nil
}

func (s *FakeStore) MessageTags(_ context.Context, messageID int64) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return setToSortedSlice(s.messageTags[messageID]), nil
}

func (s *FakeStore) CallTags(_ context.Context, callID int64) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return setToSortedSlice(s.callTags[callID]), nil
}

func (s *FakeStore) ListTagNames(_ context.Context, recordType string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var source map[int64]map[string]bool
	switch recordType {
	case "messages":
		source = s.messageTags
	case "calls":
		source = s.callTags
	default:
		return nil, unknownTypeError{recordType}
	}
	all := map[string]bool{}
	for _, set := range source {
		for n := range set {
			all[n] = true
		}
	}
	return setToSortedSlice(all), nil
}

func setToSortedSlice(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// recordHasAllTags checks that the record (by id) carries every name in tags.
// Empty tags slice always returns true.
func (s *FakeStore) recordHasAllTags(store map[int64]map[string]bool, id int64, tags []string) bool {
	if len(tags) == 0 {
		return true
	}
	set := store[id]
	for _, t := range tags {
		if !set[t] {
			return false
		}
	}
	return true
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
	s.messageTags = nil
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
	s.callTags = nil
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
