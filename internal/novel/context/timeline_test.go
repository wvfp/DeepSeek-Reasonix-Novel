package context

import (
	"testing"

	"reasonix/internal/novel/domain"
)

func TestExtractFromChapter(t *testing.T) {
	m := NewTimelineManager()
	chapter := &domain.Chapter{ID: "ch-1"}
	facts := []domain.ChapterFact{
		{Subject: "张三", Predicate: "突破", Object: "筑基期", Context: "在洞府中闭关"},
		{Subject: "张三", Predicate: "受伤", Object: "左臂", Context: "与敌人交战"},
		{Subject: "李四", Predicate: "出场", Object: "市集", Context: "首次亮相"},
	}

	m.ExtractFromChapter(chapter, facts)

	zhang := m.GetTimeline("张三")
	if len(zhang.Events) != 2 {
		t.Fatalf("expected 2 events for 张三, got %d", len(zhang.Events))
	}
	if zhang.Events[0].EventType != EventTypePowerUp {
		t.Errorf("expected first event type %q, got %q", EventTypePowerUp, zhang.Events[0].EventType)
	}
	if zhang.Events[1].EventType != EventTypeInjury {
		t.Errorf("expected second event type %q, got %q", EventTypeInjury, zhang.Events[1].EventType)
	}

	li := m.GetTimeline("李四")
	if len(li.Events) != 1 {
		t.Fatalf("expected 1 event for 李四, got %d", len(li.Events))
	}
	if li.Events[0].EventType != EventTypeAppearance {
		t.Errorf("expected event type %q, got %q", EventTypeAppearance, li.Events[0].EventType)
	}
}

func TestExtractFromChapter_NilChapter(t *testing.T) {
	m := NewTimelineManager()
	m.ExtractFromChapter(nil, []domain.ChapterFact{
		{Subject: "张三", Predicate: "突破", Object: "筑基期"},
	})
	if len(m.timelines) != 0 {
		t.Fatalf("expected no timelines when chapter is nil, got %d", len(m.timelines))
	}
}

func TestGetTimeline(t *testing.T) {
	m := NewTimelineManager()
	tm := m.GetTimeline("unknown")
	if tm.CharacterID != "unknown" {
		t.Errorf("expected character_id 'unknown', got %q", tm.CharacterID)
	}
	if len(tm.Events) != 0 {
		t.Errorf("expected 0 events for unknown character, got %d", len(tm.Events))
	}
}

func TestGetKeyEvents(t *testing.T) {
	m := NewTimelineManager()
	chapter := &domain.Chapter{ID: "ch-1"}
	facts := []domain.ChapterFact{
		{Subject: "张三", Predicate: "突破", Object: "筑基期"},
		{Subject: "张三", Predicate: "出场", Object: "市集"},
		{Subject: "张三", Predicate: "结为", Object: "盟友"},
	}
	m.ExtractFromChapter(chapter, facts)

	key := m.GetKeyEvents("张三", 3)
	if len(key) != 2 {
		t.Fatalf("expected 2 key events (importance >= 3), got %d", len(key))
	}
	for _, e := range key {
		if e.Importance < 3 {
			t.Errorf("event %q importance %d < 3", e.Description, e.Importance)
		}
	}
}

func TestCompressTimeline(t *testing.T) {
	m := NewTimelineManager()
	chapter := &domain.Chapter{ID: "ch-1"}
	facts := []domain.ChapterFact{
		{Subject: "张三", Predicate: "突破", Object: "筑基期"},
		{Subject: "张三", Predicate: "出场", Object: "市集"},
		{Subject: "张三", Predicate: "受伤", Object: "左臂"},
	}
	m.ExtractFromChapter(chapter, facts)

	compressed := m.CompressTimeline("张三")
	if len(compressed.Events) != 2 {
		t.Fatalf("expected 2 compressed events, got %d", len(compressed.Events))
	}
	for _, e := range compressed.Events {
		if e.Importance < 3 {
			t.Errorf("compressed event %q importance %d < 3", e.Description, e.Importance)
		}
	}
}

func TestGetKeyEvents_Bounds(t *testing.T) {
	m := NewTimelineManager()
	chapter := &domain.Chapter{ID: "ch-1"}
	facts := []domain.ChapterFact{
		{Subject: "张三", Predicate: "出场", Object: "市集"},
	}
	m.ExtractFromChapter(chapter, facts)

	// minImportance below 1 should be clamped.
	all := m.GetKeyEvents("张三", 0)
	if len(all) != 1 {
		t.Fatalf("expected 1 event after clamping, got %d", len(all))
	}

	// minImportance above 5 should be clamped.
	none := m.GetKeyEvents("张三", 6)
	if len(none) != 0 {
		t.Fatalf("expected 0 events after clamping, got %d", len(none))
	}
}
