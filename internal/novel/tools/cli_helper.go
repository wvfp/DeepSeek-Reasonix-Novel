package tools

import (
	"context"
	"fmt"
	"os"

	"reasonix/internal/config"
	"reasonix/internal/novel/project"
	"reasonix/internal/provider"
)

// LoadCLIProject opens the .novel-weaver/ project at cwd. Used by the
// `novel` subcommands that need a Manager before they can call any
// tool. The error message is the canonical "请先初始化小说项目" the
// spec promises when a tool is called before novel_init.
func LoadCLIProject() (*project.Manager, error) {
	mgr, err := project.Open("")
	if err != nil {
		return nil, fmt.Errorf("load project: %w; run `novel setup` first", err)
	}
	return mgr, nil
}

// InstallProviderLLM resolves the user's configured default model from
// reasonix.toml and installs a ProviderLLMCaller on the package-level
// defaultLLM slot. novel_chapter calls this at startup so chapter_write
// has a real LLM to talk to. When no model is configured (e.g. a user
// with no reasonix.toml yet) it leaves the slot untouched and returns
// (nil, nil) so chapter_write's "no LLM configured" branch fires
// cleanly with its existing Chinese error message.
func InstallProviderLLM(ctx context.Context, cfg *config.Config, modelName string) (LLMCaller, error) {
	if cfg == nil {
		var err error
		cfg, err = config.Load()
		if err != nil {
			return nil, nil
		}
	}
	if modelName == "" {
		modelName = cfg.DefaultModel
	}
	entry, ok := cfg.ResolveModel(modelName)
	if !ok {
		return nil, nil
	}
	if entry.APIKey() == "" {
		// Surface a clear Chinese message so the user knows the
		// next step; the chat banner won't have caught it on a
		// non-interactive `novel chapter` run.
		if entry.APIKeyEnv != "" {
			fmt.Fprintf(os.Stderr, "警告：未设置 %s，无法调用 LLM。请运行 `reasonix setup` 配置 API key。\n", entry.APIKeyEnv)
		}
		return nil, nil
	}
	pcfg := provider.Config{
		Name:    entry.Name,
		BaseURL: entry.BaseURL,
		Model:   entry.Model,
		APIKey:  entry.APIKey(),
		Extra: map[string]any{
			"thinking": entry.Thinking,
			"effort":   entry.Effort,
		},
	}
	p, err := provider.New(entry.Kind, pcfg)
	if err != nil {
		return nil, fmt.Errorf("install provider: %w", err)
	}
	caller := &ProviderLLMCaller{Provider: p}
	SetDefaultLLMCaller(caller)
	return caller, nil
}
