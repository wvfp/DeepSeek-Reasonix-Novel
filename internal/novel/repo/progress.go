package repo

import (
	"context"
	"fmt"

	"reasonix/internal/novel/db"
	"reasonix/internal/novel/domain"
)

// ProgressRepo produces the small per-project summary the
// novel_progress tool returns.
type ProgressRepo struct{ db *db.DB }

func NewProgressRepo(d *db.DB) *ProgressRepo { return &ProgressRepo{db: d} }

// Summary is the progress snapshot.
type Summary struct {
	TotalChapters int    `json:"total_chapters"`
	TotalWords    int    `json:"total_words"`
	CurrentArcID  string `json:"current_arc_id,omitempty"`
	CurrentArc    string `json:"current_arc,omitempty"`
	ActiveChars   int    `json:"active_characters"`
	Worlds        int    `json:"worlds"`
	PipelinePhase string `json:"pipeline_phase"`
}

// Build aggregates chapter / arc / character counts for a project. The
// "current arc" is the most recent chapter's arc_id (or the most
// recent outline row if no chapters yet).
func (r *ProgressRepo) Build(ctx context.Context, projectID string) (*Summary, error) {
	out := &Summary{}

	row := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(word_count), 0) FROM chapters WHERE project_id = ?`, projectID)
	if err := row.Scan(&out.TotalChapters, &out.TotalWords); err != nil {
		return nil, fmt.Errorf("ProgressRepo.Build chapters: %w", err)
	}

	row = r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM characters WHERE project_id = ?`, projectID)
	if err := row.Scan(&out.ActiveChars); err != nil {
		return nil, fmt.Errorf("ProgressRepo.Build characters: %w", err)
	}
	row = r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM worlds WHERE project_id = ?`, projectID)
	if err := row.Scan(&out.Worlds); err != nil {
		return nil, fmt.Errorf("ProgressRepo.Build worlds: %w", err)
	}

	// Current arc: most recent chapter's arc_id, falling back to the
	// most recent outline row.
	row = r.db.QueryRowContext(ctx,
		`SELECT arc_id FROM chapters WHERE project_id = ? AND arc_id != ''
		 ORDER BY modified_at DESC LIMIT 1`, projectID)
	var arcID string
	if err := row.Scan(&arcID); err == nil && arcID != "" {
		out.CurrentArcID = arcID
	} else {
		row = r.db.QueryRowContext(ctx,
			`SELECT id, title FROM outlines WHERE project_id = ?
			 ORDER BY modified_at DESC LIMIT 1`, projectID)
		var aid, title string
		if err := row.Scan(&aid, &title); err == nil {
			out.CurrentArcID = aid
			out.CurrentArc = title
		}
	}
	if out.CurrentArcID != "" && out.CurrentArc == "" {
		row = r.db.QueryRowContext(ctx, `SELECT title FROM outlines WHERE id = ?`, out.CurrentArcID)
		var title string
		if err := row.Scan(&title); err == nil {
			out.CurrentArc = title
		}
	}

	row = r.db.QueryRowContext(ctx, `SELECT pipeline_phase FROM projects WHERE id = ?`, projectID)
	if err := row.Scan(&out.PipelinePhase); err != nil {
		// Project may not have a row yet — fall back to domain default.
		out.PipelinePhase = domain.PhaseSetting
	}
	return out, nil
}
