package tui

import (
	"context"
	"errors"
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
func (f *fakeBackend) Exchange(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	if len(f.responses) == 0 {
		return ChatResponse{}, errors.New("missing fake response")
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, nil
}
func (f *fakeBackend) Slash(ctx context.Context, sessionID, line string) (SlashResponse, error) {
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

func runTeaCommand(cmd tea.Cmd) {
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, child := range batch {
			runTeaCommand(child)
		}
	}
}
