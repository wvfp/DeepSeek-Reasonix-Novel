package tools

import (
	"context"
	"errors"
)

// ErrNoLLMConfigured is returned by NewDefaultLLMCaller().Call when
// the binary hasn't injected a real LLM caller. The CLI's novel
// chapter command installs one at startup; in-process tests use
// FakeLLM directly so they never hit this branch.
var ErrNoLLMConfigured = errors.New("defaultLLMCaller: no LLM configured — wire one via SetDefaultLLMCaller before calling chapter_write")

// defaultLLM is the package-level LLMCaller. The CLI sets it during
// process initialisation. When unset, chapter_write returns a clear
// error rather than panicking, so a unit test that forgets to wire
// the LLM still fails loudly.
var defaultLLM LLMCaller

// SetDefaultLLMCaller installs the package-level LLM caller. Called
// by the CLI's main once a controller is built. Safe to call multiple
// times — the last call wins.
func SetDefaultLLMCaller(c LLMCaller) { defaultLLM = c }

// DefaultLLMCaller returns the currently-installed LLM caller. May be
// nil before SetDefaultLLMCaller runs.
func DefaultLLMCaller() LLMCaller { return defaultLLM }

// NewDefaultLLMCaller is a no-op factory retained for callers that
// want to construct a placeholder (e.g. for nil-checks in tests).
// The actual call dispatch is in CallDefault.
func NewDefaultLLMCaller() LLMCaller { return &dispatchCaller{} }

// dispatchCaller is the LLMCaller returned by NewDefaultLLMCaller().
// Call routes through the package-level defaultLLM so a single CLI
// process can swap implementations.
type dispatchCaller struct{}

// Call forwards to the package-level LLMCaller. Returns
// ErrNoLLMConfigured when nothing has been wired.
func (dispatchCaller) Call(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	if defaultLLM == nil {
		return "", ErrNoLLMConfigured
	}
	return defaultLLM.Call(ctx, systemPrompt, userPrompt)
}
