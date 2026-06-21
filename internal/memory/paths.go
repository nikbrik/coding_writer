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
	Title        string
	Description  string
	StartedAt    time.Time
	LastActivity time.Time
}

type SessionMetadata struct {
	ID           string    `json:"id"`
	Title        string    `json:"title,omitempty"`
	Description  string    `json:"description,omitempty"`
	StartedAt    time.Time `json:"started_at"`
	LastActivity time.Time `json:"last_activity"`
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

func sessionMetadataPath(root, sessionID string) (string, error) {
	dir, err := sessionDir(root, sessionID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "session_meta.json"), nil
}

func touchSessionActivity(root, sessionID string) error {
	dir, err := sessionDir(root, sessionID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	_ = ensureSessionMetadata(root, sessionID, "", now)
	return storage.TouchFile(filepath.Join(dir, ".last_activity"), storage.FileMode)
}

func TouchSessionActivity(root, sessionID string) error {
	return touchSessionActivity(root, sessionID)
}

func UpdateSessionDescription(root, sessionID, userInput string) error {
	return ensureSessionMetadata(root, sessionID, userInput, time.Now().UTC())
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
		if !sessionHasContent(filepath.Join(dir, entry.Name())) {
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
		summary := SessionSummary{ID: entry.Name(), LastActivity: lastActivity.UTC()}
		if meta, ok := readSessionMetadata(root, entry.Name()); ok {
			summary.Title = meta.Title
			summary.Description = meta.Description
			summary.StartedAt = meta.StartedAt.UTC()
			if !meta.LastActivity.IsZero() {
				summary.LastActivity = meta.LastActivity.UTC()
			}
		}
		if summary.StartedAt.IsZero() {
			summary.StartedAt = summary.LastActivity
		}
		if strings.TrimSpace(summary.Title) == "" {
			summary.Title = fallbackSessionTitle(summary.StartedAt)
		}
		if strings.TrimSpace(summary.Description) == "" {
			summary.Description = fallbackSessionDescription(summary.StartedAt)
		}
		sessions = append(sessions, summary)
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].LastActivity.After(sessions[j].LastActivity) })
	return sessions, nil
}

func sessionHasContent(dir string) bool {
	for _, name := range []string{"short_term.jsonl", "memory_proposals.jsonl"} {
		if hasNonEmptyFile(filepath.Join(dir, name)) {
			return true
		}
	}
	renderedDir := filepath.Join(dir, "rendered_prompts")
	entries, err := os.ReadDir(renderedDir)
	return err == nil && len(entries) > 0
}

func hasNonEmptyFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}

func readSessionMetadata(root, sessionID string) (SessionMetadata, bool) {
	path, err := sessionMetadataPath(root, sessionID)
	if err != nil {
		return SessionMetadata{}, false
	}
	if _, err := os.Stat(path); err != nil {
		return SessionMetadata{}, false
	}
	var meta SessionMetadata
	if err := storage.ReadJSON(path, &meta); err != nil {
		return SessionMetadata{}, false
	}
	if meta.ID == "" {
		meta.ID = sessionID
	}
	return meta, true
}

func ensureSessionMetadata(root, sessionID, userInput string, now time.Time) error {
	path, err := sessionMetadataPath(root, sessionID)
	if err != nil {
		return err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	meta, ok := readSessionMetadata(root, sessionID)
	if !ok {
		meta = SessionMetadata{
			ID:          sessionID,
			StartedAt:   now,
			Title:       fallbackSessionTitle(now),
			Description: fallbackSessionDescription(now),
		}
	}
	if meta.StartedAt.IsZero() {
		meta.StartedAt = now
	}
	if meta.ID == "" {
		meta.ID = sessionID
	}
	if title := sessionTitleFromInput(userInput); title != "" && isGeneratedSessionTitle(meta.Title) {
		meta.Title = title
		meta.Description = sessionDescription(meta.StartedAt, userInput)
	} else if strings.TrimSpace(meta.Description) == "" {
		meta.Description = fallbackSessionDescription(meta.StartedAt)
	}
	meta.LastActivity = now
	return storage.AtomicWriteJSON(path, meta)
}

func isGeneratedSessionTitle(title string) bool {
	title = strings.TrimSpace(title)
	return title == "" || strings.HasPrefix(title, "Started ")
}

func fallbackSessionTitle(startedAt time.Time) string {
	return "Started " + startedAt.Local().Format("2006-01-02 15:04")
}

func fallbackSessionDescription(startedAt time.Time) string {
	return "Started " + startedAt.Local().Format("2006-01-02 15:04 MST")
}

func sessionDescription(startedAt time.Time, userInput string) string {
	title := sessionTitleFromInput(userInput)
	if title == "" {
		return fallbackSessionDescription(startedAt)
	}
	return fallbackSessionDescription(startedAt) + " · " + title
}

func sessionTitleFromInput(input string) string {
	input = strings.Join(strings.Fields(strings.TrimSpace(input)), " ")
	if input == "" {
		return ""
	}
	const maxTitle = 72
	runes := []rune(input)
	if len(runes) > maxTitle {
		return strings.TrimSpace(string(runes[:maxTitle-1])) + "…"
	}
	return input
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
