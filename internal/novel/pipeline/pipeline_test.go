package pipeline

import (
	"context"
	"testing"

	"reasonix/internal/novel/domain"
	"reasonix/internal/novel/project"
	"reasonix/internal/novel/repo"
	"reasonix/internal/novel/roles"
)

// fakeLLM is the no-op LLMCaller the orchestrator tests use so the
// Switcher has something non-nil to dispatch to. The orchestrator's
// writing phase goes through tools.Registry, which already plumbs
// its own LLMCaller (installed via tools.SetDefaultLLMCaller in the
// tools test helpers), so the Switcher's LLM is only consulted when
// the test deliberately dispatches a Switcher.Call — which the
// current tests do not.
type fakeLLM struct{}

func (fakeLLM) Call(_ context.Context, _, _ string) (string, error) { return "", nil }

// seedPipelineProject creates a fresh project with a single world,
// matching what novel world create would produce. The orchestrator's
// RunSetting requires a world to be present.
func seedPipelineProject(t *testing.T) *project.Manager {
	t.Helper()
	dir := t.TempDir()
	mgr, err := project.New(dir)
	if err != nil {
		t.Fatalf("project.New: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	// Insert a project row + a world row directly — bypassing the
	// tool registry so the test stays in package pipeline. Equivalent
	// to novel_init + novel world create.
	ctx := context.Background()
	const projQ = `INSERT INTO projects (id, name, genre, pipeline_phase, created_at, modified_at)
	               VALUES (?, ?, ?, ?, ?, ?)`
	if _, err := mgr.DB().ExecContext(ctx, projQ,
		"test-pipeline-proj", "测试", domain.GenreXianxia, domain.PhaseSetting, 0, 0); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if err := repo.NewWorldRepo(mgr.DB()).Create(ctx, &domain.World{
		ProjectID: "test-pipeline-proj",
		Name:      "九州",
		Slug:      "jiuzhou",
	}); err != nil {
		t.Fatalf("seed world: %v", err)
	}
	return mgr
}

func newSwitcherForTest(t *testing.T) *roles.Switcher {
	t.Helper()
	sw, err := roles.NewSwitcher(fakeLLM{}, "BASE")
	if err != nil {
		t.Fatalf("NewSwitcher: %v", err)
	}
	return sw
}

func TestRunSetting_HappyPath(t *testing.T) {
	mgr := seedPipelineProject(t)
	o := NewOrchestrator(mgr, newSwitcherForTest(t))
	if err := o.RunSetting(context.Background()); err != nil {
		t.Fatalf("RunSetting: %v", err)
	}
}

func TestRunSetting_NoWorld(t *testing.T) {
	dir := t.TempDir()
	mgr, err := project.New(dir)
	if err != nil {
		t.Fatalf("project.New: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	const projQ = `INSERT INTO projects (id, name, genre, pipeline_phase, created_at, modified_at)
	               VALUES (?, ?, ?, ?, ?, ?)`
	if _, err := mgr.DB().ExecContext(context.Background(), projQ,
		"p-noworld", "no-world", domain.GenreXianxia, domain.PhaseSetting, 0, 0); err != nil {
		t.Fatalf("insert: %v", err)
	}
	o := NewOrchestrator(mgr, newSwitcherForTest(t))
	if err := o.RunSetting(context.Background()); err == nil {
		t.Fatal("RunSetting without world should error")
	}
}

func TestRunPlanning_CreatesMasterArc(t *testing.T) {
	mgr := seedPipelineProject(t)
	o := NewOrchestrator(mgr, newSwitcherForTest(t))
	ctx := context.Background()
	if err := o.RunPlanning(ctx); err != nil {
		t.Fatalf("RunPlanning: %v", err)
	}
	arcs, err := repo.NewArcRepo(mgr.DB()).List(ctx, "test-pipeline-proj")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, a := range arcs {
		if a.Level == domain.LevelMaster {
			found = true
		}
	}
	if !found {
		t.Errorf("RunPlanning did not create a master arc; have %d arcs", len(arcs))
	}
}

func TestPersistPhase_RoundTrip(t *testing.T) {
	mgr := seedPipelineProject(t)
	o := NewOrchestrator(mgr, newSwitcherForTest(t))
	ctx := context.Background()
	if err := o.PersistPhase(ctx, domain.PhasePlanning); err != nil {
		t.Fatalf("PersistPhase: %v", err)
	}
	p, err := mgr.Project(ctx)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if p.PipelinePhase != domain.PhasePlanning {
		t.Errorf("phase after PersistPhase = %q, want %q", p.PipelinePhase, domain.PhasePlanning)
	}
}

func TestRun_ResumesFromCurrentPhase(t *testing.T) {
	mgr := seedPipelineProject(t)
	o := NewOrchestrator(mgr, newSwitcherForTest(t))
	ctx := context.Background()
	// Project is in setting. Run from setting should advance to
	// completed through the whole pipeline; the writing + reviewing
	// phases will fail in this test because the registry is missing
	// chapter_write / chapter_review at this point in the build, so
	// the test only asserts the state machine is wired.
	if err := o.PersistPhase(ctx, domain.PhasePlanning); err != nil {
		t.Fatalf("seed phase: %v", err)
	}
	state, err := o.Run(ctx, "")
	if err == nil {
		// full pipeline succeeded: nothing to assert beyond state.
		if state.Phase != domain.PhaseCompleted {
			t.Errorf("terminal phase = %q, want completed", state.Phase)
		}
		return
	}
	// We expect a tool-error from chapter_write / chapter_review
	// since the test stub doesn't install a LLM. The phase should
	// have moved to writing before the failure.
	p, _ := mgr.Project(ctx)
	if p.PipelinePhase != domain.PhaseWriting && p.PipelinePhase != domain.PhaseReviewing {
		t.Errorf("phase after Run = %q, want writing or reviewing", p.PipelinePhase)
	}
}

func TestJSONRoundTrip(t *testing.T) {
	s := State{Phase: domain.PhaseWriting, Step: 3}
	got, err := JSONRoundTrip(s)
	if err != nil {
		t.Fatalf("JSONRoundTrip: %v", err)
	}
	if got == "" {
		t.Error("JSONRoundTrip returned empty string")
	}
}

func TestIndexOfPhase(t *testing.T) {
	cases := map[string]int{
		domain.PhaseSetting:   0,
		domain.PhasePlanning:  1,
		domain.PhaseWriting:   2,
		domain.PhaseReviewing: 3,
		domain.PhaseCompleted: 4,
		"":                    -1,
		"unknown":             -1,
	}
	for p, want := range cases {
		if got := indexOfPhase(p); got != want {
			t.Errorf("indexOfPhase(%q) = %d, want %d", p, got, want)
		}
	}
}
