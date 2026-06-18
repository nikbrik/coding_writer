package process

import "strings"

// validateExecution validates execution stage structured output.
func validateExecution(out *ExecutionOutput, trustedEvidence ...string) []string {
	if out == nil {
		return []string{"missing execution output"}
	}
	var errs []string
	if strings.TrimSpace(out.Summary) == "" || strings.TrimSpace(out.NextSignal) == "" {
		errs = append(errs, "execution output missing required summary/next_signal")
	}
	combined := strings.Join(append([]string{out.Summary}, append(out.ChangedArtifacts, out.Verification...)...), " ")
	if containsTestPassClaim(combined) && !hasTrustedEvidence(trustedEvidence) {
		errs = append(errs, "test/tool claims require trusted application evidence")
	}
	if containsSideEffectClaim(combined) && !hasTrustedEvidence(trustedEvidence) {
		errs = append(errs, "side-effect claims require trusted application evidence")
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
	switch out.NextSignal {
	case "continue_execution", "planning_required", "ready_for_validation":
		// allowed
	default:
		errs = append(errs, "unknown execution next_signal")
	}
	if out.NextSignal == "ready_for_validation" {
		if hasNonEmpty(out.Blockers) {
			errs = append(errs, "ready_for_validation is blocked by active blockers")
		}
		if !hasNonEmpty(out.ChangedArtifacts) || !hasNonEmpty(out.Verification) {
			errs = append(errs, "ready_for_validation requires changed artifacts and verification evidence")
		}
	}
	return errs
}
