package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/memory"
	"github.com/nikbrik/coding_writer/internal/process"
)

type fakeBackend struct {
	config     app.AppConfig
	task       *app.TaskState
	proposal   *app.MemoryProposal
	audit      []process.ProcessAuditEvent
	auditCalls int
	responses  []ChatResponse
	applied    []MemoryApplyRequest
	models     []string
	badModels  map[string]bool
}

func (f *fakeBackend) Config() app.AppConfig { return f.config }
func (f *fakeBackend) StorageDir() string    { return "/tmp/fake" }
func (f *fakeBackend) CurrentTask() (app.TaskState, bool, error) {
	if f.task == nil {
		return app.TaskState{}, false, nil
	}
	return *f.task, true, nil
}
func (f *fakeBackend) LatestAudit(limit int) ([]process.ProcessAuditEvent, error) {
	f.auditCalls++
	if limit > 0 && limit < len(f.audit) {
		return f.audit[len(f.audit)-limit:], nil
	}
	return f.audit, nil
}
func (f *fakeBackend) LatestPendingProposal(ctx context.Context, sessionID string) (app.MemoryProposal, bool, error) {
	if f.proposal == nil {
		return app.MemoryProposal{}, false, nil
	}
	return *f.proposal, true, nil
}
func (f *fakeBackend) ListModels(ctx context.Context) (ModelCatalog, error) {
	return ModelCatalog{Models: f.models, Favorites: f.config.FavoriteModels, Active: f.config.ActiveModel}, nil
}
func (f *fakeBackend) SelectModel(ctx context.Context, modelID string) (app.AppConfig, error) {
	if f.badModels != nil && f.badModels[modelID] {
		return f.config, app.NewError(app.CategoryValidation, "invalid_model", "model id not found", nil)
	}
	f.config.ActiveModel = modelID
	f.config.MemoryModel = modelID
	return f.config, nil
}
func (f *fakeBackend) ToggleFavoriteModel(ctx context.Context, modelID string) (app.AppConfig, error) {
	for i, favorite := range f.config.FavoriteModels {
		if favorite == modelID {
			f.config.FavoriteModels = append(f.config.FavoriteModels[:i], f.config.FavoriteModels[i+1:]...)
			return f.config, nil
		}
	}
	f.config.FavoriteModels = append(f.config.FavoriteModels, modelID)
	return f.config, nil
}
func (f *fakeBackend) SelectSession(ctx context.Context, sessionID, currentSessionID string) (SlashResponse, error) {
	return SlashResponse{ActiveSessionID: sessionID, Output: "resumed chat: " + sessionID}, nil
}
func (f *fakeBackend) SelectTask(ctx context.Context, taskID, sessionID string) (SlashResponse, error) {
	task := app.TaskState{ID: taskID, Title: "selected", Stage: app.StagePlanning, Status: app.TaskStatusActive, ExpectedAction: app.ExpectedUserInput, LastSessionID: sessionID}
	f.task = &task
	return SlashResponse{ActiveTask: &task, Output: "active task: " + taskID}, nil
}
func (f *fakeBackend) ClearTask(ctx context.Context, currentSessionID string) (SlashResponse, error) {
	f.task = nil
	return SlashResponse{TaskCleared: true, Output: "task focus: none"}, nil
}
func (f *fakeBackend) ArchiveTask(ctx context.Context, taskID, currentSessionID string) (SlashResponse, error) {
	f.task = nil
	return SlashResponse{TaskCleared: true, Output: "archived task: " + taskID}, nil
}
func (f *fakeBackend) RestoreTask(ctx context.Context, taskID, sessionID string) (SlashResponse, error) {
	task := app.TaskState{ID: taskID, Title: "restored", Stage: app.StagePlanning, Status: app.TaskStatusActive, ExpectedAction: app.ExpectedUserInput, LastSessionID: sessionID}
	f.task = &task
	return SlashResponse{ActiveTask: &task, Output: "restored and active task: " + taskID}, nil
}
func (f *fakeBackend) SelectProfile(ctx context.Context, profileID, currentSessionID string) (SlashResponse, error) {
	f.config.ActiveProfileID = profileID
	profile := app.UserProfile{ID: profileID, DisplayName: profileID}
	return SlashResponse{ActiveProfile: &profile, ActiveConfig: &f.config, Output: "active profile: " + profileID}, nil
}
func (f *fakeBackend) CreateProfile(ctx context.Context, profileID, currentSessionID string) (SlashResponse, error) {
	f.config.ActiveProfileID = profileID
	profile := app.UserProfile{ID: profileID, DisplayName: profileID}
	return SlashResponse{ActiveProfile: &profile, ActiveConfig: &f.config, Output: "created and active profile: " + profileID}, nil
}
func (f *fakeBackend) Exchange(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	if len(f.responses) == 0 {
		return ChatResponse{}, errors.New("missing fake response")
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, nil
}
func (f *fakeBackend) Slash(ctx context.Context, sessionID, line string) (SlashResponse, error) {
	switch line {
	case "/resume":
		return SlashResponse{Picker: &PickerPayload{Kind: "sessions", Sessions: []SessionSummary{{
			ID:          "session_old",
			Title:       "Реализовать ContainsDuplicate",
			Description: "Started 2026-06-21 13:40 MSK · Реализовать ContainsDuplicate",
			StartedAt:   time.Date(2026, 6, 21, 10, 40, 0, 0, time.UTC),
		}}}}, nil
	case "/task":
		return SlashResponse{Picker: &PickerPayload{Kind: "tasks", Tasks: []TaskSummary{{ID: "task_one", Title: "one", Stage: app.StagePlanning, Status: app.TaskStatusActive}}}}, nil
	case "/profile":
		return SlashResponse{Picker: &PickerPayload{Kind: "profiles", Profiles: []ProfileSummary{{ID: "student", DisplayName: "Student", Active: true}}}}, nil
	}
	return SlashResponse{Output: "ok"}, nil
}
func (f *fakeBackend) ApplyMemory(ctx context.Context, req MemoryApplyRequest) (memory.ApplyResult, error) {
	f.applied = append(f.applied, req)
	return memory.ApplyResult{}, nil
}
func (f *fakeBackend) ApprovePlanning(ctx context.Context, sessionID string) (ChatResponse, error) {
	return f.Exchange(ctx, ChatRequest{SessionID: sessionID, Input: "approve"})
}
func (f *fakeBackend) RejectPlanning(ctx context.Context, sessionID string) (ChatResponse, error) {
	return f.Exchange(ctx, ChatRequest{SessionID: sessionID, Input: "reject"})
}
func (f *fakeBackend) PauseTask() (app.TaskState, error)  { return *f.task, nil }
func (f *fakeBackend) ResumeTask() (app.TaskState, error) { return *f.task, nil }
func (f *fakeBackend) Evidence(ctx context.Context, taskID, sessionID string, refs []string) ([]EvidenceView, error) {
	return []EvidenceView{{ID: "e1", Command: "go test ./pkg", ExitCode: 0}}, nil
}

func TestModelExchangeUpdatesCodingWorkspaceView(t *testing.T) {
	task := app.TaskState{ID: "task_demo", Title: "Contains Duplicate", Stage: app.StagePlanning, ExpectedAction: app.ExpectedUserConfirmation, Status: app.TaskStatusActive}
	fake := &fakeBackend{
		config: app.AppConfig{ActiveModel: "fake/model", ActiveProfileID: "student"},
		responses: []ChatResponse{{
			OK:               true,
			Answer:           `{"stage":"planning","summary":"plan ready","acceptance_criteria":["tests pass"],"plan":["implement"],"readiness":"ready_for_execution_proposal"}`,
			Task:             &task,
			AppliedArtifacts: []string{"manual_scratch/day15_contains_duplicate/solution.go"},
			Warnings:         []string{"auto verification: go test ./manual_scratch/day15_contains_duplicate"},
		}},
	}
	m := NewModel(context.Background(), fake)
	m.width = 120
	m.height = 40
	m.input.SetValue("Спланируй задачу")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if !m.busy {
		t.Fatal("submit should mark model busy")
	}
	msg := cmd().(exchangeFinishedMsg)
	next, _ = m.Update(msg)
	m = next.(Model)
	view := m.View()
	for _, want := range []string{"codingwriter", "Status", "Plan", "Files", "plan ready", "applied:"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, `"stage"`) {
		t.Fatalf("TUI leaked raw stage JSON:\n%s", view)
	}
}

func TestMemoryShortcutAppliesPendingProposal(t *testing.T) {
	proposal := app.MemoryProposal{ID: "proposal_1", Records: []app.ProposedMemoryRecord{{ID: "rec_1", Status: app.ProposalPending, Layer: app.ProposedLayerWork, Content: "remember task"}}}
	fake := &fakeBackend{proposal: &proposal}
	m := NewModel(context.Background(), fake)
	m.proposal = &proposal
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expected memory apply command")
	}
	runTeaCommand(cmd)
	if len(fake.applied) != 1 || !fake.applied[0].AcceptAll {
		t.Fatalf("memory apply not called with accept all: %#v", fake.applied)
	}
}

func TestPlanningApprovalShortcuts(t *testing.T) {
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("a")},
		{Type: tea.KeyRunes, Runes: []rune("ф")},
		{Type: tea.KeyEnter},
	} {
		task := app.TaskState{
			ID:              "task_plan",
			Stage:           app.StagePlanning,
			ExpectedAction:  app.ExpectedUserConfirmation,
			Status:          app.TaskStatusActive,
			PendingPlanning: &app.PlanningProposalState{ID: "plan_1", Summary: "plan", Plan: []string{"step"}},
		}
		fake := &fakeBackend{
			task: &task,
			responses: []ChatResponse{{
				OK:     true,
				Answer: "approved",
				Task:   &task,
			}},
		}
		m := NewModel(context.Background(), fake)
		m.task = &task
		next, cmd := m.Update(key)
		m = next.(Model)
		if !m.busy || cmd == nil {
			t.Fatalf("approval key %q did not start approval", key.String())
		}
		msg := cmd().(exchangeFinishedMsg)
		if msg.err != nil {
			t.Fatalf("approval command failed: %v", msg.err)
		}
	}
}

func TestModelPickerSearchFavoriteAndSelect(t *testing.T) {
	fake := &fakeBackend{
		config: app.AppConfig{ActiveModel: "openai/gpt-4.1-mini", ActiveProfileID: "student"},
		models: []string{
			"anthropic/claude-3.5-sonnet",
			"google/gemini-3.1-flash-lite",
			"openai/gpt-4.1-mini",
		},
	}
	m := NewModel(context.Background(), fake)
	m.width = 120
	m.height = 40
	m.input.SetValue("/model")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if !m.busy || cmd == nil {
		t.Fatal("/model should start model catalog load")
	}
	if m.modelPicker == nil {
		t.Fatal("/model should show picker immediately")
	}
	immediate := m.View()
	for _, want := range []string{"Select model", "google/gemini-3.1-flash-lite", "loading provider model list"} {
		if !strings.Contains(immediate, want) {
			t.Fatalf("immediate picker view missing %q:\n%s", want, immediate)
		}
	}
	next, _ = m.Update(cmd().(modelsLoadedMsg))
	m = next.(Model)
	if m.modelPicker == nil {
		t.Fatal("model picker did not open")
	}
	view := m.View()
	for _, want := range []string{"Select model", "openai", "google", "anthropic"} {
		if !strings.Contains(view, want) {
			t.Fatalf("picker view missing %q:\n%s", want, view)
		}
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("gem")})
	m = next.(Model)
	if len(m.modelPicker.items) != 1 || m.modelPicker.items[0].ID != "google/gemini-3.1-flash-lite" {
		t.Fatalf("search did not filter to google model: %#v", m.modelPicker.items)
	}

	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("F")})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("favorite toggle command missing")
	}
	next, _ = m.Update(cmd().(favoriteToggledMsg))
	m = next.(Model)
	if !m.modelPicker.favorites["google/gemini-3.1-flash-lite"] {
		t.Fatalf("favorite not marked: %#v", m.modelPicker.favorites)
	}

	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if !m.busy || cmd == nil {
		t.Fatal("enter should select model")
	}
	next, _ = m.Update(cmd().(modelSelectedMsg))
	m = next.(Model)
	if m.modelPicker != nil {
		t.Fatal("picker should close after selection")
	}
	if fake.config.ActiveModel != "google/gemini-3.1-flash-lite" || fake.config.MemoryModel != "google/gemini-3.1-flash-lite" {
		t.Fatalf("model not selected: %+v", fake.config)
	}
}

func TestSlashCommandsShowWhileTypingSlash(t *testing.T) {
	fake := &fakeBackend{config: app.AppConfig{ActiveModel: "fake/model", ActiveProfileID: "student"}}
	m := NewModel(context.Background(), fake)
	m.width = 120
	m.height = 40

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = next.(Model)
	view := m.View()
	for _, want := range []string{"Slash commands", "/new", "/resume", "/profile", "/model", "/process audit", "/exit"} {
		if !strings.Contains(view, want) {
			t.Fatalf("slash help missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "+") && strings.Contains(view, "more") {
		t.Fatalf("slash help should not collapse commands behind +more:\n%s", view)
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("pro")})
	m = next.(Model)
	view = m.View()
	if !strings.Contains(view, "/profile") || !strings.Contains(view, "/process audit") {
		t.Fatalf("slash filter missing profile/process:\n%s", view)
	}
}

func TestSlashPrefixEnterRunsFirstMatchingCommand(t *testing.T) {
	fake := &fakeBackend{config: app.AppConfig{ActiveModel: "fake/model", ActiveProfileID: "student"}}
	m := NewModel(context.Background(), fake)
	m.input.SetValue("/resu")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if !m.busy || cmd == nil {
		t.Fatal("/resu should run first matching slash command")
	}
	msg := cmd().(slashFinishedMsg)
	if msg.line != "/resume" {
		t.Fatalf("expected /resu to complete to /resume, got %q", msg.line)
	}
}

func TestSlashArrowSelectsCommandFromFullList(t *testing.T) {
	fake := &fakeBackend{config: app.AppConfig{ActiveModel: "fake/model", ActiveProfileID: "student"}}
	m := NewModel(context.Background(), fake)
	m.input.SetValue("/")

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(Model)
	if m.slashCursor != 1 {
		t.Fatalf("down should select second slash command, got cursor=%d", m.slashCursor)
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if !m.busy || cmd == nil {
		t.Fatal("selected slash command should run")
	}
	msg := cmd().(slashFinishedMsg)
	if msg.line != "/resume" {
		t.Fatalf("expected selected slash command /resume, got %q", msg.line)
	}
}

func TestSlashPrefixEnterRunsModelPicker(t *testing.T) {
	fake := &fakeBackend{config: app.AppConfig{ActiveModel: "fake/model"}}
	m := NewModel(context.Background(), fake)
	m.input.SetValue("/mod")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if !m.busy || cmd == nil || m.modelPicker == nil {
		t.Fatalf("/mod should complete to /model and open picker: busy=%v picker=%v cmd=%v", m.busy, m.modelPicker, cmd)
	}
}

func TestSlashCompletionUsesFirstVisibleMatch(t *testing.T) {
	cases := map[string]string{
		"/resu":      "/resume",
		"/r":         "/resume",
		"/pro":       "/profile",
		"/profile c": "/profile create",
		"/task ar":   "/task archive",
		"/model":     "/model",
		"/unknown":   "/unknown",
	}
	for input, want := range cases {
		if got := completeSlashCommand(input, 0); got != want {
			t.Fatalf("completeSlashCommand(%q)=%q want %q", input, got, want)
		}
	}
	if got := completeSlashCommand("/", 1); got != "/resume" {
		t.Fatalf("selection should complete / to highlighted command, got %q", got)
	}
}

func TestFreshStartupShowsActiveModelInStatus(t *testing.T) {
	fake := &fakeBackend{config: app.AppConfig{ActiveModel: "openai/gpt-4.1-mini", ActiveProfileID: "student"}}
	m := NewModel(context.Background(), fake)
	m.width = 120
	m.height = 40
	view := m.View()
	for _, want := range []string{"model: openai/gpt-4.1-mini", "profile: student", "New chat"} {
		if !strings.Contains(view, want) {
			t.Fatalf("fresh view missing %q:\n%s", want, view)
		}
	}
}

func TestModelPickerInvalidSelectionDoesNotMutateConfig(t *testing.T) {
	fake := &fakeBackend{
		config:    app.AppConfig{ActiveModel: "openai/gpt-4.1-mini", MemoryModel: "openai/gpt-4.1-mini"},
		models:    []string{"missing/model", "openai/gpt-4.1-mini"},
		badModels: map[string]bool{"missing/model": true},
	}
	m := NewModel(context.Background(), fake)
	m.modelPicker = newModelPickerState(ModelCatalog{Models: fake.models, Active: fake.config.ActiveModel})
	m.modelPicker.query = "missing"
	m.modelPicker.rebuild()

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if !m.busy || cmd == nil {
		t.Fatal("expected select command")
	}
	next, _ = m.Update(cmd().(modelSelectedMsg))
	m = next.(Model)
	if fake.config.ActiveModel != "openai/gpt-4.1-mini" || fake.config.MemoryModel != "openai/gpt-4.1-mini" {
		t.Fatalf("invalid selection mutated config: %+v", fake.config)
	}
	if m.err == nil || m.err.Code != "invalid_model" {
		t.Fatalf("missing invalid_model error: %+v", m.err)
	}
}

func TestModelPickerScrollsLongCatalog(t *testing.T) {
	models := []string{"openai/gpt-4.1-mini"}
	for i := 0; i < 40; i++ {
		models = append(models, fmt.Sprintf("provider/model-%02d", i))
	}
	fake := &fakeBackend{
		config: app.AppConfig{ActiveModel: "openai/gpt-4.1-mini"},
		models: models,
	}
	m := NewModel(context.Background(), fake)
	m.width = 100
	m.height = 14
	m.modelPicker = newModelPickerState(ModelCatalog{Models: models, Active: fake.config.ActiveModel})

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = next.(Model)
	view := m.View()
	if !strings.Contains(view, "provider/model-39") {
		t.Fatalf("selected bottom model not visible:\n%s", view)
	}
	if strings.Contains(view, "provider/model-00") {
		t.Fatalf("long picker did not scroll away from first item:\n%s", view)
	}
	if !strings.Contains(view, "41/41") {
		t.Fatalf("scroll position missing:\n%s", view)
	}
}

func TestContextPickersApplyTypedSlashTransitions(t *testing.T) {
	fake := &fakeBackend{config: app.AppConfig{ActiveProfileID: "student"}}
	m := NewModel(context.Background(), fake)
	m.input.SetValue("/resume")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	msg := cmd().(slashFinishedMsg)
	next, _ = m.Update(msg)
	m = next.(Model)
	if m.contextPicker == nil || m.contextPicker.payload.Kind != "sessions" {
		t.Fatalf("resume picker missing: %#v", m.contextPicker)
	}
	view := m.View()
	for _, want := range []string{"Реализовать ContainsDuplicate", "Started 2026-06-21"} {
		if !strings.Contains(view, want) {
			t.Fatalf("resume picker missing %q:\n%s", want, view)
		}
	}
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	msg = cmd().(slashFinishedMsg)
	next, _ = m.Update(msg)
	m = next.(Model)
	if m.sessionID != "session_old" {
		t.Fatalf("session transition not applied: %s", m.sessionID)
	}

	m.input.SetValue("/task")
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	next, _ = m.Update(cmd().(slashFinishedMsg))
	m = next.(Model)
	if m.contextPicker == nil || m.contextPicker.payload.Kind != "tasks" {
		t.Fatalf("task picker missing: %#v", m.contextPicker)
	}
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	next, _ = m.Update(cmd().(slashFinishedMsg))
	m = next.(Model)
	if m.task == nil || m.task.ID != "task_one" || m.task.LastSessionID != "session_old" {
		t.Fatalf("task transition not applied: %+v", m.task)
	}
}

func TestProfileNewPickerCreatesProfileFromInput(t *testing.T) {
	fake := &fakeBackend{config: app.AppConfig{ActiveProfileID: "student"}}
	m := NewModel(context.Background(), fake)
	m.input.SetValue("/profile")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	next, _ = m.Update(cmd().(slashFinishedMsg))
	m = next.(Model)
	if m.contextPicker == nil || m.contextPicker.payload.Kind != "profiles" {
		t.Fatalf("profile picker missing: %#v", m.contextPicker)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(Model)
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil || !m.contextPicker.profileInput {
		t.Fatalf("enter on new should enter profile input, cmd=%v picker=%#v", cmd, m.contextPicker)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("custom")})
	m = next.(Model)
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("profile create command missing")
	}
	next, _ = m.Update(cmd().(slashFinishedMsg))
	m = next.(Model)
	if fake.config.ActiveProfileID != "custom" {
		t.Fatalf("profile not created/selected: %+v", fake.config)
	}
}

func TestStartupAuditFiltersToCurrentTaskOrSession(t *testing.T) {
	task := &app.TaskState{ID: "task_current", LastSessionID: "session_current"}
	events := []process.ProcessAuditEvent{
		{TaskID: "task_other", SessionID: "session_other", Stage: app.StageExecution, Decision: "provider_call"},
		{TaskID: "", SessionID: "session_other", Stage: app.StageExecution, Decision: "rejected"},
		{TaskID: "task_current", SessionID: "session_old", Stage: app.StageExecution, Decision: "retried"},
		{TaskID: "", SessionID: "session_current", Stage: app.StageExecution, Decision: "provider_call"},
	}
	got := filterAuditForStartup(events, task, "session_new")
	if len(got) != 2 {
		t.Fatalf("expected only current task/session audit, got %d: %+v", len(got), got)
	}
	if got[0].TaskID != "task_current" || got[1].SessionID != "session_current" {
		t.Fatalf("wrong audit events kept: %+v", got)
	}
}

func TestStartupStateShowsStorageTaskSessionAndAuditCount(t *testing.T) {
	task := &app.TaskState{
		ID:             "task_current",
		Title:          "Current task",
		Stage:          app.StageExecution,
		Status:         app.TaskStatusActive,
		ExpectedAction: app.ExpectedLLMResponse,
		LastSessionID:  "session_task",
	}
	fake := &fakeBackend{task: task, audit: []process.ProcessAuditEvent{
		{TaskID: "task_current", SessionID: "session_task", Decision: "provider_call"},
		{TaskID: "task_current", SessionID: "session_task", Decision: "retried"},
	}}
	m := NewModel(context.Background(), fake)
	m.sessionID = "session_current"
	m.task = task
	m.audit = fake.audit
	m.appendStartupState()
	if len(m.events) != 1 {
		t.Fatalf("expected one startup event, got %d", len(m.events))
	}
	ev := m.events[0]
	for _, want := range []string{"storage=/tmp/fake", "new chat=session_current", "current task=task_current", "stage=execution", "task session=session_task", "history: /resume"} {
		if !strings.Contains(ev.Summary, want) {
			t.Fatalf("startup state missing %q: %+v", want, ev)
		}
	}
	if ev.Kind != "startup" || ev.Stage != app.StageExecution || ev.Title != "new chat" {
		t.Fatalf("wrong startup event metadata: %+v", ev)
	}
}

func TestStartupDoesNotLoadOldAuditOrPendingProposal(t *testing.T) {
	proposal := &app.MemoryProposal{ID: "proposal_old", SessionID: "session_old", Records: []app.ProposedMemoryRecord{
		{ID: "record_old", Layer: app.ProposedLayerShort, Kind: "context", Content: "old", Status: app.ProposalPending},
	}}
	fake := &fakeBackend{
		proposal: proposal,
		audit: []process.ProcessAuditEvent{
			{SessionID: "session_old", Decision: "rejected", ValidatorErrors: []string{"old"}},
		},
	}
	m := NewModel(context.Background(), fake)
	msg := m.loadInitial()().(initialLoadedMsg)
	if msg.mode != "startup" {
		t.Fatalf("startup load mode mismatch: %q", msg.mode)
	}
	if len(msg.audit) != 0 || msg.proposal != nil || fake.auditCalls != 0 {
		t.Fatalf("startup should not load old chat context: audit=%d proposal=%v auditCalls=%d", len(msg.audit), msg.proposal, fake.auditCalls)
	}
}

func TestFreshStartupHidesOldTaskDetailsInSidebar(t *testing.T) {
	task := &app.TaskState{
		ID:             "task_current",
		Title:          "Contains Duplicate",
		Objective:      "OLD_OBJECTIVE_MARKER",
		Plan:           []string{"OLD_PLAN_MARKER"},
		Stage:          app.StageExecution,
		Status:         app.TaskStatusActive,
		ExpectedAction: app.ExpectedLLMResponse,
	}
	m := NewModel(context.Background(), &fakeBackend{task: task})
	m.width = 140
	m.height = 36
	m.task = task
	m.resize()
	view := m.View()
	if strings.Contains(view, "OLD_OBJECTIVE_MARKER") || strings.Contains(view, "OLD_PLAN_MARKER") {
		t.Fatalf("fresh startup leaked old task details:\n%s", view)
	}
	for _, want := range []string{"New chat", "old chat: /resume", "task details: /task", "task focus:"} {
		if !strings.Contains(view, want) {
			t.Fatalf("fresh startup missing %q:\n%s", want, view)
		}
	}
}

func TestSlashNewDoesNotLoadOldAuditOrExpandedTaskDetails(t *testing.T) {
	task := &app.TaskState{
		ID:                 "task_current",
		Title:              "Contains Duplicate",
		Objective:          "OLD_OBJECTIVE_MARKER",
		AcceptanceCriteria: []string{"OLD_CRITERIA_MARKER"},
		Plan:               []string{"OLD_PLAN_MARKER"},
		Stage:              app.StageExecution,
		Status:             app.TaskStatusActive,
		ExpectedAction:     app.ExpectedLLMResponse,
		LastSessionID:      "session_old",
	}
	fake := &fakeBackend{
		task: task,
		audit: []process.ProcessAuditEvent{
			{SessionID: "session_old", TaskID: "task_current", Stage: app.StageExecution, Decision: "rejected", ValidatorErrors: []string{"ready_for_validation requires trusted evidence"}},
		},
	}
	m := NewModel(context.Background(), fake)
	m.width = 140
	m.height = 36
	m.task = task
	m.contextExpanded = true
	m.appendStartupAudit(fake.audit)

	next, cmd := m.Update(slashFinishedMsg{
		line: "/new",
		resp: SlashResponse{ActiveSessionID: "session_new", ActiveTask: task, Output: "new chat: session_new"},
	})
	m = next.(Model)
	if m.sessionID != "session_new" {
		t.Fatalf("/new did not switch in-memory session: %s", m.sessionID)
	}
	if m.contextExpanded {
		t.Fatal("/new should collapse task details in fresh chat")
	}
	if cmd == nil {
		t.Fatal("/new should refresh current state without loading history")
	}
	if !teaBatchContainsType(cmd, "clearScreenMsg") {
		t.Fatal("/new should request terminal clear to remove stale slash help")
	}
	view := m.View()
	for _, blocked := range []string{"restored audit history", "validation blocked: trusted evidence required", "OLD_OBJECTIVE_MARKER", "OLD_CRITERIA_MARKER", "OLD_PLAN_MARKER"} {
		if strings.Contains(view, blocked) {
			t.Fatalf("/new leaked old context %q:\n%s", blocked, view)
		}
	}
	for _, want := range []string{"New chat", "old chat: /resume", "task focus:"} {
		if !strings.Contains(view, want) {
			t.Fatalf("/new fresh view missing %q:\n%s", want, view)
		}
	}
}

func teaBatchContainsType(cmd tea.Cmd, typeName string) bool {
	msg := cmd()
	if strings.Contains(fmt.Sprintf("%T", msg), typeName) {
		return true
	}
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return false
	}
	for _, child := range batch {
		if child != nil && teaBatchContainsType(child, typeName) {
			return true
		}
	}
	return false
}

func TestExplicitResumeLoadsSessionHistory(t *testing.T) {
	proposal := &app.MemoryProposal{ID: "proposal_old", SessionID: "session_old", Records: []app.ProposedMemoryRecord{
		{ID: "record_old", Layer: app.ProposedLayerShort, Kind: "context", Content: "old", Status: app.ProposalPending},
	}}
	fake := &fakeBackend{
		proposal: proposal,
		audit: []process.ProcessAuditEvent{
			{SessionID: "session_other", Decision: "rejected", ValidatorErrors: []string{"other"}},
			{SessionID: "session_old", Decision: "provider_call"},
		},
	}
	m := NewModel(context.Background(), fake)
	msg := m.loadSessionContext("session_old")().(initialLoadedMsg)
	if msg.mode != "history" {
		t.Fatalf("resume load mode mismatch: %q", msg.mode)
	}
	if len(msg.audit) != 1 || msg.audit[0].SessionID != "session_old" {
		t.Fatalf("resume should load only selected session audit: %+v", msg.audit)
	}
	if msg.proposal == nil || msg.proposal.ID != "proposal_old" {
		t.Fatalf("resume should load selected session pending proposal: %+v", msg.proposal)
	}
}

func TestStartupAuditCompactsLongHistory(t *testing.T) {
	m := NewModel(context.Background(), &fakeBackend{})
	events := []process.ProcessAuditEvent{}
	for i := 0; i < 8; i++ {
		events = append(events, process.ProcessAuditEvent{Stage: app.StageExecution, ActionKind: process.ActionExecutePlanStep, Decision: "provider_call"})
	}
	events = append(events, process.ProcessAuditEvent{Stage: app.StageExecution, ActionKind: process.ActionExecutePlanStep, Decision: "rejected", ValidatorErrors: []string{"ready_for_validation requires trusted evidence"}})
	m.appendStartupAudit(events)
	if len(m.events) != 1 {
		t.Fatalf("expected compact summary only, got %d events", len(m.events))
	}
	if m.events[0].Title != "restored audit history" || !strings.Contains(m.events[0].Summary, "provider_call=8") || !strings.Contains(m.events[0].Summary, "rejected=1") {
		t.Fatalf("startup audit summary missing counts: %+v", m.events[0])
	}
	if strings.Contains(m.events[0].Summary, "ready_for_validation requires trusted evidence") || !strings.Contains(m.events[0].Summary, "validation blocked: trusted evidence required") {
		t.Fatalf("startup audit summary did not normalize raw validator error: %+v", m.events[0])
	}
	if m.events[0].Severity != "error" {
		t.Fatalf("summary should inherit last rejected severity, got %q", m.events[0].Severity)
	}
}

func runTeaCommand(cmd tea.Cmd) {
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, child := range batch {
			runTeaCommand(child)
		}
	}
}
