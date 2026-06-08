package tools

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/novel/domain"
)

// fakeRecordingLLM is a minimal LLMCaller that captures the
// (system, user) prompt it was invoked with. The chapter_write
// prompt integration tests use it to assert on what was sent
// to the model.
type fakeRecordingLLM struct {
	payload  string
	system   string
	user     string
	calls    int
}

func (f *fakeRecordingLLM) Call(_ context.Context, system, user string) (string, error) {
	f.system = system
	f.user = user
	f.calls++
	return f.payload, nil
}

// TestChapterWrite_PromptIncludesGenrePack: the chapter_write
// tool injects the embedded genre pack's [GENRE_PACK] block
// into the system prompt so the LLM has access to the genre's
// style guidelines and per-role prompts. The xianxia pack has
// the distinctive "修真" prompt fragment that the
// "infinite-flow" pack does not — we use that as the assertion
// signal.
func TestChapterWrite_PromptIncludesGenrePack(t *testing.T) {
	mgr, arcID := seedChapterWriteProject(t)
	llm := &fakeRecordingLLM{payload: fixedChapterPayload}
	cw := NewChapterWriteTool(llm)
	if _, err := cw.Execute(context.Background(), map[string]any{
		"arc_id": arcID,
		"title":  "开篇",
		"genre":  domain.GenreXianxia,
	}, mgr); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(llm.system, "[GENRE_PACK]") {
		t.Errorf("system prompt missing [GENRE_PACK] marker; full prompt:\n%s", llm.system)
	}
	if !strings.Contains(llm.system, "xianxia") {
		t.Errorf("system prompt missing genre id 'xianxia'")
	}
	if !strings.Contains(llm.system, "修真") {
		t.Errorf("system prompt missing xianxia-specific 修真 guidance")
	}
}

// TestChapterWrite_PromptIncludesUnknownGenreFallback: an
// unknown genre id falls back to the neutral fantasy stub
// rather than failing the write. The system prompt gets the
// [GENRE_PACK] block but with the fallback's display name.
func TestChapterWrite_PromptIncludesUnknownGenreFallback(t *testing.T) {
	mgr, arcID := seedChapterWriteProject(t)
	llm := &fakeRecordingLLM{payload: fixedChapterPayload}
	cw := NewChapterWriteTool(llm)
	if _, err := cw.Execute(context.Background(), map[string]any{
		"arc_id": arcID,
		"title":  "开篇",
		"genre":  "made-up-genre",
	}, mgr); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(llm.system, "[GENRE_PACK]") {
		t.Errorf("system prompt missing [GENRE_PACK] marker for fallback")
	}
	if !strings.Contains(llm.system, "made-up-genre") {
		t.Errorf("system prompt missing the unknown genre id (user's choice is reflected)")
	}
	if !strings.Contains(llm.system, "Generic / Fantasy") {
		t.Errorf("system prompt missing fallback display name")
	}
}
