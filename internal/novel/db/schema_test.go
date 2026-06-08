package db

import (
	"context"
	"path/filepath"
	"sort"
	"testing"
)

func TestMigrate_FreshDatabase(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	if v, err := d.SchemaVersion(); err != nil {
		t.Fatalf("SchemaVersion before migrate: %v", err)
	} else if v != 0 {
		t.Fatalf("SchemaVersion before migrate = %d, want 0", v)
	}

	if err := d.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	if v, err := d.SchemaVersion(); err != nil {
		t.Fatalf("SchemaVersion after migrate: %v", err)
	} else if v != CurrentSchemaVersion {
		t.Fatalf("SchemaVersion = %d, want %d", v, CurrentSchemaVersion)
	}
}

func TestMigrate_CreatesAllExpectedTables(t *testing.T) {
	d := openTestDB(t)
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	got := listUserTables(t, d)
	want := append([]string{}, ExpectedTables...)
	sort.Strings(want)
	sort.Strings(got)

	if !equalStringSlices(got, want) {
		t.Fatalf("table list mismatch\n got:  %v\n want: %v", got, want)
	}
}

func TestMigrate_CreatesAllExpectedIndexes(t *testing.T) {
	d := openTestDB(t)
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	got := listUserIndexes(t, d)
	want := append([]string{}, ExpectedIndexes...)
	sort.Strings(want)
	sort.Strings(got)

	if !equalStringSlices(got, want) {
		t.Fatalf("index list mismatch\n got:  %v\n want: %v", got, want)
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	dir := t.TempDir()
	d, err := Open(filepath.Join(dir, "idempotent.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := d.Migrate(ctx); err != nil {
			t.Fatalf("Migrate iteration %d: %v", i, err)
		}
	}
	if v, err := d.SchemaVersion(); err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	} else if v != CurrentSchemaVersion {
		t.Errorf("SchemaVersion after re-migrate = %d, want %d", v, CurrentSchemaVersion)
	}
}

func TestMigrate_NilDB(t *testing.T) {
	var d *DB
	if err := d.Migrate(context.Background()); err == nil {
		t.Fatal("Migrate on nil DB should fail, got nil error")
	}
}

func TestSchemaVersion_NilDB(t *testing.T) {
	var d *DB
	if _, err := d.SchemaVersion(); err == nil {
		t.Fatal("SchemaVersion on nil DB should fail, got nil error")
	}
}

func TestMigrate_FKEnforcement(t *testing.T) {
	d := openTestDB(t)
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Insert a chapter that points to a non-existent project_id. With
	// PRAGMA foreign_keys=ON the insert must fail.
	_, err := d.ExecContext(context.Background(),
		`INSERT INTO chapters (id, project_id, volume, chapter_number, title, slug, created_at, modified_at)
		 VALUES ('c1', 'no-such-project', 1, 1, 't', 's', 0, 0)`)
	if err == nil {
		t.Fatal("expected FK violation when inserting chapter with unknown project_id, got nil")
	}
}

func TestMigrateV2ToV3_CreatesNewTables(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Simulate a v2 database: apply only the tables/indexes that existed before v3.
	for _, stmt := range v2TablesAndIndexes() {
		if _, err := d.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("setup v2 schema: %v", err)
		}
	}
	if err := d.upsertVersion(ctx, 2); err != nil {
		t.Fatalf("set version to 2: %v", err)
	}

	if v, err := d.SchemaVersion(); err != nil {
		t.Fatalf("SchemaVersion before migration: %v", err)
	} else if v != 2 {
		t.Fatalf("SchemaVersion = %d, want 2", v)
	}

	if err := d.MigrateV2ToV3(ctx); err != nil {
		t.Fatalf("MigrateV2ToV3: %v", err)
	}

	if v, err := d.SchemaVersion(); err != nil {
		t.Fatalf("SchemaVersion after migration: %v", err)
	} else if v != 3 {
		t.Fatalf("SchemaVersion = %d, want 3", v)
	}

	got := listUserTables(t, d)
	want := append([]string{}, ExpectedTables...)
	sort.Strings(want)
	sort.Strings(got)
	if !equalStringSlices(got, want) {
		t.Fatalf("table list mismatch\n got:  %v\n want: %v", got, want)
	}

	gotIdx := listUserIndexes(t, d)
	wantIdx := append([]string{}, ExpectedIndexes...)
	sort.Strings(wantIdx)
	sort.Strings(gotIdx)
	if !equalStringSlices(gotIdx, wantIdx) {
		t.Fatalf("index list mismatch\n got:  %v\n want: %v", gotIdx, wantIdx)
	}
}

func TestMigrateV2ToV3_Idempotent(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	for _, stmt := range v2TablesAndIndexes() {
		if _, err := d.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("setup v2 schema: %v", err)
		}
	}
	if err := d.upsertVersion(ctx, 2); err != nil {
		t.Fatalf("set version to 2: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := d.MigrateV2ToV3(ctx); err != nil {
			t.Fatalf("MigrateV2ToV3 iteration %d: %v", i, err)
		}
	}

	if v, err := d.SchemaVersion(); err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	} else if v != 3 {
		t.Errorf("SchemaVersion after re-run = %d, want 3", v)
	}
}

func TestMigrateV2ToV3_NilDB(t *testing.T) {
	var d *DB
	if err := d.MigrateV2ToV3(context.Background()); err == nil {
		t.Fatal("MigrateV2ToV3 on nil DB should fail, got nil error")
	}
}

// v2TablesAndIndexes returns the DDL that constituted the v2 schema (all
// tables and indexes except the three added in v3). It is used to set up a
// synthetic v2 database for migration tests.
func v2TablesAndIndexes() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS schema_version (
  version INTEGER PRIMARY KEY,
  migrated_at INTEGER NOT NULL
);`,
		`CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  genre TEXT NOT NULL DEFAULT 'fantasy',
  pipeline_phase TEXT NOT NULL DEFAULT 'setting',
  created_at INTEGER NOT NULL,
  modified_at INTEGER NOT NULL
);`,
		`CREATE TABLE IF NOT EXISTS worlds (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  name TEXT NOT NULL,
  slug TEXT NOT NULL,
  description TEXT,
  content TEXT,
  metadata TEXT,
  created_at INTEGER NOT NULL,
  modified_at INTEGER NOT NULL,
  FOREIGN KEY (project_id) REFERENCES projects(id)
);`,
		`CREATE TABLE IF NOT EXISTS characters (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  name TEXT NOT NULL,
  slug TEXT NOT NULL,
  description TEXT,
  voice_profile TEXT,
  content TEXT,
  created_at INTEGER NOT NULL,
  modified_at INTEGER NOT NULL,
  FOREIGN KEY (project_id) REFERENCES projects(id)
);`,
		`CREATE TABLE IF NOT EXISTS chapters (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  arc_id TEXT,
  volume INTEGER NOT NULL DEFAULT 1,
  chapter_number INTEGER NOT NULL,
  title TEXT NOT NULL,
  slug TEXT NOT NULL,
  content TEXT,
  word_count INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'draft',
  created_at INTEGER NOT NULL,
  modified_at INTEGER NOT NULL,
  FOREIGN KEY (project_id) REFERENCES projects(id),
  FOREIGN KEY (arc_id) REFERENCES outlines(id)
);`,
		`CREATE TABLE IF NOT EXISTS reviews (
  id TEXT PRIMARY KEY,
  chapter_id TEXT NOT NULL,
  dimension TEXT NOT NULL,
  score REAL NOT NULL,
  issues TEXT,
  created_at INTEGER NOT NULL,
  FOREIGN KEY (chapter_id) REFERENCES chapters(id)
);`,
		`CREATE TABLE IF NOT EXISTS knowledge_graph_nodes (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  entity_type TEXT NOT NULL,
  entity_id TEXT NOT NULL,
  name TEXT NOT NULL,
  attributes TEXT,
  created_at INTEGER NOT NULL,
  FOREIGN KEY (project_id) REFERENCES projects(id)
);`,
		`CREATE TABLE IF NOT EXISTS knowledge_graph_edges (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  from_node_id TEXT NOT NULL,
  to_node_id TEXT NOT NULL,
  relation TEXT NOT NULL,
  weight REAL NOT NULL DEFAULT 1.0,
  attributes TEXT,
  created_at INTEGER NOT NULL,
  FOREIGN KEY (project_id) REFERENCES projects(id),
  FOREIGN KEY (from_node_id) REFERENCES knowledge_graph_nodes(id),
  FOREIGN KEY (to_node_id) REFERENCES knowledge_graph_nodes(id)
);`,
		`CREATE TABLE IF NOT EXISTS entity_links (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  source_type TEXT NOT NULL,
  source_id TEXT NOT NULL,
  target_type TEXT NOT NULL,
  target_id TEXT NOT NULL,
  link_type TEXT NOT NULL DEFAULT 'mention',
  created_at INTEGER NOT NULL,
  FOREIGN KEY (project_id) REFERENCES projects(id)
);`,
		`CREATE TABLE IF NOT EXISTS chapter_facts (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  chapter_id TEXT NOT NULL,
  fact_type TEXT NOT NULL,
  subject TEXT NOT NULL,
  predicate TEXT NOT NULL,
  object TEXT NOT NULL,
  confidence REAL NOT NULL DEFAULT 1.0,
  context TEXT,
  created_at INTEGER NOT NULL,
  FOREIGN KEY (project_id) REFERENCES projects(id),
  FOREIGN KEY (chapter_id) REFERENCES chapters(id)
);`,
		`CREATE TABLE IF NOT EXISTS character_states (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  character_id TEXT NOT NULL,
  chapter_id TEXT NOT NULL,
  snapshot TEXT NOT NULL,
  tags TEXT,
  created_at INTEGER NOT NULL,
  FOREIGN KEY (project_id) REFERENCES projects(id),
  FOREIGN KEY (character_id) REFERENCES characters(id),
  FOREIGN KEY (chapter_id) REFERENCES chapters(id)
);`,
		`CREATE TABLE IF NOT EXISTS outlines (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  parent_id TEXT,
  level TEXT NOT NULL,
  title TEXT NOT NULL,
  summary TEXT,
  order_index INTEGER NOT NULL DEFAULT 0,
  metadata TEXT,
  created_at INTEGER NOT NULL,
  modified_at INTEGER NOT NULL,
  FOREIGN KEY (project_id) REFERENCES projects(id)
);`,
		`CREATE TABLE IF NOT EXISTS aliases (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  entity_type TEXT NOT NULL,
  entity_id TEXT NOT NULL,
  alias TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  FOREIGN KEY (project_id) REFERENCES projects(id)
);`,
		`CREATE TABLE IF NOT EXISTS foreshadows (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  planted_chapter_id TEXT,
  resolved_chapter_id TEXT,
  description TEXT NOT NULL,
  keywords TEXT,
  status TEXT NOT NULL DEFAULT 'planted',
  importance TEXT NOT NULL DEFAULT 'minor',
  created_at INTEGER NOT NULL,
  modified_at INTEGER NOT NULL,
  FOREIGN KEY (project_id) REFERENCES projects(id),
  FOREIGN KEY (planted_chapter_id) REFERENCES chapters(id),
  FOREIGN KEY (resolved_chapter_id) REFERENCES chapters(id)
);`,
		`CREATE INDEX IF NOT EXISTS idx_chapter_facts_chapter_id ON chapter_facts(chapter_id);`,
		`CREATE INDEX IF NOT EXISTS idx_character_states_character_id ON character_states(character_id);`,
		`CREATE INDEX IF NOT EXISTS idx_character_states_chapter_id ON character_states(chapter_id);`,
		`CREATE INDEX IF NOT EXISTS idx_outlines_parent_id ON outlines(parent_id);`,
		`CREATE INDEX IF NOT EXISTS idx_aliases_entity_id ON aliases(entity_id);`,
		`CREATE INDEX IF NOT EXISTS idx_aliases_alias ON aliases(alias);`,
		`CREATE INDEX IF NOT EXISTS idx_kg_edges_from ON knowledge_graph_edges(from_node_id);`,
		`CREATE INDEX IF NOT EXISTS idx_kg_edges_to ON knowledge_graph_edges(to_node_id);`,
		`CREATE INDEX IF NOT EXISTS idx_foreshadows_chapter_id ON foreshadows(planted_chapter_id);`,
		`CREATE INDEX IF NOT EXISTS idx_chapters_arc_id ON chapters(arc_id);`,
		`CREATE INDEX IF NOT EXISTS idx_chapters_project_id ON chapters(project_id);`,
	}
}

// --- helpers ---

func listUserTables(t *testing.T, d *DB) []string {
	t.Helper()
	rows, err := d.QueryContext(context.Background(),
		"SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		// Skip internal SQLite tables (none should appear, but be safe).
		if n == "sqlite_sequence" {
			continue
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	return names
}

func listUserIndexes(t *testing.T, d *DB) []string {
	t.Helper()
	rows, err := d.QueryContext(context.Background(),
		"SELECT name FROM sqlite_master WHERE type='index' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		t.Fatalf("list indexes: %v", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	return names
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
