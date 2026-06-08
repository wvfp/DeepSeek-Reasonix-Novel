package domain

import "testing"

func TestForeshadow_RoundTrip(t *testing.T) {
	f := Foreshadow{
		ID:                "fs1",
		ProjectID:         "p1",
		PlantedChapterID:  "ch3",
		ResolvedChapterID: "",
		Description:       "萧炎在黑指上留下的血印其实是远古斗帝的封印",
		Keywords:          []string{"血印", "封印", "斗帝"},
		Status:            ForeshadowPlanted,
		Importance:        ForeshadowCritical,
		CreatedAt:         1700000000,
		ModifiedAt:        1700000100,
	}
	got := roundTrip(t, f).(Foreshadow)
	assertEqual(t, "Foreshadow", f, got)
}

func TestForeshadowStatusConstants(t *testing.T) {
	want := []string{"planted", "developing", "resolved", "abandoned"}
	got := []string{ForeshadowPlanted, ForeshadowDeveloping, ForeshadowResolved, ForeshadowAbandoned}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("ForeshadowStatus[%d] = %q, want %q", i, got[i], v)
		}
	}
}

func TestForeshadowImportanceConstants(t *testing.T) {
	want := []string{"minor", "major", "critical"}
	got := []string{ForeshadowMinor, ForeshadowMajor, ForeshadowCritical}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("ForeshadowImportance[%d] = %q, want %q", i, got[i], v)
		}
	}
}
