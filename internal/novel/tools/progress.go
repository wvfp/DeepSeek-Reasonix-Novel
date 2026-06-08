package tools

import (
	"context"

	"reasonix/internal/novel/project"
	"reasonix/internal/novel/repo"
)

type progressTool struct{}

func init() { registerDefault(&progressTool{}) }

func (t *progressTool) Name() string { return "progress" }

func (t *progressTool) Description() string {
	return "查看当前项目写作进度（章节数、总字数、当前主线、活跃角色等）。"
}

func (t *progressTool) Execute(ctx context.Context, input map[string]any, mgr *project.Manager) (map[string]any, error) {
	p, err := mgr.Project(ctx)
	if err != nil {
		return nil, err
	}
	summary, err := repo.NewProgressRepo(mgr.DB()).Build(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"total_chapters": summary.TotalChapters,
		"total_words":    summary.TotalWords,
		"current_arc":    summary.CurrentArc,
		"current_arc_id": summary.CurrentArcID,
		"active_chars":   summary.ActiveChars,
		"worlds":         summary.Worlds,
		"pipeline_phase": summary.PipelinePhase,
	}, nil
}
