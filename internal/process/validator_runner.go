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
		errs = append(errs, validateAnswerQuestion(resp.Raw)...)
		return filterEmpty(errs)
	}
	switch resp.Stage {
	case app.StagePlanning:
		errs = append(errs, validatePlanning(resp.Planning, resp.Raw)...)
	case app.StageExecution:
		errs = append(errs, validateExecution(resp.Execution, resp.TrustedEvidence...)...)
	case app.StageValidation:
		errs = append(errs, validateValidation(resp.Validation, resp.TrustedEvidence...)...)
	case app.StageDone:
		errs = append(errs, validateDone(resp.Done, resp.Raw)...)
	}
	errs = append(errs, validateActionKind(resp)...)
	return filterEmpty(errs)
}

func validateAnswerQuestion(raw string) []string {
	var errs []string
	lower := strings.ToLower(raw)
	if containsSideEffectClaim(raw) || containsTestPassClaim(raw) && !isExplicitNotRun(raw) {
		errs = append(errs, "answer_question must not claim file, memory, state, command, tool, or test side effects")
	}
	transitionTokens := []string{"ready_for_execution_proposal", "ready_for_validation", "ready_for_done", "planning_required", "needs_execution_fixes", "blocked_missing_evidence", "stage=done", `"stage":"done"`, `"next_signal"`, `"readiness"`, `"verdict"`}
	if containsAny(lower, transitionTokens) {
		errs = append(errs, "answer_question must not propose or claim task transition")
	}
	return errs
}

func validateActionKind(resp ParsedResponse) []string {
	switch resp.ActionKind {
	case ActionPlanTask:
		if resp.Planning == nil || !hasNonEmpty(resp.Planning.Plan) && !hasNonEmpty(resp.Planning.OpenQuestions) {
			return []string{"plan_task requires plan items or open questions"}
		}
	case ActionAskClarification:
		if resp.Planning == nil || !hasNonEmpty(resp.Planning.OpenQuestions) || resp.Planning.Readiness != "needs_user_input" {
			return []string{"ask_clarification requires open questions and needs_user_input readiness"}
		}
	case ActionExecutePlanStep:
		if resp.Execution == nil || strings.TrimSpace(resp.Execution.Summary) == "" {
			return []string{"execute_plan_step requires execution summary"}
		}
	case ActionSummarizeExecution:
		if resp.Execution == nil || strings.TrimSpace(resp.Execution.Summary) == "" {
			return []string{"summarize_execution requires execution summary"}
		}
	case ActionReviewOutput:
		if resp.Validation == nil || (!hasNonEmpty(resp.Validation.PassedChecks) && !hasNonEmpty(resp.Validation.MissingEvidence) && len(resp.Validation.Findings) == 0) {
			return []string{"review_output requires findings, checks, or missing evidence"}
		}
	case ActionVerifyCriteria:
		if resp.Validation == nil || (!hasNonEmpty(resp.Validation.PassedChecks) && !hasNonEmpty(resp.Validation.MissingEvidence)) {
			return []string{"verify_criteria requires passed checks or missing evidence"}
		}
	case ActionSummarizeDone:
		if resp.Done == nil || strings.TrimSpace(resp.Done.Summary) == "" {
			return []string{"summarize_done requires done summary"}
		}
	case ActionProposeTransition:
		if resp.Planning != nil && resp.Planning.Readiness == "ready_for_execution_proposal" {
			return nil
		}
		if resp.Execution != nil && (resp.Execution.NextSignal == "ready_for_validation" || resp.Execution.NextSignal == "planning_required") {
			return nil
		}
		if resp.Validation != nil && (resp.Validation.Verdict == "needs_execution_fixes" || resp.Validation.Verdict == "ready_for_done") {
			return nil
		}
		return []string{"propose_transition requires a transition-ready signal"}
	}
	return nil
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
