package repo

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"reasonix/internal/novel/db"
)

// QueryRepo holds cross-entity search helpers. The novel "query" tool
// (Phase 2) wraps these into a single tagged result. The repo doesn't
// own any tables — it just runs read-only LIKE queries.
type QueryRepo struct{ db *db.DB }

func NewQueryRepo(d *db.DB) *QueryRepo { return &QueryRepo{db: d} }

// QueryResult is one row of cross-entity search output. Type tags which
// table the row came from; the other fields are flattened for cheap
// serialisation in the tool response.
type QueryResult struct {
	Type string // "world" | "character" | "chapter" | "arc"
	ID   string
	Name string
	// Preview is the matched column excerpt (or description).
	Preview string
}

// Search runs a LIKE query across the requested entity tables. typeFilter
// is one of: "", "world", "character", "chapter", "arc" — empty means
// all four. limit clamps the per-table result count.
func (r *QueryRepo) Search(ctx context.Context, projectID, entityType, query string, limit int) ([]*QueryResult, error) {
	if limit <= 0 {
		limit = 10
	}
	tables := []string{}
	switch entityType {
	case "":
		tables = []string{"worlds", "characters", "chapters", "outlines"}
	case "world":
		tables = []string{"worlds"}
	case "character":
		tables = []string{"characters"}
	case "chapter":
		tables = []string{"chapters"}
	case "arc", "outline":
		tables = []string{"outlines"}
	default:
		return nil, fmt.Errorf("QueryRepo.Search: unsupported entity_type %q", entityType)
	}

	var out []*QueryResult
	pat := "%" + query + "%"
	for _, tbl := range tables {
		tag := tagForTable(tbl)
		var q string
		switch tbl {
		case "worlds":
			q = fmt.Sprintf(`SELECT id, name, COALESCE(description, '') FROM %s
				WHERE project_id = ? AND (name LIKE ? OR description LIKE ?)
				ORDER BY name LIMIT ?`, tbl)
		case "characters":
			q = fmt.Sprintf(`SELECT id, name, COALESCE(description, '') FROM %s
				WHERE project_id = ? AND (name LIKE ? OR description LIKE ?)
				ORDER BY name LIMIT ?`, tbl)
		case "chapters":
			q = fmt.Sprintf(`SELECT id, title, COALESCE(content, '') FROM %s
				WHERE project_id = ? AND (title LIKE ? OR content LIKE ?)
				ORDER BY volume, chapter_number LIMIT ?`, tbl)
		case "outlines":
			q = fmt.Sprintf(`SELECT id, title, COALESCE(summary, '') FROM %s
				WHERE project_id = ? AND (title LIKE ? OR summary LIKE ?)
				ORDER BY level, order_index LIMIT ?`, tbl)
		}
		rows, err := r.db.QueryContext(ctx, q, projectID, pat, pat, limit)
		if err != nil {
			return nil, fmt.Errorf("QueryRepo.Search %s: %w", tbl, err)
		}
		for rows.Next() {
			var id, name, preview string
			if err := rows.Scan(&id, &name, &preview); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan %s: %w", tbl, err)
			}
			out = append(out, &QueryResult{Type: tag, ID: id, Name: name, Preview: preview})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

func tagForTable(t string) string {
	switch t {
	case "worlds":
		return "world"
	case "characters":
		return "character"
	case "chapters":
		return "chapter"
	case "outlines":
		return "arc"
	}
	return strings.TrimSuffix(t, "s")
}

// avoid unused import in case sql isn't directly referenced.
var _ = sql.ErrNoRows
