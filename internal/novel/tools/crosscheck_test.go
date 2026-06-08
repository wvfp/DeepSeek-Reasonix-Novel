package tools

import (
	"context"
	"testing"

	"reasonix/internal/novel/domain"
	"reasonix/internal/novel/project"
	"reasonix/internal/novel/repo"
)

// crosscheckSeed builds a project with 3 chapter_facts on (萧炎, 在):
//   ch1 → 乌坦城
//   ch2 → 帝都
//   ch3 → 天云宗
// plus one unrelated fact on (林动, 在) that should NOT show up as
// a conflict because the subject differs.
func crosscheckSeed(t *testing.T) *project.Manager {
	t.Helper()
	mgr, _ := withProject(t, "测试")
	ctx := context.Background()
	pid := mustProjectID(t, mgr)

	ar := repo.NewArcRepo(mgr.DB())
	a := &domain.Arc{ProjectID: pid, Level: domain.LevelChapter, Title: "arc1"}
	if err := ar.Create(ctx, a); err != nil {
		t.Fatal(err)
	}

	cr := repo.NewChapterRepo(mgr.DB())
	ch1 := &domain.Chapter{ProjectID: pid, ArcID: a.ID, Volume: 1, ChapterNumber: 1, Title: "ch1", WordCount: 1}
	ch2 := &domain.Chapter{ProjectID: pid, ArcID: a.ID, Volume: 1, ChapterNumber: 2, Title: "ch2", WordCount: 1}
	ch3 := &domain.Chapter{ProjectID: pid, ArcID: a.ID, Volume: 1, ChapterNumber: 3, Title: "ch3", WordCount: 1}
	for _, c := range []*domain.Chapter{ch1, ch2, ch3} {
		if err := cr.Create(ctx, c); err != nil {
			t.Fatal(err)
		}
	}

	fr := repo.NewFactRepo(mgr.DB())
	facts := []*domain.ChapterFact{
		{ProjectID: pid, ChapterID: ch1.ID, FactType: domain.FactTypeLocation, Subject: "萧炎", Predicate: "在", Object: "乌坦城", Confidence: 1.0},
		{ProjectID: pid, ChapterID: ch2.ID, FactType: domain.FactTypeLocation, Subject: "萧炎", Predicate: "在", Object: "帝都", Confidence: 1.0},
		{ProjectID: pid, ChapterID: ch3.ID, FactType: domain.FactTypeLocation, Subject: "萧炎", Predicate: "在", Object: "天云宗", Confidence: 1.0},
		{ProjectID: pid, ChapterID: ch1.ID, FactType: domain.FactTypeLocation, Subject: "林动", Predicate: "在", Object: "天云宗", Confidence: 1.0},
	}
	for _, f := range facts {
		if err := fr.Create(ctx, f); err != nil {
			t.Fatal(err)
		}
	}
	return mgr
}

func TestCrosscheck_DetectsContradiction(t *testing.T) {
	mgr := crosscheckSeed(t)
	tool := &crosscheckTool{}
	out, err := tool.Execute(context.Background(), map[string]any{}, mgr)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	matches, ok := out["matches"].([]match)
	if !ok {
		t.Fatalf("matches field wrong type: %T", out["matches"])
	}
	if len(matches) != 1 {
		t.Fatalf("matches len = %d, want 1", len(matches))
	}
	m := matches[0]
	if m.Subject != "萧炎" || m.Predicate != "在" {
		t.Errorf("match key = %s/%s, want 萧炎/在", m.Subject, m.Predicate)
	}
	if len(m.ConflictWith) != 2 {
		t.Errorf("conflict_with len = %d, want 2", len(m.ConflictWith))
	}
	if v, _ := out["match_count"].(int); v != 1 {
		t.Errorf("match_count = %d, want 1", v)
	}
}

func TestCrosscheck_QueryFilter(t *testing.T) {
	mgr := crosscheckSeed(t)
	tool := &crosscheckTool{}
	// query "乌坦城" should still surface the (萧炎, 在) conflict
	// because the anchor row contains it. But a query that
	// excludes every conflicting row should return zero matches.
	out, err := tool.Execute(context.Background(),
		map[string]any{"query": "林动"}, mgr)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	matches, _ := out["matches"].([]match)
	if len(matches) != 0 {
		t.Errorf("matches with query=林动 = %d, want 0", len(matches))
	}
}

func TestCrosscheck_ChapterFilter(t *testing.T) {
	mgr := crosscheckSeed(t)
	tool := &crosscheckTool{}
	// Pin to a single chapter; with only one fact row in ch1 the
	// group has length 1 → no conflict.
	cr := repo.NewChapterRepo(mgr.DB())
	chs, _ := cr.List(context.Background(), mustProjectID(t, mgr))
	ch1ID := chs[0].ID
	out, err := tool.Execute(context.Background(),
		map[string]any{"chapter_id": ch1ID}, mgr)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	matches, _ := out["matches"].([]match)
	if len(matches) != 0 {
		t.Errorf("matches scoped to ch1 = %d, want 0", len(matches))
	}
}

func TestCrosscheck_EmptyProject(t *testing.T) {
	mgr, _ := withProject(t, "空")
	tool := &crosscheckTool{}
	out, err := tool.Execute(context.Background(), map[string]any{}, mgr)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	matches, _ := out["matches"].([]match)
	if len(matches) != 0 {
		t.Errorf("matches = %d, want 0", len(matches))
	}
}

func TestCrosscheck_IdenticalObjectsNotConflict(t *testing.T) {
	// Two facts with the SAME (subject, predicate, object) in two
	// chapters is consistency (a repeated truth), not a conflict.
	// The detector must drop them.
	mgr, _ := withProject(t, "测试")
	ctx := context.Background()
	pid := mustProjectID(t, mgr)

	ar := repo.NewArcRepo(mgr.DB())
	a := &domain.Arc{ProjectID: pid, Level: domain.LevelChapter, Title: "arc1"}
	if err := ar.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	cr := repo.NewChapterRepo(mgr.DB())
	ch1 := &domain.Chapter{ProjectID: pid, ArcID: a.ID, Volume: 1, ChapterNumber: 1, Title: "ch1", WordCount: 1}
	ch2 := &domain.Chapter{ProjectID: pid, ArcID: a.ID, Volume: 1, ChapterNumber: 2, Title: "ch2", WordCount: 1}
	for _, c := range []*domain.Chapter{ch1, ch2} {
		if err := cr.Create(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	fr := repo.NewFactRepo(mgr.DB())
	if err := fr.Create(ctx, &domain.ChapterFact{
		ProjectID: pid, ChapterID: ch1.ID, FactType: domain.FactTypeLocation,
		Subject: "萧炎", Predicate: "在", Object: "乌坦城", Confidence: 1.0,
	}); err != nil {
		t.Fatal(err)
	}
	if err := fr.Create(ctx, &domain.ChapterFact{
		ProjectID: pid, ChapterID: ch2.ID, FactType: domain.FactTypeLocation,
		Subject: "萧炎", Predicate: "在", Object: "乌坦城", Confidence: 1.0,
	}); err != nil {
		t.Fatal(err)
	}
	tool := &crosscheckTool{}
	out, _ := tool.Execute(context.Background(), map[string]any{}, mgr)
	matches, _ := out["matches"].([]match)
	if len(matches) != 0 {
		t.Errorf("identical objects should not be a conflict; got %d matches", len(matches))
	}
}
