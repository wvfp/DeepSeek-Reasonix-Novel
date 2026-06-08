package tools

import (
	"context"

	"reasonix/internal/novel/project"
	"reasonix/internal/novel/repo"
)

type characterQueryTool struct{}

func init() { registerDefault(&characterQueryTool{}) }

func (t *characterQueryTool) Name() string { return "character_query" }

func (t *characterQueryTool) Description() string {
	return "按 id 或 name 模糊搜索当前项目的角色。"
}

func (t *characterQueryTool) Execute(ctx context.Context, input map[string]any, mgr *project.Manager) (map[string]any, error) {
	p, err := mgr.Project(ctx)
	if err != nil {
		return nil, err
	}
	limit := intField(input, "limit", 20)
	if limit > 200 {
		limit = 200
	}
	r := repo.NewCharacterRepo(mgr.DB())

	if id := stringField(input, "id"); id != "" {
		c, err := r.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		return map[string]any{"characters": []any{characterToMap(c)}}, nil
	}
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
	for _, c := range all {
		out = append(out, characterToMap(c))
	}
	return map[string]any{"characters": out}, nil
}
