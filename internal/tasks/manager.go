package tasks

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/storage"
)

type Manager struct {
	StorageDir string
}

func NewManager(storageDir string) *Manager { return &Manager{StorageDir: storageDir} }

func (m *Manager) currentPath() string { return filepath.Join(m.StorageDir, "tasks", "current.json") }

func (m *Manager) taskPath(id string) (string, error) {
	if err := storage.ValidateID(id); err != nil {
		return "", app.NewError(app.CategoryValidation, "unsafe_task_id", "unsafe task id", err)
	}
	return filepath.Join(m.StorageDir, "tasks", id+".json"), nil
}

func (m *Manager) Start(title string) (app.TaskState, error) {
	if strings.TrimSpace(title) == "" {
		return app.TaskState{}, app.NewError(app.CategoryCLI, "missing_title", "task title is required", nil)
	}
	now := time.Now().UTC()
	state := app.TaskState{
		ID:                 app.NewID("task"),
		Title:              strings.TrimSpace(title),
		Stage:              app.StagePlanning,
		Status:             app.TaskStatusActive,
		ExpectedAction:     app.ExpectedUserInput,
		CurrentStep:        "",
		Objective:          strings.TrimSpace(title),
		AcceptanceCriteria: []string{},
		Plan:               []string{},
		Decisions:          []string{},
		OpenQuestions:      []string{},
		UpdatedAt:          now,
	}
	return state, m.saveBoth(state)
}

func (m *Manager) Current() (app.TaskState, error) {
	var state app.TaskState
	if err := storage.ReadJSON(m.currentPath(), &state); err != nil {
		if errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "open") {
			return state, app.NewError(app.CategoryValidation, "missing_current_task", "current task does not exist", err)
		}
		return state, app.NewError(app.CategoryStorage, "task_read", err.Error(), err)
	}
	return state, nil
}

func (m *Manager) Move(next app.TaskStage) (app.TaskState, error) {
	if !ValidStage(next) {
		return app.TaskState{}, app.NewError(app.CategoryValidation, "invalid_stage", "invalid task stage", nil)
	}
	state, err := m.Current()
	if err != nil {
		return state, err
	}
	if state.Status == app.TaskStatusPaused {
		return state, app.NewError(app.CategoryValidation, "task_paused", "resume task before moving stage", nil)
	}
	if !IsAllowed(state.Stage, next) {
		return state, app.NewError(app.CategoryValidation, "forbidden_transition", "forbidden task stage transition", nil)
	}
	state.Stage = next
	state.UpdatedAt = time.Now().UTC()
	if next == app.StageDone {
		state.ExpectedAction = app.ExpectedNone
		state.Status = app.TaskStatusActive
	}
	return state, m.saveBoth(state)
}

func (m *Manager) SetStep(step string) (app.TaskState, error) {
	return m.mutateActive(func(state *app.TaskState) error {
		state.CurrentStep = strings.TrimSpace(step)
		return nil
	})
}

func (m *Manager) SetExpectedAction(action app.ExpectedAction) (app.TaskState, error) {
	if !ValidExpectedAction(action) {
		return app.TaskState{}, app.NewError(app.CategoryValidation, "invalid_expected_action", "invalid expected action", nil)
	}
	return m.mutateActive(func(state *app.TaskState) error {
		state.ExpectedAction = action
		return nil
	})
}

func (m *Manager) Pause() (app.TaskState, error) {
	state, err := m.Current()
	if err != nil {
		return state, err
	}
	if state.Stage == app.StageDone {
		state.Status = app.TaskStatusActive
		state.ExpectedAction = app.ExpectedNone
		return state, m.saveBoth(state)
	}
	now := time.Now().UTC()
	state.Status = app.TaskStatusPaused
	state.PausedAt = &now
	state.UpdatedAt = now
	return state, m.saveBoth(state)
}

func (m *Manager) Resume() (app.TaskState, error) {
	state, err := m.Current()
	if err != nil {
		return state, err
	}
	if state.Stage == app.StageDone {
		state.Status = app.TaskStatusActive
		state.ExpectedAction = app.ExpectedNone
		return state, m.saveBoth(state)
	}
	if state.Status != app.TaskStatusPaused {
		return state, app.NewError(app.CategoryValidation, "task_not_paused", "current task is not paused", nil)
	}
	now := time.Now().UTC()
	state.Status = app.TaskStatusActive
	state.ResumedAt = &now
	state.UpdatedAt = now
	return state, m.saveBoth(state)
}

func (m *Manager) mutateActive(fn func(*app.TaskState) error) (app.TaskState, error) {
	state, err := m.Current()
	if err != nil {
		return state, err
	}
	if state.Status == app.TaskStatusPaused {
		return state, app.NewError(app.CategoryValidation, "task_paused", "resume task before mutating task state", nil)
	}
	if state.Stage == app.StageDone {
		return state, app.NewError(app.CategoryValidation, "task_done", "done task is terminal", nil)
	}
	if err := fn(&state); err != nil {
		return state, err
	}
	state.UpdatedAt = time.Now().UTC()
	return state, m.saveBoth(state)
}

func (m *Manager) saveBoth(state app.TaskState) error {
	if err := storage.EnsureDir(filepath.Join(m.StorageDir, "tasks")); err != nil {
		return app.NewError(app.CategoryStorage, "task_dir", err.Error(), err)
	}
	path, err := m.taskPath(state.ID)
	if err != nil {
		return err
	}
	if err := storage.AtomicWriteJSON(path, state); err != nil {
		return app.NewError(app.CategoryStorage, "task_write", err.Error(), err)
	}
	if err := storage.AtomicWriteJSON(m.currentPath(), state); err != nil {
		return app.NewError(app.CategoryStorage, "task_write", err.Error(), err)
	}
	return nil
}
