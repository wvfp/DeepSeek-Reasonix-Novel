package style

import (
	"strings"
	"testing"

	"reasonix/internal/novel/domain"
)

func TestExtractVoice_Basic(t *testing.T) {
	dialog := "「在下林动，久仰萧兄。」林动拱手。「萧兄这厢有礼。」林动又说。萧炎冷冷道：「不必多礼。」"
	prof := ExtractVoice(dialog)
	if prof.AvgSentenceLen <= 0 {
		t.Errorf("avg sentence len = %f, want > 0", prof.AvgSentenceLen)
	}
	if len(prof.Bigrams) == 0 {
		t.Error("bigrams is empty")
	}
	if len(prof.Catchphrases) != 3 {
		t.Errorf("catchphrases = %d, want 3", len(prof.Catchphrases))
	}
}

func TestCompareVoice_NoDrift(t *testing.T) {
	persisted := domain.VoiceProfile{
		AvgSentenceLen: 12,
		Bigrams:        []string{"萧兄", "在下", "拱手", "不才"},
		Catchphrases:   []string{"萧兄", "在下", "拱手"},
	}
	fresh := persisted
	dialog := "「萧兄请。」「在下明白。」「拱手。」"
	devs := CompareVoice(fresh, persisted, dialog)
	if len(devs) != 0 {
		t.Errorf("expected 0 deviations, got %+v", devs)
	}
}

func TestCompareVoice_SentenceLengthDrift(t *testing.T) {
	persisted := domain.VoiceProfile{AvgSentenceLen: 8}
	fresh := domain.VoiceProfile{AvgSentenceLen: 20}
	devs := CompareVoice(fresh, persisted, "x")
	if len(devs) == 0 {
		t.Fatal("expected sentence-length deviation")
	}
	if devs[0].Kind != "sentence_length" {
		t.Errorf("first deviation kind = %q, want sentence_length", devs[0].Kind)
	}
}

func TestCompareVoice_CatchphraseMissing(t *testing.T) {
	persisted := domain.VoiceProfile{
		AvgSentenceLen: 12,
		Catchphrases:   []string{"萧兄", "在下"},
	}
	fresh := domain.VoiceProfile{AvgSentenceLen: 12}
	dialog := "他站到窗前，望向远山。"
	devs := CompareVoice(fresh, persisted, dialog)
	found := false
	for _, d := range devs {
		if d.Kind == "catchphrase_missing" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected catchphrase_missing, got %+v", devs)
	}
}

func TestCompareVoice_ProfanityEscalation(t *testing.T) {
	persisted := domain.VoiceProfile{AvgSentenceLen: 10, ProfanityLevel: 0}
	fresh := domain.VoiceProfile{AvgSentenceLen: 10, ProfanityLevel: 3}
	devs := CompareVoice(fresh, persisted, "x")
	found := false
	for _, d := range devs {
		if d.Kind == "profanity" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected profanity deviation, got %+v", devs)
	}
}

func TestCompareVoice_BigramDrift(t *testing.T) {
	persisted := domain.VoiceProfile{
		AvgSentenceLen: 10,
		Bigrams:        []string{"师父", "弟子", "明白", "这就", "不敢", "遵命"},
	}
	fresh := domain.VoiceProfile{
		AvgSentenceLen: 10,
		Bigrams:        []string{"haha", "lol", "ok", "yes", "cool", "wow"},
	}
	devs := CompareVoice(fresh, persisted, "haha lol ok yes")
	found := false
	for _, d := range devs {
		if d.Kind == "bigram_drift" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected bigram_drift, got %+v", devs)
	}
}

func TestIsMinimal(t *testing.T) {
	if !IsMinimal(domain.VoiceProfile{}) {
		t.Error("empty profile should be minimal")
	}
	if IsMinimal(domain.VoiceProfile{AvgSentenceLen: 10}) {
		t.Error("non-empty profile should not be minimal")
	}
}

func TestEncodeDecodeProfile(t *testing.T) {
	prof := domain.VoiceProfile{
		SpeechStyle:    "laconic",
		Bigrams:        []string{"嗯", "好", "是"},
		AvgSentenceLen: 4.5,
	}
	raw, err := EncodeProfile(prof)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "laconic") {
		t.Errorf("encoded profile missing speech_style: %s", raw)
	}
	back, err := DecodeProfile(raw)
	if err != nil {
		t.Fatal(err)
	}
	if back.SpeechStyle != "laconic" {
		t.Errorf("decoded speech_style = %q, want laconic", back.SpeechStyle)
	}
	// Empty round-trip.
	empty, err := DecodeProfile("")
	if err != nil {
		t.Fatal(err)
	}
	if empty.SpeechStyle != "" {
		t.Errorf("empty round-trip leaked value: %+v", empty)
	}
}

func TestProfanityLevel(t *testing.T) {
	cases := []struct {
		text string
		want int
	}{
		{"安静的一章", 0},
		{"他妈的，别过来", 1},
		{"他妈的，操你，去死", 3},
	}
	for _, c := range cases {
		if got := profanityLevel(c.text); got != c.want {
			t.Errorf("profanityLevel(%q) = %d, want %d", c.text, got, c.want)
		}
	}
}
