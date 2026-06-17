package process

import (
	"strings"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/validation"
)

// RunValidators executes common checks and the stage-specific validator.
func RunValidators(resp ParsedResponse) []string {
	var errs []string
	errs = append(errs, commonChecks(resp)...)
	if resp.ActionKind == ActionAnswerQuestion {
		return filterEmpty(errs)
	}
	switch resp.Stage {
	case app.StagePlanning:
		errs = append(errs, validatePlanning(resp.Planning, resp.Raw)...)
	case app.StageExecution:
		errs = append(errs, validateExecution(resp.Execution)...)
	case app.StageValidation:
		errs = append(errs, validateValidation(resp.Validation)...)
	case app.StageDone:
		errs = append(errs, validateDone(resp.Done, resp.Raw)...)
	}
	return filterEmpty(errs)
}

func commonChecks(resp ParsedResponse) []string {
	var errs []string
	if validation.HasSecret(resp.Raw) {
		errs = append(errs, "response contains secret-like data")
	}
	if strings.Contains(strings.ToLower(resp.Raw), "tool_result") {
		errs = append(errs, "response must not invent tool_result values")
	}
	return errs
}

func filterEmpty(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			out = append(out, item)
		}
	}
	return out
}
