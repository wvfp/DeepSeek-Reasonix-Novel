package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"reasonix/internal/provider"
)

// ProviderLLMCaller is the LLMCaller the CLI installs when the user
// has configured a real provider in reasonix.toml. It translates the
// (system, user) prompt pair into a single-turn provider.Request,
// streams the completion, and concatenates the text chunks into the
// final string chapter_write hands to parseChapterPayload.
//
// Reasoning-mode deltas (ChunkReasoning) are intentionally dropped —
// the chapter writer prompt asks for strict JSON, and silently
// prepending thinking to the JSON body would break the parser. Usage
// tokens are ignored; callers that want to surface them can read
// them from the wrapped provider's stream directly.
type ProviderLLMCaller struct {
	Provider    provider.Provider
	Temperature float64
	MaxTokens   int
}

// Call implements LLMCaller. Returns the assembled text or a wrapped
// error. The wrapped error preserves the chunk type when the stream
// reports an error (ChunkError) so the caller can distinguish a
// provider-side failure from a transport-level one.
func (c *ProviderLLMCaller) Call(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	if c == nil || c.Provider == nil {
		return "", errors.New("ProviderLLMCaller: provider is nil")
	}
	if strings.TrimSpace(systemPrompt) == "" && strings.TrimSpace(userPrompt) == "" {
		return "", errors.New("ProviderLLMCaller: empty prompts")
	}
	req := provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: systemPrompt},
			{Role: provider.RoleUser, Content: userPrompt},
		},
		Temperature: c.Temperature,
		MaxTokens:   c.MaxTokens,
	}
	ch, err := c.Provider.Stream(ctx, req)
	if err != nil {
		return "", fmt.Errorf("ProviderLLMCaller: stream: %w", err)
	}
	var b strings.Builder
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			b.WriteString(chunk.Text)
		case provider.ChunkError:
			if chunk.Err != nil {
				return "", fmt.Errorf("ProviderLLMCaller: stream chunk error: %w", chunk.Err)
			}
			return "", fmt.Errorf("ProviderLLMCaller: stream chunk error (no detail)")
		}
	}
	return b.String(), nil
}
