package process

import (
	"testing"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/tasks"
)

func issueGateEvidence(t *testing.T, dir string, state app.TaskState, sessionID, source string) []string {
	t.Helper()
	token, _, err := NewTrustedEvidenceStore(dir).Issue(state.ID, sessionID, source, 0, "ok")
	if err != nil {
		t.Fatal(err)
	}
	return []string{token}
}

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

func TestTransitionGateModelPlanningNeverAutoApproves(t *testing.T) {
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
	if res.Moved || res.To != app.StagePlanning {
		t.Fatalf("model planning output must stay pending user approval: %+v", res)
	}
	current, _ := mgr.Current()
	if current.Stage != app.StagePlanning {
		t.Fatalf("state moved unexpectedly: %+v", current)
	}
}

func TestTransitionGateRejectsStageMismatch(t *testing.T) {
	dir := t.TempDir()
	mgr := tasks.NewManager(dir)
	state, err := mgr.Start("task")
	if err != nil {
		t.Fatal(err)
	}
	gate := &TransitionGate{Tasks: mgr}
	parsed := ParsedResponse{Stage: app.StageExecution, Planning: &PlanningOutput{AcceptanceCriteria: []string{"c"}, Plan: []string{"p"}, Readiness: "ready_for_execution_proposal"}}
	_, err = gate.Apply(state, parsed, TransitionOptions{})
	if err == nil || app.AsError(err).Code != "stage_mismatch" {
		t.Fatalf("want stage_mismatch, got %v", err)
	}
}

func TestTransitionGateRejectsStaleTaskStateBeforeApply(t *testing.T) {
	dir := t.TempDir()
	mgr := tasks.NewManager(dir)
	_, err := mgr.Start("task")
	if err != nil {
		t.Fatal(err)
	}
	stale, err := mgr.Move(app.StageExecution)
	if err != nil {
		t.Fatal(err)
	}
	evidence := issueGateEvidence(t, dir, stale, "session_test", "go test ./...")
	if _, err := mgr.SetStep("changed concurrently"); err != nil {
		t.Fatal(err)
	}
	gate := &TransitionGate{Tasks: mgr}
	parsed := ParsedResponse{Stage: app.StageExecution, TrustedEvidence: evidence, Execution: &ExecutionOutput{ChangedArtifacts: []string{"file"}, Verification: []string{"go test ./..."}, NextSignal: "ready_for_validation"}}
	_, err = gate.Apply(stale, parsed, TransitionOptions{SessionID: "session_test"})
	if err == nil || app.AsError(err).Code != "task_changed_before_transition" {
		t.Fatalf("want task_changed_before_transition, got %v", err)
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
	res, err := gate.Apply(state, parsed, TransitionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Moved {
		t.Fatal("model planning output must not move without user approval")
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
	evidence := issueGateEvidence(t, dir, state, "session_test", "go test ./...")
	parsed := ParsedResponse{Stage: app.StageExecution, TrustedEvidence: evidence, Execution: &ExecutionOutput{ChangedArtifacts: []string{"file"}, Verification: []string{"not run"}, NextSignal: "ready_for_validation"}}
	res, err := gate.Apply(state, parsed, TransitionOptions{SessionID: "session_test"})
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
	evidence := issueGateEvidence(t, dir, state, "session_test", "go test ./...")
	state, _ = mgr.RecordAcceptedExecution("execution accepted", evidence)
	state, _ = mgr.Move(app.StageValidation)
	gate := &TransitionGate{Tasks: mgr}
	parsed := ParsedResponse{Stage: app.StageValidation, TrustedEvidence: evidence, Validation: &ValidationOutput{
		Findings:     []ValidationFinding{},
		PassedChecks: []string{"tool evidence available"},
		Verdict:      "ready_for_done",
	}}
	res, err := gate.Apply(state, parsed, TransitionOptions{SessionID: "session_test"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Moved || res.To != app.StageDone {
		t.Fatalf("expected done move: %+v", res)
	}
}

func TestTransitionGateValidationToDoneRequiresCriteriaMatchedEvidence(t *testing.T) {
	dir := t.TempDir()
	mgr := tasks.NewManager(dir)
	state, _ := mgr.Start("task")
	state, _ = mgr.AddCriteria("go test ./... passes")
	state, _ = mgr.Move(app.StageExecution)
	evidence := issueGateEvidence(t, dir, state, "session_test", "go version")
	state, _ = mgr.RecordAcceptedExecution("execution accepted", evidence)
	state, _ = mgr.Move(app.StageValidation)
	gate := &TransitionGate{Tasks: mgr}
	parsed := ParsedResponse{Stage: app.StageValidation, TrustedEvidence: evidence, Validation: &ValidationOutput{
		Findings:     []ValidationFinding{},
		PassedChecks: []string{"tool evidence available"},
		Verdict:      "ready_for_done",
	}}
	_, err := gate.Apply(state, parsed, TransitionOptions{SessionID: "session_test"})
	if err == nil || app.AsError(err).Code != "transition_precondition_failed" {
		t.Fatalf("want transition_precondition_failed, got %v", err)
	}
	current, _ := mgr.Current()
	if current.Stage != app.StageValidation {
		t.Fatalf("irrelevant evidence moved stage: %+v", current)
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
