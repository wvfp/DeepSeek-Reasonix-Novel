package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"reasonix/internal/novel/db"
)

// TimelineEvent mirrors the character_timeline table row.
type TimelineEvent struct {
	ID          string `json:"id"`
	CharacterID string `json:"character_id"`
	ChapterID   string `json:"chapter_id"`
	EventType   string `json:"event_type"`
	Description string `json:"description"`
	Importance  int    `json:"importance"`
	CreatedAt   int64  `json:"created_at"`
}

// TimelineRepo persists character timeline events (character_timeline table).
type TimelineRepo struct{ db *db.DB }

func NewTimelineRepo(d *db.DB) *TimelineRepo { return &TimelineRepo{db: d} }

// Create inserts a single timeline event.
func (r *TimelineRepo) Create(ctx context.Context, e *TimelineEvent) error {
	if e == nil {
		return fmt.Errorf("TimelineRepo.Create: nil event")
	}
	if e.CharacterID == "" || e.ChapterID == "" {
		return fmt.Errorf("TimelineRepo.Create: character_id and chapter_id are required")
	}
	if e.ID == "" {
		e.ID = newID()
	}
	if e.CreatedAt == 0 {
		e.CreatedAt = now()
	}
	const q = `INSERT INTO character_timeline (id, character_id, chapter_id, event_type, description, importance, created_at)
	           VALUES (?, ?, ?, ?, ?, ?, ?)`
	_, err := r.db.ExecContext(ctx, q,
		e.ID, e.CharacterID, e.ChapterID, e.EventType, e.Description, e.Importance, e.CreatedAt)
	if err != nil {
		return fmt.Errorf("TimelineRepo.Create: %w", err)
	}
	return nil
}

// CreateBatch inserts multiple timeline events in a transaction.
func (r *TimelineRepo) CreateBatch(ctx context.Context, events []*TimelineEvent) error {
	if len(events) == 0 {
		return nil
	}
	if err := r.db.Tx(func(tx *sql.Tx) error {
		const q = `INSERT INTO character_timeline (id, character_id, chapter_id, event_type, description, importance, created_at)
		           VALUES (?, ?, ?, ?, ?, ?, ?)`
		stmt, err := tx.PrepareContext(ctx, q)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, e := range events {
			if e.ID == "" {
				e.ID = newID()
			}
			if e.CreatedAt == 0 {
				e.CreatedAt = now()
			}
			if _, err := stmt.ExecContext(ctx,
				e.ID, e.CharacterID, e.ChapterID, e.EventType, e.Description, e.Importance, e.CreatedAt); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("TimelineRepo.CreateBatch: %w", err)
	}
	return nil
}

// Get retrieves a single timeline event by ID.
func (r *TimelineRepo) Get(ctx context.Context, id string) (*TimelineEvent, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, character_id, chapter_id, event_type, description, importance, created_at
		 FROM character_timeline WHERE id = ? LIMIT 1`, id)
	return scanTimelineEvent(row)
}

// ListByCharacter returns all timeline events for a character, ordered oldest first.
func (r *TimelineRepo) ListByCharacter(ctx context.Context, characterID string) ([]*TimelineEvent, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, character_id, chapter_id, event_type, description, importance, created_at
		 FROM character_timeline WHERE character_id = ? ORDER BY created_at`, characterID)
	if err != nil {
		return nil, fmt.Errorf("TimelineRepo.ListByCharacter: %w", err)
	}
	defer rows.Close()
	return scanTimelineEvents(rows)
}

// ListByChapter returns all timeline events tied to a chapter.
func (r *TimelineRepo) ListByChapter(ctx context.Context, chapterID string) ([]*TimelineEvent, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, character_id, chapter_id, event_type, description, importance, created_at
		 FROM character_timeline WHERE chapter_id = ? ORDER BY created_at`, chapterID)
	if err != nil {
		return nil, fmt.Errorf("TimelineRepo.ListByChapter: %w", err)
	}
	defer rows.Close()
	return scanTimelineEvents(rows)
}

// Update modifies an existing timeline event.
func (r *TimelineRepo) Update(ctx context.Context, e *TimelineEvent) error {
	if e == nil || e.ID == "" {
		return fmt.Errorf("TimelineRepo.Update: id is required")
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE character_timeline SET character_id = ?, chapter_id = ?, event_type = ?, description = ?, importance = ?
		 WHERE id = ?`,
		e.CharacterID, e.ChapterID, e.EventType, e.Description, e.Importance, e.ID)
	if err != nil {
		return fmt.Errorf("TimelineRepo.Update: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes a timeline event by ID.
func (r *TimelineRepo) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM character_timeline WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("TimelineRepo.Delete: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteByCharacter removes all timeline events for a character.
func (r *TimelineRepo) DeleteByCharacter(ctx context.Context, characterID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM character_timeline WHERE character_id = ?`, characterID)
	if err != nil {
		return fmt.Errorf("TimelineRepo.DeleteByCharacter: %w", err)
	}
	return nil
}

func scanTimelineEvent(s rowScanner) (*TimelineEvent, error) {
	var e TimelineEvent
	if err := s.Scan(&e.ID, &e.CharacterID, &e.ChapterID, &e.EventType, &e.Description, &e.Importance, &e.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan timeline event: %w", err)
	}
	return &e, nil
}

func scanTimelineEvents(rows *sql.Rows) ([]*TimelineEvent, error) {
	var out []*TimelineEvent
	for rows.Next() {
		var e TimelineEvent
		if err := rows.Scan(&e.ID, &e.CharacterID, &e.ChapterID, &e.EventType, &e.Description, &e.Importance, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan timeline events: %w", err)
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}
