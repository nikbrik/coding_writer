package cli

import (
	"bytes"
	"context"
	"strings"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/memory"
	"github.com/nikbrik/coding_writer/internal/process"
	"github.com/nikbrik/coding_writer/internal/tui"
)

type ChatBackend struct {
	rt *runtime
}

func newChatBackendFromRuntime(rt *runtime) *ChatBackend {
	return &ChatBackend{rt: rt}
}

func (b *ChatBackend) Config() app.AppConfig {
	if b == nil || b.rt == nil {
		return app.AppConfig{}
	}
	return b.rt.Config
}

func (b *ChatBackend) StorageDir() string {
	if b == nil || b.rt == nil {
		return ""
	}
	return b.rt.StorageDir
}

func (b *ChatBackend) CurrentTask() (app.TaskState, bool, error) {
	task, err := b.rt.Tasks.Current()
	if err != nil {
		appErr := app.AsError(err)
		if appErr.Category == app.CategoryValidation && appErr.Code == "missing_current_task" {
			return app.TaskState{}, false, nil
		}
		return app.TaskState{}, false, err
	}
	return task, true, nil
}

func (b *ChatBackend) LatestAudit(limit int) ([]process.ProcessAuditEvent, error) {
	return process.NewAuditStore(b.rt.StorageDir).Latest(limit)
}

func (b *ChatBackend) LatestPendingProposal(ctx context.Context, sessionID string) (app.MemoryProposal, bool, error) {
	proposal, ok, err := b.rt.Proposals.LatestPending(ctx, sessionID)
	if err != nil {
		appErr := app.AsError(err)
		if appErr.Code == "session_missing" || appErr.Code == "proposal_read" || appErr.Code == "missing_proposal" {
			return app.MemoryProposal{}, false, nil
		}
		return app.MemoryProposal{}, false, err
	}
	return proposal, ok, nil
}

func (b *ChatBackend) Exchange(ctx context.Context, req tui.ChatRequest) (tui.ChatResponse, error) {
	if strings.TrimSpace(req.Input) != "" && !req.RenderOnly {
		if err := b.rt.preflightProcess(ctx, process.ExchangeInput{SessionID: req.SessionID, Input: req.Input}); err != nil {
			return tui.ChatResponse{OK: false, SessionID: req.SessionID, Model: b.rt.Config.ActiveModel}, err
		}
		b.rt.ensureProvider()
	}
	result, err := runChatExchange(ctx, b.rt, req.SessionID, req.Input, req.RenderOnly, req.RequireMemoryProposal, req.VerifyCommand)
	return tui.ChatResponse{
		OK:               result.OK,
		SessionID:        result.SessionID,
		Answer:           result.Answer,
		Model:            result.Model,
		Proposal:         result.Proposal,
		Transition:       result.Transition,
		AppliedArtifacts: result.AppliedArtifacts,
		Warnings:         result.Warnings,
		Task:             result.Task,
		AuditEvents:      result.AuditEvents,
		RenderedPromptID: result.RenderedPromptID,
	}, err
}

func (b *ChatBackend) Slash(ctx context.Context, sessionID, line string) (tui.SlashResponse, error) {
	var out bytes.Buffer
	var diag bytes.Buffer
	done, err := handleSlash(ctx, &out, &diag, b.rt, sessionID, line)
	text := strings.TrimSpace(out.String())
	if diag.Len() > 0 {
		if text != "" {
			text += "\n"
		}
		text += strings.TrimSpace(diag.String())
	}
	return tui.SlashResponse{Done: done, Output: text}, err
}

func (b *ChatBackend) ApplyMemory(ctx context.Context, req tui.MemoryApplyRequest) (memory.ApplyResult, error) {
	taskID, workBlockedCode, workBlockedMessage, err := b.rt.workApplyContext()
	if err != nil {
		return memory.ApplyResult{}, err
	}
	if req.TaskID != "" {
		taskID = req.TaskID
	}
	profile, err := b.rt.activeProfile()
	if err != nil {
		return memory.ApplyResult{}, err
	}
	acceptIDs := map[string]bool{}
	for _, id := range req.AcceptIDs {
		acceptIDs[id] = true
	}
	rejectIDs := map[string]bool{}
	for _, id := range req.RejectIDs {
		rejectIDs[id] = true
	}
	opts := memory.ApplyOptions{
		ProposalID:         firstNonEmpty(req.ProposalID, "latest"),
		AcceptAll:          req.AcceptAll,
		AcceptIDs:          acceptIDs,
		RejectAll:          req.RejectAll,
		RejectIDs:          rejectIDs,
		SessionID:          req.SessionID,
		TaskID:             taskID,
		ProfileID:          profile.ID,
		WorkBlockedCode:    workBlockedCode,
		WorkBlockedMessage: workBlockedMessage,
	}
	if len(acceptIDs) == 0 {
		opts.AcceptIDs = nil
	}
	if len(rejectIDs) == 0 {
		opts.RejectIDs = nil
	}
	return b.rt.Proposals.Apply(ctx, opts)
}

func (b *ChatBackend) ApprovePlanning(ctx context.Context, sessionID string) (tui.ChatResponse, error) {
	return b.Exchange(ctx, tui.ChatRequest{SessionID: sessionID, Input: "Да, план принят. Приступай к выполнению.", RequireMemoryProposal: true})
}

func (b *ChatBackend) RejectPlanning(ctx context.Context, sessionID string) (tui.ChatResponse, error) {
	return b.Exchange(ctx, tui.ChatRequest{SessionID: sessionID, Input: "План отклонён. Предложи исправленный план.", RequireMemoryProposal: true})
}

func (b *ChatBackend) PauseTask() (app.TaskState, error) {
	return b.rt.Tasks.Pause()
}

func (b *ChatBackend) ResumeTask() (app.TaskState, error) {
	return b.rt.Tasks.Resume()
}

func (b *ChatBackend) Evidence(ctx context.Context, taskID, sessionID string, refs []string) ([]tui.EvidenceView, error) {
	records, err := process.NewTrustedEvidenceStore(b.rt.StorageDir).Validate(taskID, sessionID, refs)
	if err != nil {
		return nil, err
	}
	out := make([]tui.EvidenceView, 0, len(records))
	for _, record := range records {
		out = append(out, tui.EvidenceView{
			Ref:       "app:evidence:v2:" + record.ID,
			ID:        record.ID,
			Command:   record.Source,
			ExitCode:  record.ExitCode,
			Summary:   record.SHA256,
			CreatedAt: record.CreatedAt,
		})
	}
	return out, nil
}
