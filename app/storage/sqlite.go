package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const driverName = "sqlite"

// sqliteStore is the SQLite-backed Store implementation.
type sqliteStore struct {
	db *sql.DB
}

// New opens (or creates) the SQLite database at path, applies migrations, and returns a Store.
// Pass ":memory:" for an in-memory DB (used by tests).
func New(ctx context.Context, path string) (Store, error) {
	dsn := buildDSN(path)
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// In-memory databases are per-connection in SQLite. Force a single connection
	// so all callers see the same data. For file-backed DBs we let the pool
	// manage concurrency normally (WAL handles concurrent reads + serialized writes).
	if isMemoryDSN(path) {
		db.SetMaxOpenConns(1)
	}

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return &sqliteStore{db: db}, nil
}

func buildDSN(path string) string {
	// modernc.org/sqlite accepts pragmas via _pragma query parameters.
	if isMemoryDSN(path) {
		// memory + shared cache so multiple opens of the same name share state
		// (we still cap MaxOpenConns at 1 for safety).
		return "file:" + path + "?cache=shared&_pragma=busy_timeout(5000)"
	}
	return fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)",
		path,
	)
}

func isMemoryDSN(path string) bool {
	return path == ":memory:" || strings.HasPrefix(path, "file::memory:")
}

func (s *sqliteStore) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close sqlite: %w", err)
	}
	return nil
}

// --- messages ---

func (s *sqliteStore) SaveMessage(ctx context.Context, m *Message) error {
	res, err := s.db.ExecContext(ctx, `
        INSERT INTO messages (message_sid, provider, from_number, to_number, body, status, callback_url)
        VALUES (?, ?, ?, ?, ?, ?, ?)`,
		m.SID, m.Provider, m.From, m.To, m.Body, m.Status, nullableString(m.CallbackURL))
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}
	id, _ := res.LastInsertId()
	m.ID = id
	return nil
}

func (s *sqliteStore) GetMessage(ctx context.Context, sid string) (*Message, error) {
	row := s.db.QueryRowContext(ctx, `
        SELECT id, message_sid, provider, from_number, to_number, body, status, callback_url, created_at, updated_at
        FROM messages WHERE message_sid = ?`, sid)
	m := &Message{}
	var body, callbackURL sql.NullString
	if err := row.Scan(&m.ID, &m.SID, &m.Provider, &m.From, &m.To, &body, &m.Status, &callbackURL, &m.CreatedAt, &m.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan message: %w", err)
	}
	m.Body = body.String
	m.CallbackURL = callbackURL.String
	return m, nil
}

func (s *sqliteStore) UpdateMessageStatus(ctx context.Context, sid, status string) error {
	res, err := s.db.ExecContext(ctx, `
        UPDATE messages SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE message_sid = ?`,
		status, sid)
	if err != nil {
		return fmt.Errorf("update message status: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqliteStore) ListMessages(ctx context.Context, limit, offset int) ([]Message, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count messages: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
        SELECT id, message_sid, provider, from_number, to_number, body, status, callback_url, created_at, updated_at
        FROM messages ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`,
		limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()

	var out []Message
	for rows.Next() {
		var m Message
		var body, cb sql.NullString
		if err := rows.Scan(&m.ID, &m.SID, &m.Provider, &m.From, &m.To, &body, &m.Status, &cb, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan messages row: %w", err)
		}
		m.Body = body.String
		m.CallbackURL = cb.String
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate messages rows: %w", err)
	}
	return out, total, nil
}

// --- calls ---

func (s *sqliteStore) SaveCall(ctx context.Context, c *Call) error {
	res, err := s.db.ExecContext(ctx, `
        INSERT INTO calls (call_sid, provider, from_number, to_number, status, callback_url, twiml_url)
        VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.SID, c.Provider, c.From, c.To, c.Status, nullableString(c.CallbackURL), nullableString(c.TwiMLURL))
	if err != nil {
		return fmt.Errorf("insert call: %w", err)
	}
	id, _ := res.LastInsertId()
	c.ID = id
	return nil
}

func (s *sqliteStore) GetCall(ctx context.Context, sid string) (*Call, error) {
	row := s.db.QueryRowContext(ctx, `
        SELECT id, call_sid, provider, from_number, to_number, status, callback_url, twiml_url, created_at, updated_at
        FROM calls WHERE call_sid = ?`, sid)
	c := &Call{}
	var cb, tw sql.NullString
	if err := row.Scan(&c.ID, &c.SID, &c.Provider, &c.From, &c.To, &c.Status, &cb, &tw, &c.CreatedAt, &c.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan call: %w", err)
	}
	c.CallbackURL = cb.String
	c.TwiMLURL = tw.String
	return c, nil
}

func (s *sqliteStore) UpdateCallStatus(ctx context.Context, sid, status string) error {
	res, err := s.db.ExecContext(ctx, `
        UPDATE calls SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE call_sid = ?`,
		status, sid)
	if err != nil {
		return fmt.Errorf("update call status: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqliteStore) ListCalls(ctx context.Context, limit, offset int) ([]Call, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM calls`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count calls: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
        SELECT id, call_sid, provider, from_number, to_number, status, callback_url, twiml_url, created_at, updated_at
        FROM calls ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`,
		limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list calls: %w", err)
	}
	defer rows.Close()

	var out []Call
	for rows.Next() {
		var c Call
		var cb, tw sql.NullString
		if err := rows.Scan(&c.ID, &c.SID, &c.Provider, &c.From, &c.To, &c.Status, &cb, &tw, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan calls row: %w", err)
		}
		c.CallbackURL = cb.String
		c.TwiMLURL = tw.String
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate calls rows: %w", err)
	}
	return out, total, nil
}

// --- delivery events ---

func (s *sqliteStore) SaveDeliveryEvent(ctx context.Context, e *DeliveryEvent) error {
	res, err := s.db.ExecContext(ctx, `
        INSERT INTO delivery_events (message_sid, call_sid, event_type, status, callback_sent, callback_response)
        VALUES (?, ?, ?, ?, ?, ?)`,
		nullableString(e.MessageSID), nullableString(e.CallSID),
		e.EventType, e.Status, e.CallbackSent, nullableString(e.CallbackResponse))
	if err != nil {
		return fmt.Errorf("insert delivery_event: %w", err)
	}
	id, _ := res.LastInsertId()
	e.ID = id
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	return nil
}

// --- callback logs ---

func (s *sqliteStore) SaveCallbackLog(ctx context.Context, l *CallbackLog) error {
	if l.AttemptNumber == 0 {
		l.AttemptNumber = 1
	}
	var statusCode sql.NullInt64
	if l.StatusCode != 0 {
		statusCode.Int64 = int64(l.StatusCode)
		statusCode.Valid = true
	}
	res, err := s.db.ExecContext(ctx, `
        INSERT INTO callback_logs (target_url, payload, status_code, response_body, attempt_number)
        VALUES (?, ?, ?, ?, ?)`,
		l.TargetURL, l.Payload, statusCode, nullableString(l.ResponseBody), l.AttemptNumber)
	if err != nil {
		return fmt.Errorf("insert callback_log: %w", err)
	}
	id, _ := res.LastInsertId()
	l.ID = id
	return nil
}

func (s *sqliteStore) GetCallbackLog(ctx context.Context, id int64) (*CallbackLog, error) {
	row := s.db.QueryRowContext(ctx, `
        SELECT id, target_url, payload, status_code, response_body, attempt_number, created_at
        FROM callback_logs WHERE id = ?`, id)
	l := &CallbackLog{}
	var status sql.NullInt64
	var resp sql.NullString
	if err := row.Scan(&l.ID, &l.TargetURL, &l.Payload, &status, &resp, &l.AttemptNumber, &l.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan callback_log: %w", err)
	}
	if status.Valid {
		l.StatusCode = int(status.Int64)
	}
	l.ResponseBody = resp.String
	return l, nil
}

func (s *sqliteStore) ListCallbackLogs(ctx context.Context, limit, offset int) ([]CallbackLog, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM callback_logs`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count callback_logs: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
        SELECT id, target_url, payload, status_code, response_body, attempt_number, created_at
        FROM callback_logs ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`,
		limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list callback_logs: %w", err)
	}
	defer rows.Close()

	var out []CallbackLog
	for rows.Next() {
		var l CallbackLog
		var status sql.NullInt64
		var resp sql.NullString
		if err := rows.Scan(&l.ID, &l.TargetURL, &l.Payload, &status, &resp, &l.AttemptNumber, &l.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan callback_logs row: %w", err)
		}
		if status.Valid {
			l.StatusCode = int(status.Int64)
		}
		l.ResponseBody = resp.String
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate callback_logs rows: %w", err)
	}
	return out, total, nil
}

// --- stats / clear ---

func (s *sqliteStore) Stats(ctx context.Context) (Stats, error) {
	var st Stats
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages`).Scan(&st.Messages); err != nil {
		return st, fmt.Errorf("count messages: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM calls`).Scan(&st.Calls); err != nil {
		return st, fmt.Errorf("count calls: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM callback_logs`).Scan(&st.Callbacks); err != nil {
		return st, fmt.Errorf("count callback_logs: %w", err)
	}
	return st, nil
}

func (s *sqliteStore) ClearMessages(ctx context.Context) (int, error) {
	return s.clearWithCascade(ctx, "messages", "message_sid")
}

func (s *sqliteStore) ClearCalls(ctx context.Context) (int, error) {
	return s.clearWithCascade(ctx, "calls", "call_sid")
}

// clearWithCascade deletes all rows from the named table and their related
// delivery_events (by sidColumn). Wraps the count + two deletes in a single
// transaction so a partial failure leaves no orphaned events.
//
// Table and sidColumn are caller-controlled identifiers, never user input,
// so the f-string assembly is safe — but linters can't tell, so we keep
// this as the only place strings are interpolated into SQL.
func (s *sqliteStore) clearWithCascade(ctx context.Context, table, sidColumn string) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin clear %s tx: %w", table, err)
	}
	defer func() { _ = tx.Rollback() }()

	var count int
	// table and sidColumn are caller-controlled identifiers (only used by
	// ClearMessages/ClearCalls with literal strings), never user input — so
	// the string concatenation below is safe.
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
		return 0, fmt.Errorf("count %s: %w", table, err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM delivery_events WHERE "+sidColumn+" IS NOT NULL"); err != nil { //nolint:gosec // identifiers are static
		return 0, fmt.Errorf("delete delivery_events for %s: %w", table, err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM "+table); err != nil { //nolint:gosec // identifiers are static
		return 0, fmt.Errorf("delete %s: %w", table, err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit clear %s: %w", table, err)
	}
	return count, nil
}

func (s *sqliteStore) ClearCallbacks(ctx context.Context) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM callback_logs`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count callback_logs: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM callback_logs`); err != nil {
		return 0, fmt.Errorf("delete callback_logs: %w", err)
	}
	return count, nil
}

func (s *sqliteStore) ClearAll(ctx context.Context) (ClearCounts, error) {
	var c ClearCounts
	m, err := s.ClearMessages(ctx)
	if err != nil {
		return c, err
	}
	c.Messages = m
	cc, err := s.ClearCalls(ctx)
	if err != nil {
		return c, err
	}
	c.Calls = cc
	cb, err := s.ClearCallbacks(ctx)
	if err != nil {
		return c, err
	}
	c.Callbacks = cb
	return c, nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
