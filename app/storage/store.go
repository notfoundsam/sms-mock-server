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

	// Calls
	SaveCall(ctx context.Context, c *Call) error
	GetCall(ctx context.Context, sid string) (*Call, error)
	UpdateCallStatus(ctx context.Context, sid, status string) error
	ListCalls(ctx context.Context, limit, offset int) (rows []Call, total int, err error)

	// Delivery events
	SaveDeliveryEvent(ctx context.Context, e *DeliveryEvent) error

	// Callback logs
	SaveCallbackLog(ctx context.Context, l *CallbackLog) error
	GetCallbackLog(ctx context.Context, id int64) (*CallbackLog, error)
	ListCallbackLogs(ctx context.Context, limit, offset int) (rows []CallbackLog, total int, err error)

	// Stats / clear
	Stats(ctx context.Context) (Stats, error)
	ClearMessages(ctx context.Context) (int, error)
	ClearCalls(ctx context.Context) (int, error)
	ClearCallbacks(ctx context.Context) (int, error)
	ClearAll(ctx context.Context) (ClearCounts, error)

	Close() error
}
