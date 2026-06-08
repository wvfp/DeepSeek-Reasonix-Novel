package roles

import (
	"context"
	"strings"
	"testing"
)

// tokenBudget is the per-prompt cap from the Phase 3 spec. 5 prompts
// total ≤ 10000 token → each ≤ 2000. We budget by character (1 token
// ≈ 1.5 chars for Chinese) and leave 10% headroom.
const tokenBudget = 2000

// fakeLLM is the no-op LLMCaller used by Switcher tests. It records
// the (system, user) pair it was called with and returns a fixture
// string, so the test can assert the system prompt contained the
// role-specific body.
type fakeLLM struct {
	lastSystem string
	lastUser   string
	resp       string
}

func (f *fakeLLM) Call(_ context.Context, system, user string) (string, error) {
	f.lastSystem = system
	f.lastUser = user
	return f.resp, nil
}

func TestLoadAllPrompts_AllFiveRoles(t *testing.T) {
	prompts, err := LoadAllPrompts()
	if err != nil {
		t.Fatalf("LoadAllPrompts: %v", err)
	}
	for _, r := range AllRoles {
		body, ok := prompts[r]
		if !ok {
			t.Errorf("role %q missing from loaded prompts", r)
			continue
		}
		if strings.TrimSpace(body) == "" {
			t.Errorf("role %q has empty body", r)
		}
	}
}

func TestLoadAllPrompts_WithinBudget(t *testing.T) {
	prompts, err := LoadAllPrompts()
	if err != nil {
		t.Fatalf("LoadAllPrompts: %v", err)
	}
	total := 0
	for r, body := range prompts {
		// Cheap token estimate: chars / 1.5 per the spec.
		est := len([]rune(body)) * 2 / 3
		if est > tokenBudget {
			t.Errorf("prompt %q is %d token chars (budget %d); trim it", r, est, tokenBudget)
		}
		total += est
	}
	// Spec: 5 prompts total < 10000 token chars.
	if total >= 10000 {
		t.Errorf("5 prompts total %d token chars, want < 10000", total)
	}
}

func TestSwitchTo_ContainsRoleBody(t *testing.T) {
	llm := &fakeLLM{resp: "ok"}
	sw, err := NewSwitcher(llm, "BASE: project=测试, genre=xianxia")
	if err != nil {
		t.Fatalf("NewSwitcher: %v", err)
	}

	for _, r := range AllRoles {
		got := sw.SwitchTo(r)
		if !strings.HasPrefix(got, "BASE:") {
			t.Errorf("SwitchTo(%q) = %q, missing base prefix", r, got[:min(40, len(got))])
		}
		// Each prompt starts with "# 角色：<Name>" — a stable
		// signature for the role-specific body.
		marker := "# 角色："
		if !strings.Contains(got, marker) {
			t.Errorf("SwitchTo(%q) is missing role body marker %q", r, marker)
		}
	}
}

func TestSwitchTo_EmptyBase(t *testing.T) {
	llm := &fakeLLM{resp: "ok"}
	sw, err := NewSwitcher(llm, "")
	if err != nil {
		t.Fatalf("NewSwitcher: %v", err)
	}
	got := sw.SwitchTo(RolePlotWriter)
	if !strings.HasPrefix(got, "# 角色：") {
		t.Errorf("SwitchTo with empty base = %q, expected raw role body", got[:min(40, len(got))])
	}
}

func TestSwitcher_Call_PassesComposedPrompt(t *testing.T) {
	llm := &fakeLLM{resp: `{"ok": true}`}
	sw, err := NewSwitcher(llm, "BASE_PREFIX")
	if err != nil {
		t.Fatalf("NewSwitcher: %v", err)
	}
	out, err := sw.Call(context.Background(), RoleReviewer, "评审本章")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if out != `{"ok": true}` {
		t.Errorf("Call returned %q, want fake LLM resp", out)
	}
	if !strings.HasPrefix(llm.lastSystem, "BASE_PREFIX\n\n") {
		t.Errorf("system prompt = %q, expected base prefix then role body", llm.lastSystem[:min(60, len(llm.lastSystem))])
	}
	if !strings.Contains(llm.lastSystem, "# 角色：") {
		t.Errorf("system prompt missing role body marker")
	}
	if llm.lastUser != "评审本章" {
		t.Errorf("user prompt = %q, want verbatim pass-through", llm.lastUser)
	}
}

func TestSwitcher_Call_NilLLM(t *testing.T) {
	sw := &Switcher{llm: nil, suffix: map[Role]string{RolePlotWriter: "x"}}
	if _, err := sw.Call(context.Background(), RolePlotWriter, "u"); err == nil {
		t.Fatal("Call with nil LLM should error")
	}
}

func TestSetBase_UpdatesComposition(t *testing.T) {
	llm := &fakeLLM{resp: ""}
	sw, err := NewSwitcher(llm, "OLD_BASE")
	if err != nil {
		t.Fatalf("NewSwitcher: %v", err)
	}
	sw.SetBase("NEW_BASE")
	got := sw.SwitchTo(RolePlotPlanner)
	if !strings.HasPrefix(got, "NEW_BASE\n\n") {
		t.Errorf("SwitchTo after SetBase = %q, expected new base", got[:min(40, len(got))])
	}
}

func TestPromptFile_KnownRoles(t *testing.T) {
	for r, path := range promptFile {
		if !strings.HasPrefix(path, "prompts/") {
			t.Errorf("prompt file for %q = %q, expected prompts/<role>.md", r, path)
		}
		if !strings.HasSuffix(path, ".md") {
			t.Errorf("prompt file for %q = %q, expected .md extension", r, path)
		}
	}
	// Spec-required roles must be present.
	for _, r := range []Role{RoleWorldBuilder, RoleArcMaster, RolePlotPlanner, RolePlotWriter, RoleReviewer} {
		if _, ok := promptFile[r]; !ok {
			t.Errorf("prompt file missing for required role %q", r)
		}
	}
}

// lints the type assertion to keep the LLMCaller interface live in
// this file. The interface is the public contract Switcher consumes;
// referencing it here documents the dependency for anyone refactoring.
var _ LLMCaller = (*fakeLLM)(nil)

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
