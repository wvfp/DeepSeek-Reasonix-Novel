package repo

import (
	"context"
	"database/sql"
	"fmt"

	"reasonix/internal/novel/db"
	"reasonix/internal/novel/domain"
)

// KGRepo persists the knowledge graph (knowledge_graph_nodes +
// knowledge_graph_edges). The chapter-write tool calls CreateNode for
// each entity mentioned in the chapter and CreateEdge for each
// relation the LLM emits.
type KGRepo struct{ db *db.DB }

func NewKGRepo(d *db.DB) *KGRepo { return &KGRepo{db: d} }

// EnsureNode creates the node if it doesn't exist (matched by
// (entity_type, entity_id)) and returns the node ID. Idempotent — the
// chapter-write tool calls this once per subject + object of every
// edge.
func (r *KGRepo) EnsureNode(ctx context.Context, projectID, entityType, entityID, name string) (string, error) {
	if entityID == "" {
		entityID = newID()
	}
	row := r.db.QueryRowContext(ctx,
		`SELECT id FROM knowledge_graph_nodes
		 WHERE project_id = ? AND entity_type = ? AND entity_id = ? LIMIT 1`,
		projectID, entityType, entityID)
	var id string
	if err := row.Scan(&id); err == nil {
		return id, nil
	} else if err != sql.ErrNoRows {
		return "", fmt.Errorf("KGRepo.EnsureNode: lookup: %w", err)
	}
	id = newID()
	const q = `INSERT INTO knowledge_graph_nodes (id, project_id, entity_type, entity_id, name, created_at)
	           VALUES (?, ?, ?, ?, ?, ?)`
	if _, err := r.db.ExecContext(ctx, q, id, projectID, entityType, entityID, name, now()); err != nil {
		return "", fmt.Errorf("KGRepo.EnsureNode: insert: %w", err)
	}
	return id, nil
}

func (r *KGRepo) CreateEdge(ctx context.Context, e *domain.KGEdge) error {
	if e == nil {
		return fmt.Errorf("KGRepo.CreateEdge: nil")
	}
	if e.ProjectID == "" || e.FromNodeID == "" || e.ToNodeID == "" {
		return fmt.Errorf("KGRepo.CreateEdge: project_id, from_node_id, to_node_id are required")
	}
	if e.ID == "" {
		e.ID = newID()
	}
	if e.CreatedAt == 0 {
		e.CreatedAt = now()
	}
	if e.Weight == 0 {
		e.Weight = 1.0
	}
	attrs, _ := metadataToJSON(e.Attributes)
	const q = `INSERT INTO knowledge_graph_edges (id, project_id, from_node_id, to_node_id, relation, weight, attributes, created_at)
	           VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := r.db.ExecContext(ctx, q, e.ID, e.ProjectID, e.FromNodeID, e.ToNodeID, e.Relation, e.Weight, attrs, e.CreatedAt)
	if err != nil {
		return fmt.Errorf("KGRepo.CreateEdge: %w", err)
	}
	return nil
}

// CreateEdges inserts a slice of edges in a single transaction. The
// caller resolves the from/to_node_id via EnsureNode before calling.
func (r *KGRepo) CreateEdges(ctx context.Context, edges []*domain.KGEdge) error {
	if len(edges) == 0 {
		return nil
	}
	if err := r.db.Tx(func(tx *sql.Tx) error {
		const q = `INSERT INTO knowledge_graph_edges (id, project_id, from_node_id, to_node_id, relation, weight, attributes, created_at)
		           VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
		stmt, err := tx.PrepareContext(ctx, q)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, e := range edges {
			if e.ID == "" {
				e.ID = newID()
			}
			if e.CreatedAt == 0 {
				e.CreatedAt = now()
			}
			if e.Weight == 0 {
				e.Weight = 1.0
			}
			attrs, _ := metadataToJSON(e.Attributes)
			if _, err := stmt.ExecContext(ctx, e.ID, e.ProjectID, e.FromNodeID, e.ToNodeID, e.Relation, e.Weight, attrs, e.CreatedAt); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("KGRepo.CreateEdges: %w", err)
	}
	return nil
}

// ListNodesByChapter isn't part of the v2 schema (KG nodes are global
// per project, not per-chapter). Left as a placeholder hook for Phase 3
// when the schema gets a chapter_id column on nodes.
//
// Keeping the doc so a future refactor lands in the right place.
var _ = domain.KGNode{}
