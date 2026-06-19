package providers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/nikbrik/coding_writer/internal/app"
)

func TestFakeProviderRecordsCallsBeforeErr(t *testing.T) {
	fake := NewFakeProvider()
	fake.Err = errors.New("boom")
	_, err := fake.Complete(context.Background(), CompletionRequest{Purpose: PurposeChat, Model: "fake/model", Messages: []app.ChatMessage{{Role: app.RoleUser, Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error")
	}
	calls := fake.SnapshotCalls()
	if len(calls) != 1 || calls[0].Model != "fake/model" {
		t.Fatalf("call not recorded before error: %+v", calls)
	}
}

func TestFakeProviderIntentNegationOverridesFinishKeyword(t *testing.T) {
	fake := NewFakeProvider()
	resp, err := fake.Complete(context.Background(), CompletionRequest{
		Purpose: PurposeValidator,
		Model:   "fake/model",
		Messages: []app.ChatMessage{{Role: app.RoleUser, Content: `You are an out-of-band intent referee.
{"user_input":"Проверь критерии по evidence, но пока не завершай задачу; дай validation review.","deterministic":"review_output"}`}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		ActionKind       string `json:"action_kind"`
		TransitionSignal string `json:"transition_signal"`
	}
	if err := json.Unmarshal([]byte(resp.Message.Content), &parsed); err != nil {
		t.Fatalf("bad fake validator JSON: %v raw=%s", err, resp.Message.Content)
	}
	if parsed.TransitionSignal != "none" {
		t.Fatalf("negated finish intent must not become ready_for_done: %s", resp.Message.Content)
	}
	if parsed.ActionKind != "review_output" {
		t.Fatalf("negated finish with review request should stay review_output: %s", resp.Message.Content)
	}
}
