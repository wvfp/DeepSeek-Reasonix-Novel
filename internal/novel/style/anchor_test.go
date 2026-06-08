package style

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/novel/config"
)

func TestExtract_EmptyProject(t *testing.T) {
	root := t.TempDir()
	// Make sure no .novel-weaver exists.
	prof, err := Extract(root)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(prof.SentenceLengthDist) != 5 {
		t.Errorf("sentence dist len = %d, want 5", len(prof.SentenceLengthDist))
	}
	// Empty anchor should be persisted on disk so the chapter
	// write path can detect "no anchor configured".
	loaded, err := LoadProfile(root)
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	if len(loaded.SentenceLengthDist) != 5 {
		t.Errorf("LoadProfile after Extract: dist len = %d", len(loaded.SentenceLengthDist))
	}
}

func TestExtract_BuildsProfileFromChapters(t *testing.T) {
	root := t.TempDir()
	vol1 := filepath.Join(root, ".novel-weaver", "content", "chapters", "vol-1")
	if err := os.MkdirAll(vol1, 0o755); err != nil {
		t.Fatal(err)
	}
	// Three chapters with varied sentence length + dialogue.
	chapters := []string{
		"第一章\n\n萧炎踏进大厅，目光扫过全场。「你就是林动？」他淡淡问。",
		"第二章\n\n林动点头，拱手一礼，「在下林动，久仰萧兄大名。」",
		"第三章\n\n夜风凛冽。萧炎望着远山，一言不发。「走吧，」他低声道。",
	}
	for i, body := range chapters {
		path := filepath.Join(vol1, sprintf("ch%02d.md", i+1))
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	prof, err := Extract(root)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if prof.DialogueRatio <= 0 {
		t.Errorf("dialogue ratio = %f, want > 0", prof.DialogueRatio)
	}
	if sumInts(prof.SentenceLengthDist) < 3 {
		t.Errorf("sentence dist sum = %d, want ≥ 3", sumInts(prof.SentenceLengthDist))
	}
	if sumInts(prof.ParagraphLengthDist) < 3 {
		t.Errorf("paragraph dist sum = %d, want ≥ 3", sumInts(prof.ParagraphLengthDist))
	}
	if len(prof.TopBigrams) == 0 {
		t.Error("top bigrams is empty")
	}
	// New dimensions should also be populated.
	if prof.Rhetoric.Metaphor == 0 && prof.Rhetoric.Parallel == 0 && prof.Rhetoric.Hyperbole == 0 {
		t.Error("rhetoric profile is empty")
	}
	// Emotion may be empty for very short test text; only check type consistency.
	_ = prof.Emotion
	// Perspective may be "unknown" for very short test text without pronouns.
	_ = prof.Perspective
	// SaveProfile round-trip.
	data, _ := json.MarshalIndent(prof, "", "  ")
	var back StyleAnchorProfile
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("round-trip: %v", err)
	}
	if back.DialogueRatio != prof.DialogueRatio {
		t.Errorf("dialogue ratio round-trip lost: %f → %f", prof.DialogueRatio, back.DialogueRatio)
	}
}

func TestExtract_ManualOverride(t *testing.T) {
	root := t.TempDir()
	anchorDir := filepath.Join(root, ".novel-weaver", AnchorDirName)
	if err := os.MkdirAll(anchorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manual := `---
sentenceLengthDist: [10, 20, 30, 25, 15]
paragraphLengthDist: [5, 10, 20, 30, 10]
dialogueRatio: 0.42
rhetoric: {metaphor: 3, parallel: 2, hyperbole: 1}
emotion: {positive: 5, negative: 2, neutral: 8}
perspective: {dominant: first, stability: 0.95}
---
# 手动锚点

本项目的写作风格硬性要求：
- 句子偏短，避免长复合句。
- 段落不超过 200 字。
- 对话比例 40% 左右。
`
	if err := os.WriteFile(filepath.Join(anchorDir, ManualAnchorFileName), []byte(manual), 0o644); err != nil {
		t.Fatal(err)
	}
	prof, err := Extract(root)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if prof.DialogueRatio != 0.42 {
		t.Errorf("dialogue ratio = %f, want 0.42", prof.DialogueRatio)
	}
	if got := prof.SentenceLengthDist; len(got) != 5 || got[0] != 10 || got[4] != 15 {
		t.Errorf("sentence dist = %v, want [10 20 30 25 15]", got)
	}
	if prof.ManualAnchor == "" {
		t.Error("manual anchor path not recorded on profile")
	}
	if prof.Rhetoric.Metaphor != 3 || prof.Rhetoric.Parallel != 2 || prof.Rhetoric.Hyperbole != 1 {
		t.Errorf("rhetoric = %+v, want {3 2 1}", prof.Rhetoric)
	}
	if prof.Emotion.Positive != 5 || prof.Emotion.Negative != 2 || prof.Emotion.Neutral != 8 {
		t.Errorf("emotion = %+v, want {5 2 8}", prof.Emotion)
	}
	if prof.Perspective.Dominant != "first" || prof.Perspective.Stability != 0.95 {
		t.Errorf("perspective = %+v, want {first 0.95}", prof.Perspective)
	}
}

func TestExtract_ConfigurableDimensions(t *testing.T) {
	root := t.TempDir()
	vol1 := filepath.Join(root, ".novel-weaver", "content", "chapters", "vol-1")
	if err := os.MkdirAll(vol1, 0o755); err != nil {
		t.Fatal(err)
	}
	chapters := []string{
		"第一章\n\n萧炎踏进大厅，目光扫过全场。「你就是林动？」他淡淡问。",
		"第二章\n\n林动点头，拱手一礼，「在下林动，久仰萧兄大名。」",
	}
	for i, body := range chapters {
		path := filepath.Join(vol1, sprintf("ch%02d.md", i+1))
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Write a config that only enables sentence_length and dialogue_ratio.
	cfgPath := filepath.Join(root, ".novel-weaver", "config.json")
	cfg := config.Default()
	cfg.StyleAnchor.Dimensions = []string{"sentence_length", "dialogue_ratio"}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(cfgPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	prof, err := Extract(root)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if sumInts(prof.SentenceLengthDist) == 0 {
		t.Error("sentence_length should be extracted")
	}
	if prof.DialogueRatio == 0 {
		t.Error("dialogue_ratio should be extracted")
	}
	// paragraph_length is disabled → should remain zeroed.
	if sumInts(prof.ParagraphLengthDist) != 0 {
		t.Errorf("paragraph_length should be disabled, got %v", prof.ParagraphLengthDist)
	}
	if len(prof.TopBigrams) != 0 {
		t.Error("bigram_frequency should be disabled")
	}
}

func TestExtract_ConfigurableChapterCount(t *testing.T) {
	root := t.TempDir()
	vol1 := filepath.Join(root, ".novel-weaver", "content", "chapters", "vol-1")
	if err := os.MkdirAll(vol1, 0o755); err != nil {
		t.Fatal(err)
	}
	// Write 6 chapters; only the last 2 should be scanned.
	for i := 1; i <= 6; i++ {
		body := sprintf("第%d章\n\n测试文本。", i)
		path := filepath.Join(vol1, sprintf("ch%02d.md", i))
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfgPath := filepath.Join(root, ".novel-weaver", "config.json")
	cfg := config.Default()
	cfg.StyleAnchor.ChapterCount = 2
	data, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(cfgPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	prof, err := Extract(root)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if sumInts(prof.SentenceLengthDist) == 0 {
		t.Error("expected some sentences from the last 2 chapters")
	}
}

func TestFormatSystemBlock_Empty(t *testing.T) {
	if got := FormatSystemBlock(StyleAnchorProfile{}); got != "" {
		t.Errorf("FormatSystemBlock on empty = %q, want \"\"", got)
	}
}

func TestFormatSystemBlock_ContainsKeyStats(t *testing.T) {
	prof := StyleAnchorProfile{
		SentenceLengthDist:  []int{1, 2, 3, 4, 5},
		ParagraphLengthDist: []int{1, 2, 3, 4, 5},
		DialogueRatio:       0.35,
		TopBigrams: [][2]any{
			{"萧炎", 12.0}, {"林动", 8.0}, {"心中", 6.0},
		},
		PunctuationFreq: map[string]int{"。": 100, "，": 80},
		Rhetoric: RhetoricProfile{
			Metaphor:  2,
			Parallel:  1,
			Hyperbole: 0,
		},
		Emotion: EmotionProfile{
			Positive: 3,
			Negative: 1,
			Neutral:  6,
		},
		Perspective: PerspectiveProfile{
			Dominant:     "third",
			Stability:    0.88,
			FirstPerson:  2,
			ThirdPerson:  15,
		},
	}
	out := FormatSystemBlock(prof)
	if !strings.Contains(out, "[STYLE_ANCHOR]") {
		t.Errorf("missing [STYLE_ANCHOR] tag: %s", out)
	}
	if !strings.Contains(out, "dialogue_ratio: 0.35") {
		t.Errorf("missing dialogue_ratio: %s", out)
	}
	if !strings.Contains(out, "萧炎 × 12") {
		t.Errorf("missing top bigram: %s", out)
	}
	if !strings.Contains(out, "。=100") {
		t.Errorf("missing punctuation freq: %s", out)
	}
	if !strings.Contains(out, "rhetoric (metaphor=2, parallel=1, hyperbole=0)") {
		t.Errorf("missing rhetoric: %s", out)
	}
	if !strings.Contains(out, "emotion (positive=3, negative=1, neutral=6, total=10)") {
		t.Errorf("missing emotion: %s", out)
	}
	if !strings.Contains(out, "perspective (dominant=third, stability=0.88") {
		t.Errorf("missing perspective: %s", out)
	}
}

func TestLoadProfile_NoFile(t *testing.T) {
	root := t.TempDir()
	prof, err := LoadProfile(root)
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	if prof.DialogueRatio != 0 {
		t.Errorf("non-existent file should yield empty profile, got %+v", prof)
	}
}

// ---------------------------------------------------------------------------
// Dimension unit tests
// ---------------------------------------------------------------------------

func TestSentenceLengthDimension(t *testing.T) {
	d := SentenceLengthDimension{}
	chapters := []string{"第一章。第二章。第三章。"}
	val, err := d.Extract(chapters)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	dist, ok := val.([]int)
	if !ok || len(dist) != 5 {
		t.Fatalf("unexpected value %v", val)
	}
	if sumInts(dist) != 3 {
		t.Errorf("sum = %d, want 3", sumInts(dist))
	}
	formatted := d.Format(val)
	if !strings.Contains(formatted, "sentence_length_dist") {
		t.Errorf("unexpected format: %s", formatted)
	}
}

func TestParagraphLengthDimension(t *testing.T) {
	d := ParagraphLengthDimension{}
	chapters := []string{"第一段\n\n第二段\n\n第三段"}
	val, err := d.Extract(chapters)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	dist, ok := val.([]int)
	if !ok || len(dist) != 5 {
		t.Fatalf("unexpected value %v", val)
	}
	if sumInts(dist) != 3 {
		t.Errorf("sum = %d, want 3", sumInts(dist))
	}
}

func TestDialogueRatioDimension(t *testing.T) {
	d := DialogueRatioDimension{}
	chapters := []string{"「你好」他说。"}
	val, err := d.Extract(chapters)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	ratio, ok := val.(float64)
	if !ok {
		t.Fatalf("unexpected value %v", val)
	}
	if ratio <= 0 {
		t.Errorf("ratio = %f, want > 0", ratio)
	}
}

func TestBigramFrequencyDimension(t *testing.T) {
	d := BigramFrequencyDimension{}
	chapters := []string{"萧炎萧炎林动林动林动"}
	val, err := d.Extract(chapters)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	bigrams, ok := val.([][2]any)
	if !ok {
		t.Fatalf("unexpected value %v", val)
	}
	if len(bigrams) == 0 {
		t.Error("expected some bigrams")
	}
}

func TestPunctuationFrequencyDimension(t *testing.T) {
	d := PunctuationFrequencyDimension{}
	chapters := []string{"你好，世界。你好！"}
	val, err := d.Extract(chapters)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	freq, ok := val.(map[string]int)
	if !ok {
		t.Fatalf("unexpected value %v", val)
	}
	if freq["，"] != 1 {
		t.Errorf("comma count = %d, want 1", freq["，"])
	}
}

func TestRhetoricDimension(t *testing.T) {
	d := RhetoricDimension{}
	chapters := []string{"他像一座山。风如同刀割。"}
	val, err := d.Extract(chapters)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	prof, ok := val.(RhetoricProfile)
	if !ok {
		t.Fatalf("unexpected value %v", val)
	}
	if prof.Metaphor == 0 {
		t.Error("expected metaphor > 0")
	}
	formatted := d.Format(val)
	if !strings.Contains(formatted, "metaphor=") {
		t.Errorf("unexpected format: %s", formatted)
	}
}

func TestEmotionDimension(t *testing.T) {
	d := EmotionDimension{}
	chapters := []string{"他很高兴。她感到悲伤。"}
	val, err := d.Extract(chapters)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	prof, ok := val.(EmotionProfile)
	if !ok {
		t.Fatalf("unexpected value %v", val)
	}
	if prof.Positive == 0 {
		t.Error("expected positive > 0")
	}
	if prof.Negative == 0 {
		t.Error("expected negative > 0")
	}
}

func TestPerspectiveDimension(t *testing.T) {
	d := PerspectiveDimension{}
	chapters := []string{"他走了。她来了。他们在一起。"}
	val, err := d.Extract(chapters)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	prof, ok := val.(PerspectiveProfile)
	if !ok {
		t.Fatalf("unexpected value %v", val)
	}
	if prof.Dominant != "third" {
		t.Errorf("dominant = %s, want third", prof.Dominant)
	}
	if prof.Stability <= 0 {
		t.Errorf("stability = %f, want > 0", prof.Stability)
	}
}

func TestDimensionByName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"sentence_length", true},
		{"paragraph_length", true},
		{"dialogue_ratio", true},
		{"bigram_frequency", true},
		{"punctuation_frequency", true},
		{"rhetoric", true},
		{"emotion", true},
		{"perspective", true},
		{"unknown", false},
	}
	for _, tt := range tests {
		d := DimensionByName(tt.name)
		if tt.want && d == nil {
			t.Errorf("DimensionByName(%q) = nil, want non-nil", tt.name)
		}
		if !tt.want && d != nil {
			t.Errorf("DimensionByName(%q) = %v, want nil", tt.name, d)
		}
	}
}

func TestSelectDimensions(t *testing.T) {
	all := DefaultDimensions()
	if len(all) != 8 {
		t.Fatalf("expected 8 default dimensions, got %d", len(all))
	}
	filtered := selectDimensions([]string{"sentence_length", "dialogue_ratio"})
	if len(filtered) != 2 {
		t.Fatalf("expected 2 dimensions, got %d", len(filtered))
	}
	if filtered[0].Name() != "sentence_length" {
		t.Errorf("first dim = %s, want sentence_length", filtered[0].Name())
	}
	if filtered[1].Name() != "dialogue_ratio" {
		t.Errorf("second dim = %s, want dialogue_ratio", filtered[1].Name())
	}
}

// sumInts is a tiny helper for histogram assertions.
func sumInts(xs []int) int {
	s := 0
	for _, x := range xs {
		s += x
	}
	return s
}

// sprintf is a tiny shim so the test does not need to import
// fmt for the file-name generation.
func sprintf(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}
