package cli

import (
	"bytes"
	"context"
	"sort"
	"strings"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/memory"
	"github.com/nikbrik/coding_writer/internal/process"
	"github.com/nikbrik/coding_writer/internal/providers"
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

func (b *ChatBackend) BuildInfo() tui.BuildInfo {
	return tui.BuildInfo{Version: Version, Commit: Commit, BuildDate: BuildDate}
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

func (b *ChatBackend) Transcript(ctx context.Context, sessionID string) ([]tui.TranscriptEntry, error) {
	records, err := b.rt.Memory.List(ctx, app.LayerShort, sessionID, "")
	if err != nil {
		appErr := app.AsError(err)
		if appErr.Code == "session_missing" || appErr.Code == "memory_read" {
			return nil, nil
		}
		return nil, err
	}
	out := make([]tui.TranscriptEntry, 0, len(records))
	for _, record := range records {
		switch record.Kind {
		case "history_user", "message_user":
			out = append(out, tui.TranscriptEntry{Role: app.RoleUser, Content: record.Content, CreatedAt: record.CreatedAt})
		case "history_assistant", "message_assistant":
			out = append(out, tui.TranscriptEntry{Role: app.RoleAssistant, Content: record.Content, CreatedAt: record.CreatedAt})
		}
	}
	return out, nil
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

func (b *ChatBackend) ListModels(ctx context.Context) (tui.ModelCatalog, error) {
	cfg := b.rt.Config
	catalog := tui.ModelCatalog{
		Models:    append([]string(nil), cfg.FavoriteModels...),
		Favorites: append([]string(nil), cfg.FavoriteModels...),
		Active:    cfg.ActiveModel,
	}
	if cfg.ActiveModel != "" {
		catalog.Models = append(catalog.Models, cfg.ActiveModel)
	}
	b.rt.ensureProvider()
	if _, ok := b.rt.Provider.(*providers.OpenRouterProvider); ok && !b.rt.DisclosureShown {
		catalog.Warning = strings.TrimSpace(providerDisclosureText(cfg.OpenRouterBaseURL))
		b.rt.DisclosureShown = true
	}
	models, err := b.rt.Provider.ListModels(ctx)
	if err != nil {
		appErr := app.AsError(err)
		if appErr.Category == app.CategoryProvider && appErr.Code == "missing_api_key" {
			catalog.Warning = strings.TrimSpace(firstNonEmpty(catalog.Warning, appErr.Message))
		} else {
			catalog.Warning = strings.TrimSpace(firstNonEmpty(catalog.Warning, appErr.Message))
		}
		catalog.Models = appendUniqueStrings(catalog.Models, fallbackModelIDs()...)
		catalog.Models = uniqueSortedModels(catalog.Models)
		return catalog, nil
	}
	if len(models) == 0 {
		models = fallbackModelIDs()
	}
	catalog.Models = appendUniqueStrings(catalog.Models, models...)
	catalog.Models = uniqueSortedModels(catalog.Models)
	return catalog, nil
}

func (b *ChatBackend) SelectModel(ctx context.Context, modelID string) (app.AppConfig, error) {
	var diag bytes.Buffer
	return setActiveModel(ctx, b.rt, &diag, modelID)
}

func (b *ChatBackend) ToggleFavoriteModel(ctx context.Context, modelID string) (app.AppConfig, error) {
	if err := validateModelSyntax(modelID); err != nil {
		return b.rt.Config, err
	}
	cfg, err := b.rt.ConfigMgr.Update(func(cfg *app.AppConfig) error {
		cfg.FavoriteModels = toggleModelID(cfg.FavoriteModels, modelID)
		return nil
	})
	if err != nil {
		return b.rt.Config, err
	}
	b.rt.Config = cfg
	return cfg, nil
}

func (b *ChatBackend) SelectSession(ctx context.Context, targetSessionID, currentSessionID string) (tui.SlashResponse, error) {
	return b.slashResult(ctx, currentSessionID, "/resume "+targetSessionID)
}

func (b *ChatBackend) SelectTask(ctx context.Context, taskID, sessionID string) (tui.SlashResponse, error) {
	return b.slashResult(ctx, sessionID, "/task "+taskID)
}

func (b *ChatBackend) ClearTask(ctx context.Context, currentSessionID string) (tui.SlashResponse, error) {
	return b.slashResult(ctx, currentSessionID, "/task close")
}

func (b *ChatBackend) ArchiveTask(ctx context.Context, taskID, currentSessionID string) (tui.SlashResponse, error) {
	return b.slashResult(ctx, currentSessionID, "/task archive "+taskID)
}

func (b *ChatBackend) RestoreTask(ctx context.Context, taskID, sessionID string) (tui.SlashResponse, error) {
	return b.slashResult(ctx, sessionID, "/task restore "+taskID)
}

func (b *ChatBackend) SelectProfile(ctx context.Context, profileID, currentSessionID string) (tui.SlashResponse, error) {
	return b.slashResult(ctx, currentSessionID, "/profile "+profileID)
}

func (b *ChatBackend) CreateProfile(ctx context.Context, profileID, currentSessionID string) (tui.SlashResponse, error) {
	return b.slashResult(ctx, currentSessionID, "/profile create "+profileID)
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
	var diag bytes.Buffer
	result, err := handleSlashResult(ctx, &diag, b.rt, sessionID, line)
	resp := toTUISlashResponse(result)
	text := strings.TrimSpace(resp.Output)
	if diag.Len() > 0 {
		if text != "" {
			text += "\n"
		}
		text += strings.TrimSpace(diag.String())
	}
	resp.Output = text
	return resp, err
}

func (b *ChatBackend) slashResult(ctx context.Context, sessionID, line string) (tui.SlashResponse, error) {
	var diag bytes.Buffer
	result, err := handleSlashResult(ctx, &diag, b.rt, sessionID, line)
	resp := toTUISlashResponse(result)
	if diag.Len() > 0 {
		if strings.TrimSpace(resp.Output) != "" {
			resp.Output += "\n"
		}
		resp.Output += strings.TrimSpace(diag.String())
	}
	return resp, err
}

func toTUISlashResponse(result slashContextResult) tui.SlashResponse {
	resp := tui.SlashResponse{
		Done:            result.Done,
		Output:          result.Output,
		ActiveSessionID: result.ActiveSessionID,
		ActiveTask:      result.ActiveTask,
		TaskCleared:     result.TaskCleared,
		ActiveProfile:   result.ActiveProfile,
		ActiveConfig:    result.ActiveConfig,
		PendingBlocked:  result.PendingBlocked,
	}
	if result.Picker != nil {
		resp.Picker = &tui.PickerPayload{Kind: result.Picker.Kind}
		for _, session := range result.Picker.Sessions {
			resp.Picker.Sessions = append(resp.Picker.Sessions, tui.SessionSummary{
				ID:           session.ID,
				Title:        session.Title,
				Description:  session.Description,
				StartedAt:    session.StartedAt,
				LastActivity: session.LastActivity,
			})
		}
		for _, task := range result.Picker.Tasks {
			resp.Picker.Tasks = append(resp.Picker.Tasks, tui.TaskSummary{
				ID:        task.State.ID,
				Title:     task.State.Title,
				Stage:     task.State.Stage,
				Status:    task.State.Status,
				Current:   task.IsCurrent,
				Archived:  task.Archived,
				UpdatedAt: task.State.UpdatedAt,
			})
		}
		activeProfile := ""
		if result.ActiveConfig != nil {
			activeProfile = result.ActiveConfig.ActiveProfileID
		}
		for _, profile := range result.Picker.Profiles {
			resp.Picker.Profiles = append(resp.Picker.Profiles, tui.ProfileSummary{ID: profile.ID, DisplayName: profile.DisplayName, Active: profile.ID == activeProfile})
		}
	}
	return resp
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

func fallbackModelIDs() []string {
	return []string{
		"anthropic/claude-3.5-sonnet",
		"fake/model",
		"google/gemini-3.1-flash-lite",
		"openai/gpt-4.1-mini",
	}
}

func appendUniqueStrings(values []string, more ...string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		seen[value] = true
	}
	for _, value := range more {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		values = append(values, value)
	}
	return values
}

func uniqueSortedModels(values []string) []string {
	out := appendUniqueStrings(nil, values...)
	sort.Strings(out)
	return out
}

func toggleModelID(values []string, modelID string) []string {
	out := make([]string, 0, len(values)+1)
	removed := false
	for _, value := range values {
		if value == modelID {
			removed = true
			continue
		}
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	if !removed {
		out = append(out, modelID)
	}
	return uniqueSortedModels(out)
}
