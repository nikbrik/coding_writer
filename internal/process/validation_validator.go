package process

import "strings"

// validateValidation validates validation stage structured output.
func validateValidation(out *ValidationOutput) []string {
	if out == nil {
		return []string{"missing validation output"}
	}
	var errs []string
	if len(out.Findings) == 0 && len(out.PassedChecks) == 0 && len(out.MissingEvidence) == 0 {
		errs = append(errs, "validation output must contain findings, passed checks or missing evidence")
	}
	if out.Verdict == "ready_for_done" {
		for _, f := range out.Findings {
			if f.Severity == "blocker" || f.Severity == "high" {
				errs = append(errs, "ready_for_done is blocked by "+f.Severity+" finding")
			}
		}
		if len(out.MissingEvidence) > 0 {
			errs = append(errs, "ready_for_done is blocked by missing evidence")
		}
	}
	raw := strings.ToLower(joinValidationFields(out))
	if strings.Contains(raw, "implemented") || strings.Contains(raw, "added feature") || strings.Contains(raw, "new feature") {
		errs = append(errs, "validation output must not implement fixes or add features")
	}
	return errs
}

func joinValidationFields(out *ValidationOutput) string {
	var parts []string
	for _, f := range out.Findings {
		parts = append(parts, f.Problem, f.Fix)
	}
	parts = append(parts, out.ResidualRisks...)
	return strings.Join(parts, " ")
}
