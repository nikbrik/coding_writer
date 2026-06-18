package tests

import (
	"context"
	"strings"
	"testing"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/process"
	"github.com/nikbrik/coding_writer/internal/providers"
)

func newProcessAcceptanceController(rt acceptanceRuntime) *process.ProcessController {
	registry := process.NewStagePolicyRegistry()
	return &process.ProcessController{
		Tasks:           rt.tasks,
		Profiles:        rt.profiles,
		Memory:          rt.memory,
		Proposals:       rt.proposals,
		Classifier:      rt.classifier,
		Provider:        rt.provider,
		Model:           "fake/model",
		Builder:         rt.builder,
		PolicyRegistry:  registry,
		TransitionGate:  &process.TransitionGate{Tasks: rt.tasks},
		RetryController: process.NewRetryController(),
		AuditStore:      process.NewAuditStore(rt.dir),
	}
}

func processChatCalls(calls []providers.CompletionRequest) int {
	count := 0
	for _, call := range calls {
		if call.Purpose == providers.PurposeChat {
			count++
		}
	}
	return count
}

func TestProcessPlanningRejectsImplementationOutput(t *testing.T) {
	ctx := context.Background()
	rt := newAcceptanceRuntime(t)
	if _, err := rt.tasks.Start("plan task"); err != nil {
		t.Fatal(err)
	}
	rt.provider.ChatResponse = `{"stage":"planning","summary":"I implemented the solution","assumptions":[],"acceptance_criteria":["c"],"plan":["p"],"open_questions":[],"readiness":"ready_for_execution_proposal"}`
	ctrl := newProcessAcceptanceController(rt)
	_, err := ctrl.RunExchange(ctx, process.ExchangeInput{SessionID: "process_plan_reject", Input: "спланируй", ActionKind: process.ActionPlanTask})
	if err == nil || !strings.Contains(err.Error(), "validation_failed") {
		t.Fatalf("want validation_failed, got %v", err)
	}
	short, _ := rt.memory.List(ctx, app.LayerShort, "process_plan_reject", "")
	if len(short) != 0 {
		t.Fatalf("rejected response should not save short memory: %+v", short)
	}
}

func TestProcessValidationPromptUsesReviewerRole(t *testing.T) {
	ctx := context.Background()
	rt := newAcceptanceRuntime(t)
	state, _ := rt.tasks.Start("validate task")
	state, _ = rt.tasks.Move(app.StageExecution)
	state, _ = rt.tasks.Move(app.StageValidation)
	_ = state
	ctrl := newProcessAcceptanceController(rt)
	res, err := ctrl.RunExchange(ctx, process.ExchangeInput{SessionID: "process_prompt", Input: "проверь", ActionKind: process.ActionReviewOutput, RenderOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.RenderedPrompt, "Role: strict reviewer and QA validator") || !strings.Contains(res.RenderedPrompt, "Do not implement fixes in this stage") {
		t.Fatalf("validation reviewer prompt missing:\n%s", res.RenderedPrompt)
	}
}

func TestProcessPausedTaskDoesNotCallProvider(t *testing.T) {
	ctx := context.Background()
	rt := newAcceptanceRuntime(t)
	if _, err := rt.tasks.Start("paused task"); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.tasks.Pause(); err != nil {
		t.Fatal(err)
	}
	ctrl := newProcessAcceptanceController(rt)
	_, err := ctrl.RunExchange(ctx, process.ExchangeInput{SessionID: "process_paused", Input: "продолжай задачу"})
	if err == nil || app.AsError(err).Code != "task_paused" {
		t.Fatalf("want task_paused, got %v", err)
	}
	if processChatCalls(rt.provider.SnapshotCalls()) != 0 {
		t.Fatal("provider chat should not be called while paused")
	}
}

func TestProcessPausedTaskScopedQuestionDoesNotCallProvider(t *testing.T) {
	ctx := context.Background()
	rt := newAcceptanceRuntime(t)
	if _, err := rt.tasks.Start("paused task"); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.tasks.Pause(); err != nil {
		t.Fatal(err)
	}
	ctrl := newProcessAcceptanceController(rt)
	_, err := ctrl.RunExchange(ctx, process.ExchangeInput{SessionID: "process_paused_question", Input: "что по текущей задаче?"})
	if err == nil || app.AsError(err).Code != "task_paused" {
		t.Fatalf("want task_paused, got %v", err)
	}
	if processChatCalls(rt.provider.SnapshotCalls()) != 0 {
		t.Fatal("provider chat should not be called for task-scoped paused question")
	}
}

func TestProcessInvalidValidationRetriesThenBlocks(t *testing.T) {
	ctx := context.Background()
	rt := newAcceptanceRuntime(t)
	state, _ := rt.tasks.Start("validate task")
	state, _ = rt.tasks.Move(app.StageExecution)
	state, _ = rt.tasks.Move(app.StageValidation)
	_ = state
	rt.provider.ChatResponses = []string{"not json", "still bad", "bad again"}
	ctrl := newProcessAcceptanceController(rt)
	_, err := ctrl.RunExchange(ctx, process.ExchangeInput{SessionID: "process_retry", Input: "проверь", ActionKind: process.ActionReviewOutput})
	if err == nil || app.AsError(err).Code != "invalid_json" {
		t.Fatalf("want invalid_json, got %v", err)
	}
	if processChatCalls(rt.provider.SnapshotCalls()) != 3 {
		t.Fatalf("expected initial + 2 retries, got %d", processChatCalls(rt.provider.SnapshotCalls()))
	}
}

func TestProcessSuccessfulValidationTransitionsToDone(t *testing.T) {
	ctx := context.Background()
	rt := newAcceptanceRuntime(t)
	state, _ := rt.tasks.Start("validate task")
	state, _ = rt.tasks.Move(app.StageExecution)
	state, _ = rt.tasks.Move(app.StageValidation)
	_ = state
	rt.provider.ChatResponse = `{"stage":"validation","findings":[],"passed_checks":["tool evidence available"],"missing_evidence":[],"residual_risks":[],"verdict":"ready_for_done"}`
	ctrl := newProcessAcceptanceController(rt)
	res, err := ctrl.RunExchange(ctx, process.ExchangeInput{SessionID: "process_done", Input: "проверь", ActionKind: process.ActionReviewOutput, TrustedEvidence: []string{"go test ./... passed"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Transition == nil || res.Transition.To != app.StageDone {
		t.Fatalf("expected transition to done: %+v", res.Transition)
	}
	current, _ := rt.tasks.Current()
	if current.Stage != app.StageDone || current.ExpectedAction != app.ExpectedNone {
		t.Fatalf("task not done through transition gate: %+v", current)
	}
}

func TestProcessRejectedOutputDoesNotSaveAcceptedShortMemory(t *testing.T) {
	ctx := context.Background()
	rt := newAcceptanceRuntime(t)
	state, _ := rt.tasks.Start("validate task")
	state, _ = rt.tasks.Move(app.StageExecution)
	state, _ = rt.tasks.Move(app.StageValidation)
	_ = state
	rt.provider.ChatResponse = `{"stage":"validation","findings":[],"passed_checks":["tests passed"],"missing_evidence":[],"residual_risks":[],"verdict":"ready_for_done"}`
	ctrl := newProcessAcceptanceController(rt)
	_, err := ctrl.RunExchange(ctx, process.ExchangeInput{SessionID: "process_transition_fail", Input: "проверь", ActionKind: process.ActionReviewOutput})
	if err == nil || app.AsError(err).Code != "validation_failed" {
		t.Fatalf("want validation_failed, got %v", err)
	}
	short, _ := rt.memory.List(ctx, app.LayerShort, "process_transition_fail", "")
	if len(short) != 0 {
		t.Fatalf("failed transition saved accepted short memory: %+v", short)
	}
}
