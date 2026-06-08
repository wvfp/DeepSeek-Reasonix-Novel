package tools

import (
	"context"

	"reasonix/internal/novel/domain"
	"reasonix/internal/novel/project"
	"reasonix/internal/novel/repo"
)

type arcGenerateTool struct{}

func init() { registerDefault(&arcGenerateTool{}) }

func (t *arcGenerateTool) Name() string { return "arc_generate" }

func (t *arcGenerateTool) Description() string {
	return "创建一条主线/卷/章节大纲（outlines 表）。level 必填（master/volume/chapter/blueprint）。"
}

func (t *arcGenerateTool) Execute(ctx context.Context, input map[string]any, mgr *project.Manager) (map[string]any, error) {
	p, err := mgr.Project(ctx)
	if err != nil {
		return nil, err
	}
	title, err := requiredString(input, "title")
	if err != nil {
		return nil, err
	}
	level := stringField(input, "level")
	if level == "" {
		// Heuristic: if a parent_id is provided, default to chapter
		// (most common case for "make a chapter outline"); otherwise
		// treat as a new master arc.
		if stringField(input, "parent_id") != "" {
			level = domain.LevelChapter
		} else {
			level = domain.LevelMaster
		}
	}
	a := &domain.Arc{
		ProjectID:  p.ID,
		ParentID:   stringField(input, "parent_id"),
		Level:      level,
		Title:      title,
		Summary:    stringField(input, "summary"),
		OrderIndex: intField(input, "order_index", 0),
		Metadata:   nestedMapField(input, "metadata"),
	}
	if err := repo.NewArcRepo(mgr.DB()).Create(ctx, a); err != nil {
		return nil, err
	}
	return map[string]any{"arc": arcToMap(a)}, nil
}

func arcToMap(a *domain.Arc) map[string]any {
	return map[string]any{
		"id":          a.ID,
		"project_id":  a.ProjectID,
		"parent_id":   a.ParentID,
		"level":       a.Level,
		"title":       a.Title,
		"summary":     a.Summary,
		"order_index": a.OrderIndex,
		"metadata":    a.Metadata,
		"created_at":  a.CreatedAt,
		"modified_at": a.ModifiedAt,
	}
}
