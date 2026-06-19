# PRD: консольный помощник для работы с кодом в классе Claude Code / Codex CLI

## 1. Контекст

Проект недели: перейти от обычного режима "запрос-ответ" к минимальному помощнику с сохранённым состоянием. Конечная цель продукта шире: сделать консольный помощник для работы с кодом в том же классе продуктов, что Claude Code и Codex CLI.

Это не обычный чат-бот, не демонстрация памяти и не набор отладочных команд вокруг LLM. Пользовательская модель должна быть такой: разработчик открывает репозиторий, запускает ассистента в терминале, формулирует задачу обычным языком, а ассистент автономно планирует работу, понимает состояние репозитория, предлагает и применяет изменения через контролируемый слой безопасности, запускает проверки, объясняет результат и ведёт задачу до завершения через понятный чат.

Текущий P0/MVP — это слой управления такого помощника: состояние, память, профиль, запросы с учётом этапа, проверки, журнал и проверка команд со стороны приложения. P0 уже умеет применять файлы из структурированного результата `execution`: только внутри рабочего репозитория, только через безопасные пути, только перед доверенной проверкой. P1 обязан расширить это до полноценной работы с репозиторием: читать файлы, показывать diff, подтверждать опасные изменения, запускать команды и восстанавливаться после ошибок, сохраняя всё взаимодействие внутри `assistant chat`.

`day11.md`, `day12.md`, `day13.md`, `day14.md` и `day15.md` считаются жёсткими критериями приёмки. Лекция `03-memory-state-notes.md` задаёт архитектурные принципы: слои памяти, персонализация, конечный автомат задачи, инварианты, сборка запроса к модели, контролируемые переходы и сохранение состояния.

Текущее состояние кода на 2026-06-19:

- Go module уже реализован: `go.mod`, entrypoint `cmd/assistant/main.go`, packages `internal/app`, `internal/cli`, `internal/providers`, `internal/memory`, `internal/profiles`, `internal/tasks`, `internal/prompting`, `internal/process`, `internal/storage`, `internal/validation`.
- CLI построен на Cobra и поддерживает interactive REPL, `chat --once`, JSON output, rendered-prompt inspection, fake provider для deterministic tests и OpenRouter provider для live mode.
- Day 11/12/13/14 покрыты приёмочными тестами: `tests/day_acceptance_test.go` и тестами управления процессом в `tests/process_acceptance_test.go`; Day 15 имеет live-сценарий в `docs/manual-testing-demo.md`, а `scripts/day15-demo.sh --fake --auto` остаётся стабильной проверкой регрессий.
- Рабочий цикл уже реализован через `ProcessController`, `StagePolicyRegistry`, `StagePromptFactory`, проверки, ограниченный повтор, `TransitionGate`, `LifecycleGate`, уточнение запроса, обсуждение плана ролями, микрозадачи, хранилище доказательств проверки, смысловую проверку правил и `process_audit.jsonl`.

## 2. Цель продукта

Сделать CLI coding agent, который по пользовательскому ожиданию похож на Claude Code / Codex CLI по классу продукта: terminal chat + repo-aware autonomous work loop + controlled tools + explicit verification.

Продукт должен:

- принимает запросы пользователя в терминале;
- использует OpenRouter API для вызова LLM;
- даёт выбрать модель при запуске или через команду интерфейса;
- работает как ассистент разработчика внутри репозитория, а не как отдельная утилита для абстрактных ответов;
- держит user-facing happy path в `assistant chat`: пользователь ставит задачу, утверждает план, просит проверить/завершить; приложение само управляет state и verification;
- применяет файлы из структурированного результата текущей задачи через безопасный слой путей и показывает их в секции `Files`;
- в целевом продукте умеет читать файлы, показывать diff, подтверждать рискованные изменения, запускать команды и тесты через безопасный слой разрешений;
- хранит память в отдельных слоях;
- явно решает, какие данные куда сохранять, через отдельный LLM memory-classification step;
- подключает профиль пользователя к каждому запросу;
- демонстрирует разные ответы для разных профилей;
- ведёт текущую задачу как конечный автомат: этап, текущий шаг, ожидаемое действие;
- перед каждым task-scoped LLM call сообщает модели текущий `stage`, `current_step`, `expected_action`, разрешённое действие и роль этапа, чтобы code assistant не работал вслепую;
- хранит active invariants отдельно от диалога, показывает их в prompt и блокирует conflict input/output до persistence;
- автономно ведёт task lifecycle через application gates: планирование, approval, execution, validation и done без ручного управления внутренними state-командами;
- использует обсуждение плана ролями, уточнение запроса, микрозадачи и доверенные доказательства проверки как контролируемые части процесса;
- не требует от пользователя точных verification-команд в chat: пользователь выражает намерение, а приложение выводит безопасную allowlisted проверку из утверждённого плана и acceptance criteria.

## 3. MVP

MVP должен быть небольшим, но архитектурно правильным. Главная ценность P0 — не в количестве инструментов, а в фундаменте для помощника по коду: явное состояние, память, профиль, этапы работы, смысловая проверка, журнал и проверка команд со стороны приложения. Эти компоненты нужны, чтобы следующий слой чтения файлов, diff и shell-команд не превратился в небезопасную оболочку вокруг LLM.

P0 считается первым рабочим срезом продукта класса Claude Code / Codex CLI, а не финальным продуктом. Поэтому любые решения по интерфейсу и архитектуре должны проверяться вопросом: помогает ли это разработчику решать задачи в репозитории через автономную работу в чате, или уводит продукт в сторону ручной отладочной утилиты?

Минимальный сценарий:

1. Пользователь запускает CLI.
2. CLI проверяет OpenRouter API key.
3. CLI предлагает выбрать или ввести модель.
4. CLI предлагает выбрать профиль пользователя или создать новый.
5. Пользователь задаёт вопрос по коду или архитектуре.
6. Prompt builder собирает prompt из системных правил, process policy, stage-specific роли, профиля, рабочей памяти, краткосрочной истории и релевантной долговременной памяти.
7. LLM отвечает внутри текущего stage contract.
8. После принятого ответа ассистент запускает отдельный memory-classification prompt через OpenRouter.
9. LLM выделяет факты из диалога и предлагает слой памяти для каждого факта: `short`, `work`, `long` или `ignore`.
10. CLI показывает пользователю классификацию памяти.
11. Пользователь подтверждает сохранение или правит выбранные слои.
12. Приложение сохраняет факты в физически разные хранилища.
13. При следующем запросе ассистент использует выбранные слои памяти.
14. Если пользователь или provider output нарушает active invariant, приложение отказывает с `invariant_conflict`, invariant ID и evidence.
15. Для работы над задачей пользователь формулирует цель в чате, подтверждает план и просит проверить или завершить обычной фразой; приложение само создаёт задачу, применяет файлы из структурированного результата, получает точную команду проверки через `VerificationResolver`, запускает только разрешённую проверку, применяет переходы этапов и доводит задачу до `done` только после принятой проверки.

## 4. Пользовательские сценарии

### 4.1. Первый запуск

Пользователь запускает ассистента без конфигурации.

Ожидаемое поведение:

- ассистент проверяет `OPENROUTER_API_KEY`;
- если ключа нет, объясняет, как задать `OPENROUTER_API_KEY` через environment; hidden input не входит в P0;
- ключ не сохраняется в репозиторий;
- ассистент до первого provider call показывает, какие категории данных будут отправляться в OpenRouter;
- `assistant init` требует active model (`--model` или `ASSISTANT_MODEL`) и локально валидирует только syntax model id, без provider lookup и без сетевого вызова;
- provider/model lookup выполняется перед реальными provider actions, например `chat`, `/model`, memory propose; в live mode для этого нужен `OPENROUTER_API_KEY`, в tests/demo можно включить fake provider через `ASSISTANT_PROVIDER=fake` или `ASSISTANT_FAKE_PROVIDER=1`;
- `assistant init` создаёт default profiles `student` и `senior`; интерактивное интервью профиля не реализовано в текущем коде, profile CRUD доступен через commands.

### 4.2. Обычный диалог

Пользователь пишет запрос: например, `Спланируй модуль авторизации`.

Ожидаемое поведение:

- ассистент учитывает активный профиль;
- ассистент учитывает текущую рабочую задачу, если она есть;
- ассистент использует краткосрочный контекст текущего диалога;
- ассистент не записывает всё подряд в долговременную память;
- ассистент предлагает сохранить важные факты явно.

### 4.3. Явная классификация и сохранение памяти

Ассистент после ответа предлагает memory diff:

```text
== Memory proposal ==
id: proposal_...
- pmem_... [work] pending requirement: CLI должен поддерживать выбор модели OpenRouter.
- pmem_... [long] pending preference: Пользователь предпочитает короткие ответы на русском.
- pmem_... [short] pending context: В этом диалоге обсуждаем memory layers.
- pmem_... [ignore] pending smalltalk: Пользователь сказал "спасибо".

== Next ==
- Review memory proposal; apply it only if these records should be saved.
- CLI: assistant memory apply --proposal proposal_... --accept all
```

Ожидаемое поведение:

- `short` попадает в память текущей сессии;
- `work` попадает в память текущей задачи;
- `long` попадает в долговременный профиль, решения или знания;
- `ignore` не сохраняется;
- каждый слой хранится отдельно;
- пользователь может посмотреть содержимое каждого слоя.

Ручные команды тоже остаются доступны:

```text
/save work Требование: CLI должен поддерживать выбор модели OpenRouter.
/save long Предпочитаю короткие ответы на русском.
/save short В этом диалоге обсуждаем только memory layers.
```

Но основной flow Day 11 должен проверять именно LLM-классификацию: модель через OpenRouter выбирает, какие факты в какой слой попадают.

### 4.4. Разные профили

Пользователь создаёт два профиля:

- `student`: подробные объяснения, учебный тон, больше примеров;
- `senior`: короткие ответы, инженерные trade-off, минимум объяснений.

Ожидаемое поведение:

- один и тот же запрос даёт разные ответы;
- различие возникает из-за подключённого профиля, а не ручного переписывания запроса;
- ассистент автоматически учитывает стиль, формат и ограничения профиля.

### 4.5. Контролируемая coding task в chat

Пользователь пишет обычным языком: `Нужно реализовать задачу X, проверь по критериям проекта`.

Ожидаемое поведение:

- приложение распознаёт task-scoped intent и создаёт или продолжает active task без `/task start`;
- prompt improver сохраняет исходную цель и формирует улучшенный рабочий prompt перед stage-specific call;
- planning swarm запускает role-specific specialist reviews, собирает findings/proposed changes и merge-ит финальный план с acceptance criteria;
- пользователь подтверждает план обычной фразой в chat, например `одобряю, приступай`;
- приложение валидирует approval, переводит `planning -> execution` и запускает role-scoped execution/review agents;
- когда пользователь пишет `проверь`, `проверь и заверши` или аналогичную фразу, приложение запускает `VerificationResolver`: сначала ищет exact safe command в approved plan/criteria, а если её нет - вызывает structured verification planner/referee, который возвращает strict JSON с exact argv command;
- verification result сохраняется как trusted evidence: command digest, source, exit code, bounded summary и audit record;
- `validation -> done` разрешён только application gate после accepted validation output и criteria-matched trusted evidence.

Жёсткие UX-ограничения:

- пользователь не обязан вводить `/task move`, `/task step`, `/task expect`, править JSON/storage или управлять FSM руками;
- пользователь не обязан знать или вводить точную команду `go test ...`, `npm test ...` или `--verify`;
- если exact command отсутствует и structured verification planner не может вернуть безопасную allowlisted команду, ассистент уточняет acceptance criterion в chat или блокирует `done`, но не перекладывает управление state machine на пользователя;
- `--verify` допустим только как debug/recovery override и не закрывает primary Day 15 demo.

## 5. Canonical contract

PRD, FRD и architecture должны ссылаться на один canonical contract для Day 11, Day 12, Day 13, Day 14 и Day 15.

### Task state

- `stage`: `planning`, `execution`, `validation`, `done`.
- `status`: `active`, `paused`.
- `expected_action`: `user_input`, `llm_response`, `user_confirmation`, `none`.
- `tool_result` не входит в P0, потому что MVP не выполняет tools; его можно вернуть только в P1 вместе с явным tool flow.
- terminal completion фиксируется через `stage=done` и `expected_action=none`; `status=done` не используется в MVP.

### Commands

- Top-level commands и slash commands должны описывать один и тот же canonical command tree.
- Для P0 обязательны только команды, нужные для Day 11/12/13/14/15 demo path и deterministic smoke tests.
- Canonical command matrix должен указывать P0/P1 статус, JSON/stdout/stderr поведение, LLM-call behavior и non-interactive эквиваленты для smoke tests.

### Memory layers

- `short`, `work`, `long` are the only physical storage layers.
- `ignore` exists only in the proposal/audit layer.

### Process-control prompt contract

- Приложение, а не LLM, владеет `TaskState`, transitions, memory writes, provider/tool permissions and validation.
- LLM обязана получать stage awareness в каждом task-scoped prompt: `stage`, `current_step`, `expected_action`, `status`, allowed next stages and selected `ActionKind`.
- Base system prompt остаётся стабильным, но поверх него добавляется trusted stage-specific system prompt.
- `planning` даёт LLM роль planner/requirements analyst и запрещает implementation.
- `execution` даёт LLM роль implementer и запрещает менять acceptance criteria без возврата в planning.
- `validation` даёт LLM роль strict reviewer/QA validator и запрещает fixes/new features внутри review step.
- `done` даёт LLM роль terminal summarizer и запрещает task mutation.
- LLM может предлагать `next_signal`, findings или transition proposal, но применяет transition только application `TransitionGate`.
- Stage policy and process policy outrank profile, task state JSON, memory, short history, prompt audit data and user text when conflicts occur.

### UX hard gate: no internals as required workflow

- Пользовательский happy path должен быть intent-driven: пользователь формулирует цель, а приложение само создаёт task, выбирает action, ведёт stage machine, обновляет current step и применяет валидные transitions.
- Нельзя принимать сценарий, где пользователь обязан вручную дергать внутренности: `/task start`, `/task move`, `/task step`, `/task expect`, правки storage/JSON/files, прямые записи в memory/task state.
- Такие команды могут оставаться только для inspect/recovery/debug/test, но не как обязательный путь выполнения Day acceptance.
- Acceptance demo must prove agent-driven behavior, not operator-driven FSM manipulation.

### Day 15 lifecycle acceptance remains mandatory

- основной Day 15 flow идёт через один `assistant chat` session, где пользователь пишет обычные сообщения внутри REPL; набор отдельных `assistant chat --once --input ...` команд не является primary demo;
- planning stage должен собрать план и acceptance criteria через planning swarm, где audit/human output показывают role-specific specialist reviews, findings/proposed changes и финальный merged plan;
- переход `planning -> execution` требует пользовательского approval и отдельной application validation записи;
- execution и review должны выполняться role-scoped microtask agents, а не неразличимым provider call;
- trusted verification evidence для acceptance path должно быть выдано приложением автоматически после approval утвержденного плана или semantic intent signal из strict JSON referee: `VerificationResolver` берёт exact command из approved plan/criteria или вызывает structured verification planner/referee для exact argv command, затем локально проверяет allowlist/safety, запускает command и сохраняет evidence record для lifecycle gate;
- пользовательский Day 15 demo не должен требовать `--verify` или точной test command в chat; explicit override допустим только для debug/recovery/regression;
- `done` разрешён только после принятого validation output и criteria-matched app-issued trusted evidence;
- prompt improvement допустим только как сохранение исходной цели и усиление prompt перед stage-specific call.

### Day 11 acceptance remains mandatory

- минимум три типа памяти;
- раздельное хранение;
- LLM memory-classification step;
- подтверждение пользователя;
- inspect commands `/memory short|work|long`;
- следующий ответ учитывает сохранённые данные.

### Day 12 acceptance remains mandatory

- профиль пользователя обязателен;
- profile block подключается к каждому prompt автоматически;
- одинаковый запрос в `student` и `senior` профилях должен давать разное rendered prompt behavior;
- profile нельзя копировать вручную в запрос.

### Day 13 acceptance remains mandatory

- `stage`, `current_step`, `expected_action` обязательны;
- transitions валидируются;
- pause возможен на любом этапе;
- resume восстанавливает context без повторного объяснения;
- `current_step` и `expected_action` сохраняются и видны после restart.

### Day 14 acceptance is mandatory

- invariants are stored in `<storage_root>/invariants/project.jsonl`, separately from dialogue;
- prompt contains `Invariant policy` and `id="invariants.active"`; invariant policy semantically outranks profile, memory, task, and user query;
- conflict request, например `предложи переписать MVP на Python`, is refused before normal chat provider call with `stack.go` evidence after out-of-band invariant validation;
- conflict output is refused as a hard gate before short-memory persistence and memory classifier;
- invariant conflict matching is semantic: an LLM-based structured validator returns invariant violations; `forbidden_terms` are examples/fallback signals, not the final product decision;
- non-conflicting Go request still runs normal Day11/12/13 flow.

### Conflict scenario

Пользователь просит: `предложи переписать MVP на Python`.

Ожидаемое поведение:

- приложение загружает `stack.go` invariant;
- `InvariantValidator` получает input + active invariants и возвращает structured violation;
- normal chat provider call не выполняется;
- пользователь получает error/refusal `invariant_conflict` с `stack.go`, evidence и structured JSON violation data;
- task state, memory и audit не мутируются, кроме process audit rejection.

### Definition of ready for implementation

- canonical contract согласован;
- P0 vertical slice определён;
- provider/storage/privacy/test rules описаны;
- no open high-severity contract gaps.

## 6. Критерии приёмки Day 11

Задание: модель памяти ассистента.

Обязательные требования:

- есть минимум три типа памяти: краткосрочная, рабочая, долговременная;
- разные типы памяти хранятся отдельно;
- сохранение в слой происходит явно через LLM memory-classification step и подтверждение пользователя;
- можно проверить, какие данные попали в каждый слой;
- видно, как слои памяти влияют на ответы ассистента.

Важно: эти критерии обязательны для реализации и не могут быть обойдены через упрощённый flow или manual-only fallback.

Проверка:

- создать сессию и дать ассистенту выделить факт для short-term memory;
- создать задачу и дать ассистенту выделить требование для working memory;
- сообщить предпочтение пользователя и дать ассистенту выделить его для long-term memory;
- подтвердить memory proposal;
- выполнить `/memory short`, `/memory work`, `/memory long`;
- задать следующий вопрос и убедиться, что ответ учитывает сохранённые данные.

## 7. Критерии приёмки Day 12

Задание: персонализация ассистента.

Обязательные требования:

- есть профиль пользователя;
- в профиле описаны стиль, формат и ограничения;
- профиль подключается к каждому запросу;
- можно проверить ответы для разных профилей;
- ассистент учитывает профиль автоматически.

Важно: Day 12 тоже обязателен. Profile block должен попадать в каждый prompt автоматически.

Проверка:

- создать профиль `student`;
- создать профиль `senior`;
- задать одинаковый запрос в обоих профилях;
- убедиться, что ответы различаются по стилю, формату и глубине;
- убедиться, что профиль добавляется в prompt без ручного копирования пользователем.

## 8. Критерии приёмки Day 13

Задание: реализовать состояние задачи как конечный автомат.

Обязательные требования:

- у задачи есть формализованный `stage`;
- у задачи есть `current_step`;
- у задачи есть `expected_action`;
- переходы между этапами валидируются как finite state machine;
- можно поставить задачу на паузу на любом этапе;
- можно продолжить задачу без повторного объяснения контекста.

Важно: Day 13 обязателен. Pause/resume и восстановление состояния должны быть частью implementation scope, а не future extension.

Базовые состояния:

- `planning`;
- `execution`;
- `validation`;
- `done`.

`current_step` описывает конкретный шаг внутри stage. Например:

```text
stage: planning
current_step: сформировать acceptance criteria
expected_action: user_confirmation
```

`expected_action` описывает, чего агент ждёт дальше:

- `user_input`: нужно уточнение от пользователя;
- `llm_response`: нужно сгенерировать следующий ответ;
- `user_confirmation`: нужно подтверждение пользователя;
- `none`: задача завершена или явно не ждёт следующего действия.

Pause/resume contract:

- `planning`, `execution`, `validation`: `/task pause` ставит `status=paused` и сохраняет `stage`, `current_step`, `expected_action`, plan и working memory;
- `done`: `/task pause` является безопасным terminal no-op, не открывает задачу заново и сохраняет `stage=done`, `expected_action=none`;
- `/task resume` восстанавливает контекст только для paused рабочих stages; для `done` возвращает terminal status без продолжения работы.

Проверка:

- создать задачу;
- перевести её в `planning`;
- сохранить `current_step` и `expected_action`;
- поставить задачу на паузу;
- закрыть CLI;
- снова открыть CLI;
- выполнить `/task resume`;
- убедиться, что ассистент восстановил stage, current step, expected action, план и рабочую память;
- продолжить без повторного объяснения задачи.

## 9. Критерии приёмки Day 14

Задание: инварианты и ограничения состояния.

Обязательные требования:

- active invariants хранятся отдельно от диалога, short history и обычной memory;
- prompt builder явно подключает active invariants как higher-priority policy/data block;
- конфликт user request с invariant блокируется до normal chat provider call;
- конфликт provider output с invariant блокируется до memory persistence;
- отказ содержит invariant ID, evidence и понятное объяснение для пользователя;
- semantic conflict решает structured invariant validator, а не простой поиск слов.

Важно: Day 14 обязателен. Invariants не являются подсказками "для сведения"; это hard gate перед provider call и перед persistence.

Проверка:

- создать invariant проекта, например `stack.go`;
- попросить решение, конфликтующее с invariant, например `перепиши MVP на Python`;
- убедиться, что normal chat provider call не выполнен;
- убедиться, что ответ содержит `invariant_conflict`, invariant ID и evidence;
- убедиться, что task state и memory не мутировались, кроме audit rejection;
- задать non-conflicting Go request и убедиться, что обычный Day11/12/13 flow работает.

## 10. Критерии приёмки Day 15

Задание: контролируемые переходы состояний и orchestrated task lifecycle.

Обязательные требования:

- task lifecycle имеет допустимые stages и allowed transitions: `planning`, `execution`, `validation`, `done`;
- приложение не позволяет "перепрыгнуть" этап: implementation до approved plan запрещён, `done` без validation запрещён;
- planning stage запускает prompt improver и planning swarm из 5 независимых specialist agents, после чего orchestrator merge-ит финальный план и acceptance criteria;
- переход `planning -> execution` происходит только после пользовательского approval в chat и отдельной application validation записи;
- execution и review выполняются role-scoped microtask agents с отдельными system prompts и ограниченным context;
- validation использует trusted evidence от приложения, а не самооценку LLM;
- если criteria требуют tests/verification, приложение само получает exact verification command через `VerificationResolver`, проверяет command локальной policy и сохраняет evidence;
- основной пользовательский сценарий не требует `/task move`, `/task step`, `/task expect`, direct storage edits, JSON edits, `--verify` или точной test command от пользователя;
- pause/resume сохраняет stage, current step, expected action, plan, criteria and working memory;
- invalid transition даёт понятный отказ и audit record без повреждения state.

Важно: Day 15 проверяет именно code assistant chat. CLI/debug-команды могут существовать, но acceptance должен доказывать agent-driven lifecycle, а не operator-driven FSM.

Проверка:

- запустить primary Day 15 user-facing flow через один interactive `assistant chat` session без `--json`;
- в live manual demo использовать OpenRouter model `google/gemini-3.1-flash-lite`; fake provider допустим только для deterministic regression;
- дать task goal обычным языком, без `/task start`;
- дождаться planning swarm output с plan и acceptance criteria;
- подтвердить план обычной фразой в chat;
- убедиться, что приложение само перешло в execution и запустило role-scoped agents;
- попросить `проверь и заверши` без `--verify` и без команды `go test ...`;
- убедиться, что приложение само получило exact command через `VerificationResolver`, выполнило только allowlisted verification и сохранило trusted evidence;
- убедиться, что `validation -> done` произошёл только после accepted validation и app-issued trusted evidence;
- проверить audit: prompt improvement, specialist reviews, approval validation, microtask agents, trusted evidence, lifecycle gate decision.

## 11. Memory layers

### 11.1. Short-term memory

Назначение: текущий диалог.

Содержимое:

- последние сообщения пользователя и ассистента;
- временные уточнения внутри текущей сессии;
- факты, которые нужны только до завершения диалога;
- локальный контекст, который можно потерять без вреда для будущих задач.

Правило сохранения:

- добавляется автоматически как история сообщений;
- отдельный LLM memory-classifier может предложить сохранить краткую заметку в `short`;
- ручная команда `/save short ...` добавляет краткую заметку без классификации;
- после закрытия сессии может архивироваться, но не становится долговременной памятью без явного действия.

### 11.2. Working memory

Назначение: данные текущей задачи.

Содержимое:

- цель задачи;
- stage задачи;
- current step;
- expected action;
- pause/resume status;
- текущий план;
- acceptance criteria;
- выбранные файлы и ограничения задачи;
- промежуточные решения;
- открытые вопросы;
- validation status.

Правило сохранения:

- для task-scoped работы приложение создаёт и обновляет task state из natural chat, approved plan, lifecycle gates and process evidence;
- пользователь может явно сохранить рабочий факт через `/save work ...` или подтвердить LLM proposal `[work]`;
- `/task start`, `/task step`, `/task expect` и `/task move` остаются debug/recovery/test helpers, но не являются правилом сохранения для primary Day 15 flow;
- ассистент выделяет требования, acceptance criteria, решения и open questions отдельным memory-classification prompt, когда это именно memory write;
- ассистент не сохраняет пользовательские рабочие факты silently; authoritative process state, plan, criteria, validation and evidence сохраняются process controller после gates;
- при завершении задачи часть working memory можно перенести в long-term decisions.

### 11.3. Long-term memory

Назначение: профиль, решения, знания.

Содержимое:

- стиль общения пользователя;
- предпочитаемый формат ответа;
- технологические ограничения;
- долговременные решения проекта;
- повторно используемые знания;
- запреты и инварианты.

Правило сохранения:

- только явно через `/save long ...`, профильное интервью или подтверждённый LLM proposal `[long]`;
- не хранить секреты, API keys, токены, приватные данные;
- записи должны быть короткими и пригодными для prompt builder.

## 12. LLM memory classification

После каждого значимого ответа ассистент запускает отдельный запрос к той же выбранной модели OpenRouter или к дешёвой модели, выбранной для memory tasks. Перед любым classifier/provider call локальный pre-provider checker обязан заблокировать или отредактировать secret-like данные.

Цель этого запроса: не отвечать пользователю, а структурировать память.

Вход classifier prompt:

- последнее сообщение пользователя;
- последний ответ ассистента;
- active profile;
- current task state;
- текущие правила memory layers;
- запрет сохранять секреты;
- требование вернуть строгий JSON.

Classifier disablement:

- отключение classifier calls допустимо только как privacy/debug/offline режим;
- такой режим не закрывает Day 11 acceptance;
- deterministic tests могут использовать fake provider, но обязаны проходить тот же `MemoryClassifier -> proposal -> user confirmation -> apply` flow.

Выход classifier prompt:

```json
{
  "records": [
    {
      "layer": "work",
      "kind": "requirement",
      "content": "CLI должен поддерживать выбор модели OpenRouter.",
      "reason": "Это требование текущей задачи."
    },
    {
      "layer": "long",
      "kind": "preference",
      "content": "Пользователь предпочитает короткие ответы на русском.",
      "reason": "Это стабильное предпочтение профиля."
    },
    {
      "layer": "ignore",
      "kind": "smalltalk",
      "content": "Пользователь поблагодарил ассистента.",
      "reason": "Не влияет на будущие ответы."
    }
  ]
}
```

Сохранение происходит только после локальной проверки и подтверждения:

1. LLM предлагает records.
2. Invariant checker удаляет или блокирует секреты.
3. CLI показывает memory proposal.
4. Пользователь подтверждает, редактирует или отклоняет.
5. Memory manager сохраняет records в отдельные хранилища.

Так выполняется критерий `вы явно выбираете, что и куда сохраняется`: выбор делает LLM в отдельном классификационном шаге, а приложение делает этот выбор видимым и проверяемым.

## 13. Персонализация

Профиль пользователя должен состоять минимум из трёх групп данных:

- `style`: кратко или подробно, тон, язык ответа, степень объяснений;
- `format`: списки, пошаговый план, кодовые примеры, структура ответа;
- `constraints`: стек, запреты, правила проекта, границы домена.

Пример профиля:

```json
{
  "id": "student",
  "displayName": "Student profile",
  "style": {
    "language": "ru",
    "detail": "high",
    "tone": "teacher"
  },
  "format": {
    "preferSteps": true,
    "preferExamples": true,
    "avoidLongTheory": false
  },
  "constraints": [
    "Объяснять термины при первом использовании",
    "Не пропускать архитектурные причины решений"
  ]
}
```

## 14. CLI interface

Минимальные команды запуска:

```text
assistant init
assistant chat
assistant chat --once --input <text>
assistant chat --once --input <text> --json
assistant chat --once --input <text> --verify "<argv command>" --json   # debug/recovery override only
assistant chat --once --render-prompt --input <text> --json
assistant chat --profile student --model openai/gpt-4.1-mini
assistant profiles [list]
assistant profiles show [id]
assistant profiles set <id>
assistant profiles create <id> [--display-name <name>] [--style k=v] [--format k=v] [--constraint <text>]
assistant memory list <short|work|long> [--json] [--session <id>] [--task <id>] [--all-profiles]
assistant memory propose [--latest] [--json]
assistant memory apply [--proposal <id>] --accept all [--json]
assistant memory proposals [--session <id>] [--json]
assistant task status --json
assistant task pause
assistant task resume
assistant task start <title>          # debug/recovery/test helper
assistant task move <stage>           # debug/recovery/test helper
assistant task step <text>            # debug/recovery/test helper
assistant task expect <action>        # debug/recovery/test helper
assistant process audit [--latest|--limit <n>] [--json]
assistant privacy
assistant privacy purge --audit [--transcripts] --yes
```

Команды внутри диалога:

```text
/help                         Показать команды
/model                        Выбрать модель
/profile                      Переключить профиль
/profile create               Создать профиль
/task start <title>           Debug: создать task вручную
/task status                  Показать состояние задачи
/task step <text>             Debug: установить current step вручную
/task expect <action>         Debug: установить expected action вручную
/task move <stage>            Debug: перейти в stage вручную
/task plan <text>             Добавить пункт плана в REPL
/task criteria <text>         Добавить acceptance criterion в REPL
/task pause                   Поставить задачу на паузу
/task resume                  Продолжить задачу после паузы
/save short <text>            Сохранить в short-term memory
/save work <text>             Сохранить в working memory
/save long <text>             Сохранить в long-term memory
/memory propose               Запустить LLM-классификацию памяти вручную
/memory apply                 Сохранить последний memory proposal
/memory short                 Показать short-term memory
/memory work                  Показать working memory
/memory long                  Показать long-term memory
/process audit                Показать последний process audit event
/privacy                      Показать privacy/provider/storage summary
/clear short                  Очистить краткосрочный слой текущей сессии
/exit                         Завершить CLI
```

Current P0 command boundary:

- user-facing P0 включает `assistant chat`, `assistant chat --once --input <text>`, `assistant chat --once --input <text> --json`, `assistant chat --once --render-prompt --input <text> --json`, `assistant memory list|propose|apply|proposals`, `assistant profiles list|show|set|create`, `assistant task status|pause|resume`, `assistant process audit`, `assistant privacy`;
- user-facing Day 15 P0 path использует один natural `assistant chat` REPL session; если acceptance criteria требуют test/verification evidence, приложение запускает `VerificationResolver` после user approval утвержденного плана или strict semantic check/finish intent, затем выполняет только locally allowlisted argv command;
- `assistant chat --once --input <text> --verify "<argv command>" --json` является explicit debug/recovery override, а не happy path и не обязательная пользовательская команда;
- `assistant task start|move|step|expect` и matching slash commands остаются debug/recovery/test helpers, не acceptance path;
- `/task plan` и `/task criteria`, а также top-level `assistant task plan|criteria`, реализованы как conveniences для plan/acceptance criteria;
- `/task done`, `/task stage`, `/task decision` и top-level `assistant task done|stage|decision` не реализованы в текущем коде.

### 14.1. CLI chat UX/UI contract

User-facing chat output must be readable by default. Raw process JSON is an internal contract and an automation format, not the default human interface.

Default human output:

- `assistant chat` and `assistant chat --once --input <text>` print a structured transcript, not raw JSON;
- output is grouped into visible sections: `Assistant`, `Task`, `Transition`, `Evidence`, `Warnings`, `Memory proposal`, `Next`;
- internal stage schemas are rendered into human text: planning summary/criteria/plan, execution summary/deliverable/next step, validation findings/checks/verdict, done summary/status;
- planning output includes a visible `Planning swarm` review with each specialist role, concrete verdict/contribution, finding count, top finding and proposed plan/criteria changes when present; it must not degrade into specialists merely restating the user task, and the user must not inspect audit JSON to understand what agents discussed;
- task state is compact: `stage`, `expected_action`, `current_step`, `status`, validation status and microtask count;
- trusted verification is shown as a short evidence summary, for example `auto verification: go test ./pkg` and evidence ref count, not full evidence records;
- memory proposal output lists proposed records in a compact table-like format and gives the next confirmation command after the records;
- errors print `code: message` plus `hint`, never a JSON envelope unless `--json` is explicitly set;
- long lists are bounded in human mode; full raw details stay available through `--json`, `task status --json` and `process audit --json`.

Color and terminal behavior:

- when stdout is an interactive terminal and `NO_COLOR`/`ASSISTANT_NO_COLOR` are not set, headings and labels use ANSI highlighting; state/evidence/warnings remain grouped in readable sections;
- fenced code blocks use syntax-aware terminal highlighting for supported languages, including Go, in interactive color mode;
- non-TTY output, redirected files and tests must not contain ANSI codes;
- `--quiet` suppresses nonessential diagnostics but does not remove the main answer;
- `--json` remains stable machine-readable output for tests/scripts and may include raw answer schema.

Day 15 UX acceptance:

- during planning the user sees the proposed plan and acceptance criteria as readable lists;
- after approval the user sees execution deliverable and next step without reading raw JSON;
- during validation the user sees findings, passed checks, missing evidence and verdict as sections;
- when lifecycle moves, the user sees `from -> to` and the new expected action;
- no primary Day 15 demo step requires the user to inspect JSON to know what happened.

## 15. OpenRouter requirements

Требования:

- поддержать `OPENROUTER_API_KEY` из environment;
- для MVP считать env-only policy канонической; hidden input или local key file не должны стать default path без отдельного threat model;
- не сохранять ключ в git, config, profiles, memory files, prompt audit или audit trail;
- проверять chat/classifier payload локальным pre-provider secret scanner до отправки в OpenRouter;
- дать выбрать модель из списка или ввести model id;
- хранить выбранную модель в локальном config;
- позволять сменить модель через `/model`;
- использовать OpenRouter не только для основного ответа, но и для memory-classification prompt;
- явно сообщать пользователю, какие категории данных отправляются во внешний provider;
- custom base URL поддерживается через `--openrouter-base-url` или `ASSISTANT_OPENROUTER_BASE_URL`; non-default URL требует сохранённого trust list или one-shot флаг `--trust-openrouter-base-url`;
- raw transcripts не пишутся в P0; rendered prompts сохраняются как metadata/hash by default, а raw prompt audit включается только через `ASSISTANT_RAW_PROMPT_AUDIT=1`; purge доступен через `assistant privacy purge --audit --yes`.

Минимальная интеграция:

- endpoint: `https://openrouter.ai/api/v1/chat/completions`;
- model id передаётся в поле `model`;
- prompt отправляется как chat messages;
- ошибки API показываются в CLI без падения приложения.

## 16. Prompt builder

Prompt builder должен собирать контекст слоями, а не добавлять всё подряд.

Базовый порядок блоков:

1. System role ассистента.
2. Правила безопасности и запрет сохранять секреты.
3. Trusted process-control policy: приложение владеет state, transitions, memory writes and validation.
4. Trusted stage-specific policy: роль этапа, allowed actions, forbidden actions and output schema.
5. Активный профиль пользователя.
6. Инварианты профиля и проекта.
7. Task state: stage, current step, expected action, status, allowed transitions.
8. Working memory текущей задачи.
9. Релевантная long-term memory.
10. Short-term history текущего диалога.
11. Текущий запрос пользователя.

Важно:

- профиль подключается к каждому запросу;
- short-term history ограничивается размером окна;
- long-term memory подмешивается выборочно;
- task state имеет больший приоритет, чем рабочая память и случайная история диалога;
- агент должен выполнять работу, соответствующую текущему stage и expected action;
- code assistant должен знать текущий этап и свою роль на этом этапе до provider call;
- validation/review stage должен быть prompt-level ролью ревьюера, а не обычным generic assistant ответом.

Контракт безопасности:

- profile, memory, task state, short history, prompt audit data и classifier output являются untrusted data;
- такие блоки должны быть serialized/quoted/tagged как данные, а не инструкции;
- canonical prompt schema должен задавать block id, block type, source, trust label, escaping rules и порядок блоков;
- system/application/security policy всегда выше пользовательского и сохранённого контекста.
- trusted stage policy не заменяет profile block: Day 12 по-прежнему требует active profile в каждом prompt.

## 17. State machine

Для Day 13 MVP обязан хранить состояние задачи как конечный автомат.

Task state состоит минимум из:

- `stage`: этап задачи;
- `current_step`: конкретный шаг внутри этапа;
- `expected_action`: что должно произойти дальше;
- `status`: `active` или `paused`;
- `updated_at`: время последнего изменения.

Базовые стадии:

- `planning`: планирование;
- `execution`: выполнение;
- `validation`: проверка;
- `done`: завершение.

Разрешённые переходы:

- `planning -> execution`;
- `execution -> validation`;
- `validation -> execution`;
- `validation -> done`;
- `execution -> planning`, если появились новые требования.

Пауза не является отдельной стадией. Это `status=paused` поверх любого stage. При resume ассистент восстанавливает последний stage, current step, expected action, working memory и продолжает без повторного объяснения.

`status=done` не используется в MVP; completion фиксируется через `stage=done` и `expected_action=none`.

Process-control rule: LLM не выбирает текущий stage сама. Приложение читает persisted `TaskState`, выбирает stage-specific policy, сообщает модели роль этапа и принимает или отклоняет output. Любой переход stage проходит через deterministic allowed transitions и application gate.

Пример task state:

```json
{
  "stage": "planning",
  "current_step": "сформировать архитектурный план MVP",
  "expected_action": "user_confirmation",
  "status": "active"
}
```

## 18. Инварианты

Инварианты нужны как постоянные ограничения, которые не должны теряться между запросами.

Примеры для этого ассистента:

- не сохранять секреты в память;
- не коммитить и не менять файлы без явной команды пользователя;
- не смешивать memory layers;
- не сохранять LLM memory proposal без показа пользователю;
- не подменять профиль пользователя текущим запросом;
- не переходить между stage без проверки allowed transitions;
- не продолжать paused-задачу без восстановления current_step и expected_action;
- не игнорировать ограничения active profile;
- при конфликте user request и long-term constraints явно сообщить о конфликте.

Инварианты проверяются отдельным LLM-вызовом со строгим JSON-ответом. Локальные проверки допустимы только для hard gates: секреты, небезопасные значения, ошибки схемы, fallback или prefilter. Смысловой конфликт с правилом нельзя решать простым поиском слов.

## 19. Non-goals MVP

В MVP/P0 не нужно делать перечисленное ниже. Это не product non-goals; для аналога Claude Code / Codex CLI часть этих пунктов является обязательным P1/P2 roadmap.

- полноценный IDE agent с глубокими IDE integrations;
- автоматическое редактирование файлов как default path без approval/tool safety layer;
- RAG по всему репозиторию как production-quality subsystem;
- vector database;
- general-purpose multi-agent orchestration beyond the Day 15 planning/execution/validation control loop;
- web UI;
- сложную авторизацию;
- автоматическую долговременную память без подтверждения;
- silent memory writes без LLM proposal и без audit trail.

## 20. Метрики готовности

MVP считается готовым, если:

- CLI запускается и отвечает через OpenRouter;
- модель можно выбрать в интерфейсе;
- три memory layers физически разделены;
- LLM memory-classifier предлагает, какие факты в какой слой сохранить;
- пользователь видит и подтверждает memory proposal;
- содержимое каждого слоя можно вывести;
- профиль подключается к каждому prompt;
- два разных профиля дают разные ответы на один запрос;
- задача хранит stage, current step и expected action;
- LLM получает stage-aware prompt с ролью текущего этапа;
- задачу можно поставить на паузу и продолжить без повторного объяснения;
- приложение autonomously ведёт lifecycle без ручных `/task move|step|expect` в основном flow;
- `done` требует accepted validation и app-issued trusted evidence, когда criteria упоминают tests/verification;
- основной Day 15 demo проходит без `--verify` и без точной test command от пользователя;
- planning swarm, prompt improver, microtask agents и process audit доступны как контролируемые Day 15 primitives;
- API key не попадает в репозиторий и memory files;
- invariant checker и validation loop реализованы как обязательная часть current MVP.

P1 readiness after MVP:

- user-facing chat can inspect repository files through controlled read tools;
- assistant can propose and apply patches with explicit permission gates;
- assistant can run allowlisted shell/test commands and attach trusted evidence automatically;
- task flow remains inside `assistant chat`, not debug commands;
- diffs, test failures and recovery steps are readable in the chat transcript.

## 21. Test and eval matrix

Реализация должна иметь детерминированный набор проверок, который подтверждает Day 11/12/13/14/15 без reliance on live LLM eyeballing.

Минимум:

- fake provider fixtures;
- golden prompt rendering tests;
- storage failure/corruption tests;
- provider timeout/error tests;
- pre-provider secret leak tests for chat and classifier payloads;
- duplicate proposal apply tests;
- forbidden transition tests;
- golden prompt tests for planning/execution/validation/done stage roles;
- process gate tests proving wrong-stage output is rejected before accepted persistence;
- lifecycle gate tests for approval validation, trusted evidence and validation-to-done;
- Day 15 manual chat-driven script proof;
- race/locking/atomic-write/symlink/path-safety tests for file storage;
- non-interactive CLI JSON/exit-code smoke tests;
- prompt-injection/redaction tests for profile/memory/task/short history/prompt audit.

Каждый обязательный Day 11/12/13/14/15 критерий должен иметь либо автоматический тест, либо явно отмеченный manual demo proof.

## 22. Roadmap недели

Day 11:

- memory storage;
- short-term, working, long-term layers;
- LLM memory-classification prompt;
- команды `/save`, `/memory propose`, `/memory apply`, `/memory`;
- демонстрация влияния памяти на ответ.

Day 12:

- user profiles;
- profile interview;
- подключение профиля к каждому prompt;
- проверка разных ответов для разных профилей.

Day 13:

- task state machine;
- stage, current step, expected action;
- allowed transitions;
- pause/resume на любом этапе;
- продолжение без повторного объяснения.

Day 14:

- active invariants in separate storage;
- invariant policy in prompt;
- semantic invariant validation before provider call and persistence;
- conflict refusal with invariant ID and evidence;
- non-conflicting requests continue normal flow.

Day 15:

- prompt improver for task goals;
- planning swarm with 5 role-specific specialist reviews and merged plan;
- user approval validation before execution;
- role-scoped execution/review microtask agents;
- automatic trusted verification via language-agnostic `VerificationResolver` after approved-plan execution starts or semantic `ready_for_validation`/`ready_for_done` intent signal;
- lifecycle gates for `planning -> execution -> validation -> done`;
- chat-first manual demo without `--verify` or user-supplied test command.
