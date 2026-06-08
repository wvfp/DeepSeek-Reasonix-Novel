package tools

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/novel/domain"
	"reasonix/internal/novel/project"
	"reasonix/internal/novel/repo"
	"reasonix/internal/novel/roles"
)

// scriptedLLM returns fixture strings based on the system prompt the
// caller dispatches with. chapter_review fixtures the Reviewer
// output; review_fix dispatches PlotWriter; anything else returns
// the passthrough fixture. The dispatch checks the role header at
// the top of the prompt so cross-references in the role bodies
// ("→ Reviewer") don't trigger the wrong fixture.
type scriptedLLM struct {
	reviewPayload    string
	reviewFixPayload string
}

func (s *scriptedLLM) Call(_ context.Context, system, _ string) (string, error) {
	// The role body always starts with "# 角色：" and contains the
	// role's display name within the first 200 chars.
	head := system
	if len(head) > 200 {
		head = head[:200]
	}
	switch {
	case strings.Contains(head, "Reviewer") || strings.Contains(head, "审查员"):
		return s.reviewPayload, nil
	case strings.Contains(head, "PlotWriter") || strings.Contains(head, "章节写手"):
		return s.reviewFixPayload, nil
	}
	return s.reviewFixPayload, nil
}

// reviewFixture: 1 dimension with 1 blocker + 1 warning so
// chapter_review writes 1 row and review_fix has at least 1 blocker
// to repair.
const reviewFixture = `{
  "reviews": [
    {
      "dimension": "consistency",
      "score": 4.0,
      "issues": [
        {"severity": "blocker", "description": "境界前后矛盾", "location": "第 2 段"},
        {"severity": "warning", "description": "地名不一致", "location": "第 5 段"}
      ]
    },
    {
      "dimension": "hook",
      "score": 6.0,
      "issues": []
    }
  ],
  "summary": {
    "blocker": 1,
    "warning": 1,
    "info": 0,
    "overall_score": 5.0,
    "verdict": "needs_fix"
  }
}`

// fixFixture: a different chapter_text so we can prove the chapter
// was actually rewritten by review_fix.
const fixFixture = `{
  "chapter_text": "重写后的章节正文，比原版更短但修复了 blocker。\n\n对话一段，对话二段。\n\n结尾。"
}`

// seedReviewProject sets up a project with 1 world + 1 chapter
// whose content is long enough to be reviewable. The chapter_write
// fixture is invoked via the tool so facts / states / KG edges are
// also persisted (the review tool reads them via the repos).
func seedReviewProject(t *testing.T) *project.Manager {
	t.Helper()
	mgr, arcID := seedChapterWriteProject(t)

	prev := DefaultLLMCaller()
	SetDefaultLLMCaller(&fakeLLM{payload: fixedChapterPayload})
	t.Cleanup(func() { SetDefaultLLMCaller(prev) })

	ctx := context.Background()
	cw, _ := NewRegistry().Get("chapter_write")
	if _, err := cw.Execute(ctx, map[string]any{
		"arc_id": arcID,
		"title":  "被审的章",
		"prompt": "无",
	}, mgr); err != nil {
		t.Fatalf("chapter_write seed: %v", err)
	}
	return mgr
}

func newReviewSwitcher(t *testing.T, llm *scriptedLLM) *roles.Switcher {
	t.Helper()
	sw, err := roles.NewSwitcher(llm, "BASE")
	if err != nil {
		t.Fatalf("NewSwitcher: %v", err)
	}
	return sw
}

func TestChapterReview_HappyPath(t *testing.T) {
	mgr := seedReviewProject(t)
	llm := &scriptedLLM{reviewPayload: reviewFixture, reviewFixPayload: fixFixture}
	rt := NewChapterReviewTool(newReviewSwitcher(t, llm))
	// chapter_write was called once; review the first chapter.
	ctx := context.Background()
	out, err := rt.Execute(ctx, map[string]any{}, mgr)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if v, _ := out["persisted"].(int); v != 2 {
		t.Errorf("persisted = %v, want 2 (one row per dimension)", out["persisted"])
	}
	summary, _ := out["summary"].(map[string]any)
	if summary == nil {
		t.Fatal("summary missing from chapter_review output")
	}
	if v, _ := summary["verdict"].(string); v != "needs_fix" {
		t.Errorf("summary.verdict = %v, want needs_fix", summary["verdict"])
	}

	// Verify the rows landed in SQLite.
	cr := repo.NewChapterRepo(mgr.DB())
	chs, _ := cr.List(ctx, mustProjectID(t, mgr))
	if len(chs) != 1 {
		t.Fatalf("expected 1 chapter, got %d", len(chs))
	}
	rows, err := repo.NewReviewRepo(mgr.DB()).ListByChapter(ctx, chs[0].ID)
	if err != nil {
		t.Fatalf("ListByChapter: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("reviews rows = %d, want 2", len(rows))
	}
}

func TestChapterReview_NoLLM(t *testing.T) {
	mgr := seedReviewProject(t)
	rt := NewChapterReviewTool(nil)
	if _, err := rt.Execute(context.Background(), map[string]any{}, mgr); err == nil {
		t.Fatal("expected error when LLM is nil")
	}
}

func TestChapterReview_ByChapterID(t *testing.T) {
	mgr := seedReviewProject(t)
	cr := repo.NewChapterRepo(mgr.DB())
	chs, _ := cr.List(context.Background(), mustProjectID(t, mgr))
	llm := &scriptedLLM{reviewPayload: reviewFixture, reviewFixPayload: fixFixture}
	rt := NewChapterReviewTool(newReviewSwitcher(t, llm))
	out, err := rt.Execute(context.Background(),
		map[string]any{"chapter_id": chs[0].ID}, mgr)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if v, _ := out["chapter_id"].(string); v != chs[0].ID {
		t.Errorf("chapter_id = %v, want %s", out["chapter_id"], chs[0].ID)
	}
}

func TestChapterReview_ParseMarkdownFence(t *testing.T) {
	mgr := seedReviewProject(t)
	fenced := "```json\n" + reviewFixture + "\n```"
	llm := &scriptedLLM{reviewPayload: fenced, reviewFixPayload: fixFixture}
	rt := NewChapterReviewTool(newReviewSwitcher(t, llm))
	if _, err := rt.Execute(context.Background(), map[string]any{}, mgr); err != nil {
		t.Fatalf("Execute with fenced JSON: %v", err)
	}
}

func TestChapterReview_NoChapter(t *testing.T) {
	// Project without any chapter should error.
	mgr, _ := withProject(t, "空")
	llm := &scriptedLLM{reviewPayload: reviewFixture, reviewFixPayload: fixFixture}
	rt := NewChapterReviewTool(newReviewSwitcher(t, llm))
	if _, err := rt.Execute(context.Background(), map[string]any{}, mgr); err == nil {
		t.Fatal("expected error when no chapters exist")
	}
}

func TestReviewFix_NoBlockers(t *testing.T) {
	mgr := seedReviewProject(t)
	// Insert a single info-only review (no blockers).
	cr := repo.NewChapterRepo(mgr.DB())
	chs, _ := cr.List(context.Background(), mustProjectID(t, mgr))
	rr := repo.NewReviewRepo(mgr.DB())
	if err := rr.Create(context.Background(), &domain.Review{
		ChapterID: chs[0].ID,
		Dimension: "info-only",
		Score:     8.0,
		Issues: []domain.Issue{
			{Severity: domain.SeverityInfo, Description: "无害", Location: ""},
		},
		CreatedAt: 1,
	}); err != nil {
		t.Fatalf("seed review: %v", err)
	}
	llm := &scriptedLLM{reviewPayload: reviewFixture, reviewFixPayload: fixFixture}
	rt := NewReviewFixTool(newReviewSwitcher(t, llm))
	out, err := rt.Execute(context.Background(),
		map[string]any{"chapter_id": chs[0].ID}, mgr)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if v, _ := out["no_blockers"].(bool); !v {
		t.Errorf("expected no_blockers=true, got %v", out["no_blockers"])
	}
}

func TestReviewFix_FixesBlocker(t *testing.T) {
	mgr := seedReviewProject(t)
	cr := repo.NewChapterRepo(mgr.DB())
	chs, _ := cr.List(context.Background(), mustProjectID(t, mgr))
	chapterID := chs[0].ID
	orig := chs[0].Content

	// Seed 3 reviews with 1 blocker each → 3 total blockers.
	rr := repo.NewReviewRepo(mgr.DB())
	for i, dim := range []string{"plot", "style", "consistency"} {
		if err := rr.Create(context.Background(), &domain.Review{
			ChapterID: chapterID,
			Dimension: dim,
			Score:     4.0,
			Issues: []domain.Issue{
				{Severity: domain.SeverityBlocker, Description: "blocker " + dim, Location: "第 1 段"},
			},
			CreatedAt: int64(100 + i),
		}); err != nil {
			t.Fatalf("seed review %d: %v", i, err)
		}
	}

	llm := &scriptedLLM{reviewPayload: reviewFixture, reviewFixPayload: fixFixture}
	rt := NewReviewFixTool(newReviewSwitcher(t, llm))
	out, err := rt.Execute(context.Background(),
		map[string]any{"chapter_id": chapterID}, mgr)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if v, _ := out["fixed_count"].(int); v != 3 {
		t.Errorf("fixed_count = %v, want 3", out["fixed_count"])
	}

	// The chapter content must have been replaced.
	updated, err := cr.Get(context.Background(), chapterID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Content == orig {
		t.Error("review_fix did not change chapter.Content")
	}
	if !strings.Contains(updated.Content, "重写") {
		t.Errorf("chapter content not patched with fix fixture: %q", updated.Content[:min(60, len(updated.Content))])
	}
}

func TestReviewFix_NoReviews(t *testing.T) {
	mgr := seedReviewProject(t)
	llm := &scriptedLLM{reviewPayload: reviewFixture, reviewFixPayload: fixFixture}
	rt := NewReviewFixTool(newReviewSwitcher(t, llm))
	if _, err := rt.Execute(context.Background(), map[string]any{}, mgr); err == nil {
		t.Fatal("expected error when no reviews exist for the chapter")
	}
}

func TestReviewFix_NoLLM(t *testing.T) {
	mgr := seedReviewProject(t)
	rt := NewReviewFixTool(nil)
	if _, err := rt.Execute(context.Background(), map[string]any{}, mgr); err == nil {
		t.Fatal("expected error when LLM is nil")
	}
}

func TestParseReviewPayload(t *testing.T) {
	p, err := parseReviewPayload("```json\n" + reviewFixture + "\n```")
	if err != nil {
		t.Fatalf("parseReviewPayload: %v", err)
	}
	if len(p.Reviews) != 2 {
		t.Errorf("Reviews len = %d, want 2", len(p.Reviews))
	}
	if p.Summary.Blocker != 1 {
		t.Errorf("Summary.Blocker = %d, want 1", p.Summary.Blocker)
	}
}
