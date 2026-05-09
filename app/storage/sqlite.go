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
	// foreign_keys(1) enables FK enforcement (incl. ON DELETE CASCADE), which
	// SQLite leaves OFF by default. Required for tag link cleanup on delete.
	if isMemoryDSN(path) {
		// memory + shared cache so multiple opens of the same name share state
		// (we still cap MaxOpenConns at 1 for safety).
		return "file:" + path + "?cache=shared&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	}
	return fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)",
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
        SELECT id, message_sid, provider, from_number, to_number, body, status, callback_url, is_read, created_at, updated_at
        FROM messages WHERE message_sid = ?`, sid)
	m := &Message{}
	var body, callbackURL sql.NullString
	if err := row.Scan(&m.ID, &m.SID, &m.Provider, &m.From, &m.To, &body, &m.Status, &callbackURL, &m.IsRead, &m.CreatedAt, &m.UpdatedAt); err != nil {
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
	return s.SearchMessages(ctx, "", "", nil, limit, offset)
}

func (s *sqliteStore) SearchMessages(ctx context.Context, q, status string, tags []string, limit, offset int) ([]Message, int, error) {
	where, args := messageFilter(q, status, tags)
	var total int
	// where is a static template ("" or " WHERE … LIKE ? AND status = ?"); only ?-placeholders, user values go through args.
	countSQL := "SELECT COUNT(*) FROM messages m" + where
	if err := s.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count messages: %w", err)
	}

	// only ?-placeholders are concatenated; user input is bound via args.
	listSQL := `SELECT m.id, m.message_sid, m.provider, m.from_number, m.to_number, m.body, m.status, m.callback_url, m.is_read, m.created_at, m.updated_at` + //nolint:gosec // see comment above
		` FROM messages m` + where + ` ORDER BY m.created_at DESC, m.id DESC LIMIT ? OFFSET ?`
	rows, err := s.db.QueryContext(ctx, listSQL, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()

	var out []Message
	for rows.Next() {
		var m Message
		var body, cb sql.NullString
		if err := rows.Scan(&m.ID, &m.SID, &m.Provider, &m.From, &m.To, &body, &m.Status, &cb, &m.IsRead, &m.CreatedAt, &m.UpdatedAt); err != nil {
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

// messageFilter builds the WHERE clause + args for SearchMessages. q searches
// m.from_number / m.to_number / m.body via LIKE %q%. status is exact match.
// tags AND-filters (record must have all listed tags). Empty fields are skipped.
//
// All column references are qualified with `m.` since the FROM clause uses
// `messages m` (so the EXISTS subquery on message_tags can reference m.id).
func messageFilter(q, status string, tags []string) (where string, args []any) {
	var clauses []string
	if q != "" {
		clauses = append(clauses, "(m.from_number LIKE ? OR m.to_number LIKE ? OR m.body LIKE ?)")
		like := "%" + q + "%"
		args = append(args, like, like, like)
	}
	if status != "" {
		clauses = append(clauses, "m.status = ?")
		args = append(args, status)
	}
	for _, name := range tags {
		clauses = append(clauses,
			"EXISTS (SELECT 1 FROM message_tags mt JOIN tags t ON mt.tag_id = t.id WHERE mt.message_id = m.id AND t.name = ?)")
		args = append(args, name)
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

//nolint:dupl // structurally mirrors MarkCallRead/DeleteCall but operates on a different table
func (s *sqliteStore) MarkMessageRead(ctx context.Context, sid string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE messages SET is_read = 1 WHERE message_sid = ?`, sid)
	if err != nil {
		return fmt.Errorf("mark message read: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// Row may exist but already had is_read=1. Distinguish missing vs already-read
		// with a follow-up SELECT — keeps the method idempotent.
		var exists int
		checkErr := s.db.QueryRowContext(ctx, `SELECT 1 FROM messages WHERE message_sid = ?`, sid).Scan(&exists)
		if errors.Is(checkErr, sql.ErrNoRows) {
			return ErrNotFound
		}
		if checkErr != nil {
			return fmt.Errorf("check message exists: %w", checkErr)
		}
	}
	return nil
}

func (s *sqliteStore) DeleteMessage(ctx context.Context, sid string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, evErr := tx.ExecContext(ctx, `DELETE FROM delivery_events WHERE message_sid = ?`, sid); evErr != nil {
		return fmt.Errorf("delete delivery_events: %w", evErr)
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE message_sid = ?`, sid)
	if err != nil {
		return fmt.Errorf("delete message: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("commit delete: %w", commitErr)
	}
	return nil
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
        SELECT id, call_sid, provider, from_number, to_number, status, callback_url, twiml_url, is_read, created_at, updated_at
        FROM calls WHERE call_sid = ?`, sid)
	c := &Call{}
	var cb, tw sql.NullString
	if err := row.Scan(&c.ID, &c.SID, &c.Provider, &c.From, &c.To, &c.Status, &cb, &tw, &c.IsRead, &c.CreatedAt, &c.UpdatedAt); err != nil {
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
	return s.SearchCalls(ctx, "", "", nil, limit, offset)
}

func (s *sqliteStore) SearchCalls(ctx context.Context, q, status string, tags []string, limit, offset int) ([]Call, int, error) {
	where, args := callFilter(q, status, tags)
	var total int
	countSQL := "SELECT COUNT(*) FROM calls c" + where
	if err := s.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count calls: %w", err)
	}

	// only ?-placeholders are concatenated; user input is bound via args.
	listSQL := `SELECT c.id, c.call_sid, c.provider, c.from_number, c.to_number, c.status, c.callback_url, c.twiml_url, c.is_read, c.created_at, c.updated_at` + //nolint:gosec // see comment above
		` FROM calls c` + where + ` ORDER BY c.created_at DESC, c.id DESC LIMIT ? OFFSET ?`
	rows, err := s.db.QueryContext(ctx, listSQL, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list calls: %w", err)
	}
	defer rows.Close()

	var out []Call
	for rows.Next() {
		var c Call
		var cb, tw sql.NullString
		if err := rows.Scan(&c.ID, &c.SID, &c.Provider, &c.From, &c.To, &c.Status, &cb, &tw, &c.IsRead, &c.CreatedAt, &c.UpdatedAt); err != nil {
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

// callFilter mirrors messageFilter but without the body column. References use
// the `c` alias so EXISTS subqueries on call_tags can join c.id.
func callFilter(q, status string, tags []string) (where string, args []any) {
	var clauses []string
	if q != "" {
		clauses = append(clauses, "(c.from_number LIKE ? OR c.to_number LIKE ?)")
		like := "%" + q + "%"
		args = append(args, like, like)
	}
	if status != "" {
		clauses = append(clauses, "c.status = ?")
		args = append(args, status)
	}
	for _, name := range tags {
		clauses = append(clauses,
			"EXISTS (SELECT 1 FROM call_tags ct JOIN tags t ON ct.tag_id = t.id WHERE ct.call_id = c.id AND t.name = ?)")
		args = append(args, name)
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

//nolint:dupl // structurally mirrors MarkMessageRead/DeleteMessage but operates on a different table
func (s *sqliteStore) MarkCallRead(ctx context.Context, sid string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE calls SET is_read = 1 WHERE call_sid = ?`, sid)
	if err != nil {
		return fmt.Errorf("mark call read: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var exists int
		checkErr := s.db.QueryRowContext(ctx, `SELECT 1 FROM calls WHERE call_sid = ?`, sid).Scan(&exists)
		if errors.Is(checkErr, sql.ErrNoRows) {
			return ErrNotFound
		}
		if checkErr != nil {
			return fmt.Errorf("check call exists: %w", checkErr)
		}
	}
	return nil
}

func (s *sqliteStore) DeleteCall(ctx context.Context, sid string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, evErr := tx.ExecContext(ctx, `DELETE FROM delivery_events WHERE call_sid = ?`, sid); evErr != nil {
		return fmt.Errorf("delete delivery_events: %w", evErr)
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM calls WHERE call_sid = ?`, sid)
	if err != nil {
		return fmt.Errorf("delete call: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("commit delete: %w", commitErr)
	}
	return nil
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

// StatusCounts returns per-status row counts for the named record type.
// recordType must be "messages" or "calls"; any other value returns an error.
func (s *sqliteStore) StatusCounts(ctx context.Context, recordType string) (map[string]int, error) {
	var table string
	switch recordType {
	case "messages":
		table = "messages"
	case "calls":
		table = "calls"
	default:
		return nil, fmt.Errorf("unknown record type: %q", recordType)
	}
	// Table is whitelisted above, safe to interpolate.
	rows, err := s.db.QueryContext(ctx, "SELECT status, COUNT(*) FROM "+table+" GROUP BY status") //nolint:gosec // identifier whitelisted
	if err != nil {
		return nil, fmt.Errorf("status counts: %w", err)
	}
	defer rows.Close()

	out := map[string]int{}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return nil, fmt.Errorf("scan status count: %w", err)
		}
		out[status] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate status counts: %w", err)
	}
	return out, nil
}

// CallbackSummary aggregates callback_logs entries whose JSON payload references
// the given SID. Verified the payload IS JSON at app/callback/dispatcher.go:294
// (encodePayloadAsJSON before SaveCallbackLog) — the LIKE pattern is reliable.
//
// We don't know whether sid is a message or call SID, so we match either field.
func (s *sqliteStore) CallbackSummary(ctx context.Context, sid string) (CallbackSummary, error) {
	var sum CallbackSummary
	msgPattern := `%"MessageSid":"` + sid + `"%`
	callPattern := `%"CallSid":"` + sid + `"%`

	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM callback_logs WHERE payload LIKE ? OR payload LIKE ?`,
		msgPattern, callPattern,
	).Scan(&sum.Count); err != nil {
		return sum, fmt.Errorf("count callback_logs: %w", err)
	}
	if sum.Count == 0 {
		return sum, nil
	}

	// Most recent attempt's status + body (any status, including 0 / failures).
	var status sql.NullInt64
	var body sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT status_code, response_body FROM callback_logs
         WHERE payload LIKE ? OR payload LIKE ?
         ORDER BY id DESC LIMIT 1`,
		msgPattern, callPattern,
	).Scan(&status, &body)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return sum, fmt.Errorf("last callback_log: %w", err)
	}
	if status.Valid {
		sum.LastStatus = int(status.Int64)
	}
	sum.LastResponseBody = body.String
	return sum, nil
}

// --- tags ---

// SetMessageTags replaces the tag set for a single message. Names should be
// pre-trimmed and deduplicated by the caller; this method tolerates an empty
// slice (clears tags) but does not lowercase or otherwise transform names.
func (s *sqliteStore) SetMessageTags(ctx context.Context, messageID int64, names []string) error {
	return s.setTags(ctx, "message_tags", "message_id", messageID, names)
}

// SetCallTags is the call-side counterpart of SetMessageTags.
func (s *sqliteStore) SetCallTags(ctx context.Context, callID int64, names []string) error {
	return s.setTags(ctx, "call_tags", "call_id", callID, names)
}

// setTags handles the common write path for both messages and calls. linkTable
// and refColumn are caller-controlled identifiers — never user input — so
// string assembly is safe. recordID is bound as a placeholder.
func (s *sqliteStore) setTags(ctx context.Context, linkTable, refColumn string, recordID int64, names []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin set tags tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Clear existing links for this record first.
	if _, err := tx.ExecContext(ctx, "DELETE FROM "+linkTable+" WHERE "+refColumn+" = ?", recordID); err != nil { //nolint:gosec // identifiers whitelisted
		return fmt.Errorf("delete %s rows: %w", linkTable, err)
	}

	for _, name := range names {
		if name == "" {
			continue
		}
		// Upsert tag by name; SQLite's INSERT OR IGNORE leaves an existing row
		// alone, then SELECT picks up the id either way.
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO tags (name) VALUES (?)`, name); err != nil {
			return fmt.Errorf("upsert tag %q: %w", name, err)
		}
		var tagID int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM tags WHERE name = ?`, name).Scan(&tagID); err != nil {
			return fmt.Errorf("read tag id %q: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO "+linkTable+" ("+refColumn+", tag_id) VALUES (?, ?)", recordID, tagID); err != nil { //nolint:gosec // identifiers whitelisted
			return fmt.Errorf("link tag: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit set tags: %w", err)
	}
	return nil
}

func (s *sqliteStore) MessageTags(ctx context.Context, messageID int64) ([]string, error) {
	return s.tagsForRecord(ctx, "message_tags", "message_id", messageID)
}

func (s *sqliteStore) CallTags(ctx context.Context, callID int64) ([]string, error) {
	return s.tagsForRecord(ctx, "call_tags", "call_id", callID)
}

func (s *sqliteStore) tagsForRecord(ctx context.Context, linkTable, refColumn string, recordID int64) ([]string, error) {
	// linkTable / refColumn are caller-controlled identifiers (whitelisted in
	// MessageTags / CallTags), never user input — bound recordID goes via ?.
	q := "SELECT t.name FROM tags t JOIN " + linkTable + //nolint:gosec // see comment
		" l ON l.tag_id = t.id WHERE l." + refColumn + " = ? ORDER BY t.name ASC"
	rows, err := s.db.QueryContext(ctx, q, recordID)
	if err != nil {
		return nil, fmt.Errorf("read tags: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tags: %w", err)
	}
	return out, nil
}

// ListTagNames returns tag names attached to at least one record of the given
// type. recordType is "messages" or "calls"; any other value returns an error.
// Empty slice if no records of that type are tagged.
func (s *sqliteStore) ListTagNames(ctx context.Context, recordType string) ([]string, error) {
	var linkTable string
	switch recordType {
	case "messages":
		linkTable = "message_tags"
	case "calls":
		linkTable = "call_tags"
	default:
		return nil, fmt.Errorf("unknown record type: %q", recordType)
	}
	// linkTable is whitelisted above; no user input concatenated.
	q := "SELECT t.name FROM tags t " + //nolint:gosec // identifier whitelisted
		"WHERE EXISTS (SELECT 1 FROM " + linkTable + " WHERE tag_id = t.id) " +
		"ORDER BY t.name ASC"
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list tag names: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan tag name: %w", err)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tag names: %w", err)
	}
	return out, nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// --- pruning ---

func (s *sqliteStore) PruneMessagesByCount(ctx context.Context, limit int) (int, error) {
	return s.pruneByCount(ctx, "messages", "message_sid", limit)
}

func (s *sqliteStore) PruneCallsByCount(ctx context.Context, limit int) (int, error) {
	return s.pruneByCount(ctx, "calls", "call_sid", limit)
}

func (s *sqliteStore) PruneMessagesByAge(ctx context.Context, cutoff time.Time) (int, error) {
	return s.pruneByAge(ctx, "messages", "message_sid", cutoff)
}

func (s *sqliteStore) PruneCallsByAge(ctx context.Context, cutoff time.Time) (int, error) {
	return s.pruneByAge(ctx, "calls", "call_sid", cutoff)
}

// pruneByCount deletes oldest-first (ordered by created_at, id) until the
// total row count in the named table is at or below limit. table and
// sidColumn are caller-controlled identifiers (only ever literal strings
// from PruneMessages*/PruneCalls*), never user input — same convention as
// clearWithCascade.
func (s *sqliteStore) pruneByCount(ctx context.Context, table, sidColumn string, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin prune %s tx: %w", table, err)
	}
	defer func() { _ = tx.Rollback() }()

	// Collect SIDs of rows to delete: ordered newest-first, skip the first
	// `limit`, take everything that follows. SQLite needs LIMIT -1 to mean
	// "no limit" when an OFFSET is set.
	query := "SELECT " + sidColumn + " FROM " + table +
		" ORDER BY created_at DESC, id DESC LIMIT -1 OFFSET ?"
	sids, err := selectPruneSIDs(ctx, tx, query, limit)
	if err != nil {
		return 0, fmt.Errorf("select prune candidates from %s: %w", table, err)
	}
	if len(sids) == 0 {
		return 0, nil
	}

	deleted, err := deletePruneBatch(ctx, tx, table, sidColumn, sids)
	if err != nil {
		return 0, err
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return 0, fmt.Errorf("commit prune %s: %w", table, commitErr)
	}
	return deleted, nil
}

// pruneByAge deletes rows with created_at < cutoff.
func (s *sqliteStore) pruneByAge(ctx context.Context, table, sidColumn string, cutoff time.Time) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin prune-age %s tx: %w", table, err)
	}
	defer func() { _ = tx.Rollback() }()

	query := "SELECT " + sidColumn + " FROM " + table + " WHERE created_at < ?"
	sids, err := selectPruneSIDs(ctx, tx, query, cutoff)
	if err != nil {
		return 0, fmt.Errorf("select expired %s: %w", table, err)
	}
	if len(sids) == 0 {
		return 0, nil
	}

	deleted, err := deletePruneBatch(ctx, tx, table, sidColumn, sids)
	if err != nil {
		return 0, err
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return 0, fmt.Errorf("commit prune-age %s: %w", table, commitErr)
	}
	return deleted, nil
}

// selectPruneSIDs runs the given SELECT (which must project a single SID
// column as its only output) and collects the SIDs into a slice. Encapsulates
// the rows.Next/Scan/Close pattern so callers can use a single error path.
//
// The query is assembled by callers from caller-controlled literal table /
// column identifiers (never user input) plus parameterized placeholders for
// the actual values, matching the convention used by clearWithCascade.
func selectPruneSIDs(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query prune sids: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var sids []string
	for rows.Next() {
		var sid string
		if scanErr := rows.Scan(&sid); scanErr != nil {
			return nil, fmt.Errorf("scan prune sid: %w", scanErr)
		}
		sids = append(sids, sid)
	}
	if iterErr := rows.Err(); iterErr != nil {
		return nil, fmt.Errorf("iterate prune sids: %w", iterErr)
	}
	return sids, nil
}

// deletePruneBatch deletes the given SIDs from `table` (using sidColumn as
// the matching column on both the main table and delivery_events). Caller
// owns the transaction. Returns the number of main-table rows deleted.
func deletePruneBatch(ctx context.Context, tx *sql.Tx, table, sidColumn string, sids []string) (int, error) {
	placeholders := strings.Repeat("?,", len(sids))
	placeholders = placeholders[:len(placeholders)-1] // trim trailing comma
	args := make([]any, 0, len(sids))
	for _, sid := range sids {
		args = append(args, sid)
	}

	//nolint:gosec // G202: identifiers are caller-controlled literals; SID values are parameterized
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM delivery_events WHERE "+sidColumn+" IN ("+placeholders+")", args...); err != nil {
		return 0, fmt.Errorf("delete delivery_events for prune: %w", err)
	}
	//nolint:gosec // G202: identifiers are caller-controlled literals; SID values are parameterized
	res, err := tx.ExecContext(ctx,
		"DELETE FROM "+table+" WHERE "+sidColumn+" IN ("+placeholders+")", args...)
	if err != nil {
		return 0, fmt.Errorf("delete from %s for prune: %w", table, err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
