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
