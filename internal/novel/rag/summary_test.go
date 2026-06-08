package rag

import (
	"context"
	"errors"
	"testing"
)

// fakeLLMCaller implements tools.LLMCaller for tests.
type fakeLLMCaller struct {
	resp string
	err  error
}

func (f *fakeLLMCaller) Call(_ context.Context, _, _ string) (string, error) {
	return f.resp, f.err
}

func TestLLMSummarizer_Generate_HappyPath(t *testing.T) {
	resp := `{
  "summary": "萧炎在乌坦城遭遇退婚，立下三年之约。",
  "key_events": ["纳兰嫣然退婚", "萧炎立誓"],
  "key_characters": ["萧炎", "纳兰嫣然"],
  "key_locations": ["乌坦城", "萧家大厅"],
  "key_items": ["婚书", "斗之气"]
}`
	sum := NewLLMSummarizer(&fakeLLMCaller{resp: resp})
	cs, err := sum.Generate(context.Background(), "萧炎站在萧家大厅……")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cs.Summary == "" {
		t.Error("expected non-empty summary")
	}
	if len(cs.KeyEvents) != 2 {
		t.Errorf("key_events len = %d, want 2", len(cs.KeyEvents))
	}
	if len(cs.KeyCharacters) != 2 {
		t.Errorf("key_characters len = %d, want 2", len(cs.KeyCharacters))
	}
	if len(cs.KeyLocations) != 2 {
		t.Errorf("key_locations len = %d, want 2", len(cs.KeyLocations))
	}
	if len(cs.KeyItems) != 2 {
		t.Errorf("key_items len = %d, want 2", len(cs.KeyItems))
	}
}

func TestLLMSummarizer_Generate_MarkdownFence(t *testing.T) {
	resp := "```json\n" + `{"summary":" fenced ","key_events":[],"key_characters":[],"key_locations":[],"key_items":[]}` + "\n```"
	sum := NewLLMSummarizer(&fakeLLMCaller{resp: resp})
	cs, err := sum.Generate(context.Background(), "text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cs.Summary != " fenced " {
		t.Errorf("summary = %q, want %q", cs.Summary, " fenced ")
	}
}

func TestLLMSummarizer_Generate_NilSlicesNormalised(t *testing.T) {
	resp := `{"summary":"only summary","key_events":null,"key_characters":null,"key_locations":null,"key_items":null}`
	sum := NewLLMSummarizer(&fakeLLMCaller{resp: resp})
	cs, err := sum.Generate(context.Background(), "text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cs.KeyEvents == nil {
		t.Error("KeyEvents should be non-nil")
	}
	if cs.KeyCharacters == nil {
		t.Error("KeyCharacters should be non-nil")
	}
	if cs.KeyLocations == nil {
		t.Error("KeyLocations should be non-nil")
	}
	if cs.KeyItems == nil {
		t.Error("KeyItems should be non-nil")
	}
}

func TestLLMSummarizer_Generate_LLMError(t *testing.T) {
	sum := NewLLMSummarizer(&fakeLLMCaller{err: errors.New("boom")})
	if _, err := sum.Generate(context.Background(), "text"); err == nil {
		t.Fatal("expected error")
	}
}

func TestLLMSummarizer_Generate_NilLLM(t *testing.T) {
	sum := NewLLMSummarizer(nil)
	if _, err := sum.Generate(context.Background(), "text"); err == nil {
		t.Fatal("expected error for nil llm")
	}
}

func TestLLMSummarizer_Generate_EmptySummary(t *testing.T) {
	resp := `{"summary":"   ","key_events":[],"key_characters":[],"key_locations":[],"key_items":[]}`
	sum := NewLLMSummarizer(&fakeLLMCaller{resp: resp})
	if _, err := sum.Generate(context.Background(), "text"); err == nil {
		t.Fatal("expected error for empty summary")
	}
}

func TestLLMSummarizer_Generate_InvalidJSON(t *testing.T) {
	sum := NewLLMSummarizer(&fakeLLMCaller{resp: "not json"})
	if _, err := sum.Generate(context.Background(), "text"); err == nil {
		t.Fatal("expected error for invalid json")
	}
}

func TestParseSummary_NoJSON(t *testing.T) {
	if _, err := parseSummary("no braces here"); err == nil {
		t.Fatal("expected error")
	}
}
