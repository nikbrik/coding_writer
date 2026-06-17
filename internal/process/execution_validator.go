package process

import "strings"

// validateExecution validates execution stage structured output.
func validateExecution(out *ExecutionOutput) []string {
	if out == nil {
		return []string{"missing execution output"}
	}
	var errs []string
	if strings.TrimSpace(out.Summary) == "" || strings.TrimSpace(out.NextSignal) == "" {
		errs = append(errs, "execution output missing required summary/next_signal")
	}
	for _, v := range out.Verification {
		lower := strings.ToLower(v)
		if (strings.Contains(lower, "test") || strings.Contains(lower, "passed")) && !strings.Contains(lower, "not run") && !strings.Contains(lower, "tool evidence") {
			errs = append(errs, "verification claim requires tool evidence")
		}
	}
	switch out.NextSignal {
	case "continue_execution", "planning_required", "ready_for_validation":
		// allowed
	default:
		errs = append(errs, "unknown execution next_signal")
	}
	if out.NextSignal == "ready_for_validation" {
		if len(out.Blockers) > 0 {
			errs = append(errs, "ready_for_validation is blocked by active blockers")
		}
	}
	return errs
}
