package repo

import (
	"context"
	"testing"

	"reasonix/internal/novel/domain"
)

func TestSlugify(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Hello World", "hello-world"},
		{"九州大陆", "九州大陆"},
		{"  --trim-- ", "trim"},
		{"", "untitled"},
		{"A B C", "a-b-c"},
		{"X-Men!", "x-men"},
		{"凡人修仙传", "凡人修仙传"},
	}
	for _, c := range cases {
		if got := Slugify(c.in); got != c.want {
			t.Errorf("Slugify(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestWorldRepo_CreateGetListUpdateDelete(t *testing.T) {
	d := openTestDB(t)
	pid := seedProject(t, d)
	r := NewWorldRepo(d)
	ctx := context.Background()

	w := &domain.World{
		ProjectID:   pid,
		Name:        "九州",
		Description: "九州大地，浩瀚无垠",
		Content:     "# 九州\n\n...",
		Metadata:    map[string]any{"tone": "epic", "year": 1200.0},
	}
	if err := r.Create(ctx, w); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if w.ID == "" {
		t.Error("Create did not assign ID")
	}
	if w.Slug == "" {
		t.Error("Create did not assign Slug")
	}
	if w.CreatedAt == 0 || w.ModifiedAt == 0 {
		t.Error("Create did not assign timestamps")
	}

	got, err := r.Get(ctx, w.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != w.Name || got.Slug != w.Slug {
		t.Errorf("Get mismatch: name=%q slug=%q", got.Name, got.Slug)
	}
	if got.Metadata["tone"] != "epic" {
		t.Errorf("metadata not round-tripped: %v", got.Metadata)
	}

	list, err := r.List(ctx, pid)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("List len = %d, want 1", len(list))
	}

	got.Description = "更新后描述"
	if err := r.Update(ctx, got); err != nil {
		t.Fatalf("Update: %v", err)
	}
	after, _ := r.Get(ctx, w.ID)
	if after.Description != "更新后描述" {
		t.Errorf("Update did not persist: %q", after.Description)
	}
	// modified_at uses Unix-second resolution; Update should always be
	// at-or-after the original. A future sub-second refinement (or a
	// clock-jump in CI) can land as a >= check.
	if after.ModifiedAt < w.ModifiedAt {
		t.Errorf("Update set modified_at backwards: before=%d after=%d", w.ModifiedAt, after.ModifiedAt)
	}

	if err := r.Delete(ctx, w.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := r.Get(ctx, w.ID); !errIsNotFound(err) {
		t.Errorf("Get after Delete err = %v, want ErrNotFound", err)
	}
}

func TestWorldRepo_Search(t *testing.T) {
	d := openTestDB(t)
	pid := seedProject(t, d)
	r := NewWorldRepo(d)
	ctx := context.Background()

	for _, name := range []string{"九州", "天元大陆", "无尽海"} {
		if err := r.Create(ctx, &domain.World{ProjectID: pid, Name: name, Description: "x"}); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}

	got, err := r.Search(ctx, pid, "九州")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].Name != "九州" {
		t.Errorf("Search 九州 returned %v", namesOf(got))
	}

	got, err = r.Search(ctx, pid, "")
	if err != nil {
		t.Fatalf("Search empty: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("Search empty len = %d, want 3", len(got))
	}
}

func TestWorldRepo_InsertLink(t *testing.T) {
	d := openTestDB(t)
	pid := seedProject(t, d)
	r := NewWorldRepo(d)
	ctx := context.Background()
	w1 := &domain.World{ProjectID: pid, Name: "A"}
	w2 := &domain.World{ProjectID: pid, Name: "B"}
	if err := r.Create(ctx, w1); err != nil {
		t.Fatal(err)
	}
	if err := r.Create(ctx, w2); err != nil {
		t.Fatal(err)
	}
	if err := r.InsertLink(ctx, pid, w1.ID, w2.ID, "adjacent_to", "海路相通"); err != nil {
		t.Fatalf("InsertLink: %v", err)
	}
	if n := countRows(t, d, "entity_links"); n != 1 {
		t.Errorf("entity_links count = %d, want 1", n)
	}
}

func TestWorldRepo_CreateRequiresProjectID(t *testing.T) {
	d := openTestDB(t)
	r := NewWorldRepo(d)
	if err := r.Create(context.Background(), &domain.World{Name: "x"}); err == nil {
		t.Fatal("Create without project_id should have errored")
	}
}

func namesOf(ws []*domain.World) []string {
	out := make([]string, 0, len(ws))
	for _, w := range ws {
		out = append(out, w.Name)
	}
	return out
}
