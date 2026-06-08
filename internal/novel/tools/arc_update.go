package tools

import (
	"context"
	"fmt"

	"reasonix/internal/novel/project"
	"reasonix/internal/novel/repo"
)

type arcUpdateTool struct{}

func init() { registerDefault(&arcUpdateTool{}) }

func (t *arcUpdateTool) Name() string { return "arc_update" }

func (t *arcUpdateTool) Description() string {
	return "按 id 更新 arc 字段（title/summary/parent_id/order_index/metadata）。"
}

func (t *arcUpdateTool) Execute(ctx context.Context, input map[string]any, mgr *project.Manager) (map[string]any, error) {
	id, err := requiredString(input, "id")
	if err != nil {
		return nil, err
	}
	fields := nestedMapField(input, "fields")
	if fields == nil {
		return nil, fmt.Errorf("arc_update: 'fields' is required")
	}
	r := repo.NewArcRepo(mgr.DB())
	a, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if v := stringField(fields, "title"); v != "" {
		a.Title = v
	}
	if v, ok := fields["summary"]; ok {
		a.Summary = stringField(fields, "summary")
		_ = v
	}
	if v, ok := fields["parent_id"]; ok {
		a.ParentID = stringField(fields, "parent_id")
		_ = v
	}
	if _, ok := fields["order_index"]; ok {
		a.OrderIndex = intField(fields, "order_index", a.OrderIndex)
	}
	if md := nestedMapField(fields, "metadata"); md != nil {
		a.Metadata = md
	}
	if err := r.Update(ctx, a); err != nil {
		return nil, err
	}
	return map[string]any{"arc": arcToMap(a)}, nil
}
