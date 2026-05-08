package httpapi

import (
	"encoding/json"
	"net/http"
	"time"
)

// version is overridable at build time via -ldflags="-X github.com/.../httpapi.version=v1.2.3".
// Defaults to "1.0.0" to match the Python implementation's hardcoded value.
var version = "1.0.0"

type healthResponse struct {
	Status     string      `json:"status"`
	Version    string      `json:"version"`
	Provider   string      `json:"provider"`
	Timestamp  string      `json:"timestamp"`
	Statistics any         `json:"statistics"`
}

// Health handles GET /health. Returns 200 with status, version, provider,
// current UTC timestamp (RFC3339, "Z"-suffix to match Python), and stats.
func (s *Server) Health(w http.ResponseWriter, r *http.Request) {
	stats, err := s.store.Stats(r.Context())
	if err != nil {
		s.logger.Error("Stats failed", "error", err)
		writeJSONString(w, http.StatusInternalServerError, `{"status":"unhealthy"}`)
		return
	}

	resp := healthResponse{
		Status:     "healthy",
		Version:    version,
		Provider:   s.provider.Name(),
		Timestamp:  time.Now().UTC().Format("2006-01-02T15:04:05.000000Z"),
		Statistics: stats,
	}

	body, err := json.Marshal(resp)
	if err != nil {
		s.logger.Error("marshal health response", "error", err)
		writeJSONString(w, http.StatusInternalServerError, `{"status":"unhealthy"}`)
		return
	}
	writeJSONBytes(w, http.StatusOK, body)
}
