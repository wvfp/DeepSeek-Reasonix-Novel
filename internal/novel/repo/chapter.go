package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"reasonix/internal/novel/db"
	"reasonix/internal/novel/domain"
)

// ChapterRepo persists domain.Chapter. Chapters are project-scoped,
// addressable by (volume, chapter_number), and roll up to an ArcID.
type ChapterRepo struct{ db *db.DB }

func NewChapterRepo(d *db.DB) *ChapterRepo { return &ChapterRepo{db: d} }

func (r *ChapterRepo) Create(ctx context.Context, c *domain.Chapter) error {
	if c == nil {
		return fmt.Errorf("ChapterRepo.Create: nil chapter")
	}
	if c.ProjectID == "" {
		return fmt.Errorf("ChapterRepo.Create: project_id is required")
	}
	if c.Title == "" {
		return fmt.Errorf("ChapterRepo.Create: title is required")
	}
	if c.ID == "" {
		c.ID = newID()
	}
	if c.Slug == "" {
		c.Slug = Slugify(c.Title)
	}
	if c.Status == "" {
		c.Status = "draft"
	}
	if c.Volume == 0 {
		c.Volume = 1
	}
	if c.CreatedAt == 0 {
		c.CreatedAt = now()
	}
	c.ModifiedAt = now()

	const q = `INSERT INTO chapters (id, project_id, arc_id, volume, chapter_number, title, slug, content, word_count, status, created_at, modified_at)
	           VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := r.db.ExecContext(ctx, q,
		c.ID, c.ProjectID, nullString(c.ArcID), c.Volume, c.ChapterNumber, c.Title, c.Slug, c.Content, c.WordCount, c.Status, c.CreatedAt, c.ModifiedAt)
	if err != nil {
		return fmt.Errorf("ChapterRepo.Create: %w", err)
	}
	return nil
}

func (r *ChapterRepo) Get(ctx context.Context, id string) (*domain.Chapter, error) {
	row := r.db.QueryRowContext(ctx, chapterSelect+` WHERE id = ? LIMIT 1`, id)
	return scanChapter(row)
}

func (r *ChapterRepo) List(ctx context.Context, projectID string) ([]*domain.Chapter, error) {
	rows, err := r.db.QueryContext(ctx,
		chapterSelect+` WHERE project_id = ? ORDER BY volume, chapter_number`, projectID)
	if err != nil {
		return nil, fmt.Errorf("ChapterRepo.List: %w", err)
	}
	defer rows.Close()
	return scanChapters(rows)
}

// ListByArc returns the chapters tied to a given arc (outline row),
// ordered oldest first. Used by chapter_write to find the previous
// chapter when continuing.
func (r *ChapterRepo) ListByArc(ctx context.Context, arcID string) ([]*domain.Chapter, error) {
	rows, err := r.db.QueryContext(ctx,
		chapterSelect+` WHERE arc_id = ? ORDER BY volume, chapter_number`, arcID)
	if err != nil {
		return nil, fmt.Errorf("ChapterRepo.ListByArc: %w", err)
	}
	defer rows.Close()
	return scanChapters(rows)
}

func (r *ChapterRepo) Search(ctx context.Context, projectID, query string) ([]*domain.Chapter, error) {
	if query == "" {
		return r.List(ctx, projectID)
	}
	pat := "%" + query + "%"
	rows, err := r.db.QueryContext(ctx,
		chapterSelect+` WHERE project_id = ? AND (title LIKE ? OR content LIKE ?)
		 ORDER BY volume, chapter_number`, projectID, pat, pat)
	if err != nil {
		return nil, fmt.Errorf("ChapterRepo.Search: %w", err)
	}
	defer rows.Close()
	return scanChapters(rows)
}

func (r *ChapterRepo) Update(ctx context.Context, c *domain.Chapter) error {
	if c == nil || c.ID == "" {
		return fmt.Errorf("ChapterRepo.Update: id is required")
	}
	c.ModifiedAt = now()
	res, err := r.db.ExecContext(ctx,
		`UPDATE chapters SET arc_id = ?, title = ?, slug = ?, content = ?, word_count = ?, status = ?, modified_at = ?
		 WHERE id = ?`,
		nullString(c.ArcID), c.Title, c.Slug, c.Content, c.WordCount, c.Status, c.ModifiedAt, c.ID)
	if err != nil {
		return fmt.Errorf("ChapterRepo.Update: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *ChapterRepo) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM chapters WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("ChapterRepo.Delete: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

const chapterSelect = `SELECT id, project_id, arc_id, volume, chapter_number, title, slug, content, word_count, status, created_at, modified_at FROM chapters`

func scanChapter(s rowScanner) (*domain.Chapter, error) {
	var c domain.Chapter
	var arc sql.NullString
	if err := s.Scan(&c.ID, &c.ProjectID, &arc, &c.Volume, &c.ChapterNumber, &c.Title, &c.Slug, &c.Content, &c.WordCount, &c.Status, &c.CreatedAt, &c.ModifiedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan chapter: %w", err)
	}
	if arc.Valid {
		c.ArcID = arc.String
	}
	return &c, nil
}

func scanChapters(rows *sql.Rows) ([]*domain.Chapter, error) {
	var out []*domain.Chapter
	for rows.Next() {
		var c domain.Chapter
		var arc sql.NullString
		if err := rows.Scan(&c.ID, &c.ProjectID, &arc, &c.Volume, &c.ChapterNumber, &c.Title, &c.Slug, &c.Content, &c.WordCount, &c.Status, &c.CreatedAt, &c.ModifiedAt); err != nil {
			return nil, fmt.Errorf("scan chapters: %w", err)
		}
		if arc.Valid {
			c.ArcID = arc.String
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}
