package domain

// Project is the top-level container for a single novel. The
// PipelinePhase field is the state machine position; the
// novel-pipeline-start tool advances it through the four phases
// (setting → planning → writing → reviewing) and finally to completed.
type Project struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Genre         string `json:"genre"`
	PipelinePhase string `json:"pipeline_phase"`
	CreatedAt     int64  `json:"created_at"`
	ModifiedAt    int64  `json:"modified_at"`
}

// Pipeline-phase constants. The order is intentional: a project may
// only advance forward (or stay where it is).
const (
	PhaseSetting   = "setting"
	PhasePlanning  = "planning"
	PhaseWriting   = "writing"
	PhaseReviewing = "reviewing"
	PhaseCompleted = "completed"
)

// Genre defaults. The runtime default is "fantasy" — match the
// novel-plugin DEFAULT_CONFIG.defaultGenre.
const (
	GenreFantasy      = "fantasy"
	GenreXianxia      = "xianxia"
	GenreUrban        = "urban"
	GenreSciFi        = "sci-fi"
	GenreHorror       = "horror"
	GenreApocalypse   = "apocalypse"
	GenreInfiniteFlow = "infinite-flow"
)
