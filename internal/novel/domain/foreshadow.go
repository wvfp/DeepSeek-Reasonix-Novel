package domain

// Foreshadow is a planted plot seed. A foreshadow moves through four
// states: planted → developing → resolved (or abandoned at any time).
// Importance is minor / major / critical — Reviewer uses it to flag
// critical foreshadows that have not been resolved by the time the
// manuscript is wrapped.
type Foreshadow struct {
	ID                string   `json:"id"`
	ProjectID         string   `json:"project_id"`
	PlantedChapterID  string   `json:"planted_chapter_id,omitempty"`
	ResolvedChapterID string   `json:"resolved_chapter_id,omitempty"`
	Description       string   `json:"description"`
	Keywords          []string `json:"keywords,omitempty"`
	Status            string   `json:"status"`
	Importance        string   `json:"importance"`
	CreatedAt         int64    `json:"created_at"`
	ModifiedAt        int64    `json:"modified_at"`
}

// Foreshadow status values.
const (
	ForeshadowPlanted    = "planted"
	ForeshadowDeveloping = "developing"
	ForeshadowResolved   = "resolved"
	ForeshadowAbandoned  = "abandoned"
)

// Foreshadow importance values.
const (
	ForeshadowMinor    = "minor"
	ForeshadowMajor    = "major"
	ForeshadowCritical = "critical"
)
