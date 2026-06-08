package domain

import "testing"

func TestAlias_RoundTrip(t *testing.T) {
	a := Alias{
		ID:         "al1",
		ProjectID:  "p1",
		EntityType: EntityCharacter,
		EntityID:   "c1",
		Alias:      "萧火火",
		CreatedAt:  1700000000,
	}
	got := roundTrip(t, a).(Alias)
	assertEqual(t, "Alias", a, got)
}

func TestEntityTypeConstants(t *testing.T) {
	want := []string{"world", "character", "item", "location", "faction", "event"}
	got := []string{
		EntityWorld, EntityCharacter, EntityItem,
		EntityLocation, EntityFaction, EntityEvent,
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("EntityType[%d] = %q, want %q", i, got[i], v)
		}
	}
}
