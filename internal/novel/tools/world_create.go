package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"reasonix/internal/novel/domain"
	"reasonix/internal/novel/project"
	"reasonix/internal/novel/repo"
)

// worldCreateTool implements `world_create`. The name is the only
// required input. Description and metadata are optional. Returns the
// world record + the on-disk path of the world-{slug}.md file (which
// the tool also writes, mirroring the TS plugin).
type worldCreateTool struct{}

func init() { registerDefault(&worldCreateTool{}) }

func (t *worldCreateTool) Name() string { return "world_create" }

func (t *worldCreateTool) Description() string {
	return "在当前项目下创建一条世界观（world）记录，并写入对应的 Markdown 文件。"
}

func (t *worldCreateTool) Execute(ctx context.Context, input map[string]any, mgr *project.Manager) (map[string]any, error) {
	p, err := mgr.Project(ctx)
	if err != nil {
		return nil, fmt.Errorf("world_create: %w", err)
	}
	name, err := requiredString(input, "name")
	if err != nil {
		return nil, err
	}
	w := &domain.World{
		ProjectID:   p.ID,
		Name:        name,
		Description: stringField(input, "description"),
		Metadata:    nestedMapField(input, "metadata"),
	}
	if err := repo.NewWorldRepo(mgr.DB()).Create(ctx, w); err != nil {
		return nil, err
	}

	// Write the .md sidecar under .novel-weaver/content/settings/.
	path, err := writeWorldMarkdown(mgr, w)
	if err != nil {
		// Don't roll back the DB row — the .md is regenerable from
		// the row, but the row is the source of truth.
		return map[string]any{
			"world":      worldToMap(w),
			"path":       path,
			"writeWarn":  err.Error(),
		}, nil
	}

	return map[string]any{"world": worldToMap(w), "path": path}, nil
}

// writeWorldMarkdown renders and writes settings/world-{slug}.md. The
// body is a minimal stub — Phase 3 replaces it with a richer template.
func writeWorldMarkdown(mgr *project.Manager, w *domain.World) (string, error) {
	dir := filepath.Join(mgr.Root(), "content", "settings")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "world-"+w.Slug+".md")
	body := fmt.Sprintf("---\ntitle: %s\nslug: %s\nstatus: draft\ncreated_at: %d\nmodified_at: %d\n---\n\n# %s\n\n%s\n",
		w.Name, w.Slug, w.CreatedAt, w.ModifiedAt, w.Name, w.Description)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return path, err
	}
	return path, nil
}

func worldToMap(w *domain.World) map[string]any {
	return map[string]any{
		"id":          w.ID,
		"project_id":  w.ProjectID,
		"name":        w.Name,
		"slug":        w.Slug,
		"description": w.Description,
		"content":     w.Content,
		"metadata":    w.Metadata,
		"created_at":  w.CreatedAt,
		"modified_at": w.ModifiedAt,
	}
}
