package tasks

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	Hash  [sha256.Size]byte
}

type TaskSummary struct {
	State     app.TaskState
	IsCurrent bool
	Archived  bool
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
		Microtasks:         []app.MicrotaskState{},
		CompletedSteps:     []string{},
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

func (m *Manager) ListTasks() ([]TaskSummary, error) {
	tasksDir, err := storage.SafeJoin(m.StorageDir, "tasks")
	if err != nil {
		return nil, app.NewError(app.CategoryValidation, "unsafe_task_path", "unsafe task path", err)
	}
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, app.NewError(app.CategoryStorage, "tasks_list", err.Error(), err)
	}
	currentID := ""
	if current, err := m.Current(); err == nil {
		currentID = current.ID
	}
	var out []TaskSummary
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || entry.Name() == "current.json" {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		state, err := m.Get(id)
		if err != nil {
			return nil, err
		}
		out = append(out, TaskSummary{State: state, IsCurrent: state.ID == currentID, Archived: state.ArchivedAt != nil})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Archived != out[j].Archived {
			return !out[i].Archived
		}
		return out[i].State.UpdatedAt.After(out[j].State.UpdatedAt)
	})
	return out, nil
}

func (m *Manager) Get(id string) (app.TaskState, error) {
	path, err := m.taskPath(id)
	if err != nil {
		return app.TaskState{}, err
	}
	var state app.TaskState
	if err := storage.ReadJSON(path, &state); err != nil {
		if errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "open") {
			return state, app.NewError(app.CategoryValidation, "unknown_task", "unknown task", err)
		}
		return state, app.NewError(app.CategoryStorage, "task_read", err.Error(), err)
	}
	if err := ValidateState(state); err != nil {
		return state, err
	}
	return state, nil
}

func (m *Manager) SelectTask(id, sessionID string) (app.TaskState, error) {
	state, err := m.Get(id)
	if err != nil {
		return state, err
	}
	if state.ArchivedAt != nil {
		return state, app.ErrorWithHint(app.CategoryValidation, "task_archived", "task is archived", "restore task before selecting it", nil)
	}
	if state.Stage != app.StageDone && strings.TrimSpace(sessionID) != "" {
		state.LastSessionID = strings.TrimSpace(sessionID)
	}
	return state, m.saveSelectedCurrent(state)
}

func (m *Manager) ClearCurrentFocus() error {
	currentPath, err := m.currentPath()
	if err != nil {
		return err
	}
	if err := storage.EnsureNoSymlinkParents(currentPath); err != nil {
		return app.NewError(app.CategoryStorage, "task_clear_focus", err.Error(), err)
	}
	if err := storage.RejectSymlinkTarget(currentPath); err != nil {
		return app.NewError(app.CategoryStorage, "task_clear_focus", err.Error(), err)
	}
	if err := os.Remove(currentPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return app.NewError(app.CategoryStorage, "task_clear_focus", err.Error(), err)
	}
	return nil
}

func (m *Manager) ArchiveTaskMetadata(id string) (app.TaskState, error) {
	state, err := m.Get(id)
	if err != nil {
		return state, err
	}
	now := time.Now().UTC()
	state.ArchivedAt = &now
	state.UpdatedAt = now
	if err := m.saveTaskSnapshot(state); err != nil {
		return state, err
	}
	if current, err := m.Current(); err == nil && current.ID == state.ID {
		if err := m.ClearCurrentFocus(); err != nil {
			return state, err
		}
	}
	return state, nil
}

func (m *Manager) RestoreArchivedTask(id, sessionID string) (app.TaskState, error) {
	state, err := m.Get(id)
	if err != nil {
		return state, err
	}
	if state.ArchivedAt == nil {
		return state, app.NewError(app.CategoryValidation, "task_not_archived", "task is not archived", nil)
	}
	state.ArchivedAt = nil
	if state.Stage != app.StageDone && strings.TrimSpace(sessionID) != "" {
		state.LastSessionID = strings.TrimSpace(sessionID)
	}
	state.UpdatedAt = time.Now().UTC()
	if err := m.saveTaskSnapshot(state); err != nil {
		return state, err
	}
	return state, m.saveSelectedCurrent(state)
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
	if err := ValidateState(state); err != nil {
		return currentSnapshot{}, err
	}
	stateHash, err := hashTaskState(state)
	if err != nil {
		return currentSnapshot{}, app.NewError(app.CategoryStorage, "task_hash", err.Error(), err)
	}
	return currentSnapshot{State: state, MTime: after.ModTime(), Size: after.Size(), Hash: stateHash}, nil
}

func hashTaskState(state app.TaskState) ([sha256.Size]byte, error) {
	data, err := json.Marshal(state)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(data), nil
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

func (m *Manager) MoveWithPlanningOutput(summary string, criteria, plan, openQuestions []string, next app.TaskStage) (app.TaskState, error) {
	if next != app.StageExecution {
		return app.TaskState{}, app.NewError(app.CategoryValidation, "invalid_stage", "planning output can only move to execution", nil)
	}
	if validation.HasSecret(summary) || hasSecretIn(criteria) || hasSecretIn(plan) || hasSecretIn(openQuestions) {
		return app.TaskState{}, app.NewError(app.CategoryValidation, "secret_blocked", "secret-like task content cannot be saved", nil)
	}
	snapshot, err := m.currentSnapshot()
	if err != nil {
		return app.TaskState{}, err
	}
	state := snapshot.State
	if state.Status == app.TaskStatusPaused {
		return state, app.NewError(app.CategoryValidation, "task_paused", "resume task before moving stage", nil)
	}
	if state.Stage != app.StagePlanning || !IsAllowed(state.Stage, next) {
		return state, app.ErrorWithHint(app.CategoryValidation, "forbidden_transition", "forbidden task stage transition", fmt.Sprintf("allowed next stages from %s: %v", state.Stage, AllowedNext(state.Stage)), nil)
	}
	now := time.Now().UTC()
	state.Objective = strings.TrimSpace(summary)
	state.AcceptanceCriteria = trimNonEmpty(criteria)
	state.Plan = trimNonEmpty(plan)
	state.Microtasks = deriveMicrotasks(state.Plan, now)
	state.OpenQuestions = trimNonEmpty(openQuestions)
	state.ApprovedPlanID = app.NewID("plan")
	state.PlanningApprovalID = app.NewID("approval")
	state.PlanningApprovalStatus = "approved"
	state.PlanningApprovalReason = "auto-approved planning output"
	state.PlanningApprovalConfidence = 1
	state.PlanningApprovalPlanID = state.ApprovedPlanID
	state.PlanningApprovalAllowedTransition = "planning->execution"
	state.Stage = next
	state.ExpectedAction = defaultExpectedAction(next)
	if len(state.Plan) > 0 {
		state.CurrentStep = state.Plan[0]
	}
	state.PendingPlanning = nil
	state.UpdatedAt = now
	state.HistoryLog = append(state.HistoryLog, fmt.Sprintf("%s: %s -> %s", now.Format(time.RFC3339), app.StagePlanning, next))
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
		state.Microtasks = deriveMicrotasks(state.Plan, time.Now().UTC())
		state.OpenQuestions = trimNonEmpty(openQuestions)
		return nil
	})
}

func (m *Manager) SavePendingPlanningProposal(summary string, criteria, plan, openQuestions []string) (app.TaskState, error) {
	if validation.HasSecret(summary) || hasSecretIn(criteria) || hasSecretIn(plan) || hasSecretIn(openQuestions) {
		return app.TaskState{}, app.NewError(app.CategoryValidation, "secret_blocked", "secret-like task content cannot be saved", nil)
	}
	return m.mutateActive(func(state *app.TaskState) error {
		if state.Stage != app.StagePlanning {
			return app.NewError(app.CategoryValidation, "invalid_stage", "planning proposal requires planning stage", nil)
		}
		now := time.Now().UTC()
		state.PendingPlanning = &app.PlanningProposalState{
			ID:                 app.NewID("plan"),
			Summary:            strings.TrimSpace(summary),
			AcceptanceCriteria: trimNonEmpty(criteria),
			Plan:               trimNonEmpty(plan),
			OpenQuestions:      trimNonEmpty(openQuestions),
			CreatedAt:          now,
		}
		state.ExpectedAction = app.ExpectedUserConfirmation
		state.CurrentStep = "awaiting planning confirmation"
		return nil
	})
}

func (m *Manager) ApprovePendingPlanningProposal() (app.TaskState, error) {
	snapshot, err := m.currentSnapshot()
	if err != nil {
		return app.TaskState{}, err
	}
	state := snapshot.State
	if state.PendingPlanning == nil {
		return state, app.NewError(app.CategoryValidation, "missing_pending_planning", "no pending planning proposal", nil)
	}
	if state.Status == app.TaskStatusPaused {
		return state, app.NewError(app.CategoryValidation, "task_paused", "resume task before approving planning", nil)
	}
	if state.Stage != app.StagePlanning || !IsAllowed(state.Stage, app.StageExecution) {
		return state, app.NewError(app.CategoryValidation, "forbidden_transition", "forbidden task stage transition", nil)
	}
	now := time.Now().UTC()
	pending := state.PendingPlanning
	state.Objective = pending.Summary
	state.AcceptanceCriteria = append([]string(nil), pending.AcceptanceCriteria...)
	state.Plan = append([]string(nil), pending.Plan...)
	state.Microtasks = deriveMicrotasks(state.Plan, now)
	state.OpenQuestions = append([]string(nil), pending.OpenQuestions...)
	state.ApprovedPlanID = pending.ID
	if strings.TrimSpace(state.PlanningApprovalID) == "" {
		state.PlanningApprovalID = app.NewID("approval")
		state.PlanningApprovalStatus = "approved"
		state.PlanningApprovalReason = "pending planning approved"
		state.PlanningApprovalConfidence = 1
		state.PlanningApprovalPlanID = pending.ID
		state.PlanningApprovalAllowedTransition = "planning->execution"
	}
	state.Stage = app.StageExecution
	state.ExpectedAction = defaultExpectedAction(app.StageExecution)
	if len(state.Plan) > 0 {
		state.CurrentStep = state.Plan[0]
	}
	state.PendingPlanning = nil
	state.UpdatedAt = now
	state.HistoryLog = append(state.HistoryLog, fmt.Sprintf("%s: %s -> %s", now.Format(time.RFC3339), app.StagePlanning, app.StageExecution))
	return state, m.saveBothIfUnchanged(state, &snapshot)
}

func (m *Manager) ApproveCurrentPlanning() (app.TaskState, error) {
	return m.mutateActive(func(state *app.TaskState) error {
		if state.Stage != app.StagePlanning {
			return app.NewError(app.CategoryValidation, "forbidden_transition", "current planning approval requires planning stage", nil)
		}
		if len(trimNonEmpty(state.Plan)) == 0 || len(trimNonEmpty(state.AcceptanceCriteria)) == 0 {
			return app.NewError(app.CategoryValidation, "transition_precondition_failed", "planning is not ready for execution", nil)
		}
		state.Stage = app.StageExecution
		state.ExpectedAction = defaultExpectedAction(app.StageExecution)
		state.PendingPlanning = nil
		state.ApprovedPlanID = app.NewID("plan")
		state.Microtasks = deriveMicrotasks(state.Plan, time.Now().UTC())
		if strings.TrimSpace(state.PlanningApprovalID) == "" {
			state.PlanningApprovalID = app.NewID("approval")
			state.PlanningApprovalStatus = "approved"
			state.PlanningApprovalReason = "current planning approved"
			state.PlanningApprovalConfidence = 1
			state.PlanningApprovalPlanID = state.ApprovedPlanID
			state.PlanningApprovalAllowedTransition = "planning->execution"
		}
		if len(state.Plan) > 0 {
			state.CurrentStep = state.Plan[0]
		}
		return nil
	})
}

func (m *Manager) RecordPlanningApproval(status, reason string, confidence float64, originalReply string) (app.TaskState, error) {
	return m.mutateActive(func(state *app.TaskState) error {
		if state.Stage != app.StagePlanning {
			return app.NewError(app.CategoryValidation, "invalid_stage", "planning approval requires planning stage", nil)
		}
		planID := state.ApprovedPlanID
		if state.PendingPlanning != nil {
			planID = state.PendingPlanning.ID
		}
		state.PlanningApprovalID = app.NewID("approval")
		state.PlanningApprovalStatus = strings.TrimSpace(status)
		state.PlanningApprovalReason = strings.TrimSpace(reason)
		state.PlanningApprovalConfidence = confidence
		state.PlanningApprovalOriginalReply = strings.TrimSpace(originalReply)
		state.PlanningApprovalPlanID = planID
		state.PlanningApprovalAllowedTransition = "planning->execution"
		return nil
	})
}

func (m *Manager) RecordAcceptedExecution(summary string, trustedEvidence []string) (app.TaskState, error) {
	if validation.HasSecret(summary) || hasSecretIn(trustedEvidence) {
		return app.TaskState{}, app.NewError(app.CategoryValidation, "secret_blocked", "secret-like task content cannot be saved", nil)
	}
	return m.mutateActive(func(state *app.TaskState) error {
		if state.Stage != app.StageExecution {
			return app.NewError(app.CategoryValidation, "invalid_stage", "accepted execution requires execution stage", nil)
		}
		state.LastAcceptedExecutionID = app.NewID("exec")
		state.ValidationStatus = ""
		state.ValidationEvidence = trimNonEmpty(trustedEvidence)
		markCurrentMicrotask(state, "accepted_execution", summary, trustedEvidence)
		if strings.TrimSpace(summary) != "" {
			state.Decisions = append(state.Decisions, "accepted_execution: "+strings.TrimSpace(summary))
		}
		return nil
	})
}

func (m *Manager) RecordAcceptedValidation(status string, trustedEvidence []string) (app.TaskState, error) {
	if hasSecretIn(trustedEvidence) {
		return app.TaskState{}, app.NewError(app.CategoryValidation, "secret_blocked", "secret-like task content cannot be saved", nil)
	}
	return m.mutateActive(func(state *app.TaskState) error {
		if state.Stage != app.StageValidation && state.Stage != app.StageExecution {
			return app.NewError(app.CategoryValidation, "invalid_stage", "accepted validation requires execution or validation stage", nil)
		}
		state.LastValidationID = app.NewID("validation")
		state.ValidationStatus = strings.TrimSpace(status)
		state.ValidationEvidence = trimNonEmpty(trustedEvidence)
		markCurrentMicrotask(state, "accepted_validation", status, trustedEvidence)
		if state.ValidationStatus == "ready_for_done" {
			markPendingMicrotasks(state, "accepted_validation", status, trustedEvidence)
		}
		return nil
	})
}

func (m *Manager) RejectPendingPlanningProposal() (app.TaskState, error) {
	return m.mutateActive(func(state *app.TaskState) error {
		if state.PendingPlanning == nil {
			return app.NewError(app.CategoryValidation, "missing_pending_planning", "no pending planning proposal", nil)
		}
		state.PendingPlanning = nil
		state.ExpectedAction = app.ExpectedUserInput
		state.CurrentStep = ""
		return nil
	})
}

func (m *Manager) SetExecutionProgress(currentStep, nextStep string, completed []string) (app.TaskState, error) {
	return m.mutateActive(func(state *app.TaskState) error {
		if state.Stage != app.StageExecution {
			return nil
		}
		for _, step := range trimNonEmpty(completed) {
			if !containsString(state.CompletedSteps, step) {
				state.CompletedSteps = append(state.CompletedSteps, step)
			}
		}
		if strings.TrimSpace(nextStep) != "" {
			state.CurrentStep = strings.TrimSpace(nextStep)
		} else if strings.TrimSpace(currentStep) != "" {
			state.CurrentStep = strings.TrimSpace(currentStep)
		}
		return nil
	})
}

func (m *Manager) SetLastSessionID(sessionID string) (app.TaskState, error) {
	if strings.TrimSpace(sessionID) == "" {
		return app.TaskState{}, app.NewError(app.CategoryValidation, "missing_session", "session id is required", nil)
	}
	snapshot, err := m.currentSnapshot()
	if err != nil {
		return app.TaskState{}, err
	}
	state := snapshot.State
	if state.Stage == app.StageDone {
		return state, app.NewError(app.CategoryValidation, "task_done", "done task is terminal", nil)
	}
	state.LastSessionID = strings.TrimSpace(sessionID)
	state.UpdatedAt = time.Now().UTC()
	return state, m.saveBothIfUnchanged(state, &snapshot)
}

func containsString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func deriveMicrotasks(plan []string, now time.Time) []app.MicrotaskState {
	items := trimNonEmpty(plan)
	out := make([]app.MicrotaskState, 0, len(items))
	for _, item := range items {
		out = append(out, app.MicrotaskState{
			ID:        app.NewID("microtask"),
			PlanItem:  item,
			Status:    "pending",
			CreatedAt: now,
			UpdatedAt: now,
		})
	}
	return out
}

func markCurrentMicrotask(state *app.TaskState, status, summary string, evidence []string) {
	if state == nil || len(state.Microtasks) == 0 {
		return
	}
	current := strings.TrimSpace(state.CurrentStep)
	idx := -1
	for i := range state.Microtasks {
		if strings.TrimSpace(state.Microtasks[i].PlanItem) == current {
			idx = i
			break
		}
	}
	if idx < 0 {
		idx = 0
	}
	state.Microtasks[idx].Status = status
	state.Microtasks[idx].ResultSummary = strings.TrimSpace(summary)
	state.Microtasks[idx].EvidenceRefs = trimNonEmpty(evidence)
	state.Microtasks[idx].UpdatedAt = time.Now().UTC()
}

func markPendingMicrotasks(state *app.TaskState, status, summary string, evidence []string) {
	if state == nil {
		return
	}
	now := time.Now().UTC()
	refs := trimNonEmpty(evidence)
	for i := range state.Microtasks {
		if strings.TrimSpace(state.Microtasks[i].Status) != "pending" {
			continue
		}
		state.Microtasks[i].Status = status
		state.Microtasks[i].ResultSummary = strings.TrimSpace(summary)
		state.Microtasks[i].EvidenceRefs = refs
		state.Microtasks[i].UpdatedAt = now
	}
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
		return state, app.NewError(app.CategoryValidation, "task_done", "done task is terminal and cannot be paused", nil)
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
		return state, app.NewError(app.CategoryValidation, "task_done", "done task is terminal and cannot be resumed", nil)
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
	if err := ValidateState(state); err != nil {
		return err
	}
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
		} else if current, err := m.currentSnapshot(); err == nil && current.State.Stage != app.StageDone && current.State.ID != state.ID {
			return app.NewError(app.CategoryValidation, "task_already_exists", "a current task already exists; finish or archive it before starting a new one", nil)
		} else if err != nil {
			appErr := app.AsError(err)
			if appErr.Category != app.CategoryValidation || appErr.Code != "missing_current_task" {
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

func (m *Manager) saveTaskSnapshot(state app.TaskState) error {
	if err := ValidateState(state); err != nil {
		return err
	}
	path, err := m.taskPath(state.ID)
	if err != nil {
		return err
	}
	if err := storage.AtomicWriteJSON(path, state); err != nil {
		return app.NewError(app.CategoryStorage, "task_write", err.Error(), err)
	}
	return nil
}

func (m *Manager) saveSelectedCurrent(state app.TaskState) error {
	if err := ValidateState(state); err != nil {
		return err
	}
	currentPath, err := m.currentPath()
	if err != nil {
		return err
	}
	if err := storage.EnsureDir(filepath.Dir(currentPath)); err != nil {
		return app.NewError(app.CategoryStorage, "task_dir", err.Error(), err)
	}
	if err := storage.AtomicWriteJSON(currentPath, state); err != nil {
		return app.NewError(app.CategoryStorage, "task_write", err.Error(), err)
	}
	return m.saveTaskSnapshot(state)
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
	var current app.TaskState
	if err := storage.ReadJSON(currentPath, &current); err != nil {
		return app.NewError(app.CategoryStorage, "task_read", err.Error(), err)
	}
	currentHash, err := hashTaskState(current)
	if err != nil {
		return app.NewError(app.CategoryStorage, "task_hash", err.Error(), err)
	}
	if currentHash != expected.Hash {
		return app.NewError(app.CategoryStorage, "task_lost_update", "current task changed during update", nil)
	}
	return nil
}
