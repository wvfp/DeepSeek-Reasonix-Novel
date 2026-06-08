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

// consistencyFixture: 5 issues spanning every dimension. The
// parser is supposed to bucket them by (dimension, severity) and
// expose one persisted Review row per issue. The chapters array of
// every issue intentionally starts with "ch1" so the
// PersistsReviews test can verify the ch1 bucket accumulates
// every issue; the additional ch2 / ch3 / ch4 / ch5 / ch6 ids
// let the SpaceContradictionDetected test assert the prompt
// embeds those ids verbatim.
const consistencyFixture = `{
  "issues": [
    {"dimension": "space", "severity": "blocker", "chapters": ["ch1", "ch2"], "description": "萧炎 在 乌坦城 与 萧炎 在 帝都 同一章无 travel 事实"},
    {"dimension": "时间线矛盾", "severity": "warning", "chapters": ["ch1", "ch3"], "description": "事件 A 在第 2 章后发生，却在第 1 章已被引用"},
    {"dimension": "character", "severity": "blocker", "chapters": ["ch1", "ch4"], "description": "林动 境界 斗师→斗者 倒退"},
    {"dimension": "item", "severity": "info", "chapters": ["ch1", "ch5"], "description": "焚决 同时被 萧炎 和 林动 持有"},
    {"dimension": "setting", "severity": "warning", "chapters": ["ch1", "ch6"], "description": "筑基期使用元婴期功法，违反力量体系"}
  ]
}`

// consistencyScriptedLLM returns the consistency fixture whenever
// the Reviewer role is dispatched. Kept separate from scriptedLLM
// in chapter_review_test.go so the two test files don't fight
// over the same fixture.
type consistencyScriptedLLM struct {
	payload string
}

func (c *consistencyScriptedLLM) Call(_ context.Context, system, _ string) (string, error) {
	head := system
	if len(head) > 200 {
		head = head[:200]
	}
	if strings.Contains(head, "Reviewer") || strings.Contains(head, "审查员") {
		return c.payload, nil
	}
	return `{"issues": []}`, nil
}

// seedConsistencyProject creates a project with 1 world, 1 character,
// 2 chapters, 4 facts (one is a deliberate contradiction), and 1
// character state. Used by both consistency and crosscheck tests.
func seedConsistencyProject(t *testing.T) *project.Manager {
	t.Helper()
	mgr, _ := withProject(t, "测试")
	ctx := context.Background()
	pid := mustProjectID(t, mgr)

	// Insert one character with a known id so the crosscheck query
	// can target it deterministically.
	cr := repo.NewCharacterRepo(mgr.DB())
	c := &domain.Character{ProjectID: pid, Name: "萧炎"}
	if err := cr.Create(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.DB().ExecContext(ctx,
		`UPDATE characters SET id = ? WHERE id = ?`, "char-xiao-yan", c.ID); err != nil {
		t.Fatal(err)
	}

	// Create one chapter-level arc so the chapter rows are valid.
	ar := repo.NewArcRepo(mgr.DB())
	a := &domain.Arc{ProjectID: pid, Level: domain.LevelChapter, Title: "arc1"}
	if err := ar.Create(ctx, a); err != nil {
		t.Fatal(err)
	}

	// Insert 6 chapters directly. We need real chapter IDs for the
	// crosscheck assertion, so we bypass the chapter_write tool
	// (which would call the LLM). The IDs are pinned to the
	// well-known "ch1"…"ch6" labels the consistency fixture and
	// the prompt-assertion test rely on.
	chapterRepo := repo.NewChapterRepo(mgr.DB())
	chapters := []*domain.Chapter{
		{ProjectID: pid, ArcID: a.ID, Volume: 1, ChapterNumber: 1, Title: "乌坦城", Content: "萧炎在乌坦城。", WordCount: 6, Status: "draft"},
		{ProjectID: pid, ArcID: a.ID, Volume: 1, ChapterNumber: 2, Title: "帝都", Content: "萧炎在帝都。", WordCount: 6, Status: "draft"},
		{ProjectID: pid, ArcID: a.ID, Volume: 1, ChapterNumber: 3, Title: "中州", Content: "中州城外。", WordCount: 5, Status: "draft"},
		{ProjectID: pid, ArcID: a.ID, Volume: 1, ChapterNumber: 4, Title: "古族", Content: "古族祖地。", WordCount: 5, Status: "draft"},
		{ProjectID: pid, ArcID: a.ID, Volume: 1, ChapterNumber: 5, Title: "魂殿", Content: "魂殿分殿。", WordCount: 5, Status: "draft"},
		{ProjectID: pid, ArcID: a.ID, Volume: 1, ChapterNumber: 6, Title: "虚空", Content: "雷府外。", WordCount: 4, Status: "draft"},
	}
	for _, c := range chapters {
		if err := chapterRepo.Create(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	// Pin the auto-generated UUIDs to deterministic "chN" labels.
	// The fixture and prompt assertion rely on these specific ids
	// appearing in the persisted reviews / rendered prompt.
	pinned := []string{"ch1", "ch2", "ch3", "ch4", "ch5", "ch6"}
	for i, c := range chapters {
		if _, err := mgr.DB().ExecContext(ctx,
			`UPDATE chapters SET id = ? WHERE id = ?`, pinned[i], c.ID); err != nil {
			t.Fatal(err)
		}
	}
	// Refresh the in-memory id slots so the rest of the seed can
	// reference the pinned ids without round-tripping through SQL.
	for i, c := range chapters {
		c.ID = pinned[i]
	}

	ch1, ch2 := chapters[0], chapters[1]

	// Insert 4 chapter_facts. The first three are consistent; the
	// fourth contradicts the first by claiming 萧炎 is in a
	// different place in chapter 2.
	fr := repo.NewFactRepo(mgr.DB())
	if err := fr.Create(ctx, &domain.ChapterFact{
		ProjectID: pid, ChapterID: ch1.ID, FactType: domain.FactTypeLocation,
		Subject: "萧炎", Predicate: "在", Object: "乌坦城", Confidence: 1.0,
	}); err != nil {
		t.Fatal(err)
	}
	if err := fr.Create(ctx, &domain.ChapterFact{
		ProjectID: pid, ChapterID: ch2.ID, FactType: domain.FactTypeLocation,
		Subject: "萧炎", Predicate: "在", Object: "帝都", Confidence: 1.0,
	}); err != nil {
		t.Fatal(err)
	}
	// Two more facts with the same (subject, predicate) so
	// crosscheck has plenty of signal to detect the conflict.
	if err := fr.Create(ctx, &domain.ChapterFact{
		ProjectID: pid, ChapterID: ch1.ID, FactType: domain.FactTypeLocation,
		Subject: "萧炎", Predicate: "location", Object: "乌坦城东街", Confidence: 1.0,
	}); err != nil {
		t.Fatal(err)
	}
	if err := fr.Create(ctx, &domain.ChapterFact{
		ProjectID: pid, ChapterID: ch2.ID, FactType: domain.FactTypeLocation,
		Subject: "萧炎", Predicate: "location", Object: "帝都内城", Confidence: 1.0,
	}); err != nil {
		t.Fatal(err)
	}

	// Insert one character_state per chapter so the prompt has
	// the character dimension exercised.
	sr := repo.NewStateRepo(mgr.DB())
	if err := sr.Create(ctx, &domain.CharacterState{
		Location: "乌坦城", Power: "斗者", Mood: "警觉",
	}, pid, "char-xiao-yan", ch1.ID, []string{"主角"}); err != nil {
		t.Fatal(err)
	}
	if err := sr.Create(ctx, &domain.CharacterState{
		Location: "帝都", Power: "斗师", Mood: "平静",
	}, pid, "char-xiao-yan", ch2.ID, []string{"主角"}); err != nil {
		t.Fatal(err)
	}
	return mgr
}

func newConsistencySwitcher(t *testing.T, llm roles.LLMCaller) *roles.Switcher {
	t.Helper()
	sw, err := roles.NewSwitcher(llm, "BASE")
	if err != nil {
		t.Fatalf("NewSwitcher: %v", err)
	}
	return sw
}

func TestConsistency_HappyPath(t *testing.T) {
	mgr := seedConsistencyProject(t)
	llm := &consistencyScriptedLLM{payload: consistencyFixture}
	tool := NewConsistencyTool(newConsistencySwitcher(t, llm))

	out, err := tool.Execute(context.Background(), map[string]any{}, mgr)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	issues, ok := out["issues"].([]consistencyIssue)
	if !ok {
		t.Fatalf("issues field missing or wrong type: %T", out["issues"])
	}
	if len(issues) != 5 {
		t.Errorf("issues len = %d, want 5", len(issues))
	}
	counts, _ := out["dimension_counts"].(map[string]int)
	if counts == nil {
		t.Fatal("dimension_counts missing")
	}
	if counts["space"] != 1 || counts["time"] != 1 || counts["character"] != 1 || counts["item"] != 1 || counts["setting"] != 1 {
		t.Errorf("dimension_counts = %v, want 1 of every dimension", counts)
	}
	if v, _ := out["issue_count"].(int); v != 5 {
		t.Errorf("issue_count = %d, want 5", v)
	}
}

func TestConsistency_PersistsReviews(t *testing.T) {
	mgr := seedConsistencyProject(t)
	llm := &consistencyScriptedLLM{payload: consistencyFixture}
	tool := NewConsistencyTool(newConsistencySwitcher(t, llm))

	if _, err := tool.Execute(context.Background(), map[string]any{}, mgr); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	rr := repo.NewReviewRepo(mgr.DB())
	rows, err := rr.ListByChapter(context.Background(), "ch1")
	if err != nil {
		t.Fatalf("ListByChapter: %v", err)
	}
	// ch1 is the first chapter in every chapters array, so every
	// issue should produce one row in the ch1 review bucket.
	if len(rows) != 5 {
		t.Errorf("persisted rows = %d, want 5 (one per issue)", len(rows))
	}
}

func TestConsistency_NoLLM(t *testing.T) {
	mgr := seedConsistencyProject(t)
	tool := NewConsistencyTool(nil)
	if _, err := tool.Execute(context.Background(), map[string]any{}, mgr); err == nil {
		t.Fatal("expected error when LLM is nil")
	}
}

func TestConsistency_EmptyProject(t *testing.T) {
	mgr, _ := withProject(t, "空")
	llm := &consistencyScriptedLLM{payload: `{"issues": []}`}
	tool := NewConsistencyTool(newConsistencySwitcher(t, llm))
	out, err := tool.Execute(context.Background(), map[string]any{}, mgr)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	issues, _ := out["issues"].([]consistencyIssue)
	if len(issues) != 0 {
		t.Errorf("empty project should yield 0 issues, got %d", len(issues))
	}
}

func TestConsistency_ParseMarkdownFence(t *testing.T) {
	mgr := seedConsistencyProject(t)
	fenced := "```json\n" + consistencyFixture + "\n```"
	llm := &consistencyScriptedLLM{payload: fenced}
	tool := NewConsistencyTool(newConsistencySwitcher(t, llm))
	if _, err := tool.Execute(context.Background(), map[string]any{}, mgr); err != nil {
		t.Fatalf("Execute with fenced JSON: %v", err)
	}
}

func TestConsistency_NormaliseSynonyms(t *testing.T) {
	cases := map[string]string{
		"time":      "time",
		"时间":       "time",
		"时间线":      "time",
		"时间线矛盾":   "time",
		"空间":       "space",
		"地点":       "space",
		"人物":       "character",
		"角色":       "character",
		"character": "character",
		"item":      "item",
		"物品":       "item",
		"setting":   "setting",
		"世界观":      "setting",
		"未知":       "setting",
	}
	for in, want := range cases {
		if got := normaliseDimension(in); got != want {
			t.Errorf("normaliseDimension(%q) = %q, want %q", in, got, want)
		}
	}
	// Severity normalisation.
	if got := normaliseSeverity("严重"); got != "blocker" {
		t.Errorf("normaliseSeverity(严重) = %s, want blocker", got)
	}
	if got := normaliseSeverity("warning"); got != "warning" {
		t.Errorf("normaliseSeverity(warning) = %s, want warning", got)
	}
	if got := normaliseSeverity(""); got != "info" {
		t.Errorf("normaliseSeverity(empty) = %s, want info", got)
	}
}

func TestParseConsistencyPayload(t *testing.T) {
	p, err := parseConsistencyPayload("```json\n" + consistencyFixture + "\n```")
	if err != nil {
		t.Fatalf("parseConsistencyPayload: %v", err)
	}
	if len(p) != 5 {
		t.Errorf("len = %d, want 5", len(p))
	}
	if p[0].Dimension != "space" {
		t.Errorf("first issue dimension = %q, want space", p[0].Dimension)
	}
}

func TestConsistency_SpaceContradictionDetected(t *testing.T) {
	// End-to-end: the seeded project has 萧炎 in 乌坦城 (ch1) and
	// 萧炎 in 帝都 (ch2) with no travel fact. The LLM fixture
	// claims to detect exactly that. We verify the prompt actually
	// contains both chapter_ids by inspecting the user prompt the
	// tool would have built, then re-running the Execute against
	// a recording LLM.
	mgr := seedConsistencyProject(t)

	rec := &recordingLLM{}
	rec.onReviewer = func(system, user string) string {
		// Make sure the prompt embeds both chapter ids and the
		// contradicting objects.
		if !strings.Contains(user, "乌坦城") || !strings.Contains(user, "帝都") {
			t.Errorf("prompt missing contradiction anchors: %s", user)
		}
		if !strings.Contains(user, "ch1") || !strings.Contains(user, "ch2") {
			t.Errorf("prompt missing chapter ids: %s", user)
		}
		return consistencyFixture
	}
	tool := NewConsistencyTool(newConsistencySwitcher(t, rec))
	if _, err := tool.Execute(context.Background(), map[string]any{}, mgr); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

// recordingLLM is a LLMCaller that dispatches by system-prompt
// header. Used by TestConsistency_SpaceContradictionDetected to
// inspect the prompt the tool builds before the LLM answers.
type recordingLLM struct {
	onReviewer func(system, user string) string
}

func (r *recordingLLM) Call(_ context.Context, system, user string) (string, error) {
	head := system
	if len(head) > 200 {
		head = head[:200]
	}
	if r.onReviewer != nil && (strings.Contains(head, "Reviewer") || strings.Contains(head, "审查员")) {
		return r.onReviewer(system, user), nil
	}
	return `{"issues": []}`, nil
}
