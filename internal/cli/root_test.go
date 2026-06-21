package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/memory"
	"github.com/nikbrik/coding_writer/internal/process"
	"github.com/nikbrik/coding_writer/internal/providers"
)

func moveRuntimeTaskToDone(t *testing.T, rt *runtime) app.TaskState {
	t.Helper()
	if _, err := rt.Tasks.Start("done task"); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Tasks.Move(app.StageExecution); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Tasks.Move(app.StageValidation); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Tasks.RecordAcceptedValidation("ready_for_done", nil); err != nil {
		t.Fatal(err)
	}
	state, err := rt.Tasks.Move(app.StageDone)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

type blockingChatProvider struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once

	mu        sync.Mutex
	chatCalls int
}

func newBlockingChatProvider() *blockingChatProvider {
	return &blockingChatProvider{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (p *blockingChatProvider) ListModels(ctx context.Context) ([]string, error) {
	return []string{"fake/model"}, nil
}

func (p *blockingChatProvider) Complete(ctx context.Context, req providers.CompletionRequest) (providers.CompletionResponse, error) {
	if req.Purpose != providers.PurposeChat {
		return providers.NewFakeProvider().Complete(ctx, req)
	}
	p.mu.Lock()
	p.chatCalls++
	call := p.chatCalls
	p.mu.Unlock()
	if call == 1 {
		p.once.Do(func() { close(p.started) })
		select {
		case <-ctx.Done():
			return providers.CompletionResponse{}, ctx.Err()
		case <-p.release:
		}
	}
	return providers.CompletionResponse{
		Message: app.ChatMessage{
			ID:      app.NewID("msg"),
			Role:    app.RoleAssistant,
			Content: `{"stage":"execution","summary":"trusted evidence accepted","deliverable":"prepared output","current_step":"review trusted evidence","completed_steps":["review trusted evidence"],"next_step":"","changed_artifacts":["artifact.txt"],"verification":["go version"],"blockers":[],"next_signal":"ready_for_validation"}`,
		},
		Model:      req.Model,
		ProviderID: "blocking_fake",
	}, nil
}

func (p *blockingChatProvider) ChatCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.chatCalls
}

func TestChatTurnsSerializeAndBlockStaleStateMutation(t *testing.T) {
	t.Setenv("ASSISTANT_LLM_VALIDATION", "off")
	storageDir := t.TempDir()
	rt, err := newRuntime(context.Background(), &globalOptions{StorageDir: storageDir, Model: "fake/model"})
	if err != nil {
		t.Fatal(err)
	}
	provider := newBlockingChatProvider()
	rt.Provider = provider
	if _, err := rt.Tasks.Start("serialize terminal turn"); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Tasks.MoveWithPlanningOutput("verify task", []string{"trusted evidence is reviewed"}, []string{"review trusted evidence"}, nil, app.StageExecution); err != nil {
		t.Fatal(err)
	}

	firstErr := make(chan error, 1)
	go func() {
		_, err := runChatExchange(context.Background(), rt, "session_overlap", "continue execution", false, true, "go version")
		firstErr <- err
	}()
	select {
	case <-provider.started:
	case <-time.After(2 * time.Second):
		t.Fatal("first turn did not reach provider")
	}

	secondErr := make(chan error, 1)
	go func() {
		_, err := runChatExchange(context.Background(), rt, "session_overlap", "verify and finish", false, true, "")
		secondErr <- err
	}()
	select {
	case err := <-secondErr:
		t.Fatalf("second turn completed while first turn was still mutating state: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if calls := provider.ChatCalls(); calls != 1 {
		t.Fatalf("overlapping turn reached provider before lock release; chat calls=%d", calls)
	}

	close(provider.release)
	if err := <-firstErr; err != nil {
		t.Fatalf("first turn failed: %v", err)
	}
	if err := <-secondErr; err != nil {
		t.Fatalf("second turn should act on fresh post-first state, got %v", err)
	}
	if calls := provider.ChatCalls(); calls != 1 {
		t.Fatalf("fresh queued second turn should not call stale execution provider, chat calls=%d", calls)
	}
	state, err := rt.Tasks.Current()
	if err != nil {
		t.Fatal(err)
	}
	if state.Stage != app.StageDone {
		t.Fatalf("queued turns should complete task without stale execution mutation, got stage %s", state.Stage)
	}
	_, err = runChatExchange(context.Background(), rt, "session_overlap", "implement another change", false, true, "")
	if app.AsError(err).Code != "task_done" {
		t.Fatalf("terminal task should reject later mutation, got %v", err)
	}
	if calls := provider.ChatCalls(); calls != 1 {
		t.Fatalf("terminal mutation attempt should not call provider, chat calls=%d", calls)
	}
}

func TestChatOnceRenderPromptJSONUsesFakeProviderAndProfile(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	storageDir := t.TempDir()
	opts := &globalOptions{}
	cmd := newRootCommand(opts)
	var out, stderr bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--model", "fake/model", "--json", "chat", "--once", "--input", "Объясни memory layers", "--render-prompt"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v stderr=%s", err, stderr.String())
	}
	var parsed map[string]any
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
		t.Fatalf("bad JSON: %v output=%s", err, out.String())
	}
	if parsed["ok"] != true || !strings.Contains(out.String(), "profile.active") || !strings.Contains(out.String(), "memory.working") {
		t.Fatalf("bad chat render output: %s", out.String())
	}
}

func TestCWOnceJSONUsesTopLevelEntrypoint(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	storageDir := t.TempDir()
	opts := &globalOptions{}
	cmd := newRootCommandForInvocation(opts, "cw")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--model", "fake/model", "--json", "--once", "--input", "Объясни memory layers"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v output=%s", err, out.String())
	}
	var parsed map[string]any
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
		t.Fatalf("bad JSON: %v output=%s", err, out.String())
	}
	if parsed["ok"] != true || parsed["session_id"] == "" {
		t.Fatalf("bad cw JSON output: %s", out.String())
	}
}

func TestCWNonInteractiveTUIRequiresTerminal(t *testing.T) {
	storageDir := t.TempDir()
	opts := &globalOptions{}
	cmd := newRootCommandForInvocation(opts, "cw")
	var out, stderr bytes.Buffer
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--model", "fake/model", "--tui"})
	err := cmd.Execute()
	if app.AsError(err).Code != "tui_requires_terminal" {
		t.Fatalf("expected tui_requires_terminal, got %v", err)
	}
}

func TestCWPlainUsesLegacyREPLFallback(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	storageDir := t.TempDir()
	opts := &globalOptions{}
	cmd := newRootCommandForInvocation(opts, "cw")
	var out bytes.Buffer
	cmd.SetIn(strings.NewReader("/exit\n"))
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--model", "fake/model", "--plain"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("plain repl failed: %v", err)
	}
}

func TestChatOnceJSONDoesNotExposeRawPromptByDefault(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	storageDir := t.TempDir()
	cmd := newRootCommand(&globalOptions{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--model", "fake/model", "--json", "chat", "--once", "--input", "Объясни memory layers"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v output=%s", err, out.String())
	}
	var parsed map[string]any
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
		t.Fatalf("bad JSON: %v output=%s", err, out.String())
	}
	if parsed["rendered_prompt_id"] == nil || parsed["rendered_prompt"] != nil || parsed["messages"] != nil {
		t.Fatalf("raw prompt leaked in default JSON: %s", out.String())
	}
}

func TestChatOnceHumanOutputIsNotJSON(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	storageDir := t.TempDir()
	cmd := newRootCommand(&globalOptions{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--model", "fake/model", "chat", "--once", "--input", "Объясни memory layers"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v output=%s", err, out.String())
	}
	if json.Valid(out.Bytes()) {
		t.Fatalf("human output must not be raw JSON: %s", out.String())
	}
	text := out.String()
	if !strings.Contains(text, "== Assistant ==") {
		t.Fatalf("missing readable assistant section: %s", text)
	}
	if strings.ContainsRune(text, rune(0x1b)) {
		t.Fatalf("non-TTY test output must not contain ANSI escapes: %q", text)
	}
}

func TestChatHumanRendererFormatsStageJSON(t *testing.T) {
	state := app.TaskState{
		ID:                 "task_demo",
		Stage:              app.StageValidation,
		ExpectedAction:     app.ExpectedUserInput,
		Status:             app.TaskStatusActive,
		CurrentStep:        "проверить критерии",
		ValidationEvidence: []string{"app:evidence:v2:demo"},
	}
	result := chatResult{
		OK:     true,
		Answer: `{"stage":"planning","summary":"Проверить пакет.","assumptions":["пакет уже существует"],"acceptance_criteria":["go test ./pkg passes"],"plan":["Запустить проверку"],"open_questions":[],"readiness":"ready_for_execution_proposal"}`,
		Transition: &process.TransitionResult{
			Moved: true,
			From:  app.StagePlanning,
			To:    app.StageValidation,
			State: state,
		},
		AppliedArtifacts: []string{"manual_scratch/day15_contains_duplicate/contains_duplicate.go"},
		Warnings:         []string{"auto verification: go test ./pkg", "memory proposal skipped: invalid_json"},
		Task:             &state,
	}
	text := textChatResult(result)
	for _, want := range []string{"== Assistant ==", "Acceptance criteria:", "1. go test ./pkg passes", "== Task ==", "== Transition ==", "== Files ==", "applied: manual_scratch/day15_contains_duplicate/contains_duplicate.go", "== Evidence ==", "auto verification: go test ./pkg", "== Warnings =="} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, `"stage"`) || strings.Contains(text, `"acceptance_criteria"`) {
		t.Fatalf("human renderer leaked raw schema JSON:\n%s", text)
	}
}

func TestChatHumanRendererColorModeHighlightsSectionsAndLabels(t *testing.T) {
	state := app.TaskState{
		ID:             "task_demo",
		Stage:          app.StageExecution,
		ExpectedAction: app.ExpectedUserInput,
		Status:         app.TaskStatusActive,
		CurrentStep:    "run tests",
	}
	text := renderChatResult(chatResult{
		OK:       true,
		Answer:   `{"stage":"execution","summary":"ready","deliverable":"checked","current_step":"run tests","completed_steps":[],"next_step":"review","changed_artifacts":[],"verification":["go test"],"blockers":[],"next_signal":"ready_for_validation"}`,
		Task:     &state,
		Warnings: []string{"auto verification: go test ./pkg"},
	}, chatRenderOptions{Color: true})
	for _, want := range []string{
		"\x1b[1;36m== Assistant ==\x1b[0m",
		"\x1b[1mSummary\x1b[0m",
		"\x1b[1;36m== Task ==\x1b[0m",
		"\x1b[1mstage\x1b[0m",
		"\x1b[1;36m== Evidence ==\x1b[0m",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("color renderer missing %q in:\n%q", want, text)
		}
	}
}

func TestChatHumanRendererColorModeHighlightsGoCodeBlocks(t *testing.T) {
	text := renderChatResult(chatResult{
		OK:     true,
		Answer: "```go\nfunc ContainsDuplicate(nums []int) bool {\n\treturn false\n}\n```",
	}, chatRenderOptions{Color: true})
	for _, want := range []string{
		"\x1b[2m```go\x1b[0m",
		"\x1b[38;5;81mfunc\x1b[0m",
		"\x1b[38;5;81mreturn\x1b[0m",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing syntax highlight %q in:\n%q", want, text)
		}
	}

	plain := textChatResult(chatResult{
		OK:     true,
		Answer: "```go\nfunc ContainsDuplicate(nums []int) bool {\n\treturn false\n}\n```",
	})
	if strings.ContainsRune(plain, rune(0x1b)) {
		t.Fatalf("plain renderer must not contain ANSI escapes: %q", plain)
	}
}

func TestChatHumanRendererShowsPlanningSwarmSummaries(t *testing.T) {
	text := textChatResult(chatResult{
		OK:     true,
		Answer: `{"stage":"planning","summary":"plan","assumptions":[],"acceptance_criteria":["criteria"],"plan":["step"],"open_questions":[],"readiness":"ready_for_execution_proposal"}`,
		AuditEvents: []process.ProcessAuditEvent{
			{SessionID: "s1", TaskID: "t1", Decision: "planning_specialist_summary", AgentRole: "requirements_specialist", Reason: "summary=requirements reviewed;findings=0;plan_items=1;criteria=0;proposed_plan=Add explicit package path."},
			{SessionID: "s1", TaskID: "t1", Decision: "planning_specialist_summary", AgentRole: "code_research_specialist", Reason: "summary=code surface reviewed;findings=1;plan_items=0;criteria=0;top_finding=medium package: missing package name -> add package containsduplicate"},
		},
	})
	for _, want := range []string{
		"== Planning swarm ==",
		"requirements_specialist: requirements reviewed (findings=0, plan proposals=1, criteria proposals=0)",
		"proposed plan: Add explicit package path.",
		"code_research_specialist: code surface reviewed (findings=1, plan proposals=0, criteria proposals=0)",
		"finding: medium package: missing package name -> add package containsduplicate",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
}

func TestChatHumanRendererDoesNotRepeatPlanningSwarmAfterPlanning(t *testing.T) {
	text := textChatResult(chatResult{
		OK:     true,
		Answer: `{"stage":"validation","summary":"verified","checks":["tests passed"],"findings":[],"missing_evidence":[],"verdict":"pass","ready_for_done":true}`,
		AuditEvents: []process.ProcessAuditEvent{
			{SessionID: "s1", TaskID: "t1", Decision: "planning_specialist_summary", AgentRole: "requirements_specialist", Reason: "requirements reviewed;findings=0"},
		},
	})
	if strings.Contains(text, "== Planning swarm ==") {
		t.Fatalf("planning swarm should not repeat after planning:\n%s", text)
	}
}

func TestChatHumanRendererFormatsMultipleStageJSONDocuments(t *testing.T) {
	result := chatResult{
		OK: true,
		Answer: `{"stage":"execution","summary":"step one","deliverable":"first","current_step":"one","completed_steps":[],"next_step":"two","changed_artifacts":[],"verification":["not run"],"blockers":[],"next_signal":"continue_execution"}

{"stage":"execution","summary":"step two","deliverable":"second","current_step":"two","completed_steps":["one"],"next_step":"verify","changed_artifacts":[],"verification":["not run"],"blockers":[],"next_signal":"continue_execution"}`,
	}
	text := textChatResult(result)
	for _, want := range []string{"Stage output 1:", "Summary: step one", "Stage output 2:", "Summary: step two", "Deliverable:"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, `"stage"`) || strings.Contains(text, `"next_signal"`) {
		t.Fatalf("human renderer leaked raw multi-json schema:\n%s", text)
	}
}

func TestExplicitTrustedVerificationKeepsExactCommandGeneric(t *testing.T) {
	task := app.TaskState{
		ID:        "task_verify",
		Stage:     app.StageExecution,
		Status:    app.TaskStatusActive,
		Objective: "Verify Go package manual_scratch/day14_stock_profit with standard tests.",
		Plan: []string{
			"Execute 'go test -v ./...' to run existing unit tests.",
		},
	}
	got := explicitTrustedVerificationCommand(task)
	if got != "go test -v ./..." {
		t.Fatalf("exact command must not be specialized from package context, got %q", got)
	}

	task.Plan = []string{"Run `npm run test:unit -- --runInBand` as trusted verification."}
	got = explicitTrustedVerificationCommand(task)
	if got != "npm run test:unit -- --runInBand" {
		t.Fatalf("generic exact command not extracted, got %q", got)
	}

	task.Plan = []string{"run go test ./manual_scratch/day15_contains_duplicate as trusted verification"}
	got = explicitTrustedVerificationCommand(task)
	if got != "go test ./manual_scratch/day15_contains_duplicate" {
		t.Fatalf("unquoted command with trailing prose not extracted, got %q", got)
	}

	tokens := normalizeTrustedVerificationTokens([]string{"go", "test", "-v", "manual_scratch/day15_contains_duplicate/"})
	if strings.Join(tokens, " ") != "go test -v ./manual_scratch/day15_contains_duplicate" {
		t.Fatalf("go package command should be normalized, got %q", strings.Join(tokens, " "))
	}
}

func TestVerificationPlannerFallbackIsLanguageAgnostic(t *testing.T) {
	task := app.TaskState{
		ID:        "task_verify",
		Stage:     app.StageExecution,
		Status:    app.TaskStatusActive,
		Objective: "Implement Contains Duplicate in manual_scratch/day15_contains_duplicate.",
		AcceptanceCriteria: []string{
			"ContainsDuplicate uses an O(n) map approach.",
			"All tests pass successfully within the directory manual_scratch/day15_contains_duplicate.",
		},
		Plan: []string{
			"Verify the implementation by running tests in the directory.",
		},
	}
	fake := providers.NewFakeProvider()
	fake.ValidatorResponse = `{"command":"go test -v manual_scratch/day15_contains_duplicate/.","confidence":0.96,"reason":"test package"}`
	rt := &runtime{Config: app.AppConfig{ActiveModel: "fake/model"}, Provider: fake}
	got, err := resolveTrustedVerificationCommand(context.Background(), rt, task)
	if err != nil {
		t.Fatal(err)
	}
	if got != "go test -v ./manual_scratch/day15_contains_duplicate" {
		t.Fatalf("planner should choose exact verification command, got %q", got)
	}
}

func TestVerificationResolverDoesNotInferFromPathOrNaturalLanguage(t *testing.T) {
	task := app.TaskState{
		ID:        "task_verify",
		Stage:     app.StageExecution,
		Status:    app.TaskStatusActive,
		Objective: "Implement Contains Duplicate in manual_scratch/day15_contains_duplicate.",
		AcceptanceCriteria: []string{
			"The function 'ContainsDuplicate(nums []int) bool' is implemented in 'manual_scratch/day15_contains_duplicate'.",
			"Tests pass successfully when executed via standard go test commands.",
		},
		Plan: []string{
			"Create 'contains.go' implementing the function.",
			"Verify the implementation passes all tests.",
		},
	}
	if got := explicitTrustedVerificationCommand(task); got != "" {
		t.Fatalf("natural-language command fragment must not execute, got %q", got)
	}
	fake := providers.NewFakeProvider()
	fake.ValidatorResponse = `{"command":"go test commands","confidence":0.99,"reason":"invalid natural language"}`
	rt := &runtime{Config: app.AppConfig{ActiveModel: "fake/model"}, Provider: fake}
	got, err := resolveTrustedVerificationCommand(context.Background(), rt, task)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("invalid planner command must no-op, got %q", got)
	}
}

func TestAutoVerificationRequiresSemanticIntent(t *testing.T) {
	task := app.TaskState{
		ID:             "task_verify",
		Stage:          app.StageExecution,
		Status:         app.TaskStatusActive,
		ExpectedAction: app.ExpectedUserInput,
		Plan:           []string{"Run `go test ./manual_scratch/day14_stock_profit`."},
	}
	fake := providers.NewFakeProvider()
	validator := process.NewSemanticValidator(fake, "fake/model")
	fake.ValidatorResponse = `{"action_kind":"answer_question","transition_signal":"none","confidence":0.93,"reason":"user is asking a question, not requesting validation"}`
	got, err := autoTrustedVerificationCommand(context.Background(), nil, "session_semantic_auto_verify", "Готово к проверке?", task, false, validator)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("semantic validator returned no transition signal; auto verification must not run, got %q", got)
	}

	got, err = autoTrustedVerificationCommand(context.Background(), nil, "session_fallback_auto_verify", "Готово к проверке?", task, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("auto verification must not use keyword fallback when semantic validator is unavailable, got %q", got)
	}

	fake.ValidatorResponse = `{"action_kind":"summarize_execution","transition_signal":"none","confidence":0.91,"reason":"user asks the assistant to check the current result"}`
	got, err = autoTrustedVerificationCommand(context.Background(), nil, "session_semantic_review_auto_verify", "Проверь результат текущего шага.", task, false, validator)
	if err != nil {
		t.Fatal(err)
	}
	if got != "go test ./manual_scratch/day14_stock_profit" {
		t.Fatalf("semantic summarize_execution intent should trigger auto verification, got %q", got)
	}
}

func TestAutoVerificationSkipsReadyValidationEvidence(t *testing.T) {
	got, err := autoTrustedVerificationCommand(context.Background(), nil, "session_ready_done", "finish", app.TaskState{
		ID:                 "task_ready",
		Stage:              app.StageValidation,
		Status:             app.TaskStatusActive,
		ValidationStatus:   "ready_for_done",
		ValidationEvidence: []string{"app:evidence:v2:e1"},
	}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("ready validation evidence should not rerun verification, got %q", got)
	}
}

func TestExtractDeliverableFileBlocksUsesSingleTaskDirectory(t *testing.T) {
	task := app.TaskState{
		ID:        "task_1",
		Objective: "Implement ContainsDuplicate in manual_scratch/day15_contains_duplicate.",
		Plan:      []string{"Provide contains_duplicate.go", "Provide contains_duplicate_test.go"},
	}
	deliverable := "### contains_duplicate.go\n```go\npackage day15_contains_duplicate\n```\n\n### contains_duplicate_test.go\n```go\npackage day15_contains_duplicate\n```\n"

	blocks := extractDeliverableFileBlocks(deliverable, task)
	if len(blocks) != 2 {
		t.Fatalf("want 2 blocks, got %#v", blocks)
	}
	if blocks[0].Path != "manual_scratch/day15_contains_duplicate/contains_duplicate.go" {
		t.Fatalf("unexpected first path: %#v", blocks[0])
	}
	if blocks[1].Path != "manual_scratch/day15_contains_duplicate/contains_duplicate_test.go" {
		t.Fatalf("unexpected second path: %#v", blocks[1])
	}
}

func TestSafeWorkspacePathRejectsTraversal(t *testing.T) {
	cwd := t.TempDir()
	if _, err := safeWorkspacePath(cwd, "../outside.go"); err == nil || app.AsError(err).Code != "unsafe_artifact_path" {
		t.Fatalf("want unsafe_artifact_path, got %v", err)
	}
}

func TestMaterializeExecutionDeliverableWritesFiles(t *testing.T) {
	cwd := t.TempDir()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	task := app.TaskState{
		ID:        "task_1",
		Objective: "Implement ContainsDuplicate in manual_scratch/day15_contains_duplicate.",
	}
	answer := `{"stage":"execution","summary":"prepared","deliverable":"### contains_duplicate.go\n` + "```go" + `\npackage day15_contains_duplicate\n` + "```" + `","current_step":"implement","completed_steps":[],"next_step":"tests","changed_artifacts":[],"verification":["not run"],"blockers":[],"next_signal":"continue_execution"}`
	written, err := materializeExecutionDeliverable(&process.ExchangeResult{Answer: answer}, task)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 1 || written[0] != "manual_scratch/day15_contains_duplicate/contains_duplicate.go" {
		t.Fatalf("unexpected written paths: %#v", written)
	}
	data, err := os.ReadFile(filepath.Join(cwd, written[0]))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "package day15_contains_duplicate\n" {
		t.Fatalf("unexpected file content: %q", string(data))
	}
}

func TestMaterializeExecutionDeliverableHandlesCombinedExecutionAnswers(t *testing.T) {
	cwd := t.TempDir()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	task := app.TaskState{ID: "task_1", Objective: "Implement in manual_scratch/day15_contains_duplicate."}
	first := `{"stage":"execution","summary":"one","deliverable":"### contains_duplicate.go\n` + "```go" + `\npackage containsduplicate\n` + "```" + `","current_step":"one","completed_steps":[],"next_step":"two","changed_artifacts":[],"verification":["not run"],"blockers":[],"next_signal":"continue_execution"}`
	second := `{"stage":"execution","summary":"two","deliverable":"### contains_duplicate_test.go\n` + "```go" + `\npackage containsduplicate\n` + "```" + `","current_step":"two","completed_steps":[],"next_step":"verify","changed_artifacts":[],"verification":["not run"],"blockers":["Need tests"],"next_signal":"continue_execution"}`

	written, err := materializeExecutionDeliverable(&process.ExchangeResult{Answer: first + "\n\n" + second}, task)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 2 {
		t.Fatalf("want 2 materialized files, got %#v", written)
	}
}

func TestMaterializeExecutionDeliverableNormalizesGoPackageInDirectory(t *testing.T) {
	cwd := t.TempDir()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	task := app.TaskState{ID: "task_1", Objective: "Implement in manual_scratch/day15_contains_duplicate."}
	first := `{"stage":"execution","summary":"one","deliverable":"### manual_scratch/day15_contains_duplicate/contains_duplicate.go\n` + "```go" + `\npackage containsduplicate\n\nfunc ContainsDuplicate(nums []int) bool { return false }\n` + "```" + `","current_step":"one","completed_steps":[],"next_step":"two","changed_artifacts":[],"verification":["not run"],"blockers":[],"next_signal":"continue_execution"}`
	second := `{"stage":"execution","summary":"two","deliverable":"### manual_scratch/day15_contains_duplicate/contains_duplicate_test.go\n` + "```go" + `\npackage day15_contains_duplicate\n\nfunc TestPackageNamePlaceholder() {}\n` + "```" + `","current_step":"two","completed_steps":[],"next_step":"verify","changed_artifacts":[],"verification":["not run"],"blockers":[],"next_signal":"continue_execution"}`

	if _, err := materializeExecutionDeliverable(&process.ExchangeResult{Answer: first + "\n\n" + second}, task); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(cwd, "manual_scratch/day15_contains_duplicate/contains_duplicate_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "package containsduplicate") {
		t.Fatalf("package was not normalized:\n%s", string(data))
	}
}

func TestMaterializeExecutionDeliverableDoesNotNormalizeNonGoFiles(t *testing.T) {
	cwd := t.TempDir()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	dir := filepath.Join(cwd, "manual_scratch/day15_contains_duplicate")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "contains_duplicate.go"), []byte("package day15_contains_duplicate\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	task := app.TaskState{ID: "task_1", Objective: "Implement in manual_scratch/day15_contains_duplicate."}
	answer := `{"stage":"execution","summary":"docs","deliverable":"### manual_scratch/day15_contains_duplicate/README.txt\n` + "```text" + `\npackage containsduplicate\n` + "```" + `","current_step":"docs","completed_steps":[],"next_step":"verify","changed_artifacts":[],"verification":["not run"],"blockers":[],"next_signal":"continue_execution"}`

	written, err := materializeExecutionDeliverable(&process.ExchangeResult{Answer: answer}, task)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 1 || written[0] != "manual_scratch/day15_contains_duplicate/README.txt" {
		t.Fatalf("unexpected written paths: %#v", written)
	}
	data, err := os.ReadFile(filepath.Join(cwd, written[0]))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "package containsduplicate\n" {
		t.Fatalf("non-Go artifact was normalized: %q", string(data))
	}
}

func TestStartAPIProgressWritesHumanReadableStatus(t *testing.T) {
	var diag bytes.Buffer
	stop := startAPIProgress(&diag, true)
	stop()
	text := diag.String()
	if !strings.Contains(text, "[api] запрос к модели") || !strings.Contains(text, "[api] ответ получен") {
		t.Fatalf("missing progress messages: %q", text)
	}
}

func TestCLIP0DayFlowsUseScriptableCommands(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	storageDir := t.TempDir()
	runJSON := func(args ...string) map[string]any {
		t.Helper()
		cmd := newRootCommand(&globalOptions{})
		var out bytes.Buffer
		cmd.SetOut(&out)
		base := []string{"--storage-dir", storageDir, "--model", "fake/model", "--json"}
		cmd.SetArgs(append(base, args...))
		if err := cmd.Execute(); err != nil {
			t.Fatalf("command %v failed: %v output=%s", args, err, out.String())
		}
		var parsed map[string]any
		if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
			t.Fatalf("bad JSON for %v: %v output=%s", args, err, out.String())
		}
		return parsed
	}

	chat := runJSON("chat", "--once", "--input", "Спланируй модуль памяти. Требование: CLI должен поддерживать выбор модели OpenRouter. Я предпочитаю короткие ответы на русском.")
	proposal, ok := chat["proposal"].(map[string]any)
	if !ok || proposal["id"] == "" || chat["rendered_prompt"] != nil || chat["rendered_prompt_id"] == nil {
		t.Fatalf("bad chat/proposal JSON: %+v", chat)
	}
	transition, ok := chat["transition"].(map[string]any)
	if !ok || transition["To"] != "planning" {
		t.Fatalf("chat did not auto-start Day 11 planning task: %+v", chat)
	}
	proposalID := proposal["id"].(string)
	apply := runJSON("memory", "apply", "--proposal", proposalID, "--accept", "all")
	if apply["ok"] != true {
		t.Fatalf("apply failed: %+v", apply)
	}
	work := runJSON("memory", "list", "work")
	long := runJSON("memory", "list", "long")
	if !strings.Contains(fmt.Sprint(work["records"]), "выбор модели OpenRouter") || !strings.Contains(fmt.Sprint(long["records"]), "короткие ответы на русском") {
		t.Fatalf("memory apply/list did not expose Day 11 records: work=%+v long=%+v", work, long)
	}

	runJSON("profiles", "set", "student")
	student := runJSON("chat", "--once", "--input", "Объясни архитектуру memory layers.", "--render-prompt")
	runJSON("profiles", "set", "senior")
	senior := runJSON("chat", "--once", "--input", "Объясни архитектуру memory layers.", "--render-prompt")
	if student["rendered_prompt"] == senior["rendered_prompt"] || !strings.Contains(fmt.Sprint(student["rendered_prompt"]), "profile.active") || !strings.Contains(fmt.Sprint(senior["rendered_prompt"]), "profile.active") {
		t.Fatalf("profile prompts did not differ: student=%+v senior=%+v", student, senior)
	}

	runJSON("task", "step", "реализовать MemoryManager")
	runJSON("task", "plan", "реализовать MemoryManager")
	runJSON("task", "criteria", "state persists")
	runJSON("task", "expect", "llm_response")
	runJSON("task", "move", "execution")
	runJSON("task", "pause")
	resumed := runJSON("task", "resume")
	if !strings.Contains(fmt.Sprint(resumed["task"]), "реализовать MemoryManager") || !strings.Contains(fmt.Sprint(resumed["task"]), "llm_response") {
		t.Fatalf("resume did not preserve Day 13 state: %+v", resumed)
	}
}

func TestCLIDay13AgentDrivenFSM(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	t.Setenv("ASSISTANT_LLM_VALIDATION", "1")
	storageDir := t.TempDir()
	runJSON := func(args ...string) map[string]any {
		t.Helper()
		cmd := newRootCommand(&globalOptions{})
		var out bytes.Buffer
		cmd.SetOut(&out)
		base := []string{"--storage-dir", storageDir, "--model", "fake/model", "--json"}
		cmd.SetArgs(append(base, args...))
		if err := cmd.Execute(); err != nil {
			t.Fatalf("command %v failed: %v output=%s", args, err, out.String())
		}
		var parsed map[string]any
		if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
			t.Fatalf("bad JSON for %v: %v output=%s", args, err, out.String())
		}
		return parsed
	}

	plan := runJSON("chat", "--once", "--input", "Спланируй пользовательскую задачу: проверить команду go version. Цель: убедиться, что go version проходит. Предложи план проверки и критерии готовности.")
	if transition, _ := plan["transition"].(map[string]any); transition["To"] != "planning" {
		t.Fatalf("planning intent did not auto-start task: %+v", plan)
	}
	approve := runJSON("chat", "--once", "--input", "Да, план принят. Приступай к выполнению первого шага.")
	if transition, _ := approve["transition"].(map[string]any); transition["To"] != "validation" {
		t.Fatalf("approval did not auto-run trusted verification into validation: %+v", approve)
	}
	if warnings, _ := approve["warnings"].([]any); len(warnings) == 0 || !strings.Contains(fmt.Sprint(warnings), "auto verification") {
		t.Fatalf("approval step did not auto-run verification: %+v", approve)
	}
	ready := runJSON("chat", "--once", "--input", "Готово к проверке: проверь результат.")
	if _, ok := ready["transition"]; ok {
		t.Fatalf("validation review should not move lifecycle: %+v", ready)
	}
	review := runJSON("chat", "--once", "--input", "Проверь критерии по evidence, но пока не завершай задачу; дай validation review.")
	if _, ok := review["transition"]; ok {
		t.Fatalf("validation review should not finish task: %+v", review)
	}
	done := runJSON("chat", "--once", "--input", "Проверь критерии и заверши задачу, если evidence подтверждает go test.")
	if transition, _ := done["transition"].(map[string]any); transition["To"] != "done" {
		t.Fatalf("trusted validation did not auto-finish task: %+v", done)
	}
}

func TestChatVerifyRequiresOnce(t *testing.T) {
	cmd := newRootCommand(&globalOptions{})
	cmd.SetArgs([]string{"chat", "--verify", "go version"})
	if err := cmd.Execute(); err == nil || app.AsError(err).Code != "verify_requires_once" {
		t.Fatalf("want verify_requires_once, got %v", err)
	}
}

func TestTrustedVerificationPolicy(t *testing.T) {
	storageDir := t.TempDir()
	rt, err := newRuntime(context.Background(), &globalOptions{StorageDir: storageDir, Model: "fake/model"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := rt.Tasks.Start("verify task")
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "session_verify_policy"
	evidence, err := runTrustedVerification(context.Background(), storageDir, task.ID, sessionID, "go version", false)
	if err != nil {
		t.Fatalf("go version should be trusted verification: %v", err)
	}
	if len(evidence) != 1 || !strings.HasPrefix(evidence[0], "app:evidence:v2:") {
		t.Fatalf("bad trusted evidence: %+v", evidence)
	}
	records, err := process.NewTrustedEvidenceStore(storageDir).Validate(task.ID, sessionID, evidence)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Source != "go version" {
		t.Fatalf("bad trusted evidence record: %+v", records)
	}

	for _, command := range []string{
		"go test ./...",
		"go test ./manual_scratch/day15_contains_duplicate -run TestContainsDuplicate",
		"npm test",
		"npm run test:unit -- --runInBand",
		"python -m pytest",
		"pytest ./tests -k duplicate",
		"pytest ./tests",
		"cargo test",
		"dotnet test",
		"mvn test",
	} {
		tokens, err := parseTrustedVerificationCommand(command)
		if err != nil {
			t.Fatalf("%q should be in trusted verification allowlist: %v", command, err)
		}
		if !trustedVerificationCandidateUsable(tokens) {
			t.Fatalf("%q should be usable as an exact verification command", command)
		}
	}
	for _, command := range []string{
		"go test",
		"go test -run TestContainsDuplicate",
		"go test commands",
		"pytest commands",
		"npm run build",
	} {
		tokens, err := parseTrustedVerificationCommand(command)
		if err == nil && trustedVerificationCandidateUsable(tokens) {
			t.Fatalf("%q must not be usable as trusted auto-verification", command)
		}
	}

	if _, err := runTrustedVerification(context.Background(), storageDir, task.ID, sessionID, "go version; printenv OPENROUTER_API_KEY", false); err == nil || app.AsError(err).Code != "unsafe_verification_command" {
		t.Fatalf("want unsafe_verification_command for shell syntax, got %v", err)
	}
	if _, err := runTrustedVerification(context.Background(), storageDir, task.ID, sessionID, "sh -c 'go version'", false); err == nil || app.AsError(err).Code != "unsafe_verification_command" {
		t.Fatalf("want unsafe_verification_command for shell executable, got %v", err)
	}
	if _, err := runTrustedVerification(context.Background(), storageDir, task.ID, sessionID, "go test ./definitely_missing_package", false); err == nil || app.AsError(err).Code != "verification_failed" {
		t.Fatalf("want verification_failed for non-zero command, got %v", err)
	}
}

func TestCLIJSONErrorEnvelope(t *testing.T) {
	storageDir := t.TempDir()
	opts := &globalOptions{}
	cmd := newRootCommand(opts)
	var out, stderr bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--json", "chat", "--once"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	printError(&stderr, err, true)
	if !strings.Contains(stderr.String(), `"ok":false`) && !strings.Contains(stderr.String(), `"ok": false`) {
		t.Fatalf("missing JSON error envelope: %s", stderr.String())
	}
}

func TestInvalidModelSlashDoesNotMutateConfig(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	storageDir := t.TempDir()
	rt, err := newRuntime(context.Background(), &globalOptions{StorageDir: storageDir, Model: "fake/model"})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if _, err := handleSlash(context.Background(), &out, &out, rt, "session_test", "/model missing/model"); err == nil {
		t.Fatal("expected invalid model error")
	}
	cfg, err := app.NewConfigManager(storageDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ActiveModel != "" {
		t.Fatalf("flag model persisted into config: %+v", cfg)
	}
}

func TestInvalidModelFlagDoesNotMutateConfig(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	storageDir := t.TempDir()
	cmd := newRootCommand(&globalOptions{})
	var out, stderr bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--model", "missing/model", "chat", "--once", "--input", "hi"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "invalid_model") {
		t.Fatalf("want invalid_model, got %v", err)
	}
	cfg, err := app.NewConfigManager(storageDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ActiveModel != "" {
		t.Fatalf("model flag persisted into config: %+v", cfg)
	}
}

func TestInitPersistsValidatedModel(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	storageDir := t.TempDir()
	cmd := newRootCommand(&globalOptions{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--model", "fake/model", "--json", "init"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	cfg, err := app.NewConfigManager(storageDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ActiveModel != "fake/model" || cfg.MemoryModel != "fake/model" {
		t.Fatalf("model not persisted by init: %+v", cfg)
	}
	if _, err := os.Stat(filepath.Join(storageDir, "invariants", "project.jsonl")); err != nil {
		t.Fatalf("init did not persist invariants: %v", err)
	}
}

func TestCLIInvariantsListAndAddJSON(t *testing.T) {
	storageDir := t.TempDir()
	run := func(args ...string) map[string]any {
		t.Helper()
		cmd := newRootCommand(&globalOptions{})
		var out bytes.Buffer
		cmd.SetOut(&out)
		base := []string{"--storage-dir", storageDir, "--json"}
		cmd.SetArgs(append(base, args...))
		if err := cmd.Execute(); err != nil {
			t.Fatalf("command %v failed: %v output=%s", args, err, out.String())
		}
		var parsed map[string]any
		if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
			t.Fatalf("bad JSON: %v output=%s", err, out.String())
		}
		return parsed
	}
	list := run("invariants", "list")
	if !strings.Contains(fmt.Sprint(list["invariants"]), "stack.go") {
		t.Fatalf("defaults missing: %+v", list)
	}
	add := run("invariants", "add", "custom.rule", "--kind", "business", "--content", "No beta", "--forbid", "beta")
	if !strings.Contains(fmt.Sprint(add["invariant"]), "custom.rule") {
		t.Fatalf("add missing invariant: %+v", add)
	}
	list = run("invariants", "list")
	if !strings.Contains(fmt.Sprint(list["invariants"]), "custom.rule") {
		t.Fatalf("custom invariant not persisted: %+v", list)
	}
}

func TestCLIInvariantConflictJSONIncludesViolations(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	storageDir := t.TempDir()
	cmd := newRootCommand(&globalOptions{})
	var out, stderr bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--model", "fake/model", "--json", "chat", "--once", "--input", "предложи переписать MVP на Python"})
	err := cmd.Execute()
	if err == nil || app.AsError(err).Code != "invariant_conflict" {
		t.Fatalf("want invariant_conflict, got %v output=%s", err, out.String())
	}
	printError(&stderr, err, true)
	if !strings.Contains(stderr.String(), `"violations"`) || !strings.Contains(stderr.String(), `"invariant_id":"stack.go"`) && !strings.Contains(stderr.String(), `"invariant_id": "stack.go"`) {
		t.Fatalf("missing structured violations: %s", stderr.String())
	}
}

func TestREPLInvariantsListAndAdd(t *testing.T) {
	storageDir := t.TempDir()
	rt, err := newRuntime(context.Background(), &globalOptions{StorageDir: storageDir, Model: "fake/model"})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if _, err := handleSlash(context.Background(), &out, &out, rt, "session_test", "/invariants"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "stack.go") {
		t.Fatalf("defaults missing from /invariants: %s", out.String())
	}
	if strings.Contains(strings.ToLower(out.String()), "openrouter_api_key") {
		t.Fatalf("secret-like invariant pattern leaked in terminal output: %s", out.String())
	}
	out.Reset()
	line := `/invariants add custom.no_beta --kind business --content "Do not propose beta stack" --forbid "beta stack"`
	if _, err := handleSlash(context.Background(), &out, &out, rt, "session_test", line); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "custom.no_beta") {
		t.Fatalf("added invariant missing: %s", out.String())
	}
	out.Reset()
	if _, err := handleSlash(context.Background(), &out, &out, rt, "session_test", "/invariants"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "custom.no_beta") || !strings.Contains(out.String(), "beta stack") {
		t.Fatalf("custom invariant not persisted/listed: %s", out.String())
	}
}

func TestInitRequiresModel(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	t.Setenv("ASSISTANT_MODEL", "")
	cmd := newRootCommand(&globalOptions{})
	cmd.SetArgs([]string{"--storage-dir", t.TempDir(), "init"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "missing_model") {
		t.Fatalf("want missing_model, got %v", err)
	}
}

func TestInvalidModelSyntaxRejectedWithoutProviderCall(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	if _, err := newRuntime(context.Background(), &globalOptions{StorageDir: t.TempDir(), Model: "badmodel"}); err == nil || !strings.Contains(err.Error(), "invalid_model") {
		t.Fatalf("want invalid_model, got %v", err)
	}
}

func TestPausedTaskAllowsSafeQuestionAndBlocksMutations(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	storageDir := t.TempDir()
	args := [][]string{
		{"--storage-dir", storageDir, "--model", "fake/model", "--json", "task", "start", "CLI assistant MVP"},
		{"--storage-dir", storageDir, "--model", "fake/model", "--json", "task", "step", "реализовать MemoryManager"},
		{"--storage-dir", storageDir, "--model", "fake/model", "--json", "task", "plan", "реализовать MemoryManager"},
		{"--storage-dir", storageDir, "--model", "fake/model", "--json", "task", "criteria", "state persists"},
		{"--storage-dir", storageDir, "--model", "fake/model", "--json", "task", "expect", "llm_response"},
		{"--storage-dir", storageDir, "--model", "fake/model", "--json", "task", "move", "execution"},
		{"--storage-dir", storageDir, "--model", "fake/model", "--json", "task", "pause"},
	}
	for _, arg := range args {
		cmd := newRootCommand(&globalOptions{})
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetArgs(arg)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("command %v failed: %v", arg, err)
		}
	}
	cmd := newRootCommand(&globalOptions{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--model", "fake/model", "--json", "chat", "--once", "--input", "Объясни memory layers."})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("safe informational chat should be allowed while paused, got %v output=%s", err, out.String())
	}
	var parsed chatResult
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
		t.Fatalf("bad chat JSON: %v output=%s", err, out.String())
	}
	shortRecords, err := memory.NewManager(storageDir).List(context.Background(), app.LayerShort, parsed.SessionID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(shortRecords) != 2 || shortRecords[0].TaskID != "" || shortRecords[1].TaskID != "" {
		t.Fatalf("paused informational chat should save taskless short memory: %+v", shortRecords)
	}
	proposal, err := memory.NewProposalStore(storageDir, memory.NewManager(storageDir)).Latest(context.Background(), parsed.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	blockedWork := false
	for _, record := range proposal.Records {
		if record.Layer == app.ProposedLayerWork && record.Status == app.ProposalBlocked {
			blockedWork = true
		}
	}
	if !blockedWork {
		t.Fatalf("paused informational proposal should block work memory: %+v", proposal.Records)
	}
	cmd = newRootCommand(&globalOptions{})
	out.Reset()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--model", "fake/model", "--json", "chat", "--once", "--input", "продолжай задачу"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "task_paused") {
		t.Fatalf("want task_paused for task continuation while paused, got %v", err)
	}
	cmd = newRootCommand(&globalOptions{})
	out.Reset()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--model", "fake/model", "--json", "memory", "apply", "--accept", "all"})
	err = cmd.Execute()
	if err != nil {
		t.Fatalf("paused memory apply should save non-work records only, got %v output=%s", err, out.String())
	}
	workFiles, err := filepath.Glob(filepath.Join(storageDir, "tasks", "*", "work_memory.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(workFiles) != 0 {
		t.Fatalf("paused memory apply saved work memory files: %+v", workFiles)
	}
}

func TestSaveWorkRejectsDoneTask(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	rt, err := newRuntime(context.Background(), &globalOptions{StorageDir: t.TempDir(), Model: "fake/model"})
	if err != nil {
		t.Fatal(err)
	}
	state := moveRuntimeTaskToDone(t, rt)
	var out, diag bytes.Buffer
	_, err = handleSlash(context.Background(), &out, &diag, rt, "session_done_save", "/save work should not save")
	if err == nil || app.AsError(err).Code != "task_done" {
		t.Fatalf("want task_done, got %v out=%s diag=%s", err, out.String(), diag.String())
	}
	records, err := rt.Memory.List(context.Background(), app.LayerWork, "", state.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("done task received work memory: %+v", records)
	}
}

func TestMemoryApplyRejectsWorkProposalForDoneTaskWithoutPartialWrites(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	storageDir := t.TempDir()
	rt, err := newRuntime(context.Background(), &globalOptions{StorageDir: storageDir, Model: "fake/model"})
	if err != nil {
		t.Fatal(err)
	}
	state := moveRuntimeTaskToDone(t, rt)
	proposal := app.MemoryProposal{ID: "proposal_done_work", SessionID: "session_done_work", Records: []app.ProposedMemoryRecord{
		{ID: "r_short", Layer: app.ProposedLayerShort, Kind: "context", Content: "short", Reason: "short", Status: app.ProposalPending},
		{ID: "r_work", Layer: app.ProposedLayerWork, Kind: "requirement", Content: "work", Reason: "work", Status: app.ProposalPending},
		{ID: "r_long", Layer: app.ProposedLayerLong, Kind: "preference", Content: "long", ProfileID: "student", Reason: "long", Status: app.ProposalPending},
	}, CreatedAt: state.UpdatedAt}
	if err := rt.Proposals.Save(context.Background(), proposal); err != nil {
		t.Fatal(err)
	}
	cmd := newRootCommand(&globalOptions{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--json", "memory", "apply", "--proposal", proposal.ID, "--accept", "all"})
	err = cmd.Execute()
	if err == nil || app.AsError(err).Code != "task_done" {
		t.Fatalf("want task_done, got %v output=%s", err, out.String())
	}
	shortRecords, err := rt.Memory.List(context.Background(), app.LayerShort, "session_done_work", "")
	if err != nil {
		t.Fatal(err)
	}
	workRecords, err := rt.Memory.List(context.Background(), app.LayerWork, "", state.ID)
	if err != nil {
		t.Fatal(err)
	}
	longRecords, err := rt.Memory.List(context.Background(), app.LayerLong, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(shortRecords) != 0 || len(workRecords) != 0 || len(longRecords) != 0 {
		t.Fatalf("apply failure wrote partial records: short=%+v work=%+v long=%+v", shortRecords, workRecords, longRecords)
	}
}

func TestPausedTaskHardGateBeforeModelValidation(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	storageDir := t.TempDir()
	args := [][]string{
		{"--storage-dir", storageDir, "--json", "task", "start", "CLI assistant MVP"},
		{"--storage-dir", storageDir, "--json", "task", "pause"},
	}
	for _, arg := range args {
		cmd := newRootCommand(&globalOptions{})
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetArgs(arg)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("command %v failed: %v", arg, err)
		}
	}
	cmd := newRootCommand(&globalOptions{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--model", "missing/model", "--json", "chat", "--once", "--input", "продолжай задачу"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "task_paused") {
		t.Fatalf("want task_paused before invalid_model, got %v output=%s", err, out.String())
	}
}

func TestPausedTaskHardGateBeforeProviderDisclosure(t *testing.T) {
	storageDir := t.TempDir()
	args := [][]string{
		{"--storage-dir", storageDir, "--json", "task", "start", "CLI assistant MVP"},
		{"--storage-dir", storageDir, "--json", "task", "pause"},
	}
	for _, arg := range args {
		cmd := newRootCommand(&globalOptions{})
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetArgs(arg)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("command %v failed: %v", arg, err)
		}
	}
	cmd := newRootCommand(&globalOptions{})
	var out, stderr bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--model", "openai/gpt-4.1-mini", "chat", "--once", "--input", "продолжай задачу"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "task_paused") {
		t.Fatalf("want task_paused, got %v output=%s", err, out.String())
	}
	if strings.Contains(stderr.String(), "Provider disclosure") {
		t.Fatalf("provider disclosure happened before hard gate: %s", stderr.String())
	}
}

func TestSecretInputHardGateBeforeProviderValidation(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	storageDir := t.TempDir()
	cmd := newRootCommand(&globalOptions{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--model", "missing/model", "chat", "--once", "--input", "OPENROUTER_API_KEY=sk-secret123456789"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "secret_blocked") {
		t.Fatalf("want secret_blocked before invalid_model, got %v", err)
	}
}

func TestTopLevelTaskPlanCriteriaCommandsPersistState(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	storageDir := t.TempDir()
	cmd := newRootCommand(&globalOptions{})
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--model", "fake/model", "--json", "task", "start", "CLI assistant MVP"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, arg := range [][]string{
		{"--storage-dir", storageDir, "--model", "fake/model", "--json", "task", "plan", "build memory manager"},
		{"--storage-dir", storageDir, "--model", "fake/model", "--json", "task", "criteria", "memory layers are separate files"},
	} {
		cmd := newRootCommand(&globalOptions{})
		cmd.SetArgs(arg)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("top-level task plan/criteria command failed: %v err=%v", arg, err)
		}
	}
}

func TestChatTextShowsProposalRecords(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	storageDir := t.TempDir()
	cmd := newRootCommand(&globalOptions{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--model", "fake/model", "chat", "--once", "--input", "Спланируй модуль памяти"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "== Memory proposal ==") || !strings.Contains(out.String(), "[work] pending") || !strings.Contains(out.String(), "== Next ==") || !strings.Contains(out.String(), "assistant memory apply --proposal") {
		t.Fatalf("proposal records not visible: %s", out.String())
	}
	matches, err := filepath.Glob(filepath.Join(storageDir, "sessions", "*", "prompts.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("raw prompt audit should be opt-in, found: %v", matches)
	}
}

func TestMemoryApplyCLIRequiresExplicitAction(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	cmd := newRootCommand(&globalOptions{})
	cmd.SetArgs([]string{"--storage-dir", t.TempDir(), "--model", "fake/model", "--json", "memory", "apply"})
	if err := cmd.Execute(); err == nil || app.AsError(err).Code != "missing_apply_action" {
		t.Fatalf("want missing_apply_action, got %v", err)
	}
}

func TestClassifierFailureFailsClosed(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	rt, err := newRuntime(context.Background(), &globalOptions{StorageDir: t.TempDir(), Model: "fake/model"})
	if err != nil {
		t.Fatal(err)
	}
	rt.Provider = providers.NewFakeProvider()
	rt.Provider.(*providers.FakeProvider).ClassifierResponse = `not-json`
	result, err := runChatExchange(context.Background(), rt, "session_classifier_fail", "hello", false, true, "")
	if err == nil || app.AsError(err).Category != app.CategoryClassifier {
		t.Fatalf("want classifier error, got err=%v result=%+v", err, result)
	}
}

func TestChatOnceJSONFailsWhenClassifierFails(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	rt, err := newRuntime(context.Background(), &globalOptions{StorageDir: t.TempDir(), Model: "fake/model"})
	if err != nil {
		t.Fatal(err)
	}
	rt.Provider = providers.NewFakeProvider()
	rt.Provider.(*providers.FakeProvider).ClassifierResponse = `not-json`
	result, err := runChatExchange(context.Background(), rt, "session_classifier_fail_json", "hello", false, true, "")
	if err == nil || app.AsError(err).Category != app.CategoryClassifier {
		t.Fatalf("want classifier error, got err=%v result=%+v", err, result)
	}
}

func TestRunChatExchangeRejectsRawTrustedEvidence(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	rt, err := newRuntime(context.Background(), &globalOptions{StorageDir: t.TempDir(), Model: "fake/model"})
	if err != nil {
		t.Fatal(err)
	}
	fake := providers.NewFakeProvider()
	fake.ChatResponse = `{"stage":"validation","findings":[],"passed_checks":["tests passed"],"missing_evidence":[],"residual_risks":[],"verdict":"ready_for_done"}`
	rt.Provider = fake
	if _, err := rt.Tasks.Start("validation task"); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Tasks.Move(app.StageExecution); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Tasks.Move(app.StageValidation); err != nil {
		t.Fatal(err)
	}
	pc := rt.attachProviderToProcess()
	_, err = pc.RunExchange(context.Background(), process.ExchangeInput{SessionID: "session_validation_done", Input: "проверь", TrustedEvidence: []string{"go test ./... passed"}, RequireMemoryProposal: true})
	if err == nil || app.AsError(err).Code != "transition_precondition_failed" {
		t.Fatalf("raw trusted evidence should not pass validation: %v", err)
	}

	fake = providers.NewFakeProvider()
	fake.ChatResponse = `{"stage":"validation","findings":[],"passed_checks":["tests passed"],"missing_evidence":[],"residual_risks":[],"verdict":"ready_for_done"}`
	rt.Provider = fake
	pc = rt.attachProviderToProcess()
	result, err := pc.RunExchange(context.Background(), process.ExchangeInput{SessionID: "session_validation_done_2", Input: "проверь", TrustedEvidence: []string{process.NewTrustedEvidence("go test ./...", 0, "ok")}, RequireMemoryProposal: true})
	if err == nil {
		t.Fatalf("forgeable structured trusted evidence should not finish task: %+v", result)
	}
	if app.AsError(err).Code != "validation_failed" && app.AsError(err).Code != "transition_precondition_failed" {
		t.Fatalf("want trusted evidence rejection, got %v", err)
	}
}

func TestCustomOpenRouterBaseURLRequiresTrustAndDoesNotPersist(t *testing.T) {
	storageDir := t.TempDir()
	_, err := newRuntime(context.Background(), &globalOptions{StorageDir: storageDir, Model: "openai/gpt-4.1-mini", OpenRouterBaseURL: "https://gateway.example/api"})
	if err == nil || !strings.Contains(err.Error(), "untrusted_base_url") {
		t.Fatalf("want untrusted_base_url, got %v", err)
	}
	if _, err := newRuntime(context.Background(), &globalOptions{StorageDir: storageDir, Model: "openai/gpt-4.1-mini", OpenRouterBaseURL: "https://gateway.example/api", TrustOpenRouterBaseURL: true}); err != nil {
		t.Fatal(err)
	}
	cfg, err := app.NewConfigManager(storageDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OpenRouterBaseURL != app.DefaultOpenRouterBaseURL {
		t.Fatalf("trusted one-shot base URL persisted: %+v", cfg)
	}
}

func TestProfilesCreateSetShowCommands(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	storageDir := t.TempDir()
	cmd := newRootCommand(&globalOptions{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--json", "profiles", "create", "custom", "--display-name", "Custom", "--style", "language=en", "--format", "structure=bullets", "--constraint", "be exact"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	cmd = newRootCommand(&globalOptions{})
	out.Reset()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--json", "profiles", "set", "custom"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	cmd = newRootCommand(&globalOptions{})
	out.Reset()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--json", "profiles", "show"})
	if err := cmd.Execute(); err != nil || !strings.Contains(out.String(), `"id": "custom"`) {
		t.Fatalf("show active profile failed: err=%v out=%s", err, out.String())
	}
}

func TestProfileFlagAffectsRenderedPromptWithoutPersisting(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	storageDir := t.TempDir()
	cmd := newRootCommand(&globalOptions{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--model", "fake/model", "--profile", "senior", "--json", "chat", "--once", "--render-prompt", "--input", "Объясни memory layers"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Profile: senior") || !strings.Contains(out.String(), "prefer_tradeoffs") {
		t.Fatalf("senior profile override missing from prompt: %s", out.String())
	}
	cfg, err := app.NewConfigManager(storageDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ActiveProfileID != "" {
		t.Fatalf("profile flag persisted unexpectedly: %+v", cfg)
	}
}

func TestProfileFlagMemoryProposalAppliesToGenerationProfile(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	storageDir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := newRootCommand(&globalOptions{})
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetArgs(append([]string{"--storage-dir", storageDir}, args...))
		if err := cmd.Execute(); err != nil {
			t.Fatalf("command %v failed: %v output=%s", args, err, out.String())
		}
		return out.String()
	}
	run("task", "start", "profile memory")
	run("--model", "fake/model", "--profile", "senior", "--json", "chat", "--once", "--input", "Запомни: я предпочитаю короткие ответы на русском")
	run("--model", "fake/model", "--json", "memory", "apply", "--accept", "all")
	records, err := memory.NewManager(storageDir).List(context.Background(), app.LayerLong, "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if strings.Contains(record.Content, "короткие ответы на русском") {
			if record.ProfileID != "senior" || record.Scope != "profile" {
				t.Fatalf("long preference used wrong profile: %+v", record)
			}
			return
		}
	}
	t.Fatalf("long preference was not saved: %+v", records)
}

func TestREPLProfileSwitchAffectsNextExchange(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	rt, err := newRuntime(context.Background(), &globalOptions{StorageDir: t.TempDir(), Model: "fake/model"})
	if err != nil {
		t.Fatal(err)
	}
	var out, diag bytes.Buffer
	input := strings.NewReader("/profile senior\nОбъясни memory layers\n/exit\n")
	if err := runREPL(context.Background(), input, &out, &diag, rt); err != nil {
		t.Fatalf("repl failed: %v diag=%s out=%s", err, diag.String(), out.String())
	}
	text := out.String()
	if !strings.Contains(text, "active profile: senior") || !strings.Contains(text, "senior profile") {
		t.Fatalf("profile switch did not affect next exchange: out=%s diag=%s", text, diag.String())
	}
}

func TestREPLProfileCreateActivatesProfile(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	rt, err := newRuntime(context.Background(), &globalOptions{StorageDir: t.TempDir(), Model: "fake/model"})
	if err != nil {
		t.Fatal(err)
	}
	var out, diag bytes.Buffer
	input := strings.NewReader("/profile create custom\n/profile\n/exit\n")
	if err := runREPL(context.Background(), input, &out, &diag, rt); err != nil {
		t.Fatalf("repl failed: %v diag=%s out=%s", err, diag.String(), out.String())
	}
	text := out.String()
	if !strings.Contains(text, "created and active profile: custom") || !strings.Contains(text, "active profile: custom") {
		t.Fatalf("created profile was not active: out=%s diag=%s", text, diag.String())
	}
}

func TestNonInteractiveREPLExitPreservesPriorFailure(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	rt, err := newRuntime(context.Background(), &globalOptions{StorageDir: t.TempDir(), Model: "fake/model"})
	if err != nil {
		t.Fatal(err)
	}
	var out, diag bytes.Buffer
	input := strings.NewReader("/unknown\n/task status\n/exit\n")
	err = runREPL(context.Background(), input, &out, &diag, rt)
	if err == nil || app.AsError(err).Code != "batch_failed" {
		t.Fatalf("want batch_failed after prior slash error, got %v out=%s diag=%s", err, out.String(), diag.String())
	}
	if !strings.Contains(diag.String(), "unknown slash command") || !strings.Contains(diag.String(), "missing_current_task") {
		t.Fatalf("REPL did not continue before batch failure: out=%s diag=%s", out.String(), diag.String())
	}
}

func TestTextProfilesShowIncludesPreferences(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	cmd := newRootCommand(&globalOptions{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--storage-dir", t.TempDir(), "profiles", "show", "student"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "style:") || !strings.Contains(text, "response_format:") || !strings.Contains(text, "constraints:") {
		t.Fatalf("profile details missing from text output: %s", text)
	}
}

func TestMemoryListLongFiltersActiveProfileByDefault(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	storageDir := t.TempDir()
	mem := memory.NewManager(storageDir)
	if _, err := mem.Save(context.Background(), memory.SaveInput{Layer: app.LayerLong, Kind: "preference", Content: "student only", ProfileID: "student"}); err != nil {
		t.Fatal(err)
	}
	if _, err := mem.Save(context.Background(), memory.SaveInput{Layer: app.LayerLong, Kind: "preference", Content: "senior only", ProfileID: "senior"}); err != nil {
		t.Fatal(err)
	}
	cmd := newRootCommand(&globalOptions{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--profile", "student", "memory", "list", "long"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "student only") || strings.Contains(out.String(), "senior only") {
		t.Fatalf("profile filter failed: %s", out.String())
	}
	cmd = newRootCommand(&globalOptions{})
	out.Reset()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--profile", "student", "memory", "list", "long", "--all-profiles"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "student only") || !strings.Contains(out.String(), "senior only") {
		t.Fatalf("all profile listing failed: %s", out.String())
	}
}

func TestMemoryApplyRawParsesProposalID(t *testing.T) {
	opts, err := parseMemoryApplyArgsRaw("/memory apply --proposal proposal_1 --accept all")
	if err != nil {
		t.Fatal(err)
	}
	if opts.ProposalID != "proposal_1" || !opts.AcceptAll {
		t.Fatalf("bad parse: %+v", opts)
	}
}

func TestInitMissingModelDoesNotCreateStorage(t *testing.T) {
	t.Setenv("ASSISTANT_MODEL", "")
	t.Setenv("ASSISTANT_MEMORY_MODEL", "")
	storageDir := filepath.Join(t.TempDir(), "missing")
	cmd := newRootCommand(&globalOptions{})
	cmd.SetArgs([]string{"--storage-dir", storageDir, "init"})
	if err := cmd.Execute(); err == nil || app.AsError(err).Code != "missing_model" {
		t.Fatalf("want missing_model, got %v", err)
	}
	if _, err := os.Stat(storageDir); !os.IsNotExist(err) {
		t.Fatalf("storage created before validation: %v", err)
	}
}

func TestPrintErrorTextIncludesHint(t *testing.T) {
	var out bytes.Buffer
	printError(&out, app.ErrorWithHint(app.CategoryCLI, "bad", "bad input", "do this", nil), false)
	if !strings.Contains(out.String(), "hint: do this") {
		t.Fatalf("hint missing: %s", out.String())
	}
}

func TestTopLevelInvalidCommandCanEmitJSONError(t *testing.T) {
	opts := &globalOptions{}
	args := []string{"--json", "definitely-not-a-command"}
	cmd := newRootCommand(opts)
	cmd.SetArgs(args)
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected invalid command error")
	}
	var stderr bytes.Buffer
	normalized := normalizeTopLevelCLIError(err)
	printError(&stderr, normalized, opts.JSON || argvRequestsJSON(args))
	if app.AsError(normalized).Code != "command_error" || app.ExitCode(normalized) != 2 {
		t.Fatalf("bad normalized error: %v", normalized)
	}
	var payload struct {
		OK    bool      `json:"ok"`
		Error app.Error `json:"error"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &payload); err != nil {
		t.Fatalf("stderr is not JSON: %q err=%v", stderr.String(), err)
	}
	if payload.OK || payload.Error.Category != app.CategoryCLI || payload.Error.Code != "command_error" {
		t.Fatalf("bad JSON error payload: %+v raw=%s", payload, stderr.String())
	}
}

func TestJSONREPLRejectedAndBatchErrorsUseStderrAndNonZero(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	storageDir := t.TempDir()
	cmd := newRootCommand(&globalOptions{})
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--json", "chat"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "json_repl_unsupported") {
		t.Fatalf("want json_repl_unsupported, got %v", err)
	}
	rt, err := newRuntime(context.Background(), &globalOptions{StorageDir: storageDir, Model: "fake/model"})
	if err != nil {
		t.Fatal(err)
	}
	var out, diag bytes.Buffer
	err = runREPL(context.Background(), strings.NewReader("/unknown\n"), &out, &diag, rt)
	if err == nil || !strings.Contains(err.Error(), "batch_failed") || out.Len() != 0 || !strings.Contains(diag.String(), "unknown slash command") {
		t.Fatalf("bad batch error routing err=%v out=%q diag=%q", err, out.String(), diag.String())
	}
}

func TestParseEditPreservesCommaContent(t *testing.T) {
	id, edit, err := parseEdit("rec1:layer=long,content=foo, bar, baz")
	if err != nil {
		t.Fatal(err)
	}
	if id != "rec1" || edit.Layer != app.ProposedLayerLong || edit.Content != "foo, bar, baz" {
		t.Fatalf("bad edit parse: id=%s edit=%+v", id, edit)
	}
}

func TestTextOutputEscapesControlCharacters(t *testing.T) {
	text := memoryText([]app.MemoryRecord{{Layer: app.LayerShort, Kind: "context", Content: "hello\x1b[31m\u200d"}})
	if strings.ContainsRune(text, rune(0x1b)) || strings.ContainsRune(text, '\u200d') || !strings.Contains(text, `\x1b`) || !strings.Contains(text, `\u200d`) {
		t.Fatalf("control char not escaped: %q", text)
	}
	task := taskText(app.TaskState{ID: "task_test", Title: "bad\x1btitle", Stage: app.StagePlanning, CurrentStep: "step\x1b[31m", ExpectedAction: app.ExpectedUserInput, Status: app.TaskStatusActive})
	if strings.ContainsRune(task, rune(0x1b)) || !strings.Contains(task, `\x1b`) {
		t.Fatalf("task text control char not escaped: %q", task)
	}
}

func TestProviderDisclosurePrintedOnceForOpenRouter(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "")
	rt, err := newRuntime(context.Background(), &globalOptions{StorageDir: t.TempDir(), Model: "openai/gpt-4.1-mini"})
	if err != nil {
		t.Fatal(err)
	}
	rt.Provider = providers.NewOpenRouterProvider(rt.Config.OpenRouterBaseURL)
	var out bytes.Buffer
	ensureProviderDisclosure(&out, rt)
	ensureProviderDisclosure(&out, rt)
	if strings.Count(out.String(), "Provider disclosure:") != 1 || !strings.Contains(out.String(), "OPENROUTER_API_KEY") {
		t.Fatalf("bad disclosure output: %q", out.String())
	}
}

func TestSemanticValidationEnabledDefaultsAndOverrides(t *testing.T) {
	t.Setenv("ASSISTANT_LLM_VALIDATION", "")
	if semanticValidationEnabled(providers.NewFakeProvider()) {
		t.Fatal("fake provider should not enable semantic validation by default")
	}
	if !semanticValidationEnabled(providers.NewOpenRouterProvider(app.DefaultOpenRouterBaseURL)) {
		t.Fatal("openrouter provider should enable semantic validation by default")
	}
	t.Setenv("ASSISTANT_LLM_VALIDATION", "off")
	if semanticValidationEnabled(providers.NewOpenRouterProvider(app.DefaultOpenRouterBaseURL)) {
		t.Fatal("off override should disable semantic validation")
	}
	t.Setenv("ASSISTANT_LLM_VALIDATION", "on")
	if !semanticValidationEnabled(providers.NewFakeProvider()) {
		t.Fatal("on override should enable semantic validation")
	}
}

func TestInitDoesNotRequireProviderLookup(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "")
	t.Setenv("OPENROUTER_API_KEY", "")
	cmd := newRootCommand(&globalOptions{})
	var out, stderr bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--storage-dir", t.TempDir(), "--model", "openai/gpt-4.1-mini", "init"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v output=%s stderr=%s", err, out.String(), stderr.String())
	}
	if strings.Contains(stderr.String(), "Provider disclosure:") {
		t.Fatalf("init should not contact provider or print disclosure: %q", stderr.String())
	}
}

func TestMemoryProposeDisclosesProviderBeforeModelLookup(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "")
	t.Setenv("OPENROUTER_API_KEY", "")
	storageDir := t.TempDir()
	mem := memory.NewManager(storageDir)
	if _, _, err := mem.SaveShortExchange(context.Background(), "session_disclose", "student", "", "hello", "hi"); err != nil {
		t.Fatal(err)
	}
	cmd := newRootCommand(&globalOptions{})
	var out, stderr bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--model", "openai/gpt-4.1-mini", "memory", "propose"})
	if err := cmd.Execute(); err == nil || app.AsError(err).Code != "missing_api_key" {
		t.Fatalf("want missing_api_key after disclosure, got %v", err)
	}
	if !strings.Contains(stderr.String(), "Provider disclosure:") {
		t.Fatalf("provider disclosure missing: %q", stderr.String())
	}
}

func TestSlashModelDisclosesProviderBeforeLookup(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "")
	t.Setenv("OPENROUTER_API_KEY", "")
	rt, err := newRuntime(context.Background(), &globalOptions{StorageDir: t.TempDir(), Model: "openai/gpt-4.1-mini"})
	if err != nil {
		t.Fatal(err)
	}
	rt.Provider = providers.NewOpenRouterProvider(rt.Config.OpenRouterBaseURL)
	var out, diag bytes.Buffer
	if _, err := handleSlash(context.Background(), &out, &diag, rt, "session_test", "/model openai/gpt-4.1-mini"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diag.String(), "Provider disclosure:") {
		t.Fatalf("provider disclosure missing: %q", diag.String())
	}
}

func TestPromptAuditMetadataOnlyUnlessRawOptIn(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	t.Setenv("ASSISTANT_PROMPT_AUDIT", "1")
	storageDir := t.TempDir()
	cmd := newRootCommand(&globalOptions{})
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--model", "fake/model", "chat", "--once", "--input", "hello"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(storageDir, "sessions", "*", "prompts.jsonl"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("prompt audit missing matches=%v err=%v", matches, err)
	}
	body, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "rendered_prompt_sha256") || strings.Contains(text, `"rendered_prompt":`) || strings.Contains(text, `"messages":`) {
		t.Fatalf("metadata audit leaked raw prompt/messages: %s", text)
	}
}

func TestQuietSuppressesNonessentialWarnings(t *testing.T) {
	rt, err := newRuntime(context.Background(), &globalOptions{StorageDir: t.TempDir(), Quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	if !rt.Quiet {
		t.Fatal("quiet flag not propagated into runtime")
	}
}

func TestChatBlocksSecretsBeforeProviderCall(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	storageDir := t.TempDir()
	rt, err := newRuntime(context.Background(), &globalOptions{StorageDir: storageDir, Model: "fake/model"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runChatExchange(context.Background(), rt, "session_secret", "OPENROUTER_API_KEY=sk-secret123456789", false, false, "")
	if err == nil || !strings.Contains(err.Error(), "secret_blocked") {
		t.Fatalf("want secret_blocked, got %v", err)
	}
}

func TestFakeClassifierResponseEnvKeepsChatVisible(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	t.Setenv("ASSISTANT_FAKE_CLASSIFIER_RESPONSE", "not-json")
	storageDir := t.TempDir()
	cmd := newRootCommand(&globalOptions{})
	var out, stderr bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--model", "fake/model", "--json", "chat", "--once", "--input", "объясни Go MVP"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat should survive classifier failure: %v stderr=%s out=%s", err, stderr.String(), out.String())
	}
	if !strings.Contains(out.String(), `"ok": true`) || !strings.Contains(out.String(), "memory proposal skipped: invalid_json") {
		t.Fatalf("classifier failure did not preserve answer with warning: out=%s stderr=%s", out.String(), stderr.String())
	}
}

func TestPausedTaskBlocksWorkSaveSlash(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	storageDir := t.TempDir()
	rt, err := newRuntime(context.Background(), &globalOptions{StorageDir: storageDir, Model: "fake/model"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Tasks.Pause(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if _, err := handleSlash(context.Background(), &out, &out, rt, "session_test", "/save work must not save"); err == nil || !strings.Contains(err.Error(), "task_paused") {
		t.Fatalf("want task_paused, got %v", err)
	}
}

func TestPrivacyPurgeRemovesAuditAndTranscriptsOnly(t *testing.T) {
	storageDir := t.TempDir()
	sessionDir := filepath.Join(storageDir, "sessions", "session_test")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"memory_proposals.jsonl": "{}\n",
		"prompts.jsonl":          "{}\n",
		"transcript.md":          "raw transcript",
		"short_term.jsonl":       "{}\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(sessionDir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(storageDir, "process_audit.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := newRootCommand(&globalOptions{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--json", "privacy", "purge", "--audit", "--transcripts", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(sessionDir, "memory_proposals.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("audit not purged: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sessionDir, "prompts.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("prompt audit not purged: %v", err)
	}
	if _, err := os.Stat(filepath.Join(storageDir, "process_audit.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("process audit not purged: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sessionDir, "transcript.md")); !os.IsNotExist(err) {
		t.Fatalf("transcript not purged: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sessionDir, "short_term.jsonl")); err != nil {
		t.Fatalf("memory layer removed unexpectedly: %v", err)
	}
}

func TestPrivacyPurgeRejectsSymlinkAuditFile(t *testing.T) {
	storageDir := t.TempDir()
	sessionDir := filepath.Join(storageDir, "sessions", "session_test")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(storageDir, "outside.jsonl")
	if err := os.WriteFile(target, []byte("secret audit"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(sessionDir, "memory_proposals.jsonl")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	cmd := newRootCommand(&globalOptions{})
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--json", "privacy", "purge", "--audit", "--yes"})
	if err := cmd.Execute(); err == nil || app.AsError(err).Code != "privacy_purge" {
		t.Fatalf("want privacy_purge symlink rejection, got %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("purge followed symlink target: %v", err)
	}
}
