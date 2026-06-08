package review

import (
	"testing"

	"reasonix/internal/novel/domain"
)

func TestLoadDimensions_Default(t *testing.T) {
	dims := LoadDimensions(nil)
	if len(dims) != 8 {
		t.Fatalf("expected 8 default dimensions, got %d", len(dims))
	}
	want := []string{
		domain.ReviewDimPlot,
		domain.ReviewDimCharacter,
		domain.ReviewDimStyle,
		domain.ReviewDimConsistency,
		domain.ReviewDimPacing,
		domain.ReviewDimForeshadow,
		domain.ReviewDimHook,
		domain.ReviewDimValues,
	}
	for i, d := range dims {
		if got := d.Name(); got != want[i] {
			t.Errorf("dim[%d].Name() = %q, want %q", i, got, want[i])
		}
	}
}

func TestLoadDimensions_Subset(t *testing.T) {
	dims := LoadDimensions([]string{"plot", "style", "hook"})
	if len(dims) != 3 {
		t.Fatalf("expected 3 dimensions, got %d", len(dims))
	}
	if dims[0].Name() != "plot" {
		t.Errorf("dim[0] = %q, want plot", dims[0].Name())
	}
	if dims[1].Name() != "style" {
		t.Errorf("dim[1] = %q, want style", dims[1].Name())
	}
	if dims[2].Name() != "hook" {
		t.Errorf("dim[2] = %q, want hook", dims[2].Name())
	}
}

func TestLoadDimensions_UnknownIgnored(t *testing.T) {
	dims := LoadDimensions([]string{"plot", "not_a_real_dim", "hook"})
	if len(dims) != 2 {
		t.Fatalf("expected 2 dimensions (unknown ignored), got %d", len(dims))
	}
}

func TestAllDimensionNames(t *testing.T) {
	names := AllDimensionNames()
	if len(names) != 8 {
		t.Fatalf("expected 8 names, got %d", len(names))
	}
}

func TestEvaluate_ScoreRange(t *testing.T) {
	ch := &domain.Chapter{WordCount: 2000}
	for _, d := range LoadDimensions(nil) {
		score, issues := d.Evaluate(ch)
		if score < 0 || score > 10 {
			t.Errorf("%s: score %f out of range [0,10]", d.Name(), score)
		}
		if len(issues) == 0 {
			t.Errorf("%s: expected at least one info issue", d.Name())
		}
		for _, iss := range issues {
			if iss.Severity != domain.SeverityInfo {
				t.Errorf("%s: expected severity info, got %q", d.Name(), iss.Severity)
			}
		}
	}
}

func TestEvaluate_LowWordCount(t *testing.T) {
	ch := &domain.Chapter{WordCount: 200}
	dims := LoadDimensions([]string{domain.ReviewDimPlot})
	score, _ := dims[0].Evaluate(ch)
	if score != 3.0 {
		t.Errorf("low word count score = %f, want 3.0", score)
	}
}

func TestBuildPromptCriteria(t *testing.T) {
	dims := LoadDimensions([]string{"plot", "style"})
	out := BuildPromptCriteria(dims)
	if out == "" {
		t.Fatal("BuildPromptCriteria returned empty string")
	}
	if !contains(out, "情节") {
		t.Error("missing plot criteria in prompt")
	}
	if !contains(out, "文笔") {
		t.Error("missing style criteria in prompt")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
