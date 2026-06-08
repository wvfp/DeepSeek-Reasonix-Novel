package domain

import "testing"

func TestCharacter_RoundTrip(t *testing.T) {
	c := Character{
		ID:          "c1",
		ProjectID:   "p1",
		Name:        "萧炎",
		Slug:        "xiao-yan",
		Description: "天才少年",
		VoiceProfile: &VoiceProfile{
			AddressTerms:   []string{"萧兄", "老师"},
			SpeechStyle:    "casual",
			Bigrams:        []string{"萧炎", "炎儿"},
			AvgSentenceLen: 14.5,
			ProfanityLevel: 1,
			Catchphrases:   []string{"三十年河东", "莫欺少年穷"},
		},
		Content:    "# 萧炎\n\n……",
		CreatedAt:  1700000000,
		ModifiedAt: 1700000100,
	}
	got := roundTrip(t, c).(Character)
	assertEqual(t, "Character", c, got)
}

func TestCharacter_NilVoiceProfile_RoundTrip(t *testing.T) {
	c := Character{ID: "c2", ProjectID: "p1", Name: "林动", Slug: "lin-dong"}
	got := roundTrip(t, c).(Character)
	assertEqual(t, "Character(nil profile)", c, got)
}

func TestCharacterState_RoundTrip(t *testing.T) {
	s := CharacterState{
		Location:      "青云山",
		Power:         "筑基后期",
		Items:         []string{"玄铁剑", "回气丹"},
		Relationships: map[string]string{"道侣": "灵儿"},
		Mood:          "警觉",
		Knowledge:     []string{"青云剑诀", "天音洞位置"},
		Tags:          []string{"主角", "in-arc:ten-thousand-years"},
	}
	got := roundTrip(t, s).(CharacterState)
	assertEqual(t, "CharacterState", s, got)
}

func TestVoiceProfile_RoundTrip(t *testing.T) {
	v := VoiceProfile{
		AddressTerms:   []string{"我", "本座"},
		SpeechStyle:    "formal",
		Bigrams:        []string{"之乎者也"},
		AvgSentenceLen: 22.0,
		ProfanityLevel: 0,
		Catchphrases:   []string{"嗯"},
	}
	got := roundTrip(t, v).(VoiceProfile)
	assertEqual(t, "VoiceProfile", v, got)
}
