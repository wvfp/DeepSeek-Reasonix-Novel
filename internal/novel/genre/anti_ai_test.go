package genre

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func init() {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..", "..")
	root, _ = filepath.Abs(root)
	SetTestRoot(root)
}

// ---------------------------------------------------------------------------
// LoadAntiAIRules — genre-specific loading
// ---------------------------------------------------------------------------

func TestLoadAntiAIRules_Xianxia(t *testing.T) {
	rules, err := LoadAntiAIRules("xianxia")
	if err != nil {
		t.Fatalf("LoadAntiAIRules(xianxia): %v", err)
	}
	if len(rules) == 0 {
		t.Fatal("expected xianxia anti-ai rules, got none")
	}
	// Verify at least one known xianxia-specific pattern is present.
	found := false
	for _, r := range rules {
		if r.Pattern == "天材地宝" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected xianxia rule '天材地宝' not found")
	}
}

func TestLoadAntiAIRules_Urban(t *testing.T) {
	rules, err := LoadAntiAIRules("urban")
	if err != nil {
		t.Fatalf("LoadAntiAIRules(urban): %v", err)
	}
	if len(rules) == 0 {
		t.Fatal("expected urban anti-ai rules, got none")
	}
	found := false
	for _, r := range rules {
		if r.Pattern == "总裁.*办公室" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected urban rule '总裁.*办公室' not found")
	}
}

func TestLoadAntiAIRules_Horror(t *testing.T) {
	rules, err := LoadAntiAIRules("horror")
	if err != nil {
		t.Fatalf("LoadAntiAIRules(horror): %v", err)
	}
	if len(rules) == 0 {
		t.Fatal("expected horror anti-ai rules, got none")
	}
	found := false
	for _, r := range rules {
		if r.Pattern == "镜子.*倒影" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected horror rule '镜子.*倒影' not found")
	}
}

func TestLoadAntiAIRules_UnknownGenreReturnsEmpty(t *testing.T) {
	rules, err := LoadAntiAIRules("nonexistent-genre")
	if err != nil {
		t.Fatalf("LoadAntiAIRules(nonexistent-genre): expected no error, got %v", err)
	}
	if len(rules) != 0 {
		t.Fatalf("expected empty rules for unknown genre, got %d", len(rules))
	}
}

// ---------------------------------------------------------------------------
// Rule shape validation
// ---------------------------------------------------------------------------

func TestLoadAntiAIRules_RuleShape(t *testing.T) {
	for _, id := range []string{"xianxia", "urban", "horror"} {
		t.Run(id, func(t *testing.T) {
			rules, err := LoadAntiAIRules(id)
			if err != nil {
				t.Fatalf("LoadAntiAIRules(%s): %v", id, err)
			}
			for i, r := range rules {
				if r.Pattern == "" {
					t.Errorf("rule %d: empty pattern", i)
				}
				if r.Replacement == "" {
					t.Errorf("rule %d: empty replacement", i)
				}
				if r.Category == "" {
					t.Errorf("rule %d: empty category", i)
				}
				if r.Severity == "" {
					t.Errorf("rule %d: empty severity", i)
				}
				if r.Layer < 1 {
					t.Errorf("rule %d: layer must be >= 1, got %d", i, r.Layer)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Count rules per genre
// ---------------------------------------------------------------------------

func TestLoadAntiAIRules_Count(t *testing.T) {
	for _, tc := range []struct {
		id  string
		min int
		max int
	}{
		{"xianxia", 10, 15},
		{"urban", 10, 15},
		{"horror", 10, 15},
	} {
		t.Run(tc.id, func(t *testing.T) {
			rules, err := LoadAntiAIRules(tc.id)
			if err != nil {
				t.Fatalf("LoadAntiAIRules(%s): %v", tc.id, err)
			}
			if len(rules) < tc.min || len(rules) > tc.max {
				t.Errorf("expected %d-%d rules, got %d", tc.min, tc.max, len(rules))
			}
		})
	}
}

// ---------------------------------------------------------------------------
// LoadAntiAIRulesFromPath — direct path loading
// ---------------------------------------------------------------------------

func TestLoadAntiAIRulesFromPath_Success(t *testing.T) {
	path := filepath.Join("..", "..", "..", "internal", "novel", "genre", "xianxia", "anti_ai.json")
	path, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Skipf("skip: file not found at %s", path)
	}
	rules, err := LoadAntiAIRulesFromPath(path)
	if err != nil {
		t.Fatalf("LoadAntiAIRulesFromPath: %v", err)
	}
	if len(rules) == 0 {
		t.Fatal("expected rules, got none")
	}
}

func TestLoadAntiAIRulesFromPath_Missing(t *testing.T) {
	rules, err := LoadAntiAIRulesFromPath("nonexistent/path/anti_ai.json")
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if len(rules) != 0 {
		t.Fatalf("expected empty rules for missing file, got %d", len(rules))
	}
}
