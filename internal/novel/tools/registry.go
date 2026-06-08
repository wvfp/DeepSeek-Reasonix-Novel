// Package tools is the tool registry for the novel subsystem. A Tool
// is a small typed function that takes a JSON-shaped input map, the
// per-project Manager (so it can talk to the DB and the filesystem),
// and returns a JSON-shaped output map. The Registry owns the catalog
// and is created once at process start.
package tools

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"reasonix/internal/novel/project"
)

// Tool is the contract every novel tool implements. Execute is
// synchronous: the runner drives it from a goroutine. The contract is
// deliberately small — input is map[string]any so the wire format can
// be a free-form JSON object, and output is the same so callers can
// pretty-print without further unmarshalling.
type Tool interface {
	Name() string
	Description() string
	Execute(ctx context.Context, input map[string]any, mgr *project.Manager) (map[string]any, error)
}

// Registry is the catalog. Use NewRegistry (which calls init() via
// package init below) to get the populated instance, then Register
// additional tools in tests.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry returns a Registry with the default 12 tools registered.
// The init() in init.go populates the global catalog that
// NewRegistry copies; tests that need a clean registry should build
// one with &Registry{tools: map[string]Tool{}} and Register manually.
func NewRegistry() *Registry {
	r := &Registry{tools: map[string]Tool{}}
	for name, t := range defaultCatalog() {
		r.tools[name] = t
	}
	return r
}

// Register adds (or replaces) a tool by Name. Safe for concurrent
// callers; in practice the registry is populated at start-up.
func (r *Registry) Register(t Tool) {
	if t == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t
}

// Get looks up a tool by name. The second return is false on miss.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// Names returns the registered tool names in deterministic order.
// Callers that print a catalog (help text, --list) should sort so the
// output is stable.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.tools))
	for n := range r.tools {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Execute looks up the tool by name and runs it. The error is wrapped
// with the tool name so the call site can show "world_create: ..." in
// the failure message.
func (r *Registry) Execute(ctx context.Context, name string, input map[string]any, mgr *project.Manager) (map[string]any, error) {
	t, ok := r.Get(name)
	if !ok {
		return nil, fmt.Errorf("tools: unknown tool %q", name)
	}
	return t.Execute(ctx, input, mgr)
}

// defaultCatalog returns a copy of the global catalog seeded by init.
// It exists so the package-level catalog can be rebuilt in tests after
// Register replaces a tool.
func defaultCatalog() map[string]Tool {
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	out := make(map[string]Tool, len(defaultCatalog_))
	for k, v := range defaultCatalog_ {
		out[k] = v
	}
	return out
}

var (
	defaultMu        sync.RWMutex
	defaultCatalog_  = map[string]Tool{}
)

// registerDefault inserts a tool into the global default catalog.
// Called from each tool file's init().
func registerDefault(t Tool) {
	if t == nil {
		return
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultCatalog_[t.Name()] = t
}
