package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	case tea.KeyMsg:
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
				break
			}
			if text == "/exit" || text == "/quit" {
				return m, tea.Quit
			}
			m.appendEvent("user", stageOfTask(m.task), "user input", text, "info")
			m.busy = true
			m.input.Blur()
			if strings.HasPrefix(text, "/") {
				cmds = append(cmds, m.runSlash(text))
			} else {
				cmds = append(cmds, m.runExchange(text))
			}
		case "a":
			if m.hasPendingPlan() {
				m.busy = true
				cmds = append(cmds, m.approvePlan())
			} else if m.hasPendingMemory() {
				cmds = append(cmds, m.applyMemory(true))
			}
		case "r":
			if m.hasPendingPlan() {
				m.busy = true
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

	if !m.busy {
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
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81")).Render("codingwriter")
	line := fmt.Sprintf("%s | model=%s | profile=%s | task=%s | stage=%s | expected=%s | status=%s%s",
		title, emptyDash(cfg.ActiveModel), emptyDash(cfg.ActiveProfileID), task, stage, expected, status, busy)
	return trimWidth(line, m.width) + "\n" + strings.Repeat("─", max(1, m.width))
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
	return trimWidth(line, m.width)
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

func section(text string) string { return lipgloss.NewStyle().Bold(true).Render(text) }

func boxed(width int, lines []string) string {
	w := max(20, width-2)
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, wrap(trimWidth(line, w), w)...)
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Width(w).Render(strings.Join(out, "\n"))
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
