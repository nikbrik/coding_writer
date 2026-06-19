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
