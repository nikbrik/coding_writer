package process

import (
	"context"
	"strings"
	"testing"

	"github.com/nikbrik/coding_writer/internal/app"
)

func TestRetryControllerShouldRetryOnlyFixable(t *testing.T) {
	r := NewRetryController()
	if !r.ShouldRetry(app.NewError(app.CategoryValidation, "invalid_json", "bad json", nil)) {
		t.Fatal("invalid_json should retry")
	}
	if r.ShouldRetry(app.NewError(app.CategoryValidation, "stage_mismatch", "bad stage", nil)) {
		t.Fatal("stage_mismatch must not retry")
	}
	if r.ShouldRetry(app.NewError(app.CategoryValidation, "task_paused", "paused", nil)) {
		t.Fatal("task_paused must not retry")
	}
	if r.ShouldRetry(app.NewError(app.CategoryValidation, "secret_blocked", "secret", nil)) {
		t.Fatal("secret_blocked must not retry")
	}
}

func TestRetryCorrectionPrompt(t *testing.T) {
	prompt := NewRetryController().CorrectionPrompt([]string{"bad schema"})
	if !strings.Contains(prompt, "<trusted_validator_errors>") || !strings.Contains(prompt, "bad schema") {
		t.Fatalf("bad correction prompt: %s", prompt)
	}
}

func TestProcessControllerRetriesFixableParseError(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	fake.ChatResponses = []string{
		"not json",
		`{"stage":"planning","summary":"ok","assumptions":[],"acceptance_criteria":["c"],"plan":["p"],"open_questions":[],"readiness":"ready_for_execution_proposal"}`,
	}
	res, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "спланируй", ActionKind: ActionPlanTask})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Answer, "ok") {
		t.Fatalf("unexpected answer: %s", res.Answer)
	}
	if chatCalls(fake.Calls) != 2 {
		t.Fatalf("expected retry chat calls, got %d", chatCalls(fake.Calls))
	}
}

func TestProcessControllerRejectsAfterMaxRetries(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	fake.ChatResponses = []string{"bad", "still bad", "bad again"}
	_, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "спланируй", ActionKind: ActionPlanTask})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if chatCalls(fake.Calls) != 3 {
		t.Fatalf("expected initial + 2 retries, got %d", chatCalls(fake.Calls))
	}
}
