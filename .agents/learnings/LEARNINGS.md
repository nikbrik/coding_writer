# Project Learnings

> База знаний проекта. Пополняется через `/evolve` после значимых задач.
> Правило: добавляй новые записи append-only. Единственное допустимое изменение
> существующей записи — обновить `Recurrence-Count` и добавить краткую evidence
> строку для того же `pattern-key`.

<!-- LEARNINGS:START -->
## 2026-06-19 | day15-live-demo-model
**Тип**: correction
**Модуль**: manual testing
**Приоритет**: HIGH
**Recurrence-Count**: 1

### Суть
Live пользовательские demo/scenario прогоны Day 15 должны идти через OpenRouter model `google/gemini-3.1-flash-lite`, как указано в `docs/manual-testing-demo.md`. Fake provider допустим только для deterministic CI/smoke, не как доказательство live user scenario.

### Когда важно
Когда запускается ручной пользовательский сценарий, live demo, запись видео или проверка OpenRouter dashboard calls.

### Применение
Перед live-прогоном выполнить `export ASSISTANT_MODEL="google/gemini-3.1-flash-lite"`, `unset ASSISTANT_PROVIDER`, `unset ASSISTANT_LLM_VALIDATION`; не подменять модель на `openai/gpt-4.1-mini`, DeepSeek или `fake/model`.

### Evidence
- 2026-06-19 пользователь указал, что live scenario должен тестироваться через Gemini 3.1 Flash Lite; попытка запустить `openai/gpt-4.1-mini` была остановлена.

---

## 2026-06-19 | real-mode-semantic-validation-only
**Тип**: correction
**Модуль**: process validation
**Приоритет**: HIGH
**Recurrence-Count**: 2

### Суть
В real/OpenRouter mode semantic decisions должны идти через LLM structured validator; keyword/regex heuristics допустимы только для hard safety или prefilter, не как fallback decision по смыслу.

### Когда важно
Когда меняется process controller, transition routing, execution validation или manual acceptance для real CLI.

### Применение
Не добавлять semantic validators вида `containsAny`/regex для product real-mode. Для смысловых проверок использовать out-of-band LLM referee со strict structured JSON output (`verdict`/`findings`), а локально оставлять schema/safety gates без trigger-word fallback для intent/readiness/acceptance.

### Evidence
- 2026-06-19 пользователь указал, что `autoVerificationIntent` с `containsAnyText` нарушает запрет keyword-based semantic validation; auto verification intent должен идти через semantic referee.

---

## 2026-06-19 | manual-suite-rerun-failed-only
**Тип**: pattern
**Модуль**: manual testing
**Приоритет**: HIGH
**Recurrence-Count**: 1

### Суть
После полного live manual suite перезапускать только failed/ambiguous cases; отделять product bug от checker/model-language variance.

### Когда важно
Когда manual suite использует live OpenRouter models и стоит дорого/долго.

### Применение
Сохранять evidence в run-scoped dirs, читать логи упавших кейсов, чинить реальные bugs, а затем rerun только failed/ambiguous cases. Если checker ищет конкретный язык/формулировку, сверять фактическое состояние storage/audit.

---
## 2026-06-19 | goal-x-refresh-stale-goal
**Тип**: correction
**Модуль**: goal-x
**Приоритет**: HIGH
**Recurrence-Count**: 1

### Суть
При `goal-x` existing `goal.md` может относиться к прошлой задаче; если контекст не совпадает с текущим user request, goal нужно заменить или обновить перед implementation.

### Когда важно
Когда пользователь вызывает `goal-x`, а `goal.md` уже существует в repo.

### Применение
Сначала прочитать `goal.md`; если он stale, записать текущие context, acceptance criteria, constraints, blast radius и log для новой задачи.

### Evidence
- 2026-06-19 текущий `goal.md` был про CLI/manual-suite, а задача была про harness evolution.

---

## 2026-06-19 | harness-validator-frontmatter-edge-cases
**Тип**: pattern
**Модуль**: harness validator
**Приоритет**: MEDIUM
**Recurrence-Count**: 1

### Суть
Harness validator должен возвращать понятные errors на malformed frontmatter и ловить multiline/continuation `description`.

### Когда важно
Когда меняется validator или добавляются новые `.kilo/skills/*/SKILL.md`.

### Применение
Проверять missing fences, duplicate names, directory/name mismatch, YAML block scalars и indented continuation lines после `description`.

### Evidence
- 2026-06-19 self-review нашёл и исправил malformed-frontmatter fallback и continuation handling.

---

## 2026-06-19 | patch-smaller-after-context-drift
**Тип**: workaround
**Модуль**: apply_patch workflow
**Приоритет**: LOW
**Recurrence-Count**: 1

### Суть
Если большой `apply_patch` не находит expected lines, нужно re-read exact context и разбить patch на меньшие независимые куски.

### Когда важно
Когда multi-file patch меняет документы, которые могли чуть отличаться от ожидаемого текста.

### Применение
Сначала перечитать failing target range, затем применить smaller patches по файлам или секциям.

### Evidence
- 2026-06-19 multi-file patch для learnings/errors/evolve skill не применился из-за небольшой разницы текста.

---
## 2026-06-19 | internal-llm-json-tolerant-final-json-strict
**Тип**: pattern
**Модуль**: internal LLM JSON parsing
**Приоритет**: MEDIUM
**Recurrence-Count**: 1

### Суть
Для internal LLM helper outputs использовать tolerant parsing и semantic revalidation; для final user-visible/stage schemas оставлять strict parsing.

### Когда важно
Когда меняются `PromptImprover`, `PlanningSwarm`, internal agent helpers или любые промежуточные LLM JSON outputs, которые не являются финальным публичным contract.

### Применение
Internal helper output можно чинить tolerant parser'ом, bounded retry и последующей semantic/schema revalidation. Final user-visible output, stage schemas, lifecycle decisions и persisted contracts должны оставаться strict JSON/schema и не принимать "почти валидный" ответ.

### Evidence
- 2026-06-19 Day 15 live/manual flow потребовал tolerant handling для internal helper JSON, но final validation/lifecycle contracts остались strict.

---
## 2026-06-19 | readme-feature-docs-architecture-first
**Тип**: pattern
**Модуль**: documentation
**Приоритет**: MEDIUM
**Recurrence-Count**: 1

### Суть
README для новой фичи должен сначала объяснять систему, компоненты, state flow и схемы; ссылки/commands идут после, не вместо архитектурного описания.

### Когда важно
Когда добавляется крупная фича, новая demo-дока, architecture/FRD/PRD update или README summary для пользовательской записи demo.

### Применение
Начинать с product/system overview: какие компоненты участвуют, кто владеет state, какие gates/agents/evidence есть, как выглядит happy path и failure path. Команды, ссылки на demo scripts и troubleshooting добавлять после этого как операционный слой.

### Evidence
- 2026-06-19 README/demo docs для Day 15 сначала были сведены к ссылкам/commands; пользователь потребовал консистентное описание системы со схемами.

---
## 2026-06-19 | day15-primary-demo-one-tui-app-owned-verification
**Тип**: correction
**Модуль**: manual testing
**Приоритет**: HIGH
**Recurrence-Count**: 1

### Суть
Primary Day 15+ demo/manual proof должен быть одним `cw` TUI session: пользователь запускает `cw` один раз и дальше пишет normal messages inside TUI. Старые формулировки `assistant chat` / REPL считаются legacy wording and must not replace the TUI proof. App-owned trusted verification может запускаться после approval approved plan или semantic check/finish intent; пользователь не вводит точную command и не управляет FSM командами.

### Когда важно
Когда обновляется Day 15 demo-дока, manual regression script, PRD/FRD/architecture или live acceptance proof.

### Применение
Не заменять primary demo на серию `assistant chat --once --input ...`, `cw chat --once`, `cw mcp ...`, smoke scripts или direct storage edits. Сценарий должен идти через `cw` TUI, сохранять human transcript, проверять отсутствие raw stage JSON, final state `done/none/ready_for_done`, app-issued evidence, audit roles planning specialists/orchestrator/executor/reviewer, provider `google/gemini-3.1-flash-lite` для live proof.

### Evidence
- 2026-06-19 live Gemini proof passed as one interactive process with app-owned `go test ./manual_scratch/day14_stock_profit` evidence; prior five-command `--once` flow was rejected as non-user scenario.
- 2026-06-27 hard rule update: current and future user-facing homework demos default to real `cw` TUI, not legacy `assistant chat` / CLI-only flows.

---
## 2026-06-19 | product-north-star-coding-agent-cli
**Тип**: correction
**Модуль**: product
**Приоритет**: HIGH
**Recurrence-Count**: 1

### Суть
Конечная цель проекта — terminal-first AI coding agent CLI в классе Claude Code / Codex CLI, а не generic chat utility, memory demo или debug wrapper вокруг LLM.

### Когда важно
Когда меняются PRD/FRD/architecture, UX сценарии, Day acceptance, tool roadmap, task lifecycle или agent behavior.

### Применение
Оценивать решения через вопрос: помогает ли это разработчику работать в репозитории через chat-driven autonomous workflow с repo context, controlled file edits, command/test execution, diffs, evidence and recovery. P0 ограничения на file/shell tools — временная граница control-plane среза, не отказ от конечной цели.

### Evidence
- 2026-06-19 пользователь указал, что расхождение ожиданий возникло из-за незафиксированной цели: нужен аналог по классу Claude Code / Codex CLI.

---
## 2026-06-19 | day15-demo-single-source-leetcode-task
**Тип**: correction
**Модуль**: manual testing
**Приоритет**: HIGH
**Recurrence-Count**: 1

### Суть
Day 15 demo scenario должен жить только в общем `docs/manual-testing-demo.md`, без отдельного focused duplicate doc. Основная задача demo должна выглядеть как нормальная coding-agent задача, например простая LeetCode-style задача, а не "проверь существующий package".

### Когда важно
Когда обновляется Day 15 manual demo, deterministic smoke, README/PRD/FRD/architecture links или сценарий для записи видео.

### Применение
Держать Day 15 canonical scenario в `docs/manual-testing-demo.md`; если нужен smoke, он должен повторять тот же user flow через один `cw` TUI process. Для Day 15 использовать маленькую coding task вроде `Contains Duplicate`, где пользователь просит решить задачу, а приложение само выводит проверку из approved plan/criteria.

### Evidence
- 2026-06-19 пользователь указал, что Day 15 demo хранился в двух файлах и содержал странную задачу "проверить существующий Go package"; scenario заменён на `Contains Duplicate` и focused doc удалён.

---
## 2026-06-19 | day15-approved-plan-command-inference-live-variance
**Тип**: correction
**Модуль**: trusted verification
**Приоритет**: HIGH
**Recurrence-Count**: 1

### Суть
Day 15 live flow должен выводить trusted verification из approved plan/criteria даже когда модель пишет natural language вроде `standard go test commands`, package path с trailing punctuation или test/pass wording в другой criteria line. Reviewer-agent output variance не должен ломать flow, если app-issued criteria-matched evidence уже есть.

### Когда важно
Когда меняется `firstTrustedVerificationCommand`, approved-plan verification, reviewer validation path или live manual scenario через OpenRouter.

### Применение
Не принимать natural-language phrase `go test commands` как argv command; нормализовать package paths перед `directoryHasGoFiles`; infer `go test ./pkg` только из approved task state с существующим Go package path and test/pass/verification criteria. При reviewer rejection сохранять warning, но final lifecycle decision оставлять за application gate and trusted evidence.

### Evidence
- 2026-06-19 live Gemini Day 15 выявил 408 retry, `go test commands` false command, trailing-dot package path miss и reviewer variance; после fixes clean live proof passed with `DAY15_LIVE_PASS ... events=61`.
- 2026-06-19 superseded by `verification-command-selection-no-language-path-heuristics`: do not infer commands from language/path; use exact approved command or structured verification planner.

---
## 2026-06-19 | verification-command-selection-no-language-path-heuristics
**Тип**: correction
**Модуль**: verification architecture
**Приоритет**: HIGH
**Recurrence-Count**: 1

### Суть
Нельзя выбирать verification command эвристикой по языку, framework или path, например `Go package path -> go test ./pkg`. Это ломает coding-agent архитектуру для других языков и превращает app в набор случайных language hacks.

### Когда важно
Когда меняются trusted verification, Day 15 lifecycle, manual demo, CLI `--verify`, command allowlist или auto evidence.

### Применение
Правильный flow: exact command из approved task state -> иначе structured verification planner/referee strict JSON `{command, confidence, reason}` -> локальный argv-only parser/allowlist/path safety/timeout/output cap -> trusted evidence store. Natural-language fragments like `go test commands` are not commands.

### Evidence
- 2026-06-19 пользователь отклонил Go-specific fallback; implementation moved to language-agnostic `VerificationResolver` plus command policy tests.

---
## 2026-06-26 | manual-proof-must-use-requested-ui-surface
**Тип**: correction
**Модуль**: manual testing
**Приоритет**: HIGH
**Recurrence-Count**: 1

### Суть
Если acceptance/demo просит TUI, доказательство должно идти через настоящий TUI. CLI probes допустимы как setup/debug, но не как финальный proof пользовательского сценария.

### Когда важно
Когда пользователь просит показать работу в TUI, записать видео, проверить manual acceptance или доказать end-to-end UX, особенно для tool/MCP/agent workflows.

### Применение
Финальный proof должен назвать реальную UI surface, provider/model, storage/evidence path и видимый результат на экране. CLI-команды можно использовать для подготовки storage/config или диагностики, но acceptance evidence должен проходить через запрошенный интерфейс.

### Evidence
- 2026-06-26 пользователь отклонил CLI-only MCP demo и потребовал "тестируй в настоящем TUI"; финальный proof прошёл через TUI slash `/mcp add/tools/call/remove` и timeline `MCP tool call/result`.

---
## 2026-06-26 | mcp-tool-results-need-user-visible-fields
**Тип**: pattern
**Модуль**: MCP / TUI output
**Приоритет**: MEDIUM
**Recurrence-Count**: 1

### Суть
Для MCP demo мало доказать `tools/call`; TUI/CLI output должен показывать фактически используемые business fields, не только raw success или один id.

### Когда важно
Когда интегрируется MCP/API tool и acceptance требует "получить и использовать результат" или пользователь записывает demo.

### Применение
Выводить поля, на которых строится ответ агента, например `full_name`, `html_url`, `default_branch`, `language`, counters/status fields. Добавлять тесты, которые проверяют видимые поля demo-контракта.

### Evidence
- 2026-06-26 review нашёл, что `/mcp call` печатал почти только `full_name`; fix вывел `description`, `html_url`, `default_branch`, `language`, `stars`, `forks`, `open_issues`, `visibility`, `updated_at`.

---
## 2026-06-26 | scheduled-mcp-two-repo-demo-contract
**Тип**: pattern
**Модуль**: MCP / scheduled demo
**Приоритет**: MEDIUM
**Recurrence-Count**: 3

### Суть
Для scheduled MCP homework с отдельным `mcp-server` и `coding_writer` держать ownership раздельным: `mcp-server` — scheduled producer/storage/MCP read tools, `coding_writer` — LLM agent loop/demo surface. Если acceptance говорит "агент", demo must include real LLM interpretation, not only polling output.

### Когда важно
Когда задача требует MCP tool с отложенным/периодическим выполнением, persisted aggregate и наглядный demo agent workflow через несколько проектов.

### Применение
Не привязывать 24/7 работу к lifetime stdio MCP call. Делать worker/producer отдельно, MCP tools — read-only над persisted data, а agent side — periodic LLM loop over MCP aggregate. Primary homework demo доказывать через реальный `cw` TUI, когда пользователь-facing acceptance не просит иное. CLI loops such as `cw mcp watch-agent` допустимы как legacy/debug/smoke helpers, but must not be the default demo template. Для worker-based flows можно дополнительно показывать worker ticks; no fake "server terminal" for stdio flows because the client spawns that process itself.

### Evidence
- 2026-06-26 Day 18 implementation split: `/Users/nikita/Documents/mcp-server` owns GitHub scheduled worker and JSON/JSONL aggregate, while `coding_writer` owns the LLM consumer/agent demo surface.
- 2026-06-26 user clarified strict acceptance: "Не сервис, а агент. С ллм под капотом"; `cw mcp watch-agent` was added to call MCP summary, pass aggregate to active LLM, and print periodic LLM summary, but later TUI-first policy makes this a helper unless explicitly requested.
- 2026-06-27 Day 19 review found README showed a manually-started stdio `server.py` terminal that `cw mcp add --command ...` did not consume; demo docs were corrected to show `cw` spawning stdio MCP itself.

---

## 2026-06-27 | multi-repo-change-commit-boundary
**Тип**: correction
**Модуль**: git / multi-repo tasks
**Приоритет**: MEDIUM
**Recurrence-Count**: 1

### Суть
Когда задача меняет несколько git repositories, commit в текущем repo не покрывает sibling repo changes.

### Когда важно
Когда работа затрагивает `coding_writer` и внешний проект, например `/Users/nikita/Documents/mcp-server`.

### Применение
Перед утверждением "все изменения закоммичены" явно проверять и сообщать per-repo commit state: какой repo закоммичен, какой repo ещё dirty, и почему один commit не может покрыть оба.

### Evidence
- 2026-06-27 Day 19 commit was created in `coding_writer`, while `/Users/nikita/Documents/mcp-server` still had uncommitted Day 19 changes because it is a separate git repository.

---

## 2026-06-27 | llm-review-cache-ignore
**Тип**: pattern
**Модуль**: review harness / git hygiene
**Приоритет**: LOW
**Recurrence-Count**: 1

### Суть
Local `/review` harness can create `.llm-review/cache/**`; these are runtime cache files, not product artifacts.

### Когда важно
After running `/review`, `llm_auto_review`, or review prompt generation that builds symbol indexes/cache.

### Применение
Keep `.llm-review/` ignored in repo `.gitignore`; do not stage review cache files.

### Evidence
- 2026-06-27 `/review extreme -n 1` created `.llm-review/cache/symbol_index/*.json` as untracked files until `.gitignore` was updated.

---
<!-- LEARNINGS:END -->
