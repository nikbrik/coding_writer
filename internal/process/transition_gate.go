package process

import (
	"strings"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/tasks"
)

// TransitionGate owns chat-driven stage transitions.
// It wraps tasks.Manager.Move with policy-level preconditions.
type TransitionGate struct {
	Tasks         *tasks.Manager
	LifecycleGate *LifecycleGate
}

type TransitionOptions struct {
	SessionID string
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
	return g.lifecycle().Check(LifecycleTransitionRequest{
		State:           state,
		Source:          TransitionSourceModelOutput,
		Parsed:          &parsed,
		TrustedEvidence: parsed.TrustedEvidence,
		Reason:          opts.SessionID,
	})
}

// Apply evaluates a validated parsed response and moves stage only when all
// deterministic preconditions pass.
func (g *TransitionGate) Apply(state app.TaskState, parsed ParsedResponse, opts TransitionOptions) (TransitionResult, error) {
	return g.lifecycle().Apply(LifecycleTransitionRequest{
		State:           state,
		Source:          TransitionSourceModelOutput,
		Parsed:          &parsed,
		TrustedEvidence: parsed.TrustedEvidence,
		Reason:          opts.SessionID,
	})
}

func (g *TransitionGate) lifecycle() *LifecycleGate {
	if g == nil {
		return &LifecycleGate{}
	}
	if g.LifecycleGate == nil {
		g.LifecycleGate = &LifecycleGate{Tasks: g.Tasks}
	}
	if g.LifecycleGate.Tasks == nil {
		g.LifecycleGate.Tasks = g.Tasks
	}
	return g.LifecycleGate
}

func sameTaskForTransition(current, expected app.TaskState) bool {
	return current.ID == expected.ID &&
		current.Stage == expected.Stage &&
		current.Status == expected.Status &&
		current.CurrentStep == expected.CurrentStep &&
		current.ExpectedAction == expected.ExpectedAction &&
		current.Objective == expected.Objective &&
		current.ApprovedPlanID == expected.ApprovedPlanID &&
		current.PlanningApprovalID == expected.PlanningApprovalID &&
		current.PlanningApprovalStatus == expected.PlanningApprovalStatus &&
		current.PlanningApprovalReason == expected.PlanningApprovalReason &&
		current.PlanningApprovalConfidence == expected.PlanningApprovalConfidence &&
		current.PlanningApprovalOriginalReply == expected.PlanningApprovalOriginalReply &&
		current.PlanningApprovalPlanID == expected.PlanningApprovalPlanID &&
		current.PlanningApprovalAllowedTransition == expected.PlanningApprovalAllowedTransition &&
		current.LastAcceptedExecutionID == expected.LastAcceptedExecutionID &&
		current.LastValidationID == expected.LastValidationID &&
		current.ValidationStatus == expected.ValidationStatus &&
		current.LastSessionID == expected.LastSessionID &&
		current.UpdatedAt.Equal(expected.UpdatedAt) &&
		sameStrings(current.AcceptanceCriteria, expected.AcceptanceCriteria) &&
		sameStrings(current.Plan, expected.Plan) &&
		sameMicrotasks(current.Microtasks, expected.Microtasks) &&
		sameStrings(current.Decisions, expected.Decisions) &&
		sameStrings(current.OpenQuestions, expected.OpenQuestions) &&
		sameStrings(current.ValidationEvidence, expected.ValidationEvidence) &&
		sameStrings(current.HistoryLog, expected.HistoryLog) &&
		sameStrings(current.CompletedSteps, expected.CompletedSteps) &&
		samePendingPlanning(current.PendingPlanning, expected.PendingPlanning)
}

func sameMicrotasks(a, b []app.MicrotaskState) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID ||
			a[i].PlanItem != b[i].PlanItem ||
			a[i].Role != b[i].Role ||
			a[i].Status != b[i].Status ||
			a[i].ResultSummary != b[i].ResultSummary ||
			a[i].LastAuditEventID != b[i].LastAuditEventID ||
			!a[i].CreatedAt.Equal(b[i].CreatedAt) ||
			!a[i].UpdatedAt.Equal(b[i].UpdatedAt) ||
			!sameStrings(a[i].EvidenceRefs, b[i].EvidenceRefs) {
			return false
		}
	}
	return true
}

func samePendingPlanning(a, b *app.PlanningProposalState) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.ID == b.ID && a.Summary == b.Summary && a.CreatedAt.Equal(b.CreatedAt) && sameStrings(a.AcceptanceCriteria, b.AcceptanceCriteria) && sameStrings(a.Plan, b.Plan) && sameStrings(a.OpenQuestions, b.OpenQuestions)
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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
