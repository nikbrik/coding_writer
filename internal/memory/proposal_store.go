package memory

import (
	"context"
	"strings"
	"time"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/storage"
	"github.com/nikbrik/coding_writer/internal/validation"
)

type ProposalStore struct {
	StorageDir string
	Memory     *Manager
}

type ApplyOptions struct {
	ProposalID string
	AcceptAll  bool
	AcceptIDs  map[string]bool
	RejectAll  bool
	RejectIDs  map[string]bool
	Edits      map[string]ProposalEdit
	SessionID  string
	TaskID     string
	ProfileID  string
	Scope      string

	WorkBlockedCode    string
	WorkBlockedMessage string
}

type ProposalEdit struct {
	Layer   app.ProposedMemoryLayer
	Content string
}

type ApplyResult struct {
	Proposal     app.MemoryProposal `json:"proposal"`
	SavedRecords []app.MemoryRecord `json:"saved_records"`
}

type pendingMemorySave struct {
	ProposalID     string
	RecordID       string
	Layer          app.MemoryLayer
	Status         app.ProposalRecordStatus
	AppliedLayer   app.ProposedMemoryLayer
	Kind           string
	Content        string
	AppliedContent string
	Scope          string
	ProfileID      string
	UserID         string
}

func NewProposalStore(storageDir string, manager *Manager) *ProposalStore {
	return &ProposalStore{StorageDir: storageDir, Memory: manager}
}

func (s *ProposalStore) Save(ctx context.Context, proposal app.MemoryProposal) error {
	proposal = sanitizeProposal(proposal)
	path, err := proposalPath(s.StorageDir, proposal.SessionID)
	if err != nil {
		return err
	}
	if err := storage.AppendJSONL(path, proposal); err != nil {
		return app.NewError(app.CategoryStorage, "proposal_write", err.Error(), err)
	}
	return nil
}

func (s *ProposalStore) List(ctx context.Context, sessionID string) ([]app.MemoryProposal, error) {
	if sessionID == "" {
		var err error
		sessionID, err = LatestSessionID(s.StorageDir)
		if err != nil {
			return nil, err
		}
	}
	path, err := proposalPath(s.StorageDir, sessionID)
	if err != nil {
		return nil, err
	}
	return storage.ReadJSONL[app.MemoryProposal](path)
}

func (s *ProposalStore) Latest(ctx context.Context, sessionID string) (app.MemoryProposal, error) {
	proposals, err := s.List(ctx, sessionID)
	if err != nil {
		return app.MemoryProposal{}, err
	}
	if len(proposals) == 0 {
		return app.MemoryProposal{}, app.NewError(app.CategoryValidation, "missing_proposal", "no memory proposal exists", nil)
	}
	return proposals[len(proposals)-1], nil
}

func (s *ProposalStore) Apply(ctx context.Context, opts ApplyOptions) (ApplyResult, error) {
	if !hasApplyAction(opts) {
		return ApplyResult{}, app.ErrorWithHint(app.CategoryCLI, "missing_apply_action", "memory apply requires --accept, --reject, or --edit", "example: assistant memory apply --proposal latest --accept all --json", nil)
	}
	if opts.SessionID == "" {
		var err error
		opts.SessionID, err = LatestSessionID(s.StorageDir)
		if err != nil {
			return ApplyResult{}, err
		}
	}
	path, err := proposalPath(s.StorageDir, opts.SessionID)
	if err != nil {
		return ApplyResult{}, err
	}
	var result ApplyResult
	if err := storage.WithFileLock(path+".apply", true, func() error {
		var applyErr error
		result, applyErr = s.applyLocked(ctx, opts, path)
		return applyErr
	}); err != nil {
		return ApplyResult{}, err
	}
	return result, nil
}

func (s *ProposalStore) applyLocked(ctx context.Context, opts ApplyOptions, path string) (ApplyResult, error) {
	var pending []pendingMemorySave
	var appliedProposal app.MemoryProposal
	if err := storage.UpdateJSONL(path, func(proposals []app.MemoryProposal) ([]app.MemoryProposal, error) {
		idx := -1
		for i := range proposals {
			if proposals[i].ID == opts.ProposalID || opts.ProposalID == "latest" && i == len(proposals)-1 {
				idx = i
			}
		}
		if idx < 0 {
			return proposals, app.NewError(app.CategoryValidation, "missing_proposal", "memory proposal not found", nil)
		}
		proposal := proposals[idx]
		if err := preflightApply(proposal, opts); err != nil {
			return proposals, err
		}
		for i := range proposal.Records {
			record := &proposal.Records[i]
			if record.Status == app.ProposalAccepted || record.Status == app.ProposalEdited || record.Status == app.ProposalRejected || record.Status == app.ProposalBlocked {
				continue
			}
			accepted := opts.AcceptAll || opts.AcceptIDs != nil && opts.AcceptIDs[record.ID]
			if opts.RejectAll || opts.RejectIDs != nil && opts.RejectIDs[record.ID] {
				record.Status = app.ProposalRejected
				continue
			}
			appliedLayer := record.Layer
			appliedContent := record.Content
			status := app.ProposalAccepted
			if edit, ok := opts.Edits[record.ID]; ok {
				if edit.Layer != "" {
					appliedLayer = edit.Layer
				}
				if strings.TrimSpace(edit.Content) != "" {
					appliedContent = strings.TrimSpace(edit.Content)
				}
				status = app.ProposalEdited
			} else if !accepted {
				continue
			}
			now := time.Now().UTC()
			if findings := validation.DetectSecrets(appliedContent); len(findings) > 0 {
				record.AppliedLayer = appliedLayer
				record.AppliedContent = "[REDACTED_SECRET]"
				record.Status = app.ProposalBlocked
				record.BlockReason = "secret detected: " + validation.FindingTypes(findings)
				record.AppliedAt = &now
				continue
			}
			if appliedLayer == app.ProposedLayerIgnore {
				record.Status = status
				record.AppliedLayer = appliedLayer
				record.AppliedContent = appliedContent
				record.AppliedAt = &now
				continue
			}
			layer, err := physicalLayer(appliedLayer)
			if err != nil {
				return proposals, err
			}
			scope := record.Scope
			profileID := record.ProfileID
			if layer == app.LayerLong {
				if opts.Scope != "" {
					scope = opts.Scope
				}
				if profileID == "" && record.Layer != app.ProposedLayerLong {
					profileID = opts.ProfileID
				}
				if scope == "" {
					scope = defaultLongScope(record.Kind, profileID)
				}
				record.Scope = scope
				record.ProfileID = profileID
			}
			pending = append(pending, pendingMemorySave{ProposalID: proposal.ID, RecordID: record.ID, Layer: layer, Status: status, AppliedLayer: appliedLayer, Kind: record.Kind, Content: appliedContent, AppliedContent: appliedContent, Scope: scope, ProfileID: profileID, UserID: record.UserID})
		}
		proposals[idx] = proposal
		appliedProposal = proposal
		return proposals, nil
	}); err != nil {
		appErr := app.AsError(err)
		if appErr.Category != app.CategoryInternal && appErr.Category != app.CategoryStorage {
			return ApplyResult{}, err
		}
		return ApplyResult{}, app.NewError(app.CategoryStorage, "proposal_update", err.Error(), err)
	}
	saved := make([]app.MemoryRecord, 0, len(pending))
	savedByRecord := map[string]string{}
	pendingByRecord := map[string]pendingMemorySave{}
	for _, item := range pending {
		pendingByRecord[item.RecordID] = item
		if existing, ok, err := s.Memory.FindByProposalRecord(ctx, item.ProposalID, item.RecordID, opts.SessionID, opts.TaskID); err != nil {
			return ApplyResult{}, err
		} else if ok {
			savedByRecord[item.RecordID] = existing.ID
			continue
		}
		savedRecord, err := s.Memory.Save(ctx, SaveInput{Layer: item.Layer, Kind: item.Kind, Content: item.Content, Source: "proposal", Scope: item.Scope, ProfileID: item.ProfileID, UserID: item.UserID, SessionID: opts.SessionID, TaskID: opts.TaskID, ProposalID: item.ProposalID, ProposalRecordID: item.RecordID})
		if err != nil {
			return ApplyResult{}, err
		}
		savedByRecord[item.RecordID] = savedRecord.ID
		saved = append(saved, savedRecord)
	}
	if len(savedByRecord) > 0 {
		if err := storage.UpdateJSONL(path, func(proposals []app.MemoryProposal) ([]app.MemoryProposal, error) {
			idx := -1
			for i := range proposals {
				if proposals[i].ID == appliedProposal.ID {
					idx = i
				}
			}
			if idx < 0 {
				return proposals, app.NewError(app.CategoryValidation, "missing_proposal", "memory proposal not found", nil)
			}
			proposal := proposals[idx]
			now := time.Now().UTC()
			for i := range proposal.Records {
				id := savedByRecord[proposal.Records[i].ID]
				if id == "" {
					continue
				}
				pending := pendingByRecord[proposal.Records[i].ID]
				proposal.Records[i].Status = pending.Status
				proposal.Records[i].AppliedLayer = pending.AppliedLayer
				proposal.Records[i].AppliedContent = pending.AppliedContent
				proposal.Records[i].AppliedAt = &now
				proposal.Records[i].Scope = pending.Scope
				proposal.Records[i].ProfileID = pending.ProfileID
				if proposal.Records[i].SavedRecordID == "" {
					proposal.Records[i].SavedRecordID = id
				}
			}
			proposals[idx] = proposal
			appliedProposal = proposal
			return proposals, nil
		}); err != nil {
			return ApplyResult{}, app.NewError(app.CategoryStorage, "proposal_reconcile", err.Error(), err)
		}
	}
	return ApplyResult{Proposal: appliedProposal, SavedRecords: saved}, nil
}

func hasApplyAction(opts ApplyOptions) bool {
	return opts.AcceptAll || opts.RejectAll || len(opts.AcceptIDs) > 0 || len(opts.RejectIDs) > 0 || len(opts.Edits) > 0
}

func physicalLayer(layer app.ProposedMemoryLayer) (app.MemoryLayer, error) {
	switch layer {
	case app.ProposedLayerShort:
		return app.LayerShort, nil
	case app.ProposedLayerWork:
		return app.LayerWork, nil
	case app.ProposedLayerLong:
		return app.LayerLong, nil
	default:
		return "", app.NewError(app.CategoryValidation, "invalid_memory_layer", "proposal layer cannot be saved physically", nil)
	}
}

func preflightApply(proposal app.MemoryProposal, opts ApplyOptions) error {
	for _, record := range proposal.Records {
		if record.Status == app.ProposalAccepted || record.Status == app.ProposalEdited || record.Status == app.ProposalRejected || record.Status == app.ProposalBlocked {
			continue
		}
		accepted := opts.AcceptAll || opts.AcceptIDs != nil && opts.AcceptIDs[record.ID]
		if opts.RejectAll || opts.RejectIDs != nil && opts.RejectIDs[record.ID] {
			continue
		}
		appliedLayer := record.Layer
		appliedContent := record.Content
		if edit, ok := opts.Edits[record.ID]; ok {
			if edit.Layer != "" {
				appliedLayer = edit.Layer
			}
			if strings.TrimSpace(edit.Content) != "" {
				appliedContent = strings.TrimSpace(edit.Content)
			}
		} else if !accepted {
			continue
		}
		if validation.HasSecret(appliedContent) || appliedLayer == app.ProposedLayerIgnore {
			continue
		}
		layer, err := physicalLayer(appliedLayer)
		if err != nil {
			return err
		}
		if layer == app.LayerWork {
			if opts.WorkBlockedCode != "" {
				message := opts.WorkBlockedMessage
				if message == "" {
					message = "work memory mutation is not allowed"
				}
				return app.NewError(app.CategoryValidation, opts.WorkBlockedCode, message, nil)
			}
			if opts.TaskID == "" {
				return app.NewError(app.CategoryValidation, "missing_current_task", "work memory requires active task", nil)
			}
		}
		if layer == app.LayerShort && opts.SessionID == "" {
			return app.NewError(app.CategoryValidation, "missing_session", "short memory requires session id", nil)
		}
		if layer == app.LayerLong && record.Layer == app.ProposedLayerLong && record.ProfileID == "" && profileScopedLong(record.Kind, record.Scope, opts.Scope) {
			return app.ErrorWithHint(app.CategoryValidation, "missing_proposal_profile", "long profile memory proposal has no generation profile", "regenerate the memory proposal so profile ownership is explicit", nil)
		}
	}
	return nil
}

func profileScopedLong(kind, recordScope, overrideScope string) bool {
	scope := strings.TrimSpace(overrideScope)
	if scope == "" {
		scope = strings.TrimSpace(recordScope)
	}
	return scope == "profile" || strings.EqualFold(strings.TrimSpace(kind), "preference")
}

func sanitizeProposal(proposal app.MemoryProposal) app.MemoryProposal {
	for i := range proposal.Records {
		if findings := validation.DetectSecrets(proposal.Records[i].Content + "\n" + proposal.Records[i].Reason); len(findings) > 0 {
			proposal.Records[i].Content = "[REDACTED_SECRET]"
			proposal.Records[i].Reason = "[REDACTED_SECRET]"
			proposal.Records[i].Status = app.ProposalBlocked
			proposal.Records[i].BlockReason = "secret detected: " + validation.FindingTypes(findings)
		}
	}
	return proposal
}
