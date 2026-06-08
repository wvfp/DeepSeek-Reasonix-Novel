package domain

// Character is a novel character. Content is the Markdown body written to
// .novel-weaver/content/settings/char-{slug}.md; VoiceProfile is the
// per-character speech fingerprint used to keep dialogue consistent
// across chapters.
type Character struct {
	ID           string        `json:"id"`
	ProjectID    string        `json:"project_id"`
	Name         string        `json:"name"`
	Slug         string        `json:"slug"`
	Description  string        `json:"description,omitempty"`
	VoiceProfile *VoiceProfile `json:"voice_profile,omitempty"`
	Content      string        `json:"content,omitempty"`
	CreatedAt    int64         `json:"created_at"`
	ModifiedAt   int64         `json:"modified_at"`
}

// CharacterState is a per-chapter snapshot of a character: where they
// are, what they carry, who they are allied with, what they know. Stored
// as JSON inside the snapshot column of character_states; the in-Go
// shape is used by tools that need to inspect or diff states.
type CharacterState struct {
	Location      string            `json:"location,omitempty"`
	Power         string            `json:"power,omitempty"`
	Items         []string          `json:"items,omitempty"`
	Relationships map[string]string `json:"relationships,omitempty"`
	Mood          string            `json:"mood,omitempty"`
	Knowledge     []string          `json:"knowledge,omitempty"`
	Tags          []string          `json:"tags,omitempty"`
}

// VoiceProfile is the speech fingerprint of a character. It is derived
// automatically from the first 3-5 chapters that mention the character
// and used to flag drift when later chapters are written.
type VoiceProfile struct {
	// AddressTerms lists the pronouns / titles the character uses for
	// others (他/她/师父/掌门/师兄/晚辈 …).
	AddressTerms []string `json:"address_terms,omitempty"`
	// SpeechStyle is the high-level style tag: "formal", "casual",
	// "poetic", "crude", "laconic", …
	SpeechStyle string `json:"speech_style,omitempty"`
	// Bigrams are the top 20 two-character / two-word pairs the
	// character uses most often.
	Bigrams []string `json:"bigrams,omitempty"`
	// AvgSentenceLen is the mean sentence length in characters.
	AvgSentenceLen float64 `json:"avg_sentence_len,omitempty"`
	// ProfanityLevel is 0 (none) to 3 (heavy).
	ProfanityLevel int `json:"profanity_level,omitempty"`
	// Catchphrases are recurring tag-lines ("在下不才", "小的明白", …).
	Catchphrases []string `json:"catchphrases,omitempty"`
}
