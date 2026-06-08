package tools

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/novel/domain"
	"reasonix/internal/novel/repo"
	"reasonix/internal/novel/style"
)

// TestRunVoiceDrift_NoDriftWhenCharacterUnmentioned: a character
// whose name never appears in the body contributes no findings,
// even if their VoiceProfile is rich.
func TestRunVoiceDrift_NoDriftWhenCharacterUnmentioned(t *testing.T) {
	c := &domain.Character{
		Name: "萧炎",
		VoiceProfile: &domain.VoiceProfile{
			AvgSentenceLen: 14,
			Catchphrases:   []string{"在下不才"},
			Bigrams:        []string{"哈哈", "呵呵"},
		},
	}
	body := "这是第一章，没有任何角色提及。纯粹的旁白。"
	if got := runVoiceDrift([]*domain.Character{c}, body); len(got) != 0 {
		t.Errorf("expected no drift, got %d findings", len(got))
	}
}

// TestRunVoiceDrift_SkipsMinimalProfile: a character with an
// empty VoiceProfile is the "first chapter" case. We don't flag
// drift because there is no baseline to compare against.
func TestRunVoiceDrift_SkipsMinimalProfile(t *testing.T) {
	c := &domain.Character{
		Name:         "萧炎",
		VoiceProfile: &domain.VoiceProfile{},
	}
	body := "萧炎说：「好的，明白了。」"
	if got := runVoiceDrift([]*domain.Character{c}, body); len(got) != 0 {
		t.Errorf("expected no drift (minimal profile), got %d findings", len(got))
	}
}

// TestRunVoiceDrift_FlagsSentenceLengthDrift: a character whose
// baseline average sentence length is 8 chars but who suddenly
// speaks in 30-char sentences should be flagged with at least
// one deviation.
func TestRunVoiceDrift_FlagsSentenceLengthDrift(t *testing.T) {
	c := &domain.Character{
		Name: "萧炎",
		VoiceProfile: &domain.VoiceProfile{
			AvgSentenceLen: 8,
			Catchphrases:   []string{"好"},
			Bigrams:        []string{"好的"},
		},
	}
	// Build a body with dialog in 30+ char sentences.
	var b strings.Builder
	b.WriteString("萧炎走上前去。\n\n")
	for i := 0; i < 10; i++ {
		b.WriteString("萧炎说：「这是我今天要告诉大家的非常非常重要的一段非常长的对白句子内容。」\n\n")
	}
	got := runVoiceDrift([]*domain.Character{c}, b.String())
	if len(got) != 1 {
		t.Fatalf("expected 1 drift finding, got %d", len(got))
	}
	if got[0].CharacterName != "萧炎" {
		t.Errorf("character_name = %q, want 萧炎", got[0].CharacterName)
	}
	hasSentence := false
	for _, d := range got[0].Deviations {
		if d.Kind == "sentence_length" {
			hasSentence = true
		}
	}
	if !hasSentence {
		t.Errorf("expected sentence_length deviation, got %+v", got[0].Deviations)
	}
}

// TestRunVoiceDrift_FlagsCatchphraseMissing: a character with a
// known catchphrase but whose dialog in this chapter does not
// use it should be flagged.
func TestRunVoiceDrift_FlagsCatchphraseMissing(t *testing.T) {
	c := &domain.Character{
		Name: "林动",
		VoiceProfile: &domain.VoiceProfile{
			AvgSentenceLen: 12,
			Catchphrases:   []string{"在下不才", "小的明白"},
			Bigrams:        []string{"明白", "小的"},
		},
	}
	body := "林动说：「我走上前去，这次绝对不能失败。」\n\n他继续走着。"
	got := runVoiceDrift([]*domain.Character{c}, body)
	if len(got) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(got))
	}
	hasCatch := false
	for _, d := range got[0].Deviations {
		if d.Kind == "catchphrase_missing" {
			hasCatch = true
		}
	}
	if !hasCatch {
		t.Errorf("expected catchphrase_missing deviation, got %+v", got[0].Deviations)
	}
}

// TestRunVoiceDrift_NoDeviationWhenConsistent: a character whose
// baseline is similar to this chapter's dialog should produce no
// findings. The dialog is calibrated against the profile so the
// sentence-length, catchphrase, and bigram checks all agree.
func TestRunVoiceDrift_NoDeviationWhenConsistent(t *testing.T) {
	// Build several short dialog spans so the sentence-length
	// stat is 4 chars on average, matching the persisted
	// AvgSentenceLen. Bigrams and catchphrases are picked from
	// the same vocabulary to keep Jaccard overlap high.
	var body strings.Builder
	body.WriteString("萧炎说：「")
	for i := 0; i < 20; i++ {
		// 4-char "sentences" split by 。 so the average
		// sentence length converges to 4.
		body.WriteString("好的哈哈。")
	}
	body.WriteString("」")
	c := &domain.Character{
		Name: "萧炎",
		VoiceProfile: &domain.VoiceProfile{
			AvgSentenceLen: 4,
			Catchphrases:   []string{"好的", "哈哈"},
			Bigrams:        []string{"好的", "哈哈"},
		},
	}
	if got := runVoiceDrift([]*domain.Character{c}, body.String()); len(got) != 0 {
		t.Errorf("expected no drift, got %+v", got)
	}
}

// TestRunVoiceDrift_IntegrationWithChapterReview: end-to-end
// that the chapter_review tool persists a "voice" row when
// drift is detected, and the row carries the character name in
// the description.
func TestRunVoiceDrift_IntegrationWithChapterReview(t *testing.T) {
	mgr := seedReviewProject(t)
	// Seed a character with a known VoiceProfile, then update
	// the chapter content to mention that character and
	// generate drift.
	ctx := context.Background()
	cr := repo.NewCharacterRepo(mgr.DB())
	p, _ := mgr.Project(ctx)
	c := &domain.Character{
		ProjectID: p.ID,
		Name:      "萧炎",
		VoiceProfile: &domain.VoiceProfile{
			AvgSentenceLen: 4,
			Catchphrases:   []string{"在下不才"},
			Bigrams:        []string{"在下", "不才"},
		},
	}
	if err := cr.Create(ctx, c); err != nil {
		t.Fatalf("seed character: %v", err)
	}
	// Long dialog that should trigger sentence_length drift.
	var long strings.Builder
	long.WriteString("萧炎走上前去。\n\n")
	for i := 0; i < 6; i++ {
		long.WriteString("萧炎说：「这是一段相当相当长的对白，已经远远超过角色惯用句长范围了。」\n\n")
	}
	if _, err := mgr.DB().ExecContext(ctx, `UPDATE chapters SET content = ?`,
		long.String()); err != nil {
		t.Fatalf("update chapter: %v", err)
	}
	llm := &scriptedLLM{reviewPayload: reviewFixture, reviewFixPayload: fixFixture}
	rt := NewChapterReviewTool(newReviewSwitcher(t, llm))
	out, err := rt.Execute(ctx, map[string]any{}, mgr)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	drift, _ := out["voice_drift"].([]VoiceDriftFinding)
	if len(drift) == 0 {
		t.Fatalf("expected voice_drift findings, got %+v", out["voice_drift"])
	}
	// And a "voice" dimension row should be persisted.
	rows, _ := repo.NewReviewRepo(mgr.DB()).ListByChapter(ctx, mustProjectID(t, mgr))
	_ = rows
	chs, _ := repo.NewChapterRepo(mgr.DB()).List(ctx, p.ID)
	all, _ := repo.NewReviewRepo(mgr.DB()).ListByChapter(ctx, chs[0].ID)
	hasVoice := false
	for _, r := range all {
		if r.Dimension == "voice" {
			hasVoice = true
			if len(r.Issues) == 0 {
				t.Errorf("voice row has 0 issues; expected drift issues")
			}
		}
	}
	if !hasVoice {
		t.Errorf("expected a 'voice' dimension review row, got %d rows", len(all))
	}
}

// TestExtractDialogForCharacter: basic smoke that the dialog
// extractor pulls quoted spans from a body.
func TestExtractDialogForCharacter(t *testing.T) {
	c := &domain.Character{Name: "萧炎"}
	body := `萧炎说：「好的。」他继续走。旁白里讲了一些事。`
	d := extractDialogForCharacter(c, body)
	if d == "" {
		t.Fatal("expected non-empty dialog extraction")
	}
	if !strings.Contains(d, "好的") {
		t.Errorf("dialog missing quoted content: %q", d)
	}
}

// TestRunVoiceDrift_NoFindingsForEmptyCharList: a chapter with
// no characters should produce no findings.
func TestRunVoiceDrift_NoFindingsForEmptyCharList(t *testing.T) {
	body := "萧炎说：「好的。」"
	if got := runVoiceDrift(nil, body); len(got) != 0 {
		t.Errorf("expected no findings, got %+v", got)
	}
	if got := runVoiceDrift([]*domain.Character{}, body); len(got) != 0 {
		t.Errorf("expected no findings, got %+v", got)
	}
}

// Sanity test that the style package's VoiceDeviation is the
// shape runVoiceDrift returns. This guards against accidental
// refactors of style.VoiceDeviation that would silently break
// the editor UI.
func TestRunVoiceDrift_ReturnsTypedDeviations(t *testing.T) {
	c := &domain.Character{
		Name: "萧炎",
		VoiceProfile: &domain.VoiceProfile{
			AvgSentenceLen: 4,
			Catchphrases:   []string{"在下不才"},
			Bigrams:        []string{"在下", "不才"},
		},
	}
	body := strings.Repeat("萧炎说：「这段对白特别特别特别特别特别长，明显超过角色惯用句长。」\n\n", 8)
	got := runVoiceDrift([]*domain.Character{c}, body)
	if len(got) == 0 {
		t.Fatal("expected drift")
	}
	for _, d := range got {
		for _, dv := range d.Deviations {
			_ = style.CompareVoice  // ensure the import is reachable
			if dv.Kind == "" {
				t.Error("deviation has empty kind")
			}
			if dv.Severity == "" {
				t.Error("deviation has empty severity")
			}
		}
	}
}
