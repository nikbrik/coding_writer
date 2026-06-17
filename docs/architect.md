# Architecture: минимальный CLI code assistant

## 1. Архитектурная идея

Ассистент строится как маленький stateful CLI-agent.

Ключевой принцип: LLM не должна сама решать, что помнить и какие правила важны. Приложение хранит состояние явно, разделяет memory layers, собирает prompt через prompt builder и постепенно добавляет deterministic checks.

## 1.1. Canonical contract

Architecture, PRD и FRD must share one canonical contract for Day 11, Day 12, and Day 13.

### Task state

- `stage`: `planning`, `execution`, `validation`, `done`.
- `status`: `active`, `paused`.
- `expected_action`: `user_input`, `llm_response`, `user_confirmation`, `none`.
- terminal completion is `stage=done` and `expected_action=none`.
- `status=done` is not part of MVP.
- `tool_result` is not part of P0 because MVP has no tool execution; it is reserved for P1.

### Commands

- top-level and slash commands must describe one canonical command tree;
- P0 includes only commands needed for the mandatory Day 11/12/13 demo path and smoke tests.

### Memory layers

- only physical storage layers: `short`, `work`, `long`;
- `ignore` exists only in proposal/audit trail.

Главные блоки:

- CLI interface;
- OpenRouter provider;
- memory manager;
- memory classifier;
- profile manager;
- task state manager;
- prompt builder;
- response loop;
- invariant checker.

## 2. Рекомендуемый стек

Для MVP лучше взять Go 1.22+.

Причины:

- один статический CLI-бинарник без Python runtime;
- стандартная библиотека уже закрывает JSON, files, HTTP и env;
- простая модель проекта для небольшого ассистента;
- быстрый старт без тяжёлого dependency graph;
- строгая типизация достаточна для memory layers, profiles и task state;
- легко распространять локально: `go install` или один compiled binary.

Минимальные зависимости:

- `cobra` для CLI commands;
- `bubbletea` и `bubbles` опционально для интерактивного TUI;
- `lipgloss` опционально для оформления терминала;
- без внешнего HTTP client: достаточно `net/http`;
- без внешнего JSON layer: достаточно `encoding/json`.

Rust тоже подходит, если цель — максимальная строгость типов и control over errors. Но для учебного MVP Go прагматичнее: меньше boilerplate, быстрее собрать интерактивный CLI, проще сфокусироваться на memory model и персонализации.

Rust можно выбрать, если заранее важно:

- тренировать ownership/error handling;
- делать максимально надёжный локальный binary;
- проектировать storage и state machine через более строгую type system;
- использовать `clap`, `ratatui`, `reqwest`, `serde`.

Итоговое решение для MVP: Go. Rust оставить как альтернативу для переписывания или второй версии после проверки архитектуры.

## 3. Файловая структура приложения

Предлагаемая структура:

```text
cmd/assistant/
  main.go

internal/app/
  app.go
  config.go
  models.go

internal/cli/
  root.go
  chat.go
  commands.go
  interactive.go

internal/providers/
  provider.go
  openrouter.go

internal/memory/
  manager.go
  classifier.go
  short_term.go
  working.go
  long_term.go
  storage.go

internal/profiles/
  manager.go
  interview.go

internal/tasks/
  manager.go
  state_machine.go

internal/prompting/
  builder.go
  templates.go

internal/validation/
  invariants.go
  redaction.go

tests/
  memory_layers_test.go
  profiles_test.go
  prompt_builder_test.go
  state_machine_test.go
```

## 4. Runtime storage

Runtime data нужно хранить отдельно от исходного кода. Normal default: OS user-data directory with restrictive permissions (`0700` directories, `0600` sensitive files). Repo-local `.assistant/` остаётся explicit demo/test opt-in, например `--storage-dir .assistant`, и должен быть gitignored.

Storage contract:

- OS user-data directory is the normal default;
- repo-local `.assistant/` is demo/test opt-in only;
- path precedence must be documented in FRD before coding;
- storage writes must be canonical-path safe, atomic, locked, and recoverable;
- path traversal, absolute paths, encoded separators, unsafe IDs, symlinked parents, symlinked files, and symlink writes must be rejected.

Структура storage:

```text
<storage_root>/
  config.json
  profiles/
    student.json
    senior.json
  sessions/
    2026-06-17T10-00-00Z/
      short_term.jsonl
      transcript.md
  tasks/
    current.json
    task-001.json
  long_term/
    decisions.jsonl
    knowledge.jsonl
    constraints.jsonl
  logs/
    app.log
```

Секреты:

- предпочтительно хранить OpenRouter key только в `OPENROUTER_API_KEY`;
- if MVP ever supports any local key file, it must be an explicit non-default opt-in and outside git;
- pre-provider checker обязан редактировать или отклонять secret-like данные перед любым chat/classifier provider call;
- memory manager обязан редактировать или отклонять секреты перед сохранением.

Privacy contract:

- classify what categories of data are sent to the provider;
- provider calls must have timeout, typed errors, and bounded retries;
- custom base URL is explicit opt-in only and must be HTTPS/allowlisted;
- API key must not be persisted in config, profiles, memory, transcripts, or audit data.

## 5. Data models

### 5.1. ChatMessage

```go
type ChatRole string

const (
    RoleSystem    ChatRole = "system"
    RoleUser      ChatRole = "user"
    RoleAssistant ChatRole = "assistant"
)

type ChatMessage struct {
    Role      ChatRole  `json:"role"`
    Content   string    `json:"content"`
    CreatedAt time.Time `json:"created_at"`
}
```

Назначение: единый формат сообщений для OpenRouter и short-term history.

### 5.2. MemoryRecord

```go
type MemoryLayer string

const (
    LayerShort MemoryLayer = "short"
    LayerWork  MemoryLayer = "work"
    LayerLong  MemoryLayer = "long"
)

type MemoryRecord struct {
    ID        string      `json:"id"`
    Layer     MemoryLayer `json:"layer"`
    Kind      string      `json:"kind"`
    Content   string      `json:"content"`
    Source    string      `json:"source"`
    Tags      []string    `json:"tags"`
    CreatedAt time.Time   `json:"created_at"`
}
```

Назначение: универсальная запись памяти. `layer` определяет физическое хранилище.

### 5.3. UserProfile

```go
type UserProfile struct {
    ID             string            `json:"id"`
    DisplayName    string            `json:"display_name"`
    Style          map[string]string `json:"style"`
    ResponseFormat map[string]string `json:"response_format"`
    Constraints    []string          `json:"constraints"`
    DefaultModel   string            `json:"default_model,omitempty"`
    CreatedAt      time.Time         `json:"created_at"`
    UpdatedAt      time.Time         `json:"updated_at"`
}
```

Назначение: персонализация. Этот объект подключается к каждому prompt.

### 5.4. TaskState

```go
type TaskStage string

const (
    StagePlanning   TaskStage = "planning"
    StageExecution  TaskStage = "execution"
    StageValidation TaskStage = "validation"
    StageDone       TaskStage = "done"
)

type TaskStatus string

const (
    TaskStatusActive TaskStatus = "active"
    TaskStatusPaused TaskStatus = "paused"
)

type ExpectedAction string

const (
    ExpectedUserInput        ExpectedAction = "user_input"
    ExpectedLLMResponse      ExpectedAction = "llm_response"
    ExpectedUserConfirmation ExpectedAction = "user_confirmation"
    ExpectedNone             ExpectedAction = "none"
)

type TaskState struct {
    ID                 string         `json:"id"`
    Title              string         `json:"title"`
    Stage              TaskStage      `json:"stage"`
    CurrentStep        string         `json:"current_step"`
    ExpectedAction     ExpectedAction `json:"expected_action"`
    Status             TaskStatus     `json:"status"`
    Objective          string         `json:"objective"`
    AcceptanceCriteria []string       `json:"acceptance_criteria"`
    Plan               []string       `json:"plan"`
    Decisions          []string       `json:"decisions"`
    OpenQuestions      []string       `json:"open_questions"`
    ValidationStatus   string         `json:"validation_status,omitempty"`
    PausedAt           *time.Time     `json:"paused_at,omitempty"`
    ResumedAt          *time.Time     `json:"resumed_at,omitempty"`
    UpdatedAt          time.Time      `json:"updated_at"`
}
```

Назначение: формализованное состояние текущей задачи по Day 13. Оно хранит не только этап, но и текущий шаг с ожидаемым действием, чтобы задачу можно было поставить на паузу и продолжить без повторного объяснения.

### 5.5. AppConfig

```go
type AppConfig struct {
    ActiveProfileID   string `json:"active_profile_id,omitempty"`
    ActiveModel       string `json:"active_model,omitempty"`
    StorageDir        string `json:"storage_dir"`
    OpenRouterBaseURL string `json:"openrouter_base_url"`
    MemoryModel       string `json:"memory_model,omitempty"`
}
```

Назначение: локальные настройки без секретов.

Config precedence:

- CLI flags > env vars > config file > defaults.
- `OPENROUTER_API_KEY` is env-only for MVP.
- `StorageDir`, `OpenRouterBaseURL`, `ActiveModel`, `MemoryModel`, and `ActiveProfileID` use field-level flag/env/config/default rules from FRD.
- `OpenRouterBaseURL` must be explicit opt-in when not default.

## 6. Компоненты

### 6.1. CLI

Отвечает за:

- запуск `init`, `chat`, `profiles`, `memory`;
- интерактивный цикл ввода;
- обработку slash-команд;
- выбор модели;
- выбор профиля;
- вывод ошибок и статуса.

CLI не должен напрямую писать в файлы памяти. Он вызывает managers.

### 6.2. OpenRouterProvider

Отвечает за:

- получение API key только из `OPENROUTER_API_KEY` для P0;
- pre-provider scan/redaction before every chat/classifier request;
- запрос списка моделей;
- вызов `/chat/completions`;
- нормализацию ошибок OpenRouter;
- возврат текста ответа в общий формат.

Интерфейс:

```go
type LLMProvider interface {
    ListModels(ctx context.Context) ([]string, error)
    Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}

type CompletionPurpose string

const (
    PurposeChat       CompletionPurpose = "chat"
    PurposeClassifier CompletionPurpose = "classifier"
)

type CompletionRequest struct {
    Purpose     CompletionPurpose `json:"purpose"`
    Model       string            `json:"model"`
    Messages    []ChatMessage     `json:"messages"`
    JSONMode    bool              `json:"json_mode"`
    Temperature *float64          `json:"temperature,omitempty"`
}

type CompletionResponse struct {
    Message      ChatMessage `json:"message"`
    ProviderID   string      `json:"provider_id,omitempty"`
    Model        string      `json:"model"`
    RetryCount   int         `json:"retry_count"`
    UsageSummary string      `json:"usage_summary,omitempty"`
}
```

Так можно позже заменить OpenRouter на другой provider, а fake provider сможет записывать calls для Day 11/12/13 assertions без live key.

### 6.3. MemoryManager

Отвечает за:

- запись в нужный слой;
- чтение слоя;
- очистку short-term memory;
- выбор релевантных записей для prompt;
- запрет сохранения секретов;
- перенос части working memory в long-term после завершения задачи.

Публичные методы:

```go
Save(ctx context.Context, layer MemoryLayer, content, kind, source string) (MemoryRecord, error)
List(ctx context.Context, layer MemoryLayer) ([]MemoryRecord, error)
ClearShort(ctx context.Context, sessionID string) error
SelectForPrompt(ctx context.Context, profileID string, taskID string) (MemoryBundle, error)
```

Storage scope:

- save/apply methods must carry explicit session/task/proposal scope in the real implementation;
- proposal application must be idempotent by proposal ID;
- `ignore` must never be stored as a physical memory layer.

### 6.4. MemoryClassifier

Отвечает за LLM-выбор того, какие факты куда сохранять.

Это отдельный компонент поверх OpenRouterProvider. Он не сохраняет данные сам. Он только возвращает `MemoryProposal`.

Вход:

- последнее сообщение пользователя;
- последний ответ ассистента;
- active profile;
- current task state;
- краткие правила memory layers;
- список уже сохранённых похожих records, если нужен deduplication.

Выход:

- список proposed records;
- слой для каждого record: `short`, `work`, `long`, `ignore`;
- причина выбора;
- confidence;
- флаг `requires_confirmation`.

Интерфейс:

```go
type MemoryProposal struct {
    Records []ProposedMemoryRecord `json:"records"`
}

type ProposedMemoryLayer string

const (
    ProposedLayerShort  ProposedMemoryLayer = "short"
    ProposedLayerWork   ProposedMemoryLayer = "work"
    ProposedLayerLong   ProposedMemoryLayer = "long"
    ProposedLayerIgnore ProposedMemoryLayer = "ignore"
)

type ProposedMemoryRecord struct {
    Layer      ProposedMemoryLayer `json:"layer"`
    Kind       string              `json:"kind"`
    Content    string              `json:"content"`
    Reason     string              `json:"reason"`
    Confidence float64             `json:"confidence"`
}

type MemoryClassifier interface {
    Propose(ctx context.Context, input MemoryClassificationInput) (MemoryProposal, error)
}
```

`ignore` есть только в proposal. Физического memory layer `ignore` нет: такие записи не попадают в memory storage, но остаются в audit trail.

Auditability:

- classifier input is untrusted data and must be serialized/redacted before provider calls;
- invalid JSON must not create records;
- proposal audit must store provider/model/template/hash/retry/error metadata.

MemoryClassifier использует тот же OpenRouter API. Модель может быть:

- такой же, как active chat model;
- отдельной дешёвой моделью из config: `memory_model`;
- fake provider в deterministic tests, но через тот же `MemoryClassifier` interface.

Если classifier call отключён или упал, это не закрывает Day 11 acceptance: memory records не создаются, кроме redacted failure audit.

### 6.5. MemoryProposalStore

Отвечает за audit trail: что LLM предложила сохранить и что реально было сохранено.

Файл:

```text
<storage_root>/sessions/<session_id>/memory_proposals.jsonl
```

Зачем нужен:

- выполнить критерий проверки `какие данные попадают в каждый слой`;
- видеть, что было проигнорировано;
- отлаживать ошибки классификации;
- сравнивать proposal и actual saved records.

Структура записи:

```json
{
  "id": "proposal_001",
  "source_message_ids": ["msg_010", "msg_011"],
  "records": [
    {
      "layer": "work",
      "kind": "requirement",
      "content": "CLI должен поддерживать выбор модели OpenRouter.",
      "reason": "Требование текущей задачи.",
      "confidence": 0.91,
      "status": "accepted"
    }
  ],
  "created_at": "2026-06-17T10:00:00Z"
}
```

### 6.6. ProfileManager

Отвечает за:

- создание профиля;
- короткое интервью при первом запуске;
- переключение active profile;
- обновление style, format, constraints;
- сериализацию профиля в prompt-friendly текст.

Профиль не должен смешиваться с обычной историей чата. Он хранится отдельно и подключается каждый раз.

### 6.7. TaskStateManager

Отвечает за:

- создание текущей задачи;
- хранение `stage`;
- хранение `current_step`;
- хранение `expected_action`;
- хранение `status`: active, paused;
- обновление plan, decisions, acceptance criteria;
- проверку allowed transitions;
- паузу задачи на любом этапе;
- resume задачи без повторного объяснения;
- выдачу task context для prompt builder.

Day 13 требует формального конечного автомата, поэтому переходы нужно валидировать кодом. Если переход запрещён, manager возвращает ошибку и состояние не меняется.

Публичные методы:

```go
Start(ctx context.Context, title string) (TaskState, error)
Move(ctx context.Context, next TaskStage) (TaskState, error)
SetStep(ctx context.Context, step string) (TaskState, error)
SetExpectedAction(ctx context.Context, action ExpectedAction) (TaskState, error)
Pause(ctx context.Context) (TaskState, error)
Resume(ctx context.Context) (TaskState, error)
Current(ctx context.Context) (TaskState, error)
```

### 6.8. PromptBuilder

Отвечает за сборку prompt.

Вход:

- base system prompt;
- active profile;
- task state;
- memory bundle;
- short-term messages;
- user query.

Выход:

- `[]ChatMessage` для OpenRouter.

PromptBuilder должен быть чистым компонентом: без HTTP, без записи файлов, без побочных эффектов.

Canonical untrusted block schema:

```text
<context_block id="task.current" type="task_state" source="storage" trust="untrusted">
escaped structured data
</context_block>
```

Required block types: `profile`, `task_state`, `working_memory`, `long_memory`, `short_history`, `classifier_input`, `classifier_output`. Golden prompt tests must include injection strings inside every block type.

### 6.9. InvariantChecker

Отвечает за:

- поиск секретов перед сохранением памяти;
- проверку LLM memory proposal перед сохранением;
- проверку конфликтов профиля и user request;
- предупреждения о нарушении stage;
- будущую validation loop после ответа LLM.

MVP-инварианты:

- API keys не сохраняются в memory;
- secret-like content не отправляется provider до локального scan/redaction;
- long-term memory сохраняется только явно;
- LLM proposal не применяется silently;
- профиль подключён к каждому prompt;
- layer записи совпадает с LLM proposal или выбранной командой `/save`.

## 7. Схема взаимодействия компонентов

Этот раздел описывает, как части ассистента связаны в MVP и во что схема должна вырасти дальше.

### 7.1. MVP component map

```text
┌─────────────────────────────────────────────────────────────┐
│                        User terminal                        │
└──────────────────────────────┬──────────────────────────────┘
                               │ input, slash commands
                               v
┌─────────────────────────────────────────────────────────────┐
│                             CLI                             │
│ root command, chat loop, command parser, model/profile UI    │
└──────────────┬───────────────┬───────────────┬──────────────┘
               │               │               │
               v               v               v
┌───────────────────┐ ┌────────────────┐ ┌───────────────────┐
│  ProfileManager   │ │ TaskStateMgr   │ │   MemoryManager   │
│ active profile    │ │ stage/step/act │ │ short/work/long   │
└─────────┬─────────┘ └────────┬───────┘ └─────────┬─────────┘
          │                    │                   │
          └────────────┬───────┴───────────┬───────┘
                       v                   v
              ┌────────────────────────────────┐
              │        InvariantChecker        │
              │ secrets, layer rules, conflicts│
              └────────────────┬───────────────┘
                               │ validated context
                               v
              ┌────────────────────────────────┐
              │         PromptBuilder          │
              │ system + profile + task + mem  │
              └────────────────┬───────────────┘
                               │ []ChatMessage
                               v
              ┌────────────────────────────────┐
              │       OpenRouterProvider       │
              │ net/http -> chat completions   │
              └────────────────┬───────────────┘
                               │ assistant response
                               v
              ┌────────────────────────────────┐
              │       MemoryClassifier         │
              │ OpenRouter -> memory proposal  │
              └────────────────┬───────────────┘
                               │ proposed short/work/long/ignore
                               v
              ┌────────────────────────────────┐
              │              CLI               │
              │ print answer + memory proposal │
              └────────────────┬───────────────┘
                               │ user confirms/edits
                               v
              ┌────────────────────────────────┐
              │         MemoryManager          │
              │ save accepted records          │
              └────────────────────────────────┘
```

Смысл схемы: CLI только оркестрирует. Память, профиль, состояние задачи, prompt и API-клиент живут отдельно. Так MVP не превращается в один большой `chat.go`.

### 7.2. MVP request lifecycle

Обычный пользовательский запрос проходит такой путь:

```text
1. User вводит обычное сообщение
2. CLI определяет: это не slash-команда
3. App загружает runtime context
4. ProfileManager читает active profile
5. TaskStateManager читает current task, если она есть
6. MemoryManager выбирает нужные memory records
7. TaskStateManager проверяет status: active или paused
8. InvariantChecker проверяет контекст до prompt
9. PromptBuilder собирает []ChatMessage со stage/current_step/expected_action
10. PreProviderScanner блокирует или редактирует secret-like данные
11. OpenRouterProvider отправляет request
12. Response возвращается в CLI
13. CLI печатает ответ
14. MemoryManager дописывает user/assistant сообщения в short-term history
15. MemoryClassifier получает user message + assistant response
16. PreProviderScanner проверяет classifier payload
17. MemoryClassifier вызывает OpenRouter и возвращает MemoryProposal
18. InvariantChecker проверяет proposal на секреты и layer rules
19. CLI показывает proposal пользователю
20. Пользователь подтверждает, редактирует или отклоняет
21. MemoryManager сохраняет accepted records в отдельные хранилища
```

Ключевое ограничение: LLM не пишет память напрямую. Она явно выбирает слой памяти через MemoryClassifier, но запись делает только приложение после показа proposal и подтверждения пользователя.

### 7.3. Slash-command lifecycle

Slash-команда не всегда вызывает LLM. Большинство команд работают локально.

```text
User input: /save long Предпочитаю короткие ответы
  -> CLI parses command
  -> CommandRouter resolves handler SaveMemory
  -> InvariantChecker checks secret leakage
  -> MemoryManager routes record to long-term storage
  -> LongTermStorage appends <storage_root>/long_term/*.jsonl
  -> CLI prints saved record id
```

```text
User input: /memory propose
  -> CLI takes latest user+assistant exchange
  -> MemoryClassifier calls OpenRouter
  -> InvariantChecker validates proposal
  -> MemoryProposalStore saves proposal audit record
  -> CLI prints proposed short/work/long/ignore records
```

```text
User input: /memory apply
  -> CLI loads latest pending MemoryProposal
  -> User confirms selected records
  -> MemoryManager saves accepted records by layer
  -> MemoryProposalStore marks records accepted/rejected/edited
```

```text
User input: /profile senior
  -> CLI parses command
  -> ProfileManager checks profiles/senior.json
  -> ConfigManager updates active_profile_id
  -> CLI prints active profile summary
```

```text
User input: /task move execution
  -> CLI parses command
  -> TaskStateManager loads current task
  -> StateMachine checks transition
  -> if allowed: update current.json
  -> if forbidden: return warning or error
```

```text
User input: /task step "сформировать acceptance criteria"
  -> CLI parses command
  -> TaskStateManager loads current task
  -> TaskStateManager updates current_step
  -> current.json persists updated step
```

```text
User input: /task expect user_confirmation
  -> CLI parses command
  -> TaskStateManager validates expected_action value
  -> TaskStateManager updates current expected action
```

```text
User input: /task pause
  -> CLI parses command
  -> TaskStateManager loads current task
  -> TaskStateManager sets status=paused and paused_at
  -> current.json persists full task state
```

```text
User input: /task resume
  -> CLI parses command
  -> TaskStateManager loads current task
  -> TaskStateManager restores stage/current_step/expected_action
  -> TaskStateManager sets status=active and resumed_at
  -> PromptBuilder includes restored task context in next LLM call
```

### 7.4. Data ownership

Компоненты владеют данными строго:

```text
ConfigManager
  owns: <storage_root>/config.json
  used by: CLI, ProfileManager, OpenRouterProvider

ProfileManager
  owns: <storage_root>/profiles/*.json
  returns: UserProfile, prompt profile block

TaskStateManager
  owns: <storage_root>/tasks/current.json, task-*.json
  returns: TaskState, allowed transitions, task prompt block, pause/resume state

MemoryManager
  owns: <storage_root>/sessions/*, <storage_root>/tasks/<task_id>/work_memory.jsonl, <storage_root>/long_term/*
  returns: MemoryBundle for prompt, memory listings

WorkMemoryStore
  owns: <storage_root>/tasks/<task_id>/work_memory.jsonl
  used by: MemoryManager, PromptBuilder, TaskStateManager resume flow

MemoryClassifier
  owns: no final memory files
  uses: OpenRouterProvider
  returns: MemoryProposal

MemoryProposalStore
  owns: <storage_root>/sessions/*/memory_proposals.jsonl
  returns: proposal audit history

PromptBuilder
  owns: no files
  returns: []ChatMessage

OpenRouterProvider
  owns: no files
  reads: OPENROUTER_API_KEY
  returns: CompletionResponse
```

Так проще тестировать: каждый manager можно проверить отдельно на временной директории.

### 7.5. Prompt assembly pipeline

Prompt собирается как слоёный объект, а не как склейка всей истории.

```text
Base system rules
  -> security policy
  -> memory policy
  -> active user profile
  -> active task state
  -> current step
  -> expected action
  -> task allowed actions
  -> working memory records
  -> selected long-term records
  -> recent short-term messages
  -> current user query
  -> []ChatMessage
```

Приоритеты:

```text
system rules > security invariants > active profile > task state > working memory > long-term memory > short-term history > current query
```

Если текущий запрос конфликтует с профилем или инвариантом, ассистент должен явно назвать конфликт. Например: пользователь просит сохранить API key в long-term memory, но invariant checker блокирует запись.

Trust contract:

- profile, memory, task state, transcripts, and classifier output are untrusted data;
- these blocks must be serialized/quoted/tagged as data, not instructions;
- system/application/security policy always outranks user/saved context.

### 7.6. Storage interaction

MVP storage файловый и append-friendly.

```text
Read path before LLM call:
  config.json
  -> active profile json
  -> current task json
  -> memory jsonl files
  -> prompt bundle

Write path after local command:
  parsed command
  -> validation
  -> exact target file
  -> append or overwrite

Write path after LLM response:
  user message + assistant response
  -> current session short_term.jsonl
  -> transcript.md optional
  -> MemoryClassifier proposes records
  -> memory_proposals.jsonl audit entry
  -> accepted records go to exact layer files
```

Файловый storage выбран не потому, что это максимум, а потому что он прозрачен для обучения. Пользователь может открыть `<storage_root>/` и увидеть, где лежит каждый слой памяти. Для учебного demo можно явно выбрать repo-local `.assistant/`.

Storage policy:

- writes must be atomic and recoverable;
- path traversal and symlink writes are rejected;
- app should prefer single-writer or locked access in MVP;
- JSONL append must be serialized.

### 7.7. Error and retry boundaries

Ошибки делятся по границам:

```text
CLI errors:
  bad command, missing argument, unknown profile

Storage errors:
  missing file, broken JSON, permission denied

Validation errors:
  secret detected, forbidden transition, layer mismatch

Provider errors:
  missing API key, 401/403, model not found, timeout

Classifier errors:
  invalid JSON, impossible layer, low confidence, duplicate fact

LLM content errors:
  answer conflicts with invariant, answer ignores stage
```

MVP должен retry делать только для provider errors с timeout/temporary network failure и для classifier invalid JSON. Validation errors не retry, а возвращаются пользователю как локальный отказ.

CLI/output contract:

- stdout for primary data, stderr for diagnostics;
- stable error categories should map to typed errors;
- machine-readable mode must remain parseable.

### 7.8. Future architecture map

После MVP архитектура расширяется не через переписывание ядра, а через добавление слоёв вокруг тех же interfaces.

```text
┌─────────────────────────────────────────────────────────────┐
│                           CLI/TUI                           │
└──────────────────────────────┬──────────────────────────────┘
                               v
┌─────────────────────────────────────────────────────────────┐
│                         Agent Core                          │
│ task orchestration, state machine, policy, tool routing      │
└──────┬──────────────┬──────────────┬──────────────┬─────────┘
       │              │              │              │
       v              v              v              v
┌─────────────┐ ┌─────────────┐ ┌──────────────┐ ┌─────────────┐
│ Memory Core │ │ Tool System │ │ Validation   │ │ Prompt/RAG  │
│ layers+RAG  │ │ file/git    │ │ invariants   │ │ retrieval   │
└──────┬──────┘ └──────┬──────┘ └──────┬───────┘ └──────┬──────┘
       │               │               │                │
       v               v               v                v
┌─────────────┐ ┌─────────────┐ ┌──────────────┐ ┌─────────────┐
│ Storage     │ │ Workspace   │ │ Test/Review  │ │ Providers   │
│ files/db/vec│ │ repo files  │ │ loops        │ │ OpenRouter  │
└─────────────┘ └─────────────┘ └──────────────┘ └─────────────┘
```

Будущие additions:

- `Agent Core` станет главным orchestrator вместо простого chat loop;
- `Tool System` добавит controlled file read/edit, git status, tests;
- `Validation` станет post-response loop: ответ LLM проверяется инвариантами и может отправляться на исправление;
- `Prompt/RAG` будет выбирать long-term memory и repo context не только по списку, но и через search/retrieval;
- `Storage` можно заменить с JSONL на SQLite или vector DB без изменения внешних interfaces;
- `Providers` можно расширить до OpenRouter, local models, Anthropic, OpenAI-compatible endpoints.

### 7.9. Future stateful-agent loop

Целевой loop для расширенной версии после MVP:

```text
User request
  -> classify intent
  -> load profile + task + relevant memory
  -> check state machine
  -> choose allowed action
  -> build stage-specific prompt
  -> call LLM
  -> validate response against invariants
  -> if invalid: generate feedback and retry within limit
  -> if valid: show result or execute approved tool
  -> update short-term memory
  -> optionally propose working/long-term memory update
  -> wait for user confirmation
```

Главное отличие от MVP: LLM будет не просто отвечать, а работать внутри контролируемой стадии. Например, в `planning` она не должна менять файлы, а в `validation` не должна добавлять новые features.

### 7.10. Future memory evolution

Memory layers сохраняются, но становятся умнее:

```text
Short-term memory
  MVP: recent messages in JSONL
  Future: rolling summary + raw recent tail

Working memory
  MVP: task JSON + explicit records
  Future: task graph, acceptance criteria, artifacts, validation history

Long-term memory
  MVP: profile, decisions, constraints in JSONL
  Future: indexed knowledge, embeddings, project rules, reusable lessons
```

Важно: даже в future версии запись в long-term memory остаётся подтверждаемой. Автоматические предложения допустимы, silent persistence — нет.

### 7.11. Future storage migration path

Путь миграции storage:

```text
Phase 1: JSON + JSONL files
  transparent, easy debug, enough for MVP

Phase 2: SQLite
  transactions, indexes, easier querying, still local-first

Phase 3: SQLite + vector index
  semantic retrieval for long-term memory and repo notes

Phase 4: remote sync optional
  only if нужен multi-device или team mode
```

Interfaces должны пережить миграцию:

```go
type MemoryStore interface {
    Save(ctx context.Context, record MemoryRecord) error
    List(ctx context.Context, filter MemoryFilter) ([]MemoryRecord, error)
    Search(ctx context.Context, query MemoryQuery) ([]MemoryRecord, error)
}
```

В MVP `Search` может быть простым фильтром по tags/kind. Позже он станет semantic search.

### 7.12. Future tool execution model

Когда появятся code-assistant функции, tools должны быть явно ограничены.

```text
LLM proposes tool call
  -> ToolRouter checks current stage
  -> Policy checks permissions
  -> User approves destructive action if needed
  -> Tool executes
  -> Result goes back into working memory
  -> PromptBuilder includes tool result in next step
```

Примеры tools:

- `read_file`: разрешён в planning/execution/validation;
- `write_file`: разрешён только в execution и только после подтверждения;
- `run_tests`: разрешён в validation;
- `git_status`: разрешён всегда;
- `commit`: не в MVP, только explicit user command.

### 7.13. Future validation loop

Validation loop нужен, чтобы правила были не только текстом в prompt.

```text
LLM response
  -> SecretChecker
  -> StageComplianceChecker
  -> ProfileConstraintChecker
  -> ProjectInvariantChecker
  -> if passed: return response
  -> if failed: build correction prompt
  -> retry until max attempts
  -> if still failed: return validation error
```

Так ассистент постепенно становится stateful-agent, а не thin wrapper around LLM.

## 8. Memory flow

### 8.1. Входящий запрос

```text
User input
  -> CLI parses command or normal message
  -> ProfileManager loads active profile
  -> TaskStateManager loads current task
  -> MemoryManager selects memory bundle
  -> PromptBuilder builds messages
  -> PreProviderScanner redacts/blocks secret-like payload data
  -> OpenRouterProvider.Complete(purpose=chat)
  -> CLI prints assistant answer
  -> short-term history appends user + assistant messages
  -> MemoryClassifier.propose() calls OpenRouter
  -> InvariantChecker validates proposal
  -> CLI prints memory proposal
  -> user accepts/edits/rejects
  -> MemoryManager writes accepted records to separate layers
```

### 8.2. LLM-классификация памяти

```text
Latest exchange
  -> build memory-classifier prompt
  -> PreProviderScanner redacts/blocks secret-like classifier data
  -> OpenRouterProvider.Complete(purpose=classifier, model=memory_model, json_mode=true)
  -> parse strict JSON
  -> normalize layer names
  -> reject impossible records
  -> show proposal
```

Пример proposal:

```text
Memory proposal:
1. [work] requirement: CLI должен поддерживать выбор модели OpenRouter.
   reason: требование текущей задачи
2. [long] preference: Пользователь предпочитает Go для MVP.
   reason: стабильное техническое предпочтение
3. [ignore] smalltalk: Пользователь сказал "спасибо".
   reason: не влияет на будущие ответы
```

Это основной механизм Day 11. Так можно проверить не только итоговые файлы памяти, но и сам выбор: что LLM решила сохранить, куда и почему.

### 8.3. Применение memory proposal

```text
User confirms proposal
  -> CLI sends accepted records to MemoryManager
  -> short records append to sessions/<id>/short_term.jsonl
  -> work records append to tasks/<id>.json or task memory jsonl
  -> long records append to long_term/*.jsonl
  -> ignore records only stay in memory_proposals.jsonl audit
```

Статусы proposal records:

- `pending`: предложено LLM, ещё не применено;
- `accepted`: сохранено без правок;
- `edited`: пользователь изменил слой или текст перед сохранением;
- `rejected`: пользователь отклонил;
- `blocked`: invariant checker запретил сохранение.

### 8.4. Ручное сохранение

```text
/save work Требование: поддержать выбор модели
  -> CLI parses layer=work
  -> InvariantChecker checks redaction
  -> MemoryManager.save(work, content)
  -> WorkingMemoryStorage writes record
  -> CLI prints saved record id
```

Ручное сохранение нужно как escape hatch. Но для проверки Day 11 основной flow должен проходить через LLM proposal, иначе невозможно показать, как ассистент сам выбирает слой памяти.

### 8.5. Подключение памяти к prompt

Prompt включает не всю память, а выбранный bundle:

```text
System rules
Active user profile
Project/profile invariants
Current task state: stage, current_step, expected_action, status
Working memory records
Selected long-term records
Recent short-term messages
Current user message
```

Это защищает от анти-паттерна `всё в один prompt`.

## 9. Profile flow

### 9.1. Создание профиля

```text
assistant init
  -> no profiles found
  -> ask profile id
  -> ask language/style/detail
  -> ask preferred answer format
  -> ask constraints
  -> save profiles/<id>.json
  -> set active_profile_id in config.json
```

### 9.2. Использование профиля

```text
assistant chat --profile student
  -> ProfileManager loads student.json
  -> PromptBuilder renders profile block
  -> every LLM call receives profile block
```

Profile prompt block пример:

```text
Active user profile:
- language: ru
- detail: high
- tone: teacher
- format: step-by-step with examples
- constraints:
  - explain terms on first use
  - do not skip architectural reasoning
```

## 10. Task state machine

Состояния Day 13:

```text
planning -> execution -> validation -> done
          ^             |
          |             v
          └──────── execution
```

`paused` не является отдельным stage. Это `TaskStatus`, который может быть установлен поверх любого stage.

Минимальная модель состояния:

```text
stage: planning | execution | validation | done
current_step: короткое описание текущего шага
expected_action: user_input | llm_response | user_confirmation | none
status: active | paused
```

Allowed transitions:

```go
var AllowedTransitions = map[TaskStage][]TaskStage{
    StagePlanning:   {StageExecution},
    StageExecution:  {StageValidation, StagePlanning},
    StageValidation: {StageExecution, StageDone},
    StageDone:       {},
}
```

MVP commands:

```text
/task start <title>
/task status
/task move <stage>
/task step <text>
/task expect <action>
/task pause
/task resume
```

Commands `/task stage`, `/task plan`, `/task criteria`, `/task decision`, and `/task done` are P1/debug unless the canonical command matrix adopts them across all docs and tests.

Если пользователь просит запрещённый переход, manager возвращает ошибку и не меняет `current.json`.

Pause/resume:

```text
/task pause
  -> planning|execution|validation: status=paused
  -> done: terminal no-op, no reopened work
  -> stage/current_step/expected_action сохраняются без изменений
  -> next chat request не должен продолжать задачу как active execution

/task resume
  -> planning|execution|validation: status=active
  -> done: terminal status only, no execution resumes
  -> stage/current_step/expected_action восстанавливаются
  -> next prompt получает полный task context
```

Так выполняется критерий Day 13: можно остановиться на любом этапе и продолжить без повторного объяснения.

## 11. Prompt templates

### 11.1. Base system prompt

```text
You are a minimal CLI code assistant.
Follow active user profile, task state, memory layers, and invariants.
Do not claim memory was saved unless the application saved it.
Do not store secrets.
If user request conflicts with active constraints, explain the conflict.
```

### 11.2. Memory instruction

```text
Memory policy:
- short-term memory is current session context;
- working memory is current task context;
- long-term memory is stable user/project knowledge;
- memory classifier proposes records as short/work/long/ignore;
- never apply memory proposal without showing it to the user;
- never move facts between layers without explicit user confirmation.
```

### 11.3. Memory classifier prompt

```text
You are the memory classifier for a CLI assistant.
Your task is to extract durable facts from the latest exchange and decide where each fact belongs.

Memory layers:
- short: useful only for the current session/dialogue;
- work: useful for the current task, plan, requirements, decisions, validation;
- long: stable user preference, project rule, reusable knowledge, invariant;
- ignore: smalltalk, duplicate facts, low-value facts, secrets, temporary noise.

Rules:
- Return strict JSON only.
- Do not save secrets, API keys, tokens, passwords, private credentials.
- Prefer ignore when unsure.
- Keep content concise and standalone.
- Include reason and confidence.

Output schema:
{
  "records": [
    {
      "layer": "short|work|long|ignore",
      "kind": "preference|requirement|decision|constraint|context|smalltalk|other",
      "content": "...",
      "reason": "...",
      "confidence": 0.0
    }
  ]
}
```

### 11.4. Stage instruction

```text
Current task stage: {stage}
Current step: {current_step}
Expected action: {expected_action}
Task status: {status}
Allowed next stages: {allowed_next_stages}
Do work appropriate for this stage and expected action only.
If task status is paused, do not continue execution until /task resume.
```

## 12. Storage format

### 12.1. JSONL memory records

`long_term/decisions.jsonl`:

```json
{"id":"mem_001","layer":"long","kind":"decision","content":"Use OpenRouter as LLM provider for MVP.","source":"user","tags":["provider"],"created_at":"2026-06-17T09:00:00Z"}
```

JSONL удобен для append-only истории и простой отладки.

### 12.2. Memory proposal JSONL

`sessions/<session_id>/memory_proposals.jsonl`:

```json
{"id":"proposal_001","records":[{"layer":"work","kind":"requirement","content":"CLI должен поддерживать выбор модели OpenRouter.","reason":"Требование текущей задачи.","confidence":0.91,"status":"accepted"}],"created_at":"2026-06-17T10:00:00Z"}
```

Это audit trail для проверки Day 11: видно, какие данные LLM предложила, какой слой выбрала и что реально было применено.

### 12.3. Task JSON

`tasks/current.json`:

```json
{
  "id": "task-001",
  "title": "CLI assistant MVP",
  "stage": "planning",
  "current_step": "согласовать acceptance criteria Day 13",
  "expected_action": "user_confirmation",
  "status": "active",
  "objective": "Implement memory layers and personalization",
  "acceptance_criteria": [
    "three memory layers are stored separately",
    "active profile is attached to every prompt",
    "task state stores stage, current step, expected action"
  ],
  "plan": [],
  "decisions": [],
  "open_questions": [],
  "validation_status": null,
  "paused_at": null,
  "resumed_at": null
}
```

### 12.4. Profile JSON

`profiles/senior.json`:

```json
{
  "id": "senior",
  "display_name": "Senior engineer",
  "style": {
    "language": "ru",
    "detail": "low",
    "tone": "direct"
  },
  "response_format": {
    "prefer_steps": false,
    "prefer_examples": false,
    "prefer_tradeoffs": true
  },
  "constraints": [
    "Answer briefly",
    "Focus on risks and engineering decisions"
  ],
  "default_model": null
}
```

## 13. Error handling

Обязательные ошибки:

- нет OpenRouter API key;
- OpenRouter вернул 401/403;
- модель не найдена;
- network timeout;
- повреждён JSON storage;
- classifier вернул невалидный JSON;
- classifier предложил неизвестный layer;
- выбранный профиль отсутствует;
- нет активной задачи для `/save work`;
- нет активной задачи для `/task pause` или `/task resume`;
- запрещённый переход task state machine;
- попытка продолжить задачу со сломанным или неполным task state;
- попытка сохранить секрет в memory.

Поведение:

- показывать короткую понятную ошибку;
- не терять текущий ввод пользователя;
- не падать stack trace в обычном режиме;
- писать подробности в log только без секретов.

## 14. Security and privacy

Правила:

- не хранить API key в `docs/`, memory files, profiles, transcripts;
- редактировать строки вида `sk-...`, `OPENROUTER_API_KEY=...`, bearer tokens;
- repo-local `.assistant/` не коммитить и использовать только как explicit demo/test opt-in;
- transcript может содержать пользовательские данные, поэтому он local-only, redacted/minimized by default и raw only opt-in;
- перед provider calls запускать pre-provider scanner/redaction;
- перед сохранением long-term memory показывать пользователю, что именно будет записано.
- memory proposal хранить как audit trail, но секреты в proposal тоже должны быть заблокированы или отредактированы.

## 15. Testing strategy

Day 11 tests:

- `TestMemoryClassifierBuildsProposal`;
- `TestMemoryClassifierParsesStrictJSON`;
- `TestMemoryProposalSupportsIgnoreLayer`;
- `TestApplyProposalSavesShortMemory`;
- `TestApplyProposalSavesWorkingMemory`;
- `TestApplyProposalSavesLongTermMemory`;
- `TestLayersAreSeparateFiles`;
- `TestMemoryProposalAuditStoresAcceptedRejectedBlocked`;
- `TestPromptBuilderUsesSelectedLayers`.

Day 12 tests:

- `TestCreateProfile`;
- `TestSwitchProfile`;
- `TestProfileAttachedToPrompt`;
- `TestSameQueryDifferentProfilesChangePrompt`.

Day 13 tests:

- `TestTaskStateStoresStageStepExpectedAction`;
- `TestAllowedTransition`;
- `TestForbiddenTransitionFailsWithoutStateChange`;
- `TestPausePreservesStageStepExpectedAction`;
- `TestResumeRestoresStageStepExpectedAction`;
- `TestResumeKeepsWorkingMemoryAvailable`.

Additional P0 tests:

- `TestPromptBuilderMarksUntrustedBlocks`;
- `TestStorageRejectsUnsafePaths`;
- `TestAtomicWriteAndRecovery`;
- `TestProviderTimeoutAndTypedErrors`;
- `TestDuplicateProposalApplyIsIdempotent`;
- `TestGatekeeperBlocksPausedTask`;
- `TestPromptInjectionRedaction`.

Day 11/12/13 are mandatory acceptance criteria and may not be bypassed.

## 16. Definition of ready

Before implementation starts, docs must have:

- one canonical contract for state/commands/memory layers;
- explicit P0 vertical slice;
- env-only API key rule for MVP;
- storage/privacy/gatekeeper policies;
- deterministic test matrix for Day 11/12/13.

Security tests:

- `TestOpenRouterKeyNotSavedToMemory`;
- `TestSecretRedactionBeforeSave`;
- `TestSecretBlockedInMemoryProposal`.

## 16. Implementation order

1. Project skeleton, config loading, and storage-root/permission policy.
2. Safe storage primitives: path validation, symlink rejection, atomic JSON writes, locked JSONL append.
3. Provider interface and fake provider with recorded calls; no main chat call yet.
4. Profile manager and first-run provider disclosure.
5. Task state machine constants, transitions, current step, expected action, pause/resume persistence.
6. Memory storage layers and inspection commands.
7. Prompt builder with canonical untrusted block schema and profile attached to every request.
8. Pre-provider scanner/redaction gate for chat and classifier payloads.
9. Basic `assistant chat` loop through PromptBuilder and provider.
10. MemoryClassifier prompt with strict JSON parsing through the provider interface.
11. MemoryProposalStore audit file with minimized/redacted default retention.
12. `/memory propose` and proposal display.
13. `/memory apply` with accept/edit/reject/blocked statuses and idempotency.
14. Long-term memory from accepted `[long]` proposals.
15. Model selection UI and `/model` command.
16. Non-interactive/JSON smoke commands and exit-code contract.
17. Tests for Day 11, Day 12, Day 13 acceptance criteria.

## 17. Future extensions

После MVP можно добавить:

- repository context search;
- file read/edit tools with explicit approval;
- automatic memory suggestion with user confirmation;
- vector search over long-term memory;
- deterministic invariant checker for project constraints;
- summarization of long sessions;
- replayable task history;
- multi-provider support;
- non-interactive mode for scripts.

## 18. Главный архитектурный инвариант

Ассистент не является просто оболочкой вокруг OpenRouter. Его ценность в том, что приложение управляет состоянием: профиль, память, задача, стадии и ограничения существуют вне LLM и подаются в модель контролируемо.
