package storage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	s, err := New(context.Background(), ":memory:")
	require.NoError(t, err, "New")
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMessages_CRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	m := &Message{
		SID: "SM1", Provider: "twilio",
		From: "+15550000001", To: "+15551234567",
		Body: "hello", Status: "queued",
		CallbackURL: "http://example/cb",
	}
	require.NoError(t, s.SaveMessage(ctx, m), "SaveMessage")
	assert.NotZero(t, m.ID, "expected ID to be assigned")

	got, err := s.GetMessage(ctx, "SM1")
	require.NoError(t, err, "GetMessage")
	assert.Equal(t, "SM1", got.SID)
	assert.Equal(t, "hello", got.Body)
	assert.Equal(t, "http://example/cb", got.CallbackURL)
	assert.Equal(t, "queued", got.Status)

	require.NoError(t, s.UpdateMessageStatus(ctx, "SM1", "delivered"), "UpdateMessageStatus")
	got, _ = s.GetMessage(ctx, "SM1")
	assert.Equal(t, "delivered", got.Status)
}

func TestMessages_GetMissingReturnsErrNotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	_, err := s.GetMessage(ctx, "SMmissing")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestMessages_UpdateMissingReturnsErrNotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	err := s.UpdateMessageStatus(ctx, "SMmissing", "sent")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestMessages_ListPagination(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	for i := 0; i < 7; i++ {
		m := &Message{
			SID: sid("SM", i), Provider: "twilio",
			From: "+15550000001", To: "+15551234567",
			Body: "x", Status: "queued",
		}
		require.NoError(t, s.SaveMessage(ctx, m), "SaveMessage")
	}

	rows, total, err := s.ListMessages(ctx, 3, 0)
	require.NoError(t, err, "ListMessages")
	assert.Equal(t, 7, total)
	assert.Len(t, rows, 3)

	rows, total, err = s.ListMessages(ctx, 3, 6)
	require.NoError(t, err, "ListMessages page 3")
	assert.Equal(t, 7, total)
	assert.Len(t, rows, 1)

	// Newest-first ordering: most recently inserted should appear in the first page.
	rows, _, _ = s.ListMessages(ctx, 1, 0)
	assert.Equal(t, "SM6", rows[0].SID, "first row should be newest")
}

func TestCalls_CRUDAndPagination(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	c := &Call{
		SID: "CA1", Provider: "twilio",
		From: "+15550000001", To: "+15551234567",
		Status: "queued", TwiMLURL: "http://example/twiml",
	}
	require.NoError(t, s.SaveCall(ctx, c), "SaveCall")

	got, err := s.GetCall(ctx, "CA1")
	require.NoError(t, err)
	assert.Equal(t, "http://example/twiml", got.TwiMLURL)

	require.NoError(t, s.UpdateCallStatus(ctx, "CA1", "completed"), "UpdateCallStatus")
	got, _ = s.GetCall(ctx, "CA1")
	assert.Equal(t, "completed", got.Status)

	err = s.UpdateCallStatus(ctx, "CAmissing", "x")
	require.ErrorIs(t, err, ErrNotFound)

	rows, total, err := s.ListCalls(ctx, 10, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, rows, 1)
}

func TestCallbackLogs_CRUDAndPagination(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	for i := 0; i < 3; i++ {
		l := &CallbackLog{
			TargetURL: "http://cb",
			Payload:   "MessageStatus=delivered",
			// StatusCode 0 simulates "no response" (e.g. transport error)
			AttemptNumber: i + 1,
		}
		require.NoError(t, s.SaveCallbackLog(ctx, l), "SaveCallbackLog")
	}

	rows, total, err := s.ListCallbackLogs(ctx, 10, 0)
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	assert.Len(t, rows, 3)

	got, err := s.GetCallbackLog(ctx, rows[0].ID)
	require.NoError(t, err, "GetCallbackLog")
	assert.Equal(t, "http://cb", got.TargetURL)

	_, err = s.GetCallbackLog(ctx, 99999)
	require.ErrorIs(t, err, ErrNotFound)

	// Status code roundtrip: explicit 200 should persist
	l := &CallbackLog{TargetURL: "http://cb", Payload: "x", StatusCode: 200}
	require.NoError(t, s.SaveCallbackLog(ctx, l), "SaveCallbackLog")
	got, _ = s.GetCallbackLog(ctx, l.ID)
	assert.Equal(t, 200, got.StatusCode)
}

func TestStats(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	st, err := s.Stats(ctx)
	require.NoError(t, err, "Stats")
	assert.Equal(t, Stats{}, st, "empty Stats should be zero")

	require.NoError(t, s.SaveMessage(ctx, &Message{SID: "SM1", Provider: "twilio", From: "+1", To: "+2", Status: "queued"}))
	require.NoError(t, s.SaveCall(ctx, &Call{SID: "CA1", Provider: "twilio", From: "+1", To: "+2", Status: "queued"}))
	require.NoError(t, s.SaveCallbackLog(ctx, &CallbackLog{TargetURL: "x", Payload: "y", AttemptNumber: 1}))

	st, _ = s.Stats(ctx)
	assert.Equal(t, 1, st.Messages)
	assert.Equal(t, 1, st.Calls)
	assert.Equal(t, 1, st.Callbacks)
}

// ClearMessages must remove message-keyed delivery events (matches Python's explicit DELETE).
// Call-keyed delivery events must survive.
func TestClearMessages_RemovesMessageDeliveryEventsOnly(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	_ = s.SaveMessage(ctx, &Message{SID: "SM1", Provider: "twilio", From: "+1", To: "+2", Status: "queued"})
	_ = s.SaveCall(ctx, &Call{SID: "CA1", Provider: "twilio", From: "+1", To: "+2", Status: "queued"})
	_ = s.SaveDeliveryEvent(ctx, &DeliveryEvent{MessageSID: "SM1", EventType: "status_update", Status: "sent"})
	_ = s.SaveDeliveryEvent(ctx, &DeliveryEvent{CallSID: "CA1", EventType: "status_update", Status: "ringing"})

	n, err := s.ClearMessages(ctx)
	require.NoError(t, err, "ClearMessages")
	assert.Equal(t, 1, n)

	// Message-keyed events gone, call-keyed events survive. Probe via a count query
	// using direct SQL (test-only) — wrap by checking that the call-keyed event survived
	// through subsequent ClearCalls behavior.
	st, _ := s.Stats(ctx)
	assert.Equal(t, 0, st.Messages, "messages remaining should be 0")

	// ClearCalls also removes call-keyed delivery events. If the call-keyed event from
	// before was wrongly removed by ClearMessages, ClearCalls would still succeed — but
	// the easiest direct check is to count delivery_events via raw SQL using the
	// concrete sqliteStore.
	if ss, ok := s.(*sqliteStore); ok {
		var msgEvents, callEvents int
		_ = ss.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM delivery_events WHERE message_sid IS NOT NULL`).Scan(&msgEvents)
		_ = ss.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM delivery_events WHERE call_sid IS NOT NULL`).Scan(&callEvents)
		assert.Equal(t, 0, msgEvents, "message-keyed delivery_events")
		assert.Equal(t, 1, callEvents, "call-keyed delivery_events must survive ClearMessages")
	}
}

func TestClearCalls_RemovesCallDeliveryEventsOnly(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	_ = s.SaveMessage(ctx, &Message{SID: "SM1", Provider: "twilio", From: "+1", To: "+2", Status: "queued"})
	_ = s.SaveCall(ctx, &Call{SID: "CA1", Provider: "twilio", From: "+1", To: "+2", Status: "queued"})
	_ = s.SaveDeliveryEvent(ctx, &DeliveryEvent{MessageSID: "SM1", EventType: "status_update", Status: "sent"})
	_ = s.SaveDeliveryEvent(ctx, &DeliveryEvent{CallSID: "CA1", EventType: "status_update", Status: "ringing"})

	n, err := s.ClearCalls(ctx)
	require.NoError(t, err, "ClearCalls")
	assert.Equal(t, 1, n)

	if ss, ok := s.(*sqliteStore); ok {
		var msgEvents, callEvents int
		_ = ss.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM delivery_events WHERE message_sid IS NOT NULL`).Scan(&msgEvents)
		_ = ss.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM delivery_events WHERE call_sid IS NOT NULL`).Scan(&callEvents)
		assert.Equal(t, 0, callEvents, "call-keyed delivery_events")
		assert.Equal(t, 1, msgEvents, "message-keyed delivery_events must survive ClearCalls")
	}
}

func TestClearCallbacks(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_ = s.SaveCallbackLog(ctx, &CallbackLog{TargetURL: "x", Payload: "y", AttemptNumber: 1})
	_ = s.SaveCallbackLog(ctx, &CallbackLog{TargetURL: "x", Payload: "y", AttemptNumber: 2})

	n, err := s.ClearCallbacks(ctx)
	require.NoError(t, err, "ClearCallbacks")
	assert.Equal(t, 2, n)
	st, _ := s.Stats(ctx)
	assert.Equal(t, 0, st.Callbacks, "callbacks remaining")
}

func TestClearAll(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	_ = s.SaveMessage(ctx, &Message{SID: "SM1", Provider: "twilio", From: "+1", To: "+2", Status: "queued"})
	_ = s.SaveCall(ctx, &Call{SID: "CA1", Provider: "twilio", From: "+1", To: "+2", Status: "queued"})
	_ = s.SaveCallbackLog(ctx, &CallbackLog{TargetURL: "x", Payload: "y", AttemptNumber: 1})

	c, err := s.ClearAll(ctx)
	require.NoError(t, err, "ClearAll")
	assert.Equal(t, 1, c.Messages)
	assert.Equal(t, 1, c.Calls)
	assert.Equal(t, 1, c.Callbacks)

	st, _ := s.Stats(ctx)
	assert.Equal(t, Stats{}, st, "Stats after ClearAll should be zero")
}

func TestUniqueSIDConstraint(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	_ = s.SaveMessage(ctx, &Message{SID: "SM1", Provider: "twilio", From: "+1", To: "+2", Status: "queued"})
	err := s.SaveMessage(ctx, &Message{SID: "SM1", Provider: "twilio", From: "+1", To: "+2", Status: "queued"})
	require.Error(t, err, "expected error inserting duplicate SID")
}

// sid generates a deterministic SID with a small ordinal so tests can assert ordering.
func sid(prefix string, i int) string {
	return prefix + string(rune('0'+i))
}
