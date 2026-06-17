package validation

import "html"

func EscapeUntrusted(text string) string {
	return html.EscapeString(text)
}
