package httpapi

// Dispatcher is the subset of internal/callback.Dispatcher that handlers depend on.
// Defined here as an interface so:
//   - Task 6 handler tests can pass a fake without importing the callback package
//   - Task 7 dispatcher can be a concrete type and still satisfy this contract
type Dispatcher interface {
	ScheduleSMSStatusFlow(messageSID, from, to, callbackURL string, isKnown, willSucceed bool)
	ScheduleCallStatusFlow(callSID, from, to, callbackURL string, isKnown, willSucceed bool)
}
