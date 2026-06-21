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
	StorageDir() string
	CurrentTask() (app.TaskState, bool, error)
	LatestAudit(limit int) ([]process.ProcessAuditEvent, error)
	LatestPendingProposal(ctx context.Context, sessionID string) (app.MemoryProposal, bool, error)
	Exchange(ctx context.Context, req ChatRequest) (ChatResponse, error)
	Slash(ctx context.Context, sessionID, line string) (SlashResponse, error)
	ApplyMemory(ctx context.Context, req MemoryApplyRequest) (memory.ApplyResult, error)
	ApprovePlanning(ctx context.Context, sessionID string) (ChatResponse, error)
	RejectPlanning(ctx context.Context, sessionID string) (ChatResponse, error)
	PauseTask() (app.TaskState, error)
	ResumeTask() (app.TaskState, error)
	Evidence(ctx context.Context, taskID, sessionID string, refs []string) ([]EvidenceView, error)
}

type Options struct {
	In  any
	Out any
	Err any
}

type ChatRequest struct {
	SessionID             string
	Input                 string
	RenderOnly            bool
	RequireMemoryProposal bool
	VerifyCommand         string
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

type SlashResponse struct {
	Done   bool
	Output string
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
