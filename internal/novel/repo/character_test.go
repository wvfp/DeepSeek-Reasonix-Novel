package repo

import (
	"context"
	"errors"
	"testing"

	"reasonix/internal/novel/domain"
)

func TestCharacterRepo_CRUD(t *testing.T) {
	d := openTestDB(t)
	pid := seedProject(t, d)
	r := NewCharacterRepo(d)
	ctx := context.Background()

	c := &domain.Character{
		ProjectID:   pid,
		Name:        "萧炎",
		Description: "天才少年",
		VoiceProfile: &domain.VoiceProfile{
			SpeechStyle:    "casual",
			Catchphrases:   []string{"莫欺少年穷"},
			ProfanityLevel: 1,
		},
	}
	if err := r.Create(ctx, c); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if c.ID == "" || c.Slug == "" {
		t.Error("Create did not assign ID/Slug")
	}

	got, err := r.Get(ctx, c.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.VoiceProfile == nil || got.VoiceProfile.SpeechStyle != "casual" {
		t.Errorf("voice profile not round-tripped: %+v", got.VoiceProfile)
	}

	list, _ := r.List(ctx, pid)
	if len(list) != 1 {
		t.Errorf("List len = %d, want 1", len(list))
	}

	got.Description = "更新"
	if err := r.Update(ctx, got); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if err := r.Delete(ctx, c.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := r.Get(ctx, c.ID); !errIsNotFound(err) {
		t.Errorf("Get after Delete err = %v, want ErrNotFound", err)
	}
}

func TestCharacterRepo_Search(t *testing.T) {
	d := openTestDB(t)
	pid := seedProject(t, d)
	r := NewCharacterRepo(d)
	ctx := context.Background()
	for _, name := range []string{"萧炎", "林动", "牧尘"} {
		if err := r.Create(ctx, &domain.Character{ProjectID: pid, Name: name}); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}
	got, _ := r.Search(ctx, pid, "萧")
	if len(got) != 1 || got[0].Name != "萧炎" {
		t.Errorf("Search returned %v", got)
	}
}

func TestCharacterRepo_UpdateMissing(t *testing.T) {
	d := openTestDB(t)
	r := NewCharacterRepo(d)
	if err := r.Update(context.Background(), &domain.Character{ID: "no-such"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Update err = %v, want ErrNotFound", err)
	}
}
