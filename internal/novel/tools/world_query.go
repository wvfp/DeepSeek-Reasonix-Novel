package tools

import (
	"context"

	"reasonix/internal/novel/project"
	"reasonix/internal/novel/repo"
)

type worldQueryTool struct{}

func init() { registerDefault(&worldQueryTool{}) }

func (t *worldQueryTool) Name() string { return "world_query" }

func (t *worldQueryTool) Description() string {
	return "按 id 或 name 模糊搜索当前项目的世界观条目。"
}

func (t *worldQueryTool) Execute(ctx context.Context, input map[string]any, mgr *project.Manager) (map[string]any, error) {
	p, err := mgr.Project(ctx)
	if err != nil {
		return nil, err
	}
	limit := intField(input, "limit", 20)
	if limit > 200 {
		limit = 200
	}
	r := repo.NewWorldRepo(mgr.DB())

	// Exact-id short-circuit.
	if id := stringField(input, "id"); id != "" {
		w, err := r.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		return map[string]any{"worlds": []any{worldToMap(w)}}, nil
	}
	// name query (or empty → all).
	q := stringField(input, "name")
	if q == "" {
		q = stringField(input, "query")
	}
	all, err := r.Search(ctx, p.ID, q)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	out := make([]any, 0, len(all))
	for _, w := range all {
		out = append(out, worldToMap(w))
	}
	return map[string]any{"worlds": out}, nil
}
