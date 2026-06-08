package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// ChapterSummary is the structured summary for a single chapter.
type ChapterSummary struct {
	Summary       string   `json:"summary"`
	KeyEvents     []string `json:"key_events"`
	KeyCharacters []string `json:"key_characters"`
	KeyLocations  []string `json:"key_locations"`
	KeyItems      []string `json:"key_items"`
}

// Summarizer generates a structured summary from chapter text.
type Summarizer interface {
	Generate(ctx context.Context, chapterText string) (*ChapterSummary, error)
}

// LLMCaller is the minimal LLM interface needed by LLMSummarizer.
// It mirrors tools.LLMCaller to avoid an import cycle.
type LLMCaller interface {
	Call(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// LLMSummarizer uses an LLM to produce a structured ChapterSummary.
// It expects the model to return strict JSON with the fields defined
// in ChapterSummary.
type LLMSummarizer struct {
	llm LLMCaller
}

// NewLLMSummarizer creates a Summarizer backed by the given LLM caller.
func NewLLMSummarizer(llm LLMCaller) *LLMSummarizer {
	return &LLMSummarizer{llm: llm}
}

// systemPrompt is the fixed system prompt for summary generation.
const summarySystemPrompt = `你是小说章节摘要助手。请根据提供的章节正文，生成一份结构化摘要。

要求：
1. summary：用 200 字左右概括本章核心内容。
2. key_events：列出本章关键事件（3-7 条）。
3. key_characters：列出本章出现的重要角色名。
4. key_locations：列出本章涉及的关键地点。
5. key_items：列出本章出现的重要物品或功法。

请严格返回下面的 JSON（不要在 JSON 外面包任何 Markdown 围栏或额外文字）：
{
  "summary": "...",
  "key_events": ["..."],
  "key_characters": ["..."],
  "key_locations": ["..."],
  "key_items": ["..."]
}`

// Generate calls the LLM and parses the resulting JSON into a
// ChapterSummary.  It validates that the summary is non-empty and
// that all slice fields are non-nil (they may be empty).
func (s *LLMSummarizer) Generate(ctx context.Context, chapterText string) (*ChapterSummary, error) {
	if s.llm == nil {
		return nil, fmt.Errorf("LLMSummarizer: nil LLM caller")
	}

	userPrompt := fmt.Sprintf("章节正文：\n\n%s\n\n直接输出 JSON。", chapterText)

	raw, err := s.llm.Call(ctx, summarySystemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("summarizer: llm call: %w", err)
	}

	cs, err := parseSummary(raw)
	if err != nil {
		return nil, fmt.Errorf("summarizer: parse output: %w", err)
	}

	if err := validateSummary(cs); err != nil {
		return nil, fmt.Errorf("summarizer: validation: %w", err)
	}

	return cs, nil
}

var jsonObjRe = regexp.MustCompile(`(?s)\{.*\}`)

func parseSummary(raw string) (*ChapterSummary, error) {
	raw = strings.TrimSpace(raw)
	// Strip leading ```json / ``` fences.
	if strings.HasPrefix(raw, "```") {
		if i := strings.Index(raw, "\n"); i > 0 {
			raw = raw[i+1:]
		}
		if j := strings.LastIndex(raw, "```"); j > 0 {
			raw = raw[:j]
		}
		raw = strings.TrimSpace(raw)
	}

	loc := jsonObjRe.FindStringIndex(raw)
	if loc == nil {
		return nil, fmt.Errorf("no JSON object found")
	}
	body := raw[loc[0]:loc[1]]

	var cs ChapterSummary
	if err := json.Unmarshal([]byte(body), &cs); err != nil {
		return nil, fmt.Errorf("decode JSON: %w", err)
	}

	// Normalise nil slices to empty slices.
	if cs.KeyEvents == nil {
		cs.KeyEvents = []string{}
	}
	if cs.KeyCharacters == nil {
		cs.KeyCharacters = []string{}
	}
	if cs.KeyLocations == nil {
		cs.KeyLocations = []string{}
	}
	if cs.KeyItems == nil {
		cs.KeyItems = []string{}
	}

	return &cs, nil
}

func validateSummary(cs *ChapterSummary) error {
	if cs == nil {
		return fmt.Errorf("nil summary")
	}
	if strings.TrimSpace(cs.Summary) == "" {
		return fmt.Errorf("summary is empty")
	}
	return nil
}
