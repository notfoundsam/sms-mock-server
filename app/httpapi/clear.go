package httpapi

import (
	"encoding/json"
	"net/http"
)

type clearResponse struct {
	Deleted any    `json:"deleted"`          // int for single-table clears, ClearCounts for /clear/all
	Type    string `json:"type"`
}

// ClearMessages handles POST /clear/messages.
func (s *Server) ClearMessages(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.ClearMessages(r.Context())
	if err != nil {
		s.logger.Error("ClearMessages", "error", err)
		writeJSONString(w, http.StatusInternalServerError, `{"error":"clear failed"}`)
		return
	}
	s.logger.Info("cleared messages", "count", n)
	writeClearResponse(w, n, "messages")
}

// ClearCalls handles POST /clear/calls.
func (s *Server) ClearCalls(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.ClearCalls(r.Context())
	if err != nil {
		s.logger.Error("ClearCalls", "error", err)
		writeJSONString(w, http.StatusInternalServerError, `{"error":"clear failed"}`)
		return
	}
	s.logger.Info("cleared calls", "count", n)
	writeClearResponse(w, n, "calls")
}

// ClearCallbacks handles POST /clear/callbacks.
func (s *Server) ClearCallbacks(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.ClearCallbacks(r.Context())
	if err != nil {
		s.logger.Error("ClearCallbacks", "error", err)
		writeJSONString(w, http.StatusInternalServerError, `{"error":"clear failed"}`)
		return
	}
	s.logger.Info("cleared callbacks", "count", n)
	writeClearResponse(w, n, "callbacks")
}

// ClearAll handles POST /clear/all. Returns the per-table counts as a nested
// object under `deleted`, matching `app/main.py:239-248`.
func (s *Server) ClearAll(w http.ResponseWriter, r *http.Request) {
	counts, err := s.store.ClearAll(r.Context())
	if err != nil {
		s.logger.Error("ClearAll", "error", err)
		writeJSONString(w, http.StatusInternalServerError, `{"error":"clear failed"}`)
		return
	}
	s.logger.Info("cleared all", "counts", counts)
	writeClearResponse(w, counts, "all")
}

func writeClearResponse(w http.ResponseWriter, deleted any, t string) {
	body, _ := json.Marshal(clearResponse{Deleted: deleted, Type: t})
	writeJSONBytes(w, http.StatusOK, body)
}
