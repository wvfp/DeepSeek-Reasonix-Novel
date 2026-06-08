// Package project wires the .novel-weaver/ on-disk project root: it owns
// the SQLite handle, the on-disk content directory layout, and the
// reasonix.toml stub. The Manager is the single thing every tool gets
// handed — they read the DB and the root path off it, they never open
// the file themselves.
package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"reasonix/internal/novel/config"
	"reasonix/internal/novel/db"
	"reasonix/internal/novel/domain"
)

// DefaultDirName is the folder a project lives under. Matches the
// novel-plugin layout (.novel-weaver/) byte-for-byte so the Go binary
// can drop in for the TS plugin without migration.
const DefaultDirName = ".novel-weaver"

// DefaultDBName is the SQLite file name inside the project dir.
const DefaultDBName = "novel.db"

// DefaultConfigName is the optional reasonix.toml written next to the
// DB on first init.
const DefaultConfigName = "reasonix.toml"

// Manager is the per-process handle to a single .novel-weaver/ project.
// Tools that need the DB or the on-disk path take *Manager as a
// parameter; they never open the SQLite file themselves.
type Manager struct {
	root  string // absolute path to .novel-weaver/
	db    *db.DB
	paths *Paths
}

// Paths groups every well-known path under the project root. The Tool
// code reads these rather than re-deriving them with filepath.Join so
// the layout lives in one place.
type Paths struct {
	Root       string // .novel-weaver/
	DB         string // .novel-weaver/novel.db
	Config     string // .novel-weaver/reasonix.toml
	Content    string // .novel-weaver/content/
	Settings   string // .novel-weaver/content/settings/
	Dungeons   string // .novel-weaver/content/dungeons/
	Chapters   string // .novel-weaver/content/chapters/
	Reports    string // .novel-weaver/content/reports/
	StyleAnchors string // .novel-weaver/style-anchors/
}

// New creates a new project rooted at cwd/.novel-weaver (or at root if
// root is non-empty). It will refuse to overwrite an existing project:
// if the .novel-weaver/ folder is already there it returns an error.
// On success the DB is migrated to CurrentSchemaVersion and a default
// reasonix.toml is written when one is missing.
func New(root string) (*Manager, error) {
	base := root
	if base == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("project.New: getwd: %w", err)
		}
		base = cwd
	}
	projectRoot := filepath.Join(base, DefaultDirName)

	if _, err := os.Stat(projectRoot); err == nil {
		return nil, fmt.Errorf("project.New: %s already exists; delete it manually before re-initialising", projectRoot)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("project.New: stat %s: %w", projectRoot, err)
	}

	paths := &Paths{
		Root:         projectRoot,
		DB:           filepath.Join(projectRoot, DefaultDBName),
		Config:       filepath.Join(projectRoot, DefaultConfigName),
		Content:      filepath.Join(projectRoot, "content"),
		Settings:     filepath.Join(projectRoot, "content", "settings"),
		Dungeons:     filepath.Join(projectRoot, "content", "dungeons"),
		Chapters:     filepath.Join(projectRoot, "content", "chapters"),
		Reports:      filepath.Join(projectRoot, "content", "reports"),
		StyleAnchors: filepath.Join(projectRoot, "style-anchors"),
	}

	for _, d := range []string{
		paths.Root,
		paths.Content,
		paths.Settings,
		paths.Dungeons,
		paths.Chapters,
		paths.Reports,
		paths.StyleAnchors,
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, fmt.Errorf("project.New: mkdir %s: %w", d, err)
		}
	}

	d, err := db.Open(paths.DB)
	if err != nil {
		return nil, fmt.Errorf("project.New: open db: %w", err)
	}
	if err := d.Migrate(context.Background()); err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("project.New: migrate: %w", err)
	}

	mgr := &Manager{root: projectRoot, db: d, paths: paths}

	if err := mgr.writeDefaultConfigIfMissing(); err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("project.New: write config: %w", err)
	}
	if err := mgr.writeNovelConfigIfMissing(); err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("project.New: write novel config: %w", err)
	}

	return mgr, nil
}

// Open reopens an existing project at the given root. Returns an error
// when the .novel-weaver/ folder or novel.db is missing — call this only
// after confirming the project was initialised.
func Open(root string) (*Manager, error) {
	base := root
	if base == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("project.Open: getwd: %w", err)
		}
		base = cwd
	}
	projectRoot := filepath.Join(base, DefaultDirName)

	if _, err := os.Stat(projectRoot); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("project.Open: %s not found; run `novel setup` first", projectRoot)
		}
		return nil, fmt.Errorf("project.Open: stat %s: %w", projectRoot, err)
	}

	paths := &Paths{
		Root:         projectRoot,
		DB:           filepath.Join(projectRoot, DefaultDBName),
		Config:       filepath.Join(projectRoot, DefaultConfigName),
		Content:      filepath.Join(projectRoot, "content"),
		Settings:     filepath.Join(projectRoot, "content", "settings"),
		Dungeons:     filepath.Join(projectRoot, "content", "dungeons"),
		Chapters:     filepath.Join(projectRoot, "content", "chapters"),
		Reports:      filepath.Join(projectRoot, "content", "reports"),
		StyleAnchors: filepath.Join(projectRoot, "style-anchors"),
	}

	d, err := db.Open(paths.DB)
	if err != nil {
		return nil, fmt.Errorf("project.Open: open db: %w", err)
	}
	// Re-running Migrate on an already-migrated DB is a no-op (DDL uses
	// IF NOT EXISTS) so it is safe to call here.
	if err := d.Migrate(context.Background()); err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("project.Open: migrate: %w", err)
	}
	return &Manager{root: projectRoot, db: d, paths: paths}, nil
}

// Close releases the SQLite handle. Safe to call multiple times.
func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	return m.db.Close()
}

// DB returns the underlying *db.DB so repos and tools can run queries.
func (m *Manager) DB() *db.DB { return m.db }

// Root returns the .novel-weaver/ path. Callers that build paths
// under the project should use Paths() instead of joining Root() with
// hard-coded subdirectory names.
func (m *Manager) Root() string { return m.root }

// Paths returns the layout struct.
func (m *Manager) Paths() *Paths { return m.paths }

// Project returns the single row from the projects table. The
// novel_init tool inserts exactly one row on first run; later tools
// read it back via this method. Returns an error when no project row
// exists (caller forgot to run init).
func (m *Manager) Project(ctx context.Context) (*domain.Project, error) {
	if m == nil || m.db == nil {
		return nil, fmt.Errorf("project.Project: manager not open")
	}
	row := m.db.QueryRowContext(ctx,
		`SELECT id, name, genre, pipeline_phase, created_at, modified_at
		 FROM projects
		 ORDER BY created_at ASC
		 LIMIT 1`)
	var p domain.Project
	if err := row.Scan(&p.ID, &p.Name, &p.Genre, &p.PipelinePhase, &p.CreatedAt, &p.ModifiedAt); err != nil {
		return nil, fmt.Errorf("project.Project: %w", err)
	}
	return &p, nil
}

// SetPhase updates pipeline_phase for the project's only row and bumps
// modified_at. It is the only legitimate writer of pipeline_phase.
func (m *Manager) SetPhase(ctx context.Context, phase string) error {
	if m == nil || m.db == nil {
		return fmt.Errorf("project.SetPhase: manager not open")
	}
	_, err := m.db.ExecContext(ctx,
		`UPDATE projects SET pipeline_phase = ?, modified_at = ?`,
		phase, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("project.SetPhase: %w", err)
	}
	return nil
}

// defaultConfig is the bootstrap reasonix.toml content. Mirrors the
// fields the TS plugin's .novel-weaverrc.json accepts (genre / author /
// temperature / antiAi). Phase 3+ will expand it as more config is
// needed.
type defaultConfig struct {
	Genre  string         `json:"genre"`
	Author string         `json:"author,omitempty"`
	Tuning map[string]any `json:"tuning,omitempty"`
}

func (m *Manager) writeDefaultConfigIfMissing() error {
	if _, err := os.Stat(m.paths.Config); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	cfg := defaultConfig{
		Genre:  domain.GenreFantasy,
		Author: "",
		Tuning: map[string]any{},
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.paths.Config, data, 0o644)
}

func (m *Manager) writeNovelConfigIfMissing() error {
	path := filepath.Join(m.paths.Root, "config.json")
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return config.WriteDefault(path)
}
