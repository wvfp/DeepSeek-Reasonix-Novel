package style

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"reasonix/internal/novel/config"
)

// StyleAnchorProfile is the style fingerprint extracted from the
// last 3-5 chapters of a project. The same shape is written to
// .novel-weaver/style-anchors/anchor-profile.json so the editor
// UI can render a "style radar" without re-scanning chapters on
// every page load.
//
// Field semantics mirror the novel-plugin reference project —
// see src/modules/style-anchor/analyzer.ts for the upstream.
type StyleAnchorProfile struct {
	// Sentence length buckets: <10, 10-20, 20-30, 30-50, >50 chars.
	SentenceLengthDist []int `json:"sentenceLengthDist"`
	// Paragraph length buckets: <50, 50-100, 100-200, 200-500, >500.
	ParagraphLengthDist []int `json:"paragraphLengthDist"`
	// Dialogue ratio in [0, 1]. Computed as the share of text
	// inside Chinese / English quote pairs.
	DialogueRatio float64 `json:"dialogueRatio"`
	// Top 50 Chinese bigrams, sorted by frequency descending.
	TopBigrams [][2]any `json:"topBigrams"`
	// Punctuation usage frequency (e.g. "。": 412, "，": 803).
	PunctuationFreq map[string]int `json:"punctuationFreq"`
	// Rhetoric counts (metaphor, parallel, hyperbole).
	Rhetoric RhetoricProfile `json:"rhetoric,omitempty"`
	// Emotion counts (positive, negative, neutral).
	Emotion EmotionProfile `json:"emotion,omitempty"`
	// Perspective stats (dominant, stability).
	Perspective PerspectiveProfile `json:"perspective,omitempty"`
	// Path to the manual override file when present.
	ManualAnchor string `json:"manualAnchor,omitempty"`
}

// AnchorFileName is the JSON file written by SaveProfile and read
// back by LoadProfile. Kept as a constant so tests can refer to it
// without duplicating the string.
const AnchorFileName = "anchor-profile.json"

// AnchorDirName is the directory under .novel-weaver/ that holds
// the anchor profile (and any future anchor-related artefacts).
const AnchorDirName = "style-anchors"

// ManualAnchorFileName is the optional YAML-frontmatter override
// file a user can drop in to force specific anchor values.
const ManualAnchorFileName = "manual-anchor.md"

// PunctuationChars is the canonical punctuation set the analyser
// tracks. Mirrors the upstream novel-plugin constant.
const PunctuationChars = "，。！？、：；\"\"「」——……～·"

// sentenceBuckets / paragraphBuckets are the bucket boundaries the
// histogram bins into. They are package-level constants so the
// manual-override parser uses the same widths.
var (
	sentenceBuckets   = []int{10, 20, 30, 50}
	paragraphBuckets  = []int{50, 100, 200, 500}
	frontmatterRegex  = regexp.MustCompile(`^---[\s\S]*?---\s*`)
	sentenceSplitRe   = regexp.MustCompile(`[。！?!？\n]`)
	paragraphSplitRe  = regexp.MustCompile(`\n\s*\n`)
	quoteRe           = regexp.MustCompile(`[「『""][^「『""]*[」』""]`)
	whitespaceRe      = regexp.MustCompile(`\s+`)
	manualSentenceRe  = regexp.MustCompile(`(?m)sentenceLengthDist:\s*\[([0-9,\s]+)\]`)
	manualParagraphRe = regexp.MustCompile(`(?m)paragraphLengthDist:\s*\[([0-9,\s]+)\]`)
	manualDialogueRe  = regexp.MustCompile(`(?m)dialogueRatio:\s*([0-9.]+)`)
	manualRhetoricRe  = regexp.MustCompile(`(?m)rhetoric:\s*\{\s*metaphor:\s*(\d+),\s*parallel:\s*(\d+),\s*hyperbole:\s*(\d+)\s*\}`)
	manualEmotionRe   = regexp.MustCompile(`(?m)emotion:\s*\{\s*positive:\s*(\d+),\s*negative:\s*(\d+),\s*neutral:\s*(\d+)\s*\}`)
	manualPerspectiveRe = regexp.MustCompile(`(?m)perspective:\s*\{\s*dominant:\s*(\w+),\s*stability:\s*([0-9.]+)\s*\}`)
)

// Extract reads the last N chapter Markdown files from a project
// root, builds a StyleAnchorProfile, and writes it to
// .novel-weaver/style-anchors/anchor-profile.json. Manual overrides
// in manual-anchor.md (YAML frontmatter) take precedence over the
// auto-extracted values.
//
// The dimension list and chapter count are read from
// .novel-weaver/config.json (style_anchor section). When the config
// is missing the defaults are used (all dimensions, last 5 chapters).
//
// Returns the final profile (post-override). On a project with no
// chapters the function writes an empty profile and returns it; the
// chapter_write flow uses that as "no anchor" rather than failing.
func Extract(projectRoot string) (StyleAnchorProfile, error) {
	cfg, _ := config.Load(projectRoot)
	profile := scanChaptersWithConfig(projectRoot, cfg.StyleAnchor)
	if manual, ok := readManualAnchor(projectRoot); ok {
		// Manual values win where the user set them. The
		// override file is opt-in per field — unset fields fall
		// through to the auto-extracted values.
		if len(manual.SentenceLengthDist) > 0 {
			profile.SentenceLengthDist = manual.SentenceLengthDist
		}
		if len(manual.ParagraphLengthDist) > 0 {
			profile.ParagraphLengthDist = manual.ParagraphLengthDist
		}
		if manual.DialogueRatio > 0 {
			profile.DialogueRatio = manual.DialogueRatio
		}
		if manual.Rhetoric.Metaphor > 0 || manual.Rhetoric.Parallel > 0 || manual.Rhetoric.Hyperbole > 0 {
			profile.Rhetoric = manual.Rhetoric
		}
		if manual.Emotion.Positive > 0 || manual.Emotion.Negative > 0 || manual.Emotion.Neutral > 0 {
			profile.Emotion = manual.Emotion
		}
		if manual.Perspective.Dominant != "" {
			profile.Perspective = manual.Perspective
		}
		profile.ManualAnchor = filepath.Join(projectRoot, ".novel-weaver", AnchorDirName, ManualAnchorFileName)
	}
	if err := SaveProfile(projectRoot, profile); err != nil {
		return profile, err
	}
	return profile, nil
}

// LoadProfile returns the saved profile for projectRoot, or an
// empty profile if no anchor file has been written yet. Used by
// the chapter_write flow to skip the scan when an anchor is
// already cached on disk.
func LoadProfile(projectRoot string) (StyleAnchorProfile, error) {
	path := filepath.Join(projectRoot, ".novel-weaver", AnchorDirName, AnchorFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return StyleAnchorProfile{}, nil
		}
		return StyleAnchorProfile{}, fmt.Errorf("style: read anchor: %w", err)
	}
	var p StyleAnchorProfile
	if err := json.Unmarshal(data, &p); err != nil {
		return StyleAnchorProfile{}, fmt.Errorf("style: parse anchor: %w", err)
	}
	return p, nil
}

// SaveProfile writes the profile to .novel-weaver/style-anchors/.
// The directory is created if missing. The function returns the
// absolute path of the written file for the caller's logs.
func SaveProfile(projectRoot string, p StyleAnchorProfile) error {
	dir := filepath.Join(projectRoot, ".novel-weaver", AnchorDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("style: mkdir anchor dir: %w", err)
	}
	out := filepath.Join(dir, AnchorFileName)
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("style: marshal anchor: %w", err)
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		return fmt.Errorf("style: write anchor: %w", err)
	}
	return nil
}

// FormatSystemBlock renders a profile as the <<STYLE_ANCHOR>> block
// the chapter_write system prompt embeds. The block is short by
// design — the LLM only needs the dist + ratio + a sample of
// bigrams to steer its prose. The full top-50 bigram list is
// omitted to keep the prompt cacheable.
func FormatSystemBlock(p StyleAnchorProfile) string {
	if len(p.SentenceLengthDist) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n[STYLE_ANCHOR]\n")
	fmt.Fprintf(&b, "sentence_length_dist (chars: <10, 10-20, 20-30, 30-50, >50): %v\n",
		p.SentenceLengthDist)
	fmt.Fprintf(&b, "paragraph_length_dist (chars: <50, 50-100, 100-200, 200-500, >500): %v\n",
		p.ParagraphLengthDist)
	fmt.Fprintf(&b, "dialogue_ratio: %.2f\n", p.DialogueRatio)
	// Top 10 bigrams are the signal the LLM needs; the rest is
	// noise at the model side.
	if n := len(p.TopBigrams); n > 0 {
		fmt.Fprintf(&b, "top_bigrams: %d entries (top 10 shown)\n", n)
		for i, bg := range p.TopBigrams {
			if i >= 10 {
				break
			}
			word, _ := bg[0].(string)
			count, _ := bg[1].(float64)
			fmt.Fprintf(&b, "  - %s × %d\n", word, int(count))
		}
	}
	if len(p.PunctuationFreq) > 0 {
		b.WriteString("punctuation_freq: ")
		keys := make([]string, 0, len(p.PunctuationFreq))
		for k := range p.PunctuationFreq {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%s=%d", k, p.PunctuationFreq[k])
		}
		b.WriteString("\n")
	}
	if p.Rhetoric.Metaphor > 0 || p.Rhetoric.Parallel > 0 || p.Rhetoric.Hyperbole > 0 {
		fmt.Fprintf(&b, "rhetoric (metaphor=%d, parallel=%d, hyperbole=%d)\n",
			p.Rhetoric.Metaphor, p.Rhetoric.Parallel, p.Rhetoric.Hyperbole)
	}
	if p.Emotion.Positive > 0 || p.Emotion.Negative > 0 || p.Emotion.Neutral > 0 {
		total := p.Emotion.Positive + p.Emotion.Negative + p.Emotion.Neutral
		fmt.Fprintf(&b, "emotion (positive=%d, negative=%d, neutral=%d, total=%d)\n",
			p.Emotion.Positive, p.Emotion.Negative, p.Emotion.Neutral, total)
	}
	if p.Perspective.Dominant != "" {
		fmt.Fprintf(&b, "perspective (dominant=%s, stability=%.2f, first=%d, third=%d)\n",
			p.Perspective.Dominant, p.Perspective.Stability, p.Perspective.FirstPerson, p.Perspective.ThirdPerson)
	}
	b.WriteString("[/STYLE_ANCHOR]\n")
	return b.String()
}

// ---------------------------------------------------------------------------
// Chapter scanning (configurable)
// ---------------------------------------------------------------------------

// scanChaptersWithConfig walks the chapters directory and aggregates
// the last N Markdown files into one combined body, then runs the
// active dimensions through the AnchorDimension interface.
func scanChaptersWithConfig(projectRoot string, sac config.StyleAnchorConfig) StyleAnchorProfile {
	profile := StyleAnchorProfile{
		SentenceLengthDist:  []int{0, 0, 0, 0, 0},
		ParagraphLengthDist: []int{0, 0, 0, 0, 0},
		TopBigrams:          [][2]any{},
		PunctuationFreq:     map[string]int{},
	}

	chaptersDir := filepath.Join(projectRoot, ".novel-weaver", "content", "chapters")
	entries, err := os.ReadDir(chaptersDir)
	if err != nil {
		return profile
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		volDir := filepath.Join(chaptersDir, e.Name())
		chs, err := os.ReadDir(volDir)
		if err != nil {
			continue
		}
		for _, c := range chs {
			if c.IsDir() || !strings.HasSuffix(c.Name(), ".md") {
				continue
			}
			files = append(files, filepath.Join(volDir, c.Name()))
		}
	}
	sort.Strings(files)

	count := sac.ChapterCount
	if count <= 0 {
		count = 5
	}
	if len(files) > count {
		files = files[len(files)-count:]
	}

	var chapters []string
	for _, p := range files {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		body := frontmatterRegex.ReplaceAllString(string(raw), "")
		chapters = append(chapters, body)
	}
	if len(chapters) == 0 {
		return profile
	}

	// Determine active dimensions.
	dims := selectDimensions(sac.Dimensions)

	// Run each dimension.
	for _, dim := range dims {
		val, err := dim.Extract(chapters)
		if err != nil {
			continue
		}
		switch d := dim.(type) {
		case SentenceLengthDimension:
			if v, ok := val.([]int); ok {
				profile.SentenceLengthDist = v
			}
			_ = d
		case ParagraphLengthDimension:
			if v, ok := val.([]int); ok {
				profile.ParagraphLengthDist = v
			}
			_ = d
		case DialogueRatioDimension:
			if v, ok := val.(float64); ok {
				profile.DialogueRatio = v
			}
			_ = d
		case BigramFrequencyDimension:
			if v, ok := val.([][2]any); ok {
				profile.TopBigrams = v
			}
			_ = d
		case PunctuationFrequencyDimension:
			if v, ok := val.(map[string]int); ok {
				profile.PunctuationFreq = v
			}
			_ = d
		case RhetoricDimension:
			if v, ok := val.(RhetoricProfile); ok {
				profile.Rhetoric = v
			}
			_ = d
		case EmotionDimension:
			if v, ok := val.(EmotionProfile); ok {
				profile.Emotion = v
			}
			_ = d
		case PerspectiveDimension:
			if v, ok := val.(PerspectiveProfile); ok {
				profile.Perspective = v
			}
			_ = d
		}
	}

	return profile
}

// selectDimensions filters the full dimension list by name. An empty
// or nil slice means "all dimensions".
func selectDimensions(names []string) []AnchorDimension {
	all := DefaultDimensions()
	if len(names) == 0 {
		return all
	}
	set := map[string]bool{}
	for _, n := range names {
		set[n] = true
	}
	var out []AnchorDimension
	for _, d := range all {
		if set[d.Name()] {
			out = append(out, d)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Legacy helpers (kept package-private so tests and dimensions reuse them)
// ---------------------------------------------------------------------------

type lengthStats struct {
	sentenceLengths  []int
	paragraphLengths []int
	dialogueCount    int
	totalChars       int
}

func extractLengthStats(content string) lengthStats {
	var out lengthStats
	paragraphs := paragraphSplitRe.Split(content, -1)
	for _, p := range paragraphs {
		if strings.TrimSpace(p) == "" {
			continue
		}
		out.paragraphLengths = append(out.paragraphLengths, len(whitespaceRe.ReplaceAllString(p, "")))
	}
	// Sentences split on Chinese + English sentence terminators.
	sentences := sentenceSplitRe.Split(content, -1)
	for _, s := range sentences {
		if strings.TrimSpace(s) == "" {
			continue
		}
		out.sentenceLengths = append(out.sentenceLengths, len(whitespaceRe.ReplaceAllString(s, "")))
	}
	out.totalChars = len(whitespaceRe.ReplaceAllString(content, ""))
	matches := quoteRe.FindAllString(content, -1)
	for _, m := range matches {
		out.dialogueCount += len(whitespaceRe.ReplaceAllString(m, ""))
	}
	return out
}

func buildDistribution(values []int, buckets []int) []int {
	dist := make([]int, len(buckets)+1)
	for _, v := range values {
		placed := false
		for i, b := range buckets {
			if v <= b {
				dist[i]++
				placed = true
				break
			}
		}
		if !placed {
			dist[len(buckets)]++
		}
	}
	return dist
}

func extractBigrams(content string) [][2]any {
	// Keep CJK only. Iterate as runes so we never slice into the
	// middle of a multi-byte UTF-8 sequence — that would produce
	// invalid bigram strings and skew the frequency table.
	var rs []rune
	for _, r := range content {
		if r >= 0x4E00 && r <= 0x9FFF {
			rs = append(rs, r)
		}
	}
	if len(rs) < 2 {
		return [][2]any{}
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
	if len(pairs) > 50 {
		pairs = pairs[:50]
	}
	out := make([][2]any, len(pairs))
	for i, p := range pairs {
		out[i] = [2]any{p.k, p.v}
	}
	return out
}

func extractPunctuation(content string) map[string]int {
	out := map[string]int{}
	for _, r := range PunctuationChars {
		count := strings.Count(content, string(r))
		if count > 0 {
			out[string(r)] = count
		}
	}
	return out
}

// readManualAnchor parses the YAML frontmatter of
// .novel-weaver/style-anchors/manual-anchor.md. The parser is
// intentionally minimal — full YAML would balloon the binary for
// the marginal benefit of a config file most authors will not touch.
func readManualAnchor(projectRoot string) (StyleAnchorProfile, bool) {
	path := filepath.Join(projectRoot, ".novel-weaver", AnchorDirName, ManualAnchorFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return StyleAnchorProfile{}, false
	}
	body := string(data)
	if !strings.HasPrefix(body, "---") {
		return StyleAnchorProfile{}, false
	}
	end := strings.Index(body[3:], "---")
	if end < 0 {
		return StyleAnchorProfile{}, false
	}
	yaml := body[3 : 3+end]
	var p StyleAnchorProfile
	if m := manualSentenceRe.FindStringSubmatch(yaml); m != nil {
		p.SentenceLengthDist = parseIntList(m[1])
	}
	if m := manualParagraphRe.FindStringSubmatch(yaml); m != nil {
		p.ParagraphLengthDist = parseIntList(m[1])
	}
	if m := manualDialogueRe.FindStringSubmatch(yaml); m != nil {
		var v float64
		if _, err := fmt.Sscanf(m[1], "%f", &v); err == nil {
			p.DialogueRatio = v
		}
	}
	if m := manualRhetoricRe.FindStringSubmatch(yaml); m != nil {
		p.Rhetoric.Metaphor = parseInt(m[1])
		p.Rhetoric.Parallel = parseInt(m[2])
		p.Rhetoric.Hyperbole = parseInt(m[3])
	}
	if m := manualEmotionRe.FindStringSubmatch(yaml); m != nil {
		p.Emotion.Positive = parseInt(m[1])
		p.Emotion.Negative = parseInt(m[2])
		p.Emotion.Neutral = parseInt(m[3])
	}
	if m := manualPerspectiveRe.FindStringSubmatch(yaml); m != nil {
		p.Perspective.Dominant = strings.TrimSpace(m[1])
		var v float64
		if _, err := fmt.Sscanf(m[2], "%f", &v); err == nil {
			p.Perspective.Stability = v
		}
	}
	return p, true
}

func parseIntList(s string) []int {
	parts := strings.Split(s, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		var v int
		if _, err := fmt.Sscanf(p, "%d", &v); err == nil {
			out = append(out, v)
		}
	}
	return out
}

func parseInt(s string) int {
	var v int
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &v); err == nil {
		return v
	}
	return 0
}
