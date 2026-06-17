package process

import (
	"time"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/storage"
)

// AuditStore persists process audit events.
type AuditStore struct {
	StorageDir string
}

func NewAuditStore(storageDir string) *AuditStore {
	return &AuditStore{StorageDir: storageDir}
}

// Save persists an event to <storage_root>/process_audit.jsonl.
func (s *AuditStore) Save(event ProcessAuditEvent) error {
	if s == nil {
		return app.NewError(app.CategoryInternal, "missing_audit_store", "process audit store is required", nil)
	}
	if s.StorageDir == "" {
		return app.NewError(app.CategoryInternal, "missing_audit_storage", "process audit storage dir is required", nil)
	}
	if event.ID == "" {
		event.ID = app.NewID("audit")
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	path, err := storage.SafeJoin(s.StorageDir, "process_audit.jsonl")
	if err != nil {
		return app.NewError(app.CategoryValidation, "unsafe_audit_path", "unsafe process audit path", err)
	}
	return storage.AppendJSONL(path, event)
}

// Latest returns the most recent events, newest last.
func (s *AuditStore) Latest(limit int) ([]ProcessAuditEvent, error) {
	if s == nil {
		return nil, app.NewError(app.CategoryInternal, "missing_audit_store", "process audit store is required", nil)
	}
	if s.StorageDir == "" {
		return nil, app.NewError(app.CategoryInternal, "missing_audit_storage", "process audit storage dir is required", nil)
	}
	path, err := storage.SafeJoin(s.StorageDir, "process_audit.jsonl")
	if err != nil {
		return nil, app.NewError(app.CategoryValidation, "unsafe_audit_path", "unsafe process audit path", err)
	}
	events, err := storage.ReadJSONL[ProcessAuditEvent](path)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit >= len(events) {
		return events, nil
	}
	return append([]ProcessAuditEvent(nil), events[len(events)-limit:]...), nil
}
