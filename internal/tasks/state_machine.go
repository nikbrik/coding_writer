package tasks

import (
	"strings"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/storage"
)

var AllowedTransitions = map[app.TaskStage][]app.TaskStage{
	app.StagePlanning:   {app.StageExecution},
	app.StageExecution:  {app.StageValidation, app.StagePlanning},
	app.StageValidation: {app.StageExecution, app.StageDone},
	app.StageDone:       {},
}

func IsAllowed(from, to app.TaskStage) bool {
	for _, allowed := range AllowedTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

func ValidStage(stage app.TaskStage) bool {
	_, ok := AllowedTransitions[stage]
	return ok
}

func ValidExpectedAction(action app.ExpectedAction) bool {
	switch action {
	case app.ExpectedUserInput, app.ExpectedLLMResponse, app.ExpectedUserConfirmation, app.ExpectedNone:
		return true
	default:
		return false
	}
}

func AllowedNext(stage app.TaskStage) []app.TaskStage {
	out := append([]app.TaskStage(nil), AllowedTransitions[stage]...)
	return out
}

func ValidateState(state app.TaskState) error {
	if err := storage.ValidateID(state.ID); err != nil {
		return app.NewError(app.CategoryValidation, "invalid_task_state", "task id is invalid", err)
	}
	if strings.TrimSpace(state.Title) == "" {
		return app.NewError(app.CategoryValidation, "invalid_task_state", "task title is required", nil)
	}
	if !ValidStage(state.Stage) {
		return app.NewError(app.CategoryValidation, "invalid_task_state", "task stage is invalid", nil)
	}
	if state.Status != app.TaskStatusActive && state.Status != app.TaskStatusPaused {
		return app.NewError(app.CategoryValidation, "invalid_task_state", "task status is invalid", nil)
	}
	if !ValidExpectedAction(state.ExpectedAction) {
		return app.NewError(app.CategoryValidation, "invalid_task_state", "task expected_action is invalid", nil)
	}
	if state.Stage == app.StageDone && state.ExpectedAction != app.ExpectedNone {
		return app.NewError(app.CategoryValidation, "invalid_task_state", "done task must use expected_action none", nil)
	}
	if state.Stage == app.StageDone && state.Status != app.TaskStatusActive {
		return app.NewError(app.CategoryValidation, "invalid_task_state", "done task must remain active terminal state", nil)
	}
	if state.Stage == app.StageDone && strings.TrimSpace(state.LastValidationID) == "" {
		return app.NewError(app.CategoryValidation, "invalid_task_state", "done task requires accepted validation record", nil)
	}
	if state.Stage == app.StageDone && strings.TrimSpace(state.ValidationStatus) != "ready_for_done" {
		return app.NewError(app.CategoryValidation, "invalid_task_state", "done task requires ready_for_done validation status", nil)
	}
	if state.Stage != app.StageDone && state.ExpectedAction == app.ExpectedNone {
		return app.NewError(app.CategoryValidation, "invalid_task_state", "expected_action none is only valid for done task", nil)
	}
	if state.Stage == app.StageExecution && strings.TrimSpace(state.ValidationStatus) != "" && strings.TrimSpace(state.ValidationStatus) != "needs_execution_fixes" {
		return app.NewError(app.CategoryValidation, "invalid_task_state", "execution task has invalid validation status", nil)
	}
	return nil
}
