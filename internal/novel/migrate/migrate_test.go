package migrate

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/novel/project"
)

// setupSourceDB creates a temporary SQLite database with the
// novel-plugin schema and some seed data.
func setupSourceDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, project.DefaultDirName)
	content := filepath.Join(root, "content")
	for _, d := range []string{
		filepath.Join(content, "settings"),
		filepath.Join(content, "chapters", "vol-1"),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	dbPath := filepath.Join(root, "novel-weaver.db")
	ddb, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ddb.Close()

	// Create the novel-plugin schema (simplified).
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS projects (id TEXT PRIMARY KEY, name TEXT NOT NULL, genre TEXT NOT NULL DEFAULT 'fantasy', created_at TEXT, updated_at TEXT)`,
		`CREATE TABLE IF NOT EXISTS worlds (id TEXT PRIMARY KEY, project_id TEXT NOT NULL, name TEXT NOT NULL, type TEXT NOT NULL DEFAULT 'primary', status TEXT NOT NULL DEFAULT 'active', yaml_metadata TEXT)`,
		`CREATE TABLE IF NOT EXISTS characters (id TEXT PRIMARY KEY, world_id TEXT NOT NULL, name TEXT NOT NULL, role_type TEXT NOT NULL DEFAULT 'npc', aliases TEXT, description TEXT, voice_fingerprint TEXT DEFAULT '{}', address_chain TEXT DEFAULT '{}')`,
		`CREATE TABLE IF NOT EXISTS arcs (id TEXT PRIMARY KEY, world_id TEXT NOT NULL, name TEXT NOT NULL, arc_type TEXT NOT NULL DEFAULT 'storyline', theme TEXT NOT NULL DEFAULT 'generic', genre_id TEXT, difficulty INTEGER NOT NULL DEFAULT 1, rules TEXT, rewards TEXT, status TEXT NOT NULL DEFAULT 'locked')`,
		`CREATE TABLE IF NOT EXISTS chapters (id TEXT PRIMARY KEY, arc_id TEXT NOT NULL, volume_num INTEGER NOT NULL DEFAULT 1, chapter_num INTEGER NOT NULL DEFAULT 1, title TEXT NOT NULL, word_count INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL DEFAULT 'draft')`,
		`CREATE TABLE IF NOT EXISTS reviews (id TEXT PRIMARY KEY, chapter_id TEXT NOT NULL, reviewer TEXT NOT NULL, issues TEXT, verdict TEXT NOT NULL DEFAULT 'pending', reviewed_at TEXT)`,
		`CREATE TABLE IF NOT EXISTS links (id TEXT PRIMARY KEY, source_file TEXT NOT NULL, target_file TEXT NOT NULL, link_type TEXT NOT NULL DEFAULT 'reference', created_at TEXT)`,
		`CREATE TABLE IF NOT EXISTS progress (id TEXT PRIMARY KEY, arc_id TEXT NOT NULL, step_name TEXT NOT NULL, completed INTEGER NOT NULL DEFAULT 0, completed_at TEXT)`,
		`CREATE TABLE IF NOT EXISTS chapter_facts (id TEXT PRIMARY KEY, chapter_id TEXT NOT NULL, fact_type TEXT NOT NULL, entity_ref TEXT, description TEXT NOT NULL, chapter_num INTEGER NOT NULL, created_at TEXT)`,
		`CREATE TABLE IF NOT EXISTS character_states (id TEXT PRIMARY KEY, character_id TEXT NOT NULL, chapter_id TEXT NOT NULL, chapter_num INTEGER NOT NULL, status_tags TEXT, power_level TEXT, location TEXT, items TEXT, relationships TEXT, narrative_state TEXT, context TEXT, created_at TEXT)`,
		`CREATE TABLE IF NOT EXISTS outlines (id TEXT PRIMARY KEY, arc_id TEXT NOT NULL, outline_type TEXT NOT NULL, level INTEGER NOT NULL DEFAULT 1, title TEXT NOT NULL, summary TEXT, content TEXT, status TEXT NOT NULL DEFAULT 'draft', order_num INTEGER NOT NULL DEFAULT 0, created_at TEXT)`,
		`CREATE TABLE IF NOT EXISTS aliases (id TEXT PRIMARY KEY, entity_id TEXT NOT NULL, alias TEXT NOT NULL, entity_type TEXT NOT NULL, confidence REAL NOT NULL DEFAULT 1.0, created_at TEXT)`,
		`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY, applied_at TEXT)`,
	}
	for _, s := range stmts {
		if _, err := ddb.Exec(s); err != nil {
			t.Fatalf("create source table: %v\n%s", err, s)
		}
	}

	// Seed data.
	ddb.Exec(`INSERT INTO projects (id, name, genre, created_at, updated_at) VALUES ('p1', '测试小说', 'xianxia', '2025-01-01 00:00:00', '2025-01-02 00:00:00')`)
	ddb.Exec(`INSERT INTO worlds (id, project_id, name, type, status, yaml_metadata) VALUES ('w1', 'p1', '九州大陆', 'primary', 'active', '{"era":"上古"}')`)
	ddb.Exec(`INSERT INTO characters (id, world_id, name, role_type, aliases, description, voice_fingerprint, address_chain) VALUES ('c1', 'w1', '李逍遥', 'protagonist', '["逍遥"]', '剑修', '{"catchphrases":["道友"]}', '{}')`)
	ddb.Exec(`INSERT INTO arcs (id, world_id, name, arc_type, theme, genre_id, difficulty, rules, rewards, status) VALUES ('a1', 'w1', '仙门试炼', 'dungeon', 'xianxia', 'xianxia', 5, '{"time_limit":7}', '{"cultivation_boost":1}', 'active')`)
	ddb.Exec(`INSERT INTO chapters (id, arc_id, volume_num, chapter_num, title, word_count, status) VALUES ('ch1', 'a1', 1, 1, '初入仙门', 3000, 'draft')`)
	ddb.Exec(`INSERT INTO reviews (id, chapter_id, reviewer, issues, verdict, reviewed_at) VALUES ('r1', 'ch1', 'plot', '节奏偏慢', 'fail', '2025-01-03 00:00:00')`)
	ddb.Exec(`INSERT INTO chapter_facts (id, chapter_id, fact_type, entity_ref, description, chapter_num, created_at) VALUES ('f1', 'ch1', 'new_character', 'c1', '李逍遥登场', 1, '2025-01-03 00:00:00')`)
	ddb.Exec(`INSERT INTO aliases (id, entity_id, alias, entity_type, confidence, created_at) VALUES ('al1', 'c1', '逍遥', 'character', 0.9, '2025-01-01 00:00:00')`)

	// Write a sample Markdown file.
	mdPath := filepath.Join(content, "settings", "world-jiuzhou.md")
	os.WriteFile(mdPath, []byte("# 九州大陆\n\n上古世界。"), 0o644)

	return dir
}

func TestRun_MigratesAllTables(t *testing.T) {
	srcDir := setupSourceDB(t)
	dstDir := t.TempDir()

	res, err := Run(context.Background(), srcDir, dstDir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if res.TablesMigrated == 0 {
		t.Error("expected at least 1 table migrated")
	}
	if res.RowsMigrated == 0 {
		t.Error("expected at least 1 row migrated")
	}
	if len(res.Errors) > 0 {
		t.Errorf("unexpected errors: %v", res.Errors)
	}

	// Verify target project exists and has data.
	mgr, err := project.Open(dstDir)
	if err != nil {
		t.Fatalf("open target: %v", err)
	}
	defer mgr.Close()

	// Check projects row.
	p, err := mgr.Project(context.Background())
	if err != nil {
		t.Fatalf("read project: %v", err)
	}
	if p.Name != "测试小说" {
		t.Errorf("project name = %q, want %q", p.Name, "测试小说")
	}
	if p.Genre != "xianxia" {
		t.Errorf("project genre = %q, want %q", p.Genre, "xianxia")
	}

	// Check worlds row.
	var worldCount int
	row := mgr.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM worlds")
	row.Scan(&worldCount)
	if worldCount != 1 {
		t.Errorf("worlds count = %d, want 1", worldCount)
	}

	// Check characters row.
	var charCount int
	row = mgr.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM characters")
	row.Scan(&charCount)
	if charCount != 1 {
		t.Errorf("characters count = %d, want 1", charCount)
	}

	// Check chapters row.
	var chapCount int
	row = mgr.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM chapters")
	row.Scan(&chapCount)
	if chapCount != 1 {
		t.Errorf("chapters count = %d, want 1", chapCount)
	}

	// Check arcs migrated to outlines.
	var outlineCount int
	row = mgr.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM outlines")
	row.Scan(&outlineCount)
	if outlineCount == 0 {
		t.Error("expected at least 1 outline (from arcs)")
	}

	// Check content file was copied.
	mdPath := filepath.Join(mgr.Paths().Settings, "world-jiuzhou.md")
	if _, err := os.Stat(mdPath); err != nil {
		t.Errorf("content file not copied: %v", err)
	}
	if res.FilesCopied == 0 {
		t.Error("expected at least 1 file copied")
	}
}

func TestRun_Idempotent(t *testing.T) {
	srcDir := setupSourceDB(t)
	dstDir := t.TempDir()

	// Run twice.
	res1, err := Run(context.Background(), srcDir, dstDir)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	res2, err := Run(context.Background(), srcDir, dstDir)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}

	// Second run should not duplicate rows (INSERT OR IGNORE).
	if res2.RowsMigrated > res1.RowsMigrated {
		t.Errorf("second run migrated %d rows, expected 0 (idempotent)", res2.RowsMigrated)
	}
}

func TestRun_SourceNotFound(t *testing.T) {
	dstDir := t.TempDir()
	_, err := Run(context.Background(), "/nonexistent/path", dstDir)
	if err == nil {
		t.Error("expected error for missing source")
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Hello World", "hello-world"},
		{"九州大陆", ""},
		{"Test 123!", "test-123"},
		{"a-b_c", "a-b-c"},
	}
	for _, tt := range tests {
		got := slugify(tt.input)
		if got != tt.want {
			t.Errorf("slugify(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestEntityTypeFromFilename(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"world-jiuzhou.md", "world"},
		{"char-lixiaoyao.md", "char"},
		{"ch1-title.md", "ch1"},
		{"unknown.md", "unknown"},
	}
	for _, tt := range tests {
		got := entityTypeFromFilename(tt.input)
		if got != tt.want {
			t.Errorf("entityTypeFromFilename(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseTime(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"2025-01-01 00:00:00", 1735689600},
		{"", 0}, // empty → now, just check it doesn't crash
	}
	for _, tt := range tests {
		got := parseTime(tt.input)
		if tt.want != 0 && got != tt.want {
			t.Errorf("parseTime(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestCoalesceJSON(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`{"a":1}`, `{"a":1}`},
		{`[1,2]`, `[1,2]`},
		{"", "null"},
		{"undefined", "null"},
		{"null", "null"},
		{"hello", "null"},
	}
	for _, tt := range tests {
		got := coalesceJSON(tt.input)
		if got != tt.want {
			t.Errorf("coalesceJSON(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestMigrateProgress_SetsPhase(t *testing.T) {
	srcDir := t.TempDir()
	srcRoot := filepath.Join(srcDir, project.DefaultDirName)
	os.MkdirAll(filepath.Join(srcRoot, "content"), 0o755)

	dbPath := filepath.Join(srcRoot, "novel-weaver.db")
	ddb, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ddb.Close()

	ddb.Exec(`CREATE TABLE IF NOT EXISTS projects (id TEXT PRIMARY KEY, name TEXT NOT NULL, genre TEXT NOT NULL DEFAULT 'fantasy', created_at TEXT, updated_at TEXT)`)
	ddb.Exec(`CREATE TABLE IF NOT EXISTS progress (id TEXT PRIMARY KEY, arc_id TEXT NOT NULL, step_name TEXT NOT NULL, completed INTEGER NOT NULL DEFAULT 0, completed_at TEXT)`)
	ddb.Exec(`INSERT INTO projects (id, name, genre, created_at, updated_at) VALUES ('p1', 'Test', 'fantasy', '2025-01-01 00:00:00', '2025-01-01 00:00:00')`)
	ddb.Exec(`INSERT INTO progress (id, arc_id, step_name, completed, completed_at) VALUES ('pr1', 'a1', 'writing_chapter_5', 1, '2025-01-05 00:00:00')`)

	dstDir := t.TempDir()
	res, err := Run(context.Background(), srcDir, dstDir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	mgr, err := project.Open(dstDir)
	if err != nil {
		t.Fatalf("open target: %v", err)
	}
	defer mgr.Close()

	p, err := mgr.Project(context.Background())
	if err != nil {
		t.Fatalf("read project: %v", err)
	}
	if p.PipelinePhase != "writing" {
		t.Errorf("pipeline_phase = %q, want %q", p.PipelinePhase, "writing")
	}
	_ = res
}
