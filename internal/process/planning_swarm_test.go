package process

import (
	"context"
	"strings"
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

func TestPlanningSwarmSetsSpecialistRolesFromMicrotasks(t *testing.T) {
	fake := providers.NewFakeProvider()
	fake.ChatResponses = []string{
		`{"summary":"requirements ok","findings":[]}`,
		`{"summary":"code ok","findings":[]}`,
		`{"summary":"architecture ok","findings":[]}`,
		`{"summary":"tests ok","findings":[]}`,
		`{"summary":"risk ok","findings":[]}`,
		`{"stage":"planning","summary":"plan","assumptions":[],"acceptance_criteria":["criteria"],"plan":["step"],"open_questions":[],"readiness":"ready_for_execution_proposal"}`,
		`{"summary":"requirements recheck","findings":[]}`,
		`{"summary":"code recheck","findings":[]}`,
		`{"summary":"architecture recheck","findings":[]}`,
		`{"summary":"tests recheck","findings":[]}`,
		`{"summary":"risk recheck","findings":[]}`,
	}
	swarm := &PlanningSwarm{
		Runner: &AgentRunner{Provider: fake, Model: "fake/model"},
	}
	res, err := swarm.Run(context.Background(), "session_roles", &app.TaskState{ID: "task_roles", Stage: app.StagePlanning}, "plan")
	if err != nil {
		t.Fatal(err)
	}
	want := []PlanningSpecialistRole{
		PlanningSpecialistRole(AgentRoleRequirementsSpecialist),
		PlanningSpecialistRole(AgentRoleCodeResearchSpecialist),
		PlanningSpecialistRole(AgentRoleArchitectureSpecialist),
		PlanningSpecialistRole(AgentRoleTestValidationSpecialist),
		PlanningSpecialistRole(AgentRoleRiskRegressionSpecialist),
		PlanningSpecialistRole(AgentRoleRequirementsSpecialist),
		PlanningSpecialistRole(AgentRoleCodeResearchSpecialist),
		PlanningSpecialistRole(AgentRoleArchitectureSpecialist),
		PlanningSpecialistRole(AgentRoleTestValidationSpecialist),
		PlanningSpecialistRole(AgentRoleRiskRegressionSpecialist),
	}
	if len(res.Reviews) != len(want) {
		t.Fatalf("reviews count mismatch: got %d want %d", len(res.Reviews), len(want))
	}
	for i := range want {
		if res.Reviews[i].Role != want[i] {
			t.Fatalf("review %d role = %q, want %q", i, res.Reviews[i].Role, want[i])
		}
	}
}

func TestPlanningSpecialistInstructionsAreRoleSpecific(t *testing.T) {
	cases := map[AgentRole]string{
		AgentRoleRequirementsSpecialist:   "acceptance criteria completeness",
		AgentRoleCodeResearchSpecialist:   "implementation surface",
		AgentRoleArchitectureSpecialist:   "module boundaries",
		AgentRoleTestValidationSpecialist: "exact verification evidence",
		AgentRoleRiskRegressionSpecialist: "false completion risk",
	}
	for role, want := range cases {
		prompt := planningSpecialistInstruction(role, false)
		if !strings.Contains(prompt, "Do not restate the user's task as the summary") || !strings.Contains(prompt, want) {
			t.Fatalf("instruction for %s missing expected focus %q:\n%s", role, want, prompt)
		}
	}
}
