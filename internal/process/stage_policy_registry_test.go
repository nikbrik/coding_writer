package process

import (
	"testing"

	"github.com/nikbrik/coding_writer/internal/app"
)

func TestStagePolicyRegistryCanonicalStages(t *testing.T) {
	registry := NewStagePolicyRegistry()
	for _, stage := range []app.TaskStage{app.StagePlanning, app.StageExecution, app.StageValidation, app.StageDone} {
		policy, err := registry.PolicyFor(stage)
		if err != nil {
			t.Fatalf("stage %s: unexpected error: %v", stage, err)
		}
		if policy.Stage != stage {
			t.Fatalf("stage %s: policy stage mismatch %s", stage, policy.Stage)
		}
		if policy.Role == "" {
			t.Fatalf("stage %s: empty role", stage)
		}
		if len(policy.AllowedActions) == 0 {
			t.Fatalf("stage %s: no allowed actions", stage)
		}
	}
}

func TestStagePolicyRegistryUnknownStageFailsClosed(t *testing.T) {
	registry := NewStagePolicyRegistry()
	_, err := registry.PolicyFor("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown stage")
	}
	appErr := app.AsError(err)
	if appErr.Category != app.CategoryValidation || appErr.Code != "unknown_stage" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStagePolicyAllowsAndForbids(t *testing.T) {
	registry := NewStagePolicyRegistry()
	policy, _ := registry.PolicyFor(app.StagePlanning)
	if !policy.Allows(ActionPlanTask) {
		t.Fatal("planning should allow plan_task")
	}
	if policy.Allows(ActionExecutePlanStep) {
		t.Fatal("planning should forbid execute_plan_step")
	}
	if policy.Allows(ActionAnswerQuestion) && RequiresSchema(ActionAnswerQuestion) {
		t.Fatal("answer_question should not require schema")
	}
}

func TestStagePolicyRegistryPolicyForReturnsCopy(t *testing.T) {
	registry := NewStagePolicyRegistry()
	policy, err := registry.PolicyFor(app.StagePlanning)
	if err != nil {
		t.Fatal(err)
	}
	policy.AllowedActions[0] = ActionExecutePlanStep
	fresh, err := registry.PolicyFor(app.StagePlanning)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.AllowedActions[0] == ActionExecutePlanStep {
		t.Fatal("policy slices should not be externally mutable")
	}
}

func TestActionKindAllowedStages(t *testing.T) {
	if !ActionPlanTask.IsAllowedIn(app.StagePlanning) {
		t.Fatal("plan_task must be allowed in planning")
	}
	if ActionPlanTask.IsAllowedIn(app.StageExecution) {
		t.Fatal("plan_task must not be allowed in execution")
	}
	if !ActionAnswerQuestion.IsAllowedIn(app.StageDone) {
		t.Fatal("answer_question must be allowed in done")
	}
}
