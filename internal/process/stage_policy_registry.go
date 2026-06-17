package process

import (
	"encoding/json"

	"github.com/nikbrik/coding_writer/internal/app"
)

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
	return policy, nil
}

func buildPolicies() map[app.TaskStage]StagePolicy {
	planningSchema, _ := json.MarshalIndent(&PlanningOutput{}, "", "  ")
	executionSchema, _ := json.MarshalIndent(&ExecutionOutput{}, "", "  ")
	validationSchema, _ := json.MarshalIndent(&ValidationOutput{}, "", "  ")
	doneSchema, _ := json.MarshalIndent(&DoneOutput{}, "", "  ")

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
			OutputSchema: string(planningSchema),
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
			OutputSchema: string(executionSchema),
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
			OutputSchema: string(validationSchema),
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
			OutputSchema: string(doneSchema),
			Permissions:  P0Permissions(),
		},
	}
}
