package domain

// Arc is a node in the multi-level outline hierarchy. The level field
// drives how the row is rendered: "master" (overall story arc),
// "volume" (a single volume/卷), "chapter" (per-chapter outline), or
// "blueprint" (a detailed scene/beat plan for a single chapter). The
// ParentID chains levels: master → volume → chapter → blueprint.
type Arc struct {
	ID         string         `json:"id"`
	ProjectID  string         `json:"project_id"`
	ParentID   string         `json:"parent_id,omitempty"`
	Level      string         `json:"level"`
	Title      string         `json:"title"`
	Summary    string         `json:"summary,omitempty"`
	OrderIndex int            `json:"order_index"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	CreatedAt  int64          `json:"created_at"`
	ModifiedAt int64          `json:"modified_at"`
}

// Outline-level constants. Mirrors the values accepted by the
// `outlines.level` column.
const (
	LevelMaster    = "master"
	LevelVolume    = "volume"
	LevelChapter   = "chapter"
	LevelBlueprint = "blueprint"
)
