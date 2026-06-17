package profiles

import (
	"fmt"
	"sort"
	"strings"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/validation"
)

func Render(profile app.UserProfile) (string, error) {
	var b strings.Builder
	b.WriteString("Profile: ")
	b.WriteString(profile.ID)
	b.WriteString("\nUser preferences:")
	styleKeys := sortedKeys(profile.Style)
	for _, k := range styleKeys {
		b.WriteString(fmt.Sprintf(" %s=%s", k, profile.Style[k]))
	}
	formatKeys := sortedKeys(profile.ResponseFormat)
	for _, k := range formatKeys {
		b.WriteString(fmt.Sprintf(" %s=%s", k, profile.ResponseFormat[k]))
	}
	if len(profile.Constraints) > 0 {
		b.WriteString(" constraints: ")
		b.WriteString(strings.Join(profile.Constraints, ", "))
	}
	return `<context_block id="profile.active" type="profile" source="storage" trust="untrusted" priority="high">` + "\n" + validation.EscapeUntrusted(b.String()) + "\n</context_block>", nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
