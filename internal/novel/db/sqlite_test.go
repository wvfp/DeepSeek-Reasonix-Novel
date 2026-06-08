package db

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestOpenClose_InTempDir(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	d, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open(%q): %v", dbPath, err)
	}
	if d == nil {
		t.Fatal("Open returned nil DB")
	}
	if got, want := d.Path(), dbPath; got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
	if _, err := d.ExecContext(context.Background(), "CREATE TABLE t(x INTEGER)"); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestOpen_EmptyPath(t *testing.T) {
	if _, err := Open(""); err == nil {
		t.Fatal("Open(\"\") should fail, got nil error")
	}
}

func TestOpen_ReopenExistingFile(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "reopen.db")

	first, err := Open(dbPath)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if _, err := first.ExecContext(context.Background(), "CREATE TABLE a(id INTEGER)"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := first.ExecContext(context.Background(), "INSERT INTO a(id) VALUES (1)"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	second, err := Open(dbPath)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	var count int
	if err := second.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM a").Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row after reopen, got %d", count)
	}
}

func TestTx_Commit(t *testing.T) {
	d := openTestDB(t)

	if err := d.Tx(func(tx *sql.Tx) error {
		if _, err := tx.Exec("CREATE TABLE tx_commit(x INTEGER)"); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT INTO tx_commit(x) VALUES (1)"); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("Tx commit: %v", err)
	}

	var count int
	if err := d.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM tx_commit").Scan(&count); err != nil {
		t.Fatalf("table check: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row in tx_commit, got %d", count)
	}
}

func TestTx_Rollback(t *testing.T) {
	d := openTestDB(t)

	sentinel := errors.New("intentional rollback")
	err := d.Tx(func(tx *sql.Tx) error {
		if _, err := tx.Exec("CREATE TABLE tx_rb(x INTEGER)"); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT INTO tx_rb(x) VALUES (1)"); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Tx returned err = %v, want %v", err, sentinel)
	}

	// Table must NOT exist after rollback.
	row := d.QueryRowContext(context.Background(),
		"SELECT name FROM sqlite_master WHERE type='table' AND name='tx_rb'")
	var name string
	if err := row.Scan(&name); err == nil {
		t.Errorf("table tx_rb should not exist after rollback, got %q", name)
	}
}

func TestTx_NilDB(t *testing.T) {
	var d *DB
	if err := d.Tx(func(*sql.Tx) error { return nil }); err == nil {
		t.Fatal("Tx on nil DB should fail, got nil error")
	}
}

func openTestDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	d, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}
