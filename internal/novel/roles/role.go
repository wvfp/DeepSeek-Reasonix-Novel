// Package roles manages the 5 LLM roles that drive the novel pipeline.
//
// The Phase 3 pipeline (pipeline/orchestrator.go) routes different
// phases of the writing workflow to different system prompts. Rather
// than passing the role name into the chapter_write tool's prompt and
// paying the LLM's input token cost on every call, the Switcher
// composes the right prompt up front and calls the LLMCaller once.
//
// All five prompt bodies are embedded at compile time via //go:embed
// so the binary is self-contained. To add a new role: drop a *.md
// file under ./prompts/, declare the constant below, and add to
// PromptFile / LoadAllPrompts.
package roles

import (
	"context"
	"embed"
	"fmt"
	"strings"
)

// LLMCaller is the minimal interface the Switcher needs to dispatch
// a (system, user) prompt pair. The concrete implementation lives in
// the tools package (tools.LLMCaller, wired to provider.Provider by
// tools.ProviderLLMCaller). Defining the interface here lets the
// roles package avoid importing tools, which would create a cycle
// once tools/chapter_review.go and tools/review_fix.go start
// importing roles.
type LLMCaller interface {
	Call(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// Role is the typed name of an LLM persona. Constants are exported so
// tests and the orchestrator can refer to them without stringly typing.
type Role string

const (
	RoleWorldBuilder Role = "world_builder"
	RoleArcMaster    Role = "arc_master"
	RolePlotPlanner  Role = "plot_planner"
	RolePlotWriter   Role = "plot_writer"
	RoleReviewer     Role = "reviewer"
)

// AllRoles is the canonical iteration order. The pipeline depends on
// this slice being setting → planning → writing → reviewing, with the
// orchestrator's Run() method advancing one step at a time.
var AllRoles = []Role{
	RoleWorldBuilder,
	RoleArcMaster,
	RolePlotPlanner,
	RolePlotWriter,
	RoleReviewer,
}

// PromptsFS embeds every prompt markdown in the prompts/ subdir.
// Using a directory-level embed keeps the file layout in one place so
// adding a new role is just dropping a .md file.
//
//go:embed prompts/*.md
var PromptsFS embed.FS

// promptFile maps a Role to its prompts/*.md path. Kept as a
// table-driven lookup so unknown roles fail fast with a useful error.
var promptFile = map[Role]string{
	RoleWorldBuilder: "prompts/world_builder.md",
	RoleArcMaster:    "prompts/arc_master.md",
	RolePlotPlanner:  "prompts/plot_planner.md",
	RolePlotWriter:   "prompts/plot_writer.md",
	RoleReviewer:     "prompts/reviewer.md",
}

// PromptFile returns the embed-relative path of a role's prompt.
func PromptFile(r Role) string { return promptFile[r] }

// Switcher composes a base system prompt with a per-role suffix and
// dispatches LLM calls. The base prompt is the project-level wrapper
// (genre / temperature / no-leak guardrails) that every role shares;
// the suffix is the role-specific body loaded from prompts/*.md.
type Switcher struct {
	llm     LLMCaller
	baseSys string
	suffix  map[Role]string
}

// NewSwitcher builds a Switcher that uses llm as the LLMCaller and
// baseSys as the shared prefix. The base prefix may be empty, in
// which case SwitchTo returns the raw role prompt verbatim. All five
// role prompts are eagerly loaded; any disk/embed error is reported
// here so startup fails loudly.
func NewSwitcher(llm LLMCaller, baseSys string) (*Switcher, error) {
	loaded, err := LoadAllPrompts()
	if err != nil {
		return nil, err
	}
	return &Switcher{llm: llm, baseSys: baseSys, suffix: loaded}, nil
}

// SetBase updates the shared base prefix. Useful when the orchestrator
// wants to change project context between phases (e.g. switch from
// "phase=setting" to "phase=writing") without rebuilding the Switcher.
func (s *Switcher) SetBase(baseSys string) { s.baseSys = baseSys }

// LLM returns the underlying LLMCaller. The orchestrator (and any
// other consumer that wants to dispatch a custom prompt without
// routing through Switcher.Call) reads it via this accessor.
func (s *Switcher) LLM() LLMCaller { return s.llm }

// SwitchTo composes the system prompt for r by joining the base
// prefix with the role's body. Returns the full prompt as a string so
// callers can log it / inspect it.
func (s *Switcher) SwitchTo(r Role) string {
	body, ok := s.suffix[r]
	if !ok {
		body = string(r)
	}
	base := strings.TrimRight(s.baseSys, "\n")
	if base == "" {
		return body
	}
	return base + "\n\n" + body
}

// Call is the convenience wrapper: switch to r, then call the LLM
// with the composed system prompt and the user-supplied userPrompt.
// The LLMCaller contract is preserved (no retries, no streaming
// coercion) so a Switcher can sit behind any provider in tests.
func (s *Switcher) Call(ctx context.Context, r Role, userPrompt string) (string, error) {
	if s.llm == nil {
		return "", fmt.Errorf("roles.Switcher: nil LLMCaller")
	}
	return s.llm.Call(ctx, s.SwitchTo(r), userPrompt)
}

// LoadAllPrompts reads every embedded prompt into a map. Called by
// NewSwitcher and by tests that want to inspect the prompt bodies
// without going through a Switcher.
func LoadAllPrompts() (map[Role]string, error) {
	out := make(map[Role]string, len(promptFile))
	for r, path := range promptFile {
		b, err := PromptsFS.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("roles: load %s: %w", path, err)
		}
		out[r] = string(b)
	}
	return out, nil
}
