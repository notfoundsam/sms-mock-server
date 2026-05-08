package httpapi

import (
	"net/http"

	"github.com/notfoundsam/sms-mock-server/app/provider"
)

// writeError writes a Twilio-format error response based on a *ValidationError.
// On success: writes the rendered JSON template at the appropriate HTTP status.
// On failure (template missing, render error): writes a fallback 500 response and logs.
//
// Always returns nil — the handler should not act further on the request after this.
func (h *Server) writeError(w http.ResponseWriter, err error) {
	v, ok := provider.IsValidationError(err)
	if !ok {
		// Non-validation error escaped a handler; treat as 500.
		h.logger.Error("non-validation error in handler", "error", err)
		writeJSONString(w, http.StatusInternalServerError, `{"code":500,"message":"internal error","status":500}`)
		return
	}

	body, rerr := h.tmpl.RenderError(v.TemplateName, v.Vars)
	if rerr != nil {
		h.logger.Error("failed to render error template",
			"template", v.TemplateName, "render_error", rerr,
			"original_code", v.Code, "original_message", v.Message)
		writeJSONString(w, http.StatusInternalServerError, `{"code":500,"message":"internal error","status":500}`)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(v.HTTPStatus)
	// body is rendered JSON from a server-controlled template, not user-supplied HTML
	if _, werr := w.Write(body); werr != nil { //nolint:gosec // G705: body is server-rendered JSON, not user-controlled HTML
		// Connection likely closed; nothing actionable.
		h.logger.Debug("write error body failed", "error", werr)
	}
}

// writeJSONString writes a literal JSON body at the given status. Used for
// non-templated responses (health, clear, callback-test) and for the
// fallback 500 path. Errors writing to w are dropped — by that point the
// response is committed and there's nothing useful to do.
func writeJSONString(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// writeJSONBytes is like writeJSONString for arbitrary byte slices.
func writeJSONBytes(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
