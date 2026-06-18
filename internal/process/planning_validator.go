package process

import "strings"

// validatePlanning validates planning stage structured output.
func validatePlanning(out *PlanningOutput, raw string) []string {
	if out == nil {
		return []string{"missing planning output"}
	}
	errs := validatePlanningStructural(out)
	combined := raw + " " + strings.Join(out.Assumptions, " ") + " " + strings.Join(out.AcceptanceCriteria, " ") + " " + strings.Join(out.Plan, " ") + " " + out.Summary
	if containsImplementationClaim(combined) || containsTestPassClaim(combined) {
		errs = append(errs, "planning output must not claim implementation")
	}
	return errs
}

func validatePlanningStructural(out *PlanningOutput) []string {
	if out == nil {
		return []string{"missing planning output"}
	}
	var errs []string
	if strings.TrimSpace(out.Summary) == "" || strings.TrimSpace(out.Readiness) == "" {
		errs = append(errs, "planning output missing required summary/readiness")
	}
	switch out.Readiness {
	case "needs_user_input", "ready_for_execution_proposal":
		// allowed
	default:
		errs = append(errs, "unknown planning readiness")
	}
	if out.Readiness == "ready_for_execution_proposal" {
		if !hasNonEmpty(out.AcceptanceCriteria) || !hasNonEmpty(out.Plan) {
			errs = append(errs, "ready_for_execution_proposal requires non-empty plan and acceptance criteria")
		}
		if hasNonEmpty(out.OpenQuestions) {
			errs = append(errs, "open questions block readiness")
		}
	}
	return errs
}
