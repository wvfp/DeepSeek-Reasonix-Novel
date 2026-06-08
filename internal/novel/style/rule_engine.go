package style

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"reasonix/internal/novel/genre"
)

// ---------------------------------------------------------------------------
// RuleEngine interface
// ---------------------------------------------------------------------------

// RuleEngine loads, checks and fixes text against anti-AI slop rules.
type RuleEngine interface {
	// Load initialises the engine from a source identifier.
	// Supported sources: "embedded", "file:<path>", "genre:<genreID>".
	Load(source string) error
	// Check returns every violation found in text.
	Check(text string) []Violation
	// Fix returns text with all matched patterns removed or replaced.
	Fix(text string) string
}

// ---------------------------------------------------------------------------
// Violation
// ---------------------------------------------------------------------------

// Violation is one detection hit produced by a RuleEngine.
type Violation struct {
	Pattern     string `json:"pattern"`
	Replacement string `json:"replacement"`
	Category    string `json:"category"`
	Severity    string `json:"severity"`
	Position    int    `json:"position"`
	Length      int    `json:"length"`
	Snippet     string `json:"snippet"`
}

// ---------------------------------------------------------------------------
// Rule (internal representation)
// ---------------------------------------------------------------------------

// compiledRule holds the decoded rule plus a compiled regexp.
type compiledRule struct {
	AntiAIRule
	re       *regexp.Regexp
	isRegexp bool
}

// ---------------------------------------------------------------------------
// DefaultRuleEngine
// ---------------------------------------------------------------------------

// DefaultRuleEngine is the canonical RuleEngine implementation.
// It supports multi-source loading, genre merging, regexp matching
// and context-aware filtering.
type DefaultRuleEngine struct {
	mu     sync.RWMutex
	rules  []compiledRule
	genre  string
	source string
}

// NewDefaultRuleEngine returns an empty engine. Call Load before use.
func NewDefaultRuleEngine() *DefaultRuleEngine {
	return &DefaultRuleEngine{}
}

// ---------------------------------------------------------------------------
// Load — multi-source
// ---------------------------------------------------------------------------

// Load initialises the engine. The source string may be:
//   - "embedded"                → load anti_ai_rules.json (embedded)
//   - "file:/abs/or/rel/path"   → load a JSON file from disk
//   - "genre:xianxia"           → merge embedded base + genre-specific rules
//   - "auto"                    → same as "embedded"
func (e *DefaultRuleEngine) Load(source string) error {
	switch {
	case source == "" || source == "embedded" || source == "auto":
		return e.loadEmbedded()
	case strings.HasPrefix(source, "file:"):
		return e.loadFile(strings.TrimPrefix(source, "file:"))
	case strings.HasPrefix(source, "genre:"):
		return e.loadGenre(strings.TrimPrefix(source, "genre:"))
	default:
		return fmt.Errorf("style: unknown rule source %q", source)
	}
}

//go:embed anti_ai_rules.json
var embeddedRulesJSON []byte

func (e *DefaultRuleEngine) loadEmbedded() error {
	rules, err := decodeRules(embeddedRulesJSON)
	if err != nil {
		return fmt.Errorf("style: load embedded rules: %w", err)
	}
	e.setRules(rules, "embedded", "")
	return nil
}

func (e *DefaultRuleEngine) loadFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("style: load file %s: %w", path, err)
	}
	rules, err := decodeRules(data)
	if err != nil {
		return fmt.Errorf("style: parse file %s: %w", path, err)
	}
	e.setRules(rules, path, "")
	return nil
}

func (e *DefaultRuleEngine) loadGenre(genreID string) error {
	base, err := decodeRules(embeddedRulesJSON)
	if err != nil {
		return fmt.Errorf("style: load base rules: %w", err)
	}

	genreRules, err := genre.LoadAntiAIRules(genreID)
	if err != nil {
		return fmt.Errorf("style: load genre rules for %s: %w", genreID, err)
	}
	if len(genreRules) > 0 {
		// Convert genre.AntiAIRule to style.AntiAIRule for merging.
		converted := make([]AntiAIRule, len(genreRules))
		for i, r := range genreRules {
			converted[i] = AntiAIRule{
				Pattern:     r.Pattern,
				Replacement: r.Replacement,
				Category:    r.Category,
				Severity:    r.Severity,
				Layer:       r.Layer,
			}
		}
		base = mergeRules(base, converted)
	} else {
		// Fallback: try direct file path relative to working directory.
		genrePath := filepath.Join("internal", "novel", "genre", genreID, "anti_ai.json")
		if data, err := os.ReadFile(genrePath); err == nil {
			if rules, err := decodeRules(data); err == nil && len(rules) > 0 {
				base = mergeRules(base, rules)
			}
		}
	}

	e.setRules(base, "genre:"+genreID, genreID)
	return nil
}

func (e *DefaultRuleEngine) setRules(raw []AntiAIRule, source, genre string) {
	compiled := make([]compiledRule, 0, len(raw))
	for _, r := range raw {
		cr := compiledRule{AntiAIRule: r}
		if looksLikeRegexp(r.Pattern) {
			if re, err := regexp.Compile(r.Pattern); err == nil {
				cr.re = re
				cr.isRegexp = true
			}
		}
		compiled = append(compiled, cr)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rules = compiled
	e.source = source
	e.genre = genre
}

// ---------------------------------------------------------------------------
// Check
// ---------------------------------------------------------------------------

// Check scans text and returns one Violation per occurrence.
// It respects context-aware filters (negative look-behind / look-ahead
// expressed as simple heuristics) so reasonable usage is not flagged.
func (e *DefaultRuleEngine) Check(text string) []Violation {
	e.mu.RLock()
	rules := e.rules
	e.mu.RUnlock()

	var out []Violation
	for _, r := range rules {
		if r.Pattern == "" {
			continue
		}
		matches := e.findMatches(text, r)
		for _, m := range matches {
			if e.shouldSkip(text, m, r) {
				continue
			}
			out = append(out, Violation{
				Pattern:     r.Pattern,
				Replacement: r.Replacement,
				Category:    r.Category,
				Severity:    r.Severity,
				Position:    m[0],
				Length:      m[1] - m[0],
				Snippet:     snippetAround(text, m[0], m[1], 12),
			})
		}
	}
	return out
}

func (e *DefaultRuleEngine) findMatches(text string, r compiledRule) [][2]int {
	if r.isRegexp && r.re != nil {
		var out [][2]int
		for _, m := range r.re.FindAllStringIndex(text, -1) {
			out = append(out, [2]int{m[0], m[1]})
		}
		return out
	}
	var out [][2]int
	from := 0
	for {
		idx := strings.Index(text[from:], r.Pattern)
		if idx < 0 {
			break
		}
		absStart := from + idx
		absEnd := absStart + len(r.Pattern)
		out = append(out, [2]int{absStart, absEnd})
		from = absEnd
		if from >= len(text) {
			break
		}
	}
	return out
}

// shouldSkip implements context-aware filtering.
// It returns true when the match sits inside a context that makes the
// usage reasonable (e.g. inside quotation marks, immediately preceded
// by a negation, or part of a larger compound word that changes meaning).
func (e *DefaultRuleEngine) shouldSkip(text string, m [2]int, r compiledRule) bool {
	start, end := m[0], m[1]

	// 1. Inside quotation marks — the pattern is being *discussed*, not used.
	if insideQuotes(text, start) {
		return true
	}

	// 2. Preceded by "不是" / "并非" — negation often legitimises the phrase.
	if hasNegationPrefix(text, start) {
		return true
	}

	// 3. Genre-specific whitelist: if the pattern is preceded/followed by
	//    genre-typical modifiers the hit is likely legitimate.
	if e.genre != "" && hasGenreModifier(text, start, end, e.genre) {
		return true
	}

	_ = end
	_ = r
	return false
}

// ---------------------------------------------------------------------------
// Fix
// ---------------------------------------------------------------------------

// Fix returns text with every matched pattern replaced by its Replacement
// string. If Replacement is empty the pattern is deleted.
func (e *DefaultRuleEngine) Fix(text string) string {
	e.mu.RLock()
	rules := e.rules
	e.mu.RUnlock()

	out := text
	for _, r := range rules {
		if r.Pattern == "" {
			continue
		}
		if r.isRegexp && r.re != nil {
			out = r.re.ReplaceAllString(out, r.Replacement)
		} else {
			out = strings.ReplaceAll(out, r.Pattern, r.Replacement)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func decodeRules(data []byte) ([]AntiAIRule, error) {
	var out []AntiAIRule
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// mergeRules overlays genreRules on top of base. If a genre rule shares
// the same Pattern with a base rule, the genre rule wins.
func mergeRules(base, genreRules []AntiAIRule) []AntiAIRule {
	seen := make(map[string]int, len(base))
	for i, r := range base {
		seen[r.Pattern] = i
	}
	out := make([]AntiAIRule, len(base))
	copy(out, base)
	for _, r := range genreRules {
		if idx, ok := seen[r.Pattern]; ok {
			out[idx] = r
		} else {
			out = append(out, r)
			seen[r.Pattern] = len(out) - 1
		}
	}
	return out
}

// looksLikeRegexp returns true when the pattern contains regexp metacharacters.
func looksLikeRegexp(s string) bool {
	for _, ch := range s {
		switch ch {
		case '.', '*', '+', '?', '^', '$', '(', ')', '[', ']', '{', '}', '|', '\\':
			return true
		}
	}
	return false
}

// insideQuotes reports whether pos is between paired Chinese or ASCII quotes.
func insideQuotes(text string, pos int) bool {
	inSingle := false
	inDouble := false
	inChinese := false
	for i, ch := range text {
		if i > pos {
			break
		}
		switch ch {
		case '"':
			inDouble = !inDouble
		case '\'':
			inSingle = !inSingle
		case '「', '『':
			inChinese = true
		case '」', '』':
			inChinese = false
		}
	}
	return inSingle || inDouble || inChinese
}

// hasNegationPrefix checks whether the 2-4 runes immediately before start
// form a Chinese negation that legitimises the phrase.
func hasNegationPrefix(text string, start int) bool {
	if start <= 0 {
		return false
	}
	prefix := text[:start]
	// Trim trailing punctuation / spaces.
	prefix = strings.TrimRight(prefix, " ，。、！？\t\n")
	negations := []string{"不是", "并非", "绝不", "没有", "并未", "不曾"}
	for _, n := range negations {
		if strings.HasSuffix(prefix, n) {
			return true
		}
	}
	return false
}

// hasGenreModifier checks whether the immediate context around the match
// contains genre-typical modifiers that make the phrase legitimate.
func hasGenreModifier(text string, start, end int, genre string) bool {
	window := 12
	lo := start - window
	if lo < 0 {
		lo = 0
	}
	hi := end + window
	if hi > len(text) {
		hi = len(text)
	}
	ctx := text[lo:hi]

	switch genre {
	case "xianxia":
		// In xianxia, "缓缓" + cultivation action is often legitimate.
		return strings.Contains(ctx, "灵气") || strings.Contains(ctx, "真元") || strings.Contains(ctx, "剑气")
	case "urban":
		return strings.Contains(ctx, "合同") || strings.Contains(ctx, "会议") || strings.Contains(ctx, "咖啡")
	case "horror":
		return strings.Contains(ctx, "规则") || strings.Contains(ctx, "怪谈") || strings.Contains(ctx, "日记")
	}
	return false
}

// ---------------------------------------------------------------------------
// Global singleton (backward-compatible hot path)
// ---------------------------------------------------------------------------

var (
	engineOnce  sync.Once
	engineCache RuleEngine
	engineErr   error
)

// DefaultEngine returns the shared RuleEngine, initialised from the
// embedded rule set on first call. The result is cached.
func DefaultEngine() (RuleEngine, error) {
	engineOnce.Do(func() {
		eng := NewDefaultRuleEngine()
		engineErr = eng.Load("embedded")
		if engineErr == nil {
			engineCache = eng
		}
	})
	if engineErr != nil {
		return nil, engineErr
	}
	return engineCache, nil
}

// EngineForGenre returns a RuleEngine loaded with base + genre-specific rules.
func EngineForGenre(genreID string) (RuleEngine, error) {
	eng := NewDefaultRuleEngine()
	if err := eng.Load("genre:" + genreID); err != nil {
		return nil, err
	}
	return eng, nil
}

// ResetEngineForTests clears the singleton so tests can re-initialise it.
func ResetEngineForTests() {
	engineOnce = sync.Once{}
	engineCache = nil
	engineErr = nil
}
