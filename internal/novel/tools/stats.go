package tools

import (
	"context"

	"reasonix/internal/novel/project"
	"reasonix/internal/novel/repo"
)

type statsTool struct{}

func init() { registerDefault(&statsTool{}) }

func (t *statsTool) Name() string { return "stats" }

func (t *statsTool) Description() string {
	return "写作统计：总章节/字数、按状态分布、按主线分布、近 30 天每日字数。"
}

func (t *statsTool) Execute(ctx context.Context, input map[string]any, mgr *project.Manager) (map[string]any, error) {
	p, err := mgr.Project(ctx)
	if err != nil {
		return nil, err
	}
	s, err := repo.NewStatsRepo(mgr.DB()).Build(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"total_chapters":     s.TotalChapters,
		"total_words":        s.TotalWords,
		"average_words":      s.AverageWords,
		"chapters_by_status": s.ChaptersByStatus,
		"chapters_by_arc":    s.ChaptersByArc,
		"daily_words":        s.DailyWords,
	}, nil
}
