package review

import (
	"context"
	"testing"

	"reasonix/internal/novel/domain"
)

// mockLLM 是一个返回固定字符串的 LLM 模拟器，用于测试建议生成。
type mockLLM struct {
	payload string
}

func (m *mockLLM) Call(_ context.Context, _, _ string) (string, error) {
	return m.payload, nil
}

// validSuggestionsPayload 是包含两条合法建议的 JSON 数组。
const validSuggestionsPayload = `[
  {"type": "fix_consistency", "location": "paragraph_3", "reason": "境界描述前后矛盾", "priority": 5},
  {"type": "enhance_description", "location": "line_50", "reason": "场景描写过于单薄", "priority": 3}
]`

// fencedSuggestionsPayload 模拟 LLM 用 Markdown 代码块包裹 JSON 数组的情况。
const fencedSuggestionsPayload = "```json\n" + validSuggestionsPayload + "\n```"

// invalidTypePayload 包含非法 type 值，应被过滤掉。
const invalidTypePayload = `[
  {"type": "fix_consistency", "location": "paragraph_1", "reason": "合法建议", "priority": 4},
  {"type": "unknown_type", "location": "paragraph_2", "reason": "非法类型", "priority": 2}
]`

// missingFieldPayload 包含缺少 location 和 reason 的条目，应被过滤。
const missingFieldPayload = `[
  {"type": "add_conflict", "location": "paragraph_1", "reason": "缺少冲突", "priority": 2},
  {"type": "remove_redundancy", "location": "", "reason": "位置为空", "priority": 1},
  {"type": "adjust_pacing", "location": "line_10", "reason": "", "priority": 3}
]`

// outOfRangePriorityPayload 包含越界 priority，应被裁剪到 1-5。
const outOfRangePriorityPayload = `[
  {"type": "enhance_description", "location": "paragraph_1", "reason": "测试下界", "priority": 0},
  {"type": "enhance_description", "location": "paragraph_2", "reason": "测试上界", "priority": 10}
]`

func TestLLMSuggestionGenerator_HappyPath(t *testing.T) {
	llm := &mockLLM{payload: validSuggestionsPayload}
	gen := NewLLMSuggestionGenerator(llm)

	review := &domain.Review{
		Dimension: "consistency",
		Score:     4.0,
		Issues: []domain.Issue{
			{Severity: domain.SeverityBlocker, Description: "境界前后矛盾", Location: "第 2 段"},
		},
	}

	suggestions, err := gen.Generate(context.Background(), review)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(suggestions) != 2 {
		t.Fatalf("expected 2 suggestions, got %d", len(suggestions))
	}

	if suggestions[0].Type != "fix_consistency" {
		t.Errorf("suggestion[0].Type = %q, want fix_consistency", suggestions[0].Type)
	}
	if suggestions[0].Location != "paragraph_3" {
		t.Errorf("suggestion[0].Location = %q, want paragraph_3", suggestions[0].Location)
	}
	if suggestions[0].Priority != 5 {
		t.Errorf("suggestion[0].Priority = %d, want 5", suggestions[0].Priority)
	}

	if suggestions[1].Type != "enhance_description" {
		t.Errorf("suggestion[1].Type = %q, want enhance_description", suggestions[1].Type)
	}
	if suggestions[1].Priority != 3 {
		t.Errorf("suggestion[1].Priority = %d, want 3", suggestions[1].Priority)
	}
}

func TestLLMSuggestionGenerator_FencedJSON(t *testing.T) {
	llm := &mockLLM{payload: fencedSuggestionsPayload}
	gen := NewLLMSuggestionGenerator(llm)

	review := &domain.Review{
		Dimension: "style",
		Score:     6.0,
		Issues:    []domain.Issue{},
	}

	suggestions, err := gen.Generate(context.Background(), review)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(suggestions) != 2 {
		t.Errorf("expected 2 suggestions, got %d", len(suggestions))
	}
}

func TestLLMSuggestionGenerator_InvalidTypeFiltered(t *testing.T) {
	llm := &mockLLM{payload: invalidTypePayload}
	gen := NewLLMSuggestionGenerator(llm)

	review := &domain.Review{
		Dimension: "plot",
		Score:     5.0,
		Issues: []domain.Issue{
			{Severity: domain.SeverityWarning, Description: "节奏拖沓", Location: "第 3 段"},
		},
	}

	suggestions, err := gen.Generate(context.Background(), review)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(suggestions) != 1 {
		t.Fatalf("expected 1 suggestion after filtering, got %d", len(suggestions))
	}
	if suggestions[0].Type != "fix_consistency" {
		t.Errorf("suggestion.Type = %q, want fix_consistency", suggestions[0].Type)
	}
}

func TestLLMSuggestionGenerator_MissingFieldFiltered(t *testing.T) {
	llm := &mockLLM{payload: missingFieldPayload}
	gen := NewLLMSuggestionGenerator(llm)

	review := &domain.Review{
		Dimension: "pacing",
		Score:     3.0,
		Issues: []domain.Issue{
			{Severity: domain.SeverityInfo, Description: "节奏一般", Location: "第 1 段"},
		},
	}

	suggestions, err := gen.Generate(context.Background(), review)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(suggestions) != 1 {
		t.Fatalf("expected 1 suggestion after filtering, got %d", len(suggestions))
	}
	if suggestions[0].Type != "add_conflict" {
		t.Errorf("suggestion.Type = %q, want add_conflict", suggestions[0].Type)
	}
}

func TestLLMSuggestionGenerator_OutOfRangePriorityClamped(t *testing.T) {
	llm := &mockLLM{payload: outOfRangePriorityPayload}
	gen := NewLLMSuggestionGenerator(llm)

	review := &domain.Review{
		Dimension: "hook",
		Score:     7.0,
		Issues:    []domain.Issue{},
	}

	suggestions, err := gen.Generate(context.Background(), review)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(suggestions) != 2 {
		t.Fatalf("expected 2 suggestions, got %d", len(suggestions))
	}
	if suggestions[0].Priority != 1 {
		t.Errorf("suggestion[0].Priority = %d, want 1 (clamped)", suggestions[0].Priority)
	}
	if suggestions[1].Priority != 5 {
		t.Errorf("suggestion[1].Priority = %d, want 5 (clamped)", suggestions[1].Priority)
	}
}

func TestLLMSuggestionGenerator_NoLLM(t *testing.T) {
	gen := NewLLMSuggestionGenerator(nil)
	review := &domain.Review{Dimension: "style", Score: 5.0}
	if _, err := gen.Generate(context.Background(), review); err == nil {
		t.Fatal("expected error when LLM is nil")
	}
}

func TestLLMSuggestionGenerator_NilReview(t *testing.T) {
	llm := &mockLLM{payload: validSuggestionsPayload}
	gen := NewLLMSuggestionGenerator(llm)
	if _, err := gen.Generate(context.Background(), nil); err == nil {
		t.Fatal("expected error when review is nil")
	}
}

func TestParseSuggestions_InvalidJSON(t *testing.T) {
	_, err := parseSuggestions("not json")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestValidateSuggestions_Empty(t *testing.T) {
	out := validateSuggestions([]Suggestion{})
	if len(out) != 0 {
		t.Errorf("expected empty output for empty input, got %d", len(out))
	}
}
