package process

import "strings"

// validateValidation validates validation stage structured output.
func validateValidation(out *ValidationOutput, trustedEvidence ...string) []string {
	if out == nil {
		return []string{"missing validation output"}
	}
	var errs []string
	if strings.TrimSpace(out.Verdict) == "" {
		errs = append(errs, "validation output missing required verdict")
	}
	if len(out.Findings) == 0 && len(out.PassedChecks) == 0 && len(out.MissingEvidence) == 0 {
		errs = append(errs, "validation output must contain findings, passed checks or missing evidence")
	}
	for _, f := range out.Findings {
		if !isKnownFindingSeverity(f.Severity) {
			errs = append(errs, "unknown finding severity")
		}
		if strings.TrimSpace(f.Location) == "" || strings.TrimSpace(f.Problem) == "" || strings.TrimSpace(f.Fix) == "" {
			errs = append(errs, "finding missing required location/problem/fix")
		}
	}
	switch out.Verdict {
	case "needs_execution_fixes", "blocked_missing_evidence", "ready_for_done":
		// allowed
	default:
		errs = append(errs, "unknown validation verdict")
	}
	if out.Verdict == "ready_for_done" {
		for _, f := range out.Findings {
			if isBlockerOrHighSeverity(f.Severity) {
				errs = append(errs, "ready_for_done is blocked by "+f.Severity+" finding")
			}
		}
		if len(out.MissingEvidence) > 0 {
			errs = append(errs, "ready_for_done is blocked by missing evidence")
		}
		if !hasNonEmpty(out.PassedChecks) {
			errs = append(errs, "ready_for_done requires validation evidence")
		}
		if !hasTrustedEvidence(trustedEvidence) {
			errs = append(errs, "ready_for_done requires trusted application evidence")
		}
	}
	if out.Verdict == "needs_execution_fixes" && !hasActionableFinding(out.Findings) {
		errs = append(errs, "needs_execution_fixes requires actionable findings")
	}
	if out.Verdict == "blocked_missing_evidence" && !hasNonEmpty(out.MissingEvidence) {
		errs = append(errs, "blocked_missing_evidence requires missing evidence")
	}
	raw := joinValidationFields(out)
	if containsImplementationClaim(raw) || strings.Contains(strings.ToLower(raw), "added feature") || strings.Contains(strings.ToLower(raw), "new feature") {
		errs = append(errs, "validation output must not implement fixes or add features")
	}
	return errs
}

func joinValidationFields(out *ValidationOutput) string {
	var parts []string
	for _, f := range out.Findings {
		parts = append(parts, f.Problem, f.Fix)
	}
	parts = append(parts, out.PassedChecks...)
	parts = append(parts, out.MissingEvidence...)
	parts = append(parts, out.ResidualRisks...)
	return strings.Join(parts, " ")
}
