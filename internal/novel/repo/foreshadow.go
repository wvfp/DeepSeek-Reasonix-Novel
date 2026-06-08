package repo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"reasonix/internal/novel/db"
	"reasonix/internal/novel/domain"
)

// ForeshadowRepo persists domain.Foreshadow (foreshadows table). The
// status field moves through planted → developing → resolved (or
// abandoned).
type ForeshadowRepo struct{ db *db.DB }

func NewForeshadowRepo(d *db.DB) *ForeshadowRepo { return &ForeshadowRepo{db: d} }

func (r *ForeshadowRepo) Create(ctx context.Context, f *domain.Foreshadow) error {
	if f == nil {
		return fmt.Errorf("ForeshadowRepo.Create: nil")
	}
	if f.ProjectID == "" || f.Description == "" {
		return fmt.Errorf("ForeshadowRepo.Create: project_id, description are required")
	}
	if f.Status == "" {
		f.Status = domain.ForeshadowPlanted
	}
	if f.Importance == "" {
		f.Importance = domain.ForeshadowMinor
	}
	if f.ID == "" {
		f.ID = newID()
	}
	if f.CreatedAt == 0 {
		f.CreatedAt = now()
	}
	f.ModifiedAt = now()

	kw, _ := json.Marshal(f.Keywords)
	const q = `INSERT INTO foreshadows (id, project_id, planted_chapter_id, resolved_chapter_id, description, keywords, status, importance, created_at, modified_at)
	           VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := r.db.ExecContext(ctx, q,
		f.ID, f.ProjectID, nullString(f.PlantedChapterID), nullString(f.ResolvedChapterID),
		f.Description, string(kw), f.Status, f.Importance, f.CreatedAt, f.ModifiedAt)
	if err != nil {
		return fmt.Errorf("ForeshadowRepo.Create: %w", err)
	}
	return nil
}

func (r *ForeshadowRepo) List(ctx context.Context, projectID string) ([]*domain.Foreshadow, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, project_id, planted_chapter_id, resolved_chapter_id, description, keywords, status, importance, created_at, modified_at
		 FROM foreshadows WHERE project_id = ? ORDER BY importance, created_at`, projectID)
	if err != nil {
		return nil, fmt.Errorf("ForeshadowRepo.List: %w", err)
	}
	defer rows.Close()
	return scanForeshadows(rows)
}

func (r *ForeshadowRepo) Get(ctx context.Context, id string) (*domain.Foreshadow, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, project_id, planted_chapter_id, resolved_chapter_id, description, keywords, status, importance, created_at, modified_at
		 FROM foreshadows WHERE id = ? LIMIT 1`, id)
	return scanForeshadow(row)
}

func (r *ForeshadowRepo) Update(ctx context.Context, f *domain.Foreshadow) error {
	if f == nil || f.ID == "" {
		return fmt.Errorf("ForeshadowRepo.Update: id required")
	}
	f.ModifiedAt = now()
	kw, _ := json.Marshal(f.Keywords)
	res, err := r.db.ExecContext(ctx,
		`UPDATE foreshadows SET planted_chapter_id = ?, resolved_chapter_id = ?, description = ?, keywords = ?, status = ?, importance = ?, modified_at = ?
		 WHERE id = ?`,
		nullString(f.PlantedChapterID), nullString(f.ResolvedChapterID), f.Description, string(kw), f.Status, f.Importance, f.ModifiedAt, f.ID)
	if err != nil {
		return fmt.Errorf("ForeshadowRepo.Update: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *ForeshadowRepo) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM foreshadows WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("ForeshadowRepo.Delete: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanForeshadow(s rowScanner) (*domain.Foreshadow, error) {
	var f domain.Foreshadow
	var planted, resolved sql.NullString
	var kw string
	if err := s.Scan(&f.ID, &f.ProjectID, &planted, &resolved, &f.Description, &kw, &f.Status, &f.Importance, &f.CreatedAt, &f.ModifiedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan foreshadow: %w", err)
	}
	if planted.Valid {
		f.PlantedChapterID = planted.String
	}
	if resolved.Valid {
		f.ResolvedChapterID = resolved.String
	}
	if kw != "" {
		_ = json.Unmarshal([]byte(kw), &f.Keywords)
	}
	return &f, nil
}

func scanForeshadows(rows *sql.Rows) ([]*domain.Foreshadow, error) {
	var out []*domain.Foreshadow
	for rows.Next() {
		var f domain.Foreshadow
		var planted, resolved sql.NullString
		var kw string
		if err := rows.Scan(&f.ID, &f.ProjectID, &planted, &resolved, &f.Description, &kw, &f.Status, &f.Importance, &f.CreatedAt, &f.ModifiedAt); err != nil {
			return nil, fmt.Errorf("scan foreshadows: %w", err)
		}
		if planted.Valid {
			f.PlantedChapterID = planted.String
		}
		if resolved.Valid {
			f.ResolvedChapterID = resolved.String
		}
		if kw != "" {
			_ = json.Unmarshal([]byte(kw), &f.Keywords)
		}
		out = append(out, &f)
	}
	return out, rows.Err()
}
