package process

import (
	"context"
	"testing"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/providers"
)

func TestPlanningSwarmRetriesMalformedAgentJSON(t *testing.T) {
	fake := providers.NewFakeProvider()
	fake.ChatResponses = []string{
		`{"role":"requirements_specialist","summary":`,
		`{"role":"requirements_specialist","summary":"ok","findings":[]}`,
	}
	swarm := &PlanningSwarm{
		Runner: &AgentRunner{Provider: fake, Model: "fake/model"},
	}
	var review SpecialistReview
	res, err := swarm.runDecodedAgent(context.Background(), AgentRunInput{
		SessionID: "session_retry",
		Task:      &app.TaskState{ID: "task_retry", Stage: app.StagePlanning},
		UserInput: "plan",
		Microtask: Microtask{
			ID:         "microtask_retry",
			Role:       AgentRoleRequirementsSpecialist,
			Stage:      app.StagePlanning,
			ActionKind: ActionPlanTask,
		},
	}, 1, "agent_call", "agent_accepted", &review)
	if err != nil {
		t.Fatalf("runDecodedAgent returned error: %v", err)
	}
	if review.Summary != "ok" || res.Raw == "" {
		t.Fatalf("unexpected retry result: review=%+v res=%+v", review, res)
	}
	if got := len(fake.SnapshotCalls()); got != 2 {
		t.Fatalf("expected retry to call provider twice, got %d", got)
	}
}

func TestPlanningSwarmContinuesAfterMalformedSpecialistJSON(t *testing.T) {
	fake := providers.NewFakeProvider()
	fake.ChatResponses = []string{
		`{"role":"requirements_specialist","summary":`,
		`{"role":"requirements_specialist","summary":`,
		`{"role":"code_research_specialist","summary":"ok","findings":[]}`,
		`{"role":"architecture_specialist","summary":"ok","findings":[]}`,
		`{"role":"test_validation_specialist","summary":"ok","findings":[]}`,
		`{"role":"risk_regression_specialist","summary":"ok","findings":[]}`,
		`{"stage":"planning","summary":"verify package","assumptions":[],"acceptance_criteria":["go test passes"],"plan":["Run go test ./manual_scratch/day14_stock_profit"],"open_questions":[],"readiness":"ready_for_execution_proposal"}`,
		`{"role":"requirements_specialist","summary":"ok","findings":[]}`,
		`{"role":"code_research_specialist","summary":"ok","findings":[]}`,
		`{"role":"architecture_specialist","summary":"ok","findings":[]}`,
		`{"role":"test_validation_specialist","summary":"ok","findings":[]}`,
		`{"role":"risk_regression_specialist","summary":"ok","findings":[]}`,
	}
	swarm := &PlanningSwarm{
		Runner: &AgentRunner{Provider: fake, Model: "fake/model"},
	}
	res, err := swarm.Run(context.Background(), "session_retry", &app.TaskState{ID: "task_retry", Stage: app.StagePlanning}, "verify package")
	if err != nil {
		t.Fatalf("planning swarm should tolerate one malformed specialist: %v", err)
	}
	if res.FinalSummary != "verify package" || len(res.FinalPlan) != 1 {
		t.Fatalf("unexpected final plan: %+v", res)
	}
	if len(res.Findings) != 1 || res.Findings[0].Severity != PlanFindingMedium {
		t.Fatalf("expected medium helper finding, got %+v", res.Findings)
	}
}
