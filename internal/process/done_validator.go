package process

import "strings"

// validateDone validates done stage structured output.
func validateDone(out *DoneOutput, raw string) []string {
	if out == nil {
		return []string{"missing done output"}
	}
	var errs []string
	if strings.TrimSpace(out.Summary) == "" {
		errs = append(errs, "done output missing required summary")
	}
	lower := strings.ToLower(raw)
	if strings.Contains(lower, "```") || strings.Contains(lower, "implement") || strings.Contains(lower, "edit file") {
		errs = append(errs, "done output must not contain mutation commands or implementation instructions")
	}
	return errs
}
