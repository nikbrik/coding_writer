package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/memory"
	"github.com/nikbrik/coding_writer/internal/process"
)

type fakeBackend struct {
	config    app.AppConfig
	task      *app.TaskState
	proposal  *app.MemoryProposal
	responses []ChatResponse
	applied   []MemoryApplyRequest
	models    []string
	badModels map[string]bool
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
	return nil, nil
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
func (f *fakeBackend) SelectSession(ctx context.Context, sessionID string) (SlashResponse, error) {
	return SlashResponse{ActiveSessionID: sessionID, Output: "resumed chat: " + sessionID}, nil
}
func (f *fakeBackend) SelectTask(ctx context.Context, taskID, sessionID string) (SlashResponse, error) {
	task := app.TaskState{ID: taskID, Title: "selected", Stage: app.StagePlanning, Status: app.TaskStatusActive, ExpectedAction: app.ExpectedUserInput, LastSessionID: sessionID}
	f.task = &task
	return SlashResponse{ActiveTask: &task, Output: "active task: " + taskID}, nil
}
func (f *fakeBackend) ClearTask(ctx context.Context) (SlashResponse, error) {
	f.task = nil
	return SlashResponse{TaskCleared: true, Output: "task focus: none"}, nil
}
func (f *fakeBackend) ArchiveTask(ctx context.Context, taskID string) (SlashResponse, error) {
	f.task = nil
	return SlashResponse{TaskCleared: true, Output: "archived task: " + taskID}, nil
}
func (f *fakeBackend) RestoreTask(ctx context.Context, taskID, sessionID string) (SlashResponse, error) {
	task := app.TaskState{ID: taskID, Title: "restored", Stage: app.StagePlanning, Status: app.TaskStatusActive, ExpectedAction: app.ExpectedUserInput, LastSessionID: sessionID}
	f.task = &task
	return SlashResponse{ActiveTask: &task, Output: "restored and active task: " + taskID}, nil
}
func (f *fakeBackend) SelectProfile(ctx context.Context, profileID string) (SlashResponse, error) {
	f.config.ActiveProfileID = profileID
	profile := app.UserProfile{ID: profileID, DisplayName: profileID}
	return SlashResponse{ActiveProfile: &profile, ActiveConfig: &f.config, Output: "active profile: " + profileID}, nil
}
func (f *fakeBackend) CreateProfile(ctx context.Context, profileID string) (SlashResponse, error) {
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
		return SlashResponse{Picker: &PickerPayload{Kind: "sessions", Sessions: []SessionSummary{{ID: "session_old"}}}}, nil
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

func runTeaCommand(cmd tea.Cmd) {
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, child := range batch {
			runTeaCommand(child)
		}
	}
}
