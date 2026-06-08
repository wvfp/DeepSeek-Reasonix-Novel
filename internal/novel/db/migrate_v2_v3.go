package db

import (
	"context"
	"fmt"
)

// MigrateV2ToV3 upgrades a v2 schema to v3. It is idempotent: all DDL uses
// IF NOT EXISTS and the version upsert is safe to re-run.
func (d *DB) MigrateV2ToV3(ctx context.Context) error {
	if d == nil || d.DB == nil {
		return fmt.Errorf("db.MigrateV2ToV3: database is not open")
	}

	if _, err := d.DB.ExecContext(ctx, PragmaForeignKeys); err != nil {
		return fmt.Errorf("db.MigrateV2ToV3: enable foreign keys: %w", err)
	}

	v2v3Tables := []string{
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

	v2v3Indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_chapter_summaries_chapter_id ON chapter_summaries(chapter_id);`,
		`CREATE INDEX IF NOT EXISTS idx_chapter_summaries_project_id ON chapter_summaries(project_id);`,
		`CREATE INDEX IF NOT EXISTS idx_character_timeline_character_id ON character_timeline(character_id);`,
		`CREATE INDEX IF NOT EXISTS idx_character_timeline_chapter_id ON character_timeline(chapter_id);`,
		`CREATE INDEX IF NOT EXISTS idx_world_lore_project_id ON world_lore(project_id);`,
		`CREATE INDEX IF NOT EXISTS idx_world_lore_category ON world_lore(category);`,
	}

	for _, stmt := range v2v3Tables {
		if _, err := d.DB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("db.MigrateV2ToV3: create table: %w\nstatement: %s", err, stmt)
		}
	}

	for _, stmt := range v2v3Indexes {
		if _, err := d.DB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("db.MigrateV2ToV3: create index: %w\nstatement: %s", err, stmt)
		}
	}

	if err := d.upsertVersion(ctx, 3); err != nil {
		return fmt.Errorf("db.MigrateV2ToV3: %w", err)
	}
	return nil
}
