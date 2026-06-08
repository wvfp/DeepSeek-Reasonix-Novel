package repo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"reasonix/internal/novel/db"
	"reasonix/internal/novel/domain"
)

// StateRepo persists domain.CharacterState as a per-chapter snapshot
// (character_states table). The snapshot column carries a JSON blob
// matching domain.CharacterState.
type StateRepo struct{ db *db.DB }

func NewStateRepo(d *db.DB) *StateRepo { return &StateRepo{db: d} }

func (r *StateRepo) Create(ctx context.Context, s *domain.CharacterState, projectID, characterID, chapterID string, tags []string) error {
	if projectID == "" || characterID == "" || chapterID == "" {
		return fmt.Errorf("StateRepo.Create: project_id, character_id, chapter_id are required")
	}
	if s == nil {
		return fmt.Errorf("StateRepo.Create: nil state")
	}
	snapshot, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("StateRepo.Create: marshal: %w", err)
	}
	tagsJSON, _ := json.Marshal(tags)
	const q = `INSERT INTO character_states (id, project_id, character_id, chapter_id, snapshot, tags, created_at)
	           VALUES (?, ?, ?, ?, ?, ?, ?)`
	_, err = r.db.ExecContext(ctx, q, newID(), projectID, characterID, chapterID, string(snapshot), string(tagsJSON), now())
	if err != nil {
		return fmt.Errorf("StateRepo.Create: %w", err)
	}
	return nil
}

func (r *StateRepo) CreateBatch(ctx context.Context, states []*StateInput) error {
	if len(states) == 0 {
		return nil
	}
	if err := r.db.Tx(func(tx *sql.Tx) error {
		const q = `INSERT INTO character_states (id, project_id, character_id, chapter_id, snapshot, tags, created_at)
		           VALUES (?, ?, ?, ?, ?, ?, ?)`
		stmt, err := tx.PrepareContext(ctx, q)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, s := range states {
			if s.CharacterID == "" {
				return fmt.Errorf("StateRepo.CreateBatch: empty character_id")
			}
			if s.ChapterID == "" {
				return fmt.Errorf("StateRepo.CreateBatch: empty chapter_id")
			}
			if s.ProjectID == "" {
				return fmt.Errorf("StateRepo.CreateBatch: empty project_id")
			}
			snap, err := json.Marshal(s.Snapshot)
			if err != nil {
				return err
			}
			tagsJSON, _ := json.Marshal(s.Tags)
			if _, err := stmt.ExecContext(ctx, newID(), s.ProjectID, s.CharacterID, s.ChapterID, string(snap), string(tagsJSON), now()); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("StateRepo.CreateBatch: %w", err)
	}
	return nil
}

// StateInput is the param struct for CreateBatch — a domain.CharacterState
// plus the FK triple and optional tags. Letting the snapshot be nil
// is allowed; the column is the empty string in that case.
type StateInput struct {
	ProjectID   string
	CharacterID string
	ChapterID   string
	Snapshot    *domain.CharacterState
	Tags        []string
}

// RecentByProject returns the most recent N states (most recent first).
// chapter_write uses it to feed "active character states" into the
// system prompt.
func (r *StateRepo) RecentByProject(ctx context.Context, projectID string, limit int) ([]*RecentState, error) {
	if limit <= 0 {
		limit = 3
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, project_id, character_id, chapter_id, snapshot, tags, created_at
		 FROM character_states WHERE project_id = ?
		 ORDER BY created_at DESC LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, fmt.Errorf("StateRepo.RecentByProject: %w", err)
	}
	defer rows.Close()

	var out []*RecentState
	for rows.Next() {
		var s RecentState
		var snap, tags string
		if err := rows.Scan(&s.ID, &s.ProjectID, &s.CharacterID, &s.ChapterID, &snap, &tags, &s.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan states: %w", err)
		}
		if snap != "" {
			_ = json.Unmarshal([]byte(snap), &s.Snapshot)
		}
		if tags != "" {
			_ = json.Unmarshal([]byte(tags), &s.Tags)
		}
		out = append(out, &s)
	}
	return out, rows.Err()
}

// RecentState is a flattened state row with the snapshot decoded.
type RecentState struct {
	ID          string
	ProjectID   string
	CharacterID string
	ChapterID   string
	Snapshot    domain.CharacterState
	Tags        []string
	CreatedAt   int64
}
