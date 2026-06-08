package domain

import "testing"

func TestChapter_RoundTrip(t *testing.T) {
	c := Chapter{
		ID:            "ch1",
		ProjectID:     "p1",
		ArcID:         "arc-1",
		Volume:        1,
		ChapterNumber: 1,
		Title:         "陨落的天才",
		Slug:          "fallen-genius",
		Content:       "正文……",
		WordCount:     3500,
		Status:        "draft",
		CreatedAt:     1700000000,
		ModifiedAt:    1700000100,
	}
	got := roundTrip(t, c).(Chapter)
	assertEqual(t, "Chapter", c, got)
}

func TestChapterFact_RoundTrip(t *testing.T) {
	f := ChapterFact{
		ID:         "f1",
		ProjectID:  "p1",
		ChapterID:  "ch1",
		FactType:   FactTypeEvent,
		Subject:    "萧炎",
		Predicate:  "突破到",
		Object:     "斗者",
		Confidence: 0.92,
		Context:    "萧炎在丹药的帮助下终于突破到了斗者。",
		CreatedAt:  1700000000,
	}
	got := roundTrip(t, f).(ChapterFact)
	assertEqual(t, "ChapterFact", f, got)
}

func TestFactTypeConstants(t *testing.T) {
	// Sanity: there are exactly nine fact types and they are all
	// distinct. The novel-plugin reviewer and the chapter-write tool
	// both switch on these strings; a regression would silently drop
	// facts.
	want := []string{
		"event", "state_change", "revelation", "location", "time",
		"relation", "action", "dialogue", "item",
	}
	got := []string{
		FactTypeEvent, FactTypeStateChange, FactTypeRevelation,
		FactTypeLocation, FactTypeTime, FactTypeRelation,
		FactTypeAction, FactTypeDialogue, FactTypeItem,
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("FactType[%d] = %q, want %q", i, got[i], v)
		}
	}
}
