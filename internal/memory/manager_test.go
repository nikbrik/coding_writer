package memory

import (
	"context"
	"fmt"
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

func TestLongMemoryFiltersByProfileAndKeepsGlobal(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager(t.TempDir())
	if _, err := mgr.Save(ctx, SaveInput{Layer: app.LayerLong, Kind: "preference", Content: "student preference", Source: "test", ProfileID: "student"}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Save(ctx, SaveInput{Layer: app.LayerLong, Kind: "preference", Content: "senior preference", Source: "test", ProfileID: "senior"}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Save(ctx, SaveInput{Layer: app.LayerLong, Kind: "knowledge", Content: "global fact", Source: "test", Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	bundle, err := mgr.SelectForPrompt(ctx, "", "", "student")
	if err != nil {
		t.Fatal(err)
	}
	if containsContent(bundle.Long, "senior preference") || !containsContent(bundle.Long, "student preference") || !containsContent(bundle.Long, "global fact") {
		t.Fatalf("bad profile-filtered long memory: %+v", bundle.Long)
	}
}

func TestLongMemorySelectionUsesRecordTimeAcrossKinds(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager(t.TempDir())
	for i := 0; i < 25; i++ {
		kind := "knowledge"
		if i == 24 {
			kind = "preference"
		}
		if _, err := mgr.Save(ctx, SaveInput{Layer: app.LayerLong, Kind: kind, Content: fmt.Sprintf("record-%02d", i), Source: "test", Scope: "global"}); err != nil {
			t.Fatal(err)
		}
	}
	bundle, err := mgr.SelectForPrompt(ctx, "", "", "student")
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Long) != 20 || !containsContent(bundle.Long, "record-24") || containsContent(bundle.Long, "record-00") {
		t.Fatalf("long memory selection is not latest-by-time: %+v", bundle.Long)
	}
}

func containsContent(records []app.MemoryRecord, want string) bool {
	for _, record := range records {
		if strings.Contains(record.Content, want) {
			return true
		}
	}
	return false
}

func TestSecretBlockedInManualSave(t *testing.T) {
	_, err := NewManager(t.TempDir()).Save(context.Background(), SaveInput{Layer: app.LayerLong, Kind: "preference", Content: "OPENROUTER_API_KEY=sk-secret123456789", Source: "test"})
	if err == nil || !strings.Contains(err.Error(), "secret") {
		t.Fatalf("want secret blocked, got %v", err)
	}
}

func TestLongMemoryScopeRulesAreExplicit(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager(t.TempDir())
	cases := []struct {
		name            string
		scope           string
		profileID       string
		activeProfileID string
		wantIncluded    bool
	}{
		{"global always visible", "global", "", "student", true},
		{"global with profileID still global", "global", "senior", "student", true},
		{"profile matching active", "profile", "student", "student", true},
		{"profile mismatch excluded", "profile", "senior", "student", false},
		{"profile empty profileID excluded", "profile", "", "student", false},
		{"empty scope defaults to global", "", "", "student", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := mgr.Save(ctx, SaveInput{Layer: app.LayerLong, Kind: "preference", Content: tc.name, Source: "test", Scope: tc.scope, ProfileID: tc.profileID}); err != nil {
				t.Fatal(err)
			}
			bundle, err := mgr.SelectForPrompt(ctx, "", "", tc.activeProfileID)
			if err != nil {
				t.Fatal(err)
			}
			got := containsContent(bundle.Long, tc.name)
			if got != tc.wantIncluded {
				t.Fatalf("want included=%v, got %v for %+v; long=%+v", tc.wantIncluded, got, tc, bundle.Long)
			}
		})
	}
}
