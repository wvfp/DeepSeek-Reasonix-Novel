package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"reasonix/internal/novel/domain"
	"reasonix/internal/novel/project"
	"reasonix/internal/novel/repo"
	"reasonix/internal/novel/roles"
)

// consistencyTool is the `consistency` tool. It walks the structured
// memory (chapter_facts, character_states, knowledge_graph) for the
// current project, builds a focused context window, and asks the
// Reviewer role to flag contradictions across five dimensions:
//
//	time     — chapter-ordering / time-of-day inconsistencies
//	space    — same character at two places without a travel fact
//	character — same character with two irreconcilable states
//	item     — item ownership / disappearance contradictions
//	setting  — world / magic-system / Faction rules contradictions
//
// The Reviewer returns a JSON object with an "issues" array; we
// normalise the output into the standard Issue shape and write one
// row per issue into the reviews table so the chapter_review UI can
// render the same colour-coded badges for "time inconsistency" as for
// "plot hole".
//
// The tool is registered on the default catalog with a Switcher
// wired at use-time. Tests that need a deterministic response pass
// their own scriptedLLM via NewConsistencyTool.
type consistencyTool struct {
	sw *roles.Switcher
}

// NewConsistencyTool returns a consistencyTool bound to a role
// switcher. Mirrors NewChapterReviewTool so the CLI's
// install-default-LLM step has a one-liner to call.
func NewConsistencyTool(sw *roles.Switcher) *consistencyTool {
	return &consistencyTool{sw: sw}
}

func init() { registerDefault(&consistencyTool{}) }

func (t *consistencyTool) Name() string { return "consistency" }

func (t *consistencyTool) Description() string {
	return "跨 chapter_facts / character_states / knowledge_graph 检测 5 维度矛盾（time/space/character/item/setting），调 Reviewer 跑一次 LLM 写出 issues。"
}

// Issue is the canonical shape of one consistency finding. The
// review-fix / chapter-review tools share the same field names so
// the UI can render them identically.
type consistencyIssue struct {
	Dimension   string   `json:"dimension"`   // time|space|character|item|setting
	Severity    string   `json:"severity"`    // blocker|warning|info
	Chapters    []string `json:"chapters"`    // chapter ids implicated
	Description string   `json:"description"` // free-form Chinese description
}

// Execute is the entry point. It returns:
//
//	{"issues": [...], "issue_count": N, "dimension_counts": {...}}
//
// on success. Errors out when no LLM is wired or when the project
// is empty (no chapter_facts → nothing to check).
func (t *consistencyTool) Execute(ctx context.Context, input map[string]any, mgr *project.Manager) (map[string]any, error) {
	if t.sw == nil || t.sw.LLM() == nil {
		return nil, ErrNoLLMConfigured
	}
	p, err := mgr.Project(ctx)
	if err != nil {
		return nil, err
	}

	bundle, err := t.loadBundle(ctx, mgr, p)
	if err != nil {
		return nil, err
	}
	if bundle.empty() {
		return map[string]any{
			"issues":           []any{},
			"issue_count":      0,
			"dimension_counts": map[string]int{},
			"message":          "项目暂无章节事实或角色状态，无法做一致性检查；请先写至少 1 章。",
		}, nil
	}

	systemPrompt := t.sw.SwitchTo(roles.RoleReviewer)
	userPrompt := t.buildUserPrompt(p, bundle)
	raw, err := t.sw.LLM().Call(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("consistency: llm call: %w", err)
	}
	issues, err := parseConsistencyPayload(raw)
	if err != nil {
		return nil, fmt.Errorf("consistency: parse: %w", err)
	}

	// Persist each issue as a Review row so the existing 8-dim
	// dashboard surfaces it without a new table.
	reviewRepo := repo.NewReviewRepo(mgr.DB())
	chapterRepo := repo.NewChapterRepo(mgr.DB())
	knownChapters := map[string]bool{}
	if chs, _ := chapterRepo.List(ctx, p.ID); len(chs) > 0 {
		for _, c := range chs {
			knownChapters[c.ID] = true
		}
	}
	written := 0
	now := bundle.now
	for _, iss := range issues {
		// Skip issues with no chapter binding — they would
		// show up as "global" issues in the UI but the
		// reviews table is chapter-scoped.
		chapterID := ""
		for _, cid := range iss.Chapters {
			if knownChapters[cid] {
				chapterID = cid
				break
			}
		}
		if chapterID == "" {
			continue
		}
		rv := &domain.Review{
			ChapterID: chapterID,
			Dimension: "consistency_" + iss.Dimension,
			Score:     5.0,
			Issues: []domain.Issue{{
				Severity:    iss.Severity,
				Description: iss.Description,
				Location:    strings.Join(iss.Chapters, ","),
			}},
			CreatedAt: now,
		}
		if err := reviewRepo.Create(ctx, rv); err != nil {
			return nil, err
		}
		written++
	}

	return map[string]any{
		"issues":           issues,
		"issue_count":      len(issues),
		"dimension_counts": countByDimension(issues),
		"persisted":        written,
	}, nil
}

// consistencyBundle is the in-memory dump the prompt embeds.
type consistencyBundle struct {
	Chapters        []chapterRow
	Facts           []*domain.ChapterFact
	CharacterStates []*repo.RecentState
	KGNodeCount     int
	KGEdgeCount     int
	now             int64
}

// chapterRow is a slim view of the chapters table — only the id +
// title + chapter_number + volume columns the LLM needs to anchor
// each issue to a chapter. Keeping the Content out of the bundle
// keeps the prompt small (the consistency check reasons over
// facts, not over prose).
type chapterRow struct {
	ID            string
	Volume        int
	ChapterNumber int
	Title         string
}

func (b *consistencyBundle) empty() bool {
	return len(b.Facts) == 0 && len(b.CharacterStates) == 0 && b.KGNodeCount == 0
}

// loadBundle reads the four tables in a single pass. The chapter +
// chapter_facts join is the hot path; character_states is per-chapter
// so we only load the latest snapshot per (project, character) to
// avoid bloating the prompt with stale rows.
func (t *consistencyTool) loadBundle(ctx context.Context, mgr *project.Manager, p *domain.Project) (*consistencyBundle, error) {
	out := &consistencyBundle{now: time.Now().Unix()}

	cr := repo.NewChapterRepo(mgr.DB())
	chs, err := cr.List(ctx, p.ID)
	if err != nil {
		return nil, fmt.Errorf("consistency: list chapters: %w", err)
	}
	for _, c := range chs {
		out.Chapters = append(out.Chapters, chapterRow{
			ID: c.ID, Volume: c.Volume, ChapterNumber: c.ChapterNumber, Title: c.Title,
		})
	}

	fr := repo.NewFactRepo(mgr.DB())
	for _, c := range chs {
		fs, err := fr.ListByChapter(ctx, c.ID)
		if err != nil {
			return nil, fmt.Errorf("consistency: list facts for %s: %w", c.ID, err)
		}
		out.Facts = append(out.Facts, fs...)
	}

	// StateRepo.RecentByProject already returns the latest N states;
	// raise the cap to cover the whole project so the LLM sees
	// every per-chapter snapshot.
	sr := repo.NewStateRepo(mgr.DB())
	states, err := sr.RecentByProject(ctx, p.ID, 200)
	if err != nil {
		return nil, fmt.Errorf("consistency: list states: %w", err)
	}
	out.CharacterStates = states

	// KG node + edge count are cheap and tell the LLM "there is
	// also a knowledge graph, just trust the chapter_facts for
	// the actual triples". Avoids a multi-table join we don't need.
	if n, err := countTable(ctx, mgr, "knowledge_graph_nodes"); err == nil {
		out.KGNodeCount = n
	}
	if n, err := countTable(ctx, mgr, "knowledge_graph_edges"); err == nil {
		out.KGEdgeCount = n
	}
	return out, nil
}

// countTable returns COUNT(*) for a table. Used to surface KG size
// to the LLM without dumping every row.
func countTable(ctx context.Context, mgr *project.Manager, table string) (int, error) {
	row := mgr.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table)
	var n int
	if err := row.Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// buildUserPrompt assembles the context the Reviewer will inspect.
// Format: chapter list (id → title), then per-chapter facts, then
// character states, then KG size. We deliberately do NOT dump the
// chapter body — the LLM is reasoning over structured facts, not
// over prose, and a 5 000 字 chapter would blow the budget.
func (t *consistencyTool) buildUserPrompt(p *domain.Project, b *consistencyBundle) string {
	var s strings.Builder
	s.WriteString("项目：")
	s.WriteString(p.Name)
	s.WriteString("（")
	s.WriteString(p.Genre)
	s.WriteString("）\n")

	if len(b.Chapters) > 0 {
		s.WriteString("\n## 章节列表\n")
		for _, c := range b.Chapters {
			fmt.Fprintf(&s, "- %d-%d %s (id=%s)\n", c.Volume, c.ChapterNumber, c.Title, c.ID)
		}
	}

	if len(b.Facts) > 0 {
		s.WriteString("\n## chapter_facts（按章节分组的 (subject, predicate, object) 三元组）\n")
		// Group facts by chapter so the prompt is easier to scan.
		byChapter := map[string][]*domain.ChapterFact{}
		chapterID := map[string]chapterRow{}
		for _, c := range b.Chapters {
			chapterID[c.ID] = c
		}
		for _, f := range b.Facts {
			byChapter[f.ChapterID] = append(byChapter[f.ChapterID], f)
		}
		for cid, fs := range byChapter {
			cr := chapterID[cid]
			fmt.Fprintf(&s, "\n### %d-%d %s (id=%s)\n", cr.Volume, cr.ChapterNumber, cr.Title, cid)
			for _, f := range fs {
				fmt.Fprintf(&s, "- [%s] %s — %s → %s\n", f.FactType, f.Subject, f.Predicate, f.Object)
			}
		}
	}

	if len(b.CharacterStates) > 0 {
		s.WriteString("\n## character_states（每章角色快照）\n")
		for _, st := range b.CharacterStates {
			fmt.Fprintf(&s, "- ch=%s char=%s location=%s power=%s mood=%s\n",
				shortID(st.ChapterID), st.CharacterID, st.Snapshot.Location, st.Snapshot.Power, st.Snapshot.Mood)
		}
	}

	fmt.Fprintf(&s, "\n## knowledge_graph 规模\nnodes=%d edges=%d\n", b.KGNodeCount, b.KGEdgeCount)

	s.WriteString(`
请基于以上结构化事实做 5 维度一致性检查：
- time：同一事件/角色出现在时间上不可能的章节顺序
- space：同一角色在同一章出现在两个地点，中间没有 travel 事实
- character：同一角色在不同章节有不可调和的状态（境界/身份/关系）
- item：同一道具同时被两个角色持有，或突然消失
- setting：与世界规则/力量体系/势力关系冲突

只返回严格 JSON（不要在 JSON 外加任何文字或 Markdown 围栏）：
{
  "issues": [
    {"dimension": "time|space|character|item|setting",
     "severity": "blocker|warning|info",
     "chapters": ["<chapter_id>", "..."],
     "description": "用中文描述矛盾点，引用 subject/predicate/object"}
  ]
}

如果没有任何矛盾，返回：{"issues": []}。`)
	return s.String()
}

// shortID trims a UUID to its first 8 hex chars for compact
// rendering in the prompt. The LLM never has to round-trip these
// IDs — they are reference material, not inputs.
func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

// countByDimension tallies issues per dimension. Returned in the
// tool output so the dashboard can show a 5-cell bar chart.
func countByDimension(issues []consistencyIssue) map[string]int {
	out := map[string]int{
		"time": 0, "space": 0, "character": 0, "item": 0, "setting": 0,
	}
	for _, iss := range issues {
		if _, ok := out[iss.Dimension]; ok {
			out[iss.Dimension]++
		}
	}
	return out
}

// consistencyPayload is the JSON shape we ask the LLM for.
type consistencyPayload struct {
	Issues []consistencyIssue `json:"issues"`
}

// consistencyJSONRe extracts the outermost { … } from the LLM
// output. Same regex as chapter_review / chapter_write.
var consistencyJSONRe = regexp.MustCompile(`(?s)\{.*\}`)

func parseConsistencyPayload(raw string) ([]consistencyIssue, error) {
	raw = strings.TrimSpace(raw)
	// Strip ```json / ``` fences.
	if strings.HasPrefix(raw, "```") {
		if i := strings.Index(raw, "\n"); i > 0 {
			raw = raw[i+1:]
		}
		if j := strings.LastIndex(raw, "```"); j > 0 {
			raw = raw[:j]
		}
		raw = strings.TrimSpace(raw)
	}
	loc := consistencyJSONRe.FindStringIndex(raw)
	if loc == nil {
		return nil, fmt.Errorf("no JSON object found in reviewer output")
	}
	var p consistencyPayload
	if err := json.Unmarshal([]byte(raw[loc[0]:loc[1]]), &p); err != nil {
		return nil, fmt.Errorf("decode JSON: %w", err)
	}
	// Normalise dimensions and severities. The LLM occasionally
	// slips in "consistency/time" or "time_consistency" — accept
	// a few synonyms and bucket everything else as a warning.
	norm := make([]consistencyIssue, 0, len(p.Issues))
	for _, iss := range p.Issues {
		dim := normaliseDimension(iss.Dimension)
		sev := normaliseSeverity(iss.Severity)
		if iss.Description == "" {
			continue
		}
		norm = append(norm, consistencyIssue{
			Dimension:   dim,
			Severity:    sev,
			Chapters:    iss.Chapters,
			Description: iss.Description,
		})
	}
	return norm, nil
}

// normaliseDimension maps free-form LLM labels to the canonical 5.
// Accepts both English (time/space/character/item/setting) and a
// handful of Chinese synonyms that show up in long context outputs
// ("时间线矛盾", "地点不符" etc.). Anything we cannot map falls
// through to "setting" so a single typo does not nuke the run.
func normaliseDimension(d string) string {
	d = strings.ToLower(strings.TrimSpace(d))
	switch d {
	case "time", "时间", "时间线", "时间线矛盾", "时间不符", "时间错乱", "顺序错乱", "temporal":
		return "time"
	case "space", "空间", "地点", "位置", "地点不符", "空间错乱", "spatial", "location":
		return "space"
	case "character", "人物", "角色", "人设", "角色矛盾", "人设矛盾", "人物矛盾":
		return "character"
	case "item", "道具", "物品", "物件", "道具矛盾", "物品矛盾":
		return "item"
	case "setting", "设定", "世界观", "规则", "设定矛盾", "力量体系", "势力":
		return "setting"
	}
	// Catch "consistency_time" or "time_consistency" style strings.
	switch {
	case strings.Contains(d, "time"), strings.Contains(d, "时间"):
		return "time"
	case strings.Contains(d, "space"), strings.Contains(d, "地点"), strings.Contains(d, "位置"), strings.Contains(d, "空间"):
		return "space"
	case strings.Contains(d, "character"), strings.Contains(d, "人物"), strings.Contains(d, "角色"):
		return "character"
	case strings.Contains(d, "item"), strings.Contains(d, "道具"), strings.Contains(d, "物品"):
		return "item"
	case strings.Contains(d, "setting"), strings.Contains(d, "设定"), strings.Contains(d, "世界观"), strings.Contains(d, "规则"):
		return "setting"
	}
	return "setting" // safest default
}

func normaliseSeverity(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "blocker", "严重", "硬伤":
		return "blocker"
	case "warning", "警告", "中":
		return "warning"
	}
	return "info"
}
