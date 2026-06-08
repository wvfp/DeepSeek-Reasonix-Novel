// Package migrate imports a novel-plugin (TypeScript / sql.js) project
// into the Go novel-weaver format. The old project is a .novel-weaver/
// directory with a sql.js SQLite database and Markdown content files;
// the new project uses modernc.org/sqlite with a different schema.
//
// The migration is one-way and idempotent: re-running it on an already-
// migrated target is a no-op for rows that already exist (INSERT OR
// IGNORE on primary keys).
package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/novel/db"
	"reasonix/internal/novel/project"
)

// oldTableSet lists the novel-plugin tables we read from. Tables not
// listed here (annotations, chapter_summaries, genre_config) are
// skipped because the Go schema does not have an equivalent.
var oldTableSet = []string{
	"projects", "worlds", "characters", "arcs", "chapters",
	"reviews", "links", "progress", "chapter_facts",
	"character_states", "outlines", "aliases",
}

// Result holds the counts from a completed migration.
type Result struct {
	TablesMigrated int
	RowsMigrated   int
	FilesCopied    int
	SkippedTables  []string
	Errors         []string
}

// Run executes the migration from srcDir/.novel-weaver/ to
// dstDir/.novel-weaver/. The source must be a valid novel-plugin
// project; the destination is created if it does not exist.
//
// The source database is opened read-only; the target database is
// created and migrated before any data is written.
func Run(ctx context.Context, srcDir, dstDir string) (*Result, error) {
	srcRoot := filepath.Join(srcDir, project.DefaultDirName)
	dstRoot := filepath.Join(dstDir, project.DefaultDirName)

	srcDBPath := filepath.Join(srcRoot, "novel-weaver.db")
	// The novel-plugin may use either name; check both.
	if _, err := os.Stat(srcDBPath); err != nil {
		alt := filepath.Join(srcRoot, "novel.db")
		if _, err2 := os.Stat(alt); err2 == nil {
			srcDBPath = alt
		}
	}

	if _, err := os.Stat(srcDBPath); err != nil {
		return nil, fmt.Errorf("migrate: source database not found at %s: %w", srcDBPath, err)
	}

	// Open source DB read-only via modernc.org/sqlite (it can read
	// any SQLite file, including sql.js output).
	srcDB, err := sql.Open("sqlite", srcDBPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("migrate: open source db: %w", err)
	}
	defer srcDB.Close()

	// Create or open the target project.
	var mgr *project.Manager
	if _, err := os.Stat(dstRoot); err == nil {
		mgr, err = project.Open(dstDir)
	} else {
		mgr, err = project.New(dstDir)
	}
	if err != nil {
		return nil, fmt.Errorf("migrate: open target project: %w", err)
	}
	defer mgr.Close()

	res := &Result{}

	// Migrate each table.
	for _, table := range oldTableSet {
		n, err := migrateTable(ctx, srcDB, mgr.DB(), table)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", table, err))
			continue
		}
		if n >= 0 {
			res.TablesMigrated++
			res.RowsMigrated += n
		} else {
			res.SkippedTables = append(res.SkippedTables, table)
		}
	}

	// Copy Markdown content files.
	if n, err := copyContentFiles(srcRoot, mgr.Paths()); err != nil {
		res.Errors = append(res.Errors, fmt.Sprintf("content: %v", err))
	} else {
		res.FilesCopied = n
	}

	// Regenerate aliases from the migrated data.
	if err := regenerateAliases(ctx, mgr); err != nil {
		res.Errors = append(res.Errors, fmt.Sprintf("aliases: %v", err))
	}

	return res, nil
}

// migrateTable reads all rows from the source table and writes them
// into the target database with column mapping. Returns the number of
// rows written, or -1 if the source table does not exist.
func migrateTable(ctx context.Context, src *sql.DB, dst *db.DB, table string) (int, error) {
	// Check the source table exists.
	var count int
	row := src.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table)
	if err := row.Scan(&count); err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return -1, nil
		}
		return 0, fmt.Errorf("read %s: %w", table, err)
	}
	if count == 0 {
		return 0, nil
	}

	switch table {
	case "projects":
		return migrateProjects(ctx, src, dst)
	case "worlds":
		return migrateWorlds(ctx, src, dst)
	case "characters":
		return migrateCharacters(ctx, src, dst)
	case "arcs":
		return migrateArcs(ctx, src, dst)
	case "chapters":
		return migrateChapters(ctx, src, dst)
	case "reviews":
		return migrateReviews(ctx, src, dst)
	case "links":
		return migrateLinks(ctx, src, dst)
	case "progress":
		return migrateProgress(ctx, src, dst)
	case "chapter_facts":
		return migrateChapterFacts(ctx, src, dst)
	case "character_states":
		return migrateCharacterStates(ctx, src, dst)
	case "outlines":
		return migrateOutlines(ctx, src, dst)
	case "aliases":
		return migrateAliases(ctx, src, dst)
	default:
		return -1, nil
	}
}

// parseTime converts a novel-plugin TEXT timestamp (ISO 8601 or
// datetime('now') format) to a Unix timestamp. Falls back to the
// current time on parse failure.
func parseTime(s string) int64 {
	if s == "" {
		return time.Now().Unix()
	}
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		time.RFC3339,
		"2006-01-02T15:04:05Z",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Unix()
		}
	}
	return time.Now().Unix()
}

// projectID resolves the single project ID from the target DB. All
// novel-plugin tables reference a project; the Go schema does too.
func projectID(ctx context.Context, dst *db.DB) string {
	row := dst.QueryRowContext(ctx, "SELECT id FROM projects LIMIT 1")
	var id string
	if err := row.Scan(&id); err != nil {
		return "migrated"
	}
	return id
}

// slugify converts a name to a slug (lowercase, hyphens, no special
// chars). Matches the novel-plugin's slugify behaviour.
func slugify(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Per-table migration functions
// ---------------------------------------------------------------------------

func migrateProjects(ctx context.Context, src *sql.DB, dst *db.DB) (int, error) {
	rows, err := src.QueryContext(ctx, "SELECT id, name, genre, created_at, updated_at FROM projects")
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	n := 0
	for rows.Next() {
		var id, name, genre, createdAt, updatedAt string
		if err := rows.Scan(&id, &name, &genre, &createdAt, &updatedAt); err != nil {
			return n, err
		}
		if genre == "" {
			genre = "fantasy"
		}
		_, err := dst.ExecContext(ctx,
			`INSERT OR IGNORE INTO projects (id, name, genre, pipeline_phase, created_at, modified_at)
			 VALUES (?, ?, ?, 'setting', ?, ?)`,
			id, name, genre, parseTime(createdAt), parseTime(updatedAt))
		if err != nil {
			return n, fmt.Errorf("insert project %s: %w", id, err)
		}
		n++
	}
	return n, nil
}

func migrateWorlds(ctx context.Context, src *sql.DB, dst *db.DB) (int, error) {
	pid := projectID(ctx, dst)
	rows, err := src.QueryContext(ctx, "SELECT id, project_id, name, type, status, yaml_metadata FROM worlds")
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	n := 0
	for rows.Next() {
		var id, projectID, name, wType, status, yamlMeta string
		if err := rows.Scan(&id, &projectID, &name, &wType, &status, &yamlMeta); err != nil {
			return n, err
		}
		slug := slugify(name)
		description := ""
		content := ""
		metadata := yamlMeta
		now := time.Now().Unix()
		_, err := dst.ExecContext(ctx,
			`INSERT OR IGNORE INTO worlds (id, project_id, name, slug, description, content, metadata, created_at, modified_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, pid, name, slug, description, content, metadata, now, now)
		if err != nil {
			return n, fmt.Errorf("insert world %s: %w", id, err)
		}
		n++
	}
	return n, nil
}

func migrateCharacters(ctx context.Context, src *sql.DB, dst *db.DB) (int, error) {
	pid := projectID(ctx, dst)
	rows, err := src.QueryContext(ctx,
		`SELECT id, world_id, name, role_type, aliases, description, voice_fingerprint, address_chain FROM characters`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	n := 0
	for rows.Next() {
		var id, worldID, name, roleType, aliases, description, voiceFP, addrChain string
		if err := rows.Scan(&id, &worldID, &name, &roleType, &aliases, &description, &voiceFP, &addrChain); err != nil {
			return n, err
		}
		slug := slugify(name)
		// Merge voice_fingerprint and address_chain into voice_profile.
		voiceProfile := voiceFP
		if voiceProfile == "" || voiceProfile == "{}" {
			voiceProfile = addrChain
		}
		now := time.Now().Unix()
		_, err := dst.ExecContext(ctx,
			`INSERT OR IGNORE INTO characters (id, project_id, name, slug, description, voice_profile, content, created_at, modified_at)
			 VALUES (?, ?, ?, ?, ?, ?, '', ?, ?)`,
			id, pid, name, slug, description, voiceProfile, now, now)
		if err != nil {
			return n, fmt.Errorf("insert character %s: %w", id, err)
		}
		n++
	}
	return n, nil
}

func migrateArcs(ctx context.Context, src *sql.DB, dst *db.DB) (int, error) {
	pid := projectID(ctx, dst)
	rows, err := src.QueryContext(ctx,
		`SELECT id, world_id, name, arc_type, theme, genre_id, difficulty, rules, rewards, status FROM arcs`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	n := 0
	for rows.Next() {
		var id, worldID, name, arcType, theme, genreID string
		var difficulty int
		var rules, rewards, status string
		if err := rows.Scan(&id, &worldID, &name, &arcType, &theme, &genreID, &difficulty, &rules, &rewards, &status); err != nil {
			return n, err
		}
		// Map arc_type to outline level.
		level := "chapter"
		switch arcType {
		case "campaign":
			level = "master"
		case "storyline":
			level = "volume"
		case "dungeon", "trial", "quest":
			level = "chapter"
		}
		metadata := rules
		now := time.Now().Unix()
		_, err := dst.ExecContext(ctx,
			`INSERT OR IGNORE INTO outlines (id, project_id, parent_id, level, title, summary, order_index, metadata, created_at, modified_at)
			 VALUES (?, ?, NULL, ?, ?, ?, 0, ?, ?, ?)`,
			id, pid, level, name, rewards, metadata, now, now)
		if err != nil {
			return n, fmt.Errorf("insert arc→outline %s: %w", id, err)
		}
		n++
	}
	return n, nil
}

func migrateChapters(ctx context.Context, src *sql.DB, dst *db.DB) (int, error) {
	pid := projectID(ctx, dst)
	rows, err := src.QueryContext(ctx,
		`SELECT id, arc_id, volume_num, chapter_num, title, word_count, status FROM chapters`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	n := 0
	for rows.Next() {
		var id, arcID, title, status string
		var volumeNum, chapterNum, wordCount int
		if err := rows.Scan(&id, &arcID, &volumeNum, &chapterNum, &title, &wordCount, &status); err != nil {
			return n, err
		}
		slug := slugify(title)
		now := time.Now().Unix()
		_, err := dst.ExecContext(ctx,
			`INSERT OR IGNORE INTO chapters (id, project_id, arc_id, volume, chapter_number, title, slug, content, word_count, status, created_at, modified_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, '', ?, ?, ?, ?)`,
			id, pid, arcID, volumeNum, chapterNum, title, slug, wordCount, status, now, now)
		if err != nil {
			return n, fmt.Errorf("insert chapter %s: %w", id, err)
		}
		n++
	}
	return n, nil
}

func migrateReviews(ctx context.Context, src *sql.DB, dst *db.DB) (int, error) {
	rows, err := src.QueryContext(ctx,
		`SELECT id, chapter_id, reviewer, issues, verdict FROM reviews`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	n := 0
	for rows.Next() {
		var id, chapterID, reviewer, issues, verdict string
		if err := rows.Scan(&id, &chapterID, &reviewer, &issues, &verdict); err != nil {
			return n, err
		}
		dimension := reviewer
		if dimension == "" {
			dimension = "overall"
		}
		score := 5.0
		if verdict == "pass" {
			score = 8.0
		} else if verdict == "pending" {
			score = 5.0
		} else if verdict == "fail" {
			score = 3.0
		}
		now := time.Now().Unix()
		_, err := dst.ExecContext(ctx,
			`INSERT OR IGNORE INTO reviews (id, chapter_id, dimension, score, issues, created_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			id, chapterID, dimension, score, issues, now)
		if err != nil {
			return n, fmt.Errorf("insert review %s: %w", id, err)
		}
		n++
	}
	return n, nil
}

func migrateLinks(ctx context.Context, src *sql.DB, dst *db.DB) (int, error) {
	pid := projectID(ctx, dst)
	rows, err := src.QueryContext(ctx,
		`SELECT id, source_file, target_file, link_type, created_at FROM links`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	n := 0
	for rows.Next() {
		var id, sourceFile, targetFile, linkType, createdAt string
		if err := rows.Scan(&id, &sourceFile, &targetFile, &linkType, &createdAt); err != nil {
			return n, err
		}
		// Derive source/target types from file names.
		srcType := entityTypeFromFilename(sourceFile)
		tgtType := entityTypeFromFilename(targetFile)
		srcID := entityIDFromFilename(sourceFile)
		tgtID := entityIDFromFilename(targetFile)
		now := time.Now().Unix()
		_, err := dst.ExecContext(ctx,
			`INSERT OR IGNORE INTO entity_links (id, project_id, source_type, source_id, target_type, target_id, link_type, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			id, pid, srcType, srcID, tgtType, tgtID, linkType, now)
		if err != nil {
			return n, fmt.Errorf("insert link %s: %w", id, err)
		}
		n++
	}
	return n, nil
}

func migrateProgress(ctx context.Context, src *sql.DB, dst *db.DB) (int, error) {
	// The novel-plugin progress table maps to pipeline_phase in
	// the projects table. We read the latest completed step and
	// set the phase accordingly.
	rows, err := src.QueryContext(ctx,
		`SELECT step_name, completed FROM progress ORDER BY completed_at DESC LIMIT 1`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	if rows.Next() {
		var stepName string
		var completed int
		if err := rows.Scan(&stepName, &completed); err != nil {
			return 0, err
		}
		if completed == 1 {
			phase := "setting"
			switch {
			case strings.Contains(stepName, "writ"):
				phase = "writing"
			case strings.Contains(stepName, "review"):
				phase = "reviewing"
			case strings.Contains(stepName, "plan"):
				phase = "planning"
			}
			dst.ExecContext(ctx, `UPDATE projects SET pipeline_phase = ?`, phase)
		}
	}
	return 0, nil
}

func migrateChapterFacts(ctx context.Context, src *sql.DB, dst *db.DB) (int, error) {
	pid := projectID(ctx, dst)
	rows, err := src.QueryContext(ctx,
		`SELECT id, chapter_id, fact_type, entity_ref, description, chapter_num, created_at FROM chapter_facts`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	n := 0
	for rows.Next() {
		var id, chapterID, factType, entityRef, description, createdAt string
		var chapterNum int
		if err := rows.Scan(&id, &chapterID, &factType, &entityRef, &description, &chapterNum, &createdAt); err != nil {
			return n, err
		}
		subject := entityRef
		if subject == "" {
			subject = factType
		}
		now := time.Now().Unix()
		_, err := dst.ExecContext(ctx,
			`INSERT OR IGNORE INTO chapter_facts (id, project_id, chapter_id, fact_type, subject, predicate, object, confidence, context, created_at)
			 VALUES (?, ?, ?, ?, ?, 'is', ?, 1.0, '', ?)`,
			id, pid, chapterID, factType, subject, description, now)
		if err != nil {
			return n, fmt.Errorf("insert chapter_fact %s: %w", id, err)
		}
		n++
	}
	return n, nil
}

func migrateCharacterStates(ctx context.Context, src *sql.DB, dst *db.DB) (int, error) {
	pid := projectID(ctx, dst)
	rows, err := src.QueryContext(ctx,
		`SELECT id, character_id, chapter_id, chapter_num, status_tags, power_level, location, items, relationships, narrative_state, context, created_at FROM character_states`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	n := 0
	for rows.Next() {
		var id, characterID, chapterID string
		var chapterNum int
		var statusTags, powerLevel, location, items, relationships, narrativeState, ctxStr, createdAt string
		if err := rows.Scan(&id, &characterID, &chapterID, &chapterNum, &statusTags, &powerLevel, &location, &items, &relationships, &narrativeState, &ctxStr, &createdAt); err != nil {
			return n, err
		}
		// Merge all state fields into a single JSON snapshot.
		snapshot := fmt.Sprintf(`{"tags":%s,"power":"%s","location":"%s","items":%s,"relationships":%s,"narrative":"%s","context":"%s"}`,
			coalesceJSON(statusTags), powerLevel, location, coalesceJSON(items), coalesceJSON(relationships), narrativeState, ctxStr)
		now := time.Now().Unix()
		_, err := dst.ExecContext(ctx,
			`INSERT OR IGNORE INTO character_states (id, project_id, character_id, chapter_id, snapshot, tags, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, pid, characterID, chapterID, snapshot, statusTags, now)
		if err != nil {
			return n, fmt.Errorf("insert character_state %s: %w", id, err)
		}
		n++
	}
	return n, nil
}

func migrateOutlines(ctx context.Context, src *sql.DB, dst *db.DB) (int, error) {
	pid := projectID(ctx, dst)
	rows, err := src.QueryContext(ctx,
		`SELECT id, arc_id, outline_type, level, title, summary, content, status, order_num, created_at FROM outlines`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	n := 0
	for rows.Next() {
		var id, arcID, outlineType string
		var level int
		var title, summary, content, status string
		var orderNum int
		var createdAt string
		if err := rows.Scan(&id, &arcID, &outlineType, &level, &title, &summary, &content, &status, &orderNum, &createdAt); err != nil {
			return n, err
		}
		now := time.Now().Unix()
		_, err := dst.ExecContext(ctx,
			`INSERT OR IGNORE INTO outlines (id, project_id, parent_id, level, title, summary, order_index, metadata, created_at, modified_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, '', ?, ?)`,
			id, pid, arcID, outlineType, title, summary, orderNum, now, now)
		if err != nil {
			return n, fmt.Errorf("insert outline %s: %w", id, err)
		}
		n++
	}
	return n, nil
}

func migrateAliases(ctx context.Context, src *sql.DB, dst *db.DB) (int, error) {
	pid := projectID(ctx, dst)
	rows, err := src.QueryContext(ctx,
		`SELECT id, entity_id, alias, entity_type, confidence, created_at FROM aliases`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	n := 0
	for rows.Next() {
		var id, entityID, alias, entityType string
		var confidence float64
		var createdAt string
		if err := rows.Scan(&id, &entityID, &alias, &entityType, &confidence, &createdAt); err != nil {
			return n, err
		}
		now := time.Now().Unix()
		_, err := dst.ExecContext(ctx,
			`INSERT OR IGNORE INTO aliases (id, project_id, entity_type, entity_id, alias, created_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			id, pid, entityType, entityID, alias, now)
		if err != nil {
			return n, fmt.Errorf("insert alias %s: %w", id, err)
		}
		n++
	}
	return n, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// entityTypeFromFilename derives an entity type from a novel-plugin
// Markdown filename (e.g. "world-xxx.md" → "world").
func entityTypeFromFilename(filename string) string {
	base := filepath.Base(filename)
	base = strings.TrimSuffix(base, ".md")
	if idx := strings.Index(base, "-"); idx >= 0 {
		return base[:idx]
	}
	return "unknown"
}

// entityIDFromFilename extracts the entity ID from a filename.
// The novel-plugin uses the full stem as ID.
func entityIDFromFilename(filename string) string {
	base := filepath.Base(filename)
	return strings.TrimSuffix(base, ".md")
}

// coalesceJSON returns s if it looks like valid JSON, else "null".
func coalesceJSON(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s == "undefined" {
		return "null"
	}
	if strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[") || s == "null" {
		return s
	}
	return "null"
}

// copyContentFiles copies Markdown and asset files from the old
// content/ directory to the new one. Returns the number of files copied.
func copyContentFiles(srcRoot string, dstPaths *project.Paths) (int, error) {
	srcContent := filepath.Join(srcRoot, "content")
	if _, err := os.Stat(srcContent); err != nil {
		return 0, nil // no content dir, skip
	}

	n := 0
	err := filepath.WalkDir(srcContent, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcContent, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dstPaths.Content, rel)
		if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dstPath, data, 0o644); err != nil {
			return err
		}
		n++
		return nil
	})
	return n, err
}

// regenerateAliases scans worlds and characters and inserts alias rows
// for their names and slugs. This is a best-effort pass — the user can
// add more aliases later via the alias tool.
func regenerateAliases(ctx context.Context, mgr *project.Manager) error {
	pid := projectID(ctx, mgr.DB())

	// World name → alias.
	rows, err := mgr.DB().QueryContext(ctx, "SELECT id, name, slug FROM worlds")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name, slug string
		if err := rows.Scan(&id, &name, &slug); err != nil {
			continue
		}
		mgr.DB().ExecContext(ctx,
			`INSERT OR IGNORE INTO aliases (id, project_id, entity_type, entity_id, alias, created_at)
			 VALUES (?, ?, 'world', ?, ?, ?)`,
			"alias-w-"+id, pid, id, name, time.Now().Unix())
		if slug != "" && slug != name {
			mgr.DB().ExecContext(ctx,
				`INSERT OR IGNORE INTO aliases (id, project_id, entity_type, entity_id, alias, created_at)
				 VALUES (?, ?, 'world', ?, ?, ?)`,
				"alias-ws-"+id, pid, id, slug, time.Now().Unix())
		}
	}

	// Character name → alias.
	rows2, err := mgr.DB().QueryContext(ctx, "SELECT id, name, slug FROM characters")
	if err != nil {
		return err
	}
	defer rows2.Close()
	for rows2.Next() {
		var id, name, slug string
		if err := rows2.Scan(&id, &name, &slug); err != nil {
			continue
		}
		mgr.DB().ExecContext(ctx,
			`INSERT OR IGNORE INTO aliases (id, project_id, entity_type, entity_id, alias, created_at)
			 VALUES (?, ?, 'character', ?, ?, ?)`,
			"alias-c-"+id, pid, id, name, time.Now().Unix())
		if slug != "" && slug != name {
			mgr.DB().ExecContext(ctx,
				`INSERT OR IGNORE INTO aliases (id, project_id, entity_type, entity_id, alias, created_at)
				 VALUES (?, ?, 'character', ?, ?, ?)`,
				"alias-cs-"+id, pid, id, slug, time.Now().Unix())
		}
	}
	return nil
}
