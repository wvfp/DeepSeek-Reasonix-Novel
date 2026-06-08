// Package safety provides optional content safety checking for prompts
// and generated chapter text. It is disabled by default and can be
// enabled via config.ContentSafety.Enabled.
package safety

import (
	"log"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"reasonix/internal/novel/config"
)

// Checker defines the contract for content safety validation.
type Checker interface {
	// CheckPrompt inspects a user-supplied prompt before it is sent
	// to the LLM. Returns safe=false and a list of matched issues
	// when sensitive words are found.
	CheckPrompt(prompt string) (safe bool, issues []string)

	// CheckContent inspects generated chapter text after the LLM
	// returns it. Returns safe=false and a list of matched issues
	// when sensitive words are found.
	CheckContent(content string) (safe bool, issues []string)
}

// DetectionLog records a single detection event for auditing.
type DetectionLog struct {
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`      // "prompt" or "content"
	Matched   []string  `json:"matched"`   // matched keywords/patterns
	Text      string    `json:"text"`      // the inspected text (truncated)
}

// LogStore is a thread-safe in-memory store for detection logs.
type LogStore struct {
	mu   sync.RWMutex
	logs []DetectionLog
}

// Append adds a new detection log.
func (s *LogStore) Append(l DetectionLog) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logs = append(s.logs, l)
}

// All returns a copy of all logs.
func (s *LogStore) All() []DetectionLog {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]DetectionLog, len(s.logs))
	copy(out, s.logs)
	return out
}

// trieNode is a node in the Aho-Corasick trie.
type trieNode struct {
	children map[rune]*trieNode
	fail     *trieNode
	outputs  []string
	isTerminal bool
}

// trie implements multi-pattern matching with Aho-Corasick.
type trie struct {
	root *trieNode
}

// newTrie builds a trie from the given words.
func newTrie(words []string) *trie {
	root := &trieNode{children: make(map[rune]*trieNode)}
	for _, w := range words {
		if w == "" {
			continue
		}
		node := root
		for _, r := range w {
			if node.children[r] == nil {
				node.children[r] = &trieNode{children: make(map[rune]*trieNode)}
			}
			node = node.children[r]
		}
		node.isTerminal = true
		node.outputs = append(node.outputs, w)
	}
	buildFailLinks(root)
	return &trie{root: root}
}

// buildFailLinks constructs failure links for Aho-Corasick.
func buildFailLinks(root *trieNode) {
	queue := make([]*trieNode, 0, 64)
	for _, child := range root.children {
		child.fail = root
		queue = append(queue, child)
	}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for r, child := range current.children {
			queue = append(queue, child)
			failNode := current.fail
			for failNode != nil && failNode.children[r] == nil {
				failNode = failNode.fail
			}
			if failNode == nil {
				child.fail = root
			} else {
				child.fail = failNode.children[r]
				child.outputs = append(child.outputs, child.fail.outputs...)
			}
		}
	}
}

// search finds all occurrences of patterns in text and returns matched words.
func (t *trie) search(text string) []string {
	if t.root == nil {
		return nil
	}
	matchedSet := make(map[string]struct{})
	node := t.root
	for _, r := range text {
		for node != t.root && node.children[r] == nil {
			node = node.fail
		}
		if next, ok := node.children[r]; ok {
			node = next
		} else {
			node = t.root
		}
		for _, out := range node.outputs {
			matchedSet[out] = struct{}{}
		}
	}
	if len(matchedSet) == 0 {
		return nil
	}
	result := make([]string, 0, len(matchedSet))
	for w := range matchedSet {
		result = append(result, w)
	}
	return result
}

// SimpleChecker implements Checker with a static word list plus
// user-defined custom words from configuration. Matching is performed
// with Aho-Corasick trie for performance, plus regex rules for variants,
// context-aware exclusions, and a whitelist.
type SimpleChecker struct {
	enabled           bool
	customWords       []string
	whitelist         []string
	regexRules        []*regexp.Regexp
	contextExclusions map[string][]string // keyword -> forbidden prefixes
	trie              *trie
	logStore          *LogStore
}

// NewSimpleChecker builds a Checker from the current configuration.
// When cfg.Enabled is false the checker is a no-op: every input is
// considered safe.
func NewSimpleChecker(cfg config.ContentSafetyConfig) *SimpleChecker {
	sc := &SimpleChecker{
		enabled:     cfg.Enabled,
		customWords: cfg.CustomWords,
		whitelist:   cfg.Whitelist,
		logStore:    &LogStore{},
	}
	if !sc.enabled {
		return sc
	}

	// Build trie from default + custom words.
	allWords := make([]string, 0, len(defaultSensitiveWords)+len(sc.customWords))
	allWords = append(allWords, defaultSensitiveWords...)
	allWords = append(allWords, sc.customWords...)
	sc.trie = newTrie(allWords)

	// Compile regex rules for variant detection.
	sc.regexRules = compileRegexRules(cfg.RegexPatterns)

	// Context exclusions: e.g. "毒品" should not match when preceded by "禁".
	sc.contextExclusions = map[string][]string{
		"毒品": {"禁", "反", "缉"},
		"赌博": {"禁", "反", "打击"},
		"色情": {"禁", "反", "扫"},
	}

	return sc
}

// compileRegexRules compiles regex patterns from strings.
func compileRegexRules(patterns []string) []*regexp.Regexp {
	var rules []*regexp.Regexp
	for _, p := range patterns {
		if p == "" {
			continue
		}
		re, err := regexp.Compile(p)
		if err != nil {
			log.Printf("safety: invalid regex pattern %q: %v", p, err)
			continue
		}
		rules = append(rules, re)
	}
	return rules
}

// defaultSensitiveWords is the built-in list of sensitive words.
// It is intentionally conservative to avoid over-blocking creative
// prose.
var defaultSensitiveWords = []string{
	"色情", "赌博", "毒品", "暴力", "恐怖",
}

// CheckPrompt implements Checker.
func (c *SimpleChecker) CheckPrompt(prompt string) (bool, []string) {
	return c.check("prompt", prompt)
}

// CheckContent implements Checker.
func (c *SimpleChecker) CheckContent(content string) (bool, []string) {
	return c.check("content", content)
}

func (c *SimpleChecker) check(checkType, text string) (bool, []string) {
	if !c.enabled {
		return true, nil
	}
	var issues []string

	// Trie matching.
	if c.trie != nil {
		matches := c.trie.search(text)
		for _, m := range matches {
			if c.isWhitelisted(text, m) {
				continue
			}
			if c.isContextExcluded(text, m) {
				continue
			}
			issues = append(issues, m)
		}
	}

	// Regex matching.
	for _, re := range c.regexRules {
		found := re.FindAllString(text, -1)
		for _, f := range found {
			if c.isWhitelisted(text, f) {
				continue
			}
			issues = append(issues, f)
		}
	}

	if len(issues) > 0 {
		// Deduplicate.
		seen := make(map[string]struct{})
		uniq := make([]string, 0, len(issues))
		for _, i := range issues {
			if _, ok := seen[i]; !ok {
				seen[i] = struct{}{}
				uniq = append(uniq, i)
			}
		}
		issues = uniq

		// Log detection.
		c.logStore.Append(DetectionLog{
			Timestamp: time.Now(),
			Type:      checkType,
			Matched:   issues,
			Text:      truncate(text, 200),
		})
		return false, issues
	}
	return true, nil
}

// isWhitelisted returns true if the matched word appears inside any whitelist entry.
func (c *SimpleChecker) isWhitelisted(text, matched string) bool {
	for _, w := range c.whitelist {
		if w == "" {
			continue
		}
		if strings.Contains(text, w) {
			// If the whitelist entry covers the matched word's position, skip.
			if strings.Contains(w, matched) || strings.Contains(matched, w) {
				return true
			}
		}
	}
	return false
}

// isContextExcluded returns true if the matched word is preceded by an exclusion prefix.
func (c *SimpleChecker) isContextExcluded(text, matched string) bool {
	exclusions, ok := c.contextExclusions[matched]
	if !ok {
		return false
	}
	idx := strings.Index(text, matched)
	if idx < 0 {
		return false
	}
	// Check up to 10 runes before the match.
	before := text[:idx]
	beforeRunes := []rune(before)
	checkLen := 10
	if len(beforeRunes) < checkLen {
		checkLen = len(beforeRunes)
	}
	if checkLen == 0 {
		return false
	}
	window := string(beforeRunes[len(beforeRunes)-checkLen:])
	for _, prefix := range exclusions {
		if strings.HasSuffix(window, prefix) {
			return true
		}
	}
	return false
}

// Filter replaces every occurrence of each sensitive word in text
// with "[内容已过滤]". It respects the enabled flag: when disabled
// the text is returned unchanged.
func (c *SimpleChecker) Filter(text string) string {
	if !c.enabled {
		return text
	}
	for _, w := range defaultSensitiveWords {
		text = strings.ReplaceAll(text, w, "[内容已过滤]")
	}
	for _, w := range c.customWords {
		if w != "" {
			text = strings.ReplaceAll(text, w, "[内容已过滤]")
		}
	}
	return text
}

// Logs returns all detection logs.
func (c *SimpleChecker) Logs() []DetectionLog {
	return c.logStore.All()
}

// truncate truncates s to maxRunes runes.
func truncate(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxRunes]) + "..."
}

// Ensure SimpleChecker implements Checker.
var _ Checker = (*SimpleChecker)(nil)
