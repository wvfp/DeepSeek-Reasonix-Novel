package tools

import (
	"context"
	"path/filepath"
	"testing"

	"reasonix/internal/novel/domain"
	"reasonix/internal/novel/project"
)

// withProject is the shared test helper: it creates a fresh project in
// a temp dir, calls novel_init, and registers the new project with the
// returned manager. Returns the manager + the domain.Project so the
// caller doesn't have to re-fetch.
func withProject(t *testing.T, name string) (*project.Manager, *domain.Project) {
	t.Helper()
	dir := t.TempDir()
	mgr, err := project.New(dir)
	if err != nil {
		t.Fatalf("project.New: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	reg := NewRegistry()
	init, _ := reg.Get("novel_init")
	out, err := init.Execute(context.Background(), map[string]any{"name": name}, mgr)
	if err != nil {
		t.Fatalf("novel_init: %v", err)
	}
	return mgr, projFromOutput(t, out)
}

func projFromOutput(t *testing.T, out map[string]any) *domain.Project {
	t.Helper()
	pm, _ := out["project"].(map[string]any)
	if pm == nil {
		t.Fatal("project field missing in init output")
	}
	return &domain.Project{
		ID:    stringField(pm, "id"),
		Name:  stringField(pm, "name"),
		Genre: stringField(pm, "genre"),
	}
}

func TestWorldCreateAndQuery(t *testing.T) {
	mgr, _ := withProject(t, "测试")
	reg := NewRegistry()
	ctx := context.Background()

	wc, _ := reg.Get("world_create")
	out, err := wc.Execute(ctx, map[string]any{"name": "九州", "description": "九州大地"}, mgr)
	if err != nil {
		t.Fatalf("world_create: %v", err)
	}
	wm, _ := out["world"].(map[string]any)
	if wm["name"] != "九州" {
		t.Errorf("world.name = %v, want 九州", wm["name"])
	}
	if path, _ := out["path"].(string); path == "" {
		t.Error("path missing in output")
	}

	wq, _ := reg.Get("world_query")
	q, err := wq.Execute(ctx, map[string]any{"name": "九州"}, mgr)
	if err != nil {
		t.Fatalf("world_query: %v", err)
	}
	list, _ := q["worlds"].([]any)
	if len(list) != 1 {
		t.Errorf("world_query len = %d, want 1", len(list))
	}
}

func TestWorldLink(t *testing.T) {
	mgr, _ := withProject(t, "测试")
	reg := NewRegistry()
	ctx := context.Background()

	wc, _ := reg.Get("world_create")
	w1, _ := wc.Execute(ctx, map[string]any{"name": "A"}, mgr)
	w2, _ := wc.Execute(ctx, map[string]any{"name": "B"}, mgr)

	wl, _ := reg.Get("world_link")
	out, err := wl.Execute(ctx, map[string]any{
		"from_id":  w1["world"].(map[string]any)["id"],
		"to_id":    w2["world"].(map[string]any)["id"],
		"relation": "adjacent_to",
		"note":     "海路相通",
	}, mgr)
	if err != nil {
		t.Fatalf("world_link: %v", err)
	}
	if _, ok := out["link"]; !ok {
		t.Error("link missing in output")
	}
}

func TestCharacterCreateQueryUpdate(t *testing.T) {
	mgr, _ := withProject(t, "测试")
	reg := NewRegistry()
	ctx := context.Background()

	cc, _ := reg.Get("character_create")
	out, err := cc.Execute(ctx, map[string]any{
		"name":        "萧炎",
		"description": "天才少年",
		"voice_profile": map[string]any{
			"speech_style":    "casual",
			"profanity_level": 1,
		},
	}, mgr)
	if err != nil {
		t.Fatalf("character_create: %v", err)
	}
	cm := out["character"].(map[string]any)
	id, _ := cm["id"].(string)

	cq, _ := reg.Get("character_query")
	q, err := cq.Execute(ctx, map[string]any{"id": id}, mgr)
	if err != nil {
		t.Fatalf("character_query: %v", err)
	}
	if list, _ := q["characters"].([]any); len(list) != 1 {
		t.Errorf("character_query len = %d, want 1", len(list))
	}

	cu, _ := reg.Get("character_update")
	u, err := cu.Execute(ctx, map[string]any{
		"id": id,
		"fields": map[string]any{
			"description": "更新后",
		},
	}, mgr)
	if err != nil {
		t.Fatalf("character_update: %v", err)
	}
	um, _ := u["character"].(map[string]any)
	if um["description"] != "更新后" {
		t.Errorf("description after update = %v, want 更新后", um["description"])
	}
}

func TestArcGenerateAndUpdate(t *testing.T) {
	mgr, _ := withProject(t, "测试")
	reg := NewRegistry()
	ctx := context.Background()

	ag, _ := reg.Get("arc_generate")
	a, err := ag.Execute(ctx, map[string]any{
		"title":   "第一卷",
		"level":   domain.LevelVolume,
		"summary": "开篇",
	}, mgr)
	if err != nil {
		t.Fatalf("arc_generate: %v", err)
	}
	am := a["arc"].(map[string]any)
	id, _ := am["id"].(string)

	au, _ := reg.Get("arc_update")
	u, err := au.Execute(ctx, map[string]any{
		"id": id,
		"fields": map[string]any{
			"summary": "更新后摘要",
		},
	}, mgr)
	if err != nil {
		t.Fatalf("arc_update: %v", err)
	}
	if um := u["arc"].(map[string]any); um["summary"] != "更新后摘要" {
		t.Errorf("summary = %v", um["summary"])
	}
}

func TestQueryTool(t *testing.T) {
	mgr, _ := withProject(t, "测试")
	reg := NewRegistry()
	ctx := context.Background()

	wc, _ := reg.Get("world_create")
	if _, err := wc.Execute(ctx, map[string]any{"name": "九州", "description": "epic"}, mgr); err != nil {
		t.Fatal(err)
	}
	cc, _ := reg.Get("character_create")
	if _, err := cc.Execute(ctx, map[string]any{"name": "萧炎", "description": "主角"}, mgr); err != nil {
		t.Fatal(err)
	}

	q, _ := reg.Get("query")
	out, err := q.Execute(ctx, map[string]any{"query": "萧"}, mgr)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	results, _ := out["results"].([]any)
	if len(results) == 0 {
		t.Error("query returned no results")
	}
}

func TestProgressAndStats(t *testing.T) {
	mgr, _ := withProject(t, "测试")
	reg := NewRegistry()
	ctx := context.Background()

	// chapter_write requires a LLMCaller. Install a fixture-returning
	// one in the package-level slot for the duration of this test.
	prev := DefaultLLMCaller()
	SetDefaultLLMCaller(&fakeLLM{payload: fixedChapterPayload})
	t.Cleanup(func() { SetDefaultLLMCaller(prev) })

	// Seed: 1 chapter + 1 world + 2 characters. The chapter_write
	// fixture references fixed IDs (char-xiao-yan / char-lin-dong /
	// world-jiuzhou) and the character_states table has a FK on
	// character_id, so we rename the rows we just inserted to those
	// IDs to satisfy the constraint. Same trick seedChapterWriteProject
	// uses; the test stays self-contained rather than depending on the
	// helper because we only need the data alive long enough to verify
	// progress + stats.
	wc, _ := reg.Get("world_create")
	if _, err := wc.Execute(ctx, map[string]any{"name": "X"}, mgr); err != nil {
		t.Fatal(err)
	}
	cc, _ := reg.Get("character_create")
	if _, err := cc.Execute(ctx, map[string]any{"name": "A"}, mgr); err != nil {
		t.Fatal(err)
	}
	if _, err := cc.Execute(ctx, map[string]any{"name": "B"}, mgr); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.DB().ExecContext(ctx,
		`UPDATE characters SET id = ? WHERE name = 'A' AND project_id = (SELECT id FROM projects LIMIT 1)`,
		"char-xiao-yan"); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.DB().ExecContext(ctx,
		`UPDATE characters SET id = ? WHERE name = 'B' AND project_id = (SELECT id FROM projects LIMIT 1)`,
		"char-lin-dong"); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.DB().ExecContext(ctx,
		`UPDATE worlds SET id = ? WHERE name = 'X' AND project_id = (SELECT id FROM projects LIMIT 1)`,
		"world-jiuzhou"); err != nil {
		t.Fatal(err)
	}
	ag, _ := reg.Get("arc_generate")
	arcOut, err := ag.Execute(ctx, map[string]any{"title": "arc1", "level": domain.LevelChapter}, mgr)
	if err != nil {
		t.Fatal(err)
	}
	arcID, _ := arcOut["arc"].(map[string]any)["id"].(string)

	cw, _ := reg.Get("chapter_write")
	if _, err := cw.Execute(ctx, map[string]any{
		"arc_id": arcID, "title": "ch1", "prompt": "",
	}, mgr); err != nil {
		t.Fatal(err)
	}

	pTool, _ := reg.Get("progress")
	p, err := pTool.Execute(ctx, map[string]any{}, mgr)
	if err != nil {
		t.Fatalf("progress: %v", err)
	}
	if v, _ := p["total_chapters"].(int); v != 1 {
		t.Errorf("progress.total_chapters = %v, want 1", p["total_chapters"])
	}

	sTool, _ := reg.Get("stats")
	s, err := sTool.Execute(ctx, map[string]any{}, mgr)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if _, ok := s["chapters_by_status"]; !ok {
		t.Error("stats: chapters_by_status missing")
	}
}

// keep the import live in case path-based test fixtures need to be
// added later.
var _ = filepath.Join
