# FRD: AI coding agent CLI в классе Claude Code / Codex CLI

## 1. Назначение документа

FRD описывает функциональные требования к MVP консольного помощника для работы с кодом. Документ дополняет `prd.md` и `architect.md`: PRD объясняет цель продукта, architecture описывает устройство системы, FRD фиксирует конкретное поведение функций и проверяемые требования.

Цель продукта: консольный помощник для работы с кодом в том же классе, что Claude Code и Codex CLI. Пользователь должен воспринимать систему как помощника для работы в репозитории: он пишет задачу в чате, помощник планирует, читает контекст, применяет изменения через контролируемый слой безопасности, запускает проверки, показывает diff и доказательства, ведёт задачу до результата. P0 реализует слой управления для этого поведения и уже применяет файлы из структурированного результата `execution`; общего shell/tool layer в P0 ещё нет.

Жёсткие критерии приёмки берутся из:

- `day11.md`: явная модель памяти с отдельными слоями;
- `day12.md`: персонализация через профиль пользователя;
- `day13.md`: состояние задачи как конечный автомат;
- `day14.md`: отдельный invariant layer с prompt visibility и semantic conflict refusal через out-of-band LLM validator;
- `day15.md`: контролируемый lifecycle, planning swarm, microtask agents, app-issued evidence и запрет ручного управления внутренним state в основном пользовательском flow.

## 2. Scope MVP

MVP/P0 должен реализовать foundation для coding-agent CLI:

- CLI-интерфейс;
- подключение к OpenRouter;
- выбор модели;
- интерактивный chat loop;
- профили пользователей;
- подключение активного профиля к каждому запросу;
- memory layers: `short`, `work`, `long`;
- LLM memory classification через OpenRouter;
- memory proposal с подтверждением пользователя;
- физически раздельное хранение слоёв памяти;
- task state machine с `stage`, `current_step`, `expected_action`;
- stage-aware prompt contract: LLM получает текущий этап, роль этапа, allowed actions и forbidden actions;
- pause/resume задачи без повторного объяснения контекста;
- invariant manager/checker: отдельное storage, prompt block, input/output enforcement.
- lifecycle gate: application-level допуск переходов `planning -> execution -> validation -> done`;
- prompt improvement перед provider call;
- planning swarm для role-specific review планирования и финального merged plan; specialist outputs должны содержать verdict/contribution, findings и proposed plan/criteria changes, а не пересказ исходной задачи;
- microtask agents для execution/review roles;
- безопасная материализация файлов из `execution.deliverable`: заголовок файла + fenced code block, путь только внутри репозитория, создание каталога, запись файла, секция `Files` в пользовательском выводе;
- trusted evidence store: app-issued verification evidence создаётся автоматически через language-agnostic `VerificationResolver` after approved-plan approval or semantic intent signal; resolver uses exact approved command first, otherwise asks a structured verification planner/referee for strict JSON, and local command policy/sandbox remain the only execution authority. `--verify` остаётся explicit override/debug, а в provider уходит только bounded summary/hash.

Текущее состояние реализации на 2026-06-19:

- Все базовые компоненты MVP существуют в Go-коде: Cobra CLI, OpenRouter/fake provider, profile manager, memory manager/classifier/proposal store, task FSM, prompt builder, process controller, structural validators, semantic validators, transition gate, lifecycle gate, prompt improver, planning swarm, agent runner, trusted evidence store, audit store.
- Default storage root: `os.UserConfigDir()/coding-writer-assistant`; repo-local `.assistant/` используется только через explicit `--storage-dir` или `ASSISTANT_STORAGE_DIR`.
- Acceptance flow проверяется fake provider tests: `TestDay11EndToEndMemoryProposalApplyInfluence`, `TestDay12ProfilesChangePromptAndResponse`, `TestDay13PauseResumeAfterRestartUsesWorkingMemory`.
- Process-control flow проверяется тестами reviewer prompt, paused hard gate, invalid-output retry, validation-to-done transition, rejected-output no-persistence, approval validation, lifecycle evidence и materialized execution artifacts. Day 15 live manual proof описан в `docs/manual-testing-demo.md`; `scripts/day15-demo.sh --fake --auto` является стабильной проверкой регрессий.

MVP не должен реализовывать:

- произвольное автоматическое редактирование файлов проекта вне структурированного `execution.deliverable` и безопасного слоя путей;
- полноценный IDE agent с IDE-specific integrations;
- production-grade RAG по репозиторию;
- vector database;
- general-purpose autonomous multi-agent IDE workflow beyond the Day 15 planning/execution/validation orchestration;
- web UI;
- silent long-term memory writes.

Эти ограничения относятся только к P0. Для продукта в классе Claude Code / Codex CLI P1/P2 должны добавить полноценные инструменты репозитория: чтение файлов, diff, подтверждение рискованных изменений, shell/test execution и восстановление после ошибок с сохранением того же пользовательского пути в чате.

## 3. Термины

`Profile` — профиль пользователя со стилем, форматом ответа и ограничениями.

`Session` — текущий запуск или диалоговая сессия CLI.

`Task` — текущая рабочая задача ассистента.

`Task state` — формальное состояние задачи: `stage`, `current_step`, `expected_action`, `status`.

`Short-term memory` — память текущего диалога.

`Working memory` — память текущей задачи.

`Long-term memory` — долговременные предпочтения, решения, знания и ограничения.

`Memory proposal` — JSON-результат LLM-классификации фактов по слоям памяти.

`Memory classifier` — отдельный LLM-вызов через OpenRouter, который решает, какие факты куда предложить сохранить.

`Invariant` — ограничение, которое нельзя нарушать между запросами.

`InvariantViolation` — structured refusal record с `invariant_id`, `severity`, `message`, `evidence`.

`ProcessController` — application-level controller, который до provider call выбирает current stage, разрешённое действие, stage policy, prompt contract и после provider call принимает или отклоняет output.

`StagePolicy` — trusted policy для конкретного `stage`: роль LLM, allowed actions, forbidden actions, output schema, validation rules.

`ActionKind` — выбранное приложением действие для текущего exchange, например `plan_task`, `execute_plan_step`, `review_output`, `summarize_done`.

`StagePromptFactory` — компонент, который добавляет trusted stage-specific system prompt перед untrusted profile/task/memory blocks.

`TransitionGate` — единственный компонент, который применяет stage transition после структурной и смысловой validation; LLM может только предложить signal.

## 4. Пользовательские роли

`User` — человек, который запускает CLI, выбирает профиль и модель, задаёт вопросы, подтверждает memory proposal и управляет задачами.

`Assistant` — CLI-приложение, которое вызывает OpenRouter, собирает prompt, хранит состояние и показывает ответы.

`LLM Provider` — OpenRouter-compatible API, который возвращает ответы и memory classification JSON.

## 5. Functional Requirements

### Canonical contract

FRD is source of truth for the implementation contract together with PRD and architecture. The three documents must not disagree on Day 11, Day 12, Day 13, Day 14, or Day 15 semantics.

#### Task state canonical values

- `stage`: `planning`, `execution`, `validation`, `done`.
- `status`: `active`, `paused`.
- `expected_action`: `user_input`, `llm_response`, `user_confirmation`, `none`.
- `tool_result` не входит в P0 как общий инструментальный поток; текущий P0 поддерживает только безопасную материализацию файлов из `execution.deliverable` и доверенную проверку разрешённых команд.
- completion is represented by `stage=done` and `expected_action=none`; `status=done` is not part of MVP.

#### Command contract

- top-level and slash commands must map to one canonical command tree;
- P0 commands are only the ones required for Day 11/12/13/14/15 demo and smoke tests.
- the command tree must define P0/P1 status, top-level and slash forms, JSON output, exit behavior, and whether a command calls OpenRouter.

#### Memory layers

- physical storage layers: `short`, `work`, `long`;
- `ignore` exists only in memory proposal and audit trail;
- all examples and tests must use the same layer names.

#### Process-control contract

- LLM must know the current `stage`, `current_step`, `expected_action`, task `status`, selected `ActionKind`, allowed actions and forbidden actions for task-scoped prompts.
- Stage-specific trusted prompt is required for process-controlled task work: planner in `planning`, implementer in `execution`, strict reviewer/QA in `validation`, terminal summarizer in `done`.
- LLM does not update `TaskState`, write memory, run tools, persist output or apply transitions directly.
- `ProcessController`, `TransitionGate` and `LifecycleGate` own hard gates, output acceptance and state transitions.
- This contract extends Day 13 behavior but must not bypass Day 11 classifier/proposal/user-confirmation flow or Day 12 profile-in-every-prompt flow.

#### UX hard requirements: no internal-state choreography

- Normal user flows MUST NOT require manual orchestration of internal state. The product must infer intent and drive task lifecycle through application code.
- The happy path for task work MUST NOT require `/task start`, `/task move`, `/task step`, `/task expect`, manual storage edits, direct JSON edits, or direct writes to memory/task/invariant files.
- Slash/top-level commands for task state are allowed only for optional inspection, explicit pause/resume, recovery, debugging, and deterministic tests. They are not valid substitutes for agent-driven behavior in acceptance demos.
- If a scenario needs a user decision, the CLI must expose it as product semantics, not internals. Examples: memory proposal apply/reject, planning approval, pause/resume, and user-level "проверь/заверши" verification intent.
- Any new feature that exposes implementation details as required user steps is rejected until it has an intent-driven flow and regression coverage.

#### Day 15 lifecycle contract

- The primary task path is chat-driven inside one `cw` TUI session: user states the goal, approves a plan and asks to check/finish in normal language; after approval or strict semantic check/finish intent, the application resolves trusted verification through exact approved commands or a structured verification planner, then drives task creation, stage changes, current step, validation status and done state.
- `planning -> execution` requires a concrete plan, acceptance criteria and a separate approval-validation record.
- `execution -> validation` requires accepted execution output plus app-issued trusted evidence when criteria mention tests or verification.
- Accepted execution output may include materialized files; the app must write only repo-local safe paths extracted from structured `deliverable` blocks and must show applied files in the human output.
- `validation -> done` requires accepted validation output and criteria-matched trusted evidence; LLM text alone cannot mark a task done.
- Planning swarm must produce role-specific specialist reviews and one final merged plan; audit and human output must expose specialist roles, concrete verdict/contribution, finding count and proposed changes when present.
- Execution and review must run through role-scoped microtask agents, not generic untracked provider calls.
- Prompt improvement may rewrite the outbound task prompt, but it must preserve the user's objective and stage policy.
- Manual `/task move`, `/task step`, `/task expect`, storage edits and JSON edits are invalid as Day 15 acceptance proof.

#### Invariant contract

- active invariants are stored in `<storage_root>/invariants/project.jsonl`, not in session dialogue;
- prompt builder renders active invariants with `Invariant policy` and `id="invariants.active"`; invariants semantically outrank profile, memory, task, and user query even when rendered after profile for prompt readability;
- input conflict is checked by an out-of-band LLM invariant validator and returns `invariant_conflict` before the normal chat provider call;
- output conflict is checked by the same invariant validator and returns `invariant_conflict` immediately, with no correction retry, before short-memory persistence, process transition, and memory classifier;
- refusal must name invariant ID and evidence, and JSON output must expose structured violations;
- `forbidden_terms` are examples/fallback signals, not the primary product decision for policy conflicts;
- local invariant checks may only be hard gates/fallbacks; semantic policy conflict decisions must use LLM structured validation or another documented semantic method;
- custom/user invariants are privileged local policy data, rendered with source labels and bounded count/length limits; invariant content may be provider-visible;
- default invariants cover stack, process ownership, memory layers, no silent long-term writes, secrets, paused task, done terminal task, and OpenRouter key env-only.

### FR-001. Первый запуск

Требование: приложение должно поддерживать первый запуск без существующей конфигурации.

Поведение:

- CLI проверяет наличие runtime storage;
- CLI создаёт runtime storage, если его нет;
- CLI проверяет наличие выбранной модели;
- `assistant init` требует model id через `--model` или `ASSISTANT_MODEL`;
- `assistant init` validates model id syntax locally and saves config without provider lookup;
- CLI создаёт default profiles `student` и `senior`, если профилей нет;
- активный профиль по умолчанию `student`, если другой не задан через config/env/flag.

MVP policy:

- default config/storage root must be documented explicitly;
- env vars override config;
- hidden input or local key file is not part of P0 API key flow;
- first run must show provider data-disclosure before any OpenRouter call;
- interactive model/profile interview is not implemented; current flow is scriptable commands and default profiles.

Acceptance criteria:

- запуск без existing storage root не падает;
- `assistant init --model <id>` создаёт `config.json`;
- `assistant init --model <id>` создаёт default profiles `student` и `senior`;
- `assistant init --model <id>` создаёт default project invariants in `<storage_root>/invariants/project.jsonl`;
- API key не записывается в `config.json`.

### FR-002. OpenRouter API key

Требование: приложение должно использовать OpenRouter API key безопасно.

Поведение:

- CLI читает `OPENROUTER_API_KEY` из environment;
- если ключ отсутствует, CLI сообщает пользователю, что ключ нужен;
- ключ не должен сохраняться в docs, profiles, memory files, prompt audit, audit trail или config;
- chat и classifier payload проходят локальный pre-provider secret scan до отправки;
- при 401/403 CLI показывает понятную ошибку.

Privacy requirements:

- classify what categories of data are sent to the provider;
- classifier calls may be disabled only in explicit privacy/debug/offline mode, and that mode is not valid for Day 11 acceptance;
- deterministic tests may use a fake provider through the same classifier interface, not a bypass;
- document timeout and retry behavior.

Acceptance criteria:

- при отсутствии ключа запрос к модели не выполняется;
- при неверном ключе пользователь видит ошибку авторизации;
- fake-provider tests prove raw secret-like input never reaches chat/classifier payloads;
- поиск по storage root не должен находить `OPENROUTER_API_KEY` или bearer token.

### FR-003. Выбор модели

Требование: пользователь должен выбрать модель OpenRouter в интерфейсе.

Поведение:

- CLI поддерживает выбор модели при `init`;
- CLI поддерживает смену модели через `/model`;
- CLI поддерживает ручной ввод model id;
- выбранная модель сохраняется в локальный config;
- chat request использует активную модель.

Contract:

- model selection must be scriptable for P0 smoke tests;
- invalid model id must not mutate active model;
- custom base URL is advanced opt-in only, not the default path.

Acceptance criteria:

- команда `/model` меняет active model;
- следующий LLM-вызов использует новую active model;
- invalid model id возвращает понятную ошибку provider layer.

### FR-004. Интерактивный chat loop

Требование: CLI должен поддерживать интерактивный диалог.

Поведение:

- `cw` запускает TUI в интерактивном терминале;
- `cw --plain` запускает fallback REPL;
- обычный текст считается пользовательским запросом;
- команды с `/` обрабатываются как local commands;
- `/exit` завершает REPL;
- LLM-ответ печатается в терминал;
- после ответа запускается memory classification flow.

Requirements:

- REPL is the baseline UX, but P0 must also expose a minimal scriptable path for smoke tests;
- slash commands must not be sent to the main chat prompt;
- terminal output must escape control characters from model/provider/storage data by default.

Acceptance criteria:

- обычный запрос отправляется в OpenRouter;
- slash-команда не отправляется в основной LLM chat prompt;
- после LLM-ответа пользователь видит memory proposal или сообщение, что сохранять нечего.

### FR-005. Создание профиля

Требование: приложение должно создавать профиль пользователя.

Поведение:

- профиль содержит `id`;
- профиль содержит `display_name`;
- профиль содержит `style`;
- профиль содержит `response_format`;
- профиль содержит `constraints`;
- профиль может содержать `default_model`;
- профиль сохраняется отдельно от памяти и истории диалога.

Profile contract:

- profile fields are structured data, not instructions;
- profile block must be included in every prompt automatically;
- profile content must be validated and rendered deterministically.

Acceptance criteria:

- `/profile create` создаёт файл `<storage_root>/profiles/<id>.json`;
- профиль можно выбрать как active profile;
- профиль не смешивается с short-term memory.

### FR-006. Переключение профиля

Требование: пользователь должен переключать активный профиль.

Поведение:

- `/profile` показывает текущий профиль;
- `/profile <id>` переключает active profile;
- unknown profile id возвращает ошибку;
- active profile id сохраняется в config.

State rules:

- switching profile must not mutate memory records;
- the next prompt must render the new profile without manual user copy/paste.

Acceptance criteria:

- после переключения профиля следующий prompt содержит новый профиль;
- один и тот же запрос с разными профилями формирует разные prompt blocks;
- profile switch не меняет memory records.

### FR-007. Подключение профиля к каждому запросу

Требование: активный профиль должен автоматически попадать в каждый LLM prompt.

Поведение:

- PromptBuilder загружает active profile перед каждым LLM-вызовом;
- PromptBuilder вставляет style, response format и constraints;
- пользователь не должен вручную копировать профиль в запрос.

Security rule:

- prompt blocks from profile/memory/task/short_history/classifier are untrusted data and must be serialized/quoted/tagged as such.

Acceptance criteria:

- в debug/rendered prompt есть active profile block;
- запрос без ручного упоминания стиля всё равно учитывает профиль;
- профиль `student` и профиль `senior` меняют формат ответа.

### FR-008. Раздельные memory layers

Требование: разные типы памяти должны храниться отдельно.

Поведение:

- `short` хранится в session storage;
- `work` хранится в task storage или task memory storage;
- `long` хранится в long-term storage;
- `ignore` не сохраняется как memory layer;
- каждый слой читается отдельной командой.

Acceptance criteria:

- `/memory short` показывает только short-term records;
- `/memory work` показывает только working records;
- `/memory long` показывает только long-term records;
- accepted `[ignore]` не появляется ни в одном memory layer;
- файлы хранения слоёв физически разные.

### FR-009. Short-term memory

Требование: приложение должно хранить память текущего диалога.

Поведение:

- short-term memory содержит последние user/assistant messages;
- short-term memory может содержать краткие session notes из memory proposal;
- `/clear short` очищает short-term records текущей сессии;
- short-term memory не становится long-term без явного proposal/apply.

Acceptance criteria:

- после LLM-ответа user и assistant messages записаны в session history;
- `/memory short` показывает текущую session history или summary records;
- `/clear short` очищает short-term layer, но не трогает work и long.

### FR-010. Working memory

Требование: приложение должно хранить память текущей задачи.

Поведение:

- working memory содержит цель задачи;
- working memory содержит task state;
- working memory содержит plan;
- working memory содержит acceptance criteria;
- working memory содержит decisions;
- working memory содержит open questions;
- `[work]` records из memory proposal сохраняются в рабочий слой после подтверждения.

Acceptance criteria:

- после `/task start` появляется current task;
- после accepted `[work]` proposal запись видна в `/memory work`;
- working memory попадает в prompt перед short-term history.

### FR-011. Long-term memory

Требование: приложение должно хранить долговременные предпочтения, решения и знания.

Поведение:

- long-term memory хранит stable preferences;
- long-term memory хранит reusable decisions;
- long-term memory хранит constraints;
- `[long]` records сохраняются только после подтверждения;
- secrets запрещены в long-term memory.

Acceptance criteria:

- accepted `[long]` proposal виден в `/memory long`;
- long-term preference влияет на следующий ответ;
- попытка сохранить API key в long-term memory блокируется.

### FR-012. LLM memory classification

Требование: выбор слоя памяти должен выполняться через отдельный LLM-вызов OpenRouter.

Поведение:

- после значимого LLM-ответа запускается memory classifier;
- classifier получает последнее user message;
- classifier получает последний assistant response;
- classifier получает active profile;
- classifier получает current task state;
- classifier получает правила memory layers;
- classifier возвращает strict JSON;
- JSON содержит records с `layer`, `kind`, `content`, `reason`, `confidence`.

Acceptance boundary:

- Day 11 считается закрытым только если classifier вызван через provider interface и вернул proposal;
- ручной `/save` и отключённый classifier являются escape hatch/debug режимами, но не Day 11 acceptance;
- classifier failure не создаёт memory records, кроме redacted failure audit.

Acceptance criteria:

- classifier может вернуть `short`, `work`, `long`, `ignore`;
- invalid JSON приводит к понятной ошибке или retry;
- proposal показывается пользователю до сохранения;
- proposal audit сохраняет, что LLM предложила.
- fake provider tests проходят тот же classifier/proposal/apply flow без live key.

### FR-013. Memory proposal review

Требование: пользователь должен видеть и подтверждать memory proposal.

Поведение:

- CLI показывает список proposed records;
- каждый record показывает layer;
- каждый record показывает content;
- каждый record показывает reason;
- пользователь может принять proposal;
- пользователь может отклонить proposal;
- пользователь может отредактировать layer или content перед сохранением.

Acceptance criteria:

- без подтверждения proposal не применяется;
- rejected record не попадает в memory layer;
- edited record сохраняется с обновлёнными данными;
- status proposal record обновляется как `accepted`, `edited`, `rejected` или `blocked`.

### FR-014. Memory proposal audit trail

Требование: приложение должно хранить audit trail предложений памяти.

Поведение:

- каждый proposal получает id;
- proposal сохраняет source message ids;
- proposal сохраняет records;
- proposal сохраняет created_at;
- proposal сохраняет status каждого record;
- blocked/rejected/ignore records остаются в audit trail, но не в memory layer.

Acceptance criteria:

- файл `memory_proposals.jsonl` появляется в session storage;
- можно проверить, какие данные LLM предложила для каждого слоя;
- можно сравнить proposal и фактически сохранённые records.

### FR-015. Ручное сохранение памяти

Требование: ручное сохранение должно быть доступно как escape hatch.

Поведение:

- `/save short <text>` сохраняет в short-term memory;
- `/save work <text>` сохраняет в working memory;
- `/save long <text>` сохраняет в long-term memory;
- ручное сохранение проходит secret redaction/invariant checks;
- ручное сохранение не заменяет основной Day 11 flow через LLM classifier.

Acceptance criteria:

- `/save work ...` создаёт work record;
- `/save long ...` с секретом блокируется;
- ручной record виден в соответствующем `/memory <layer>`.

### FR-016. Task start

Требование: приложение должно создавать текущую задачу.

Поведение:

- `/task start <title>` создаёт task id;
- task получает title;
- task получает initial stage `planning`;
- task получает initial status `active`;
- task получает empty или default current_step;
- task получает expected_action `user_input` или `llm_response`.

Acceptance criteria:

- после `/task start` файл `tasks/current.json` существует;
- `/task status` показывает title, stage, current_step, expected_action, status;
- task context попадает в следующий prompt.

### FR-017. Task finite state machine

Требование: stage задачи должен валидироваться как конечный автомат.

Поведение:

- допустимые stage: `planning`, `execution`, `validation`, `done`;
- `planning -> execution` разрешён;
- `execution -> validation` разрешён;
- `validation -> execution` разрешён;
- `validation -> done` разрешён;
- `execution -> planning` разрешён при изменении требований;
- остальные переходы запрещены.

Acceptance criteria:

- `/task move execution` из `planning` работает;
- запрещённый переход возвращает ошибку;
- запрещённый переход не меняет `current.json`;
- allowed transitions попадают в prompt.

### FR-018. Current step

Требование: задача должна хранить текущий шаг.

Поведение:

- `/task step <text>` обновляет `current_step`;
- current_step сохраняется в task storage;
- current_step отображается в `/task status`;
- current_step попадает в PromptBuilder.

Acceptance criteria:

- после `/task step "согласовать план"` статус показывает этот шаг;
- после перезапуска CLI current_step сохраняется;
- LLM-ответ учитывает текущий шаг.

### FR-019. Expected action

Требование: задача должна хранить ожидаемое действие.

Поведение:

- `/task expect <action>` обновляет `expected_action`;
- допустимые values: `user_input`, `llm_response`, `user_confirmation`, `none`;
- invalid action возвращает ошибку;
- expected_action попадает в PromptBuilder.

Acceptance criteria:

- `/task expect user_confirmation` сохраняется;
- invalid expected action не меняет task state;
- при `expected_action=user_confirmation` ассистент не должен делать вид, что подтверждение уже получено.

### FR-020. Task pause

Требование: задачу можно поставить на паузу на любом этапе.

Поведение:

- `/task pause` устанавливает `status=paused`;
- pause сохраняет текущий stage;
- pause сохраняет current_step;
- pause сохраняет expected_action;
- pause записывает `paused_at`;
- paused task не должна продолжаться как active execution.

Acceptance criteria:

- pause работает из `planning`;
- pause работает из `execution`;
- pause работает из `validation`;
- pause из `done` является terminal no-op и не открывает задачу заново;
- после pause `/task status` показывает `paused`;
- следующий chat prompt содержит warning, что задача paused.

### FR-021. Task resume

Требование: задачу можно продолжить без повторного объяснения.

Поведение:

- `/task resume` читает current task;
- resume требует существующий paused task;
- resume устанавливает `status=active`;
- resume записывает `resumed_at`;
- resume не сбрасывает stage;
- resume не сбрасывает current_step;
- resume не сбрасывает expected_action;
- следующий prompt получает восстановленный task context и working memory.

Acceptance criteria:

- после закрытия и повторного открытия CLI `/task resume` восстанавливает task state;
- пользователь не повторяет описание задачи;
- ассистент продолжает с сохранённого stage и current_step;
- working memory доступна после resume.

### FR-022. Task status

Требование: пользователь должен видеть текущее состояние задачи.

Поведение:

- `/task status` показывает task id;
- `/task status` показывает title;
- `/task status` показывает stage;
- `/task status` показывает current_step;
- `/task status` показывает expected_action;
- `/task status` показывает status;
- `/task status` показывает allowed next stages.

Acceptance criteria:

- `/task status` работает после `/task start`;
- `/task status` работает после restart CLI;
- status не требует LLM-вызова.

### FR-023. Prompt builder

Требование: PromptBuilder должен собирать контекст слоями.

Порядок блоков:

1. Base system rules.
2. Security and memory policy.
3. Trusted process-control policy.
4. Trusted stage-specific policy: role, allowed actions, forbidden actions, output schema.
5. Active profile.
6. Invariants from `<storage_root>/invariants/project.jsonl` with `Invariant policy` and `id="invariants.active"`; semantic priority is above profile/memory/task/user content.
7. Task state: stage, current_step, expected_action, status, allowed transitions.
8. Working memory.
9. Selected long-term memory.
10. Short-term history.
11. Current user query.

Acceptance criteria:

- task state идёт раньше working memory;
- profile block присутствует в каждом prompt;
- short-term history ограничивается размером окна;
- untrusted blocks use canonical tagged schema with block id, type, source, trust label, and escaping;
- task-scoped prompt includes a stage role before untrusted context;
- validation stage prompt gives the LLM strict reviewer/QA role and forbids implementation fixes in that exchange;
- stage policy does not replace active profile; profile block remains present in every prompt;
- PromptBuilder не пишет файлы и не вызывает OpenRouter.

Canonical untrusted block schema:

```text
<context_block id="profile.active" type="profile" source="storage" trust="untrusted">
escaped structured data
</context_block>
```

Required block types:

- `profile`;
- `task_state`;
- `working_memory`;
- `long_memory`;
- `short_history`;
- `classifier_input`;
- `classifier_output`.

Golden prompt fixtures must include prompt-injection strings inside each untrusted block.

### FR-023A. Stage-specific process control

Требование: task-scoped chat должен проходить через deterministic process policy, чтобы code assistant знал текущий этап и не действовал как generic model.

Поведение:

- приложение загружает current `TaskState` до provider call;
- приложение выбирает `ActionKind` и проверяет его против `StagePolicy`;
- приложение добавляет trusted stage-specific system prompt;
- LLM получает роль этапа: planner, implementer, reviewer/QA или terminal summarizer;
- LLM возвращает answer или structured candidate output в рамках выбранного stage;
- приложение валидирует output до accepted persistence and memory classifier;
- application `TransitionGate` применяет stage transition только после успешной проверки.

Acceptance criteria:

- `planning` prompt forbids implementation;
- `execution` prompt forbids acceptance criteria rewrite unless routed back to planning;
- `validation` prompt uses strict reviewer/QA role and forbids fixes/new features;
- `done` prompt forbids mutation;
- wrong-stage output is rejected before normal short-memory persistence;
- LLM transition proposal does not change task state without `TransitionGate`.

### FR-024. Chat behavior with paused task

Требование: paused task должна влиять на поведение ассистента.

Поведение:

- если task status `paused`, assistant не должен продолжать execution;
- assistant может ответить на общие вопросы;
- assistant должен напомнить, что задача paused, если запрос касается текущей задачи;
- для продолжения текущей задачи нужен `/task resume`.

Acceptance criteria:

- paused task не теряет context;
- chat request не меняет paused task state без команды;
- после `/task resume` агент продолжает работу по сохранённому context.

### FR-025. Memory influence on answers

Требование: сохранённая память должна влиять на ответы ассистента.

Поведение:

- short-term records влияют в рамках текущей session;
- working records влияют при active task;
- long-term records влияют между sessions;
- если memory conflict возникает, assistant явно сообщает о конфликте.

Acceptance criteria:

- после сохранения `[long] Пользователь предпочитает Go` ассистент предлагает Go без повторного указания;
- после сохранения `[work] CLI должен иметь OpenRouter model selection` план учитывает model selection;
- после очистки short-term memory session-only факт больше не влияет.

### FR-026. Error handling

Требование: ошибки должны быть понятными и не ломать CLI session.

Поведение:

- provider errors показываются как provider errors;
- storage errors показываются как storage errors;
- validation errors показываются как validation errors;
- invalid command показывает подсказку;
- CLI не печатает stack trace в обычном режиме.

CLI output rule:

- stdout is primary output only;
- stderr is for diagnostics and error hints;
- machine-readable `--json` output must stay parseable.

Acceptance criteria:

- network timeout не завершает процесс аварийно;
- invalid `/task expect` не меняет task state;
- broken JSON storage возвращает понятную ошибку и путь к файлу.

### FR-027. Secret blocking

Требование: приложение должно блокировать сохранение секретов.

Поведение:

- secret checker проверяет manual save;
- secret checker проверяет memory proposal;
- secret checker проверяет long-term memory writes;
- detected secrets блокируются или редактируются;
- blocked record получает status `blocked` в proposal audit.

Redaction rule:

- redact or block before persistence and before provider calls;
- store secret fingerprints or types instead of raw values.

Acceptance criteria:

- строка с `OPENROUTER_API_KEY=` не сохраняется;
- bearer token не сохраняется;
- blocked secret виден в audit как blocked без раскрытия полного секрета.

### FR-028. Local storage

Требование: runtime data должна храниться локально и прозрачно.

Поведение:

- normal default storage root is an OS user-data directory with `0700` directories and `0600` sensitive files;
- repo-local `.assistant/` is explicit demo/test opt-in only, for example `--storage-dir .assistant`;
- config хранится в `<storage_root>/config.json`;
- profiles хранятся в `<storage_root>/profiles/*.json`;
- sessions хранятся в `<storage_root>/sessions/<session_id>/`;
- tasks хранятся в `<storage_root>/tasks/`;
- long-term memory хранится в `<storage_root>/long_term/`;
- logs хранятся в `<storage_root>/logs/`.

Acceptance criteria:

- пользователь может открыть файлы и увидеть memory layers;
- task state сохраняется между запусками;
- repo-local `.assistant/` должен быть gitignored при demo/test режиме;
- normal storage root has restrictive permissions.

Storage rule:

- storage writes must be canonical-path safe, atomic, and recoverable;
- path traversal, absolute paths, encoded separators, unsafe IDs, symlinked parents, symlinked files, and symlink writes must be rejected;
- JSON writes use temp file + fsync + rename; JSONL append is locked; proposal apply is idempotent under the same lock.

### FR-029. Inspection commands

Требование: пользователь должен проверять состояние системы через CLI.

Команды:

```text
/memory short
/memory work
/memory long
/task status
/profile
/model
```

P0 scriptability:

- core P0 flows must also be possible via a minimal non-interactive path for smoke tests;
- stdout is for primary data, stderr for diagnostics;
- `--json` is required for smoke-test inspection commands.

Acceptance criteria:

- inspection commands не вызывают основной LLM chat;
- inspection commands показывают актуальное состояние storage;
- inspection commands работают после restart CLI.

Diagnostics rule:

- inspection commands must work without invoking the main chat prompt.

### FR-030. Traceability к Day 11

Требование: MVP должен закрывать Day 11.

Проверка:

- есть `short`, `work`, `long`;
- слои физически разделены;
- LLM classifier выбирает слой;
- пользователь подтверждает proposal;
- `/memory short|work|long` показывает содержимое каждого слоя;
- следующий ответ учитывает сохранённые records.

Traceability rule:

- Day 11 is mandatory and may not be bypassed.

### FR-031. Traceability к Day 12

Требование: MVP должен закрывать Day 12.

Проверка:

- есть user profile;
- profile содержит style, format, constraints;
- active profile подключается к каждому prompt;
- разные профили меняют ответы;
- пользователь не копирует профиль вручную в запрос.

Traceability rule:

- Day 12 is mandatory and may not be bypassed.

### FR-032. Traceability к Day 13

Требование: MVP должен закрывать Day 13.

Проверка:

- task state хранит `stage`;
- task state хранит `current_step`;
- task state хранит `expected_action`;
- state machine валидирует transitions;
- task-scoped prompt exposes current stage, role and expected action to the LLM;
- `/task pause` работает на любом этапе;
- `/task resume` восстанавливает контекст;
- пользователь продолжает задачу без повторного объяснения.

Traceability rule:

- Day 13 is mandatory and may not be bypassed.

## 6. CLI commands

Обязательные команды запуска:

```text
cw init
cw
cw --once --input <text>
cw --json --once --input <text>
cw --json --once --input <text> --verify "<argv command>"   # debug/recovery override only
cw --json --once --render-prompt --input <text>
cw --profile <profile_id> --model <model_id>
cw profiles [list]
assistant profiles show [id]
assistant profiles set <id>
assistant profiles create <id> [--display-name <name>] [--style k=v] [--format k=v] [--constraint <text>]
assistant memory list <short|work|long> --json
assistant memory propose --latest --json
assistant memory apply --proposal <id> --accept all --json
assistant memory proposals [--session <id>] --json
assistant task status --json
assistant task pause --json
assistant task resume --json
assistant task start <title> --json         # debug/recovery/test helper, not Day acceptance path
assistant task move <stage> --json          # debug/recovery/test helper only
assistant task step <text> --json           # debug/recovery/test helper only
assistant task expect <action> --json       # debug/recovery/test helper only
assistant process audit [--latest|--limit <n>] --json
assistant privacy
assistant privacy purge --audit [--transcripts] --yes
```

Обязательные команды внутри chat:

```text
/help
/new
/resume
/resume <session_id>
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
/process audit
/privacy
/clear short
/exit
```

Chat/session semantics:

- `cw` in an interactive terminal starts a fresh chat session by default.
- Startup may show current task/work metadata, but it must not auto-open old
  short memory, old audit history, or pending memory proposals.
- `/resume` lists old chat sessions.
- `/resume <session_id>` explicitly resumes that chat/short context and loads
  its relevant audit/proposal context.
- `/task resume` is different: it resumes a paused task/work lifecycle in the
  current chat.

Canonical P0 automation matrix:

| Flow | P0 command/protocol | Output | Calls OpenRouter |
| --- | --- | --- | --- |
| Human one-shot chat | `cw --once --input <text>` | readable sections: Assistant, Task, Transition, Evidence, Warnings, Memory proposal, Next | yes |
| JSON one-shot chat | `cw --json --once --input <text>` | JSON answer, rendered prompt id, session id | yes |
| Auto verified chat | approved plan or `cw --once --input "Проверь и заверши"`; `VerificationResolver` chooses exact approved command or strict-JSON planner command | readable evidence summary, transition summary and task state | yes |
| Explicit verification override | `cw --json --once --input <text> --verify "<argv command>"` | JSON answer, trusted evidence hash, stage/audit effects | yes |
| Render prompt | `cw --json --once --render-prompt --input <text>` | JSON rendered prompt, messages, prompt id | no |
| Render/inspect memory | `cw memory list <short|work|long> --json` | JSON records | no |
| Propose memory | `cw memory propose --latest --json` | JSON proposal with proposal id and record ids | classifier only |
| Apply memory | `cw memory apply --proposal <id> --accept all --json` | JSON apply result | no |
| Reject memory record | `cw memory apply --proposal <id> --reject <record_id> --json` | JSON apply result | no |
| Edit memory record | `cw memory apply --proposal <id> --edit <record_id>:layer=<layer>,content=<text> --json` | JSON apply result | no |
| Task lifecycle | natural `cw` TUI; `cw task status|pause|resume` for inspection/control | JSON/task text/process audit | chat calls provider; status/pause/resume do not |
| Task debug/recovery | `cw task start|move|step|expect ... --json` or matching slash commands | JSON/task text | no |
| Profile switch | `cw --profile <id> ...` or `/profile <id>` | active profile summary | no |
| Privacy summary | `assistant privacy` or `/privacy` | provider/storage disclosure | no |
| Process audit | `assistant process audit --latest --json` or `/process audit` | JSON/process event text | no |

Current command boundary: slash `/task plan` and `/task criteria` plus top-level `assistant task plan` and `assistant task criteria` are implemented conveniences for recovery/debug/tests. `/task decision`, `/task done`, `/task stage`, and top-level `assistant task decision|done|stage` are not implemented in current code. None of these commands are valid substitutes for Day 15 chat-driven acceptance.

## 6.1. Config, env, flags, and exit codes

Field-level precedence is `CLI flag > env var > config file > default`, except secrets.

| Setting | CLI flag | Env var | Config key | Default | Persisted | P0 |
| --- | --- | --- | --- | --- | --- | --- |
| API key | none | `OPENROUTER_API_KEY` | none | none | never | yes |
| Storage root | `--storage-dir` | `ASSISTANT_STORAGE_DIR` | `storage_dir` | OS user-data dir | yes | yes |
| Active model | `--model` | `ASSISTANT_MODEL` | `active_model` | none/user selected | yes | yes |
| Memory model | `--memory-model` | `ASSISTANT_MEMORY_MODEL` | `memory_model` | active model | yes | yes |
| Active profile | `--profile` | `ASSISTANT_PROFILE` | `active_profile_id` | first profile | yes | yes |
| OpenRouter base URL | `--openrouter-base-url` | `ASSISTANT_OPENROUTER_BASE_URL` | `openrouter_base_url` | OpenRouter HTTPS endpoint | yes, explicit opt-in | no |
| JSON output | `--json` | none | none | false | never | yes |
| Non-interactive chat | `chat --once` | none | none | false | never | yes |

Debug/test env vars implemented in current code:

- `ASSISTANT_PROVIDER=fake` or `ASSISTANT_FAKE_PROVIDER=1` selects `FakeProvider`.
- `ASSISTANT_PROMPT_AUDIT=1` stores prompt metadata/hash in `sessions/<session_id>/prompts.jsonl`.
- `ASSISTANT_RAW_PROMPT_AUDIT=1` stores raw rendered prompts; default JSON output returns only `rendered_prompt_id` unless `--render-prompt` is used.
- Non-default `ASSISTANT_OPENROUTER_BASE_URL` must pass HTTPS validation and explicit trust (`trusted_openrouter_base_urls` or `--trust-openrouter-base-url`).

Exit codes for non-interactive commands:

| Code | Category |
| --- | --- |
| `0` | success |
| `1` | unexpected internal error |
| `2` | CLI usage, invalid command, missing args |
| `3` | validation/invariant failure, forbidden transition, secret blocked |
| `4` | storage, IO, corruption, lock timeout |
| `5` | provider/auth/model/network/missing API key |
| `6` | classifier parse/schema/failure |

When `--json` is set, errors use this envelope:

```json
{"ok":false,"error":{"category":"validation","code":"secret_blocked","message":"...","hint":"..."}}
```

## 7. Data requirements

### 7.1. Profile JSON

Обязательные поля:

- `id`;
- `display_name`;
- `style`;
- `response_format`;
- `constraints`;
- `created_at`;
- `updated_at`.

### 7.2. MemoryRecord JSON

Обязательные поля:

- `id`;
- `layer`;
- `kind`;
- `content`;
- `source`;
- `created_at`.

Опциональные поля:

- `tags`;
- `task_id`;
- `session_id`;
- `proposal_id`.

### 7.3. MemoryProposal JSON

Обязательные поля:

- `id`;
- `source_message_ids`;
- `records`;
- `created_at`.

Поля proposal record:

- `layer`;
- `kind`;
- `content`;
- `reason`;
- `confidence`;
- `status`.

### 7.4. TaskState JSON

Обязательные поля:

- `id`;
- `title`;
- `stage`;
- `current_step`;
- `expected_action`;
- `status`;
- `objective`;
- `acceptance_criteria`;
- `plan`;
- `decisions`;
- `open_questions`;
- `updated_at`.

Опциональные поля:

- `validation_status`;
- `paused_at`;
- `resumed_at`.

## 8. State values

Task stages:

```text
planning
execution
validation
done
```

Task statuses:

```text
active
paused
```

Expected actions:

```text
user_input
llm_response
user_confirmation
none
```

Memory proposal layers:

```text
short
work
long
ignore
```

Memory storage layers:

```text
short
work
long
```

## 9. Validation rules

VR-001: `ignore` нельзя сохранять как memory layer.

VR-002: `long` нельзя сохранять без подтверждения пользователя.

VR-003: secret-like content нельзя сохранять ни в один layer.

VR-004: invalid task transition не должен менять task state.

VR-005: paused task нельзя продолжать как active без `/task resume`.

VR-006: unknown profile id не должен менять active profile.

VR-007: unknown model id не должен менять active model, если provider вернул ошибку.

VR-008: invalid classifier JSON не должен создавать memory records.

VR-009: PromptBuilder не должен включать все long-term records без выбора.

VR-010: API key не должен попадать в storage.

VR-011: task-scoped provider call without current stage policy must fail closed.

VR-012: LLM output with a different `stage` than current `TaskState.stage` must be rejected before accepted persistence.

VR-013: validation-stage output that implements fixes or adds new features must be rejected or routed back to execution.

VR-014: LLM transition proposal must not mutate task state without `TransitionGate`.

VR-015: rejected wrong-stage output must not trigger normal memory classification or accepted short-memory append.

VR-016: invariant-conflicting input must return `invariant_conflict` before provider call.

VR-017: invariant-conflicting output must return `invariant_conflict` before accepted persistence and memory classifier.

VR-018: invariant policy conflicts must be judged by out-of-band LLM structured validation in real mode; local keyword/regex checks are allowed only as hard gates, fallback, or prefilters, not final semantic decisions.

VR-019: custom invariants must be bounded by count/content/term limits before storage/rendering.

## 10. Acceptance checklist

- `assistant init` создаёт storage и профиль.
- `assistant chat` отправляет запрос через OpenRouter.
- `/model` меняет active model.
- `/profile create` создаёт профиль.
- Active profile есть в каждом prompt.
- Memory classifier возвращает proposal.
- Proposal показывается до сохранения.
- Accepted `[short]` сохраняется в short layer.
- Accepted `[work]` сохраняется в work layer.
- Accepted `[long]` сохраняется в long layer.
- `[ignore]` остаётся только в audit trail.
- `/memory short|work|long` показывает раздельные данные.
- `/task start` создаёт current task.
- `/task step` обновляет current_step.
- `/task expect` обновляет expected_action.
- `/task move` валидирует transitions.
- `/task pause` сохраняет paused state.
- `/task resume` восстанавливает контекст.
- Task-scoped prompt includes trusted stage role and allowed actions.
- Validation stage prompt gives reviewer/QA role.
- Wrong-stage LLM output is rejected before accepted persistence.
- Stage transitions are applied only by application gate, not by LLM text.
- `assistant invariants list --json` показывает default invariants.
- `assistant invariants add ... --forbid ... --json` сохраняет custom invariant.
- Prompt contains `Invariant policy`, `id="invariants.active"`, and active invariant IDs.
- Request `предложи переписать MVP на Python` is refused with `invariant_conflict`, `stack.go`, and evidence before normal chat provider call.
- JSON refusal exposes structured `violations` data.
- Output conflict test proves no memory classifier/proposal path ran.
- Restart CLI не теряет profile, memory и task state.
- Secrets не сохраняются.
- Day 11, Day 12, Day 13 и Day 14 criteria закрыты.
