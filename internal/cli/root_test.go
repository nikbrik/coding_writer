package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	state, err := rt.Tasks.Move(app.StageDone)
	if err != nil {
		t.Fatal(err)
	}
	return state
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

	runJSON("task", "start", "CLI assistant MVP")
	chat := runJSON("chat", "--once", "--input", "Спланируй модуль памяти. Требование: CLI должен поддерживать выбор модели OpenRouter. Я предпочитаю короткие ответы на русском.")
	proposal, ok := chat["proposal"].(map[string]any)
	if !ok || proposal["id"] == "" || chat["rendered_prompt"] != nil || chat["rendered_prompt_id"] == nil {
		t.Fatalf("bad chat/proposal JSON: %+v", chat)
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
	runJSON("task", "expect", "llm_response")
	runJSON("task", "move", "execution")
	runJSON("task", "pause")
	resumed := runJSON("task", "resume")
	if !strings.Contains(fmt.Sprint(resumed["task"]), "реализовать MemoryManager") || !strings.Contains(fmt.Sprint(resumed["task"]), "llm_response") {
		t.Fatalf("resume did not preserve Day 13 state: %+v", resumed)
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

func TestTopLevelTaskPlanCriteriaCommandsAreNotP0(t *testing.T) {
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
		if err := cmd.Execute(); err == nil {
			t.Fatalf("P1 top-level command should be absent: %v", arg)
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
	if !strings.Contains(out.String(), "Memory proposal:") || !strings.Contains(out.String(), "[work] pending") || !strings.Contains(out.String(), "Next: assistant memory apply") {
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
	result, err := runChatExchange(context.Background(), rt, "session_classifier_fail", "hello", false, true)
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
	result, err := runChatExchange(context.Background(), rt, "session_classifier_fail_json", "hello", false, true)
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
	if err == nil || app.AsError(err).Code != "validation_failed" {
		t.Fatalf("raw trusted evidence should not pass validation: %v", err)
	}

	fake = providers.NewFakeProvider()
	fake.ChatResponse = `{"stage":"validation","findings":[],"passed_checks":["tests passed"],"missing_evidence":[],"residual_risks":[],"verdict":"ready_for_done"}`
	rt.Provider = fake
	pc = rt.attachProviderToProcess()
	result, err := pc.RunExchange(context.Background(), process.ExchangeInput{SessionID: "session_validation_done_2", Input: "проверь", TrustedEvidence: []string{process.NewTrustedEvidence("go test ./...", 0, "ok")}, RequireMemoryProposal: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Transition == nil || result.Transition.To != app.StageDone {
		t.Fatalf("structured trusted evidence was not passed to transition gate: %+v", result.Transition)
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

func TestInitDisclosesProviderBeforeModelLookup(t *testing.T) {
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
	if !strings.Contains(stderr.String(), "Provider disclosure:") {
		t.Fatalf("provider disclosure missing during init: %q", stderr.String())
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
	_, err = runChatExchange(context.Background(), rt, "session_secret", "OPENROUTER_API_KEY=sk-secret123456789", false, false)
	if err == nil || !strings.Contains(err.Error(), "secret_blocked") {
		t.Fatalf("want secret_blocked, got %v", err)
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
