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
		{ID: "r_long", Layer: app.ProposedLayerLong, Kind: "preference", Content: "long", ProfileID: "student", Status: app.ProposalPending},
		{ID: "r_ignore", Layer: app.ProposedLayerIgnore, Kind: "smalltalk", Content: "ignore", Status: app.ProposalPending},
		{ID: "r_reject", Layer: app.ProposedLayerLong, Kind: "other", Content: "reject", Status: app.ProposalPending},
	}}
	if err := store.Save(ctx, proposal); err != nil {
		t.Fatal(err)
	}
	result, err := store.Apply(ctx, ApplyOptions{ProposalID: proposal.ID, AcceptAll: true, SessionID: "session_test", TaskID: taskState.ID, ProfileID: "student", RejectIDs: map[string]bool{"r_reject": true}, Edits: map[string]ProposalEdit{"r_work": {Layer: app.ProposedLayerLong, Content: "edited long"}}})
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
		switch record.Kind {
		case "preference":
			if record.Scope != "profile" || record.ProfileID != "student" {
				t.Fatalf("preference should be profile-scoped: %+v", record)
			}
		default:
			if record.Scope != "global" || record.ProfileID != "student" {
				t.Fatalf("non-preference long memory should be global and auditable to profile: %+v", record)
			}
		}
	}
}

func TestProposalApplyUsesStoredGenerationProfile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	mgr := NewManager(dir)
	store := NewProposalStore(dir, mgr)
	proposal := app.MemoryProposal{ID: "proposal_profile", SessionID: "session_profile", CreatedAt: time.Now().UTC(), Records: []app.ProposedMemoryRecord{
		{ID: "r_long", Layer: app.ProposedLayerLong, Kind: "preference", Content: "prefers senior style", ProfileID: "senior", Status: app.ProposalPending},
	}}
	if err := store.Save(ctx, proposal); err != nil {
		t.Fatal(err)
	}
	result, err := store.Apply(ctx, ApplyOptions{ProposalID: proposal.ID, AcceptAll: true, SessionID: "session_profile", ProfileID: "student"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.SavedRecords) != 1 {
		t.Fatalf("want one saved record, got %+v", result.SavedRecords)
	}
	if result.SavedRecords[0].ProfileID != "senior" || result.SavedRecords[0].Scope != "profile" {
		t.Fatalf("apply-time profile overrode generation profile: %+v", result.SavedRecords[0])
	}
}

func TestProposalApplyRejectsLegacyProfileMemoryWithoutGenerationProfile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	mgr := NewManager(dir)
	store := NewProposalStore(dir, mgr)
	proposal := app.MemoryProposal{ID: "proposal_legacy_profile", SessionID: "session_legacy_profile", CreatedAt: time.Now().UTC(), Records: []app.ProposedMemoryRecord{
		{ID: "r_short", Layer: app.ProposedLayerShort, Kind: "context", Content: "short", Status: app.ProposalPending},
		{ID: "r_long", Layer: app.ProposedLayerLong, Kind: "preference", Content: "legacy preference", Status: app.ProposalPending},
	}}
	if err := store.Save(ctx, proposal); err != nil {
		t.Fatal(err)
	}
	_, err := store.Apply(ctx, ApplyOptions{ProposalID: proposal.ID, AcceptAll: true, SessionID: "session_legacy_profile", ProfileID: "student"})
	if err == nil || app.AsError(err).Code != "missing_proposal_profile" {
		t.Fatalf("want missing_proposal_profile, got %v", err)
	}
	shortRecords, err := mgr.List(ctx, app.LayerShort, "session_legacy_profile", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(shortRecords) != 0 {
		t.Fatalf("preflight failure allowed partial write: %+v", shortRecords)
	}
}

func TestProposalApplyRequiresExplicitAction(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	mgr := NewManager(dir)
	store := NewProposalStore(dir, mgr)
	proposal := app.MemoryProposal{ID: "proposal_empty_apply", SessionID: "session_empty_apply", CreatedAt: time.Now().UTC(), Records: []app.ProposedMemoryRecord{
		{ID: "r_short", Layer: app.ProposedLayerShort, Kind: "context", Content: "short", Reason: "context", Status: app.ProposalPending},
	}}
	if err := store.Save(ctx, proposal); err != nil {
		t.Fatal(err)
	}
	_, err := store.Apply(ctx, ApplyOptions{ProposalID: proposal.ID, SessionID: "session_empty_apply"})
	if err == nil || app.AsError(err).Code != "missing_apply_action" {
		t.Fatalf("want missing_apply_action, got %v", err)
	}
}

func TestLongDecisionDefaultsGlobalAcrossProfiles(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	mgr := NewManager(dir)
	record, err := mgr.Save(ctx, SaveInput{Layer: app.LayerLong, Kind: "decision", Content: "Use PostgreSQL", Source: "proposal", ProfileID: "student"})
	if err != nil {
		t.Fatal(err)
	}
	if record.Scope != "global" || record.ProfileID != "student" {
		t.Fatalf("decision should be global while preserving source profile: %+v", record)
	}
	bundle, err := mgr.SelectForPrompt(ctx, "session_scope", "", "senior")
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Long) != 1 || bundle.Long[0].Content != "Use PostgreSQL" {
		t.Fatalf("global decision hidden from other profile: %+v", bundle.Long)
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
