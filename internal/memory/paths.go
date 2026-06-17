package memory

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/storage"
)

func sessionDir(root, sessionID string) (string, error) {
	if err := storage.ValidateID(sessionID); err != nil {
		return "", app.NewError(app.CategoryValidation, "unsafe_session_id", "unsafe session id", err)
	}
	return filepath.Join(root, "sessions", sessionID), nil
}

func shortPath(root, sessionID string) (string, error) {
	dir, err := sessionDir(root, sessionID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "short_term.jsonl"), nil
}

func proposalPath(root, sessionID string) (string, error) {
	dir, err := sessionDir(root, sessionID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "memory_proposals.jsonl"), nil
}

func workPath(root, taskID string) (string, error) {
	if err := storage.ValidateID(taskID); err != nil {
		return "", app.NewError(app.CategoryValidation, "unsafe_task_id", "unsafe task id", err)
	}
	return filepath.Join(root, "tasks", taskID, "work_memory.jsonl"), nil
}

func longPath(root, kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case "preference", "preferences":
		return filepath.Join(root, "long_term", "preferences.jsonl")
	case "decision", "decisions":
		return filepath.Join(root, "long_term", "decisions.jsonl")
	case "constraint", "constraints":
		return filepath.Join(root, "long_term", "constraints.jsonl")
	default:
		return filepath.Join(root, "long_term", "knowledge.jsonl")
	}
}

func LatestSessionID(root string) (string, error) {
	dir := filepath.Join(root, "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", app.NewError(app.CategoryValidation, "missing_session", "no session exists", err)
		}
		return "", app.NewError(app.CategoryStorage, "sessions_list", err.Error(), err)
	}
	type candidate struct {
		id    string
		mtime int64
	}
	var candidates []candidate
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		candidates = append(candidates, candidate{id: entry.Name(), mtime: info.ModTime().UnixNano()})
	}
	if len(candidates) == 0 {
		return "", app.NewError(app.CategoryValidation, "missing_session", "no session exists", nil)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].mtime > candidates[j].mtime })
	return candidates[0].id, nil
}
