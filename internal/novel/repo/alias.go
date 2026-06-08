package repo

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"reasonix/internal/novel/db"
	"reasonix/internal/novel/domain"
)

// AliasRepo persists domain.Alias (aliases table). The chapter-write
// tool auto-registers a name (e.g. a character name) as an alias for
// its entity so the same person is never rendered under two names.
type AliasRepo struct{ db *db.DB }

func NewAliasRepo(d *db.DB) *AliasRepo { return &AliasRepo{db: d} }

func (r *AliasRepo) Create(ctx context.Context, a *domain.Alias) error {
	if a == nil {
		return fmt.Errorf("AliasRepo.Create: nil alias")
	}
	if a.ProjectID == "" || a.EntityID == "" || a.Alias == "" {
		return fmt.Errorf("AliasRepo.Create: project_id, entity_id, alias are required")
	}
	if a.ID == "" {
		a.ID = newID()
	}
	if a.CreatedAt == 0 {
		a.CreatedAt = now()
	}
	const q = `INSERT INTO aliases (id, project_id, entity_type, entity_id, alias, created_at)
	           VALUES (?, ?, ?, ?, ?, ?)`
	_, err := r.db.ExecContext(ctx, q, a.ID, a.ProjectID, a.EntityType, a.EntityID, a.Alias, a.CreatedAt)
	if err != nil {
		return fmt.Errorf("AliasRepo.Create: %w", err)
	}
	return nil
}

// CreateBatch inserts a slice of aliases in a single transaction. The
// chapter_write tool uses this to register the subject + object of
// every KG edge it produces.
func (r *AliasRepo) CreateBatch(ctx context.Context, aliases []*domain.Alias) error {
	if len(aliases) == 0 {
		return nil
	}
	if err := r.db.Tx(func(tx *sql.Tx) error {
		const q = `INSERT INTO aliases (id, project_id, entity_type, entity_id, alias, created_at)
		           VALUES (?, ?, ?, ?, ?, ?)`
		stmt, err := tx.PrepareContext(ctx, q)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, a := range aliases {
			if a.Alias == "" {
				continue
			}
			if a.ID == "" {
				a.ID = newID()
			}
			if a.CreatedAt == 0 {
				a.CreatedAt = now()
			}
			if _, err := stmt.ExecContext(ctx, a.ID, a.ProjectID, a.EntityType, a.EntityID, a.Alias, a.CreatedAt); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("AliasRepo.CreateBatch: %w", err)
	}
	return nil
}

func (r *AliasRepo) ListByEntity(ctx context.Context, entityID string) ([]*domain.Alias, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, project_id, entity_type, entity_id, alias, created_at
		 FROM aliases WHERE entity_id = ? ORDER BY alias`, entityID)
	if err != nil {
		return nil, fmt.Errorf("AliasRepo.ListByEntity: %w", err)
	}
	defer rows.Close()
	return scanAliases(rows)
}

// Resolve looks up the canonical entity_id for a free-text alias.
// Returns ("", false) on miss.
func (r *AliasRepo) Resolve(ctx context.Context, projectID, alias string) (string, bool, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT entity_id FROM aliases
		 WHERE project_id = ? AND alias = ? LIMIT 1`, projectID, alias)
	var id string
	if err := row.Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, fmt.Errorf("AliasRepo.Resolve: %w", err)
	}
	return id, true, nil
}

// AliasesForNames takes a list of human-readable names and returns the
// subset that already have an alias row (so callers can skip them).
func (r *AliasRepo) AliasesForNames(ctx context.Context, projectID string, names []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(names) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(names))
	args := make([]any, 0, len(names)+1)
	args = append(args, projectID)
	for i, n := range names {
		placeholders[i] = "?"
		args = append(args, n)
	}
	q := `SELECT alias FROM aliases WHERE project_id = ? AND alias IN (` + strings.Join(placeholders, ",") + `)`
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("AliasRepo.AliasesForNames: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out[s] = true
	}
	return out, rows.Err()
}

func scanAliases(rows *sql.Rows) ([]*domain.Alias, error) {
	var out []*domain.Alias
	for rows.Next() {
		var a domain.Alias
		if err := rows.Scan(&a.ID, &a.ProjectID, &a.EntityType, &a.EntityID, &a.Alias, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan aliases: %w", err)
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}
