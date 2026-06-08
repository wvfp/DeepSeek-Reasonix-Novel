package domain

// Chapter is a single chapter of a novel. Status follows the lifecycle
// draft → reviewing → final. ArcID is the outline row (level = "chapter"
// or "blueprint") this chapter rolls up to.
type Chapter struct {
	ID            string `json:"id"`
	ProjectID     string `json:"project_id"`
	ArcID         string `json:"arc_id,omitempty"`
	Volume        int    `json:"volume"`
	ChapterNumber int    `json:"chapter_number"`
	Title         string `json:"title"`
	Slug          string `json:"slug"`
	Content       string `json:"content"`
	WordCount     int    `json:"word_count"`
	Status        string `json:"status"`
	CreatedAt     int64  `json:"created_at"`
	ModifiedAt    int64  `json:"modified_at"`
}

// Fact-type constants for ChapterFact.FactType. The nine values mirror
// the TS-side enum in novel-plugin and cover the structured fact
// extraction needed for long-form consistency.
const (
	FactTypeEvent       = "event"
	FactTypeStateChange = "state_change"
	FactTypeRevelation  = "revelation"
	FactTypeLocation    = "location"
	FactTypeTime        = "time"
	FactTypeRelation    = "relation"
	FactTypeAction      = "action"
	FactTypeDialogue    = "dialogue"
	FactTypeItem        = "item"
)

// ChapterFact is one structured fact extracted from a chapter. Subject
// + Predicate + Object form a triple; Confidence is the model's
// self-reported confidence (0.0–1.0).
type ChapterFact struct {
	ID         string  `json:"id"`
	ProjectID  string  `json:"project_id"`
	ChapterID  string  `json:"chapter_id"`
	FactType   string  `json:"fact_type"`
	Subject    string  `json:"subject"`
	Predicate  string  `json:"predicate"`
	Object     string  `json:"object"`
	Confidence float64 `json:"confidence"`
	Context    string  `json:"context,omitempty"`
	CreatedAt  int64   `json:"created_at"`
}
