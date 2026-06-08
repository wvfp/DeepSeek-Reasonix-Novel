// Package progress provides terminal progress reporting for the novel
// pipeline. It is intentionally minimal: a single reporter interface
// plus a CLI implementation that draws multi-line progress bars to
// stderr.
package progress

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// Stage names used across the novel pipeline.
const (
	StageIndexing       = "Indexing"
	StageGenerating     = "Generating"
	StageScrubbing      = "Scrubbing"
	StageSaving         = "Saving"
	StageSetup          = "Setup"
	StageWorldBuilding  = "WorldBuilding"
	StageCharacterCreation = "CharacterCreation"
	StageArcPlanning    = "ArcPlanning"
	StageChapterWriting = "ChapterWriting"
	StageReviewing      = "Reviewing"
	StageExporting      = "Exporting"
)

// ProgressEvent represents a single progress update event.
type ProgressEvent struct {
	Stage      string
	SubTask    string
	Progress   float64
	Done       bool
	Err        error
}

// ProgressListener is called whenever a progress event occurs.
type ProgressListener func(event ProgressEvent)

// ProgressReporter is the contract for surfacing pipeline progress.
type ProgressReporter interface {
	Start(stage string)
	StartSubTask(stage string, subTask string)
	Update(stage string, progress float64)
	UpdateSubTask(stage string, subTask string, progress float64)
	Done(stage string)
	DoneSubTask(stage string, subTask string)
	Error(stage string, err error)
	ErrorSubTask(stage string, subTask string, err error)
}

// CLIPReporter prints progress bars to stderr. It supports multiple
// stages displayed in parallel, each on its own line.
type CLIPReporter struct {
	mu        sync.Mutex
	stages    map[string]*stageState
	order     []string
	w         *os.File
	listeners []ProgressListener
}

// stageState tracks the current progress of one stage.
type stageState struct {
	name     string
	percent  float64
	done     bool
	err      string
	subTasks map[string]*subTaskState
}

// subTaskState tracks the current progress of one sub-task.
type subTaskState struct {
	name    string
	percent float64
	done    bool
	err     string
}

// NewCLIPReporter creates a reporter that writes to stderr.
func NewCLIPReporter() *CLIPReporter {
	return &CLIPReporter{
		stages: make(map[string]*stageState),
		w:      os.Stderr,
	}
}

// AddListener registers a new progress event listener.
func (r *CLIPReporter) AddListener(listener ProgressListener) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listeners = append(r.listeners, listener)
}

// RemoveListener removes a previously registered progress event listener.
func (r *CLIPReporter) RemoveListener(listener ProgressListener) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, l := range r.listeners {
		if fmt.Sprintf("%p", l) == fmt.Sprintf("%p", listener) {
			r.listeners = append(r.listeners[:i], r.listeners[i+1:]...)
			break
		}
	}
}

func (r *CLIPReporter) broadcast(event ProgressEvent) {
	for _, l := range r.listeners {
		l(event)
	}
}

// Start registers a new stage and prints an empty progress bar.
func (r *CLIPReporter) Start(stage string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.stages[stage]; !ok {
		r.order = append(r.order, stage)
	}
	r.stages[stage] = &stageState{name: stage, percent: 0, subTasks: make(map[string]*subTaskState)}
	r.broadcast(ProgressEvent{Stage: stage, Progress: 0})
	r.redraw()
}

// StartSubTask registers a new sub-task under a stage.
func (r *CLIPReporter) StartSubTask(stage string, subTask string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, ok := r.stages[stage]
	if !ok {
		r.order = append(r.order, stage)
		s = &stageState{name: stage, percent: 0, subTasks: make(map[string]*subTaskState)}
		r.stages[stage] = s
	}
	s.subTasks[subTask] = &subTaskState{name: subTask, percent: 0}
	r.broadcast(ProgressEvent{Stage: stage, SubTask: subTask, Progress: 0})
	r.redraw()
}

// Update sets the progress for a stage (0.0 – 1.0) and redraws.
func (r *CLIPReporter) Update(stage string, progress float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if s, ok := r.stages[stage]; ok {
		s.percent = clamp01(progress)
		r.broadcast(ProgressEvent{Stage: stage, Progress: s.percent})
		r.redraw()
	}
}

// UpdateSubTask sets the progress for a sub-task (0.0 – 1.0) and redraws.
func (r *CLIPReporter) UpdateSubTask(stage string, subTask string, progress float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if s, ok := r.stages[stage]; ok {
		if st, ok := s.subTasks[subTask]; ok {
			st.percent = clamp01(progress)
			r.broadcast(ProgressEvent{Stage: stage, SubTask: subTask, Progress: st.percent})
			r.redraw()
		}
	}
}

// Done marks a stage as finished and redraws.
func (r *CLIPReporter) Done(stage string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if s, ok := r.stages[stage]; ok {
		s.percent = 1.0
		s.done = true
		r.broadcast(ProgressEvent{Stage: stage, Progress: 1.0, Done: true})
		r.redraw()
	}
}

// DoneSubTask marks a sub-task as finished and redraws.
func (r *CLIPReporter) DoneSubTask(stage string, subTask string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if s, ok := r.stages[stage]; ok {
		if st, ok := s.subTasks[subTask]; ok {
			st.percent = 1.0
			st.done = true
			r.broadcast(ProgressEvent{Stage: stage, SubTask: subTask, Progress: 1.0, Done: true})
			r.redraw()
		}
	}
}

// Error marks a stage as failed with a short message and redraws.
func (r *CLIPReporter) Error(stage string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if s, ok := r.stages[stage]; ok {
		s.err = err.Error()
		r.broadcast(ProgressEvent{Stage: stage, Err: err})
		r.redraw()
	}
}

// ErrorSubTask marks a sub-task as failed with a short message and redraws.
func (r *CLIPReporter) ErrorSubTask(stage string, subTask string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if s, ok := r.stages[stage]; ok {
		if st, ok := s.subTasks[subTask]; ok {
			st.err = err.Error()
			r.broadcast(ProgressEvent{Stage: stage, SubTask: subTask, Err: err})
			r.redraw()
		}
	}
}

// redraw re-prints every active stage line. It assumes the caller
// holds r.mu.
func (r *CLIPReporter) redraw() {
	// Move cursor up to overwrite previous lines.
	lineCount := len(r.order)
	for _, name := range r.order {
		s := r.stages[name]
		lineCount += len(s.subTasks)
	}
	if lineCount > 0 {
		fmt.Fprint(r.w, strings.Repeat("\033[A\r", lineCount))
	}
	for _, name := range r.order {
		s := r.stages[name]
		line := formatBar(s.name, s.percent, s.done, s.err)
		fmt.Fprintln(r.w, line)
		for _, st := range s.subTasks {
			subLine := formatSubBar("  └─ "+st.name, st.percent, st.done, st.err)
			fmt.Fprintln(r.w, subLine)
		}
	}
}

// SilentReporter is a no-op reporter for non-interactive environments.
type SilentReporter struct {
	listeners []ProgressListener
}

// NewSilentReporter creates a no-op reporter.
func NewSilentReporter() *SilentReporter {
	return &SilentReporter{}
}

// AddListener registers a new progress event listener.
func (r *SilentReporter) AddListener(listener ProgressListener) {
	r.listeners = append(r.listeners, listener)
}

// RemoveListener removes a previously registered progress event listener.
func (r *SilentReporter) RemoveListener(listener ProgressListener) {
	for i, l := range r.listeners {
		if fmt.Sprintf("%p", l) == fmt.Sprintf("%p", listener) {
			r.listeners = append(r.listeners[:i], r.listeners[i+1:]...)
			break
		}
	}
}

func (r *SilentReporter) broadcast(event ProgressEvent) {
	for _, l := range r.listeners {
		l(event)
	}
}

func (r *SilentReporter) Start(stage string)                         { r.broadcast(ProgressEvent{Stage: stage, Progress: 0}) }
func (r *SilentReporter) StartSubTask(stage string, subTask string)  { r.broadcast(ProgressEvent{Stage: stage, SubTask: subTask, Progress: 0}) }
func (r *SilentReporter) Update(stage string, progress float64)      { r.broadcast(ProgressEvent{Stage: stage, Progress: progress}) }
func (r *SilentReporter) UpdateSubTask(stage string, subTask string, progress float64) {
	r.broadcast(ProgressEvent{Stage: stage, SubTask: subTask, Progress: progress})
}
func (r *SilentReporter) Done(stage string)                          { r.broadcast(ProgressEvent{Stage: stage, Progress: 1.0, Done: true}) }
func (r *SilentReporter) DoneSubTask(stage string, subTask string)   { r.broadcast(ProgressEvent{Stage: stage, SubTask: subTask, Progress: 1.0, Done: true}) }
func (r *SilentReporter) Error(stage string, err error)              { r.broadcast(ProgressEvent{Stage: stage, Err: err}) }
func (r *SilentReporter) ErrorSubTask(stage string, subTask string, err error) {
	r.broadcast(ProgressEvent{Stage: stage, SubTask: subTask, Err: err})
}

// formatBar renders one line like:
//
//	[Indexing] ████████░░ 80%
func formatBar(name string, pct float64, done bool, err string) string {
	const barWidth = 20
	filled := int(pct * barWidth)
	if filled > barWidth {
		filled = barWidth
	}
	empty := barWidth - filled
	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
	percent := int(pct * 100)
	if done && err == "" {
		return fmt.Sprintf("[%s] %s 100%% ✓", name, strings.Repeat("█", barWidth))
	}
	if err != "" {
		return fmt.Sprintf("[%s] %s %3d%% ✗ %s", name, bar, percent, err)
	}
	return fmt.Sprintf("[%s] %s %3d%%", name, bar, percent)
}

// formatSubBar renders a sub-task line like:
//
//	  └─ SubTask] ████████░░ 80%
func formatSubBar(name string, pct float64, done bool, err string) string {
	const barWidth = 20
	filled := int(pct * barWidth)
	if filled > barWidth {
		filled = barWidth
	}
	empty := barWidth - filled
	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
	percent := int(pct * 100)
	if done && err == "" {
		return fmt.Sprintf("%s %s 100%% ✓", name, strings.Repeat("█", barWidth))
	}
	if err != "" {
		return fmt.Sprintf("%s %s %3d%% ✗ %s", name, bar, percent, err)
	}
	return fmt.Sprintf("%s %s %3d%%", name, bar, percent)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
