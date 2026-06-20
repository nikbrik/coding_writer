# Product Vision Plan: coding_writer как AI coding assistant

## 1. Текущее позиционирование vs целевое

### Текущее позиционирование

`coding_writer` описан как консольный помощник для работы с кодом в классе Claude Code и Codex CLI. По фактической архитектуре это Go CLI на Cobra с управляемым chat loop, OpenRouter provider, локальным storage, профилями, памятью, task state machine и строгим process controller.

Сильная сторона текущего продукта: не “просто чат”, а control plane для coding-agent workflow. Приложение уже владеет состоянием задачи, памятью, invariant policy, stage policy, validation, audit, trusted evidence и безопасной материализацией файлов из structured `execution.deliverable`. Это правильный фундамент: модель не пишет в state напрямую, не решает сама, что сохранено, и не может объявить задачу завершённой без application gate.

Фактическое ограничение: продукт пока не является полноценным автономным coding assistant в смысле Claude Code/Codex CLI. В `StagePromptFactory.ToolPolicyPrompt` прямо зафиксировано P0: у LLM нет file-editing tools, shell/tool execution, git automation и общего `tool_result` flow. Исполнение строится вокруг structured JSON, fenced code blocks, guarded file materialization и allowlisted verification, а не вокруг интерактивного tool loop, где агент читает repo, применяет patch, запускает команды, наблюдает вывод и исправляет ошибки.

### Целевое позиционирование

Цель: terminal-first AI coding agent CLI для реальной работы в локальном репозитории.

Пользовательская модель:

1. Разработчик открывает репозиторий и запускает `assistant chat`.
2. Пишет задачу обычным языком.
3. Ассистент сам исследует контекст проекта через безопасные read/search tools.
4. Предлагает план и критерии.
5. После approval делает изменения через patch/edit tool, показывает diff и affected files.
6. Запускает проверки через sandboxed command runner.
7. При ошибках сам анализирует output, вносит исправления и повторяет verification.
8. Завершает задачу только после trusted evidence и self-review.
9. Оставляет понятный transcript: что изменено, какие команды запускались, что прошло, какие риски остались.

Такой продукт сохраняет текущую сильную сторону `coding_writer`: application-owned state, safety gates, evidence и memory discipline. Но добавляет недостающий слой: repository tools, patch lifecycle, command execution loop, diff UX, git/CI workflow и recovery.

## 2. Метрика сравнения с Claude Code и Codex CLI

Официальные материалы Codex CLI описывают его как coding agent, который локально работает из терминала, может читать, менять и запускать код в выбранной директории, а interactive mode даёт TUI, где Codex читает repo, делает edits, запускает команды, объясняет план и позволяет approve/reject шаги inline.

Официальный README Claude Code описывает его как agentic coding tool в терминале, который понимает codebase, выполняет routine tasks, объясняет сложный код и помогает с git workflows через natural language. Claude Agent SDK документация дополнительно фиксирует базовые возможности agent loop: read files, run commands, search web, edit code; built-in tools включают Read, Write, Edit, Bash, Glob, Grep, WebSearch/WebFetch.

Codex skills показывают ещё один важный слой зрелости: reusable workflows через `.agents/skills`, progressive disclosure, repo/user/admin/system locations и optional scripts/references/assets. В `coding_writer` repo-local `.agents/skills` уже есть как harness layer, но сам продукт `assistant` пока не использует skills как runtime capability layer для задач пользователя.

Из этого следует практическая метрика “coding AI assistant”, а не “CLI утилита”:

- Repo awareness: умеет читать файлы, искать символы, понимать структуру проекта и цитировать конкретные места.
- Tool loop: модель может запрашивать read/search/edit/shell actions, приложение выполняет их через policy и возвращает наблюдение.
- Patch workflow: изменения идут через diff/patch с preview, approval, rollback и clear ownership.
- Verification loop: команды запускаются приложением, output становится evidence, агент исправляет failures.
- Git workflow: status/diff/commit/branch/PR/CI интеграции как product flow, а не ручные shell hints.
- Interactive UX: терминальный интерфейс показывает план, actions, diffs, command output, approvals, queued follow-up и final summary.
- Context management: repo context, memory, skills, rules, compaction, session resume.
- Safety/permissions: allow/deny policies, sandbox, path safety, secret protection, explicit approvals.
- Observability: transcript, audit, tool evidence, decision log, reproducible verification.
- Extensibility: skills/plugins/hooks/MCP-like integrations without hardcoding every workflow into core.

## 3. Что уже есть в coding_writer

### Уже сильное

- Go CLI с entrypoint `cmd/assistant/main.go` и command tree в `internal/cli/root.go`.
- OpenRouter provider и fake provider для deterministic tests.
- `assistant chat` REPL, `chat --once`, JSON mode и human renderer.
- Runtime config/storage, profile manager, memory manager, proposal store.
- Три physical memory layers: `short`, `work`, `long`; `ignore` только proposal/audit.
- Task FSM: `planning`, `execution`, `validation`, `done`; `active/paused`; `expected_action`.
- Stage-aware prompt builder: base policy, security policy, process contract, stage policy, profile, invariants, task, memory, current query.
- Process controller с preflight, prompt improvement, semantic intent, parsing, validation, retry, transition gates, persistence и audit.
- Planning swarm с ролями requirements/code research/architecture/test validation/risk.
- Microtask/agent runner как internal multi-role LLM calls.
- Lifecycle gate для approval, execution readiness, trusted evidence и done.
- Safe artifact materialization из structured execution deliverable.
- Trusted verification store и allowlisted command policy.
- Invariant layer: отдельное storage, prompt visibility, semantic validator path.
- Repo-local `.agents` harness, skills, rules, learnings и validator discipline.

### Уже правильные продуктовые решения

- Пользовательский primary path задуман как один chat, а не ручное управление FSM.
- Приложение, а не модель, владеет state mutation.
- Verification evidence привязано к task/session и хранится отдельно.
- Secret scanning и safe path checks встроены в critical paths.
- README/docs уже честно говорят, что P0 является control-plane срезом, а не финальной agentic системой.

## 4. Ключевые gaps

### Gap 1. Нет общего tool runtime

Сейчас LLM не может запросить “прочитай файл”, “найди usage”, “примени patch”, “запусти command” как typed tool call. Вместо этого она возвращает structured output, а приложение частично извлекает fenced file blocks или verification command из task state.

Последствие: агент не видит реальное состояние repo во время работы, не может итеративно исследовать код, не может сам исправить test failure, а execution превращается в “сгенерируй артефакт в ответе”.

### Gap 2. Repo context слабый и не first-class

Prompt builder подключает profile/task/memory/invariants, но нет системного repo map: git root, file tree, language/package metadata, dependency graph, recent diffs, relevant files, symbol search. Есть правило “prefer ast-index”, но сам продукт не имеет встроенного code search/read layer.

Последствие: code_research_specialist может советовать likely files, но не имеет реальных read/search observations.

### Gap 3. Patch/diff workflow не продуктовый

Материализация файлов из headings + fenced code blocks полезна как P0, но это не equivalent Codex/Claude editing workflow. Нет unified patch model, preview before apply, partial accept/reject, backup/rollback, conflict handling, dirty-worktree awareness, ownership of edits.

Последствие: пользователь не получает нормальную engineering петлю “посмотри diff -> approve -> apply -> verify”.

### Gap 4. Verification ограничен allowlist command resolver

Allowlist и trusted evidence правильны, но verification пока не является полноценным iterative repair loop. Агент не получает structured command output как observation для следующего edit step; нет retry budget по failures; нет classification “product bug vs environment issue vs flaky test”; нет plan update after failure.

### Gap 5. Git/CI workflow почти отсутствует

Есть allowlisted `git diff/status`, но нет product flows: explain diff, stage/commit, generate commit message, branch/PR, inspect CI failures, apply review comments, release notes. Для coding assistant это core workflow.

### Gap 6. UX пока больше line REPL, чем coding TUI

Текущий REPL печатает секции и ANSI styling, но нет полноценной composer/history/diff panes/progress/action timeline/approval prompts/tool log. Для короткого MVP нормально, но целевой класс требует более богатого interactive loop.

### Gap 7. Skills существуют в harness, но не в assistant runtime

`.agents/skills` используется агентами-разработчиками repo, а не самим продуктом `assistant`. Нет capability discovery: “для задачи code review загрузи skill X”, “для docs generation используй skill Y”, “запусти skill script”.

### Gap 8. Multi-agent роли пока LLM-only, не tool-owning

Planning swarm и AgentRunner вызывают role-specific prompts, но роли не имеют собственных tools, observations, workspace slices или measurable outputs кроме JSON. Это полезно для review planning, но недостаточно для autonomous coding tasks.

### Gap 9. State model ещё task-centric, не run/turn/tool-centric

`TaskState` сильный, но нет отдельной сущности `AgentRun`/`Turn`/`ToolCall`/`PatchSet`/`CommandRun` как first-class records. Audit есть, trusted evidence есть, но продукту нужен единый run ledger.

### Gap 10. Messaging всё ещё обещает “в классе Claude Code/Codex CLI” без достаточно явного уровня зрелости

README хорошо объясняет архитектуру, но headline может создавать ожидание полного аналога уже сейчас. Нужно явно разделить: current = controlled coding-agent foundation; target = repo-editing autonomous coding assistant.

## 5. Vision: coding_writer как coding AI assistant

### Product north star

`coding_writer` — локальный terminal-first coding agent, который работает в репозитории через безопасный application-owned agent loop: исследует код, планирует, применяет patches, запускает проверки, исправляет failures, сохраняет evidence и ведёт пользователя до merged-ready результата.

### Основной принцип

Не копировать Claude Code/Codex CLI поверхностно. Сохранить отличие `coding_writer`: строгий deterministic control plane, typed lifecycle, memory proposals, invariant enforcement, auditability. Добавить к этому tools и UX, которые делают продукт реально полезным для coding tasks.

### Целевая архитектура

```text
User chat
  -> Intent/router
  -> Task/run state
  -> Context planner
  -> Tool-capable agent loop
       -> read/search tools
       -> patch tools
       -> shell/test tools
       -> git tools
       -> skill tools
       -> observations
       -> self-review
  -> Lifecycle/evidence gates
  -> Human transcript + diff + verification summary
  -> Memory proposal + durable learnings
```

### Главные продуктовые promises

- “Открой repo, напиши задачу, получи verified diff”.
- “Каждое изменение видно в diff и может быть отклонено”.
- “Каждая проверка имеет command, exit code, bounded output и evidence ID”.
- “Ассистент не завершает задачу без self-review и trusted verification”.
- “Память и правила сохраняются только через явные безопасные flows”.

## 6. Дорожная карта

### Near-term: 1-3 недели

Цель: превратить P0 control plane в минимальный repo-aware agent loop без потери safety.

#### 1. Ввести AgentRun ledger

Добавить first-class записи:

- `AgentRun`: id, task_id, session_id, objective, status, started_at, ended_at.
- `AgentTurn`: user_input, selected_action, stage, model, result.
- `ToolCall`: id, tool_name, args_hash/redacted_args, status, started_at, ended_at.
- `ToolObservation`: exit/status, bounded output, affected files, evidence refs.
- `PatchSet`: proposed files, diff, apply status, rollback metadata.

Хранить в `<storage_root>/runs/<run_id>/...` и писать summary в existing audit. Это не заменяет `TaskState`; это operational ledger вокруг него.

Acceptance:

- `assistant process audit` показывает run/turn/tool timeline.
- Каждый applied artifact и verification command связан с run_id.
- Existing Day 11-15 behavior не ломается.

#### 2. Добавить read/search tools

Минимальный набор:

- `workspace.list_files`
- `workspace.read_file`
- `workspace.search_text`
- `workspace.project_map`
- `workspace.git_status`
- `workspace.git_diff`

Policy:

- read-only tools разрешены после preflight.
- path safety через existing `storage.SafeJoin`-подобные правила для workspace.
- output caps и binary file rejection.
- secret redaction before provider-visible observation.

Prefer:

- `ast-index` как backend для symbol/project map, если index доступен.
- `rg` fallback для plain text.

Acceptance:

- На задачу “объясни architecture” ассистент реально читает README/docs/internal files, а transcript показывает read/search observations.
- Code research specialist получает не “likely files”, а actual file evidence.

#### 3. Заменить deliverable materialization на PatchSet v1

Поддержать:

- unified diff parsing;
- full-file replacement как compatibility path;
- preview before apply;
- safe path validation;
- dirty file check: если target changed after read, block/apply with conflict;
- rollback file backup для applied patch.

UI:

- секция `Diff`;
- секция `Files changed`;
- approval prompt: apply/reject;
- `--yes` только для non-interactive trusted flows.

Acceptance:

- Модель предлагает patch.
- Приложение показывает diff.
- После approval patch применяется.
- `git diff` подтверждает изменения.

#### 4. Tool-capable execution loop

Добавить новый execution mode:

```text
plan approved
-> agent requests read/search
-> agent proposes patch
-> app previews/applies patch after approval policy
-> app runs verification
-> agent observes failure/success
-> agent fixes or moves validation
```

Важно: LLM не вызывает shell напрямую. Она возвращает typed `tool_request` JSON; приложение решает allow/deny/ask.

Acceptance:

- Простая Go task выполняется без fenced file block headings.
- Агент читает relevant files перед edit.
- При failing test делает хотя бы один repair iteration.

#### 5. Обновить README messaging

Сделать headline честнее:

- Current: “controlled foundation for terminal coding agent”.
- Target: “roadmap to Claude Code/Codex CLI class”.
- Добавить maturity table: P0 implemented, P1 planned, P2 planned.

Не удалять существующие diagrams; добавить product maturity section.

### Mid-term: 1-2 месяца

Цель: сделать assistant полезным для ежедневных repo tasks.

#### 1. Sandboxed command runner

Расширить trusted verification в общий command tool:

- exact argv only;
- per-command policy;
- cwd inside repo;
- timeout/output caps;
- env allowlist;
- network policy;
- approval levels: read-only, test, write/build, destructive;
- background process monitor для dev servers.

Команды:

- tests/build/lint;
- `git status/diff/log`;
- package manager read commands;
- no arbitrary shell by default.

Acceptance:

- Агент может запускать approved test command, читать failure output и исправлять код.
- Destructive commands требуют explicit user approval.

#### 2. Git workflow

Product flows:

- summarize current diff;
- split changes by intent;
- generate commit message;
- stage selected files;
- create commit after approval;
- branch status;
- PR summary template;
- CI failure inspection через optional GitHub integration later.

Acceptance:

- После task done user может попросить “подготовь commit”, ассистент показывает diff summary и commit message, но не коммитит без approval.

#### 3. Context planner

Перед execution запускать context planning step:

- определить языки/frameworks;
- выбрать relevant files;
- выбрать search strategy;
- оценить blast radius;
- сформировать context pack для model.

Использовать:

- `go.mod`, package structure, docs, README;
- ast-index symbols;
- recent git diff;
- task memory.

Acceptance:

- Для задачи в конкретном package ассистент читает package files, tests, docs before edit.
- Context pack bounded и audit-visible.

#### 4. Skills runtime

Сделать `.agents/skills` first-class для `assistant`:

- skill discovery;
- trigger by description;
- load `SKILL.md` progressively;
- optional scripts with approval/policy;
- skill-specific context budget.

Initial product skills:

- code review;
- bug fix;
- test repair;
- docs update;
- PR summary;
- harness evolution.

Acceptance:

- Задача “review diff” активирует repo-local review skill.
- Skill instructions видны в run ledger.

#### 5. TUI v1

Без чрезмерной сложности, но лучше line REPL:

- persistent composer;
- action timeline;
- collapsible tool output;
- diff viewer;
- inline approve/reject;
- queued follow-up;
- session resume.

Вероятный стек: Bubble Tea/Bubbles/Lip Gloss, если Go остаётся основным runtime.

Acceptance:

- Пользователь видит running tool, pending approval, diff и final evidence без чтения raw logs.

### Long-term: 3-6 месяцев

Цель: конкурентный coding assistant с собственным differentiation.

#### 1. Multi-agent tool ownership

Роли должны иметь measurable duties:

- researcher: read/search only, emits cited context pack;
- planner: plan + criteria;
- implementer: patch proposal;
- reviewer: diff review + risk findings;
- verifier: command plan + evidence evaluation;
- finalizer: summary + follow-ups.

Каждая роль получает bounded tools и пишет structured artifacts в run ledger.

#### 2. Advanced repository intelligence

- incremental symbol index;
- dependency graph;
- test impact analysis;
- ownership/conventions extraction;
- style guide memory;
- architecture map;
- generated context citations.

#### 3. CI/CD and GitHub integration

- inspect PR checks;
- fetch failing logs;
- address review comments;
- create draft PR;
- update PR description;
- link local evidence to remote CI evidence.

#### 4. Team memory and policy

- project rules with provenance;
- shared skills;
- approved command policies;
- org-level profiles;
- memory review/garbage collection;
- privacy modes.

#### 5. Evaluation harness

- repo task benchmark;
- patch correctness eval;
- command safety eval;
- hallucinated evidence detector;
- regression suite for “agentic behavior”, not only unit tests.

## 7. Конкретные шаги по направлениям

### Direction A. Agent loop

1. Define `ToolRequest` schema: `name`, `args`, `reason`, `risk`, `expected_observation`.
2. Define `ToolResult` schema: `status`, `summary`, `observation_ref`, `evidence_refs`.
3. Add process stage `tool_wait` only if needed; preferable first keep stage stable and store tool state in `AgentRun`.
4. Extend `StagePromptFactory.ToolPolicyPrompt`: replace P0 “No tools” with per-stage allowed tools.
5. Add `ToolExecutor` interface and registry.
6. Add read-only tools first.
7. Add patch tool second.
8. Add command tool third.
9. Add tests for denied unsafe path, denied shell syntax, redacted secret observation.

### Direction B. Repository context

1. Add workspace root resolver with git root detection.
2. Add file inventory with `.gitignore` respect.
3. Add `ast-index stats`/project map integration as optional provider.
4. Add `rg` fallback for text search.
5. Add context budgeter: max files, max bytes, max output per observation.
6. Add citations in assistant output: file path + line where available.
7. Add “read before edit” gate: patch to existing file requires prior read observation for same file version/hash.

### Direction C. Patch safety

1. Introduce `PatchSet` storage.
2. Require target file hash before apply.
3. Support apply preview.
4. Use atomic writes.
5. Block absolute paths, parent traversal, symlinks and binary files.
6. Show diff in human output.
7. Add rollback command: `assistant patch rollback <patch_id>`.
8. Add non-interactive `--approve-patches` only for trusted scripted demos.

### Direction D. Verification and repair

1. Promote `runTrustedVerification` into command runner service.
2. Store full bounded command observation, not only evidence token.
3. Feed failed command output back to agent as observation.
4. Add repair loop budget: max 3 edit/verify cycles by default.
5. Classify failure: test failure, build failure, command policy denied, environment failure, flaky/timeout.
6. Require final self-review after last successful verification.
7. Mark task blocked if command cannot run safely or environment dependency is missing.

### Direction E. UX

1. Keep current plain REPL as stable fallback.
2. Add richer interactive mode behind `assistant chat --tui`.
3. Render action timeline:
   - planning;
   - read/search;
   - proposed patch;
   - applied patch;
   - command run;
   - repair;
   - validation;
   - done.
4. Add inline approval prompts for plan, patch, command and commit.
5. Add `/diff`, `/evidence`, `/runs`, `/resume`, `/abort`.
6. Keep `--json` automation stable.

### Direction F. Docs and messaging

1. Update README first paragraph:
   - current: controlled stateful assistant foundation;
   - target: terminal coding agent comparable in workflow to Claude Code/Codex CLI.
2. Add maturity table:
   - P0: state/memory/profile/invariants/lifecycle/trusted verification/file materialization;
   - P1: read/search/patch/command tools;
   - P2: git/CI/TUI/skills/runtime evals.
3. Move Day 15 from “proof of full assistant” to “proof of controlled lifecycle foundation”.
4. Add “What coding_writer does not do yet” section.
5. Add `docs/roadmap.md` or make this file canonical roadmap.

## 8. Как изменить описание продукта

### Новый короткий tagline

`coding_writer` — terminal-first coding agent foundation: строгий control plane для задач в репозитории, который развивается в полноценный AI coding assistant с repo tools, patches, verification и git workflows.

### Новый README opening

```text
Coding Writer — локальный CLI-помощник для coding-agent workflow.

Сегодня это контролируемый foundation: chat loop, task lifecycle, memory,
profiles, invariants, safe file materialization, trusted verification and audit.
Цель продукта — полноценный terminal-first AI coding assistant в классе
Claude Code / Codex CLI: repo-aware tool loop, patches, command execution,
diff review, git/CI integration and recovery.
```

### Maturity table для README

| Capability | Current | Target |
| --- | --- | --- |
| Chat REPL | есть | richer TUI + plain fallback |
| Task lifecycle | есть | run ledger + tool loop |
| Memory/profile | есть | team/project memory governance |
| Invariants | есть | policy packs + org rules |
| Repo read/search | нет как product tool | read/search/symbol tools |
| File edits | fenced deliverable materialization | patch/diff workflow |
| Command execution | trusted verification allowlist | sandboxed command runner |
| Repair loop | ограниченно | iterative edit/test/fix |
| Git workflow | минимально | status/diff/commit/PR/CI |
| Skills | repo harness only | assistant runtime skills |

### Что перестать обещать без уточнения

- Не писать “аналог Claude Code/Codex CLI” без maturity qualifier.
- Не называть current P0 “полноценным автономным coding assistant”.
- Не описывать fenced code materialization как equivalent file editing tools.

### Что обещать уверенно

- Strong control plane.
- Safe local state.
- Explicit lifecycle.
- Memory with confirmation.
- Stage-aware prompts.
- Trusted evidence.
- Roadmap to repo-tool agent.

## 9. Приоритеты реализации

### P0.5: обязательный ближайший срез

1. AgentRun ledger.
2. Read/search tools.
3. PatchSet preview/apply.
4. Failed verification observation.
5. README maturity messaging.

Это минимальный набор, после которого продукт можно честно показывать как ранний coding assistant, а не только process-controlled chat.

### P1: ежедневная полезность

1. Sandboxed command runner.
2. Iterative repair loop.
3. Git diff/status/commit workflow.
4. Context planner.
5. Skills runtime.

### P2: конкурентность

1. TUI.
2. CI/GitHub integrations.
3. Multi-agent tool ownership.
4. Eval harness.
5. Team policy/memory.

## 10. Главные инженерные риски

### Риск: tool layer сломает текущую safety модель

Mitigation: не давать LLM прямой shell. Только typed tool requests, application policy, audit, evidence, bounded output, explicit approvals.

### Риск: hardcoded language heuristics вернутся

Mitigation: сохранять правило из текущих learnings: verification command selection не должен угадывать язык по path. Использовать exact approved command или structured planner + policy.

### Риск: context explosion

Mitigation: context planner, file budgets, progressive disclosure skills, ast-index summaries, citations instead of dumping files.

### Риск: UX станет debug console

Mitigation: every feature must have chat-first product path. Slash/top-level commands остаются inspect/recovery, не primary workflow.

### Риск: multi-agent усложнит продукт без пользы

Mitigation: каждая роль получает measurable artifact and tool bounds. Если роль не производит уникальный artifact, объединить её.

## 11. Итоговое направление

`coding_writer` не нужно переписывать с нуля. Самое ценное уже есть: строгая application-owned process architecture. Нужно перестать развивать его как всё более умный structured-chat controller и сделать следующий слой: typed tools, repo observations, patch workflow, command runner, repair loop и git UX.

Ключевая формула:

```text
Current value: controlled lifecycle + memory + validation.
Missing value: real repository tool loop.
Product path: keep control plane, add tools.
```

## 12. Использованные внешние ориентиры

- Codex CLI docs: https://developers.openai.com/codex/cli
- Codex CLI features: https://developers.openai.com/codex/cli/features
- Codex skills docs: https://developers.openai.com/codex/skills
- Claude Code GitHub README: https://github.com/anthropics/claude-code
- Claude Agent SDK overview: https://code.claude.com/docs/en/agent-sdk/overview
