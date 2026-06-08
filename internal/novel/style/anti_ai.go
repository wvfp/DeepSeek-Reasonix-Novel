// Package style provides style enforcement, anti-AI hygiene, voice
// profile extraction, and style-anchor analysis for novel chapters.
//
// anti_ai.go is the rule engine that ships ~100 Chinese web-novel
// anti-AI slop patterns. The rules are loaded from the embedded
// anti_ai_rules.json so tests and binaries do not depend on a
// side-loaded config file. Use LoadRules() to obtain the canonical
// slice, ApplyFix() to scrub chapter text post-write, and
// DetectPatterns() to surface every match (used by the editor UI
// to render inline highlights).
package style

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	novelconfig "reasonix/internal/novel/config"
)

//go:embed anti_ai_rules.json
var antiAIRulesJSON []byte

// AntiAIRule is one row in the rules table. The fields mirror the
// JSON shape; Layer + Severity drive the inline highlight colour
// in the editor UI.
type AntiAIRule struct {
	Pattern     string `json:"pattern"`
	Replacement string `json:"replacement"`
	Category    string `json:"category"`
	Severity    string `json:"severity"`
	Layer       int    `json:"layer"`
}

// Severity buckets. The ChapterReview tool maps these to its own
// severity vocabulary so the dashboard can render them with the
// same colour palette.
const (
	AntiAISeverityLow    = "low"
	AntiAISeverityMedium = "medium"
	AntiAISeverityWarning = "warning"
	AntiAISeverityHigh   = "high"
)

// Category buckets. Use GetRulesByLayer / GetRulesByCategory to
// narrow a rule set for a focused review pass.
const (
	AntiAICategoryAdverbOveruse     = "adverb_overuse"
	AntiAICategoryEmotionTagging    = "emotion_tagging"
	AntiAICategoryDialogFormality   = "dialog_formality"
	AntiAICategorySummaryTendency   = "summary_tendency"
	AntiAICategoryStructureClosure  = "structure_closure"
	AntiAICategoryTransitionFormula = "transition_formula"
	AntiAICategoryInfoExposition    = "info_exposition"
)

var (
	rulesOnce  sync.Once
	rulesCache []AntiAIRule
	rulesErr   error
)

// LoadRules returns the canonical rule list. The first call decodes
// the embedded JSON; subsequent calls return the cached slice so
// hot paths (chapter_write post-processing) do not pay the parse
// cost. The slice is never mutated by callers — the engine treats
// it as read-only.
func LoadRules() ([]AntiAIRule, error) {
	rulesOnce.Do(func() {
		var out []AntiAIRule
		rulesErr = json.Unmarshal(antiAIRulesJSON, &out)
		if rulesErr != nil {
			return
		}
		// Sort by descending layer so the higher-priority rules
		// (later layers in the source schema) are easier to
		// inspect in tests. Stable: order within a layer is the
		// original JSON order.
		sort.SliceStable(out, func(i, j int) bool {
			return out[i].Layer > out[j].Layer
		})
		rulesCache = out
	})
	if rulesErr != nil {
		return nil, fmt.Errorf("style: load anti-AI rules: %w", rulesErr)
	}
	return rulesCache, nil
}

// ---------------------------------------------------------------------------
// Config-driven engine
// ---------------------------------------------------------------------------

// engineFromConfig returns a RuleEngine initialised according to cfg.
// If cfg is nil or the source is "embedded"/"auto" it falls back to
// the shared DefaultEngine().
func engineFromConfig(cfg *novelconfig.AntiAIConfig) (RuleEngine, error) {
	if cfg == nil || cfg.RulesSource == "" || cfg.RulesSource == "embedded" || cfg.RulesSource == "auto" {
		return DefaultEngine()
	}
	eng := NewDefaultRuleEngine()
	src := cfg.RulesSource
	if src == "file" && cfg.RulesFile != "" {
		src = "file:" + cfg.RulesFile
	}
	if err := eng.Load(src); err != nil {
		return nil, err
	}
	return eng, nil
}

// severityAbove returns true when sev meets or exceeds threshold.
// Severity order: low < medium < warning < high.
func severityAbove(sev, threshold string) bool {
	order := map[string]int{
		AntiAISeverityLow:     1,
		AntiAISeverityMedium:  2,
		AntiAISeverityWarning: 3,
		AntiAISeverityHigh:    4,
	}
	return order[sev] >= order[threshold]
}

// GetRulesByLayer narrows LoadRules to a single layer. Used by the
// editor UI to render one "AI slop level" tab at a time.
func GetRulesByLayer(layer int) []AntiAIRule {
	all, err := LoadRules()
	if err != nil {
		return nil
	}
	out := make([]AntiAIRule, 0, len(all)/7+1)
	for _, r := range all {
		if r.Layer == layer {
			out = append(out, r)
		}
	}
	return out
}

// GetRulesByCategory narrows LoadRules to a single category.
// Useful when the chapter_review tool wants to surface only
// "dialog_formality" findings.
func GetRulesByCategory(category string) []AntiAIRule {
	all, err := LoadRules()
	if err != nil {
		return nil
	}
	out := make([]AntiAIRule, 0, len(all)/7+1)
	for _, r := range all {
		if r.Category == category {
			out = append(out, r)
		}
	}
	return out
}

// AntiAIMatch is one detection hit. PatternIndex is the index into
// the LoadRules() slice; the caller can look up the rule's
// replacement / severity / layer from there.
type AntiAIMatch struct {
	Pattern     string `json:"pattern"`
	Replacement string `json:"replacement"`
	Category    string `json:"category"`
	Severity    string `json:"severity"`
	Layer       int    `json:"layer"`
	Start       int    `json:"start"`
	End         int    `json:"end"`
	Snippet     string `json:"snippet"`
}

// DetectPatterns scans text for every rule and returns one
// AntiAIMatch per occurrence. Overlapping matches (a longer rule
// that contains a shorter one) are returned in source order. The
// caller can choose how to surface them — the editor UI uses
// (Start, End) for inline highlights, the chapter_write post-
// processor uses Snippet for diagnostic logs.
//
// The function is allocation-light: each iteration builds one
// AntiAIMatch on the stack. The biggest cost is the strings.Index
// calls; for very long chapter bodies (≥ 50 000 chars) consider
// using DetectPatternsWithLimit to cap output.
func DetectPatterns(text string) []AntiAIMatch {
	return DetectPatternsWithLimit(text, 0)
}

// DetectPatternsWithLimit caps the number of returned matches per
// text. A limit of 0 means "no cap". The chapter_write flow passes
// a sensible cap (e.g. 50) so a runaway pattern does not blow the
// log buffer.
func DetectPatternsWithLimit(text string, limit int) []AntiAIMatch {
	return DetectPatternsWithConfig(text, limit, nil)
}

// DetectPatternsWithConfig uses the RuleEngine selected by cfg.
// If cfg is nil it falls back to the embedded rule set.
func DetectPatternsWithConfig(text string, limit int, cfg *novelconfig.AntiAIConfig) []AntiAIMatch {
	eng, err := engineFromConfig(cfg)
	if err != nil {
		return nil
	}
	violations := eng.Check(text)
	var out []AntiAIMatch
	for _, v := range violations {
		if cfg != nil && !severityAbove(v.Severity, cfg.SeverityThreshold) {
			continue
		}
		out = append(out, AntiAIMatch{
			Pattern:     v.Pattern,
			Replacement: v.Replacement,
			Category:    v.Category,
			Severity:    v.Severity,
			Start:       v.Position,
			End:         v.Position + v.Length,
			Snippet:     v.Snippet,
		})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// ApplyFix is the post-write scrub. It walks every rule and strips
// matched phrases from the chapter body. The chapter_write tool
// runs ApplyFix on the LLM's output before persisting so the
// anti-AI hygiene check passes downstream.
//
// ApplyFix is conservative: it only removes the literal pattern
// substring. It does not synthesise a replacement (the source
// rules describe what to replace with, but synthesising natural
// Chinese prose is a job for a follow-up LLM call, not a regex
// pass). The fix is therefore "delete the AI-slop phrase" — the
// LLM in a later review pass can re-flow the sentence.
func ApplyFix(text string) string {
	return ApplyFixWithConfig(text, nil)
}

// ApplyFixWithConfig uses the RuleEngine selected by cfg.
// If cfg is nil it falls back to the embedded rule set.
func ApplyFixWithConfig(text string, cfg *novelconfig.AntiAIConfig) string {
	eng, err := engineFromConfig(cfg)
	if err != nil {
		return text
	}
	return eng.Fix(text)
}

// ApplyFixWithRules is the rule-list injection point used by
// tests. Pass nil to fall back to the embedded rule set.
func ApplyFixWithRules(text string, customRules []AntiAIRule) string {
	rules := customRules
	if rules == nil {
		var err error
		rules, err = LoadRules()
		if err != nil {
			return text
		}
	}
	out := text
	for _, r := range rules {
		if r.Pattern == "" {
			continue
		}
		out = strings.ReplaceAll(out, r.Pattern, "")
	}
	return out
}

// CountBySeverity tallies the matches per severity bucket. Used
// by the chapter_review integration to add an "ai_slop_score"
// field to the 8-dimension radar.
func CountBySeverity(matches []AntiAIMatch) map[string]int {
	out := map[string]int{
		AntiAISeverityLow:     0,
		AntiAISeverityMedium:  0,
		AntiAISeverityWarning: 0,
		AntiAISeverityHigh:    0,
	}
	for _, m := range matches {
		out[m.Severity]++
	}
	return out
}

// snippetAround returns up to pad chars on each side of the
// [start, end) match in text. Bounds-checked and ellipsis-trimmed
// so the result fits in a log line.
func snippetAround(text string, start, end, pad int) string {
	lo := start - pad
	if lo < 0 {
		lo = 0
	}
	hi := end + pad
	if hi > len(text) {
		hi = len(text)
	}
	prefix := ""
	if lo > 0 {
		prefix = "…"
	}
	suffix := ""
	if hi < len(text) {
		suffix = "…"
	}
	return prefix + text[lo:hi] + suffix
}
