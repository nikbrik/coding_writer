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
	path, err := storage.SafeJoin(root, "sessions", sessionID)
	if err != nil {
		return "", app.NewError(app.CategoryValidation, "unsafe_session_path", "unsafe session path", err)
	}
	return path, nil
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
	path, err := storage.SafeJoin(root, "tasks", taskID, "work_memory.jsonl")
	if err != nil {
		return "", app.NewError(app.CategoryValidation, "unsafe_task_path", "unsafe task path", err)
	}
	return path, nil
}

func longPath(root, kind string) (string, error) {
	file := "knowledge.jsonl"
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case "preference", "preferences":
		file = "preferences.jsonl"
	case "decision", "decisions":
		file = "decisions.jsonl"
	case "constraint", "constraints":
		file = "constraints.jsonl"
	}
	path, err := storage.SafeJoin(root, "long_term", file)
	if err != nil {
		return "", app.NewError(app.CategoryValidation, "unsafe_long_path", "unsafe long-term memory path", err)
	}
	return path, nil
}

func LatestSessionID(root string) (string, error) {
	dir, safeErr := storage.SafeJoin(root, "sessions")
	if safeErr != nil {
		return "", app.NewError(app.CategoryValidation, "unsafe_session_path", "unsafe session path", safeErr)
	}
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
		mtime := info.ModTime().UnixNano()
		activityPath := filepath.Join(dir, entry.Name(), ".last_activity")
		if activityInfo, err := os.Stat(activityPath); err == nil {
			mtime = activityInfo.ModTime().UnixNano()
		}
		candidates = append(candidates, candidate{id: entry.Name(), mtime: mtime})
	}
	if len(candidates) == 0 {
		return "", app.NewError(app.CategoryValidation, "missing_session", "no session exists", nil)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].mtime > candidates[j].mtime })
	return candidates[0].id, nil
}
