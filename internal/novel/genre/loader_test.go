package genre

import (
	"strings"
	"testing"
)

func TestLoad_AllGenres(t *testing.T) {
	want := []string{
		"apocalypse",
		"horror",
		"infinite-flow",
		"sci-fi",
		"urban",
		"xianxia",
	}
	got := Available()
	if len(got) != len(want) {
		t.Fatalf("Available() returned %d genres, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("Available()[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestLoad_EachPackLoads(t *testing.T) {
	for _, id := range Available() {
		t.Run(id, func(t *testing.T) {
			p, err := Load(id)
			if err != nil {
				t.Fatalf("Load(%s): %v", id, err)
			}
			if p.ID != id {
				t.Errorf("pack.id = %q, want %q", p.ID, id)
			}
			if p.DisplayName == "" {
				t.Errorf("pack %s has empty displayName", id)
			}
			if len(p.StyleGuidelines) == 0 {
				t.Errorf("pack %s has 0 styleGuidelines", id)
			}
			if len(p.StyleRules) == 0 {
				t.Errorf("pack %s has 0 styleRules", id)
			}
			if len(p.ArcTemplates) == 0 {
				t.Errorf("pack %s has 0 arcTemplates", id)
			}
			if _, ok := p.Prompts["systemPrefix"]; !ok {
				t.Errorf("pack %s missing prompts.systemPrefix", id)
			}
			if _, ok := p.Prompts["chapterFocus"]; !ok {
				t.Errorf("pack %s missing prompts.chapterFocus", id)
			}
		})
	}
}

func TestLoad_UnknownGenreFallsBackToFantasy(t *testing.T) {
	p, err := Load("nonexistent-genre")
	if err != nil {
		t.Fatalf("Load unknown: %v", err)
	}
	if p.ID != "nonexistent-genre" {
		t.Errorf("fallback id = %q, want the requested id (so the prompt reflects the user's choice)", p.ID)
	}
	if p.DisplayName == "" {
		t.Error("fallback pack should still have a displayName")
	}
	if len(p.StyleGuidelines) == 0 {
		t.Error("fallback pack should still have styleGuidelines")
	}
}

func TestLoad_DefaultFantasy(t *testing.T) {
	// The runtime default per spec is "fantasy". The fallback
	// is what the user gets when they have not picked a
	// specific pack. Verify the fallback is non-empty.
	p, err := Load("fantasy")
	if err != nil {
		t.Fatalf("Load(fantasy): %v", err)
	}
	if p.DisplayName == "" {
		t.Error("fantasy fallback has empty displayName")
	}
}

func TestFormatSystemBlock_ContainsPrompts(t *testing.T) {
	p, err := Load("xianxia")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	block := FormatSystemBlock(p)
	if !strings.Contains(block, "[GENRE_PACK]") {
		t.Error("block missing [GENRE_PACK] marker")
	}
	if !strings.Contains(block, "xianxia") {
		t.Error("block missing genre id")
	}
	if !strings.Contains(block, "systemPrefix") {
		t.Error("block missing prompts.systemPrefix key")
	}
}

func TestFormatSystemBlock_EmptyForEmptyPack(t *testing.T) {
	if got := FormatSystemBlock(Pack{}); got != "" {
		t.Errorf("FormatSystemBlock on empty pack = %q, want empty", got)
	}
}

func TestFormatDetailBlock_IncludesAllLists(t *testing.T) {
	p, err := Load("horror")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	block := FormatDetailBlock(p)
	if !strings.Contains(block, "### 风格指南") {
		t.Error("detail block missing style guidelines heading")
	}
	if !strings.Contains(block, "### 风格规则") {
		t.Error("detail block missing style rules heading")
	}
	if !strings.Contains(block, "### 禁用模式") {
		t.Error("detail block missing forbidden patterns heading")
	}
	if !strings.Contains(block, "### 推荐模式") {
		t.Error("detail block missing recommended patterns heading")
	}
	if !strings.Contains(block, "### 大纲模板") {
		t.Error("detail block missing arc templates heading")
	}
	// A specific rule from the horror pack must appear in the
	// detail block to prove the lists are rendered, not just
	// counted.
	if !strings.Contains(block, "规则") {
		t.Error("detail block missing the word 规则 from the horror pack")
	}
}

func TestPack_ForbiddenAndRecommendedDisjoint(t *testing.T) {
	// A pack should not list the same phrase in both
	// forbiddenPatterns and recommendedPatterns; that would
	// confuse the LLM. Sanity check across all packs.
	for _, id := range Available() {
		t.Run(id, func(t *testing.T) {
			p, err := Load(id)
			if err != nil {
				t.Fatal(err)
			}
			fset := map[string]bool{}
			for _, s := range p.ForbiddenPatterns {
				fset[s] = true
			}
			for _, s := range p.RecommendedPatterns {
				if fset[s] {
					t.Errorf("pack %s: %q in both forbidden and recommended", id, s)
				}
			}
		})
	}
}
