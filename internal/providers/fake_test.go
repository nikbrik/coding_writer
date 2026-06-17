package providers

import (
	"context"
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
