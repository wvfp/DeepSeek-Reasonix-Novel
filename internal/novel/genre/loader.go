// Package genre bundles the embedded per-genre style packs.
//
// Each genre (xianxia / urban / infinite-flow / apocalypse /
// sci-fi / horror) ships a pack.json with styleGuidelines,
// styleRules, forbiddenPatterns, recommendedPatterns,
// arcTemplates and per-role prompts. The chapter_write tool
// reads the project.genre field, looks up the pack via Load(),
// and appends the rendered block to the system prompt. The
// default fallback is "fantasy" (no pack file, just a stub
// block) so a fresh project without a configured genre still
// runs.
package genre

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

//go:embed xianxia/pack.json
var xianxiaJSON []byte

//go:embed urban/pack.json
var urbanJSON []byte

//go:embed infinite-flow/pack.json
var infiniteFlowJSON []byte

//go:embed apocalypse/pack.json
var apocalypseJSON []byte

//go:embed sci-fi/pack.json
var scifiJSON []byte

//go:embed horror/pack.json
var horrorJSON []byte

//go:embed xianxia/scenes.json
var xianxiaScenesJSON []byte

//go:embed urban/scenes.json
var urbanScenesJSON []byte

//go:embed horror/scenes.json
var horrorScenesJSON []byte

// Pack is the decoded shape of a pack.json file. The struct
// matches the field names in the JSON; see one of the embedded
// files for an example of the schema.
type Pack struct {
	ID                  string            `json:"id"`
	DisplayName         string            `json:"displayName"`
	Description         string            `json:"description"`
	StyleGuidelines     []string          `json:"styleGuidelines"`
	StyleRules          []string          `json:"styleRules"`
	ForbiddenPatterns   []string          `json:"forbiddenPatterns"`
	RecommendedPatterns []string          `json:"recommendedPatterns"`
	ArcTemplates        []string          `json:"arcTemplates"`
	Prompts             map[string]string `json:"prompts"`
}

var (
	loadOnce sync.Once
	loadErr  error
	registry map[string][]byte
)

// loadRegistry decodes every embedded pack once. The
// per-genre JSON is the source of truth; if a pack fails to
// parse we surface the error from Load so the CLI can refuse
// to start (rather than silently using a partial pack).
func loadRegistry() {
	registry = map[string][]byte{
		"xianxia":       xianxiaJSON,
		"urban":         urbanJSON,
		"infinite-flow": infiniteFlowJSON,
		"apocalypse":    apocalypseJSON,
		"sci-fi":        scifiJSON,
		"horror":        horrorJSON,
	}
	for k, raw := range registry {
		var p Pack
		if err := json.Unmarshal(raw, &p); err != nil {
			loadErr = fmt.Errorf("genre: parse %s: %w", k, err)
			return
		}
		if p.ID != k {
			loadErr = fmt.Errorf("genre: pack %s declares id=%q, mismatch", k, p.ID)
			return
		}
	}
}

// Load returns the pack for the given genre id, or the
// default-fantasy fallback when the id is unknown. The
// returned error means the embedded JSON is malformed — not
// that the genre is unknown.
func Load(genreID string) (Pack, error) {
	loadOnce.Do(func() {
		loadRegistry()
	})
	if loadErr != nil {
		return Pack{}, loadErr
	}
	raw, ok := registry[genreID]
	if !ok {
		return fallbackPack(genreID), nil
	}
	var p Pack
	if err := json.Unmarshal(raw, &p); err != nil {
		return Pack{}, fmt.Errorf("genre: load %s: %w", genreID, err)
	}
	return p, nil
}

// Available returns the canonical list of genre ids, sorted
// alphabetically. Used by `novel setup` to render a
// choose-your-genre prompt and by the doctor command to
// verify every pack is parseable.
func Available() []string {
	loadOnce.Do(func() {
		loadRegistry()
	})
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// fallbackPack is the stub returned for an unknown genre id
// (including the "fantasy" default). The content is generic
// on purpose — a project that has not picked a genre gets a
// neutral style nudge, not a hard error.
func fallbackPack(genreID string) Pack {
	return Pack{
		ID:          genreID,
		DisplayName: "Generic / Fantasy",
		Description: "默认通用类型（未选择任何具体类型时的回退）。",
		StyleGuidelines: []string{
			"保持情节推进，每章末尾留下钩子",
			"对话占比 30-50%，避免大段独白",
			"主角成长要'慢热'，避免'突然觉醒'",
		},
		StyleRules: []string{
			"每章至少出现一个具体场景（地点/天气/光线/声音）",
			"主要人物的前后行动要符合其人设与动机",
			"伏笔在前 3 章内必须出现一次，否则会被遗忘",
		},
		ForbiddenPatterns: []string{
			"仿佛", "宛如", "冷笑一声", "不由得",
		},
		RecommendedPatterns: []string{
			"具体场景描写", "短促对话", "动作+心理双线",
		},
		ArcTemplates: []string{
			"起点 → 第一次冲突 → 转折点 → 高潮 → 收束",
		},
		Prompts: map[string]string{
			"systemPrefix": "你是一位资深网文作者，笔法稳健，擅长构建宏大世界观与人物成长线。",
			"chapterFocus": "本章需要：1) 推进至少一条剧情线；2) 引入一个新角色或新地点；3) 章末留下一个钩子。",
			"reviewFocus":  "剧情合理性 / 人物一致性 / 节奏感 / 钩子设计 / 伏笔追踪",
		},
	}
}

// FormatSystemBlock renders a pack as the <<GENRE_PACK>> block
// the chapter_write system prompt embeds. The block is
// intentionally compact: a 4-line header + the prompts map
// (which the role bodies can use directly), then a single
// line of bullet counts for the long lists. The full list is
// not inlined so the prompt stays cache-friendly across
// chapters.
func FormatSystemBlock(p Pack) string {
	if p.ID == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n[GENRE_PACK]\n")
	fmt.Fprintf(&b, "id: %s\n", p.ID)
	fmt.Fprintf(&b, "display_name: %s\n", p.DisplayName)
	if p.Description != "" {
		fmt.Fprintf(&b, "description: %s\n", p.Description)
	}
	if n := len(p.StyleGuidelines); n > 0 {
		fmt.Fprintf(&b, "style_guidelines: %d 条\n", n)
	}
	if n := len(p.StyleRules); n > 0 {
		fmt.Fprintf(&b, "style_rules: %d 条\n", n)
	}
	if n := len(p.ForbiddenPatterns); n > 0 {
		fmt.Fprintf(&b, "forbidden_patterns: %d 条\n", n)
	}
	if n := len(p.RecommendedPatterns); n > 0 {
		fmt.Fprintf(&b, "recommended_patterns: %d 条\n", n)
	}
	if n := len(p.ArcTemplates); n > 0 {
		fmt.Fprintf(&b, "arc_templates: %d 条\n", n)
	}
	if len(p.Prompts) > 0 {
		b.WriteString("prompts:\n")
		keys := make([]string, 0, len(p.Prompts))
		for k := range p.Prompts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "  %s: %s\n", k, p.Prompts[k])
		}
	}
	b.WriteString("[/GENRE_PACK]\n")
	return b.String()
}

// FormatDetailBlock renders a pack as a full multi-line block —
// used by the editor UI when the user opens the "genre pack"
// tab. The compact FormatSystemBlock is what the chapter_write
// prompt embeds; this is the long-form view.
func FormatDetailBlock(p Pack) string {
	var b strings.Builder
	b.WriteString(FormatSystemBlock(p))
	if len(p.StyleGuidelines) > 0 {
		b.WriteString("\n### 风格指南\n")
		for _, s := range p.StyleGuidelines {
			fmt.Fprintf(&b, "- %s\n", s)
		}
	}
	if len(p.StyleRules) > 0 {
		b.WriteString("\n### 风格规则\n")
		for _, s := range p.StyleRules {
			fmt.Fprintf(&b, "- %s\n", s)
		}
	}
	if len(p.ForbiddenPatterns) > 0 {
		b.WriteString("\n### 禁用模式\n")
		for _, s := range p.ForbiddenPatterns {
			fmt.Fprintf(&b, "- %s\n", s)
		}
	}
	if len(p.RecommendedPatterns) > 0 {
		b.WriteString("\n### 推荐模式\n")
		for _, s := range p.RecommendedPatterns {
			fmt.Fprintf(&b, "- %s\n", s)
		}
	}
	if len(p.ArcTemplates) > 0 {
		b.WriteString("\n### 大纲模板\n")
		for i, s := range p.ArcTemplates {
			fmt.Fprintf(&b, "%d. %s\n", i+1, s)
		}
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Anti-AI Rules per genre
// ---------------------------------------------------------------------------

// AntiAIRule is one row in the anti-AI rules table. It mirrors
// style.AntiAIRule so the genre package can decode JSON without
// importing style (avoiding an import cycle).
type AntiAIRule struct {
	Pattern     string `json:"pattern"`
	Replacement string `json:"replacement"`
	Category    string `json:"category"`
	Severity    string `json:"severity"`
	Layer       int    `json:"layer"`
}

// ---------------------------------------------------------------------------
// Scene templates
// ---------------------------------------------------------------------------

// SceneTemplate is the decoded shape of a single scene template
// from a genre's scenes.json file.
type SceneTemplate struct {
	ID            string   `json:"id"`
	DisplayName   string   `json:"displayName"`
	Description   string   `json:"description"`
	SceneType     string   `json:"sceneType"`
	Beats         []string `json:"beats"`
	Pacing        string   `json:"pacing"`
	VisualNotes   []string `json:"visualNotes"`
	DialogueStyle string   `json:"dialogueStyle"`
	SensoryFocus  []string `json:"sensoryFocus"`
	ExampleHook   string   `json:"exampleHook"`
}

var (
	sceneOnce     sync.Once
	sceneErr      error
	sceneRegistry map[string][]byte
)

func loadSceneRegistry() {
	sceneRegistry = map[string][]byte{
		"xianxia": xianxiaScenesJSON,
		"urban":   urbanScenesJSON,
		"horror":  horrorScenesJSON,
	}
	for k, raw := range sceneRegistry {
		var ts []SceneTemplate
		if err := json.Unmarshal(raw, &ts); err != nil {
			sceneErr = fmt.Errorf("genre: parse scenes %s: %w", k, err)
			return
		}
		_ = ts
	}
}

// LoadScenes returns the scene templates for the given genre id.
// An unknown genre returns an empty slice and no error.
func LoadScenes(genreID string) ([]SceneTemplate, error) {
	sceneOnce.Do(func() {
		loadSceneRegistry()
	})
	if sceneErr != nil {
		return nil, sceneErr
	}
	raw, ok := sceneRegistry[genreID]
	if !ok {
		return nil, nil
	}
	var ts []SceneTemplate
	if err := json.Unmarshal(raw, &ts); err != nil {
		return nil, fmt.Errorf("genre: load scenes %s: %w", genreID, err)
	}
	return ts, nil
}

// FormatSceneBlock renders a slice of scene templates as the
// <<SCENE_TEMPLATES>> block for the chapter_write system prompt.
func FormatSceneBlock(templates []SceneTemplate) string {
	if len(templates) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n[SCENE_TEMPLATES]\n")
	for _, t := range templates {
		fmt.Fprintf(&b, "- %s (%s): %s\n", t.DisplayName, t.ID, t.Description)
		fmt.Fprintf(&b, "  type: %s | pacing: %s\n", t.SceneType, t.Pacing)
		if len(t.Beats) > 0 {
			b.WriteString("  beats:\n")
			for _, beat := range t.Beats {
				fmt.Fprintf(&b, "    - %s\n", beat)
			}
		}
		if len(t.VisualNotes) > 0 {
			b.WriteString("  visual:\n")
			for _, note := range t.VisualNotes {
				fmt.Fprintf(&b, "    - %s\n", note)
			}
		}
		if t.DialogueStyle != "" {
			fmt.Fprintf(&b, "  dialogue: %s\n", t.DialogueStyle)
		}
		if len(t.SensoryFocus) > 0 {
			fmt.Fprintf(&b, "  sensory: %s\n", strings.Join(t.SensoryFocus, ", "))
		}
		if t.ExampleHook != "" {
			fmt.Fprintf(&b, "  hook: %s\n", t.ExampleHook)
		}
		b.WriteString("\n")
	}
	b.WriteString("[/SCENE_TEMPLATES]\n")
	return b.String()
}

// ---------------------------------------------------------------------------
// Anti-AI Rules per genre
// ---------------------------------------------------------------------------

// LoadAntiAIRules loads the genre-specific anti-AI rules from
// internal/novel/genre/<genreID>/anti_ai.json.
// If the file does not exist, it returns an empty slice and no error.
func LoadAntiAIRules(genreID string) ([]AntiAIRule, error) {
	if testRoot != "" {
		return loadAntiAIRulesWithRoot(genreID, testRoot)
	}
	// Try relative to working directory first (tests / dev).
	genrePath := filepath.Join("internal", "novel", "genre", genreID, "anti_ai.json")
	if rules, err := LoadAntiAIRulesFromPath(genrePath); err == nil && len(rules) > 0 {
		return rules, nil
	}
	// Fallback: relative to executable (production binary).
	exe, err := os.Executable()
	if err != nil {
		exe = "."
	}
	genrePath = filepath.Join(filepath.Dir(exe), "internal", "novel", "genre", genreID, "anti_ai.json")
	if rules, err := LoadAntiAIRulesFromPath(genrePath); err == nil && len(rules) > 0 {
		return rules, nil
	}
	// Final fallback: search upward from executable for project root.
	dir := filepath.Dir(exe)
	for i := 0; i < 5; i++ {
		genrePath = filepath.Join(dir, "internal", "novel", "genre", genreID, "anti_ai.json")
		if rules, err := LoadAntiAIRulesFromPath(genrePath); err == nil && len(rules) > 0 {
			return rules, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return []AntiAIRule{}, nil
}

// SetTestRoot is used by tests to override the project root for file loading.
// It is not safe for concurrent use and should only be called in test init.
var testRoot string

func SetTestRoot(root string) {
	testRoot = root
}

// LoadAntiAIRulesFromPath loads anti-AI rules from an explicit file path.
// If the file does not exist, it returns an empty slice and no error.
func LoadAntiAIRulesFromPath(path string) ([]AntiAIRule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []AntiAIRule{}, nil
		}
		return nil, fmt.Errorf("genre: load anti-ai rules from %s: %w", path, err)
	}
	var out []AntiAIRule
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("genre: parse anti-ai rules from %s: %w", path, err)
	}
	return out, nil
}

// loadAntiAIRulesWithRoot loads anti-AI rules given an explicit project root.
func loadAntiAIRulesWithRoot(genreID, root string) ([]AntiAIRule, error) {
	genrePath := filepath.Join(root, "internal", "novel", "genre", genreID, "anti_ai.json")
	return LoadAntiAIRulesFromPath(genrePath)
}
