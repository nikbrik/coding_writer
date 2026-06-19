package process

import "strings"

// validateExecution validates execution stage structured output.
func validateExecution(out *ExecutionOutput, trustedEvidence ...string) []string {
	if out == nil {
		return []string{"missing execution output"}
	}
	errs := validateExecutionStructural(out, trustedEvidence...)
	if out.NextSignal == "ready_for_validation" {
		if hasNonEmpty(out.Blockers) {
			errs = append(errs, "ready_for_validation is blocked by active blockers")
		}
		if !hasNonEmpty(out.ChangedArtifacts) || !hasNonEmpty(out.Verification) {
			errs = append(errs, "ready_for_validation requires changed artifacts and verification evidence")
		}
	}
	progressClaims := []string{out.CurrentStep, out.NextStep}
	progressClaims = append(progressClaims, out.CompletedSteps...)
	combinedParts := append([]string{out.Summary}, progressClaims...)
	combinedParts = append(combinedParts, out.ChangedArtifacts...)
	combinedParts = append(combinedParts, out.Verification...)
	combined := strings.Join(combinedParts, " ")
	if containsTestPassClaim(combined) && !hasTrustedEvidence(trustedEvidence) {
		errs = append(errs, "test/tool claims require trusted application evidence")
	}
	if containsSideEffectClaim(combined) && !hasTrustedEvidence(trustedEvidence) {
		errs = append(errs, "side-effect claims require trusted application evidence")
	}
	for _, claim := range progressClaims {
		if containsProgressToolClaim(claim) && !hasTrustedEvidence(trustedEvidence) {
			errs = append(errs, "execution progress claims require trusted application evidence")
		}
	}
	for _, v := range out.Verification {
		lower := strings.ToLower(v)
		claimsRun := strings.Contains(lower, "test") || strings.Contains(lower, "passed")
		if claimsRun && isExplicitNotRun(v) && (strings.Contains(lower, "passed") || strings.Contains(lower, "pass")) {
			errs = append(errs, "verification must not mix passed and not-run claims")
			continue
		}
		if claimsRun && !isExplicitNotRun(v) && !hasTrustedEvidence(trustedEvidence) {
			errs = append(errs, "verification claim requires tool evidence")
		}
	}
	return errs
}

func validateExecutionStructural(out *ExecutionOutput, trustedEvidence ...string) []string {
	if out == nil {
		return []string{"missing execution output"}
	}
	var errs []string
	if strings.TrimSpace(out.Summary) == "" || strings.TrimSpace(out.NextSignal) == "" {
		errs = append(errs, "execution output missing required summary/next_signal")
	}
	switch out.NextSignal {
	case "continue_execution", "planning_required", "ready_for_validation":
		// allowed
	default:
		errs = append(errs, "unknown execution next_signal")
	}
	return errs
}

func containsProgressToolClaim(text string) bool {
	lower := strings.ToLower(text)
	if strings.Contains(lower, "tool_result") || strings.Contains(lower, "tool result") {
		return true
	}
	claimVerb := strings.Contains(lower, "ran ") || strings.Contains(lower, "executed ") || strings.Contains(lower, "запустил")
	if claimVerb && strings.Contains(lower, "test") {
		return true
	}
	if strings.Contains(lower, "go test") && (strings.Contains(lower, "passed") || strings.Contains(lower, "ok") || strings.Contains(lower, "success")) {
		return true
	}
	return false
}
