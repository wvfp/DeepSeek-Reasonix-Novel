// Package novelbridge adapts the novel subsystem's tools.Tool interface
// into the host agent's tool.Tool interface so the LLM can discover and
// invoke them during a chat session. The bridge is a thin shim: it
// unmarshals the model's JSON args into map[string]any, resolves the
// per-session project.Manager, calls the novel tool, and marshals the
// result back into the string the host agent expects.
package novelbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"reasonix/internal/novel/project"
	"reasonix/internal/novel/roles"
	"reasonix/internal/novel/tools"
	"reasonix/internal/tool"
)

// managerResolver is a function that returns the project manager for
// the current working directory. It's set once at startup.
var (
	managerResolver func() (*project.Manager, error)
	managerMu       sync.RWMutex
)

// SetManagerResolver installs the function that resolves a
// *project.Manager for the current session. Called once from boot.
func SetManagerResolver(fn func() (*project.Manager, error)) {
	managerMu.Lock()
	defer managerMu.Unlock()
	managerResolver = fn
}

// resolveManager returns the project manager, or an error if no
// .novel-weaver/ project exists at cwd.
func resolveManager() (*project.Manager, error) {
	managerMu.RLock()
	fn := managerResolver
	managerMu.RUnlock()
	if fn == nil {
		return nil, fmt.Errorf("novel: 项目管理器未初始化，请先运行 novel_init")
	}
	return fn()
}

// adapter wraps a tools.Tool as a tool.Tool.
type adapter struct {
	inner  tools.Tool
	schema json.RawMessage
}

var _ tool.Tool = (*adapter)(nil)

func (a *adapter) Name() string        { return a.inner.Name() }
func (a *adapter) Description() string { return a.inner.Description() }
func (a *adapter) Schema() json.RawMessage {
	if a.schema != nil {
		return a.schema
	}
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (a *adapter) ReadOnly() bool { return false }

func (a *adapter) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	mgr, err := resolveManager()
	if err != nil {
		// novel_init is special: it creates the project, so it must
		// work even when no .novel-weaver/ exists yet. We create the
		// project directory and database here, then let the tool
		// insert the project row.
		if a.Name() == "novel_init" {
			mgr, err = project.New("")
			if err != nil {
				return "", fmt.Errorf(
					"novel_init: 无法创建项目目录: %w", err,
				)
			}
		} else {
			// Return a user-friendly Chinese error so the LLM knows to
			// suggest running novel_init first.
			return "", fmt.Errorf(
				"%s: 请先初始化小说项目。在当前目录运行 novel_init 工具或 `novel setup --name 项目名` 来创建项目。",
				a.Name(),
			)
		}
	}
	defer mgr.Close()

	var input map[string]any
	if len(args) > 0 {
		if err := json.Unmarshal(args, &input); err != nil {
			return "", fmt.Errorf("%s: invalid JSON args: %w", a.Name(), err)
		}
	}
	if input == nil {
		input = map[string]any{}
	}

	out, err := a.inner.Execute(ctx, input, mgr)
	if err != nil {
		return "", err
	}

	b, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("%s: marshal result: %w", a.Name(), err)
	}
	return string(b), nil
}

// toolSchemas maps tool names to their JSON Schema definitions.
// These are the parameter schemas the LLM sees when deciding which
// tool to call.
var toolSchemas = map[string]json.RawMessage{
	"novel_init": json.RawMessage(`{
		"type": "object",
		"properties": {
			"name":  {"type": "string", "description": "项目名称"},
			"genre": {"type": "string", "description": "题材类型：fantasy/xianxia/urban/infinite-flow/apocalypse/sci-fi/horror", "default": "fantasy"}
		},
		"required": ["name"]
	}`),

	"novel_world_create": json.RawMessage(`{
		"type": "object",
		"properties": {
			"name":        {"type": "string", "description": "世界观名称"},
			"description": {"type": "string", "description": "世界观描述"},
			"type":        {"type": "string", "description": "类型：primary/secondary/arc", "default": "primary"}
		},
		"required": ["name"]
	}`),

	"novel_world_query": json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "搜索关键词"}
		},
		"required": ["query"]
	}`),

	"novel_character_create": json.RawMessage(`{
		"type": "object",
		"properties": {
			"name":        {"type": "string", "description": "角色名称"},
			"description": {"type": "string", "description": "角色描述"},
			"role_type":   {"type": "string", "description": "角色类型：protagonist/antagonist/supporting/npc", "default": "npc"}
		},
		"required": ["name"]
	}`),

	"novel_character_query": json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "搜索关键词"}
		},
		"required": ["query"]
	}`),

	"novel_character_update": json.RawMessage(`{
		"type": "object",
		"properties": {
			"name":   {"type": "string", "description": "角色名称"},
			"fields": {"type": "object", "description": "要更新的字段，如 description、voice_profile 等"}
		},
		"required": ["name"]
	}`),

	"novel_chapter_write": json.RawMessage(`{
		"type": "object",
		"properties": {
			"title":       {"type": "string", "description": "章节标题"},
			"outline":     {"type": "string", "description": "章节大纲/概要"},
			"volume":      {"type": "integer", "description": "卷号", "default": 1},
			"chapter_num": {"type": "integer", "description": "章节序号"},
			"genre":       {"type": "string", "description": "题材包ID，覆盖项目默认题材"}
		},
		"required": ["title"]
	}`),

	"novel_chapter_continue": json.RawMessage(`{
		"type": "object",
		"properties": {
			"target_count": {"type": "integer", "description": "连续写作章节数", "default": 1}
		}
	}`),

	"novel_chapter_edit": json.RawMessage(`{
		"type": "object",
		"properties": {
			"chapter_id": {"type": "string", "description": "章节ID"},
			"action":     {"type": "string", "description": "编辑动作：replace/insert/delete"},
			"content":    {"type": "string", "description": "新内容或编辑指令"}
		},
		"required": ["chapter_id", "action"]
	}`),

	"novel_chapter_review": json.RawMessage(`{
		"type": "object",
		"properties": {
			"chapter_id": {"type": "string", "description": "章节ID，留空则审查最新章节"}
		}
	}`),

	"novel_review_fix": json.RawMessage(`{
		"type": "object",
		"properties": {
			"chapter_id": {"type": "string", "description": "章节ID"},
			"issues":     {"type": "array", "items": {"type": "string"}, "description": "要修复的问题列表"}
		},
		"required": ["chapter_id"]
	}`),

	"novel_consistency_check": json.RawMessage(`{
		"type": "object",
		"properties": {
			"chapter_id": {"type": "string", "description": "章节ID，留空则检查全部"},
			"dimensions": {"type": "array", "items": {"type": "string"}, "description": "检查维度：timeline/character_state/power_level/location/relationship"}
		}
	}`),

	"novel_arc_generate": json.RawMessage(`{
		"type": "object",
		"properties": {
			"title":    {"type": "string", "description": "故事弧标题"},
			"level":    {"type": "string", "description": "层级：master/volume/chapter/blueprint", "default": "chapter"},
			"parent_id": {"type": "string", "description": "父弧ID"}
		},
		"required": ["title"]
	}`),

	"novel_arc_show": json.RawMessage(`{
		"type": "object",
		"properties": {
			"arc_id": {"type": "string", "description": "故事弧ID，留空则显示全部"}
		}
	}`),

	"novel_arc_update": json.RawMessage(`{
		"type": "object",
		"properties": {
			"id":     {"type": "string", "description": "故事弧ID"},
			"fields": {"type": "object", "description": "要更新的字段"}
		},
		"required": ["id"]
	}`),

	"novel_pipeline_start": json.RawMessage(`{
		"type": "object",
		"properties": {
			"phase":     {"type": "string", "description": "从哪个阶段开始：setting/planning/writing/reviewing"},
			"skip_to":   {"type": "string", "description": "跳转到指定阶段"},
			"target":    {"type": "integer", "description": "写作目标章节数"}
		}
	}`),

	"novel_pipeline_status": json.RawMessage(`{
		"type": "object",
		"properties": {}
	}`),

	"novel_foreshadow_plant": json.RawMessage(`{
		"type": "object",
		"properties": {
			"description": {"type": "string", "description": "伏笔描述"},
			"keywords":    {"type": "array", "items": {"type": "string"}, "description": "触发关键词"}
		},
		"required": ["description"]
	}`),

	"novel_foreshadow_resolve": json.RawMessage(`{
		"type": "object",
		"properties": {
			"id":     {"type": "string", "description": "伏笔ID"},
			"method": {"type": "string", "description": "回收方式"}
		},
		"required": ["id"]
	}`),

	"novel_foreshadow_list": json.RawMessage(`{
		"type": "object",
		"properties": {
			"status": {"type": "string", "description": "筛选状态：planted/developing/resolved/abandoned"}
		}
	}`),

	"novel_progress_track": json.RawMessage(`{
		"type": "object",
		"properties": {}
	}`),

	"novel_progress_summary": json.RawMessage(`{
		"type": "object",
		"properties": {}
	}`),

	"novel_stats": json.RawMessage(`{
		"type": "object",
		"properties": {}
	}`),

	"novel_query": json.RawMessage(`{
		"type": "object",
		"properties": {
			"query":  {"type": "string", "description": "搜索查询"},
			"entity": {"type": "string", "description": "实体类型过滤：world/character/chapter/arc"}
		},
		"required": ["query"]
	}`),

	"novel_consistency_rules": json.RawMessage(`{
		"type": "object",
		"properties": {
			"action": {"type": "string", "description": "操作：list/add/remove", "default": "list"},
			"rule":   {"type": "string", "description": "规则内容（add/remove 时必填）"}
		}
	}`),
}

// RegisterAll creates adapters for every tool in the novel registry
// and adds them to the host tool.Registry. If sw is provided, it binds
// LLM-dependent tools (chapter_review, review_fix, consistency) to it.
// Call this once during boot after the novel subsystem is initialised.
func RegisterAll(host *tool.Registry, novelReg *tools.Registry, sw *roles.Switcher) {
	// If a switcher is provided, bind LLM-dependent tools first.
	if sw != nil {
		tools.BindSwitcher(novelReg, sw)
	}
	for _, name := range novelReg.Names() {
		t, ok := novelReg.Get(name)
		if !ok {
			continue
		}
		schema := toolSchemas[name]
		host.Add(&adapter{inner: t, schema: schema})
	}
}
