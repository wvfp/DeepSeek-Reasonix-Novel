package tools

import (
	"context"
	"path/filepath"
	"testing"

	"reasonix/internal/novel/db"
	"reasonix/internal/novel/domain"
	"reasonix/internal/novel/project"
)

func TestRegistry_Defaults(t *testing.T) {
	r := NewRegistry()
	if got := r.Names(); len(got) == 0 {
		t.Fatal("default registry is empty")
	}
	if _, ok := r.Get("novel_init"); !ok {
		t.Error("novel_init not registered")
	}
}

func TestNovelInit_HappyPath(t *testing.T) {
	dir := t.TempDir()
	mgr, err := project.New(dir)
	if err != nil {
		t.Fatalf("project.New: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	reg := NewRegistry()
	tool, _ := reg.Get("novel_init")
	out, err := tool.Execute(context.Background(),
		map[string]any{"name": "测试小说", "genre": domain.GenreXianxia}, mgr)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if v, _ := out["schema_version"].(int); v != db.CurrentSchemaVersion {
		t.Errorf("schema_version = %v, want %d", out["schema_version"], db.CurrentSchemaVersion)
	}
	projMap, _ := out["project"].(map[string]any)
	if projMap == nil {
		t.Fatal("project field missing from output")
	}
	if name, _ := projMap["name"].(string); name != "测试小说" {
		t.Errorf("name = %q, want %q", name, "测试小说")
	}
	if genre, _ := projMap["genre"].(string); genre != domain.GenreXianxia {
		t.Errorf("genre = %q, want %q", genre, domain.GenreXianxia)
	}
	if path, _ := out["path"].(string); path != filepath.Join(dir, project.DefaultDirName) {
		t.Errorf("path = %q, want %q", path, filepath.Join(dir, project.DefaultDirName))
	}

	// Project must be readable back through the manager.
	p, err := mgr.Project(context.Background())
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if p.Name != "测试小说" {
		t.Errorf("Project().Name = %q, want %q", p.Name, "测试小说")
	}
}

func TestNovelInit_DefaultGenre(t *testing.T) {
	dir := t.TempDir()
	mgr, err := project.New(dir)
	if err != nil {
		t.Fatalf("project.New: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	tool, _ := NewRegistry().Get("novel_init")
	if _, err := tool.Execute(context.Background(),
		map[string]any{"name": "默认题材"}, mgr); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	p, _ := mgr.Project(context.Background())
	if p.Genre != domain.GenreFantasy {
		t.Errorf("default genre = %q, want %q", p.Genre, domain.GenreFantasy)
	}
}

func TestNovelInit_RefusesDoubleInit(t *testing.T) {
	dir := t.TempDir()
	mgr, err := project.New(dir)
	if err != nil {
		t.Fatalf("project.New: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	tool, _ := NewRegistry().Get("novel_init")
	if _, err := tool.Execute(context.Background(),
		map[string]any{"name": "第一次"}, mgr); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	if _, err := tool.Execute(context.Background(),
		map[string]any{"name": "第二次"}, mgr); err == nil {
		t.Fatal("second Execute should have errored")
	}
}

func TestNovelInit_MissingName(t *testing.T) {
	dir := t.TempDir()
	mgr, err := project.New(dir)
	if err != nil {
		t.Fatalf("project.New: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	tool, _ := NewRegistry().Get("novel_init")
	if _, err := tool.Execute(context.Background(),
		map[string]any{}, mgr); err == nil {
		t.Fatal("Execute with empty input should have errored")
	}
}
