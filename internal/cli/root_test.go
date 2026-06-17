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
	if _, err := handleSlash(context.Background(), &out, rt, "session_test", "/model missing/model"); err == nil {
		t.Fatal("expected invalid model error")
	}
	cfg, err := app.NewConfigManager(storageDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ActiveModel != "fake/model" {
		t.Fatalf("invalid model mutated config: %+v", cfg)
	}
}

func TestInvalidModelFlagDoesNotMutateConfig(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	storageDir := t.TempDir()
	if _, err := newRuntime(context.Background(), &globalOptions{StorageDir: storageDir, Model: "fake/model"}); err != nil {
		t.Fatal(err)
	}
	if _, err := newRuntime(context.Background(), &globalOptions{StorageDir: storageDir, Model: "missing/model"}); err == nil {
		t.Fatal("expected invalid model error")
	}
	cfg, err := app.NewConfigManager(storageDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ActiveModel != "fake/model" {
		t.Fatalf("invalid model flag mutated config: %+v", cfg)
	}
}

func TestInvalidModelSyntaxRejectedWithoutProviderCall(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	if _, err := newRuntime(context.Background(), &globalOptions{StorageDir: t.TempDir(), Model: "badmodel"}); err == nil || !strings.Contains(err.Error(), "invalid_model") {
		t.Fatalf("want invalid_model, got %v", err)
	}
}

func TestTopLevelTaskCommandsAndPausedGate(t *testing.T) {
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
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--model", "fake/model", "--json", "chat", "--once", "--input", "Продолжай задачу."})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "task_paused") {
		t.Fatalf("want paused gate error, got %v output=%s", err, out.String())
	}
	cmd = newRootCommand(&globalOptions{})
	out.Reset()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--storage-dir", storageDir, "--model", "fake/model", "--json", "chat", "--once", "--input", "Объясни memory layers."})
	err = cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "task_paused") {
		t.Fatalf("want paused gate for all chat, got %v output=%s", err, out.String())
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
	if !strings.Contains(out.String(), "Memory proposal:") || !strings.Contains(out.String(), "[work]") || !strings.Contains(out.String(), "[long]") {
		t.Fatalf("proposal records not visible: %s", out.String())
	}
}

func TestTextOutputEscapesControlCharacters(t *testing.T) {
	text := memoryText([]app.MemoryRecord{{Layer: app.LayerShort, Kind: "context", Content: "hello\x1b[31m"}})
	if strings.ContainsRune(text, rune(0x1b)) || !strings.Contains(text, `\x1b`) {
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
	var out bytes.Buffer
	ensureProviderDisclosure(&out, rt)
	ensureProviderDisclosure(&out, rt)
	if strings.Count(out.String(), "Provider disclosure:") != 1 || !strings.Contains(out.String(), "OPENROUTER_API_KEY") {
		t.Fatalf("bad disclosure output: %q", out.String())
	}
}

func TestChatBlocksSecretsBeforeProviderCall(t *testing.T) {
	t.Setenv("ASSISTANT_PROVIDER", "fake")
	storageDir := t.TempDir()
	rt, err := newRuntime(context.Background(), &globalOptions{StorageDir: storageDir, Model: "fake/model"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runChatExchange(context.Background(), rt, "session_secret", "OPENROUTER_API_KEY=sk-secret123456789", false)
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
	if _, err := handleSlash(context.Background(), &out, rt, "session_test", "/save work must not save"); err == nil || !strings.Contains(err.Error(), "task_paused") {
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
		"transcript.md":          "raw transcript",
		"short_term.jsonl":       "{}\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(sessionDir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
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
	if _, err := os.Stat(filepath.Join(sessionDir, "transcript.md")); !os.IsNotExist(err) {
		t.Fatalf("transcript not purged: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sessionDir, "short_term.jsonl")); err != nil {
		t.Fatalf("memory layer removed unexpectedly: %v", err)
	}
}
