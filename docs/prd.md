# PRD: TUI-first видение продукта

## 1. Контекст

`coding_writer` развивается в terminal-first AI coding assistant для реальной работы в локальном репозитории.

Этот документ описывает будущее развитие продукта, а не текущее состояние реализации. Существующий контур управления считается фундаментом: chat loop, профили, память, task lifecycle, stage-aware prompts, invariants, audit, safe artifact materialization и trusted verification уже задают правильную архитектурную дисциплину. Следующий продуктовый шаг — сделать этот фундамент видимым и удобным через TUI.

Главный сдвиг: `cw` / `codingwriter` должен восприниматься не как строчный REPL и не как debug-консоль, а как полноценная рабочая среда в терминале. Пользователь видит задачу, план, diff, подтверждения, запущенные инструменты, доказательства проверки, предупреждения, предложения памяти и финальную сводку в одном управляемом интерфейсе.

## 2. Целевой образ продукта

`coding_writer` — локальный TUI-first coding agent, который работает в репозитории через безопасный сценарий под управлением приложения: помогает понять задачу, ведёт план, показывает изменения, запускает проверки по разрешению, сохраняет evidence и доводит пользователя до проверенного результата.

Ключевое обещание:

```text
Открой репозиторий, запусти cw, работай с задачей в TUI.
Все действия видны, подтверждаемы и воспроизводимы.
```

Продукт не должен развиваться как набор отдельных CLI-команд вокруг LLM. Команды, JSON-режимы, scripts и recovery helpers остаются полезными, но пользовательский happy path должен жить в интерактивном TUI.

## 3. Приоритеты развития

### P0: TUI-first пользовательский опыт

TUI — главный ближайший приоритет и основной критерий развития продукта.

Цель P0: сделать `cw` / `codingwriter` рабочей терминальной средой, где пользователь может вести coding task без чтения raw JSON, ручного управления FSM и переключения между несвязанными командами.

TUI должен включать:

- постоянное поле ввода;
- историю диалога и ленту событий задачи;
- компактную панель текущего состояния: `stage`, `current_step`, `expected_action`, `status`;
- видимые план и acceptance criteria;
- встроенные подтверждения для плана, patch, verification command и других рискованных действий;
- diff viewer для proposed/applied changes;
- сворачиваемый вывод инструментов и ограниченные логи команд;
- панель evidence: command, exit code, summary, evidence id;
- warnings/errors с code, message и hint;
- просмотр предложений памяти;
- session resume;
- обычный REPL как запасной режим для простых терминалов и автоматизации.

TUI не должен быть декоративной оболочкой. Он должен управлять продуктовым сценарием: показывать, что агент собирается сделать, что уже сделал, какие решения требуют подтверждения и почему задача ещё не завершена.

### P1+: всё остальное по требованию

Все остальные направления добавляются по требованию: только когда конкретный пользовательский сценарий требует их для TUI-first рабочего процесса.

Направления по требованию:

- AgentRun ledger;
- read/search tools;
- PatchSet storage and preview/apply;
- failed verification observation;
- sandboxed command runner;
- iterative repair loop;
- git workflow;
- context planner;
- skills runtime;
- multi-agent tool ownership;
- CI/GitHub integration;
- team memory and policy;
- evaluation harness.

Важно: пункты из `PRODUCT-VISION-PLAN.md`, которые раньше выглядели как P0.5, не являются ближайшим обязательным срезом. AgentRun ledger, read/search tools, PatchSet и verification observation должны появляться позже, когда TUI-сценарий потребует их как часть понятного пользовательского опыта.

## 4. Целевая пользовательская модель

Основной сценарий:

1. Разработчик открывает репозиторий.
2. Запускает `cw`.
3. Видит TUI с текущей сессией, активным профилем, моделью и состоянием задачи.
4. Формулирует цель обычным языком.
5. Ассистент уточняет задачу, предлагает план и acceptance criteria.
6. Пользователь подтверждает, отклоняет или просит изменить план прямо в TUI.
7. Ассистент предлагает следующие действия, показывает diff или запрашивает разрешение на tool/verification step, если такой слой включён.
8. Пользователь видит прогресс, evidence, предупреждения и финальную сводку в TUI.
9. Завершение возможно только после validation под управлением приложения, trusted evidence и self-review.

Пользователь не обязан:

- читать raw JSON;
- вручную дёргать `/task move`, `/task step`, `/task expect`;
- редактировать storage;
- знать точную verification command;
- переключаться в отдельные debug-команды, чтобы понять состояние работы.

## 5. Фундамент из текущего PRD

Текущее сильное ядро остаётся фундаментом для будущего TUI-first продукта.

### Контур управления под управлением приложения

Приложение, а не модель, владеет:

- task state;
- transitions;
- memory writes;
- invariant checks;
- provider/tool permissions;
- verification gates;
- persistence;
- audit.

LLM может предлагать план, изменения, выводы и next signals, но не мутирует authoritative state напрямую.

### Слои памяти

Физические слои памяти сохраняются:

- `short`: текущий диалог;
- `work`: текущая задача;
- `long`: профиль, решения, знания;
- `ignore`: только proposal/audit слой, без физического durable storage.

Memory writes должны оставаться явными: LLM memory-classification step предлагает записи, приложение проверяет safety, пользователь подтверждает.

### Профили

Профиль остаётся обязательным блоком prompt context. Разные профили должны менять стиль, формат и ограничения ответа без ручного копирования пользователем.

### Task lifecycle

Task state остаётся finite state machine:

- `planning`;
- `execution`;
- `validation`;
- `done`.

Состояние включает `current_step`, `expected_action`, `status`, plan, criteria, validation state and evidence refs. Pause/resume должны работать как часть основного UX, а не только как debug helper.

### Инварианты

Active invariants остаются hard gate:

- хранятся отдельно от диалога и memory;
- подключаются в prompt как higher-priority policy/data block;
- конфликт user input блокируется до normal provider call;
- конфликт provider output блокируется до memory persistence;
- semantic conflict решает structured validator, а не keyword fallback.

### Доверенная проверка

Verification остаётся под управлением приложения:

- exact command из approved plan/criteria или structured verification planner/referee;
- локальная policy проверяет argv, cwd, allowlist, path safety, timeout и output caps;
- результат сохраняется как trusted evidence;
- `done` невозможен без accepted validation и criteria-matched evidence, если критерии требуют проверки.

## 6. Контракт TUI

TUI должен иметь стабильные области.

### Поле ввода

Ввод пользователя всегда доступен. Пользователь пишет обычным языком, а не внутренними командами state machine. Slash commands могут существовать как shortcuts, но не должны быть обязательным сценарием.

### Лента событий

Лента событий показывает:

- user goal;
- prompt improvement;
- planning;
- approval request;
- execution action;
- proposed change;
- verification;
- repair;
- validation;
- done/blocker.

Каждое событие должно иметь краткую сводку, понятную пользователю. Сырые детали доступны через раскрытие события.

### Панель состояния

Панель состояния показывает:

- active model;
- profile;
- task id;
- stage;
- current step;
- expected action;
- status;
- latest evidence;
- pending approval.

### Панель плана и критериев

План и acceptance criteria видны как управляемые списки. Пользователь может подтвердить, отклонить или попросить изменить их без ручного редактирования JSON.

### Панель diff

Когда появится patch layer, diff panel станет главным способом проверки изменений. До появления PatchSet TUI всё равно должен иметь место для будущего diff workflow и показывать текущие materialized files в понятном виде.

### Панель доказательств

Панель доказательств показывает проверку как продуктовый результат:

```text
command: go test ./...
exit: 0
summary: passed
evidence: ev_...
```

Полные ограниченные логи раскрываются по запросу.

### Панель предложений памяти

Memory proposal показывается после значимых шагов:

- layer;
- kind;
- content;
- reason;
- accept/reject/edit action.

## 7. Дорожная карта по требованию

### 7.1. AgentRun ledger

Добавлять, когда TUI начнёт показывать run/turn/tool timeline и потребуется durable backend для resume, audit и навигации по evidence.

Цель:

- `AgentRun`;
- `AgentTurn`;
- `ToolCall`;
- `ToolObservation`;
- `PatchSet`;
- command/evidence refs.

Не делать ledger ради внутренней полноты. Делать как storage model для TUI timeline и воспроизводимого просмотра сессии.

### 7.2. Read/search tools

Добавлять, когда пользовательский TUI workflow требует реального repo context: объяснить архитектуру, найти место изменения, проверить usage, собрать context pack.

Минимальные tools:

- `workspace.list_files`;
- `workspace.read_file`;
- `workspace.search_text`;
- `workspace.project_map`;
- `workspace.git_status`;
- `workspace.git_diff`.

Policy:

- path safety;
- output caps;
- binary rejection;
- secret redaction;
- audit-visible observations;
- `ast-index` preferred, `rg` fallback для plain text.

### 7.3. PatchSet и diff workflow

Добавлять, когда TUI diff panel становится основным сценарием редактирования.

Требования:

- unified diff parsing;
- preview before apply;
- approval before apply;
- dirty file check;
- rollback metadata;
- safe path validation;
- affected files summary;
- conflict handling.

Fenced file materialization остаётся compatibility path, но целевой UX — patch/diff workflow.

### 7.4. Verification observation and repair

Добавлять, когда TUI должен показывать failed command output и вести repair loop.

Требования:

- command output как bounded observation;
- failure classification;
- retry budget;
- repair iteration;
- final self-review;
- blocker state для unsafe command или missing environment.

Не возвращаться к language/path heuristics для выбора команд. Verification command selection остаётся exact approved command или structured planner/referee.

### 7.5. Sandboxed command runner

Добавлять, когда trusted verification станет недостаточно для рабочих задач.

Требования:

- exact argv only;
- cwd inside repo;
- env allowlist;
- timeout/output caps;
- approval levels;
- destructive command block by default;
- background process monitor for dev servers.

### 7.6. Git workflow

Добавлять, когда TUI сможет безопасно показывать diff and evidence.

Сценарии:

- summarize diff;
- split changes by intent;
- generate commit message;
- stage selected files after approval;
- commit after approval;
- PR summary;
- CI inspection later.

### 7.7. Skills runtime

Добавлять, когда product flow потребует reusable workflows внутри assistant, а не только в repo harness.

Требования:

- discover `.agents/skills`;
- load `SKILL.md` progressively;
- expose loaded skill in TUI timeline;
- run optional scripts only through policy and approval;
- keep context budget bounded.

### 7.8. Multi-agent tool ownership

Добавлять только когда роли дают measurable artifacts.

Целевые роли:

- researcher: cited context pack;
- planner: plan and criteria;
- implementer: patch proposal;
- reviewer: findings;
- verifier: command plan and evidence evaluation;
- finalizer: summary and follow-ups.

Если роль не производит уникальный artifact, её не нужно выделять.

## 8. Модель команд CLI и TUI

Основной запуск:

```text
cw
cw --tui
cw --plain
cw --once --input <text>
cw --json --once --input <text>
```

Целевое поведение по умолчанию: интерактивный TUI, если терминал поддерживает его. Обычный REPL остаётся запасным режимом.

Стабильные inspect/recovery команды:

```text
assistant profiles list|show|set|create
assistant memory list|propose|apply|proposals
assistant task status|pause|resume
assistant process audit
assistant privacy
```

Debug helpers допустимы, но не являются основным сценарием:

```text
assistant task start
assistant task move
assistant task step
assistant task expect
cw --json --once --verify "<argv command>" --input "<text>"
```

В TUI slash commands могут быть shortcuts:

```text
/model
/profile
/task status
/task pause
/task resume
/memory
/diff
/evidence
/runs
/privacy
/exit
```

Но пользовательский сценарий должен работать без знания внутренних state-machine команд.

## 9. Provider и privacy

OpenRouter остаётся provider path:

- `OPENROUTER_API_KEY` из environment;
- key не сохраняется в repo, profiles, memory, audit or prompt logs;
- selected model хранится локально;
- memory classifier может использовать основную или отдельную configured модель;
- перед provider call выполняется secret scanner;
- raw prompt audit выключен по умолчанию;
- purge commands доступны для audit/transcripts.

TUI должен явно показывать provider/model и privacy summary до первого внешнего call в новой сессии.

## 10. Контракт prompt и context

Prompt builder остаётся layered:

1. system role;
2. security rules;
3. trusted process-control policy;
4. trusted stage-specific policy;
5. profile;
6. invariants;
7. task state;
8. working memory;
9. relevant long-term memory;
10. short-term history;
11. current user request.

Untrusted blocks должны быть serialized/tagged as data. Stage and process policy outrank profile, memory, task text, short history and user text when conflicts occur.

TUI не должен менять этот контракт. Он только делает state, approvals и evidence видимыми.

## 11. Критерии соответствия этому направлению

Будущее развитие считается aligned, если:

- TUI стоит первым приоритетом в roadmap и продуктовых решениях;
- line REPL and JSON remain fallback/automation modes;
- internal commands не становятся обязательным пользовательским workflow;
- текущий memory/profile/task/invariant/verification foundation сохранён;
- AgentRun/read/search/PatchSet/command/git/skills явно помечены как работа по требованию;
- feature не добавляется только потому, что это “как у Claude Code/Codex CLI”;
- каждая новая возможность улучшает TUI-first сценарий работы над задачей в репозитории.

## 12. Не цели ближайшего среза

Не делать сейчас:

- обязательный AgentRun ledger before TUI;
- read/search tools как отдельный backend project без TUI use case;
- PatchSet rewrite до появления нормального diff UX;
- general shell access;
- git commit/PR automation before diff/approval UX;
- CI/GitHub integration;
- team/org policy system;
- eval harness;
- новый runtime или переписывание Go core.

## 13. Продуктовые риски

### Риск: TUI станет просто красивым log viewer

Снижение риска: approvals, state, plan, diff, evidence и memory review должны быть actionable внутри TUI.

### Риск: продукт снова уйдёт в debug CLI

Снижение риска: команды, которые раскрывают internals, должны оставаться inspect/recovery. Primary task flow остаётся natural chat внутри TUI.

### Риск: tool layer появится без safety model

Снижение риска: LLM не получает direct shell. Только typed requests, application policy, bounded observations, approvals and audit.

### Риск: roadmap начнёт копировать конкурентов поверхностно

Снижение риска: сохранить отличие продукта: deterministic control plane, explicit lifecycle, memory confirmation, invariant gates, trusted evidence.

### Риск: слишком ранний backend rewrite

Снижение риска: добавлять backend primitives только когда они нужны TUI-сценарию. Сначала пользовательский опыт, затем durable model.

## 14. Итоговое направление

`coding_writer` не нужно переписывать с нуля. Его главная сила — строгий контур управления под управлением приложения. Но следующий продуктовый шаг не AgentRun ledger и не patch backend сам по себе. Следующий шаг — TUI-first рабочий опыт, где этот контур управления становится видимым, управляемым и полезным для разработчика.

Формула развития:

```text
Текущий фундамент: lifecycle + memory + profile + invariants + verification.
Следующий приоритет: TUI-first workflow.
Всё остальное: возможности по требованию вокруг этого workflow.
```
