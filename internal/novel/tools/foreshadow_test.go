package tools

import (
	"context"
	"testing"

	"reasonix/internal/novel/domain"
	"reasonix/internal/novel/repo"
)

func TestForeshadow_PlantDevelopResolve(t *testing.T) {
	mgr, _ := withProject(t, "伏笔")
	ctx := context.Background()
	tool := &foreshadowTool{}
	// First we need a chapter so the plant can pass (or skip chapter_id).
	// Let's skip chapter_id for plant and use a chapter id for resolve.
	pid := mustProjectID(t, mgr)
	ar := repo.NewArcRepo(mgr.DB())
	a := &domain.Arc{ProjectID: pid, Level: domain.LevelChapter, Title: "arc1"}
	if err := ar.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	cr := repo.NewChapterRepo(mgr.DB())
	c := &domain.Chapter{
		ProjectID: pid, ArcID: a.ID, Volume: 1, ChapterNumber: 1,
		Title: "起", Content: "正文", WordCount: 2, Status: "draft",
	}
	if err := cr.Create(ctx, c); err != nil {
		t.Fatal(err)
	}

	out, err := tool.Execute(ctx, map[string]any{
		"operation":   "plant",
		"description": "萧炎左手有古玉",
		"importance":  "major",
		"keywords":    []string{"古玉", "玉佩"},
	}, mgr)
	if err != nil {
		t.Fatalf("plant: %v", err)
	}
	fs, _ := out["foreshadow"].(*domain.Foreshadow)
	if fs == nil || fs.ID == "" {
		t.Fatalf("plant returned empty foreshadow: %+v", out)
	}
	if fs.Status != domain.ForeshadowPlanted {
		t.Errorf("status = %q, want planted", fs.Status)
	}
	if fs.Importance != domain.ForeshadowMajor {
		t.Errorf("importance = %q, want major", fs.Importance)
	}

	// develop
	out, err = tool.Execute(ctx, map[string]any{
		"operation": "develop",
		"id":        fs.ID,
	}, mgr)
	if err != nil {
		t.Fatalf("develop: %v", err)
	}
	fs2, _ := out["foreshadow"].(*domain.Foreshadow)
	if fs2.Status != domain.ForeshadowDeveloping {
		t.Errorf("status after develop = %q", fs2.Status)
	}

	// resolve with non-existent chapter should fail
	_, err = tool.Execute(ctx, map[string]any{
		"operation":  "resolve",
		"id":         fs.ID,
		"chapter_id": "nonexistent",
	}, mgr)
	if err == nil {
		t.Error("resolve with non-existent chapter should fail")
	}

	// list
	out, err = tool.Execute(ctx, map[string]any{"operation": "list"}, mgr)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if c, _ := out["count"].(int); c < 1 {
		t.Errorf("list count = %d, want ≥ 1", c)
	}
}

func TestForeshadow_AutoDetect(t *testing.T) {
	mgr, _ := withProject(t, "伏笔")
	ctx := context.Background()
	tool := &foreshadowTool{}
	pid := mustProjectID(t, mgr)
	ar := repo.NewArcRepo(mgr.DB())
	a := &domain.Arc{ProjectID: pid, Level: domain.LevelChapter, Title: "arc1"}
	if err := ar.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	cr := repo.NewChapterRepo(mgr.DB())
	c := &domain.Chapter{
		ProjectID: pid, ArcID: a.ID, Volume: 1, ChapterNumber: 1,
		Title: "起", Content: "正文", WordCount: 2, Status: "draft",
	}
	if err := cr.Create(ctx, c); err != nil {
		t.Fatal(err)
	}

	out, err := tool.Execute(ctx, map[string]any{
		"operation":   "plant",
		"description": "萧炎左手有古玉",
		"importance":  "major",
		"keywords":    []string{"古玉"},
	}, mgr)
	if err != nil {
		t.Fatal(err)
	}

	// Get the first chapter id
	var chapID string
	row := mgr.DB().QueryRowContext(ctx, `SELECT id FROM chapters ORDER BY chapter_number LIMIT 1`)
	if err := row.Scan(&chapID); err != nil {
		t.Fatal(err)
	}

	// auto_detect with content containing the keyword
	out, err = tool.Execute(ctx, map[string]any{
		"operation":  "auto_detect",
		"chapter_id": chapID,
		"content":    "萧炎拿起古玉一看，竟是千年寒玉。",
	}, mgr)
	if err != nil {
		t.Fatalf("auto_detect: %v", err)
	}
	developed, _ := out["auto_developed"].([]string)
	if len(developed) == 0 {
		t.Errorf("expected auto_developed entries, got %+v", out)
	}

	// auto_detect with resolve=true
	out, err = tool.Execute(ctx, map[string]any{
		"operation":  "auto_detect",
		"chapter_id": chapID,
		"content":    "古玉碎裂，化为飞灰。",
		"resolve":    "true",
	}, mgr)
	if err != nil {
		t.Fatalf("auto_detect resolve: %v", err)
	}
	resolved, _ := out["auto_resolved"].([]string)
	if len(resolved) == 0 {
		t.Errorf("expected auto_resolved entries, got %+v", out)
	}
}

func TestForeshadow_Abandon(t *testing.T) {
	mgr, _ := withProject(t, "伏笔")
	ctx := context.Background()
	tool := &foreshadowTool{}
	out, err := tool.Execute(ctx, map[string]any{
		"operation":   "plant",
		"description": "路边的老乞丐",
		"importance":  "minor",
		"keywords":    []string{"老乞丐"},
	}, mgr)
	if err != nil {
		t.Fatal(err)
	}
	fs, _ := out["foreshadow"].(*domain.Foreshadow)
	out, err = tool.Execute(ctx, map[string]any{
		"operation": "abandon",
		"id":        fs.ID,
	}, mgr)
	if err != nil {
		t.Fatalf("abandon: %v", err)
	}
	fs2, _ := out["foreshadow"].(*domain.Foreshadow)
	if fs2.Status != domain.ForeshadowAbandoned {
		t.Errorf("status after abandon = %q", fs2.Status)
	}
}

func TestForeshadow_InvalidImportance(t *testing.T) {
	mgr, _ := withProject(t, "伏笔")
	ctx := context.Background()
	tool := &foreshadowTool{}
	_, err := tool.Execute(ctx, map[string]any{
		"operation":   "plant",
		"description": "x",
		"importance":  "extreme",
	}, mgr)
	if err == nil {
		t.Error("expected error for invalid importance")
	}
}

func TestForeshadow_UnknownOperation(t *testing.T) {
	mgr, _ := withProject(t, "伏笔")
	ctx := context.Background()
	tool := &foreshadowTool{}
	_, err := tool.Execute(ctx, map[string]any{
		"operation": "teleport",
	}, mgr)
	if err == nil {
		t.Error("expected error for unknown operation")
	}
}

// TestForeshadow_AutoDetect_OverlappingKeywords: a row with
// multiple overlapping keywords (e.g. "刀" and "长刀") should
// match on either one. The function does substring matching
// per keyword, so both report a hit when the content contains
// the longer form.
func TestForeshadow_AutoDetect_OverlappingKeywords(t *testing.T) {
	mgr, _ := withProject(t, "伏笔")
	ctx := context.Background()
	tool := &foreshadowTool{}
	pid := mustProjectID(t, mgr)
	ar := repo.NewArcRepo(mgr.DB())
	a := &domain.Arc{ProjectID: pid, Level: domain.LevelChapter, Title: "arc1"}
	if err := ar.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	cr := repo.NewChapterRepo(mgr.DB())
	c := &domain.Chapter{
		ProjectID: pid, ArcID: a.ID, Volume: 1, ChapterNumber: 1,
		Title: "起", Content: "正文", WordCount: 2, Status: "draft",
	}
	if err := cr.Create(ctx, c); err != nil {
		t.Fatal(err)
	}
	// Plant a row whose keywords are "刀" (short) and
	// "长刀" (longer form).
	plant, err := tool.Execute(ctx, map[string]any{
		"operation":   "plant",
		"description": "一把不寻常的刀",
		"importance":  "minor",
		"keywords":    []string{"刀", "长刀"},
	}, mgr)
	if err != nil {
		t.Fatalf("plant: %v", err)
	}
	fs, _ := plant["foreshadow"].(*domain.Foreshadow)

	// Scan content with "长刀" — only one match expected per
	// row, but it should match (the short keyword is contained
	// in the long one).
	var chapID string
	if err := mgr.DB().QueryRowContext(ctx, `SELECT id FROM chapters ORDER BY chapter_number LIMIT 1`).Scan(&chapID); err != nil {
		t.Fatal(err)
	}
	out, err := tool.Execute(ctx, map[string]any{
		"operation":  "auto_detect",
		"chapter_id": chapID,
		"content":    "萧炎拔出长刀，凌空一挥。",
	}, mgr)
	if err != nil {
		t.Fatalf("auto_detect: %v", err)
	}
	developed, _ := out["auto_developed"].([]string)
	if len(developed) != 1 || developed[0] != fs.ID {
		t.Errorf("expected exactly 1 developed (id %s), got %+v", fs.ID, developed)
	}
}

// TestForeshadow_AutoDetect_WhitespaceInKeyword: a keyword with
// surrounding whitespace should be trimmed before matching, so
// "  古玉  " still triggers on "古玉" in the content.
func TestForeshadow_AutoDetect_WhitespaceInKeyword(t *testing.T) {
	mgr, _ := withProject(t, "伏笔")
	ctx := context.Background()
	tool := &foreshadowTool{}
	pid := mustProjectID(t, mgr)
	ar := repo.NewArcRepo(mgr.DB())
	a := &domain.Arc{ProjectID: pid, Level: domain.LevelChapter, Title: "arc1"}
	if err := ar.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	cr := repo.NewChapterRepo(mgr.DB())
	c := &domain.Chapter{
		ProjectID: pid, ArcID: a.ID, Volume: 1, ChapterNumber: 1,
		Title: "起", Content: "正文", WordCount: 2, Status: "draft",
	}
	if err := cr.Create(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(ctx, map[string]any{
		"operation":   "plant",
		"description": "古玉",
		"importance":  "major",
		"keywords":    []string{"  古玉  "},
	}, mgr); err != nil {
		t.Fatalf("plant: %v", err)
	}
	var chapID string
	if err := mgr.DB().QueryRowContext(ctx, `SELECT id FROM chapters ORDER BY chapter_number LIMIT 1`).Scan(&chapID); err != nil {
		t.Fatal(err)
	}
	out, err := tool.Execute(ctx, map[string]any{
		"operation":  "auto_detect",
		"chapter_id": chapID,
		"content":    "古玉闪着幽光。",
	}, mgr)
	if err != nil {
		t.Fatalf("auto_detect: %v", err)
	}
	developed, _ := out["auto_developed"].([]string)
	if len(developed) != 1 {
		t.Errorf("expected 1 developed (whitespace trimmed), got %+v", developed)
	}
}

// TestForeshadow_AutoDetect_EmptyKeyword: a row with no keywords
// at all should never be developed — the tool does not invent
// matches out of thin air.
func TestForeshadow_AutoDetect_EmptyKeyword(t *testing.T) {
	mgr, _ := withProject(t, "伏笔")
	ctx := context.Background()
	tool := &foreshadowTool{}
	pid := mustProjectID(t, mgr)
	ar := repo.NewArcRepo(mgr.DB())
	a := &domain.Arc{ProjectID: pid, Level: domain.LevelChapter, Title: "arc1"}
	if err := ar.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	cr := repo.NewChapterRepo(mgr.DB())
	c := &domain.Chapter{
		ProjectID: pid, ArcID: a.ID, Volume: 1, ChapterNumber: 1,
		Title: "起", Content: "正文", WordCount: 2, Status: "draft",
	}
	if err := cr.Create(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(ctx, map[string]any{
		"operation":   "plant",
		"description": "无关键词的伏笔",
		"importance":  "minor",
		"keywords":    []string{},
	}, mgr); err != nil {
		t.Fatalf("plant: %v", err)
	}
	var chapID string
	if err := mgr.DB().QueryRowContext(ctx, `SELECT id FROM chapters ORDER BY chapter_number LIMIT 1`).Scan(&chapID); err != nil {
		t.Fatal(err)
	}
	out, err := tool.Execute(ctx, map[string]any{
		"operation":  "auto_detect",
		"chapter_id": chapID,
		"content":    "无关内容，不应触发任何伏笔。",
	}, mgr)
	if err != nil {
		t.Fatalf("auto_detect: %v", err)
	}
	developed, _ := out["auto_developed"].([]string)
	if len(developed) != 0 {
		t.Errorf("expected no developed (empty keywords), got %+v", developed)
	}
}

// TestForeshadow_AutoDetect_NoKeywordInContent: a row whose
// keywords are nowhere in the chapter content is left alone.
func TestForeshadow_AutoDetect_NoKeywordInContent(t *testing.T) {
	mgr, _ := withProject(t, "伏笔")
	ctx := context.Background()
	tool := &foreshadowTool{}
	pid := mustProjectID(t, mgr)
	ar := repo.NewArcRepo(mgr.DB())
	a := &domain.Arc{ProjectID: pid, Level: domain.LevelChapter, Title: "arc1"}
	if err := ar.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	cr := repo.NewChapterRepo(mgr.DB())
	c := &domain.Chapter{
		ProjectID: pid, ArcID: a.ID, Volume: 1, ChapterNumber: 1,
		Title: "起", Content: "正文", WordCount: 2, Status: "draft",
	}
	if err := cr.Create(ctx, c); err != nil {
		t.Fatal(err)
	}
	out, err := tool.Execute(ctx, map[string]any{
		"operation":   "plant",
		"description": "古玉",
		"importance":  "minor",
		"keywords":    []string{"古玉"},
	}, mgr)
	if err != nil {
		t.Fatalf("plant: %v", err)
	}
	fs, _ := out["foreshadow"].(*domain.Foreshadow)
	var chapID string
	if err := mgr.DB().QueryRowContext(ctx, `SELECT id FROM chapters ORDER BY chapter_number LIMIT 1`).Scan(&chapID); err != nil {
		t.Fatal(err)
	}
	out, err = tool.Execute(ctx, map[string]any{
		"operation":  "auto_detect",
		"chapter_id": chapID,
		"content":    "今天天气真好，街上人来人往。",
	}, mgr)
	if err != nil {
		t.Fatalf("auto_detect: %v", err)
	}
	developed, _ := out["auto_developed"].([]string)
	if len(developed) != 0 {
		t.Errorf("expected no developed, got %+v", developed)
	}
	// Status unchanged: still "planted".
	got, _ := repo.NewForeshadowRepo(mgr.DB()).Get(ctx, fs.ID)
	if got.Status != domain.ForeshadowPlanted {
		t.Errorf("status = %q, want planted", got.Status)
	}
}

// TestForeshadow_AutoDetect_RequiresContent: the operation
// refuses to run without content (otherwise it would always
// report zero hits and confuse the caller).
func TestForeshadow_AutoDetect_RequiresContent(t *testing.T) {
	mgr, _ := withProject(t, "伏笔")
	ctx := context.Background()
	tool := &foreshadowTool{}
	pid := mustProjectID(t, mgr)
	ar := repo.NewArcRepo(mgr.DB())
	a := &domain.Arc{ProjectID: pid, Level: domain.LevelChapter, Title: "arc1"}
	if err := ar.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	cr := repo.NewChapterRepo(mgr.DB())
	c := &domain.Chapter{
		ProjectID: pid, ArcID: a.ID, Volume: 1, ChapterNumber: 1,
		Title: "起", Content: "正文", WordCount: 2, Status: "draft",
	}
	if err := cr.Create(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(ctx, map[string]any{
		"operation":  "auto_detect",
		"chapter_id": "ch1",
		// no content
	}, mgr); err == nil {
		t.Error("expected error for missing content")
	}
}

// TestForeshadow_Develop_AfterResolve: developing a row that
// is already resolved should fail. This guards the lifecycle
// invariant that resolved rows are immutable until abandoned.
func TestForeshadow_Develop_AfterResolve(t *testing.T) {
	mgr, _ := withProject(t, "伏笔")
	ctx := context.Background()
	tool := &foreshadowTool{}
	pid := mustProjectID(t, mgr)
	ar := repo.NewArcRepo(mgr.DB())
	a := &domain.Arc{ProjectID: pid, Level: domain.LevelChapter, Title: "arc1"}
	if err := ar.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	cr := repo.NewChapterRepo(mgr.DB())
	c := &domain.Chapter{
		ProjectID: pid, ArcID: a.ID, Volume: 1, ChapterNumber: 1,
		Title: "起", Content: "正文", WordCount: 2, Status: "draft",
	}
	if err := cr.Create(ctx, c); err != nil {
		t.Fatal(err)
	}
	plant, err := tool.Execute(ctx, map[string]any{
		"operation":   "plant",
		"description": "古玉",
		"importance":  "major",
		"keywords":    []string{"古玉"},
	}, mgr)
	if err != nil {
		t.Fatalf("plant: %v", err)
	}
	fs, _ := plant["foreshadow"].(*domain.Foreshadow)
	if _, err := tool.Execute(ctx, map[string]any{
		"operation":  "resolve",
		"id":         fs.ID,
		"chapter_id": c.ID,
	}, mgr); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if _, err := tool.Execute(ctx, map[string]any{
		"operation": "develop",
		"id":        fs.ID,
	}, mgr); err == nil {
		t.Error("expected error when developing a resolved row")
	}
}
