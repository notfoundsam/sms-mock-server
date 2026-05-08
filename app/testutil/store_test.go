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
