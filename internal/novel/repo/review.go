package repo

import (
	"context"
	"encoding/json"
	"fmt"

	"reasonix/internal/novel/db"
	"reasonix/internal/novel/domain"
)

// ReviewRepo persists domain.Review (reviews table). The Issues field
// is serialised as JSON.
type ReviewRepo struct{ db *db.DB }

func NewReviewRepo(d *db.DB) *ReviewRepo { return &ReviewRepo{db: d} }

func (r *ReviewRepo) Create(ctx context.Context, rv *domain.Review) error {
	if rv == nil {
		return fmt.Errorf("ReviewRepo.Create: nil")
	}
	if rv.ChapterID == "" || rv.Dimension == "" {
		return fmt.Errorf("ReviewRepo.Create: chapter_id, dimension are required")
	}
	if rv.ID == "" {
		rv.ID = newID()
	}
	if rv.CreatedAt == 0 {
		rv.CreatedAt = now()
	}
	issues, _ := json.Marshal(rv.Issues)
	const q = `INSERT INTO reviews (id, chapter_id, dimension, score, issues, created_at)
	           VALUES (?, ?, ?, ?, ?, ?)`
	_, err := r.db.ExecContext(ctx, q, rv.ID, rv.ChapterID, rv.Dimension, rv.Score, string(issues), rv.CreatedAt)
	if err != nil {
		return fmt.Errorf("ReviewRepo.Create: %w", err)
	}
	return nil
}

// SetSuggestions updates the suggestions JSON column for all review rows of a chapter.
func (r *ReviewRepo) SetSuggestions(ctx context.Context, chapterID string, suggestionsJSON string) error {
	if chapterID == "" {
		return fmt.Errorf("ReviewRepo.SetSuggestions: chapter_id is required")
	}
	const q = `UPDATE reviews SET suggestions = ? WHERE chapter_id = ?`
	_, err := r.db.ExecContext(ctx, q, suggestionsJSON, chapterID)
	if err != nil {
		return fmt.Errorf("ReviewRepo.SetSuggestions: %w", err)
	}
	return nil
}

func (r *ReviewRepo) ListByChapter(ctx context.Context, chapterID string) ([]*domain.Review, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, chapter_id, dimension, score, issues, created_at
		 FROM reviews WHERE chapter_id = ? ORDER BY dimension`, chapterID)
	if err != nil {
		return nil, fmt.Errorf("ReviewRepo.ListByChapter: %w", err)
	}
	defer rows.Close()
	var out []*domain.Review
	for rows.Next() {
		var rv domain.Review
		var issues string
		if err := rows.Scan(&rv.ID, &rv.ChapterID, &rv.Dimension, &rv.Score, &issues, &rv.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan reviews: %w", err)
		}
		if issues != "" {
			_ = json.Unmarshal([]byte(issues), &rv.Issues)
		}
		out = append(out, &rv)
	}
	return out, rows.Err()
}
