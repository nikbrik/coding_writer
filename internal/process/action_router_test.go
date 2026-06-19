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

func TestResolveActionKindNoTaskPlanningIntentPlansTask(t *testing.T) {
	got := ResolveActionKind("спланируй модуль памяти", "", "")
	if got != ActionPlanTask {
		t.Fatalf("want plan_task, got %s", got)
	}
}

func TestResolveActionKindNoTaskGoalIntentPlansTask(t *testing.T) {
	cases := []string{
		"Спланируй и реши LeetCode-задачу Contains Duplicate на Go с тестами.",
		"Надо реализовать обработку ошибок в CLI.",
		"Please verify package behavior before release.",
	}
	for _, input := range cases {
		got := ResolveActionKind(input, "", "")
		if got != ActionPlanTask {
			t.Fatalf("%q: want plan_task, got %s", input, got)
		}
	}
}

func TestResolveActionKindNoTaskQuestionAboutNeedIsAnswerQuestion(t *testing.T) {
	got := ResolveActionKind("Нужно ли использовать Cobra для CLI?", "", "")
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

func TestResolveActionKindExecutionContinueWithNoReexplainExecutesStep(t *testing.T) {
	got := ResolveActionKind("Продолжай задачу. Не проси заново объяснить контекст.", app.StageExecution, app.ExpectedLLMResponse)
	if got != ActionExecutePlanStep {
		t.Fatalf("want execute_plan_step, got %s", got)
	}
}

func TestResolveActionKindExecutionReviewRequiresReadyIntent(t *testing.T) {
	for _, input := range []string{
		"Продолжи выполнение текущего шага утвержденного плана. Не повторяй исходные требования.",
		"Не переходи к проверке, просто продолжай текущий шаг.",
		"проверь контекст перед следующим шагом",
	} {
		got := ResolveActionKind(input, app.StageExecution, app.ExpectedLLMResponse)
		if got != ActionExecutePlanStep && got != ActionAnswerQuestion {
			t.Fatalf("%q: want no validation transition action, got %s", input, got)
		}
	}
	got := ResolveActionKind("Готово к проверке.", app.StageExecution, app.ExpectedLLMResponse)
	if got != ActionSummarizeExecution {
		t.Fatalf("want summarize_execution for ready intent, got %s", got)
	}
}

func TestResolveActionKindExecutionPlanningIntentPlansTask(t *testing.T) {
	got := ResolveActionKind("спланируй модуль памяти", app.StageExecution, app.ExpectedLLMResponse)
	if got != ActionPlanTask {
		t.Fatalf("want plan_task, got %s", got)
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

func TestResolveActionKindExplicitNegationFallbackDoesNotProceed(t *testing.T) {
	for _, tc := range []struct {
		input string
		stage app.TaskStage
	}{
		{"do not proceed yet", app.StagePlanning},
		{"not yet, do not continue", app.StagePlanning},
		{"not ready for validation", app.StageExecution},
		{"не продолжай пока", app.StagePlanning},
		{"не готово к проверке", app.StageExecution},
	} {
		got := ResolveActionKind(tc.input, tc.stage, app.ExpectedUserInput)
		if got != ActionAnswerQuestion {
			t.Fatalf("%q in %s: want answer_question, got %s", tc.input, tc.stage, got)
		}
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
	for _, input := range []string{"summarize completed task", "what changed?", "status update?"} {
		got := ResolveActionKind(input, app.StageDone, app.ExpectedNone)
		if got != ActionSummarizeDone {
			t.Fatalf("%q: want summarize_done, got %s", input, got)
		}
	}
}

func TestResolveActionKindDoneBenignQuestionsAreReadOnly(t *testing.T) {
	for _, input := range []string{"can you update me?", "what changes were made?", "what change was made?"} {
		got := ResolveActionKind(input, app.StageDone, app.ExpectedNone)
		if got != ActionAnswerQuestion {
			t.Fatalf("%q: want answer_question, got %s", input, got)
		}
	}
}

func TestResolveActionKindDoneMutationIntentIsForbiddenAction(t *testing.T) {
	for _, input := range []string{"реализуй ещё", "доработай done task", "add another file", "update docs", "создай новый модуль", "refactor module", "rename file", "modify config", "continue work", "can you implement X?"} {
		got := ResolveActionKind(input, app.StageDone, app.ExpectedNone)
		if got != ActionExecutePlanStep {
			t.Fatalf("%q: want execute_plan_step for done mutation gate, got %s", input, got)
		}
	}
}
