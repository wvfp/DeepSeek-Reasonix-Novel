package tools

import "context"

// LLMCaller is the abstraction the chapter_write tool uses to talk to
// an LLM. The default implementation (llm_default.go) wraps the
// reasonix agent. Tests substitute FakeLLM (in *_test.go) so the
// tool's logic — context loading, JSON parsing, persistence — can
// be exercised without a real model.
type LLMCaller interface {
	// Call invokes the LLM with system + user prompts and returns the
	// raw text response. Implementations are expected to return the
	// model's complete output; chapter_write is responsible for
	// extracting the JSON object out of the surrounding prose / code
	// fence.
	Call(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// styleAnchorPending is the literal placeholder the chapter_write
// system prompt injects in the spot Phase 4's style-anchor extractor
// will fill. Kept here so the writer and reviewer share a single
// source of truth.
const styleAnchorPending = "<<STYLE_ANCHOR_PENDING>>"
