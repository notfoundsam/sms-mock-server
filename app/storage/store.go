// Package storage persists messages, calls, delivery events, and callback logs.
package storage

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned by lookups when no row matches.
var ErrNotFound = errors.New("not found")

type Message struct {
	ID          int64
	SID         string
	Provider    string
	From        string
	To          string
	Body        string
	Status      string
	CallbackURL string
	IsRead      bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Call struct {
	ID          int64
	SID         string
	Provider    string
	From        string
	To          string
	Status      string
	CallbackURL string
	TwiMLURL    string
	IsRead      bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type DeliveryEvent struct {
	ID               int64
	MessageSID       string // empty when this event is for a call
	CallSID          string // empty when this event is for a message
	EventType        string
	Status           string
	CallbackSent     bool
	CallbackResponse string
	CreatedAt        time.Time
}

type CallbackLog struct {
	ID            int64
	TargetURL     string
	Payload       string
	StatusCode    int // 0 if request failed before getting a response
	ResponseBody  string
	AttemptNumber int
	CreatedAt     time.Time
}

type Stats struct {
	Messages  int `json:"messages"`
	Calls     int `json:"calls"`
	Callbacks int `json:"callbacks"`
}

// CallbackSummary aggregates callback_logs rows that reference a single record
// (message or call) by SID. Used by the UI detail page to show a one-line
// delivery summary. Count is 0 when no callbacks have been recorded.
type CallbackSummary struct {
	Count            int
	LastStatus       int    // 0 if no callback recorded or last attempt didn't get an HTTP response
	LastResponseBody string // most recent response body (or last error body if last attempt failed)
}

// ClearCounts is the result of clearing all tables.
type ClearCounts struct {
	Messages  int `json:"messages"`
	Calls     int `json:"calls"`
	Callbacks int `json:"callbacks"`
}

// Store is the persistence interface used by the rest of the app.
// Implementations: SQLite (storage.New), in-memory fake (testutil.FakeStore).
type Store interface {
	// Messages
	SaveMessage(ctx context.Context, m *Message) error
	GetMessage(ctx context.Context, sid string) (*Message, error)
	UpdateMessageStatus(ctx context.Context, sid, status string) error
	ListMessages(ctx context.Context, limit, offset int) (rows []Message, total int, err error)
	// SearchMessages is like ListMessages but filtered. q matches LIKE %q% against
	// from_number, to_number, and body. status matches exactly. tags AND-filters:
	// each name in tags must be attached to the message. Empty fields are skipped.
	SearchMessages(ctx context.Context, q, status string, tags []string, limit, offset int) (rows []Message, total int, err error)
	// MarkMessageRead sets is_read=1. Idempotent (no-op when already read).
	// Returns ErrNotFound if no message has the given SID.
	MarkMessageRead(ctx context.Context, sid string) error
	// DeleteMessage removes a single message by SID. Cascades to delivery_events
	// rows that reference it. Returns ErrNotFound if no row matches.
	DeleteMessage(ctx context.Context, sid string) error

	// Calls
	SaveCall(ctx context.Context, c *Call) error
	GetCall(ctx context.Context, sid string) (*Call, error)
	UpdateCallStatus(ctx context.Context, sid, status string) error
	ListCalls(ctx context.Context, limit, offset int) (rows []Call, total int, err error)
	// SearchCalls is like ListCalls but filtered. q matches LIKE %q% against
	// from_number and to_number (calls have no body). status matches exactly.
	// tags AND-filters as in SearchMessages.
	SearchCalls(ctx context.Context, q, status string, tags []string, limit, offset int) (rows []Call, total int, err error)
	// MarkCallRead sets is_read=1. Idempotent. Returns ErrNotFound if SID missing.
	MarkCallRead(ctx context.Context, sid string) error
	// DeleteCall removes a single call by SID. Cascades to delivery_events.
	// Returns ErrNotFound if no row matches.
	DeleteCall(ctx context.Context, sid string) error

	// Delivery events
	SaveDeliveryEvent(ctx context.Context, e *DeliveryEvent) error

	// Callback logs
	SaveCallbackLog(ctx context.Context, l *CallbackLog) error
	GetCallbackLog(ctx context.Context, id int64) (*CallbackLog, error)
	ListCallbackLogs(ctx context.Context, limit, offset int) (rows []CallbackLog, total int, err error)

	// Stats / clear
	Stats(ctx context.Context) (Stats, error)
	// StatusCounts returns a per-status row count for the named record type
	// ("messages" or "calls"). Drives the Tags section of the UI sidebar.
	StatusCounts(ctx context.Context, recordType string) (map[string]int, error)
	// CallbackSummary aggregates callback_logs entries that reference the given
	// SID in their JSON payload. Returns Count=0 if no callbacks were sent.
	CallbackSummary(ctx context.Context, sid string) (CallbackSummary, error)

	// Tags. Names are stored once globally (tags table) and linked per record.
	// SetMessageTags / SetCallTags replace the full tag set for the record.
	// Empty/duplicate names should be filtered by the caller. Idempotent.
	SetMessageTags(ctx context.Context, messageID int64, names []string) error
	SetCallTags(ctx context.Context, callID int64, names []string) error
	// MessageTags / CallTags return the names attached to a record, sorted asc.
	MessageTags(ctx context.Context, messageID int64) ([]string, error)
	CallTags(ctx context.Context, callID int64) ([]string, error)
	// ListTagNames returns names of tags currently attached to at least one
	// record of the given type. recordType must be "messages" or "calls".
	// Sorted ascending. Empty when no records of that type are tagged.
	ListTagNames(ctx context.Context, recordType string) ([]string, error)
	ClearMessages(ctx context.Context) (int, error)
	ClearCalls(ctx context.Context) (int, error)
	ClearCallbacks(ctx context.Context) (int, error)
	ClearAll(ctx context.Context) (ClearCounts, error)

	// Pruning. The background pruner (app/prune) calls these on a periodic
	// tick to enforce retention limits. Each method deletes related
	// delivery_events rows in the same transaction; tags cascade via FK.
	//
	// PruneMessagesByCount deletes oldest-first until the total count is at
	// or below limit. limit=0 is a no-op (returns 0, nil). Returns the
	// number of messages deleted.
	PruneMessagesByCount(ctx context.Context, limit int) (int, error)
	// PruneMessagesByAge deletes messages with created_at < cutoff. Returns
	// the number deleted.
	PruneMessagesByAge(ctx context.Context, cutoff time.Time) (int, error)
	// PruneCallsByCount mirrors PruneMessagesByCount for the calls table.
	PruneCallsByCount(ctx context.Context, limit int) (int, error)
	// PruneCallsByAge mirrors PruneMessagesByAge for the calls table.
	PruneCallsByAge(ctx context.Context, cutoff time.Time) (int, error)

	Close() error
}
