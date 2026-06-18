package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/providers"
)

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
	mutated, err := filepath.Glob(filepath.Join(storageDir, "sessions", "*", "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(mutated) != 0 {
		t.Fatalf("paused informational chat must not write session memory/proposals: %v", mutated)
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
	if err == nil || !strings.Contains(err.Error(), "task_paused") {
		t.Fatalf("want task_paused for memory apply while paused, got %v output=%s", err, out.String())
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

func TestTopLevelTaskPlanCriteriaCommands(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	storageDir := t.TempDir()
	args := [][]string{
		{"--storage-dir", storageDir, "--model", "fake/model", "--json", "task", "start", "CLI assistant MVP"},
		{"--storage-dir", storageDir, "--model", "fake/model", "--json", "task", "plan", "build memory manager"},
		{"--storage-dir", storageDir, "--model", "fake/model", "--json", "task", "criteria", "memory layers are separate files"},
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
	rt, err := newRuntime(context.Background(), &globalOptions{StorageDir: storageDir, Model: "fake/model"})
	if err != nil {
		t.Fatal(err)
	}
	state, err := rt.Tasks.Current()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Plan) != 1 || len(state.AcceptanceCriteria) != 1 {
		t.Fatalf("plan/criteria commands failed: %+v", state)
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

func TestClassifierFailureReturnsAnswerWithWarning(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	rt, err := newRuntime(context.Background(), &globalOptions{StorageDir: t.TempDir(), Model: "fake/model"})
	if err != nil {
		t.Fatal(err)
	}
	rt.Provider = providers.NewFakeProvider()
	rt.Provider.(*providers.FakeProvider).ClassifierResponse = `not-json`
	result, err := runChatExchange(context.Background(), rt, "session_classifier_fail", "hello", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Answer == "" || len(result.Warnings) == 0 || result.Proposal != nil {
		t.Fatalf("classifier failure did not return answer with warning: %+v", result)
	}
}

func TestRunChatExchangePassesTrustedEvidenceToValidation(t *testing.T) {
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
	result, err := runChatExchange(context.Background(), rt, "session_validation_done", "проверь", false, []string{"go test ./... passed"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Transition == nil || result.Transition.To != app.StageDone {
		t.Fatalf("trusted evidence was not passed to transition gate: %+v", result.Transition)
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

func TestChatBlocksSecretsBeforeProviderCall(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	storageDir := t.TempDir()
	rt, err := newRuntime(context.Background(), &globalOptions{StorageDir: storageDir, Model: "fake/model"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runChatExchange(context.Background(), rt, "session_secret", "OPENROUTER_API_KEY=sk-secret123456789", false, nil)
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
