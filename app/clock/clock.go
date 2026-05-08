// Package clock abstracts time.Now and time.AfterFunc so dispatcher logic
// can be tested deterministically with a fake clock.
package clock

import "time"

// Clock is the time source used by the callback dispatcher.
// Real production code uses Real{}; tests use Fake.
type Clock interface {
	Now() time.Time
	// AfterFunc schedules f to run after d. Implementations decide whether
	// f runs in a goroutine (Real) or inline when time advances (Fake).
	AfterFunc(d time.Duration, f func())
}

// Real wraps the standard library.
type Real struct{}

func (Real) Now() time.Time                          { return time.Now() }
func (Real) AfterFunc(d time.Duration, f func())     { time.AfterFunc(d, f) }
