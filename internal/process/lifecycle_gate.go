package process

import (
	"strings"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/tasks"
)

type TransitionSource string

const (
	TransitionSourceModelOutput         TransitionSource = "model_output"
	TransitionSourceUserApproval        TransitionSource = "user_approval"
	TransitionSourceTrustedVerification TransitionSource = "trusted_verification"
	TransitionSourceSystemReplan        TransitionSource = "system_replan"
	TransitionSourceRecoveryDebug       TransitionSource = "recovery_debug"
)

type TransitionSignal string

const (
	SignalApprovePlanning     TransitionSignal = "approve_planning"
	SignalRejectPlanning      TransitionSignal = "reject_planning"
	SignalReadyForValidation  TransitionSignal = "ready_for_validation"
	SignalPlanningRequired    TransitionSignal = "planning_required"
	SignalNeedsExecutionFixes TransitionSignal = "needs_execution_fixes"
	SignalReadyForDone        TransitionSignal = "ready_for_done"
)

type LifecycleTransitionRequest struct {
	State           app.TaskState
	Source          TransitionSource
	Signal          TransitionSignal
	Parsed          *ParsedResponse
	Target          app.TaskStage
	TrustedEvidence []string
	Reason          string
	RecoveryDebug   bool
}

type LifecycleGate struct {
	Tasks *tasks.Manager
}

func (g *LifecycleGate) Check(req LifecycleTransitionRequest) (TransitionResult, error) {
	state := req.State
	result := TransitionResult{From: state.Stage, To: state.Stage, State: state}
	if g == nil || g.Tasks == nil || state.Stage == app.StageDone {
		return result, nil
	}
	if req.Parsed != nil && req.Parsed.Stage != "" && req.Parsed.Stage != state.Stage {
		return result, app.NewError(app.CategoryValidation, "stage_mismatch", "transition candidate stage does not match task stage", nil)
	}
	next, reason, shouldMove, err := g.next(req)
	if err != nil || !shouldMove {
		return result, err
	}
	result.To = next
	result.Reason = reason
	return result, nil
}

func (g *LifecycleGate) Apply(req LifecycleTransitionRequest) (TransitionResult, error) {
	result, err := g.Check(req)
	if err != nil || result.To == req.State.Stage {
		return result, err
	}
	current, err := g.Tasks.Current()
	if err != nil {
		return result, err
	}
	if !sameTaskForTransition(current, req.State) {
		return result, app.NewError(app.CategoryStorage, "task_changed_before_transition", "task changed before transition could be applied", nil)
	}
	state := req.State
	switch {
	case state.Stage == app.StagePlanning && result.To == app.StageExecution:
		moved, err := g.applyPlanning(req)
		if err != nil {
			return result, err
		}
		result.Moved = true
		result.To = moved.Stage
		result.State = moved
		return result, nil
	case state.Stage == app.StageExecution && result.To == app.StageValidation:
		if req.Parsed != nil && req.Parsed.Execution != nil {
			if _, err := g.Tasks.RecordAcceptedExecution(req.Parsed.Execution.Summary, req.TrustedEvidence); err != nil {
				return result, err
			}
		} else if len(req.TrustedEvidence) > 0 {
			if _, err := g.Tasks.RecordAcceptedExecution("trusted verification evidence accepted", req.TrustedEvidence); err != nil {
				return result, err
			}
		}
		moved, err := g.Tasks.Move(app.StageValidation)
		if err != nil {
			return result, err
		}
		result.Moved = true
		result.State = moved
		return result, nil
	case state.Stage == app.StageExecution && result.To == app.StagePlanning:
		moved, err := g.Tasks.Move(app.StagePlanning)
		if err != nil {
			return result, err
		}
		result.Moved = true
		result.State = moved
		return result, nil
	case state.Stage == app.StageValidation && result.To == app.StageExecution:
		if req.Parsed != nil && req.Parsed.Validation != nil {
			if _, err := g.Tasks.RecordAcceptedValidation(req.Parsed.Validation.Verdict, req.TrustedEvidence); err != nil {
				return result, err
			}
		}
		moved, err := g.Tasks.Move(app.StageExecution)
		if err != nil {
			return result, err
		}
		result.Moved = true
		result.State = moved
		return result, nil
	case state.Stage == app.StageValidation && result.To == app.StageDone:
		if req.Parsed != nil && req.Parsed.Validation != nil {
			if _, err := g.Tasks.RecordAcceptedValidation(req.Parsed.Validation.Verdict, req.TrustedEvidence); err != nil {
				return result, err
			}
		}
		moved, err := g.Tasks.Move(app.StageDone)
		if err != nil {
			return result, err
		}
		result.Moved = true
		result.State = moved
		return result, nil
	default:
		return result, app.NewError(app.CategoryValidation, "forbidden_transition", "unsupported lifecycle transition", nil)
	}
}

func (g *LifecycleGate) applyPlanning(req LifecycleTransitionRequest) (app.TaskState, error) {
	if req.Parsed != nil && req.Parsed.Planning != nil {
		return g.Tasks.MoveWithPlanningOutput(
			req.Parsed.Planning.Summary,
			req.Parsed.Planning.AcceptanceCriteria,
			req.Parsed.Planning.Plan,
			req.Parsed.Planning.OpenQuestions,
			app.StageExecution,
		)
	}
	if req.State.PendingPlanning != nil {
		return g.Tasks.ApprovePendingPlanningProposal()
	}
	return g.Tasks.ApproveCurrentPlanning()
}

func (g *LifecycleGate) next(req LifecycleTransitionRequest) (app.TaskStage, string, bool, error) {
	state := req.State
	signal := req.Signal
	if signal == "" && req.Parsed != nil {
		signal = inferSignalFromParsed(state.Stage, *req.Parsed)
	}
	switch state.Stage {
	case app.StagePlanning:
		if signal != SignalApprovePlanning {
			return state.Stage, "", false, nil
		}
		if state.Status != app.TaskStatusActive {
			return state.Stage, "", false, app.NewError(app.CategoryValidation, "task_paused", "resume task before approving planning", nil)
		}
		if req.Parsed != nil && req.Parsed.Planning != nil {
			if req.Source == TransitionSourceModelOutput {
				return state.Stage, "", false, nil
			}
			if !hasNonEmpty(req.Parsed.Planning.Plan) || !hasNonEmpty(req.Parsed.Planning.AcceptanceCriteria) || hasNonEmpty(req.Parsed.Planning.OpenQuestions) {
				return state.Stage, "", false, app.NewError(app.CategoryValidation, "transition_precondition_failed", "planning output is not ready for execution", nil)
			}
			return app.StageExecution, "planning approved", true, nil
		}
		if state.PendingPlanning != nil {
			if !hasNonEmpty(state.PendingPlanning.Plan) || !hasNonEmpty(state.PendingPlanning.AcceptanceCriteria) || hasNonEmpty(state.PendingPlanning.OpenQuestions) {
				return state.Stage, "", false, app.NewError(app.CategoryValidation, "transition_precondition_failed", "pending planning proposal is not ready for execution", nil)
			}
			if req.Source == TransitionSourceUserApproval && state.PlanningApprovalStatus != "approved" {
				return state.Stage, "", false, app.NewError(app.CategoryValidation, "transition_precondition_failed", "planning approval requires accepted LLM approval record", nil)
			}
			return app.StageExecution, "pending planning approved", true, nil
		}
		if !hasRunnablePlanningState(state) {
			return state.Stage, "", false, app.NewError(app.CategoryValidation, "transition_precondition_failed", "current planning is not ready for execution", nil)
		}
		if req.Source == TransitionSourceUserApproval && state.PlanningApprovalStatus != "approved" {
			return state.Stage, "", false, app.NewError(app.CategoryValidation, "transition_precondition_failed", "planning approval requires accepted LLM approval record", nil)
		}
		return app.StageExecution, "current planning approved", true, nil
	case app.StageExecution:
		switch signal {
		case SignalReadyForValidation:
			if req.Parsed != nil && req.Parsed.Execution != nil {
				if hasNonEmpty(req.Parsed.Execution.Blockers) || !g.hasBoundTrustedEvidence(state, req) || !hasNonEmpty(req.Parsed.Execution.ChangedArtifacts) || !hasNonEmpty(req.Parsed.Execution.Verification) {
					return state.Stage, "", false, app.NewError(app.CategoryValidation, "transition_precondition_failed", "execution is not ready for validation", nil)
				}
			} else {
				if strings.TrimSpace(state.LastAcceptedExecutionID) == "" && !g.hasBoundTrustedEvidence(state, req) {
					return state.Stage, "", false, app.NewError(app.CategoryValidation, "transition_precondition_failed", "ready_for_validation requires accepted execution or trusted evidence", nil)
				}
			}
			return app.StageValidation, "execution ready for validation", true, nil
		case SignalPlanningRequired:
			if req.Source != TransitionSourceSystemReplan && req.Parsed != nil && req.Parsed.Execution != nil && strings.TrimSpace(req.Parsed.Execution.Summary) == "" {
				return state.Stage, "", false, app.NewError(app.CategoryValidation, "transition_precondition_failed", "replanning requires recorded reason", nil)
			}
			return app.StagePlanning, "execution requires replanning", true, nil
		default:
			return state.Stage, "", false, nil
		}
	case app.StageValidation:
		switch signal {
		case SignalNeedsExecutionFixes:
			if req.Parsed == nil || req.Parsed.Validation == nil || !hasActionableFinding(req.Parsed.Validation.Findings) {
				return state.Stage, "", false, app.NewError(app.CategoryValidation, "transition_precondition_failed", "validation fixes require actionable findings", nil)
			}
			return app.StageExecution, "validation requested execution fixes", true, nil
		case SignalReadyForDone:
			if req.Parsed != nil && req.Parsed.Validation != nil {
				if len(req.Parsed.Validation.MissingEvidence) > 0 || hasBlockerOrHigh(req.Parsed.Validation.Findings) || !hasNonEmpty(req.Parsed.Validation.PassedChecks) || !g.trustedEvidenceSatisfiesState(state, req) {
					return state.Stage, "", false, app.NewError(app.CategoryValidation, "transition_precondition_failed", "validation is not ready for done", nil)
				}
				return app.StageDone, "validation ready for done", true, nil
			}
			if strings.TrimSpace(state.LastValidationID) == "" {
				return state.Stage, "", false, app.NewError(app.CategoryValidation, "transition_precondition_failed", "done requires accepted validation record", nil)
			}
			if strings.TrimSpace(state.ValidationStatus) != "ready_for_done" {
				return state.Stage, "", false, app.NewError(app.CategoryValidation, "transition_precondition_failed", "done requires ready_for_done validation status", nil)
			}
			if !g.trustedEvidenceSatisfiesState(state, req) {
				return state.Stage, "", false, app.NewError(app.CategoryValidation, "transition_precondition_failed", "trusted verification does not satisfy acceptance criteria", nil)
			}
			return app.StageDone, "trusted verification completed", true, nil
		default:
			return state.Stage, "", false, nil
		}
	default:
		return state.Stage, "", false, nil
	}
}

func (g *LifecycleGate) hasBoundTrustedEvidence(state app.TaskState, req LifecycleTransitionRequest) bool {
	return len(g.boundEvidenceRecords(state, req)) > 0
}

func (g *LifecycleGate) trustedEvidenceSatisfiesState(state app.TaskState, req LifecycleTransitionRequest) bool {
	records := g.boundEvidenceRecords(state, req)
	if len(records) == 0 {
		return false
	}
	required := requiredTrustedEvidenceSources(state.AcceptanceCriteria)
	if len(required) == 0 {
		return true
	}
	sources := make([]string, 0, len(records))
	for _, rec := range records {
		sources = append(sources, strings.ToLower(strings.TrimSpace(rec.Source)))
	}
	for _, source := range required {
		if !hasEvidenceSourcePrefix(sources, source) {
			return false
		}
	}
	return true
}

func (g *LifecycleGate) boundEvidenceRecords(state app.TaskState, req LifecycleTransitionRequest) []TrustedEvidenceRecord {
	if g == nil || g.Tasks == nil || strings.TrimSpace(g.Tasks.StorageDir) == "" || strings.TrimSpace(state.ID) == "" || strings.TrimSpace(req.Reason) == "" {
		return nil
	}
	records, err := NewTrustedEvidenceStore(g.Tasks.StorageDir).Validate(state.ID, req.Reason, req.TrustedEvidence)
	if err != nil {
		return nil
	}
	return records
}

func inferSignalFromParsed(stage app.TaskStage, parsed ParsedResponse) TransitionSignal {
	switch stage {
	case app.StagePlanning:
		if parsed.Planning != nil && parsed.Planning.Readiness == "ready_for_execution_proposal" {
			return SignalApprovePlanning
		}
	case app.StageExecution:
		if parsed.Execution != nil {
			switch parsed.Execution.NextSignal {
			case "ready_for_validation":
				return SignalReadyForValidation
			case "planning_required":
				return SignalPlanningRequired
			}
		}
	case app.StageValidation:
		if parsed.Validation != nil {
			switch parsed.Validation.Verdict {
			case "needs_execution_fixes":
				return SignalNeedsExecutionFixes
			case "ready_for_done":
				return SignalReadyForDone
			}
		}
	}
	return ""
}
