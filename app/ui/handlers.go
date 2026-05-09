// Package ui registers the HTML/HTMX UI routes (mailbox-style inbox) on the
// supplied http.ServeMux. Templates are rendered by template.Engine using
// the storage.Store as the data source.
package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/notfoundsam/sms-mock-server/app/storage"
	tmpl "github.com/notfoundsam/sms-mock-server/app/template"
)

const itemsPerPage = 50

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

// Register attaches all UI routes to mux.
func (h *Handler) Register(mux *http.ServeMux) {
	// Page routes. "/{$}" matches exactly "/" (Go 1.22 mux syntax).
	mux.HandleFunc("GET /{$}", h.messagesPage)
	mux.HandleFunc("GET /calls", h.callsPage)

	// Detail (full-page) routes.
	mux.HandleFunc("GET /view/messages/{message_sid}", h.messageDetail)
	mux.HandleFunc("GET /view/calls/{call_sid}", h.callDetail)

	// Fragment routes (HTMX polling targets).
	mux.HandleFunc("GET /ui/fragments/list", h.listFragment)
	mux.HandleFunc("GET /ui/fragments/sidebar", h.sidebarFragment)

	// Per-record delete (bulk delete uses the existing /clear/* API endpoints).
	mux.HandleFunc("DELETE /ui/messages/{message_sid}", h.deleteMessage)
	mux.HandleFunc("DELETE /ui/calls/{call_sid}", h.deleteCall)
}

// --- shared types ---

// pageData is the base shape passed to every page template. It includes the
// data the sidebar and list need, so the first paint is server-rendered (no
// HTMX hydration flash).
type pageData struct {
	Provider    string
	Timezone    string
	Type        string // "messages" or "calls" — drives sidebar active state
	Q           string // current search query (raw, may include tag:foo tokens)
	Status      string // current status filter (URL-only, no UI today)
	Query       Query  // parsed Q — exposes .Text and .HasTag for template use
	Page        int
	TotalPages  int
	Total       int
	Messages    []storage.Message // non-nil when Type=="messages"
	Calls       []storage.Call    // non-nil when Type=="calls"
	TagNames    []string          // all tag names attached to any record (sidebar)
	ActiveCount int               // total for the active type (sidebar nav badge)
	OtherCount  int               // total for the OTHER type (sidebar nav badge)
}

// listFragmentData feeds fragments/list.html.
type listFragmentData struct {
	Type       string
	Q          string
	Status     string // legacy URL-only filter, still threaded through pagination links
	Page       int
	TotalPages int
	Total      int
	Messages   []storage.Message
	Calls      []storage.Call
}

// sidebarFragmentData feeds fragments/sidebar.html.
type sidebarFragmentData struct {
	Type        string
	Q           string
	Query       Query
	TagNames    []string
	ActiveCount int
	OtherCount  int
}

// detailData feeds view/message.html and view/call.html. The Q/Status/sidebar
// fields mirror pageData so the same fragments/sidebar.html template renders
// here, and the Back link can rebuild the inbox URL with filters preserved.
type detailData struct {
	Provider        string
	Timezone        string
	Type            string           // "messages" or "calls"
	Message         *storage.Message // populated when Type=="messages"
	Call            *storage.Call    // populated when Type=="calls"
	RawJSON         string
	CallbackSummary storage.CallbackSummary
	RecordTags      []string // tags attached to this specific record

	// Filter state carried through from the inbox URL so the Back link
	// preserves it and the sidebar Tags section highlights correctly.
	Q           string
	Status      string
	Query       Query
	TagNames    []string
	ActiveCount int
	OtherCount  int
}

// BackURL renders the Back-to-inbox URL with q/status preserved.
func (d detailData) BackURL() string {
	base := "/"
	if d.Type == "calls" {
		base = "/calls"
	}
	parts := []string{}
	if d.Q != "" {
		parts = append(parts, "q="+url.QueryEscape(d.Q))
	}
	if d.Status != "" {
		parts = append(parts, "status="+url.QueryEscape(d.Status))
	}
	if len(parts) == 0 {
		return base
	}
	return base + "?" + strings.Join(parts, "&")
}

// sidebarBundle is the data every page + detail view needs to render the
// sidebar: the global tag list plus inbox totals for each type.
type sidebarBundle struct {
	TagNames    []string
	ActiveCount int
	OtherCount  int
}

// loadSidebar fetches the sidebar payload for the given active type
// ("messages" or "calls"). ActiveCount is the total for typ; OtherCount is
// the total for the other type. TagNames is the global list of tags used by
// at least one record (sorted asc), or nil if no records are tagged.
func (h *Handler) loadSidebar(r *http.Request, typ string) (sidebarBundle, error) {
	var sb sidebarBundle
	_, msgTotal, err := h.store.ListMessages(r.Context(), 1, 0)
	if err != nil {
		return sb, fmt.Errorf("list messages count: %w", err)
	}
	_, callTotal, err := h.store.ListCalls(r.Context(), 1, 0)
	if err != nil {
		return sb, fmt.Errorf("list calls count: %w", err)
	}
	names, err := h.store.ListTagNames(r.Context(), typ)
	if err != nil {
		return sb, fmt.Errorf("list tag names: %w", err)
	}
	sb.TagNames = names
	if typ == "calls" {
		sb.ActiveCount = callTotal
		sb.OtherCount = msgTotal
	} else {
		sb.ActiveCount = msgTotal
		sb.OtherCount = callTotal
	}
	return sb, nil
}

// --- pages ---

//nolint:dupl // structurally mirrors callsPage but reads a different store table
func (h *Handler) messagesPage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	status := r.URL.Query().Get("status")
	parsed := ParseQuery(q)
	page, offset := pageOffset(r)

	rows, total, err := h.store.SearchMessages(r.Context(), parsed.Text, status, parsed.Tags, itemsPerPage, offset)
	if err != nil {
		h.serverError(w, "SearchMessages", err)
		return
	}
	sb, err := h.loadSidebar(r, "messages")
	if err != nil {
		h.serverError(w, "loadSidebar", err)
		return
	}

	h.renderUI(w, "messages.html", pageData{
		Provider:    h.provider,
		Timezone:    h.timezone,
		Type:        "messages",
		Q:           q,
		Status:      status,
		Query:       parsed,
		Page:        page,
		TotalPages:  totalPages(total),
		Total:       total,
		Messages:    rows,
		TagNames:    sb.TagNames,
		ActiveCount: sb.ActiveCount,
		OtherCount:  sb.OtherCount,
	})
}

//nolint:dupl // structurally mirrors messagesPage but reads a different store table
func (h *Handler) callsPage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	status := r.URL.Query().Get("status")
	parsed := ParseQuery(q)
	page, offset := pageOffset(r)

	rows, total, err := h.store.SearchCalls(r.Context(), parsed.Text, status, parsed.Tags, itemsPerPage, offset)
	if err != nil {
		h.serverError(w, "SearchCalls", err)
		return
	}
	sb, err := h.loadSidebar(r, "calls")
	if err != nil {
		h.serverError(w, "loadSidebar", err)
		return
	}

	h.renderUI(w, "calls.html", pageData{
		Provider:    h.provider,
		Timezone:    h.timezone,
		Type:        "calls",
		Q:           q,
		Status:      status,
		Query:       parsed,
		Page:        page,
		TotalPages:  totalPages(total),
		Total:       total,
		Calls:       rows,
		TagNames:    sb.TagNames,
		ActiveCount: sb.ActiveCount,
		OtherCount:  sb.OtherCount,
	})
}

// --- detail pages ---

//nolint:dupl // structurally mirrors callDetail
func (h *Handler) messageDetail(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("message_sid")
	m, err := h.store.GetMessage(r.Context(), sid)
	if errors.Is(err, storage.ErrNotFound) {
		h.notFoundPage(w, "Message not found")
		return
	}
	if err != nil {
		h.serverError(w, "GetMessage", err)
		return
	}
	// Mark read after we know the record exists.
	if markErr := h.store.MarkMessageRead(r.Context(), sid); markErr != nil {
		h.logger.Warn("MarkMessageRead", "sid", sid, "error", markErr)
	}
	// Re-fetch so the rendered page reflects IsRead=true (purely cosmetic — the
	// row in the list updates on the next poll regardless).
	m.IsRead = true

	summary, err := h.store.CallbackSummary(r.Context(), sid)
	if err != nil {
		h.logger.Warn("CallbackSummary", "sid", sid, "error", err)
	}

	recordTags, err := h.store.MessageTags(r.Context(), m.ID)
	if err != nil {
		h.logger.Warn("MessageTags", "sid", sid, "error", err)
	}

	sb, err := h.loadSidebar(r, "messages")
	if err != nil {
		h.serverError(w, "loadSidebar", err)
		return
	}

	q := r.URL.Query().Get("q")
	raw, _ := json.MarshalIndent(m, "", "  ")
	h.renderUI(w, "view/message.html", detailData{
		Provider:        h.provider,
		Timezone:        h.timezone,
		Type:            "messages",
		Message:         m,
		RawJSON:         string(raw),
		CallbackSummary: summary,
		RecordTags:      recordTags,
		Q:               q,
		Status:          r.URL.Query().Get("status"),
		Query:           ParseQuery(q),
		TagNames:        sb.TagNames,
		ActiveCount:     sb.ActiveCount,
		OtherCount:      sb.OtherCount,
	})
}

//nolint:dupl // structurally mirrors messageDetail
func (h *Handler) callDetail(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("call_sid")
	c, err := h.store.GetCall(r.Context(), sid)
	if errors.Is(err, storage.ErrNotFound) {
		h.notFoundPage(w, "Call not found")
		return
	}
	if err != nil {
		h.serverError(w, "GetCall", err)
		return
	}
	if markErr := h.store.MarkCallRead(r.Context(), sid); markErr != nil {
		h.logger.Warn("MarkCallRead", "sid", sid, "error", markErr)
	}
	c.IsRead = true

	summary, err := h.store.CallbackSummary(r.Context(), sid)
	if err != nil {
		h.logger.Warn("CallbackSummary", "sid", sid, "error", err)
	}

	recordTags, err := h.store.CallTags(r.Context(), c.ID)
	if err != nil {
		h.logger.Warn("CallTags", "sid", sid, "error", err)
	}

	sb, err := h.loadSidebar(r, "calls")
	if err != nil {
		h.serverError(w, "loadSidebar", err)
		return
	}

	q := r.URL.Query().Get("q")
	raw, _ := json.MarshalIndent(c, "", "  ")
	h.renderUI(w, "view/call.html", detailData{
		Provider:        h.provider,
		Timezone:        h.timezone,
		Type:            "calls",
		Call:            c,
		RawJSON:         string(raw),
		CallbackSummary: summary,
		RecordTags:      recordTags,
		Q:               q,
		Status:          r.URL.Query().Get("status"),
		Query:           ParseQuery(q),
		TagNames:        sb.TagNames,
		ActiveCount:     sb.ActiveCount,
		OtherCount:      sb.OtherCount,
	})
}

// --- fragments ---

func (h *Handler) listFragment(w http.ResponseWriter, r *http.Request) {
	typ := normalizeType(r.URL.Query().Get("type"))
	q := r.URL.Query().Get("q")
	status := r.URL.Query().Get("status")
	parsed := ParseQuery(q)
	page, offset := pageOffset(r)

	data := listFragmentData{Type: typ, Q: q, Status: status, Page: page}
	switch typ {
	case "messages":
		rows, total, err := h.store.SearchMessages(r.Context(), parsed.Text, status, parsed.Tags, itemsPerPage, offset)
		if err != nil {
			h.serverError(w, "SearchMessages", err)
			return
		}
		data.Messages = rows
		data.Total = total
		data.TotalPages = totalPages(total)
	case "calls":
		rows, total, err := h.store.SearchCalls(r.Context(), parsed.Text, status, parsed.Tags, itemsPerPage, offset)
		if err != nil {
			h.serverError(w, "SearchCalls", err)
			return
		}
		data.Calls = rows
		data.Total = total
		data.TotalPages = totalPages(total)
	}
	h.renderUI(w, "fragments/list.html", data)
}

func (h *Handler) sidebarFragment(w http.ResponseWriter, r *http.Request) {
	typ := normalizeType(r.URL.Query().Get("type"))
	q := r.URL.Query().Get("q")
	parsed := ParseQuery(q)

	sb, err := h.loadSidebar(r, typ)
	if err != nil {
		h.serverError(w, "loadSidebar", err)
		return
	}
	h.renderUI(w, "fragments/sidebar.html", sidebarFragmentData{
		Type:        typ,
		Q:           q,
		Query:       parsed,
		TagNames:    sb.TagNames,
		ActiveCount: sb.ActiveCount,
		OtherCount:  sb.OtherCount,
	})
}

// --- delete ---

func (h *Handler) deleteMessage(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("message_sid")
	err := h.store.DeleteMessage(r.Context(), sid)
	if errors.Is(err, storage.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.serverError(w, "DeleteMessage", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) deleteCall(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("call_sid")
	err := h.store.DeleteCall(r.Context(), sid)
	if errors.Is(err, storage.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.serverError(w, "DeleteCall", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ---

// normalizeType clamps the ?type= query parameter to "messages" or "calls",
// defaulting to "messages".
func normalizeType(t string) string {
	if t == "calls" {
		return "calls"
	}
	return "messages"
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

func (h *Handler) notFoundPage(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte("<!DOCTYPE html><html><body><h1>404</h1><p>" + msg + "</p><p><a href='/'>Back to inbox</a></p></body></html>"))
}
