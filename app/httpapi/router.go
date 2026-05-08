// Package httpapi wires HTTP handlers, middleware, and the router for the
// Twilio-compatible API surface. Server holds the per-process dependencies
// (config, provider, storage, template engine, callback dispatcher) and
// exposes them to handler methods.
package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/notfoundsam/sms-mock-server/app/provider"
	"github.com/notfoundsam/sms-mock-server/app/storage"
	tmpl "github.com/notfoundsam/sms-mock-server/app/template"
)

// Server is the HTTP layer. Construct via NewServer; obtain the
// http.Handler via Handler.
type Server struct {
	logger     *slog.Logger
	provider   provider.Provider
	store      storage.Store
	tmpl       *tmpl.Engine
	dispatcher Dispatcher

	accountSid       string // copied from config.Twilio.AccountSid for response rendering
	callbacksEnabled bool   // copied from config.Twilio.Callbacks.Enabled
}

// Deps bundles the constructor inputs.
type Deps struct {
	Logger           *slog.Logger
	Provider         provider.Provider
	Store            storage.Store
	Templates        *tmpl.Engine
	Dispatcher       Dispatcher
	AccountSid       string
	CallbacksEnabled bool
}

// NewServer assembles a Server from its dependencies. Caller must ensure
// no field is nil except CallbacksEnabled (zero-value false is fine).
func NewServer(deps Deps) *Server {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		logger:           logger,
		provider:         deps.Provider,
		store:            deps.Store,
		tmpl:             deps.Templates,
		dispatcher:       deps.Dispatcher,
		accountSid:       deps.AccountSid,
		callbacksEnabled: deps.CallbacksEnabled,
	}
}

// WrapWithMiddleware wraps the supplied handler in the standard middleware
// chain (panic recovery + request logging). Callers (main.go, tests) build
// a mux, register routes via RegisterRoutes (and any UI/static routes they
// own), then pass the result here.
func WrapWithMiddleware(h http.Handler, logger *slog.Logger) http.Handler {
	return chain(recoverer(logger), requestLogger(logger))(h)
}

// RegisterRoutes attaches all httpapi-owned routes to mux without applying
// middleware. main.go calls this on a shared mux that also receives the
// UI handler's routes and the static file mount.
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /2010-04-01/Accounts/{AccountSid}/Messages.json", s.SendMessage)
	mux.HandleFunc("POST /2010-04-01/Accounts/{AccountSid}/Calls.json", s.MakeCall)

	mux.HandleFunc("GET /health", s.Health)
	mux.HandleFunc("GET /favicon.ico", s.Favicon)
	mux.HandleFunc("POST /callback-test", s.CallbackTest)

	mux.HandleFunc("POST /clear/messages", s.ClearMessages)
	mux.HandleFunc("POST /clear/calls", s.ClearCalls)
	mux.HandleFunc("POST /clear/callbacks", s.ClearCallbacks)
	mux.HandleFunc("POST /clear/all", s.ClearAll)
}
