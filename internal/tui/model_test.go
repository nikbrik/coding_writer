package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/memory"
	"github.com/nikbrik/coding_writer/internal/process"
)

type fakeBackend struct {
	config     app.AppConfig
	build      BuildInfo
	task       *app.TaskState
	proposal   *app.MemoryProposal
	transcript []TranscriptEntry
	audit      []process.ProcessAuditEvent
	auditCalls int
	responses  []ChatResponse
	requests   []ChatRequest
	applied    []MemoryApplyRequest
	ragActions []RAGPendingAction
	models     []string
	badModels  map[string]bool
}

func (f *fakeBackend) Config() app.AppConfig { return f.config }
func (f *fakeBackend) BuildInfo() BuildInfo {
	if f.build.Version == "" {
		return BuildInfo{Version: "0.1.0", Commit: "testcommit"}
	}
	return f.build
}
func (f *fakeBackend) StorageDir() string { return "/tmp/fake" }
func (f *fakeBackend) CurrentTask() (app.TaskState, bool, error) {
	if f.task == nil {
		return app.TaskState{}, false, nil
	}
	return *f.task, true, nil
}
func (f *fakeBackend) Transcript(ctx context.Context, sessionID string) ([]TranscriptEntry, error) {
	return append([]TranscriptEntry(nil), f.transcript...), nil
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
	f.requests = append(f.requests, req)
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
func (f *fakeBackend) ConfirmRAGAction(ctx context.Context, sessionID string, action RAGPendingAction) (SlashResponse, error) {
	f.ragActions = append(f.ragActions, action)
	return SlashResponse{Output: "RAG setup complete"}, nil
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
	userInput := "Спланируй и реши простую LeetCode-задачу Contains Duplicate на Go"
	m.input.SetValue(userInput)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if !m.busy {
		t.Fatal("submit should mark model busy")
	}
	msg := exchangeFinishedFromCmd(t, cmd)
	next, _ = m.Update(msg)
	m = next.(Model)
	view := m.View()
	for _, want := range []string{"codingwriter", "Status", "Plan", "Files", "plan ready", "applied file", "last input:", "Contains Duplicate"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, `"stage"`) {
		t.Fatalf("TUI leaked raw stage JSON:\n%s", view)
	}
}

func TestSubmitClearsInputImmediately(t *testing.T) {
	fake := &fakeBackend{
		responses: []ChatResponse{{OK: true, Answer: "ok"}},
	}
	m := NewModel(context.Background(), fake)
	m.input.SetValue(strings.Repeat("длинный prompt ", 12))

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if !m.busy || cmd == nil {
		t.Fatal("submit should start exchange")
	}
	if got := strings.TrimSpace(m.input.Value()); got != "" {
		t.Fatalf("input should clear immediately, got %q", got)
	}
}

func TestSubmitShowsInlineProgressInTimeline(t *testing.T) {
	fake := &fakeBackend{
		config:    app.AppConfig{ActiveModel: "fake/model", ActiveProfileID: "student"},
		responses: []ChatResponse{{OK: true, Answer: "ok"}},
	}
	m := NewModel(context.Background(), fake)
	m.width = 100
	m.height = 20
	m.resize()
	m.input.SetValue("сделай summary")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if !m.busy || cmd == nil {
		t.Fatal("submit should start exchange")
	}
	view := m.View()
	for _, want := range []string{"progress", "LLM отвечает", "ожидаю ответ"} {
		if !strings.Contains(view, want) {
			t.Fatalf("busy timeline missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "model call") {
		t.Fatalf("busy state should not be hidden in header as model call:\n%s", view)
	}
}

func TestSubmitLongTaskAnchorsTimelineAtMessageBeginning(t *testing.T) {
	fake := &fakeBackend{
		responses: []ChatResponse{{OK: true, Answer: "ok"}},
	}
	m := NewModel(context.Background(), fake)
	m.width = 100
	m.height = 8
	m.resize()
	for i := 0; i < 12; i++ {
		m.appendEvent("audit", app.StagePlanning, fmt.Sprintf("old event %02d", i), "stale", "info")
	}
	m.updateViewport()
	m.timeline.GotoBottom()
	longTask := "BEGIN_NEW_TASK Спланируй и реши простую LeetCode-задачу Contains Duplicate на Go. " +
		strings.Repeat("Нужны tests для empty, single, duplicate positive, duplicate negative, no duplicate. ", 8) +
		"END_NEW_TASK"
	m.input.SetValue(longTask)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	if !m.busy || cmd == nil {
		t.Fatal("long task submit should start exchange")
	}
	view := m.View()
	if !strings.Contains(view, "BEGIN_NEW_TASK") {
		t.Fatalf("new chat did not show beginning of submitted task:\n%s", view)
	}
	for _, want := range []string{"progress", "LLM отвечает", "ждём ответ"} {
		if !strings.Contains(view, want) {
			t.Fatalf("busy footer missing %q while timeline is anchored:\n%s", want, view)
		}
	}
	for _, blocked := range []string{"END_NEW_TASK", "old event"} {
		if strings.Contains(view, blocked) {
			t.Fatalf("new chat viewport leaked %q instead of submitted task beginning:\n%s", blocked, view)
		}
	}
}

func TestLongUserMessageWrapsWithoutTitleTruncation(t *testing.T) {
	m := NewModel(context.Background(), &fakeBackend{})
	m.width = 100
	m.height = 20
	m.resize()
	message := "Найди GitHub репозитории про mcp server python, сделай короткий отчет и сохрани его в файл."
	m.appendUserEvent(message)

	rendered := strings.Join(m.renderTimelineLines(m.events), "\n")
	if !strings.Contains(rendered, "user you") {
		t.Fatalf("user event should use a stable short title:\n%s", rendered)
	}
	if strings.Contains(rendered, "user you:") || strings.Contains(rendered, "коротки…") || strings.Contains(rendered, "�") {
		t.Fatalf("user event should not truncate or corrupt text in title:\n%s", rendered)
	}
	for _, want := range []string{"Найди GitHub репозитории", "сделай короткий отчет", "сохрани его в файл"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("wrapped user message missing %q:\n%s", want, rendered)
		}
	}
}

func TestExchangeResponseKeepsSubmittedTaskAnchor(t *testing.T) {
	longAnswer := "ASSISTANT_HEAD " + strings.Repeat("response detail ", 80) + "ASSISTANT_TAIL"
	fake := &fakeBackend{
		responses: []ChatResponse{{OK: true, Answer: longAnswer}},
	}
	m := NewModel(context.Background(), fake)
	m.width = 100
	m.height = 8
	m.resize()
	for i := 0; i < 12; i++ {
		m.appendEvent("audit", app.StagePlanning, fmt.Sprintf("old event %02d", i), "stale", "info")
	}
	m.updateViewport()
	m.timeline.GotoBottom()
	longTask := "BEGIN_NEW_EXCHANGE Спланируй и реши простую LeetCode-задачу Contains Duplicate на Go. " +
		strings.Repeat("Нужны tests для разных сценариев. ", 8) +
		"END_NEW_EXCHANGE"
	m.input.SetValue(longTask)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	next, _ = m.Update(exchangeFinishedFromCmd(t, cmd))
	m = next.(Model)

	view := m.View()
	if !strings.Contains(view, "BEGIN_NEW_EXCHANGE") {
		t.Fatalf("exchange response moved viewport away from submitted task beginning:\n%s", view)
	}
	for _, blocked := range []string{"END_NEW_EXCHANGE", "ASSISTANT_TAIL", "old event"} {
		if strings.Contains(view, blocked) {
			t.Fatalf("exchange response viewport leaked %q instead of staying at exchange beginning:\n%s", blocked, view)
		}
	}
}

func TestExchangeCompactsAuditAndEndsWithNextAction(t *testing.T) {
	task := app.TaskState{
		ID:              "task_demo",
		Title:           "Contains Duplicate",
		Stage:           app.StagePlanning,
		ExpectedAction:  app.ExpectedUserConfirmation,
		Status:          app.TaskStatusActive,
		PendingPlanning: &app.PlanningProposalState{ID: "plan_1", Summary: "plan", Plan: []string{"implement"}},
	}
	fake := &fakeBackend{
		responses: []ChatResponse{{
			OK:     true,
			Answer: `{"stage":"planning","summary":"plan ready","readiness":"ready_for_execution_proposal"}`,
			Task:   &task,
			AuditEvents: []process.ProcessAuditEvent{
				{Stage: app.StagePlanning, ActionKind: process.ActionPlanTask, Decision: "semantic_output_call"},
				{Stage: app.StagePlanning, ActionKind: process.ActionPlanTask, Decision: "accepted"},
				{Stage: app.StagePlanning, ActionKind: process.ActionPlanTask, Decision: "provider_call"},
			},
		}},
	}
	m := NewModel(context.Background(), fake)
	m.input.SetValue("plan task")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	next, _ = m.Update(exchangeFinishedFromCmd(t, cmd))
	m = next.(Model)

	if len(m.events) == 0 || m.events[len(m.events)-1].Kind != "next" {
		t.Fatalf("last event should be next action, got %#v", m.events)
	}
	if m.events[len(m.events)-1].Title != "Review pending plan." {
		t.Fatalf("wrong next action: %#v", m.events[len(m.events)-1])
	}
	auditEvents := 0
	for _, event := range m.events {
		if event.Kind == "audit" {
			auditEvents++
		}
		if event.Title == "semantic_output_call" || event.Title == "provider_call" {
			t.Fatalf("raw audit event leaked into timeline: %#v", event)
		}
	}
	if auditEvents != 1 {
		t.Fatalf("audit should be compacted to one summary, got %d events: %#v", auditEvents, m.events)
	}
}

func TestExchangeShowsMCPToolEvents(t *testing.T) {
	fake := &fakeBackend{
		responses: []ChatResponse{{
			OK:     true,
			Answer: "Repository summary ready.",
			AuditEvents: []process.ProcessAuditEvent{
				{Stage: app.StageExecution, Decision: "provider_call"},
				{Stage: app.StageExecution, Decision: "mcp_tool_call", Reason: "github_api__github_repo_info"},
				{Stage: app.StageExecution, Decision: "mcp_tool_result", Reason: "github_api__github_repo_info"},
				{Stage: app.StageExecution, Decision: "accepted"},
			},
		}},
	}
	m := NewModel(context.Background(), fake)
	m.input.SetValue("use github repo tool")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	next, _ = m.Update(exchangeFinishedFromCmd(t, cmd))
	m = next.(Model)

	view := m.View()
	for _, want := range []string{"MCP tool call", "MCP tool result", "github_api__github_repo_info", "Repository summary ready."} {
		if !strings.Contains(view, want) {
			t.Fatalf("timeline missing %q:\n%s", want, view)
		}
	}
}

func TestSummarizeAnswerHidesStructuredControlFields(t *testing.T) {
	answerBytes, err := json.Marshal(map[string]any{
		"stage":        "execution",
		"summary":      "Implemented table-driven tests for ContainsDuplicate.",
		"current_step": "Написать табличные тесты",
		"next_signal":  "continue_execution",
		"deliverable":  "### manual_scratch/day15_contains_duplicate/solution_test.go\n```go\npackage main\n```",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := summarizeAnswer(string(answerBytes))
	for _, blocked := range []string{"stage=", "current_step=", "next_signal=", "deliverable=", "package main"} {
		if strings.Contains(got, blocked) {
			t.Fatalf("structured control field leaked %q in %q", blocked, got)
		}
	}
	for _, want := range []string{"Implemented table-driven tests for ContainsDuplicate.", "Deliverable prepared."} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary missing %q in %q", want, got)
		}
	}
}

func TestMemoryProposalDoesNotHijackApprovalShortcut(t *testing.T) {
	proposal := app.MemoryProposal{ID: "proposal_1", Records: []app.ProposedMemoryRecord{{ID: "rec_1", Status: app.ProposalPending, Layer: app.ProposedLayerWork, Content: "remember task"}}}
	fake := &fakeBackend{proposal: &proposal}
	m := NewModel(context.Background(), fake)
	m.proposal = &proposal
	view := m.View()
	for _, want := range []string{"Optional memory proposal", "Review details", "Save memory", "Hide for now"} {
		if !strings.Contains(view, want) {
			t.Fatalf("memory decision menu missing %q:\n%s", want, view)
		}
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = next.(Model)
	if len(fake.applied) != 0 {
		t.Fatalf("memory apply should require explicit decision, got %#v", fake.applied)
	}
	if strings.TrimSpace(m.input.Value()) != "a" {
		t.Fatalf("plain key should go to input, got %q", m.input.Value())
	}
}

func TestPlanningDecisionMenuApprovesDefault(t *testing.T) {
	task := pendingPlanTask()
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
	view := m.View()
	for _, want := range []string{"Decision", "Pending plan needs your confirmation.", "Approve plan", "Request changes"} {
		if !strings.Contains(view, want) {
			t.Fatalf("planning decision menu missing %q:\n%s", want, view)
		}
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if !m.busy || cmd == nil {
		t.Fatal("enter on default decision should start approval")
	}
	msg := exchangeFinishedFromCmd(t, cmd)
	if msg.err != nil {
		t.Fatalf("approval command failed: %v", msg.err)
	}
	if msg.input != "approve planning" {
		t.Fatalf("wrong decision command input: %q", msg.input)
	}
}

func TestPlanningDecisionMenuRejectsSelectedItem(t *testing.T) {
	task := pendingPlanTask()
	fake := &fakeBackend{
		task: &task,
		responses: []ChatResponse{{
			OK:     true,
			Answer: "rejected",
			Task:   &task,
		}},
	}
	m := NewModel(context.Background(), fake)
	m.task = &task

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(Model)
	if m.decisionCursor != 1 {
		t.Fatalf("down should select reject item, cursor=%d", m.decisionCursor)
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if !m.busy || cmd == nil {
		t.Fatal("enter on selected decision should start rejection")
	}
	msg := exchangeFinishedFromCmd(t, cmd)
	if msg.input != "reject planning" {
		t.Fatalf("wrong decision command input: %q", msg.input)
	}
}

func TestTypingHidesPlanningDecisionMenuForChanges(t *testing.T) {
	task := pendingPlanTask()
	m := NewModel(context.Background(), &fakeBackend{task: &task})
	m.task = &task

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("нужны правки")})
	m = next.(Model)
	if m.busy {
		t.Fatal("typing changes should not submit decision")
	}
	view := m.View()
	if strings.Contains(view, "Pending plan needs your confirmation.") {
		t.Fatalf("decision menu should hide while user types changes:\n%s", view)
	}
	if !strings.Contains(m.input.Value(), "нужны правки") {
		t.Fatalf("typed changes missing from input: %q", m.input.Value())
	}
}

func TestMemoryDecisionMenuReviewsSavesAndHides(t *testing.T) {
	proposal := app.MemoryProposal{ID: "proposal_1", Records: []app.ProposedMemoryRecord{{ID: "rec_1", Status: app.ProposalPending, Layer: app.ProposedLayerWork, Content: "remember task"}}}
	m := NewModel(context.Background(), &fakeBackend{proposal: &proposal})
	m.proposal = &proposal

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil || m.active != PaneMemory {
		t.Fatalf("default memory decision should review details, pane=%v cmd=%v", m.active, cmd)
	}

	fake := &fakeBackend{proposal: &proposal}
	m = NewModel(context.Background(), fake)
	m.proposal = &proposal
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(Model)
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if !m.busy || cmd == nil {
		t.Fatal("save memory decision should call backend")
	}
	next, _ = m.Update(messageFromCmd[memoryAppliedMsg](t, cmd))
	m = next.(Model)
	if m.busy {
		t.Fatal("memory apply completion should clear busy state")
	}
	if len(fake.applied) != 1 || !fake.applied[0].AcceptAll {
		t.Fatalf("memory save did not accept all: %#v", fake.applied)
	}

	m = NewModel(context.Background(), &fakeBackend{proposal: &proposal})
	m.proposal = &proposal
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.hasPendingMemory() && m.decisionMenuActive() {
		t.Fatalf("esc should hide optional memory decision menu:\n%s", m.View())
	}
}

func pendingPlanTask() app.TaskState {
	return app.TaskState{
		ID:              "task_plan",
		Stage:           app.StagePlanning,
		ExpectedAction:  app.ExpectedUserConfirmation,
		Status:          app.TaskStatusActive,
		PendingPlanning: &app.PlanningProposalState{ID: "plan_1", Summary: "plan", Plan: []string{"step"}},
	}
}

func TestPlanningApprovalOnlyAutoContinuesExecution(t *testing.T) {
	executionTask := app.TaskState{
		ID:             "task_exec",
		Stage:          app.StageExecution,
		ExpectedAction: app.ExpectedLLMResponse,
		Status:         app.TaskStatusActive,
	}
	fake := &fakeBackend{}
	m := NewModel(context.Background(), fake)
	msg := exchangeFinishedMsg{resp: ChatResponse{
		OK:     true,
		Answer: "planning proposal approved",
		Task:   &executionTask,
		Transition: &process.TransitionResult{
			Moved: true,
			From:  app.StagePlanning,
			To:    app.StageExecution,
			State: executionTask,
		},
	}}
	next, cmd := m.Update(msg)
	m = next.(Model)
	if !m.busy || cmd == nil {
		t.Fatal("approval-only response should auto-continue execution")
	}
	finished := exchangeFinishedFromCmd(t, cmd)
	if !strings.Contains(finished.input, "Продолжай выполнение") {
		t.Fatalf("wrong auto-continue input: %q", finished.input)
	}
	if len(m.events) == 0 || m.events[len(m.events)-1].Title != "execution started" {
		t.Fatalf("missing execution started event: %#v", m.events)
	}
}

func TestPlanningApprovalValidationFailureDoesNotAutoContinue(t *testing.T) {
	executionTask := app.TaskState{
		ID:             "task_exec",
		Stage:          app.StageExecution,
		ExpectedAction: app.ExpectedLLMResponse,
		Status:         app.TaskStatusActive,
	}
	m := NewModel(context.Background(), &fakeBackend{})
	msg := exchangeFinishedMsg{resp: ChatResponse{
		OK:       true,
		Answer:   "planning proposal approved",
		Task:     &executionTask,
		Warnings: []string{"execution continuation skipped: validation_failed"},
		Transition: &process.TransitionResult{
			Moved: true,
			From:  app.StagePlanning,
			To:    app.StageExecution,
			State: executionTask,
		},
	}}
	next, cmd := m.Update(msg)
	m = next.(Model)
	if m.busy || cmd != nil {
		t.Fatalf("validation failure must not auto-continue: busy=%v cmd=%v", m.busy, cmd != nil)
	}
	title, detail := m.nextAction()
	if title != "Execution blocked." || !strings.Contains(detail, "validation") {
		t.Fatalf("wrong next action: title=%q detail=%q", title, detail)
	}
}

func TestEmptyEnterContinuesExecutionWhenLLMResponseExpected(t *testing.T) {
	task := app.TaskState{
		ID:             "task_exec",
		Stage:          app.StageExecution,
		ExpectedAction: app.ExpectedLLMResponse,
		Status:         app.TaskStatusActive,
	}
	fake := &fakeBackend{}
	m := NewModel(context.Background(), fake)
	m.task = &task
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if !m.busy || cmd == nil {
		t.Fatal("empty enter should continue execution")
	}
	finished := exchangeFinishedFromCmd(t, cmd)
	if !strings.Contains(finished.input, "Продолжай выполнение") {
		t.Fatalf("wrong continuation input: %q", finished.input)
	}
}

func TestEmptyEnterDoesNotContinueAfterValidationFailure(t *testing.T) {
	task := app.TaskState{
		ID:             "task_exec",
		Stage:          app.StageExecution,
		ExpectedAction: app.ExpectedLLMResponse,
		Status:         app.TaskStatusActive,
	}
	m := NewModel(context.Background(), &fakeBackend{})
	m.task = &task
	m.warnings = []string{"execution continuation skipped: validation_failed"}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if m.busy {
		t.Fatal("empty enter must not loop after validation failure")
	}
	for _, event := range m.events {
		if event.Title == "execution continued" {
			t.Fatalf("unexpected execution continuation event: %#v", m.events)
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
	next, _ = m.Update(messageFromCmd[modelsLoadedMsg](t, cmd))
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
	next, _ = m.Update(messageFromCmd[favoriteToggledMsg](t, cmd))
	m = next.(Model)
	if !m.modelPicker.favorites["google/gemini-3.1-flash-lite"] {
		t.Fatalf("favorite not marked: %#v", m.modelPicker.favorites)
	}

	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if !m.busy || cmd == nil {
		t.Fatal("enter should select model")
	}
	next, _ = m.Update(messageFromCmd[modelSelectedMsg](t, cmd))
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
	for _, want := range []string{"Slash commands", "/new", "/resume", "/profile", "/model", "/rag setup", "/process audit", "/exit"} {
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

func TestRAGSlashPendingActionUsesDecisionMenu(t *testing.T) {
	fake := &fakeBackend{config: app.AppConfig{ActiveModel: "fake/model", ActiveProfileID: "student"}}
	m := NewModel(context.Background(), fake)
	m.width = 120
	m.height = 40

	m.applySlashResponse(SlashResponse{PendingRAG: &RAGPendingAction{
		Action: "setup",
		Title:  "Install embeddings and index workspace",
		Detail: "Install/check Ollama, pull bge-m3, run smoke test, index workspace, enable RAG.",
	}})

	view := m.View()
	for _, want := range []string{"RAG approval", "Install/check Ollama", "Approve", "Cancel"} {
		if !strings.Contains(view, want) {
			t.Fatalf("RAG approval view missing %q:\n%s", want, view)
		}
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if !m.busy || cmd == nil {
		t.Fatal("approving RAG action should enter busy state and call backend")
	}
	msg := messageFromCmd[slashFinishedMsg](t, cmd)
	next, _ = m.Update(msg)
	m = next.(Model)
	if len(fake.ragActions) != 1 || fake.ragActions[0].Action != "setup" {
		t.Fatalf("RAG action not confirmed: %+v", fake.ragActions)
	}
	if m.pendingRAG != nil {
		t.Fatalf("pending RAG action should clear after completion: %+v", m.pendingRAG)
	}
}

func TestRAGDeleteRequiresTypedConfirmation(t *testing.T) {
	fake := &fakeBackend{config: app.AppConfig{ActiveModel: "fake/model", ActiveProfileID: "student"}}
	m := NewModel(context.Background(), fake)
	m.width = 120
	m.height = 40

	m.applySlashResponse(SlashResponse{PendingRAG: &RAGPendingAction{
		Action:  "delete",
		Title:   "Delete local RAG stack",
		Detail:  "Type DELETE RAG to confirm.",
		Confirm: "DELETE RAG",
	}})

	view := m.View()
	if !strings.Contains(view, "type DELETE RAG") {
		t.Fatalf("typed confirmation hint missing:\n%s", view)
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil || m.busy || len(fake.ragActions) != 0 {
		t.Fatalf("delete should not run without typed confirmation: busy=%v cmd=%v actions=%v", m.busy, cmd, fake.ragActions)
	}
	if m.pendingRAG == nil {
		t.Fatal("pending RAG delete should remain after missing confirmation")
	}

	m.input.SetValue("DELETE RAG")
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if !m.busy || cmd == nil {
		t.Fatal("typed confirmation should execute RAG delete")
	}
	msg := messageFromCmd[slashFinishedMsg](t, cmd)
	next, _ = m.Update(msg)
	m = next.(Model)
	if len(fake.ragActions) != 1 || fake.ragActions[0].Action != "delete" {
		t.Fatalf("RAG delete not confirmed: %+v", fake.ragActions)
	}
	if m.pendingRAG != nil {
		t.Fatalf("pending RAG delete should clear after completion: %+v", m.pendingRAG)
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
	msg := messageFromCmd[slashFinishedMsg](t, cmd)
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
	msg := messageFromCmd[slashFinishedMsg](t, cmd)
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

func TestFreshStartupShowsActiveModelAndVersionInStatus(t *testing.T) {
	fake := &fakeBackend{
		config: app.AppConfig{ActiveModel: "openai/gpt-4.1-mini", ActiveProfileID: "student"},
		build:  BuildInfo{Version: "1.2.3", Commit: "abcdef123456", BuildDate: "2026-06-21T10:00:00Z"},
	}
	m := NewModel(context.Background(), fake)
	m.width = 120
	m.height = 40
	view := m.View()
	for _, want := range []string{"codingwriter v1.2.3+abcdef123456", "version: v1.2.3+abcdef123456", "model: openai/gpt-4.1-mini", "profile: student", "New chat"} {
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
	next, _ = m.Update(messageFromCmd[modelSelectedMsg](t, cmd))
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

func TestTimelineScrollsAndKeepsManualPosition(t *testing.T) {
	m := NewModel(context.Background(), &fakeBackend{})
	m.width = 80
	m.height = 10
	m.resize()
	for i := 0; i < 30; i++ {
		m.appendEvent("audit", app.StagePlanning, fmt.Sprintf("event-%02d", i), strings.Repeat("details ", 4), "info")
	}
	m.updateViewport()
	if !strings.Contains(m.View(), "event-29") {
		t.Fatalf("timeline did not start at bottom:\n%s", m.View())
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyHome})
	m = next.(Model)
	if !strings.Contains(m.View(), "event-00") {
		t.Fatalf("home did not scroll to top:\n%s", m.View())
	}

	m.appendEvent("audit", app.StagePlanning, "event-30", "new event", "info")
	m.updateViewport()
	if !strings.Contains(m.View(), "event-00") || strings.Contains(m.View(), "event-30") {
		t.Fatalf("manual scroll position was not preserved:\n%s", m.View())
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = next.(Model)
	if !strings.Contains(m.View(), "event-30") {
		t.Fatalf("end did not scroll to bottom:\n%s", m.View())
	}
}

func TestTUIMouseEnabledDefaultsOnWithExplicitOptOut(t *testing.T) {
	t.Setenv("CODINGWRITER_TUI_MOUSE", "")
	if !tuiMouseEnabled() {
		t.Fatal("mouse wheel support should be enabled by default")
	}

	for _, value := range []string{"0", "false", "no", "off"} {
		t.Setenv("CODINGWRITER_TUI_MOUSE", value)
		if tuiMouseEnabled() {
			t.Fatalf("mouse wheel support should be disabled for %q", value)
		}
	}

	for _, value := range []string{"1", "true", "yes", "on", "unexpected"} {
		t.Setenv("CODINGWRITER_TUI_MOUSE", value)
		if !tuiMouseEnabled() {
			t.Fatalf("mouse wheel support should be enabled for %q", value)
		}
	}
}

func TestTimelineMouseWheelScrolls(t *testing.T) {
	m := NewModel(context.Background(), &fakeBackend{})
	m.width = 140
	m.height = 10
	m.resize()
	for i := 0; i < 30; i++ {
		m.appendEvent("audit", app.StagePlanning, fmt.Sprintf("event-%02d", i), strings.Repeat("details ", 3), "info")
	}
	m.updateViewport()
	before := m.timeline.YOffset
	next, _ := m.Update(tea.MouseMsg{X: 2, Y: 3, Type: tea.MouseWheelUp, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	m = next.(Model)
	if m.timeline.YOffset >= before {
		t.Fatalf("mouse wheel did not scroll up: before=%d after=%d", before, m.timeline.YOffset)
	}
}

func TestWideRightPaneMouseWheelScrollsSidebarOnly(t *testing.T) {
	m := NewModel(context.Background(), &fakeBackend{})
	m.width = 140
	m.height = 10
	m.contextExpanded = true
	m.task = longSidebarTask()
	m.resize()
	for i := 0; i < 30; i++ {
		m.appendEvent("audit", app.StagePlanning, fmt.Sprintf("event-%02d", i), strings.Repeat("details ", 3), "info")
	}
	m.updateViewport()
	timelineBefore := m.timeline.YOffset
	sidebarBefore := m.sidebar.YOffset
	next, _ := m.Update(tea.MouseMsg{X: 120, Y: 3, Type: tea.MouseWheelDown, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	m = next.(Model)
	if m.sidebar.YOffset <= sidebarBefore {
		t.Fatalf("right-pane wheel did not scroll sidebar: before=%d after=%d", sidebarBefore, m.sidebar.YOffset)
	}
	if m.timeline.YOffset != timelineBefore {
		t.Fatalf("right-pane wheel should not scroll timeline: before=%d after=%d", timelineBefore, m.timeline.YOffset)
	}
}

func TestWideSidebarKeyboardScrollsWhenInfoPaneActive(t *testing.T) {
	m := NewModel(context.Background(), &fakeBackend{})
	m.width = 140
	m.height = 10
	m.contextExpanded = true
	m.task = longSidebarTask()
	m.appliedArtifacts = []string{"RIGHT_BOTTOM_MARKER"}
	m.active = PanePlan
	m.resize()
	for i := 0; i < 30; i++ {
		m.appendEvent("audit", app.StagePlanning, fmt.Sprintf("event-%02d", i), strings.Repeat("details ", 3), "info")
	}
	m.updateViewport()
	timelineBefore := m.timeline.YOffset
	sidebarBefore := m.sidebar.YOffset

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = next.(Model)
	if m.sidebar.YOffset <= sidebarBefore {
		t.Fatalf("pgdown with info pane active did not scroll sidebar: before=%d after=%d", sidebarBefore, m.sidebar.YOffset)
	}
	if m.timeline.YOffset != timelineBefore {
		t.Fatalf("pgdown with info pane active should not scroll timeline: before=%d after=%d", timelineBefore, m.timeline.YOffset)
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = next.(Model)
	if !strings.Contains(m.sidebar.View(), "RIGHT_BOTTOM_MARKER") {
		t.Fatalf("end did not move sidebar to bottom:\n%s", m.sidebar.View())
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyHome})
	m = next.(Model)
	if m.sidebar.YOffset != 0 {
		t.Fatalf("home did not move sidebar to top: offset=%d", m.sidebar.YOffset)
	}
}

func longSidebarTask() *app.TaskState {
	task := &app.TaskState{
		ID:             "task_sidebar",
		Title:          "Long sidebar task",
		Objective:      "Keep enough right panel content to require independent scrolling.",
		Stage:          app.StageExecution,
		Status:         app.TaskStatusActive,
		ExpectedAction: app.ExpectedUserInput,
	}
	for i := 0; i < 35; i++ {
		task.Plan = append(task.Plan, fmt.Sprintf("sidebar plan item %02d", i))
		task.AcceptanceCriteria = append(task.AcceptanceCriteria, fmt.Sprintf("sidebar criterion %02d", i))
	}
	task.AcceptanceCriteria = append(task.AcceptanceCriteria, "RIGHT_BOTTOM_MARKER")
	return task
}

func TestTimelineMouseWheelScrollsAfterLongExchange(t *testing.T) {
	fake := &fakeBackend{
		responses: []ChatResponse{{
			OK:     true,
			Answer: "assistant answer " + strings.Repeat("with enough wrapped detail to overflow the timeline viewport ", 24),
		}},
	}
	m := NewModel(context.Background(), fake)
	m.width = 100
	m.height = 8
	m.resize()
	m.input.SetValue("BEGIN_SCROLL_EXCHANGE " + strings.Repeat("long task details ", 12))

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	next, _ = m.Update(exchangeFinishedFromCmd(t, cmd))
	m = next.(Model)

	if m.timeline.YOffset != 0 {
		t.Fatalf("test setup expected exchange anchor at top, got offset=%d", m.timeline.YOffset)
	}
	next, _ = m.Update(tea.MouseMsg{X: 2, Y: 3, Type: tea.MouseWheelDown, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	m = next.(Model)
	down := m.timeline.YOffset
	if down <= 0 {
		t.Fatalf("mouse wheel down did not scroll after long exchange: offset=%d", down)
	}
	next, _ = m.Update(tea.MouseMsg{X: 2, Y: 3, Type: tea.MouseWheelUp, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	m = next.(Model)
	if m.timeline.YOffset >= down {
		t.Fatalf("mouse wheel up did not scroll back after long exchange: before=%d after=%d", down, m.timeline.YOffset)
	}
}

func TestWideTimelineWrapsToVisibleLeftPane(t *testing.T) {
	m := NewModel(context.Background(), &fakeBackend{})
	m.width = 120
	m.height = 40
	m.resize()
	if m.timeline.Width != 72 {
		t.Fatalf("wide timeline width should match visible left pane: got %d", m.timeline.Width)
	}
	m.appendEvent("user", app.StagePlanning, "you: BEGIN_WIDE_SCROLL", strings.Repeat("wide timeline text ", 12), "info")
	m.updateViewport()

	wrapped := 0
	for _, line := range strings.Split(m.timeline.View(), "\n") {
		if strings.Contains(line, "wide timeline text") {
			wrapped++
		}
	}
	if wrapped < 3 {
		t.Fatalf("wide timeline content did not wrap inside visible pane, wrapped=%d view=\n%s", wrapped, m.timeline.View())
	}
}

func TestWideViewDoesNotExceedTerminalHeight(t *testing.T) {
	proposal := &app.MemoryProposal{ID: "proposal", Records: []app.ProposedMemoryRecord{}}
	for i := 0; i < 12; i++ {
		proposal.Records = append(proposal.Records, app.ProposedMemoryRecord{
			ID:      fmt.Sprintf("record_%02d", i),
			Layer:   app.ProposedLayerShort,
			Kind:    "context",
			Content: strings.Repeat("memory detail ", 4),
			Status:  app.ProposalPending,
		})
	}
	m := NewModel(context.Background(), &fakeBackend{})
	m.width = 120
	m.height = 20
	m.proposal = proposal
	m.contextExpanded = true
	m.appendEvent("user", app.StagePlanning, "you: long task", strings.Repeat("timeline detail ", 20), "info")
	m.resize()

	viewHeight := lipgloss.Height(m.View())
	if viewHeight > m.height {
		t.Fatalf("wide TUI view overflowed terminal height: got=%d want<=%d\n%s", viewHeight, m.height, m.View())
	}
}

func TestTimelineWrapsStyledSummaryWithoutDroppingBeginning(t *testing.T) {
	m := NewModel(context.Background(), &fakeBackend{})
	m.width = 120
	m.height = 40
	m.resize()
	m.appendEvent("user", app.StagePlanning, "you: BEGIN_STYLED_WRAP", "BEGIN_STYLED_WRAP "+strings.Repeat("table tests и подробный план. ", 18)+"END_STYLED_WRAP", "info")
	m.updateViewport()

	view := m.timeline.View()
	if !strings.Contains(view, "BEGIN_STYLED_WRAP") {
		t.Fatalf("styled summary wrap dropped beginning:\n%s", view)
	}
	if strings.Index(view, "END_STYLED_WRAP") < strings.Index(view, "BEGIN_STYLED_WRAP") {
		t.Fatalf("styled summary order is wrong:\n%s", view)
	}
}

func TestTimelinePreservesMultilineCommandOutput(t *testing.T) {
	m := NewModel(context.Background(), &fakeBackend{})
	m.width = 120
	m.height = 40
	m.resize()
	m.timeline.Width = 92
	m.appendEvent("command", app.StagePlanning, "/mcp", strings.Join([]string{
		"mcp servers:",
		"  github       enabled  stdio  cmd=github-mcp-server args=4",
		"    search_repositories      read    auto",
		"  context7     enabled  stdio  cmd=npx args=2",
		"    get-library-docs         read    auto",
	}, "\n"), "info")

	rendered := strings.Join(m.renderTimelineLines(m.events), "\n")
	if strings.Contains(rendered, "auto context7") {
		t.Fatalf("multiline command output was collapsed:\n%s", rendered)
	}
	if !strings.Contains(rendered, "read    auto\n  context7") {
		t.Fatalf("command output line breaks were not preserved:\n%s", rendered)
	}
}

func TestTimelineUsesDistinctEventAndIdentifierColors(t *testing.T) {
	eventKinds := []string{"user", "assistant", "command", "mcp", "files", "next", "progress", "warning", "error"}
	seenEventColors := map[string]string{}
	for _, kind := range eventKinds {
		color := fmt.Sprint(eventKindStyle(kind).GetForeground())
		if color == "" || color == "250" {
			t.Fatalf("event kind %q uses default/empty color %q", kind, color)
		}
		if previous, ok := seenEventColors[color]; ok {
			t.Fatalf("event kinds %q and %q share color %q", previous, kind, color)
		}
		seenEventColors[color] = kind
	}

	identifiers := []string{"github", "context7", "playwright", "filesystem"}
	seenIdentifierColors := map[string]string{}
	for _, identifier := range identifiers {
		color := string(timelineIdentifierColor(identifier))
		if previous, ok := seenIdentifierColors[color]; ok {
			t.Fatalf("identifiers %q and %q share color %q", previous, identifier, color)
		}
		seenIdentifierColors[color] = identifier
	}

	line := "  github       enabled  stdio  cmd=github-mcp-server args=4"
	rendered := renderEventSummaryLine(timelineEvent{Kind: "command"}, line)
	if lipgloss.Width(rendered) != lipgloss.Width(line) {
		t.Fatalf("color rendering changed visible width: got=%d want=%d line=%q", lipgloss.Width(rendered), lipgloss.Width(line), rendered)
	}
}

func TestTimelineResizeClampsPastBottom(t *testing.T) {
	m := NewModel(context.Background(), &fakeBackend{})
	m.width = 80
	m.height = 8
	m.resize()
	for i := 0; i < 12; i++ {
		m.appendEvent("audit", app.StagePlanning, fmt.Sprintf("event-%02d", i), "detail", "info")
	}
	m.updateViewport()
	m.timeline.GotoBottom()
	m.timeline.Height = 30
	if !m.timeline.PastBottom() {
		t.Fatal("test setup should put timeline past bottom")
	}
	m.height = 30
	m.resize()
	if m.timeline.PastBottom() {
		t.Fatalf("resize left timeline past bottom: offset=%d", m.timeline.YOffset)
	}
	if strings.TrimSpace(m.timeline.View()) == "" {
		t.Fatalf("timeline rendered blank after resize")
	}
}

func TestTimelineShortContentDoesNotAddLeadingPadding(t *testing.T) {
	m := NewModel(context.Background(), &fakeBackend{})
	m.width = 100
	m.height = 20
	m.resize()
	m.appendEvent("assistant", app.StagePlanning, "assistant answer", "short answer", "info")
	m.updateViewport()
	lines := strings.Split(m.timeline.View(), "\n")
	firstContent := -1
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			firstContent = i
			break
		}
	}
	if firstContent != 0 {
		t.Fatalf("short timeline content has leading padding, firstContent=%d view=\n%s", firstContent, m.timeline.View())
	}
}

func TestTimelineSeparatesEventsWithBlankLine(t *testing.T) {
	m := NewModel(context.Background(), &fakeBackend{})
	m.width = 100
	m.height = 20
	m.resize()
	m.appendEvent("user", app.StagePlanning, "you: first", "first detail", "info")
	m.appendEvent("assistant", app.StagePlanning, "assistant answer", "assistant detail", "info")
	lines := m.renderTimelineLines(m.events)
	if len(lines) < 4 {
		t.Fatalf("expected multi-line timeline, got %#v", lines)
	}
	if strings.TrimSpace(lines[2]) != "" {
		t.Fatalf("expected blank separator between events, got %#v", lines)
	}
	if !strings.Contains(lines[3], "assistant answer") {
		t.Fatalf("assistant event should start after separator, got %#v", lines)
	}
}

func TestTimelineBlankViewportFallsBackToNextAction(t *testing.T) {
	task := app.TaskState{
		ID:             "task_exec",
		Stage:          app.StageExecution,
		ExpectedAction: app.ExpectedLLMResponse,
		Status:         app.TaskStatusActive,
	}
	m := NewModel(context.Background(), &fakeBackend{})
	m.width = 140
	m.height = 20
	m.task = &task
	m.timeline.SetContent("")
	m.timeline.Height = 10

	view := m.bodyView()
	for _, want := range []string{"Continue execution.", "Press Enter to run the next approved step.", "task_exec"} {
		if !strings.Contains(view, want) {
			t.Fatalf("fallback view missing %q:\n%s", want, view)
		}
	}
}

func TestTrimAndWrapAreRuneSafe(t *testing.T) {
	input := "Сигнатура функции ContainsDuplicate(nums []int) bool"
	trimmed := trimWidth(input, 18)
	if strings.Contains(trimmed, "�") || strings.Contains(trimmed, "?") {
		t.Fatalf("trimWidth corrupted utf8: %q", trimmed)
	}
	for _, line := range wrap(input, 12) {
		if strings.Contains(line, "�") || strings.Contains(line, "?") {
			t.Fatalf("wrap corrupted utf8: %q", line)
		}
	}
}

func TestNextActionExplainsPendingPlan(t *testing.T) {
	task := app.TaskState{
		ID:              "task_plan",
		Stage:           app.StagePlanning,
		ExpectedAction:  app.ExpectedUserConfirmation,
		Status:          app.TaskStatusActive,
		PendingPlanning: &app.PlanningProposalState{ID: "plan_1", Summary: "plan", Plan: []string{"step"}},
	}
	m := NewModel(context.Background(), &fakeBackend{task: &task})
	m.width = 140
	m.height = 30
	m.task = &task
	m.contextExpanded = true
	m.resize()

	title, detail := m.nextAction()
	if title != "Review pending plan." || !strings.Contains(detail, "decision menu") {
		t.Fatalf("wrong next action: title=%q detail=%q", title, detail)
	}
	view := m.View()
	for _, want := range []string{"Decision", "Pending plan needs your confirmation.", "Approve plan", "Request changes"} {
		if !strings.Contains(view, want) {
			t.Fatalf("next action view missing %q:\n%s", want, view)
		}
	}
}

func TestInputHeightExpandsForLongText(t *testing.T) {
	m := NewModel(context.Background(), &fakeBackend{})
	m.width = 60
	m.height = 20
	m.resize()
	if got := m.inputHeight(); got != 1 {
		t.Fatalf("empty input height=%d want 1", got)
	}
	m.input.SetValue(strings.Repeat("длинный текст ", 12))
	m.resize()
	if got := m.inputHeight(); got <= 1 {
		t.Fatalf("long input did not expand, height=%d", got)
	}
	m.input.SetValue(strings.Repeat("very long text ", 200))
	m.resize()
	if got := m.inputHeight(); got != min(maxInputHeight, m.height-7) {
		t.Fatalf("long input height=%d want terminal-capped max", got)
	}
}

func TestInputShowsDay20DemoPromptWithoutClipping(t *testing.T) {
	m := NewModel(context.Background(), &fakeBackend{})
	m.width = 120
	m.height = 32
	prompt := strings.TrimSpace(`
Собери Day 20 отчет о популярных MCP-серверах для coding agent.

Нужны 4 типа evidence: репозитории, документация, браузерная проверка страницы проекта и сохраненный markdown-файл .data/day20/multi-mcp-report.md.

В конце покажи путь к файлу и фактический порядок вызванных инструментов.
`)
	m.input.SetValue(prompt)
	m.resize()
	view := m.input.View()
	for _, want := range []string{
		"Собери Day 20 отчет",
		"Нужны 4 типа evidence",
		".data/day20/multi-mcp-report.md",
		"фактический порядок",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("input view clipped %q:\n%s", want, view)
		}
	}
}

func TestContextPickersApplyTypedSlashTransitions(t *testing.T) {
	fake := &fakeBackend{config: app.AppConfig{ActiveProfileID: "student"}}
	m := NewModel(context.Background(), fake)
	m.input.SetValue("/resume")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	msg := messageFromCmd[slashFinishedMsg](t, cmd)
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
	msg = messageFromCmd[slashFinishedMsg](t, cmd)
	next, _ = m.Update(msg)
	m = next.(Model)
	if m.sessionID != "session_old" {
		t.Fatalf("session transition not applied: %s", m.sessionID)
	}

	m.input.SetValue("/task")
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	next, _ = m.Update(messageFromCmd[slashFinishedMsg](t, cmd))
	m = next.(Model)
	if m.contextPicker == nil || m.contextPicker.payload.Kind != "tasks" {
		t.Fatalf("task picker missing: %#v", m.contextPicker)
	}
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	next, _ = m.Update(messageFromCmd[slashFinishedMsg](t, cmd))
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
	next, _ = m.Update(messageFromCmd[slashFinishedMsg](t, cmd))
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
	next, _ = m.Update(messageFromCmd[slashFinishedMsg](t, cmd))
	m = next.(Model)
	if fake.config.ActiveProfileID != "custom" {
		t.Fatalf("profile not created/selected: %+v", fake.config)
	}
}

func TestResumeLoadsFullTranscriptIntoTimeline(t *testing.T) {
	fake := &fakeBackend{
		config: app.AppConfig{ActiveProfileID: "student"},
		transcript: []TranscriptEntry{
			{Role: app.RoleUser, Content: "first user request", CreatedAt: time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)},
			{Role: app.RoleAssistant, Content: "first assistant answer with full details", CreatedAt: time.Date(2026, 6, 21, 10, 1, 0, 0, time.UTC)},
			{Role: app.RoleUser, Content: "second user request that must stay visible", CreatedAt: time.Date(2026, 6, 21, 10, 2, 0, 0, time.UTC)},
			{Role: app.RoleAssistant, Content: "second assistant answer", CreatedAt: time.Date(2026, 6, 21, 10, 3, 0, 0, time.UTC)},
		},
	}
	m := NewModel(context.Background(), fake)
	m.appendEvent("user", "", "old current chat event", "must be cleared on resume", "info")
	m.sessionID = "session_old"
	msg := m.loadSessionContext("session_old")().(initialLoadedMsg)
	next, _ := m.Update(msg)
	m = next.(Model)
	view := m.View()
	for _, want := range []string{"first user request", "first assistant answer with full details", "second user request that must stay visible", "second assistant answer"} {
		if !strings.Contains(view, want) {
			t.Fatalf("resumed transcript missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "must be cleared on resume") {
		t.Fatalf("resume mixed old current timeline with transcript:\n%s", view)
	}
}

func TestResumeAnchorsLongTranscriptAtBeginning(t *testing.T) {
	transcript := []TranscriptEntry{
		{Role: app.RoleUser, Content: "BEGIN_CHAT_MARKER Спланируй и реши задачу полностью", CreatedAt: time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)},
	}
	for i := 0; i < 24; i++ {
		transcript = append(transcript, TranscriptEntry{
			Role:      app.RoleAssistant,
			Content:   fmt.Sprintf("middle assistant event %02d with enough text to fill the viewport", i),
			CreatedAt: time.Date(2026, 6, 21, 10, i+1, 0, 0, time.UTC),
		})
	}
	transcript = append(transcript, TranscriptEntry{
		Role:      app.RoleAssistant,
		Content:   "END_CHAT_MARKER final answer at the bottom",
		CreatedAt: time.Date(2026, 6, 21, 11, 0, 0, 0, time.UTC),
	})
	fake := &fakeBackend{
		config:     app.AppConfig{ActiveProfileID: "student"},
		transcript: transcript,
		audit: []process.ProcessAuditEvent{
			{SessionID: "session_old", Decision: "rejected", ValidatorErrors: []string{"ready_for_validation requires trusted evidence"}},
		},
	}
	m := NewModel(context.Background(), fake)
	m.width = 100
	m.height = 10
	m.resize()
	for i := 0; i < 10; i++ {
		m.appendEvent("audit", app.StageExecution, fmt.Sprintf("old event %02d", i), "stale", "info")
	}
	m.updateViewport()
	m.timeline.GotoBottom()

	msg := m.loadSessionContext("session_old")().(initialLoadedMsg)
	next, _ := m.Update(msg)
	m = next.(Model)

	if m.timeline.YOffset != 0 {
		t.Fatalf("resume should anchor transcript at top, got offset=%d", m.timeline.YOffset)
	}
	view := m.View()
	if !strings.Contains(view, "BEGIN_CHAT_MARKER") {
		t.Fatalf("resume did not show beginning of transcript:\n%s", view)
	}
	for _, blocked := range []string{"END_CHAT_MARKER", "old event", "restored audit history"} {
		if strings.Contains(view, blocked) {
			t.Fatalf("resume viewport leaked %q instead of chat beginning:\n%s", blocked, view)
		}
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
	task := &app.TaskState{ID: "task_old", Title: "old task", Stage: app.StageExecution, Status: app.TaskStatusPaused, ExpectedAction: app.ExpectedLLMResponse}
	fake := &fakeBackend{
		proposal: proposal,
		task:     task,
		audit: []process.ProcessAuditEvent{
			{SessionID: "session_old", Decision: "rejected", ValidatorErrors: []string{"old"}},
		},
	}
	m := NewModel(context.Background(), fake)
	msg := m.loadInitial()().(initialLoadedMsg)
	if msg.mode != "startup" {
		t.Fatalf("startup load mode mismatch: %q", msg.mode)
	}
	if msg.task != nil || len(msg.audit) != 0 || msg.proposal != nil || fake.auditCalls != 0 {
		t.Fatalf("startup should not load old task/chat context: task=%v audit=%d proposal=%v auditCalls=%d", msg.task, len(msg.audit), msg.proposal, fake.auditCalls)
	}
}

func TestFreshStartupDoesNotAttachOldCurrentTask(t *testing.T) {
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
	msg := m.loadInitial()().(initialLoadedMsg)
	next, _ := m.Update(msg)
	m = next.(Model)
	view := m.View()
	for _, blocked := range []string{"task_current", "Contains Duplicate", "OLD_OBJECTIVE_MARKER", "OLD_PLAN_MARKER", "task focus:", "Continue execution.", "Task is paused."} {
		if strings.Contains(view, blocked) {
			t.Fatalf("fresh startup leaked old task marker %q:\n%s", blocked, view)
		}
	}
	for _, want := range []string{"New chat", "old chat: /resume", "task details: /task", "task: none", "Type a coding task."} {
		if !strings.Contains(view, want) {
			t.Fatalf("fresh startup missing %q:\n%s", want, view)
		}
	}
}

func TestFreshStartupExchangeIgnoresOldCurrentTask(t *testing.T) {
	task := &app.TaskState{
		ID:             "task_current",
		Title:          "Contains Duplicate",
		Stage:          app.StageExecution,
		Status:         app.TaskStatusActive,
		ExpectedAction: app.ExpectedLLMResponse,
	}
	fake := &fakeBackend{task: task, responses: []ChatResponse{{OK: true, Answer: "ok"}}}
	m := NewModel(context.Background(), fake)
	m.width = 120
	m.height = 30
	m.resize()
	next, _ := m.Update(m.loadInitial()().(initialLoadedMsg))
	m = next.(Model)
	if m.task != nil {
		t.Fatalf("fresh startup should not attach task: %+v", m.task)
	}
	m.input.SetValue("Найди GitHub репозитории про mcp server python")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("submit should start exchange")
	}
	_ = cmd()
	if len(fake.requests) != 1 || !fake.requests[0].IgnoreCurrentTask {
		t.Fatalf("fresh TUI exchange should ignore hidden old current task: %+v", fake.requests)
	}
}

func TestSelectedTaskExchangeKeepsCurrentTask(t *testing.T) {
	fake := &fakeBackend{responses: []ChatResponse{{OK: true, Answer: "ok"}}}
	m := NewModel(context.Background(), fake)
	m.width = 120
	m.height = 30
	m.resize()
	task := app.TaskState{ID: "task_selected", Stage: app.StagePlanning, Status: app.TaskStatusActive}
	m.task = &task
	m.input.SetValue("продолжай")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("submit should start exchange")
	}
	_ = cmd()
	if len(fake.requests) != 1 || fake.requests[0].IgnoreCurrentTask {
		t.Fatalf("selected task exchange should keep task context: %+v", fake.requests)
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
	if m.task != nil {
		t.Fatalf("/new should clear task focus, got %+v", m.task)
	}
	if cmd == nil {
		t.Fatal("/new should refresh current state without loading history")
	}
	if !teaBatchContainsType(cmd, "clearScreenMsg") {
		t.Fatal("/new should request terminal clear to remove stale slash help")
	}
	view := m.View()
	for _, blocked := range []string{"restored audit history", "validation blocked: trusted evidence required", "task_current", "OLD_OBJECTIVE_MARKER", "OLD_CRITERIA_MARKER", "OLD_PLAN_MARKER"} {
		if strings.Contains(view, blocked) {
			t.Fatalf("/new leaked old context %q:\n%s", blocked, view)
		}
	}
	for _, want := range []string{"New chat", "old chat: /resume", "task details: /task", "task: none"} {
		if !strings.Contains(view, want) {
			t.Fatalf("/new fresh view missing %q:\n%s", want, view)
		}
	}
}

func TestResumeDoesNotAttachUnrelatedCurrentTask(t *testing.T) {
	task := &app.TaskState{
		ID:            "task_unrelated",
		Title:         "Unrelated task",
		Stage:         app.StageExecution,
		Status:        app.TaskStatusActive,
		LastSessionID: "session_other",
	}
	fake := &fakeBackend{
		task: task,
		transcript: []TranscriptEntry{
			{Role: app.RoleUser, Content: "selected session request", CreatedAt: time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)},
		},
		audit: []process.ProcessAuditEvent{
			{SessionID: "session_old", Decision: "provider_call"},
			{SessionID: "session_other", TaskID: "task_unrelated", Decision: "rejected"},
		},
	}
	m := NewModel(context.Background(), fake)
	msg := m.loadSessionContext("session_old")().(initialLoadedMsg)
	next, _ := m.Update(msg)
	m = next.(Model)
	view := m.View()
	for _, blocked := range []string{"task_unrelated", "Unrelated task", "Task is paused.", "Continue execution."} {
		if strings.Contains(view, blocked) {
			t.Fatalf("resume leaked unrelated current task marker %q:\n%s", blocked, view)
		}
	}
	if !strings.Contains(view, "selected session request") {
		t.Fatalf("resume did not show selected session transcript:\n%s", view)
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

func exchangeFinishedFromCmd(t *testing.T, cmd tea.Cmd) exchangeFinishedMsg {
	t.Helper()
	return messageFromCmd[exchangeFinishedMsg](t, cmd)
}

func messageFromCmd[T any](t *testing.T, cmd tea.Cmd) T {
	t.Helper()
	var zero T
	msg := cmd()
	if typed, ok := msg.(T); ok {
		return typed
	}
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("command returned %T, want %T", msg, zero)
	}
	for _, child := range batch {
		if child == nil {
			continue
		}
		if typed, ok := child().(T); ok {
			return typed
		}
	}
	t.Fatalf("batch did not contain %T: %T", zero, msg)
	return zero
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
