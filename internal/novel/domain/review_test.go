package domain

import "testing"

func TestReview_RoundTrip(t *testing.T) {
	r := Review{
		ID:        "rv1",
		ChapterID: "ch1",
		Dimension: ReviewDimPlot,
		Score:     7.5,
		Issues: []Issue{
			{Severity: SeverityBlocker, Description: "主线跳跃过大", Location: "§3"},
			{Severity: SeverityWarning, Description: "情感线铺垫不足", Location: "§2"},
		},
		CreatedAt: 1700000000,
	}
	got := roundTrip(t, r).(Review)
	assertEqual(t, "Review", r, got)
}

func TestReview_NoIssues_RoundTrip(t *testing.T) {
	r := Review{ID: "rv2", ChapterID: "ch2", Dimension: ReviewDimStyle, Score: 9.0}
	got := roundTrip(t, r).(Review)
	assertEqual(t, "Review(no issues)", r, got)
}

func TestIssue_RoundTrip(t *testing.T) {
	iss := Issue{Severity: SeverityInfo, Description: "提示", Location: "§1"}
	got := roundTrip(t, iss).(Issue)
	assertEqual(t, "Issue", iss, got)
}

func TestReviewDimensionConstants(t *testing.T) {
	want := []string{
		"plot", "character", "style", "consistency",
		"pacing", "foreshadow", "hook", "values",
	}
	got := []string{
		ReviewDimPlot, ReviewDimCharacter, ReviewDimStyle, ReviewDimConsistency,
		ReviewDimPacing, ReviewDimForeshadow, ReviewDimHook, ReviewDimValues,
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("ReviewDim[%d] = %q, want %q", i, got[i], v)
		}
	}
}

func TestIssueSeverityConstants(t *testing.T) {
	want := []string{"blocker", "warning", "info"}
	got := []string{SeverityBlocker, SeverityWarning, SeverityInfo}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("Severity[%d] = %q, want %q", i, got[i], v)
		}
	}
}
