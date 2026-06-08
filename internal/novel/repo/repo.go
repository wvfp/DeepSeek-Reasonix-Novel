// Package repo provides typed repositories for the domain types. Each
// repo takes a *db.DB and exposes a small set of CRUD operations
// (Create / Get / List / Update / Delete) plus a few domain-specific
// queries (Search by LIKE for textual columns, ListByProject for
// project-scoped reads, etc.). The repos are intentionally thin —
// they hand back *domain values, never raw rows, so the rest of the
// codebase can mock at the repo level when it needs to.
package repo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"reasonix/internal/novel/db"
	"reasonix/internal/novel/domain"
)

// Slugify turns a free-form name into the file-system-friendly slug
// used for *.md filenames. The transformation: lowercase, replace
// every non-alphanumeric run with '-', trim leading/trailing dashes.
// Matches the novel-plugin's slug behaviour closely enough that a Go
// project and a TS project can interop.
func Slugify(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return "untitled"
	}
	var b strings.Builder
	prevDash := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r >= 0x4e00 && r <= 0x9fff:
			// CJK characters: include as-is. Obsidian wikilinks
			// accept CJK directly in filenames, so this is the
			// most useful behaviour for a Chinese web novel.
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if out == "" {
		return "untitled"
	}
	return out
}

// newID wraps uuid.New with a no-error API for the repos. It panics
// only on the (theoretical) UUID failure, which is fine in tests.
func newID() string { return uuid.New().String() }

// now returns the current Unix time in seconds. Repos that need a
// monotonic timestamp use this.
func now() int64 { return time.Now().Unix() }

// metadataToJSON serialises a map[string]any to a string for storage.
// nil maps become the empty string so the column is searchable.
func metadataToJSON(m map[string]any) (string, error) {
	if len(m) == 0 {
		return "", nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// metadataFromJSON deserialises a metadata column into a map. Empty
// input returns nil.
func metadataFromJSON(s string) (map[string]any, error) {
	if s == "" {
		return nil, nil
	}
	out := map[string]any{}
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// notFound is the canonical error for "row not found". Returned by
// Get when the lookup misses so callers can errors.Is(err,
// ErrNotFound) to branch.
var ErrNotFound = errors.New("repo: not found")

// --- WorldRepo --------------------------------------------------------

// WorldRepo persists domain.World. Worlds are project-scoped, sorted
// by name, and addressed by a slug for the file system layout.
type WorldRepo struct{ db *db.DB }

func NewWorldRepo(d *db.DB) *WorldRepo { return &WorldRepo{db: d} }

// Create inserts a new world. Empty ID/slug are populated from name.
func (r *WorldRepo) Create(ctx context.Context, w *domain.World) error {
	if w == nil {
		return fmt.Errorf("WorldRepo.Create: nil world")
	}
	if w.ProjectID == "" {
		return fmt.Errorf("WorldRepo.Create: project_id is required")
	}
	if w.ID == "" {
		w.ID = newID()
	}
	if w.Slug == "" {
		w.Slug = Slugify(w.Name)
	}
	if w.CreatedAt == 0 {
		w.CreatedAt = now()
	}
	w.ModifiedAt = now()

	meta, err := metadataToJSON(w.Metadata)
	if err != nil {
		return fmt.Errorf("WorldRepo.Create: metadata: %w", err)
	}
	const q = `INSERT INTO worlds (id, project_id, name, slug, description, content, metadata, created_at, modified_at)
	           VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err = r.db.ExecContext(ctx, q,
		w.ID, w.ProjectID, w.Name, w.Slug, w.Description, w.Content, meta, w.CreatedAt, w.ModifiedAt)
	if err != nil {
		return fmt.Errorf("WorldRepo.Create: %w", err)
	}
	return nil
}

func (r *WorldRepo) Get(ctx context.Context, id string) (*domain.World, error) {
	return getWorld(ctx, r.db, "id = ?", id)
}

func (r *WorldRepo) List(ctx context.Context, projectID string) ([]*domain.World, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, project_id, name, slug, description, content, metadata, created_at, modified_at
		 FROM worlds WHERE project_id = ? ORDER BY name`, projectID)
	if err != nil {
		return nil, fmt.Errorf("WorldRepo.List: %w", err)
	}
	defer rows.Close()
	return scanWorlds(rows)
}

// Search does a LIKE query on name / description. Empty query is
// equivalent to List.
func (r *WorldRepo) Search(ctx context.Context, projectID, query string) ([]*domain.World, error) {
	if query == "" {
		return r.List(ctx, projectID)
	}
	pat := "%" + query + "%"
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, project_id, name, slug, description, content, metadata, created_at, modified_at
		 FROM worlds
		 WHERE project_id = ? AND (name LIKE ? OR description LIKE ?)
		 ORDER BY name`, projectID, pat, pat)
	if err != nil {
		return nil, fmt.Errorf("WorldRepo.Search: %w", err)
	}
	defer rows.Close()
	return scanWorlds(rows)
}

func (r *WorldRepo) Update(ctx context.Context, w *domain.World) error {
	if w == nil || w.ID == "" {
		return fmt.Errorf("WorldRepo.Update: id is required")
	}
	w.ModifiedAt = now()
	meta, err := metadataToJSON(w.Metadata)
	if err != nil {
		return fmt.Errorf("WorldRepo.Update: metadata: %w", err)
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE worlds SET name = ?, slug = ?, description = ?, content = ?, metadata = ?, modified_at = ?
		 WHERE id = ?`,
		w.Name, w.Slug, w.Description, w.Content, meta, w.ModifiedAt, w.ID)
	if err != nil {
		return fmt.Errorf("WorldRepo.Update: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *WorldRepo) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM worlds WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("WorldRepo.Delete: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Slugify is exposed on the repo so the tool layer can produce the
// same slug the DB column expects.
func (r *WorldRepo) Slugify(name string) string { return Slugify(name) }

// InsertLink records a relation between two worlds. The link table is
// entity_links; the source/target type is always "world" and the
// default link_type is "relation" (caller can override via
// InsertLinkTyped).
func (r *WorldRepo) InsertLink(ctx context.Context, projectID, fromID, toID, relation, note string) error {
	if projectID == "" || fromID == "" || toID == "" {
		return fmt.Errorf("WorldRepo.InsertLink: project_id, from_id, to_id are required")
	}
	linkType := relation
	if linkType == "" {
		linkType = "relation"
	}
	// Note: the entity_links table has no `attributes` column in v2; the
	// relation + note are encoded into the link_type/created_at only
	// for now. A future schema migration can split them out.
	_ = note
	const q = `INSERT INTO entity_links (id, project_id, source_type, source_id, target_type, target_id, link_type, created_at)
	           VALUES (?, ?, 'world', ?, 'world', ?, ?, ?)`
	_, err := r.db.ExecContext(ctx, q, newID(), projectID, fromID, toID, linkType, now())
	if err != nil {
		return fmt.Errorf("WorldRepo.InsertLink: %w", err)
	}
	return nil
}

// scanWorlds reads world rows into domain values. The metadata column
// is deserialised; nil on parse error is treated as "no metadata".
func scanWorlds(rows *sql.Rows) ([]*domain.World, error) {
	var out []*domain.World
	for rows.Next() {
		var w domain.World
		var meta string
		if err := rows.Scan(&w.ID, &w.ProjectID, &w.Name, &w.Slug, &w.Description, &w.Content, &meta, &w.CreatedAt, &w.ModifiedAt); err != nil {
			return nil, fmt.Errorf("scan worlds: %w", err)
		}
		m, _ := metadataFromJSON(meta)
		w.Metadata = m
		out = append(out, &w)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func getWorld(ctx context.Context, d *db.DB, whereClause string, arg any) (*domain.World, error) {
	q := `SELECT id, project_id, name, slug, description, content, metadata, created_at, modified_at
	      FROM worlds WHERE ` + whereClause + ` LIMIT 1`
	row := d.QueryRowContext(ctx, q, arg)
	var w domain.World
	var meta string
	if err := row.Scan(&w.ID, &w.ProjectID, &w.Name, &w.Slug, &w.Description, &w.Content, &meta, &w.CreatedAt, &w.ModifiedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("getWorld: %w", err)
	}
	m, _ := metadataFromJSON(meta)
	w.Metadata = m
	return &w, nil
}

// --- Generic helpers -------------------------------------------------

// BindID sets the ID field on v to a new UUID if it's empty. Used by
// the insert functions below.
func BindID(idPtr *string) {
	if *idPtr == "" {
		*idPtr = newID()
	}
}

// validName returns true if name is non-empty and contains at least
// one non-whitespace character. Cheap validation for tool inputs.
var whitespaceRe = regexp.MustCompile(`\s`)

func validName(s string) bool { return strings.TrimSpace(s) != "" }
