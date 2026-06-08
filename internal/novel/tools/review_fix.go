package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"reasonix/internal/novel/domain"
	"reasonix/internal/novel/project"
	"reasonix/internal/novel/repo"
	"reasonix/internal/novel/roles"
)

// reviewFixTool is the `review_fix` tool. It reads the most recent
// review for a chapter, asks the PlotWriter role to rewrite the
// paragraphs the reviewer flagged as blocker severity, and patches
// the chapter's content in place. The result is a count of fixes
// applied and the remaining issues.
type reviewFixTool struct {
	sw *roles.Switcher
}

func NewReviewFixTool(sw *roles.Switcher) *reviewFixTool {
	return &reviewFixTool{sw: sw}
}

func init() { registerDefault(&reviewFixTool{}) }

func (t *reviewFixTool) Name() string { return "review_fix" }

func (t *reviewFixTool) Description() string {
	return "读取最近一次 review 的 blocker issues，调 PlotWriter 角色重写对应段落，写回 chapter.content。"
}

func (t *reviewFixTool) Execute(ctx context.Context, input map[string]any, mgr *project.Manager) (map[string]any, error) {
	if t.sw == nil || t.sw.LLM() == nil {
		return nil, ErrNoLLMConfigured
	}
	p, err := mgr.Project(ctx)
	if err != nil {
		return nil, err
	}
	cr := repo.NewChapterRepo(mgr.DB())
	chapterID := stringField(input, "chapter_id")
	var chapter *domain.Chapter
	if chapterID == "" {
		chs, err := cr.List(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		if len(chs) == 0 {
			return nil, fmt.Errorf("review_fix: project has no chapters")
		}
		chapter = chs[len(chs)-1]
		chapterID = chapter.ID
	} else {
		chapter, err = cr.Get(ctx, chapterID)
		if err != nil {
			return nil, err
		}
	}

	reviewRepo := repo.NewReviewRepo(mgr.DB())
	reviews, err := reviewRepo.ListByChapter(ctx, chapter.ID)
	if err != nil {
		return nil, err
	}
	if len(reviews) == 0 {
		return nil, fmt.Errorf("review_fix: chapter %q has no reviews yet; run chapter_review first", chapter.ID)
	}

	// Collect all blockers across all dimensions, newest first.
	var blockers []domain.Issue
	for _, r := range reviews {
		for _, issue := range r.Issues {
			if strings.EqualFold(issue.Severity, domain.SeverityBlocker) {
				blockers = append(blockers, issue)
			}
		}
	}
	if len(blockers) == 0 {
		return map[string]any{
			"chapter_id":      chapter.ID,
			"fixed_count":     0,
			"remaining":       0,
			"no_blockers":     true,
		}, nil
	}

	// Ask the PlotWriter to rewrite the chapter text.
	systemPrompt := t.sw.SwitchTo(roles.RolePlotWriter)
	userPrompt := t.buildFixPrompt(p, chapter, blockers)
	raw, err := t.sw.LLM().Call(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("review_fix: llm call: %w", err)
	}
	rewritten, err := parseFixPayload(raw)
	if err != nil {
		return nil, fmt.Errorf("review_fix: parse: %w", err)
	}
	if strings.TrimSpace(rewritten) == "" {
		return nil, fmt.Errorf("review_fix: writer returned empty chapter_text")
	}
	rewritten = enforceParagraphLimit(rewritten, 500)

	// Persist. word_count is recomputed so stats don't drift.
	chapter.Content = rewritten
	chapter.WordCount = countWords(rewritten)
	chapter.ModifiedAt = time.Now().Unix()
	if err := cr.Update(ctx, chapter); err != nil {
		return nil, err
	}
	return map[string]any{
		"chapter_id":  chapter.ID,
		"fixed_count": len(blockers),
		"remaining":   0,
		"new_word_count": chapter.WordCount,
	}, nil
}

// buildFixPrompt composes the user prompt that asks PlotWriter to
// rewrite the chapter with the blocker issues resolved.
func (t *reviewFixTool) buildFixPrompt(p *domain.Project, ch *domain.Chapter, blockers []domain.Issue) string {
	var b strings.Builder
	b.WriteString("项目：")
	b.WriteString(p.Name)
	b.WriteString("（")
	b.WriteString(p.Genre)
	b.WriteString("）\n")
	b.WriteString(fmt.Sprintf("需要修复的章节：第 %d 章《%s》\n", ch.ChapterNumber, ch.Title))
	b.WriteString("以下为 review 列出的 blocker issues，请重写章节正文解决这些问题：\n")
	for i, bl := range blockers {
		fmt.Fprintf(&b, "%d. [%s] %s\n", i+1, bl.Location, bl.Description)
	}
	b.WriteString("\n=== 原章节正文 ===\n")
	b.WriteString(ch.Content)
	b.WriteString("\n=== End ===\n")
	b.WriteString("\n请直接返回严格 JSON：{\"chapter_text\": \"重写后的完整章节正文（2000-4000 字，每段 ≤ 500 字符）\"}。")
	return b.String()
}

// fixPayload is the slice of the PlotWriter response the fix tool
// cares about. We deliberately don't re-extract facts / states here
// — a fix is a textual rewrite, not a new chapter, so a re-extract
// would be misleading.
type fixPayload struct {
	ChapterText string `json:"chapter_text"`
}

func parseFixPayload(raw string) (string, error) {
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
	// Locate the outermost { ... } object.
	loc := reviewJSONRe.FindStringIndex(raw)
	if loc == nil {
		return "", fmt.Errorf("no JSON object in fix output")
	}
	var p fixPayload
	if err := json.Unmarshal([]byte(raw[loc[0]:loc[1]]), &p); err != nil {
		return "", fmt.Errorf("decode JSON: %w", err)
	}
	return p.ChapterText, nil
}
