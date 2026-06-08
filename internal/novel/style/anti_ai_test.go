package style

import (
	"strings"
	"testing"
)

func TestLoadRules(t *testing.T) {
	rules, err := LoadRules()
	if err != nil {
		t.Fatalf("LoadRules: %v", err)
	}
	if len(rules) < 100 {
		t.Errorf("got %d rules, want ≥ 100", len(rules))
	}
	// Spot-check a handful of well-known patterns from the
	// source JSON to make sure the embed + decode pipeline is
	// not silently dropping rows.
	patterns := map[string]bool{
		"缓缓说道": false, "瞳孔骤缩": false, "邪魅一笑": false,
		"夜幕降临": false, "她心里很清楚": false, "与此同时": false,
	}
	for _, r := range rules {
		if _, ok := patterns[r.Pattern]; ok {
			patterns[r.Pattern] = true
		}
	}
	for p, seen := range patterns {
		if !seen {
			t.Errorf("rule %q missing from embedded set", p)
		}
	}
	// Severity vocabulary is bounded; any value outside the
	// four known buckets would confuse the dashboard.
	for _, r := range rules {
		switch r.Severity {
		case AntiAISeverityLow, AntiAISeverityMedium, AntiAISeverityWarning, AntiAISeverityHigh:
		default:
			t.Errorf("rule %q has unknown severity %q", r.Pattern, r.Severity)
		}
	}
}

func TestDetectPatterns_AllMatch(t *testing.T) {
	rules, err := LoadRules()
	if err != nil {
		t.Fatalf("LoadRules: %v", err)
	}
	for _, r := range rules {
		text := "他说：" + r.Pattern + " 然后转身离开。"
		matches := DetectPatterns(text)
		found := false
		for _, m := range matches {
			if m.Pattern == r.Pattern {
				found = true
				if m.Snippet == "" {
					t.Errorf("rule %q: empty snippet", r.Pattern)
				}
				if m.Start < 0 || m.End <= m.Start {
					t.Errorf("rule %q: bad range [%d,%d)", r.Pattern, m.Start, m.End)
				}
			}
		}
		if !found {
			t.Errorf("rule %q not detected in %q", r.Pattern, text)
		}
	}
}

func TestDetectPatterns_NoFalsePositive(t *testing.T) {
	clean := "萧炎踏进大厅，目光扫过全场，对着林动抱了抱拳。众人都站了起来。"
	matches := DetectPatterns(clean)
	if len(matches) > 0 {
		t.Errorf("clean text produced %d matches, expected 0: %+v", len(matches), matches)
	}
}

func TestApplyFix(t *testing.T) {
	in := "他瞳孔骤缩，缓缓说道：『此事断不可行。』她心里很清楚这一点。"
	out := ApplyFix(in)
	for _, banned := range []string{"瞳孔骤缩", "缓缓说道", "她心里很清楚"} {
		if strings.Contains(out, banned) {
			t.Errorf("ApplyFix left %q in %q", banned, out)
		}
	}
	if !strings.Contains(out, "此事断不可行") {
		t.Errorf("ApplyFix deleted non-banned content: %q", out)
	}
}

func TestDetectPatternsWithLimit(t *testing.T) {
	text := strings.Repeat("他点了点头。", 10)
	matches := DetectPatternsWithLimit(text, 3)
	if len(matches) != 3 {
		t.Errorf("limit=3 returned %d matches, want exactly 3", len(matches))
	}
}

func TestCountBySeverity(t *testing.T) {
	matches := []AntiAIMatch{
		{Severity: AntiAISeverityHigh}, {Severity: AntiAISeverityHigh},
		{Severity: AntiAISeverityWarning},
	}
	c := CountBySeverity(matches)
	if c[AntiAISeverityHigh] != 2 || c[AntiAISeverityWarning] != 1 {
		t.Errorf("CountBySeverity = %+v", c)
	}
}

func TestGetRulesByLayer(t *testing.T) {
	rules, err := LoadRules()
	if err != nil {
		t.Fatalf("LoadRules: %v", err)
	}
	for layer := 1; layer <= 7; layer++ {
		got := GetRulesByLayer(layer)
		// Each layer is guaranteed to have at least one rule in
		// the source set; if the loop returns 0, the embed
		// pipeline missed a layer entirely.
		if len(got) == 0 {
			t.Errorf("layer %d has 0 rules", layer)
		}
		for _, r := range got {
			if r.Layer != layer {
				t.Errorf("layer %d filter returned rule with layer %d", layer, r.Layer)
			}
		}
	}
	_ = rules
}

func TestGetRulesByCategory(t *testing.T) {
	cases := []string{
		AntiAICategoryAdverbOveruse, AntiAICategoryEmotionTagging,
		AntiAICategoryDialogFormality, AntiAICategorySummaryTendency,
		AntiAICategoryStructureClosure, AntiAICategoryTransitionFormula,
		AntiAICategoryInfoExposition,
	}
	for _, c := range cases {
		got := GetRulesByCategory(c)
		if len(got) == 0 {
			t.Errorf("category %q has 0 rules", c)
		}
		for _, r := range got {
			if r.Category != c {
				t.Errorf("category filter for %q returned %q", c, r.Category)
			}
		}
	}
}

func TestSnippetAround(t *testing.T) {
	got := snippetAround("abcdefghij", 3, 5, 2)
	if !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") {
		t.Errorf("snippetAround: missing ellipsis on %q", got)
	}
	if !strings.Contains(got, "cde") {
		t.Errorf("snippetAround: missing match body in %q", got)
	}
}
