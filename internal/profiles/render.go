package profiles

import (
	"encoding/json"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/validation"
)

func Render(profile app.UserProfile) (string, error) {
	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return "", err
	}
	return `<context_block id="profile.active" type="profile" source="storage" trust="trusted_preference">` + "\n" + validation.EscapeUntrusted(string(data)) + "\n</context_block>", nil
}
