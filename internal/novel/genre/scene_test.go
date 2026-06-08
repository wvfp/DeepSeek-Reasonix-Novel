package genre

import (
	"strings"
	"testing"
)

func TestLoadScenes_Xianxia(t *testing.T) {
	ts, err := LoadScenes("xianxia")
	if err != nil {
		t.Fatalf("LoadScenes(xianxia): %v", err)
	}
	if len(ts) == 0 {
		t.Fatal("expected xianxia scenes, got none")
	}
	wantIDs := map[string]bool{"combat": true, "cultivation": true, "dialogue": true, "emotion": true}
	gotIDs := map[string]bool{}
	for _, s := range ts {
		gotIDs[s.ID] = true
		if s.DisplayName == "" {
			t.Errorf("scene %s has empty displayName", s.ID)
		}
		if s.SceneType == "" {
			t.Errorf("scene %s has empty sceneType", s.ID)
		}
		if len(s.Beats) == 0 {
			t.Errorf("scene %s has no beats", s.ID)
		}
	}
	for k := range wantIDs {
		if !gotIDs[k] {
			t.Errorf("missing expected scene id %q", k)
		}
	}
}

func TestLoadScenes_Urban(t *testing.T) {
	ts, err := LoadScenes("urban")
	if err != nil {
		t.Fatalf("LoadScenes(urban): %v", err)
	}
	if len(ts) == 0 {
		t.Fatal("expected urban scenes, got none")
	}
	wantIDs := map[string]bool{"workplace": true, "social": true, "romance": true}
	gotIDs := map[string]bool{}
	for _, s := range ts {
		gotIDs[s.ID] = true
	}
	for k := range wantIDs {
		if !gotIDs[k] {
			t.Errorf("missing expected scene id %q", k)
		}
	}
}

func TestLoadScenes_Horror(t *testing.T) {
	ts, err := LoadScenes("horror")
	if err != nil {
		t.Fatalf("LoadScenes(horror): %v", err)
	}
	if len(ts) == 0 {
		t.Fatal("expected horror scenes, got none")
	}
	wantIDs := map[string]bool{"horror": true, "mystery": true, "escape": true}
	gotIDs := map[string]bool{}
	for _, s := range ts {
		gotIDs[s.ID] = true
	}
	for k := range wantIDs {
		if !gotIDs[k] {
			t.Errorf("missing expected scene id %q", k)
		}
	}
}

func TestLoadScenes_UnknownGenre(t *testing.T) {
	ts, err := LoadScenes("nonexistent-genre")
	if err != nil {
		t.Fatalf("LoadScenes unknown: %v", err)
	}
	if ts != nil && len(ts) > 0 {
		t.Errorf("expected empty scenes for unknown genre, got %d", len(ts))
	}
}

func TestFormatSceneBlock(t *testing.T) {
	ts, err := LoadScenes("xianxia")
	if err != nil {
		t.Fatalf("LoadScenes: %v", err)
	}
	block := FormatSceneBlock(ts)
	if !strings.Contains(block, "[SCENE_TEMPLATES]") {
		t.Error("block missing [SCENE_TEMPLATES] marker")
	}
	if !strings.Contains(block, "[/SCENE_TEMPLATES]") {
		t.Error("block missing [/SCENE_TEMPLATES] marker")
	}
	if !strings.Contains(block, "战斗场景") {
		t.Error("block missing expected xianxia combat scene displayName")
	}
	if !strings.Contains(block, "beats:") {
		t.Error("block missing beats section")
	}
	if !strings.Contains(block, "hook:") {
		t.Error("block missing hook section")
	}
}

func TestFormatSceneBlock_Empty(t *testing.T) {
	if got := FormatSceneBlock(nil); got != "" {
		t.Errorf("FormatSceneBlock(nil) = %q, want empty", got)
	}
	if got := FormatSceneBlock([]SceneTemplate{}); got != "" {
		t.Errorf("FormatSceneBlock(empty) = %q, want empty", got)
	}
}

func TestLoadScenes_AllGenresParse(t *testing.T) {
	for _, id := range Available() {
		t.Run(id, func(t *testing.T) {
			ts, err := LoadScenes(id)
			if err != nil {
				t.Fatalf("LoadScenes(%s): %v", id, err)
			}
			// Not every genre has scenes yet; only xianxia, urban, horror do.
			if len(ts) > 0 {
				for _, s := range ts {
					if s.ID == "" {
						t.Error("scene has empty id")
					}
					if s.DisplayName == "" {
						t.Error("scene has empty displayName")
					}
				}
			}
		})
	}
}
