package tui

import (
	"context"
	"time"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/memory"
	"github.com/nikbrik/coding_writer/internal/process"
)

type Backend interface {
	Config() app.AppConfig
	BuildInfo() BuildInfo
	StorageDir() string
	CurrentTask() (app.TaskState, bool, error)
	Transcript(ctx context.Context, sessionID string) ([]TranscriptEntry, error)
	LatestAudit(limit int) ([]process.ProcessAuditEvent, error)
	LatestPendingProposal(ctx context.Context, sessionID string) (app.MemoryProposal, bool, error)
	ListModels(ctx context.Context) (ModelCatalog, error)
	SelectModel(ctx context.Context, modelID string) (app.AppConfig, error)
	ToggleFavoriteModel(ctx context.Context, modelID string) (app.AppConfig, error)
	SelectSession(ctx context.Context, targetSessionID, currentSessionID string) (SlashResponse, error)
	SelectTask(ctx context.Context, taskID, sessionID string) (SlashResponse, error)
	ClearTask(ctx context.Context, currentSessionID string) (SlashResponse, error)
	ArchiveTask(ctx context.Context, taskID, currentSessionID string) (SlashResponse, error)
	RestoreTask(ctx context.Context, taskID, sessionID string) (SlashResponse, error)
	SelectProfile(ctx context.Context, profileID, currentSessionID string) (SlashResponse, error)
	CreateProfile(ctx context.Context, profileID, currentSessionID string) (SlashResponse, error)
	Exchange(ctx context.Context, req ChatRequest) (ChatResponse, error)
	Slash(ctx context.Context, sessionID, line string) (SlashResponse, error)
	ApplyMemory(ctx context.Context, req MemoryApplyRequest) (memory.ApplyResult, error)
	ApprovePlanning(ctx context.Context, sessionID string) (ChatResponse, error)
	RejectPlanning(ctx context.Context, sessionID string) (ChatResponse, error)
	PauseTask() (app.TaskState, error)
	ResumeTask() (app.TaskState, error)
	Evidence(ctx context.Context, taskID, sessionID string, refs []string) ([]EvidenceView, error)
}

type BuildInfo struct {
	Version   string
	Commit    string
	BuildDate string
}

type Options struct {
	In  any
	Out any
	Err any
}

type ModelCatalog struct {
	Models    []string
	Favorites []string
	Active    string
	Warning   string
}

type ChatRequest struct {
	SessionID             string
	Input                 string
	RenderOnly            bool
	RequireMemoryProposal bool
	VerifyCommand         string
	IgnoreCurrentTask     bool
}

type ChatResponse struct {
	OK               bool
	SessionID        string
	Answer           string
	Model            string
	Proposal         *app.MemoryProposal
	Transition       *process.TransitionResult
	AppliedArtifacts []string
	Warnings         []string
	Task             *app.TaskState
	AuditEvents      []process.ProcessAuditEvent
	RenderedPromptID string
}

type TranscriptEntry struct {
	Role      app.ChatRole
	Content   string
	CreatedAt time.Time
}

type SlashResponse struct {
	Done            bool
	Output          string
	ActiveSessionID string
	ActiveTask      *app.TaskState
	TaskCleared     bool
	ActiveProfile   *app.UserProfile
	ActiveConfig    *app.AppConfig
	Picker          *PickerPayload
	PendingBlocked  string
}

type PickerPayload struct {
	Kind     string
	Sessions []SessionSummary
	Tasks    []TaskSummary
	Profiles []ProfileSummary
}

type SessionSummary struct {
	ID           string
	Title        string
	Description  string
	StartedAt    time.Time
	LastActivity time.Time
}

type TaskSummary struct {
	ID        string
	Title     string
	Stage     app.TaskStage
	Status    app.TaskStatus
	Current   bool
	Archived  bool
	UpdatedAt time.Time
}

type ProfileSummary struct {
	ID          string
	DisplayName string
	Active      bool
}

type MemoryApplyRequest struct {
	SessionID  string
	TaskID     string
	ProposalID string
	AcceptAll  bool
	RejectAll  bool
	AcceptIDs  []string
	RejectIDs  []string
}

type EvidenceView struct {
	Ref           string
	ID            string
	Command       string
	ExitCode      int
	Summary       string
	CreatedAt     time.Time
	OutputPreview string
}
