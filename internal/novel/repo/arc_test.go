package repo

import (
	"context"
	"testing"

	"reasonix/internal/novel/domain"
)

func TestArcRepo_CRUD(t *testing.T) {
	d := openTestDB(t)
	pid := seedProject(t, d)
	r := NewArcRepo(d)
	ctx := context.Background()

	master := &domain.Arc{ProjectID: pid, Level: domain.LevelMaster, Title: "全书主线", OrderIndex: 0}
	if err := r.Create(ctx, master); err != nil {
		t.Fatalf("Create master: %v", err)
	}
	vol := &domain.Arc{ProjectID: pid, ParentID: master.ID, Level: domain.LevelVolume, Title: "第一卷", OrderIndex: 1}
	if err := r.Create(ctx, vol); err != nil {
		t.Fatalf("Create volume: %v", err)
	}

	if got, _ := r.Get(ctx, vol.ID); got.ParentID != master.ID {
		t.Errorf("ParentID = %q, want %q", got.ParentID, master.ID)
	}

	kids, _ := r.ListByParent(ctx, master.ID)
	if len(kids) != 1 || kids[0].ID != vol.ID {
		t.Errorf("ListByParent = %v, want [vol]", kids)
	}

	list, _ := r.List(ctx, pid)
	if len(list) != 2 {
		t.Errorf("List len = %d, want 2", len(list))
	}

	vol.Title = "第一卷 (改)"
	if err := r.Update(ctx, vol); err != nil {
		t.Fatalf("Update: %v", err)
	}
	after, _ := r.Get(ctx, vol.ID)
	if after.Title != "第一卷 (改)" {
		t.Errorf("Update did not persist: %q", after.Title)
	}

	if err := r.Delete(ctx, vol.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := r.Get(ctx, vol.ID); !errIsNotFound(err) {
		t.Errorf("Get after Delete err = %v, want ErrNotFound", err)
	}
}

func TestArcRepo_CreateRequiresLevel(t *testing.T) {
	d := openTestDB(t)
	pid := seedProject(t, d)
	r := NewArcRepo(d)
	if err := r.Create(context.Background(), &domain.Arc{ProjectID: pid, Title: "x"}); err == nil {
		t.Fatal("Create without level should have errored")
	}
}
