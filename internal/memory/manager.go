package memory

import (
	"context"
	"os"
	"path/filepath"
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
		input.Scope = defaultLongScope(input.Kind, input.ProfileID)
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
		if err == nil {
			if dir, err := sessionDir(m.StorageDir, input.SessionID); err == nil {
				_ = os.WriteFile(filepath.Join(dir, ".last_activity"), []byte{}, 0o600)
			}
		}
	case app.LayerWork:
		if input.TaskID == "" {
			return record, app.NewError(app.CategoryValidation, "missing_current_task", "work memory requires active task", nil)
		}
		path, err = workPath(m.StorageDir, input.TaskID)
	case app.LayerLong:
		path, err = longPath(m.StorageDir, input.Kind)
	}
	if err != nil {
		return record, err
	}
	if err := storage.AppendJSONL(path, record); err != nil {
		return record, app.NewError(app.CategoryStorage, "memory_write", err.Error(), err)
	}
	return record, nil
}

func defaultLongScope(kind, profileID string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "preference":
		if profileID != "" {
			return "profile"
		}
	}
	return "global"
}

func (m *Manager) SaveShortExchange(ctx context.Context, sessionID, profileID, taskID, userContent, assistantContent string) (app.MemoryRecord, app.MemoryRecord, error) {
	if sessionID == "" {
		return app.MemoryRecord{}, app.MemoryRecord{}, app.NewError(app.CategoryValidation, "missing_session", "short memory requires session id", nil)
	}
	if validation.HasSecret(userContent) || validation.HasSecret(assistantContent) {
		return app.MemoryRecord{}, app.MemoryRecord{}, app.NewError(app.CategoryValidation, "secret_blocked", "secret-like content cannot be saved", nil)
	}
	if strings.TrimSpace(userContent) == "" || strings.TrimSpace(assistantContent) == "" {
		return app.MemoryRecord{}, app.MemoryRecord{}, app.NewError(app.CategoryValidation, "empty_memory", "memory content is empty", nil)
	}
	path, err := shortPath(m.StorageDir, sessionID)
	if err != nil {
		return app.MemoryRecord{}, app.MemoryRecord{}, err
	}
	if dir, err := sessionDir(m.StorageDir, sessionID); err == nil {
		_ = os.WriteFile(filepath.Join(dir, ".last_activity"), []byte{}, 0o600)
	}
	now := time.Now().UTC()
	userRecord := app.MemoryRecord{
		ID:        app.NewID("mem"),
		Layer:     app.LayerShort,
		Kind:      "message_user",
		Content:   strings.TrimSpace(userContent),
		Source:    "chat",
		ProfileID: profileID,
		TaskID:    taskID,
		SessionID: sessionID,
		CreatedAt: now,
	}
	assistantRecord := app.MemoryRecord{
		ID:        app.NewID("mem"),
		Layer:     app.LayerShort,
		Kind:      "message_assistant",
		Content:   strings.TrimSpace(assistantContent),
		Source:    "chat",
		ProfileID: profileID,
		TaskID:    taskID,
		SessionID: sessionID,
		CreatedAt: now,
	}
	if err := storage.UpdateJSONL[app.MemoryRecord](path, func(records []app.MemoryRecord) ([]app.MemoryRecord, error) {
		return append(records, userRecord, assistantRecord), nil
	}); err != nil {
		return app.MemoryRecord{}, app.MemoryRecord{}, app.NewError(app.CategoryStorage, "memory_write", err.Error(), err)
	}
	return userRecord, assistantRecord, nil
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
		for _, kind := range LongTermKinds {
			path, err := longPath(m.StorageDir, kind)
			if err != nil {
				return nil, err
			}
			records, err := storage.ReadJSONL[app.MemoryRecord](path)
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

const (
	shortMemoryMaxRecords = 12
	shortMemoryMaxBytes   = 6000
	workMemoryMaxRecords  = 20
	workMemoryMaxBytes    = 12000
	longMemoryMaxRecords  = 20
	longMemoryMaxBytes    = 12000
)

func (m *Manager) SelectForPrompt(ctx context.Context, sessionID, taskID, profileID string) (app.MemoryBundle, error) {
	var bundle app.MemoryBundle
	if sessionID != "" {
		short, err := m.List(ctx, app.LayerShort, sessionID, "")
		if err != nil {
			return bundle, err
		}
		bundle.Short = latestWithinBudget(short, shortMemoryMaxRecords, shortMemoryMaxBytes)
	}
	if taskID != "" {
		work, err := m.List(ctx, app.LayerWork, "", taskID)
		if err != nil {
			return bundle, err
		}
		bundle.Work = latestWithinBudget(work, workMemoryMaxRecords, workMemoryMaxBytes)
	}
	long, err := m.List(ctx, app.LayerLong, "", "")
	if err != nil {
		return bundle, err
	}
	bundle.Long = latestWithinBudget(filterLongForProfile(long, profileID), longMemoryMaxRecords, longMemoryMaxBytes)
	return bundle, nil
}

func latest(records []app.MemoryRecord, n int) []app.MemoryRecord {
	sort.SliceStable(records, func(i, j int) bool { return records[i].CreatedAt.Before(records[j].CreatedAt) })
	if len(records) <= n {
		return records
	}
	return records[len(records)-n:]
}

func latestWithinBudget(records []app.MemoryRecord, maxCount, maxBytes int) []app.MemoryRecord {
	sort.SliceStable(records, func(i, j int) bool { return records[i].CreatedAt.Before(records[j].CreatedAt) })
	if len(records) > maxCount {
		records = records[len(records)-maxCount:]
	}
	var total int
	start := len(records)
	for i := len(records) - 1; i >= 0; i-- {
		total += len(records[i].Content)
		if total > maxBytes {
			start = i + 1
			break
		}
		start = i
	}
	result := records[start:]
	if len(records) > 0 && start > 0 {
		first := records[0]
		resultBytes := 0
		for _, r := range result {
			resultBytes += len(r.Content)
		}
		found := false
		for _, r := range result {
			if r.ID == first.ID {
				found = true
				break
			}
		}
		if !found && first.Kind == "message_user" && resultBytes+len(first.Content) <= maxBytes {
			result = append([]app.MemoryRecord{first}, result...)
		}
	}
	return result
}

func filterLongForProfile(records []app.MemoryRecord, activeProfileID string) []app.MemoryRecord {
	out := make([]app.MemoryRecord, 0, len(records))
	for _, record := range records {
		scope := record.Scope
		if scope == "" {
			scope = "global"
		}
		switch scope {
		case "global":
			out = append(out, record)
		case "profile":
			if record.ProfileID != "" && record.ProfileID == activeProfileID {
				out = append(out, record)
			}
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
