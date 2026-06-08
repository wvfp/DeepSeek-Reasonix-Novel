// Package context provides timeline tracking for characters across chapters.
package context

import (
	"strings"

	"reasonix/internal/novel/domain"
)

// EventType constants for TimelineEvent.EventType.
const (
	EventTypeAppearance        = "appearance"
	EventTypePowerUp           = "power_up"
	EventTypeInjury            = "injury"
	EventTypeRelationshipChange = "relationship_change"
)

// TimelineEvent represents a single event in a character's timeline.
type TimelineEvent struct {
	ChapterID   string `json:"chapter_id"`
	EventType   string `json:"event_type"`
	Description string `json:"description"`
	Importance  int    `json:"importance"`
}

// CharacterTimeline aggregates all events for one character.
type CharacterTimeline struct {
	CharacterID string          `json:"character_id"`
	Events      []TimelineEvent `json:"events"`
}

// TimelineManager extracts and manages character timelines from chapters.
type TimelineManager struct {
	timelines map[string]*CharacterTimeline
}

// NewTimelineManager creates an empty TimelineManager.
func NewTimelineManager() *TimelineManager {
	return &TimelineManager{
		timelines: make(map[string]*CharacterTimeline),
	}
}

// ExtractFromChapter extracts character events from a chapter and its facts.
// It maps facts and character states to timeline events.
func (m *TimelineManager) ExtractFromChapter(chapter *domain.Chapter, facts []domain.ChapterFact) {
	if chapter == nil {
		return
	}

	// Index facts by subject (character name or ID) for quick lookup.
	factsBySubject := make(map[string][]domain.ChapterFact)
	for _, f := range facts {
		if f.Subject != "" {
			factsBySubject[f.Subject] = append(factsBySubject[f.Subject], f)
		}
	}

	// Derive events from facts.
	for subject, subjectFacts := range factsBySubject {
		for _, f := range subjectFacts {
			event := factToEvent(chapter.ID, f)
			if event != nil {
				m.addEvent(subject, *event)
			}
		}
	}
}

// factToEvent converts a ChapterFact into a TimelineEvent based on predicate keywords.
func factToEvent(chapterID string, f domain.ChapterFact) *TimelineEvent {
	pred := strings.ToLower(f.Predicate)
	_ = strings.ToLower(f.Object)

	var eventType string
	var importance int

	switch {
	// Appearance
	case strings.Contains(pred, "appear") || strings.Contains(pred, "登场") || strings.Contains(pred, "出场"):
		eventType = EventTypeAppearance
		importance = 2

	// Power-up
	case strings.Contains(pred, "breakthrough") || strings.Contains(pred, "突破") ||
		strings.Contains(pred, "advance") || strings.Contains(pred, "提升") ||
		strings.Contains(pred, "power") || strings.Contains(pred, "实力") ||
		strings.Contains(pred, "realm") || strings.Contains(pred, "境界"):
		eventType = EventTypePowerUp
		importance = 4

	// Injury
	case strings.Contains(pred, "injur") || strings.Contains(pred, "wound") ||
		strings.Contains(pred, "hurt") || strings.Contains(pred, "damage") ||
		strings.Contains(pred, "受伤") || strings.Contains(pred, "负伤") ||
		strings.Contains(pred, "重创") || strings.Contains(pred, "击杀"):
		eventType = EventTypeInjury
		importance = 4

	// Relationship change
	case strings.Contains(pred, "ally") || strings.Contains(pred, "enemy") ||
		strings.Contains(pred, "friend") || strings.Contains(pred, "love") ||
		strings.Contains(pred, "betray") || strings.Contains(pred, "结盟") ||
		strings.Contains(pred, "反目") || strings.Contains(pred, "结仇") ||
		strings.Contains(pred, "结为") || strings.Contains(pred, "师徒") ||
		strings.Contains(pred, "恋人") || strings.Contains(pred, "夫妻"):
		eventType = EventTypeRelationshipChange
		importance = 3

	default:
		// Skip unrecognised facts.
		return nil
	}

	desc := f.Subject + " " + f.Predicate + " " + f.Object
	if f.Context != "" {
		desc += "（" + f.Context + "）"
	}

	return &TimelineEvent{
		ChapterID:   chapterID,
		EventType:   eventType,
		Description: desc,
		Importance:  importance,
	}
}

// addEvent appends an event to a character's timeline, creating the timeline if needed.
func (m *TimelineManager) addEvent(characterID string, event TimelineEvent) {
	t, ok := m.timelines[characterID]
	if !ok {
		t = &CharacterTimeline{CharacterID: characterID}
		m.timelines[characterID] = t
	}
	t.Events = append(t.Events, event)
}

// GetTimeline returns the full timeline for a character.
func (m *TimelineManager) GetTimeline(characterID string) *CharacterTimeline {
	if t, ok := m.timelines[characterID]; ok {
		// Return a shallow copy so callers can't mutate internal state directly.
		eventsCopy := make([]TimelineEvent, len(t.Events))
		copy(eventsCopy, t.Events)
		return &CharacterTimeline{
			CharacterID: t.CharacterID,
			Events:      eventsCopy,
		}
	}
	return &CharacterTimeline{CharacterID: characterID}
}

// GetKeyEvents returns events with importance >= minImportance.
func (m *TimelineManager) GetKeyEvents(characterID string, minImportance int) []TimelineEvent {
	if minImportance < 1 {
		minImportance = 1
	}
	if minImportance > 5 {
		minImportance = 5
	}
	t := m.GetTimeline(characterID)
	var out []TimelineEvent
	for _, e := range t.Events {
		if e.Importance >= minImportance {
			out = append(out, e)
		}
	}
	return out
}

// CompressTimeline returns a compressed timeline keeping only events with importance >= 3.
func (m *TimelineManager) CompressTimeline(characterID string) *CharacterTimeline {
	keyEvents := m.GetKeyEvents(characterID, 3)
	return &CharacterTimeline{
		CharacterID: characterID,
		Events:      keyEvents,
	}
}

// GetAllTimelines returns a shallow copy of all character timelines.
func (m *TimelineManager) GetAllTimelines() map[string]*CharacterTimeline {
	out := make(map[string]*CharacterTimeline, len(m.timelines))
	for id, t := range m.timelines {
		eventsCopy := make([]TimelineEvent, len(t.Events))
		copy(eventsCopy, t.Events)
		out[id] = &CharacterTimeline{
			CharacterID: t.CharacterID,
			Events:      eventsCopy,
		}
	}
	return out
}
