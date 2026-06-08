// Package db provides SQLite database access for the novel subsystem.
//
// It uses modernc.org/sqlite, a pure-Go (CGO_ENABLED=0) build of SQLite, so
// the binary can be cross-compiled on any platform without a C toolchain.
package db

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, registers itself as "sqlite"
)

// DB wraps *sql.DB and the path on disk so callers can re-open the same
// database or read the file path via Path() for diagnostics / migrations.
type DB struct {
	*sql.DB
	path string
}

// Open opens (or creates) a SQLite database at path. The modernc.org/sqlite
// driver is registered under the name "sqlite". Foreign keys and WAL are not
// enabled here — that is the responsibility of Migrate() / schema setup so
// tests can open transient in-memory databases without pragma side effects.
func Open(path string) (*DB, error) {
	if path == "" {
		return nil, fmt.Errorf("db.Open: path must not be empty")
	}
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("db.Open: sql.Open(%q): %w", path, err)
	}
	if err := conn.Ping(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("db.Open: ping %q: %w", path, err)
	}
	return &DB{DB: conn, path: path}, nil
}

// Close releases the underlying *sql.DB. Safe to call multiple times by
// relying on *sql.DB's behaviour (it returns an error on double-close, which
// we intentionally swallow only when the handle is already nil).
func (d *DB) Close() error {
	if d == nil || d.DB == nil {
		return nil
	}
	err := d.DB.Close()
	d.DB = nil
	return err
}

// Path returns the file path the database was opened with. Useful for
// diagnostic output and for the schema migration tools.
func (d *DB) Path() string {
	if d == nil {
		return ""
	}
	return d.path
}

// Tx runs fn inside a transaction. The transaction is committed if fn
// returns nil, otherwise it is rolled back. The *sql.Tx is passed to fn so
// callers must not call d methods that would start another transaction on
// the same connection.
func (d *DB) Tx(fn func(*sql.Tx) error) error {
	if d == nil || d.DB == nil {
		return fmt.Errorf("db.Tx: database is not open")
	}
	tx, err := d.DB.Begin()
	if err != nil {
		return fmt.Errorf("db.Tx: begin: %w", err)
	}
	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("db.Tx: rollback after error %v: %w", err, rbErr)
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("db.Tx: commit: %w", err)
	}
	return nil
}

// ExecContext is a thin wrapper that ensures a non-nil database before
// delegating. Provided as a convenience for tools that need to run a single
// statement without spinning up a transaction.
func (d *DB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if d == nil || d.DB == nil {
		return nil, fmt.Errorf("db.ExecContext: database is not open")
	}
	return d.DB.ExecContext(ctx, query, args...)
}
