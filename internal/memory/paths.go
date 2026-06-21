package memory

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/storage"
)

type SessionSummary struct {
	ID           string
	LastActivity time.Time
}

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

func touchSessionActivity(root, sessionID string) error {
	dir, err := sessionDir(root, sessionID)
	if err != nil {
		return err
	}
	return storage.TouchFile(filepath.Join(dir, ".last_activity"), storage.FileMode)
}

func TouchSessionActivity(root, sessionID string) error {
	return touchSessionActivity(root, sessionID)
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

var LongTermKinds = []string{"preference", "decision", "constraint", "knowledge"}

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
	sessions, err := ListSessions(root)
	if err != nil {
		return "", err
	}
	if len(sessions) == 0 {
		return "", app.NewError(app.CategoryValidation, "missing_session", "no session exists", nil)
	}
	return sessions[0].ID, nil
}

func ListSessions(root string) ([]SessionSummary, error) {
	dir, safeErr := storage.SafeJoin(root, "sessions")
	if safeErr != nil {
		return nil, app.NewError(app.CategoryValidation, "unsafe_session_path", "unsafe session path", safeErr)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, app.NewError(app.CategoryStorage, "sessions_list", err.Error(), err)
	}
	var sessions []SessionSummary
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if err := storage.ValidateID(entry.Name()); err != nil {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		lastActivity := info.ModTime()
		activityPath := filepath.Join(dir, entry.Name(), ".last_activity")
		if activityInfo, err := os.Stat(activityPath); err == nil {
			lastActivity = activityInfo.ModTime()
		}
		sessions = append(sessions, SessionSummary{ID: entry.Name(), LastActivity: lastActivity.UTC()})
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].LastActivity.After(sessions[j].LastActivity) })
	return sessions, nil
}

func LookupSession(root, sessionID string) (SessionSummary, error) {
	if err := storage.ValidateID(sessionID); err != nil {
		return SessionSummary{}, app.NewError(app.CategoryValidation, "unsafe_session_id", "unsafe session id", err)
	}
	sessions, err := ListSessions(root)
	if err != nil {
		return SessionSummary{}, err
	}
	for _, session := range sessions {
		if session.ID == sessionID {
			return session, nil
		}
	}
	return SessionSummary{}, app.NewError(app.CategoryValidation, "unknown_session", "unknown session", nil)
}
