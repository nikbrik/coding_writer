package tasks

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nikbrik/coding_writer/internal/app"
)

func TestTaskStateMachinePauseResume(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	state, err := mgr.Start("CLI assistant MVP")
	if err != nil {
		t.Fatal(err)
	}
	if state.Stage != app.StagePlanning || state.Status != app.TaskStatusActive || state.ExpectedAction != app.ExpectedUserInput {
		t.Fatalf("bad initial state: %+v", state)
	}
	state, err = mgr.SetStep("реализовать MemoryManager")
	if err != nil {
		t.Fatal(err)
	}
	state, err = mgr.SetExpectedAction(app.ExpectedLLMResponse)
	if err != nil {
		t.Fatal(err)
	}
	state, err = mgr.Move(app.StageExecution)
	if err != nil {
		t.Fatal(err)
	}
	if state.Stage != app.StageExecution || state.CurrentStep != "реализовать MemoryManager" || state.ExpectedAction != app.ExpectedLLMResponse {
		t.Fatalf("state not persisted: %+v", state)
	}
	state, err = mgr.Pause()
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != app.TaskStatusPaused || state.Stage != app.StageExecution || state.CurrentStep != "реализовать MemoryManager" {
		t.Fatalf("pause lost state: %+v", state)
	}
	restarted := NewManager(dir)
	state, err = restarted.Resume()
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != app.TaskStatusActive || state.Stage != app.StageExecution || state.ExpectedAction != app.ExpectedLLMResponse {
		t.Fatalf("resume lost state: %+v", state)
	}
}

func TestTaskPlanCriteriaPersistAfterRestart(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	if _, err := mgr.Start("test"); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.AddPlanItem("build memory manager"); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.AddCriteria("memory layers are separate files"); err != nil {
		t.Fatal(err)
	}
	restarted := NewManager(dir)
	state, err := restarted.Current()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Plan) != 1 || state.Plan[0] != "build memory manager" || len(state.AcceptanceCriteria) != 1 || state.AcceptanceCriteria[0] != "memory layers are separate files" {
		t.Fatalf("plan/criteria not restored: %+v", state)
	}
}

func TestForbiddenTransitionDoesNotMutateCurrentFile(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	if _, err := mgr.Start("test"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "tasks", "current.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Move(app.StageValidation); err == nil {
		t.Fatal("expected forbidden transition")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("current.json mutated after forbidden transition")
	}
}

func TestDoneStageUsesExpectedNoneNoStatusDone(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	if _, err := mgr.Start("test"); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Move(app.StageExecution); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Move(app.StageValidation); err != nil {
		t.Fatal(err)
	}
	state, err := mgr.Move(app.StageDone)
	if err != nil {
		t.Fatal(err)
	}
	if state.Stage != app.StageDone || state.ExpectedAction != app.ExpectedNone || state.Status != app.TaskStatusActive {
		t.Fatalf("bad done state: %+v", state)
	}
	state, err = mgr.Pause()
	if err != nil {
		t.Fatal(err)
	}
	if state.Stage != app.StageDone || state.Status != app.TaskStatusActive {
		t.Fatalf("done pause reopened task: %+v", state)
	}
}

func TestTaskLostUpdateGuard(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	state, err := mgr.Start("test")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := mgr.currentSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if _, err := mgr.SetStep("newer step"); err != nil {
		t.Fatal(err)
	}
	state.CurrentStep = "stale step"
	err = mgr.saveBothIfUnchanged(state, &snapshot)
	if err == nil || !strings.Contains(err.Error(), "task_lost_update") {
		t.Fatalf("want task_lost_update, got %v", err)
	}
	current, err := mgr.Current()
	if err != nil {
		t.Fatal(err)
	}
	if current.CurrentStep != "newer step" {
		t.Fatalf("lost update guard failed: %+v", current)
	}
}

func TestTaskRejectsSecretContent(t *testing.T) {
	mgr := NewManager(t.TempDir())
	if _, err := mgr.Start("OPENROUTER_API_KEY=sk-secret123456789"); err == nil || !strings.Contains(err.Error(), "secret_blocked") {
		t.Fatalf("want secret blocked on start, got %v", err)
	}
	if _, err := mgr.Start("safe task"); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.SetStep("Bearer abcdefghijklmnop"); err == nil || !strings.Contains(err.Error(), "secret_blocked") {
		t.Fatalf("want secret blocked on step, got %v", err)
	}
}

func TestCurrentRejectsInvalidPersistedState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "current.json"), []byte(`{"id":"task_bad","title":"bad","stage":"done","status":"active","expected_action":"llm_response","updated_at":"2026-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewManager(dir).Current()
	if err == nil || !strings.Contains(err.Error(), "invalid_task_state") {
		t.Fatalf("want invalid_task_state, got %v", err)
	}
}
