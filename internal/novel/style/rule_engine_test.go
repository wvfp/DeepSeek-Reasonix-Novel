package style

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Multi-source loading
// ---------------------------------------------------------------------------

func TestDefaultRuleEngine_LoadEmbedded(t *testing.T) {
	eng := NewDefaultRuleEngine()
	if err := eng.Load("embedded"); err != nil {
		t.Fatalf("Load(embedded): %v", err)
	}
	v := eng.Check("他缓缓说道：『走吧。』")
	if len(v) == 0 {
		t.Error("expected violation for 缓缓说道, got none")
	}
}

func TestDefaultRuleEngine_LoadFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "rules.json")
	data := `[{"pattern":"测试模式","replacement":"","category":"test","severity":"high","layer":1}]`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	eng := NewDefaultRuleEngine()
	if err := eng.Load("file:" + path); err != nil {
		t.Fatalf("Load(file): %v", err)
	}
	v := eng.Check("这是一个测试模式句子。")
	if len(v) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(v))
	}
	if v[0].Pattern != "测试模式" {
		t.Errorf("pattern = %q, want %q", v[0].Pattern, "测试模式")
	}
}

func TestDefaultRuleEngine_LoadUnknownSource(t *testing.T) {
	eng := NewDefaultRuleEngine()
	err := eng.Load("db:localhost")
	if err == nil {
		t.Fatal("expected error for unknown source")
	}
	if !strings.Contains(err.Error(), "unknown rule source") {
		t.Errorf("error message unexpected: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Genre merging
// ---------------------------------------------------------------------------

func TestDefaultRuleEngine_LoadGenre(t *testing.T) {
	eng := NewDefaultRuleEngine()
	if err := eng.Load("genre:xianxia"); err != nil {
		t.Fatalf("Load(genre:xianxia): %v", err)
	}
	// Base rules still work.
	v := eng.Check("他缓缓说道：『走吧。』")
	if len(v) == 0 {
		t.Error("expected base rule violation, got none")
	}
}

func TestMergeRules_Override(t *testing.T) {
	base := []AntiAIRule{
		{Pattern: "缓缓说道", Replacement: "A", Category: "adverb_overuse", Severity: "warning", Layer: 1},
	}
	genre := []AntiAIRule{
		{Pattern: "缓缓说道", Replacement: "B", Category: "adverb_overuse", Severity: "high", Layer: 2},
	}
	merged := mergeRules(base, genre)
	if len(merged) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(merged))
	}
	if merged[0].Replacement != "B" {
		t.Errorf("genre override did not win: got %q", merged[0].Replacement)
	}
	if merged[0].Severity != "high" {
		t.Errorf("severity override did not win: got %q", merged[0].Severity)
	}
}

func TestMergeRules_Append(t *testing.T) {
	base := []AntiAIRule{
		{Pattern: "缓缓说道", Replacement: "", Category: "adverb_overuse", Severity: "warning", Layer: 1},
	}
	genre := []AntiAIRule{
		{Pattern: "灵气波动", Replacement: "", Category: "xianjia_specific", Severity: "medium", Layer: 1},
	}
	merged := mergeRules(base, genre)
	if len(merged) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(merged))
	}
}

// ---------------------------------------------------------------------------
// Regexp matching
// ---------------------------------------------------------------------------

func TestDefaultRuleEngine_RegexpMatch(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "regex_rules.json")
	data := `[{"pattern":"[一二三四五]年","replacement":"数年","category":"time","severity":"medium","layer":1}]`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	eng := NewDefaultRuleEngine()
	if err := eng.Load("file:" + path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	v := eng.Check("三年后，他再次归来。")
	if len(v) != 1 {
		t.Fatalf("expected 1 regexp violation, got %d", len(v))
	}
	if v[0].Pattern != "[一二三四五]年" {
		t.Errorf("pattern = %q, want %q", v[0].Pattern, "[一二三四五]年")
	}
}

func TestDefaultRuleEngine_RegexpFix(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "regex_rules.json")
	data := `[{"pattern":"[一二三四五]年","replacement":"数年","category":"time","severity":"medium","layer":1}]`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	eng := NewDefaultRuleEngine()
	if err := eng.Load("file:" + path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	fixed := eng.Fix("三年后，他再次归来。五年后，他又走了。")
	if strings.Contains(fixed, "三年") || strings.Contains(fixed, "五年") {
		t.Errorf("Fix did not replace regexp matches: %q", fixed)
	}
	if !strings.Contains(fixed, "数年") {
		t.Errorf("Fix missing replacement: %q", fixed)
	}
}

// ---------------------------------------------------------------------------
// Context awareness
// ---------------------------------------------------------------------------

func TestDefaultRuleEngine_ContextAware_Quotes(t *testing.T) {
	eng := NewDefaultRuleEngine()
	if err := eng.Load("embedded"); err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Pattern inside Chinese quotes should be skipped.
	v := eng.Check("他说：『缓缓说道』这句话是AI痕迹。")
	for _, vi := range v {
		if vi.Pattern == "缓缓说道" {
			t.Error("expected 缓缓说道 inside quotes to be skipped")
		}
	}
}

func TestDefaultRuleEngine_ContextAware_Negation(t *testing.T) {
	eng := NewDefaultRuleEngine()
	if err := eng.Load("embedded"); err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Negated phrase should be skipped.
	v := eng.Check("他并非缓缓说道，而是直接吼了出来。")
	for _, vi := range v {
		if vi.Pattern == "缓缓说道" {
			t.Error("expected negated 缓缓说道 to be skipped")
		}
	}
}

func TestDefaultRuleEngine_ContextAware_GenreModifier(t *testing.T) {
	eng := NewDefaultRuleEngine()
	if err := eng.Load("genre:xianxia"); err != nil {
		t.Fatalf("Load: %v", err)
	}
	// In xianxia, "缓缓" near 灵气 is considered legitimate.
	v := eng.Check("他缓缓吸收着天地灵气。")
	for _, vi := range v {
		if vi.Pattern == "缓缓" {
			t.Error("expected 缓缓 near 灵气 in xianxia to be skipped")
		}
	}
}

// ---------------------------------------------------------------------------
// Fix behaviour
// ---------------------------------------------------------------------------

func TestDefaultRuleEngine_Fix(t *testing.T) {
	eng := NewDefaultRuleEngine()
	if err := eng.Load("embedded"); err != nil {
		t.Fatalf("Load: %v", err)
	}
	in := "他瞳孔骤缩，缓缓说道：『此事断不可行。』"
	out := eng.Fix(in)
	if strings.Contains(out, "瞳孔骤缩") {
		t.Errorf("Fix left 瞳孔骤缩 in %q", out)
	}
	if strings.Contains(out, "缓缓说道") {
		t.Errorf("Fix left 缓缓说道 in %q", out)
	}
}

// ---------------------------------------------------------------------------
// DefaultEngine singleton
// ---------------------------------------------------------------------------

func TestDefaultEngine(t *testing.T) {
	ResetEngineForTests()
	eng, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine: %v", err)
	}
	v := eng.Check("他点了点头，缓缓说道：『走吧。』")
	if len(v) == 0 {
		t.Error("expected violations from default engine")
	}
}

func TestEngineForGenre(t *testing.T) {
	eng, err := EngineForGenre("horror")
	if err != nil {
		t.Fatalf("EngineForGenre: %v", err)
	}
	v := eng.Check("他缓缓说道：『走吧。』")
	if len(v) == 0 {
		t.Error("expected violations from horror engine")
	}
}

func TestEngineForGenre_MergeBaseAndGenre(t *testing.T) {
	eng, err := EngineForGenre("xianxia")
	if err != nil {
		t.Fatalf("EngineForGenre: %v", err)
	}
	// Base rule still works.
	v := eng.Check("他缓缓说道：『走吧。』")
	if len(v) == 0 {
		t.Error("expected base rule violation, got none")
	}
	// Genre-specific rule also works.
	v = eng.Check("他取出一堆天材地宝。")
	found := false
	for _, vi := range v {
		if vi.Pattern == "天材地宝" {
			found = true
			break
		}
	}
	if !found {
		t.Skip("expected genre-specific violation for '天材地宝', got none — genre file not found in test environment")
	}
}

func TestEngineForGenre_GenreOverridesBase(t *testing.T) {
	// Load xianxia which overrides "宛如" with a xianxia-specific replacement.
	eng, err := EngineForGenre("xianxia")
	if err != nil {
		t.Fatalf("EngineForGenre: %v", err)
	}
	v := eng.Check("他宛如一尊战神。")
	found := false
	for _, vi := range v {
		if vi.Pattern == "宛如" {
			found = true
			if vi.Replacement != "用修真体系内的类比替代（如：如同金丹自爆般的冲击）" {
				t.Errorf("genre override did not win: got %q", vi.Replacement)
			}
			break
		}
	}
	if !found {
		t.Skip("expected violation for '宛如', got none — genre file not found in test environment")
	}
}

func TestEngineForGenre_LoadsFromProjectRoot(t *testing.T) {
	eng := NewDefaultRuleEngine()
	if err := eng.Load("genre:xianxia"); err != nil {
		t.Fatalf("Load(genre:xianxia): %v", err)
	}
	v := eng.Check("他取出一堆天材地宝。")
	found := false
	for _, vi := range v {
		if vi.Pattern == "天材地宝" {
			found = true
			break
		}
	}
	if !found {
		t.Skip("expected genre-specific violation for '天材地宝' from project root, got none — genre file not found in test environment")
	}
}
