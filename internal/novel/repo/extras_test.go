package repo

import (
	"context"
	"testing"

	"reasonix/internal/novel/db"
	"reasonix/internal/novel/domain"
)

func TestFactRepo_Batch(t *testing.T) {
	d := openTestDB(t)
	pid := seedProject(t, d)
	arc := seedChapter(t, d, pid)
	r := NewFactRepo(d)
	ctx := context.Background()

	facts := []*domain.ChapterFact{
		{ProjectID: pid, ChapterID: arc, FactType: domain.FactTypeEvent, Subject: "萧炎", Predicate: "突破到", Object: "斗者", Confidence: 0.95},
		{ProjectID: pid, ChapterID: arc, FactType: domain.FactTypeLocation, Subject: "萧炎", Predicate: "在", Object: "乌坦城", Confidence: 0.9},
		{ProjectID: pid, ChapterID: arc, FactType: domain.FactTypeItem, Subject: "萧炎", Predicate: "获得", Object: "玄铁剑", Confidence: 0.8},
	}
	if err := r.CreateBatch(ctx, facts); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}

	list, _ := r.ListByChapter(ctx, arc)
	if len(list) != 3 {
		t.Errorf("ListByChapter len = %d, want 3", len(list))
	}
}

func TestStateRepo_BatchAndRecent(t *testing.T) {
	d := openTestDB(t)
	pid := seedProject(t, d)
	ch := seedChapter(t, d, pid)
	// Need at least one character row for the FK to resolve.
	cid := seedCharacter(t, d, pid)

	r := NewStateRepo(d)
	ctx := context.Background()

	states := []*StateInput{
		{
			ProjectID: pid, CharacterID: cid, ChapterID: ch,
			Snapshot: &domain.CharacterState{Location: "乌坦城", Power: "斗者", Mood: "警觉"},
			Tags:     []string{"主角", "in-arc:demo"},
		},
		{
			ProjectID: pid, CharacterID: cid, ChapterID: ch,
			Snapshot: &domain.CharacterState{Location: "天云宗", Power: "斗师"},
			Tags:     []string{"in-arc:demo"},
		},
	}
	if err := r.CreateBatch(ctx, states); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}

	recent, err := r.RecentByProject(ctx, pid, 5)
	if err != nil {
		t.Fatalf("RecentByProject: %v", err)
	}
	if len(recent) != 2 {
		t.Errorf("Recent len = %d, want 2", len(recent))
	}
}

func TestAliasRepo_ResolveAndBatch(t *testing.T) {
	d := openTestDB(t)
	pid := seedProject(t, d)
	r := NewAliasRepo(d)
	ctx := context.Background()

	if err := r.CreateBatch(ctx, []*domain.Alias{
		{ProjectID: pid, EntityType: domain.EntityCharacter, EntityID: "c-1", Alias: "萧炎"},
		{ProjectID: pid, EntityType: domain.EntityCharacter, EntityID: "c-1", Alias: "炎儿"},
		{ProjectID: pid, EntityType: domain.EntityCharacter, EntityID: "c-2", Alias: "林动"},
	}); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}

	id, ok, err := r.Resolve(ctx, pid, "炎儿")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !ok || id != "c-1" {
		t.Errorf("Resolve 炎儿 = %q, ok=%v; want c-1, true", id, ok)
	}
	if _, ok, _ := r.Resolve(ctx, pid, "不存在"); ok {
		t.Error("Resolve on miss should return ok=false")
	}

	got, _ := r.AliasesForNames(ctx, pid, []string{"萧炎", "林动", "无"})
	if !got["萧炎"] || !got["林动"] {
		t.Errorf("AliasesForNames = %v", got)
	}
}

func TestForeshadowRepo_CRUD(t *testing.T) {
	d := openTestDB(t)
	pid := seedProject(t, d)
	r := NewForeshadowRepo(d)
	ctx := context.Background()

	f := &domain.Foreshadow{
		ProjectID:  pid,
		Description: "古帝洞府的伏笔",
		Keywords:    []string{"古帝", "洞府"},
		Status:      domain.ForeshadowPlanted,
		Importance:  domain.ForeshadowMajor,
	}
	if err := r.Create(ctx, f); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if f.ID == "" {
		t.Error("ID not assigned")
	}

	got, _ := r.Get(ctx, f.ID)
	if got.Description != f.Description || len(got.Keywords) != 2 {
		t.Errorf("Get mismatch: %+v", got)
	}

	list, _ := r.List(ctx, pid)
	if len(list) != 1 {
		t.Errorf("List len = %d, want 1", len(list))
	}

	got.Status = domain.ForeshadowDeveloping
	if err := r.Update(ctx, got); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if err := r.Delete(ctx, f.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

// --- shared helpers ---

func seedChapter(t *testing.T, d *db.DB, pid string) string {
	t.Helper()
	cr := NewChapterRepo(d)
	c := &domain.Chapter{ProjectID: pid, Title: "种子章节", Volume: 1, ChapterNumber: 1}
	if err := cr.Create(context.Background(), c); err != nil {
		t.Fatalf("seed chapter: %v", err)
	}
	return c.ID
}

func seedCharacter(t *testing.T, d *db.DB, pid string) string {
	t.Helper()
	cr := NewCharacterRepo(d)
	c := &domain.Character{ProjectID: pid, Name: "萧炎"}
	if err := cr.Create(context.Background(), c); err != nil {
		t.Fatalf("seed character: %v", err)
	}
	return c.ID
}
