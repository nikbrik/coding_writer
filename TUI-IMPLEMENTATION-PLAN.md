# План реализации TUI для `coding_writer`

## 1. Цель

Сделать `assistant chat` основным TUI-first рабочим интерфейсом поверх текущего control-plane: CLI runtime, `ProcessController`, task lifecycle, memory proposals, audit и trusted evidence.

План ниже не требует переписывать процессный слой. Первый срез должен обернуть существующий поток:

```text
cmd/assistant/main.go
  -> internal/cli.Execute()
  -> internal/cli.chatCommand()
  -> internal/cli.newRuntime()
  -> internal/cli.runChatExchange()
  -> internal/process.ProcessController.RunExchange()
  -> internal/tasks.Manager + audit + memory proposals + trusted evidence
  -> internal/tui Bubble Tea program
```

Главный принцип: TUI не владеет authoritative state. Он показывает состояние, принимает ввод и approvals, а все мутации идут через существующие managers/gates.

## 2. Что уже есть в коде

### CLI entrypoint

- `cmd/assistant/main.go` вызывает только `cli.Execute()`.
- `internal/cli/root.go` строит Cobra root command.
- `assistant chat` сейчас имеет два режима:
  - `--once`: один exchange через `runChatExchange()`;
  - без `--once`: plain REPL через `runREPL()`.

### Runtime composition

`internal/cli.runtime` уже собирает все зависимости:

- `ConfigMgr`, `Profiles`, `Tasks`, `Memory`, `Invariants`, `Proposals`;
- provider, prompt builder, classifier;
- `ProcessController`;
- `StagePolicyRegistry`, `TransitionGate`, `RetryController`, `AuditStore`.

Ключевые методы:

- `newRuntime(ctx, opts)`;
- `ensureProcessController()`;
- `attachProviderToProcess()`;
- `preflightProcess()`;
- `syncActiveProfile()`.

TUI должен использовать этот runtime, а не создавать параллельный composition root.

### Chat exchange

`runChatExchange()` и `runChatExchangeLocked()` уже делают:

- preflight через `ProcessController.PreflightContext()`;
- provider validation;
- process controller setup;
- auto verification command planning;
- `runTrustedVerification()`;
- `ProcessController.RunExchange()`;
- materialization execution deliverables;
- post-approval trusted verification;
- current task reload;
- audit events lookup через `chatAuditEvents()`.

Итоговый `chatResult` уже содержит данные для TUI:

- `Answer`;
- `Model`;
- `Proposal`;
- `Transition`;
- `AppliedArtifacts`;
- `Warnings`;
- `Task`;
- `AuditEvents`.

### Plain REPL

`runREPL()` сейчас:

- создаёт `sessionID`;
- читает строки через `bufio.Scanner`;
- обрабатывает slash-команды через `handleSlash()`;
- для normal input вызывает `runChatExchange()`;
- печатает `renderChatResult()`.

Это хороший fallback. TUI должен стать default для interactive terminal, а `runREPL()` остаться запасным режимом.

### Task state

`app.TaskState` уже содержит:

- `ID`, `Title`, `Objective`;
- `Stage`: `planning`, `execution`, `validation`, `done`;
- `ExpectedAction`: `user_input`, `llm_response`, `user_confirmation`, `none`;
- `Status`: `active`, `paused`;
- `CurrentStep`;
- `AcceptanceCriteria`;
- `Plan`;
- `Microtasks`;
- `PendingPlanning`;
- planning approval fields;
- accepted execution/validation ids;
- `ValidationStatus`;
- `ValidationEvidence`;
- `HistoryLog`;
- pause/resume timestamps.

`internal/tasks.Manager` уже умеет:

- `Start`, `Current`;
- `Move`, `MoveWithPlanningOutput`;
- `SetStep`, `AddPlanItem`, `AddCriteria`;
- `SavePendingPlanningProposal`;
- `ApprovePendingPlanningProposal`;
- `RejectPendingPlanningProposal`;
- `RecordAcceptedExecution`;
- `RecordAcceptedValidation`;
- `SetExecutionProgress`;
- `SetExpectedAction`;
- `Pause`, `Resume`.

### Process controller

`ProcessController.RunExchange()` уже является deterministic process loop:

- blocks secret-like input;
- resolves process state;
- improves prompt;
- checks invariants;
- auto-starts task when needed;
- routes semantic intent when enabled;
- persists pending planning;
- accepts/rejects planning approval;
- transitions execution/validation/done through `LifecycleGate`;
- stores audit events;
- creates memory proposals.

TUI должен вызывать этот слой как backend-цикл, не переносить lifecycle logic в UI.

### Audit, evidence, proposals

`process.ProcessAuditEvent` даёт timeline:

- `Stage`;
- `ActionKind`;
- `Decision`;
- validator errors;
- transition fields;
- agent role / microtask;
- evidence refs;
- model;
- `CreatedAt`.

`process.TrustedEvidenceStore` хранит issued evidence records:

- `ID`;
- `TaskID`;
- `SessionID`;
- source command;
- exit code;
- output hash/output;
- timestamp.

`memory.ProposalStore` уже даёт:

- `Save`;
- `List`;
- `Latest`;
- `LatestPending`;
- `Apply`.

`app.MemoryProposal` и `app.ProposedMemoryRecord` уже подходят для memory proposal panel.

## 3. Выбор стека

### Основной TUI stack

Использовать:

- `github.com/charmbracelet/bubbletea` как event loop;
- `github.com/charmbracelet/lipgloss` для layout/styling;
- `github.com/charmbracelet/bubbles/textinput` для строки ввода;
- `github.com/charmbracelet/bubbles/viewport` для timeline, plan, diff, evidence;
- `github.com/charmbracelet/bubbles/key` для key bindings;
- `github.com/charmbracelet/bubbles/spinner` для provider/tool progress;
- `github.com/charmbracelet/bubbles/list` только для command palette/session picker, не для основной timeline.

Не добавлять отдельный тяжелый layout framework. Для текущего продукта достаточно собственного layout manager на `lipgloss`:

- проще контролировать responsive split;
- меньше dependency surface;
- проще тестировать string snapshots;
- Bubble Tea examples обычно строятся так же.

### Layout framework внутри проекта

Создать легкий пакет:

```text
internal/tui/layout
```

Основные типы:

```go
type Breakpoint int

const (
    BreakpointNarrow Breakpoint = iota
    BreakpointWide
)

type Rect struct {
    X, Y int
    Width, Height int
}

type Regions struct {
    Header Rect
    Timeline Rect
    Sidebar Rect
    Detail Rect
    Input Rect
    Footer Rect
}

func Compute(width, height int, mode ViewMode) Regions
```

Первый layout:

- wide terminal: timeline слева, sidebar справа;
- narrow terminal: tabs/fullscreen panes;
- input всегда снизу;
- footer/status bar над input или в header.

### Почему Bubble Tea

- Go-native, без отдельного runtime;
- совместим с Cobra command;
- легко тестировать update/render;
- работает с async commands;
- подходит для streaming/progress later;
- экосистема Charm уже покрывает textinput/viewport/spinner/styles.

## 4. Архитектура пакетов

Добавить TUI как новый слой, без business logic внутри UI components:

```text
internal/tui/
  app.go
  model.go
  messages.go
  commands.go
  keymap.go
  styles.go
  render.go
  session.go
  adapter.go
  layout/
    layout.go
    layout_test.go
  components/
    input.go
    timeline.go
    status.go
    plan.go
    diff.go
    evidence.go
    memory.go
    approvals.go
    help.go
```

Добавить thin backend adapter в `internal/cli`, потому что нужные функции сейчас unexported:

```text
internal/cli/tui_adapter.go
```

Предлагаемый API:

```go
type ChatBackend struct {
    rt *runtime
}

type TUIOptions struct {
    Plain bool
}

func NewChatBackend(ctx context.Context, opts *globalOptions) (*ChatBackend, error)
func (b *ChatBackend) Config() app.AppConfig
func (b *ChatBackend) StorageDir() string
func (b *ChatBackend) CurrentTask() (app.TaskState, bool, error)
func (b *ChatBackend) LatestAudit(limit int) ([]process.ProcessAuditEvent, error)
func (b *ChatBackend) LatestPendingProposal(ctx context.Context, sessionID string) (app.MemoryProposal, bool, error)
func (b *ChatBackend) Exchange(ctx context.Context, req ChatRequest) (ChatResponse, error)
func (b *ChatBackend) ApplyMemory(ctx context.Context, req MemoryApplyRequest) (memory.ApplyResult, error)
func (b *ChatBackend) ApprovePlanning(ctx context.Context, sessionID string) (app.TaskState, error)
func (b *ChatBackend) RejectPlanning(ctx context.Context, sessionID string) (app.TaskState, error)
func (b *ChatBackend) PauseTask() (app.TaskState, error)
func (b *ChatBackend) ResumeTask() (app.TaskState, error)
```

`ChatResponse` на первом шаге может быть thin wrapper над текущим `chatResult`, но лучше не экспортировать `chatResult` напрямую из CLI forever:

```go
type ChatRequest struct {
    SessionID string
    Input string
    RenderOnly bool
    RequireMemoryProposal bool
    VerifyCommand string
}

type ChatResponse struct {
    OK bool
    SessionID string
    Answer string
    Model string
    Proposal *app.MemoryProposal
    Transition *process.TransitionResult
    AppliedArtifacts []string
    Warnings []string
    Task *app.TaskState
    AuditEvents []process.ProcessAuditEvent
    RenderedPromptID string
}
```

Задача adapter: сохранить доступ к существующим private functions без переноса TUI в пакет `cli`.

## 5. Изменения CLI

### Flags

Расширить `chatOptions`:

```go
type chatOptions struct {
    Once         bool
    Input        string
    RenderPrompt bool
    Verify       string
    TUI          bool
    Plain        bool
}
```

Flags:

```text
assistant chat --tui
assistant chat --plain
assistant chat --once ...
```

Рекомендованное поведение:

- interactive terminal + no `--once` + no `--json` -> TUI default;
- `--plain` -> старый `runREPL()`;
- non-interactive stdin -> старый `runREPL()`;
- `--json` по-прежнему требует `--once`;
- `--tui` в non-interactive mode возвращает typed CLI error.

Для определения interactive уже есть `isInteractiveReader(in)`. Для output желательно добавить:

```go
func isInteractiveWriter(w io.Writer) bool
```

Если терминал не интерактивный:

```text
assistant chat --tui
```

ошибка:

```text
category=cli code=tui_requires_terminal message="TUI requires an interactive terminal"
hint="use assistant chat --plain"
```

### Control flow в `chatCommand`

Текущий конец `RunE`:

```go
return runREPL(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), rt)
```

Заменить на:

```go
if shouldRunTUI(cmd, chatOpts, opts) {
    backend := newChatBackendFromRuntime(rt)
    return tui.Run(cmd.Context(), backend, tui.Options{
        In: cmd.InOrStdin(),
        Out: cmd.OutOrStdout(),
        Err: cmd.ErrOrStderr(),
    })
}
return runREPL(...)
```

`runREPL()` не удалять.

## 6. TUI модель данных

### Главная модель

```go
type Model struct {
    backend Backend
    session SessionState
    size Size
    keymap KeyMap
    styles Styles

    input InputModel
    timeline TimelineModel
    status StatusModel
    plan PlanModel
    diff DiffModel
    evidence EvidenceModel
    memory MemoryModel
    approvals ApprovalModel

    activePane Pane
    focus FocusTarget
    busy bool
    spinner spinner.Model
    err *app.Error
}
```

### Session state

```go
type SessionState struct {
    ID string
    StartedAt time.Time
    Model string
    ProfileID string
    StorageDir string
    Task *app.TaskState
    Events []TimelineEvent
    LatestProposal *app.MemoryProposal
    PendingApproval *Approval
    Evidence []EvidenceView
    Warnings []WarningView
    AppliedArtifacts []string
}
```

Session ID должен быть создан один раз при запуске TUI:

```go
sessionID := app.NewID("session")
```

И передаваться в каждый `backend.Exchange()`.

### Timeline event

```go
type TimelineEvent struct {
    ID string
    At time.Time
    Kind EventKind
    Stage app.TaskStage
    Title string
    Summary string
    Detail string
    Severity Severity
    Audit *process.ProcessAuditEvent
    Task *app.TaskState
    Proposal *app.MemoryProposal
    EvidenceRefs []string
    Collapsed bool
}
```

`Kind`:

```go
const (
    EventUserInput EventKind = "user_input"
    EventAssistantAnswer EventKind = "assistant_answer"
    EventPromptImprovement EventKind = "prompt_improvement"
    EventPlanning EventKind = "planning"
    EventApprovalRequest EventKind = "approval_request"
    EventApprovalResult EventKind = "approval_result"
    EventExecution EventKind = "execution"
    EventTransition EventKind = "transition"
    EventVerification EventKind = "verification"
    EventValidation EventKind = "validation"
    EventMemoryProposal EventKind = "memory_proposal"
    EventWarning EventKind = "warning"
    EventError EventKind = "error"
    EventDone EventKind = "done"
)
```

Источники events:

- user input -> local event before exchange;
- assistant answer -> `ChatResponse.Answer`;
- transitions -> `ChatResponse.Transition`;
- audit -> `ChatResponse.AuditEvents`;
- proposals -> `ChatResponse.Proposal`;
- evidence -> `Task.ValidationEvidence` + audit `EvidenceRefs`;
- warnings/errors -> `Warnings` and typed errors.

### Approval model

```go
type Approval struct {
    ID string
    Kind ApprovalKind
    Title string
    Body string
    PrimaryLabel string
    SecondaryLabel string
    TaskID string
    ProposalID string
    RecordID string
}
```

`ApprovalKind`:

- `planning`;
- `memory_record`;
- `verification_command`;
- `patch`;
- `resume`;

Первый срез реально поддерживает:

- `planning`;
- `memory_record`;
- `resume`.

`verification_command` и `patch` оставить в модели и UI placeholders, но не fake-implement.

## 7. Backend contract для TUI

В `internal/tui/adapter.go` определить интерфейс, чтобы TUI тестировался fake backend:

```go
type Backend interface {
    Config() app.AppConfig
    StorageDir() string
    CurrentTask() (app.TaskState, bool, error)
    LatestAudit(limit int) ([]process.ProcessAuditEvent, error)
    LatestPendingProposal(ctx context.Context, sessionID string) (app.MemoryProposal, bool, error)
    Exchange(ctx context.Context, req ChatRequest) (ChatResponse, error)
    ApplyMemory(ctx context.Context, req MemoryApplyRequest) (memory.ApplyResult, error)
    ApprovePlanning(ctx context.Context, sessionID string) (app.TaskState, error)
    RejectPlanning(ctx context.Context, sessionID string) (app.TaskState, error)
    PauseTask() (app.TaskState, error)
    ResumeTask() (app.TaskState, error)
}
```

`internal/cli.ChatBackend` должен реализовать этот интерфейс.

Важно: `internal/tui` не должен импортировать `internal/cli`, иначе получится цикл. `internal/cli` импортирует `internal/tui` и передает backend implementation.

## 8. Компоненты TUI

### 8.1. Поле ввода с историей

Файл:

```text
internal/tui/components/input.go
```

Структура:

```go
type InputModel struct {
    textinput.Model
    history []string
    historyIndex int
    disabled bool
}
```

Поведение:

- `enter` sends current text;
- `ctrl+p` / `up` cycles history when cursor at start or input empty;
- `ctrl+n` / `down` moves forward;
- `esc` clears current draft;
- disabled during backend exchange unless focused approval expects text edit;
- placeholder depends on task state:
  - no task: `Опишите задачу...`;
  - planning confirmation: `Напишите правки к плану или подтвердите`;
  - execution: `Следующее действие...`;
  - validation: `Попросите проверить или завершить`;
  - done: `Новая задача или /resume`.

On submit:

```go
func submitInputCmd(text string) tea.Cmd
```

returns async command:

```go
backend.Exchange(ctx, ChatRequest{
    SessionID: m.session.ID,
    Input: text,
    RequireMemoryProposal: true,
})
```

### 8.2. Лента событий задачи

Файл:

```text
internal/tui/components/timeline.go
```

Использовать `viewport.Model`.

Показывать:

- timestamp short;
- kind;
- stage badge;
- summary;
- collapsed detail.

Audit mapping:

```go
func AuditEventToTimeline(e process.ProcessAuditEvent) TimelineEvent
```

Decision mapping:

- `semantic_intent_call` -> internal step;
- `prompt_improvement_skipped` -> warning/info;
- `transitioned` -> transition event;
- `rejected` -> error event;
- `planning_specialist_summary` -> planning event;
- `agent_call` / `agent_accepted` / `agent_rejected` -> validation/reviewer event.

Do not show raw JSON by default. Structured stage answers should be parsed via existing `process.Parse()` or a new exported render helper, then summarized.

### 8.3. Панель состояния

Файл:

```text
internal/tui/components/status.go
```

Поля:

- model;
- memory model if different;
- profile;
- session id short;
- task id short;
- stage;
- expected action;
- task status;
- current step;
- pending approval;
- latest evidence count.

Input:

```go
type StatusView struct {
    Config app.AppConfig
    Task *app.TaskState
    PendingApproval *Approval
    Busy bool
}
```

Status must degrade when no current task:

```text
task: none | stage: - | expected: -
```

### 8.4. Панель плана и acceptance criteria

Файл:

```text
internal/tui/components/plan.go
```

Показывать:

- objective;
- `PendingPlanning` if exists;
- approved `Plan`;
- `AcceptanceCriteria`;
- `OpenQuestions`;
- `Microtasks`.

Если `Task.PendingPlanning != nil`, панель переходит в approval mode:

```text
Pending plan
[a] approve  [r] reject  [e] ask edits
```

Actions:

- `a`: `backend.ApprovePlanning(ctx, sessionID)`;
- `r`: `backend.RejectPlanning(ctx, sessionID)`;
- `e`: focus input with prompt text like `Что изменить в плане?`.

Backend implementation for approve/reject should call existing process/task path. Preferred:

- approve via `ProcessController` semantic flow if possible: send user approval text through `Exchange`;
- or direct `Tasks.ApprovePendingPlanningProposal()` only if this remains a UI shortcut over an explicit user approval and audit is recorded.

Recommended first implementation: use `Exchange()` with deterministic user text:

```text
Подтверждаю план. Переходи к execution.
```

This keeps lifecycle/audit inside `ProcessController`.

### 8.5. Панель diff

Файл:

```text
internal/tui/components/diff.go
```

First slice placeholder, not fake patch manager:

- show `AppliedArtifacts` from `ChatResponse`;
- optionally show `git diff -- <file>` only after future controlled read/search/git layer exists;
- panel title: `Diff`;
- empty state: `нет patch preview; applied artifacts будут показаны здесь`.

Future contract:

```go
type DiffView struct {
    PatchSetID string
    Files []DiffFile
    Selected int
}

type DiffFile struct {
    Path string
    Status string
    Hunks []DiffHunk
}
```

Do not shell out from TUI component. Future diff loading must go through backend/policy.

### 8.6. Панель evidence

Файл:

```text
internal/tui/components/evidence.go
```

Sources:

- `Task.ValidationEvidence`;
- `ProcessAuditEvent.EvidenceRefs`;
- `process.TrustedEvidenceStore.Validate()` for full records when task/session match.

Backend method:

```go
Evidence(ctx context.Context, taskID, sessionID string, refs []string) ([]EvidenceView, error)
```

View:

```go
type EvidenceView struct {
    Ref string
    ID string
    Command string
    ExitCode int
    Summary string
    CreatedAt time.Time
    OutputPreview string
}
```

Panel rendering:

```text
go test ./...
exit: 0
evidence: ev_...
summary: passed
```

Logs collapsed by default. Key:

- `o`: toggle output preview;
- `y`: copy evidence ref later, if clipboard support is added.

### 8.7. Панель memory proposal

Файл:

```text
internal/tui/components/memory.go
```

Sources:

- `ChatResponse.Proposal`;
- `backend.LatestPendingProposal(ctx, sessionID)`;
- `ProposalStore.LatestPending()`.

Show per record:

- layer;
- kind;
- content;
- reason;
- confidence;
- status;
- block reason.

Actions:

- `a`: accept selected;
- `r`: reject selected;
- `A`: accept all;
- `R`: reject all;
- `e`: edit selected content/layer later.

Backend:

```go
type MemoryApplyRequest struct {
    SessionID string
    TaskID string
    ProposalID string
    AcceptAll bool
    RejectAll bool
    AcceptIDs []string
    RejectIDs []string
    Edits map[string]MemoryEdit
}
```

Map to `memory.ApplyOptions`, including work-layer blocking via existing `runtime.workApplyContext()`.

### 8.8. Approvals inline approve/reject

Файл:

```text
internal/tui/components/approvals.go
```

Approval bar appears above input when pending:

```text
Требуется подтверждение: план готов к execution
[a] approve  [r] reject  [m] modify
```

Detection:

```go
func DerivePendingApproval(state SessionState) *Approval
```

Rules:

- `Task.PendingPlanning != nil` -> planning approval;
- latest `MemoryProposal` has pending records -> memory approval;
- task paused -> resume approval;
- future patchset pending -> patch approval;
- future verification command pending -> verification approval.

Key handling lives in root `Model.Update()`, not inside every component, so shortcuts do not conflict.

### 8.9. Session resume

Файлы:

```text
internal/tui/session.go
internal/tui/components/help.go
```

First slice:

- on startup load current task via `backend.CurrentTask()`;
- load latest audit events via `backend.LatestAudit(80)`;
- load latest pending memory proposal for latest session if possible;
- reconstruct timeline from audit + task history;
- show resume banner if current task exists and not done.

Later slice:

- persistent TUI session records;
- session picker;
- resume exact session ID;
- timeline from AgentRun ledger if implemented.

Current storage does not have a full TUI session ledger. Do not invent one in P0 unless needed. Use current task + audit as enough resume data.

## 9. Chat model: TUI <-> backend loop

### Message types

```go
type exchangeStartedMsg struct {
    Input string
}

type exchangeFinishedMsg struct {
    Response ChatResponse
}

type exchangeFailedMsg struct {
    Err error
}

type taskUpdatedMsg struct {
    Task app.TaskState
}

type proposalAppliedMsg struct {
    Result memory.ApplyResult
}
```

### Update flow

1. User presses enter.
2. Model appends `EventUserInput`.
3. Model sets `busy=true`, disables input, starts spinner.
4. `tea.Cmd` calls `backend.Exchange()`.
5. On success:
   - append assistant event;
   - append audit-derived events;
   - update task;
   - update model/profile from response;
   - update proposal/evidence/warnings;
   - derive pending approval.
6. On failure:
   - append error event;
   - show typed code/message/hint;
   - keep input enabled.
7. Always set `busy=false`.

### Provider progress

Existing `startAPIProgress()` prints to stderr. TUI must not use it. Instead:

- set spinner on exchange start;
- status bar shows `model call...`;
- event added only when exchange completes/fails.

Backend should accept a quiet/no-progress option so TUI owns rendering.

## 10. Structured answer rendering

Current CLI renderer has useful private functions:

- `renderStructuredStageAnswer()`;
- `renderStructuredStageJSON()`;
- `writeTaskSummary()`;
- `writeEvidenceAndWarnings()`;
- `writeProposalSummary()`.

Do not copy all of them into TUI.

Recommended extraction:

```text
internal/render/
  chat.go
  structured.go
  task.go
  proposal.go
  terminal.go
```

Exports:

```go
func ParseStageAnswer(answer string) (StageAnswer, bool)
func SummarizeStageAnswer(answer string) []SummaryBlock
func TaskSummary(state app.TaskState) TaskSummaryView
func ProposalSummary(proposal app.MemoryProposal) ProposalSummaryView
func SafeTerminalText(text string) string
```

Then:

- CLI text renderer uses `internal/render`;
- TUI uses `internal/render`;
- no raw JSON in user happy path.

This extraction can be a separate early step before Bubble Tea wiring.

## 11. Implementation phases

### Phase 0: prepare boundaries

Files:

- add `internal/cli/tui_adapter.go`;
- add `internal/tui/adapter.go`;
- optionally add `internal/render/*`.

Work:

1. Define backend interfaces and DTOs.
2. Wrap existing `runtime` and `runChatExchange()` behind `ChatBackend`.
3. Add tests for adapter using fake provider/storage temp dir.
4. Extract render helpers only where needed.

Acceptance:

- old `assistant chat --plain` behavior unchanged;
- no new TUI yet required.

### Phase 1: minimal TUI shell

Files:

- `internal/tui/app.go`;
- `internal/tui/model.go`;
- `internal/tui/messages.go`;
- `internal/tui/keymap.go`;
- `internal/tui/styles.go`;
- `internal/tui/layout/layout.go`;
- `internal/tui/components/input.go`;
- `internal/tui/components/timeline.go`;
- `internal/tui/components/status.go`.

Work:

1. Build Bubble Tea program with header, timeline, status, input.
2. Wire `tea.WindowSizeMsg`.
3. Implement input submit -> backend exchange.
4. Render answer summaries and errors.
5. Add `--tui` flag.
6. Keep `--plain` fallback.

Acceptance:

- `assistant chat --tui` opens TUI in interactive terminal;
- user can send normal message;
- response appears in timeline;
- task status panel updates;
- errors render in TUI, not as broken terminal output.

### Phase 2: task panels

Files:

- `internal/tui/components/plan.go`;
- extend `status.go`;
- extend `timeline.go`.

Work:

1. Show current task on startup.
2. Show plan, criteria, open questions, microtasks.
3. Detect `PendingPlanning`.
4. Add approve/reject planning keys.
5. Update task state after approval/rejection.
6. Render transition events.

Acceptance:

- planning proposal appears as pending approval;
- approve moves task through existing process flow;
- reject clears pending planning;
- plan/criteria panel reflects persisted state.

### Phase 3: evidence and warnings

Files:

- `internal/tui/components/evidence.go`;
- add backend evidence method;
- possibly add exported evidence read method in `internal/process`.

Work:

1. Show validation evidence refs from task/audit.
2. Resolve refs to records with `TrustedEvidenceStore.Validate()`.
3. Show command, exit code, output preview.
4. Show warnings/errors with code/message/hint.

Acceptance:

- after trusted verification, evidence panel shows command and ref;
- missing/unreadable evidence is shown as warning, not crash.

### Phase 4: memory proposals

Files:

- `internal/tui/components/memory.go`;
- adapter memory apply methods.

Work:

1. Show latest proposal records.
2. Support accept/reject selected and all.
3. Use `ProposalStore.Apply()`.
4. Respect work memory apply context.
5. Update proposal statuses after apply.

Acceptance:

- pending records visible;
- accept/reject changes status;
- blocked records show block reason;
- no direct writes bypassing `ProposalStore`.

### Phase 5: diff placeholder and applied artifacts

Files:

- `internal/tui/components/diff.go`.

Work:

1. Show applied artifact paths from `ChatResponse.AppliedArtifacts`.
2. Provide placeholder for future PatchSet preview.
3. Add tab/pane navigation to inspect artifacts list.

Acceptance:

- applied files are visible in TUI;
- UI does not claim patch preview exists before PatchSet exists.

### Phase 6: session resume

Files:

- `internal/tui/session.go`;
- maybe `internal/tui/components/session.go`.

Work:

1. Startup reads current task.
2. Startup reads latest audit events.
3. Rebuild initial timeline.
4. Show resume banner for active/paused task.
5. Add resume action for paused task.

Acceptance:

- restarting `assistant chat --tui` shows current task context;
- paused task can be resumed;
- done task does not pretend to be active.

### Phase 7: make TUI default

Work:

1. Set interactive `assistant chat` default to TUI.
2. Keep `assistant chat --plain` as fallback.
3. Document fallback in help text.
4. Ensure `--json --once` unchanged.

Acceptance:

- normal interactive `assistant chat` starts TUI;
- scripts/non-interactive flows still use old REPL semantics;
- `--plain` always bypasses Bubble Tea.

## 12. Fallback: plain REPL

Keep `runREPL()` as supported mode.

Use cases:

- dumb terminals;
- non-interactive scripts;
- CI smoke;
- debugging raw process output;
- users who explicitly pass `--plain`.

Rules:

- no TUI dependency should be required for `--once`;
- no TUI dependency should change JSON output;
- terminal setup failure in default TUI should offer `--plain`;
- `runREPL()` should continue to use `renderChatResult()`.

## 13. Testing strategy

Do not test terminal escape sequences as product behavior. Test state transitions and rendered text snapshots.

### Unit tests

Files:

```text
internal/tui/layout/layout_test.go
internal/tui/model_test.go
internal/tui/components/input_test.go
internal/tui/components/timeline_test.go
internal/tui/components/plan_test.go
internal/tui/components/evidence_test.go
internal/tui/components/memory_test.go
```

Test:

- layout computes stable regions for wide/narrow;
- input history navigation;
- submit disables input and emits command;
- exchange success updates timeline/task/proposal;
- exchange error appends error event and re-enables input;
- pending planning derives approval;
- memory proposal actions call backend with correct IDs;
- evidence refs are rendered compactly.

### Fake backend

Create:

```text
internal/tui/fake_backend_test.go
```

Fake backend fields:

```go
type fakeBackend struct {
    config app.AppConfig
    task *app.TaskState
    responses []ChatResponse
    err error
    applied []MemoryApplyRequest
}
```

Use it to test Bubble Tea `Update()` without provider calls.

### CLI integration tests

Extend `internal/cli/root_test.go`:

- `assistant chat --plain` uses old REPL path;
- `assistant chat --json` without `--once` still errors;
- `assistant chat --tui` rejects non-interactive stdin/stdout;
- `assistant chat --once` does not initialize TUI;
- interactive detection can be tested through helper functions, not real terminal.

### Golden render tests

Use stable string snapshots for:

- status panel with no task;
- status panel with execution task;
- plan panel with pending planning;
- evidence panel with command;
- memory proposal panel with accepted/rejected/blocked records.

Avoid brittle color snapshots. Styles should be disabled in tests or rendered with plain style.

### Manual verification later

When implementation exists, manual test:

```text
assistant chat --tui
```

Scenario:

1. start with empty storage;
2. enter normal coding task;
3. see planning proposal;
4. approve plan inline;
5. see stage/status update;
6. produce execution output;
7. see applied artifacts/diff placeholder;
8. trigger trusted verification;
9. see evidence panel;
10. accept/reject memory proposal;
11. quit and restart;
12. see session resume from current task/audit.

## 14. Risk areas

### Private CLI functions

Many useful functions are unexported in `internal/cli/root.go`. Avoid moving everything at once. First add adapter methods in same package. Later extract shared rendering/backend types.

### TUI ownership creep

TUI must not implement semantic routing, lifecycle transitions, verification command inference, invariant checks, or memory safety. It only sends user intent and approval actions to backend.

### Raw JSON leakage

Stage outputs are currently structured JSON from process agents. TUI must parse/summarize them. Raw JSON can be available in detail view, but not the default timeline.

### Evidence display

Evidence tokens may be stale or session-bound. If `TrustedEvidenceStore.Validate()` fails, show warning and token, not panic.

### Materialized artifacts

Current `materializeExecutionDeliverable()` writes files directly from execution output. TUI should show `AppliedArtifacts`, but future PatchSet preview should replace direct materialization in the happy path.

### Terminal resize

Every component must handle tiny sizes. Minimum behavior:

- header one line;
- input one line;
- active pane fills remaining space;
- sidebar hidden under narrow breakpoint.

## 15. Recommended first PR shape

First implementation PR should be small:

1. Add Bubble Tea dependencies.
2. Add backend interface and CLI adapter.
3. Add minimal TUI with input/timeline/status.
4. Add `--tui` and `--plain`.
5. Keep default still plain until minimal TUI is stable.

Second PR:

1. Make TUI default for interactive `assistant chat`.
2. Add plan/approval panel.
3. Add resume from current task/audit.

Third PR:

1. Add evidence panel.
2. Add memory proposal panel.
3. Add diff placeholder.

This reduces risk: no lifecycle rewrite, no renderer rewrite, no PatchSet dependency before product needs it.

## 16. Done definition for TUI P0

TUI P0 is done when:

- interactive `assistant chat` opens TUI by default;
- user can complete a normal task flow without raw JSON;
- task status, stage, expected action, plan and criteria are visible;
- pending planning approval is inline approve/reject;
- memory proposal is visible with accept/reject;
- trusted evidence is visible after verification;
- applied artifacts are visible;
- session resume shows current task after restart;
- `assistant chat --plain` still works;
- `assistant chat --once --json` remains unchanged;
- TUI model/components have unit tests with fake backend.
