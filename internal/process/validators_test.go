package process

import "testing"

func TestPlanningRejectsImplementation(t *testing.T) {
	errs := validatePlanning(&PlanningOutput{Summary: "I implemented it", Readiness: "needs_user_input"}, "")
	if len(errs) == 0 {
		t.Fatal("expected implementation rejection")
	}
}

func TestPlanningRejectsCodePatchOutsideSummary(t *testing.T) {
	errs := validatePlanning(&PlanningOutput{Summary: "plan", Plan: []string{"```go\nfmt.Println(1)\n```"}, Readiness: "needs_user_input"}, "")
	if len(errs) == 0 {
		t.Fatal("expected code patch rejection")
	}
}

func TestPlanningReadinessRequiresPlanAndCriteria(t *testing.T) {
	errs := validatePlanning(&PlanningOutput{Readiness: "ready_for_execution_proposal"}, "")
	if len(errs) == 0 {
		t.Fatal("expected readiness rejection")
	}
}

func TestPlanningOpenQuestionsBlockReadiness(t *testing.T) {
	errs := validatePlanning(&PlanningOutput{
		AcceptanceCriteria: []string{"c"},
		Plan:               []string{"p"},
		OpenQuestions:      []string{"q"},
		Readiness:          "ready_for_execution_proposal",
	}, "")
	if len(errs) == 0 {
		t.Fatal("expected open questions to block readiness")
	}
}

func TestExecutionRejectsFakeTestResult(t *testing.T) {
	errs := validateExecution(&ExecutionOutput{
		Verification: []string{"all tests passed"},
		NextSignal:   "ready_for_validation",
	})
	if len(errs) == 0 {
		t.Fatal("expected fake test rejection")
	}
}

func TestExecutionRejectsReadyWithBlockers(t *testing.T) {
	errs := validateExecution(&ExecutionOutput{
		Blockers:   []string{"blocked"},
		NextSignal: "ready_for_validation",
	})
	if len(errs) == 0 {
		t.Fatal("expected blocker rejection")
	}
}

func TestValidationRejectsFeatureImplementation(t *testing.T) {
	errs := validateValidation(&ValidationOutput{
		Findings: []ValidationFinding{{Severity: "low", Problem: "style"}},
		Verdict:  "needs_execution_fixes",
	})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	errs = validateValidation(&ValidationOutput{
		Findings: []ValidationFinding{{Severity: "low", Problem: "style", Fix: "I will add a new feature"}},
		Verdict:  "needs_execution_fixes",
	})
	if len(errs) == 0 {
		t.Fatal("expected feature rejection")
	}
}

func TestValidationReadyForDoneBlockedByHighFinding(t *testing.T) {
	errs := validateValidation(&ValidationOutput{
		Findings: []ValidationFinding{{Severity: "high", Problem: "bug"}},
		Verdict:  "ready_for_done",
	})
	if len(errs) == 0 {
		t.Fatal("expected high finding rejection")
	}
}

func TestValidationReadyForDoneBlockedByMissingEvidence(t *testing.T) {
	errs := validateValidation(&ValidationOutput{
		Findings:        []ValidationFinding{},
		MissingEvidence: []string{"no test output"},
		Verdict:         "ready_for_done",
	})
	if len(errs) == 0 {
		t.Fatal("expected missing evidence rejection")
	}
}

func TestDoneRejectsMutation(t *testing.T) {
	errs := validateDone(&DoneOutput{Summary: "done"}, "now implement more")
	if len(errs) == 0 {
		t.Fatal("expected mutation rejection")
	}
}
