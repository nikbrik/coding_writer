package process

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/storage"
)

const trustedEvidenceV2Prefix = "app:evidence:v2:"

type TrustedEvidenceRecord struct {
	ID        string    `json:"id"`
	TaskID    string    `json:"task_id"`
	SessionID string    `json:"session_id"`
	Source    string    `json:"source"`
	ExitCode  int       `json:"exit_code"`
	SHA256    string    `json:"sha256"`
	CreatedAt time.Time `json:"created_at"`
}

type TrustedEvidenceStore struct {
	StorageDir string
}

func NewTrustedEvidenceStore(storageDir string) *TrustedEvidenceStore {
	return &TrustedEvidenceStore{StorageDir: storageDir}
}

func (s *TrustedEvidenceStore) Issue(taskID, sessionID, source string, exitCode int, output string) (string, TrustedEvidenceRecord, error) {
	if s == nil || strings.TrimSpace(s.StorageDir) == "" {
		return "", TrustedEvidenceRecord{}, app.NewError(app.CategoryInternal, "missing_evidence_store", "trusted evidence store is required", nil)
	}
	if err := storage.ValidateID(taskID); err != nil {
		return "", TrustedEvidenceRecord{}, app.NewError(app.CategoryValidation, "unsafe_task_id", "unsafe task id", err)
	}
	if err := storage.ValidateID(sessionID); err != nil {
		return "", TrustedEvidenceRecord{}, app.NewError(app.CategoryValidation, "unsafe_session_id", "unsafe session id", err)
	}
	source = sanitizeEvidenceSource(source)
	digest := sha256.Sum256([]byte(output))
	record := TrustedEvidenceRecord{
		ID:        app.NewID("evidence"),
		TaskID:    taskID,
		SessionID: sessionID,
		Source:    source,
		ExitCode:  exitCode,
		SHA256:    hex.EncodeToString(digest[:]),
		CreatedAt: time.Now().UTC(),
	}
	path, err := s.path(record.ID)
	if err != nil {
		return "", TrustedEvidenceRecord{}, err
	}
	if err := storage.AtomicWriteJSON(path, record); err != nil {
		return "", TrustedEvidenceRecord{}, app.NewError(app.CategoryStorage, "evidence_write", "failed to persist trusted evidence", err)
	}
	return trustedEvidenceV2Prefix + record.ID, record, nil
}

func (s *TrustedEvidenceStore) Validate(taskID, sessionID string, evidence []string) ([]TrustedEvidenceRecord, error) {
	if s == nil || strings.TrimSpace(s.StorageDir) == "" {
		return nil, app.NewError(app.CategoryInternal, "missing_evidence_store", "trusted evidence store is required", nil)
	}
	out := []TrustedEvidenceRecord{}
	for _, token := range evidence {
		id, ok := trustedEvidenceID(token)
		if !ok {
			continue
		}
		rec, err := s.read(id)
		if err != nil {
			return nil, err
		}
		if rec.TaskID != taskID || rec.SessionID != sessionID || rec.ExitCode != 0 || strings.TrimSpace(rec.SHA256) == "" {
			return nil, app.NewError(app.CategoryValidation, "invalid_trusted_evidence", "trusted evidence is not bound to this task/session", nil)
		}
		out = append(out, rec)
	}
	return out, nil
}

func (s *TrustedEvidenceStore) path(id string) (string, error) {
	if err := storage.ValidateID(id); err != nil {
		return "", app.NewError(app.CategoryValidation, "unsafe_evidence_id", "unsafe evidence id", err)
	}
	path, err := storage.SafeJoin(s.StorageDir, "trusted_evidence", id+".json")
	if err != nil {
		return "", app.NewError(app.CategoryValidation, "unsafe_evidence_path", "unsafe trusted evidence path", err)
	}
	return path, nil
}

func (s *TrustedEvidenceStore) read(id string) (TrustedEvidenceRecord, error) {
	path, err := s.path(id)
	if err != nil {
		return TrustedEvidenceRecord{}, err
	}
	var rec TrustedEvidenceRecord
	if err := storage.ReadJSON(path, &rec); err != nil {
		return TrustedEvidenceRecord{}, app.NewError(app.CategoryValidation, "invalid_trusted_evidence", "trusted evidence record is missing or unreadable", err)
	}
	return rec, nil
}

func trustedEvidenceID(token string) (string, bool) {
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, trustedEvidenceV2Prefix) {
		return "", false
	}
	id := strings.TrimSpace(strings.TrimPrefix(token, trustedEvidenceV2Prefix))
	return id, id != ""
}

func sanitizeEvidenceSource(source string) string {
	source = strings.NewReplacer(";", "_", "=", "_", "\n", "_", "\r", "_").Replace(strings.TrimSpace(source))
	if source == "" {
		return "tool"
	}
	return source
}
