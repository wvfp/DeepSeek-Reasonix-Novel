package safety

import (
	"strings"
	"testing"

	"reasonix/internal/novel/config"
)

func TestSimpleChecker_DisabledByDefault(t *testing.T) {
	cfg := config.ContentSafetyConfig{Enabled: false}
	checker := NewSimpleChecker(cfg)

	safe, issues := checker.CheckPrompt("这句话包含色情内容")
	if !safe {
		t.Errorf("expected safe=true when disabled, got false")
	}
	if len(issues) != 0 {
		t.Errorf("expected no issues when disabled, got %v", issues)
	}

	filtered := checker.Filter("这句话包含色情内容")
	if filtered != "这句话包含色情内容" {
		t.Errorf("expected unchanged text when disabled, got %s", filtered)
	}
}

func TestSimpleChecker_EnabledFiltersDefaultWords(t *testing.T) {
	cfg := config.ContentSafetyConfig{Enabled: true}
	checker := NewSimpleChecker(cfg)

	safe, issues := checker.CheckContent("这里有毒品和赌博")
	if safe {
		t.Error("expected safe=false for default sensitive words")
	}
	if len(issues) < 2 {
		t.Errorf("expected at least 2 issues, got %v", issues)
	}

	filtered := checker.Filter("这里有毒品和赌博")
	if strings.Contains(filtered, "毒品") || strings.Contains(filtered, "赌博") {
		t.Errorf("expected sensitive words to be replaced, got %s", filtered)
	}
}

func TestSimpleChecker_CustomWords(t *testing.T) {
	cfg := config.ContentSafetyConfig{
		Enabled:     true,
		CustomWords: []string{"自定义敏感词", "测试词"},
	}
	checker := NewSimpleChecker(cfg)

	safe, issues := checker.CheckPrompt("这句话有自定义敏感词")
	if safe {
		t.Error("expected safe=false for custom word")
	}
	found := false
	for _, w := range issues {
		if w == "自定义敏感词" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected custom word in issues, got %v", issues)
	}

	filtered := checker.Filter("这句话有自定义敏感词和测试词")
	if strings.Contains(filtered, "自定义敏感词") || strings.Contains(filtered, "测试词") {
		t.Errorf("expected custom words to be replaced, got %s", filtered)
	}
}

func TestSimpleChecker_SafeText(t *testing.T) {
	cfg := config.ContentSafetyConfig{Enabled: true}
	checker := NewSimpleChecker(cfg)

	safe, issues := checker.CheckContent("这是一段正常的网络小说内容")
	if !safe {
		t.Errorf("expected safe=true for clean text, got false with issues %v", issues)
	}
	if len(issues) != 0 {
		t.Errorf("expected no issues for clean text, got %v", issues)
	}
}

// TestTrieMultiPattern verifies that the trie matches multiple patterns efficiently.
func TestTrieMultiPattern(t *testing.T) {
	cfg := config.ContentSafetyConfig{
		Enabled:     true,
		CustomWords: []string{"敏感", "违规", "违法"},
	}
	checker := NewSimpleChecker(cfg)

	safe, issues := checker.CheckContent("这段文字包含敏感和违规内容")
	if safe {
		t.Error("expected safe=false for multiple matches")
	}
	if len(issues) < 2 {
		t.Errorf("expected at least 2 issues, got %v", issues)
	}
}

// TestContextExclusion verifies that context prefixes prevent false positives.
func TestContextExclusion(t *testing.T) {
	cfg := config.ContentSafetyConfig{Enabled: true}
	checker := NewSimpleChecker(cfg)

	cases := []struct {
		text     string
		expected bool // true = safe
	}{
		{"我们要禁毒", true},
		{"打击赌博行为", true},
		{"扫色情活动", true},
		{"这里有毒品", false},
		{"赌博害人", false},
		{"色情内容", false},
	}

	for _, c := range cases {
		safe, issues := checker.CheckContent(c.text)
		if safe != c.expected {
			t.Errorf("text %q: expected safe=%v, got safe=%v, issues=%v", c.text, c.expected, safe, issues)
		}
	}
}

// TestRegexRules verifies that regex patterns detect variant words.
func TestRegexRules(t *testing.T) {
	cfg := config.ContentSafetyConfig{
		Enabled:       true,
		RegexPatterns: []string{"色\\.情", "赌\\*博", "毒\\s品"},
	}
	checker := NewSimpleChecker(cfg)

	cases := []struct {
		text     string
		expected bool // true = safe
	}{
		{"色.情内容", false},
		{"赌*博行为", false},
		{"毒 品交易", false},
		{"正常色情", false}, // matched by trie, not regex
		{"正常内容", true},
	}

	for _, c := range cases {
		safe, issues := checker.CheckContent(c.text)
		if safe != c.expected {
			t.Errorf("text %q: expected safe=%v, got safe=%v, issues=%v", c.text, c.expected, safe, issues)
		}
	}
}

// TestWhitelist verifies that whitelisted terms are ignored.
func TestWhitelist(t *testing.T) {
	cfg := config.ContentSafetyConfig{
		Enabled:   true,
		Whitelist: []string{"角色名毒品", "专业术语赌博"},
	}
	checker := NewSimpleChecker(cfg)

	cases := []struct {
		text     string
		expected bool // true = safe
	}{
		{"角色名毒品出现了", true},
		{"专业术语赌博解释", true},
		{"这里有毒品", false},
		{"赌博害人", false},
	}

	for _, c := range cases {
		safe, issues := checker.CheckContent(c.text)
		if safe != c.expected {
			t.Errorf("text %q: expected safe=%v, got safe=%v, issues=%v", c.text, c.expected, safe, issues)
		}
	}
}

// TestDetectionLog verifies that detection events are recorded.
func TestDetectionLog(t *testing.T) {
	cfg := config.ContentSafetyConfig{Enabled: true}
	checker := NewSimpleChecker(cfg)

	_, issues := checker.CheckPrompt("这句话包含色情内容")
	if len(issues) == 0 {
		t.Fatal("expected issues for prompt check")
	}

	logs := checker.Logs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}
	if logs[0].Type != "prompt" {
		t.Errorf("expected log type 'prompt', got %s", logs[0].Type)
	}
	if len(logs[0].Matched) == 0 {
		t.Error("expected non-empty matched in log")
	}
	if !strings.Contains(logs[0].Text, "色情") {
		t.Errorf("expected log text to contain inspected text, got %s", logs[0].Text)
	}
}

// TestDetectionLogDeduplication verifies that duplicate matches are deduplicated.
func TestDetectionLogDeduplication(t *testing.T) {
	cfg := config.ContentSafetyConfig{Enabled: true}
	checker := NewSimpleChecker(cfg)

	_, issues := checker.CheckContent("色情色情色情")
	if len(issues) != 1 {
		t.Errorf("expected 1 deduplicated issue, got %v", issues)
	}
}

// TestFilterWithEnabled verifies Filter replaces sensitive words.
func TestFilterWithEnabled(t *testing.T) {
	cfg := config.ContentSafetyConfig{Enabled: true}
	checker := NewSimpleChecker(cfg)

	filtered := checker.Filter("这里有毒品和赌博")
	if strings.Contains(filtered, "毒品") || strings.Contains(filtered, "赌博") {
		t.Errorf("expected sensitive words to be replaced, got %s", filtered)
	}
}

// TestTruncate verifies the truncate helper.
func TestTruncate(t *testing.T) {
	cases := []struct {
		input    string
		max      int
		expected string
	}{
		{"short", 10, "short"},
		{"这是一个很长的中文文本", 5, "这是一个很..."},
		{"", 5, ""},
	}

	for _, c := range cases {
		got := truncate(c.input, c.max)
		if got != c.expected {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.input, c.max, got, c.expected)
		}
	}
}
