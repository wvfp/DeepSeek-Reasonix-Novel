package progress

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// captureReporter is a test helper that records output into a buffer.
type captureReporter struct {
	*CLIPReporter
	buf *bytes.Buffer
}

func newCaptureReporter() *captureReporter {
	buf := &bytes.Buffer{}
	// We can't redirect os.Stderr easily in parallel tests, so we
	// exercise the formatting helpers directly.
	return &captureReporter{buf: buf}
}

func TestFormatBar(t *testing.T) {
	tests := []struct {
		name   string
		pct    float64
		done   bool
		err    string
		wantIn []string
	}{
		{
			name:   "zero",
			pct:    0,
			wantIn: []string{"[zero]", "0%"},
		},
		{
			name:   "half",
			pct:    0.5,
			wantIn: []string{"[half]", "50%"},
		},
		{
			name:   "done",
			pct:    1,
			done:   true,
			wantIn: []string{"[done]", "100%", "✓"},
		},
		{
			name:   "error",
			pct:    0.3,
			err:    "boom",
			wantIn: []string{"[error]", "30%", "✗", "boom"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatBar(tt.name, tt.pct, tt.done, tt.err)
			for _, w := range tt.wantIn {
				if !strings.Contains(got, w) {
					t.Fatalf("expected %q to contain %q", got, w)
				}
			}
		})
	}
}

func TestCLIPReporter_Lifecycle(t *testing.T) {
	r := NewCLIPReporter()

	// Start two stages.
	r.Start(StageIndexing)
	r.Start(StageGenerating)

	// Update one.
	r.Update(StageIndexing, 0.5)

	// Finish the other.
	r.Done(StageGenerating)

	// Error the first.
	r.Error(StageIndexing, errors.New("parse failed"))

	// Verify internal state.
	s := r.stages[StageIndexing]
	if s == nil {
		t.Fatal("missing indexing stage")
	}
	if s.percent != 0.5 {
		t.Fatalf("expected percent 0.5, got %f", s.percent)
	}
	if s.err != "parse failed" {
		t.Fatalf("expected err 'parse failed', got %q", s.err)
	}

	s2 := r.stages[StageGenerating]
	if s2 == nil {
		t.Fatal("missing generating stage")
	}
	if !s2.done {
		t.Fatal("expected generating done")
	}
}

func TestClamp01(t *testing.T) {
	if clamp01(-0.1) != 0 {
		t.Fatal("expected 0")
	}
	if clamp01(1.5) != 1 {
		t.Fatal("expected 1")
	}
	if clamp01(0.5) != 0.5 {
		t.Fatal("expected 0.5")
	}
}

func TestFormatBarWidth(t *testing.T) {
	// The bar should always contain exactly barWidth block characters.
	const barWidth = 20
	for _, pct := range []float64{0, 0.25, 0.5, 0.75, 1} {
		line := formatBar("X", pct, false, "")
		blocks := strings.Count(line, "█") + strings.Count(line, "░")
		if blocks != barWidth {
			t.Fatalf("pct=%f: expected %d blocks, got %d in %q", pct, barWidth, blocks, line)
		}
	}
}

func ExampleCLIPReporter() {
	r := NewCLIPReporter()
	r.Start(StageIndexing)
	r.Update(StageIndexing, 0.8)
	fmt.Println(formatBar(StageIndexing, 0.8, false, ""))
	// Output: [Indexing] ████████████████░░░░  80%
}
