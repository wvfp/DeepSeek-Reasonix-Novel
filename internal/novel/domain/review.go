package domain

// Review is a single dimension of a chapter review. The Reviewer
// produces 8 of these per chapter (one per Dimension). Issues is the
// JSON array of concrete problems; the Reviewer ranks each by
// Severity.
type Review struct {
	ID        string  `json:"id"`
	ChapterID string  `json:"chapter_id"`
	Dimension string  `json:"dimension"`
	Score     float64 `json:"score"`
	Issues    []Issue `json:"issues,omitempty"`
	CreatedAt int64   `json:"created_at"`
}

// Issue is a single problem found in a chapter. Severity drives the
// auto-fix policy: blocker must be fixed before publication, warning is
// recommended, info is FYI.
type Issue struct {
	Severity    string `json:"severity"`
	Description string `json:"description"`
	Location    string `json:"location,omitempty"`
}

// Review dimension constants. Eight dimensions, matching the
// novel-plugin reviewer.
const (
	ReviewDimPlot        = "plot"
	ReviewDimCharacter   = "character"
	ReviewDimStyle       = "style"
	ReviewDimConsistency = "consistency"
	ReviewDimPacing      = "pacing"
	ReviewDimForeshadow  = "foreshadow"
	ReviewDimHook        = "hook"
	ReviewDimValues      = "values"
)

// Issue severity constants.
const (
	SeverityBlocker = "blocker"
	SeverityWarning = "warning"
	SeverityInfo    = "info"
)
