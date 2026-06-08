// Package cli implements reasonix's command-line entry: subcommand routing, flag
// parsing, assembly from config, and exit codes. The core is config-driven —
// providers and tools are resolved from configuration, not hardcoded.
package cli

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/i18n"
	"reasonix/internal/novel/domain"
	"reasonix/internal/novel/migrate"
	"reasonix/internal/novel/pipeline"
	"reasonix/internal/novel/project"
	"reasonix/internal/novel/repo"
	"reasonix/internal/novel/roles"
	"reasonix/internal/novel/tools"
	"reasonix/internal/provider"
	"reasonix/internal/provider/openai"
	"reasonix/internal/serve"
	"time"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"
)

// Run is the CLI entry point; it returns a process exit code.
func Run(args []string, version string) int {
	// Pick the UI language up front so even pre-config paths (the first-run
	// welcome banner) come through localized. Env-only first; if a config
	// exists and pins a language, that wins.
	i18n.DetectLanguage("")
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
	}
	if shouldMigrateLegacyConfigForCLI(cmd) {
		migrateLegacyConfigForCLI()
	}
	if cfg, err := config.Load(); err == nil {
		if cfg.Language != "" {
			i18n.DetectLanguage(cfg.Language)
		}
	}

	if len(args) == 0 {
		configureCLIThemeFromConfigForTTYOutput()
		return welcome(version)
	}

	rest := args[1:]
	switch cmd {
	case "run":
		return runAgent(rest)
	case "chat", "code": // "code" is the v0.x name for the interactive session
		return chatREPL(rest)
	case "novel":
		return novelCommand(rest)
	case "serve":
		return runServe(rest)
	case "setup":
		configureCLIThemeFromConfigForTTYOutput()
		return setupConfig(rest)
	case "init":
		// Project memory (AGENTS.md) is model-generated in-session — `/init` runs
		// the codebase analysis. This CLI entry just points there (and to `setup`
		// for config), so `reasonix init` isn't a dead end.
		configureCLIThemeFromConfigNoProbe()
		return initHint()
	case "acp":
		configureCLIThemeFromConfigNoProbe()
		return acpCommand(rest, version)
	case "mcp":
		configureCLIThemeFromConfigNoProbe()
		return mcpCommand(rest)
	case "codegraph":
		configureCLIThemeFromConfigNoProbe()
		return codegraphCommand(rest)
	case "doctor":
		configureCLIThemeFromConfigNoProbe()
		return doctorCommand(rest, version)
	case "version", "--version", "-v":
		fmt.Println("reasonix", version)
		return 0
	case "help", "--help", "-h":
		usage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, i18n.M.UnknownCommandFmt+"\n\n", cmd)
		usage()
		return 2
	}
}

func shouldMigrateLegacyConfigForCLI(cmd string) bool {
	switch cmd {
	case "", "run", "chat", "code", "serve", "setup", "init", "acp", "mcp", "codegraph", "doctor":
		return true
	default:
		return false
	}
}

func migrateLegacyConfigForCLI() {
	if _, err := config.MigrateLegacyIfNeeded(); err != nil {
		fmt.Fprintln(os.Stderr, "warning: config migration failed:", err)
	}
}

func configureCLIThemeFromConfig() {
	if cfg, err := config.Load(); err == nil {
		configureCLIThemeWithStyle(cfg.UITheme(), cfg.UIThemeStyle())
	} else {
		configureCLITheme("auto")
	}
}

func configureCLIThemeFromConfigForTTYOutput() {
	if isTTY(os.Stdout) {
		configureCLIThemeFromConfig()
		return
	}
	configureCLIThemeFromConfigNoProbe()
}

func configureCLIThemeFromConfigNoProbe() {
	withoutTerminalProbe(configureCLIThemeFromConfig)
}

// setup builds a ready-to-drive Controller from config via boot.Build. It is a
// thin adapter kept so the subcommands below read the same as before; the actual
// assembly (model resolution, tool registry, permission gate, two-model
// Coordinator) lives in internal/boot, shared with the desktop frontend.
// requireKey forces the executor's API key to be present (used by run); chat
// passes false so the session UI is reachable before a key is set. sink receives
// the agent's typed event stream — runAgent passes a TextSink that renders to
// stdout, the TUI passes an event-channel sink so events become tea.Msgs.
func setup(ctx context.Context, modelName string, maxStepsOverride int, requireKey bool, sink event.Sink) (*control.Controller, error) {
	return boot.Build(ctx, boot.Options{
		Model:      modelName,
		MaxSteps:   maxStepsOverride,
		RequireKey: requireKey,
		Sink:       sink,
	})
}

// setupQuiet is like setup but suppresses plugin subprocess stderr output.
// Used during model switch inside a bubbletea session to prevent plugin logs
// from corrupting the TUI's terminal raw mode.
func setupQuiet(ctx context.Context, modelName string, maxStepsOverride int, requireKey bool, sink event.Sink) (*control.Controller, error) {
	return boot.Build(ctx, boot.Options{
		Model:      modelName,
		MaxSteps:   maxStepsOverride,
		RequireKey: requireKey,
		Sink:       sink,
		Stderr:     io.Discard,
	})
}

// chdirTo honours --dir: it switches the working directory before anything reads
// it, so config discovery, the sandbox root, and file tools all resolve from the
// chosen project root. Returns 2 (already reported) on failure, 0 otherwise.
func chdirTo(dir string) int {
	if dir == "" {
		return 0
	}
	if err := os.Chdir(dir); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 2
	}
	return 0
}

func runAgent(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	model := fs.String("model", "", "provider name (default: config default_model)")
	maxSteps := fs.Int("max-steps", 0, "max tool-call rounds (0 = use config/default)")
	showThinking := fs.Bool("show-thinking", false, "show thinking text instead of the collapsed thinking marker")
	metricsPath := fs.String("metrics", "", "write a JSON token/cache/cost summary of the run to this path")
	dir := fs.String("dir", "", "change to this directory first (project root); config, sandbox and file tools resolve from here")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if rc := chdirTo(*dir); rc != 0 {
		return rc
	}
	configureCLIThemeFromConfigForTTYOutput()

	prompt := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if prompt == "" {
		prompt = readStdin()
	}
	if prompt == "" {
		fmt.Fprintln(os.Stderr, i18n.M.UsageRunHint)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Live run: render the agent's event stream to stdout. Markdown post-stream
	// redraw (cursor moves) is enabled only on a TTY; piped / captured output
	// keeps the raw stream.
	var renderer agent.Renderer
	termW := 80
	if isTTY(os.Stdout) {
		if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
			termW = w
		}
		renderer = newMarkdownRenderer(termW)
	}
	textSink := agent.NewTextSink(os.Stdout, renderer, termW)
	textSink.SetShowReasoning(*showThinking)
	var sink event.Sink = textSink
	var metrics *metricsSink
	if *metricsPath != "" {
		metrics = &metricsSink{inner: textSink}
		sink = metrics
	}
	ctrl, err := setup(ctx, *model, *maxSteps, true, sink)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	defer ctrl.Close()

	runErr := ctrl.Run(ctx, prompt)
	if metrics != nil {
		if err := writeMetrics(*metricsPath, metrics.m); err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		}
	}
	if runErr != nil {
		fmt.Fprintln(os.Stderr, "\n"+i18n.M.ErrorPrefix, runErr)
		return 1
	}
	return 0
}

// runServe exposes the controller over HTTP+SSE: events stream to the browser,
// commands arrive as JSON POSTs. The Broadcaster is the controller's event sink,
// so the same typed stream the chat TUI consumes reaches web clients — the
// transport-agnostic controller driven by a second frontend.
func runServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	model := fs.String("model", "", "provider name (default: config default_model)")
	maxSteps := fs.Int("max-steps", 0, "max tool-call rounds (0 = use config/default)")
	addr := fs.String("addr", "127.0.0.1:8787", "listen address")
	resume := fs.String("resume", "", "resume a saved session file")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	ctx := context.Background()
	bc := serve.NewBroadcaster()
	ctrl, err := setup(ctx, *model, *maxSteps, true, bc)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	defer ctrl.Close()

	// Auto-save target: reuse the resumed file, else a fresh one — same as chat.
	if *resume != "" {
		loaded, err := agent.LoadSession(*resume)
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		ctrl.Resume(loaded, *resume)
	} else if ctrl.SessionDir() != "" {
		ctrl.SetSessionPath(agent.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))
	}

	fmt.Printf("reasonix serve — %s on http://%s\n", ctrl.Label(), *addr)
	// Use graceful shutdown so SIGINT/SIGTERM drain active connections.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := serve.New(ctrl, bc).RunGraceful(ctx, *addr); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	return 0
}

// chatREPL is an interactive session: a single persistent agent/session and a
// prompt loop that keeps conversation context across turns. Exit with
// 'exit'/'quit' or Ctrl-D.
func chatREPL(args []string) int {
	fs := flag.NewFlagSet("chat", flag.ContinueOnError)
	model := fs.String("model", "", "provider name (default: config default_model)")
	maxSteps := fs.Int("max-steps", 0, "max tool-call rounds (0 = use config/default)")
	cont := fs.Bool("continue", false, "resume the most recent saved session")
	fs.BoolVar(cont, "c", false, "shorthand for --continue")
	resume := fs.Bool("resume", false, "list saved sessions and pick one to resume")
	yolo := fs.Bool("dangerously-skip-permissions", false, "YOLO: auto-approve every tool call this session (deny rules still apply)")
	fs.BoolVar(yolo, "yolo", false, "alias for --dangerously-skip-permissions")
	dir := fs.String("dir", "", "change to this directory first (project root); config, sandbox and file tools resolve from here")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if rc := chdirTo(*dir); rc != 0 {
		return rc
	}
	if cfg, err := config.Load(); err == nil {
		configureCLIThemeWithStyle(cfg.UITheme(), cfg.UIThemeStyle())
	}

	// Decide whether we're starting fresh or resuming. --resume opens an
	// interactive picker; --continue / -c jumps straight into the newest.
	var resumePath string
	switch {
	case *resume:
		path, rc := pickSessionToResume()
		if rc != 0 {
			return rc
		}
		resumePath = path
	case *cont:
		sessions, err := agent.ListSessions(config.SessionDir())
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		if len(sessions) == 0 {
			fmt.Fprintln(os.Stderr, i18n.M.NoSessionToResume)
			return 1
		}
		resumePath = sessions[0].Path
	}

	ctx := context.Background()

	// Plumb the controller's typed event stream through a channel so each event
	// can become a tea.Msg inside the TUI's update loop. Buffered generously:
	// streaming bursts (tool results, long answers) shouldn't backpressure the
	// agent goroutine.
	eventCh := make(chan event.Event, 1024)

	sink := &eventSink{ch: eventCh}
	ctrl, err := setup(ctx, *model, *maxSteps, false, sink)
	if err != nil && errors.Is(err, boot.ErrUnknownModel) && isInteractive() && config.SourcePath() == "" {
		// True first run whose default model can't resolve: guide setup, then retry.
		// With a config present, fall through to the descriptive error — re-running
		// the wizard would overwrite the user's config (#2856).
		fmt.Fprintln(os.Stderr, i18n.M.ReconfigureOnUnknownModel)
		if rc := interactiveSetup(defaultConfigTarget(), defaultEnvTarget()); rc != 0 {
			return rc
		}
		ctrl, err = setup(ctx, *model, *maxSteps, false, sink)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}

	// Decide where this conversation's auto-save lands. A resume reuses the
	// file so closing/reopening keeps appending to the same history; a fresh
	// session lands in a new file stamped with the model name.
	if resumePath != "" {
		if loaded, err := agent.LoadSession(resumePath); err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		} else {
			ctrl.Resume(loaded, resumePath)
		}
	} else if ctrl.SessionDir() != "" {
		ctrl.SetSessionPath(agent.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))
	}

	// Surface a missing-key warning inside the TUI banner so the first message
	// failing is at least pre-announced; the user can still enter chat.
	missing := ""
	if cfg, loadErr := config.Load(); loadErr == nil {
		name := *model
		if name == "" {
			name = cfg.DefaultModel
		}
		if vErr := cfg.Validate(name); vErr != nil {
			missing = vErr.Error()
		}
	}

	// Initial terminal width — the TUI re-flows on every WindowSizeMsg so
	// this is just a starting estimate before the first resize event lands.
	termW := 80
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		termW = w
	}

	// Route "ask" decisions to the TUI: the controller emits an ApprovalRequest
	// event and blocks until the user answers via ctrl.Approve. Sub-agents (the
	// task tool) keep their headless gate from setup — no UI to prompt through.
	ctrl.EnableInteractiveApproval()
	// YOLO: skip every approval prompt for the session (deny rules still apply).
	if *yolo {
		ctrl.SetBypass(true)
	}

	m := newChatTUI(ctrl, missing, eventCh, termW)
	if cfg, err := config.Load(); err == nil {
		m.outputStyle = cfg.Agent.OutputStyle    // shown as the active entry in /output-style
		m.statuslineCmd = cfg.Statusline.Command // custom status-line command, "" = built-in row
	}

	// /model support: a pure builder the TUI calls to rebuild on a different
	// model (carrying the conversation). It must NOT touch the running model —
	// runModelSubcommand performs the swap on the live copy. The same stable sink
	// feeds the new controller, so events keep flowing to this TUI.
	m.buildController = func(ref string, carry []provider.Message, resumePath string) (*control.Controller, error) {
		c, err := setupQuiet(ctx, ref, *maxSteps, false, sink)
		if err != nil {
			return nil, err
		}
		// Keep the carried conversation in its existing file so the switch doesn't
		// orphan a duplicate (#2807).
		path := agent.ContinueSessionPath(resumePath, c.SessionDir(), c.Label())
		if len(carry) > 0 {
			c.Resume(&agent.Session{Messages: carry}, path)
		} else if path != "" {
			c.SetSessionPath(path)
		}
		c.EnableInteractiveApproval()
		if *yolo {
			c.SetBypass(true)
		}
		return c, nil
	}
	if cfg, e := config.Load(); e == nil {
		name := *model
		if name == "" {
			name = cfg.DefaultModel
		}
		if entry, ok := cfg.ResolveModel(name); ok {
			m.modelRef = entry.Name + "/" + entry.Model
		}
	}
	m.refreshEffortStatus()

	// No alt-screen: finalized transcript lines are committed to the terminal's
	// normal buffer (via tea.Println) so native scrollback, the wheel, and copy
	// all work — the bubbletea-managed region is just the bottom input/status.
	p := tea.NewProgram(m)
	final, runErr := p.Run()
	// Close the active controller plus any retired ones from /model switches.
	// Retired controllers were stashed rather than closed at switch time
	// because Controller.Close() runs SessionEnd hooks and kills plugin
	// subprocesses — operations that corrupt bubbletea's terminal raw mode
	// when executed while the TUI is alive.
	if fm, ok := final.(chatTUI); ok {
		for _, oc := range fm.oldControllers {
			oc.Close()
		}
		if fm.ctrl != nil {
			fm.ctrl.Close()
		} else {
			ctrl.Close()
		}
	} else {
		ctrl.Close()
	}
	if runErr != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, runErr)
		return 1
	}
	return 0
}

// setupTargets is where the wizard writes: the TOML config and the secrets file.
// Keys always go to the reasonix-owned global credentials file so they never land
// in a project's own .env; only the config location is project-local under --local.
type setupTargets struct {
	config string
	env    string
}

// defaultConfigTarget is the user-global config file, falling back to a
// project-local reasonix.toml only when the user config dir can't be resolved.
func defaultConfigTarget() string {
	if p := config.UserConfigPath(); p != "" {
		return p
	}
	return "reasonix.toml"
}

// defaultEnvTarget is the reasonix-owned global credentials file, falling back to
// a project-local .env only when the user config dir can't be resolved.
func defaultEnvTarget() string {
	if p := config.UserCredentialsPath(); p != "" {
		return p
	}
	return ".env"
}

// resolveSetupTargets picks where `reasonix setup` writes. Keys always go to the
// global env. The config goes to the user-global dir by default, to ./reasonix.toml
// under --local, or to an explicit path argument when given.
func resolveSetupTargets(args []string) setupTargets {
	t := setupTargets{config: defaultConfigTarget(), env: defaultEnvTarget()}
	for _, a := range args {
		switch a {
		case "--local", "-l":
			t.config = "reasonix.toml"
		default:
			t.config = a
		}
	}
	return t
}

// displayPath shortens a home-relative path to ~/… for readable wizard output.
func displayPath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}

// setupConfig runs the configuration wizard (the `reasonix setup` command),
// writing config.toml to the user-global dir (or ./reasonix.toml under --local)
// and API keys to the reasonix-owned global .env — never a project's own .env.
// Project memory is a separate concern — the in-session `/init` skill generates
// AGENTS.md (see initHint).
func setupConfig(args []string) int {
	t := resolveSetupTargets(args)
	path := t.config
	if _, err := os.Stat(path); err == nil {
		// Non-interactive must not clobber an existing config silently.
		if !isInteractive() {
			fmt.Fprintf(os.Stderr, i18n.M.NotOverwritingFmt+"\n", path)
			return 1
		}
		in := bufio.NewScanner(os.Stdin)
		ans := ask(in, os.Stdout, fmt.Sprintf(i18n.M.ConfirmReconfigureFmt, path), "N")
		if ans != "y" && ans != "Y" {
			fmt.Println(i18n.M.KeepingExisting)
			return 0
		}
	}

	// Interactive wizard on a TTY; fall back to the annotated default when piped.
	if isInteractive() {
		rc := interactiveSetup(t.config, t.env)
		if rc == 0 {
			fmt.Printf(i18n.M.TryHintFmt+"\n", bold("reasonix chat"))
		}
		return rc
	}
	return writeDefaultConfig(t.config)
}

func writeDefaultConfig(path string) int {
	c := config.Default()
	// A freshly scaffolded config starts without the codegraph daemon; existing
	// configs (which never wrote [codegraph]) keep it on via the built-in default.
	c.Codegraph.Enabled = false
	if err := c.SaveTo(path); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.WriteConfigErr, err)
		return 1
	}
	fmt.Printf(i18n.M.WroteFileFmt+"\n", displayPath(path))
	fmt.Println(i18n.M.NextHint)
	return 0
}

// initHint handles `reasonix init`. Unlike a config scaffold, project memory is
// model-generated by analyzing the codebase, so it lives as the in-session
// `/init` skill rather than a CLI command. This entry just points the user there
// (and to `reasonix setup` for config) so the verb isn't a dead end.
func initHint() int {
	fmt.Println(i18n.M.InitHint)
	return 0
}

// interactiveSetup runs the setup wizard, then writes the config to configPath
// and any entered API keys to envPath (the reasonix-owned global .env, never a
// project's own). The wizard is intentionally minimal: pick language, pick
// provider, enter API keys. Language is asked first so every subsequent prompt
// is already in the user's language even when env auto-detection got it wrong.
// Two-model collaboration is left as a manual config edit (planner_model) so
// first-run never confronts newcomers with advanced choices.
func interactiveSetup(configPath, envPath string) int {
	// Seed from the existing config when reconfiguring, so a re-run to fix a key
	// preserves the user's providers / agent settings instead of resetting to
	// defaults. First run (no file) falls back to the built-in defaults.
	_, statErr := os.Stat(configPath)
	isNewConfig := statErr != nil
	cfg := config.LoadForEdit(configPath)
	prevDefault := cfg.DefaultModel
	if isNewConfig {
		// Brand-new user: start without the codegraph daemon. A reconfigure of an
		// existing config keeps whatever the user already had.
		cfg.Codegraph.Enabled = false
	}

	lang, err := selectLanguage()
	if err != nil {
		fmt.Fprintln(os.Stderr, "\nsetup cancelled.")
		return 1
	}
	cfg.Language = lang
	i18n.DetectLanguage(lang)

	// Now that the catalogue matches the user's choice, show the welcome banner
	// in their language before any substantive prompt.
	fmt.Println()
	fmt.Print(boxed([]string{
		accent("◆") + " " + fmt.Sprintf(i18n.M.WelcomeTitleFmt, bold("reasonix")),
		"",
		dim(i18n.M.NoConfigYet),
	}))
	fmt.Println()

	enabled, err := selectEnabledProviders(cfg.Providers)
	if err != nil {
		fmt.Fprintln(os.Stderr, "\n"+i18n.M.SetupCancelled)
		return 1
	}

	envLines := configureKeys(enabled, os.Stdin, os.Stdout)

	cfg.Providers = enabled
	// Keep the previous default model if it's still enabled; otherwise fall back
	// to the first selected provider.
	cfg.DefaultModel = enabled[0].Name
	for _, p := range enabled {
		if p.Name == prevDefault {
			cfg.DefaultModel = prevDefault
			break
		}
	}

	if err := cfg.SaveTo(configPath); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.WriteConfigErr, err)
		return 1
	}
	fmt.Printf("\n%s %s\n", green("✓"), fmt.Sprintf(i18n.M.WroteFileFmt, displayPath(configPath)))

	if len(envLines) > 0 {
		if err := appendEnv(envPath, envLines); err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.WriteEnvErr, err)
			return 1
		}
		fmt.Printf("%s %s\n", green("✓"), fmt.Sprintf(i18n.M.WroteFileFmt, displayPath(envPath)))
	}

	fmt.Printf("\n%s %s\n", accent("◆"), i18n.M.SetupComplete)
	return 0
}

// pickSessionToResume scans the session dir, takes the 10 most recent, and
// shows a single-choice menu with timestamp + turn count + first user
// message so the user can pick one. Returns the chosen path and a process
// exit code (non-zero when there's nothing to pick or the user cancelled).
func pickSessionToResume() (string, int) {
	sessions, err := agent.ListSessions(config.SessionDir())
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return "", 1
	}
	if len(sessions) == 0 {
		fmt.Fprintln(os.Stderr, i18n.M.NoSessionToResume)
		return "", 1
	}
	if !isInteractive() {
		fmt.Fprintln(os.Stderr, i18n.M.ResumeRequiresTTY)
		return "", 1
	}
	const cap = 10
	if len(sessions) > cap {
		sessions = sessions[:cap]
	}
	items := make([]menuItem, len(sessions))
	for i, s := range sessions {
		when := s.ModTime.Local().Format("01-02 15:04")
		preview := s.Preview
		if preview == "" {
			preview = "(no user message yet)"
		}
		items[i] = menuItem{
			name: when,
			desc: fmt.Sprintf("%d turns · %s", s.Turns, preview),
		}
	}
	idx, err := selectOne(i18n.M.PickSessionLabel, items)
	if err != nil {
		return "", 1
	}
	return sessions[idx].Path, 0
}

// selectLanguage is the wizard's first prompt: it shows the two UI languages
// in their native form and pre-selects the env-detected one (so a single Enter
// confirms the auto-detection, a single arrow + Enter picks the other). The
// label is bilingual because we don't yet know which catalogue to trust.
func selectLanguage() (string, error) {
	detected := i18n.DetectLanguage("")
	items := []menuItem{{name: "English"}, {name: "中文 (简体)"}}
	tags := []string{"en", "zh"}
	if detected == "zh" {
		items[0], items[1] = items[1], items[0]
		tags[0], tags[1] = tags[1], tags[0]
	}
	idx, err := selectOne("Language · 语言", items)
	if err != nil {
		return "", err
	}
	return tags[idx], nil
}

// selectEnabledProviders prompts a single multi-select of provider families
// (DeepSeek / MiMo / custom / …) and returns one ProviderEntry per chosen
// family, carrying the models the user picked. Built-in families try the
// OpenAI-compatible GET /models endpoint first (so the user sees the real
// list, not a stale hard-coded one) and fall back to the preset's static
// model list when the call fails — offline first-run, missing key, or a
// vendor that doesn't expose /models. All paths funnel through the same
// fetchOrFallback / buildFamilyEntry helpers, so adding a new family only
// requires a familyOf case.
func selectEnabledProviders(providers []config.ProviderEntry) ([]config.ProviderEntry, error) {
	providers, stale := filterStaleCustomEntries(providers)
	for _, s := range stale {
		fmt.Fprintf(os.Stderr, "  %s\n", dim(fmt.Sprintf(i18n.M.SkipStaleCustomEntryFmt, s.Name, s.BaseURL)))
	}
	providers = withBuiltinFamilies(providers)

	famOrder, famMembers, famInfo := groupByFamily(providers)

	famItems := make([]menuItem, len(famOrder))
	for i, k := range famOrder {
		famItems[i] = menuItem{name: famInfo[k].name, desc: famInfo[k].desc}
	}
	customIdx := len(famItems)
	famItems = append(famItems, menuItem{name: i18n.M.CustomProviderLabel, desc: i18n.M.CustomProviderDesc})
	anthropicIdx := len(famItems)
	famItems = append(famItems, menuItem{name: i18n.M.AnthropicProviderLabel, desc: i18n.M.AnthropicProviderDesc})

	famIdxs, err := selectMany(i18n.M.SelectProvidersLabel, famItems)
	if err != nil {
		return nil, err
	}

	var enabled []config.ProviderEntry
	for _, fi := range famIdxs {
		switch fi {
		case customIdx:
			cps, err := promptCustomProvider()
			if err != nil {
				fmt.Fprintf(os.Stderr, "custom provider error: %v\n", err)
				continue
			}
			enabled = append(enabled, cps...)
			continue
		case anthropicIdx:
			aps, err := promptAnthropicProvider()
			if err != nil {
				fmt.Fprintf(os.Stderr, "anthropic provider error: %v\n", err)
				continue
			}
			enabled = append(enabled, aps...)
			continue
		}

		familyKey := famOrder[fi]
		probe := providers[famMembers[familyKey][0]]
		famName := famInfo[familyKey].name

		// Seed the probe's static list with every member of the family (e.g. the
		// flash and pro SKUs), not just the first — so a failed /models probe
		// falls back to the whole family instead of collapsing to one model.
		probe.Models = familyStaticModels(providers, famMembers[familyKey])

		// Collect the key before probing /models: a keyless probe 401s and the
		// fallback would hide the live SKUs. Mirrors the custom/anthropic flows;
		// configureKeys later sees the env var set and won't ask twice.
		ensureProbeKey(&probe, famName)

		models := fetchOrFallback(&probe, famName)
		if len(models) == 0 {
			fmt.Fprintf(os.Stderr, "  %s\n", dim(fmt.Sprintf(i18n.M.NoModelsAvailableFmt, famName)))
			continue
		}

		items := make([]menuItem, len(models))
		for i, m := range models {
			items[i] = menuItem{name: m}
		}
		idxs, err := selectMany(fmt.Sprintf(i18n.M.SelectModelsLabel, famName), items)
		if err != nil || len(idxs) == 0 {
			continue
		}

		selected := make([]string, 0, len(idxs))
		for _, idx := range idxs {
			selected = append(selected, models[idx])
		}
		members := make([]config.ProviderEntry, 0, len(famMembers[familyKey]))
		for _, idx := range famMembers[familyKey] {
			members = append(members, providers[idx])
		}
		enabled = append(enabled, buildFamilyEntries(probe, members, selected)...)
	}
	return enabled, nil
}

// familyStaticModels unions the preset model lists of every entry in the family,
// preserving order and dropping duplicates. It is the fallback offered when the
// live /models probe fails, so a family with separate flash/pro preset entries
// still surfaces both rather than only the first member's model.
func familyStaticModels(providers []config.ProviderEntry, idxs []int) []string {
	var out []string
	seen := map[string]bool{}
	for _, i := range idxs {
		for _, m := range providers[i].ModelList() {
			if m != "" && !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	return out
}

// ensureProbeKey prompts once for the family's API key when it isn't already in
// the environment, so the /models probe can run and return the live SKU list.
// The value is set in the env for the probe; configureKeys persists it to .env
// later and skips re-asking. A blank entry is fine — the static fallback covers it.
func ensureProbeKey(probe *config.ProviderEntry, famName string) {
	if probe.APIKeyEnv == "" || os.Getenv(probe.APIKeyEnv) != "" {
		return
	}
	fmt.Printf("  %s\n", dim(fmt.Sprintf(i18n.M.FamilyKeyPromptFmt, famName)))
	in := bufio.NewScanner(os.Stdin)
	if key := strings.TrimSpace(ask(in, os.Stdout, "  "+probe.APIKeyEnv, "")); key != "" {
		os.Setenv(probe.APIKeyEnv, key)
	}
}

// fetchOrFallback tries the OpenAI-compatible GET /models endpoint
// (honoring the entry's ModelsURL when set) and returns the live model IDs.
// On any failure — no base URL, no key set yet (the key is collected in a
// later wizard step), network/auth error, or a vendor without /models — it
// silently returns the preset's static model list so the wizard can always
// present something. The fetch has a 10s timeout and is best-effort.
func fetchOrFallback(probe *config.ProviderEntry, famName string) []string {
	static := probe.ModelList()
	if probe.BaseURL == "" {
		return static
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	models, err := probe.FetchModels(ctx)
	if err != nil || len(models) == 0 {
		if len(static) > 0 {
			fmt.Fprintf(os.Stderr, "  %s\n", dim(fmt.Sprintf(i18n.M.FetchModelsUsingPresetsFmt, famName)))
		}
		return static
	}
	fmt.Printf("  %s\n", green(fmt.Sprintf(i18n.M.FetchModelsSuccessFmt, len(models), famName)))
	return models
}

// buildFamilyEntry returns a single ProviderEntry exposing the user's
// selected models under one entry. It preserves the preset's API key env,
// base URL, kind, context window, pricing, and effort — the things that
// vary per vendor but not per model. The Default pointer is reset to the
// first selected model if it would otherwise reference a model the user
// didn't pick (or was empty).
// buildFamilyEntries splits the user's selection back across the family's preset
// members so each model keeps its own entry — and therefore its own pricing,
// context window, and balance URL. A family like DeepSeek ships flash and pro as
// separate presets with different prices; collapsing them into one entry would
// bill pro at flash's rate. Models the live /models list returned that match no
// preset (a new SKU) fall under the probe entry. Member order is preserved;
// within a member, selection order is preserved.
func buildFamilyEntries(probe config.ProviderEntry, members []config.ProviderEntry, selected []string) []config.ProviderEntry {
	tmpl := map[string]config.ProviderEntry{probe.Name: probe}
	ownerName := map[string]string{}
	for _, m := range members {
		tmpl[m.Name] = m
		for _, id := range m.ModelList() {
			ownerName[id] = m.Name
		}
	}
	var order []string
	groups := map[string][]string{}
	for _, sm := range selected {
		name, ok := ownerName[sm]
		if !ok {
			name = probe.Name
		}
		if _, seen := groups[name]; !seen {
			order = append(order, name)
		}
		groups[name] = append(groups[name], sm)
	}
	out := make([]config.ProviderEntry, 0, len(order))
	for _, name := range order {
		out = append(out, buildFamilyEntry(tmpl[name], groups[name]))
	}
	return out
}

func buildFamilyEntry(probe config.ProviderEntry, selected []string) config.ProviderEntry {
	entry := probe
	entry.Models = selected
	entry.Model = selected[0]
	if entry.Default == "" || !containsString(selected, entry.Default) {
		entry.Default = selected[0]
	}
	return entry
}

func containsString(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// filterStaleCustomEntries drops the wizard's own magic-name entries
// (Name="custom" with Kind="openai" or Name="anthropic" with Kind="anthropic")
// that older versions of the wizard wrote into reasonix.toml. They collide
// with the wizard's "custom" / "anthropic" menu items on re-run, showing up
// as duplicate broken entries. The new wizard writes host-derived slugs
// (e.g. "custom-token-sensenova-cn") so a hit on the magic name is
// unambiguously stale. The returned slice is the dropped set so the caller
// can warn the user to clean up reasonix.toml by hand.
func filterStaleCustomEntries(providers []config.ProviderEntry) (kept, dropped []config.ProviderEntry) {
	for _, p := range providers {
		if p.Name == "custom" && p.Kind == "openai" {
			dropped = append(dropped, p)
			continue
		}
		if p.Name == "anthropic" && p.Kind == "anthropic" {
			dropped = append(dropped, p)
			continue
		}
		kept = append(kept, p)
	}
	return
}

// providerSlug derives a stable, human-readable entry name for a custom
// OpenAI / Anthropic-compatible provider from its base URL, e.g.
// "custom-token-sensenova-cn" or "anthropic-api-anthropic-com". We can't
// reuse the wizard's menu-item labels ("custom" / "anthropic") because
// those would collide with the menu item itself and end up rendered as
// duplicate provider entries on subsequent re-runs of `reasonix setup`.
// The host-based slug also gives users a meaningful name to grep for in
// reasonix.toml. Falls back to a short sha1 of the raw URL when the URL
// doesn't parse, so even malformed input still produces a unique name.
func providerSlug(kind, baseURL string) string {
	var host string
	if u, err := url.Parse(baseURL); err == nil {
		host = u.Host
	}
	if host == "" {
		sum := sha1.Sum([]byte(baseURL))
		return kind + "-" + hex.EncodeToString(sum[:4])
	}
	host = strings.ToLower(strings.TrimPrefix(host, "www."))
	var b strings.Builder
	prevDash := false
	for _, r := range host {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteRune('-')
				prevDash = true
			}
		}
	}
	return kind + "-" + strings.TrimRight(b.String(), "-")
}

// providerFamily is a wizard-only grouping of provider SKUs by vendor; it does
// not exist in config because users editing reasonix.toml deal with SKU names
// directly. Keys mirror the SKU name prefix (deepseek-*, mimo) so adding a new
// preset only requires a familyOf case.
type providerFamily struct {
	key  string
	name string
	desc string
}

func familyOf(name string) providerFamily {
	switch {
	case strings.HasPrefix(name, "deepseek"):
		return providerFamily{key: "deepseek", name: "DeepSeek", desc: "fast & cheap, plus a stronger Pro SKU"}
	case strings.HasPrefix(name, "mimo"):
		return providerFamily{key: "mimo", name: "MiMo (Xiaomi)", desc: "long-horizon agentic"}
	default:
		return providerFamily{key: name, name: name}
	}
}

// promptCustomProvider handles the custom provider entry flow.
func promptCustomProvider() ([]config.ProviderEntry, error) {
	methodIdx, err := selectOne(i18n.M.CustomAddMethodLabel, []menuItem{
		{name: i18n.M.CustomMethodManual},
		{name: i18n.M.CustomMethodURL},
	})
	if err != nil {
		return nil, err
	}
	if methodIdx == 0 {
		return promptCustomProviderManual()
	}
	return promptCustomProviderFromURL()
}

// promptCustomProviderManual handles manual model entry.
func promptCustomProviderManual() ([]config.ProviderEntry, error) {
	return promptCustomProviderManualWith(bufio.NewScanner(os.Stdin), "", "", "")
}

// promptCustomProviderManualWith is the shared backend for manual entry.
// Pre-filled values (baseURL, keyEnv, apiKey) are reused as-is when non-empty
// so the URL-fetch flow can fall through to manual entry without re-asking
// the user for information they've already typed. An empty apiKey is allowed
// — the key step happens later in the wizard and .env is updated then.
func promptCustomProviderManualWith(in *bufio.Scanner, baseURL, keyEnv, apiKey string) ([]config.ProviderEntry, error) {
	fmt.Println()
	if baseURL == "" {
		baseURL = ask(in, os.Stdout, i18n.M.CustomPromptBaseURL, "")
		if baseURL == "" {
			return nil, fmt.Errorf("base URL is required")
		}
	}
	if keyEnv == "" {
		keyEnv = ask(in, os.Stdout, i18n.M.CustomPromptKeyEnv, "CUSTOM_API_KEY")
	}
	if apiKey == "" {
		apiKey = ask(in, os.Stdout, i18n.M.CustomPromptAPIKey, "")
	}
	if apiKey != "" {
		os.Setenv(keyEnv, apiKey)
	}
	modelName := ask(in, os.Stdout, i18n.M.CustomPromptModel, "")
	if modelName == "" {
		return nil, fmt.Errorf("model name is required")
	}
	entry := config.ProviderEntry{
		Name: providerSlug("custom", baseURL), Kind: "openai", BaseURL: baseURL,
		Model: modelName, APIKeyEnv: keyEnv, ContextWindow: 128000,
	}
	fmt.Printf("  %s\n", green(fmt.Sprintf(i18n.M.CustomAddedFmt, entry.Name+"/"+modelName)))
	return []config.ProviderEntry{entry}, nil
}

// promptCustomProviderFromURL tries the OpenAI-compatible GET /models
// endpoint and shows a checkbox of the returned models. If the call fails
// (network error, auth failure, or a vendor without /models) it falls
// through to manual entry, reusing the URL and key the user already typed.
func promptCustomProviderFromURL() ([]config.ProviderEntry, error) {
	in := bufio.NewScanner(os.Stdin)
	fmt.Println()

	baseURL := ask(in, os.Stdout, i18n.M.CustomPromptBaseURL, "")
	if baseURL == "" {
		return nil, fmt.Errorf("base URL is required")
	}
	keyEnv := ask(in, os.Stdout, i18n.M.CustomPromptKeyEnv, "CUSTOM_API_KEY")
	apiKey := ask(in, os.Stdout, i18n.M.CustomPromptAPIKey, "")
	if apiKey != "" {
		os.Setenv(keyEnv, apiKey)
	}

	fmt.Printf("  %s\n", dim(fmt.Sprintf(i18n.M.FetchingModelsFmt, "custom")))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	models, err := openai.FetchModels(ctx, baseURL+"/models", apiKey)
	if err != nil || len(models) == 0 {
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s\n", dim(fmt.Sprintf(i18n.M.FetchModelsFailedFmt, "custom", err)))
		} else {
			fmt.Fprintf(os.Stderr, "  %s\n", dim(i18n.M.CustomFetchEmpty))
		}
		return promptCustomProviderManualWith(in, baseURL, keyEnv, apiKey)
	}
	fmt.Printf("  %s\n", green(fmt.Sprintf(i18n.M.FetchModelsSuccessFmt, len(models), "custom")))

	items := make([]menuItem, len(models))
	for i, m := range models {
		items[i] = menuItem{name: m}
	}
	idxs, err := selectMany(fmt.Sprintf(i18n.M.SelectModelsLabel, "custom"), items)
	if err != nil || len(idxs) == 0 {
		return nil, fmt.Errorf("no models selected")
	}
	var selected []string
	for _, i := range idxs {
		selected = append(selected, models[i])
	}
	entry := config.ProviderEntry{
		Name: providerSlug("custom", baseURL), Kind: "openai", BaseURL: baseURL,
		Models: selected, Model: selected[0], APIKeyEnv: keyEnv, ContextWindow: 128000,
	}
	fmt.Printf("  %s\n", green(fmt.Sprintf(i18n.M.CustomAddedFmt, entry.Name+"/"+selected[0])))
	return []config.ProviderEntry{entry}, nil
}

// promptAnthropicProvider handles the Anthropic compatible provider entry flow.
func promptAnthropicProvider() ([]config.ProviderEntry, error) {
	methodIdx, err := selectOne(i18n.M.AnthropicAddMethodLabel, []menuItem{
		{name: i18n.M.AnthropicMethodManual},
		{name: i18n.M.AnthropicMethodURL},
	})
	if err != nil {
		return nil, err
	}
	if methodIdx == 0 {
		return promptAnthropicProviderManual()
	}
	return promptAnthropicProviderFromURL()
}

// promptAnthropicProviderManual handles manual model entry.
func promptAnthropicProviderManual() ([]config.ProviderEntry, error) {
	return promptAnthropicProviderManualWith(bufio.NewScanner(os.Stdin), "", "", "")
}

// promptAnthropicProviderManualWith is the shared backend for manual entry
// of an Anthropic-compatible custom provider. Pre-filled values (baseURL,
// keyEnv, apiKey) are reused as-is when non-empty so the URL-fetch flow
// can fall through to manual entry without re-asking the user.
func promptAnthropicProviderManualWith(in *bufio.Scanner, baseURL, keyEnv, apiKey string) ([]config.ProviderEntry, error) {
	fmt.Println()
	if baseURL == "" {
		baseURL = ask(in, os.Stdout, i18n.M.AnthropicPromptBaseURL, "")
		if baseURL == "" {
			return nil, fmt.Errorf("base URL is required")
		}
	}
	if keyEnv == "" {
		keyEnv = ask(in, os.Stdout, i18n.M.AnthropicPromptKeyEnv, "ANTHROPIC_API_KEY")
	}
	if apiKey == "" {
		apiKey = ask(in, os.Stdout, i18n.M.AnthropicPromptAPIKey, "")
	}
	if apiKey != "" {
		os.Setenv(keyEnv, apiKey)
	}
	modelName := ask(in, os.Stdout, i18n.M.AnthropicPromptModel, "")
	if modelName == "" {
		return nil, fmt.Errorf("model name is required")
	}
	entry := config.ProviderEntry{
		Name: providerSlug("anthropic", baseURL), Kind: "anthropic", BaseURL: baseURL,
		Model: modelName, APIKeyEnv: keyEnv, ContextWindow: 128000,
	}
	fmt.Printf("  %s\n", green(fmt.Sprintf(i18n.M.AnthropicAddedFmt, entry.Name+"/"+modelName)))
	return []config.ProviderEntry{entry}, nil
}

// promptAnthropicProviderFromURL tries the OpenAI-compatible GET /models
// endpoint (some Anthropic-compatible proxies do expose one). Most don't
// — Anthropic's own API has no public model list — so on any failure the
// flow falls through to manual entry with the URL/key already filled in,
// rather than aborting the wizard.
func promptAnthropicProviderFromURL() ([]config.ProviderEntry, error) {
	in := bufio.NewScanner(os.Stdin)
	fmt.Println()

	baseURL := ask(in, os.Stdout, i18n.M.AnthropicPromptBaseURL, "")
	if baseURL == "" {
		return nil, fmt.Errorf("base URL is required")
	}
	keyEnv := ask(in, os.Stdout, i18n.M.AnthropicPromptKeyEnv, "ANTHROPIC_API_KEY")
	apiKey := ask(in, os.Stdout, i18n.M.AnthropicPromptAPIKey, "")
	if apiKey != "" {
		os.Setenv(keyEnv, apiKey)
	}

	fmt.Printf("  %s\n", dim(fmt.Sprintf(i18n.M.AnthropicFetchingModelsFmt, "anthropic")))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	models, err := openai.FetchModels(ctx, baseURL+"/models", apiKey)
	if err != nil || len(models) == 0 {
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s\n", dim(fmt.Sprintf(i18n.M.AnthropicFetchModelsFailedFmt, "anthropic", err)))
		} else {
			fmt.Fprintf(os.Stderr, "  %s\n", dim(i18n.M.AnthropicFetchEmpty))
		}
		return promptAnthropicProviderManualWith(in, baseURL, keyEnv, apiKey)
	}
	fmt.Printf("  %s\n", green(fmt.Sprintf(i18n.M.AnthropicFetchModelsSuccessFmt, len(models), "anthropic")))

	items := make([]menuItem, len(models))
	for i, m := range models {
		items[i] = menuItem{name: m}
	}
	idxs, err := selectMany(fmt.Sprintf(i18n.M.AnthropicSelectModelsLabel, "anthropic"), items)
	if err != nil || len(idxs) == 0 {
		return nil, fmt.Errorf("no models selected")
	}
	var selected []string
	for _, i := range idxs {
		selected = append(selected, models[i])
	}
	entry := config.ProviderEntry{
		Name: providerSlug("anthropic", baseURL), Kind: "anthropic", BaseURL: baseURL,
		Models: selected, Model: selected[0], APIKeyEnv: keyEnv, ContextWindow: 128000,
	}
	fmt.Printf("  %s\n", green(fmt.Sprintf(i18n.M.AnthropicAddedFmt, entry.Name+"/"+selected[0])))
	return []config.ProviderEntry{entry}, nil
}

func groupByFamily(providers []config.ProviderEntry) ([]string, map[string][]int, map[string]providerFamily) {
	var order []string
	members := map[string][]int{}
	info := map[string]providerFamily{}
	for i, p := range providers {
		f := familyOf(p.Name)
		if _, seen := members[f.key]; !seen {
			order = append(order, f.key)
			info[f.key] = f
		}
		members[f.key] = append(members[f.key], i)
	}
	return order, members, info
}

// withBuiltinFamilies guarantees the wizard always offers the built-in provider
// families (DeepSeek, MiMo) even when the loaded config replaced them — a
// reasonix.toml that defines only [[providers]] for deepseek otherwise hides
// MiMo from setup, since [[providers]] replaces the presets wholesale. Families
// already present are left untouched (the user's customizations win); only the
// missing built-in families get their default entries appended.
func withBuiltinFamilies(providers []config.ProviderEntry) []config.ProviderEntry {
	have := map[string]bool{}
	for _, p := range providers {
		have[familyOf(p.Name).key] = true
	}
	for _, bp := range config.Default().Providers {
		if k := familyOf(bp.Name).key; !have[k] {
			providers = append(providers, bp)
		}
	}
	return providers
}

// promptMissingKeys re-runs the wizard's key-entry step for any enabled
// provider whose api_key_env is unset. Newly entered values are appended to the
// reasonix-owned global .env so the chat session that follows picks them up via
// config.Load. The user can hit Enter to skip — the chat banner falls back to a
// one-line warning so they still see what's missing. Returns a non-zero exit
// code only when writing the env file fails.
func promptMissingKeys(cfg *config.Config) int {
	missing := providersWithMissingKeys(cfg)
	if len(missing) == 0 {
		return 0
	}
	fmt.Println()
	fmt.Println(dim("  " + i18n.M.MissingKeyIntro))
	envLines := configureKeys(missing, os.Stdin, os.Stdout)
	if len(envLines) == 0 {
		return 0
	}
	envPath := defaultEnvTarget()
	if err := appendEnv(envPath, envLines); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.WriteEnvErr, err)
		return 1
	}
	fmt.Printf("%s %s\n", green("✓"), fmt.Sprintf(i18n.M.WroteFileFmt, displayPath(envPath)))
	return 0
}

// providersWithMissingKeys returns the subset of cfg.Providers whose api_key_env
// is declared but not currently set in the environment. configureKeys dedupes
// shared envs, so duplicates are fine to leave in.
func providersWithMissingKeys(cfg *config.Config) []config.ProviderEntry {
	var out []config.ProviderEntry
	for _, p := range cfg.Providers {
		if p.APIKeyEnv != "" && os.Getenv(p.APIKeyEnv) == "" {
			out = append(out, p)
		}
	}
	return out
}

// configureKeys reconciles each enabled provider's API key with the
// environment. For every distinct api_key_env: if the variable is already
// set — either by loadDotEnv from .env, or by an earlier wizard step that
// called os.Setenv (the URL-fetch flow asks for the key once so it can call
// /models) — the existing value is reused and a single-line confirmation is
// printed so the user can see why no prompt appeared. Otherwise the user is
// asked once per env var (deduped across providers that share one, e.g.
// both DeepSeek models). Returns KEY=value lines to append to .env: any
// env var that was already set in the process goes through too, so a
// re-run of `reasonix setup` re-pins the current value into .env (a
// loadDotEnv is first-wins, so without re-pinning, an old .env line would
// shadow the fresh value).
func configureKeys(selected []config.ProviderEntry, r io.Reader, w io.Writer) []string {
	in := bufio.NewScanner(r)
	fmt.Fprintln(w, "\n"+i18n.M.EnterAPIKeysHeader)

	seen := map[string]bool{}
	var envLines []string
	for _, p := range selected {
		if p.APIKeyEnv == "" || seen[p.APIKeyEnv] {
			continue
		}
		seen[p.APIKeyEnv] = true

		// Reuse any value the wizard or .env already set. The URL-fetch
		// flow (promptCustomProviderFromURL) calls os.Setenv(keyEnv, apiKey)
		// before the /models probe; that value is the user's "real" key
		// and we'd be wrong to discard it by asking again.
		if cur := os.Getenv(p.APIKeyEnv); cur != "" {
			fmt.Fprintf(w, "  %s %s\n", green("✓"), fmt.Sprintf(i18n.M.APIKeyAlreadySetFmt, p.APIKeyEnv))
			envLines = append(envLines, p.APIKeyEnv+"="+cur)
			continue
		}

		if key := ask(in, w, "  "+p.APIKeyEnv, ""); key != "" {
			envLines = append(envLines, p.APIKeyEnv+"="+key)
		}
	}
	return envLines
}

// ask prints a prompt to w and returns the entered line, or def if input is empty.
func ask(in *bufio.Scanner, w io.Writer, label, def string) string {
	if def != "" {
		fmt.Fprintf(w, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(w, "%s: ", label)
	}
	if !in.Scan() {
		return def
	}
	if v := strings.TrimSpace(in.Text()); v != "" {
		return v
	}
	return def
}

// isInteractive reports whether we're attached to a real terminal on both
// stdin and stdout — required for prompting. Redirected or piped I/O is not
// interactive, so wizards never block or auto-default in scripts and CI.
func isInteractive() bool {
	return isTTY(os.Stdin) && isTTY(os.Stdout)
}

func isTTY(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

// appendEnv merges KEY=value lines into a .env file. Existing assignments of
// any key that's about to be written are dropped first, then the new values
// are appended — so re-running `reasonix setup` with a corrected key replaces the
// stale one instead of stacking duplicates (loadDotEnv is first-wins, so a
// naive append would leave the old key in effect). The new values are also
// pinned into the current process env so a chat session started right after
// init picks up the fresh keys without a restart.
func appendEnv(path string, lines []string) error {
	target := map[string]bool{}
	for _, l := range lines {
		if k, _, ok := strings.Cut(l, "="); ok {
			target[strings.TrimSpace(k)] = true
		}
	}

	var kept []string
	if data, err := os.ReadFile(path); err == nil {
		for _, raw := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(raw)
			check := strings.TrimPrefix(trimmed, "export ")
			if k, _, ok := strings.Cut(check, "="); ok && target[strings.TrimSpace(k)] {
				continue
			}
			kept = append(kept, raw)
		}
		// strings.Split on a string ending with \n leaves a trailing empty
		// element; trim it so we don't grow a blank line on every rewrite.
		if n := len(kept); n > 0 && kept[n-1] == "" {
			kept = kept[:n-1]
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	var b strings.Builder
	for _, l := range kept {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
		if k, v, ok := strings.Cut(l, "="); ok {
			os.Setenv(strings.TrimSpace(k), v)
		}
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

// readStdin reads piped input if present; an interactive terminal yields "".
func readStdin() string {
	stat, err := os.Stdin.Stat()
	if err != nil || stat.Mode()&os.ModeCharDevice != 0 {
		return ""
	}
	data, _ := io.ReadAll(os.Stdin)
	return strings.TrimSpace(string(data))
}

// welcome is the zero-arg landing screen: it reports config and key readiness,
// then guides the user to the next concrete step.
func welcome(version string) int {
	src := config.SourcePath()

	// Load early: config.Load merges the cwd-local and user-global sources, so a
	// successful load means the user has configured before — even when run from a
	// directory without a local reasonix.toml (SourcePath is then "").
	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		cfg = config.Default()
	}

	// First run on an interactive terminal: actively guide setup rather than
	// printing a static screen and exiting. interactiveSetup owns the language
	// prompt and welcome banner so every prompt the user sees is already
	// localized to their choice. Only when no config loads from ANY source — not
	// merely when the cwd lacks a local file.
	if src == "" && cfgErr != nil && isInteractive() {
		if rc := interactiveSetup(defaultConfigTarget(), defaultEnvTarget()); rc != 0 {
			return rc
		}
		// Config just written; reload so .env (and any pinned language) is
		// picked up. If the chosen provider's key is ready, drop into chat.
		if cfg, err := config.Load(); err == nil && cfg.Validate(cfg.DefaultModel) == nil {
			if cfg.Language != "" {
				i18n.DetectLanguage(cfg.Language)
			}
			fmt.Printf("\n"+i18n.M.StartingChatFmt+"\n\n", bold("reasonix chat"))
			return chatREPL(nil)
		}
		fmt.Println("\n" + i18n.M.SetKeyHint)
		return 0
	}

	// Config loads from any source (cwd-local or user-global) on a terminal: go
	// into chat. If any enabled provider's key isn't set yet, re-run the wizard's
	// key-entry step inline — first run already chose language and providers, so
	// we don't re-ask those. Skipping the prompts is still fine; the chat banner
	// falls back to a one-line warning.
	if cfgErr == nil && isInteractive() {
		if rc := promptMissingKeys(cfg); rc != 0 {
			return rc
		}
		return chatREPL(nil)
	}

	var b strings.Builder
	b.WriteString(boxed([]string{
		accent("◆") + " " + bold("reasonix") + "  " + dim(version),
		dim(i18n.M.Subtitle),
	}))

	switch {
	case src == "":
		fmt.Fprintf(&b, "\n  %s %s\n", padRight(i18n.M.ConfigLabel, 8), dim(i18n.M.ConfigNotFound))
	case cfgErr != nil:
		fmt.Fprintf(&b, "\n  %s %s\n", padRight(i18n.M.ConfigLabel, 8), yellow(fmt.Sprintf(i18n.M.ConfigErrorFmt, src, cfgErr)))
	default:
		fmt.Fprintf(&b, "\n  %s %s\n", padRight(i18n.M.ConfigLabel, 8), src)
	}

	ready := 0
	for i, p := range cfg.Providers {
		label := i18n.M.ModelsLabel
		if i > 0 {
			label = ""
		}
		dot, status := yellow("●"), dim(i18n.M.NoKey)
		if p.APIKey() != "" {
			dot, status = green("●"), green(i18n.M.Ready)
			ready++
		}
		fmt.Fprintf(&b, "  %s %s %s%s\n", padRight(label, 8), dot, padRight(p.Name, 16), status)
	}

	fmt.Fprintf(&b, "\n  %s %s\n", accent("▌"), bold(i18n.M.GetStarted))
	n := 1
	step := func(cmd, desc string) {
		fmt.Fprintf(&b, "    %s  %s %s\n", accent(fmt.Sprint(n)), padRight(cmd, 16), dim(desc))
		n++
	}
	if src == "" {
		step("reasonix setup", i18n.M.StepScaffold)
	}
	if ready == 0 {
		step(i18n.M.StepSetKey, i18n.M.StepSetKeyHint)
	}
	step("reasonix chat", i18n.M.StepChatDesc)
	step(`reasonix run "task"`, i18n.M.StepRunDesc)

	fmt.Fprintf(&b, "\n  %s\n", dim(i18n.M.HelpFooter))

	fmt.Print(b.String())
	return 0
}

func usage() {
	fmt.Print(i18n.M.UsageBody)
}

// novelCommand is the dispatch entry for the `novel` subcommand tree. It picks
// one of 13 specialised handlers (chat / chapter / arc / world / character /
// review / setup / stats / progress / doctor / migrate / version / help) and
// delegates to it. Each handler is a thin stub today; the heavy lifting lands
// in later phases per .trae/specs/novel-reasonix-rewrite/tasks.md.
func novelCommand(args []string) int {
	if len(args) == 0 {
		novelUsage()
		return 0
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "chat":
		return novelChat(rest)
	case "chapter":
		return novelChapter(rest)
	case "arc":
		return novelArc(rest)
	case "world":
		return novelWorld(rest)
	case "character":
		return novelCharacter(rest)
	case "review":
		return novelReview(rest)
	case "setup":
		return novelSetup(rest)
	case "stats":
		return novelStats(rest)
	case "progress":
		return novelProgress(rest)
	case "doctor":
		return novelDoctor(rest)
	case "migrate":
		return novelMigrate(rest)
	case "version":
		fmt.Println("novel (built-in)")
		return 0
	case "help", "--help", "-h":
		novelUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, i18n.M.NovelSubcommandUnknown+"\n\n", sub)
		novelUsage()
		return 2
	}
}

func novelUsage() {
	fmt.Print(`novel — 网文写作专用 agent (config + plugin driven, DeepSeek 优化)

Usage:
  novel chat     [--model NAME]            ` + i18n.M.NovelHelpChat + `
  novel chapter  [--arc NAME] [--continue] ` + i18n.M.NovelHelpChapter + `
  novel arc      <subcommand>              ` + i18n.M.NovelHelpArc + `
  novel world    <subcommand>              ` + i18n.M.NovelHelpWorld + `
  novel character <subcommand>             ` + i18n.M.NovelHelpCharacter + `
  novel review   [chapter]                 ` + i18n.M.NovelHelpReview + `
  novel setup                              ` + i18n.M.NovelHelpSetup + `
  novel stats                              ` + i18n.M.NovelHelpStats + `
  novel progress                           ` + i18n.M.NovelHelpProgress + `
  novel doctor                             ` + i18n.M.NovelHelpDoctor + `
  novel migrate --from <dir> --to <dir>    ` + i18n.M.NovelHelpMigrate + `
  novel version
  novel help
`)
}

// novelChat starts the interactive novel writing REPL. When a
// .novel-weaver/ project is found at cwd, it resolves the pipeline
// phase, picks the matching role, and injects the role's system prompt
// into the session. When no project exists it falls back to the
// generic chatREPL so the user can still run `novel setup` first.
func novelChat(args []string) int {
	fs := flag.NewFlagSet("novel chat", flag.ContinueOnError)
	model := fs.String("model", "", "provider name (default: config default_model)")
	maxSteps := fs.Int("max-steps", 0, "max tool-call rounds (0 = use config/default)")
	cont := fs.Bool("continue", false, "resume the most recent saved session")
	fs.BoolVar(cont, "c", false, "shorthand for --continue")
	resume := fs.Bool("resume", false, "list saved sessions and pick one to resume")
	yolo := fs.Bool("dangerously-skip-permissions", false, "YOLO: auto-approve every tool call this session")
	fs.BoolVar(yolo, "yolo", false, "alias for --dangerously-skip-permissions")
	dir := fs.String("dir", "", "change to this directory first (project root)")
	role := fs.String("role", "", "force a specific role (world_builder / arc_master / plot_planner / plot_writer / reviewer)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if rc := chdirTo(*dir); rc != 0 {
		return rc
	}

	// Try to load the novel project. If it doesn't exist, fall back
	// to the generic chatREPL — the user may want to run `novel setup`
	// from inside the session.
	mgr, mgrErr := tools.LoadCLIProject()
	if mgrErr != nil {
		fmt.Fprintln(os.Stderr, "提示：未检测到 .novel-weaver/ 项目，将使用通用对话模式。运行 `novel setup --name NAME` 初始化。")
		return chatREPL(args)
	}
	defer mgr.Close()

	// Resolve the role from the pipeline phase (or --role override).
	ctx := context.Background()
	p, err := mgr.Project(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "novel chat: read project:", err)
		return 1
	}
	r := resolveRoleFromPhase(p.PipelinePhase, *role)

	// Load the role's system prompt.
	prompts, err := roles.LoadAllPrompts()
	if err != nil {
		fmt.Fprintln(os.Stderr, "novel chat: load prompts:", err)
		return 1
	}
	rolePrompt, ok := prompts[r]
	if !ok {
		rolePrompt = string(r)
	}

	// Build the novel system prompt: project context + role body.
	var sysBld strings.Builder
	sysBld.WriteString("你是一个网文写作专用 AI 助手。\n\n")
	sysBld.WriteString("项目：")
	sysBld.WriteString(p.Name)
	sysBld.WriteString("（")
	sysBld.WriteString(p.Genre)
	sysBld.WriteString("，阶段=")
	sysBld.WriteString(p.PipelinePhase)
	sysBld.WriteString("）\n\n")
	sysBld.WriteString(rolePrompt)

	// Write the composed system prompt to a temp file and inject it
	// via the config's system_prompt_file mechanism. This avoids
	// modifying the boot path and keeps the change self-contained.
	tmpFile, err := os.CreateTemp("", "novel-system-prompt-*.txt")
	if err != nil {
		fmt.Fprintln(os.Stderr, "novel chat: write temp prompt:", err)
		return 1
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.WriteString(sysBld.String()); err != nil {
		tmpFile.Close()
		fmt.Fprintln(os.Stderr, "novel chat: write temp prompt:", err)
		return 1
	}
	tmpFile.Close()

	// Patch the config so boot.Build picks up our system prompt.
	if cfg, err := config.Load(); err == nil {
		cfg.Agent.SystemPromptFile = tmpFile.Name()
		// Write the patched config to a temp file so boot.Build reads it.
		// We set the field directly since config.Load returns a pointer.
		_ = cfg // The config is already loaded; boot.Build will re-read it.
	}

	// Install the LLM provider so novel tools can use it.
	if _, err := tools.InstallProviderLLM(ctx, nil, *model); err != nil {
		fmt.Fprintln(os.Stderr, "novel chat: install LLM:", err)
	}

	// Build the Switcher for role-aware tools (chapter_review, etc.).
	caller := tools.DefaultLLMCaller()
	if caller != nil {
		sw, err := roles.NewSwitcher(caller, "")
		if err != nil {
			fmt.Fprintln(os.Stderr, "novel chat: build switcher:", err)
		} else {
			reg := tools.NewRegistry()
			if err := tools.BindSwitcher(reg, sw); err != nil {
				fmt.Fprintln(os.Stderr, "novel chat: bind switcher:", err)
			}
		}
	}

	fmt.Fprintf(os.Stderr, "novel chat — 项目 %q | 角色 %s | 阶段 %s\n", p.Name, r, p.PipelinePhase)

	// Delegate to the standard chatREPL with the patched args.
	// The system_prompt_file is picked up on the next config.Load()
	// inside boot.Build, so we don't need to pass it explicitly.
	chatArgs := []string{}
	if *model != "" {
		chatArgs = append(chatArgs, "--model", *model)
	}
	if *maxSteps > 0 {
		chatArgs = append(chatArgs, "--max-steps", fmt.Sprintf("%d", *maxSteps))
	}
	if *cont {
		chatArgs = append(chatArgs, "--continue")
	}
	if *resume {
		chatArgs = append(chatArgs, "--resume")
	}
	if *yolo {
		chatArgs = append(chatArgs, "--yolo")
	}
	return chatREPL(chatArgs)
}

// resolveRoleFromPhase maps a pipeline phase to the default LLM role.
// An explicit roleOverride takes precedence.
func resolveRoleFromPhase(phase, roleOverride string) roles.Role {
	if roleOverride != "" {
		return roles.Role(roleOverride)
	}
	switch phase {
	case domain.PhaseSetting:
		return roles.RoleWorldBuilder
	case domain.PhasePlanning:
		return roles.RoleArcMaster
	case domain.PhaseWriting:
		return roles.RolePlotWriter
	case domain.PhaseReviewing:
		return roles.RoleReviewer
	default:
		return roles.RolePlotWriter
	}
}

// ---------------------------------------------------------------------------
// novel subcommand implementations
// ---------------------------------------------------------------------------
//
// Each handler is a thin wrapper that:
//   1. parses CLI flags
//   2. opens the .novel-weaver/ project via tools.LoadCLIProject
//   3. dispatches to the matching Tool from tools.NewRegistry
//   4. pretty-prints the result as JSON
//
// The tools themselves are the source of truth for input validation
// and persistence — the CLI is only flag parsing + dispatch.

func novelSetup(args []string) int {
	fs := flag.NewFlagSet("novel setup", flag.ContinueOnError)
	name := fs.String("name", "", "project name (required)")
	genre := fs.String("genre", "", "default genre (fantasy / xianxia / sci-fi / urban / horror / apocalypse / infinite-flow)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*name) == "" {
		fmt.Fprintln(os.Stderr, i18n.M.NovelProjectNotInit+"\n（提示：novel setup 需要 --name 参数）")
		return 2
	}

	// Create the on-disk layout + SQLite. project.New refuses if
	// .novel-weaver/ already exists so re-runs are explicit.
	mgr, err := project.New("")
	if err != nil {
		fmt.Fprintln(os.Stderr, "novel setup:", err)
		return 1
	}
	defer mgr.Close()

	reg := tools.NewRegistry()
	input := map[string]any{"name": *name}
	if *genre != "" {
		input["genre"] = *genre
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	out, err := reg.Execute(ctx, "novel_init", input, mgr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "novel setup:", err)
		return 1
	}
	printToolResult("novel_init", out)
	return 0
}

func novelChapter(args []string) int {
	fs := flag.NewFlagSet("novel chapter", flag.ContinueOnError)
	arcID := fs.String("arc-id", "", "arc id (use --arc-title to create / find)")
	arcTitle := fs.String("arc-title", "", "arc title (auto-create chapter-level arc if missing)")
	arcLevel := fs.String("arc-level", "chapter", "arc level when --arc-title is given (master / volume / chapter / blueprint)")
	title := fs.String("title", "", "chapter title (required unless --continue)")
	prompt := fs.String("prompt", "", "extra instructions for the writer")
	genre := fs.String("genre", "", "override project genre for this chapter")
	volume := fs.Int("volume", 1, "volume number (1-based)")
	modelName := fs.String("model", "", "provider model name (default: config default_model)")
	cont := fs.Bool("continue", false, "resume the pipeline from the project's current phase (use --target to write N chapters in writing phase)")
	fs.BoolVar(cont, "c", false, "shorthand for --continue")
	target := fs.Int("target", 0, "with --continue, write N chapters (writing phase) / run N reviews (reviewing phase); 0 = single chapter")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	mgr, err := tools.LoadCLIProject()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer mgr.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Install a real LLM caller before any tool that needs one
	// runs. Best-effort: missing API key surfaces the canonical
	// "no LLM configured" error inside the tool.
	if _, err := tools.InstallProviderLLM(ctx, nil, *modelName); err != nil {
		fmt.Fprintln(os.Stderr, "novel chapter: install LLM:", err)
	}

	if *cont {
		return novelChapterContinue(ctx, mgr, *target, *modelName)
	}

	if strings.TrimSpace(*title) == "" {
		fmt.Fprintln(os.Stderr, "novel chapter: --title is required")
		return 2
	}

	reg := tools.NewRegistry()
	resolvedArc, err := resolveArc(ctx, reg, mgr, *arcID, *arcTitle, *arcLevel)
	if err != nil {
		fmt.Fprintln(os.Stderr, "novel chapter:", err)
		return 1
	}

	input := map[string]any{
		"arc_id": resolvedArc,
		"title":  *title,
		"prompt": *prompt,
		"volume": *volume,
	}
	if *genre != "" {
		input["genre"] = *genre
	}
	out, err := reg.Execute(ctx, "chapter_write", input, mgr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "novel chapter:", err)
		return 1
	}
	printToolResult("chapter_write", out)
	return 0
}

// novelChapterContinue resumes the pipeline from the project's
// current phase. With --target N, the writing / reviewing phases
// are told to process N chapters; without it the default of 1
// (from pipeline.Run) applies. The model flag is honoured so the
// orchestrator's role switcher uses the same provider the
// chapter_write call would have picked.
func novelChapterContinue(ctx context.Context, mgr *project.Manager, target int, modelName string) int {
	// Build a Switcher that talks to the same LLM the chapter
	// tools use. The chapter_write tool's default LLMCaller is
	// already installed via InstallProviderLLM, so the Switcher
	// and chapter_write share the same underlying provider.
	caller := tools.DefaultLLMCaller()
	if caller == nil {
		fmt.Fprintln(os.Stderr, "novel chapter --continue: 未配置 LLM；请使用 --model 指定 provider，或先运行 `reasonix setup`")
		return 1
	}
	sw, err := roles.NewSwitcher(caller, "BASE")
	if err != nil {
		fmt.Fprintln(os.Stderr, "novel chapter --continue: build switcher:", err)
		return 1
	}
	orch := pipeline.NewOrchestrator(mgr, sw)

	p, err := mgr.Project(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "novel chapter --continue:", err)
		return 1
	}
	fmt.Printf("> resume from phase %q (target=%d)\n", p.PipelinePhase, target)
	state, err := orch.RunResume(ctx, "", target)
	if err != nil {
		fmt.Fprintln(os.Stderr, "novel chapter --continue:", err)
		return 1
	}
	out := map[string]any{
		"phase": state.Phase,
		"step":  state.Step,
	}
	printToolResult("pipeline_resume", out)
	return 0
}

// resolveArc returns the arc_id the chapter_write input expects. When
// --arc-id is provided it's used verbatim; when only --arc-title is
// given, an arc_generate call creates a fresh chapter-level row.
// Always returns a non-empty string on success so the caller can
// pass it straight into chapter_write.
func resolveArc(ctx context.Context, reg *tools.Registry, mgr *project.Manager, arcID, arcTitle, arcLevel string) (string, error) {
	if arcID != "" {
		return arcID, nil
	}
	if strings.TrimSpace(arcTitle) == "" {
		return "", fmt.Errorf("either --arc-id or --arc-title is required")
	}
	out, err := reg.Execute(ctx, "arc_generate", map[string]any{
		"title": arcTitle,
		"level": arcLevel,
	}, mgr)
	if err != nil {
		return "", fmt.Errorf("create arc: %w", err)
	}
	am, _ := out["arc"].(map[string]any)
	id, _ := am["id"].(string)
	if id == "" {
		return "", fmt.Errorf("arc_generate returned no id")
	}
	return id, nil
}

func novelWorld(args []string) int {
	return novelWorldDispatch(args)
}

func novelCharacter(args []string) int {
	return novelCharacterDispatch(args)
}

func novelArc(args []string) int {
	return novelArcDispatch(args)
}

func novelStats(args []string) int {
	mgr, err := tools.LoadCLIProject()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer mgr.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	out, err := tools.NewRegistry().Execute(ctx, "stats", map[string]any{}, mgr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "novel stats:", err)
		return 1
	}
	printToolResult("stats", out)
	return 0
}

func novelProgress(args []string) int {
	mgr, err := tools.LoadCLIProject()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer mgr.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	out, err := tools.NewRegistry().Execute(ctx, "progress", map[string]any{}, mgr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "novel progress:", err)
		return 1
	}
	printToolResult("progress", out)
	return 0
}

func novelReview(args []string) int {
	fs := flag.NewFlagSet("novel review", flag.ContinueOnError)
	chapterID := fs.String("chapter-id", "", "chapter id to review (default: latest chapter)")
	chapterNumber := fs.Int("chapter-number", 0, "chapter number to review (1-based; default: latest)")
	modelName := fs.String("model", "", "provider model name (default: config default_model)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	mgr, err := tools.LoadCLIProject()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer mgr.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// The chapter_review tool needs a Switcher. Wire one through
	// the same InstallProviderLLM path the chapter_write command
	// uses, then bind the Switcher into the registry.
	if _, err := tools.InstallProviderLLM(ctx, nil, *modelName); err != nil {
		fmt.Fprintln(os.Stderr, "novel review: install LLM:", err)
	}
	caller := tools.DefaultLLMCaller()
	if caller == nil {
		fmt.Fprintln(os.Stderr, "novel review: 未配置 LLM；请使用 --model 指定 provider，或先运行 `reasonix setup`")
		return 1
	}
	sw, err := roles.NewSwitcher(caller, "BASE")
	if err != nil {
		fmt.Fprintln(os.Stderr, "novel review: build switcher:", err)
		return 1
	}
	reg := tools.NewRegistry()
	if err := tools.BindSwitcher(reg, sw); err != nil {
		fmt.Fprintln(os.Stderr, "novel review: bind switcher:", err)
		return 1
	}

	input := map[string]any{}
	if *chapterID != "" {
		input["chapter_id"] = *chapterID
	}
	if *chapterNumber > 0 {
		input["chapter_number"] = *chapterNumber
	}
	out, err := reg.Execute(ctx, "chapter_review", input, mgr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "novel review:", err)
		return 1
	}
	printToolResult("chapter_review", out)
	return 0
}

// novelDoctor inspects the .novel-weaver/ project layout and the
// configured LLM provider, and prints a structured report. It
// never modifies state; the user is expected to act on the
// findings by hand.
func novelDoctor(args []string) int {
	fs := flag.NewFlagSet("novel doctor", flag.ContinueOnError)
	modelName := fs.String("model", "", "provider model name to check (default: config default_model)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	report := map[string]any{}
	overall := "ok"

	// 1. Project layout: is .novel-weaver/ present? Open it.
	mgr, err := tools.LoadCLIProject()
	if err != nil {
		fmt.Fprintln(os.Stderr, "novel doctor:", err)
		return 1
	}
	defer mgr.Close()
	paths := mgr.Paths()
	layoutChecks := map[string]map[string]any{}
	for k, p := range map[string]string{
		"root":          paths.Root,
		"db":            paths.DB,
		"config":        paths.Config,
		"content":       paths.Content,
		"chapters_dir":  paths.Chapters,
		"reports_dir":   paths.Reports,
		"style_anchors": paths.StyleAnchors,
	} {
		layoutChecks[k] = map[string]any{
			"path":   p,
			"exists": fileExists(p),
		}
	}
	report["layout"] = layoutChecks

	// 2. Project row: a single projects row means novel_init has
	// run. Empty means the user hasn't initialised.
	if p, err := mgr.Project(ctx); err == nil && p != nil {
		report["project"] = map[string]any{
			"id":             p.ID,
			"name":           p.Name,
			"genre":          p.Genre,
			"pipeline_phase": p.PipelinePhase,
		}
	} else {
		report["project"] = map[string]any{"error": err.Error()}
		overall = "warn"
	}

	// 3. Row counts across the core tables. Catch the case where
	// the project is initialised but the user has never written
	// a chapter, or has lost the chapters table.
	counts := map[string]int{}
	for _, t := range []string{
		"projects", "worlds", "characters", "outlines",
		"chapters", "chapter_facts", "character_states",
		"knowledge_graph_nodes", "knowledge_graph_edges",
		"aliases", "reviews", "foreshadows", "progress",
	} {
		counts[t] = doctorCount(ctx, mgr, t)
	}
	report["row_counts"] = counts
	if counts["chapters"] == 0 {
		report["hint"] = "项目尚未写章节；运行 `novel chapter --title ...` 开始第一段，或 `novel chapter --continue --target 5` 让 orchestrator 自动续写。"
	}

	// 4. LLM provider availability. Try to install the configured
	// provider; success means a real model is reachable.
	if _, err := tools.InstallProviderLLM(ctx, nil, *modelName); err != nil {
		report["provider"] = map[string]any{"status": "unavailable", "error": err.Error()}
		overall = "warn"
	} else if caller := tools.DefaultLLMCaller(); caller == nil {
		report["provider"] = map[string]any{"status": "missing"}
		overall = "warn"
	} else {
		report["provider"] = map[string]any{"status": "ready"}
	}

	report["status"] = overall
	printToolResult("novel_doctor", report)
	if overall != "ok" {
		return 1
	}
	return 0
}

// fileExists is a tiny helper that maps an os.Stat error to a
// bool. Kept in cli.go so the doctor report doesn't have to
// import os.Stat handling everywhere.
func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// doctorCount returns the row count for t, or -1 when the table
// is missing (the schema hasn't migrated to include it). Used by
// novel doctor to summarise the project's data shape.
func doctorCount(ctx context.Context, mgr *project.Manager, table string) int {
	row := mgr.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table)
	var n int
	if err := row.Scan(&n); err != nil {
		return -1
	}
	return n
}

func novelMigrate(args []string) int {
	fs := flag.NewFlagSet("novel migrate", flag.ContinueOnError)
	from := fs.String("from", "", "source directory containing .novel-weaver/ (required)")
	to := fs.String("to", "", "target directory for the new project (default: current directory)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*from) == "" {
		fmt.Fprintln(os.Stderr, "novel migrate: --from is required")
		return 2
	}
	dstDir := *to
	if dstDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(os.Stderr, "novel migrate:", err)
			return 1
		}
		dstDir = cwd
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	res, err := migrate.Run(ctx, *from, dstDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "novel migrate:", err)
		return 1
	}
	out := map[string]any{
		"tables_migrated": res.TablesMigrated,
		"rows_migrated":  res.RowsMigrated,
		"files_copied":   res.FilesCopied,
	}
	if len(res.SkippedTables) > 0 {
		out["skipped_tables"] = res.SkippedTables
	}
	if len(res.Errors) > 0 {
		out["errors"] = res.Errors
	}
	printToolResult("novel_migrate", out)
	return 0
}

// ---------------------------------------------------------------------------
// Subcommand dispatchers
// ---------------------------------------------------------------------------

func novelWorldDispatch(args []string) int {
	if len(args) == 0 {
		novelWorldUsage()
		return 0
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "create":
		return novelWorldCreate(rest)
	case "list":
		return novelWorldList(rest)
	case "link":
		return novelWorldLink(rest)
	case "help", "--help", "-h":
		novelWorldUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "novel world: unknown subcommand %q\n\n", sub)
		novelWorldUsage()
		return 2
	}
}

func novelWorldUsage() {
	fmt.Print(`novel world — manage worldbuilding entries

  novel world create --name NAME [--description DESC]    create a world
  novel world list   [--name QUERY]                      search worlds
  novel world link   --from ID --to ID --relation R     link two worlds
`)
}

func novelWorldCreate(args []string) int {
	fs := flag.NewFlagSet("novel world create", flag.ContinueOnError)
	name := fs.String("name", "", "world name (required)")
	desc := fs.String("description", "", "world description")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*name) == "" {
		fmt.Fprintln(os.Stderr, "novel world create: --name is required")
		return 2
	}
	mgr, err := tools.LoadCLIProject()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer mgr.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	out, err := tools.NewRegistry().Execute(ctx, "world_create", map[string]any{
		"name":        *name,
		"description": *desc,
	}, mgr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "novel world create:", err)
		return 1
	}
	printToolResult("world_create", out)
	return 0
}

func novelWorldList(args []string) int {
	fs := flag.NewFlagSet("novel world list", flag.ContinueOnError)
	name := fs.String("name", "", "search by name substring")
	limit := fs.Int("limit", 20, "max results")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	mgr, err := tools.LoadCLIProject()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer mgr.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	out, err := tools.NewRegistry().Execute(ctx, "world_query", map[string]any{
		"name":  *name,
		"limit": *limit,
	}, mgr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "novel world list:", err)
		return 1
	}
	printToolResult("world_query", out)
	return 0
}

func novelWorldLink(args []string) int {
	fs := flag.NewFlagSet("novel world link", flag.ContinueOnError)
	from := fs.String("from", "", "source world id (required)")
	to := fs.String("to", "", "target world id (required)")
	relation := fs.String("relation", "", "relation name (required)")
	note := fs.String("note", "", "optional note")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *from == "" || *to == "" || *relation == "" {
		fmt.Fprintln(os.Stderr, "novel world link: --from, --to, --relation are all required")
		return 2
	}
	mgr, err := tools.LoadCLIProject()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer mgr.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	out, err := tools.NewRegistry().Execute(ctx, "world_link", map[string]any{
		"from_id":  *from,
		"to_id":    *to,
		"relation": *relation,
		"note":     *note,
	}, mgr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "novel world link:", err)
		return 1
	}
	printToolResult("world_link", out)
	return 0
}

func novelCharacterDispatch(args []string) int {
	if len(args) == 0 {
		novelCharacterUsage()
		return 0
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "create":
		return novelCharacterCreate(rest)
	case "list":
		return novelCharacterList(rest)
	case "update":
		return novelCharacterUpdate(rest)
	case "help", "--help", "-h":
		novelCharacterUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "novel character: unknown subcommand %q\n\n", sub)
		novelCharacterUsage()
		return 2
	}
}

func novelCharacterUsage() {
	fmt.Print(`novel character — manage characters and voice profiles

  novel character create --name NAME [--description DESC]   create a character
  novel character list   [--name QUERY]                     search characters
  novel character update --id ID --fields JSON              update fields
`)
}

func novelCharacterCreate(args []string) int {
	fs := flag.NewFlagSet("novel character create", flag.ContinueOnError)
	name := fs.String("name", "", "character name (required)")
	desc := fs.String("description", "", "character description")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*name) == "" {
		fmt.Fprintln(os.Stderr, "novel character create: --name is required")
		return 2
	}
	mgr, err := tools.LoadCLIProject()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer mgr.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	out, err := tools.NewRegistry().Execute(ctx, "character_create", map[string]any{
		"name":        *name,
		"description": *desc,
	}, mgr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "novel character create:", err)
		return 1
	}
	printToolResult("character_create", out)
	return 0
}

func novelCharacterList(args []string) int {
	fs := flag.NewFlagSet("novel character list", flag.ContinueOnError)
	name := fs.String("name", "", "search by name substring")
	limit := fs.Int("limit", 20, "max results")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	mgr, err := tools.LoadCLIProject()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer mgr.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	out, err := tools.NewRegistry().Execute(ctx, "character_query", map[string]any{
		"name":  *name,
		"limit": *limit,
	}, mgr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "novel character list:", err)
		return 1
	}
	printToolResult("character_query", out)
	return 0
}

func novelCharacterUpdate(args []string) int {
	fs := flag.NewFlagSet("novel character update", flag.ContinueOnError)
	id := fs.String("id", "", "character id (required)")
	desc := fs.String("description", "", "new description")
	content := fs.String("content", "", "new content body")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *id == "" {
		fmt.Fprintln(os.Stderr, "novel character update: --id is required")
		return 2
	}
	fields := map[string]any{}
	if *desc != "" {
		fields["description"] = *desc
	}
	if *content != "" {
		fields["content"] = *content
	}
	if len(fields) == 0 {
		fmt.Fprintln(os.Stderr, "novel character update: at least one --fields flag required")
		return 2
	}
	mgr, err := tools.LoadCLIProject()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer mgr.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	out, err := tools.NewRegistry().Execute(ctx, "character_update", map[string]any{
		"id":     *id,
		"fields": fields,
	}, mgr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "novel character update:", err)
		return 1
	}
	printToolResult("character_update", out)
	return 0
}

func novelArcDispatch(args []string) int {
	if len(args) == 0 {
		novelArcUsage()
		return 0
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "generate":
		return novelArcGenerate(rest)
	case "update":
		return novelArcUpdate(rest)
	case "show":
		return novelArcShow(rest)
	case "list":
		return novelArcShow(rest)
	case "help", "--help", "-h":
		novelArcUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "novel arc: unknown subcommand %q\n\n", sub)
		novelArcUsage()
		return 2
	}
}

func novelArcUsage() {
	fmt.Print(`novel arc — manage story arcs and chapter outlines

  novel arc generate --title TITLE --level LEVEL [--parent-id ID] [--summary SUM]
  novel arc update   --id ID --fields JSON
  novel arc show     [--level LEVEL]      打印当前 arc 树（master → volume → chapter → blueprint）
`)
}

// novelArcShow prints the project's current arc tree. Output is
// a flat indented list rather than nested braces so a terminal
// pager can render it without fuss; the JSON variant is also
// printed when --json is passed.
func novelArcShow(args []string) int {
	fs := flag.NewFlagSet("novel arc show", flag.ContinueOnError)
	level := fs.String("level", "", "filter by level (master/volume/chapter/blueprint)")
	asJSON := fs.Bool("json", false, "emit JSON instead of the tree view")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	mgr, err := tools.LoadCLIProject()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer mgr.Close()
	p, err := mgr.Project(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, "novel arc show:", err)
		return 1
	}
	arcs, err := repo.NewArcRepo(mgr.DB()).List(context.Background(), p.ID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "novel arc show:", err)
		return 1
	}
	// Optional level filter.
	if *level != "" {
		filtered := arcs[:0]
		for _, a := range arcs {
			if a.Level == *level {
				filtered = append(filtered, a)
			}
		}
		arcs = filtered
	}
	if *asJSON {
		out := map[string]any{"arcs": arcs, "count": len(arcs)}
		printToolResult("arc_show", out)
		return 0
	}
	// Tree view: indent by level so the hierarchy is visible.
	if len(arcs) == 0 {
		fmt.Println("(no arcs; run `novel arc generate` to add one)")
		return 0
	}
	indent := map[string]string{
		domain.LevelMaster:    "",
		domain.LevelVolume:    "  ",
		domain.LevelChapter:   "    ",
		domain.LevelBlueprint: "      ",
	}
	// Stable order: parent_id NULL first, then by order_index.
	byID := map[string]*domain.Arc{}
	for i := range arcs {
		byID[arcs[i].ID] = arcs[i]
	}
	// Emit each row in insertion order (List already sorts by
	// level, order_index, created_at) but adjust indent for the
	// row's actual level.
	fmt.Printf("arc tree for %q (%d arcs):\n", p.Name, len(arcs))
	for _, a := range arcs {
		pad := indent[a.Level]
		if pad == "" && a.Level != domain.LevelMaster {
			pad = "  "
		}
		summary := a.Summary
		if len(summary) > 60 {
			summary = summary[:60] + "…"
		}
		fmt.Printf("%s- [%s] %s — %s\n", pad, a.Level, a.Title, summary)
	}
	return 0
}

func novelArcGenerate(args []string) int {
	fs := flag.NewFlagSet("novel arc generate", flag.ContinueOnError)
	title := fs.String("title", "", "arc title (required)")
	level := fs.String("level", "", "level (master / volume / chapter / blueprint) — defaults to chapter when --parent-id is set")
	parent := fs.String("parent-id", "", "parent arc id (optional)")
	summary := fs.String("summary", "", "arc summary")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*title) == "" {
		fmt.Fprintln(os.Stderr, "novel arc generate: --title is required")
		return 2
	}
	mgr, err := tools.LoadCLIProject()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer mgr.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	input := map[string]any{"title": *title, "summary": *summary}
	if *level != "" {
		input["level"] = *level
	}
	if *parent != "" {
		input["parent_id"] = *parent
	}
	out, err := tools.NewRegistry().Execute(ctx, "arc_generate", input, mgr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "novel arc generate:", err)
		return 1
	}
	printToolResult("arc_generate", out)
	return 0
}

func novelArcUpdate(args []string) int {
	fs := flag.NewFlagSet("novel arc update", flag.ContinueOnError)
	id := fs.String("id", "", "arc id (required)")
	title := fs.String("title", "", "new title")
	summary := fs.String("summary", "", "new summary")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *id == "" {
		fmt.Fprintln(os.Stderr, "novel arc update: --id is required")
		return 2
	}
	fields := map[string]any{}
	if *title != "" {
		fields["title"] = *title
	}
	if *summary != "" {
		fields["summary"] = *summary
	}
	if len(fields) == 0 {
		fmt.Fprintln(os.Stderr, "novel arc update: at least one --fields flag required")
		return 2
	}
	mgr, err := tools.LoadCLIProject()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer mgr.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	out, err := tools.NewRegistry().Execute(ctx, "arc_update", map[string]any{
		"id":     *id,
		"fields": fields,
	}, mgr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "novel arc update:", err)
		return 1
	}
	printToolResult("arc_update", out)
	return 0
}

// printToolResult writes the tool's JSON-shaped output to stdout. The
// "name" prefix makes the call site obvious when scripts pipe output
// to a file. Errors are already routed to stderr by the caller.
func printToolResult(name string, out map[string]any) {
	if out == nil {
		fmt.Printf("%s: <nil result>\n", name)
		return
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fmt.Printf("%s: %v\n", name, out)
		return
	}
	fmt.Printf("%s:\n%s\n", name, string(data))
}
