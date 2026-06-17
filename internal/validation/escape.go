package validation

import "strings"

func EscapeUntrusted(text string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"'", "&#39;",
	)
	return replacer.Replace(text)
}
