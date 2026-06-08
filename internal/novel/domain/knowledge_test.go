package domain

import "testing"

func TestKGNode_RoundTrip(t *testing.T) {
	n := KGNode{
		ID:         "n1",
		ProjectID:  "p1",
		EntityType: EntityCharacter,
		EntityID:   "c1",
		Name:       "萧炎",
		Attributes: map[string]any{"role": "protagonist", "age": 17.0},
		CreatedAt:  1700000000,
	}
	got := roundTrip(t, n).(KGNode)
	assertEqual(t, "KGNode", n, got)
}

func TestKGEdge_RoundTrip(t *testing.T) {
	e := KGEdge{
		ID:         "e1",
		ProjectID:  "p1",
		FromNodeID: "n1",
		ToNodeID:   "n2",
		Relation:   "owns",
		Weight:     0.8,
		Attributes: map[string]any{"since": "chapter-3"},
		CreatedAt:  1700000000,
	}
	got := roundTrip(t, e).(KGEdge)
	assertEqual(t, "KGEdge", e, got)
}

func TestEntityLink_RoundTrip(t *testing.T) {
	l := EntityLink{
		ID:         "l1",
		ProjectID:  "p1",
		SourceType: "chapter",
		SourceID:   "ch1",
		TargetType: "character",
		TargetID:   "c1",
		LinkType:   "mention",
		CreatedAt:  1700000000,
	}
	got := roundTrip(t, l).(EntityLink)
	assertEqual(t, "EntityLink", l, got)
}
