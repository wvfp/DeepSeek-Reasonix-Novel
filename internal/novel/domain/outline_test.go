package domain

import "testing"

func TestArc_RoundTrip(t *testing.T) {
	a := Arc{
		ID:         "a1",
		ProjectID:  "p1",
		ParentID:   "a0",
		Level:      LevelVolume,
		Title:      "第一卷 少年崛起",
		Summary:    "萧炎从天才陨落到重修斗帝之路的开端。",
		OrderIndex: 1,
		Metadata:   map[string]any{"target_words": 300000.0},
		CreatedAt:  1700000000,
		ModifiedAt: 1700000100,
	}
	got := roundTrip(t, a).(Arc)
	assertEqual(t, "Arc", a, got)
}

func TestArc_NoParent_RoundTrip(t *testing.T) {
	a := Arc{ID: "a0", ProjectID: "p1", Level: LevelMaster, Title: "全书主线", OrderIndex: 0}
	got := roundTrip(t, a).(Arc)
	assertEqual(t, "Arc(no parent)", a, got)
}

func TestArcLevelConstants(t *testing.T) {
	want := []string{"master", "volume", "chapter", "blueprint"}
	got := []string{LevelMaster, LevelVolume, LevelChapter, LevelBlueprint}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("Level[%d] = %q, want %q", i, got[i], v)
		}
	}
}
