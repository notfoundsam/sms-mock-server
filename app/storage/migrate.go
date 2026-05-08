package storage

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrate applies any pending SQL migrations from the embedded migrations/ dir.
// Migration files must be named NNN_description.sql; they are applied in numeric order
// and tracked in schema_migrations(version, applied_at).
func migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
        CREATE TABLE IF NOT EXISTS schema_migrations (
            version INTEGER PRIMARY KEY,
            applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
        )`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := readAppliedVersions(ctx, db)
	if err != nil {
		return err
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	type migration struct {
		version int
		name    string
	}
	var pending []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		// expect NNN_description.sql
		parts := strings.SplitN(e.Name(), "_", 2)
		if len(parts) < 2 {
			return fmt.Errorf("invalid migration filename: %s", e.Name())
		}
		v, err := strconv.Atoi(parts[0])
		if err != nil {
			return fmt.Errorf("invalid migration version %q: %w", parts[0], err)
		}
		if applied[v] {
			continue
		}
		pending = append(pending, migration{version: v, name: e.Name()})
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].version < pending[j].version })

	for _, m := range pending {
		body, err := fs.ReadFile(migrationsFS, "migrations/"+m.name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", m.name, err)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", m.name, err)
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", m.name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES (?)`, m.version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", m.name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", m.name, err)
		}
	}

	return nil
}

// readAppliedVersions returns the set of migration versions already applied,
// extracted into its own function so rows.Close() can be deferred (the parent
// function does too much else for a single defer to be appropriate).
func readAppliedVersions(ctx context.Context, db *sql.DB) (map[int]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	applied := map[int]bool{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scan schema_migrations: %w", err)
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schema_migrations: %w", err)
	}
	return applied, nil
}
