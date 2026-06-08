package tools

import "reasonix/internal/novel/style"

// applyAntiAIFix is the chapter_write post-processor. It strips
// every banned phrase from the LLM output. Wrapper around
// style.ApplyFix so the tool can be re-pointed to a different
// implementation in tests without rewriting the call site.
func applyAntiAIFix(text string) string {
	return style.ApplyFix(text)
}

// detectAntiAIPatterns returns the matches that the scrubber
// would have stripped. We re-scan the pre-fix text (not the
// fixed one) so the count reflects the LLM's raw output.
func detectAntiAIPatterns(text string) []style.AntiAIMatch {
	return style.DetectPatterns(text)
}

// countAntiAISeverity tallies matches per severity bucket. The
// chapter_write output surfaces the map so the editor UI can
// render an "AI slop" badge without re-scanning.
func countAntiAISeverity(matches []style.AntiAIMatch) map[string]int {
	return style.CountBySeverity(matches)
}
