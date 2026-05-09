package testutil

import (
	"context"
	"testing"

	"github.com/notfoundsam/sms-mock-server/app/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFakeStore_MessagesRoundtripAndPagination(t *testing.T) {
	ctx := context.Background()
	s := NewFakeStore()

	for i := 0; i < 5; i++ {
		m := &storage.Message{
			SID: "SM" + string(rune('0'+i)), Provider: "twilio",
			From: "+1", To: "+2", Status: "queued",
		}
		require.NoError(t, s.SaveMessage(ctx, m), "SaveMessage")
		assert.NotEqual(t, int64(0), m.ID, "ID not assigned for %s", m.SID)
	}

	got, err := s.GetMessage(ctx, "SM2")
	require.NoError(t, err)
	assert.Equal(t, "SM2", got.SID)

	rows, total, err := s.ListMessages(ctx, 2, 0)
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Len(t, rows, 2)
	// newest-first
	assert.Equal(t, "SM4", rows[0].SID)
}

func TestFakeStore_ErrNotFound(t *testing.T) {
	ctx := context.Background()
	s := NewFakeStore()

	_, err := s.GetMessage(ctx, "missing")
	require.ErrorIs(t, err, storage.ErrNotFound)

	err = s.UpdateMessageStatus(ctx, "missing", "x")
	require.ErrorIs(t, err, storage.ErrNotFound)

	_, err = s.GetCallbackLog(ctx, 99)
	require.ErrorIs(t, err, storage.ErrNotFound)
}

func TestFakeStore_DuplicateSID(t *testing.T) {
	ctx := context.Background()
	s := NewFakeStore()
	_ = s.SaveMessage(ctx, &storage.Message{SID: "SM1", Provider: "twilio", From: "+1", To: "+2", Status: "queued"})
	err := s.SaveMessage(ctx, &storage.Message{SID: "SM1", Provider: "twilio", From: "+1", To: "+2", Status: "queued"})
	assert.Error(t, err)
}

func TestFakeStore_ClearMessagesPreservesCallEvents(t *testing.T) {
	ctx := context.Background()
	s := NewFakeStore()

	_ = s.SaveMessage(ctx, &storage.Message{SID: "SM1", Provider: "twilio", From: "+1", To: "+2", Status: "queued"})
	_ = s.SaveCall(ctx, &storage.Call{SID: "CA1", Provider: "twilio", From: "+1", To: "+2", Status: "queued"})
	_ = s.SaveDeliveryEvent(ctx, &storage.DeliveryEvent{MessageSID: "SM1", EventType: "status_update", Status: "sent"})
	_ = s.SaveDeliveryEvent(ctx, &storage.DeliveryEvent{CallSID: "CA1", EventType: "status_update", Status: "ringing"})

	_, err := s.ClearMessages(ctx)
	require.NoError(t, err, "ClearMessages")

	events := s.DeliveryEvents()
	require.Len(t, events, 1)
	assert.Equal(t, "CA1", events[0].CallSID)
}

func TestFakeStore_StatsAndClearAll(t *testing.T) {
	ctx := context.Background()
	s := NewFakeStore()

	_ = s.SaveMessage(ctx, &storage.Message{SID: "SM1", Provider: "twilio", From: "+1", To: "+2", Status: "queued"})
	_ = s.SaveCall(ctx, &storage.Call{SID: "CA1", Provider: "twilio", From: "+1", To: "+2", Status: "queued"})
	_ = s.SaveCallbackLog(ctx, &storage.CallbackLog{TargetURL: "x", Payload: "y"})

	st, _ := s.Stats(ctx)
	assert.Equal(t, 1, st.Messages)
	assert.Equal(t, 1, st.Calls)
	assert.Equal(t, 1, st.Callbacks)

	c, err := s.ClearAll(ctx)
	require.NoError(t, err, "ClearAll")
	assert.Equal(t, 1, c.Messages)
	assert.Equal(t, 1, c.Calls)
	assert.Equal(t, 1, c.Callbacks)

	st, _ = s.Stats(ctx)
	assert.Equal(t, storage.Stats{}, st)
}

func TestFakeStore_CloseTracksState(t *testing.T) {
	s := NewFakeStore()
	assert.False(t, s.Closed())
	_ = s.Close()
	assert.True(t, s.Closed())
}

// FakeStore must satisfy storage.Store so handlers can accept either implementation.
func TestFakeStore_ImplementsInterface(t *testing.T) {
	var _ storage.Store = (*FakeStore)(nil)
}

func TestFakeStore_SearchAndFilters(t *testing.T) {
	ctx := context.Background()
	s := NewFakeStore()
	_ = s.SaveMessage(ctx, &storage.Message{SID: "SM1", From: "+15550001", To: "+14441111", Body: "hello", Status: "queued", Provider: "twilio"})
	_ = s.SaveMessage(ctx, &storage.Message{SID: "SM2", From: "+15550002", To: "+14441111", Body: "goodbye", Status: "delivered", Provider: "twilio"})
	_ = s.SaveMessage(ctx, &storage.Message{SID: "SM3", From: "+15550001", To: "+14442222", Body: "hello again", Status: "delivered", Provider: "twilio"})

	_, total, err := s.SearchMessages(ctx, "hello", "", nil, 50, 0)
	require.NoError(t, err)
	assert.Equal(t, 2, total)

	_, total, err = s.SearchMessages(ctx, "", "delivered", nil, 50, 0)
	require.NoError(t, err)
	assert.Equal(t, 2, total)

	rows, total, err := s.SearchMessages(ctx, "hello", "delivered", nil, 50, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Equal(t, "SM3", rows[0].SID)
}

func TestFakeStore_MarkReadAndDelete(t *testing.T) {
	ctx := context.Background()
	s := NewFakeStore()
	_ = s.SaveMessage(ctx, &storage.Message{SID: "SM1", From: "+1", To: "+2", Status: "queued", Provider: "twilio"})
	_ = s.SaveDeliveryEvent(ctx, &storage.DeliveryEvent{MessageSID: "SM1", EventType: "status", Status: "delivered"})

	require.NoError(t, s.MarkMessageRead(ctx, "SM1"))
	got, _ := s.GetMessage(ctx, "SM1")
	assert.True(t, got.IsRead)

	// Idempotent
	require.NoError(t, s.MarkMessageRead(ctx, "SM1"))
	require.ErrorIs(t, s.MarkMessageRead(ctx, "SMmissing"), storage.ErrNotFound)

	require.NoError(t, s.DeleteMessage(ctx, "SM1"))
	_, err := s.GetMessage(ctx, "SM1")
	require.ErrorIs(t, err, storage.ErrNotFound)
	assert.Empty(t, s.DeliveryEvents(), "delivery events cascaded with delete")
	require.ErrorIs(t, s.DeleteMessage(ctx, "SMmissing"), storage.ErrNotFound)
}

func TestFakeStore_StatusCounts(t *testing.T) {
	ctx := context.Background()
	s := NewFakeStore()
	_ = s.SaveMessage(ctx, &storage.Message{SID: "SM1", From: "+1", To: "+2", Status: "queued", Provider: "twilio"})
	_ = s.SaveMessage(ctx, &storage.Message{SID: "SM2", From: "+1", To: "+2", Status: "delivered", Provider: "twilio"})
	_ = s.SaveMessage(ctx, &storage.Message{SID: "SM3", From: "+1", To: "+2", Status: "delivered", Provider: "twilio"})

	counts, err := s.StatusCounts(ctx, "messages")
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"queued": 1, "delivered": 2}, counts)

	_, err = s.StatusCounts(ctx, "garbage")
	assert.Error(t, err)
}

func TestFakeStore_CallbackSummary(t *testing.T) {
	ctx := context.Background()
	s := NewFakeStore()
	_ = s.SaveCallbackLog(ctx, &storage.CallbackLog{TargetURL: "x", Payload: `{"MessageSid":"SM1","MessageStatus":"sent"}`, StatusCode: 202, ResponseBody: "ok"})
	_ = s.SaveCallbackLog(ctx, &storage.CallbackLog{TargetURL: "x", Payload: `{"MessageSid":"SM1","MessageStatus":"delivered"}`, StatusCode: 200, ResponseBody: "ok"})
	_ = s.SaveCallbackLog(ctx, &storage.CallbackLog{TargetURL: "y", Payload: `{"MessageSid":"SM2"}`, StatusCode: 500})

	sum, err := s.CallbackSummary(ctx, "SM1")
	require.NoError(t, err)
	assert.Equal(t, 2, sum.Count)
	assert.Equal(t, 200, sum.LastStatus, "most recent matching log wins")

	sum, err = s.CallbackSummary(ctx, "SMmissing")
	require.NoError(t, err)
	assert.Equal(t, 0, sum.Count)
}

func TestFakeStore_DeleteCall(t *testing.T) {
	ctx := context.Background()
	s := NewFakeStore()
	_ = s.SaveCall(ctx, &storage.Call{SID: "CA1", From: "+1", To: "+2", Status: "queued", Provider: "twilio"})
	_ = s.SaveDeliveryEvent(ctx, &storage.DeliveryEvent{CallSID: "CA1", EventType: "ringing", Status: "ringing"})

	require.NoError(t, s.DeleteCall(ctx, "CA1"))
	_, err := s.GetCall(ctx, "CA1")
	require.ErrorIs(t, err, storage.ErrNotFound)
	assert.Empty(t, s.DeliveryEvents())

	require.ErrorIs(t, s.MarkCallRead(ctx, "CAmissing"), storage.ErrNotFound)
	require.ErrorIs(t, s.DeleteCall(ctx, "CAmissing"), storage.ErrNotFound)
}

func TestFakeStore_TagsRoundtrip(t *testing.T) {
	ctx := context.Background()
	s := NewFakeStore()
	_ = s.SaveMessage(ctx, &storage.Message{SID: "SM1", From: "+1", To: "+2", Status: "queued", Provider: "twilio"})
	m, _ := s.GetMessage(ctx, "SM1")

	require.NoError(t, s.SetMessageTags(ctx, m.ID, []string{"verification", "auth"}))
	got, err := s.MessageTags(ctx, m.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"auth", "verification"}, got, "sorted asc")

	// Replace
	require.NoError(t, s.SetMessageTags(ctx, m.ID, []string{"x"}))
	got, _ = s.MessageTags(ctx, m.ID)
	assert.Equal(t, []string{"x"}, got)

	// Empty clears
	require.NoError(t, s.SetMessageTags(ctx, m.ID, nil))
	got, _ = s.MessageTags(ctx, m.ID)
	assert.Empty(t, got)
}

func TestFakeStore_SearchByTag(t *testing.T) {
	ctx := context.Background()
	s := NewFakeStore()
	_ = s.SaveMessage(ctx, &storage.Message{SID: "SM1", From: "+1", To: "+2", Status: "queued", Provider: "twilio"})
	_ = s.SaveMessage(ctx, &storage.Message{SID: "SM2", From: "+1", To: "+2", Status: "queued", Provider: "twilio"})
	m1, _ := s.GetMessage(ctx, "SM1")
	m2, _ := s.GetMessage(ctx, "SM2")
	_ = s.SetMessageTags(ctx, m1.ID, []string{"a", "b"})
	_ = s.SetMessageTags(ctx, m2.ID, []string{"a"})

	_, total, err := s.SearchMessages(ctx, "", "", []string{"a"}, 50, 0)
	require.NoError(t, err)
	assert.Equal(t, 2, total)

	rows, total, err := s.SearchMessages(ctx, "", "", []string{"a", "b"}, 50, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Equal(t, "SM1", rows[0].SID)
}

func TestFakeStore_ListTagNames_PerType(t *testing.T) {
	ctx := context.Background()
	s := NewFakeStore()
	_ = s.SaveMessage(ctx, &storage.Message{SID: "SM1", From: "+1", To: "+2", Status: "queued", Provider: "twilio"})
	_ = s.SaveCall(ctx, &storage.Call{SID: "CA1", From: "+1", To: "+2", Status: "queued", Provider: "twilio"})
	m, _ := s.GetMessage(ctx, "SM1")
	c, _ := s.GetCall(ctx, "CA1")

	msgNames, err := s.ListTagNames(ctx, "messages")
	require.NoError(t, err)
	assert.Empty(t, msgNames)

	_ = s.SetMessageTags(ctx, m.ID, []string{"verification"})
	_ = s.SetCallTags(ctx, c.ID, []string{"support"})

	msgNames, _ = s.ListTagNames(ctx, "messages")
	assert.Equal(t, []string{"verification"}, msgNames, "messages list excludes call-only tags")
	callNames, _ := s.ListTagNames(ctx, "calls")
	assert.Equal(t, []string{"support"}, callNames, "calls list excludes message-only tags")

	_, err = s.ListTagNames(ctx, "garbage")
	assert.Error(t, err)
}
