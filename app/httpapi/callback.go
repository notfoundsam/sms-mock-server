package httpapi

import (
	"encoding/json"
	"net/http"
)

// CallbackTest handles POST /callback-test. Mirrors `app/main.py:188-200`:
// accepts any form data, returns `{status: "received", data: {...form fields...}}`.
// On parse error returns `{status: "received"}` (no data field) — same as Python.
func (s *Server) CallbackTest(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.logger.Debug("callback-test ParseForm failed", "error", err)
		writeJSONString(w, http.StatusOK, `{"status":"received"}`)
		return
	}

	// Flatten r.PostForm to map[string]string (first value per key, matching
	// Python's `dict(form_data)` which yields one value per key).
	flat := make(map[string]string, len(r.PostForm))
	for k, v := range r.PostForm {
		if len(v) > 0 {
			flat[k] = v[0]
		}
	}

	s.logger.Info("callback-test received", "fields", len(flat))

	body, err := json.Marshal(map[string]any{
		"status": "received",
		"data":   flat,
	})
	if err != nil {
		writeJSONString(w, http.StatusOK, `{"status":"received"}`)
		return
	}
	writeJSONBytes(w, http.StatusOK, body)
}
