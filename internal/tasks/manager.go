package tasks

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/storage"
	"github.com/nikbrik/coding_writer/internal/validation"
)

type Manager struct {
	StorageDir string
}

type currentSnapshot struct {
	State app.TaskState
	MTime time.Time
	Size  int64
}

func NewManager(storageDir string) *Manager { return &Manager{StorageDir: storageDir} }

func (m *Manager) currentPath() (string, error) {
	path, err := storage.SafeJoin(m.StorageDir, "tasks", "current.json")
	if err != nil {
		return "", app.NewError(app.CategoryValidation, "unsafe_task_path", "unsafe current task path", err)
	}
	return path, nil
}

func (m *Manager) taskPath(id string) (string, error) {
	if err := storage.ValidateID(id); err != nil {
		return "", app.NewError(app.CategoryValidation, "unsafe_task_id", "unsafe task id", err)
	}
	path, err := storage.SafeJoin(m.StorageDir, "tasks", id+".json")
	if err != nil {
		return "", app.NewError(app.CategoryValidation, "unsafe_task_path", "unsafe task path", err)
	}
	return path, nil
}

func (m *Manager) Start(title string) (app.TaskState, error) {
	if strings.TrimSpace(title) == "" {
		return app.TaskState{}, app.NewError(app.CategoryCLI, "missing_title", "task title is required", nil)
	}
	if validation.HasSecret(title) {
		return app.TaskState{}, app.NewError(app.CategoryValidation, "secret_blocked", "secret-like task content cannot be saved", nil)
	}
	if current, err := m.currentSnapshot(); err == nil && current.State.Stage != app.StageDone {
		return app.TaskState{}, app.NewError(app.CategoryValidation, "task_already_exists", "a current task already exists; finish or archive it before starting a new one", nil)
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
	snapshot, err := m.currentSnapshot()
	if err != nil {
		return app.TaskState{}, err
	}
	return snapshot.State, nil
}

func (m *Manager) currentSnapshot() (currentSnapshot, error) {
	var state app.TaskState
	path, err := m.currentPath()
	if err != nil {
		return currentSnapshot{}, err
	}
	before, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return currentSnapshot{}, app.NewError(app.CategoryValidation, "missing_current_task", "current task does not exist", err)
		}
		return currentSnapshot{}, app.NewError(app.CategoryStorage, "task_stat", err.Error(), err)
	}
	if err := storage.ReadJSON(path, &state); err != nil {
		if errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "open") {
			return currentSnapshot{}, app.NewError(app.CategoryValidation, "missing_current_task", "current task does not exist", err)
		}
		return currentSnapshot{}, app.NewError(app.CategoryStorage, "task_read", err.Error(), err)
	}
	after, err := os.Stat(path)
	if err != nil {
		return currentSnapshot{}, app.NewError(app.CategoryStorage, "task_stat", err.Error(), err)
	}
	if !before.ModTime().Equal(after.ModTime()) || before.Size() != after.Size() {
		return currentSnapshot{}, app.NewError(app.CategoryStorage, "task_changed_during_read", "current task changed during read", nil)
	}
	return currentSnapshot{State: state, MTime: after.ModTime(), Size: after.Size()}, nil
}

func (m *Manager) Move(next app.TaskStage) (app.TaskState, error) {
	if !ValidStage(next) {
		return app.TaskState{}, app.NewError(app.CategoryValidation, "invalid_stage", "invalid task stage", nil)
	}
	snapshot, err := m.currentSnapshot()
	if err != nil {
		return app.TaskState{}, err
	}
	state := snapshot.State
	if state.Status == app.TaskStatusPaused {
		return state, app.NewError(app.CategoryValidation, "task_paused", "resume task before moving stage", nil)
	}
	if !IsAllowed(state.Stage, next) {
		return state, app.ErrorWithHint(app.CategoryValidation, "forbidden_transition", "forbidden task stage transition", fmt.Sprintf("allowed next stages from %s: %v", state.Stage, AllowedNext(state.Stage)), nil)
	}
	prevStage := state.Stage
	state.Stage = next
	state.ExpectedAction = defaultExpectedAction(next)
	state.UpdatedAt = time.Now().UTC()
	state.HistoryLog = append(state.HistoryLog, fmt.Sprintf("%s: %s -> %s", time.Now().UTC().Format(time.RFC3339), prevStage, next))
	if next == app.StageDone {
		state.Status = app.TaskStatusActive
	}
	return state, m.saveBothIfUnchanged(state, &snapshot)
}

func defaultExpectedAction(stage app.TaskStage) app.ExpectedAction {
	switch stage {
	case app.StagePlanning:
		return app.ExpectedUserInput
	case app.StageExecution:
		return app.ExpectedLLMResponse
	case app.StageValidation:
		return app.ExpectedUserConfirmation
	case app.StageDone:
		return app.ExpectedNone
	}
	return app.ExpectedUserInput
}

func (m *Manager) SetStep(step string) (app.TaskState, error) {
	if validation.HasSecret(step) {
		return app.TaskState{}, app.NewError(app.CategoryValidation, "secret_blocked", "secret-like task content cannot be saved", nil)
	}
	return m.mutateActive(func(state *app.TaskState) error {
		state.CurrentStep = strings.TrimSpace(step)
		return nil
	})
}

func (m *Manager) AddPlanItem(item string) (app.TaskState, error) {
	if strings.TrimSpace(item) == "" {
		return app.TaskState{}, app.NewError(app.CategoryCLI, "missing_plan_item", "plan item is required", nil)
	}
	if validation.HasSecret(item) {
		return app.TaskState{}, app.NewError(app.CategoryValidation, "secret_blocked", "secret-like task content cannot be saved", nil)
	}
	return m.mutateActive(func(state *app.TaskState) error {
		state.Plan = append(state.Plan, strings.TrimSpace(item))
		return nil
	})
}

func (m *Manager) AddCriteria(criteria string) (app.TaskState, error) {
	if strings.TrimSpace(criteria) == "" {
		return app.TaskState{}, app.NewError(app.CategoryCLI, "missing_criteria", "acceptance criteria is required", nil)
	}
	if validation.HasSecret(criteria) {
		return app.TaskState{}, app.NewError(app.CategoryValidation, "secret_blocked", "secret-like task content cannot be saved", nil)
	}
	return m.mutateActive(func(state *app.TaskState) error {
		state.AcceptanceCriteria = append(state.AcceptanceCriteria, strings.TrimSpace(criteria))
		return nil
	})
}

func (m *Manager) SetPlanningOutput(summary string, criteria, plan, openQuestions []string) (app.TaskState, error) {
	if validation.HasSecret(summary) || hasSecretIn(criteria) || hasSecretIn(plan) || hasSecretIn(openQuestions) {
		return app.TaskState{}, app.NewError(app.CategoryValidation, "secret_blocked", "secret-like task content cannot be saved", nil)
	}
	return m.mutateActive(func(state *app.TaskState) error {
		state.Objective = strings.TrimSpace(summary)
		state.AcceptanceCriteria = trimNonEmpty(criteria)
		state.Plan = trimNonEmpty(plan)
		state.OpenQuestions = trimNonEmpty(openQuestions)
		return nil
	})
}

func hasSecretIn(items []string) bool {
	for _, item := range items {
		if validation.HasSecret(item) {
			return true
		}
	}
	return false
}

func trimNonEmpty(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func (m *Manager) SetExpectedAction(action app.ExpectedAction) (app.TaskState, error) {
	if !ValidExpectedAction(action) {
		return app.TaskState{}, app.NewError(app.CategoryValidation, "invalid_expected_action", "invalid expected action", nil)
	}
	return m.mutateActive(func(state *app.TaskState) error {
		if state.Stage == app.StageDone && action != app.ExpectedNone {
			return app.NewError(app.CategoryValidation, "invalid_expected_action", "done task only allows expected_action none", nil)
		}
		if state.Stage != app.StageDone && action == app.ExpectedNone {
			return app.NewError(app.CategoryValidation, "invalid_expected_action", "expected_action none is only valid for done tasks", nil)
		}
		state.ExpectedAction = action
		return nil
	})
}

func (m *Manager) Pause() (app.TaskState, error) {
	snapshot, err := m.currentSnapshot()
	if err != nil {
		return app.TaskState{}, err
	}
	state := snapshot.State
	if state.Stage == app.StageDone {
		state.Status = app.TaskStatusActive
		state.ExpectedAction = app.ExpectedNone
		return state, m.saveBothIfUnchanged(state, &snapshot)
	}
	if state.Status == app.TaskStatusPaused {
		return state, nil
	}
	now := time.Now().UTC()
	state.Status = app.TaskStatusPaused
	state.PausedAt = &now
	state.UpdatedAt = now
	return state, m.saveBothIfUnchanged(state, &snapshot)
}

func (m *Manager) Resume() (app.TaskState, error) {
	snapshot, err := m.currentSnapshot()
	if err != nil {
		return app.TaskState{}, err
	}
	state := snapshot.State
	if state.Stage == app.StageDone {
		state.Status = app.TaskStatusActive
		state.ExpectedAction = app.ExpectedNone
		return state, m.saveBothIfUnchanged(state, &snapshot)
	}
	if state.Status != app.TaskStatusPaused {
		return state, app.NewError(app.CategoryValidation, "task_not_paused", "current task is not paused", nil)
	}
	now := time.Now().UTC()
	state.Status = app.TaskStatusActive
	state.ResumedAt = &now
	state.UpdatedAt = now
	return state, m.saveBothIfUnchanged(state, &snapshot)
}

func (m *Manager) mutateActive(fn func(*app.TaskState) error) (app.TaskState, error) {
	snapshot, err := m.currentSnapshot()
	if err != nil {
		return app.TaskState{}, err
	}
	state := snapshot.State
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
	return state, m.saveBothIfUnchanged(state, &snapshot)
}

func (m *Manager) saveBoth(state app.TaskState) error {
	return m.saveBothIfUnchanged(state, nil)
}

func (m *Manager) saveBothIfUnchanged(state app.TaskState, expected *currentSnapshot) error {
	currentPath, err := m.currentPath()
	if err != nil {
		return err
	}
	return storage.WithFileLock(currentPath, true, func() error {
		tasksDir, err := storage.SafeJoin(m.StorageDir, "tasks")
		if err != nil {
			return app.NewError(app.CategoryValidation, "unsafe_task_path", "unsafe task path", err)
		}
		if err := storage.EnsureDir(tasksDir); err != nil {
			return app.NewError(app.CategoryStorage, "task_dir", err.Error(), err)
		}
		if expected != nil {
			if err := m.ensureCurrentUnchanged(*expected); err != nil {
				return err
			}
		}
		path, err := m.taskPath(state.ID)
		if err != nil {
			return err
		}
		if err := storage.AtomicWriteJSON(path, state); err != nil {
			return app.NewError(app.CategoryStorage, "task_write", err.Error(), err)
		}
		if err := storage.AtomicWriteJSON(currentPath, state); err != nil {
			return app.NewError(app.CategoryStorage, "task_write", err.Error(), err)
		}
		return nil
	})
}

func (m *Manager) ensureCurrentUnchanged(expected currentSnapshot) error {
	currentPath, err := m.currentPath()
	if err != nil {
		return err
	}
	info, err := os.Stat(currentPath)
	if err != nil {
		return app.NewError(app.CategoryStorage, "task_stat", err.Error(), err)
	}
	if !info.ModTime().Equal(expected.MTime) || info.Size() != expected.Size {
		return app.NewError(app.CategoryStorage, "task_lost_update", "current task changed during update", nil)
	}
	return nil
}
