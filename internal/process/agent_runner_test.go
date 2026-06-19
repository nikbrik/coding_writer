package process

import (
	"testing"

	"github.com/nikbrik/coding_writer/internal/app"
)

func TestMicrotaskTaskSummaryIncludesApprovedPlanContext(t *testing.T) {
	task := &app.TaskState{
		ID:                 "task_1",
		Stage:              app.StageExecution,
		Objective:          "implement contains duplicate",
		CurrentStep:        "create package directory",
		ApprovedPlanID:     "plan_1",
		AcceptanceCriteria: []string{"has implementation", "has tests"},
		Plan: []string{
			"create package directory",
			"provide contains_duplicate.go implementation",
			"provide contains_duplicate_test.go table tests",
		},
		Microtasks: []app.MicrotaskState{
			{ID: "m1", PlanItem: "create package directory", Status: "pending"},
			{ID: "m2", PlanItem: "provide contains_duplicate.go implementation", Status: "pending"},
		},
		CompletedSteps:     []string{"planning approved"},
		ValidationStatus:   "ready_for_review",
		ValidationEvidence: []string{"evidence_1"},
	}

	summary := microtaskTaskSummary(task)
	if got := summary["objective"]; got != task.Objective {
		t.Fatalf("objective missing from summary: %#v", summary)
	}
	if got := summary["plan"].([]string); len(got) != len(task.Plan) || got[1] != task.Plan[1] {
		t.Fatalf("plan missing from summary: %#v", summary)
	}
	if got := summary["microtasks"].([]app.MicrotaskState); len(got) != len(task.Microtasks) || got[1].PlanItem != task.Microtasks[1].PlanItem {
		t.Fatalf("microtasks missing from summary: %#v", summary)
	}
	if got := summary["validation_evidence"].([]string); len(got) != 1 || got[0] != "evidence_1" {
		t.Fatalf("validation evidence missing from summary: %#v", summary)
	}
}
