package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"reasonix/internal/novel/domain"
	"reasonix/internal/novel/project"
	"reasonix/internal/novel/repo"
)

type characterCreateTool struct{}

func init() { registerDefault(&characterCreateTool{}) }

func (t *characterCreateTool) Name() string { return "character_create" }

func (t *characterCreateTool) Description() string {
	return "创建角色，并写入 char-{slug}.md 文件。"
}

func (t *characterCreateTool) Execute(ctx context.Context, input map[string]any, mgr *project.Manager) (map[string]any, error) {
	p, err := mgr.Project(ctx)
	if err != nil {
		return nil, err
	}
	name, err := requiredString(input, "name")
	if err != nil {
		return nil, err
	}
	c := &domain.Character{
		ProjectID:   p.ID,
		Name:        name,
		Description: stringField(input, "description"),
	}
	if vp := nestedMapField(input, "voice_profile"); vp != nil {
		c.VoiceProfile = &domain.VoiceProfile{
			SpeechStyle:    stringField(vp, "speech_style"),
			ProfanityLevel: intField(vp, "profanity_level", 0),
		}
		if xs := stringListField(vp, "address_terms"); xs != nil {
			c.VoiceProfile.AddressTerms = xs
		}
		if xs := stringListField(vp, "bigrams"); xs != nil {
			c.VoiceProfile.Bigrams = xs
		}
		if xs := stringListField(vp, "catchphrases"); xs != nil {
			c.VoiceProfile.Catchphrases = xs
		}
	}
	if err := repo.NewCharacterRepo(mgr.DB()).Create(ctx, c); err != nil {
		return nil, err
	}
	path, writeErr := writeCharacterMarkdown(mgr, c)
	out := map[string]any{"character": characterToMap(c), "path": path}
	if writeErr != nil {
		out["writeWarn"] = writeErr.Error()
	}
	return out, nil
}

func writeCharacterMarkdown(mgr *project.Manager, c *domain.Character) (string, error) {
	dir := filepath.Join(mgr.Root(), "content", "settings")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "char-"+c.Slug+".md")
	vp, _ := json.Marshal(c.VoiceProfile)
	body := fmt.Sprintf("---\ntitle: %s\nslug: %s\nstatus: draft\ncreated_at: %d\nmodified_at: %d\nvoice_profile: %s\n---\n\n# %s\n\n%s\n",
		c.Name, c.Slug, c.CreatedAt, c.ModifiedAt, string(vp), c.Name, c.Description)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return path, err
	}
	return path, nil
}

func characterToMap(c *domain.Character) map[string]any {
	out := map[string]any{
		"id":          c.ID,
		"project_id":  c.ProjectID,
		"name":        c.Name,
		"slug":        c.Slug,
		"description": c.Description,
		"content":     c.Content,
		"created_at":  c.CreatedAt,
		"modified_at": c.ModifiedAt,
	}
	if c.VoiceProfile != nil {
		out["voice_profile"] = c.VoiceProfile
	}
	return out
}
