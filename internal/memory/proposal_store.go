package memory

import (
	"context"
	"strings"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/storage"
)

type ProposalStore struct {
	StorageDir string
	Memory     *Manager
}

type ApplyOptions struct {
	ProposalID string
	AcceptAll  bool
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
	proposals, err := storage.ReadJSONL[app.MemoryProposal](path)
	if err != nil {
		return ApplyResult{}, app.NewError(app.CategoryStorage, "proposal_read", err.Error(), err)
	}
	idx := -1
	for i := range proposals {
		if proposals[i].ID == opts.ProposalID || opts.ProposalID == "latest" && i == len(proposals)-1 {
			idx = i
		}
	}
	if idx < 0 {
		return ApplyResult{}, app.NewError(app.CategoryValidation, "missing_proposal", "memory proposal not found", nil)
	}
	proposal := proposals[idx]
	var saved []app.MemoryRecord
	for i := range proposal.Records {
		record := &proposal.Records[i]
		if record.Status == app.ProposalAccepted || record.Status == app.ProposalEdited || record.Status == app.ProposalRejected || record.Status == app.ProposalBlocked {
			continue
		}
		if opts.RejectIDs != nil && opts.RejectIDs[record.ID] {
			record.Status = app.ProposalRejected
			continue
		}
		if edit, ok := opts.Edits[record.ID]; ok {
			if edit.Layer != "" {
				record.Layer = edit.Layer
			}
			if strings.TrimSpace(edit.Content) != "" {
				record.Content = strings.TrimSpace(edit.Content)
			}
			record.Status = app.ProposalEdited
		} else if opts.AcceptAll {
			record.Status = app.ProposalAccepted
		} else {
			continue
		}
		if record.Layer == app.ProposedLayerIgnore || record.Status == app.ProposalBlocked || record.Status == app.ProposalRejected {
			continue
		}
		layer, err := physicalLayer(record.Layer)
		if err != nil {
			return ApplyResult{}, err
		}
		savedRecord, err := s.Memory.Save(ctx, SaveInput{Layer: layer, Kind: record.Kind, Content: record.Content, Source: "proposal", SessionID: opts.SessionID, TaskID: opts.TaskID, ProposalID: proposal.ID})
		if err != nil {
			return ApplyResult{}, err
		}
		saved = append(saved, savedRecord)
	}
	proposals[idx] = proposal
	if err := storage.RewriteJSONL(path, proposals); err != nil {
		return ApplyResult{}, app.NewError(app.CategoryStorage, "proposal_update", err.Error(), err)
	}
	return ApplyResult{Proposal: proposal, SavedRecords: saved}, nil
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
