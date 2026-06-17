package process

import (
	"testing"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/tasks"
)

func TestTransitionGatePlanningRequiresAutoApprove(t *testing.T) {
	dir := t.TempDir()
	mgr := tasks.NewManager(dir)
	state, err := mgr.Start("task")
	if err != nil {
		t.Fatal(err)
	}
	gate := &TransitionGate{Tasks: mgr}
	parsed := ParsedResponse{Stage: app.StagePlanning, Planning: &PlanningOutput{
		AcceptanceCriteria: []string{"c"},
		Plan:               []string{"p"},
		Readiness:          "ready_for_execution_proposal",
	}}
	res, err := gate.Apply(state, parsed, TransitionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Moved {
		t.Fatal("planning should require auto approval")
	}
	current, _ := mgr.Current()
	if current.Stage != app.StagePlanning {
		t.Fatalf("state moved unexpectedly: %s", current.Stage)
	}
}

func TestTransitionGateValidPlanningMoveUsesTaskManager(t *testing.T) {
	dir := t.TempDir()
	mgr := tasks.NewManager(dir)
	state, err := mgr.Start("task")
	if err != nil {
		t.Fatal(err)
	}
	gate := &TransitionGate{Tasks: mgr}
	parsed := ParsedResponse{Stage: app.StagePlanning, Planning: &PlanningOutput{
		AcceptanceCriteria: []string{"c"},
		Plan:               []string{"p"},
		Readiness:          "ready_for_execution_proposal",
	}}
	res, err := gate.Apply(state, parsed, TransitionOptions{AutoApprovePlanning: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Moved || res.To != app.StageExecution {
		t.Fatalf("expected move to execution: %+v", res)
	}
	current, _ := mgr.Current()
	if current.Stage != app.StageExecution || current.ExpectedAction != app.ExpectedLLMResponse {
		t.Fatalf("manager did not persist transition: %+v", current)
	}
}

func TestTransitionGateForbiddenProposalPreservesState(t *testing.T) {
	dir := t.TempDir()
	mgr := tasks.NewManager(dir)
	state, err := mgr.Start("task")
	if err != nil {
		t.Fatal(err)
	}
	gate := &TransitionGate{Tasks: mgr}
	parsed := ParsedResponse{Stage: app.StagePlanning, Planning: &PlanningOutput{Readiness: "ready_for_execution_proposal"}}
	_, err = gate.Apply(state, parsed, TransitionOptions{AutoApprovePlanning: true})
	if err == nil {
		t.Fatal("expected precondition error")
	}
	current, _ := mgr.Current()
	if current.Stage != app.StagePlanning {
		t.Fatalf("state moved unexpectedly: %s", current.Stage)
	}
}

func TestTransitionGateExecutionToValidation(t *testing.T) {
	dir := t.TempDir()
	mgr := tasks.NewManager(dir)
	state, _ := mgr.Start("task")
	state, _ = mgr.Move(app.StageExecution)
	gate := &TransitionGate{Tasks: mgr}
	parsed := ParsedResponse{Stage: app.StageExecution, Execution: &ExecutionOutput{NextSignal: "ready_for_validation"}}
	res, err := gate.Apply(state, parsed, TransitionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Moved || res.To != app.StageValidation {
		t.Fatalf("expected validation move: %+v", res)
	}
}

func TestTransitionGateValidationToDone(t *testing.T) {
	dir := t.TempDir()
	mgr := tasks.NewManager(dir)
	state, _ := mgr.Start("task")
	state, _ = mgr.Move(app.StageExecution)
	state, _ = mgr.Move(app.StageValidation)
	gate := &TransitionGate{Tasks: mgr}
	parsed := ParsedResponse{Stage: app.StageValidation, Validation: &ValidationOutput{
		Findings:     []ValidationFinding{},
		PassedChecks: []string{"tool evidence available"},
		Verdict:      "ready_for_done",
	}}
	res, err := gate.Apply(state, parsed, TransitionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Moved || res.To != app.StageDone {
		t.Fatalf("expected done move: %+v", res)
	}
}

func TestTransitionGateValidationToDoneBlockedByMixedCaseHighFinding(t *testing.T) {
	dir := t.TempDir()
	mgr := tasks.NewManager(dir)
	state, _ := mgr.Start("task")
	state, _ = mgr.Move(app.StageExecution)
	state, _ = mgr.Move(app.StageValidation)
	gate := &TransitionGate{Tasks: mgr}
	parsed := ParsedResponse{Stage: app.StageValidation, Validation: &ValidationOutput{
		Findings: []ValidationFinding{{Severity: " HIGH ", Problem: "bug", Fix: "fix it"}},
		Verdict:  "ready_for_done",
	}}
	_, err := gate.Apply(state, parsed, TransitionOptions{})
	if err == nil || app.AsError(err).Code != "transition_precondition_failed" {
		t.Fatalf("want transition_precondition_failed, got %v", err)
	}
	current, _ := mgr.Current()
	if current.Stage != app.StageValidation {
		t.Fatalf("state moved unexpectedly: %+v", current)
	}
}
