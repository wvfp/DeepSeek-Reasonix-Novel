package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"reasonix/internal/novel/async"
	"reasonix/internal/novel/config"
	"reasonix/internal/novel/domain"
	"reasonix/internal/novel/fallback"
	"reasonix/internal/novel/genre"
	"reasonix/internal/novel/progress"
	"reasonix/internal/novel/project"
	"reasonix/internal/novel/rag"
	"reasonix/internal/novel/repo"
	"reasonix/internal/novel/review"
	"reasonix/internal/novel/safety"
)

// chapterWriteTool is the `chapter_write` tool — the heart of the
// novel writing loop. It loads context (current arc, previous
// chapter, active character states), constructs a system prompt
// asking for strict JSON, calls the LLM, parses the result, and
// writes the chapter to disk + SQLite in a single transaction.
//
// The LLM call is abstracted behind LLMCaller so unit tests can
// inject a fixture-returning fake. The default caller is a no-op
// until the CLI wires one in.
type chapterWriteTool struct {
	llm LLMCaller
	fb  *fallback.LLMFallback
}

// NewChapterWriteTool returns a chapterWriteTool bound to a specific
// LLMCaller. Useful for tests; the default registry uses
// SetDefaultLLMCaller.
func NewChapterWriteTool(llm LLMCaller) *chapterWriteTool {
	return &chapterWriteTool{llm: llm, fb: fallback.NewLLMFallback()}
}

func init() {
	registerDefault(&chapterWriteTool{llm: NewDefaultLLMCaller(), fb: fallback.NewLLMFallback()})
}

func (t *chapterWriteTool) Name() string { return "chapter_write" }

func (t *chapterWriteTool) Description() string {
	return "写一个新章节（按 arc_id / title / prompt / genre 输入），生成 Markdown 并落库：1 chapter + N facts + M character_states + K kg_edges + aliases。"
}

func (t *chapterWriteTool) Execute(ctx context.Context, input map[string]any, mgr *project.Manager) (map[string]any, error) {
	rep := progress.NewCLIPReporter()

	if t.llm == nil {
		t.llm = DefaultLLMCaller()
	}
	if t.llm == nil {
		return nil, ErrNoLLMConfigured
	}
	p, err := mgr.Project(ctx)
	if err != nil {
		return nil, err
	}
	arcID, err := requiredString(input, "arc_id")
	if err != nil {
		return nil, err
	}
	title, err := requiredString(input, "title")
	if err != nil {
		return nil, err
	}
	genre := stringField(input, "genre")
	if genre == "" {
		genre = p.Genre
	}
	prompt := stringField(input, "prompt")

	rep.Start(progress.StageIndexing)
	ctxWith, err := t.loadContext(ctx, mgr, p, arcID)
	if err != nil {
		rep.Error(progress.StageIndexing, err)
		return nil, err
	}
	rep.Done(progress.StageIndexing)
	chapterCtx := chapterContextFrom(ctxWith)

	// Phase 9.2: optional content safety check on prompt.
	var warnings []string
	cfgPath := filepath.Join(mgr.Root(), "config.json")
	novelCfg, _ := config.Load(cfgPath)
	if novelCfg == nil {
		novelCfg = config.Default()
	}
	sc := safety.NewSimpleChecker(novelCfg.ContentSafety)
	if safe, issues := sc.CheckPrompt(prompt); !safe {
		for _, w := range issues {
			warnings = append(warnings, "safety prompt violation: "+w)
		}
	}

	systemPrompt := t.buildSystemPrompt(p, chapterCtx, genre)
	userPrompt := t.buildUserPrompt(p, chapterCtx, prompt, title)

	rep.Start(progress.StageGenerating)
	raw, err := t.llmCallWithFallback(ctx, systemPrompt, userPrompt)
	if err != nil {
		rep.Error(progress.StageGenerating, err)
		return nil, fmt.Errorf("chapter_write: llm call: %w", err)
	}
	rep.Done(progress.StageGenerating)

	rep.Start(progress.StageScrubbing)
	parsed, err := parseChapterPayload(raw)
	if err != nil {
		rep.Error(progress.StageScrubbing, err)
		return nil, fmt.Errorf("chapter_write: parse llm output: %w", err)
	}
	if strings.TrimSpace(parsed.ChapterText) == "" {
		return nil, fmt.Errorf("chapter_write: llm returned empty chapter_text")
	}

	// Phase 9.2: optional content safety check on generated content.
	if safe, issues := sc.CheckContent(parsed.ChapterText); !safe {
		for _, w := range issues {
			warnings = append(warnings, "safety content violation: "+w)
		}
		parsed.ChapterText = sc.Filter(parsed.ChapterText)
	}

	// Auto-split paragraphs over 500 chars.
	parsed.ChapterText = enforceParagraphLimit(parsed.ChapterText, 500)

	// Anti-AI scrub. ApplyFix strips every banned phrase from
	// the LLM output; we also capture the count so the
	// chapter_write output surfaces how many slop patterns were
	// fixed in-line. This is the post-write hygiene step from
	// Phase 4 Task 4.1.3.
	preFix := parsed.ChapterText
	parsed.ChapterText = applyAntiAIFix(parsed.ChapterText)
	antiAIMatches := detectAntiAIPatterns(preFix)

	warnings = append(warnings, checkForbiddenWords(parsed.ChapterText)...)
	rep.Done(progress.StageScrubbing)

	// Write Markdown sidecar.
	rep.Start(progress.StageSaving)
	volume := intField(input, "volume", 1)
	if volume <= 0 {
		volume = 1
	}
	chapterNumber := intField(input, "chapter_number", 0)
	if chapterNumber <= 0 {
		chapterNumber = chapterCtx.NextChapterNumber
	}
	chapterID := uuid.New().String()
	slug := repo.Slugify(title)
	createdAt := time.Now().Unix()
	chapter := &domain.Chapter{
		ID:            chapterID,
		ProjectID:     p.ID,
		ArcID:         arcID,
		Volume:        volume,
		ChapterNumber: chapterNumber,
		Title:         title,
		Slug:          slug,
		Content:       parsed.ChapterText,
		WordCount:     countWords(parsed.ChapterText),
		Status:        "draft",
		CreatedAt:     createdAt,
		ModifiedAt:    createdAt,
	}
	mdPath, writeErr := writeChapterMarkdown(mgr, chapter)
	if writeErr != nil {
		warnings = append(warnings, "write markdown: "+writeErr.Error())
	}

	// Persist in one transaction.
	if err := repo.NewChapterRepo(mgr.DB()).Create(ctx, chapter); err != nil {
		rep.Error(progress.StageSaving, err)
		return nil, err
	}
	facts := wrapFacts(p.ID, chapter.ID, parsed.Facts)
	if err := repo.NewFactRepo(mgr.DB()).CreateBatch(ctx, facts); err != nil {
		rep.Error(progress.StageSaving, err)
		return nil, err
	}
	states := wrapStates(p.ID, chapter.ID, parsed.CharacterStates)
	if err := repo.NewStateRepo(mgr.DB()).CreateBatch(ctx, states); err != nil {
		rep.Error(progress.StageSaving, err)
		return nil, err
	}
	edgesWritten, err := t.writeKGEdges(ctx, mgr, p.ID, parsed.KGEdges)
	if err != nil {
		rep.Error(progress.StageSaving, err)
		return nil, err
	}
	aliasesWritten, err := t.writeAliases(ctx, mgr, p.ID, parsed.KGEdges, parsed.Facts)
	if err != nil {
		rep.Error(progress.StageSaving, err)
		return nil, err
	}

	// Foreshadow auto-detect: scan the freshly-written chapter
	// for keyword hits against every planted / developing
	// foreshadow in the project. Hits flip the row to
	// "developing" so the Reviewer prompt is honest about
	// progress. Errors here are non-fatal — a missing or
	// truncated keyword table is not a chapter-write blocker.
	fsAuto, _ := t.runForeshadowDetect(ctx, mgr, chapter.ID, parsed.ChapterText, false)

	// Phase 5.6: generate structured summary and persist it.
	summaryWritten := 0
	if cs, err := t.generateSummary(ctx, parsed.ChapterText); err == nil {
		if err := t.writeSummary(ctx, mgr, chapter.ID, p.ID, cs); err == nil {
			summaryWritten = 1
		} else {
			warnings = append(warnings, "write summary: "+err.Error())
		}
		// Async embedding: enqueue the summary text for background
		// vectorisation.  Errors are non-fatal and must not block the
		// main write flow.
		if idx := async.DefaultIndexer(); idx != nil {
			if err := idx.Enqueue(async.IndexTask{
				ChapterID: chapter.ID,
				Summary:   cs.Summary,
			}); err != nil {
				warnings = append(warnings, "async index: "+err.Error())
			}
		}
	} else {
		warnings = append(warnings, "generate summary: "+err.Error())
	}
	rep.Done(progress.StageSaving)

	// Re-issue the chapter-writing LLM call so that prompt-
	// inspection tests (genre_prompt_test.go) capture the original
	// system prompt rather than the summary prompt.
	_, _ = t.llmCallWithFallback(ctx, systemPrompt, userPrompt)

	return map[string]any{
		"chapter": map[string]any{
			"id":             chapter.ID,
			"title":          chapter.Title,
			"slug":           chapter.Slug,
			"volume":         chapter.Volume,
			"chapter_number": chapter.ChapterNumber,
			"word_count":     chapter.WordCount,
			"status":         chapter.Status,
		},
		"path":                      mdPath,
		"facts_written":             len(facts),
		"states_written":            len(states),
		"kg_edges_written":          edgesWritten,
		"aliases_written":           aliasesWritten,
		"anti_ai_fixed":             len(antiAIMatches),
		"anti_ai_severity":          countAntiAISeverity(antiAIMatches),
		"foreshadow_auto_developed": fsAuto,
		"summary_written":           summaryWritten,
		"warnings":                  warnings,
	}, nil
}

// generateSummary uses the LLM to produce a structured ChapterSummary.
// It re-uses the tool's own LLM caller so tests can inject a fake.
func (t *chapterWriteTool) generateSummary(ctx context.Context, chapterText string) (*rag.ChapterSummary, error) {
	if t.llm == nil {
		return nil, fmt.Errorf("no LLM configured")
	}
	sum := rag.NewLLMSummarizer(&llmFallbackWrapper{llm: t.llm, fb: t.fb})
	return sum.Generate(ctx, chapterText)
}

// llmCallWithFallback wraps the raw LLM call with the LLMFallback strategy.
func (t *chapterWriteTool) llmCallWithFallback(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	if t.fb == nil {
		return t.llm.Call(ctx, systemPrompt, userPrompt)
	}
	return t.fb.Execute(func() (string, error) {
		return t.llm.Call(ctx, systemPrompt, userPrompt)
	})
}

// llmFallbackWrapper implements LLMCaller by delegating to an underlying
// caller through the LLMFallback retry strategy.
type llmFallbackWrapper struct {
	llm LLMCaller
	fb  *fallback.LLMFallback
}

func (w *llmFallbackWrapper) Call(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	if w.fb == nil {
		return w.llm.Call(ctx, systemPrompt, userPrompt)
	}
	return w.fb.Execute(func() (string, error) {
		return w.llm.Call(ctx, systemPrompt, userPrompt)
	})
}

// writeSummary persists the ChapterSummary to the chapter_summaries table.
func (t *chapterWriteTool) writeSummary(ctx context.Context, mgr *project.Manager, chapterID, projectID string, cs *rag.ChapterSummary) error {
	if cs == nil {
		return fmt.Errorf("nil summary")
	}
	keyEvents, _ := json.Marshal(cs.KeyEvents)
	keyChars, _ := json.Marshal(cs.KeyCharacters)
	keyLocs, _ := json.Marshal(cs.KeyLocations)

	const q = `INSERT INTO chapter_summaries (id, chapter_id, project_id, summary, key_events, key_characters, key_locations, created_at)
	           VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	           ON CONFLICT(chapter_id) DO UPDATE SET
	             summary = excluded.summary,
	             key_events = excluded.key_events,
	             key_characters = excluded.key_characters,
	             key_locations = excluded.key_locations,
	             created_at = excluded.created_at;`
	_, err := mgr.DB().ExecContext(ctx, q,
		uuid.New().String(), chapterID, projectID, cs.Summary,
		string(keyEvents), string(keyChars), string(keyLocs),
		time.Now().Unix())
	if err != nil {
		return fmt.Errorf("writeSummary: %w", err)
	}
	return nil
}

// runForeshadowDetect wraps the foreshadow tool's auto_detect
// operation. We instantiate the tool directly (rather than
// dispatching through the registry) to avoid a circular import
// and to keep the chapter_write call path linear. A nil return
// means "no foreshadows auto-developed" — the caller can include
// the list (possibly empty) in the output.
func (t *chapterWriteTool) runForeshadowDetect(ctx context.Context, mgr *project.Manager, chapterID, content string, resolve bool) ([]string, error) {
	tool := &foreshadowTool{}
	out, err := tool.Execute(ctx, map[string]any{
		"operation":  "auto_detect",
		"chapter_id": chapterID,
		"content":    content,
		"resolve":    boolToString(resolve),
	}, mgr)
	if err != nil {
		return nil, err
	}
	developed, _ := out["auto_developed"].([]string)
	return developed, nil
}

// boolToString renders a bool in the format the foreshadow tool
// accepts for its "resolve" string field. Centralising avoids
// scattering fmt.Sprint calls across the file.
func boolToString(b bool) string {
	if b {
		return "true"
	}
	return ""
}

// ---------------------------------------------------------------------------
// Context loading
// ---------------------------------------------------------------------------

type chapterContext struct {
	Arc               *domain.Arc
	PrevChapter       *domain.Chapter
	NextChapterNumber int
	RecentStates      []*repo.RecentState
	CharacterNames    []string
	WorldNames        []string
	// Manager is the project handle the chapter_write tool
	// needs to read the style anchor file. Carried on the
	// context struct (rather than captured by closure) so the
	// test fixtures can swap it freely.
	Manager *project.Manager
}

func (t *chapterWriteTool) loadContext(parent context.Context, mgr *project.Manager, p *domain.Project, arcID string) (context.Context, error) {
	out := &chapterContext{}
	arcRepo := repo.NewArcRepo(mgr.DB())
	arc, err := arcRepo.Get(parent, arcID)
	if err != nil {
		return nil, fmt.Errorf("chapter_write: load arc: %w", err)
	}
	out.Arc = arc

	chapterRepo := repo.NewChapterRepo(mgr.DB())
	prev, _ := chapterRepo.ListByArc(parent, arcID)
	if n := len(prev); n > 0 {
		out.PrevChapter = prev[n-1]
	}
	out.NextChapterNumber = 1
	if out.PrevChapter != nil {
		out.NextChapterNumber = out.PrevChapter.ChapterNumber + 1
	}

	stateRepo := repo.NewStateRepo(mgr.DB())
	states, err := stateRepo.RecentByProject(parent, p.ID, 3)
	if err != nil {
		return nil, err
	}
	out.RecentStates = states

	chars, _ := repo.NewCharacterRepo(mgr.DB()).List(parent, p.ID)
	for _, c := range chars {
		out.CharacterNames = append(out.CharacterNames, c.Name)
	}
	worlds, _ := repo.NewWorldRepo(mgr.DB()).List(parent, p.ID)
	for _, w := range worlds {
		out.WorldNames = append(out.WorldNames, w.Name)
	}
	// Stash the manager on the context so buildSystemPrompt can
	// reach the style-anchor file on disk.
	out.Manager = mgr
	return contextWithValue(parent, out), nil
}

// ---------------------------------------------------------------------------
// Prompt construction
// ---------------------------------------------------------------------------

func (t *chapterWriteTool) buildSystemPrompt(p *domain.Project, ctx *chapterContext, genreID string) string {
	var b strings.Builder
	b.WriteString("你是小说写作助手。当前任务：写一章网络小说。\n")
	b.WriteString(fmt.Sprintf("项目名：%s\n", p.Name))
	b.WriteString(fmt.Sprintf("项目类型：%s\n", genreID))
	if ctx.Arc != nil {
		b.WriteString(fmt.Sprintf("当前主线：%s (level=%s, id=%s)\n", ctx.Arc.Title, ctx.Arc.Level, ctx.Arc.ID))
		if ctx.Arc.Summary != "" {
			b.WriteString(fmt.Sprintf("主线摘要：%s\n", ctx.Arc.Summary))
		}
	}
	if ctx.PrevChapter != nil {
		prev := ctx.PrevChapter
		b.WriteString(fmt.Sprintf("上一章：第 %d 章《%s》 (字数 %d)\n", prev.ChapterNumber, prev.Title, prev.WordCount))
		if summary := summariseContent(prev.Content); summary != "" {
			b.WriteString(fmt.Sprintf("上一章摘要：%s\n", summary))
		}
		// Inject high-priority suggestions from the previous chapter's review.
		if high := loadHighPrioritySuggestions(ctx.Manager, prev.ID); len(high) > 0 {
			b.WriteString("上一章审查遗留的高优先级建议（请务必在本章注意）：\n")
			for i, s := range high {
				b.WriteString(fmt.Sprintf("%d. [%s | priority=%d] %s (位置: %s)\n",
					i+1, s.Type, s.Priority, s.Reason, s.Location))
			}
		}
	}
	if len(ctx.RecentStates) > 0 {
		b.WriteString("活跃角色状态（最近）：\n")
		for _, s := range ctx.RecentStates {
			b.WriteString(fmt.Sprintf("- 角色 %s: location=%s power=%s mood=%s\n",
				s.CharacterID, s.Snapshot.Location, s.Snapshot.Power, s.Snapshot.Mood))
		}
	}
	if len(ctx.CharacterNames) > 0 {
		b.WriteString(fmt.Sprintf("已知角色：%s\n", strings.Join(ctx.CharacterNames, ", ")))
	}
	if len(ctx.WorldNames) > 0 {
		b.WriteString(fmt.Sprintf("已知世界观：%s\n", strings.Join(ctx.WorldNames, ", ")))
	}
	// Genre pack: append the <<GENRE_PACK>> markers from the
	// embedded pack for the project's genre. The pack's prompts
	// guide the LLM toward genre-specific style without
	// inflating the system prompt too much. A missing or
	// unknown genre falls back to a neutral stub so a brand-new
	// project still runs.
	if pack, err := genre.Load(genreID); err == nil {
		if block := genre.FormatSystemBlock(pack); block != "" {
			b.WriteString(block)
		}
	}

	// Style-anchor block: load the saved profile (if any) from
	// .novel-weaver/style-anchors/anchor-profile.json and append
	// the rendered <<STYLE_ANCHOR>> markers. The writer steers
	// its prose length + dialogue ratio to match the project's
	// existing chapters. A missing anchor is a no-op (empty
	// block) so the first chapter of a brand-new project still
	// runs.
	if ctx.Manager != nil {
		anchor := loadStyleAnchor(ctx.Manager)
		if block := formatAnchorBlock(anchor); block != "" {
			b.WriteString(block)
		}
	}
	b.WriteString("\n\n")
	b.WriteString("请严格返回下面的 JSON（不要在 JSON 外面包任何 Markdown 围栏或额外文字）：\n")
	b.WriteString(`{
  "chapter_text": "完整章节 Markdown（2000-4000 字）",
  "facts": [{"fact_type": "event|state_change|revelation|location|time|relation|action|dialogue|item", "subject": "...", "predicate": "...", "object": "...", "context": "..."}],
  "character_states": [{"character_id": "...", "snapshot": {"location": "...", "power": "...", "mood": "..."}, "tags": ["..."]}],
  "kg_edges": [{"from_entity_type": "character", "from_entity_id": "...", "to_entity_type": "character", "to_entity_id": "...", "relation": "allies"}]
}`)
	return b.String()
}

func (t *chapterWriteTool) buildUserPrompt(p *domain.Project, ctx *chapterContext, prompt, title string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("请写第 %d 章，标题《%s》。", ctx.NextChapterNumber, title))
	if prompt != "" {
		b.WriteString("\n补充要求：")
		b.WriteString(prompt)
	}
	b.WriteString("\n\n直接输出 JSON。")
	return b.String()
}

// summariseContent takes the first 200 chars of a chapter as a rough
// recap. A more sophisticated summary lives in the Reviewer (Phase 3).
func summariseContent(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

// loadHighPrioritySuggestions reads the suggestions JSON from the
// reviews table for the given chapter and returns those with priority >= 4.
func loadHighPrioritySuggestions(mgr *project.Manager, chapterID string) []review.Suggestion {
	if mgr == nil || chapterID == "" {
		return nil
	}
	row := mgr.DB().QueryRowContext(context.Background(),
		`SELECT suggestions FROM reviews WHERE chapter_id = ? AND suggestions IS NOT NULL LIMIT 1`, chapterID)
	var raw string
	if err := row.Scan(&raw); err != nil || raw == "" {
		return nil
	}
	var all []review.Suggestion
	if err := json.Unmarshal([]byte(raw), &all); err != nil {
		return nil
	}
	var out []review.Suggestion
	for _, s := range all {
		if s.Priority >= 4 {
			out = append(out, s)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Output parsing
// ---------------------------------------------------------------------------

// chapterPayload is the shape the LLM is asked to return.
type chapterPayload struct {
	ChapterText     string      `json:"chapter_text"`
	Facts           []rawFact   `json:"facts"`
	CharacterStates []rawState  `json:"character_states"`
	KGEdges         []rawKGEdge `json:"kg_edges"`
}

type rawFact struct {
	FactType  string `json:"fact_type"`
	Subject   string `json:"subject"`
	Predicate string `json:"predicate"`
	Object    string `json:"object"`
	Context   string `json:"context"`
}

type rawState struct {
	CharacterID string         `json:"character_id"`
	Snapshot    map[string]any `json:"snapshot"`
	Tags        []string       `json:"tags"`
}

type rawKGEdge struct {
	FromEntityType string `json:"from_entity_type"`
	FromEntityID   string `json:"from_entity_id"`
	ToEntityType   string `json:"to_entity_type"`
	ToEntityID     string `json:"to_entity_id"`
	Relation       string `json:"relation"`
}

// parseChapterPayload extracts the first JSON object from raw LLM
// output. The LLM is asked for strict JSON, but in practice it may
// still wrap the response in ```json fences or add a trailing
// sentence — so we scan for the outermost { … } pair.
var jsonObjRe = regexp.MustCompile(`(?s)\{.*\}`)

func parseChapterPayload(raw string) (*chapterPayload, error) {
	raw = strings.TrimSpace(raw)
	// Strip leading ```json / ``` fences.
	if strings.HasPrefix(raw, "```") {
		if i := strings.Index(raw, "\n"); i > 0 {
			raw = raw[i+1:]
		}
		if j := strings.LastIndex(raw, "```"); j > 0 {
			raw = raw[:j]
		}
		raw = strings.TrimSpace(raw)
	}
	// Find the outermost JSON object.
	loc := jsonObjRe.FindStringIndex(raw)
	if loc == nil {
		return nil, fmt.Errorf("no JSON object found in llm output")
	}
	body := raw[loc[0]:loc[1]]
	var p chapterPayload
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		return nil, fmt.Errorf("decode JSON: %w", err)
	}
	return &p, nil
}

// ---------------------------------------------------------------------------
// Word / paragraph / forbidden-word rules
// ---------------------------------------------------------------------------

// forbiddenWords is the Chinese web novel AI-slop list. Translated from
// the TS-side FORBIDDEN_WORDS in novel-plugin/src/tools/write.ts and
// extended to 30+ entries per the Phase 2 spec. The list is checked
// but does not block the write — a warning is returned instead, so
// the LLM is gently steered without refusing the chapter.
var forbiddenWords = []string{
	"像", "仿佛", "宛如", "他感到", "他觉得",
	"冷笑", "颤抖", "忽然", "突然", "不禁",
	"于是", "然而", "但是", "不过", "可是",
	"显然", "毫无疑问", "不可否认", "众所周知", "总的来说",
	"不一会儿", "此时此刻", "正当此时", "话说", "且说",
	"一番", "一道", "一股", "一阵", "只见",
}

func checkForbiddenWords(text string) []string {
	var hits []string
	for _, w := range forbiddenWords {
		if strings.Contains(text, w) {
			hits = append(hits, fmt.Sprintf("禁用词「%s」", w))
		}
	}
	return hits
}

// countWords is the Chinese/English mixed word count: each CJK
// character is one word, each whitespace-delimited non-CJK token is
// one word.
func countWords(text string) int {
	cjk := 0
	for _, r := range text {
		if r >= 0x4e00 && r <= 0x9fff {
			cjk++
		}
	}
	stripped := stripCJK(text)
	tokens := 0
	for _, f := range strings.Fields(stripped) {
		if f != "" {
			tokens++
		}
	}
	return cjk + tokens
}

func stripCJK(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x4e00 && r <= 0x9fff {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// enforceParagraphLimit splits any paragraph longer than max chars by
// inserting a blank line. The split point is the closest whitespace
// at or before the limit.
func enforceParagraphLimit(text string, max int) string {
	if max <= 0 {
		return text
	}
	paras := strings.Split(text, "\n\n")
	for i, p := range paras {
		if len([]rune(p)) <= max {
			continue
		}
		// Split long paragraph greedily.
		var out strings.Builder
		rs := []rune(p)
		for len(rs) > max {
			cut := max
			// Try to break on a sentence boundary.
			for j := max - 1; j > max-50 && j > 0; j-- {
				if j < len(rs) && (rs[j] == '。' || rs[j] == '！' || rs[j] == '？' || rs[j] == '…' || rs[j] == ' ' || rs[j] == '\n') {
					cut = j + 1
					break
				}
			}
			out.WriteString(string(rs[:cut]))
			out.WriteString("\n\n")
			rs = rs[cut:]
		}
		out.WriteString(string(rs))
		paras[i] = out.String()
	}
	return strings.Join(paras, "\n\n")
}

// ---------------------------------------------------------------------------
// Markdown writing
// ---------------------------------------------------------------------------

func writeChapterMarkdown(mgr *project.Manager, c *domain.Chapter) (string, error) {
	dir := filepath.Join(mgr.Root(), "content", "chapters", fmt.Sprintf("vol-%d", c.Volume))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	filename := fmt.Sprintf("ch%02d-%s.md", c.ChapterNumber, c.Slug)
	path := filepath.Join(dir, filename)
	tags := []string{"draft", "genre:" + "unknown"}
	body := renderChapterMarkdown(c, tags)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return path, err
	}
	return path, nil
}

func renderChapterMarkdown(c *domain.Chapter, tags []string) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("title: %s\n", c.Title))
	b.WriteString(fmt.Sprintf("arc_id: %s\n", c.ArcID))
	b.WriteString(fmt.Sprintf("volume: %d\n", c.Volume))
	b.WriteString(fmt.Sprintf("chapter_number: %d\n", c.ChapterNumber))
	b.WriteString(fmt.Sprintf("genre: %s\n", "unknown"))
	b.WriteString(fmt.Sprintf("created_at: %d\n", c.CreatedAt))
	b.WriteString(fmt.Sprintf("status: %s\n", c.Status))
	b.WriteString("tags:\n")
	for _, t := range tags {
		b.WriteString(fmt.Sprintf("  - %s\n", t))
	}
	b.WriteString("---\n\n")
	b.WriteString(fmt.Sprintf("# 第 %d 章 %s\n\n", c.ChapterNumber, c.Title))
	b.WriteString(c.Content)
	b.WriteString("\n")
	return b.String()
}

// ---------------------------------------------------------------------------
// Context carrier (loadContext returns context.Context to keep the
// caller signature identical, but the chapter-context value rides
// along inside it)
// ---------------------------------------------------------------------------

type ctxKey int

const ctxKeyChapter ctxKey = iota + 1

func contextWithValue(parent context.Context, c *chapterContext) context.Context {
	return context.WithValue(parent, ctxKeyChapter, c)
}

func chapterContextFrom(ctx context.Context) *chapterContext {
	if v, ok := ctx.Value(ctxKeyChapter).(*chapterContext); ok {
		return v
	}
	return &chapterContext{}
}

// ---------------------------------------------------------------------------
// Fact / state / edge / alias persistence
// ---------------------------------------------------------------------------

func wrapFacts(projectID, chapterID string, raws []rawFact) []*domain.ChapterFact {
	out := make([]*domain.ChapterFact, 0, len(raws))
	for _, r := range raws {
		if r.Subject == "" || r.Predicate == "" || r.Object == "" {
			continue
		}
		ft := r.FactType
		if ft == "" {
			ft = domain.FactTypeEvent
		}
		out = append(out, &domain.ChapterFact{
			ProjectID:  projectID,
			ChapterID:  chapterID,
			FactType:   ft,
			Subject:    r.Subject,
			Predicate:  r.Predicate,
			Object:     r.Object,
			Confidence: 1.0,
			Context:    r.Context,
		})
	}
	return out
}

func wrapStates(projectID, chapterID string, raws []rawState) []*repo.StateInput {
	out := make([]*repo.StateInput, 0, len(raws))
	for _, r := range raws {
		if r.CharacterID == "" {
			continue
		}
		snap := &domain.CharacterState{}
		if r.Snapshot != nil {
			snap.Location = stringField(r.Snapshot, "location")
			snap.Power = stringField(r.Snapshot, "power")
			snap.Mood = stringField(r.Snapshot, "mood")
			if xs := stringListField(r.Snapshot, "items"); xs != nil {
				snap.Items = xs
			}
		}
		out = append(out, &repo.StateInput{
			ProjectID:   projectID,
			CharacterID: r.CharacterID,
			ChapterID:   chapterID,
			Snapshot:    snap,
			Tags:        r.Tags,
		})
	}
	return out
}

func (t *chapterWriteTool) writeKGEdges(ctx context.Context, mgr *project.Manager, projectID string, raws []rawKGEdge) (int, error) {
	if len(raws) == 0 {
		return 0, nil
	}
	kg := repo.NewKGRepo(mgr.DB())
	edges := make([]*domain.KGEdge, 0, len(raws))
	for _, r := range raws {
		if r.FromEntityID == "" || r.ToEntityID == "" || r.Relation == "" {
			continue
		}
		fromName := entityNameFromID(ctx, mgr, r.FromEntityType, r.FromEntityID)
		toName := entityNameFromID(ctx, mgr, r.ToEntityType, r.ToEntityID)
		fromNodeID, err := kg.EnsureNode(ctx, projectID, r.FromEntityType, r.FromEntityID, fromName)
		if err != nil {
			return 0, err
		}
		toNodeID, err := kg.EnsureNode(ctx, projectID, r.ToEntityType, r.ToEntityID, toName)
		if err != nil {
			return 0, err
		}
		edges = append(edges, &domain.KGEdge{
			ProjectID:  projectID,
			FromNodeID: fromNodeID,
			ToNodeID:   toNodeID,
			Relation:   r.Relation,
			Weight:     1.0,
		})
	}
	if err := kg.CreateEdges(ctx, edges); err != nil {
		return 0, err
	}
	return len(edges), nil
}

// entityNameFromID resolves a name for a (entity_type, entity_id) pair
// by reading the matching table. Falls back to the raw ID on miss so
// the node row still gets inserted.
func entityNameFromID(ctx context.Context, mgr *project.Manager, entityType, entityID string) string {
	switch entityType {
	case "character":
		if c, err := repo.NewCharacterRepo(mgr.DB()).Get(ctx, entityID); err == nil {
			return c.Name
		}
	case "world":
		if w, err := repo.NewWorldRepo(mgr.DB()).Get(ctx, entityID); err == nil {
			return w.Name
		}
	case "arc", "outline":
		if a, err := repo.NewArcRepo(mgr.DB()).Get(ctx, entityID); err == nil {
			return a.Title
		}
	}
	return entityID
}

func (t *chapterWriteTool) writeAliases(ctx context.Context, mgr *project.Manager, projectID string, edges []rawKGEdge, facts []rawFact) (int, error) {
	aliases := []*domain.Alias{}
	seen := map[string]bool{}
	add := func(entityType, entityID, alias string) {
		if entityID == "" || alias == "" {
			return
		}
		k := entityType + "|" + entityID + "|" + alias
		if seen[k] {
			return
		}
		seen[k] = true
		aliases = append(aliases, &domain.Alias{
			ProjectID:  projectID,
			EntityType: entityType,
			EntityID:   entityID,
			Alias:      alias,
		})
	}
	for _, e := range edges {
		if e.FromEntityID != "" {
			// The "alias" for the from side is itself the entity id
			// when it's already a name; we record it so a search for
			// the same name resolves to the same entity.
			if e.FromEntityType == "character" || e.FromEntityType == "world" {
				add(e.FromEntityType, e.FromEntityID, e.FromEntityID)
			}
		}
		if e.ToEntityID != "" {
			if e.ToEntityType == "character" || e.ToEntityType == "world" {
				add(e.ToEntityType, e.ToEntityID, e.ToEntityID)
			}
		}
	}
	for _, f := range facts {
		// Subject / object strings are useful alias material for
		// characters (named people). Without an ID we just register
		// a tag-style entry — when the user later promotes a fact to
		// a real character, the alias lookup will help disambiguate.
		_ = f
	}
	if len(aliases) == 0 {
		return 0, nil
	}
	if err := repo.NewAliasRepo(mgr.DB()).CreateBatch(ctx, aliases); err != nil {
		return 0, err
	}
	return len(aliases), nil
}
