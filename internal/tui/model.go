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
	"github.com/charmbracelet/bubbles/textarea"
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
	Decision string
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

type slashCommandItem struct {
	Command     string
	Description string
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

	input    textarea.Model
	timeline viewport.Model
	spinner  spinner.Model
	active   Pane
	busy     bool

	task               *app.TaskState
	proposal           *app.MemoryProposal
	evidence           []EvidenceView
	events             []timelineEvent
	audit              []process.ProcessAuditEvent
	warnings           []string
	appliedArtifacts   []string
	contextExpanded    bool
	timelineAnchorTop  bool
	timelineAnchorLine int
	timelineAnchorSet  bool
	lastUserInput      string
	err                *app.Error
	modelPicker        *modelPickerState
	contextPicker      *contextPickerState
	slashCursor        int
}

type initialLoadedMsg struct {
	mode       string
	task       *app.TaskState
	audit      []process.ProcessAuditEvent
	proposal   *app.MemoryProposal
	transcript []TranscriptEntry
	err        error
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
	ti := textarea.New()
	ti.Focus()
	ti.Prompt = "> "
	ti.ShowLineNumbers = false
	ti.EndOfBufferCharacter = ' '
	ti.Placeholder = "Type a task..."
	ti.CharLimit = 4096
	ti.MaxHeight = 6
	ti.SetWidth(80)
	ti.SetHeight(1)

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
	return tea.Batch(tea.ClearScreen, m.loadInitial(), m.spinner.Tick)
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
		switch msg.mode {
		case "history":
			m.events = nil
			m.contextExpanded = true
			if len(msg.transcript) > 0 {
				m.appendTranscript(msg.transcript)
			} else {
				m.appendResumeState()
				m.appendStartupAudit(m.audit)
			}
			m.timelineAnchorTop = true
			if m.proposal != nil && pendingProposalRecords(*m.proposal) > 0 {
				m.appendEvent("memory", stageOfTask(m.task), "pending memory proposal", fmt.Sprintf("%d records", pendingProposalRecords(*m.proposal)), "info")
			}
			m.refreshEvidence()
		case "refresh":
			m.refreshEvidence()
		default:
			m.rebuildFromState()
		}
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
		if shouldAutoContinueAfterPlanningApproval(msg.resp) {
			m.busy = true
			m.input.Blur()
			m.appendEvent("system", stageOfTask(m.task), "execution started", "approved plan is running", "info")
			cmds = append(cmds, m.runExchange(executionContinuationInput()))
		}
	case slashFinishedMsg:
		m.busy = false
		m.input.SetValue("")
		m.input.Focus()
		if msg.err != nil {
			m.err = app.AsError(msg.err)
			m.appendEvent("error", "", m.err.Code, m.err.Message, "error")
			break
		}
		line := strings.TrimSpace(msg.line)
		m.applySlashResponse(msg.resp)
		if line == "/new" {
			m.resetNewChatView()
		} else if strings.HasPrefix(line, "/task") || msg.resp.ActiveSessionID != "" {
			m.contextExpanded = true
		}
		if line != "/new" && strings.TrimSpace(msg.resp.Output) != "" {
			m.appendEvent("command", stageOfTask(m.task), line, strings.TrimSpace(msg.resp.Output), "info")
		}
		if msg.resp.Done {
			return m, tea.Quit
		}
		if line == "/new" {
			cmds = append(cmds, tea.ClearScreen, m.loadCurrentState())
		} else if msg.resp.ActiveSessionID != "" {
			cmds = append(cmds, m.loadSessionContext(msg.resp.ActiveSessionID))
		} else {
			cmds = append(cmds, m.loadCurrentState())
		}
	case memoryAppliedMsg:
		if msg.err != nil {
			m.err = app.AsError(msg.err)
			m.appendEvent("error", "", m.err.Code, m.err.Message, "error")
		} else {
			m.appendEvent("memory", stageOfTask(m.task), "memory proposal updated", "proposal records applied/rejected", "info")
			m.contextExpanded = true
			cmds = append(cmds, m.loadCurrentState())
		}
	case taskActionMsg:
		if msg.err != nil {
			m.err = app.AsError(msg.err)
			m.appendEvent("error", "", m.err.Code, m.err.Message, "error")
		} else {
			m.task = &msg.task
			m.contextExpanded = true
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
	case tea.MouseMsg:
		if m.contextPicker == nil && m.modelPicker == nil && isMouseWheel(msg) && m.mouseScrollsTimeline(msg) {
			var cmd tea.Cmd
			m.timeline, cmd = m.timeline.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			break
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
		case "up":
			if m.slashMenuActive() {
				m.moveSlashCursor(-1)
				break
			}
		case "down":
			if m.slashMenuActive() {
				m.moveSlashCursor(1)
				break
			}
		case "pgup", "ctrl+u":
			if m.active == PaneTimeline {
				m.timeline.ViewUp()
				break
			}
		case "pgdown", "ctrl+d":
			if m.active == PaneTimeline {
				m.timeline.ViewDown()
				break
			}
		case "home":
			if m.active == PaneTimeline && m.input.Value() == "" {
				m.timeline.GotoTop()
				break
			}
		case "end":
			if m.active == PaneTimeline && m.input.Value() == "" {
				m.timeline.GotoBottom()
				break
			}
		case "tab":
			m.active = (m.active + 1) % 5
		case "shift+tab":
			m.active = (m.active + 4) % 5
		case "enter":
			text := strings.TrimSpace(m.input.Value())
			text = completeSlashCommand(text, m.slashCursor)
			if text == "" {
				if m.hasPendingPlan() {
					m.busy = true
					m.input.Blur()
					cmds = append(cmds, m.approvePlan())
				} else if m.hasPendingMemory() {
					cmds = append(cmds, m.applyMemory(true))
				} else if m.canContinueExecution() {
					m.busy = true
					m.input.Blur()
					m.appendEvent("system", stageOfTask(m.task), "execution continued", "running next approved step", "info")
					cmds = append(cmds, m.runExchange(executionContinuationInput()))
				}
				break
			}
			if text == "/exit" || text == "/quit" {
				return m, tea.Quit
			}
			if !strings.HasPrefix(text, "/") {
				m.lastUserInput = text
				m.anchorNextTimelineEvent()
				m.appendUserEvent(text)
			} else {
				m.appendEvent("user", stageOfTask(m.task), "command", text, "info")
			}
			m.busy = true
			m.input.Blur()
			m.input.SetValue("")
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
		m.clampSlashCursor()
	}
	m.resize()
	return m, tea.Batch(cmds...)
}

func (m Model) View() string {
	if m.width == 0 {
		return "codingwriter tui\n"
	}
	header := m.headerView()
	slashHelp := m.slashHelpView()
	bodyModel := m
	if slashHelp != "" {
		bodyModel.timeline.Height = max(4, m.timeline.Height-lipgloss.Height(slashHelp))
	}
	body := bodyModel.bodyView()
	footer := m.footerView()
	input := m.input.View()
	if m.modelPicker != nil {
		body = m.modelPickerView()
		slashHelp = ""
		input = ""
	} else if m.contextPicker != nil {
		body = m.contextPickerView()
		slashHelp = ""
		input = ""
	}
	parts := []string{header, body}
	if slashHelp != "" {
		parts = append(parts, slashHelp)
	}
	parts = append(parts, footer, input)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m *Model) resize() {
	inputHeight := m.inputHeight()
	headerHeight := 2
	footerHeight := 1
	bodyHeight := max(4, m.height-headerHeight-footerHeight-inputHeight)
	m.input.SetWidth(max(20, m.width-2))
	m.input.SetHeight(inputHeight)
	m.timeline.Width = max(20, m.width-2)
	m.timeline.Height = bodyHeight
	m.updateViewport()
}

func isMouseWheel(msg tea.MouseMsg) bool {
	return msg.Action == tea.MouseActionPress && (msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown)
}

func (m Model) mouseScrollsTimeline(msg tea.MouseMsg) bool {
	return m.active == PaneTimeline
}

func (m Model) inputHeight() int {
	width := max(1, m.width-6)
	value := m.input.Value()
	if value == "" {
		return 1
	}
	lines := strings.Split(value, "\n")
	height := 0
	for _, line := range lines {
		lineWidth := len([]rune(line))
		height += max(1, (lineWidth+width-1)/width)
	}
	return min(max(1, height), 6)
}

func (m Model) headerView() string {
	cfg := m.backend.Config()
	build := m.backend.BuildInfo()
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
	line := fmt.Sprintf("%s %s | model=%s | profile=%s | task=%s | stage=%s | expected=%s | status=%s%s",
		title, shortBuildVersion(build), emptyDash(cfg.ActiveModel), emptyDash(cfg.ActiveProfileID), task, stage, expected, status, busy)
	return trimWidth(line, m.width) + "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("63")).Render(strings.Repeat("─", max(1, m.width)))
}

func (m Model) bodyView() string {
	if m.width >= 120 {
		leftW := m.width * 60 / 100
		rightW := m.width - leftW - 1
		leftText := m.timeline.View()
		if strings.TrimSpace(leftText) == "" {
			leftText = m.timelineFallbackView(leftW)
		}
		left := lipgloss.NewStyle().Width(leftW).Height(m.timeline.Height).Render(leftText)
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
		view := m.timeline.View()
		if strings.TrimSpace(view) == "" {
			return m.timelineFallbackView(m.width)
		}
		return view
	}
}

func (m Model) timelineFallbackView(width int) string {
	title, detail := m.nextAction()
	lines := []string{
		fmt.Sprintf("%s %s", renderEventPrefix(timelineEvent{Kind: "next", Stage: stageOfTask(m.task)}), eventTitleStyle(timelineEvent{Kind: "next"}).Render(title)),
	}
	if detail != "" {
		lines = append(lines, wrap(eventSummaryStyle(timelineEvent{Kind: "next"}).Render(detail), max(20, width-2))...)
	}
	if m.task != nil && m.task.ID != "" {
		lines = append(lines, fmt.Sprintf("task: %s | stage=%s | expected=%s | status=%s", shortID(m.task.ID), m.task.Stage, m.task.ExpectedAction, m.task.Status))
	}
	return strings.Join(lines, "\n")
}

func (m Model) sidebarView(width int) string {
	if !m.contextExpanded {
		return strings.Join([]string{
			m.statusView(width),
			m.nextActionView(width),
			m.freshChatView(width),
		}, "\n")
	}
	sections := []string{
		m.statusView(width),
		m.nextActionView(width),
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
	line := fmt.Sprintf("tab pane=%s | pgup/pgdown scroll | home/end top/bottom | enter send | /exit quit | p pause/resume%s", pane, approval)
	if m.err != nil && m.err.Hint != "" {
		line += " | hint: " + m.err.Hint
	}
	return styleHint().Render(trimWidth(line, m.width))
}

func (m Model) slashHelpView() string {
	if m.busy || m.modelPicker != nil || m.contextPicker != nil {
		return ""
	}
	query := strings.TrimSpace(m.input.Value())
	if !strings.HasPrefix(query, "/") {
		return ""
	}
	items := matchingSlashCommands(query)
	lines := []string{section("Slash commands")}
	if len(items) == 0 {
		lines = append(lines, "no matches")
		return boxed(m.width, lines)
	}
	cursor := clampIndex(m.slashCursor, len(items))
	limit := min(len(items), max(4, m.height-8))
	visible := visibleSlashCommands(items, cursor, limit)
	for _, visibleItem := range visible {
		item := visibleItem.Item
		line := fmt.Sprintf("%-24s %s", item.Command, item.Description)
		if visibleItem.Index == cursor {
			line = styleSoftSelected().Render(line)
		}
		lines = append(lines, line)
	}
	if len(items) > limit {
		lines = append(lines, styleHint().Render(fmt.Sprintf("↑/↓ select | enter run | %d/%d", cursor+1, len(items))))
	}
	return boxed(m.width, lines)
}

func (m Model) slashMenuActive() bool {
	return !m.busy && m.modelPicker == nil && m.contextPicker == nil && strings.HasPrefix(strings.TrimSpace(m.input.Value()), "/")
}

func (m *Model) moveSlashCursor(delta int) {
	items := matchingSlashCommands(m.input.Value())
	if len(items) == 0 {
		m.slashCursor = 0
		return
	}
	m.slashCursor = (clampIndex(m.slashCursor, len(items)) + len(items) + delta) % len(items)
}

func (m *Model) clampSlashCursor() {
	items := matchingSlashCommands(m.input.Value())
	if len(items) == 0 {
		m.slashCursor = 0
		return
	}
	m.slashCursor = clampIndex(m.slashCursor, len(items))
}

func (m Model) statusView(width int) string {
	cfg := m.backend.Config()
	build := m.backend.BuildInfo()
	lines := []string{section("Status")}
	lines = append(lines,
		"version: "+shortBuildVersion(build),
		"storage: "+emptyDash(m.backend.StorageDir()),
		"model: "+emptyDash(cfg.ActiveModel),
		"profile: "+emptyDash(cfg.ActiveProfileID),
		"session: "+emptyDash(m.sessionID),
		"started: "+tuiSessionTime(m.startedAt),
		fmt.Sprintf("latest audit: %d", len(m.audit)),
	)
	if strings.TrimSpace(m.lastUserInput) != "" {
		lines = append(lines, "last input: "+truncate(safe(m.lastUserInput), 120))
	}
	if m.task == nil || m.task.ID == "" {
		lines = append(lines, "task: none", "stage: -", "expected: -")
		return boxed(width, lines)
	}
	if !m.contextExpanded {
		lines = append(lines,
			"task focus: "+shortID(m.task.ID),
			"stage: "+string(m.task.Stage),
			"status: "+string(m.task.Status),
			"work details hidden in new chat",
		)
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

func shortBuildVersion(info BuildInfo) string {
	version := strings.TrimSpace(info.Version)
	if version == "" {
		version = "dev"
	}
	commit := strings.TrimSpace(info.Commit)
	if commit == "" || commit == "unknown" {
		return "v" + version
	}
	return "v" + version + "+" + shortID(commit)
}

func matchingSlashCommands(query string) []slashCommandItem {
	query = strings.TrimSpace(query)
	if query == "" || query == "/" {
		return slashCommandCatalog()
	}
	matches := make([]slashCommandItem, 0)
	for _, item := range slashCommandCatalog() {
		if strings.HasPrefix(item.Command, query) || strings.HasPrefix(slashCommandBase(item.Command), query) {
			matches = append(matches, item)
		}
	}
	return matches
}

func completeSlashCommand(query string, cursor int) string {
	query = strings.TrimSpace(query)
	if query == "" || !strings.HasPrefix(query, "/") {
		return query
	}
	matches := matchingSlashCommands(query)
	if len(matches) == 0 {
		return query
	}
	if exactSlashCommand(query) && cursor == 0 {
		return query
	}
	return slashCommandRunnable(matches[clampIndex(cursor, len(matches))].Command)
}

func exactSlashCommand(query string) bool {
	for _, item := range slashCommandCatalog() {
		if item.Command == query {
			return true
		}
	}
	return false
}

func slashCommandBase(command string) string {
	base, _, _ := strings.Cut(command, " ")
	return base
}

func slashCommandRunnable(command string) string {
	parts := strings.Fields(command)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.HasPrefix(part, "<") && strings.HasSuffix(part, ">") {
			break
		}
		out = append(out, part)
	}
	return strings.Join(out, " ")
}

type visibleSlashCommand struct {
	Index int
	Item  slashCommandItem
}

func visibleSlashCommands(items []slashCommandItem, cursor, limit int) []visibleSlashCommand {
	if len(items) == 0 {
		return nil
	}
	limit = max(1, min(limit, len(items)))
	cursor = clampIndex(cursor, len(items))
	start := cursor - limit/2
	if start < 0 {
		start = 0
	}
	if start+limit > len(items) {
		start = len(items) - limit
	}
	out := make([]visibleSlashCommand, 0, limit)
	for i := start; i < start+limit; i++ {
		out = append(out, visibleSlashCommand{Index: i, Item: items[i]})
	}
	return out
}

func clampIndex(index, count int) int {
	if count <= 0 || index < 0 {
		return 0
	}
	if index >= count {
		return count - 1
	}
	return index
}

func slashCommandCatalog() []slashCommandItem {
	return []slashCommandItem{
		{Command: "/new", Description: "start new chat; keep task/work/profile/model"},
		{Command: "/resume", Description: "list old chats"},
		{Command: "/resume <session_id>", Description: "resume chat short memory"},
		{Command: "/task", Description: "list saved tasks"},
		{Command: "/task <task_id>", Description: "select task/work in this chat"},
		{Command: "/task close", Description: "clear current task focus"},
		{Command: "/task archive <task_id>", Description: "archive task from list"},
		{Command: "/task restore <task_id>", Description: "restore archived task"},
		{Command: "/profile", Description: "list profiles; includes new"},
		{Command: "/profile <id>", Description: "switch profile/long memory"},
		{Command: "/profile create <id>", Description: "create profile"},
		{Command: "/model", Description: "open model picker"},
		{Command: "/model <id>", Description: "set active model"},
		{Command: "/memory <short|work|long>", Description: "show memory layer"},
		{Command: "/memory propose", Description: "propose memory from latest exchange"},
		{Command: "/memory apply", Description: "apply pending memory"},
		{Command: "/save <short|work|long>", Description: "save note to memory"},
		{Command: "/clear short", Description: "clear current chat memory"},
		{Command: "/privacy", Description: "show privacy/storage summary"},
		{Command: "/process audit", Description: "show process audit"},
		{Command: "/help", Description: "show available commands"},
		{Command: "/exit", Description: "quit TUI"},
	}
}

func (m Model) freshChatView(width int) string {
	lines := []string{section("New chat")}
	lines = append(lines,
		"timeline starts empty",
		"old chat: /resume",
		"task details: /task",
	)
	if m.task != nil && m.task.ID != "" {
		lines = append(lines, "current task/work is preserved")
	}
	return boxed(width, lines)
}

func (m Model) nextActionView(width int) string {
	title, detail := m.nextAction()
	lines := []string{section("Next action"), title}
	if strings.TrimSpace(detail) != "" {
		lines = append(lines, detail)
	}
	return boxed(width, lines)
}

func (m Model) nextAction() (string, string) {
	if m.busy {
		return "Wait for model response.", ""
	}
	if m.contextPicker != nil {
		return "Choose an item.", "Use arrows, then Enter."
	}
	if m.modelPicker != nil {
		return "Choose model.", "Type to filter, Enter to select."
	}
	if m.hasPendingPlan() {
		return "Review pending plan.", "Press a to approve, r to reject."
	}
	if m.hasPendingMemory() {
		return "Review memory proposal.", "Press a to accept all, r to reject all."
	}
	if m.task != nil && m.task.Status == app.TaskStatusPaused {
		return "Task is paused.", "Press p to resume, or type a new task."
	}
	if m.task != nil && m.task.Stage == app.StagePlanning {
		return "Continue planning.", "Answer the question, or type approval."
	}
	if m.task != nil && m.task.Stage == app.StageExecution {
		if m.executionContinuationFailed() {
			return "Execution blocked.", "Provider output failed validation; revise the instruction or inspect /process audit."
		}
		if m.task.ExpectedAction == app.ExpectedLLMResponse {
			return "Continue execution.", "Press Enter to run the next approved step."
		}
		return "Continue execution.", "Send the next instruction."
	}
	if m.task != nil && m.task.Stage == app.StageValidation {
		return "Validate result.", "Send validation feedback or final approval."
	}
	if m.task != nil && m.task.Stage == app.StageDone {
		return "Task is done.", "Type /new for a new chat or /task for saved tasks."
	}
	return "Type a coding task.", "Use /help or / for commands."
}

func (m Model) canContinueExecution() bool {
	return m.task != nil && m.task.Stage == app.StageExecution && m.task.Status != app.TaskStatusPaused && m.task.ExpectedAction == app.ExpectedLLMResponse && !m.executionContinuationFailed()
}

func (m Model) executionContinuationFailed() bool {
	if m.err != nil && m.err.Code == "validation_failed" {
		return true
	}
	return hasExecutionContinuationFailure(m.warnings)
}

func shouldAutoContinueAfterPlanningApproval(resp ChatResponse) bool {
	if resp.Transition == nil || !resp.Transition.Moved || resp.Transition.From != app.StagePlanning || resp.Transition.To != app.StageExecution {
		return false
	}
	if resp.Task == nil || resp.Task.Stage != app.StageExecution || resp.Task.Status == app.TaskStatusPaused {
		return false
	}
	if hasExecutionContinuationFailure(resp.Warnings) {
		return false
	}
	return strings.TrimSpace(resp.Answer) == "planning proposal approved"
}

func hasExecutionContinuationFailure(warnings []string) bool {
	for _, warning := range warnings {
		normalized := strings.ToLower(strings.TrimSpace(warning))
		if strings.Contains(normalized, "execution continuation skipped: validation_failed") ||
			strings.Contains(normalized, "execution auto-continue stopped: validation_failed") {
			return true
		}
	}
	return false
}

func executionContinuationInput() string {
	return "Продолжай выполнение следующего шага утвержденного плана автоматически. Не жди дополнительной команды пользователя."
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
		msg := initialLoadedMsg{mode: "startup"}
		if task, ok, err := m.backend.CurrentTask(); err != nil {
			msg.err = err
		} else if ok {
			msg.task = &task
		}
		return msg
	}
}

func (m *Model) loadCurrentState() tea.Cmd {
	sessionID := m.sessionID
	return func() tea.Msg {
		msg := initialLoadedMsg{mode: "refresh"}
		if task, ok, err := m.backend.CurrentTask(); err != nil {
			msg.err = err
		} else if ok {
			msg.task = &task
		}
		if proposal, ok, err := m.backend.LatestPendingProposal(m.ctx, sessionID); err == nil && ok {
			msg.proposal = &proposal
		}
		return msg
	}
}

func (m *Model) loadSessionContext(sessionID string) tea.Cmd {
	return func() tea.Msg {
		msg := initialLoadedMsg{mode: "history"}
		if task, ok, err := m.backend.CurrentTask(); err != nil {
			msg.err = err
		} else if ok {
			msg.task = &task
		}
		if audit, err := m.backend.LatestAudit(80); err == nil {
			msg.audit = filterAuditForStartup(audit, msg.task, sessionID)
		}
		if transcript, err := m.backend.Transcript(m.ctx, sessionID); err == nil {
			msg.transcript = transcript
		}
		if proposal, ok, err := m.backend.LatestPendingProposal(m.ctx, sessionID); err == nil && ok {
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
	currentSessionID := m.sessionID
	return func() tea.Msg {
		resp, err := m.backend.SelectSession(m.ctx, sessionID, currentSessionID)
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
	sessionID := m.sessionID
	return func() tea.Msg {
		resp, err := m.backend.ClearTask(m.ctx, sessionID)
		return slashFinishedMsg{line: "/task close", resp: resp, err: err}
	}
}

func (m Model) archiveTask(taskID string) tea.Cmd {
	sessionID := m.sessionID
	return func() tea.Msg {
		resp, err := m.backend.ArchiveTask(m.ctx, taskID, sessionID)
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
	sessionID := m.sessionID
	return func() tea.Msg {
		resp, err := m.backend.SelectProfile(m.ctx, profileID, sessionID)
		return slashFinishedMsg{line: "/profile " + profileID, resp: resp, err: err}
	}
}

func (m Model) createProfile(profileID string) tea.Cmd {
	sessionID := m.sessionID
	return func() tea.Msg {
		resp, err := m.backend.CreateProfile(m.ctx, profileID, sessionID)
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
	m.contextExpanded = true
	if resp.Task != nil {
		m.task = resp.Task
	}
	if resp.Proposal != nil {
		m.proposal = resp.Proposal
	}
	m.audit = resp.AuditEvents
	m.warnings = append(m.warnings, resp.Warnings...)
	m.appliedArtifacts = appendUnique(m.appliedArtifacts, resp.AppliedArtifacts...)
	m.appendResponseAuditSummary(resp.AuditEvents)
	if resp.Transition != nil {
		m.appendEvent("transition", resp.Transition.To, "transition", fmt.Sprintf("%s -> %s: %s", resp.Transition.From, resp.Transition.To, resp.Transition.Reason), "info")
	}
	for _, warning := range resp.Warnings {
		m.appendEvent("warning", stageOfTask(m.task), "warning", warning, "warning")
	}
	for _, file := range resp.AppliedArtifacts {
		m.appendEvent("files", stageOfTask(m.task), "applied file", file, "info")
	}
	if summary := summarizeAnswer(resp.Answer); summary != "" {
		m.appendEvent("assistant", stageOfTask(m.task), "assistant answer", summary, "info")
	}
	title, detail := m.nextAction()
	m.appendEvent("next", stageOfTask(m.task), title, detail, "info")
	m.refreshEvidence()
}

func (m *Model) appendResponseAuditSummary(events []process.ProcessAuditEvent) {
	if len(events) == 0 {
		return
	}
	last := events[len(events)-1]
	m.appendEvent("audit", last.Stage, "audit summary", startupAuditSummary(events, last), auditSeverity(last))
}

func (m *Model) rebuildFromState() {
	m.appendStartupState()
	m.refreshEvidence()
}

func (m *Model) resetNewChatView() {
	m.events = nil
	m.audit = nil
	m.proposal = nil
	m.evidence = nil
	m.appliedArtifacts = nil
	m.warnings = nil
	m.err = nil
	m.contextExpanded = false
	m.appendStartupState()
}

func (m *Model) appendStartupState() {
	parts := []string{
		"new chat=" + emptyDash(m.sessionID),
		"storage=" + emptyDash(m.backend.StorageDir()),
	}
	stage := app.TaskStage("")
	if m.task != nil && m.task.ID != "" {
		stage = m.task.Stage
		parts = append(parts,
			"current task="+m.task.ID,
			"stage="+string(m.task.Stage),
			"expected="+string(m.task.ExpectedAction),
			"status="+string(m.task.Status),
		)
		if strings.TrimSpace(m.task.LastSessionID) != "" {
			parts = append(parts, "task session="+m.task.LastSessionID)
		}
	} else {
		parts = append(parts, "current task=none")
	}
	parts = append(parts, "history: /resume")
	m.appendEvent("startup", stage, "new chat", strings.Join(parts, " | "), "info")
}

func (m *Model) appendResumeState() {
	parts := []string{
		"resumed chat=" + emptyDash(m.sessionID),
		"storage=" + emptyDash(m.backend.StorageDir()),
	}
	stage := app.TaskStage("")
	if m.task != nil && m.task.ID != "" {
		stage = m.task.Stage
		parts = append(parts,
			"current task="+m.task.ID,
			"stage="+string(m.task.Stage),
			"expected="+string(m.task.ExpectedAction),
			"status="+string(m.task.Status),
		)
	} else {
		parts = append(parts, "current task=none")
	}
	parts = append(parts, fmt.Sprintf("loaded audit=%d", len(m.audit)))
	m.appendEvent("startup", stage, "chat resumed", strings.Join(parts, " | "), "info")
}

func (m *Model) appendTranscript(entries []TranscriptEntry) {
	for _, entry := range entries {
		content := strings.TrimSpace(entry.Content)
		if content == "" {
			continue
		}
		switch entry.Role {
		case app.RoleUser:
			title := "you: " + truncate(safe(content), 96)
			summary := ""
			if len([]rune(content)) > 96 {
				summary = content
			}
			m.events = append(m.events, timelineEvent{At: entry.CreatedAt, Kind: "user", Stage: stageOfTask(m.task), Title: title, Summary: summary, Severity: "info"})
		case app.RoleAssistant:
			m.events = append(m.events, timelineEvent{At: entry.CreatedAt, Kind: "assistant", Stage: stageOfTask(m.task), Title: "assistant answer", Summary: summarizeAnswer(content), Severity: "info"})
		}
	}
}

func filterAuditForStartup(events []process.ProcessAuditEvent, task *app.TaskState, sessionID string) []process.ProcessAuditEvent {
	taskID := ""
	contextSessionID := strings.TrimSpace(sessionID)
	if task != nil {
		taskID = strings.TrimSpace(task.ID)
		if strings.TrimSpace(task.LastSessionID) != "" {
			contextSessionID = strings.TrimSpace(task.LastSessionID)
		}
	}
	out := make([]process.ProcessAuditEvent, 0, len(events))
	for _, event := range events {
		eventTaskID := strings.TrimSpace(event.TaskID)
		eventSessionID := strings.TrimSpace(event.SessionID)
		if taskID != "" {
			if eventTaskID == taskID || eventTaskID == "" && eventSessionID == contextSessionID {
				out = append(out, event)
			}
			continue
		}
		if contextSessionID != "" && eventSessionID == contextSessionID {
			out = append(out, event)
		}
	}
	return out
}

func (m *Model) appendStartupAudit(events []process.ProcessAuditEvent) {
	if len(events) == 0 {
		return
	}
	last := events[len(events)-1]
	summary := startupAuditSummary(events, last)
	m.appendEvent("audit", last.Stage, "restored audit history", summary, auditSeverity(last))
}

func startupAuditSummary(events []process.ProcessAuditEvent, last process.ProcessAuditEvent) string {
	counts := map[string]int{}
	for _, event := range events {
		key := strings.TrimSpace(event.Decision)
		if key == "" {
			key = string(event.ActionKind)
		}
		if key == "" {
			key = "event"
		}
		counts[key]++
	}
	parts := []string{}
	for _, key := range []string{"provider_call", "agent_call", "agent_accepted", "retried", "rejected", "transitioned", "accepted"} {
		if counts[key] > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
			delete(counts, key)
		}
	}
	lastReason := summarizeAuditReason(firstNonEmpty(strings.Join(last.ValidatorErrors, "; "), last.Reason, last.TransitionReason))
	if lastReason != "" {
		parts = append(parts, "last: "+lastReason)
	}
	if len(parts) == 0 {
		return fmt.Sprintf("%d context audit events", len(events))
	}
	return fmt.Sprintf("%d context audit events; %s", len(events), strings.Join(parts, " "))
}

func summarizeAuditReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ""
	}
	lower := strings.ToLower(reason)
	switch {
	case strings.Contains(lower, "ready_for_validation") && strings.Contains(lower, "trusted evidence"):
		return "validation blocked: trusted evidence required"
	case strings.Contains(lower, "trusted evidence"):
		return "trusted evidence required"
	default:
		return trimWidth(reason, 96)
	}
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

func (m *Model) appendUserEvent(text string) {
	clean := safe(text)
	title := "you: " + truncate(clean, 96)
	summary := ""
	if len([]rune(clean)) > 96 {
		summary = clean
	}
	m.appendEvent("user", stageOfTask(m.task), title, summary, "info")
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
	m.events = append(m.events, timelineEvent{At: event.CreatedAt, Kind: "audit", Stage: event.Stage, Decision: event.Decision, Title: title, Summary: summary, Severity: auditSeverity(event)})
}

func (m *Model) updateViewport() {
	wasAtBottom := m.timeline.AtBottom()
	lines := m.renderTimelineLines(m.events)
	if len(lines) == 0 {
		lines = append(lines, "Опишите coding task. План, progress, files и evidence появятся здесь.")
	}
	m.timeline.SetContent(strings.Join(lines, "\n"))
	if m.timelineAnchorTop {
		m.timeline.GotoTop()
		m.timelineAnchorTop = false
	} else if m.timelineAnchorSet {
		m.timeline.SetYOffset(m.timelineAnchorLine)
		m.timelineAnchorSet = false
	} else if wasAtBottom || m.timeline.PastBottom() {
		m.timeline.GotoBottom()
	}
	m.input.Placeholder = placeholder(m.task)
}

func (m *Model) anchorNextTimelineEvent() {
	m.timelineAnchorLine = len(m.renderTimelineLines(m.events))
	m.timelineAnchorSet = true
}

func (m Model) renderTimelineLines(events []timelineEvent) []string {
	lines := []string{}
	for _, ev := range events {
		lines = append(lines, fmt.Sprintf("%s %s", renderEventPrefix(ev), eventTitleStyle(ev).Render(safe(ev.Title))))
		if ev.Summary != "" {
			summary := eventSummaryStyle(ev).Render(safe(ev.Summary))
			lines = append(lines, wrap(summary, max(20, m.timeline.Width-2))...)
		}
	}
	return lines
}

func auditSeverity(event process.ProcessAuditEvent) string {
	switch event.Decision {
	case "rejected", "transition_failed", "persistence_failed":
		return "error"
	case "retried", "prompt_improvement_skipped", "agent_rejected", "agent_invalid_json":
		return "warning"
	default:
		return "info"
	}
}

func renderEventPrefix(ev timelineEvent) string {
	kind := eventKindStyle(ev.Kind).Render(emptyDash(ev.Kind))
	if ev.Stage == "" {
		return kind
	}
	return kind + styleHint().Render("/") + eventStageStyle(ev.Stage).Render(string(ev.Stage))
}

func eventKindStyle(kind string) lipgloss.Style {
	style := lipgloss.NewStyle().Bold(true)
	switch kind {
	case "startup":
		return style.Foreground(lipgloss.Color("117"))
	case "task":
		return style.Foreground(lipgloss.Color("81"))
	case "transition":
		return style.Foreground(lipgloss.Color("120"))
	case "memory":
		return style.Foreground(lipgloss.Color("177"))
	case "files":
		return style.Foreground(lipgloss.Color("110"))
	case "audit":
		return style.Foreground(lipgloss.Color("244"))
	case "warning":
		return style.Foreground(lipgloss.Color("214"))
	case "error":
		return style.Foreground(lipgloss.Color("209"))
	default:
		return style.Foreground(lipgloss.Color("250"))
	}
}

func eventStageStyle(stage app.TaskStage) lipgloss.Style {
	style := lipgloss.NewStyle().Bold(true)
	switch stage {
	case app.StagePlanning:
		return style.Foreground(lipgloss.Color("81"))
	case app.StageExecution:
		return style.Foreground(lipgloss.Color("214"))
	case app.StageValidation:
		return style.Foreground(lipgloss.Color("177"))
	case app.StageDone:
		return style.Foreground(lipgloss.Color("120"))
	default:
		return style.Foreground(lipgloss.Color("244"))
	}
}

func eventTitleStyle(ev timelineEvent) lipgloss.Style {
	style := lipgloss.NewStyle()
	switch ev.Severity {
	case "error":
		return style.Foreground(lipgloss.Color("209"))
	case "warning":
		return style.Foreground(lipgloss.Color("214"))
	}
	return eventDecisionStyle(ev.Decision)
}

func eventDecisionStyle(decision string) lipgloss.Style {
	style := lipgloss.NewStyle()
	switch decision {
	case "accepted", "agent_accepted", "transitioned":
		return style.Foreground(lipgloss.Color("120"))
	case "retried", "prompt_improvement_skipped":
		return style.Foreground(lipgloss.Color("214"))
	case "rejected", "agent_rejected", "transition_failed", "persistence_failed":
		return style.Foreground(lipgloss.Color("209"))
	case "provider_call", "agent_call", "semantic_output_call", "semantic_intent_call":
		return style.Foreground(lipgloss.Color("110"))
	default:
		return style.Foreground(lipgloss.Color("250"))
	}
}

func eventSummaryStyle(ev timelineEvent) lipgloss.Style {
	switch ev.Severity {
	case "error":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("209"))
	case "warning":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	default:
		return styleHint()
	}
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
		out := make([]string, 0, len(p.payload.Sessions)*2)
		for i, item := range p.payload.Sessions {
			title := firstNonEmpty(item.Title, item.ID)
			out = append(out, pickerLine(i == p.cursor, fmt.Sprintf("%s  %s", tuiSessionTime(item.StartedAt), title), width))
			if item.Description != "" {
				out = append(out, styleHint().Render(trimWidth("    "+item.Description, width-2)))
			}
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

func tuiSessionTime(ts time.Time) string {
	if ts.IsZero() {
		return "-"
	}
	return ts.Local().Format("2006-01-02 15:04")
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
			if summary := userFacingJSONSummary(obj); summary != "" {
				out = append(out, summary)
				continue
			}
			out = append(out, "Structured response received.")
			continue
		}
		if looksLikeStructuredAnswer(part) {
			out = append(out, "Structured response received.")
			continue
		}
		out = append(out, part)
	}
	return strings.Join(out, "\n")
}

func userFacingJSONSummary(obj map[string]any) string {
	lines := []string{}
	for _, key := range []string{"summary", "verdict"} {
		if value, ok := obj[key]; ok && strings.TrimSpace(fmt.Sprint(value)) != "" {
			lines = append(lines, truncate(strings.TrimSpace(fmt.Sprint(value)), 360))
			break
		}
	}
	if deliverable, ok := obj["deliverable"].(string); ok && strings.TrimSpace(deliverable) != "" {
		lines = append(lines, "Deliverable prepared.")
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func looksLikeStructuredAnswer(text string) bool {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return true
	}
	lower := strings.ToLower(trimmed)
	signals := 0
	for _, marker := range []string{"stage=", "summary=", "current_step=", "next_signal=", "deliverable="} {
		if strings.Contains(lower, marker) {
			signals++
		}
	}
	return signals >= 2
}

func placeholder(task *app.TaskState) string {
	if task == nil || task.ID == "" {
		return "Type a task..."
	}
	if task.PendingPlanning != nil {
		return "Approve plan or type changes..."
	}
	switch task.Stage {
	case app.StageExecution:
		return "Next action..."
	case app.StageValidation:
		return "Ask to verify or finish..."
	case app.StageDone:
		return "New task..."
	default:
		return "Type the next step..."
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

func styleSoftSelected() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Background(lipgloss.Color("238"))
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
	if width <= 0 || len([]rune(text)) <= width {
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
		if len([]rune(current))+1+len([]rune(word)) > width {
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
	runes := []rune(text)
	if width <= 0 || len(runes) <= width {
		return text
	}
	if width <= 1 {
		return string(runes[:width])
	}
	return string(runes[:width-1]) + "…"
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
