package testutil

import "sync"

// FakeDispatcher records ScheduleSMS/CallStatusFlow calls without doing any
// real work. Used by handler tests to verify that the right scheduling
// happened without spinning up the real worker pool.
type FakeDispatcher struct {
	mu  sync.Mutex
	sms []ScheduleSMSCall
	cal []ScheduleCallCall
}

type ScheduleSMSCall struct {
	MessageSID  string
	From        string
	To          string
	CallbackURL string
	IsKnown     bool
	WillSucceed bool
}

type ScheduleCallCall struct {
	CallSID     string
	From        string
	To          string
	CallbackURL string
	IsKnown     bool
	WillSucceed bool
}

func NewFakeDispatcher() *FakeDispatcher { return &FakeDispatcher{} }

func (f *FakeDispatcher) ScheduleSMSStatusFlow(sid, from, to, cb string, known, ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sms = append(f.sms, ScheduleSMSCall{
		MessageSID: sid, From: from, To: to, CallbackURL: cb,
		IsKnown: known, WillSucceed: ok,
	})
}

func (f *FakeDispatcher) ScheduleCallStatusFlow(sid, from, to, cb string, known, ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cal = append(f.cal, ScheduleCallCall{
		CallSID: sid, From: from, To: to, CallbackURL: cb,
		IsKnown: known, WillSucceed: ok,
	})
}

// SMSCalls returns a snapshot of recorded SMS scheduling calls.
func (f *FakeDispatcher) SMSCalls() []ScheduleSMSCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]ScheduleSMSCall, len(f.sms))
	copy(out, f.sms)
	return out
}

// CallCalls returns a snapshot of recorded Call scheduling calls.
func (f *FakeDispatcher) CallCalls() []ScheduleCallCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]ScheduleCallCall, len(f.cal))
	copy(out, f.cal)
	return out
}
