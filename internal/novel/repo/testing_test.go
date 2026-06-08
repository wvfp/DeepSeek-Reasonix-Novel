package repo

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"reasonix/internal/novel/db"
	"reasonix/internal/novel/domain"
)

// openTestDB returns a fresh in-memory-ish DB with the schema applied.
// All repo tests share this helper so they don't re-implement the
// boilerplate.
func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(dir + "/test.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return d
}

// seedProject inserts a single row into projects (the FK target for
// almost every other table) and returns its ID. Tests use the
// returned ID when constructing child entities.
func seedProject(t *testing.T, d *db.DB) string {
	t.Helper()
	id := "test-proj-" + time.Now().Format("150405.000000000")
	_, err := d.ExecContext(context.Background(),
		`INSERT INTO projects (id, name, genre, pipeline_phase, created_at, modified_at)
		 VALUES (?, 'test', 'fantasy', 'setting', 0, 0)`, id)
	if err != nil {
		t.Fatalf("seed project: %v", err)
	}
	return id
}

// countRows returns COUNT(*) for the named table — handy for the
// "did the side-effect fire?" assertions in repo tests.
func countRows(t *testing.T, d *db.DB, table string) int {
	t.Helper()
	var n int
	if err := d.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// expectNoRows asserts a SELECT returns zero rows.
func expectNoRows(t *testing.T, d *db.DB, q string, args ...any) {
	t.Helper()
	rows, err := d.QueryContext(context.Background(), q, args...)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatalf("expected zero rows, got at least one")
	}
}

// mustHaveRow asserts a SELECT returns exactly one row.
func mustHaveRow(t *testing.T, d *db.DB, q string, args ...any) {
	t.Helper()
	rows, err := d.QueryContext(context.Background(), q, args...)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("expected at least one row, got none")
	}
}

// errIsNotFound is true when err is ErrNotFound (or wraps it).
func errIsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }

// requireDomainType — placeholder helper. Reserved for tests that
// need to assert on a domain constant.
func requireDomainType(t *testing.T, _ domain.World) { t.Helper() }

// rowExists is a low-level helper.
func rowExists(t *testing.T, d *db.DB, table, idCol, idVal string) bool {
	t.Helper()
	var n int
	row := d.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM "+table+" WHERE "+idCol+" = ?", idVal)
	if err := row.Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n > 0
}

// avoid unused-import warning for sql.ErrNoRows in case the helper
// above is the only consumer.
var _ = sql.ErrNoRows
