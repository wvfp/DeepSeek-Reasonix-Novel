package tools

import (
	"regexp"
	"strings"

	"reasonix/internal/novel/domain"
	"reasonix/internal/novel/style"
)

// voiceQuoteRe matches Chinese-style dialog spans (「…」, "…",
// 『…』) that the voice extractor slices per speaker. We use a
// permissive match so partial dialog at the end of a paragraph
// (e.g. missing close quote due to LLM truncation) still
// contributes at least one sentence.
var voiceQuoteRe = regexp.MustCompile(`[「『"][^」』"\n]{2,}[」』"]?`)

// characterMentionRe is a quick gate: we only run voice drift for
// characters whose name appears at least once in the chapter.
// The check is intentionally lax (substring on the chapter body)
// because the LLM is unlikely to use a character's exact ID
// string in dialog.
func characterMentioned(name, body string) bool {
	return name != "" && strings.Contains(body, name)
}

// extractDialogForCharacter returns the joined dialog text for
// every quote span in body. The current implementation does not
// attribute lines to specific speakers — the caller passes the
// whole chapter's dialog when the character is mentioned. A more
// sophisticated pass could split lines by speaker tags, but the
// "diff against persisted fingerprint" use case is robust to
// whole-chapter dialog because it averages over all lines.
func extractDialogForCharacter(_ *domain.Character, body string) string {
	matches := voiceQuoteRe.FindAllString(body, -1)
	if len(matches) == 0 {
		return ""
	}
	return strings.Join(matches, "\n")
}

// runVoiceDrift returns one VoiceDriftFinding per character whose
// dialog in this chapter drifts from the persisted fingerprint.
// An empty character list (no characters, or no mentions) returns
// nil so the caller can branch on "no signal" cheaply.
//
// The function is allocation-light: it iterates each character
// once, runs the cheap regex match, and short-circuits when the
// profile is too minimal to drive a comparison.
func runVoiceDrift(chars []*domain.Character, body string) []VoiceDriftFinding {
	var out []VoiceDriftFinding
	for _, c := range chars {
		if c == nil || c.VoiceProfile == nil {
			continue
		}
		if !characterMentioned(c.Name, body) {
			continue
		}
		dialog := extractDialogForCharacter(c, body)
		if strings.TrimSpace(dialog) == "" {
			continue
		}
		if style.IsMinimal(*c.VoiceProfile) {
			// First chapter for this character — no
			// baseline, so don't flag drift.
			continue
		}
		fresh := style.ExtractVoice(dialog)
		deviations := style.CompareVoice(fresh, *c.VoiceProfile, dialog)
		if len(deviations) == 0 {
			continue
		}
		out = append(out, VoiceDriftFinding{
			CharacterID:   c.ID,
			CharacterName: c.Name,
			Deviations:    deviations,
		})
	}
	return out
}

// VoiceDriftFinding is one voice-drift report the chapter_review
// tool attaches to its output. The shape mirrors what the
// editor UI uses to render an inline "voice drift" badge in
// the dialog column.
type VoiceDriftFinding struct {
	CharacterID   string                  `json:"character_id"`
	CharacterName string                  `json:"character_name"`
	Deviations    []style.VoiceDeviation  `json:"deviations"`
}
