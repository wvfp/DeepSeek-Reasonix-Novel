package domain

import (
	"encoding/json"
	"reflect"
	"testing"
)

// roundTrip marshals v to JSON, unmarshals back into a fresh value, and
// returns it. The original value is included for richer error messages.
func roundTrip(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	ptr := reflect.New(reflect.TypeOf(v))
	if err := json.Unmarshal(raw, ptr.Interface()); err != nil {
		t.Fatalf("unmarshal: %v\njson: %s", err, raw)
	}
	return ptr.Elem().Interface()
}

// assertEqual uses reflect.DeepEqual to compare original and got. The
// "want" value is captured via closure so callers do not need to retype
// the seed literal.
func assertEqual(t *testing.T, name string, want, got any) {
	t.Helper()
	if !reflect.DeepEqual(want, got) {
		t.Errorf("%s: round-trip mismatch\n want: %#v\n  got: %#v", name, want, got)
	}
}

func TestWorld_RoundTrip(t *testing.T) {
	w := World{
		ID: "w1", ProjectID: "p1", Name: "九州", Slug: "jiu-zhou",
		Description: "九州大地", Content: "# 九州\n\n……",
		Metadata:  map[string]any{"tone": "epic", "year": 1200.0},
		CreatedAt: 1700000000, ModifiedAt: 1700000100,
	}
	got := roundTrip(t, w).(World)
	assertEqual(t, "World", w, got)
}

func TestWorldField_RoundTrip(t *testing.T) {
	f := WorldField{Key: "population", Value: "10亿", Note: "含修士"}
	got := roundTrip(t, f).(WorldField)
	assertEqual(t, "WorldField", f, got)
}

func TestWorldLink_RoundTrip(t *testing.T) {
	l := WorldLink{FromWorldID: "w1", ToWorldID: "w2", Relation: "adjacent_to", Note: "海路相通"}
	got := roundTrip(t, l).(WorldLink)
	assertEqual(t, "WorldLink", l, got)
}
