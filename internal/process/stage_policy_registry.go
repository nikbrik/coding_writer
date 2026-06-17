package process

import "github.com/nikbrik/coding_writer/internal/app"

// StagePolicyRegistry holds the canonical trusted policies for each stage.
type StagePolicyRegistry struct {
	policies map[app.TaskStage]StagePolicy
}

func NewStagePolicyRegistry() *StagePolicyRegistry {
	return &StagePolicyRegistry{policies: buildPolicies()}
}

func (r *StagePolicyRegistry) PolicyFor(stage app.TaskStage) (StagePolicy, error) {
	policy, ok := r.policies[stage]
	if !ok {
		return StagePolicy{}, app.NewError(app.CategoryValidation, "unknown_stage", "no process policy for stage", nil)
	}
	policy.AllowedActions = append([]ActionKind(nil), policy.AllowedActions...)
	policy.ForbiddenActions = append([]ActionKind(nil), policy.ForbiddenActions...)
	return policy, nil
}

func buildPolicies() map[app.TaskStage]StagePolicy {
	return map[app.TaskStage]StagePolicy{
		app.StagePlanning: {
			Stage:          app.StagePlanning,
			Role:           "requirements analyst and implementation planner",
			AllowedActions: []ActionKind{ActionAnswerQuestion, ActionPlanTask, ActionAskClarification, ActionProposeTransition},
			ForbiddenActions: []ActionKind{
				ActionExecutePlanStep,
				ActionSummarizeExecution,
				ActionReviewOutput,
				ActionVerifyCriteria,
				ActionSummarizeDone,
			},
			OutputSchema: planningSchemaText(),
			Permissions:  P0Permissions(),
		},
		app.StageExecution: {
			Stage:          app.StageExecution,
			Role:           "implementer",
			AllowedActions: []ActionKind{ActionAnswerQuestion, ActionExecutePlanStep, ActionSummarizeExecution, ActionProposeTransition},
			ForbiddenActions: []ActionKind{
				ActionPlanTask,
				ActionAskClarification,
				ActionReviewOutput,
				ActionVerifyCriteria,
				ActionSummarizeDone,
			},
			OutputSchema: executionSchemaText(),
			Permissions:  P0Permissions(),
		},
		app.StageValidation: {
			Stage:          app.StageValidation,
			Role:           "strict reviewer and QA validator",
			AllowedActions: []ActionKind{ActionAnswerQuestion, ActionReviewOutput, ActionVerifyCriteria, ActionProposeTransition},
			ForbiddenActions: []ActionKind{
				ActionPlanTask,
				ActionAskClarification,
				ActionExecutePlanStep,
				ActionSummarizeExecution,
				ActionSummarizeDone,
			},
			OutputSchema: validationSchemaText(),
			Permissions:  P0Permissions(),
		},
		app.StageDone: {
			Stage:          app.StageDone,
			Role:           "completion summarizer",
			AllowedActions: []ActionKind{ActionAnswerQuestion, ActionSummarizeDone},
			ForbiddenActions: []ActionKind{
				ActionPlanTask,
				ActionAskClarification,
				ActionExecutePlanStep,
				ActionSummarizeExecution,
				ActionReviewOutput,
				ActionVerifyCriteria,
				ActionProposeTransition,
			},
			OutputSchema: doneSchemaText(),
			Permissions:  P0Permissions(),
		},
	}
}

func planningSchemaText() string {
	return `{
  "stage": "planning",
  "summary": "string, required",
  "assumptions": ["string"],
  "acceptance_criteria": ["string"],
  "plan": ["string"],
  "open_questions": ["string"],
  "readiness": "needs_user_input | ready_for_execution_proposal"
}`
}

func executionSchemaText() string {
	return `{
  "stage": "execution",
  "summary": "string, required",
  "changed_artifacts": ["string"],
  "verification": ["string; use 'not run' unless trusted tool evidence exists"],
  "blockers": ["string"],
  "next_signal": "continue_execution | planning_required | ready_for_validation"
}`
}

func validationSchemaText() string {
	return `{
  "stage": "validation",
  "findings": [{"severity":"blocker | high | medium | low","location":"string","problem":"string","fix":"string"}],
  "passed_checks": ["string"],
  "missing_evidence": ["string"],
  "residual_risks": ["string"],
  "verdict": "needs_execution_fixes | blocked_missing_evidence | ready_for_done"
}`
}

func doneSchemaText() string {
	return `{
  "stage": "done",
  "summary": "string, required",
  "acceptance_status": ["string"],
  "validation_evidence": ["string"],
  "follow_up_task_proposals": ["string"]
}`
}
