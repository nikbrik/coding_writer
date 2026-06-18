package process

import (
	"fmt"
	"strings"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/tasks"
)

// TransitionGate owns chat-driven stage transitions.
// It wraps tasks.Manager.Move with policy-level preconditions.
type TransitionGate struct {
	Tasks *tasks.Manager
}

type TransitionOptions struct {
	AutoApprovePlanning bool
}

// TransitionResult records an attempted transition.
type TransitionResult struct {
	Moved  bool
	From   app.TaskStage
	To     app.TaskStage
	Reason string
	State  app.TaskState
}

func (g *TransitionGate) Check(state app.TaskState, parsed ParsedResponse, opts TransitionOptions) (TransitionResult, error) {
	result := TransitionResult{From: state.Stage, To: state.Stage, State: state}
	if g == nil || g.Tasks == nil || state.Stage == app.StageDone {
		return result, nil
	}
	if parsed.Stage != "" && parsed.Stage != state.Stage {
		return result, app.NewError(app.CategoryValidation, "stage_mismatch", "transition candidate stage does not match task stage", nil)
	}
	next, reason, shouldMove, err := g.nextStage(state, parsed, opts)
	if err != nil || !shouldMove {
		return result, err
	}
	result.To = next
	result.Reason = reason
	return result, nil
}

// Apply evaluates a validated parsed response and moves stage only when all
// deterministic preconditions pass.
func (g *TransitionGate) Apply(state app.TaskState, parsed ParsedResponse, opts TransitionOptions) (TransitionResult, error) {
	result, err := g.Check(state, parsed, opts)
	if err != nil || result.To == state.Stage {
		return result, err
	}
	next := result.To
	reason := result.Reason
	current, err := g.Tasks.Current()
	if err != nil {
		return result, err
	}
	if current.ID != state.ID || current.Stage != state.Stage || current.Status != state.Status {
		return result, app.NewError(app.CategoryStorage, "task_changed_before_transition", "task changed before transition could be applied", nil)
	}
	if state.Stage == app.StagePlanning && next == app.StageExecution && parsed.Planning != nil {
		if _, err := g.Tasks.SetPlanningOutput(parsed.Planning.Summary, parsed.Planning.AcceptanceCriteria, parsed.Planning.Plan, parsed.Planning.OpenQuestions); err != nil {
			return result, err
		}
	}
	moved, err := g.Tasks.Move(next)
	if err != nil {
		return result, err
	}
	result.Moved = true
	result.To = moved.Stage
	result.Reason = reason
	result.State = moved
	return result, nil
}

func (g *TransitionGate) nextStage(state app.TaskState, parsed ParsedResponse, opts TransitionOptions) (app.TaskStage, string, bool, error) {
	switch state.Stage {
	case app.StagePlanning:
		if parsed.Planning == nil || parsed.Planning.Readiness != "ready_for_execution_proposal" {
			return state.Stage, "", false, nil
		}
		if !hasNonEmpty(parsed.Planning.Plan) || !hasNonEmpty(parsed.Planning.AcceptanceCriteria) || hasNonEmpty(parsed.Planning.OpenQuestions) {
			return state.Stage, "", false, app.NewError(app.CategoryValidation, "transition_precondition_failed", "planning output is not ready for execution", nil)
		}
		if !opts.AutoApprovePlanning {
			return state.Stage, "", false, nil
		}
		return app.StageExecution, "planning readiness approved", true, nil
	case app.StageExecution:
		if parsed.Execution == nil {
			return state.Stage, "", false, nil
		}
		switch parsed.Execution.NextSignal {
		case "ready_for_validation":
			if hasNonEmpty(parsed.Execution.Blockers) {
				return state.Stage, "", false, app.NewError(app.CategoryValidation, "transition_precondition_failed", "execution has blockers", nil)
			}
			if !hasNonEmpty(parsed.Execution.ChangedArtifacts) || !hasNonEmpty(parsed.Execution.Verification) {
				return state.Stage, "", false, app.NewError(app.CategoryValidation, "transition_precondition_failed", "execution evidence is missing", nil)
			}
			return app.StageValidation, "execution ready for validation", true, nil
		case "planning_required":
			return app.StagePlanning, "execution requires replanning", true, nil
		default:
			return state.Stage, "", false, nil
		}
	case app.StageValidation:
		if parsed.Validation == nil {
			return state.Stage, "", false, nil
		}
		switch parsed.Validation.Verdict {
		case "needs_execution_fixes":
			if !hasActionableFinding(parsed.Validation.Findings) {
				return state.Stage, "", false, app.NewError(app.CategoryValidation, "transition_precondition_failed", "validation fixes require actionable findings", nil)
			}
			return app.StageExecution, "validation requested execution fixes", true, nil
		case "ready_for_done":
			if len(parsed.Validation.MissingEvidence) > 0 || hasBlockerOrHigh(parsed.Validation.Findings) || !hasNonEmpty(parsed.Validation.PassedChecks) || !hasTrustedEvidence(parsed.TrustedEvidence) {
				return state.Stage, "", false, app.NewError(app.CategoryValidation, "transition_precondition_failed", "validation is not ready for done", nil)
			}
			return app.StageDone, "validation ready for done", true, nil
		default:
			return state.Stage, "", false, nil
		}
	case app.StageDone:
		return state.Stage, "", false, nil
	default:
		return state.Stage, "", false, app.NewError(app.CategoryValidation, "unknown_stage", fmt.Sprintf("no transition policy for stage %s", state.Stage), nil)
	}
}

func hasActionableFinding(findings []ValidationFinding) bool {
	for _, f := range findings {
		if strings.TrimSpace(f.Problem) != "" && strings.TrimSpace(f.Fix) != "" {
			return true
		}
	}
	return false
}

func hasBlockerOrHigh(findings []ValidationFinding) bool {
	for _, f := range findings {
		if isBlockerOrHighSeverity(f.Severity) {
			return true
		}
	}
	return false
}
