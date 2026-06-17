package tasks

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

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
