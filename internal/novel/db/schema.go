package db

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Schema version constants. The schema is at v3; Migrate() writes this
// number to the schema_version table on success.
const (
	CurrentSchemaVersion = 3
)

// Pragmas applied before any DDL. WAL is skipped for in-memory databases
// (SQLite returns an error), so we issue it unconditionally and let
// callers open a path that supports it. The migrate runner must
// distinguish, but for the typical file-based case both pragmas are
// appropriate.
const (
	PragmaWAL         = "PRAGMA journal_mode=WAL;"
	PragmaForeignKeys = "PRAGMA foreign_keys=ON;"
)

// CreateTablesSQL contains the DDL for the 14 business tables of the
// novel subsystem. Order matters: tables that are referenced by foreign
// keys must come first. The list is exposed so callers (e.g. tools that
// want to inspect or patch the schema) can read it; Migrate() iterates
// over it directly.
var CreateTablesSQL = []string{
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
  suggestions TEXT,
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

	`CREATE TABLE IF NOT EXISTS chapter_summaries (
  id TEXT PRIMARY KEY,
  chapter_id TEXT NOT NULL,
  project_id TEXT NOT NULL,
  summary TEXT,
  key_events TEXT,
  key_characters TEXT,
  key_locations TEXT,
  embedding BLOB,
  created_at INTEGER NOT NULL,
  FOREIGN KEY (chapter_id) REFERENCES chapters(id),
  FOREIGN KEY (project_id) REFERENCES projects(id)
);`,

	`CREATE TABLE IF NOT EXISTS character_timeline (
  id TEXT PRIMARY KEY,
  character_id TEXT NOT NULL,
  chapter_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  description TEXT NOT NULL,
  importance TEXT NOT NULL DEFAULT 'minor',
  created_at INTEGER NOT NULL,
  FOREIGN KEY (character_id) REFERENCES characters(id),
  FOREIGN KEY (chapter_id) REFERENCES chapters(id)
);`,

	`CREATE TABLE IF NOT EXISTS world_lore (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  category TEXT NOT NULL,
  title TEXT NOT NULL,
  content TEXT,
  first_chapter INTEGER,
  last_chapter INTEGER,
  ref_count INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  FOREIGN KEY (project_id) REFERENCES projects(id)
);`,
}

// CreateIndexesSQL is the list of all FK and lookup indexes. They use
// IF NOT EXISTS so Migrate() is idempotent and safe to re-run.
var CreateIndexesSQL = []string{
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
	`CREATE INDEX IF NOT EXISTS idx_chapter_summaries_chapter_id ON chapter_summaries(chapter_id);`,
	`CREATE INDEX IF NOT EXISTS idx_chapter_summaries_project_id ON chapter_summaries(project_id);`,
	`CREATE INDEX IF NOT EXISTS idx_character_timeline_character_id ON character_timeline(character_id);`,
	`CREATE INDEX IF NOT EXISTS idx_character_timeline_chapter_id ON character_timeline(chapter_id);`,
	`CREATE INDEX IF NOT EXISTS idx_world_lore_project_id ON world_lore(project_id);`,
	`CREATE INDEX IF NOT EXISTS idx_world_lore_category ON world_lore(category);`,
}

// ExpectedTables lists the business tables that Migrate() must create.
// Tests use this list to assert that every table exists in sqlite_master.
var ExpectedTables = []string{
	"schema_version",
	"projects",
	"worlds",
	"characters",
	"chapters",
	"reviews",
	"knowledge_graph_nodes",
	"knowledge_graph_edges",
	"entity_links",
	"chapter_facts",
	"character_states",
	"outlines",
	"aliases",
	"foreshadows",
	"chapter_summaries",
	"character_timeline",
	"world_lore",
}

// ExpectedIndexes lists every index Migrate() creates. Tests use this to
// verify that all indexes are present in sqlite_master.
var ExpectedIndexes = []string{
	"idx_chapter_facts_chapter_id",
	"idx_character_states_character_id",
	"idx_character_states_chapter_id",
	"idx_outlines_parent_id",
	"idx_aliases_entity_id",
	"idx_aliases_alias",
	"idx_kg_edges_from",
	"idx_kg_edges_to",
	"idx_foreshadows_chapter_id",
	"idx_chapters_arc_id",
	"idx_chapters_project_id",
	"idx_chapter_summaries_chapter_id",
	"idx_chapter_summaries_project_id",
	"idx_character_timeline_character_id",
	"idx_character_timeline_chapter_id",
	"idx_world_lore_project_id",
	"idx_world_lore_category",
}

// Migrate applies the schema to the database. It is idempotent: all DDL
// uses IF NOT EXISTS and the schema_version row is only inserted on a
// fresh database. If a previous run left a different schema_version the
// migration is a no-op for tables/indexes already present and the version
// row is updated to the current value.
func (d *DB) Migrate(ctx context.Context) error {
	if d == nil || d.DB == nil {
		return fmt.Errorf("db.Migrate: database is not open")
	}

	if _, err := d.DB.ExecContext(ctx, PragmaForeignKeys); err != nil {
		return fmt.Errorf("db.Migrate: enable foreign keys: %w", err)
	}

	for _, stmt := range CreateTablesSQL {
		if _, err := d.DB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("db.Migrate: create table: %w\nstatement: %s", err, stmt)
		}
	}

	for _, stmt := range CreateIndexesSQL {
		if _, err := d.DB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("db.Migrate: create index: %w\nstatement: %s", err, stmt)
		}
	}

	// Upsert the version row so re-runs converge on CurrentSchemaVersion.
	if err := d.upsertVersion(ctx, CurrentSchemaVersion); err != nil {
		return err
	}
	return nil
}

// SchemaVersion returns the version recorded in the schema_version table.
// If the table is empty (fresh DB before Migrate) it returns 0 with no
// error so callers can branch on "needs migrate" vs "already migrated".
// If the table itself does not exist yet, the same 0 is returned — this
// keeps the call cheap to use as a "have we migrated yet?" probe.
func (d *DB) SchemaVersion() (int, error) {
	if d == nil || d.DB == nil {
		return 0, fmt.Errorf("db.SchemaVersion: database is not open")
	}
	row := d.DB.QueryRowContext(context.Background(),
		"SELECT COALESCE(MAX(version), 0) FROM schema_version")
	var v int
	if err := row.Scan(&v); err != nil {
		// Pre-migration the table does not exist; treat as v0.
		if isNoSuchTableError(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("db.SchemaVersion: %w", err)
	}
	return v, nil
}

// isNoSuchTableError returns true when err is a SQLite "no such table"
// error. modernc.org/sqlite returns the literal phrase in Error().
func isNoSuchTableError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no such table")
}

func (d *DB) upsertVersion(ctx context.Context, version int) error {
	const q = `INSERT INTO schema_version (version, migrated_at) VALUES (?, ?)
               ON CONFLICT(version) DO UPDATE SET migrated_at = excluded.migrated_at;`
	if _, err := d.DB.ExecContext(ctx, q, version, time.Now().Unix()); err != nil {
		return fmt.Errorf("db.Migrate: write schema_version: %w", err)
	}
	return nil
}
