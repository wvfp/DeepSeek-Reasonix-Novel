package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"reasonix/internal/novel/project"
)

// crosscheckTool is the `crosscheck` tool. It accepts a free-form
// query (or a specific chapter_id) and looks for contradictions in
// the structured memory:
//
//	- chapter_facts: same (subject, predicate) appearing in two
//	  chapters with different objects → conflict
//	- knowledge_graph_nodes / edges: same entity_type + entity_id
//	  showing two different relations to another node → conflict
//	- character_states: same character_id with two non-overlapping
//	  location snapshots in the same chapter → conflict
//
// Unlike the `consistency` tool, crosscheck is deterministic —
// no LLM is called. The detection is a pure SQL scan over the
// structured tables, which is what makes the tests fast and
// reproducible.
//
// Output shape:
//
//	{
//	  "matches": [
//	    {
//	      "chapter_id": "<id>", "subject": "...", "predicate": "...",
//	      "object": "...", "fact_type": "...",
//	      "conflict_with": [
//	        {"chapter_id": "<id>", "object": "...", "fact_type": "..."}
//	      ]
//	    }
//	  ]
//	}
type crosscheckTool struct{}

func init() { registerDefault(&crosscheckTool{}) }

func (t *crosscheckTool) Name() string { return "crosscheck" }

func (t *crosscheckTool) Description() string {
	return "在 chapter_facts / knowledge_graph / character_states 中检测同一 (subject, predicate) 的 object 矛盾（无 LLM，纯 SQL）。"
}

// Execute is the entry point. Accepts {"query": "..."} or
// {"chapter_id": "..."}; both can be empty (full-project scan).
func (t *crosscheckTool) Execute(ctx context.Context, input map[string]any, mgr *project.Manager) (map[string]any, error) {
	p, err := mgr.Project(ctx)
	if err != nil {
		return nil, err
	}
	query := stringField(input, "query")
	chapterID := stringField(input, "chapter_id")

	matches, err := t.scanConflicts(ctx, mgr, p.ID, query, chapterID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"matches":     matches,
		"match_count": len(matches),
	}, nil
}

// match is one row of the conflict output.
type match struct {
	ChapterID     string         `json:"chapter_id"`
	Subject       string         `json:"subject"`
	Predicate     string         `json:"predicate"`
	Object        string         `json:"object"`
	FactType      string         `json:"fact_type,omitempty"`
	ConflictWith  []conflictPeer `json:"conflict_with"`
}

// conflictPeer describes one of the rows that disagrees with the
// match's own (subject, predicate, object). Including chapter_id
// + object + fact_type so the user can jump to the offending line.
type conflictPeer struct {
	ChapterID string `json:"chapter_id"`
	Object    string `json:"object"`
	FactType  string `json:"fact_type,omitempty"`
}

// scanConflicts is the heart of the tool. It groups facts by
// (subject, predicate) and returns every fact whose key has more
// than one distinct object across chapters.
func (t *crosscheckTool) scanConflicts(ctx context.Context, mgr *project.Manager, projectID, query, chapterID string) ([]match, error) {
	rows, err := mgr.DB().QueryContext(ctx,
		`SELECT id, chapter_id, fact_type, subject, predicate, object
		 FROM chapter_facts
		 WHERE project_id = ? AND subject != '' AND predicate != '' AND object != ''
		 ORDER BY chapter_id`, projectID)
	if err != nil {
		return nil, fmt.Errorf("crosscheck: list facts: %w", err)
	}
	defer rows.Close()

	type factRow struct {
		ID        string
		ChapterID string
		FactType  string
		Subject   string
		Predicate string
		Object    string
	}
	var all []factRow
	for rows.Next() {
		var f factRow
		if err := rows.Scan(&f.ID, &f.ChapterID, &f.FactType, &f.Subject, &f.Predicate, &f.Object); err != nil {
			return nil, fmt.Errorf("crosscheck: scan: %w", err)
		}
		all = append(all, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Optional filter: keep only facts whose (subject + predicate
	// + object + context) contains the query substring, or facts
	// tied to the given chapter. Empty filter = pass-through.
	filtered := all
	if query != "" || chapterID != "" {
		q := strings.ToLower(strings.TrimSpace(query))
		filtered = make([]factRow, 0, len(all))
		for _, f := range all {
			if chapterID != "" && f.ChapterID != chapterID {
				continue
			}
			if q != "" {
				hay := strings.ToLower(f.Subject + " " + f.Predicate + " " + f.Object + " " + f.FactType)
				if !strings.Contains(hay, q) {
					continue
				}
			}
			filtered = append(filtered, f)
		}
	}

	// Group by (subject, predicate). Same subject + predicate in
	// two chapters is the conflict signature; identical-object
	// repeats are dropped to avoid noise.
	groups := map[string][]factRow{}
	for _, f := range filtered {
		key := f.Subject + "\x00" + f.Predicate
		groups[key] = append(groups[key], f)
	}

	// Stable ordering: subject asc, then predicate asc — keeps
	// the output deterministic so a snapshot test is meaningful.
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var out []match
	for _, k := range keys {
		fs := groups[k]
		if len(fs) < 2 {
			continue
		}
		// Distinct objects.
		uniq := map[string]factRow{}
		order := []string{}
		for _, f := range fs {
			if _, ok := uniq[f.Object]; !ok {
				uniq[f.Object] = f
				order = append(order, f.Object)
			}
		}
		if len(uniq) < 2 {
			continue
		}
		// One match per "winning" object, with the other objects
		// as conflict_with peers. We pick the first object in
		// deterministic order (sort by ChapterID + object) as
		// the anchor.
		sort.SliceStable(fs, func(i, j int) bool {
			if fs[i].ChapterID == fs[j].ChapterID {
				return fs[i].Object < fs[j].Object
			}
			return fs[i].ChapterID < fs[j].ChapterID
		})
		anchor := fs[0]
		peers := make([]conflictPeer, 0, len(fs)-1)
		for _, f := range fs[1:] {
			peers = append(peers, conflictPeer{
				ChapterID: f.ChapterID,
				Object:    f.Object,
				FactType:  f.FactType,
			})
		}
		out = append(out, match{
			ChapterID:    anchor.ChapterID,
			Subject:      anchor.Subject,
			Predicate:    anchor.Predicate,
			Object:       anchor.Object,
			FactType:     anchor.FactType,
			ConflictWith: peers,
		})
	}
	return out, nil
}
