package tools

import (
	"context"
	"fmt"

	"reasonix/internal/novel/project"
	"reasonix/internal/novel/repo"
)

type worldLinkTool struct{}

func init() { registerDefault(&worldLinkTool{}) }

func (t *worldLinkTool) Name() string { return "world_link" }

func (t *worldLinkTool) Description() string {
	return "在两条世界观之间建立一条关系链接（写入 entity_links 表）。"
}

func (t *worldLinkTool) Execute(ctx context.Context, input map[string]any, mgr *project.Manager) (map[string]any, error) {
	p, err := mgr.Project(ctx)
	if err != nil {
		return nil, err
	}
	fromID, err := requiredString(input, "from_id")
	if err != nil {
		return nil, err
	}
	toID, err := requiredString(input, "to_id")
	if err != nil {
		return nil, err
	}
	relation := stringField(input, "relation")
	if relation == "" {
		return nil, fmt.Errorf("world_link: 'relation' is required")
	}
	note := stringField(input, "note")

	r := repo.NewWorldRepo(mgr.DB())
	// Validate both worlds exist.
	if _, err := r.Get(ctx, fromID); err != nil {
		return nil, fmt.Errorf("world_link: from_id: %w", err)
	}
	if _, err := r.Get(ctx, toID); err != nil {
		return nil, fmt.Errorf("world_link: to_id: %w", err)
	}
	if err := r.InsertLink(ctx, p.ID, fromID, toID, relation, note); err != nil {
		return nil, err
	}
	return map[string]any{"link": map[string]any{
		"from_id":  fromID,
		"to_id":    toID,
		"relation": relation,
		"note":     note,
	}}, nil
}
