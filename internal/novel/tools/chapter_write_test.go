package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/novel/domain"
	"reasonix/internal/novel/project"
	"reasonix/internal/novel/repo"
)

// fakeLLM is the LLMCaller the chapter_write tests use. It returns a
// fixture payload that the parseChapterPayload helper is expected to
// read. The fixture is realistic: 3 facts, 2 character states, 2 KG
// edges, plus a chapter body.
type fakeLLM struct {
	payload string
}

func (f *fakeLLM) Call(_ context.Context, _, _ string) (string, error) {
	return f.payload, nil
}

// fixedChapterPayload is the fixture returned by fakeLLM. It deliberately
// includes a forbidden word ("忽然") so the warning path is exercised.
const fixedChapterPayload = `{
  "chapter_text": "第一章 开篇。\n\n萧炎在乌坦城的街道上缓缓而行。忽然，一阵风吹过，他的衣袂翻飞。\n\n远处的天云宗若隐若现，灵气氤氲。\n\n他继续前行，脚步沉稳。",
  "facts": [
    {"fact_type": "event", "subject": "萧炎", "predicate": "走在", "object": "乌坦城街道", "context": "开头段落"},
    {"fact_type": "location", "subject": "萧炎", "predicate": "在", "object": "乌坦城", "context": ""},
    {"fact_type": "item", "subject": "萧炎", "predicate": "穿着", "object": "灰色长袍", "context": ""}
  ],
  "character_states": [
    {"character_id": "char-xiao-yan", "snapshot": {"location": "乌坦城街道", "power": "斗者", "mood": "警觉"}, "tags": ["主角"]},
    {"character_id": "char-lin-dong", "snapshot": {"location": "天云宗", "power": "斗师", "mood": "平静"}, "tags": ["盟友"]}
  ],
  "kg_edges": [
    {"from_entity_type": "character", "from_entity_id": "char-xiao-yan", "to_entity_type": "character", "to_entity_id": "char-lin-dong", "relation": "allies"},
    {"from_entity_type": "character", "from_entity_id": "char-xiao-yan", "to_entity_type": "world", "to_entity_id": "world-jiuzhou", "relation": "located_in"}
  ]
}`

// seedChapterWriteProject sets up a project with one world + two
// characters + one arc, ready for chapter_write. Returns the manager
// and the arc id.
func seedChapterWriteProject(t *testing.T) (*project.Manager, string) {
	t.Helper()
	mgr, _ := withProject(t, "测试")
	ctx := context.Background()
	wr := repo.NewWorldRepo(mgr.DB())
	cr := repo.NewCharacterRepo(mgr.DB())

	w := &domain.World{ProjectID: mustProjectID(t, mgr), Name: "九州"}
	if err := wr.Create(ctx, w); err != nil {
		t.Fatal(err)
	}
	c1 := &domain.Character{ProjectID: mustProjectID(t, mgr), Name: "萧炎"}
	if err := cr.Create(ctx, c1); err != nil {
		t.Fatal(err)
	}
	c2 := &domain.Character{ProjectID: mustProjectID(t, mgr), Name: "林动"}
	if err := cr.Create(ctx, c2); err != nil {
		t.Fatal(err)
	}

	// chapter_write reads character_id as a literal string in its
	// character_states payload — that string is the domain row's ID.
	// The fixture references char-xiao-yan / char-lin-dong, so we
	// rename the characters to those IDs.
	if _, err := mgr.DB().ExecContext(ctx, `UPDATE characters SET id = ? WHERE id = ?`, "char-xiao-yan", c1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.DB().ExecContext(ctx, `UPDATE characters SET id = ? WHERE id = ?`, "char-lin-dong", c2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.DB().ExecContext(ctx, `UPDATE worlds SET id = ? WHERE id = ?`, "world-jiuzhou", w.ID); err != nil {
		t.Fatal(err)
	}

	ar := repo.NewArcRepo(mgr.DB())
	a := &domain.Arc{ProjectID: mustProjectID(t, mgr), Level: domain.LevelChapter, Title: "第一章大纲"}
	if err := ar.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	return mgr, a.ID
}

func mustProjectID(t *testing.T, mgr *project.Manager) string {
	t.Helper()
	p, err := mgr.Project(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return p.ID
}

func TestChapterWrite_HappyPath(t *testing.T) {
	mgr, arcID := seedChapterWriteProject(t)
	llm := &fakeLLM{payload: fixedChapterPayload}
	cw := NewChapterWriteTool(llm)
	ctx := context.Background()

	out, err := cw.Execute(ctx, map[string]any{
		"arc_id": arcID,
		"title":  "陨落的天才",
		"prompt": "突出开篇的意境",
		"genre":  domain.GenreXianxia,
	}, mgr)
	if err != nil {
		t.Fatalf("chapter_write: %v", err)
	}
	if v, _ := out["facts_written"].(int); v != 3 {
		t.Errorf("facts_written = %v, want 3", out["facts_written"])
	}
	if v, _ := out["states_written"].(int); v != 2 {
		t.Errorf("states_written = %v, want 2", out["states_written"])
	}
	if v, _ := out["kg_edges_written"].(int); v != 2 {
		t.Errorf("kg_edges_written = %v, want 2", out["kg_edges_written"])
	}
	if v, _ := out["aliases_written"].(int); v < 2 {
		t.Errorf("aliases_written = %v, want ≥ 2", out["aliases_written"])
	}

	// After Phase 4 the chapter_write flow runs the anti-AI
	// scrubber first, so "忽然" / "缓缓" are stripped before the
	// forbidden-words pass. "一阵" is in FORBIDDEN_WORDS but
	// not in the anti-AI rule set, so it is the surviving hit.
	// The anti_ai_fixed field independently proves the scrubber
	// saw the LLM's slop.
	warnings, _ := out["warnings"].([]string)
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "一阵") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 一阵 in warnings, got %v", warnings)
	}
	if v, _ := out["anti_ai_fixed"].(int); v < 1 {
		t.Errorf("anti_ai_fixed = %v, want ≥ 1", out["anti_ai_fixed"])
	}

	// Markdown file should exist.
	chapter := out["chapter"].(map[string]any)
	if path, _ := out["path"].(string); path == "" {
		t.Fatal("path missing")
	} else {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("markdown file missing: %v", err)
		}
		if !strings.Contains(filepath.Base(path), "陨落") && !strings.Contains(filepath.Base(path), "ch01") {
			t.Errorf("unexpected chapter filename: %s", filepath.Base(path))
		}
	}
	_ = chapter

	// Verify SQLite: chapter row + facts + states + KG nodes + edges + aliases.
	if n := dbCount(t, mgr, "chapters"); n != 1 {
		t.Errorf("chapters count = %d, want 1", n)
	}
	if n := dbCount(t, mgr, "chapter_facts"); n != 3 {
		t.Errorf("chapter_facts count = %d, want 3", n)
	}
	if n := dbCount(t, mgr, "character_states"); n != 2 {
		t.Errorf("character_states count = %d, want 2", n)
	}
	if n := dbCount(t, mgr, "knowledge_graph_nodes"); n < 3 {
		// character-x2, world-x1
		t.Errorf("kg nodes count = %d, want ≥ 3", n)
	}
	if n := dbCount(t, mgr, "knowledge_graph_edges"); n != 2 {
		t.Errorf("kg edges count = %d, want 2", n)
	}
	if n := dbCount(t, mgr, "aliases"); n < 2 {
		t.Errorf("aliases count = %d, want ≥ 2", n)
	}
}

func TestChapterWrite_NoLLM(t *testing.T) {
	mgr, arcID := seedChapterWriteProject(t)
	cw := &chapterWriteTool{} // nil LLM
	if _, err := cw.Execute(context.Background(), map[string]any{
		"arc_id": arcID, "title": "t",
	}, mgr); err == nil {
		t.Fatal("expected error when LLM is nil")
	}
}

func TestChapterWrite_EmptyOutput(t *testing.T) {
	mgr, arcID := seedChapterWriteProject(t)
	cw := NewChapterWriteTool(&fakeLLM{payload: `{"chapter_text": "", "facts": []}`})
	if _, err := cw.Execute(context.Background(), map[string]any{
		"arc_id": arcID, "title": "t",
	}, mgr); err == nil {
		t.Fatal("expected error on empty chapter_text")
	}
}

func TestChapterWrite_ParseMarkdownFence(t *testing.T) {
	mgr, arcID := seedChapterWriteProject(t)
	payload := "```json\n" + fixedChapterPayload + "\n```"
	cw := NewChapterWriteTool(&fakeLLM{payload: payload})
	if _, err := cw.Execute(context.Background(), map[string]any{
		"arc_id": arcID, "title": "t",
	}, mgr); err != nil {
		t.Fatalf("Execute with fenced JSON: %v", err)
	}
}

func TestEnforceParagraphLimit(t *testing.T) {
	longPara := strings.Repeat("啊", 600)
	got := enforceParagraphLimit(longPara+"\n\nshort", 500)
	parts := strings.Split(got, "\n\n")
	if len(parts) < 2 {
		t.Errorf("expected split, got %d parts", len(parts))
	}
}

func TestCheckForbiddenWords(t *testing.T) {
	hits := checkForbiddenWords("他忽然出现")
	if len(hits) == 0 {
		t.Error("expected at least one hit for 忽然")
	}
	hits = checkForbiddenWords("clean text here")
	if len(hits) != 0 {
		t.Errorf("expected 0 hits, got %v", hits)
	}
}

func TestCountWords(t *testing.T) {
	if got := countWords("中文 english"); got < 3 {
		t.Errorf("countWords = %d, want ≥ 3", got)
	}
}

func dbCount(t *testing.T, mgr *project.Manager, table string) int {
	t.Helper()
	var n int
	row := mgr.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+table)
	if err := row.Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}
