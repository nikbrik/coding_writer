# FRD: TUI-интерфейс `cw`

## 1. Назначение

Этот документ описывает функциональные требования к TUI для `cw`/`codingwriter`.
Он не заменяет `docs/frd.md`: основной FRD фиксирует продуктовый control-plane,
а этот документ фиксирует пользовательский терминальный интерфейс поверх него.

Главный критерий готовности TUI: сценарий Day 15 из
`docs/manual-testing-demo.md` должен проходить end-to-end внутри TUI через один
обычный `cw`, без ручного управления внутренним состоянием задачи.

TUI не является отдельным владельцем lifecycle. Он показывает состояние,
принимает пользовательский ввод и подтверждения, но все authoritative мутации
должны идти через существующие runtime managers, `ProcessController`,
`TransitionGate`, `LifecycleGate`, memory proposal flow и trusted evidence store.

## 2. Scope

### 2.1. Входит в P0

- Единая команда `cw`/`codingwriter` как основная точка входа продукта.
- TUI как основной интерактивный режим `cw` для terminal session.
- Plain REPL как fallback через `cw --plain`.
- Одно постоянное поле ввода с историей.
- Лента событий task/session timeline.
- Панель состояния: model, profile, task id, stage, current step,
  expected action, status, latest evidence, pending approval.
- Панель плана и acceptance criteria.
- Inline approve/reject для pending planning proposal.
- Панель planning swarm: роли, verdict/contribution, findings, proposed changes.
- Панель applied files и diff placeholder.
- Панель trusted evidence: command, exit code, summary/output preview, evidence id.
- Панель memory proposal: layer, kind, content, reason, accept/reject/edit.
- Inline approve/reject для memory records.
- Resume banner/action после restart, если есть active/paused task.
- Отображение warnings/errors/invariant conflicts с code, message, hint/evidence.
- Отображение audit-derived событий: prompt improvement, swarm, approval,
  executor/reviewer roles, transitions, verification.
- Поддержка Day 15 lifecycle в одном TUI session:
  planning -> execution -> validation -> done.

### 2.2. Не входит в P0

- Отдельный web UI.
- Полноценный IDE-like editor.
- Самостоятельный TUI lifecycle engine.
- Произвольный shell/tool runner внутри UI components.
- Silent memory writes без user approval.
- Полноценный PatchSet/diff apply workflow, если backend его еще не реализует.
- General-purpose repo search/read tools, если они еще не входят в backend policy.

Если backend уже умеет materialize files из `execution.deliverable`, TUI обязан
показывать этот результат. Если backend еще не умеет полноценный PatchSet, TUI
показывает diff placeholder и applied files, а не симулирует diff.

## 3. Пользовательские роли

`Разработчик` — основной пользователь. Запускает `cw`, формулирует
задачу обычным языком, подтверждает план, смотрит изменения, evidence и финал.
Не обязан знать внутренние команды task FSM.

`Проверяющий demo/acceptance` — записывает Day 11-15 demo и проверяет, что TUI
показывает память, профиль, lifecycle, invariants, swarm, evidence и final state.

`Maintainer продукта` — использует TUI для диагностики: смотрит audit timeline,
pending approvals, evidence refs, warnings и причину незавершенного lifecycle.

## 4. Принципы UX

- Пользователь пишет обычные фразы, не управляет state machine вручную.
- `stage`, `expected_action`, approvals и evidence всегда видны.
- Любое рискованное действие требует видимого пользовательского решения или
  backend-approved semantic intent.
- Raw JSON не является основным UX.
- Slash commands допустимы для inspection/recovery/debug, но не являются
  обязательным happy path.
- Если draft input начинается с `/`, TUI показывает список доступных
  slash-команд без нажатия Enter и фильтрует список по префиксу.
- TUI не скрывает блокеры: если задача не может перейти дальше, интерфейс
  показывает конкретный gate, missing evidence или pending approval.
- Provider/model/storage данные, пришедшие извне, отображаются безопасно:
  control characters escaped, длинные выводы ограничены.

## 5. Layout

### 5.1. Wide terminal

Рекомендуемый layout для ширины от 120 колонок:

```text
┌────────────────────────────────────────────────────────────────────────────┐
│ Header: model | profile | task | stage | expected_action | status          │
├──────────────────────────────────────────────┬─────────────────────────────┤
│ Timeline                                     │ Status / approvals          │
│ - user input                                 │ Plan + criteria             │
│ - prompt improvement                         │ Planning swarm              │
│ - assistant answer                           │ Evidence                    │
│ - transitions                                │ Memory proposal             │
│ - warnings/errors                            │ Applied files / diff        │
├──────────────────────────────────────────────┴─────────────────────────────┤
│ Footer/help: keys, pending approval, latest warning                         │
├────────────────────────────────────────────────────────────────────────────┤
│ Input                                                                        │
└────────────────────────────────────────────────────────────────────────────┘
```

Timeline остается главным рабочим контекстом. Sidebar показывает текущее
решение, требующее внимания: pending plan, memory proposal, evidence или blocker.

### 5.2. Narrow terminal

Для узких терминалов TUI переключается в tabbed/fullscreen panes:

- `Timeline`;
- `Status`;
- `Plan`;
- `Swarm`;
- `Evidence`;
- `Memory`;
- `Files`;
- `Help`.

Поле ввода остается снизу. Если ширины недостаточно для таблиц, строки
переносятся, а secondary columns скрываются в detail view.

### 5.3. Минимальные размеры

- Ниже 80x24 TUI показывает compact mode.
- В compact mode обязательны input, status line, timeline и current approval.
- Если терминал слишком мал для безопасного отображения, TUI показывает typed
  error с hint использовать `cw --plain`.

## 6. Functional Requirements

### TUI-FR-001. Запуск TUI

`cw`/`codingwriter` в интерактивном терминале должен запускать TUI по
умолчанию.

Поведение:

- `cw` запускает TUI.
- `cw --tui` принудительно запускает TUI.
- `cw --plain` запускает старый REPL fallback.
- `cw --once` не запускает TUI.
- `--json` совместим только с `--once`, не с TUI.
- В non-interactive stdin/stdout `--tui` возвращает typed error.

Acceptance:

- интерактивный `cw` открывает TUI;
- TUI использует terminal alternate screen: занимает всё окно, очищает view при
  входе/выходе и не оставляет partial render в scrollback;
- `cw --plain` сохраняет прежний REPL;
- non-interactive `cw --tui` не зависает и предлагает `--plain`.

### TUI-FR-002. Поле ввода и история

TUI должен иметь одно постоянное поле ввода.

Поведение:

- `enter` отправляет текущий текст в backend exchange;
- пустой ввод не отправляется;
- `up`/`ctrl+p` и `down`/`ctrl+n` ходят по истории;
- `esc` очищает текущий draft;
- во время backend exchange input disabled, но экран продолжает показывать
  progress/spinner;
- placeholder зависит от task state:
  - нет task: `Опишите задачу...`;
  - planning approval: `Подтвердите план или напишите правки...`;
  - execution: `Следующее действие...`;
  - validation: `Попросите проверить или завершить...`;
  - done: `Новая задача...`.

Acceptance:

- пользователь может пройти Day 15 только обычными фразами в input;
- история сохраняется внутри TUI session;
- pending approval ясно отражается в placeholder/status.

### TUI-FR-002A. Slash command hints

TUI должен показывать доступные slash-команды сразу при наборе `/`.

Поведение:

- draft `/` открывает компактный список команд;
- draft `/pro` показывает только matching commands вроде `/profile` и
  `/process audit`;
- подсказка исчезает, когда input больше не начинается с `/`, открыт picker
  или выполняется backend exchange;
- список не отправляет команду сам по себе: команда выполняется только по
  `enter`.

Acceptance:

- пользователь видит `/new`, `/resume`, `/task`, `/profile`, `/model` без
  отдельного `/help`;
- фильтр не ломает обычный chat input;
- подсказка помещается в TUI layout и не вытесняет input.

### TUI-FR-003. Timeline событий

TUI должен показывать action timeline текущей session/task.

Источники:

- локальный user input;
- assistant answer;
- `ChatResponse.Transition`;
- `ProcessAuditEvent`;
- memory proposal;
- applied artifacts;
- evidence refs;
- warnings/errors.

Обязательные типы событий:

- user input;
- assistant answer;
- prompt improvement;
- planning;
- planning swarm final;
- approval request/result;
- execution;
- applied files;
- verification;
- validation;
- transition;
- memory proposal;
- invariant conflict;
- warning/error;
- done/blocker.

Acceptance:

- Day 15 timeline показывает prompt improvement, planning swarm, approval,
  executor/reviewer roles, verification и transitions;
- raw audit JSON не нужен для понимания visible flow;
- каждое событие имеет краткий summary и раскрываемый detail.

### TUI-FR-004. Панель состояния

TUI должен постоянно показывать authoritative state из backend.

Поля:

- active model;
- active profile;
- storage/session id в compact form;
- task id/title;
- `stage`: `planning`, `execution`, `validation`, `done`;
- `current_step`;
- `expected_action`;
- `status`: `active`, `paused`;
- pending approval kind;
- latest transition;
- latest evidence id/count;
- latest warning/error.

Acceptance:

- active model виден в header/status даже в новом пустом chat;
- модель, выбранная через `/model`, сохраняется в storage config и остаётся
  active после restart;
- после первого Day 15 запроса видно `stage=planning`;
- после approval видно `stage=execution`;
- после verification/validation видно `stage=validation`;
- после завершения видно `stage=done` и `expected_action=none`.

### TUI-FR-005. План и acceptance criteria

TUI должен показывать persisted plan, pending planning proposal и acceptance
criteria.

Поведение:

- Pending plan отделяется от approved plan.
- Criteria показываются как checklist.
- Open questions или blockers показываются отдельно.
- Пользователь может approve/reject pending plan inline.
- Reject требует видимого результата: pending plan cleared или возвращен
  в planning с reason.
- Правки к плану отправляются как обычный user input, а не как JSON edit.

Acceptance:

- Day 15 planning output показывает pending plan/criteria, не execution;
- approval фразой или inline action переводит planning -> execution через backend;
- пользователь не использует `/task move`, `/task step`, `/task expect`.

### TUI-FR-006. Planning swarm

TUI должен показывать результат planning swarm как отдельный блок.

Минимальные роли Day 15:

- `requirements_specialist`;
- `code_research_specialist`;
- `architecture_specialist`;
- `test_validation_specialist`;
- `risk_regression_specialist`;
- `planning_orchestrator`.

Для каждой роли показывать:

- role;
- verdict/contribution;
- finding count;
- top finding, если есть;
- proposed plan/criteria change, если есть.

Acceptance:

- Day 15 после planning показывает `Planning swarm`;
- блок содержит role-specific review, а не пересказ исходного prompt;
- audit-derived роли можно увидеть в timeline/detail.

### TUI-FR-007. Execution и applied files

TUI должен показывать execution result и файлы, материализованные backend из
`execution.deliverable`.

Поведение:

- список файлов отображается отдельной секцией `Files`;
- для каждого файла показывается repo-relative path и status;
- если backend вернул applied artifacts, TUI не требует от пользователя
  вручную копировать код;
- если materialization failed, TUI показывает typed warning/error и не сообщает
  ложный success.

Acceptance:

- в Day 15 после execution видны файлы
  `manual_scratch/day15_contains_duplicate/...`;
- пакет появляется в workspace до trusted verification;
- TUI не запускает shell самостоятельно из component layer.

### TUI-FR-008. Diff panel

P0 должен иметь diff panel как placeholder-aware область.

Поведение:

- если backend предоставляет applied artifacts, panel показывает список файлов;
- если backend предоставляет diff, panel показывает unified diff;
- если diff недоступен, panel явно пишет `diff unavailable in P0` и показывает
  applied files;
- будущий PatchSet approval должен занять эту же область.

Acceptance:

- отсутствие полноценного PatchSet не блокирует Day 15;
- пользователь видит, какие файлы были созданы/изменены.

### TUI-FR-009. Trusted evidence panel

TUI должен показывать trusted evidence как продуктовый результат, а не как
сырой лог.

Поля:

- evidence id;
- task id;
- source command;
- cwd/repo scope, если доступно;
- exit code;
- status summary;
- bounded output preview;
- output hash/ref, если доступно;
- timestamp;
- relation to acceptance criteria.

Acceptance:

- Day 15 verification показывает command, exit code и evidence id;
- пользователь не вводит exact command проверки;
- final done невозможен в UI без criteria-matched trusted evidence, если
  criteria требуют verification.

### TUI-FR-010. Verification intent

TUI должен передавать пользовательское намерение проверки в backend как обычный
chat input.

Правила:

- TUI не выбирает command по языку, framework или path.
- Command selection делает backend: exact approved command или structured
  verification planner/referee, затем local argv/policy/sandbox gates.
- TUI отображает pending/running/completed verification.

Acceptance:

- фраза Day 15 `Проверь критерии и заверши...` приводит к app-owned trusted
  verification;
- TUI не просит пользователя ввести `go test ...`;
- TUI показывает, если resolver не смог получить разрешенную команду.

### TUI-FR-011. Memory proposal panel

TUI должен показывать latest pending memory proposal.

Поля record:

- record id;
- layer: `short`, `work`, `long`, `ignore`;
- kind;
- content;
- reason;
- confidence/status, если доступно.

Actions:

- accept record;
- reject record;
- accept all;
- reject all;
- edit, если backend поддерживает edits;
- show saved/blocked result.

Acceptance:

- Day 11/15 memory proposal виден после значимых ответов;
- long/work memory не сохраняются silently;
- accepted/rejected records отражаются в timeline.

### TUI-FR-012. Profiles

TUI должен показывать active profile и поддерживать profile commands без выхода
из TUI.

Поведение:

- status показывает active profile;
- `/profile <id>` и profile-related slash commands остаются доступными;
- после смены profile status обновляется;
- следующий provider call идет с новым profile через backend prompt builder.

Acceptance:

- Day 12 можно показать смену `student`/`senior`/`tester` в TUI;
- пользователь не копирует style instructions вручную.

### TUI-FR-013. Invariants

TUI должен показывать invariant conflicts и не превращать их в обычные ошибки
provider.

Поведение:

- input conflict показывается до normal provider call;
- output conflict показывается как blocked response;
- refusal содержит invariant id и evidence/summary;
- safe flow после refusal может продолжиться.

Acceptance:

- Day 14 conflict request виден как `invariant_conflict`;
- после refusal пользователь может вернуться к safe request в той же TUI session.

### TUI-FR-014. Session resume

TUI должен восстанавливать видимое состояние после restart.

Поведение:

- при запуске создает новый пустой chat session;
- новый пустой chat не появляется в `/resume`, пока пользователь не отправил
  первое сообщение и не появилась short memory/history;
- при запуске может показать только компактный current task focus, но не
  открывает старые plan/criteria/work details, short/audit/pending proposal
  автоматически;
- timeline startup показывает `new chat`, storage, current task и hint
  `history: /resume`;
- sidebar startup показывает hint для `/resume` и `/task`, а не старый план
  текущей задачи;
- список `/resume` показывает дату/время старта session и автозаголовок из
  первого user input; raw `session_id` не должен быть единственным сигналом;
- старый chat/short/audit/pending proposal открывается только через `/resume`;
- после `/resume <session_id>` timeline показывает audit только выбранного
  session/current task, с collapse/windowing для retry/audit noise;
- показывает active/paused task banner;
- для paused task предлагает resume action;
- для active task позволяет продолжить обычным input;
- если task done, показывает final summary и готовность к новой задаче.

Acceptance:

- Day 13 pause/restart/resume можно пройти в TUI;
- current task state не теряется после нового `cw`;
- запуск `cw` не поднимает старый short/audit/pending proposal без явного
  `/resume`;
- запуск `cw` не показывает старые task instructions/plan/criteria в sidebar
  без явного `/task` или `/resume`;
- chat resume и task resume разделены: `/resume` продолжает старый chat/short,
  `/task resume` продолжает paused task/work;
- resume action идет через backend, не через локальную мутацию UI state.

### TUI-FR-015. Errors, warnings, blockers

TUI должен отображать typed errors и blockers без потери контекста.

Минимальные категории:

- provider auth/config error;
- missing API key;
- non-interactive TUI error;
- invariant conflict;
- lifecycle gate rejection;
- missing/invalid evidence;
- materialization failure;
- memory proposal apply failure;
- backend timeout/cancel.

Acceptance:

- warning не ломает TUI render;
- blocker виден в status/timeline;
- пользователь понимает следующий безопасный шаг.

## 7. Точка входа

### TUI-FR-016. Единая команда `cw` / `codingwriter`

Продукт должен иметь одну пользовательскую точку входа: `cw` или
`codingwriter`. TUI всегда является режимом по умолчанию для интерактивного
запуска без флагов.

Поведение:

- `cw` открывает TUI в interactive terminal.
- `codingwriter` является тем же entry point или полным алиасом `cw`.
- `cw --plain` запускает plain REPL fallback.
- `cw --once "..."` выполняет one-shot запрос без TUI.
- `cw --json --once ...` выполняет one-shot JSON mode без TUI.
- `cw <subcommand>` запускает CLI subcommands: `profiles`, `task`, `memory`
  и другие product commands.
- `--json` совместим только с non-TUI flows, в первую очередь с `--once`.

Acceptance:

- пользователь запускает основной interactive UX командой `cw`;
- `cw` без флагов не показывает выбор режима, а сразу открывает TUI;
- все существующие пользовательские режимы доступны через ту же команду;
- `assistant`, `assistant chat`, `assistant chat --once`,
  `assistant chat --json` не остаются основным documented happy path.

### TUI-FR-017. Переименование бинарника

Пользовательский бинарник должен быть переименован из legacy `assistant` в
`cw`/`codingwriter`, без смены Go module path.

Поведение:

- вместо `.assistant/bin/assistant` используется `.codingwriter/bin/cw` или
  `.codingwriter/bin/codingwriter`;
- `go.mod` module name остается `github.com/nikbrik/coding_writer`;
- `cmd/assistant/` переименовывается в `cmd/cw/` или `cmd/codingwriter/`;
- compatibility shim для legacy `assistant` допустим только как переходный
  слой, не как canonical entry point.

Acceptance:

- install/build artifacts публикуют новый бинарник `cw` или `codingwriter`;
- документация P0 использует `cw`/`codingwriter` как canonical command;
- repo structure не требует смены module path ради переименования binary.

## 8. Day 15 acceptance matrix

| Day 15 проверяет | Требование к TUI |
| --- | --- |
| Один `cw`, обычные фразы | TUI default для `cw`; input принимает весь flow без ручных FSM commands |
| Первый запрос создает planning task | Status panel показывает task id и `stage=planning`; timeline показывает user goal и planning |
| Prompt improvement | Timeline/detail показывает `prompt_improvement_call` summary |
| Planning swarm | Swarm panel показывает specialist roles, verdict/contribution, findings/proposals |
| Pending plan/criteria | Plan panel показывает pending plan и acceptance criteria, не execution |
| Approval через chat | Input или inline approval отправляет intent в backend; transition planning -> execution виден |
| Execution через microtask agent | Timeline показывает executor role / execution event |
| Safe file materialization | Files/diff panel показывает `manual_scratch/day15_contains_duplicate/...` после backend materialization |
| Пользователь не вводит exact command | Verification intent идет обычной фразой; TUI не просит `go test ...` |
| App-owned VerificationResolver | Evidence panel показывает resolved trusted command/result/evidence id |
| execution -> validation требует evidence | Status/timeline показывают transition только после trusted evidence |
| Validation через reviewer | Timeline показывает reviewer role / validation event |
| done требует accepted validation | Status показывает `stage=done`, `expected_action=none`, evidence остается видимым |
| Audit содержит роли и transitions | Timeline строится из audit events и раскрывает детали |
| Нет `/task move`, `/task step`, `/task expect` | Happy path не требует этих команд; они остаются только debug/inspection |
| Единая точка входа | `cw` открывает TUI; `cw --plain`, `cw --once`, `cw --json --once ...` и `cw <subcommand>` покрывают остальные режимы |

## 9. Keybindings

Обязательные:

- `enter`: отправить input или подтвердить focused inline action;
- `tab` / `shift+tab`: переключить pane/focus;
- `ctrl+p` / `up`: предыдущий input history;
- `ctrl+n` / `down`: следующий input history;
- `esc`: очистить draft или закрыть detail;
- `?`: help overlay;
- `q` / `ctrl+c`: quit с подтверждением, если backend exchange не завершен;
- `a`: approve focused approval;
- `r`: reject focused approval;
- `e`: edit focused memory proposal, если backend поддерживает;
- `m`: focus memory panel;
- `p`: focus plan panel;
- `v`: focus evidence panel;
- `f`: focus files/diff panel;
- `t`: focus timeline.

Клавиши не должны быть единственным способом пройти Day 15: обычные фразы в
input остаются canonical flow.

## 10. Non-functional Requirements

### Производительность

- Первый render до backend call: до 200 ms на типичной локальной машине.
- Resize/render без backend call: визуально мгновенно, без заметных зависаний.
- Timeline должен оставаться usable при сотнях audit events через collapse/windowing.
- Provider/tool latency показывается spinner/progress state.

### Надежность

- Panic в render недопустим из-за пустого task/proposal/evidence.
- Missing audit/evidence record показывается warning, не crash.
- Backend cancel/timeout возвращает управляемый error и сохраняет session context.
- TUI не должен corrupt storage при exit.

### Терминалы

- Поддержка macOS Terminal/iTerm2 и стандартных ANSI terminals.
- Plain fallback обязателен для dumb/non-interactive terminals.
- Цвет не должен быть единственным carrier состояния; нужны labels/icons/text.
- Узкий терминал должен использовать tabs/compact mode.

### Безопасность

- TUI не хранит secrets.
- Provider/storage text escapes control characters.
- Длинный command output bounded и раскрывается по запросу.
- TUI не обходит path safety, command policy, memory safety или invariant gates.

## 11. Edge cases

- Нет active task: status показывает `no task`, input предлагает описать задачу.
- Task paused: banner предлагает resume; обычный input не должен silently mutate paused task.
- Task done: status показывает final state; новый input начинает новую задачу через backend policy.
- Pending planning + memory proposal одновременно: approval panel приоритизирует planning, memory доступна отдельной pane.
- Verification failed: evidence panel показывает exit code/output preview; status не показывает done.
- Materialization failed: files panel показывает failed artifact; validation не должна считаться готовой.
- Invariant conflict: timeline показывает refusal, safe next input разрешен.
- Terminal narrow: таблицы превращаются в vertical detail blocks.
- Lost evidence ref: warning + audit ref, без crash.
- Provider 401/403: typed auth error, no fake success.
- User quits during exchange: либо cancel через context, либо явный "exchange still running" prompt.

## 12. P0 plan expansion

Текущий P0 TUI plan не должен ограничиваться минимальным shell с input,
timeline и status. Для Day 15 P0 считается достаточным только если включает:

- план и criteria panel;
- planning swarm panel;
- inline planning approval;
- applied files/diff placeholder;
- trusted evidence panel;
- memory proposal panel;
- audit-derived timeline с prompt improvement, specialist roles, executor,
  reviewer и transitions после явного `/resume` или текущего exchange;
- fresh chat startup: обычный `cw` открывает новый пустой chat session, не
  поднимает старый short/audit/pending proposal автоматически;
- explicit resume: старый chat/short/audit/pending proposal открывается только
  через `/resume`;
- visible final done state.
- единая точка входа `cw`/`codingwriter`, где интерактивный запуск по умолчанию
  открывает TUI, а fallback/one-shot/subcommand режимы остаются в той же команде.

Фазы реализации можно сохранять инкрементальными, но definition of done для P0
должен соответствовать Day 15, а не Phase 1 minimal shell.

## 13. Acceptance criteria TUI P0

TUI P0 готов, когда выполнен весь чеклист:

- `cw` в interactive terminal открывает TUI по умолчанию.
- `codingwriter` ведет себя как полный entry point или documented alias `cw`.
- `cw --plain` открывает старый REPL fallback.
- `cw --once ...` работает без TUI.
- `cw --json --once ...` работает как JSON mode без TUI.
- `cw <subcommand>` покрывает CLI subcommands вроде `profiles`, `task`,
  `memory`.
- Пользователь может пройти Day 15 visible flow в одном TUI session:
  1. описать Contains Duplicate;
  2. увидеть pending plan/criteria и planning swarm;
  3. принять план обычной фразой или inline approval;
  4. увидеть execution и applied files;
  5. попросить проверить и завершить без exact command;
  6. увидеть trusted evidence;
  7. увидеть final `stage=done`, `expected_action=none`.
- TUI не требует `/task move`, `/task step`, `/task expect` в happy path.
- Status panel показывает model/profile/task/stage/current_step/expected_action/status.
- Plan panel показывает pending и approved plan/criteria.
- Swarm panel показывает role-specific planning reviews.
- Evidence panel показывает command, exit code, output preview, evidence id.
- Memory panel показывает proposals и поддерживает accept/reject.
- Files/diff panel показывает applied artifacts, даже если full diff недоступен.
- Resume после restart показывает current active/paused task.
- Invariant conflicts показываются typed refusal с id/evidence.
- Узкий терминал остается usable через compact/tabs.
- TUI components не выполняют shell commands и не мутируют authoritative state
  напрямую.
- Проверка P0 включает `scripts/day15-demo.sh --fake --auto` или эквивалентный
  TUI-level regression, плюс manual/live Day 15 через OpenRouter model из
  `docs/manual-testing-demo.md` для acceptance video.
