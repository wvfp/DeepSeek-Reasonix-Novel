package tools

import (
	"path/filepath"

	"reasonix/internal/novel/project"
	"reasonix/internal/novel/style"
)

// loadStyleAnchor returns the project's saved style profile, or
// an empty profile if none has been written. Errors are swallowed
// — a missing or malformed anchor is a no-op rather than a
// chapter-write blocker, so the writer can still produce a
// chapter on a brand-new project.
func loadStyleAnchor(mgr *project.Manager) style.StyleAnchorProfile {
	if mgr == nil {
		return style.StyleAnchorProfile{}
	}
	// Root() returns the .novel-weaver/ path; the style.Extract
	// helper wants the project root, which is the parent dir.
	root := filepath.Dir(mgr.Root())
	prof, err := style.LoadProfile(root)
	if err != nil {
		return style.StyleAnchorProfile{}
	}
	return prof
}

// formatAnchorBlock is a tiny wrapper that returns the rendered
// <<STYLE_ANCHOR>> markers, or the empty string when the profile
// has no signal. Kept as a separate function so the chapter_write
// flow can swap in a stub for tests.
func formatAnchorBlock(p style.StyleAnchorProfile) string {
	if len(p.SentenceLengthDist) == 0 {
		return ""
	}
	return style.FormatSystemBlock(p)
}
