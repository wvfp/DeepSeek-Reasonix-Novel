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

// CharacterRepo persists domain.Character. VoiceProfile is serialised
// into a JSON column by the schema (the column is text); we round-trip
// via json.Marshal/Unmarshal here.
type CharacterRepo struct{ db *db.DB }

func NewCharacterRepo(d *db.DB) *CharacterRepo { return &CharacterRepo{db: d} }

func (r *CharacterRepo) Create(ctx context.Context, c *domain.Character) error {
	if c == nil {
		return fmt.Errorf("CharacterRepo.Create: nil character")
	}
	if c.ProjectID == "" {
		return fmt.Errorf("CharacterRepo.Create: project_id is required")
	}
	if c.ID == "" {
		c.ID = newID()
	}
	if c.Slug == "" {
		c.Slug = Slugify(c.Name)
	}
	if c.CreatedAt == 0 {
		c.CreatedAt = now()
	}
	c.ModifiedAt = now()

	vp, err := voiceProfileToJSON(c.VoiceProfile)
	if err != nil {
		return fmt.Errorf("CharacterRepo.Create: voice_profile: %w", err)
	}
	const q = `INSERT INTO characters (id, project_id, name, slug, description, voice_profile, content, created_at, modified_at)
	           VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err = r.db.ExecContext(ctx, q,
		c.ID, c.ProjectID, c.Name, c.Slug, c.Description, vp, c.Content, c.CreatedAt, c.ModifiedAt)
	if err != nil {
		return fmt.Errorf("CharacterRepo.Create: %w", err)
	}
	return nil
}

func (r *CharacterRepo) Get(ctx context.Context, id string) (*domain.Character, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, project_id, name, slug, description, voice_profile, content, created_at, modified_at
		 FROM characters WHERE id = ? LIMIT 1`, id)
	return scanCharacter(row)
}

func (r *CharacterRepo) List(ctx context.Context, projectID string) ([]*domain.Character, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, project_id, name, slug, description, voice_profile, content, created_at, modified_at
		 FROM characters WHERE project_id = ? ORDER BY name`, projectID)
	if err != nil {
		return nil, fmt.Errorf("CharacterRepo.List: %w", err)
	}
	defer rows.Close()
	return scanCharacters(rows)
}

func (r *CharacterRepo) Search(ctx context.Context, projectID, query string) ([]*domain.Character, error) {
	if query == "" {
		return r.List(ctx, projectID)
	}
	pat := "%" + query + "%"
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, project_id, name, slug, description, voice_profile, content, created_at, modified_at
		 FROM characters
		 WHERE project_id = ? AND (name LIKE ? OR description LIKE ?)
		 ORDER BY name`, projectID, pat, pat)
	if err != nil {
		return nil, fmt.Errorf("CharacterRepo.Search: %w", err)
	}
	defer rows.Close()
	return scanCharacters(rows)
}

func (r *CharacterRepo) Update(ctx context.Context, c *domain.Character) error {
	if c == nil || c.ID == "" {
		return fmt.Errorf("CharacterRepo.Update: id is required")
	}
	c.ModifiedAt = now()
	vp, err := voiceProfileToJSON(c.VoiceProfile)
	if err != nil {
		return fmt.Errorf("CharacterRepo.Update: voice_profile: %w", err)
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE characters SET name = ?, slug = ?, description = ?, voice_profile = ?, content = ?, modified_at = ?
		 WHERE id = ?`,
		c.Name, c.Slug, c.Description, vp, c.Content, c.ModifiedAt, c.ID)
	if err != nil {
		return fmt.Errorf("CharacterRepo.Update: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *CharacterRepo) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM characters WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("CharacterRepo.Delete: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *CharacterRepo) Slugify(name string) string { return Slugify(name) }

func voiceProfileToJSON(vp *domain.VoiceProfile) (string, error) {
	if vp == nil {
		return "", nil
	}
	b, err := json.Marshal(vp)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func voiceProfileFromJSON(s string) (*domain.VoiceProfile, error) {
	if s == "" {
		return nil, nil
	}
	var vp domain.VoiceProfile
	if err := json.Unmarshal([]byte(s), &vp); err != nil {
		return nil, err
	}
	return &vp, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanCharacter(s rowScanner) (*domain.Character, error) {
	var c domain.Character
	var vp string
	if err := s.Scan(&c.ID, &c.ProjectID, &c.Name, &c.Slug, &c.Description, &vp, &c.Content, &c.CreatedAt, &c.ModifiedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan character: %w", err)
	}
	profile, _ := voiceProfileFromJSON(vp)
	c.VoiceProfile = profile
	return &c, nil
}

func scanCharacters(rows *sql.Rows) ([]*domain.Character, error) {
	var out []*domain.Character
	for rows.Next() {
		var c domain.Character
		var vp string
		if err := rows.Scan(&c.ID, &c.ProjectID, &c.Name, &c.Slug, &c.Description, &vp, &c.Content, &c.CreatedAt, &c.ModifiedAt); err != nil {
			return nil, fmt.Errorf("scan characters: %w", err)
		}
		profile, _ := voiceProfileFromJSON(vp)
		c.VoiceProfile = profile
		out = append(out, &c)
	}
	return out, rows.Err()
}
