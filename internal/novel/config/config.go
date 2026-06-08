// Package config provides novel-subsystem configuration loaded from
// .novel-weaver/config.json. It is separate from the top-level
// reasonix.toml so the novel plugin can evolve its own schema without
// touching the agent's runtime config.
//
// The package supports:
//   - JSON loading with default fallback for missing fields.
//   - Hot-reload via polling (fsnotify is not used to avoid an extra
//     dependency; the polling interval is 5 s).
//   - Thread-safe access via atomic pointer swap.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------------------
// Config shape
// ---------------------------------------------------------------------------

// Config is the root of the novel-weaver configuration.
type Config struct {
	Writing       WritingConfig       `json:"writing"`
	AntiAI        AntiAIConfig        `json:"anti_ai"`
	Review        ReviewConfig        `json:"review"`
	Consistency   ConsistencyConfig   `json:"consistency"`
	Genre         GenreConfig         `json:"genre"`
	ContentSafety ContentSafetyConfig `json:"content_safety"`
	StyleAnchor   StyleAnchorConfig   `json:"style_anchor"`
}

// WritingConfig controls the chapter-writing pipeline.
type WritingConfig struct {
	ParagraphLimit int `json:"paragraph_limit"`
	WordCountMin   int `json:"word_count_min"`
	WordCountMax   int `json:"word_count_max"`
	ContextWindow  int `json:"context_window"`
}

// AntiAIConfig controls the anti-AI-slop rule engine.
type AntiAIConfig struct {
	RulesSource      string `json:"rules_source"`
	RulesFile        string `json:"rules_file"`
	SeverityThreshold string `json:"severity_threshold"`
}

// ReviewConfig controls the chapter-review dimensions.
type ReviewConfig struct {
	Dimensions     []string `json:"dimensions"`
	ScoreThreshold float64  `json:"score_threshold"`
}

// DimensionsOrDefault returns the configured dimension names, or the
// built-in 8-dimension list when the config field is empty.
func (rc ReviewConfig) DimensionsOrDefault() []string {
	if len(rc.Dimensions) == 0 {
		return []string{
			"plot",
			"character",
			"style",
			"consistency",
			"pacing",
			"foreshadow",
			"hook",
			"values",
		}
	}
	return rc.Dimensions
}

// ConsistencyConfig controls the cross-chapter consistency checker.
type ConsistencyConfig struct {
	Dimensions []string `json:"dimensions"`
}

// GenreConfig holds the active genre and per-genre overrides.
type GenreConfig struct {
	ID       string            `json:"id"`
	Overrides map[string]any   `json:"overrides"`
}

// ContentSafetyConfig enables optional word filtering.
type ContentSafetyConfig struct {
	Enabled       bool     `json:"enabled"`
	CustomWords   []string `json:"custom_words"`
	Whitelist     []string `json:"whitelist"`
	RegexPatterns []string `json:"regex_patterns"`
}

// StyleAnchorConfig controls the style-anchor extraction behaviour.
type StyleAnchorConfig struct {
	// Dimensions lists the active dimension names. Empty means "all".
	Dimensions []string `json:"dimensions"`
	// ChapterCount is how many recent chapters to scan. Zero defaults to 5.
	ChapterCount int `json:"chapter_count"`
}

// ---------------------------------------------------------------------------
// Defaults
// ---------------------------------------------------------------------------

// Default returns the built-in default configuration. Every field has
// a sensible value so a missing config.json never breaks the pipeline.
func Default() *Config {
	return &Config{
		Writing: WritingConfig{
			ParagraphLimit: 500,
			WordCountMin:   2000,
			WordCountMax:   4000,
			ContextWindow:  8192,
		},
		AntiAI: AntiAIConfig{
			RulesSource:       "embedded",
			RulesFile:         "",
			SeverityThreshold: "medium",
		},
		Review: ReviewConfig{
			Dimensions: []string{
				"plot",
				"character",
				"style",
				"consistency",
				"pacing",
				"foreshadow",
				"hook",
				"values",
			},
			ScoreThreshold: 6.0,
		},
		Consistency: ConsistencyConfig{
			Dimensions: []string{
				"time",
				"space",
				"character",
				"item",
				"setting",
			},
		},
		Genre: GenreConfig{
			ID:        "fantasy",
			Overrides: map[string]any{},
		},
		ContentSafety: ContentSafetyConfig{
			Enabled:     false,
			CustomWords: nil,
		},
		StyleAnchor: StyleAnchorConfig{
			Dimensions:   nil,
			ChapterCount: 0,
		},
	}
}

// ---------------------------------------------------------------------------
// Loading
// ---------------------------------------------------------------------------

// Load reads .novel-weaver/config.json under projectRoot and merges it
// on top of Default(). Missing or malformed fields fall back to their
// default values; a missing file is not an error — the defaults are
// returned instead.
func Load(projectRoot string) (*Config, error) {
	cfg := Default()
	path := filepath.Join(projectRoot, ".novel-weaver", "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("novel config: read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("novel config: parse %s: %w", path, err)
	}
	// Ensure nested maps are non-nil after partial unmarshal.
	if cfg.Genre.Overrides == nil {
		cfg.Genre.Overrides = map[string]any{}
	}
	return cfg, nil
}

// MustLoad is like Load but panics on error. Useful for tests and
// one-shot CLI commands that already validated the project exists.
func MustLoad(projectRoot string) *Config {
	cfg, err := Load(projectRoot)
	if err != nil {
		panic(err)
	}
	return cfg
}

// WriteDefault writes a pretty-printed default config.json to path.
// It is called by project.New when the file is missing.
func WriteDefault(path string) error {
	data, err := json.MarshalIndent(Default(), "", "  ")
	if err != nil {
		return fmt.Errorf("novel config: marshal default: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("novel config: write %s: %w", path, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Hot reload (polling)
// ---------------------------------------------------------------------------

// Reloader watches a config file and reloads it when the mtime changes.
// Access the latest config via Config() — it is always non-nil because
// the constructor seeds it with Default().
type Reloader struct {
	path     string
	interval time.Duration
	stop     chan struct{}
	wg       sync.WaitGroup

	mu       sync.RWMutex
	mtime    time.Time
	cfg      atomic.Pointer[Config]
	onChange func(*Config)
}

// NewReloader starts a background poller for path. interval must be > 0;
// if <= 0 it defaults to 5 s. onChange is called (if non-nil) whenever
// the file is successfully reloaded.
func NewReloader(path string, interval time.Duration, onChange func(*Config)) *Reloader {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	r := &Reloader{
		path:     path,
		interval: interval,
		stop:     make(chan struct{}),
		onChange: onChange,
	}
	// Seed with current file or defaults.
	cfg, _ := loadOrDefault(path)
	r.cfg.Store(cfg)
	if st, err := os.Stat(path); err == nil {
		r.mtime = st.ModTime()
	}
	r.wg.Add(1)
	go r.loop()
	return r
}

// Config returns the latest configuration. It is always non-nil.
func (r *Reloader) Config() *Config {
	if c := r.cfg.Load(); c != nil {
		return c
	}
	return Default()
}

// Stop halts the background poller and waits for the goroutine to exit.
func (r *Reloader) Stop() {
	close(r.stop)
	r.wg.Wait()
}

func (r *Reloader) loop() {
	defer r.wg.Done()
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.poll()
		case <-r.stop:
			return
		}
	}
}

func (r *Reloader) poll() {
	st, err := os.Stat(r.path)
	if err != nil {
		return // missing file → keep current config
	}
	r.mu.RLock()
	unchanged := st.ModTime().Equal(r.mtime)
	r.mu.RUnlock()
	if unchanged {
		return
	}
	cfg, err := loadOrDefault(r.path)
	if err != nil {
		return // corrupt file → keep current config
	}
	r.cfg.Store(cfg)
	r.mu.Lock()
	r.mtime = st.ModTime()
	r.mu.Unlock()
	if r.onChange != nil {
		r.onChange(cfg)
	}
}

func loadOrDefault(path string) (*Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	if cfg.Genre.Overrides == nil {
		cfg.Genre.Overrides = map[string]any{}
	}
	return cfg, nil
}
