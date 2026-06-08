package style

import (
	"encoding/json"
	"math"
	"regexp"
	"sort"
	"strings"

	"reasonix/internal/novel/domain"
)

// voiceSentenceRe splits a paragraph into sentences for the
// sentence-length analysis. The same set of terminators as
// anchor.go so the two analysers agree on what counts as a sentence.
var voiceSentenceRe = regexp.MustCompile(`[。！?!？\n]`)

// CJKRange is the Unicode block the bigram counter keeps. The
// rest of the text (latin, punctuation, whitespace) is dropped so
// the fingerprint reflects only Chinese word choices.
func cjkOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ExtractVoice derives a VoiceProfile from the dialog text of a
// character. The dialog is the union of every quoted line in the
// chapters that mention the character's name. The caller is
// expected to have pre-filtered the text — this function does not
// try to attribute lines to specific speakers.
//
// The returned profile is best-effort: when the input is too short
// (< 5 sentences) the caller can still persist it as a "minimal"
// fingerprint, but deviation checks will be noisy. Use IsMinimal
// to detect that state.
func ExtractVoice(dialogText string) domain.VoiceProfile {
	prof := domain.VoiceProfile{}
	if strings.TrimSpace(dialogText) == "" {
		return prof
	}
	// Sentence length analysis.
	parts := voiceSentenceRe.Split(dialogText, -1)
	var lens []float64
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s == "" {
			continue
		}
		lens = append(lens, float64(strings.Count(s, "")-1))
	}
	if len(lens) > 0 {
		prof.AvgSentenceLen = mean(lens)
	}
	// Bigram frequency.
	prof.Bigrams = topBigrams(cjkOnly(dialogText), 20)
	// Speech style tag. Cutoffs are rough; the goal is to give the
	// downstream check a numeric-to-nominal mapping, not a
	// fine-grained classifier.
	prof.SpeechStyle = speechStyleTag(prof.AvgSentenceLen)
	// Profanity: count occurrences of a small seed list. Real
	// projects will add their own custom list later; the seed
	// covers the obvious cases.
	prof.ProfanityLevel = profanityLevel(dialogText)
	// Catchphrases: top 3 most common CJK bigrams are usually
	// phrases in disguise when the dialog is long enough.
	if len(prof.Bigrams) >= 3 {
		prof.Catchphrases = append([]string{}, prof.Bigrams[0], prof.Bigrams[1], prof.Bigrams[2])
	}
	return prof
}

// VoiceDeviation is one finding from CompareVoice. The shape
// matches what the chapter_review tool persists to the reviews
// table so the dashboard renders a single "voice drift" badge
// alongside other 8-dimension issues.
type VoiceDeviation struct {
	Kind        string  `json:"kind"`
	Severity    string  `json:"severity"`
	Description string  `json:"description"`
	Evidence    string  `json:"evidence"`
	Suggestion  string  `json:"suggestion"`
	Distance    float64 `json:"distance,omitempty"`
}

// CompareVoice diffs a freshly extracted VoiceProfile against the
// persisted one. It returns one VoiceDeviation per detected drift
// (sentence length, catchphrase absence, profanity level, etc.).
// The function is purely analytic — no LLM, no DB — so it is safe
// to run synchronously on every chapter write.
func CompareVoice(fresh, persisted domain.VoiceProfile, freshDialog string) []VoiceDeviation {
	if persisted.AvgSentenceLen == 0 && len(persisted.Catchphrases) == 0 {
		// Empty fingerprint → first chapter; nothing to diff.
		return nil
	}
	var out []VoiceDeviation
	// Sentence length drift. > 30 % delta is a "warning"; > 60 %
	// is a "high".
	if persisted.AvgSentenceLen > 0 {
		dev := fresh.AvgSentenceLen - persisted.AvgSentenceLen
		ratio := math.Abs(dev) / persisted.AvgSentenceLen
		if ratio > 0.3 {
			sev := "warning"
			if ratio > 0.6 {
				sev = "high"
			}
			out = append(out, VoiceDeviation{
				Kind:        "sentence_length",
				Severity:    sev,
				Description: "对白平均句长偏离声纹",
				Evidence:    snippet(freshDialog, 60),
				Suggestion:  "回到角色惯用句长区间",
				Distance:    ratio,
			})
		}
	}
	// Profanity drift. ±1 level is fine; ±2 is a "high".
	if fresh.ProfanityLevel-persisted.ProfanityLevel >= 2 {
		out = append(out, VoiceDeviation{
			Kind:        "profanity",
			Severity:    "high",
			Description: "脏话密度高于声纹",
			Evidence:    snippet(freshDialog, 60),
			Suggestion:  "删除或弱化粗口",
		})
	}
	// Catchphrase absence. The fingerprint declared top phrases;
	// this chapter uses none of them → remind the writer.
	if len(persisted.Catchphrases) > 0 {
		used := false
		for _, cp := range persisted.Catchphrases {
			if cp == "" {
				continue
			}
			if strings.Contains(freshDialog, cp) {
				used = true
				break
			}
		}
		if !used {
			out = append(out, VoiceDeviation{
				Kind:        "catchphrase_missing",
				Severity:    "info",
				Description: "本章未使用角色常用口头禅",
				Evidence:    "常用口头禅: " + strings.Join(persisted.Catchphrases, "、"),
				Suggestion:  "在 1-2 处对白中自然融入口头禅",
			})
		}
	}
	// Bigram overlap. A simple Jaccard distance over the top
	// bigrams catches dialect drift (e.g. 角色突然开始用「嗯」
	// 而不是惯用的「好的」).
	if len(persisted.Bigrams) > 0 && len(fresh.Bigrams) > 0 {
		j := jaccard(persisted.Bigrams, fresh.Bigrams, 10)
		if j < 0.2 {
			out = append(out, VoiceDeviation{
				Kind:        "bigram_drift",
				Severity:    "warning",
				Description: "对白常用词与声纹差异较大",
				Evidence:    "声纹 top10 vs 本章 top10 不重叠",
				Suggestion:  "重读角色前几章对白，回归惯用词",
				Distance:    1 - j,
			})
		}
	}
	return out
}

// IsMinimal reports whether a profile is too sparse to drive
// CompareVoice. The chapter_write flow uses it to decide whether
// to skip the drift check on the first chapter of a new
// character.
func IsMinimal(p domain.VoiceProfile) bool {
	return p.AvgSentenceLen == 0 && len(p.Catchphrases) == 0 && len(p.Bigrams) == 0
}

// EncodeProfile / DecodeProfile are the JSON helpers for storing
// the VoiceProfile in the characters.voice_profile column. They
// are tiny wrappers today (the struct is already JSON-tagged) but
// they give the writer a single import surface and let us swap
// to a more compact encoding (e.g. msgpack) without touching
// callers.
func EncodeProfile(p domain.VoiceProfile) (string, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func DecodeProfile(raw string) (domain.VoiceProfile, error) {
	var p domain.VoiceProfile
	if strings.TrimSpace(raw) == "" {
		return p, nil
	}
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return p, err
	}
	return p, nil
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

// topBigrams returns the top-N most common CJK bigrams in s, in
// descending frequency. Ties are broken lexicographically so the
// output is deterministic across runs. The input is iterated as
// runes (not bytes) so multi-byte UTF-8 characters split on
// their natural boundary.
func topBigrams(s string, n int) []string {
	rs := []rune(s)
	if len(rs) < 2 {
		return nil
	}
	freq := map[string]int{}
	for i := 0; i < len(rs)-1; i++ {
		freq[string(rs[i:i+2])]++
	}
	type kv struct {
		k string
		v int
	}
	pairs := make([]kv, 0, len(freq))
	for k, v := range freq {
		pairs = append(pairs, kv{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].v != pairs[j].v {
			return pairs[i].v > pairs[j].v
		}
		return pairs[i].k < pairs[j].k
	})
	if len(pairs) > n {
		pairs = pairs[:n]
	}
	out := make([]string, len(pairs))
	for i, p := range pairs {
		out[i] = p.k
	}
	return out
}

func speechStyleTag(avg float64) string {
	switch {
	case avg == 0:
		return ""
	case avg < 8:
		return "laconic"
	case avg < 20:
		return "casual"
	case avg < 35:
		return "formal"
	default:
		return "verbose"
	}
}

// profanityLevel returns 0..3 based on the density of seed
// profanity words. The seed list is hand-picked for the most
// common Chinese web-novel profanity; real projects can extend
// it later. The density (hits / total) is preferred over the
// raw count so a long chapter with a single instance does not
// register as "high".
func profanityLevel(s string) int {
	seeds := []string{"他妈的", "操你", "滚开", "去死"}
	hits := 0
	for _, w := range seeds {
		if strings.Contains(s, w) {
			hits++
		}
	}
	totalRunes := len([]rune(s))
	if totalRunes == 0 {
		return 0
	}
	density := float64(hits) / float64(totalRunes)
	switch {
	case hits == 0:
		return 0
	case hits == 1:
		return 1
	case density > 0.005 || hits >= 4:
		return 3
	default:
		return 2
	}
}

func jaccard(a, b []string, topN int) float64 {
	setA := map[string]bool{}
	setB := map[string]bool{}
	for i := 0; i < len(a) && i < topN; i++ {
		setA[a[i]] = true
	}
	for i := 0; i < len(b) && i < topN; i++ {
		setB[b[i]] = true
	}
	inter := 0
	for k := range setA {
		if setB[k] {
			inter++
		}
	}
	union := len(setA) + len(setB) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func snippet(s string, pad int) string {
	if len(s) <= pad*2 {
		return s
	}
	return s[:pad] + "…" + s[len(s)-pad:]
}
