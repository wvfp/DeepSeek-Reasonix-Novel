package tools

import (
	"context"
	"fmt"

	"reasonix/internal/novel/project"
	"reasonix/internal/novel/repo"
)

type queryTool struct{}

func init() { registerDefault(&queryTool{}) }

func (t *queryTool) Name() string { return "query" }

func (t *queryTool) Description() string {
	return "跨实体（world/character/chapter/arc）模糊搜索当前项目。"
}

func (t *queryTool) Execute(ctx context.Context, input map[string]any, mgr *project.Manager) (map[string]any, error) {
	p, err := mgr.Project(ctx)
	if err != nil {
		return nil, err
	}
	q := stringField(input, "query")
	if q == "" {
		return nil, fmt.Errorf("query: 'query' is required")
	}
	entity := stringField(input, "entity_type")
	limit := intField(input, "limit", 10)
	results, err := repo.NewQueryRepo(mgr.DB()).Search(ctx, p.ID, entity, q, limit)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(results))
	for _, r := range results {
		out = append(out, map[string]any{
			"type":    r.Type,
			"id":      r.ID,
			"name":    r.Name,
			"preview": r.Preview,
		})
	}
	return map[string]any{"results": out, "count": len(out)}, nil
}
