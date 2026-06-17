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
	RejectAll  bool
	RejectIDs  map[string]bool
	Edits      map[string]ProposalEdit
	SessionID  string
	TaskID     string
}

type ProposalEdit struct {
	Layer   app.ProposedMemoryLayer
	Content string
}

type ApplyResult struct {
	Proposal     app.MemoryProposal `json:"proposal"`
	SavedRecords []app.MemoryRecord `json:"saved_records"`
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
	var saved []app.MemoryRecord
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
			if opts.RejectAll || opts.RejectIDs != nil && opts.RejectIDs[record.ID] {
				record.Status = app.ProposalRejected
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
				record.Status = app.ProposalEdited
			} else if opts.AcceptAll {
				record.Status = app.ProposalAccepted
			} else {
				continue
			}
			now := time.Now().UTC()
			record.AppliedLayer = appliedLayer
			record.AppliedContent = appliedContent
			record.AppliedAt = &now
			if findings := validation.DetectSecrets(appliedContent); len(findings) > 0 {
				record.AppliedContent = "[REDACTED_SECRET]"
				record.Status = app.ProposalBlocked
				record.BlockReason = "secret detected: " + validation.FindingTypes(findings)
				continue
			}
			if appliedLayer == app.ProposedLayerIgnore || record.Status == app.ProposalBlocked || record.Status == app.ProposalRejected {
				continue
			}
			layer, err := physicalLayer(appliedLayer)
			if err != nil {
				return proposals, err
			}
			if existing, ok, err := s.Memory.FindByProposalRecord(ctx, proposal.ID, record.ID, opts.SessionID, opts.TaskID); err != nil {
				return proposals, err
			} else if ok {
				record.SavedRecordID = existing.ID
				continue
			}
			savedRecord, err := s.Memory.Save(ctx, SaveInput{Layer: layer, Kind: record.Kind, Content: appliedContent, Source: "proposal", SessionID: opts.SessionID, TaskID: opts.TaskID, ProposalID: proposal.ID, ProposalRecordID: record.ID})
			if err != nil {
				return proposals, err
			}
			record.SavedRecordID = savedRecord.ID
			saved = append(saved, savedRecord)
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
	return ApplyResult{Proposal: appliedProposal, SavedRecords: saved}, nil
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
		} else if !opts.AcceptAll {
			continue
		}
		if validation.HasSecret(appliedContent) || appliedLayer == app.ProposedLayerIgnore {
			continue
		}
		layer, err := physicalLayer(appliedLayer)
		if err != nil {
			return err
		}
		if layer == app.LayerWork && opts.TaskID == "" {
			return app.NewError(app.CategoryValidation, "missing_current_task", "work memory requires active task", nil)
		}
		if layer == app.LayerShort && opts.SessionID == "" {
			return app.NewError(app.CategoryValidation, "missing_session", "short memory requires session id", nil)
		}
	}
	return nil
}

func sanitizeProposal(proposal app.MemoryProposal) app.MemoryProposal {
	for i := range proposal.Records {
		if findings := validation.DetectSecrets(proposal.Records[i].Content); len(findings) > 0 {
			proposal.Records[i].Content = "[REDACTED_SECRET]"
			proposal.Records[i].Status = app.ProposalBlocked
			proposal.Records[i].BlockReason = "secret detected: " + validation.FindingTypes(findings)
		}
	}
	return proposal
}
