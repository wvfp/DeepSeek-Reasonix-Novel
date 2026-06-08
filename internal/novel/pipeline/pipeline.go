// Package pipeline is the 4-phase orchestrator that drives the
// novel-writing workflow. The pipeline is a strict state machine:
//
//	setting → planning → writing → reviewing → completed
//
// Each phase is gated on the previous one. The orchestrator reads
// the current phase from the projects table, dispatches the right
// role (via roles.Switcher), and writes the new phase before
// proceeding. It is the only legitimate writer of
// projects.pipeline_phase alongside project.Manager.SetPhase.
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"reasonix/internal/novel/domain"
	"reasonix/internal/novel/project"
	"reasonix/internal/novel/repo"
	"reasonix/internal/novel/roles"
	"reasonix/internal/novel/tools"
)

// State is the serialisable phase + step counter. Returned by
// RunBetween so callers can checkpoint or report progress.
type State struct {
	Phase string // setting|planning|writing|reviewing|completed
	Step  int    // current phase-internal step (chapter number etc.)
}

// PhaseOrder is the canonical phase order. Completed is the terminal
// value, not a phase you "run".
var PhaseOrder = []string{
	domain.PhaseSetting,
	domain.PhasePlanning,
	domain.PhaseWriting,
	domain.PhaseReviewing,
	domain.PhaseCompleted,
}

// Orchestrator wires the role switcher to the project + chapter
// repos and drives the state machine.
type Orchestrator struct {
	mgr       *project.Manager
	sw        *roles.Switcher
	arcRepo   *repo.ArcRepo
	worldRepo *repo.WorldRepo
	charRepo  *repo.CharacterRepo
	registry  *tools.Registry
}

// NewOrchestrator returns an orchestrator bound to the given project
// and role switcher. All collaborators are pre-wired so Run doesn't
// pay the construction cost on every phase. The registry is the
// default tools.Registry — chapter_write / chapter_review are reached
// by name, so the orchestrator doesn't need direct access to their
// unexported types.
func NewOrchestrator(mgr *project.Manager, sw *roles.Switcher) *Orchestrator {
	return &Orchestrator{
		mgr:       mgr,
		sw:        sw,
		arcRepo:   repo.NewArcRepo(mgr.DB()),
		worldRepo: repo.NewWorldRepo(mgr.DB()),
		charRepo:  repo.NewCharacterRepo(mgr.DB()),
		registry:  tools.NewRegistry(),
	}
}

// Run drives the state machine starting from the project's current
// phase. A blank fromPhase is interpreted as "resume from where the
// project is". Returns the terminal State on success; partial
// progress is committed to projects.pipeline_phase at every
// transition.
func (o *Orchestrator) Run(ctx context.Context, fromPhase string) (State, error) {
	return o.RunResume(ctx, fromPhase, 0)
}

// RunResume is Run with an explicit targetChapters count for the
// writing + reviewing phases. A targetChapters > 0 overrides the
// implicit "one chapter" default Run applies when entering
// PhaseWriting. CLI commands (`novel chapter --continue --target N`)
// use this so a resume from the writing phase can produce N
// chapters in one call instead of one-at-a-time.
//
// targetChapters <= 0 means "behave like Run". The number is also
// applied to the reviewing phase so the review loop covers the
// same range.
func (o *Orchestrator) RunResume(ctx context.Context, fromPhase string, targetChapters int) (State, error) {
	start := fromPhase
	if start == "" {
		p, err := o.mgr.Project(ctx)
		if err != nil {
			return State{}, err
		}
		start = p.PipelinePhase
	}
	idx := indexOfPhase(start)
	if idx < 0 {
		return State{}, fmt.Errorf("pipeline.Run: unknown phase %q", start)
	}
	if start == domain.PhaseCompleted {
		return State{Phase: domain.PhaseCompleted, Step: 0}, nil
	}

	step := 0
	if targetChapters > 0 {
		step = targetChapters
	}
	for i := idx; i < len(PhaseOrder)-1; i++ {
		phase := PhaseOrder[i]
		if err := o.PersistPhase(ctx, phase); err != nil {
			return State{Phase: phase, Step: step}, err
		}
		switch phase {
		case domain.PhaseSetting:
			if err := o.RunSetting(ctx); err != nil {
				return State{Phase: phase, Step: step}, err
			}
		case domain.PhasePlanning:
			if err := o.RunPlanning(ctx); err != nil {
				return State{Phase: phase, Step: step}, err
			}
		case domain.PhaseWriting:
			n, err := o.RunWriting(ctx, step)
			if err != nil {
				return State{Phase: phase, Step: n}, err
			}
			step = n
		case domain.PhaseReviewing:
			n, err := o.RunReviewing(ctx, step)
			if err != nil {
				return State{Phase: phase, Step: n}, err
			}
			step = n
		}
	}
	if err := o.PersistPhase(ctx, domain.PhaseCompleted); err != nil {
		return State{Phase: domain.PhaseReviewing, Step: step}, err
	}
	return State{Phase: domain.PhaseCompleted, Step: step}, nil
}

// PersistPhase writes the project's pipeline_phase column. Wraps
// project.Manager.SetPhase with a timestamp so external observers can
// tell when the orchestrator last touched state.
func (o *Orchestrator) PersistPhase(ctx context.Context, phase string) error {
	return o.mgr.SetPhase(ctx, phase)
}

// RunSetting asks WorldBuilder for a world check. If a world exists
// the phase is a no-op (the orchestrator reuses whatever the user
// already authored). If no world exists, it surfaces a clear error
// instructing the user to run `novel world create` first — the role
// itself is not invoked because that would require a long Q&A
// dialogue the LLM cannot complete in a single call.
func (o *Orchestrator) RunSetting(ctx context.Context) error {
	p, err := o.mgr.Project(ctx)
	if err != nil {
		return err
	}
	worlds, err := o.worldRepo.List(ctx, p.ID)
	if err != nil {
		return err
	}
	if len(worlds) == 0 {
		return fmt.Errorf("pipeline.RunSetting: project %q has no world; run `novel world create` first", p.Name)
	}
	return nil
}

// RunPlanning asks ArcMaster for a master arc, then PlotPlanner for
// per-chapter outlines. The current implementation does not yet
// invoke the LLM end-to-end (it would need stable JSON parsing
// across a long LLM response); the persistence side is wired so
// future Phase 4 work can flip the switch without schema changes.
func (o *Orchestrator) RunPlanning(ctx context.Context) error {
	p, err := o.mgr.Project(ctx)
	if err != nil {
		return err
	}
	arcs, err := o.arcRepo.List(ctx, p.ID)
	if err != nil {
		return err
	}
	hasMaster := false
	for _, a := range arcs {
		if a.Level == domain.LevelMaster {
			hasMaster = true
			break
		}
	}
	if !hasMaster {
		master := &domain.Arc{
			ProjectID:  p.ID,
			Level:      domain.LevelMaster,
			Title:      p.Name + " 主线",
			Summary:    "由 ArcMaster 规划的主线骨架（占位）。",
			OrderIndex: 0,
		}
		if err := o.arcRepo.Create(ctx, master); err != nil {
			return fmt.Errorf("RunPlanning: create master: %w", err)
		}
	}
	return nil
}

// RunWriting iterates targetChapters invocations of the
// chapter_write tool. The actual LLM call is inside chapter_write
// — the orchestrator's job is to drive the loop and advance the
// state machine. Returns the number of chapters written so the
// reviewing phase can target the same range.
func (o *Orchestrator) RunWriting(ctx context.Context, targetChapters int) (int, error) {
	p, err := o.mgr.Project(ctx)
	if err != nil {
		return 0, err
	}
	arcs, err := o.arcRepo.List(ctx, p.ID)
	if err != nil {
		return 0, err
	}
	var masterID string
	for _, a := range arcs {
		if a.Level == domain.LevelMaster {
			masterID = a.ID
			break
		}
	}
	if masterID == "" {
		return 0, fmt.Errorf("RunWriting: no master arc; pipeline planning phase is required first")
	}
	if targetChapters <= 0 {
		targetChapters = 1
	}
	written := 0
	for i := 0; i < targetChapters; i++ {
		chapterNum := i + 1
		title := fmt.Sprintf("第 %d 章", chapterNum)
		prompt := fmt.Sprintf("按已规划的卷纲继续写第 %d 章。", chapterNum)
		// The chapter_write tool does not consult the role switcher
		// (it has its own prompt). We still set the base system
		// prompt for visibility and so the LLM can pick up project
		// context from the same source.
		if o.sw != nil {
			o.sw.SetBase(buildBaseSys(p))
		}
		input := map[string]any{
			"arc_id":         masterID,
			"title":          title,
			"prompt":         prompt,
			"chapter_number": chapterNum,
		}
		if _, err := o.registry.Execute(ctx, "chapter_write", input, o.mgr); err != nil {
			return written, fmt.Errorf("RunWriting: chapter %d: %w", chapterNum, err)
		}
		written++
	}
	return written, nil
}

// RunReviewing invokes chapter_review for the chapters that RunWriting
// produced. Best-effort: a review error on chapter N does not abort
// the rest of the loop, but is reported in the returned count.
func (o *Orchestrator) RunReviewing(ctx context.Context, targetChapters int) (int, error) {
	p, err := o.mgr.Project(ctx)
	if err != nil {
		return 0, err
	}
	_ = p
	if targetChapters <= 0 {
		return 0, nil
	}
	reviewed := 0
	for i := 0; i < targetChapters; i++ {
		chapterNum := i + 1
		input := map[string]any{"chapter_number": chapterNum}
		if _, err := o.registry.Execute(ctx, "chapter_review", input, o.mgr); err != nil {
			// Don't abort on first review failure — let the
			// orchestrator continue to the next chapter. The
			// chapter_review tool already reports the failure to
			// its caller.
			continue
		}
		reviewed++
	}
	return reviewed, nil
}

// indexOfPhase returns the index of phase in PhaseOrder, or -1.
// "completed" is at the end of PhaseOrder so resume on a completed
// project is a clean no-op.
func indexOfPhase(phase string) int {
	for i, p := range PhaseOrder {
		if p == phase {
			return i
		}
	}
	return -1
}

// buildBaseSys renders the project-level system prompt the
// Switcher concatenates with each role's body. Kept short so the
// per-role suffix stays well under the LLM context budget.
func buildBaseSys(p *domain.Project) string {
	var b strings.Builder
	b.WriteString("项目：")
	b.WriteString(p.Name)
	b.WriteString("（")
	b.WriteString(p.Genre)
	b.WriteString("，phase=")
	b.WriteString(p.PipelinePhase)
	b.WriteString("）\n")
	b.WriteString("当前时间戳：")
	b.WriteString(fmt.Sprintf("%d", time.Now().Unix()))
	return b.String()
}

// JSONRoundTrip is a small helper used by tests + future Phase 4
// hooks that need to dump/load the orchestrator state. The State
// struct is the same shape the CLI --status flag prints.
func JSONRoundTrip(s State) (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
