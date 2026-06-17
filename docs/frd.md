# FRD: минимальный CLI code assistant

## 1. Назначение документа

FRD описывает функциональные требования к MVP CLI code assistant. Документ дополняет `prd.md` и `architect.md`: PRD объясняет цель продукта, architecture описывает устройство системы, FRD фиксирует конкретное поведение функций и проверяемые требования.

Жёсткие критерии приёмки берутся из:

- `day11.md`: явная модель памяти с отдельными слоями;
- `day12.md`: персонализация через профиль пользователя;
- `day13.md`: состояние задачи как конечный автомат.

## 2. Scope MVP

MVP должен реализовать:

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
- pause/resume задачи без повторного объяснения контекста.

MVP не должен реализовывать:

- автоматическое редактирование файлов проекта;
- полноценный IDE agent;
- RAG по репозиторию;
- vector database;
- multi-agent workflow;
- web UI;
- silent long-term memory writes.

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

## 4. Пользовательские роли

`User` — человек, который запускает CLI, выбирает профиль и модель, задаёт вопросы, подтверждает memory proposal и управляет задачами.

`Assistant` — CLI-приложение, которое вызывает OpenRouter, собирает prompt, хранит состояние и показывает ответы.

`LLM Provider` — OpenRouter-compatible API, который возвращает ответы и memory classification JSON.

## 5. Functional Requirements

### Canonical contract

FRD is source of truth for the implementation contract together with PRD and architecture. The three documents must not disagree on Day 11, Day 12, or Day 13 semantics.

#### Task state canonical values

- `stage`: `planning`, `execution`, `validation`, `done`.
- `status`: `active`, `paused`.
- `expected_action`: `user_input`, `llm_response`, `tool_result`, `user_confirmation`, `none`.
- `tool_result` remains in MVP only if the Day 13 flow truly needs it; otherwise remove it from all docs.
- completion is represented by `stage=done` and `expected_action=none`; `status=done` is not part of MVP.

#### Command contract

- top-level and slash commands must map to one canonical command tree;
- P0 commands are only the ones required for Day 11/12/13 demo and smoke tests.

#### Memory layers

- physical storage layers: `short`, `work`, `long`;
- `ignore` exists only in memory proposal and audit trail;
- all examples and tests must use the same layer names.

### FR-001. Первый запуск

Требование: приложение должно поддерживать первый запуск без существующей конфигурации.

Поведение:

- CLI проверяет наличие runtime storage;
- CLI создаёт runtime storage, если его нет;
- CLI проверяет наличие активного профиля;
- CLI запускает создание профиля, если профилей нет;
- CLI проверяет наличие выбранной модели;
- CLI предлагает выбрать или ввести модель, если модель не задана.

MVP policy:

- default config/storage root must be documented explicitly;
- env vars override config;
- hidden input or local key file is not the default API key flow for MVP.

Acceptance criteria:

- запуск без `.assistant/` не падает;
- после `assistant init` появляется `config.json`;
- после `assistant init` появляется минимум один profile JSON;
- API key не записывается в `config.json`.

### FR-002. OpenRouter API key

Требование: приложение должно использовать OpenRouter API key безопасно.

Поведение:

- CLI читает `OPENROUTER_API_KEY` из environment;
- если ключ отсутствует, CLI сообщает пользователю, что ключ нужен;
- ключ не должен сохраняться в docs, profiles, memory files, transcripts, audit trail или config;
- при 401/403 CLI показывает понятную ошибку.

Privacy requirements:

- classify what categories of data are sent to the provider;
- allow disabling classifier calls or using a safe fallback path for P0;
- document timeout and retry behavior.

Acceptance criteria:

- при отсутствии ключа запрос к модели не выполняется;
- при неверном ключе пользователь видит ошибку авторизации;
- поиск по `.assistant/` не должен находить `OPENROUTER_API_KEY` или bearer token.

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

- `assistant chat` запускает REPL;
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

- `/profile create` создаёт файл `.assistant/profiles/<id>.json`;
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

- prompt blocks from profile/memory/task/transcript/classifier are untrusted data and must be serialized/quoted/tagged as such.

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

Acceptance criteria:

- classifier может вернуть `short`, `work`, `long`, `ignore`;
- invalid JSON приводит к понятной ошибке или retry;
- proposal показывается пользователю до сохранения;
- proposal audit сохраняет, что LLM предложила.

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
- допустимые values: `user_input`, `llm_response`, `tool_result`, `user_confirmation`, `none`;
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
3. Active profile.
4. Invariants.
5. Task state: stage, current_step, expected_action, status, allowed transitions.
6. Working memory.
7. Selected long-term memory.
8. Short-term history.
9. Current user query.

Acceptance criteria:

- task state идёт раньше working memory;
- profile block присутствует в каждом prompt;
- short-term history ограничивается размером окна;
- PromptBuilder не пишет файлы и не вызывает OpenRouter.

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

- redact before persistence and before provider calls when feasible;
- store secret fingerprints or types instead of raw values.

Acceptance criteria:

- строка с `OPENROUTER_API_KEY=` не сохраняется;
- bearer token не сохраняется;
- blocked secret виден в audit как blocked без раскрытия полного секрета.

### FR-028. Local storage

Требование: runtime data должна храниться локально и прозрачно.

Поведение:

- config хранится в `.assistant/config.json`;
- profiles хранятся в `.assistant/profiles/*.json`;
- sessions хранятся в `.assistant/sessions/<session_id>/`;
- tasks хранятся в `.assistant/tasks/`;
- long-term memory хранится в `.assistant/long_term/`;
- logs хранятся в `.assistant/logs/`.

Acceptance criteria:

- пользователь может открыть файлы и увидеть memory layers;
- task state сохраняется между запусками;
- `.assistant/` должен быть gitignored при реализации.

Storage rule:

- storage writes must be canonical-path safe, atomic, and recoverable;
- path traversal, unsafe IDs, and symlink writes must be rejected.

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
- stdout is for primary data, stderr for diagnostics.

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
- `/task pause` работает на любом этапе;
- `/task resume` восстанавливает контекст;
- пользователь продолжает задачу без повторного объяснения.

Traceability rule:

- Day 13 is mandatory and may not be bypassed.

## 6. CLI commands

Обязательные команды запуска:

```text
assistant init
assistant chat
assistant chat --profile <profile_id> --model <model_id>
assistant profiles
assistant memory
```

Обязательные команды внутри chat:

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
/clear short
/exit
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
- Restart CLI не теряет profile, memory и task state.
- Secrets не сохраняются.
- Day 11, Day 12 и Day 13 criteria закрыты.
