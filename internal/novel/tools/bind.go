package tools

import (
	"fmt"

	"reasonix/internal/novel/roles"
)

// BindSwitcher registers LLM-dependent tools (chapter_review,
// review_fix, consistency) into reg, each bound to sw. The default
// catalog seeds these with a nil Switcher so the registry can be
// constructed without one; CLI commands that talk to a real LLM
// call this helper to swap in a live Switcher.
//
// Existing registrations are replaced. The intent is that a CLI
// subcommand does:
//
//	caller, _ := tools.InstallProviderLLM(...)
//	sw, _ := roles.NewSwitcher(caller, "")
//	tools.BindSwitcher(reg, sw)
//
// right after `reg := tools.NewRegistry()`.
func BindSwitcher(reg *Registry, sw *roles.Switcher) error {
	if reg == nil {
		return fmt.Errorf("tools.BindSwitcher: nil registry")
	}
	if sw == nil {
		return fmt.Errorf("tools.BindSwitcher: nil switcher")
	}
	reg.Register(NewChapterReviewTool(sw))
	reg.Register(NewReviewFixTool(sw))
	reg.Register(NewConsistencyTool(sw))
	return nil
}
