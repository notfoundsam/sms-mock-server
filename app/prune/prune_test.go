package prune

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/notfoundsam/sms-mock-server/app/config"
	"github.com/notfoundsam/sms-mock-server/app/storage"
	"github.com/notfoundsam/sms-mock-server/app/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeClock struct{ now time.Time }

func (f *fakeClock) Now() time.Time                      { return f.now }
func (f *fakeClock) AfterFunc(_ time.Duration, _ func()) {}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// saveMessage saves a message with an explicit CreatedAt. The FakeStore
// preserves whatever timestamp the caller supplies (unlike the SQLite impl,
// which uses CURRENT_TIMESTAMP), so this is sufficient for ordering tests.
func saveMessage(t *testing.T, s *testutil.FakeStore, sid string, createdAt time.Time) {
	t.Helper()
	require.NoError(t, s.SaveMessage(context.Background(), &storage.Message{
		SID: sid, Provider: "twilio",
		From: "+1", To: "+1", Body: "x", Status: "queued",
		CreatedAt: createdAt,
	}))
}

func TestNew_ReturnsNilWhenAllLimitsDisabled(t *testing.T) {
	p := New(testutil.NewFakeStore(), discardLogger(), config.Limits{}, &fakeClock{})
	assert.Nil(t, p, "all-zero limits should disable the pruner entirely")
}

func TestNew_ReturnsPrunerWhenAnyLimitSet(t *testing.T) {
	cases := []config.Limits{
		{MaxMessages: 100},
		{MaxCalls: 100},
		{MaxAge: time.Hour},
	}
	for _, lim := range cases {
		p := New(testutil.NewFakeStore(), discardLogger(), lim, &fakeClock{})
		assert.NotNil(t, p, "limits %+v should produce a pruner", lim)
	}
}

func TestPruneOnce_CapEvictsOldest(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()

	base := time.Now().Add(-time.Hour)
	for i := 0; i < 5; i++ {
		saveMessage(t, store, "SM"+string(rune('A'+i)), base.Add(time.Duration(i)*time.Minute))
	}

	p := New(store, discardLogger(), config.Limits{MaxMessages: 2}, &fakeClock{now: time.Now()})
	require.NotNil(t, p)

	p.PruneOnce(ctx)

	_, total, _ := store.ListMessages(ctx, 100, 0)
	assert.Equal(t, 2, total, "should keep 2 newest")
	// Oldest three (SMA..SMC) should be gone, newest two (SMD, SME) kept.
	for _, sid := range []string{"SMA", "SMB", "SMC"} {
		_, err := store.GetMessage(ctx, sid)
		require.ErrorIs(t, err, storage.ErrNotFound, "expected %s deleted", sid)
	}
	for _, sid := range []string{"SMD", "SME"} {
		_, err := store.GetMessage(ctx, sid)
		assert.NoError(t, err, "expected %s kept", sid)
	}
}

// FakeStore.PruneMessagesByCount must not reorder s.messages — ListMessages
// and SearchMessages assume insertion order. Regression test for a bug where
// the prune sorted in place.
func TestPruneOnce_PreservesListInsertionOrder(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()

	base := time.Now().Add(-time.Hour)
	for i := 0; i < 5; i++ {
		saveMessage(t, store, "SM"+string(rune('A'+i)), base.Add(time.Duration(i)*time.Minute))
	}

	p := New(store, discardLogger(), config.Limits{MaxMessages: 3}, &fakeClock{now: time.Now()})
	require.NotNil(t, p)
	p.PruneOnce(ctx)

	rows, total, err := store.ListMessages(ctx, 100, 0)
	require.NoError(t, err)
	require.Equal(t, 3, total, "should keep 3 newest")

	// ListMessages returns newest-first by SearchMessages convention; the
	// surviving SIDs should be the 3 newest in newest-first order, i.e.
	// SME, SMD, SMC. If the underlying slice was reordered, the iteration
	// order would be wrong.
	gotSIDs := make([]string, len(rows))
	for i, r := range rows {
		gotSIDs[i] = r.SID
	}
	assert.Equal(t, []string{"SME", "SMD", "SMC"}, gotSIDs)
}

func TestPruneOnce_TTLEvictsOlderThanCutoff(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()

	now := time.Now()
	saveMessage(t, store, "SMold1", now.Add(-2*time.Hour))
	saveMessage(t, store, "SMold2", now.Add(-1*time.Hour))
	saveMessage(t, store, "SMnew", now.Add(-30*time.Minute))

	clk := &fakeClock{now: now}
	p := New(store, discardLogger(), config.Limits{MaxAge: 45 * time.Minute}, clk)
	require.NotNil(t, p)

	p.PruneOnce(ctx)

	_, err := store.GetMessage(ctx, "SMold1")
	require.ErrorIs(t, err, storage.ErrNotFound)
	_, err = store.GetMessage(ctx, "SMold2")
	require.ErrorIs(t, err, storage.ErrNotFound)
	_, err = store.GetMessage(ctx, "SMnew")
	require.NoError(t, err)
}

func TestRun_ImmediateFirstPassThenTicks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := testutil.NewFakeStore()
	saveMessage(t, store, "SMx", time.Now().Add(-time.Hour))

	// MaxAge=1ns means "anything older than now" — immediate first pass
	// should delete SMx.
	p := New(store, discardLogger(), config.Limits{MaxAge: time.Nanosecond}, &fakeClock{now: time.Now()})
	require.NotNil(t, p)
	p.SetInterval(20 * time.Millisecond)

	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()

	require.Eventually(t, func() bool {
		_, total, _ := store.ListMessages(ctx, 10, 0)
		return total == 0
	}, time.Second, 10*time.Millisecond, "first pass should have pruned the message")

	cancel()
	err := <-done
	assert.ErrorIs(t, err, context.Canceled, "Run should return context.Canceled, got %v", err)
}

func TestRun_StopsCleanlyOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := testutil.NewFakeStore()
	p := New(store, discardLogger(), config.Limits{MaxMessages: 1}, &fakeClock{now: time.Now()})
	require.NotNil(t, p)
	p.SetInterval(time.Hour) // long enough that the goroutine is parked at the select

	var returned atomic.Int32
	done := make(chan struct{})
	go func() {
		_ = p.Run(ctx)
		returned.Add(1)
		close(done)
	}()

	cancel()
	select {
	case <-done:
		assert.Equal(t, int32(1), returned.Load())
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}

func TestWait_ReturnsAfterRunExits(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := testutil.NewFakeStore()
	p := New(store, discardLogger(), config.Limits{MaxMessages: 1}, &fakeClock{now: time.Now()})
	require.NotNil(t, p)
	p.SetInterval(time.Hour)

	go func() { _ = p.Run(ctx) }()

	cancel()
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer waitCancel()
	require.NoError(t, p.Wait(waitCtx), "Wait should return nil after Run exits")
}

func TestWait_TimesOutWhenRunStillRunning(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := testutil.NewFakeStore()
	p := New(store, discardLogger(), config.Limits{MaxMessages: 1}, &fakeClock{now: time.Now()})
	require.NotNil(t, p)
	p.SetInterval(time.Hour)

	go func() { _ = p.Run(ctx) }()

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer waitCancel()
	err := p.Wait(waitCtx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}
