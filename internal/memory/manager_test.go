package memory

import (
	"context"
	"strings"
	"testing"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/tasks"
)

func TestMemoryLayersAreSeparateAndClearShortOnly(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	taskMgr := tasks.NewManager(dir)
	taskState, err := taskMgr.Start("task")
	if err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(dir)
	sessionID := "session_test"
	if _, err := mgr.Save(ctx, SaveInput{Layer: app.LayerShort, Kind: "context", Content: "short fact", Source: "test", SessionID: sessionID}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Save(ctx, SaveInput{Layer: app.LayerWork, Kind: "requirement", Content: "work fact", Source: "test", TaskID: taskState.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Save(ctx, SaveInput{Layer: app.LayerLong, Kind: "preference", Content: "long fact", Source: "test"}); err != nil {
		t.Fatal(err)
	}
	short, _ := mgr.List(ctx, app.LayerShort, sessionID, "")
	work, _ := mgr.List(ctx, app.LayerWork, "", taskState.ID)
	long, _ := mgr.List(ctx, app.LayerLong, "", "")
	if len(short) != 1 || len(work) != 1 || len(long) != 1 {
		t.Fatalf("bad layer counts short=%d work=%d long=%d", len(short), len(work), len(long))
	}
	if err := mgr.ClearShort(ctx, sessionID); err != nil {
		t.Fatal(err)
	}
	short, _ = mgr.List(ctx, app.LayerShort, sessionID, "")
	work, _ = mgr.List(ctx, app.LayerWork, "", taskState.ID)
	long, _ = mgr.List(ctx, app.LayerLong, "", "")
	if len(short) != 0 || len(work) != 1 || len(long) != 1 {
		t.Fatalf("clear short touched other layers short=%d work=%d long=%d", len(short), len(work), len(long))
	}
}

func TestSecretBlockedInManualSave(t *testing.T) {
	_, err := NewManager(t.TempDir()).Save(context.Background(), SaveInput{Layer: app.LayerLong, Kind: "preference", Content: "OPENROUTER_API_KEY=sk-secret123456789", Source: "test"})
	if err == nil || !strings.Contains(err.Error(), "secret") {
		t.Fatalf("want secret blocked, got %v", err)
	}
}
