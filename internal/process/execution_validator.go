package process

import "strings"

// validateExecution validates execution stage structured output.
func validateExecution(out *ExecutionOutput) []string {
	if out == nil {
		return []string{"missing execution output"}
	}
	var errs []string
	for _, v := range out.Verification {
		lower := strings.ToLower(v)
		if (strings.Contains(lower, "test") || strings.Contains(lower, "passed")) && !strings.Contains(lower, "not run") && !strings.Contains(lower, "tool") {
			errs = append(errs, "verification claim requires tool evidence")
		}
	}
	if out.NextSignal == "ready_for_validation" {
		if len(out.Blockers) > 0 {
			errs = append(errs, "ready_for_validation is blocked by active blockers")
		}
	}
	return errs
}
