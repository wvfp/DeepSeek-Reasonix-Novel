package project

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNew_CreatesLayout(t *testing.T) {
	dir := t.TempDir()
	mgr, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	if got, want := mgr.Root(), filepath.Join(dir, DefaultDirName); got != want {
		t.Errorf("Root() = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(mgr.Root(), "novel.db")); err != nil {
		t.Errorf("novel.db missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(mgr.Root(), "reasonix.toml")); err != nil {
		t.Errorf("reasonix.toml missing: %v", err)
	}
	for _, sub := range []string{"content", "content/settings", "content/dungeons", "content/chapters", "content/reports", "style-anchors"} {
		if _, err := os.Stat(filepath.Join(mgr.Root(), sub)); err != nil {
			t.Errorf("subdir %s missing: %v", sub, err)
		}
	}
}

func TestNew_RefusesExistingProject(t *testing.T) {
	dir := t.TempDir()
	first, err := New(dir)
	if err != nil {
		t.Fatalf("first New: %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })

	if _, err := New(dir); err == nil {
		t.Fatal("second New should have refused; got nil error")
	}
}

func TestOpen_ReopensProject(t *testing.T) {
	dir := t.TempDir()
	first, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_ = first.Close()

	second, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	if second.DB() == nil {
		t.Error("DB() returned nil after Open")
	}
}

func TestOpen_MissingProject(t *testing.T) {
	dir := t.TempDir()
	if _, err := Open(dir); err == nil {
		t.Fatal("Open on missing project should have errored")
	}
}

func TestProject_SetPhase(t *testing.T) {
	dir := t.TempDir()
	mgr, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	ctx := context.Background()
	if err := insertProjectRow(t, mgr); err != nil {
		t.Fatalf("insert seed project: %v", err)
	}

	p, err := mgr.Project(ctx)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if p.PipelinePhase != domainPhase(t) {
		t.Errorf("phase = %q, want %q", p.PipelinePhase, domainPhase(t))
	}
	if err := mgr.SetPhase(ctx, "writing"); err != nil {
		t.Fatalf("SetPhase: %v", err)
	}
	p2, err := mgr.Project(ctx)
	if err != nil {
		t.Fatalf("Project after SetPhase: %v", err)
	}
	if p2.PipelinePhase != "writing" {
		t.Errorf("phase after SetPhase = %q, want %q", p2.PipelinePhase, "writing")
	}
}
