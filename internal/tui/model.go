package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/process"
)

type Pane int

const (
	PaneTimeline Pane = iota
	PanePlan
	PaneEvidence
	PaneMemory
	PaneFiles
)

type timelineEvent struct {
	At       time.Time
	Kind     string
	Stage    app.TaskStage
	Title    string
	Summary  string
	Detail   string
	Severity string
}

type modelPickerItem struct {
	ID       string
	Provider string
}

type modelPickerState struct {
	models    []string
	favorites map[string]bool
	active    string
	query     string
	cursor    int
	warning   string
	items     []modelPickerItem
}

type contextPickerState struct {
	payload        PickerPayload
	cursor         int
	profileInput   bool
	profileID      string
	restoreConfirm bool
}

type Model struct {
	ctx       context.Context
	backend   Backend
	sessionID string
	startedAt time.Time

	width  int
	height int

	input    textinput.Model
	timeline viewport.Model
	spinner  spinner.Model
	active   Pane
	busy     bool

	task             *app.TaskState
	proposal         *app.MemoryProposal
	evidence         []EvidenceView
	events           []timelineEvent
	audit            []process.ProcessAuditEvent
	warnings         []string
	appliedArtifacts []string
	err              *app.Error
	modelPicker      *modelPickerState
	contextPicker    *contextPickerState
}

type initialLoadedMsg struct {
	task     *app.TaskState
	audit    []process.ProcessAuditEvent
	proposal *app.MemoryProposal
	err      error
}

type exchangeFinishedMsg struct {
	input string
	resp  ChatResponse
	err   error
}

type slashFinishedMsg struct {
	line string
	resp SlashResponse
	err  error
}

type memoryAppliedMsg struct {
	err error
}

type taskActionMsg struct {
	task app.TaskState
	err  error
}

type modelsLoadedMsg struct {
	catalog ModelCatalog
	err     error
}

type modelSelectedMsg struct {
	config app.AppConfig
	model  string
	err    error
}

type favoriteToggledMsg struct {
	config app.AppConfig
	model  string
	err    error
}

func Run(ctx context.Context, backend Backend, in io.Reader, out io.Writer) error {
	m := NewModel(ctx, backend)
	opts := []tea.ProgramOption{tea.WithInput(in), tea.WithOutput(out)}
	_, err := tea.NewProgram(m, opts...).Run()
	return err
}

func NewModel(ctx context.Context, backend Backend) Model {
	ti := textinput.New()
	ti.Focus()
	ti.Prompt = "> "
	ti.Placeholder = "Опишите задачу..."
	ti.CharLimit = 4096
	ti.Width = 80

	sp := spinner.New()
	sp.Spinner = spinner.Line

	m := Model{
		ctx:       ctx,
		backend:   backend,
		sessionID: app.NewID("session"),
		startedAt: time.Now().UTC(),
		width:     120,
		height:    40,
		input:     ti,
		timeline:  viewport.New(80, 20),
		spinner:   sp,
		active:    PaneTimeline,
	}
	m.resize()
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadInitial(), m.spinner.Tick)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Width > 0 && msg.Height > 0 {
			m.width, m.height = msg.Width, msg.Height
			m.resize()
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
	case initialLoadedMsg:
		if msg.err != nil {
			m.err = app.AsError(msg.err)
			m.appendEvent("error", "", "startup failed", m.err.Message, "error")
		}
		m.task = msg.task
		m.audit = msg.audit
		m.proposal = msg.proposal
		m.rebuildFromState()
	case exchangeFinishedMsg:
		m.busy = false
		m.input.SetValue("")
		m.input.Focus()
		if msg.err != nil {
			m.err = app.AsError(msg.err)
			m.appendEvent("error", "", m.err.Code, m.err.Message, "error")
			break
		}
		m.err = nil
		m.applyResponse(msg.resp)
	case slashFinishedMsg:
		m.busy = false
		m.input.SetValue("")
		m.input.Focus()
		if msg.err != nil {
			m.err = app.AsError(msg.err)
			m.appendEvent("error", "", m.err.Code, m.err.Message, "error")
			break
		}
		m.applySlashResponse(msg.resp)
		if strings.TrimSpace(msg.resp.Output) != "" {
			m.appendEvent("command", stageOfTask(m.task), msg.line, strings.TrimSpace(msg.resp.Output), "info")
		}
		if msg.resp.Done {
			return m, tea.Quit
		}
		cmds = append(cmds, m.loadInitial())
	case memoryAppliedMsg:
		if msg.err != nil {
			m.err = app.AsError(msg.err)
			m.appendEvent("error", "", m.err.Code, m.err.Message, "error")
		} else {
			m.appendEvent("memory", stageOfTask(m.task), "memory proposal updated", "proposal records applied/rejected", "info")
			cmds = append(cmds, m.loadInitial())
		}
	case taskActionMsg:
		if msg.err != nil {
			m.err = app.AsError(msg.err)
			m.appendEvent("error", "", m.err.Code, m.err.Message, "error")
		} else {
			m.task = &msg.task
			m.appendEvent("task", msg.task.Stage, "task updated", fmt.Sprintf("status=%s expected=%s", msg.task.Status, msg.task.ExpectedAction), "info")
		}
	case modelsLoadedMsg:
		m.busy = false
		m.input.SetValue("")
		m.input.Focus()
		if msg.err != nil {
			m.err = app.AsError(msg.err)
			m.appendEvent("error", "", m.err.Code, m.err.Message, "error")
			break
		}
		m.err = nil
		if m.modelPicker != nil {
			m.modelPicker.merge(msg.catalog)
		}
	case modelSelectedMsg:
		m.busy = false
		m.input.Focus()
		if msg.err != nil {
			m.err = app.AsError(msg.err)
			m.appendEvent("error", "", m.err.Code, m.err.Message, "error")
			break
		}
		m.err = nil
		m.modelPicker = nil
		m.appendEvent("model", stageOfTask(m.task), "active model", msg.config.ActiveModel, "info")
	case favoriteToggledMsg:
		if msg.err != nil {
			m.err = app.AsError(msg.err)
			m.appendEvent("error", "", m.err.Code, m.err.Message, "error")
			break
		}
		m.err = nil
		if m.modelPicker != nil {
			m.modelPicker.favorites = favoriteMap(msg.config.FavoriteModels)
			m.modelPicker.rebuild()
		}
	case tea.KeyMsg:
		if m.contextPicker != nil {
			next, cmd := m.updateContextPicker(msg)
			m = next
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			break
		}
		if m.modelPicker != nil {
			next, cmd := m.updateModelPicker(msg)
			m = next
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			break
		}
		if m.busy {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			}
			break
		}
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "tab":
			m.active = (m.active + 1) % 5
		case "shift+tab":
			m.active = (m.active + 4) % 5
		case "enter":
			text := strings.TrimSpace(m.input.Value())
			if text == "" {
				if m.hasPendingPlan() {
					m.busy = true
					m.input.Blur()
					cmds = append(cmds, m.approvePlan())
				} else if m.hasPendingMemory() {
					cmds = append(cmds, m.applyMemory(true))
				}
				break
			}
			if text == "/exit" || text == "/quit" {
				return m, tea.Quit
			}
			m.appendEvent("user", stageOfTask(m.task), "user input", text, "info")
			m.busy = true
			m.input.Blur()
			if text == "/model" {
				m.modelPicker = newModelPickerState(ModelCatalog{
					Models:    fallbackModelIDs(),
					Favorites: m.backend.Config().FavoriteModels,
					Active:    m.backend.Config().ActiveModel,
					Warning:   "loading provider model list...",
				})
				cmds = append(cmds, m.loadModels())
			} else if strings.HasPrefix(text, "/") {
				cmds = append(cmds, m.runSlash(text))
			} else {
				cmds = append(cmds, m.runExchange(text))
			}
		case "a", "A", "y", "Y", "ф", "Ф", "н", "Н":
			if m.hasPendingPlan() {
				m.busy = true
				m.input.Blur()
				cmds = append(cmds, m.approvePlan())
			} else if m.hasPendingMemory() {
				cmds = append(cmds, m.applyMemory(true))
			}
		case "r", "R", "n", "N", "к", "К", "т", "Т":
			if m.hasPendingPlan() {
				m.busy = true
				m.input.Blur()
				cmds = append(cmds, m.rejectPlan())
			} else if m.hasPendingMemory() {
				cmds = append(cmds, m.applyMemory(false))
			}
		case "p":
			if m.task != nil && m.task.Status == app.TaskStatusPaused {
				cmds = append(cmds, m.resumeTask())
			} else if m.task != nil && m.task.ID != "" && m.task.Stage != app.StageDone {
				cmds = append(cmds, m.pauseTask())
			}
		}
	}

	if !m.busy && m.modelPicker == nil && m.contextPicker == nil {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		cmds = append(cmds, cmd)
	}
	m.updateViewport()
	return m, tea.Batch(cmds...)
}

func (m Model) View() string {
	if m.width == 0 {
		return "codingwriter tui\n"
	}
	header := m.headerView()
	body := m.bodyView()
	footer := m.footerView()
	input := m.input.View()
	if m.modelPicker != nil {
		body = m.modelPickerView()
		input = ""
	} else if m.contextPicker != nil {
		body = m.contextPickerView()
		input = ""
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer, input)
}

func (m *Model) resize() {
	inputHeight := 1
	headerHeight := 2
	footerHeight := 1
	bodyHeight := max(4, m.height-headerHeight-footerHeight-inputHeight)
	m.input.Width = max(20, m.width-2)
	m.timeline.Width = max(20, m.width-2)
	m.timeline.Height = bodyHeight
	m.updateViewport()
}

func (m Model) headerView() string {
	cfg := m.backend.Config()
	task := "none"
	stage := "-"
	expected := "-"
	status := "-"
	if m.task != nil && m.task.ID != "" {
		task = shortID(m.task.ID)
		stage = string(m.task.Stage)
		expected = string(m.task.ExpectedAction)
		status = string(m.task.Status)
	}
	busy := ""
	if m.busy {
		busy = " " + m.spinner.View() + " model call"
	}
	title := styleTitle().Render("codingwriter")
	line := fmt.Sprintf("%s | model=%s | profile=%s | task=%s | stage=%s | expected=%s | status=%s%s",
		title, emptyDash(cfg.ActiveModel), emptyDash(cfg.ActiveProfileID), task, stage, expected, status, busy)
	return trimWidth(line, m.width) + "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("63")).Render(strings.Repeat("─", max(1, m.width)))
}

func (m Model) bodyView() string {
	if m.width >= 120 {
		leftW := m.width * 60 / 100
		rightW := m.width - leftW - 1
		left := lipgloss.NewStyle().Width(leftW).Height(m.timeline.Height).Render(m.timeline.View())
		right := lipgloss.NewStyle().Width(rightW).Height(m.timeline.Height).Render(m.sidebarView(rightW))
		return lipgloss.JoinHorizontal(lipgloss.Top, left, "│", right)
	}
	switch m.active {
	case PanePlan:
		return m.planView(m.width)
	case PaneEvidence:
		return m.evidenceView(m.width)
	case PaneMemory:
		return m.memoryView(m.width)
	case PaneFiles:
		return m.filesView(m.width)
	default:
		return m.timeline.View()
	}
}

func (m Model) sidebarView(width int) string {
	sections := []string{
		m.statusView(width),
		m.planView(width),
		m.evidenceView(width),
		m.memoryView(width),
		m.filesView(width),
	}
	return strings.Join(sections, "\n")
}

func (m Model) footerView() string {
	if m.contextPicker != nil {
		line := m.contextPicker.footer()
		return styleHint().Render(trimWidth(line, m.width))
	}
	if m.modelPicker != nil {
		line := "model picker | type search | ↑/↓ move | enter select | F favorite | esc close"
		if m.modelPicker.warning != "" {
			line += " | " + m.modelPicker.warning
		}
		return styleHint().Render(trimWidth(line, m.width))
	}
	pane := []string{"timeline", "plan", "evidence", "memory", "files"}[m.active]
	approval := ""
	if m.hasPendingPlan() {
		approval = " | approval: a approve, r reject"
	} else if m.hasPendingMemory() {
		approval = " | memory: a accept all, r reject all"
	}
	line := fmt.Sprintf("tab pane=%s | enter send | /exit quit | p pause/resume%s", pane, approval)
	if m.err != nil && m.err.Hint != "" {
		line += " | hint: " + m.err.Hint
	}
	return styleHint().Render(trimWidth(line, m.width))
}

func (m Model) statusView(width int) string {
	lines := []string{section("Status")}
	if m.task == nil || m.task.ID == "" {
		lines = append(lines, "task: none", "stage: -", "expected: -")
		return boxed(width, lines)
	}
	lines = append(lines,
		"title: "+safe(m.task.Title),
		"id: "+shortID(m.task.ID),
		"stage: "+string(m.task.Stage),
		"expected: "+string(m.task.ExpectedAction),
		"status: "+string(m.task.Status),
		"step: "+safe(m.task.CurrentStep),
		fmt.Sprintf("evidence: %d", len(m.task.ValidationEvidence)),
	)
	if m.task.PendingPlanning != nil {
		lines = append(lines, "pending: planning approval")
	}
	if m.proposal != nil && pendingProposalRecords(*m.proposal) > 0 {
		lines = append(lines, fmt.Sprintf("pending memory: %d", pendingProposalRecords(*m.proposal)))
	}
	return boxed(width, lines)
}

func (m Model) planView(width int) string {
	lines := []string{section("Plan")}
	if m.task == nil || m.task.ID == "" {
		lines = append(lines, "no active task")
		return boxed(width, lines)
	}
	if m.task.PendingPlanning != nil {
		pp := m.task.PendingPlanning
		lines = append(lines, "Pending plan:", safe(pp.Summary))
		for i, item := range pp.Plan {
			lines = append(lines, fmt.Sprintf("%d. %s", i+1, safe(item)))
		}
		lines = append(lines, "[a] approve  [r] reject")
	} else {
		if m.task.Objective != "" {
			lines = append(lines, "Objective: "+safe(m.task.Objective))
		}
		for i, item := range m.task.Plan {
			lines = append(lines, fmt.Sprintf("%d. %s", i+1, safe(item)))
		}
	}
	if len(m.task.AcceptanceCriteria) > 0 {
		lines = append(lines, "Criteria:")
		for _, c := range m.task.AcceptanceCriteria {
			lines = append(lines, "- "+safe(c))
		}
	}
	if len(m.task.Microtasks) > 0 {
		lines = append(lines, "Microtasks:")
		for _, mt := range m.task.Microtasks {
			lines = append(lines, fmt.Sprintf("- %s: %s", safe(mt.Role), safe(mt.Status)))
		}
	}
	return boxed(width, lines)
}

func (m Model) evidenceView(width int) string {
	lines := []string{section("Evidence")}
	if len(m.evidence) == 0 {
		if m.task != nil && len(m.task.ValidationEvidence) > 0 {
			for _, ref := range m.task.ValidationEvidence {
				lines = append(lines, ref)
			}
		} else {
			lines = append(lines, "no trusted evidence yet")
		}
		return boxed(width, lines)
	}
	for _, ev := range m.evidence {
		lines = append(lines, fmt.Sprintf("%s exit=%d", safe(ev.Command), ev.ExitCode), "id: "+safe(firstNonEmpty(ev.ID, ev.Ref)))
		if ev.OutputPreview != "" {
			lines = append(lines, safe(ev.OutputPreview))
		}
	}
	return boxed(width, lines)
}

func (m Model) memoryView(width int) string {
	lines := []string{section("Memory proposal")}
	if m.proposal == nil || len(m.proposal.Records) == 0 {
		lines = append(lines, "no pending proposal")
		return boxed(width, lines)
	}
	for _, r := range m.proposal.Records {
		lines = append(lines, fmt.Sprintf("%s %s %s", r.ID, r.Layer, r.Status))
		lines = append(lines, safe(r.Content))
	}
	if pendingProposalRecords(*m.proposal) > 0 {
		lines = append(lines, "[a] accept all  [r] reject all")
	}
	return boxed(width, lines)
}

func (m Model) filesView(width int) string {
	lines := []string{section("Files")}
	if len(m.appliedArtifacts) == 0 {
		lines = append(lines, "no applied artifacts yet", "diff preview: not available in P0")
		return boxed(width, lines)
	}
	for _, file := range m.appliedArtifacts {
		lines = append(lines, "applied: "+safe(file))
	}
	lines = append(lines, "diff preview: placeholder")
	return boxed(width, lines)
}

func (m *Model) loadInitial() tea.Cmd {
	return func() tea.Msg {
		var msg initialLoadedMsg
		if task, ok, err := m.backend.CurrentTask(); err != nil {
			msg.err = err
		} else if ok {
			msg.task = &task
		}
		if audit, err := m.backend.LatestAudit(80); err == nil {
			msg.audit = audit
		}
		if proposal, ok, err := m.backend.LatestPendingProposal(m.ctx, ""); err == nil && ok {
			msg.proposal = &proposal
		}
		return msg
	}
}

func (m Model) loadModels() tea.Cmd {
	return func() tea.Msg {
		catalog, err := m.backend.ListModels(m.ctx)
		return modelsLoadedMsg{catalog: catalog, err: err}
	}
}

func (m Model) selectModel(modelID string) tea.Cmd {
	return func() tea.Msg {
		cfg, err := m.backend.SelectModel(m.ctx, modelID)
		return modelSelectedMsg{config: cfg, model: modelID, err: err}
	}
}

func (m Model) selectSession(sessionID string) tea.Cmd {
	return func() tea.Msg {
		resp, err := m.backend.SelectSession(m.ctx, sessionID)
		return slashFinishedMsg{line: "/resume " + sessionID, resp: resp, err: err}
	}
}

func (m Model) selectTask(taskID string) tea.Cmd {
	sessionID := m.sessionID
	return func() tea.Msg {
		resp, err := m.backend.SelectTask(m.ctx, taskID, sessionID)
		return slashFinishedMsg{line: "/task " + taskID, resp: resp, err: err}
	}
}

func (m Model) clearTask() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.backend.ClearTask(m.ctx)
		return slashFinishedMsg{line: "/task close", resp: resp, err: err}
	}
}

func (m Model) archiveTask(taskID string) tea.Cmd {
	return func() tea.Msg {
		resp, err := m.backend.ArchiveTask(m.ctx, taskID)
		return slashFinishedMsg{line: "/task archive " + taskID, resp: resp, err: err}
	}
}

func (m Model) restoreTask(taskID string) tea.Cmd {
	sessionID := m.sessionID
	return func() tea.Msg {
		resp, err := m.backend.RestoreTask(m.ctx, taskID, sessionID)
		return slashFinishedMsg{line: "/task restore " + taskID, resp: resp, err: err}
	}
}

func (m Model) selectProfile(profileID string) tea.Cmd {
	return func() tea.Msg {
		resp, err := m.backend.SelectProfile(m.ctx, profileID)
		return slashFinishedMsg{line: "/profile " + profileID, resp: resp, err: err}
	}
}

func (m Model) createProfile(profileID string) tea.Cmd {
	return func() tea.Msg {
		resp, err := m.backend.CreateProfile(m.ctx, profileID)
		return slashFinishedMsg{line: "/profile create " + profileID, resp: resp, err: err}
	}
}

func (m Model) toggleFavoriteModel(modelID string) tea.Cmd {
	return func() tea.Msg {
		cfg, err := m.backend.ToggleFavoriteModel(m.ctx, modelID)
		return favoriteToggledMsg{config: cfg, model: modelID, err: err}
	}
}

func (m Model) runExchange(text string) tea.Cmd {
	sessionID := m.sessionID
	return func() tea.Msg {
		resp, err := m.backend.Exchange(m.ctx, ChatRequest{SessionID: sessionID, Input: text, RequireMemoryProposal: true})
		return exchangeFinishedMsg{input: text, resp: resp, err: err}
	}
}

func (m Model) runSlash(line string) tea.Cmd {
	sessionID := m.sessionID
	return func() tea.Msg {
		resp, err := m.backend.Slash(m.ctx, sessionID, line)
		return slashFinishedMsg{line: line, resp: resp, err: err}
	}
}

func (m *Model) applySlashResponse(resp SlashResponse) {
	if resp.ActiveSessionID != "" {
		m.sessionID = resp.ActiveSessionID
	}
	if resp.TaskCleared {
		m.task = nil
	}
	if resp.ActiveTask != nil {
		m.task = resp.ActiveTask
	}
	if resp.ActiveConfig != nil {
		// The authoritative config lives in the backend. This branch exists so
		// slash responses can carry the typed transition; header reads backend
		// config on the next render.
	}
	if resp.Picker != nil {
		m.contextPicker = newContextPickerState(*resp.Picker)
	} else {
		m.contextPicker = nil
	}
}

func (m Model) approvePlan() tea.Cmd {
	sessionID := m.sessionID
	return func() tea.Msg {
		resp, err := m.backend.ApprovePlanning(m.ctx, sessionID)
		return exchangeFinishedMsg{input: "approve planning", resp: resp, err: err}
	}
}

func (m Model) rejectPlan() tea.Cmd {
	sessionID := m.sessionID
	return func() tea.Msg {
		resp, err := m.backend.RejectPlanning(m.ctx, sessionID)
		return exchangeFinishedMsg{input: "reject planning", resp: resp, err: err}
	}
}

func (m Model) applyMemory(accept bool) tea.Cmd {
	sessionID := m.sessionID
	taskID := ""
	if m.task != nil {
		taskID = m.task.ID
	}
	proposalID := "latest"
	if m.proposal != nil && m.proposal.ID != "" {
		proposalID = m.proposal.ID
	}
	return func() tea.Msg {
		_, err := m.backend.ApplyMemory(m.ctx, MemoryApplyRequest{SessionID: sessionID, TaskID: taskID, ProposalID: proposalID, AcceptAll: accept, RejectAll: !accept})
		return memoryAppliedMsg{err: err}
	}
}

func (m Model) pauseTask() tea.Cmd {
	return func() tea.Msg {
		task, err := m.backend.PauseTask()
		return taskActionMsg{task: task, err: err}
	}
}

func (m Model) resumeTask() tea.Cmd {
	return func() tea.Msg {
		task, err := m.backend.ResumeTask()
		return taskActionMsg{task: task, err: err}
	}
}

func (m *Model) applyResponse(resp ChatResponse) {
	if resp.Task != nil {
		m.task = resp.Task
	}
	if resp.Proposal != nil {
		m.proposal = resp.Proposal
	}
	m.audit = resp.AuditEvents
	m.warnings = append(m.warnings, resp.Warnings...)
	m.appliedArtifacts = appendUnique(m.appliedArtifacts, resp.AppliedArtifacts...)
	m.appendEvent("assistant", stageOfTask(m.task), "assistant answer", summarizeAnswer(resp.Answer), "info")
	if resp.Transition != nil {
		m.appendEvent("transition", resp.Transition.To, "transition", fmt.Sprintf("%s -> %s: %s", resp.Transition.From, resp.Transition.To, resp.Transition.Reason), "info")
	}
	for _, warning := range resp.Warnings {
		m.appendEvent("warning", stageOfTask(m.task), "warning", warning, "warning")
	}
	for _, file := range resp.AppliedArtifacts {
		m.appendEvent("files", stageOfTask(m.task), "applied file", file, "info")
	}
	for _, event := range resp.AuditEvents {
		m.appendAuditEvent(event)
	}
	m.refreshEvidence()
}

func (m *Model) rebuildFromState() {
	if m.task != nil && m.task.ID != "" {
		m.appendEvent("task", m.task.Stage, "resume task", fmt.Sprintf("%s expected=%s", m.task.Title, m.task.ExpectedAction), "info")
	}
	for _, event := range m.audit {
		m.appendAuditEvent(event)
	}
	if m.proposal != nil && pendingProposalRecords(*m.proposal) > 0 {
		m.appendEvent("memory", stageOfTask(m.task), "pending memory proposal", fmt.Sprintf("%d records", pendingProposalRecords(*m.proposal)), "info")
	}
	m.refreshEvidence()
}

func (m *Model) refreshEvidence() {
	if m.task == nil || len(m.task.ValidationEvidence) == 0 {
		return
	}
	records, err := m.backend.Evidence(m.ctx, m.task.ID, firstNonEmpty(m.task.LastSessionID, m.sessionID), m.task.ValidationEvidence)
	if err == nil {
		m.evidence = records
	}
}

func (m *Model) appendEvent(kind string, stage app.TaskStage, title, summary, severity string) {
	if strings.TrimSpace(summary) == "" && strings.TrimSpace(title) == "" {
		return
	}
	m.events = append(m.events, timelineEvent{At: time.Now().UTC(), Kind: kind, Stage: stage, Title: title, Summary: summary, Severity: severity})
}

func (m *Model) appendAuditEvent(event process.ProcessAuditEvent) {
	title := event.Decision
	if event.AgentRole != "" {
		title = event.AgentRole + ": " + title
	}
	summary := firstNonEmpty(event.Reason, strings.Join(event.ValidatorErrors, "; "), event.TransitionReason, string(event.ActionKind))
	if event.TransitionFrom != "" || event.TransitionTo != "" {
		summary = strings.TrimSpace(summary + " " + event.TransitionFrom + " -> " + event.TransitionTo)
	}
	if event.Decision == "" && summary == "" {
		return
	}
	m.events = append(m.events, timelineEvent{At: event.CreatedAt, Kind: "audit", Stage: event.Stage, Title: title, Summary: summary, Severity: "info"})
}

func (m *Model) updateViewport() {
	lines := []string{}
	for _, ev := range m.events {
		prefix := ev.Kind
		if ev.Stage != "" {
			prefix += "/" + string(ev.Stage)
		}
		lines = append(lines, fmt.Sprintf("%s %s", lipgloss.NewStyle().Bold(true).Render(prefix), safe(ev.Title)))
		if ev.Summary != "" {
			lines = append(lines, wrap(safe(ev.Summary), max(20, m.timeline.Width-2))...)
		}
	}
	if len(lines) == 0 {
		lines = append(lines, "Опишите coding task. План, progress, files и evidence появятся здесь.")
	}
	m.timeline.SetContent(strings.Join(lines, "\n"))
	m.timeline.GotoBottom()
	m.input.Placeholder = placeholder(m.task)
}

func (m Model) updateContextPicker(msg tea.KeyMsg) (Model, tea.Cmd) {
	p := m.contextPicker
	if p == nil {
		return m, nil
	}
	if p.profileInput {
		switch msg.String() {
		case "esc":
			p.profileInput = false
			p.profileID = ""
		case "enter":
			id := strings.TrimSpace(p.profileID)
			if id == "" {
				m.err = app.NewError(app.CategoryValidation, "missing_profile_id", "profile id is required", nil)
				return m, nil
			}
			m.busy = true
			return m, m.createProfile(id)
		case "backspace":
			if p.profileID != "" {
				runes := []rune(p.profileID)
				p.profileID = string(runes[:len(runes)-1])
			}
		default:
			if len(msg.Runes) > 0 {
				p.profileID += string(msg.Runes)
			}
		}
		return m, nil
	}
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.contextPicker = nil
		m.input.Focus()
	case "up", "k":
		p.move(-1)
	case "down", "j":
		p.move(1)
	case "enter":
		cmd := m.contextPickerEnter()
		if cmd != nil {
			m.busy = true
		}
		return m, cmd
	case "c":
		if p.payload.Kind == "tasks" {
			m.busy = true
			return m, m.clearTask()
		}
	case "a":
		if p.payload.Kind == "tasks" {
			if task, ok := p.currentTask(); ok && !task.Archived {
				m.busy = true
				return m, m.archiveTask(task.ID)
			}
		}
	case "r":
		if p.payload.Kind == "tasks" {
			if task, ok := p.currentTask(); ok && task.Archived {
				if !p.restoreConfirm {
					p.restoreConfirm = true
					return m, nil
				}
				m.busy = true
				return m, m.restoreTask(task.ID)
			}
		}
	}
	return m, nil
}

func (m Model) contextPickerEnter() tea.Cmd {
	p := m.contextPicker
	if p == nil {
		return nil
	}
	switch p.payload.Kind {
	case "sessions":
		if item, ok := p.currentSession(); ok {
			return m.selectSession(item.ID)
		}
	case "tasks":
		if item, ok := p.currentTask(); ok && !item.Archived {
			return m.selectTask(item.ID)
		}
	case "profiles":
		if p.cursor == len(p.payload.Profiles) {
			p.profileInput = true
			p.profileID = ""
			return nil
		}
		if item, ok := p.currentProfile(); ok {
			return m.selectProfile(item.ID)
		}
	}
	return nil
}

func newContextPickerState(payload PickerPayload) *contextPickerState {
	return &contextPickerState{payload: payload}
}

func (p *contextPickerState) itemCount() int {
	if p == nil {
		return 0
	}
	switch p.payload.Kind {
	case "sessions":
		return len(p.payload.Sessions)
	case "tasks":
		return len(p.payload.Tasks)
	case "profiles":
		return len(p.payload.Profiles) + 1
	default:
		return 0
	}
}

func (p *contextPickerState) move(delta int) {
	count := p.itemCount()
	if count == 0 {
		return
	}
	p.cursor = (p.cursor + count + delta) % count
	p.restoreConfirm = false
}

func (p *contextPickerState) currentSession() (SessionSummary, bool) {
	if p == nil || p.cursor < 0 || p.cursor >= len(p.payload.Sessions) {
		return SessionSummary{}, false
	}
	return p.payload.Sessions[p.cursor], true
}

func (p *contextPickerState) currentTask() (TaskSummary, bool) {
	if p == nil || p.cursor < 0 || p.cursor >= len(p.payload.Tasks) {
		return TaskSummary{}, false
	}
	return p.payload.Tasks[p.cursor], true
}

func (p *contextPickerState) currentProfile() (ProfileSummary, bool) {
	if p == nil || p.cursor < 0 || p.cursor >= len(p.payload.Profiles) {
		return ProfileSummary{}, false
	}
	return p.payload.Profiles[p.cursor], true
}

func (p *contextPickerState) footer() string {
	if p == nil {
		return ""
	}
	if p.profileInput {
		return "profile new | type id | enter create | esc cancel"
	}
	switch p.payload.Kind {
	case "tasks":
		if p.restoreConfirm {
			return "task picker | press r again to restore | esc cancel"
		}
		return "task picker | ↑/↓ move | enter select | c close | a archive | r restore | esc close"
	case "profiles":
		return "profile picker | ↑/↓ move | enter select/new | esc close"
	case "sessions":
		return "session picker | ↑/↓ move | enter resume | esc close"
	default:
		return "picker | esc close"
	}
}

func (m Model) contextPickerView() string {
	p := m.contextPicker
	if p == nil {
		return ""
	}
	width := max(40, m.width-4)
	height := max(8, m.timeline.Height)
	lines := []string{styleTitle().Render(pickerTitle(p.payload.Kind))}
	if p.profileInput {
		lines = append(lines, "new profile id: "+emptyDash(p.profileID))
		if m.err != nil {
			lines = append(lines, styleWarn().Render(m.err.Message))
		}
	} else {
		lines = append(lines, p.contextLines(width)...)
	}
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(1, 2).
		Render(strings.Join(lines, "\n"))
}

func pickerTitle(kind string) string {
	switch kind {
	case "sessions":
		return "Resume chat"
	case "tasks":
		return "Select task"
	case "profiles":
		return "Select profile"
	default:
		return "Select"
	}
}

func (p *contextPickerState) contextLines(width int) []string {
	if p == nil {
		return nil
	}
	switch p.payload.Kind {
	case "sessions":
		if len(p.payload.Sessions) == 0 {
			return []string{"no sessions"}
		}
		out := make([]string, 0, len(p.payload.Sessions))
		for i, item := range p.payload.Sessions {
			out = append(out, pickerLine(i == p.cursor, fmt.Sprintf("%s  %s", item.ID, item.LastActivity.Format(time.RFC3339)), width))
		}
		return out
	case "tasks":
		if len(p.payload.Tasks) == 0 {
			return []string{"no tasks"}
		}
		out := []string{}
		section := ""
		for i, item := range p.payload.Tasks {
			next := "active"
			if item.Archived {
				next = "archived"
			}
			if next != section {
				out = append(out, styleGroup().Render(next))
				section = next
			}
			marker := " "
			if item.Current {
				marker = "*"
			}
			label := fmt.Sprintf("%s %s  %s/%s  %s", marker, item.ID, item.Stage, item.Status, item.Title)
			out = append(out, pickerLine(i == p.cursor, label, width))
		}
		return out
	case "profiles":
		out := make([]string, 0, len(p.payload.Profiles)+1)
		for i, item := range p.payload.Profiles {
			marker := " "
			if item.Active {
				marker = "*"
			}
			out = append(out, pickerLine(i == p.cursor, fmt.Sprintf("%s %s  %s", marker, item.ID, item.DisplayName), width))
		}
		out = append(out, pickerLine(p.cursor == len(p.payload.Profiles), "+ new", width))
		return out
	default:
		return []string{"no items"}
	}
}

func pickerLine(selected bool, text string, width int) string {
	prefix := "  "
	if selected {
		prefix = "› "
	}
	line := trimWidth(prefix+text, width-2)
	if selected {
		return styleSelected().Render(line)
	}
	return line
}

func (m Model) updateModelPicker(msg tea.KeyMsg) (Model, tea.Cmd) {
	if m.modelPicker == nil {
		return m, nil
	}
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.modelPicker = nil
		m.input.Focus()
		return m, nil
	case "up", "k":
		if len(m.modelPicker.items) > 0 {
			m.modelPicker.cursor = (m.modelPicker.cursor + len(m.modelPicker.items) - 1) % len(m.modelPicker.items)
		}
	case "down", "j":
		if len(m.modelPicker.items) > 0 {
			m.modelPicker.cursor = (m.modelPicker.cursor + 1) % len(m.modelPicker.items)
		}
	case "pgup":
		m.modelPicker.moveCursor(-m.modelPickerPageSize())
	case "pgdown":
		m.modelPicker.moveCursor(m.modelPickerPageSize())
	case "home":
		if len(m.modelPicker.items) > 0 {
			m.modelPicker.cursor = 0
		}
	case "end":
		if len(m.modelPicker.items) > 0 {
			m.modelPicker.cursor = len(m.modelPicker.items) - 1
		}
	case "enter":
		if item, ok := m.modelPicker.current(); ok {
			m.busy = true
			return m, m.selectModel(item.ID)
		}
	case "backspace":
		if m.modelPicker.query != "" {
			runes := []rune(m.modelPicker.query)
			m.modelPicker.query = string(runes[:len(runes)-1])
			m.modelPicker.rebuild()
		}
	case "F":
		if item, ok := m.modelPicker.current(); ok {
			return m, m.toggleFavoriteModel(item.ID)
		}
	default:
		if len(msg.Runes) > 0 {
			m.modelPicker.query += string(msg.Runes)
			m.modelPicker.rebuild()
		}
	}
	return m, nil
}

func newModelPickerState(catalog ModelCatalog) *modelPickerState {
	state := &modelPickerState{
		models:    appendUnique(nil, append(catalog.Models, catalog.Active)...),
		favorites: favoriteMap(catalog.Favorites),
		active:    catalog.Active,
		warning:   catalog.Warning,
		cursor:    0,
	}
	sort.Strings(state.models)
	state.rebuild()
	return state
}

func fallbackModelIDs() []string {
	return []string{
		"anthropic/claude-3.5-sonnet",
		"fake/model",
		"google/gemini-3.1-flash-lite",
		"openai/gpt-4.1-mini",
	}
}

func (p *modelPickerState) merge(catalog ModelCatalog) {
	if p == nil {
		return
	}
	p.models = appendUnique(p.models, append(catalog.Models, catalog.Active)...)
	if catalog.Active != "" {
		p.active = catalog.Active
	}
	p.favorites = favoriteMap(catalog.Favorites)
	p.warning = catalog.Warning
	sort.Strings(p.models)
	p.rebuild()
}

func (p *modelPickerState) rebuild() {
	if p == nil {
		return
	}
	query := strings.ToLower(strings.TrimSpace(p.query))
	items := make([]modelPickerItem, 0, len(p.models))
	for _, id := range p.models {
		if strings.TrimSpace(id) == "" {
			continue
		}
		provider := modelProvider(id)
		if query != "" && !strings.Contains(strings.ToLower(id), query) && !strings.Contains(strings.ToLower(provider), query) {
			continue
		}
		items = append(items, modelPickerItem{ID: id, Provider: provider})
	}
	sort.SliceStable(items, func(i, j int) bool {
		fi := p.favorites[items[i].ID]
		fj := p.favorites[items[j].ID]
		if fi != fj {
			return fi
		}
		if items[i].Provider != items[j].Provider {
			return items[i].Provider < items[j].Provider
		}
		return items[i].ID < items[j].ID
	})
	p.items = items
	if p.cursor >= len(p.items) {
		p.cursor = max(0, len(p.items)-1)
	}
}

func (p *modelPickerState) current() (modelPickerItem, bool) {
	if p == nil || len(p.items) == 0 || p.cursor < 0 || p.cursor >= len(p.items) {
		return modelPickerItem{}, false
	}
	return p.items[p.cursor], true
}

func (p *modelPickerState) moveCursor(delta int) {
	if p == nil || len(p.items) == 0 {
		return
	}
	p.cursor += delta
	if p.cursor < 0 {
		p.cursor = 0
	}
	if p.cursor >= len(p.items) {
		p.cursor = len(p.items) - 1
	}
}

func (m Model) modelPickerPageSize() int {
	return max(1, m.modelPickerListHeight()-1)
}

func (m Model) modelPickerListHeight() int {
	return max(4, m.timeline.Height-5)
}

func (m Model) modelPickerView() string {
	picker := m.modelPicker
	if picker == nil {
		return ""
	}
	width := max(40, m.width-4)
	height := max(8, m.timeline.Height)
	lines := []string{
		styleTitle().Render("Select model"),
		styleHint().Render("search: " + emptyDash(picker.query)),
	}
	if picker.warning != "" {
		lines = append(lines, styleWarn().Render(trimWidth(picker.warning, width-2)))
	}
	if len(picker.items) == 0 {
		lines = append(lines, "no matching models")
	} else {
		visible := modelPickerVisibleItems(picker, m.modelPickerListHeight())
		lastGroup := ""
		for _, visibleItem := range visible {
			i := visibleItem.Index
			item := visibleItem.Item
			group := item.Provider
			if picker.favorites[item.ID] {
				group = "favorites"
			}
			if group != lastGroup {
				lines = append(lines, styleGroup().Render(group))
				lastGroup = group
			}
			cursor := "  "
			if i == picker.cursor {
				cursor = "› "
			}
			star := " "
			if picker.favorites[item.ID] {
				star = "*"
			}
			active := ""
			if item.ID == picker.active {
				active = " active"
			}
			line := fmt.Sprintf("%s%s %s%s", cursor, star, item.ID, active)
			if i == picker.cursor {
				line = styleSelected().Render(trimWidth(line, width-2))
			} else if item.ID == picker.active {
				line = styleActive().Render(trimWidth(line, width-2))
			} else {
				line = trimWidth(line, width-2)
			}
			lines = append(lines, line)
		}
		if len(visible) < len(picker.items) {
			lines = append(lines, styleHint().Render(fmt.Sprintf("%d/%d", picker.cursor+1, len(picker.items))))
		}
	}
	body := strings.Join(lines, "\n")
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(1, 2).
		Render(body)
}

type visibleModelPickerItem struct {
	Index int
	Item  modelPickerItem
}

func modelPickerVisibleItems(picker *modelPickerState, limit int) []visibleModelPickerItem {
	if picker == nil || len(picker.items) == 0 {
		return nil
	}
	limit = max(1, limit)
	if limit >= len(picker.items) {
		out := make([]visibleModelPickerItem, 0, len(picker.items))
		for i, item := range picker.items {
			out = append(out, visibleModelPickerItem{Index: i, Item: item})
		}
		return out
	}
	start := picker.cursor - limit/2
	if start < 0 {
		start = 0
	}
	if start+limit > len(picker.items) {
		start = len(picker.items) - limit
	}
	out := make([]visibleModelPickerItem, 0, limit)
	for i := start; i < start+limit; i++ {
		out = append(out, visibleModelPickerItem{Index: i, Item: picker.items[i]})
	}
	return out
}

func favoriteMap(models []string) map[string]bool {
	out := map[string]bool{}
	for _, model := range models {
		if strings.TrimSpace(model) != "" {
			out[model] = true
		}
	}
	return out
}

func modelProvider(modelID string) string {
	provider, _, ok := strings.Cut(modelID, "/")
	if !ok || strings.TrimSpace(provider) == "" {
		return "custom"
	}
	return provider
}

func (m Model) hasPendingPlan() bool {
	return m.task != nil && m.task.PendingPlanning != nil
}

func (m Model) hasPendingMemory() bool {
	return m.proposal != nil && pendingProposalRecords(*m.proposal) > 0
}

func summarizeAnswer(answer string) string {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return ""
	}
	parts := strings.Split(answer, "\n\n")
	out := []string{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		var obj map[string]any
		if json.Unmarshal([]byte(part), &obj) == nil {
			fields := []string{}
			for _, key := range []string{"stage", "summary", "current_step", "readiness", "next_signal", "verdict"} {
				if value, ok := obj[key]; ok && fmt.Sprint(value) != "" {
					fields = append(fields, fmt.Sprintf("%s=%s", key, fmt.Sprint(value)))
				}
			}
			if deliverable, ok := obj["deliverable"].(string); ok && deliverable != "" {
				fields = append(fields, "deliverable="+truncate(deliverable, 240))
			}
			if len(fields) > 0 {
				out = append(out, strings.Join(fields, " | "))
				continue
			}
		}
		out = append(out, truncate(part, 600))
	}
	return strings.Join(out, "\n")
}

func placeholder(task *app.TaskState) string {
	if task == nil || task.ID == "" {
		return "Опишите задачу..."
	}
	if task.PendingPlanning != nil {
		return "Подтвердите план или напишите правки..."
	}
	switch task.Stage {
	case app.StageExecution:
		return "Следующее действие..."
	case app.StageValidation:
		return "Попросите проверить или завершить..."
	case app.StageDone:
		return "Новая задача..."
	default:
		return "Опишите следующий шаг..."
	}
}

func pendingProposalRecords(proposal app.MemoryProposal) int {
	n := 0
	for _, r := range proposal.Records {
		if r.Status == "" || r.Status == app.ProposalPending {
			n++
		}
	}
	return n
}

func stageOfTask(task *app.TaskState) app.TaskStage {
	if task == nil {
		return ""
	}
	return task.Stage
}

func section(text string) string { return styleGroup().Render(text) }

func styleTitle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
}

func styleGroup() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
}

func styleHint() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
}

func styleSelected() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("63"))
}

func styleActive() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("120"))
}

func styleWarn() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("209"))
}

func boxed(width int, lines []string) string {
	w := max(20, width-2)
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, wrap(trimWidth(line, w), w)...)
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63")).Width(w).Render(strings.Join(out, "\n"))
}

func safe(text string) string {
	text = strings.ReplaceAll(text, "\x1b", "")
	text = strings.ReplaceAll(text, "\r", " ")
	return strings.TrimSpace(text)
}

func wrap(text string, width int) []string {
	if width <= 0 || len(text) <= width {
		return []string{text}
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{text}
	}
	lines := []string{}
	current := ""
	for _, word := range words {
		if current == "" {
			current = word
			continue
		}
		if len(current)+1+len(word) > width {
			lines = append(lines, current)
			current = word
		} else {
			current += " " + word
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func trimWidth(text string, width int) string {
	if width <= 0 || len(text) <= width {
		return text
	}
	if width <= 1 {
		return text[:width]
	}
	return text[:width-1] + "…"
}

func shortID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}

func emptyDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func truncate(text string, limit int) string {
	text = strings.TrimSpace(text)
	if len(text) <= limit {
		return text
	}
	return text[:limit-1] + "…"
}

func appendUnique(values []string, more ...string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		seen[value] = true
	}
	for _, value := range more {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		values = append(values, value)
	}
	return values
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
