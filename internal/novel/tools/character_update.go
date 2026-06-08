package tools

import (
	"context"
	"fmt"

	"reasonix/internal/novel/domain"
	"reasonix/internal/novel/project"
	"reasonix/internal/novel/repo"
)

type characterUpdateTool struct{}

func init() { registerDefault(&characterUpdateTool{}) }

func (t *characterUpdateTool) Name() string { return "character_update" }

func (t *characterUpdateTool) Description() string {
	return "按 id 更新角色的指定字段（fields 字典：name/description/voice_profile/content）。"
}

func (t *characterUpdateTool) Execute(ctx context.Context, input map[string]any, mgr *project.Manager) (map[string]any, error) {
	id, err := requiredString(input, "id")
	if err != nil {
		return nil, err
	}
	fields := nestedMapField(input, "fields")
	if fields == nil {
		return nil, fmt.Errorf("character_update: 'fields' is required")
	}

	r := repo.NewCharacterRepo(mgr.DB())
	c, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if v := stringField(fields, "name"); v != "" {
		c.Name = v
		c.Slug = repo.Slugify(v)
	}
	if _, ok := fields["description"]; ok {
		c.Description = stringField(fields, "description")
	}
	if v := stringField(fields, "content"); v != "" {
		c.Content = v
	}
	if vp := nestedMapField(fields, "voice_profile"); vp != nil {
		if c.VoiceProfile == nil {
			c.VoiceProfile = &domain.VoiceProfile{}
		}
		c.VoiceProfile.SpeechStyle = stringField(vp, "speech_style")
		c.VoiceProfile.ProfanityLevel = intField(vp, "profanity_level", c.VoiceProfile.ProfanityLevel)
		if xs := stringListField(vp, "address_terms"); xs != nil {
			c.VoiceProfile.AddressTerms = xs
		}
		if xs := stringListField(vp, "catchphrases"); xs != nil {
			c.VoiceProfile.Catchphrases = xs
		}
	}
	if err := r.Update(ctx, c); err != nil {
		return nil, err
	}
	return map[string]any{"character": characterToMap(c)}, nil
}
