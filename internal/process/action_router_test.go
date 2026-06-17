package process

import (
	"testing"

	"github.com/nikbrik/coding_writer/internal/app"
)

func TestResolveActionKindExecutionQuestionIsAnswerQuestion(t *testing.T) {
	got := ResolveActionKind("как работает этот модуль?", app.StageExecution, app.ExpectedLLMResponse)
	if got != ActionAnswerQuestion {
		t.Fatalf("want answer_question, got %s", got)
	}
}

func TestResolveActionKindExecutionDefaultExecutesStep(t *testing.T) {
	got := ResolveActionKind("реализуй шаг", app.StageExecution, app.ExpectedLLMResponse)
	if got != ActionExecutePlanStep {
		t.Fatalf("want execute_plan_step, got %s", got)
	}
}

func TestResolveActionKindPlanningPlanQuestionPlansTask(t *testing.T) {
	got := ResolveActionKind("спланируй MVP?", app.StagePlanning, app.ExpectedUserInput)
	if got != ActionPlanTask {
		t.Fatalf("want plan_task, got %s", got)
	}
}

func TestResolveActionKindPlanningOrdinaryQuestionIsAnswerQuestion(t *testing.T) {
	got := ResolveActionKind("что такое MVP?", app.StagePlanning, app.ExpectedUserInput)
	if got != ActionAnswerQuestion {
		t.Fatalf("want answer_question, got %s", got)
	}
}

func TestResolveActionKindPlanningQuestionAboutPlanIsAnswerQuestion(t *testing.T) {
	got := ResolveActionKind("what is the plan?", app.StagePlanning, app.ExpectedUserInput)
	if got != ActionAnswerQuestion {
		t.Fatalf("want answer_question, got %s", got)
	}
}

func TestResolveActionKindPlanningConceptQuestionIsAnswerQuestion(t *testing.T) {
	got := ResolveActionKind("what is planning?", app.StagePlanning, app.ExpectedUserInput)
	if got != ActionAnswerQuestion {
		t.Fatalf("want answer_question, got %s", got)
	}
}

func TestResolveActionKindValidationQuestionIsAnswerQuestion(t *testing.T) {
	got := ResolveActionKind("почему это high?", app.StageValidation, app.ExpectedUserConfirmation)
	if got != ActionAnswerQuestion {
		t.Fatalf("want answer_question, got %s", got)
	}
}

func TestResolveActionKindValidationVerifyQuestionVerifiesCriteria(t *testing.T) {
	got := ResolveActionKind("verify criteria?", app.StageValidation, app.ExpectedUserConfirmation)
	if got != ActionVerifyCriteria {
		t.Fatalf("want verify_criteria, got %s", got)
	}
}

func TestResolveActionKindDoneSummaryIntent(t *testing.T) {
	got := ResolveActionKind("summarize completed task", app.StageDone, app.ExpectedNone)
	if got != ActionSummarizeDone {
		t.Fatalf("want summarize_done, got %s", got)
	}
}
