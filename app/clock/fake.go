package clock

import (
	"sort"
	"sync"
	"time"
)

// Fake is a deterministic Clock for tests.
//
// Now() returns whatever time.Now was last set to (default: zero time, but
// callers should set it explicitly). AfterFunc registers a pending callback;
// callbacks fire (in due-time order) when Advance moves the clock past their
// deadline.
//
// Fake is safe for concurrent use, but for predictable tests it's simplest to
// drive Advance from a single goroutine.
type Fake struct {
	mu      sync.Mutex
	now     time.Time
	pending []*pendingTimer
}

type pendingTimer struct {
	deadline time.Time
	fn       func()
}

// NewFake returns a Fake with its clock set to t.
func NewFake(t time.Time) *Fake {
	return &Fake{now: t}
}

func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *Fake) AfterFunc(d time.Duration, fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pending = append(f.pending, &pendingTimer{
		deadline: f.now.Add(d),
		fn:       fn,
	})
}

// Advance moves the clock forward by d, firing any registered callbacks
// whose deadline falls within the advanced window, in deadline order.
//
// Time is stepped to each timer's deadline before its callback fires, so a
// callback that schedules another callback with a relative delay observes
// "now" at the deadline (not at the end of the advance). If the inner
// callback's deadline still falls within the advanced window, it fires too.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	target := f.now.Add(d)
	f.mu.Unlock()

	for {
		f.mu.Lock()
		next := f.popNextDueLocked(target)
		f.mu.Unlock()
		if next == nil {
			break
		}
		next.fn()
	}

	// Move now to target last, in case callbacks scheduled timers beyond it.
	f.mu.Lock()
	if target.After(f.now) {
		f.now = target
	}
	f.mu.Unlock()
}

// popNextDueLocked returns the earliest-deadline timer whose deadline is <= target,
// removing it from the pending list and advancing f.now to that deadline.
// Caller must hold f.mu.
func (f *Fake) popNextDueLocked(target time.Time) *pendingTimer {
	if len(f.pending) == 0 {
		return nil
	}
	sort.SliceStable(f.pending, func(i, j int) bool {
		return f.pending[i].deadline.Before(f.pending[j].deadline)
	})
	if f.pending[0].deadline.After(target) {
		return nil
	}
	t := f.pending[0]
	f.pending = f.pending[1:]
	if t.deadline.After(f.now) {
		f.now = t.deadline
	}
	return t
}

// Pending returns the number of timers that have not yet fired. Useful in
// tests to assert "no callbacks scheduled" or "exactly N pending."
func (f *Fake) Pending() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.pending)
}
