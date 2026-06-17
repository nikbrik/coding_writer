package memory

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/storage"
	"github.com/nikbrik/coding_writer/internal/validation"
)

type Manager struct {
	StorageDir string
}

type SaveInput struct {
	Layer            app.MemoryLayer
	Kind             string
	Content          string
	Source           string
	Scope            string
	ProfileID        string
	UserID           string
	SessionID        string
	TaskID           string
	ProposalID       string
	ProposalRecordID string
	Tags             []string
}

func NewManager(storageDir string) *Manager { return &Manager{StorageDir: storageDir} }

func (m *Manager) Save(ctx context.Context, input SaveInput) (app.MemoryRecord, error) {
	if validation.HasSecret(input.Content) {
		return app.MemoryRecord{}, app.NewError(app.CategoryValidation, "secret_blocked", "secret-like content cannot be saved", nil)
	}
	if input.Layer != app.LayerShort && input.Layer != app.LayerWork && input.Layer != app.LayerLong {
		return app.MemoryRecord{}, app.NewError(app.CategoryValidation, "invalid_memory_layer", "invalid physical memory layer", nil)
	}
	if strings.TrimSpace(input.Content) == "" {
		return app.MemoryRecord{}, app.NewError(app.CategoryValidation, "empty_memory", "memory content is empty", nil)
	}
	if input.Kind == "" {
		input.Kind = "other"
	}
	if input.Source == "" {
		input.Source = "user"
	}
	if input.Layer == app.LayerLong && input.Scope == "" {
		if input.ProfileID != "" {
			input.Scope = "profile"
		} else {
			input.Scope = "global"
		}
	}
	record := app.MemoryRecord{
		ID:               app.NewID("mem"),
		Layer:            input.Layer,
		Kind:             input.Kind,
		Content:          strings.TrimSpace(input.Content),
		Source:           input.Source,
		Scope:            input.Scope,
		ProfileID:        input.ProfileID,
		UserID:           input.UserID,
		Tags:             input.Tags,
		TaskID:           input.TaskID,
		SessionID:        input.SessionID,
		ProposalID:       input.ProposalID,
		ProposalRecordID: input.ProposalRecordID,
		CreatedAt:        time.Now().UTC(),
	}
	var path string
	var err error
	switch input.Layer {
	case app.LayerShort:
		if input.SessionID == "" {
			return record, app.NewError(app.CategoryValidation, "missing_session", "short memory requires session id", nil)
		}
		path, err = shortPath(m.StorageDir, input.SessionID)
	case app.LayerWork:
		if input.TaskID == "" {
			return record, app.NewError(app.CategoryValidation, "missing_current_task", "work memory requires active task", nil)
		}
		path, err = workPath(m.StorageDir, input.TaskID)
	case app.LayerLong:
		path = longPath(m.StorageDir, input.Kind)
	}
	if err != nil {
		return record, err
	}
	if err := storage.AppendJSONL(path, record); err != nil {
		return record, app.NewError(app.CategoryStorage, "memory_write", err.Error(), err)
	}
	return record, nil
}

func (m *Manager) List(ctx context.Context, layer app.MemoryLayer, sessionID, taskID string) ([]app.MemoryRecord, error) {
	switch layer {
	case app.LayerShort:
		if sessionID == "" {
			var err error
			sessionID, err = LatestSessionID(m.StorageDir)
			if err != nil {
				return nil, err
			}
		}
		path, err := shortPath(m.StorageDir, sessionID)
		if err != nil {
			return nil, err
		}
		return storage.ReadJSONL[app.MemoryRecord](path)
	case app.LayerWork:
		if taskID == "" {
			return nil, app.NewError(app.CategoryValidation, "missing_current_task", "work memory requires active task", nil)
		}
		path, err := workPath(m.StorageDir, taskID)
		if err != nil {
			return nil, err
		}
		return storage.ReadJSONL[app.MemoryRecord](path)
	case app.LayerLong:
		var all []app.MemoryRecord
		for _, kind := range []string{"preference", "decision", "constraint", "knowledge"} {
			records, err := storage.ReadJSONL[app.MemoryRecord](longPath(m.StorageDir, kind))
			if err != nil {
				return nil, err
			}
			all = append(all, records...)
		}
		return all, nil
	default:
		return nil, app.NewError(app.CategoryValidation, "invalid_memory_layer", "invalid physical memory layer", nil)
	}
}

func (m *Manager) ClearShort(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		var err error
		sessionID, err = LatestSessionID(m.StorageDir)
		if err != nil {
			return err
		}
	}
	path, err := shortPath(m.StorageDir, sessionID)
	if err != nil {
		return err
	}
	if err := storage.TruncateJSONL(path); err != nil {
		return app.NewError(app.CategoryStorage, "short_clear", err.Error(), err)
	}
	return nil
}

func (m *Manager) SelectForPrompt(ctx context.Context, sessionID, taskID, profileID string) (app.MemoryBundle, error) {
	var bundle app.MemoryBundle
	if sessionID != "" {
		short, err := m.List(ctx, app.LayerShort, sessionID, "")
		if err != nil {
			return bundle, err
		}
		bundle.Short = latest(short, 12)
	}
	if taskID != "" {
		work, err := m.List(ctx, app.LayerWork, "", taskID)
		if err != nil {
			return bundle, err
		}
		bundle.Work = latest(work, 20)
	}
	long, err := m.List(ctx, app.LayerLong, "", "")
	if err != nil {
		return bundle, err
	}
	bundle.Long = latest(filterLongForProfile(long, profileID), 20)
	return bundle, nil
}

func latest(records []app.MemoryRecord, n int) []app.MemoryRecord {
	sort.SliceStable(records, func(i, j int) bool { return records[i].CreatedAt.Before(records[j].CreatedAt) })
	if len(records) <= n {
		return records
	}
	return records[len(records)-n:]
}

func filterLongForProfile(records []app.MemoryRecord, profileID string) []app.MemoryRecord {
	out := make([]app.MemoryRecord, 0, len(records))
	for _, record := range records {
		if record.Layer != app.LayerLong {
			out = append(out, record)
			continue
		}
		scope := record.Scope
		if scope == "" {
			scope = "global"
		}
		if scope == "global" || record.ProfileID == "" && scope != "profile" || record.ProfileID == profileID {
			out = append(out, record)
		}
	}
	return out
}

func (m *Manager) LatestExchange(ctx context.Context, sessionID string) (app.MemoryRecord, app.MemoryRecord, error) {
	records, err := m.List(ctx, app.LayerShort, sessionID, "")
	if err != nil {
		return app.MemoryRecord{}, app.MemoryRecord{}, err
	}
	var user, assistant app.MemoryRecord
	for i := len(records) - 1; i >= 0; i-- {
		if assistant.ID == "" && records[i].Kind == "message_assistant" {
			assistant = records[i]
			continue
		}
		if assistant.ID != "" && records[i].Kind == "message_user" {
			user = records[i]
			break
		}
	}
	if user.ID == "" || assistant.ID == "" {
		return user, assistant, app.NewError(app.CategoryValidation, "missing_latest_exchange", "latest user/assistant exchange not found", nil)
	}
	return user, assistant, nil
}

func (m *Manager) FindByProposalRecord(ctx context.Context, proposalID, proposalRecordID, sessionID, taskID string) (app.MemoryRecord, bool, error) {
	if proposalID == "" || proposalRecordID == "" {
		return app.MemoryRecord{}, false, nil
	}
	candidates := [][]app.MemoryRecord{}
	if sessionID != "" {
		if records, err := m.List(ctx, app.LayerShort, sessionID, ""); err == nil {
			candidates = append(candidates, records)
		} else {
			return app.MemoryRecord{}, false, err
		}
	}
	if taskID != "" {
		if records, err := m.List(ctx, app.LayerWork, "", taskID); err == nil {
			candidates = append(candidates, records)
		} else {
			return app.MemoryRecord{}, false, err
		}
	}
	if records, err := m.List(ctx, app.LayerLong, "", ""); err == nil {
		candidates = append(candidates, records)
	} else {
		return app.MemoryRecord{}, false, err
	}
	for _, records := range candidates {
		for _, record := range records {
			if record.ProposalID == proposalID && record.ProposalRecordID == proposalRecordID {
				return record, true, nil
			}
		}
	}
	return app.MemoryRecord{}, false, nil
}
