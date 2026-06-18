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

func TestPlanningRejectsUnknownReadiness(t *testing.T) {
	errs := validatePlanning(&PlanningOutput{Summary: "s", Readiness: "done_enough"}, "")
	if len(errs) == 0 {
		t.Fatal("expected unknown readiness rejection")
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

func TestExecutionRejectsWeakToolSubstring(t *testing.T) {
	errs := validateExecution(&ExecutionOutput{Summary: "s", Verification: []string{"no tool output but tests passed"}, NextSignal: "continue_execution"})
	if len(errs) == 0 {
		t.Fatal("expected weak tool substring rejection")
	}
}

func TestExecutionRejectsInventedTrustedToolEvidence(t *testing.T) {
	errs := validateExecution(&ExecutionOutput{Summary: "s", Verification: []string{"trusted tool evidence: tests passed"}, NextSignal: "continue_execution"})
	if len(errs) == 0 {
		t.Fatal("expected invented tool evidence rejection")
	}
}

func TestExecutionRejectsImplementationClaimWithoutTrustedEvidence(t *testing.T) {
	errs := validateExecution(&ExecutionOutput{Summary: "updated file internal/foo.go", NextSignal: "continue_execution"})
	if len(errs) == 0 {
		t.Fatal("expected implementation claim rejection")
	}
}

func TestExecutionRejectsImplementationClaimInCurrentStep(t *testing.T) {
	errs := validateExecution(&ExecutionOutput{Summary: "s", CurrentStep: "updated file internal/foo.go", NextSignal: "continue_execution"})
	if len(errs) == 0 {
		t.Fatal("expected current_step implementation claim rejection")
	}
}

func TestExecutionRejectsTestClaimInNextStep(t *testing.T) {
	errs := validateExecution(&ExecutionOutput{Summary: "s", NextStep: "tests passed", NextSignal: "continue_execution"})
	if len(errs) == 0 {
		t.Fatal("expected next_step test claim rejection")
	}
}

func TestExecutionRejectsRanTestClaimInCurrentStep(t *testing.T) {
	errs := validateExecution(&ExecutionOutput{Summary: "s", CurrentStep: "ran go test ./...", NextSignal: "continue_execution"})
	if len(errs) == 0 {
		t.Fatal("expected current_step test command claim rejection")
	}
}

func TestExecutionRejectsImplementationClaimInCompletedSteps(t *testing.T) {
	errs := validateExecution(&ExecutionOutput{Summary: "s", CompletedSteps: []string{"updated file internal/foo.go"}, NextSignal: "continue_execution"})
	if len(errs) == 0 {
		t.Fatal("expected completed_steps implementation claim rejection")
	}
}

func TestExecutionRejectsGoTestPassedClaimInCompletedSteps(t *testing.T) {
	errs := validateExecution(&ExecutionOutput{Summary: "s", CompletedSteps: []string{"go test ./... passed"}, NextSignal: "continue_execution"})
	if len(errs) == 0 {
		t.Fatal("expected completed_steps terse test claim rejection")
	}
}

func TestExecutionRejectsToolResultClaimInNextStep(t *testing.T) {
	errs := validateExecution(&ExecutionOutput{Summary: "s", NextStep: "tool_result: go test ok", NextSignal: "continue_execution"})
	if len(errs) == 0 {
		t.Fatal("expected next_step tool result claim rejection")
	}
}

func TestExecutionAllowsBenignProgressFields(t *testing.T) {
	errs := validateExecution(&ExecutionOutput{Summary: "worked", CurrentStep: "first", CompletedSteps: []string{"first"}, NextStep: "second", NextSignal: "continue_execution"})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
}

func TestAnswerQuestionRejectsImplementationClaim(t *testing.T) {
	errs := validateAnswerQuestion("updated file internal/foo.go")
	if len(errs) == 0 {
		t.Fatal("expected answer_question implementation claim rejection")
	}
}

func TestAnswerQuestionRejectsTransitionSignals(t *testing.T) {
	errs := validateAnswerQuestion(`{"next_signal":"ready_for_validation"}`)
	if len(errs) == 0 {
		t.Fatal("expected answer_question transition signal rejection")
	}
	errs = validateAnswerQuestion("readiness: ready_for_execution_proposal")
	if len(errs) == 0 {
		t.Fatal("expected answer_question readiness rejection")
	}
}

func TestExecutionRejectsPassedAndNotRunMix(t *testing.T) {
	errs := validateExecution(&ExecutionOutput{Summary: "s", Verification: []string{"tests passed; not run"}, NextSignal: "continue_execution"})
	if len(errs) == 0 {
		t.Fatal("expected contradictory verification rejection")
	}
}

func TestExecutionRejectsUnknownNextSignal(t *testing.T) {
	errs := validateExecution(&ExecutionOutput{Summary: "s", NextSignal: "done"})
	if len(errs) == 0 {
		t.Fatal("expected unknown next_signal rejection")
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
		Findings: []ValidationFinding{{Severity: "low", Location: "file", Problem: "style", Fix: "fix style"}},
		Verdict:  "needs_execution_fixes",
	})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	errs = validateValidation(&ValidationOutput{
		Findings: []ValidationFinding{{Severity: "low", Location: "file", Problem: "style", Fix: "I will add a new feature"}},
		Verdict:  "needs_execution_fixes",
	})
	if len(errs) == 0 {
		t.Fatal("expected feature rejection")
	}
}

func TestValidationRejectsImplementationInPassedChecks(t *testing.T) {
	errs := validateValidation(&ValidationOutput{PassedChecks: []string{"implemented fix while reviewing"}, Verdict: "blocked_missing_evidence"})
	if len(errs) == 0 {
		t.Fatal("expected implementation claim rejection")
	}
}

func TestValidationRejectsUnknownVerdict(t *testing.T) {
	errs := validateValidation(&ValidationOutput{PassedChecks: []string{"checked"}, Verdict: "ship_it"})
	if len(errs) == 0 {
		t.Fatal("expected unknown verdict rejection")
	}
}

func TestValidationRejectsUnknownSeverityAndIncompleteFinding(t *testing.T) {
	errs := validateValidation(&ValidationOutput{Findings: []ValidationFinding{{Severity: "critical", Problem: "bug"}}, Verdict: "needs_execution_fixes"})
	if len(errs) == 0 {
		t.Fatal("expected finding validation errors")
	}
}

func TestValidationNeedsExecutionFixesRequiresActionableFinding(t *testing.T) {
	errs := validateValidation(&ValidationOutput{PassedChecks: []string{"checked"}, Verdict: "needs_execution_fixes"})
	if len(errs) == 0 {
		t.Fatal("expected actionable finding requirement")
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

func TestValidationReadyForDoneBlockedByMixedCaseHighFinding(t *testing.T) {
	errs := validateValidation(&ValidationOutput{
		Findings: []ValidationFinding{{Severity: " High ", Problem: "bug"}},
		Verdict:  "ready_for_done",
	})
	if len(errs) == 0 {
		t.Fatal("expected mixed-case high finding rejection")
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

func TestValidationReadyForDoneRequiresPassedChecks(t *testing.T) {
	errs := validateValidation(&ValidationOutput{Findings: []ValidationFinding{}, Verdict: "ready_for_done"})
	if len(errs) == 0 {
		t.Fatal("expected evidence requirement")
	}
}

func TestValidationReadyForDoneRequiresTrustedEvidence(t *testing.T) {
	errs := validateValidation(&ValidationOutput{PassedChecks: []string{"tests passed"}, Verdict: "ready_for_done"})
	if len(errs) == 0 {
		t.Fatal("expected missing trusted evidence rejection")
	}
	errs = validateValidation(&ValidationOutput{PassedChecks: []string{"tests passed"}, Verdict: "ready_for_done"}, NewTrustedEvidence("go test ./...", 0, "ok"))
	if len(errs) != 0 {
		t.Fatalf("trusted evidence should satisfy ready_for_done: %v", errs)
	}
}

func TestDoneRejectsMutation(t *testing.T) {
	errs := validateDone(&DoneOutput{Summary: "done"}, "now implement more")
	if len(errs) == 0 {
		t.Fatal("expected mutation rejection")
	}
}
