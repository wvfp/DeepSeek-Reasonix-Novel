// Package domain holds the in-memory data model for the novel subsystem.
// Types here are the JSON-serialisable shape used by the SQLite layer and
// passed between tools. The field tags are stable wire format — do not
// rename without a migration step.
package domain

// World represents a single novel setting (e.g. an xianxia realm, a
// sci-fi galaxy, a horror house). The Content field carries the Markdown
// body written to .novel-weaver/content/settings/world-{slug}.md; Metadata
// is opaque JSON.
type World struct {
	ID          string         `json:"id"`
	ProjectID   string         `json:"project_id"`
	Name        string         `json:"name"`
	Slug        string         `json:"slug"`
	Description string         `json:"description,omitempty"`
	Content     string         `json:"content,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   int64          `json:"created_at"`
	ModifiedAt  int64          `json:"modified_at"`
}

// WorldField is a single key/value entry on a world. Worlds with many
// fields (population, climate, magic system, …) can be flattened to a
// list of these for table-style rendering in the UI.
type WorldField struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Note  string `json:"note,omitempty"`
}

// WorldLink connects two worlds (e.g. "located_in", "adjacent_to",
// "ruled_by"). Stored separately from World.Metadata so the link list can
// be queried efficiently.
type WorldLink struct {
	FromWorldID string `json:"from_world_id"`
	ToWorldID   string `json:"to_world_id"`
	Relation    string `json:"relation"`
	Note        string `json:"note,omitempty"`
}
