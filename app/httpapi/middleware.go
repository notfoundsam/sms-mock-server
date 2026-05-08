package httpapi

import (
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
)

// requestLogger logs each request matching the path-prefix-based level rules
// from the Python implementation (`app/main.py:40-58`):
//
//	/static/...     → skip entirely
//	/health         → skip entirely
//	/2010-04-01/... → INFO  "API {method} {path} from {client_host}"
//	otherwise       → DEBUG "UI {method} {path}"
//
// Logging happens before handler invocation, matching Python — no status code
// or duration (those would require a ResponseWriter wrapper, and the Python
// version doesn't provide them, so neither do we).
func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			switch {
			case strings.HasPrefix(path, "/static/"):
				// skip
			case strings.HasPrefix(path, "/2010-04-01/"):
				logger.Info("API "+r.Method+" "+path+" from "+clientHost(r),
					"method", r.Method, "path", path, "client", clientHost(r))
			case strings.HasPrefix(path, "/health"):
				// skip
			default:
				logger.Debug("UI "+r.Method+" "+path,
					"method", r.Method, "path", path)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// recoverer catches panics from downstream handlers and writes a 500.
// Logs the panic value plus stack trace at ERROR level.
func recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic in handler",
						"path", r.URL.Path, "method", r.Method,
						"panic", rec, "stack", string(debug.Stack()))
					writeJSONString(w, http.StatusInternalServerError,
						`{"code":500,"message":"internal error","status":500}`)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// clientHost extracts the client's IP/hostname for logging. Falls back to
// "unknown" if RemoteAddr is empty or malformed.
func clientHost(r *http.Request) string {
	if r.RemoteAddr == "" {
		return "unknown"
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// chain composes middleware in order: chain(a, b, c)(h) == a(b(c(h))).
// First middleware wraps everything (so it runs first on the way in,
// last on the way out).
func chain(mws ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		for i := len(mws) - 1; i >= 0; i-- {
			h = mws[i](h)
		}
		return h
	}
}
