package review

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"reasonix/internal/novel/domain"
)

// SuggestionType 是可执行的修改建议类型。
type SuggestionType string

const (
	SuggestionAddConflict       SuggestionType = "add_conflict"
	SuggestionRemoveRedundancy  SuggestionType = "remove_redundancy"
	SuggestionAdjustPacing      SuggestionType = "adjust_pacing"
	SuggestionFixConsistency    SuggestionType = "fix_consistency"
	SuggestionEnhanceDescription SuggestionType = "enhance_description"
)

// ValidSuggestionTypes 是所有合法的建议类型。
var ValidSuggestionTypes = []SuggestionType{
	SuggestionAddConflict,
	SuggestionRemoveRedundancy,
	SuggestionAdjustPacing,
	SuggestionFixConsistency,
	SuggestionEnhanceDescription,
}

// Suggestion 是一条可执行的修改建议。
type Suggestion struct {
	Type     string `json:"type"`
	Location string `json:"location"`
	Reason   string `json:"reason"`
	Priority int    `json:"priority"`
}

// SuggestionGenerator 根据 Review 结果生成可执行建议。
type SuggestionGenerator interface {
	Generate(ctx context.Context, review *domain.Review) ([]Suggestion, error)
}

// LLMCaller 是 suggestion 包对 LLM 的最小抽象，与 tools.LLMCaller 解耦。
type LLMCaller interface {
	Call(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// LLMSuggestionGenerator 调用 LLM 生成可执行建议。
type LLMSuggestionGenerator struct {
	llm LLMCaller
}

// NewLLMSuggestionGenerator 创建一个新的 LLM 建议生成器。
func NewLLMSuggestionGenerator(llm LLMCaller) *LLMSuggestionGenerator {
	return &LLMSuggestionGenerator{llm: llm}
}

// Generate 调用 LLM 为给定的 Review 生成建议列表。
func (g *LLMSuggestionGenerator) Generate(ctx context.Context, review *domain.Review) ([]Suggestion, error) {
	if g.llm == nil {
		return nil, fmt.Errorf("LLMSuggestionGenerator: no LLM configured")
	}
	if review == nil {
		return nil, fmt.Errorf("LLMSuggestionGenerator: nil review")
	}

	systemPrompt := buildSuggestionSystemPrompt()
	userPrompt := buildSuggestionUserPrompt(review)

	raw, err := g.llm.Call(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("LLMSuggestionGenerator: llm call: %w", err)
	}

	suggestions, err := parseSuggestions(raw)
	if err != nil {
		return nil, fmt.Errorf("LLMSuggestionGenerator: parse: %w", err)
	}

	valid := validateSuggestions(suggestions)
	return valid, nil
}

func buildSuggestionSystemPrompt() string {
	return `你是一位小说编辑助手。请根据提供的审查维度与问题，生成可执行的修改建议。

要求：
1. 每条建议必须包含 type、location、reason、priority 四个字段。
2. type 只能是以下五种之一：add_conflict、remove_redundancy、adjust_pacing、fix_consistency、enhance_description。
3. location 使用 paragraph_N 或 line_N 格式，表示建议作用的位置。
4. priority 为 1-5 的整数，1 最低，5 最高。
5. 请严格返回 JSON 数组，不要在 JSON 外包裹 Markdown 代码块或额外文字。`
}

func buildSuggestionUserPrompt(review *domain.Review) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("审查维度：%s\n", review.Dimension))
	b.WriteString(fmt.Sprintf("评分：%.1f\n", review.Score))
	b.WriteString("问题列表：\n")
	for i, issue := range review.Issues {
		b.WriteString(fmt.Sprintf("%d. [%s] %s (位置: %s)\n", i+1, issue.Severity, issue.Description, issue.Location))
	}
	b.WriteString("\n请生成可执行的修改建议（JSON 数组）：")
	return b.String()
}

var suggestionJSONRe = regexp.MustCompile(`(?s)\[.*\]`)

func parseSuggestions(raw string) ([]Suggestion, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		if i := strings.Index(raw, "\n"); i > 0 {
			raw = raw[i+1:]
		}
		if j := strings.LastIndex(raw, "```"); j > 0 {
			raw = raw[:j]
		}
		raw = strings.TrimSpace(raw)
	}

	loc := suggestionJSONRe.FindStringIndex(raw)
	if loc == nil {
		return nil, fmt.Errorf("no JSON array found in llm output")
	}

	var suggestions []Suggestion
	if err := json.Unmarshal([]byte(raw[loc[0]:loc[1]]), &suggestions); err != nil {
		return nil, fmt.Errorf("decode JSON: %w", err)
	}
	return suggestions, nil
}

func validateSuggestions(suggestions []Suggestion) []Suggestion {
	out := make([]Suggestion, 0, len(suggestions))
	for _, s := range suggestions {
		if !isValidSuggestionType(s.Type) {
			continue
		}
		if s.Location == "" {
			continue
		}
		if s.Reason == "" {
			continue
		}
		if s.Priority < 1 {
			s.Priority = 1
		}
		if s.Priority > 5 {
			s.Priority = 5
		}
		out = append(out, s)
	}
	return out
}

func isValidSuggestionType(t string) bool {
	for _, vt := range ValidSuggestionTypes {
		if string(vt) == t {
			return true
		}
	}
	return false
}
