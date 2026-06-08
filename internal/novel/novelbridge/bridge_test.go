package novelbridge

import (
	"context"
	"encoding/json"
	"testing"

	"reasonix/internal/novel/project"
	"reasonix/internal/novel/tools"
	"reasonix/internal/tool"
)

func TestAdapter_ImplementsToolInterface(t *testing.T) {
	novelReg := tools.NewRegistry()
	host := tool.NewRegistry()
	RegisterAll(host, novelReg, nil)

	// Verify every novel tool was registered in the host.
	for _, name := range novelReg.Names() {
		if _, ok := host.Get(name); !ok {
			t.Errorf("novel tool %q not found in host registry", name)
		}
	}
}

func TestAdapter_NameAndDescription(t *testing.T) {
	novelReg := tools.NewRegistry()
	host := tool.NewRegistry()
	RegisterAll(host, novelReg, nil)

	tt, ok := host.Get("novel_init")
	if !ok {
		t.Fatal("novel_init not registered")
	}
	if tt.Name() != "novel_init" {
		t.Errorf("Name() = %q, want %q", tt.Name(), "novel_init")
	}
	if tt.Description() == "" {
		t.Error("Description() is empty")
	}
	if tt.ReadOnly() {
		t.Error("ReadOnly() should be false for novel tools")
	}
}

func TestAdapter_Schema(t *testing.T) {
	novelReg := tools.NewRegistry()
	host := tool.NewRegistry()
	RegisterAll(host, novelReg, nil)

	tt, ok := host.Get("novel_init")
	if !ok {
		t.Fatal("novel_init not registered")
	}

	schema := tt.Schema()
	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	props, ok := parsed["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema missing 'properties'")
	}
	if _, ok := props["name"]; !ok {
		t.Error("schema missing 'name' property")
	}
}

func TestAdapter_Execute_NoManager(t *testing.T) {
	// Temporarily clear the resolver so the test sees the "no resolver" path.
	SetManagerResolver(nil)

	novelReg := tools.NewRegistry()
	host := tool.NewRegistry()
	RegisterAll(host, novelReg, nil)

	tt, _ := host.Get("novel_init")
	args := json.RawMessage(`{"name":"测试"}`)
	_, err := tt.Execute(context.Background(), args)
	if err == nil {
		t.Error("expected error when no manager resolver is set")
	}
}

func TestAdapter_Execute_WithManager(t *testing.T) {
	// Create a temp project so the manager resolver succeeds.
	tmpDir := t.TempDir()
	mgr, err := project.New(tmpDir)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	mgr.Close()

	// Set up the resolver — each call opens and closes its own manager
	// to avoid holding a SQLite lock across the test lifecycle.
	SetManagerResolver(func() (*project.Manager, error) {
		m, err := project.Open(tmpDir)
		if err != nil {
			return nil, err
		}
		return m, nil
	})

	novelReg := tools.NewRegistry()
	host := tool.NewRegistry()
	RegisterAll(host, novelReg, nil)

	tt, _ := host.Get("novel_init")
	args := json.RawMessage(`{"name":"测试小说","genre":"xianxia"}`)
	result, err := tt.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Result should be valid JSON with a "project" key.
	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	proj, ok := parsed["project"].(map[string]any)
	if !ok {
		t.Fatal("result missing 'project' key")
	}
	if proj["name"] != "测试小说" {
		t.Errorf("project name = %v, want %q", proj["name"], "测试小说")
	}
}

func TestAdapter_Execute_InvalidJSON(t *testing.T) {
	novelReg := tools.NewRegistry()
	host := tool.NewRegistry()
	RegisterAll(host, novelReg, nil)

	SetManagerResolver(func() (*project.Manager, error) {
		return nil, nil // won't be reached
	})

	tt, _ := host.Get("novel_init")
	_, err := tt.Execute(context.Background(), json.RawMessage(`{invalid`))
	if err == nil {
		t.Error("expected error for invalid JSON args")
	}
}

func TestRegisterAll_AllToolsHaveSchemas(t *testing.T) {
	novelReg := tools.NewRegistry()
	host := tool.NewRegistry()
	RegisterAll(host, novelReg, nil)

	for _, name := range novelReg.Names() {
		tt, ok := host.Get(name)
		if !ok {
			continue
		}
		schema := tt.Schema()
		if len(schema) == 0 {
			t.Errorf("tool %q has empty schema", name)
			continue
		}
		var parsed map[string]any
		if err := json.Unmarshal(schema, &parsed); err != nil {
			t.Errorf("tool %q schema is not valid JSON: %v", name, err)
		}
	}
}
