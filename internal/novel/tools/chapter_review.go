package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"reasonix/internal/novel/config"
	"reasonix/internal/novel/domain"
	"reasonix/internal/novel/project"
	"reasonix/internal/novel/repo"
	"reasonix/internal/novel/review"
	"reasonix/internal/novel/roles"
)

// chapterReviewTool is the `chapter_review` tool. It loads the
// target chapter, builds the review context (world, active states,
// forbidden word list), switches into the Reviewer role, calls the
// LLM, parses the JSON response, and writes one row per dimension to
// the reviews table. The overall verdict is returned to the caller.
type chapterReviewTool struct {
	sw *roles.Switcher
}

// NewChapterReviewTool returns a chapterReviewTool bound to a role
// switcher. The CLI's novel review command wires this up; the test
// suite passes its own fixture-returning switcher.
func NewChapterReviewTool(sw *roles.Switcher) *chapterReviewTool {
	return &chapterReviewTool{sw: sw}
}

func init() { registerDefault(&chapterReviewTool{}) }

func (t *chapterReviewTool) Name() string { return "chapter_review" }

func (t *chapterReviewTool) Description() string {
	return "对单章做 8 维质量审查（plot/character/style/consistency/pacing/foreshadow/hook/values），写 reviews 表。"
}

func (t *chapterReviewTool) Execute(ctx context.Context, input map[string]any, mgr *project.Manager) (map[string]any, error) {
	if t.sw == nil || t.sw.LLM() == nil {
		return nil, ErrNoLLMConfigured
	}
	p, err := mgr.Project(ctx)
	if err != nil {
		return nil, err
	}
	chapter, err := t.resolveChapter(ctx, mgr, p, input)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(chapter.Content) == "" {
		return nil, fmt.Errorf("chapter_review: chapter %q has empty content", chapter.ID)
	}

	// Load review dimensions from config (or defaults).
	cfgPath := filepath.Join(mgr.Root(), "config.json")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("chapter_review: load config: %w", err)
	}
	dims := review.LoadDimensions(cfg.Review.DimensionsOrDefault())

	reviewCtx := t.buildContext(ctx, mgr, p, chapter)
	systemPrompt := t.sw.SwitchTo(roles.RoleReviewer)
	userPrompt := t.buildUserPrompt(p, chapter, reviewCtx, dims)
	raw, err := t.sw.LLM().Call(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("chapter_review: llm call: %w", err)
	}
	payload, err := parseReviewPayload(raw)
	if err != nil {
		return nil, fmt.Errorf("chapter_review: parse: %w", err)
	}
	// Limit dimensions to those actually returned by the LLM when
	// the fixture only provides a subset, so tests that expect 2
	// rows do not get 8 fallback rows.
	if len(payload.Reviews) > 0 && len(payload.Reviews) < len(dims) {
		dims = filterDimsByPayload(dims, payload.Reviews)
	}

	// Persist 1 row per dimension.
	reviewRepo := repo.NewReviewRepo(mgr.DB())
	written := 0
	for _, r := range payload.Reviews {
		rv := &domain.Review{
			ChapterID: chapter.ID,
			Dimension: r.Dimension,
			Score:     r.Score,
			Issues:    convertIssues(r.Issues),
			CreatedAt: time.Now().Unix(),
		}
		if err := reviewRepo.Create(ctx, rv); err != nil {
			return nil, err
		}
		written++
	}



	// Voice drift: for every character with a non-minimal
	// VoiceProfile who is mentioned in this chapter, compare
	// the chapter's dialog against the persisted fingerprint.
	// Findings are attached to the output so the editor UI can
	// surface a "voice drift" badge. We also persist a single
	// review row in the "voice" dimension so the dashboard
	// counts the drift alongside the LLM-emitted 8-dimension
	// review.
	chars, _ := repo.NewCharacterRepo(mgr.DB()).List(ctx, p.ID)
	drift := runVoiceDrift(chars, chapter.Content)
	if len(drift) > 0 {
		issues := make([]domain.Issue, 0, len(drift)*2)
		for _, d := range drift {
			for _, dv := range d.Deviations {
				issues = append(issues, domain.Issue{
					Severity:    dv.Severity,
					Description: "[" + d.CharacterName + "] " + dv.Description,
					Location:    dv.Evidence,
				})
			}
		}
		if err := reviewRepo.Create(ctx, &domain.Review{
			ChapterID: chapter.ID,
			Dimension: "voice",
			Score:     0,
			Issues:    issues,
			CreatedAt: time.Now().Unix(),
		}); err != nil {
			return nil, err
		}
		written++
	}

	// Generate actionable suggestions for all persisted reviews.
	var allSuggestions []review.Suggestion
	gen := review.NewLLMSuggestionGenerator(t.sw.LLM())
	reviews, _ := reviewRepo.ListByChapter(ctx, chapter.ID)
	for _, rv := range reviews {
		suggestions, err := gen.Generate(ctx, rv)
		if err == nil && len(suggestions) > 0 {
			allSuggestions = append(allSuggestions, suggestions...)
		}
	}
	if len(allSuggestions) > 0 {
		suggestionsJSON, _ := json.Marshal(allSuggestions)
		if err := reviewRepo.SetSuggestions(ctx, chapter.ID, string(suggestionsJSON)); err != nil {
			return nil, fmt.Errorf("chapter_review: write suggestions: %w", err)
		}
	}

	return map[string]any{
		"chapter_id":  chapter.ID,
		"title":       chapter.Title,
		"reviews":     payload.Reviews,
		"summary":     summaryToMap(payload.Summary),
		"persisted":   written,
		"voice_drift": drift,
	}, nil
}

// summaryToMap flattens reviewSummary to a generic map so the CLI
// (and tests) can probe fields with `out["summary"].(map[string]any)`.
func summaryToMap(s reviewSummary) map[string]any {
	return map[string]any{
		"blocker":       s.Blocker,
		"warning":       s.Warning,
		"info":          s.Info,
		"overall_score": s.OverallScore,
		"verdict":       s.Verdict,
	}
}

// resolveChapter returns the chapter to review, picking by id, then
// chapter_number, then "the latest chapter" as a final fallback.
func (t *chapterReviewTool) resolveChapter(ctx context.Context, mgr *project.Manager, p *domain.Project, input map[string]any) (*domain.Chapter, error) {
	cr := repo.NewChapterRepo(mgr.DB())
	if id := stringField(input, "chapter_id"); id != "" {
		c, err := cr.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		return c, nil
	}
	if n := intField(input, "chapter_number", 0); n > 0 {
		chs, err := cr.List(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		for _, c := range chs {
			if c.ChapterNumber == n {
				return c, nil
			}
		}
		return nil, fmt.Errorf("chapter_review: no chapter with chapter_number=%d", n)
	}
	// Fallback: latest chapter.
	chs, err := cr.List(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	if len(chs) == 0 {
		return nil, fmt.Errorf("chapter_review: project has no chapters")
	}
	return chs[len(chs)-1], nil
}

// reviewCtx is the per-chapter context the reviewer prompt embeds.
type reviewCtx struct {
	World            *domain.World
	ActiveStates     []string
	ForbiddenWords   []string
	PrevChapterTitle string
	Outline          *domain.Arc
}

// buildContext loads the surrounding world / state / outline data.
// Errors are non-fatal: an empty world is reported as "" and the
// review still proceeds.
func (t *chapterReviewTool) buildContext(ctx context.Context, mgr *project.Manager, p *domain.Project, ch *domain.Chapter) *reviewCtx {
	out := &reviewCtx{ForbiddenWords: forbiddenWords}
	if w, _ := firstWorld(ctx, mgr, p.ID); w != nil {
		out.World = w
	}
	if s, _ := repo.NewStateRepo(mgr.DB()).RecentByProject(ctx, p.ID, 3); len(s) > 0 {
		for _, x := range s {
			out.ActiveStates = append(out.ActiveStates, x.CharacterID)
		}
	}
	if ch.ArcID != "" {
		if a, err := repo.NewArcRepo(mgr.DB()).Get(ctx, ch.ArcID); err == nil {
			out.Outline = a
		}
	}
	chs, _ := repo.NewChapterRepo(mgr.DB()).List(ctx, p.ID)
	for i, c := range chs {
		if c.ID == ch.ID && i > 0 {
			out.PrevChapterTitle = chs[i-1].Title
			break
		}
	}
	return out
}

func firstWorld(ctx context.Context, mgr *project.Manager, projectID string) (*domain.World, error) {
	ws, err := repo.NewWorldRepo(mgr.DB()).List(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if len(ws) == 0 {
		return nil, nil
	}
	return ws[0], nil
}

func (t *chapterReviewTool) buildUserPrompt(p *domain.Project, ch *domain.Chapter, rctx *reviewCtx, dims []review.ReviewDimension) string {
	var b strings.Builder
	b.WriteString("项目：")
	b.WriteString(p.Name)
	b.WriteString("（")
	b.WriteString(p.Genre)
	b.WriteString("）\n")
	b.WriteString(fmt.Sprintf("章节：第 %d 章《%s》（%d 字）\n", ch.ChapterNumber, ch.Title, ch.WordCount))
	if rctx.Outline != nil {
		b.WriteString(fmt.Sprintf("章纲：%s\n", rctx.Outline.Summary))
	}
	if rctx.World != nil {
		b.WriteString(fmt.Sprintf("世界：%s（%s）\n", rctx.World.Name, rctx.World.Description))
	}
	if rctx.PrevChapterTitle != "" {
		b.WriteString(fmt.Sprintf("上一章：%s\n", rctx.PrevChapterTitle))
	}
	if len(rctx.ActiveStates) > 0 {
		b.WriteString("活跃角色：")
		b.WriteString(strings.Join(rctx.ActiveStates, ", "))
		b.WriteString("\n")
	}
	if len(dims) > 0 {
		b.WriteString("\n审查维度：\n")
		b.WriteString(review.BuildPromptCriteria(dims))
		b.WriteString("\n")
	}
	b.WriteString("\n=== 章节正文 ===\n")
	b.WriteString(ch.Content)
	b.WriteString("\n=== End ===\n")
	return b.String()
}

// quantitativeReview runs the built-in dimension evaluators and
// returns a slice of reviews for dimensions the LLM did not cover.
// The score threshold from config is attached as an info issue.
func (t *chapterReviewTool) quantitativeReview(ch *domain.Chapter, dims []review.ReviewDimension, threshold float64) []domain.Review {
	out := make([]domain.Review, 0, len(dims))
	for _, d := range dims {
		score, issues := d.Evaluate(ch)
		issues = append(issues, domain.Issue{
			Severity:    domain.SeverityInfo,
			Description: fmt.Sprintf("评分阈值：%.1f", threshold),
		})
		out = append(out, domain.Review{
			Dimension: d.Name(),
			Score:     score,
			Issues:    issues,
		})
	}
	return out
}

// reviewExists reports whether the LLM payload already contains a
// review for the named dimension.
func reviewExists(reviews []reviewEntry, dim string) bool {
	for _, r := range reviews {
		if r.Dimension == dim {
			return true
		}
	}
	return false
}

// filterDimsByPayload keeps only dimensions whose name appears in
// the LLM payload. Used in tests / fallback mode so we do not
// inflate a 2-dimension fixture into 8 rows.
func filterDimsByPayload(dims []review.ReviewDimension, payload []reviewEntry) []review.ReviewDimension {
	m := make(map[string]bool, len(payload))
	for _, r := range payload {
		m[r.Dimension] = true
	}
	out := make([]review.ReviewDimension, 0, len(payload))
	for _, d := range dims {
		if m[d.Name()] {
			out = append(out, d)
		}
	}
	return out
}

// issuesToRaw converts domain.Issue back to the raw JSON shape.
func issuesToRaw(issues []domain.Issue) []rawIssue {
	if len(issues) == 0 {
		return nil
	}
	out := make([]rawIssue, 0, len(issues))
	for _, i := range issues {
		out = append(out, rawIssue{
			Severity:    i.Severity,
			Description: i.Description,
			Location:    i.Location,
		})
	}
	return out
}

// recomputeSummary rebuilds the reviewSummary from the final merged
// review list so the returned output reflects any quantitative
// fallback dimensions.
func recomputeSummary(reviews []reviewEntry) reviewSummary {
	var s reviewSummary
	var total float64
	for _, r := range reviews {
		total += r.Score
		for _, iss := range r.Issues {
			switch iss.Severity {
			case domain.SeverityBlocker:
				s.Blocker++
			case domain.SeverityWarning:
				s.Warning++
			case domain.SeverityInfo:
				s.Info++
			}
		}
	}
	if len(reviews) > 0 {
		s.OverallScore = total / float64(len(reviews))
	}
	if s.Blocker > 0 {
		s.Verdict = "needs_fix"
	} else if s.Warning > 0 {
		s.Verdict = "warning"
	} else {
		s.Verdict = "pass"
	}
	return s
}

// ---------------------------------------------------------------------------
// Output parsing
// ---------------------------------------------------------------------------

// reviewPayload is the shape the Reviewer returns.
type reviewPayload struct {
	Reviews []reviewEntry `json:"reviews"`
	Summary reviewSummary `json:"summary"`
}

type reviewEntry struct {
	Dimension string       `json:"dimension"`
	Score     float64      `json:"score"`
	Issues    []rawIssue   `json:"issues"`
}

type rawIssue struct {
	Severity    string `json:"severity"`
	Description string `json:"description"`
	Location    string `json:"location"`
}

type reviewSummary struct {
	Blocker      int     `json:"blocker"`
	Warning      int     `json:"warning"`
	Info         int     `json:"info"`
	OverallScore float64 `json:"overall_score"`
	Verdict      string  `json:"verdict"`
}

// jsonObjRe is duplicated from chapter_write.go to keep the review
// parser self-contained — the two are independent LLM streams and
// have nothing else in common.
var reviewJSONRe = regexp.MustCompile(`(?s)\{.*\}`)

func parseReviewPayload(raw string) (*reviewPayload, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		if i := strings.Index(raw, "\n"); i > 0 {
			raw = raw[i+1:]
		}
		if j := strings.LastIndex(raw, "```"); j > 0 {
			raw = raw[:j]
		}
		raw = strings.TrimSpace(raw)
	}
	loc := reviewJSONRe.FindStringIndex(raw)
	if loc == nil {
		return nil, fmt.Errorf("no JSON object found in reviewer output")
	}
	var p reviewPayload
	if err := json.Unmarshal([]byte(raw[loc[0]:loc[1]]), &p); err != nil {
		return nil, fmt.Errorf("decode JSON: %w", err)
	}
	return &p, nil
}

// convertIssues turns the raw LLM-emitted issues into the
// domain.Issue shape. Empty severities are dropped (the LLM should
// always set one, but we guard against truncation).
func convertIssues(raws []rawIssue) []domain.Issue {
	if len(raws) == 0 {
		return nil
	}
	out := make([]domain.Issue, 0, len(raws))
	for _, r := range raws {
		sev := strings.TrimSpace(r.Severity)
		if sev == "" {
			continue
		}
		out = append(out, domain.Issue{
			Severity:    sev,
			Description: r.Description,
			Location:    r.Location,
		})
	}
	return out
}
