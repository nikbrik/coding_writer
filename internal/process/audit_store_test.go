package process

import (
	"testing"

	"github.com/nikbrik/coding_writer/internal/app"
)

func TestAuditStoreSaveAndLatest(t *testing.T) {
	store := NewAuditStore(t.TempDir())
	if err := store.Save(ProcessAuditEvent{SessionID: "s1", TaskID: "t1", Stage: app.StagePlanning, ActionKind: ActionPlanTask, Decision: "accepted", Model: "fake/model"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ProcessAuditEvent{SessionID: "s2", TaskID: "t2", Stage: app.StageExecution, ActionKind: ActionExecutePlanStep, Decision: "rejected", ValidatorErrors: []string{"bad"}}); err != nil {
		t.Fatal(err)
	}
	events, err := store.Latest(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].SessionID != "s2" || events[0].Decision != "rejected" {
		t.Fatalf("unexpected latest events: %+v", events)
	}
	if events[0].ID == "" || events[0].CreatedAt.IsZero() {
		t.Fatalf("missing generated fields: %+v", events[0])
	}
}
