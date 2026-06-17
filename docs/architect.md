# Architecture: минимальный CLI code assistant

## 1. Архитектурная идея

Ассистент строится как маленький stateful CLI-agent.

Ключевой принцип: LLM не должна сама решать, что помнить и какие правила важны. Приложение хранит состояние явно, разделяет memory layers, собирает prompt через prompt builder и постепенно добавляет deterministic checks.

Главные блоки:

- CLI interface;
- OpenRouter provider;
- memory manager;
- profile manager;
- task state manager;
- prompt builder;
- response loop;
- invariant checker.

## 2. Рекомендуемый стек

Для MVP подходит Python 3.11+.

Причины:

- быстрый CLI prototype;
- простая работа с JSON/Markdown files;
- удобный HTTP client для OpenRouter;
- легко добавить pydantic-модели и тесты;
- низкий порог для учебного проекта.

Минимальные зависимости:

- `typer` для CLI commands;
- `rich` для удобного терминального UI;
- `httpx` для HTTP запросов;
- `pydantic` для моделей данных;
- `python-dotenv` опционально для локального `.env`.

Если хочется ещё меньше зависимостей, CLI можно начать на `argparse`, HTTP на `urllib.request`, а JSON на стандартной библиотеке. Архитектура от этого не меняется.

## 3. Файловая структура приложения

Предлагаемая структура:

```text
src/cli_assistant/
  __init__.py
  main.py
  cli.py
  config.py
  models.py

  providers/
    __init__.py
    base.py
    openrouter.py

  memory/
    __init__.py
    manager.py
    short_term.py
    working.py
    long_term.py
    storage.py

  profiles/
    __init__.py
    manager.py
    interview.py

  tasks/
    __init__.py
    state_machine.py
    manager.py

  prompting/
    __init__.py
    builder.py
    templates.py

  validation/
    __init__.py
    invariants.py
    redaction.py

tests/
  test_memory_layers.py
  test_profiles.py
  test_prompt_builder.py
  test_state_machine.py
```

## 4. Runtime storage

Runtime data лучше хранить отдельно от исходного кода. Для учебного проекта можно использовать локальную папку `.assistant/` в корне репозитория или пользовательскую папку `~/.local/share/minicli-assistant/`.

Для этого проекта удобнее `.assistant/`, но её нужно добавить в `.gitignore` при реализации.

Структура storage:

```text
.assistant/
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
- если нужен локальный файл с ключом, он должен быть вне git и явно игнорироваться;
- memory manager обязан редактировать или отклонять секреты перед сохранением.

## 5. Data models

### 5.1. ChatMessage

```python
class ChatMessage(BaseModel):
    role: Literal["system", "user", "assistant"]
    content: str
    created_at: datetime
```

Назначение: единый формат сообщений для OpenRouter и short-term history.

### 5.2. MemoryRecord

```python
class MemoryRecord(BaseModel):
    id: str
    layer: Literal["short", "work", "long"]
    kind: str
    content: str
    source: Literal["user", "assistant", "system"]
    tags: list[str] = []
    created_at: datetime
```

Назначение: универсальная запись памяти. `layer` определяет физическое хранилище.

### 5.3. UserProfile

```python
class UserProfile(BaseModel):
    id: str
    display_name: str
    style: dict[str, Any]
    response_format: dict[str, Any]
    constraints: list[str]
    default_model: str | None = None
    created_at: datetime
    updated_at: datetime
```

Назначение: персонализация. Этот объект подключается к каждому prompt.

### 5.4. TaskState

```python
class TaskState(BaseModel):
    id: str
    title: str
    stage: Literal["intake", "planning", "execution", "validation", "done"]
    objective: str
    acceptance_criteria: list[str]
    plan: list[str]
    decisions: list[str]
    open_questions: list[str]
    validation_status: str | None = None
    updated_at: datetime
```

Назначение: рабочее состояние задачи. Это ядро future day13-15.

### 5.5. AppConfig

```python
class AppConfig(BaseModel):
    active_profile_id: str | None = None
    active_model: str | None = None
    storage_dir: Path
    openrouter_base_url: str = "https://openrouter.ai/api/v1"
```

Назначение: локальные настройки без секретов.

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

- получение API key из environment или secret provider;
- запрос списка моделей;
- вызов `/chat/completions`;
- нормализацию ошибок OpenRouter;
- возврат текста ответа в общий формат.

Интерфейс:

```python
class LlmProvider(Protocol):
    def list_models(self) -> list[str]: ...
    def complete(self, model: str, messages: list[ChatMessage]) -> ChatMessage: ...
```

Так можно позже заменить OpenRouter на другой provider.

### 6.3. MemoryManager

Отвечает за:

- запись в нужный слой;
- чтение слоя;
- очистку short-term memory;
- выбор релевантных записей для prompt;
- запрет сохранения секретов;
- перенос части working memory в long-term после завершения задачи.

Публичные методы:

```python
save(layer: MemoryLayer, content: str, kind: str, source: str) -> MemoryRecord
list(layer: MemoryLayer) -> list[MemoryRecord]
clear_short(session_id: str) -> None
select_for_prompt(profile_id: str, task_id: str | None) -> MemoryBundle
```

### 6.4. ProfileManager

Отвечает за:

- создание профиля;
- короткое интервью при первом запуске;
- переключение active profile;
- обновление style, format, constraints;
- сериализацию профиля в prompt-friendly текст.

Профиль не должен смешиваться с обычной историей чата. Он хранится отдельно и подключается каждый раз.

### 6.5. TaskStateManager

Отвечает за:

- создание текущей задачи;
- хранение stage;
- обновление plan, decisions, acceptance criteria;
- проверку allowed transitions;
- выдачу task context для prompt builder.

MVP может начать с мягкой проверки: предупреждать о неправильном переходе. Следующий шаг: блокировать переход кодом.

### 6.6. PromptBuilder

Отвечает за сборку prompt.

Вход:

- base system prompt;
- active profile;
- task state;
- memory bundle;
- short-term messages;
- user query.

Выход:

- `list[ChatMessage]` для OpenRouter.

PromptBuilder должен быть чистым компонентом: без HTTP, без записи файлов, без побочных эффектов.

### 6.7. InvariantChecker

Отвечает за:

- поиск секретов перед сохранением памяти;
- проверку конфликтов профиля и user request;
- предупреждения о нарушении stage;
- будущую validation loop после ответа LLM.

MVP-инварианты:

- API keys не сохраняются в memory;
- long-term memory сохраняется только явно;
- профиль подключён к каждому prompt;
- layer записи совпадает с выбранной командой `/save`.

## 7. Memory flow

### 7.1. Входящий запрос

```text
User input
  -> CLI parses command or normal message
  -> ProfileManager loads active profile
  -> TaskStateManager loads current task
  -> MemoryManager selects memory bundle
  -> PromptBuilder builds messages
  -> OpenRouterProvider.complete()
  -> CLI prints assistant answer
  -> short-term history appends user + assistant messages
```

### 7.2. Явное сохранение

```text
/save work Требование: поддержать выбор модели
  -> CLI parses layer=work
  -> InvariantChecker checks redaction
  -> MemoryManager.save(work, content)
  -> WorkingMemoryStorage writes record
  -> CLI prints saved record id
```

### 7.3. Подключение памяти к prompt

Prompt включает не всю память, а выбранный bundle:

```text
System rules
Active user profile
Project/profile invariants
Current task state
Working memory records
Selected long-term records
Recent short-term messages
Current user message
```

Это защищает от анти-паттерна `всё в один prompt`.

## 8. Profile flow

### 8.1. Создание профиля

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

### 8.2. Использование профиля

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

## 9. Task state machine

Состояния:

```text
intake -> planning -> execution -> validation -> done
                     -> planning
          execution <- validation
```

Allowed transitions:

```python
ALLOWED_TRANSITIONS = {
    "intake": {"planning"},
    "planning": {"execution"},
    "execution": {"validation", "planning"},
    "validation": {"execution", "done"},
    "done": set(),
}
```

MVP commands:

```text
/task start <title>
/task stage
/task move planning
/task plan <item>
/task criteria <item>
/task decision <item>
/task done
```

Если пользователь просит перейти из `intake` сразу в `execution`, менеджер должен предупредить: нужен `planning`. Позже можно сделать жёсткий отказ.

## 10. Prompt templates

### 10.1. Base system prompt

```text
You are a minimal CLI code assistant.
Follow active user profile, task state, memory layers, and invariants.
Do not claim memory was saved unless the application saved it.
Do not store secrets.
If user request conflicts with active constraints, explain the conflict.
```

### 10.2. Memory instruction

```text
Memory policy:
- short-term memory is current session context;
- working memory is current task context;
- long-term memory is stable user/project knowledge;
- never move facts between layers without explicit user confirmation.
```

### 10.3. Stage instruction

```text
Current task stage: {stage}
Allowed next stages: {allowed_next_stages}
Do work appropriate for this stage only.
```

## 11. Storage format

### 11.1. JSONL memory records

`long_term/decisions.jsonl`:

```json
{"id":"mem_001","layer":"long","kind":"decision","content":"Use OpenRouter as LLM provider for MVP.","source":"user","tags":["provider"],"created_at":"2026-06-17T09:00:00Z"}
```

JSONL удобен для append-only истории и простой отладки.

### 11.2. Task JSON

`tasks/current.json`:

```json
{
  "id": "task-001",
  "title": "CLI assistant MVP",
  "stage": "planning",
  "objective": "Implement memory layers and personalization",
  "acceptance_criteria": [
    "three memory layers are stored separately",
    "active profile is attached to every prompt"
  ],
  "plan": [],
  "decisions": [],
  "open_questions": [],
  "validation_status": null
}
```

### 11.3. Profile JSON

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

## 12. Error handling

Обязательные ошибки:

- нет OpenRouter API key;
- OpenRouter вернул 401/403;
- модель не найдена;
- network timeout;
- повреждён JSON storage;
- выбранный профиль отсутствует;
- нет активной задачи для `/save work`;
- попытка сохранить секрет в memory.

Поведение:

- показывать короткую понятную ошибку;
- не терять текущий ввод пользователя;
- не падать stack trace в обычном режиме;
- писать подробности в log только без секретов.

## 13. Security and privacy

Правила:

- не хранить API key в `docs/`, memory files, profiles, transcripts;
- редактировать строки вида `sk-...`, `OPENROUTER_API_KEY=...`, bearer tokens;
- `.assistant/` не коммитить;
- transcript может содержать пользовательские данные, поэтому он local-only;
- перед сохранением long-term memory показывать пользователю, что именно будет записано.

## 14. Testing strategy

Day 11 tests:

- `test_save_short_memory`;
- `test_save_working_memory`;
- `test_save_long_term_memory`;
- `test_layers_are_separate_files`;
- `test_prompt_builder_uses_selected_layers`.

Day 12 tests:

- `test_create_profile`;
- `test_switch_profile`;
- `test_profile_attached_to_prompt`;
- `test_same_query_different_profiles_change_prompt`.

State tests:

- `test_allowed_transition`;
- `test_forbidden_transition_warns_or_fails`.

Security tests:

- `test_openrouter_key_not_saved_to_memory`;
- `test_secret_redaction_before_save`.

## 15. Implementation order

1. Project skeleton and config loading.
2. OpenRouter provider with manual model id.
3. Basic `assistant chat` loop.
4. Storage layer with JSON/JSONL.
5. Short-term memory history.
6. Working memory and `/task start`.
7. Long-term memory and `/save long`.
8. `/memory short|work|long` inspection.
9. Profile manager and first-run interview.
10. Prompt builder with profile attached to every request.
11. Model selection UI and `/model` command.
12. Invariant checker for secrets and memory routing.
13. State machine transition checks.
14. Tests for day11/day12 acceptance criteria.

## 16. Future extensions

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

## 17. Главный архитектурный инвариант

Ассистент не является просто оболочкой вокруг OpenRouter. Его ценность в том, что приложение управляет состоянием: профиль, память, задача, стадии и ограничения существуют вне LLM и подаются в модель контролируемо.
