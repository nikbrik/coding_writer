package process

import (
	"context"
	"strings"
	"testing"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/invariants"
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

func validatorCalls(calls []providers.CompletionRequest) int {
	n := 0
	for _, c := range calls {
		if c.Purpose == providers.PurposeValidator {
			n++
		}
	}
	return n
}

func auditDecisionCount(events []ProcessAuditEvent, decision string) int {
	n := 0
	for _, event := range events {
		if event.Decision == decision {
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
	invMgr := invariants.NewManager(dir)
	if err := invMgr.EnsureDefaults(); err != nil {
		t.Fatal(err)
	}
	fake := providers.NewFakeProvider()
	taskMgr := tasks.NewManager(dir)
	return &ProcessController{
		Tasks:           taskMgr,
		Profiles:        profMgr,
		Memory:          memMgr,
		Invariants:      invMgr,
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

func issueTestEvidence(t *testing.T, dir string, task app.TaskState, sessionID, source string) []string {
	t.Helper()
	token, _, err := NewTrustedEvidenceStore(dir).Issue(task.ID, sessionID, source, 0, "ok")
	if err != nil {
		t.Fatal(err)
	}
	return []string{token}
}

func moveCurrentTaskToValidatedDone(t *testing.T, ctrl *ProcessController) {
	t.Helper()
	if _, err := ctrl.Tasks.Move(app.StageExecution); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.Move(app.StageValidation); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.RecordAcceptedValidation("ready_for_done", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.Move(app.StageDone); err != nil {
		t.Fatal(err)
	}
}

func moveCurrentTaskToExecutionWithApprovedPlan(t *testing.T, ctrl *ProcessController) {
	t.Helper()
	if _, err := ctrl.Tasks.SetPlanningOutput("build it", []string{"tests pass"}, []string{"first step"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.RecordPlanningApproval("approved", "test setup", 1, "test approval"); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.ApproveCurrentPlanning(); err != nil {
		t.Fatal(err)
	}
}

func TestProcessControllerInvariantInputConflictSkipsProvider(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	_, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "предложи переписать MVP на Python"})
	if err == nil || app.AsError(err).Code != "invariant_conflict" {
		t.Fatalf("want invariant_conflict, got %v", err)
	}
	if !strings.Contains(app.AsError(err).Message, "stack.go") || !strings.Contains(app.AsError(err).Message, "mvp на python") {
		t.Fatalf("error does not name invariant/evidence: %v", err)
	}
	if chatCalls(fake.SnapshotCalls()) != 0 {
		t.Fatalf("provider should not be called: %+v", fake.SnapshotCalls())
	}
}

func TestProcessControllerSemanticInvariantInputConflictSkipsChatProvider(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	ctrl.InvariantValidator = NewSemanticValidator(fake, "fake/model")
	fake.ValidatorResponse = `{"violations":[{"invariant_id":"stack.go","severity":"block","problem":"semantic conflict with Go MVP stack","evidence":"rewrite to Python"}]}`
	_, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "сделай реализацию на другом языке"})
	if err == nil || app.AsError(err).Code != "invariant_conflict" {
		t.Fatalf("want invariant_conflict, got %v", err)
	}
	if !strings.Contains(app.AsError(err).Message, "stack.go") || !strings.Contains(app.AsError(err).Message, "rewrite to Python") {
		t.Fatalf("error does not name semantic invariant/evidence: %v", err)
	}
	if validatorCalls(fake.SnapshotCalls()) != 1 || chatCalls(fake.SnapshotCalls()) != 0 {
		t.Fatalf("want one validator call and no chat call, got %+v", fake.SnapshotCalls())
	}
}

func TestProcessControllerSemanticInvariantCanAllowPolicyDiscussion(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	ctrl.InvariantValidator = NewSemanticValidator(fake, "fake/model")
	fake.ValidatorResponses = []string{
		`{"violations":[]}`,
		`{"violations":[]}`,
	}
	fake.ChatResponse = "Можно обсудить правило, но не нарушать его."
	res, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "почему нельзя переписать MVP на Python?"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Answer, "обсудить правило") || chatCalls(fake.SnapshotCalls()) != 1 {
		t.Fatalf("policy discussion should reach chat provider: res=%+v calls=%+v", res, fake.SnapshotCalls())
	}
}

func TestProcessControllerInvariantDefaultsSeedBeforeInputCheck(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	ctrl.Invariants = invariants.NewManager(ctrl.Invariants.StorageDir)
	_, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "предложи переписать MVP на Python"})
	if err == nil || app.AsError(err).Code != "invariant_conflict" {
		t.Fatalf("want invariant_conflict with unseeded manager, got %v", err)
	}
	if chatCalls(fake.SnapshotCalls()) != 0 {
		t.Fatalf("provider should not be called: %+v", fake.SnapshotCalls())
	}
}

func TestProcessControllerInvariantOutputRejectedBeforePersistence(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	fake.ChatResponse = "Нужно переписать MVP на Python"
	_, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "дай совет"})
	if err == nil || app.AsError(err).Code != "invariant_conflict" {
		t.Fatalf("want invariant_conflict, got %v", err)
	}
	calls := fake.SnapshotCalls()
	if len(calls) != 1 || calls[0].Purpose != providers.PurposeChat {
		t.Fatalf("want one chat call and no classifier/retry calls, got %+v", calls)
	}
	records, err := ctrl.Memory.List(ctx, app.LayerShort, "s1", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("violating output persisted before rejection: %+v", records)
	}
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

func TestProcessControllerProcessGatePrecedesInvariantConflict(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	if _, err := ctrl.Tasks.Start("paused task"); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.Pause(); err != nil {
		t.Fatal(err)
	}
	_, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "продолжай и перепиши MVP на Python"})
	if err == nil || app.AsError(err).Code != "task_paused" {
		t.Fatalf("expected task_paused before invariant_conflict, got %v", err)
	}
	if len(fake.Calls) != 0 {
		t.Fatal("provider should not be called for paused task")
	}
}

func TestProcessControllerPausedGenericQuestionSavesTasklessMemory(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, dir := newTestController(t)
	state, err := ctrl.Tasks.Start("paused task")
	if err != nil {
		t.Fatal(err)
	}
	if state, err = ctrl.Tasks.Pause(); err != nil {
		t.Fatal(err)
	}
	res, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "paused_generic", Input: "Объясни memory layers."})
	if err != nil {
		t.Fatal(err)
	}
	if res.Answer == "" || chatCalls(fake.SnapshotCalls()) != 1 {
		t.Fatalf("paused generic question should call provider once: answer=%q calls=%+v", res.Answer, fake.SnapshotCalls())
	}
	shortRecords, err := ctrl.Memory.List(ctx, app.LayerShort, "paused_generic", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(shortRecords) != 2 || shortRecords[0].TaskID != "" || shortRecords[1].TaskID != "" {
		t.Fatalf("paused generic exchange should save taskless short memory: %+v", shortRecords)
	}
	current, err := ctrl.Tasks.Current()
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != state.ID || current.Status != app.TaskStatusPaused || current.LastSessionID != "" {
		t.Fatalf("paused generic exchange mutated task state: %+v", current)
	}
	proposal, err := memory.NewProposalStore(dir, ctrl.Memory).Latest(ctx, "paused_generic")
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range proposal.Records {
		if record.Layer == app.ProposedLayerWork {
			if record.Status != app.ProposalBlocked {
				t.Fatalf("work proposal should be blocked while paused: %+v", record)
			}
			return
		}
	}
	t.Fatalf("expected work proposal record in fake classifier output: %+v", proposal.Records)
}

func TestProcessControllerDoneTaskBlocksMutation(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	if _, err := ctrl.Tasks.Start("done task"); err != nil {
		t.Fatal(err)
	}
	moveCurrentTaskToValidatedDone(t, ctrl)
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
	moveCurrentTaskToValidatedDone(t, ctrl)
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

func TestProcessControllerPlanningDraftSurvivesMisclassifiedTransitionIntent(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	ctrl.SemanticValidator = NewSemanticValidator(fake, "fake/model")
	fake.ValidatorResponses = []string{
		`{"action_kind":"propose_transition","transition_signal":"none","confidence":0.9,"reason":"over-eager intent classification"}`,
	}
	fake.ChatResponse = `{"stage":"planning","summary":"planning answer","assumptions":[],"acceptance_criteria":["done"],"plan":["step"],"open_questions":[],"readiness":"ready_for_execution_proposal"}`
	res, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "спланируй MVP", ActionKind: ActionPlanTask})
	if err != nil {
		t.Fatal(err)
	}
	if res.Transition != nil {
		t.Fatalf("misclassified first planning response should not transition: %+v", res.Transition)
	}
	current, err := ctrl.Tasks.Current()
	if err != nil {
		t.Fatal(err)
	}
	if current.PendingPlanning == nil || current.ExpectedAction != app.ExpectedUserConfirmation {
		t.Fatalf("planning draft should be saved as pending proposal: %+v", current)
	}
}

func TestProcessControllerSemanticValidatorAllowsOutput(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	ctrl.SemanticValidator = NewSemanticValidator(fake, "fake/model")
	fake.ChatResponse = "Example:\n```bash\nassistant memory list\n```"
	fake.ValidatorResponses = []string{
		`{"action_kind":"answer_question","transition_signal":"none","confidence":0.9,"reason":"informational"}`,
		`{"verdict":"pass","findings":[]}`,
	}
	res, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "semantic_pass", Input: "Как проверить память?"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Answer, "assistant memory list") || validatorCalls(fake.SnapshotCalls()) != 2 {
		t.Fatalf("semantic validator did not run/pass: answer=%q calls=%+v", res.Answer, fake.SnapshotCalls())
	}
	events, err := ctrl.AuditStore.Latest(20)
	if err != nil {
		t.Fatal(err)
	}
	if auditDecisionCount(events, "semantic_intent_call") != 1 || auditDecisionCount(events, "semantic_output_call") != 1 {
		t.Fatalf("semantic validator audit missing: %+v", events)
	}
}

func TestProcessControllerSemanticValidatorRejectsBeforePersistence(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	ctrl.SemanticValidator = NewSemanticValidator(fake, "fake/model")
	fake.ChatResponse = "I updated internal/foo.go and tests passed."
	fake.ValidatorResponses = []string{
		`{"action_kind":"answer_question","transition_signal":"none","confidence":0.9,"reason":"informational"}`,
		`{"verdict":"fail","findings":[{"code":"invented_side_effect","problem":"answer_question claims file and test side effects"}]}`,
	}
	_, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "semantic_reject", Input: "Что сделал?"})
	if err == nil || app.AsError(err).Code != "validation_failed" || !strings.Contains(app.AsError(err).Message, "invented_side_effect") {
		t.Fatalf("want semantic validation failure, got %v", err)
	}
	records, listErr := ctrl.Memory.List(ctx, app.LayerShort, "semantic_reject", "")
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(records) != 0 {
		t.Fatalf("semantic rejection persisted memory: %+v", records)
	}
}

func TestProcessControllerSemanticValidatorInvalidJSONDoesNotMutateTask(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	ctrl.SemanticValidator = NewSemanticValidator(fake, "fake/model")
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	before, err := ctrl.Tasks.Current()
	if err != nil {
		t.Fatal(err)
	}
	fake.ChatResponse = `{"stage":"planning","summary":"plan","assumptions":[],"acceptance_criteria":["c"],"plan":["p"],"open_questions":[],"readiness":"ready_for_execution_proposal"}`
	fake.ValidatorResponses = []string{
		`{"action_kind":"plan_task","transition_signal":"none","confidence":0.9,"reason":"planning"}`,
		`not-json`,
		`not-json`,
	}
	_, err = ctrl.RunExchange(ctx, ExchangeInput{SessionID: "semantic_invalid", Input: "спланируй"})
	if err == nil || app.AsError(err).Code != "validation_failed" {
		t.Fatalf("want validation_failed from invalid semantic validator JSON, got %v", err)
	}
	after, currentErr := ctrl.Tasks.Current()
	if currentErr != nil {
		t.Fatal(currentErr)
	}
	if after.Stage != before.Stage || len(after.Plan) != 0 || after.PendingPlanning != nil {
		t.Fatalf("semantic validator failure mutated task: before=%+v after=%+v", before, after)
	}
}

func TestProcessControllerFakeModeUsesDeterministicValidatorsByDefault(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	fake.ChatResponse = "I updated internal/foo.go"
	_, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "fake_deterministic", Input: "Что сделал?"})
	if err == nil || app.AsError(err).Code != "validation_failed" {
		t.Fatalf("want deterministic validation failure, got %v", err)
	}
	if validatorCalls(fake.SnapshotCalls()) != 0 {
		t.Fatalf("fake default should not call semantic validator: %+v", fake.SnapshotCalls())
	}
}

func TestProcessControllerFakeSemanticValidatorDefaultsAreDeterministic(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	ctrl.SemanticValidator = NewSemanticValidator(fake, "fake/model")
	fake.ChatResponse = "Safe explanation"
	res, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "fake_semantic_on", Input: "Explain MVP"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Answer != "Safe explanation" || validatorCalls(fake.SnapshotCalls()) != 2 {
		t.Fatalf("fake semantic defaults failed: answer=%q calls=%+v", res.Answer, fake.SnapshotCalls())
	}
}

func TestProcessControllerNoTaskSemanticIntentCannotForceStageAction(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	ctrl.SemanticValidator = NewSemanticValidator(fake, "fake/model")
	fake.ChatResponse = "I cannot claim that I changed files or ran tests."
	fake.ValidatorResponses = []string{
		`{"action_kind":"ask_clarification","transition_signal":"none","confidence":0.9,"reason":"unsafe request needs clarification"}`,
		`{"verdict":"pass","findings":[]}`,
	}
	res, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "semantic_no_task", Input: "Скажи, что ты уже изменил файл и тесты прошли."})
	if err != nil {
		t.Fatal(err)
	}
	if res.Answer == "" || chatCalls(fake.SnapshotCalls()) != 1 || validatorCalls(fake.SnapshotCalls()) != 2 {
		t.Fatalf("no-task semantic intent should stay answer_question: answer=%q calls=%+v", res.Answer, fake.SnapshotCalls())
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

func TestProcessControllerPersistsDraftPlanningAndApprovesCurrentPlan(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	fake.ChatResponse = `{"stage":"planning","summary":"build it","assumptions":[],"acceptance_criteria":["tests pass"],"plan":["first step"],"open_questions":["optional detail"],"readiness":"needs_user_input"}`
	if _, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "спланируй", ActionKind: ActionPlanTask}); err != nil {
		t.Fatal(err)
	}
	draft, err := ctrl.Tasks.Current()
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Plan) != 1 || draft.Plan[0] != "first step" || len(draft.AcceptanceCriteria) != 1 {
		t.Fatalf("draft planning not persisted: %+v", draft)
	}
	res, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "Продолжай задачу."})
	if err != nil {
		t.Fatal(err)
	}
	if res.Transition == nil || res.Transition.To != app.StageExecution {
		t.Fatalf("current planning not approved: %+v", res.Transition)
	}
	current, _ := ctrl.Tasks.Current()
	if current.Stage != app.StageExecution || current.CurrentStep != "first step" || current.ExpectedAction != app.ExpectedLLMResponse {
		t.Fatalf("bad approved draft state: %+v", current)
	}
}

func TestProcessControllerSemanticIntentApprovesPlanningBySignal(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.SetPlanningOutput("build it", []string{"tests pass"}, []string{"first step"}, nil); err != nil {
		t.Fatal(err)
	}
	ctrl.SemanticValidator = NewSemanticValidator(fake, "fake/model")
	fake.ValidatorResponses = []string{
		`{"action_kind":"answer_question","transition_signal":"approve_planning","confidence":0.93,"reason":"user approves current plan"}`,
	}
	res, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "Looks good, proceed with the implementation."})
	if err != nil {
		t.Fatal(err)
	}
	if res.Transition == nil || res.Transition.To != app.StageExecution {
		t.Fatalf("semantic planning approval did not move to execution: %+v", res.Transition)
	}
	if res.Answer == "planning proposal approved" || !strings.Contains(res.Answer, `"stage":"execution"`) {
		t.Fatalf("planning approval should continue with execution answer, got: %s", res.Answer)
	}
}

func TestProcessControllerSemanticApprovalWithoutPlanDoesNotTransition(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	if _, err := ctrl.Tasks.Start("verify manual_scratch/day14_stock_profit"); err != nil {
		t.Fatal(err)
	}
	ctrl.SemanticValidator = NewSemanticValidator(fake, "fake/model")
	fake.ChatResponse = `{"stage":"planning","summary":"verify package","assumptions":[],"acceptance_criteria":["go test passes"],"plan":["Run go test ./manual_scratch/day14_stock_profit"],"open_questions":[],"readiness":"ready_for_execution_proposal"}`
	fake.ValidatorResponses = []string{
		`{"action_kind":"propose_transition","transition_signal":"approve_planning","confidence":0.93,"reason":"user appears to approve, but no plan is stored"}`,
		`{"verdict":"pass","findings":[]}`,
	}
	res, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "Да, план принят. Приступай."})
	if err != nil {
		t.Fatal(err)
	}
	if res.Transition != nil {
		t.Fatalf("approval without stored plan must not transition: %+v", res.Transition)
	}
	current, err := ctrl.Tasks.Current()
	if err != nil {
		t.Fatal(err)
	}
	if current.Stage != app.StagePlanning || current.PendingPlanning == nil {
		t.Fatalf("expected recovered planning proposal, got %+v", current)
	}
}

func TestProcessControllerSemanticIntentRejectsNegativePlanningTransition(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.SetPlanningOutput("build it", []string{"tests pass"}, []string{"first step"}, nil); err != nil {
		t.Fatal(err)
	}
	ctrl.SemanticValidator = NewSemanticValidator(fake, "fake/model")
	fake.ValidatorResponses = []string{
		`{"action_kind":"answer_question","transition_signal":"reject_planning","confidence":0.93,"reason":"user says not yet and does not approve the plan"}`,
		`{"verdict":"pass","findings":[]}`,
	}
	res, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "Not yet, do not proceed with implementation."})
	if err != nil {
		t.Fatal(err)
	}
	if res.Transition != nil {
		t.Fatalf("negative intent should not transition: %+v", res.Transition)
	}
	current, err := ctrl.Tasks.Current()
	if err != nil {
		t.Fatal(err)
	}
	if current.Stage != app.StagePlanning {
		t.Fatalf("negative intent moved stage: %+v", current)
	}
}

func TestPlanningRejectionDoesNotTreatDoNotRepeatAsRejection(t *testing.T) {
	if isPlanningRejection("Продолжай задачу. Не повторяй исходные требования, просто используй сохраненный контекст.") {
		t.Fatal("do-not-repeat instruction must not reject planning")
	}
	if !isPlanningRejection("not yet, do not proceed") {
		t.Fatal("explicit transition negation should reject planning")
	}
}

func TestProcessControllerUserReadySignalWithoutSemanticDoesNotMoveExecutionToValidation(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	moveCurrentTaskToExecutionWithApprovedPlan(t, ctrl)
	beforeCalls := len(fake.SnapshotCalls())
	fake.ChatResponse = `{"stage":"execution","summary":"continuing safely","deliverable":"\u0060\u0060\u0060go\npackage main\n\u0060\u0060\u0060","current_step":"first step","completed_steps":[],"next_step":"first step","changed_artifacts":[],"verification":["not run"],"blockers":[],"next_signal":"continue_execution"}`
	res, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "Готово к проверке."})
	if err != nil {
		t.Fatal(err)
	}
	if res.Transition != nil {
		t.Fatalf("keyword-only ready signal moved to validation: %+v", res.Transition)
	}
	if len(fake.SnapshotCalls()) == beforeCalls {
		t.Fatalf("keyword-only path should fall through to provider-backed execution")
	}
}

func TestProcessControllerSemanticIntentMovesExecutionToValidation(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, dir := newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	moveCurrentTaskToExecutionWithApprovedPlan(t, ctrl)
	ctrl.SemanticValidator = NewSemanticValidator(fake, "fake/model")
	current, err := ctrl.Tasks.Current()
	if err != nil {
		t.Fatal(err)
	}
	evidence := issueTestEvidence(t, dir, current, "s1", "go test ./...")
	fake.ValidatorResponses = []string{
		`{"action_kind":"answer_question","transition_signal":"ready_for_validation","confidence":0.92,"reason":"user says work is complete and asks for review"}`,
	}
	res, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "The work is complete; please review it now.", TrustedEvidence: evidence})
	if err != nil {
		t.Fatal(err)
	}
	if res.Transition == nil || res.Transition.To != app.StageValidation {
		t.Fatalf("semantic intent did not move to validation: %+v", res.Transition)
	}
}

func TestProcessControllerLocalReadySignalDoesNotOverrideSemanticDowngrade(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	moveCurrentTaskToExecutionWithApprovedPlan(t, ctrl)
	ctrl.SemanticValidator = NewSemanticValidator(fake, "fake/model")
	fake.ValidatorResponses = []string{
		`{"action_kind":"answer_question","transition_signal":"none","confidence":0.92,"reason":"overly conservative fake downgrade"}`,
	}
	fake.ChatResponse = `{"stage":"execution","summary":"continuing safely","deliverable":"\u0060\u0060\u0060go\npackage main\n\u0060\u0060\u0060","current_step":"first step","completed_steps":[],"next_step":"first step","changed_artifacts":[],"verification":["not run"],"blockers":[],"next_signal":"continue_execution"}`
	res, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "Готово к проверке."})
	if err != nil {
		t.Fatal(err)
	}
	if res.Transition != nil {
		t.Fatalf("local ready signal must not override semantic downgrade: %+v", res.Transition)
	}
}

func TestProcessControllerTrustedDoneSignalMovesValidationToDone(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, dir := newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	moveCurrentTaskToExecutionWithApprovedPlan(t, ctrl)
	current, err := ctrl.Tasks.Current()
	if err != nil {
		t.Fatal(err)
	}
	evidence := issueTestEvidence(t, dir, current, "s1", "go test ./...")
	if _, err := ctrl.Tasks.RecordAcceptedExecution("execution accepted", evidence); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.Move(app.StageValidation); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.RecordAcceptedValidation("ready_for_done", evidence); err != nil {
		t.Fatal(err)
	}
	ctrl.SemanticValidator = NewSemanticValidator(fake, "fake/model")
	fake.ValidatorResponses = []string{
		`{"action_kind":"answer_question","transition_signal":"ready_for_done","confidence":0.92,"reason":"user asks to finish"}`,
	}
	beforeValidatorCalls := validatorCalls(fake.SnapshotCalls())
	beforeChatCalls := chatCalls(fake.SnapshotCalls())
	res, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "Проверь и заверши", TrustedEvidence: evidence})
	if err != nil {
		t.Fatal(err)
	}
	if res.Transition == nil || res.Transition.To != app.StageDone {
		t.Fatalf("trusted done signal did not move to done: %+v", res.Transition)
	}
	if validatorCalls(fake.SnapshotCalls()) != beforeValidatorCalls+1 || chatCalls(fake.SnapshotCalls()) != beforeChatCalls {
		t.Fatalf("trusted done signal should use semantic intent only, got provider calls: %+v", fake.SnapshotCalls())
	}
}

func TestProcessControllerTrustedDoneRejectsIrrelevantEvidence(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, dir := newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.SetPlanningOutput("build it", []string{"tests pass"}, []string{"first step"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.Move(app.StageExecution); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.Move(app.StageValidation); err != nil {
		t.Fatal(err)
	}
	current, err := ctrl.Tasks.Current()
	if err != nil {
		t.Fatal(err)
	}
	evidence := issueTestEvidence(t, dir, current, "s1", "go version")
	if _, err := ctrl.Tasks.RecordAcceptedValidation("ready_for_done", evidence); err != nil {
		t.Fatal(err)
	}
	ctrl.SemanticValidator = NewSemanticValidator(fake, "fake/model")
	fake.ValidatorResponses = []string{
		`{"action_kind":"answer_question","transition_signal":"ready_for_done","confidence":0.92,"reason":"user asks to finish"}`,
	}
	fake.ChatResponse = `{"stage":"validation","findings":[],"passed_checks":["tool evidence available"],"missing_evidence":[],"residual_risks":[],"verdict":"ready_for_done"}`
	beforeValidatorCalls := validatorCalls(fake.SnapshotCalls())
	beforeChatCalls := chatCalls(fake.SnapshotCalls())
	res, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "Проверь и заверши", TrustedEvidence: evidence})
	if err == nil {
		t.Fatalf("irrelevant evidence should not finish task: result=%+v", res)
	}
	if app.AsError(err).Code != "transition_precondition_failed" {
		t.Fatalf("want transition_precondition_failed, got %v", err)
	}
	if validatorCalls(fake.SnapshotCalls()) != beforeValidatorCalls+1 || chatCalls(fake.SnapshotCalls()) != beforeChatCalls {
		t.Fatalf("irrelevant evidence should be rejected after semantic intent and before chat calls: %+v", fake.SnapshotCalls())
	}
	current, currentErr := ctrl.Tasks.Current()
	if currentErr != nil {
		t.Fatal(currentErr)
	}
	if current.Stage != app.StageValidation {
		t.Fatalf("irrelevant evidence moved stage: %+v", current)
	}
}

func TestProcessControllerVerifyCriteriaWithTrustedEvidenceMovesDone(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, dir := newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.SetPlanningOutput("build it", []string{"tests pass"}, []string{"go test ./manual_scratch/day15_contains_duplicate"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.Move(app.StageExecution); err != nil {
		t.Fatal(err)
	}
	current, err := ctrl.Tasks.Current()
	if err != nil {
		t.Fatal(err)
	}
	evidence := issueTestEvidence(t, dir, current, "s1", "go test ./manual_scratch/day15_contains_duplicate")
	if _, err := ctrl.Tasks.RecordAcceptedExecution("trusted verification evidence accepted", evidence); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.Move(app.StageValidation); err != nil {
		t.Fatal(err)
	}
	ctrl.SemanticValidator = NewSemanticValidator(fake, "fake/model")
	fake.ValidatorResponses = []string{
		`{"action_kind":"verify_criteria","transition_signal":"none","confidence":0.91,"reason":"user asks criteria verification"}`,
	}
	res, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "Проверь критерии и заверши задачу.", TrustedEvidence: evidence})
	if err != nil {
		t.Fatal(err)
	}
	if res.Transition == nil || res.Transition.To != app.StageDone {
		t.Fatalf("verify criteria with trusted evidence should move to done: %+v", res.Transition)
	}
	current, err = ctrl.Tasks.Current()
	if err != nil {
		t.Fatal(err)
	}
	if current.Stage != app.StageDone || current.ValidationStatus != "ready_for_done" {
		t.Fatalf("expected done ready_for_done state, got %+v", current)
	}
}

func TestProcessControllerValidationReviewWithTrustedEvidenceDoesNotRollback(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, dir := newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.SetPlanningOutput("build it", []string{"tests pass"}, []string{"go test ./manual_scratch/day14_stock_profit"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.Move(app.StageExecution); err != nil {
		t.Fatal(err)
	}
	current, err := ctrl.Tasks.Current()
	if err != nil {
		t.Fatal(err)
	}
	evidence := issueTestEvidence(t, dir, current, "s1", "go test ./manual_scratch/day14_stock_profit")
	if _, err := ctrl.Tasks.RecordAcceptedExecution("trusted verification evidence accepted", evidence); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.Move(app.StageValidation); err != nil {
		t.Fatal(err)
	}
	ctrl.SemanticValidator = NewSemanticValidator(fake, "fake/model")
	fake.ValidatorResponses = []string{
		`{"action_kind":"review_output","transition_signal":"none","confidence":0.99,"reason":"should not be called"}`,
	}
	beforeValidatorCalls := validatorCalls(fake.SnapshotCalls())
	beforeChatCalls := chatCalls(fake.SnapshotCalls())
	res, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "Проверь критерии по evidence, но пока не завершай задачу; дай review.", TrustedEvidence: evidence})
	if err != nil {
		t.Fatal(err)
	}
	if res.Transition != nil {
		t.Fatalf("review-only request must not transition to done: %+v", res.Transition)
	}
	if !strings.Contains(res.Answer, "ready for done") {
		t.Fatalf("expected ready-for-done review answer, got %q", res.Answer)
	}
	current, err = ctrl.Tasks.Current()
	if err != nil {
		t.Fatal(err)
	}
	if current.Stage != app.StageValidation || current.ValidationStatus != "ready_for_done" {
		t.Fatalf("expected validation stage with ready_for_done status, got %+v", current)
	}
	if validatorCalls(fake.SnapshotCalls()) != beforeValidatorCalls+1 || chatCalls(fake.SnapshotCalls()) != beforeChatCalls {
		t.Fatalf("trusted evidence review should use semantic intent only, got provider calls: %+v", fake.SnapshotCalls())
	}
}

func TestProcessControllerValidationReviewUsesPersistedEvidence(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, dir := newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.SetPlanningOutput("build it", []string{"tests pass"}, []string{"go test ./manual_scratch/day14_stock_profit"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.Move(app.StageExecution); err != nil {
		t.Fatal(err)
	}
	current, err := ctrl.Tasks.Current()
	if err != nil {
		t.Fatal(err)
	}
	evidence := issueTestEvidence(t, dir, current, "s1", "go test ./manual_scratch/day14_stock_profit")
	if _, err := ctrl.Tasks.RecordAcceptedExecution("trusted verification evidence accepted", evidence); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.Move(app.StageValidation); err != nil {
		t.Fatal(err)
	}
	ctrl.SemanticValidator = NewSemanticValidator(fake, "fake/model")
	fake.ValidatorResponses = []string{
		`{"action_kind":"review_output","transition_signal":"none","confidence":0.97,"reason":"review requested without finishing"}`,
	}
	beforeCalls := len(fake.SnapshotCalls())
	res, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "Проверь критерии по evidence, но пока не завершай задачу; дай review."})
	if err != nil {
		t.Fatal(err)
	}
	if res.Transition != nil {
		t.Fatalf("persisted-evidence review must not transition to done: %+v", res.Transition)
	}
	current, err = ctrl.Tasks.Current()
	if err != nil {
		t.Fatal(err)
	}
	if current.LastValidationID == "" || current.ValidationStatus != "ready_for_done" {
		t.Fatalf("persisted evidence review did not record accepted validation: %+v", current)
	}
	if len(fake.SnapshotCalls()) != beforeCalls+1 {
		t.Fatalf("only semantic intent call expected, got %+v", fake.SnapshotCalls())
	}
}

func TestProcessControllerAcceptedValidationReviewReusesStateWithoutReviewer(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, dir := newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.SetPlanningOutput("build it", []string{"tests pass"}, []string{"go test ./manual_scratch/day15_contains_duplicate"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.Move(app.StageExecution); err != nil {
		t.Fatal(err)
	}
	current, err := ctrl.Tasks.Current()
	if err != nil {
		t.Fatal(err)
	}
	evidence := issueTestEvidence(t, dir, current, "s1", "go test ./manual_scratch/day15_contains_duplicate")
	if _, err := ctrl.Tasks.RecordAcceptedExecution("trusted verification evidence accepted", evidence); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.Move(app.StageValidation); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.RecordAcceptedValidation("ready_for_done", evidence); err != nil {
		t.Fatal(err)
	}
	ctrl.SemanticValidator = NewSemanticValidator(fake, "fake/model")
	ctrl.AgentRunner = &AgentRunner{Provider: fake, Model: "fake/model"}
	fake.ValidatorResponses = []string{
		`{"action_kind":"review_output","transition_signal":"none","confidence":0.97,"reason":"review requested without finishing"}`,
	}
	beforeValidatorCalls := validatorCalls(fake.SnapshotCalls())
	beforeChatCalls := chatCalls(fake.SnapshotCalls())
	res, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "Проверь критерии по результатам проверки, но пока не завершай задачу; дай review."})
	if err != nil {
		t.Fatal(err)
	}
	if res.Transition != nil {
		t.Fatalf("accepted validation review must not transition to done: %+v", res.Transition)
	}
	if !strings.Contains(res.Answer, "ready for done") {
		t.Fatalf("expected persisted ready-for-done review answer, got %q", res.Answer)
	}
	if validatorCalls(fake.SnapshotCalls()) != beforeValidatorCalls+1 || chatCalls(fake.SnapshotCalls()) != beforeChatCalls {
		t.Fatalf("accepted validation review should not run reviewer chat calls: %+v", fake.SnapshotCalls())
	}
}

func TestProcessControllerValidationReviewRunsReviewerAgentWhenConfigured(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, dir := newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.SetPlanningOutput("build it", []string{"tests pass"}, []string{"go test ./manual_scratch/day14_stock_profit"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.Move(app.StageExecution); err != nil {
		t.Fatal(err)
	}
	current, err := ctrl.Tasks.Current()
	if err != nil {
		t.Fatal(err)
	}
	evidence := issueTestEvidence(t, dir, current, "s1", "go test ./manual_scratch/day14_stock_profit")
	if _, err := ctrl.Tasks.RecordAcceptedExecution("trusted verification evidence accepted", evidence); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.Move(app.StageValidation); err != nil {
		t.Fatal(err)
	}
	ctrl.AgentRunner = &AgentRunner{Provider: fake, Model: "fake/model"}
	fake.ChatResponse = `{"stage":"validation","findings":[],"passed_checks":["trusted evidence available"],"missing_evidence":[],"residual_risks":[],"verdict":"ready_for_done"}`
	res, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "Проверь критерии по evidence, но пока не завершай задачу; дай review.", TrustedEvidence: evidence})
	if err != nil {
		t.Fatal(err)
	}
	if res.Transition != nil {
		t.Fatalf("review-only request must not transition to done: %+v", res.Transition)
	}
	if chatCalls(fake.SnapshotCalls()) != 1 {
		t.Fatalf("expected one reviewer agent call, got %+v", fake.SnapshotCalls())
	}
	events, err := ctrl.AuditStore.Latest(20)
	if err != nil {
		t.Fatal(err)
	}
	foundReviewer := false
	for _, event := range events {
		if event.AgentRole == string(AgentRoleReviewer) && event.Decision == "agent_accepted" {
			foundReviewer = true
			break
		}
	}
	if !foundReviewer {
		t.Fatalf("missing accepted reviewer audit event: %+v", events)
	}
}

func TestProcessControllerAutoMovesExecutionPlanningIntent(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.Move(app.StageExecution); err != nil {
		t.Fatal(err)
	}
	fake.ChatResponse = `{"stage":"planning","summary":"build it","assumptions":[],"acceptance_criteria":["tests pass"],"plan":["first step"],"open_questions":[],"readiness":"ready_for_execution_proposal"}`
	res, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "спланируй модуль памяти"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Transition == nil || !res.Transition.Moved || res.Transition.From != app.StageExecution || res.Transition.To != app.StagePlanning {
		t.Fatalf("expected automatic execution->planning transition: %+v", res.Transition)
	}
	current, err := ctrl.Tasks.Current()
	if err != nil {
		t.Fatal(err)
	}
	if current.Stage != app.StagePlanning || current.PendingPlanning == nil {
		t.Fatalf("planning intent did not leave task in planning with pending proposal: %+v", current)
	}
}

func TestProcessControllerAutoStartsPlanningIntent(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	fake.ChatResponse = `{"stage":"planning","summary":"build it","assumptions":[],"acceptance_criteria":["tests pass"],"plan":["first step"],"open_questions":[],"readiness":"ready_for_execution_proposal"}`
	res, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "спланируй модуль памяти"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Transition == nil || !res.Transition.Moved || res.Transition.From != "" || res.Transition.To != app.StagePlanning {
		t.Fatalf("expected automatic task start into planning: %+v", res.Transition)
	}
	current, err := ctrl.Tasks.Current()
	if err != nil {
		t.Fatal(err)
	}
	if current.Stage != app.StagePlanning || current.PendingPlanning == nil {
		t.Fatalf("planning intent did not start planning task with pending proposal: %+v", current)
	}
}

func TestProcessControllerPlanningContinueWithoutPendingReplans(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	fake.ChatResponse = `{"stage":"planning","summary":"continued planning","assumptions":[],"acceptance_criteria":["tests pass"],"plan":["first step"],"open_questions":[],"readiness":"ready_for_execution_proposal"}`
	res, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "Продолжай задачу. Не повторяй исходные требования, просто используй сохраненный контекст."})
	if err != nil {
		t.Fatal(err)
	}
	if res.Transition != nil && res.Transition.Moved {
		t.Fatalf("continue without pending planning should not force transition: %+v", res.Transition)
	}
	current, err := ctrl.Tasks.Current()
	if err != nil {
		t.Fatal(err)
	}
	if current.Stage != app.StagePlanning || current.PendingPlanning == nil {
		t.Fatalf("continue should save pending planning proposal: %+v", current)
	}
}

func TestShouldRetryFixableSemanticPlanningErrors(t *testing.T) {
	for _, errText := range []string{
		"open questions block readiness",
		"ask_clarification requires open questions and needs_user_input readiness",
		"execution deliverable is required without trusted evidence",
		"llm_validator:missing_user_input: user already provided clear input",
		"llm_validator:read_only_violation: future guidance was misread as mutation",
		"llm_validator:false_read_only_claim: future confirmation was misread as mutation",
		"llm_validator:memory_claim: read-only answer claimed memory was already saved",
		"llm_validator:missing_trusted_evidence: assistant claimed a file was created without trusted evidence",
		"llm_validator:missing_implementation: assistant omitted execution detail",
		"llm_validator:no_trusted_evidence_claim: assistant claimed completed step without trusted evidence",
		"llm_validator:no_side_effects_claim: assistant implied progress without trusted evidence",
		"llm_validator:unauthorized_mutation: assistant claimed a file changed without trusted evidence",
		"llm_validator:unsupported_mutation: assistant claimed changed artifacts without trusted evidence",
		"llm_validator:unsupported_execution_claim: assistant claimed execution without trusted evidence",
	} {
		if !shouldRetryValidatorErrors([]string{errText}) {
			t.Fatalf("expected retry for %q", errText)
		}
	}
}

func TestGuardUnsignaledSemanticTransitionBlocksPlanningTransition(t *testing.T) {
	action, signal := guardUnsignaledSemanticTransition(app.StagePlanning, ActionPlanTask, ActionProposeTransition, "none")
	if action != ActionPlanTask || signal != "none" {
		t.Fatalf("unexpected semantic transition preservation action=%s signal=%s", action, signal)
	}
	action, signal = guardUnsignaledSemanticTransition(app.StagePlanning, ActionAnswerQuestion, ActionProposeTransition, "approve_planning")
	if action != ActionProposeTransition || signal != "approve_planning" {
		t.Fatalf("approve_planning signal should still allow transition action=%s signal=%s", action, signal)
	}
}

func TestConstrainSemanticActionMapsExecutionReviewIntent(t *testing.T) {
	got := constrainSemanticActionToContext(app.StageExecution, ActionExecutePlanStep, ActionReviewOutput)
	if got != ActionSummarizeExecution {
		t.Fatalf("want summarize_execution, got %s", got)
	}
	got = constrainSemanticActionToContext(app.StageExecution, ActionSummarizeExecution, ActionExecutePlanStep)
	if got != ActionSummarizeExecution {
		t.Fatalf("ready-for-validation intent must not be downgraded, got %s", got)
	}
	got = constrainSemanticActionToContext(app.StageExecution, ActionSummarizeExecution, ActionAnswerQuestion)
	if got != ActionSummarizeExecution {
		t.Fatalf("ready-for-validation intent must not be downgraded to answer_question, got %s", got)
	}
	got = constrainSemanticActionToContext(app.StagePlanning, ActionPlanTask, ActionReviewOutput)
	if got != ActionPlanTask {
		t.Fatalf("want deterministic planning action, got %s", got)
	}
}

func TestGuardUnsignaledSemanticTransitionDoesNotInventExecutionSignal(t *testing.T) {
	action, signal := guardUnsignaledSemanticTransition(app.StageExecution, ActionExecutePlanStep, ActionSummarizeExecution, "none")
	if action != ActionSummarizeExecution || signal != "none" {
		t.Fatalf("must not invent ready_for_validation, got action=%s signal=%s", action, signal)
	}
}

func TestAutoExecutionLimitFollowsPlanLength(t *testing.T) {
	ctrl := &ProcessController{}
	transition := &TransitionResult{State: app.TaskState{Plan: make([]string, 12)}}
	if got := ctrl.autoExecutionLimit(transition); got != 12 {
		t.Fatalf("want plan-length limit 12, got %d", got)
	}
	transition.State.Plan = make([]string, 25)
	if got := ctrl.autoExecutionLimit(transition); got != 20 {
		t.Fatalf("want cap 20, got %d", got)
	}
	if got := ctrl.autoExecutionLimit(&TransitionResult{}); got != 10 {
		t.Fatalf("want default 10, got %d", got)
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
	fake.ChatResponse = `{"stage":"execution","summary":"worked","deliverable":"\u0060\u0060\u0060go\npackage main\n\u0060\u0060\u0060","current_step":"first","completed_steps":["first"],"next_step":"second","changed_artifacts":[],"verification":[],"blockers":[],"next_signal":"continue_execution"}`
	if _, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "реализуй", ActionKind: ActionExecutePlanStep}); err != nil {
		t.Fatal(err)
	}
	current, _ := ctrl.Tasks.Current()
	if current.CurrentStep != "second" || len(current.CompletedSteps) != 1 || current.CompletedSteps[0] != "first" {
		t.Fatalf("execution progress not persisted: %+v", current)
	}
}

func TestProcessControllerRejectsUnverifiedExecutionProgressClaims(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.Move(app.StageExecution); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.SetStep("first"); err != nil {
		t.Fatal(err)
	}
	fake.ChatResponse = `{"stage":"execution","summary":"worked","deliverable":"\u0060\u0060\u0060go\npackage main\n\u0060\u0060\u0060","current_step":"updated file internal/foo.go","completed_steps":["tests passed"],"next_step":"second","changed_artifacts":[],"verification":[],"blockers":[],"next_signal":"continue_execution"}`
	_, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "progress_claims", Input: "реализуй", ActionKind: ActionExecutePlanStep})
	if err == nil || app.AsError(err).Code != "validation_failed" {
		t.Fatalf("want validation_failed, got %v", err)
	}
	current, err := ctrl.Tasks.Current()
	if err != nil {
		t.Fatal(err)
	}
	if current.CurrentStep != "first" || len(current.CompletedSteps) != 0 {
		t.Fatalf("execution progress claim mutated state: %+v", current)
	}
	records, err := ctrl.Memory.List(ctx, app.LayerShort, "progress_claims", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("rejected progress claim should not save short memory: %+v", records)
	}
}

func TestProcessControllerDoneBenignInputIsReadOnlyAnswer(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	if _, err := ctrl.Tasks.Start("done task"); err != nil {
		t.Fatal(err)
	}
	moveCurrentTaskToValidatedDone(t, ctrl)
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

func TestProcessControllerPromptImproverFailureFallsBackToOriginal(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	ctrl.PromptImprover = &PromptImprover{Provider: fake, Model: "fake/model"}
	fake.ValidatorResponses = []string{"not-json"}
	res, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "prompt_improver_fallback", Input: "hello", ActionKind: ActionAnswerQuestion})
	if err != nil {
		t.Fatalf("prompt improver failure should not block exchange: %v", err)
	}
	if res == nil || !strings.Contains(res.Answer, "fake assistant response") {
		t.Fatalf("main answer missing after prompt improver fallback: %+v", res)
	}
	if !strings.Contains(strings.Join(res.Warnings, "\n"), "prompt improvement skipped") {
		t.Fatalf("missing prompt improvement warning: %+v", res.Warnings)
	}
}

func TestProcessControllerReadyWithTrustedEvidenceTransitionsBeforeProvider(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, dir := newTestController(t)
	task, err := ctrl.Tasks.Start("verify task")
	if err != nil {
		t.Fatal(err)
	}
	moveCurrentTaskToExecutionWithApprovedPlan(t, ctrl)
	current, err := ctrl.Tasks.Current()
	if err != nil {
		t.Fatal(err)
	}
	evidence := issueTestEvidence(t, dir, task, "ready_with_evidence", "go test ./manual_scratch/day14_stock_profit")
	res, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "ready_with_evidence", Input: "Готово к проверке: проверь результат.", TrustedEvidence: evidence})
	if err != nil {
		t.Fatalf("ready with trusted evidence should transition before provider: %v", err)
	}
	if res.Transition == nil || res.Transition.From != current.Stage || res.Transition.To != app.StageValidation {
		t.Fatalf("expected execution->validation transition, got %+v", res.Transition)
	}
	if chatCalls(fake.SnapshotCalls()) != 0 {
		t.Fatalf("provider should not be called for app-owned ready transition: %+v", fake.SnapshotCalls())
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
