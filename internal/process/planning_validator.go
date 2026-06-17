package process

import "strings"

// validatePlanning validates planning stage structured output.
func validatePlanning(out *PlanningOutput, raw string) []string {
	if out == nil {
		return []string{"missing planning output"}
	}
	var errs []string
	if strings.TrimSpace(out.Summary) == "" || strings.TrimSpace(out.Readiness) == "" {
		errs = append(errs, "planning output missing required summary/readiness")
	}
	rawLower := strings.ToLower(raw + " " + strings.Join(out.Assumptions, " ") + " " + strings.Join(out.AcceptanceCriteria, " ") + " " + strings.Join(out.Plan, " ") + " " + out.Summary)
	if strings.Contains(rawLower, "implemented") || strings.Contains(rawLower, "changed file") || strings.Contains(rawLower, "test passed") || strings.Contains(rawLower, "```") || strings.Contains(rawLower, "diff --git") || strings.Contains(rawLower, "+++ b/") || strings.Contains(rawLower, "--- a/") || strings.Contains(rawLower, "@@") {
		errs = append(errs, "planning output must not claim implementation")
	}
	switch out.Readiness {
	case "needs_user_input", "ready_for_execution_proposal":
		// allowed
	default:
		errs = append(errs, "unknown planning readiness")
	}
	if out.Readiness == "ready_for_execution_proposal" {
		if len(out.AcceptanceCriteria) == 0 || len(out.Plan) == 0 {
			errs = append(errs, "ready_for_execution_proposal requires non-empty plan and acceptance criteria")
		}
		if len(out.OpenQuestions) > 0 {
			errs = append(errs, "open questions block readiness")
		}
	}
	return errs
}
