package clock

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFake_NowAdvance(t *testing.T) {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	f := NewFake(start)

	assert.True(t, f.Now().Equal(start))

	f.Advance(2 * time.Second)
	assert.True(t, f.Now().Equal(start.Add(2*time.Second)))
}

func TestFake_AfterFunc_FiresWhenAdvancedPastDeadline(t *testing.T) {
	f := NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	var fired int32
	f.AfterFunc(5*time.Second, func() { atomic.AddInt32(&fired, 1) })

	f.Advance(4 * time.Second)
	assert.Equal(t, int32(0), atomic.LoadInt32(&fired))

	f.Advance(2 * time.Second) // total 6s, past deadline
	assert.Equal(t, int32(1), atomic.LoadInt32(&fired))

	assert.Equal(t, 0, f.Pending())
}

func TestFake_FiresInDeadlineOrder(t *testing.T) {
	f := NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	var order []int
	f.AfterFunc(3*time.Second, func() { order = append(order, 3) })
	f.AfterFunc(1*time.Second, func() { order = append(order, 1) })
	f.AfterFunc(2*time.Second, func() { order = append(order, 2) })

	f.Advance(5 * time.Second)

	assert.Equal(t, []int{1, 2, 3}, order)
}

// A callback that schedules another callback within the advanced window
// should also fire in the same Advance call.
func TestFake_NestedScheduling(t *testing.T) {
	f := NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	var fired []string
	f.AfterFunc(1*time.Second, func() {
		fired = append(fired, "outer")
		f.AfterFunc(1*time.Second, func() { fired = append(fired, "inner") })
	})

	f.Advance(3 * time.Second)

	assert.Equal(t, []string{"outer", "inner"}, fired)
}

func TestFake_PendingTracksUnfiredTimers(t *testing.T) {
	f := NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	f.AfterFunc(1*time.Second, func() {})
	f.AfterFunc(10*time.Second, func() {})

	assert.Equal(t, 2, f.Pending())

	f.Advance(2 * time.Second)
	assert.Equal(t, 1, f.Pending())
}

// Real.AfterFunc actually schedules: a tiny smoke test, kept short so it
// doesn't slow the suite.
func TestReal_AfterFunc(t *testing.T) {
	done := make(chan struct{})
	Real{}.AfterFunc(5*time.Millisecond, func() { close(done) })
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Real.AfterFunc never fired")
	}
}
