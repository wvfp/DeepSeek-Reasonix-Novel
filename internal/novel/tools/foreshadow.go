package tools

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/novel/domain"
	"reasonix/internal/novel/project"
	"reasonix/internal/novel/repo"
)

// foreshadowTool manages the foreshadow (伏笔) lifecycle:
//
//	plant    — register a new seed in the foreshadows table
//	develop  — flip status to "developing"; no chapter binding change
//	resolve  — flip status to "resolved", bind resolved_chapter_id
//	abandon  — flip status to "abandoned" (writer dropped the seed)
//
// Auto-detection runs as a side effect of every operation: the
// tool inspects the project's existing foreshadows and, when a new
// chapter is reported, flips any "planted" or "developing" row
// whose keywords appear in the chapter to "developing" (or
// "resolved" when `resolve` is requested). This keeps the
// Reviewer prompt honest without forcing the writer to call the
// tool manually on every chapter.
//
// Output shape:
//
//	{
//	  "foreshadow": { ... full row ... },
//	  "auto_developed": ["<id>", ...],
//	  "auto_resolved":  ["<id>", ...]
//	}
type foreshadowTool struct{}

func init() { registerDefault(&foreshadowTool{}) }

func (t *foreshadowTool) Name() string { return "foreshadow" }

func (t *foreshadowTool) Description() string {
	return "管理伏笔：plant/develop/resolve/abandon。operation 必填；description + importance 必填；可选 chapter_id 绑定 chapter。"
}

// Execute dispatches on the "operation" field. Unknown op → error
// with the canonical list, so the LLM can self-correct on a
// retry.
func (t *foreshadowTool) Execute(ctx context.Context, input map[string]any, mgr *project.Manager) (map[string]any, error) {
	p, err := mgr.Project(ctx)
	if err != nil {
		return nil, err
	}
	op := strings.ToLower(stringField(input, "operation"))
	switch op {
	case "plant":
		return t.plant(ctx, mgr, p, input)
	case "develop":
		return t.develop(ctx, mgr, p, input)
	case "resolve":
		return t.resolve(ctx, mgr, p, input)
	case "abandon":
		return t.abandon(ctx, mgr, p, input)
	case "list":
		return t.list(ctx, mgr, p)
	case "auto_detect":
		// Public entry point so the chapter_write tool can
		// ping it after persisting a new chapter.
		return t.autoDetect(ctx, mgr, p, input)
	default:
		return nil, fmt.Errorf("foreshadow: unknown operation %q (want plant|develop|resolve|abandon|list|auto_detect)", op)
	}
}

// plant inserts a new foreshadow. Requires description and
// importance; chapter_id is optional. When present the row binds
// planted_chapter_id so the Reviewer can plot a timeline.
func (t *foreshadowTool) plant(ctx context.Context, mgr *project.Manager, p *domain.Project, input map[string]any) (map[string]any, error) {
	desc, err := requiredString(input, "description")
	if err != nil {
		return nil, err
	}
	imp := stringField(input, "importance")
	if imp == "" {
		imp = domain.ForeshadowMinor
	}
	if !validImportance(imp) {
		return nil, fmt.Errorf("foreshadow: invalid importance %q (want minor|major|critical)", imp)
	}
	chapterID := stringField(input, "chapter_id")
	if err := ensureChapterExists(ctx, mgr, chapterID); err != nil {
		return nil, err
	}
	f := &domain.Foreshadow{
		ProjectID:        p.ID,
		Description:      desc,
		Keywords:         stringListField(input, "keywords"),
		Status:           domain.ForeshadowPlanted,
		Importance:       imp,
		PlantedChapterID: chapterID,
	}
	if err := repo.NewForeshadowRepo(mgr.DB()).Create(ctx, f); err != nil {
		return nil, err
	}
	return map[string]any{
		"foreshadow":     f,
		"auto_developed": []string{},
		"auto_resolved":  []string{},
	}, nil
}

// develop flips a row to "developing". Idempotent: calling
// develop on an already-developing row is a no-op.
func (t *foreshadowTool) develop(ctx context.Context, mgr *project.Manager, p *domain.Project, input map[string]any) (map[string]any, error) {
	id, err := requiredString(input, "id")
	if err != nil {
		return nil, err
	}
	fr := repo.NewForeshadowRepo(mgr.DB())
	f, err := fr.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if f.Status == domain.ForeshadowResolved || f.Status == domain.ForeshadowAbandoned {
		return nil, fmt.Errorf("foreshadow: cannot develop a %s row", f.Status)
	}
	f.Status = domain.ForeshadowDeveloping
	if err := fr.Update(ctx, f); err != nil {
		return nil, err
	}
	return map[string]any{
		"foreshadow":     f,
		"auto_developed": []string{},
		"auto_resolved":  []string{},
	}, nil
}

// resolve flips a row to "resolved" and binds the chapter. The
// chapter must exist (FK safety).
func (t *foreshadowTool) resolve(ctx context.Context, mgr *project.Manager, p *domain.Project, input map[string]any) (map[string]any, error) {
	id, err := requiredString(input, "id")
	if err != nil {
		return nil, err
	}
	chapterID := stringField(input, "chapter_id")
	if err := ensureChapterExists(ctx, mgr, chapterID); err != nil {
		return nil, err
	}
	fr := repo.NewForeshadowRepo(mgr.DB())
	f, err := fr.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if f.Status == domain.ForeshadowAbandoned {
		return nil, fmt.Errorf("foreshadow: cannot resolve an abandoned row")
	}
	f.Status = domain.ForeshadowResolved
	f.ResolvedChapterID = chapterID
	if err := fr.Update(ctx, f); err != nil {
		return nil, err
	}
	return map[string]any{
		"foreshadow":     f,
		"auto_developed": []string{},
		"auto_resolved":  []string{},
	}, nil
}

// abandon flips a row to "abandoned". Use this when the writer
// consciously drops a seed. The Reviewer flags abandoned "major"
// or "critical" rows as a separate severity.
func (t *foreshadowTool) abandon(ctx context.Context, mgr *project.Manager, p *domain.Project, input map[string]any) (map[string]any, error) {
	id, err := requiredString(input, "id")
	if err != nil {
		return nil, err
	}
	fr := repo.NewForeshadowRepo(mgr.DB())
	f, err := fr.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	f.Status = domain.ForeshadowAbandoned
	if err := fr.Update(ctx, f); err != nil {
		return nil, err
	}
	return map[string]any{
		"foreshadow":     f,
		"auto_developed": []string{},
		"auto_resolved":  []string{},
	}, nil
}

// list dumps every foreshadow for the project. Used by the
// editor's "伏笔状态" sidebar.
func (t *foreshadowTool) list(ctx context.Context, mgr *project.Manager, p *domain.Project) (map[string]any, error) {
	fr := repo.NewForeshadowRepo(mgr.DB())
	all, err := fr.List(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, f := range all {
		counts[f.Status]++
	}
	return map[string]any{
		"foreshadows":     all,
		"count":           len(all),
		"status_breakdown": counts,
	}, nil
}

// autoDetect scans a freshly-written chapter for keyword hits
// against every planted/developing foreshadow in the project.
// Hits bump the row to "developing"; an explicit `resolve` flag
// in the input promotes to "resolved". This is the auto-pipeline
// the chapter_write tool calls after persisting a chapter.
func (t *foreshadowTool) autoDetect(ctx context.Context, mgr *project.Manager, p *domain.Project, input map[string]any) (map[string]any, error) {
	chapterID := stringField(input, "chapter_id")
	content := stringField(input, "content")
	if content == "" {
		return nil, fmt.Errorf("foreshadow: auto_detect requires content")
	}
	if err := ensureChapterExists(ctx, mgr, chapterID); err != nil {
		return nil, err
	}
	autoResolve := stringField(input, "resolve") == "true" || boolField(input, "resolve")
	fr := repo.NewForeshadowRepo(mgr.DB())
	all, err := fr.List(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	var developed, resolved []string
	for _, f := range all {
		if f.Status != domain.ForeshadowPlanted && f.Status != domain.ForeshadowDeveloping {
			continue
		}
		if !matchKeywords(content, f.Keywords) {
			continue
		}
		// Hit found. Either develop or resolve depending on
		// the caller's intent.
		if autoResolve {
			f.Status = domain.ForeshadowResolved
			f.ResolvedChapterID = chapterID
			resolved = append(resolved, f.ID)
		} else {
			if f.Status == domain.ForeshadowPlanted {
				f.Status = domain.ForeshadowDeveloping
				developed = append(developed, f.ID)
			}
		}
		if err := fr.Update(ctx, f); err != nil {
			return nil, err
		}
	}
	return map[string]any{
		"auto_developed": developed,
		"auto_resolved":  resolved,
		"scanned":        len(all),
	}, nil
}

// validImportance is the whitelist gate at the plant boundary.
// Unknown values get a clear error so the LLM can self-correct.
func validImportance(s string) bool {
	switch s {
	case domain.ForeshadowMinor, domain.ForeshadowMajor, domain.ForeshadowCritical:
		return true
	}
	return false
}

// matchKeywords returns true when any of the row's keywords
// appears as a substring in content. Empty keyword list → false
// (the tool does not invent matches).
func matchKeywords(content string, kws []string) bool {
	for _, k := range kws {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if strings.Contains(content, k) {
			return true
		}
	}
	return false
}

// ensureChapterExists validates the chapter_id against the
// chapters table. Centralised here so the four operation paths
// do not duplicate the SQL.
func ensureChapterExists(ctx context.Context, mgr *project.Manager, chapterID string) error {
	if chapterID == "" {
		return nil
	}
	var n int
	if err := mgr.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM chapters WHERE id = ?`, chapterID).Scan(&n); err != nil {
		return fmt.Errorf("foreshadow: check chapter: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("foreshadow: chapter %q does not exist", chapterID)
	}
	return nil
}

// boolField mirrors stringField for booleans — JSON booleans
// arrive as bool, but the LLM sometimes passes "true" / "false"
// strings. Centralising the coercion keeps the tool inputs
// forgiving.
func boolField(input map[string]any, key string) bool {
	v, ok := input[key]
	if !ok || v == nil {
		return false
	}
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return strings.EqualFold(strings.TrimSpace(b), "true")
	}
	return false
}
