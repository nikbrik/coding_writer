package process

import (
	"context"
	"strings"
	"testing"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/memory"
	"github.com/nikbrik/coding_writer/internal/profiles"
	"github.com/nikbrik/coding_writer/internal/providers"
	"github.com/nikbrik/coding_writer/internal/tasks"
)

func chatCalls(calls []providers.CompletionRequest) int {
	n := 0
	for _, c := range calls {
		if c.Purpose == providers.PurposeChat {
			n++
		}
	}
	return n
}

func newTestController(t *testing.T) (*ProcessController, *providers.FakeProvider, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := app.NewConfigManager(dir)
	if err := cfg.EnsureStorageTree(); err != nil {
		t.Fatal(err)
	}
	profMgr := profiles.NewManager(dir, cfg)
	if err := profMgr.EnsureDefaults(); err != nil {
		t.Fatal(err)
	}
	loaded, _ := cfg.Load()
	loaded.ActiveProfileID = "student"
	if err := cfg.Save(loaded); err != nil {
		t.Fatal(err)
	}
	memMgr := memory.NewManager(dir)
	fake := providers.NewFakeProvider()
	taskMgr := tasks.NewManager(dir)
	return &ProcessController{
		Tasks:           taskMgr,
		Profiles:        profMgr,
		Memory:          memMgr,
		Proposals:       memory.NewProposalStore(dir, memMgr),
		Classifier:      memory.NewClassifier(fake),
		Provider:        fake,
		Model:           "fake/model",
		Builder:         newTestPromptBuilder(),
		PolicyRegistry:  NewStagePolicyRegistry(),
		TransitionGate:  &TransitionGate{Tasks: taskMgr},
		RetryController: NewRetryController(),
		AuditStore:      NewAuditStore(dir),
	}, fake, dir
}

func newTestPromptBuilder() PromptBuilder {
	return &testPromptBuilder{factory: NewStagePromptFactory(NewStagePolicyRegistry())}
}

type testPromptBuilder struct {
	factory *StagePromptFactory
}

func (b *testPromptBuilder) Build(input PromptBuildInput) ([]app.ChatMessage, error) {
	profileBlock, _ := profiles.Render(input.Profile)
	msgs := []app.ChatMessage{
		{Role: app.RoleSystem, Content: b.factory.BaseSystemPrompt()},
		{Role: app.RoleSystem, Content: b.factory.ProcessContractPrompt()},
	}
	if input.Stage != "" {
		stagePrompt, _ := b.factory.StagePrompt(input.Stage, input.ActionKind)
		toolPrompt, _ := b.factory.ToolPolicyPrompt(input.Stage, input.ActionKind)
		msgs = append(msgs,
			app.ChatMessage{Role: app.RoleSystem, Content: stagePrompt},
			app.ChatMessage{Role: app.RoleSystem, Content: toolPrompt},
		)
	}
	msgs = append(msgs,
		app.ChatMessage{Role: app.RoleSystem, Content: profileBlock},
		app.ChatMessage{Role: app.RoleUser, Content: input.Query},
	)
	return msgs, nil
}

func TestProcessControllerPausedTaskBlocksChat(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	if _, err := ctrl.Tasks.Start("paused task"); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.Pause(); err != nil {
		t.Fatal(err)
	}
	_, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "продолжай"})
	if err == nil {
		t.Fatal("expected error for paused task")
	}
	if app.AsError(err).Code != "task_paused" {
		t.Fatalf("expected task_paused, got %v", err)
	}
	if len(fake.Calls) != 0 {
		t.Fatal("provider should not be called for paused task")
	}
}

func TestProcessControllerDoneTaskBlocksMutation(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	if _, err := ctrl.Tasks.Start("done task"); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.Move(app.StageExecution); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.Move(app.StageValidation); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.Move(app.StageDone); err != nil {
		t.Fatal(err)
	}
	_, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "реализуй ещё", ActionKind: ActionExecutePlanStep})
	if err == nil {
		t.Fatal("expected error for done task mutation")
	}
	if app.AsError(err).Code != "task_done" {
		t.Fatalf("expected task_done, got %v", err)
	}
	if len(fake.Calls) != 0 {
		t.Fatal("provider should not be called for done task mutation")
	}
}

func TestProcessControllerDoneTaskBlocksImplicitMutationQuestion(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	if _, err := ctrl.Tasks.Start("done task"); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.Move(app.StageExecution); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.Move(app.StageValidation); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.Move(app.StageDone); err != nil {
		t.Fatal(err)
	}
	_, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "can you implement X?"})
	if err == nil || app.AsError(err).Code != "task_done" {
		t.Fatalf("want task_done, got %v", err)
	}
	if len(fake.Calls) != 0 {
		t.Fatal("provider should not be called for implicit done mutation")
	}
}

func TestProcessControllerForbiddenActionBlocked(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	_, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "реализуй", ActionKind: ActionExecutePlanStep})
	if err == nil {
		t.Fatal("expected error for forbidden action")
	}
	if app.AsError(err).Code != "forbidden_action" {
		t.Fatalf("expected forbidden_action, got %v", err)
	}
	if len(fake.Calls) != 0 {
		t.Fatal("provider should not be called for forbidden action")
	}
}

func TestProcessControllerSuccessfulExchangeCallsProvider(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	fake.ChatResponse = `{"stage":"planning","summary":"planning answer","assumptions":[],"acceptance_criteria":["done"],"plan":["step"],"open_questions":[],"readiness":"ready_for_execution_proposal"}`
	res, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "спланируй MVP", ActionKind: ActionPlanTask})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Answer, "planning answer") {
		t.Fatalf("unexpected answer: %q", res.Answer)
	}
	if chatCalls(fake.Calls) != 1 {
		t.Fatalf("expected one chat provider call, got %d", chatCalls(fake.Calls))
	}
}

func TestProcessControllerPersistsPendingPlanningAndApprovesAfterRestart(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, dir := newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	fake.ChatResponse = `{"stage":"planning","summary":"build it","assumptions":[],"acceptance_criteria":["tests pass"],"plan":["first step"],"open_questions":[],"readiness":"ready_for_execution_proposal"}`
	if _, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "спланируй", ActionKind: ActionPlanTask}); err != nil {
		t.Fatal(err)
	}
	pending, err := ctrl.Tasks.Current()
	if err != nil {
		t.Fatal(err)
	}
	if pending.PendingPlanning == nil || pending.ExpectedAction != app.ExpectedUserConfirmation {
		t.Fatalf("pending planning not persisted: %+v", pending)
	}
	restarted := *ctrl
	restarted.Tasks = tasks.NewManager(dir)
	restarted.TransitionGate = &TransitionGate{Tasks: restarted.Tasks}
	approved, err := restarted.RunExchange(ctx, ExchangeInput{SessionID: "s2", Input: "да"})
	if err != nil {
		t.Fatal(err)
	}
	if approved.Transition == nil || approved.Transition.To != app.StageExecution {
		t.Fatalf("pending planning not approved: %+v", approved.Transition)
	}
	current, _ := restarted.Tasks.Current()
	if current.Stage != app.StageExecution || current.CurrentStep != "first step" || current.PendingPlanning != nil {
		t.Fatalf("bad approved state: %+v", current)
	}
}

func TestProcessControllerExecutionProgressUpdatesCurrentStep(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.Move(app.StageExecution); err != nil {
		t.Fatal(err)
	}
	fake.ChatResponse = `{"stage":"execution","summary":"worked","current_step":"first","completed_steps":["first"],"next_step":"second","changed_artifacts":[],"verification":[],"blockers":[],"next_signal":"continue_execution"}`
	if _, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "реализуй", ActionKind: ActionExecutePlanStep}); err != nil {
		t.Fatal(err)
	}
	current, _ := ctrl.Tasks.Current()
	if current.CurrentStep != "second" || len(current.CompletedSteps) != 1 || current.CompletedSteps[0] != "first" {
		t.Fatalf("execution progress not persisted: %+v", current)
	}
}

func TestProcessControllerDoneBenignInputIsReadOnlyAnswer(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	if _, err := ctrl.Tasks.Start("done task"); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.Move(app.StageExecution); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.Move(app.StageValidation); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.Move(app.StageDone); err != nil {
		t.Fatal(err)
	}
	fake.ChatResponse = "you are welcome"
	res, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "thanks"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Answer != "you are welcome" {
		t.Fatalf("bad done answer: %+v", res)
	}
}

func TestProcessControllerAnswerQuestionAllowsPlainInfo(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	fake.ChatResponse = "plain informational answer"
	res, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "hello", ActionKind: ActionAnswerQuestion})
	if err != nil {
		t.Fatal(err)
	}
	if res.Answer != "plain informational answer" {
		t.Fatalf("unexpected answer: %q", res.Answer)
	}
}

func TestProcessControllerAnswerQuestionRejectsSideEffectClaims(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	fake.ChatResponse = "I edited files and all tests passed"
	_, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "answer_side_effect", Input: "hello", ActionKind: ActionAnswerQuestion})
	if err == nil || app.AsError(err).Code != "validation_failed" {
		t.Fatalf("want validation_failed, got %v", err)
	}
	records, _ := ctrl.Memory.List(ctx, app.LayerShort, "answer_side_effect", "")
	if len(records) != 0 {
		t.Fatalf("rejected answer should not save short memory: %+v", records)
	}
}

func TestProcessControllerUsesMemoryModelForClassifier(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	ctrl.MemoryModel = "memory/model"
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	fake.ChatResponse = "plain informational answer"
	if _, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "hello", ActionKind: ActionAnswerQuestion}); err != nil {
		t.Fatal(err)
	}
	for _, call := range fake.SnapshotCalls() {
		if call.Purpose == providers.PurposeClassifier && call.Model == "memory/model" {
			return
		}
	}
	t.Fatalf("classifier did not use memory model: %+v", fake.SnapshotCalls())
}

func TestProcessControllerRejectsEmptyAssistantOutputBeforeMemory(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	fake.ChatResponse = "   "
	_, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "empty", Input: "hello", ActionKind: ActionAnswerQuestion})
	if err == nil || app.AsError(err).Code != "empty_output" {
		t.Fatalf("want empty_output, got %v", err)
	}
	records, _ := ctrl.Memory.List(ctx, app.LayerShort, "empty", "")
	if len(records) != 0 {
		t.Fatalf("empty output should not save partial exchange: %+v", records)
	}
}

func TestProcessControllerRequiresClassifierAndProposalStore(t *testing.T) {
	ctx := context.Background()
	ctrl, _, _ := newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	ctrl.Classifier = nil
	_, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "hello", ActionKind: ActionAnswerQuestion})
	if err == nil || app.AsError(err).Code != "missing_classifier" {
		t.Fatalf("want missing_classifier, got %v", err)
	}
	ctrl, _, _ = newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	ctrl.Proposals = nil
	_, err = ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s2", Input: "hello", ActionKind: ActionAnswerQuestion})
	if err == nil || app.AsError(err).Code != "missing_proposal_store" {
		t.Fatalf("want missing_proposal_store, got %v", err)
	}
}

func TestProcessControllerMissingCoreDependenciesReturnErrors(t *testing.T) {
	ctx := context.Background()
	_, err := (*ProcessController)(nil).RunExchange(ctx, ExchangeInput{Input: "hello"})
	if err == nil || app.AsError(err).Code != "missing_process_controller" {
		t.Fatalf("want missing_process_controller, got %v", err)
	}
	ctrl := &ProcessController{}
	_, err = ctrl.RunExchange(ctx, ExchangeInput{Input: "hello"})
	if err == nil || app.AsError(err).Code != "missing_task_manager" {
		t.Fatalf("want missing_task_manager, got %v", err)
	}
	ctrl, _, _ = newTestController(t)
	ctrl.Profiles = nil
	_, err = ctrl.RunExchange(ctx, ExchangeInput{Input: "hello"})
	if err == nil || app.AsError(err).Code != "missing_profile_manager" {
		t.Fatalf("want missing_profile_manager, got %v", err)
	}
	ctrl, _, _ = newTestController(t)
	ctrl.Builder = nil
	_, err = ctrl.RunExchange(ctx, ExchangeInput{Input: "hello"})
	if err == nil || app.AsError(err).Code != "missing_prompt_builder" {
		t.Fatalf("want missing_prompt_builder, got %v", err)
	}
}

func TestProcessControllerRenderOnlyDoesNotCallProvider(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	res, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "спланируй MVP", ActionKind: ActionPlanTask, RenderOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.RenderedPrompt == "" {
		t.Fatal("expected rendered prompt")
	}
	if len(fake.Calls) != 0 {
		t.Fatal("provider should not be called in render-only mode")
	}
}

func TestProcessControllerPromptContainsStageRole(t *testing.T) {
	ctx := context.Background()
	ctrl, _, _ := newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	res, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "спланируй MVP", ActionKind: ActionPlanTask, RenderOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.RenderedPrompt, "Current stage: planning") {
		t.Fatalf("missing stage prompt:\n%s", res.RenderedPrompt)
	}
	if !strings.Contains(res.RenderedPrompt, "Role: requirements analyst") {
		t.Fatalf("missing role:\n%s", res.RenderedPrompt)
	}
}
