package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"reasonix/internal/novel/db"
	"reasonix/internal/novel/domain"
)

// ArcRepo persists domain.Arc (the outlines table). Level / parent_id
// form the hierarchy: master → volume → chapter → blueprint.
type ArcRepo struct{ db *db.DB }

func NewArcRepo(d *db.DB) *ArcRepo { return &ArcRepo{db: d} }

func (r *ArcRepo) Create(ctx context.Context, a *domain.Arc) error {
	if a == nil {
		return fmt.Errorf("ArcRepo.Create: nil arc")
	}
	if a.ProjectID == "" {
		return fmt.Errorf("ArcRepo.Create: project_id is required")
	}
	if a.Level == "" {
		return fmt.Errorf("ArcRepo.Create: level is required")
	}
	if a.ID == "" {
		a.ID = newID()
	}
	if a.CreatedAt == 0 {
		a.CreatedAt = now()
	}
	a.ModifiedAt = now()

	meta, err := metadataToJSON(a.Metadata)
	if err != nil {
		return fmt.Errorf("ArcRepo.Create: metadata: %w", err)
	}
	const q = `INSERT INTO outlines (id, project_id, parent_id, level, title, summary, order_index, metadata, created_at, modified_at)
	           VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err = r.db.ExecContext(ctx, q,
		a.ID, a.ProjectID, nullString(a.ParentID), a.Level, a.Title, a.Summary, a.OrderIndex, meta, a.CreatedAt, a.ModifiedAt)
	if err != nil {
		return fmt.Errorf("ArcRepo.Create: %w", err)
	}
	return nil
}

func (r *ArcRepo) Get(ctx context.Context, id string) (*domain.Arc, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, project_id, parent_id, level, title, summary, order_index, metadata, created_at, modified_at
		 FROM outlines WHERE id = ? LIMIT 1`, id)
	return scanArc(row)
}

func (r *ArcRepo) List(ctx context.Context, projectID string) ([]*domain.Arc, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, project_id, parent_id, level, title, summary, order_index, metadata, created_at, modified_at
		 FROM outlines WHERE project_id = ?
		 ORDER BY level, order_index, created_at`, projectID)
	if err != nil {
		return nil, fmt.Errorf("ArcRepo.List: %w", err)
	}
	defer rows.Close()
	return scanArcs(rows)
}

func (r *ArcRepo) ListByParent(ctx context.Context, parentID string) ([]*domain.Arc, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, project_id, parent_id, level, title, summary, order_index, metadata, created_at, modified_at
		 FROM outlines WHERE parent_id = ?
		 ORDER BY order_index, created_at`, parentID)
	if err != nil {
		return nil, fmt.Errorf("ArcRepo.ListByParent: %w", err)
	}
	defer rows.Close()
	return scanArcs(rows)
}

func (r *ArcRepo) Search(ctx context.Context, projectID, query string) ([]*domain.Arc, error) {
	if query == "" {
		return r.List(ctx, projectID)
	}
	pat := "%" + query + "%"
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, project_id, parent_id, level, title, summary, order_index, metadata, created_at, modified_at
		 FROM outlines
		 WHERE project_id = ? AND (title LIKE ? OR summary LIKE ?)
		 ORDER BY level, order_index, created_at`, projectID, pat, pat)
	if err != nil {
		return nil, fmt.Errorf("ArcRepo.Search: %w", err)
	}
	defer rows.Close()
	return scanArcs(rows)
}

func (r *ArcRepo) Update(ctx context.Context, a *domain.Arc) error {
	if a == nil || a.ID == "" {
		return fmt.Errorf("ArcRepo.Update: id is required")
	}
	a.ModifiedAt = now()
	meta, err := metadataToJSON(a.Metadata)
	if err != nil {
		return fmt.Errorf("ArcRepo.Update: metadata: %w", err)
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE outlines SET parent_id = ?, level = ?, title = ?, summary = ?, order_index = ?, metadata = ?, modified_at = ?
		 WHERE id = ?`,
		nullString(a.ParentID), a.Level, a.Title, a.Summary, a.OrderIndex, meta, a.ModifiedAt, a.ID)
	if err != nil {
		return fmt.Errorf("ArcRepo.Update: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *ArcRepo) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM outlines WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("ArcRepo.Delete: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanArc(s rowScanner) (*domain.Arc, error) {
	var a domain.Arc
	var parent sql.NullString
	var meta string
	if err := s.Scan(&a.ID, &a.ProjectID, &parent, &a.Level, &a.Title, &a.Summary, &a.OrderIndex, &meta, &a.CreatedAt, &a.ModifiedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan arc: %w", err)
	}
	if parent.Valid {
		a.ParentID = parent.String
	}
	m, _ := metadataFromJSON(meta)
	a.Metadata = m
	return &a, nil
}

func scanArcs(rows *sql.Rows) ([]*domain.Arc, error) {
	var out []*domain.Arc
	for rows.Next() {
		var a domain.Arc
		var parent sql.NullString
		var meta string
		if err := rows.Scan(&a.ID, &a.ProjectID, &parent, &a.Level, &a.Title, &a.Summary, &a.OrderIndex, &meta, &a.CreatedAt, &a.ModifiedAt); err != nil {
			return nil, fmt.Errorf("scan arcs: %w", err)
		}
		if parent.Valid {
			a.ParentID = parent.String
		}
		m, _ := metadataFromJSON(meta)
		a.Metadata = m
		out = append(out, &a)
	}
	return out, rows.Err()
}

// nullString returns a valid sql.NullString for empty inputs.
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
