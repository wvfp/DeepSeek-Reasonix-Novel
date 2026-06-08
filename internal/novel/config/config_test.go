package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.Writing.ParagraphLimit != 500 {
		t.Errorf("ParagraphLimit = %d, want 500", cfg.Writing.ParagraphLimit)
	}
	if cfg.Writing.WordCountMin != 2000 {
		t.Errorf("WordCountMin = %d, want 2000", cfg.Writing.WordCountMin)
	}
	if cfg.Writing.WordCountMax != 4000 {
		t.Errorf("WordCountMax = %d, want 4000", cfg.Writing.WordCountMax)
	}
	if cfg.Writing.ContextWindow != 8192 {
		t.Errorf("ContextWindow = %d, want 8192", cfg.Writing.ContextWindow)
	}
	if cfg.AntiAI.RulesSource != "embedded" {
		t.Errorf("RulesSource = %q, want embedded", cfg.AntiAI.RulesSource)
	}
	if cfg.AntiAI.SeverityThreshold != "medium" {
		t.Errorf("SeverityThreshold = %q, want medium", cfg.AntiAI.SeverityThreshold)
	}
	if len(cfg.Review.Dimensions) != 8 {
		t.Errorf("len(Review.Dimensions) = %d, want 8", len(cfg.Review.Dimensions))
	}
	if cfg.Review.ScoreThreshold != 6.0 {
		t.Errorf("ScoreThreshold = %v, want 6.0", cfg.Review.ScoreThreshold)
	}
	if len(cfg.Consistency.Dimensions) != 5 {
		t.Errorf("len(Consistency.Dimensions) = %d, want 5", len(cfg.Consistency.Dimensions))
	}
	if cfg.Genre.ID != "fantasy" {
		t.Errorf("Genre.ID = %q, want fantasy", cfg.Genre.ID)
	}
	if cfg.Genre.Overrides == nil {
		t.Error("Genre.Overrides is nil, want empty map")
	}
	if cfg.ContentSafety.Enabled {
		t.Error("ContentSafety.Enabled = true, want false")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load missing file: %v", err)
	}
	want := Default()
	if cfg.Writing.ParagraphLimit != want.Writing.ParagraphLimit {
		t.Errorf("missing file fallback: ParagraphLimit = %d, want %d", cfg.Writing.ParagraphLimit, want.Writing.ParagraphLimit)
	}
}

func TestLoad_FullConfig(t *testing.T) {
	dir := t.TempDir()
	projectRoot := filepath.Join(dir, "project")
	nw := filepath.Join(projectRoot, ".novel-weaver")
	if err := os.MkdirAll(nw, 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{
  "writing": {
    "paragraph_limit": 300,
    "word_count_min": 1500,
    "word_count_max": 3500,
    "context_window": 4096
  },
  "anti_ai": {
    "rules_source": "file",
    "rules_file": "rules.json",
    "severity_threshold": "high"
  },
  "review": {
    "dimensions": ["plot", "style"],
    "score_threshold": 7.5
  },
  "consistency": {
    "dimensions": ["time", "space"]
  },
  "genre": {
    "id": "xianxia",
    "overrides": {"tone": "dark"}
  },
  "content_safety": {
    "enabled": true,
    "custom_words": ["foo", "bar"]
  }
}`)
	if err := os.WriteFile(filepath.Join(nw, "config.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(projectRoot)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Writing.ParagraphLimit != 300 {
		t.Errorf("ParagraphLimit = %d, want 300", cfg.Writing.ParagraphLimit)
	}
	if cfg.Writing.WordCountMin != 1500 {
		t.Errorf("WordCountMin = %d, want 1500", cfg.Writing.WordCountMin)
	}
	if cfg.Writing.WordCountMax != 3500 {
		t.Errorf("WordCountMax = %d, want 3500", cfg.Writing.WordCountMax)
	}
	if cfg.Writing.ContextWindow != 4096 {
		t.Errorf("ContextWindow = %d, want 4096", cfg.Writing.ContextWindow)
	}
	if cfg.AntiAI.RulesSource != "file" {
		t.Errorf("RulesSource = %q, want file", cfg.AntiAI.RulesSource)
	}
	if cfg.AntiAI.RulesFile != "rules.json" {
		t.Errorf("RulesFile = %q, want rules.json", cfg.AntiAI.RulesFile)
	}
	if cfg.AntiAI.SeverityThreshold != "high" {
		t.Errorf("SeverityThreshold = %q, want high", cfg.AntiAI.SeverityThreshold)
	}
	if len(cfg.Review.Dimensions) != 2 {
		t.Errorf("len(Review.Dimensions) = %d, want 2", len(cfg.Review.Dimensions))
	}
	if cfg.Review.ScoreThreshold != 7.5 {
		t.Errorf("ScoreThreshold = %v, want 7.5", cfg.Review.ScoreThreshold)
	}
	if len(cfg.Consistency.Dimensions) != 2 {
		t.Errorf("len(Consistency.Dimensions) = %d, want 2", len(cfg.Consistency.Dimensions))
	}
	if cfg.Genre.ID != "xianxia" {
		t.Errorf("Genre.ID = %q, want xianxia", cfg.Genre.ID)
	}
	if cfg.Genre.Overrides["tone"] != "dark" {
		t.Errorf("Genre.Overrides[tone] = %v, want dark", cfg.Genre.Overrides["tone"])
	}
	if !cfg.ContentSafety.Enabled {
		t.Error("ContentSafety.Enabled = false, want true")
	}
	if len(cfg.ContentSafety.CustomWords) != 2 {
		t.Errorf("len(CustomWords) = %d, want 2", len(cfg.ContentSafety.CustomWords))
	}
}

func TestLoad_PartialFallback(t *testing.T) {
	dir := t.TempDir()
	projectRoot := filepath.Join(dir, "project")
	nw := filepath.Join(projectRoot, ".novel-weaver")
	if err := os.MkdirAll(nw, 0o755); err != nil {
		t.Fatal(err)
	}
	// Only override one field; the rest must keep defaults.
	data := []byte(`{"writing": {"paragraph_limit": 123}}`)
	if err := os.WriteFile(filepath.Join(nw, "config.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(projectRoot)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Writing.ParagraphLimit != 123 {
		t.Errorf("ParagraphLimit = %d, want 123", cfg.Writing.ParagraphLimit)
	}
	if cfg.Writing.WordCountMin != 2000 {
		t.Errorf("WordCountMin fallback = %d, want 2000", cfg.Writing.WordCountMin)
	}
	if cfg.Review.ScoreThreshold != 6.0 {
		t.Errorf("ScoreThreshold fallback = %v, want 6.0", cfg.Review.ScoreThreshold)
	}
}

func TestWriteDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := WriteDefault(path); err != nil {
		t.Fatalf("WriteDefault: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load after write: %v", err)
	}
	want := Default()
	if cfg.Writing.ParagraphLimit != want.Writing.ParagraphLimit {
		t.Errorf("written ParagraphLimit = %d, want %d", cfg.Writing.ParagraphLimit, want.Writing.ParagraphLimit)
	}
	// Verify the file is pretty-printed (contains newline).
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Error("written file does not end with newline")
	}
}

func TestReloader_HotReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := WriteDefault(path); err != nil {
		t.Fatal(err)
	}

	changed := make(chan *Config, 1)
	r := NewReloader(path, 100*time.Millisecond, func(cfg *Config) {
		select {
		case changed <- cfg:
		default:
		}
	})
	defer r.Stop()

	if r.Config().Writing.ParagraphLimit != 500 {
		t.Errorf("initial ParagraphLimit = %d, want 500", r.Config().Writing.ParagraphLimit)
	}

	// Mutate file on disk.
	data := []byte(`{"writing": {"paragraph_limit": 999}}`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	select {
	case cfg := <-changed:
		if cfg.Writing.ParagraphLimit != 999 {
			t.Errorf("reloaded ParagraphLimit = %d, want 999", cfg.Writing.ParagraphLimit)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("hot reload did not fire within 2s")
	}

	if r.Config().Writing.ParagraphLimit != 999 {
		t.Errorf("Config() after reload = %d, want 999", r.Config().Writing.ParagraphLimit)
	}
}

func TestReloader_KeepsCurrentOnCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := WriteDefault(path); err != nil {
		t.Fatal(err)
	}

	r := NewReloader(path, 100*time.Millisecond, nil)
	defer r.Stop()

	if r.Config().Writing.ParagraphLimit != 500 {
		t.Fatalf("initial ParagraphLimit = %d, want 500", r.Config().Writing.ParagraphLimit)
	}

	// Overwrite with garbage.
	if err := os.WriteFile(path, []byte(`{broken`), 0o644); err != nil {
		t.Fatal(err)
	}

	time.Sleep(300 * time.Millisecond)
	if r.Config().Writing.ParagraphLimit != 500 {
		t.Errorf("after corrupt write ParagraphLimit = %d, want 500", r.Config().Writing.ParagraphLimit)
	}
}
