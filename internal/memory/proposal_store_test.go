package memory

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/tasks"
)

func TestProposalApplyRoutesRejectsEditsAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	taskState, err := tasks.NewManager(dir).Start("test")
	if err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(dir)
	store := NewProposalStore(dir, mgr)
	proposal := app.MemoryProposal{ID: "proposal_test", SessionID: "session_test", CreatedAt: time.Now().UTC(), Records: []app.ProposedMemoryRecord{
		{ID: "r_short", Layer: app.ProposedLayerShort, Kind: "context", Content: "short", Status: app.ProposalPending},
		{ID: "r_work", Layer: app.ProposedLayerWork, Kind: "requirement", Content: "work", Status: app.ProposalPending},
		{ID: "r_long", Layer: app.ProposedLayerLong, Kind: "preference", Content: "long", Status: app.ProposalPending},
		{ID: "r_ignore", Layer: app.ProposedLayerIgnore, Kind: "smalltalk", Content: "ignore", Status: app.ProposalPending},
		{ID: "r_reject", Layer: app.ProposedLayerLong, Kind: "other", Content: "reject", Status: app.ProposalPending},
	}}
	if err := store.Save(ctx, proposal); err != nil {
		t.Fatal(err)
	}
	result, err := store.Apply(ctx, ApplyOptions{ProposalID: proposal.ID, AcceptAll: true, SessionID: "session_test", TaskID: taskState.ID, RejectIDs: map[string]bool{"r_reject": true}, Edits: map[string]ProposalEdit{"r_work": {Layer: app.ProposedLayerLong, Content: "edited long"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.SavedRecords) != 3 {
		t.Fatalf("want 3 saved records, got %d", len(result.SavedRecords))
	}
	if result.Proposal.Records[1].Layer != app.ProposedLayerWork || result.Proposal.Records[1].AppliedLayer != app.ProposedLayerLong || result.Proposal.Records[1].AppliedContent != "edited long" || result.Proposal.Records[1].SavedRecordID == "" {
		t.Fatalf("edit audit lost proposed/applied fields: %+v", result.Proposal.Records[1])
	}
	second, err := store.Apply(ctx, ApplyOptions{ProposalID: proposal.ID, AcceptAll: true, SessionID: "session_test", TaskID: taskState.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.SavedRecords) != 0 {
		t.Fatalf("idempotent apply saved duplicates: %+v", second.SavedRecords)
	}
	shortRecords, _ := mgr.List(ctx, app.LayerShort, "session_test", "")
	workRecords, _ := mgr.List(ctx, app.LayerWork, "", taskState.ID)
	longRecords, _ := mgr.List(ctx, app.LayerLong, "", "")
	if len(shortRecords) != 1 || len(workRecords) != 0 || len(longRecords) != 2 {
		t.Fatalf("bad routed counts short=%d work=%d long=%d", len(shortRecords), len(workRecords), len(longRecords))
	}
	for _, record := range longRecords {
		if record.ProposalID == proposal.ID && record.ProposalRecordID == "" {
			t.Fatalf("saved proposal record id missing: %+v", record)
		}
	}
}

func TestConcurrentProposalApplyIsIdempotent(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	mgr := NewManager(dir)
	store := NewProposalStore(dir, mgr)
	proposal := app.MemoryProposal{ID: "proposal_concurrent", SessionID: "session_concurrent", CreatedAt: time.Now().UTC(), Records: []app.ProposedMemoryRecord{
		{ID: "r_short", Layer: app.ProposedLayerShort, Kind: "context", Content: "short", Status: app.ProposalPending},
	}}
	if err := store.Save(ctx, proposal); err != nil {
		t.Fatal(err)
	}
	const workers = 12
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Apply(ctx, ApplyOptions{ProposalID: proposal.ID, AcceptAll: true, SessionID: "session_concurrent"})
			errCh <- err
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	records, err := mgr.List(ctx, app.LayerShort, "session_concurrent", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("concurrent apply saved duplicates: %+v", records)
	}
}

func TestProposalAuditRedactsSecretsOnSaveAndEdit(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	taskState, err := tasks.NewManager(dir).Start("test")
	if err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(dir)
	store := NewProposalStore(dir, mgr)
	proposal := app.MemoryProposal{ID: "proposal_secret", SessionID: "session_secret", CreatedAt: time.Now().UTC(), Records: []app.ProposedMemoryRecord{
		{ID: "r_secret", Layer: app.ProposedLayerLong, Kind: "preference", Content: "OPENROUTER_API_KEY=sk-secret123456789", Status: app.ProposalPending},
		{ID: "r_edit", Layer: app.ProposedLayerLong, Kind: "preference", Content: "safe", Status: app.ProposalPending},
	}}
	if err := store.Save(ctx, proposal); err != nil {
		t.Fatal(err)
	}
	stored, err := store.Latest(ctx, "session_secret")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Records[0].Status != app.ProposalBlocked || stored.Records[0].Content != "[REDACTED_SECRET]" {
		t.Fatalf("secret not redacted on save: %+v", stored.Records[0])
	}
	result, err := store.Apply(ctx, ApplyOptions{ProposalID: proposal.ID, AcceptAll: true, SessionID: "session_secret", TaskID: taskState.ID, Edits: map[string]ProposalEdit{"r_edit": {Content: "Bearer abcdefghijklmnop"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.SavedRecords) != 0 {
		t.Fatalf("secret edit saved records: %+v", result.SavedRecords)
	}
	stored, err = store.Latest(ctx, "session_secret")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Records[1].Status != app.ProposalBlocked || stored.Records[1].Content != "safe" || stored.Records[1].AppliedContent != "[REDACTED_SECRET]" {
		t.Fatalf("secret not redacted on edit: %+v", stored.Records[1])
	}
}

func TestProposalApplyPreflightPreventsPartialWrites(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	mgr := NewManager(dir)
	store := NewProposalStore(dir, mgr)
	proposal := app.MemoryProposal{ID: "proposal_preflight", SessionID: "session_preflight", CreatedAt: time.Now().UTC(), Records: []app.ProposedMemoryRecord{
		{ID: "r_short", Layer: app.ProposedLayerShort, Kind: "context", Content: "short", Status: app.ProposalPending},
		{ID: "r_work", Layer: app.ProposedLayerWork, Kind: "requirement", Content: "work", Status: app.ProposalPending},
	}}
	if err := store.Save(ctx, proposal); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(ctx, ApplyOptions{ProposalID: proposal.ID, AcceptAll: true, SessionID: "session_preflight"}); err == nil {
		t.Fatal("expected missing current task")
	}
	shortRecords, err := mgr.List(ctx, app.LayerShort, "session_preflight", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(shortRecords) != 0 {
		t.Fatalf("partial save occurred before preflight failure: %+v", shortRecords)
	}
}
