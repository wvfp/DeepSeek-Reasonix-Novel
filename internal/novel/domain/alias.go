package domain

// Alias maps a free-text name to a canonical entity. The chapter-write
// pipeline scans the generated text for any alias and replaces the match
// with the canonical name so the same person/place is never rendered
// under two different names by accident.
type Alias struct {
	ID         string `json:"id"`
	ProjectID  string `json:"project_id"`
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
	Alias      string `json:"alias"`
	CreatedAt  int64  `json:"created_at"`
}

// Entity-type constants for Alias.EntityType and KGNode.EntityType.
const (
	EntityWorld     = "world"
	EntityCharacter = "character"
	EntityItem      = "item"
	EntityLocation  = "location"
	EntityFaction   = "faction"
	EntityEvent     = "event"
)
