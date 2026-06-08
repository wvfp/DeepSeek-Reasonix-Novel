package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"reasonix/internal/novel/db"
	"reasonix/internal/novel/domain"
	"reasonix/internal/novel/project"
)

// initTool is the `novel_init` tool. It must be the first tool any
// session runs — every other tool calls GetDatabase() under the hood
// and returns the Chinese "请先初始化小说项目" error when no project
// row exists. Execute mirrors the TS novel_init flow: take a project
// name + genre, refuse to overwrite, then insert a single project
// row. The DB schema + on-disk layout is created by project.New
// before this tool is called, so by the time Execute runs the manager
// is already initialised.
type initTool struct{}

func init() { registerDefault(&initTool{}) }

func (t *initTool) Name() string { return "novel_init" }

func (t *initTool) Description() string {
	return "初始化 .novel-weaver/ 项目目录、SQLite 数据库与默认 reasonix.toml。必须第一个调用。"
}

func (t *initTool) Execute(ctx context.Context, input map[string]any, mgr *project.Manager) (map[string]any, error) {
	if mgr == nil {
		return nil, fmt.Errorf("novel_init: project manager is nil")
	}
	name := stringField(input, "name")
	if name == "" {
		return nil, fmt.Errorf("novel_init: 'name' is required")
	}
	genre := stringField(input, "genre")
	if genre == "" {
		genre = domain.GenreFantasy
	}

	// Check we don't already have a project row — repeated init is a
	// user error, not a silent upsert.
	if existing, _ := mgr.Project(ctx); existing != nil {
		return nil, fmt.Errorf("novel_init: project %q already exists (id=%s); delete .novel-weaver/ to re-init", existing.Name, existing.ID)
	}

	now := time.Now().Unix()
	p := &domain.Project{
		ID:            uuid.New().String(),
		Name:          name,
		Genre:         genre,
		PipelinePhase: domain.PhaseSetting,
		CreatedAt:     now,
		ModifiedAt:    now,
	}
	if err := insertProject(ctx, mgr.DB(), p); err != nil {
		return nil, err
	}

	return map[string]any{
		"project": map[string]any{
			"id":             p.ID,
			"name":           p.Name,
			"genre":          p.Genre,
			"pipeline_phase": p.PipelinePhase,
			"created_at":     p.CreatedAt,
			"modified_at":    p.ModifiedAt,
		},
		"path":           mgr.Root(),
		"schema_version": db.CurrentSchemaVersion,
	}, nil
}

func insertProject(ctx context.Context, d *db.DB, p *domain.Project) error {
	const q = `INSERT INTO projects (id, name, genre, pipeline_phase, created_at, modified_at)
	           VALUES (?, ?, ?, ?, ?, ?)`
	_, err := d.ExecContext(ctx, q, p.ID, p.Name, p.Genre, p.PipelinePhase, p.CreatedAt, p.ModifiedAt)
	if err != nil {
		return fmt.Errorf("novel_init: insert projects: %w", err)
	}
	return nil
}
