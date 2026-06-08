package repo

import (
	"context"
	"testing"

	"reasonix/internal/novel/domain"
)

func TestChapterRepo_CRUD(t *testing.T) {
	d := openTestDB(t)
	pid := seedProject(t, d)
	arcRepo := NewArcRepo(d)
	arc := &domain.Arc{ProjectID: pid, Level: domain.LevelChapter, Title: "章节大纲"}
	if err := arcRepo.Create(context.Background(), arc); err != nil {
		t.Fatalf("seed arc: %v", err)
	}

	r := NewChapterRepo(d)
	ctx := context.Background()
	c := &domain.Chapter{
		ProjectID:     pid,
		ArcID:         arc.ID,
		Volume:        1,
		ChapterNumber: 1,
		Title:         "陨落的天才",
		Content:       "正文……",
		WordCount:     3500,
	}
	if err := r.Create(ctx, c); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, _ := r.Get(ctx, c.ID)
	if got.Title != "陨落的天才" {
		t.Errorf("Get Title = %q", got.Title)
	}
	if got.ArcID != arc.ID {
		t.Errorf("ArcID = %q, want %q", got.ArcID, arc.ID)
	}

	list, _ := r.ListByArc(ctx, arc.ID)
	if len(list) != 1 {
		t.Errorf("ListByArc len = %d", len(list))
	}

	c.Content = "更新后"
	if err := r.Update(ctx, c); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if err := r.Delete(ctx, c.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestChapterRepo_CreateRequiresTitle(t *testing.T) {
	d := openTestDB(t)
	pid := seedProject(t, d)
	r := NewChapterRepo(d)
	if err := r.Create(context.Background(), &domain.Chapter{ProjectID: pid, ChapterNumber: 1}); err == nil {
		t.Fatal("Create without title should have errored")
	}
}
