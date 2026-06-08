package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"reasonix/internal/novel/db"
	"reasonix/internal/novel/domain"
)

// FactRepo persists domain.ChapterFact (chapter_facts table). Each
// row is a (subject, predicate, object) triple extracted from a
// chapter by the LLM.
type FactRepo struct{ db *db.DB }

func NewFactRepo(d *db.DB) *FactRepo { return &FactRepo{db: d} }

func (r *FactRepo) Create(ctx context.Context, f *domain.ChapterFact) error {
	if f == nil {
		return fmt.Errorf("FactRepo.Create: nil fact")
	}
	if f.ProjectID == "" || f.ChapterID == "" {
		return fmt.Errorf("FactRepo.Create: project_id and chapter_id are required")
	}
	if f.ID == "" {
		f.ID = newID()
	}
	if f.CreatedAt == 0 {
		f.CreatedAt = now()
	}
	const q = `INSERT INTO chapter_facts (id, project_id, chapter_id, fact_type, subject, predicate, object, confidence, context, created_at)
	           VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := r.db.ExecContext(ctx, q,
		f.ID, f.ProjectID, f.ChapterID, f.FactType, f.Subject, f.Predicate, f.Object, f.Confidence, f.Context, f.CreatedAt)
	if err != nil {
		return fmt.Errorf("FactRepo.Create: %w", err)
	}
	return nil
}

func (r *FactRepo) CreateBatch(ctx context.Context, fs []*domain.ChapterFact) error {
	if len(fs) == 0 {
		return nil
	}
	if err := r.db.Tx(func(tx *sql.Tx) error {
		const q = `INSERT INTO chapter_facts (id, project_id, chapter_id, fact_type, subject, predicate, object, confidence, context, created_at)
		           VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
		stmt, err := tx.PrepareContext(ctx, q)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, f := range fs {
			if f.ID == "" {
				f.ID = newID()
			}
			if f.CreatedAt == 0 {
				f.CreatedAt = now()
			}
			if _, err := stmt.ExecContext(ctx,
				f.ID, f.ProjectID, f.ChapterID, f.FactType, f.Subject, f.Predicate, f.Object, f.Confidence, f.Context, f.CreatedAt); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("FactRepo.CreateBatch: %w", err)
	}
	return nil
}

func (r *FactRepo) ListByChapter(ctx context.Context, chapterID string) ([]*domain.ChapterFact, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, project_id, chapter_id, fact_type, subject, predicate, object, confidence, context, created_at
		 FROM chapter_facts WHERE chapter_id = ? ORDER BY created_at`, chapterID)
	if err != nil {
		return nil, fmt.Errorf("FactRepo.ListByChapter: %w", err)
	}
	defer rows.Close()
	return scanFacts(rows)
}

func (r *FactRepo) ListByChapterWithError(ctx context.Context, chapterID string) ([]*domain.ChapterFact, error) {
	return r.ListByChapter(ctx, chapterID)
}

func scanFacts(rows *sql.Rows) ([]*domain.ChapterFact, error) {
	var out []*domain.ChapterFact
	for rows.Next() {
		var f domain.ChapterFact
		var ctx string
		if err := rows.Scan(&f.ID, &f.ProjectID, &f.ChapterID, &f.FactType, &f.Subject, &f.Predicate, &f.Object, &f.Confidence, &ctx, &f.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan facts: %w", err)
		}
		f.Context = ctx
		out = append(out, &f)
	}
	return out, rows.Err()
}

// avoid unused import when errors isn't otherwise referenced.
var _ = errors.New
