package process

import (
	"context"
	"strings"
	"testing"

	"github.com/nikbrik/coding_writer/internal/app"
)

func TestProcessControllerAuditsAcceptedAndRejected(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	fake.ChatResponse = `{"stage":"planning","summary":"ok","assumptions":[],"acceptance_criteria":["c"],"plan":["p"],"open_questions":[],"readiness":"ready_for_execution_proposal"}`
	if _, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s1", Input: "спланируй", ActionKind: ActionPlanTask}); err != nil {
		t.Fatal(err)
	}
	fake.ChatResponse = `{"stage":"planning","summary":"I implemented it","assumptions":[],"acceptance_criteria":["c"],"plan":["p"],"open_questions":[],"readiness":"ready_for_execution_proposal"}`
	_, _ = ctrl.RunExchange(ctx, ExchangeInput{SessionID: "s2", Input: "спланируй", ActionKind: ActionPlanTask})
	events, err := ctrl.AuditStore.Latest(10)
	if err != nil {
		t.Fatal(err)
	}
	var accepted, rejected bool
	for _, e := range events {
		if e.Decision == "accepted" && e.SessionID == "s1" {
			accepted = true
		}
		if e.Decision == "rejected" && e.SessionID == "s2" && strings.Contains(strings.Join(e.ValidatorErrors, ";"), "implementation") {
			rejected = true
		}
	}
	if !accepted || !rejected {
		t.Fatalf("missing audit events: %+v", events)
	}
}

func TestProcessControllerRejectedOutputDoesNotAuditAccepted(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	state, err := ctrl.Tasks.Start("task")
	if err != nil {
		t.Fatal(err)
	}
	state, err = ctrl.Tasks.Move(app.StageExecution)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Tasks.Move(app.StageValidation); err != nil {
		t.Fatal(err)
	}
	_ = state
	fake.ChatResponse = `{"stage":"validation","findings":[{"severity":"low","location":"file","problem":"bug","fix":""}],"passed_checks":[],"missing_evidence":[],"residual_risks":[],"verdict":"needs_execution_fixes"}`
	_, err = ctrl.RunExchange(ctx, ExchangeInput{SessionID: "transition_error", Input: "review", ActionKind: ActionReviewOutput})
	if err == nil || app.AsError(err).Code != "validation_failed" {
		t.Fatalf("want validation_failed, got %v", err)
	}
	events, err := ctrl.AuditStore.Latest(20)
	if err != nil {
		t.Fatal(err)
	}
	var rejected bool
	for _, e := range events {
		if e.SessionID != "transition_error" {
			continue
		}
		if e.Decision == "accepted" {
			t.Fatalf("transition error must not audit accepted: %+v", events)
		}
		if e.Decision == "rejected" && strings.Contains(strings.Join(e.ValidatorErrors, ";"), "finding missing required") {
			rejected = true
		}
	}
	if !rejected {
		t.Fatalf("missing rejected transition audit: %+v", events)
	}
}

func TestProcessControllerAuditsProviderError(t *testing.T) {
	ctx := context.Background()
	ctrl, fake, _ := newTestController(t)
	if _, err := ctrl.Tasks.Start("task"); err != nil {
		t.Fatal(err)
	}
	fake.Err = app.NewError(app.CategoryProvider, "network", "network down", nil)
	_, err := ctrl.RunExchange(ctx, ExchangeInput{SessionID: "provider_error", Input: "hello", ActionKind: ActionAnswerQuestion})
	if err == nil || app.AsError(err).Code != "network" {
		t.Fatalf("want provider network error, got %v", err)
	}
	events, err := ctrl.AuditStore.Latest(10)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if e.SessionID == "provider_error" && e.Decision == "rejected" && strings.Contains(strings.Join(e.ValidatorErrors, ";"), "network down") {
			return
		}
	}
	t.Fatalf("missing provider error audit: %+v", events)
}
