// Package ui registers the HTML/HTMX UI routes (dashboard, list pages,
// fragment endpoints) on the supplied http.ServeMux. Templates are rendered
// by template.Engine using the storage.Store as the data source.
package ui

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/notfoundsam/sms-mock-server/app/storage"
	tmpl "github.com/notfoundsam/sms-mock-server/app/template"
)

const (
	itemsPerPage    = 50
	recentItemCount = 10
)

// Handler holds dependencies for UI routes. Build via New, then call Register
// on a ServeMux.
type Handler struct {
	logger   *slog.Logger
	store    storage.Store
	tmpl     *tmpl.Engine
	provider string // for nav header
	timezone string // displayed in nav header
}

// New constructs a UI Handler.
func New(logger *slog.Logger, store storage.Store, engine *tmpl.Engine, providerName, timezone string) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{logger: logger, store: store, tmpl: engine, provider: providerName, timezone: timezone}
}

// Register attaches all UI routes (page + fragment) to mux.
func (h *Handler) Register(mux *http.ServeMux) {
	// "/{$}" matches exactly "/" (Go 1.22 mux syntax). Without {$}, "GET /"
	// is a sub-tree match that catches every unmatched GET, which would
	// suppress 405-not-allowed responses for paths registered with other methods.
	mux.HandleFunc("GET /{$}", h.dashboard)
	mux.HandleFunc("GET /ui/messages", h.messagesPage)
	mux.HandleFunc("GET /ui/calls", h.callsPage)
	mux.HandleFunc("GET /ui/callbacks", h.callbacksPage)

	mux.HandleFunc("GET /ui/fragments/stats", h.statsFragment)
	mux.HandleFunc("GET /ui/fragments/recent-messages", h.recentMessagesFragment)
	mux.HandleFunc("GET /ui/fragments/recent-calls", h.recentCallsFragment)
	mux.HandleFunc("GET /ui/fragments/messages-table", h.messagesTableFragment)
	mux.HandleFunc("GET /ui/fragments/calls-table", h.callsTableFragment)
	mux.HandleFunc("GET /ui/fragments/callbacks-table", h.callbacksTableFragment)
	mux.HandleFunc("GET /ui/fragments/message/{message_sid}", h.messageDetailFragment)
	mux.HandleFunc("GET /ui/fragments/call/{call_sid}", h.callDetailFragment)
	mux.HandleFunc("GET /ui/fragments/callback-detail/{callback_id}", h.callbackDetailFragment)
}

// --- shared data ---

// pageData is base data passed to every page (top-level UI route). Fragment
// data structs embed nothing — fragments don't need provider/timezone since
// they don't use base.html.
type pageData struct {
	Provider string
	Timezone string
	Path     string // for active-tab highlighting in nav
}

func (h *Handler) base(r *http.Request) pageData {
	return pageData{Provider: h.provider, Timezone: h.timezone, Path: r.URL.Path}
}

// --- pages ---

type dashboardData struct {
	pageData
	Stats          storage.Stats
	RecentMessages []storage.Message
	RecentCalls    []storage.Call
}

func (h *Handler) dashboard(w http.ResponseWriter, r *http.Request) {
	stats, err := h.store.Stats(r.Context())
	if err != nil {
		h.serverError(w, "Stats", err)
		return
	}
	recentMessages, _, err := h.store.ListMessages(r.Context(), recentItemCount, 0)
	if err != nil {
		h.serverError(w, "ListMessages", err)
		return
	}
	recentCalls, _, err := h.store.ListCalls(r.Context(), recentItemCount, 0)
	if err != nil {
		h.serverError(w, "ListCalls", err)
		return
	}
	h.renderUI(w, "dashboard.html", dashboardData{
		pageData: h.base(r), Stats: stats,
		RecentMessages: recentMessages, RecentCalls: recentCalls,
	})
}

type messagesPageData struct {
	pageData
	Messages   []storage.Message
	Page       int
	TotalPages int
}

func (h *Handler) messagesPage(w http.ResponseWriter, r *http.Request) {
	page, offset := pageOffset(r)
	rows, total, err := h.store.ListMessages(r.Context(), itemsPerPage, offset)
	if err != nil {
		h.serverError(w, "ListMessages", err)
		return
	}
	h.renderUI(w, "messages.html", messagesPageData{
		pageData: h.base(r), Messages: rows,
		Page: page, TotalPages: totalPages(total),
	})
}

type callsPageData struct {
	pageData
	Calls      []storage.Call
	Page       int
	TotalPages int
}

func (h *Handler) callsPage(w http.ResponseWriter, r *http.Request) {
	page, offset := pageOffset(r)
	rows, total, err := h.store.ListCalls(r.Context(), itemsPerPage, offset)
	if err != nil {
		h.serverError(w, "ListCalls", err)
		return
	}
	h.renderUI(w, "calls.html", callsPageData{
		pageData: h.base(r), Calls: rows,
		Page: page, TotalPages: totalPages(total),
	})
}

type callbacksPageData struct {
	pageData
	Callbacks  []callbackRow
	Page       int
	TotalPages int
}

func (h *Handler) callbacksPage(w http.ResponseWriter, r *http.Request) {
	page, offset := pageOffset(r)
	rows, total, err := h.store.ListCallbackLogs(r.Context(), itemsPerPage, offset)
	if err != nil {
		h.serverError(w, "ListCallbackLogs", err)
		return
	}
	h.renderUI(w, "callbacks.html", callbacksPageData{
		pageData: h.base(r), Callbacks: enrichCallbacks(rows),
		Page: page, TotalPages: totalPages(total),
	})
}

// --- fragments ---

type statsFragmentData struct {
	Stats storage.Stats
}

func (h *Handler) statsFragment(w http.ResponseWriter, r *http.Request) {
	stats, err := h.store.Stats(r.Context())
	if err != nil {
		h.serverError(w, "Stats", err)
		return
	}
	h.renderUI(w, "fragments/stats.html", statsFragmentData{Stats: stats})
}

type recentMessagesData struct {
	RecentMessages []storage.Message
}

func (h *Handler) recentMessagesFragment(w http.ResponseWriter, r *http.Request) {
	rows, _, err := h.store.ListMessages(r.Context(), recentItemCount, 0)
	if err != nil {
		h.serverError(w, "ListMessages", err)
		return
	}
	h.renderUI(w, "fragments/recent_messages.html", recentMessagesData{RecentMessages: rows})
}

type recentCallsData struct {
	RecentCalls []storage.Call
}

func (h *Handler) recentCallsFragment(w http.ResponseWriter, r *http.Request) {
	rows, _, err := h.store.ListCalls(r.Context(), recentItemCount, 0)
	if err != nil {
		h.serverError(w, "ListCalls", err)
		return
	}
	h.renderUI(w, "fragments/recent_calls.html", recentCallsData{RecentCalls: rows})
}

type messagesTableData struct {
	Messages   []storage.Message
	Page       int
	TotalPages int
}

func (h *Handler) messagesTableFragment(w http.ResponseWriter, r *http.Request) {
	page, offset := pageOffset(r)
	rows, total, err := h.store.ListMessages(r.Context(), itemsPerPage, offset)
	if err != nil {
		h.serverError(w, "ListMessages", err)
		return
	}
	h.renderUI(w, "fragments/messages_table.html", messagesTableData{
		Messages: rows, Page: page, TotalPages: totalPages(total),
	})
}

type callsTableData struct {
	Calls      []storage.Call
	Page       int
	TotalPages int
}

func (h *Handler) callsTableFragment(w http.ResponseWriter, r *http.Request) {
	page, offset := pageOffset(r)
	rows, total, err := h.store.ListCalls(r.Context(), itemsPerPage, offset)
	if err != nil {
		h.serverError(w, "ListCalls", err)
		return
	}
	h.renderUI(w, "fragments/calls_table.html", callsTableData{
		Calls: rows, Page: page, TotalPages: totalPages(total),
	})
}

type callbacksTableData struct {
	Callbacks  []callbackRow
	Page       int
	TotalPages int
}

func (h *Handler) callbacksTableFragment(w http.ResponseWriter, r *http.Request) {
	page, offset := pageOffset(r)
	rows, total, err := h.store.ListCallbackLogs(r.Context(), itemsPerPage, offset)
	if err != nil {
		h.serverError(w, "ListCallbackLogs", err)
		return
	}
	h.renderUI(w, "fragments/callbacks_table.html", callbacksTableData{
		Callbacks: enrichCallbacks(rows), Page: page, TotalPages: totalPages(total),
	})
}

type messageDetailData struct {
	Message *storage.Message
}

func (h *Handler) messageDetailFragment(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("message_sid")
	m, err := h.store.GetMessage(r.Context(), sid)
	if errors.Is(err, storage.ErrNotFound) {
		h.notFoundFragment(w, "Message not found")
		return
	}
	if err != nil {
		h.serverError(w, "GetMessage", err)
		return
	}
	h.renderUI(w, "fragments/message_detail.html", messageDetailData{Message: m})
}

type callDetailData struct {
	Call *storage.Call
}

func (h *Handler) callDetailFragment(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("call_sid")
	c, err := h.store.GetCall(r.Context(), sid)
	if errors.Is(err, storage.ErrNotFound) {
		h.notFoundFragment(w, "Call not found")
		return
	}
	if err != nil {
		h.serverError(w, "GetCall", err)
		return
	}
	h.renderUI(w, "fragments/call_detail.html", callDetailData{Call: c})
}

func (h *Handler) callbackDetailFragment(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("callback_id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		h.notFoundFragment(w, "Callback not found")
		return
	}
	log, err := h.store.GetCallbackLog(r.Context(), id)
	if errors.Is(err, storage.ErrNotFound) {
		h.notFoundFragment(w, "Callback not found")
		return
	}
	if err != nil {
		h.serverError(w, "GetCallbackLog", err)
		return
	}
	row := enrichCallback(log)
	h.renderUI(w, "fragments/callback_detail.html", row)
}

// --- helpers ---

// callbackRow is the enriched view of a CallbackLog: the original log plus
// the parsed status fields from its JSON payload (mirrors `app/ui.py:63-85`).
type callbackRow struct {
	Log           *storage.CallbackLog
	MessageSID    string
	CallSID       string
	MessageStatus string
	CallStatus    string
}

// enrichCallbacks converts a slice of CallbackLogs to enriched rows.
func enrichCallbacks(logs []storage.CallbackLog) []callbackRow {
	out := make([]callbackRow, len(logs))
	for i := range logs {
		out[i] = enrichCallback(&logs[i])
	}
	return out
}

// enrichCallback extracts MessageSid, CallSid, MessageStatus, CallStatus from
// the log's JSON payload. Silently leaves fields empty on parse failure.
func enrichCallback(log *storage.CallbackLog) callbackRow {
	row := callbackRow{Log: log}
	if log.Payload == "" {
		return row
	}
	var p map[string]any
	if err := json.Unmarshal([]byte(log.Payload), &p); err != nil {
		return row
	}
	if v, ok := p["MessageSid"].(string); ok {
		row.MessageSID = v
	}
	if v, ok := p["CallSid"].(string); ok {
		row.CallSID = v
	}
	if v, ok := p["MessageStatus"].(string); ok {
		row.MessageStatus = v
	}
	if v, ok := p["CallStatus"].(string); ok {
		row.CallStatus = v
	}
	return row
}

// pageOffset parses ?page=N from the query string. Clamps to ≥1; returns
// (page, offset).
func pageOffset(r *http.Request) (page, offset int) {
	page = 1
	if v := r.URL.Query().Get("page"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p >= 1 {
			page = p
		}
	}
	offset = (page - 1) * itemsPerPage
	return page, offset
}

func totalPages(totalItems int) int {
	if totalItems <= 0 {
		return 0
	}
	return (totalItems + itemsPerPage - 1) / itemsPerPage
}

func (h *Handler) renderUI(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.RenderUI(w, name, data); err != nil {
		h.logger.Error("template render failed", "name", name, "error", err)
	}
}

func (h *Handler) serverError(w http.ResponseWriter, op string, err error) {
	h.logger.Error("ui handler error", "op", op, "error", err)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write([]byte("<p>Internal server error</p>"))
}

func (h *Handler) notFoundFragment(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`<div class='modal-body'><p>` + msg + `</p></div>`))
}
