# Implementation Status And Regression Plan

Назначение: historical implementation status, regression checklist и обзор уже закрытого Day11-14 контракта.

Этот файл не является source of truth для продукта или архитектуры. При конфликте приоритет имеют `docs/prd.md`, `docs/frd.md`, `docs/architect.md`, acceptance tests и актуальные task notes/goal-файлы, если они есть. `day11.md`, `day12.md`, `day13.md`, `day14.md` остаются исходными учебными критериями приёмки.

Ключевой принцип: `day11.md`, `day12.md`, `day13.md`, `day14.md` являются жёсткими критериями приёмки. Нельзя заменить LLM-классификацию ручным `/save`, нельзя хранить память одним файлом, нельзя подключать профиль только по желанию пользователя, нельзя имитировать pause/resume без сохранённого состояния задачи, нельзя держать инварианты только в статическом prompt text без отдельного storage/enforcement layer.

Текущее состояние кода на 2026-06-18: базовый MVP, deterministic process-control loop и Day14 invariant layer реализованы. Этот документ больше не задаёт план работ с нуля; он фиксирует статус и помогает проверять регрессии при будущих изменениях.

## 1. Текущее Поведение Проекта

Репозиторий содержит минимальный stateful CLI code assistant на Go, который:

- работает через терминальный CLI;
- вызывает OpenRouter для основного ответа;
- позволяет выбрать модель;
- хранит runtime state локально в configured storage root: OS user-data directory по умолчанию, repo-local `.assistant/` только как demo/test opt-in;
- имеет три физически раздельных слоя памяти: `short`, `work`, `long`;
- после значимого ответа запускает отдельный LLM memory-classification step через OpenRouter;
- показывает пользователю memory proposal до сохранения;
- сохраняет только подтверждённые записи;
- имеет user profile со стилем, форматом и ограничениями;
- автоматически подключает active profile к каждому prompt;
- ведёт текущую задачу как конечный автомат с `stage`, `current_step`, `expected_action`, `status`;
- сообщает LLM текущий stage, роль этапа, allowed actions and forbidden actions перед task-scoped provider call;
- поддерживает pause/resume задачи на любом рабочем этапе без повторного объяснения контекста;
- имеет отдельный invariant layer, который хранится вне диалога, рендерится в prompt и deterministic блокирует конфликты;
- имеет deterministic tests и smoke/demo path, закрывающие Day 11/12/13/14.

## 2. Regression Acceptance Contract

### 2.1. Day 11: memory layers

Обязательные свойства:

- есть минимум три типа памяти: краткосрочная, рабочая, долговременная;
- физические storage layers только `short`, `work`, `long`;
- `ignore` существует только в `MemoryProposal` и audit trail, но не как storage layer;
- разные типы памяти лежат в разных файлах/каталогах;
- LLM явно предлагает, какие факты куда сохранить;
- пользователь видит proposal и подтверждает/редактирует/отклоняет записи;
- можно выполнить `/memory short`, `/memory work`, `/memory long` и увидеть содержимое каждого слоя;
- сохранённая память влияет на следующий prompt и ответ ассистента;
- manual `/save` допустим только как escape hatch и не закрывает Day 11 сам по себе.

### 2.2. Day 12: personalization

Обязательные свойства:

- есть профиль пользователя;
- профиль содержит `style`, `response_format`, `constraints`;
- профиль хранится отдельно от short-term history и memory records;
- active profile подключается к каждому LLM prompt автоматически;
- пользователь не копирует профиль вручную в запрос;
- одинаковый запрос с профилями `student` и `senior` даёт разный rendered prompt и разное поведение ответа;
- `/profile` и `/profile <id>` позволяют проверить и переключить active profile.

### 2.3. Day 13: task finite state machine

Обязательные свойства:

- task state хранит `stage`;
- task state хранит `current_step`;
- task state хранит `expected_action`;
- переходы между stage проверяются кодом как конечный автомат;
- forbidden transition возвращает ошибку и не меняет `current.json`;
- pause возможен на рабочих этапах `planning`, `execution`, `validation`;
- resume после restart CLI восстанавливает `stage`, `current_step`, `expected_action`, plan и working memory;
- ассистент продолжает задачу без повторного объяснения пользователем.

### 2.4. Day 14: invariants

Обязательные свойства:

- инварианты хранятся отдельно от диалога в `<storage_root>/invariants/project.jsonl`;
- active invariants попадают в prompt как trusted system policy с marker `Invariant policy` и `id="invariants.active"`;
- user input, конфликтующий с invariant, блокируется до provider call;
- provider output, конфликтующий с invariant, отклоняется как hard gate без correction retry до accepted persistence и до memory classifier;
- отказ содержит `invariant_conflict`, ID инварианта, evidence и structured JSON violations;
- P0 matcher is normalized literal forbidden-term matching; semantic invariant matching and `RequiredTerms` are future work unless explicitly implemented;
- custom invariants are bounded privileged local policy data; content may be provider-visible and is rendered with source/provenance labels;
- tests используют fake provider/deterministic checks, не live OpenRouter.

## 3. Канонический Runtime Contract

### 3.1. Task state

Текущий контракт:

```text
stage: planning | execution | validation | done
status: active | paused
expected_action: user_input | llm_response | user_confirmation | none
terminal completion: stage=done + expected_action=none
```

Правила:

- `status=done` не реализовывать;
- `done` является terminal stage, а не статусом;
- `tool_result` не входит в P0, потому что tools не входят в MVP;
- если позже нужен `tool_result`, это P1 вместе с явным lifecycle: кто ставит, кто ждёт, кто снимает;
- `paused` не является stage, это status поверх текущего stage.

### 3.2. Allowed transitions

```text
planning -> execution
execution -> validation
execution -> planning
validation -> execution
validation -> done
done -> <none>
```

Правила:

- `execution -> planning` разрешён для возврата при новых требованиях;
- прямой `planning -> validation` запрещён;
- прямой `planning -> done` запрещён;
- прямой `done -> execution` запрещён;
- переходы при `status=paused` запрещены до `/task resume`, кроме локальных inspection commands;
- completion происходит через allowed transition `validation -> done` и устанавливает `stage=done`, `expected_action=none`, `status=active`;
- `/task done` не входит в P0, если canonical command matrix не добавит его во все docs/tests.

### 3.3. Memory layers

```text
short: current session/dialogue
work: current task data
long: stable profile/project preferences, decisions, knowledge, constraints
ignore: proposal/audit only, never physical memory layer
```

Правила:

- storage paths для слоёв разные;
- `LayerIgnore` в `MemoryRecord` не создавать;
- proposal record может иметь `layer=ignore`, а applied memory record нет;
- long-term write требует явного подтверждения;
- secret-like content блокируется во всех слоях.

### 3.4. Prompt priority

Порядок prompt blocks:

1. Base system rules.
2. Security and memory policy.
3. Trusted process-control policy: app owns state, transitions, memory writes and validation.
4. Trusted stage-specific policy: role, allowed actions, forbidden actions and output schema.
5. Active profile.
6. Invariants: active policy from `<storage_root>/invariants/project.jsonl`, semantic priority below base/security/process/stage and above profile/memory/task/user text.
7. Task state: `stage`, `current_step`, `expected_action`, `status`, allowed transitions.
8. Working memory.
9. Selected long-term memory.
10. Short-term history.
11. Current user query.

Правила:

- profile/memory/task/short_history/classifier output являются untrusted data;
- они должны рендериться как quoted/tagged data, а не как инструкции;
- canonical prompt block содержит `id`, `type`, `source`, `trust`, escaped content и стабильный ordering;
- system/application/security policy всегда выше сохранённого контекста;
- stage policy выше profile/memory/task/short_history data, но не удаляет profile block;
- LLM должна знать текущий этап: planner в `planning`, implementer в `execution`, reviewer/QA в `validation`, terminal summarizer в `done`;
- LLM может предложить process signal, но task transition применяет только приложение;
- PromptBuilder не пишет файлы и не вызывает provider.

## 4. Текущая Структурная Точка

Репозиторий уже содержит working Go implementation:

- `go.mod` с module `github.com/nikbrik/coding_writer` и dependency `github.com/spf13/cobra`;
- entrypoint `cmd/assistant/main.go`;
- packages `internal/app`, `internal/cli`, `internal/providers`, `internal/memory`, `internal/profiles`, `internal/tasks`, `internal/invariants`, `internal/prompting`, `internal/process`, `internal/storage`, `internal/validation`;
- acceptance tests in `tests/day_acceptance_test.go` and `tests/process_acceptance_test.go`;
- `.assistant/` используется как repo-local demo/test storage opt-in and must stay gitignored.

Следовательно, дальнейшая работа начинается не с bootstrap, а с поддержки и расширения существующего CLI assistant.

## 5. Целевой стек

Основной стек:

- Go 1.22+;
- `cobra` для CLI commands;
- `net/http` для OpenRouter;
- `encoding/json` для JSON/JSONL;
- стандартные `context`, `errors`, `os`, `path/filepath`, `time`, `bufio`, `strings`;
- без Bubble Tea в P0, чтобы не усложнять интерактивный loop;
- fake provider в тестах вместо live OpenRouter.

Обоснование:

- docs рекомендуют Go для MVP;
- CLI должен быть простым binary;
- memory/profile/task contracts хорошо ложатся на typed structs;
- файловое JSON/JSONL storage прозрачно для учебной проверки.

## 6. Текущая структура файлов

```text
go.mod
go.sum

cmd/assistant/
  main.go

internal/app/
  config.go
  errors.go
  id.go
  models.go

internal/cli/
  root.go

internal/providers/
  provider.go
  openrouter.go
  fake.go

internal/storage/
  safe_path.go
  json.go
  jsonl.go
  errors.go
  file_lock_unix.go
  file_lock_unsupported.go

internal/memory/
  manager.go
  classifier.go
  proposal_store.go
  paths.go

internal/profiles/
  manager.go
  render.go
  defaults.go

internal/tasks/
  manager.go
  state_machine.go
  render.go

internal/prompting/
  builder.go

internal/validation/
  escape.go
  secrets.go

internal/process/
  action_kind.go
  action_router.go
  audit_event.go
  audit_store.go
  controller.go
  execution_validator.go
  planning_validator.go
  validation_validator.go
  done_validator.go
  permission.go
  prompt_build_input.go
  response_parser.go
  retry_controller.go
  result.go
  stage_policy.go
  stage_policy_registry.go
  stage_prompt_factory.go
  transition_gate.go
  validator_runner.go

tests/
  day_acceptance_test.go
  process_acceptance_test.go
```

Примечание по тестам:

- unit tests можно размещать рядом с packages;
- `tests/*` содержит end-to-end/acceptance-style tests for Day 11/12/13 and process control;
- fake provider должен позволять проверить behavior без live API key.

## 7. Runtime storage layout

Default storage root для normal mode: OS user-data directory с `0700` directories и `0600` sensitive files. Repo-local `.assistant/` разрешён только как explicit demo/test opt-in: `--storage-dir .assistant` или `ASSISTANT_STORAGE_DIR=.assistant`.

```text
<storage_root>/
  config.json
  profiles/
    student.json
    senior.json
  sessions/
    <session_id>/
      short_term.jsonl
      memory_proposals.jsonl
      prompts.jsonl       # only when prompt audit env is enabled
      .last_activity
  tasks/
    current.json
    <task_id>.json
    <task_id>/
      work_memory.jsonl
  long_term/
    preferences.jsonl
    decisions.jsonl
    knowledge.jsonl
    constraints.jsonl
  invariants/
    project.jsonl
  logs/
```

Current note: P0 does not write `transcript.md`; `logs/` exists for extension, but no `app.log` writer is implemented.

Storage rules:

- добавить `.assistant/` в `.gitignore` в первом implementation PR для demo/test opt-in;
- все writes через atomic temp file + fsync + rename для JSON overwrite;
- JSONL append сериализовать через per-file lock;
- proposal apply делать idempotent под тем же lock;
- path traversal, absolute paths, encoded separators и unsafe IDs отклонять;
- symlinked parents, symlinked files и symlink writes отклонять;
- optimistic version/mtime check защищает `current.json` от lost updates;
- broken JSON возвращает typed storage error с путём файла;
- API key не писать ни в один файл;
- pre-provider scanner блокирует secret-like content до chat/classifier calls.

## 8. Основные data models

### 8.1. ChatMessage

```go
type ChatRole string

const (
    RoleSystem    ChatRole = "system"
    RoleUser      ChatRole = "user"
    RoleAssistant ChatRole = "assistant"
)

type ChatMessage struct {
    ID        string    `json:"id,omitempty"`
    Role      ChatRole  `json:"role"`
    Content   string    `json:"content"`
    CreatedAt time.Time `json:"created_at"`
}
```

### 8.2. MemoryRecord

```go
type MemoryLayer string

const (
    LayerShort MemoryLayer = "short"
    LayerWork  MemoryLayer = "work"
    LayerLong  MemoryLayer = "long"
)

type MemoryRecord struct {
    ID         string      `json:"id"`
    Layer      MemoryLayer `json:"layer"`
    Kind       string      `json:"kind"`
    Content    string      `json:"content"`
    Source     string      `json:"source"`
    Tags       []string    `json:"tags,omitempty"`
    TaskID     string      `json:"task_id,omitempty"`
    SessionID  string      `json:"session_id,omitempty"`
    ProposalID string      `json:"proposal_id,omitempty"`
    CreatedAt  time.Time   `json:"created_at"`
}
```

### 8.3. MemoryProposal

```go
type ProposedMemoryLayer string

const (
    ProposedLayerShort  ProposedMemoryLayer = "short"
    ProposedLayerWork   ProposedMemoryLayer = "work"
    ProposedLayerLong   ProposedMemoryLayer = "long"
    ProposedLayerIgnore ProposedMemoryLayer = "ignore"
)

type ProposalRecordStatus string

const (
    ProposalPending  ProposalRecordStatus = "pending"
    ProposalAccepted ProposalRecordStatus = "accepted"
    ProposalEdited   ProposalRecordStatus = "edited"
    ProposalRejected ProposalRecordStatus = "rejected"
    ProposalBlocked  ProposalRecordStatus = "blocked"
)

type MemoryProposal struct {
    ID               string                 `json:"id"`
    SourceMessageIDs []string               `json:"source_message_ids"`
    Records          []ProposedMemoryRecord `json:"records"`
    Provider         string                 `json:"provider,omitempty"`
    Model            string                 `json:"model,omitempty"`
    TemplateHash     string                 `json:"template_hash,omitempty"`
    CreatedAt        time.Time              `json:"created_at"`
}

type ProposedMemoryRecord struct {
    ID         string              `json:"id"`
    Layer      ProposedMemoryLayer `json:"layer"`
    Kind       string              `json:"kind"`
    Content    string              `json:"content"`
    Reason     string              `json:"reason"`
    Confidence float64             `json:"confidence"`
    Status     ProposalRecordStatus `json:"status"`
    BlockReason string              `json:"block_reason,omitempty"`
}
```

### 8.4. UserProfile

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

### 8.5. TaskState

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

### 8.6. AppConfig

```go
type AppConfig struct {
    ActiveProfileID   string `json:"active_profile_id,omitempty"`
    ActiveModel       string `json:"active_model,omitempty"`
    StorageDir        string `json:"storage_dir"`
    OpenRouterBaseURL string `json:"openrouter_base_url"`
    MemoryModel       string `json:"memory_model,omitempty"`
}
```

Config precedence:

```text
CLI flags > env vars > config file > defaults
```

API key rule:

- `OPENROUTER_API_KEY` env-only for MVP;
- no key in config/profile/memory/provider payload records/audit/logs; raw transcripts are not written in P0.

Field-level config contract:

| Setting | CLI flag | Env var | Config key | Default | Persisted | P0 |
| --- | --- | --- | --- | --- | --- | --- |
| API key | none | `OPENROUTER_API_KEY` | none | none | never | yes |
| Storage root | `--storage-dir` | `ASSISTANT_STORAGE_DIR` | `storage_dir` | OS user-data dir | yes | yes |
| Active model | `--model` | `ASSISTANT_MODEL` | `active_model` | user selected | yes | yes |
| Memory model | `--memory-model` | `ASSISTANT_MEMORY_MODEL` | `memory_model` | active model | yes | yes |
| Active profile | `--profile` | `ASSISTANT_PROFILE` | `active_profile_id` | first profile | yes | yes |
| OpenRouter base URL | `--openrouter-base-url` | `ASSISTANT_OPENROUTER_BASE_URL` | `openrouter_base_url` | OpenRouter HTTPS endpoint | yes, explicit opt-in | no |
| JSON output | `--json` | none | none | false | never | yes |
| Non-interactive chat | `chat --once` | none | none | false | never | yes |

Exit codes:

| Code | Category |
| --- | --- |
| `0` | success |
| `1` | unexpected internal error |
| `2` | CLI usage / missing args |
| `3` | validation or invariant failure |
| `4` | storage / IO / corruption / lock timeout |
| `5` | provider / auth / model / network / missing API key |
| `6` | classifier parse/schema/failure |

## 9. CLI command contract

Current command surface is implemented in `internal/cli/root.go`.

### 9.1. Top-level commands

```text
assistant init
assistant chat
assistant chat --once --input <text> --json
assistant chat --once --render-prompt --input <text> --json
assistant chat --profile <profile_id> --model <model_id>
assistant profiles [list]
assistant profiles show [id]
assistant profiles set <id>
assistant profiles create <id> [--display-name <name>] [--style k=v] [--format k=v] [--constraint <text>]
assistant memory list <short|work|long> --json
assistant memory propose --latest --json
assistant memory apply --proposal <id> --accept all --json
assistant memory proposals [--session <id>] --json
assistant invariants list --json
assistant invariants add <id> --kind <kind> --content <text> [--severity block] [--forbid <term>...] --json
assistant task start <title> --json
assistant task status --json
assistant task move <stage> --json
assistant task step <text> --json
assistant task expect <action> --json
assistant task pause --json
assistant task resume --json
assistant process audit [--latest|--limit <n>] --json
assistant privacy
assistant privacy purge --audit [--transcripts] --yes
```

P0 must be scriptable:

- commands should support flags for tests where possible;
- output should be deterministic enough for smoke tests;
- stdout is primary data;
- stderr is diagnostics/errors;
- `--json` is required for acceptance smoke commands.

### 9.2. Slash commands in chat

```text
/help
/model
/profile
/profile create
/task start <title>
/task status
/task step <text>
/task expect <action>
/task move <stage>
/task plan <text>
/task criteria <text>
/task pause
/task resume
/save short <text>
/save work <text>
/save long <text>
/memory propose
/memory apply
/memory short
/memory work
/memory long
/invariants
/invariants add <id> --kind <kind> --content <text> --forbid <term>
/process audit
/privacy
/clear short
/exit
```

Current boundary: slash `/task plan` and `/task criteria` are implemented. `/task decision`, `/task done`, `/task stage`, and top-level `assistant task plan|criteria|decision|done|stage` are not implemented.

Memory apply scriptable syntax:

```text
assistant memory apply --proposal <id> --accept all --json
assistant memory apply --proposal <id> --reject <record_id> --json
assistant memory apply --proposal <id> --edit <record_id>:layer=<layer>,content=<text> --json
```

Parsing rules:

- slash command не отправляется в main LLM prompt;
- invalid command показывает короткую подсказку;
- local inspection commands не вызывают OpenRouter;
- commands mutate storage только через соответствующий manager.

## 10. Historical Phase Checklist

Status on 2026-06-18:

| Area | Current status |
| --- | --- |
| Bootstrap, Cobra CLI, `go.mod`, `.assistant/` hygiene | Implemented |
| Config/storage permissions/safe paths/atomic JSON/locked JSONL | Implemented |
| OpenRouter provider, fake provider, model validation, retries/timeouts | Implemented |
| Default profiles, profile CRUD, active profile in prompt | Implemented |
| Task FSM, allowed transitions, pause/resume, top-level task commands | Implemented |
| Memory layers, classifier, proposal store, apply accept/reject/edit, audit | Implemented |
| Prompt builder, untrusted context blocks, rendered prompt inspection | Implemented |
| ProcessController, stage policies, validators, retry, TransitionGate, audit | Implemented |
| Acceptance tests for Day 11/12/13 and process control | Implemented |
| Day 14 invariant storage, prompt rendering, input/output conflict refusal, CLI inspection | Implemented |

The detailed phase list below is a historical regression checklist. Items phrased as "implement" / "реализовать" describe completed historical work unless a current gap is explicitly noted.

### Фаза 0. Contract freeze и bootstrap hygiene

Статус: completed.

Цель: зафиксировать неизменяемые semantics и не начать с конфликтующего контракта.

Фактически выполнено:

- зафиксировать в implementation constants canonical values из раздела 3;
- не добавлять `status=done`;
- не добавлять physical `ignore` layer;
- решить, что OS user-data directory является normal default storage;
- оставить `.assistant/` только как demo/test opt-in и держать его в `.gitignore`;
- создать `go.mod` с module `github.com/nikbrik/coding_writer`;
- добавить `cmd/assistant/main.go`;
- добавлять package directories только с реальными файлами;
- настроить `go test ./...` как базовую проверку.

Done criteria:

- `go test ./...` проходит на текущем implementation;
- `.assistant/` игнорируется git для demo/test opt-in;
- `assistant --help` запускается.

### Фаза 1. AppConfig и safe file storage

Цель: сделать надёжную основу для всех последующих stateful features.

Действия:

- реализовать `internal/app/config.go`;
- реализовать default config values;
- реализовать config load/save из `<storage_root>/config.json`;
- реализовать field-level flag/env/config/default precedence из раздела 8.6;
- реализовать `internal/storage/paths.go` для canonical path resolution;
- реализовать safe ID validation для session/task/profile IDs;
- реализовать atomic JSON write через temp file + fsync + rename;
- реализовать locked JSONL append;
- реализовать symlink parent/file rejection и permission setup;
- реализовать broken JSON typed errors;
- создать normal storage root при `assistant init`;
- создать subdirectories `profiles`, `sessions`, `tasks`, `long_term`, `logs`.

Tests:

- `TestInitCreatesStorageRoot`;
- `TestConfigDoesNotStoreAPIKey`;
- `TestStorageRejectsUnsafePaths`;
- `TestStorageRejectsSymlinkParentsAndTargets`;
- `TestStorageUsesRestrictivePermissions`;
- `TestAtomicWriteAndRecovery`;
- `TestLockedJSONLAppendAndConcurrentProposalApply`;
- `TestBrokenJSONReturnsTypedError`.

Done criteria:

- `assistant init --model <id> --profile <id>` может создать базовый config без LLM call;
- storage root можно переопределить в tests;
- API key никогда не появляется в config.

### Фаза 2. OpenRouter provider и fake provider

Цель: отделить LLM calls от CLI и дать тестам deterministic provider.

Действия:

- определить `LLMProvider` interface:

```go
type LLMProvider interface {
    ListModels(ctx context.Context) ([]string, error)
    Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}

type CompletionRequest struct {
    Purpose  CompletionPurpose
    Model    string
    Messages []ChatMessage
    JSONMode bool
}

type CompletionResponse struct {
    Message    ChatMessage
    ProviderID string
    Model      string
    RetryCount int
}
```

- реализовать `OpenRouterProvider` через `net/http`;
- читать API key только из `OPENROUTER_API_KEY`;
- встроить pre-provider secret scan/redaction перед HTTP request;
- добавить request timeout;
- нормализовать provider errors: missing key, auth, model not found, timeout, malformed response;
- не логировать raw Authorization header;
- реализовать `FakeProvider` для tests и smoke fixtures, который записывает calls и purpose `chat|classifier`;
- сделать model selection ручным через config до полноценного list UI;
- добавить `/model` local command, который меняет config после validation.

Tests:

- `TestProviderMissingAPIKeyDoesNotCallHTTP`;
- `TestPreProviderScannerBlocksRawSecretsForChatAndClassifier`;
- `TestProviderAuthErrorTyped`;
- `TestProviderTimeoutAndTypedErrors`;
- `TestModelCommandChangesActiveModel`;
- `TestInvalidModelDoesNotMutateActiveModel`.

Done criteria:

- основной LLM call работает с env key;
- tests не требуют live key;
- active model сохраняется в config;
- invalid model не портит config.

### Фаза 3. ProfileManager и Day 12 foundation

Цель: создать персонализацию как отдельный слой данных.

Действия:

- реализовать `ProfileManager.Create`;
- реализовать `ProfileManager.Get`;
- реализовать `ProfileManager.List`;
- реализовать `ProfileManager.SetActive` через config update;
- реализовать validation профиля: required fields, safe id, non-empty style/response_format/constraints;
- реализовать deterministic render profile block для prompt;
- добавить default fixtures `student` и `senior` для smoke/demo;
- реализовать `/profile`, `/profile <id>`, `/profile create`;
- top-level `assistant profiles` должен показывать profiles без LLM call;
- profile не должен записываться в short-term memory.

Profile examples:

```text
student:
  language=ru
  detail=high
  tone=teacher
  prefer_steps=true
  prefer_examples=true
  constraints=["explain terms", "show reasoning"]

senior:
  language=ru
  detail=low
  tone=direct
  prefer_steps=false
  prefer_tradeoffs=true
  constraints=["be concise", "focus risks and decisions"]
```

Tests:

- `TestCreateProfile`;
- `TestSwitchProfile`;
- `TestUnknownProfileDoesNotMutateActiveProfile`;
- `TestProfileRenderIsDeterministic`;
- `TestProfileNotMixedWithShortTermMemory`.

Done criteria:

- `/profile create` создаёт `<storage_root>/profiles/<id>.json`;
- active profile id сохраняется в config;
- профиль можно отрендерить для prompt;
- profile switch не меняет memory records.

### Фаза 4. TaskStateManager и Day 13 foundation

Цель: реализовать конечный автомат задачи до integration with chat.

Действия:

- реализовать `TaskStateManager.Start(title)`;
- создать `<storage_root>/tasks/current.json` и `<storage_root>/tasks/<task_id>.json`;
- initial state: `stage=planning`, `status=active`, `expected_action=user_input`, `current_step=""`;
- реализовать `Move(nextStage)` с `AllowedTransitions`;
- реализовать `SetStep(text)`;
- реализовать `SetExpectedAction(action)`;
- `AddPlanItem` и `AddCriteria` реализованы для REPL slash commands; `AddDecision` не exposed in current command surface;
- реализовать `Pause()`;
- реализовать `Resume()`;
- реализовать `Current()`;
- реализовать task prompt render block;
- реализовать P0: `/task start`, `/task status`, `/task move`, `/task step`, `/task expect`, `/task pause`, `/task resume`;
- slash `/task plan` and `/task criteria` are implemented; `/task decision`, `/task done`, `/task stage` stay out of current command surface;
- forbid state mutation when current task is paused except `/task resume` and local metadata inspection;
- preserve state across process restart.

Tests:

- `TestTaskStartCreatesCurrentTask`;
- `TestTaskStateStoresStageStepExpectedAction`;
- `TestAllowedTransition`;
- `TestForbiddenTransitionFailsWithoutStateChange`;
- `TestSetCurrentStepPersistsAfterRestart`;
- `TestSetExpectedActionPersistsAfterRestart`;
- `TestPausePreservesStageStepExpectedAction`;
- `TestPauseWorksFromPlanningExecutionValidation`;
- `TestPauseDoneIsTerminalNoOp`;
- `TestResumeRestoresStageStepExpectedAction`;
- `TestDoneUsesStageDoneExpectedNoneNoStatusDone`.

Done criteria:

- `/task status` работает без LLM;
- forbidden transition не меняет `current.json`;
- pause/resume работает после restart;
- task render block содержит allowed transitions.

### Фаза 5. MemoryManager storage для Day 11

Цель: реализовать физически раздельные memory layers до classifier.

Действия:

- реализовать `MemoryManager.Save(ctx, layer, record)`;
- реализовать `MemoryManager.List(ctx, layer)`;
- реализовать `MemoryManager.ClearShort(sessionID)`;
- реализовать `MemoryManager.SelectForPrompt(profileID, taskID, sessionID)`;
- short layer писать в `<storage_root>/sessions/<session_id>/short_term.jsonl`;
- work layer писать в `<storage_root>/tasks/<task_id>/work_memory.jsonl` через `WorkMemoryStore`;
- long layer писать в `<storage_root>/long_term/<kind>.jsonl` или routing по kind;
- запретить save `ignore` на type level;
- реализовать `/save short|work|long <text>` как escape hatch;
- реализовать `/memory short|work|long`;
- реализовать `/clear short`;
- ensure work save требует active task;
- ensure long save проходит confirmation/invariant path, даже manual command должен явно указывать layer.

Tests:

- `TestApplyManualSaveSavesShortMemory`;
- `TestApplyManualSaveSavesWorkingMemory`;
- `TestApplyManualSaveSavesLongTermMemory`;
- `TestLayersAreSeparateFiles`;
- `TestIgnoreCannotBeSavedAsMemoryRecord`;
- `TestClearShortDoesNotTouchWorkAndLong`;
- `TestWorkMemoryRequiresActiveTask`.

Done criteria:

- каждый слой можно сохранить и прочитать отдельно;
- files physically separate;
- `/memory <layer>` не смешивает records;
- manual path не считается полной Day 11 приёмкой без classifier.

### Фаза 6. PromptBuilder

Цель: собрать controlled prompt из профиля, задачи и памяти.

Действия:

- реализовать `PromptBuilder.Build(input) []ChatMessage`;
- вход: base rules, trusted process policy, trusted stage policy, active profile, task state, memory bundle, short-term messages, current query;
- рендерить каждый untrusted block в canonical tagged format: `id`, `type`, `source`, `trust`, escaped content;
- рендерить stage-specific role до untrusted context blocks;
- включать profile block всегда;
- включать task state всегда, если current task exists;
- при `status=paused` добавлять warning: task paused, do not continue execution until `/task resume`;
- для `planning` добавлять planner role and no-implementation rule;
- для `execution` добавлять implementer role and approved-plan-only rule;
- для `validation` добавлять strict reviewer/QA role and no-fixes/no-new-features rule;
- для `done` добавлять terminal summarizer role and no-mutation rule;
- working memory ставить раньше long/short;
- short-term history ограничивать окном;
- long-term memory выбирать по простой MVP-логике: latest N per kind/tags, не весь архив;
- добавить debug/render command или flag для tests: rendered prompt должен быть доступен без live call.

Tests:

- `TestPromptBuilderOrder`;
- `TestProfileAttachedToPrompt`;
- `TestTaskStateBeforeWorkingMemory`;
- `TestWorkingMemoryBeforeShortTermHistory`;
- `TestShortTermHistoryIsWindowed`;
- `TestPausedTaskWarningInPrompt`;
- `TestPlanningPromptHasPlannerRole`;
- `TestExecutionPromptHasImplementerRole`;
- `TestValidationPromptHasReviewerRole`;
- `TestDonePromptHasTerminalRole`;
- `TestPromptBuilderMarksUntrustedBlocks`;
- `TestPromptBuilderGoldenInjectionFixtures`;
- `TestSameQueryDifferentProfilesChangePrompt`;
- `TestPromptBuilderDoesNotWriteFilesOrCallProvider`.

Done criteria:

- rendered prompt можно inspect в tests;
- active profile есть в каждом prompt;
- task state и memory влияют через prompt, а не скрытую global state;
- LLM получает stage awareness and role before user query.

### Фаза 7. Basic chat loop

Цель: связать CLI, PromptBuilder и provider без memory classifier apply, но только после profile/task/memory/prompt/privacy gates.

Действия:

- реализовать `assistant chat` REPL;
- обычный текст отправлять через ProcessController gate -> PromptBuilder -> provider;
- перед provider call запускать pre-provider scanner/redaction;
- до provider call блокировать paused/done/forbidden stage actions;
- после provider response валидировать candidate output before accepted short-memory persistence;
- slash commands направлять в command router;
- `/exit` завершает loop;
- после provider response печатать assistant answer;
- писать user/assistant messages в short-term history текущей session;
- поддержать `assistant chat --profile <id> --model <id>`;
- поддержать `assistant chat --once --input <text> --json` для smoke tests;
- session id создавать при старте chat;
- raw transcript is not written in current P0; prompt audit is optional and must stay local-only and secret-redacted/minimized by default.

Tests:

- `TestChatSendsNormalInputToProvider`;
- `TestSlashCommandNotSentToProvider`;
- `TestChatAppendsUserAssistantToShortTermHistory`;
- `TestChatUsesActiveProfileAndModel`;
- `TestPausedTaskDoesNotContinueExecutionByDefault`;
- `TestPausedTaskDoesNotCallProviderForTaskContinuation`;
- `TestWrongStageOutputNotSavedAsAcceptedShortMemory`.

Done criteria:

- минимальный chat отвечает через fake provider in tests;
- live mode готов к OpenRouter key;
- short-term history появляется после ответа.

### Фаза 8. MemoryClassifier и strict JSON parsing

Цель: реализовать обязательный Day 11 LLM-классификационный шаг.

Действия:

- реализовать classifier prompt template из docs;
- classifier input: latest user message, latest assistant response, active profile, current task state, memory layer rules, existing similar records optional;
- использовать OpenRouter/fake provider через тот же provider interface с `purpose=classifier` и `JSONMode=true`;
- перед provider call запускать pre-provider scanner/redaction;
- model: `config.MemoryModel` если задан, иначе active model;
- response должен быть strict JSON;
- parse errors должны давать typed error и bounded retry для invalid JSON;
- normalize layer names;
- reject unknown layers;
- clamp/validate confidence;
- add `status=pending` для каждого proposal record;
- run secret checker до сохранения proposal audit;
- blocked secrets должны попасть в audit как `blocked` без raw secret;
- `ignore` records остаются в proposal, не применяются в memory layer;
- disabled classifier/manual fallback не закрывает Day 11 acceptance и не создаёт memory records.

Tests:

- `TestMemoryClassifierBuildsProposal`;
- `TestMemoryClassifierParsesStrictJSON`;
- `TestMemoryClassifierRejectsUnknownLayer`;
- `TestMemoryProposalSupportsIgnoreLayer`;
- `TestInvalidClassifierJSONDoesNotCreateRecords`;
- `TestSecretBlockedInMemoryProposal`;
- `TestClassifierDisabledModeIsNotDay11Acceptance`;
- `TestClassifierPayloadPreProviderRedaction`;
- `TestClassifierUsesProfileAndTaskState`.

Done criteria:

- `/memory propose` запускает classifier на latest exchange;
- proposal содержит `short|work|long|ignore`;
- invalid JSON не создаёт memory records;
- proposal показывается пользователю до сохранения.

### Фаза 9. MemoryProposalStore и apply flow

Цель: сделать visible, auditable, confirmable memory save.

Действия:

- реализовать `<storage_root>/sessions/<session_id>/memory_proposals.jsonl`;
- сохранить каждый proposal до apply;
- реализовать proposal id и record ids;
- реализовать `/memory apply`;
- apply должен поддерживать accept all, reject all, edit layer/content для scriptable P0;
- proposal apply должен быть idempotent по proposal id + record id;
- accepted `short` -> short-term storage;
- accepted `work` -> current task work storage;
- accepted `long` -> long-term storage;
- accepted/edited `ignore` не сохранять в memory layer;
- rejected не сохранять;
- blocked не сохранять;
- status каждого record обновлять в audit;
- layer mismatch между proposal и saved record должен быть невозможен без explicit edit status;
- audit по умолчанию хранит minimized/redacted content; raw prompt/proposal retention только opt-in;
- добавить purge/retention behavior для audit/prompt audit files.

Tests:

- `TestMemoryProposalAuditStoresPendingProposal`;
- `TestApplyProposalSavesShortMemory`;
- `TestApplyProposalSavesWorkingMemory`;
- `TestApplyProposalSavesLongTermMemory`;
- `TestRejectedProposalDoesNotSaveRecord`;
- `TestEditedProposalSavesUpdatedContentAndLayer`;
- `TestIgnoreOnlyStaysInAuditTrail`;
- `TestDuplicateProposalApplyIsIdempotent`;
- `TestMemoryProposalAuditStoresAcceptedRejectedBlocked`.

Done criteria:

- Day 11 explicit choice реализован: LLM proposes, user confirms, app saves;
- audit показывает proposed vs actual;
- layers physically separate after apply.

### Фаза 10. InvariantChecker и redaction

Цель: защитить storage/prompt от очевидных нарушений.

Действия:

- реализовать secret patterns: `OPENROUTER_API_KEY=...`, `Bearer ...`, `sk-...`, common token/password patterns;
- check manual save;
- check proposal records;
- check long-term write;
- redaction before persistence;
- pre-provider redaction/blocking before chat/classifier payloads;
- block raw secret in audit content;
- add canonical prompt-injection tagging/escaping for untrusted blocks;
- add first-run provider disclosure and `assistant privacy` summary;
- default audit/prompt-audit retention to minimized/redacted data with raw opt-in and purge behavior;
- detect profile/user conflict minimally;
- ensure paused task gate before execution-like continuation;
- ensure no silent long-term write.

Tests:

- `TestOpenRouterKeyNotSavedToMemory`;
- `TestSecretRedactionBeforeSave`;
- `TestSecretBlockedInMemoryProposal`;
- `TestManualSaveWithSecretBlocked`;
- `TestRawSecretNeverReachesFakeProvider`;
- `TestRejectedIgnoredRecordsDoNotPersistSensitiveRawTextByDefault`;
- `TestGatekeeperBlocksPausedTask`;
- `TestPromptInjectionRedaction`;
- `TestLongTermCannotBeSavedWithoutExplicitAction`.

Done criteria:

- secrets do not appear in storage root, provider payload records, audit, logs, prompt audit, or memory files;
- blocked proposal records visible as blocked without raw secret;
- paused task не продолжает execution without resume.

### Фаза 11. Day 12 end-to-end personalization demo

Цель: доказать, что профиль реально влияет на prompt и response behavior.

Действия:

- создать deterministic default profiles `student` и `senior` через command/fixtures;
- добавить prompt render debug mode для проверки profile block;
- fake provider должен возвращать response variation based on profile block;
- live demo path должен задавать одинаковый запрос под двумя профилями;
- assert profile switch не меняет memory records;
- assert next prompt after switch содержит новый profile block.

Tests:

- `TestDay12StudentSeniorPromptsDiffer`;
- `TestDay12SameQueryDifferentProfilesChangeFakeProviderResponse`;
- `TestProfileAttachedToEveryLLMCall`;
- `TestProfileSwitchDoesNotMutateMemory`.

Done criteria:

- Day 12 можно проверить без manual prompt copy;
- профиль учитывается автоматически;
- tests фиксируют difference at rendered prompt level.

### Фаза 12. Day 13 end-to-end pause/resume demo

Цель: доказать восстановление state после restart.

Действия:

- создать task;
- установить plan, criteria, current_step, expected_action;
- move planning -> execution -> validation in allowed path;
- pause на каждом рабочем stage in tests;
- проверить `stage=done` pause как terminal no-op без reopening work;
- simulate restart через новый App instance с тем же temp storage;
- `/task resume` восстанавливает current task;
- PromptBuilder после resume включает state и working memory;
- next fake provider answer reflects resumed stage/current_step.

Tests:

- `TestDay13PauseResumeAfterRestart`;
- `TestDay13ResumeKeepsWorkingMemoryAvailable`;
- `TestDay13NoRepeatedTaskExplanationNeeded`;
- `TestDay13ForbiddenTransitionPreservesFileBytesOrState`.

Done criteria:

- task можно продолжить без повторного описания;
- `current_step` и `expected_action` видны после restart;
- working memory доступна после resume.

### Фаза 13. Day 11 end-to-end memory influence demo

Цель: доказать, что сохранённые layers влияют на ответы.

Действия:

- создать session;
- создать task;
- задать запрос, который fake classifier раскладывает в `short`, `work`, `long`, `ignore`;
- показать proposal;
- apply proposal;
- inspect `/memory short`, `/memory work`, `/memory long`;
- убедиться, что `ignore` есть только в audit;
- задать следующий запрос;
- fake provider должен получить prompt с applied memory bundle;
- verify response changes based on memory bundle;
- clear short;
- verify short-only fact no longer affects prompt while work/long remain.

Tests:

- `TestDay11EndToEndMemoryProposalApplyInspect`;
- `TestDay11MemoryInfluencesNextPromptAndResponse`;
- `TestDay11ClearShortRemovesSessionOnlyInfluence`;
- `TestDay11IgnoreNeverBecomesPhysicalLayer`.

Done criteria:

- Day 11 acceptance закрыт full flow, not manual-only;
- visible files and commands prove layer separation;
- memory influence проверяется deterministic fake provider.

### Фаза 14. Error handling и CLI polish

Цель: сделать MVP устойчивым при типовых ошибках.

Действия:

- определить error categories: CLI, storage, validation, provider, classifier;
- no stack trace in normal mode;
- diagnostics to stderr;
- primary command output to stdout;
- `--json` обязателен для ключевых inspection/smoke commands;
- non-interactive commands use stable exit codes and JSON error envelope;
- control characters from provider/storage output escape by default;
- graceful handling for missing current task/profile/model.

Tests:

- `TestInvalidCommandShowsHint`;
- `TestMissingActiveProfileError`;
- `TestMissingActiveTaskForWorkSave`;
- `TestBrokenStorageDoesNotPanic`;
- `TestMachineReadableOutputIsParseable`;
- `TestExitCodesAndJSONErrorEnvelope`.

Done criteria:

- common errors readable;
- CLI session does not crash on invalid command/provider timeout/broken JSON;
- smoke scripts can parse needed output.

### Фаза 15. Final smoke scripts and manual demo checklist

Цель: иметь один воспроизводимый путь приёмки.

Действия:

- добавить documented smoke flow, например `docs` не менять без отдельного запроса, но можно держать в test names или README later if asked;
- использовать temp storage для automated tests;
- live manual demo описать в plan/test output comments;
- run `go test ./...`;
- run CLI smoke with fake provider if app supports provider injection/env flag;
- run optional live OpenRouter smoke only when `OPENROUTER_API_KEY` present.

Done criteria:

- `go test ./...` подтверждает Day 11/12/13;
- no test requires live key by default;
- optional live smoke documented in command output or plan.

## 11. Traceability matrix

### 11.1. Day 11 traceability

| Criterion | Implementation | Verification |
| --- | --- | --- |
| Минимум 3 типа памяти | `LayerShort`, `LayerWork`, `LayerLong` | `TestLayersAreSeparateFiles` |
| Раздельное хранение | session JSONL, task work JSONL, long-term JSONL | `/memory short|work|long`, file checks |
| Явный выбор что и куда | `MemoryClassifier` returns proposal; `/memory apply` confirms | `TestDay11EndToEndMemoryProposalApplyInspect` |
| LLM classifier mandatory | classifier provider call after response and `/memory propose` | fake provider call assertions |
| Проверка данных каждого слоя | inspection commands | `TestMemoryCommandsShowSeparateLayers` |
| Влияние на ответы | PromptBuilder includes memory bundle; fake provider varies response | `TestDay11MemoryInfluencesNextPromptAndResponse` |
| `ignore` не storage | proposal audit only | `TestIgnoreOnlyStaysInAuditTrail` |

### 11.2. Day 12 traceability

| Criterion | Implementation | Verification |
| --- | --- | --- |
| User profile exists | `<storage_root>/profiles/<id>.json` | `TestCreateProfile` |
| Style/format/constraints | `UserProfile` fields | profile validation tests |
| Profile every prompt | PromptBuilder active profile block | `TestProfileAttachedToPrompt` |
| Different profiles | `student`, `senior` render differently | `TestSameQueryDifferentProfilesChangePrompt` |
| Automatic use | chat loads active profile from config | `TestProfileAttachedToEveryLLMCall` |

### 11.3. Day 13 traceability

| Criterion | Implementation | Verification |
| --- | --- | --- |
| `stage` | `TaskState.Stage` | `TestTaskStateStoresStageStepExpectedAction` |
| `current_step` | `TaskState.CurrentStep` and `/task step` | restart persistence test |
| `expected_action` | `TaskState.ExpectedAction` and `/task expect` | invalid action test |
| FSM transitions | `AllowedTransitions` map | allowed/forbidden transition tests |
| Pause any stage | `/task pause` preserves state | planning/execution/validation table test |
| Resume no repeated explanation | persisted task + working memory in prompt | `TestDay13PauseResumeAfterRestart` |

## 12. Acceptance smoke scenario

### 12.1. Initial setup

```text
assistant init --model openai/gpt-4.1-mini
/profile create student
/profile create senior
/profile student
/task start "CLI assistant MVP"
/task step "сформировать acceptance criteria"
/task expect user_confirmation
```

Expected:

- `<storage_root>/config.json` exists;
- profile files exist;
- `tasks/current.json` contains `stage=planning`, `current_step`, `expected_action=user_confirmation`, `status=active`;
- no API key in storage root.

### 12.2. Day 11 memory flow

```text
User: Спланируй модуль памяти. Требование: CLI должен поддерживать выбор модели OpenRouter. Я предпочитаю короткие ответы на русском.
Assistant: <answer>
Memory proposal:
  [work] requirement: CLI должен поддерживать выбор модели OpenRouter.
  [long] preference: Пользователь предпочитает короткие ответы на русском.
  [short] context: В текущем диалоге планируем модуль памяти.
  [ignore] smalltalk/noise if any
/memory apply
/memory short
/memory work
/memory long
```

Expected:

- proposal visible before save;
- accepted records appear in correct layer;
- `[ignore]` absent from memory layers;
- audit file contains proposal and statuses;
- next answer uses saved work/long records.

### 12.3. Day 12 profile flow

```text
/profile student
User: Объясни архитектуру memory layers.
/profile senior
User: Объясни архитектуру memory layers.
```

Expected:

- rendered prompts contain different profile blocks;
- `student` response is more explanatory;
- `senior` response is shorter and trade-off focused;
- user did not copy profile text into query.

### 12.4. Day 13 pause/resume flow

```text
/task status
/task move execution
/task step "реализовать MemoryManager"
/task expect llm_response
/save work "Acceptance: memory layers must be separate files"
/task pause
/exit
assistant chat
/task resume
/task status
User: Продолжай задачу.
```

Expected:

- resume restores `stage=execution`;
- resume restores `current_step=реализовать MemoryManager`;
- resume restores `expected_action=llm_response`;
- working memory still available;
- assistant continues without asking user to explain task again.

## 13. Test matrix

### 13.1. Unit tests

- config load/save and precedence;
- safe path validation;
- atomic writes;
- JSONL append/list;
- profile validation/rendering;
- state machine transitions;
- memory record validation;
- proposal JSON parsing;
- secret redaction;
- prompt rendering order.

### 13.2. Integration tests

- init creates storage tree;
- chat with fake provider appends short-term history;
- classifier with fake provider creates proposal;
- apply proposal writes separate layers;
- profile switch affects next prompt;
- task pause/resume after new App instance;
- model switch affects provider call.

### 13.3. Acceptance tests

- Day 11 full flow: classify -> proposal -> apply -> inspect -> prompt influence;
- Day 12 full flow: student/senior profiles -> same query -> different prompt/response;
- Day 13 full flow: start -> step/expect -> pause -> restart -> resume -> continue.

### 13.4. Security/privacy tests

- no `OPENROUTER_API_KEY` in config/profiles/memory/provider payload records/audit/logs; raw transcripts are not written in P0;
- bearer token blocked;
- `sk-...` token blocked/redacted;
- prompt-injection text in memory/profile is tagged as data;
- custom base URL not default;
- provider timeout does not crash CLI.

### 13.5. Deterministic process-control tests

- stage-specific golden prompts for `planning`, `execution`, `validation`, `done`;
- validation stage prompt contains strict reviewer/QA role;
- `ProcessController` blocks normal task continuation while paused before provider call;
- `ProcessController` rejects `ActionKind` not allowed by current `StagePolicy`;
- wrong-stage LLM output is rejected before accepted short-memory persistence;
- validation-stage output that implements fixes is rejected or routes back to `execution`;
- LLM transition signal does not mutate task state without `TransitionGate`;
- memory classifier runs only after accepted assistant output, preserving Day 11 flow.

### 13.6. Day 11/12/13 non-regression checks

- ProcessController must not replace `MemoryClassifier -> proposal -> user confirmation -> apply`.
- Stage-specific prompts must not remove active profile from any prompt.
- Stage policy must not add `status=done`; completion remains `stage=done` and `expected_action=none`.
- Validation/review role must not block Day 13 pause/resume and restored context behavior.

## 14. Implementation sequence with checkpoints

1. Bootstrap Go module and CLI help.
2. Add `.assistant/` to `.gitignore` for demo/test opt-in.
3. Implement config and safe storage with OS user-data default, permissions, locks, and path/symlink checks.
4. Implement provider interface, OpenRouter provider, fake provider, and pre-provider scanner.
5. Implement profile manager and commands.
6. Implement task state manager and P0 commands.
7. Implement memory manager, `WorkMemoryStore`, and layer inspection.
8. Implement prompt builder, canonical block schema, stage-specific role prompts, and rendered prompt tests.
9. Implement basic chat loop with ProcessController hard gates and short-term history after profile/task/memory/prompt gates.
10. Implement memory classifier prompt and strict JSON parser through provider interface.
11. Implement proposal audit store with minimized/redacted default retention.
12. Implement proposal apply with accept/edit/reject/blocked statuses and idempotency.
13. Add invariant checker, provider disclosure, redaction, and purge behavior.
14. Add non-interactive JSON command smoke path and exit-code tests.
15. Add Day 11 acceptance test.
16. Add Day 12 acceptance test.
17. Add Day 13 acceptance test.
18. Add ProcessController, StagePolicyRegistry, ResponseValidator, TransitionGate and deterministic process-control tests.
19. Add error handling polish.
20. Run `go test ./...`.
21. Run fake-provider CLI smoke.
22. Run optional live OpenRouter smoke if `OPENROUTER_API_KEY` exists.

## 15. Non-goals for MVP

Do not implement in P0:

- automatic file editing tools;
- IDE agent behavior;
- repository RAG;
- vector DB;
- web UI;
- multi-agent workflow;
- automatic long-term memory writes without confirmation;
- hidden local API key persistence;
- commits/git automation;
- full TUI with Bubble Tea.

These non-goals prevent bypassing Day 11/12/13 by building unrelated functionality.

## 16. Main risks and mitigations

### Risk: classifier returns bad JSON

Mitigation:

- strict parser;
- one bounded retry with stricter correction prompt;
- no records created on failure;
- user sees classifier error.

### Risk: memory layers accidentally mix

Mitigation:

- type-level `MemoryLayer` excludes `ignore`;
- separate storage methods and paths;
- tests assert physical files;
- `/memory <layer>` reads only one layer.

### Risk: profile becomes prompt injection vector

Mitigation:

- render profile as data block;
- system rules outrank profile;
- prompt-injection tests.

### Risk: paused task continues execution

Mitigation:

- TaskState gate before normal chat;
- PromptBuilder paused warning;
- mutation commands blocked while paused except resume/inspection;
- tests for paused behavior.

### Risk: secrets saved in audit trail

Mitigation:

- run redaction/blocking before proposal persistence;
- store block reason and secret type, not raw secret;
- scan storage root in tests for forbidden patterns.

### Risk: live OpenRouter makes tests flaky

Mitigation:

- fake provider for all deterministic tests;
- optional live smoke only behind env key;
- provider interface and fixtures.

## 17. Definition of done

Implementation is done only when all conditions hold:

- `go test ./...` passes;
- `assistant init` creates storage and profile without saving API key;
- active model can be selected and changed;
- active profile is included in every rendered prompt;
- `student` and `senior` profiles produce different prompt behavior;
- `/task start/status/step/expect/move/pause/resume` work;
- forbidden transition fails without state mutation;
- pause/resume after restart works without repeated task explanation;
- after chat response, memory classifier can propose `short`, `work`, `long`, `ignore`;
- proposal is visible before save;
- accepted `short`, `work`, `long` records land in separate physical files;
- `ignore` remains audit-only;
- `/memory short|work|long` inspect each layer separately;
- saved memory affects next prompt/response;
- secrets are blocked/redacted in manual save, proposal, memory, audit;
- `.assistant/` is gitignored for demo/test opt-in;
- no Day 11/12/13 criterion is replaced by a manual-only or future-only flow.
