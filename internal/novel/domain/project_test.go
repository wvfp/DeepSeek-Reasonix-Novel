package domain

import "testing"

func TestProject_RoundTrip(t *testing.T) {
	p := Project{
		ID:            "p1",
		Name:          "斗破苍穹同人",
		Genre:         GenreXianxia,
		PipelinePhase: PhaseSetting,
		CreatedAt:     1700000000,
		ModifiedAt:    1700000100,
	}
	got := roundTrip(t, p).(Project)
	assertEqual(t, "Project", p, got)
}

func TestPhaseConstants(t *testing.T) {
	want := []string{"setting", "planning", "writing", "reviewing", "completed"}
	got := []string{
		PhaseSetting, PhasePlanning, PhaseWriting, PhaseReviewing, PhaseCompleted,
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("Phase[%d] = %q, want %q", i, got[i], v)
		}
	}
}

func TestDefaultGenre(t *testing.T) {
	// The novel-plugin defaults to "fantasy" — keep parity.
	if GenreFantasy != "fantasy" {
		t.Errorf("GenreFantasy = %q, want %q", GenreFantasy, "fantasy")
	}
}
