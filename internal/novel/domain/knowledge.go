package domain

// KGNode is one node in the knowledge graph. The graph is generic — an
// EntityType of "world" / "character" / "item" / "location" / "faction"
// / "event" lets the same table hold all six kinds. EntityID is the FK
// into the matching table (worlds, characters, …).
type KGNode struct {
	ID         string         `json:"id"`
	ProjectID  string         `json:"project_id"`
	EntityType string         `json:"entity_type"`
	EntityID   string         `json:"entity_id"`
	Name       string         `json:"name"`
	Attributes map[string]any `json:"attributes,omitempty"`
	CreatedAt  int64          `json:"created_at"`
}

// KGEdge is a directed edge between two KGNodes. Relation is a free-form
// tag ("owns", "allies", "located_in", "participates_in", "related_to",
// …). Weight is used by the visual graph renderer to scale line
// thickness; default 1.0.
type KGEdge struct {
	ID         string         `json:"id"`
	ProjectID  string         `json:"project_id"`
	FromNodeID string         `json:"from_node_id"`
	ToNodeID   string         `json:"to_node_id"`
	Relation   string         `json:"relation"`
	Weight     float64        `json:"weight"`
	Attributes map[string]any `json:"attributes,omitempty"`
	CreatedAt  int64          `json:"created_at"`
}

// EntityLink is a generic mention-style link: a chapter (or review)
// references a world, character, item, etc. It is separate from KG edges
// because EntityLink tracks the *provenance* of a mention (which
// chapter said what about whom), whereas KG edges track the canonical
// relationship that the project authorises.
type EntityLink struct {
	ID         string `json:"id"`
	ProjectID  string `json:"project_id"`
	SourceType string `json:"source_type"`
	SourceID   string `json:"source_id"`
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	LinkType   string `json:"link_type"`
	CreatedAt  int64  `json:"created_at"`
}
