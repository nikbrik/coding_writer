package process

import "github.com/nikbrik/coding_writer/internal/app"

type ActionKind string

const (
	ActionAnswerQuestion     ActionKind = "answer_question"
	ActionPlanTask           ActionKind = "plan_task"
	ActionAskClarification   ActionKind = "ask_clarification"
	ActionExecutePlanStep    ActionKind = "execute_plan_step"
	ActionSummarizeExecution ActionKind = "summarize_execution"
	ActionReviewOutput       ActionKind = "review_output"
	ActionVerifyCriteria     ActionKind = "verify_criteria"
	ActionSummarizeDone      ActionKind = "summarize_done"
	ActionProposeTransition  ActionKind = "propose_transition"
)

func AllActionKinds() []ActionKind {
	return []ActionKind{
		ActionAnswerQuestion,
		ActionPlanTask,
		ActionAskClarification,
		ActionExecutePlanStep,
		ActionSummarizeExecution,
		ActionReviewOutput,
		ActionVerifyCriteria,
		ActionSummarizeDone,
		ActionProposeTransition,
	}
}

func (k ActionKind) Valid() bool {
	for _, candidate := range AllActionKinds() {
		if candidate == k {
			return true
		}
	}
	return false
}

func (k ActionKind) AllowedStages() []app.TaskStage {
	switch k {
	case ActionAnswerQuestion:
		return []app.TaskStage{app.StagePlanning, app.StageExecution, app.StageValidation, app.StageDone}
	case ActionPlanTask, ActionAskClarification:
		return []app.TaskStage{app.StagePlanning}
	case ActionExecutePlanStep, ActionSummarizeExecution:
		return []app.TaskStage{app.StageExecution}
	case ActionReviewOutput, ActionVerifyCriteria:
		return []app.TaskStage{app.StageValidation}
	case ActionSummarizeDone:
		return []app.TaskStage{app.StageDone}
	case ActionProposeTransition:
		return []app.TaskStage{app.StagePlanning, app.StageExecution, app.StageValidation}
	default:
		return nil
	}
}

func (k ActionKind) IsAllowedIn(stage app.TaskStage) bool {
	for _, allowed := range k.AllowedStages() {
		if allowed == stage {
			return true
		}
	}
	return false
}

func RequiresSchema(kind ActionKind) bool {
	return kind != ActionAnswerQuestion
}
