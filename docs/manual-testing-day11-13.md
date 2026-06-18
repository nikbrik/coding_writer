# Ручное тестирование дней 11-14

Цель: проверить дни 11, 12, 13 и 14 максимально похоже на обычный пользовательский опыт. Один раз настраиваем команду `assistant`, добавляем ее в `PATH`, задаем понятный path для данных и дальше работаем через интерактивный чат с короткими slash-командами.

## 1. Предварительная Настройка

### 1.1. Задать path проекта

```bash
export CW_ROOT="/Users/nikita/code/coding_writer"
cd "$CW_ROOT"
pwd
```

Ожидаемо:

- вывод: `/Users/nikita/code/coding_writer`;
- все следующие команды запускаются из корня проекта.

### 1.2. Собрать `assistant` и добавить в `PATH`

```bash
mkdir -p "$CW_ROOT/.assistant/bin"
go build -o "$CW_ROOT/.assistant/bin/assistant" ./cmd/assistant
export PATH="$CW_ROOT/.assistant/bin:$PATH"
which assistant
assistant --help
```

Ожидаемо:

- `which assistant` показывает `/Users/nikita/code/coding_writer/.assistant/bin/assistant`;
- `assistant --help` показывает справку CLI;
- дальше не нужно писать `go run ./cmd/assistant`, достаточно писать `assistant`.

Важно:

- этот `PATH` задан только для текущего терминала;
- если откроете новый терминал, повторите команды из этого блока;
- `.assistant/` уже игнорируется git, поэтому бинарь и локальные данные не попадут в коммит.

### 1.3. Задать path хранилища

```bash
export ASSISTANT_STORAGE_DIR="$CW_ROOT/.assistant/storage/manual-day11-14"
mkdir -p "$ASSISTANT_STORAGE_DIR"
```

Ожидаемо:

- данные ручного теста будут лежать в `/Users/nikita/code/coding_writer/.assistant/storage/manual-day11-14`;
- память, задачи, профили и audit будут сохраняться между запусками `assistant chat`;
- это удобнее, чем `mktemp`, потому что после перезапуска терминала можно вернуться к тому же состоянию.

Если нужен полностью чистый прогон, задайте другой path, например:

```bash
export ASSISTANT_STORAGE_DIR="$CW_ROOT/.assistant/storage/manual-day11-14-run2"
mkdir -p "$ASSISTANT_STORAGE_DIR"
```

### 1.4. Подключить OpenRouter key и модель

```bash
export OPENROUTER_API_KEY="ваш_ключ_OpenRouter"
export ASSISTANT_MODEL="deepseek/deepseek-v4-flash"
test -n "$OPENROUTER_API_KEY" && echo "OPENROUTER_API_KEY set"
```

Ожидаемо:

- вывод: `OPENROUTER_API_KEY set`;
- ключ не выводится в терминал полностью;
- модель можно заменить на любую доступную в вашем OpenRouter аккаунте.

Важно:

- не пишите ключ в чат, профиль, память или название задачи;
- приложение читает ключ из env и не сохраняет его в config;
- реальные запросы к OpenRouter могут тратить деньги.

Для сухого прогона без OpenRouter можно использовать deterministic fake provider:

```bash
export ASSISTANT_PROVIDER=fake
export ASSISTANT_MODEL="fake/model"
```

В fake mode ключ `OPENROUTER_API_KEY` не нужен, но ответы будут тестовыми.

### 1.5. Проверить тесты проекта без влияния ручных env

```bash
env -u ASSISTANT_MODEL -u ASSISTANT_STORAGE_DIR go test ./...
```

Ожидаемо:

- все пакеты проходят;
- нет `FAIL`.

Почему так: `ASSISTANT_MODEL` нужен для ручного запуска, но часть unit-тестов специально проверяет сценарии без выбранной модели.

### 1.6. Инициализировать ассистента

```bash
assistant init --model "$ASSISTANT_MODEL"
assistant profiles list
assistant invariants list
```

Ожидаемо:

- `init` выводит `initialized /Users/nikita/code/coding_writer/.assistant/storage/manual-day11-14`;
- в профилях есть `student` и `senior`;
- в invariants есть default `stack.go`, `memory.layers`, `security.no_secrets`;
- ключ OpenRouter не печатается.

Если в live mode ошибка `OPENROUTER_API_KEY is required`, значит ключ не задан в этом терминале. В fake mode (`ASSISTANT_PROVIDER=fake`) ключ не нужен.

Если ошибка про модель, проверьте ID модели в OpenRouter.

### 1.7. Запустить пользовательский режим

```bash
assistant chat
```

Дальше вы внутри интерактивного режима. Обычный текст отправляется ассистенту. Строки с `/` управляют локальным CLI.

Быстрая проверка внутри `assistant chat`:

```text
/help
Ответь одним коротким предложением: ассистент подключен?
/privacy
/exit
```

Ожидаемо:

- `/help` показывает slash-команды;
- обычное сообщение получает ответ от OpenRouter;
- `/privacy` объясняет, что ключ хранится только в env;
- `/exit` закрывает чат.

## 2. День 11. Память Через Удобный REPL

### Что проверяем

- Есть 3 слоя памяти: `short`, `work`, `long`.
- Данные разных типов не смешиваются.
- Ассистент предлагает, что сохранить и в какой слой.
- Пользователь явно применяет память.
- Следующий ответ использует сохраненную память.
- Мусорная информация не сохраняется как полезная память.

### Ручной сценарий

Запустите чат:

```bash
assistant chat
```

Введите внутри чата:

```text
/task start CLI assistant MVP
/task move execution
Спланируй модуль памяти. Требование текущей задачи: CLI должен поддерживать выбор модели OpenRouter. Мое стабильное предпочтение: отвечай коротко на русском. Случайная фраза для игнорирования: сегодня на улице облачно.
/memory apply --accept all
/memory short
/memory work
/memory long
Продолжай задачу. Не повторяй исходные требования, просто используй сохраненный контекст.
/exit
```

Ожидаемо:

- после обычного сообщения ассистент отвечает и показывает memory proposal;
- если proposal не появился автоматически, выполните `/memory propose`;
- `/memory apply --accept all` сохраняет предложенные полезные записи;
- `/memory short` показывает контекст текущего диалога;
- `/memory work` показывает требование текущей задачи про OpenRouter;
- `/memory long` показывает стабильное предпочтение отвечать коротко на русском;
- случайная фраза про погоду не должна сохраниться как полезная память;
- финальное сообщение `Продолжай задачу...` должно использовать сохраненный контекст без повторного объяснения.

Критерий готовности дня 11: `short`, `work`, `long` содержат разные типы данных, память применяется явно, следующий ответ учитывает сохраненные записи.

## 3. День 12. Персонализация Через Профили

### Что проверяем

- Есть профили `student` и `senior`.
- Профиль подключается к каждому запросу.
- Разные профили меняют стиль ответа.
- Можно создать свой профиль.

### Ручной сценарий

Запустите чат:

```bash
assistant chat
```

Введите внутри чата:

```text
/profile student
Объясни архитектуру memory layers.
/profile senior
Объясни архитектуру memory layers.
/profile create tester --style language=ru --style tone=checklist --format structure=checklist --constraint "answer as checklist"
/profile tester
Как проверить память?
/exit
```

Ожидаемо:

- `/profile student` включает учебный профиль;
- ответ для `student` подробнее, с шагами или объяснениями;
- `/profile senior` включает профиль senior engineer;
- ответ для `senior` короче, прямее, с фокусом на рисках и решениях;
- `/profile create tester ...` создает новый профиль;
- `/profile tester` активирует новый профиль;
- ответ на `Как проверить память?` похож на чек-лист.

Дополнительная проверка вне REPL:

```bash
assistant profiles show student --json
assistant profiles show senior --json
assistant profiles show tester --json
```

Ожидаемо:

- в JSON видны `style`, `response_format`, `constraints`;
- у каждого профиля разные настройки.

Критерий готовности дня 12: активный профиль автоматически влияет на prompt и ответ, без повторения предпочтений в каждом запросе.

## 4. День 13. Состояние Задачи Через REPL

### Что проверяем

- У задачи есть стадия: `planning`, `execution`, `validation`, `done`.
- Есть текущий шаг.
- Есть ожидаемое действие.
- Можно поставить задачу на паузу.
- Можно выйти из CLI, запустить снова и продолжить без повторного объяснения.

### Ручной сценарий с паузой и перезапуском

Запустите чат:

```bash
assistant chat
```

Если после дня 11 текущая задача уже есть, используйте ее. Если задачи нет, начните новую:

```text
/task start Проверка конечного автомата
/task move execution
```

Теперь проверьте состояние:

```text
/task step реализовать MemoryManager
/task expect llm_response
/task status
/task pause
/task status
/exit
```

Ожидаемо:

- `current_step` содержит `реализовать MemoryManager`;
- `expected_action` равен `llm_response`;
- после `/task pause` статус становится `paused`;
- стадия остается `execution`;
- после `/exit` состояние остается в `ASSISTANT_STORAGE_DIR`.

Запустите CLI заново в том же терминале:

```bash
assistant chat
```

Введите внутри чата:

```text
/task resume
/task status
Продолжай задачу. Не проси заново объяснить контекст.
/task move validation
/task pause
/task status
/task resume
/task move done
/task status
/exit
```

Ожидаемо:

- после `/task resume` задача снова `active`;
- стадия, шаг и ожидаемое действие не потерялись;
- ассистент продолжает задачу без просьбы заново объяснить контекст;
- переходы идут по цепочке `execution -> validation -> done`;
- пауза на `validation` тоже сохраняет стадию;
- финальный статус показывает `done` и `expected_action: none`.

Если открыли новый терминал, перед `assistant chat` повторите блок env:

```bash
export CW_ROOT="/Users/nikita/code/coding_writer"
cd "$CW_ROOT"
export PATH="$CW_ROOT/.assistant/bin:$PATH"
export ASSISTANT_STORAGE_DIR="$CW_ROOT/.assistant/storage/manual-day11-14"
export OPENROUTER_API_KEY="ваш_ключ_OpenRouter"
export ASSISTANT_MODEL="deepseek/deepseek-v4-flash"
```

Критерий готовности дня 13: задача хранит stage, step, expected action, выдерживает pause/resume и продолжает после нового запуска CLI.

## 5. День 14. Инварианты

### Что проверяем

- Инварианты хранятся отдельно от диалога.
- Prompt показывает active invariants.
- Конфликтный запрос блокируется до provider call.
- Отказ называет invariant ID и evidence.
- P0 matching is normalized literal forbidden-term matching; paraphrase/semantic matching is not claimed in this manual path.

### Ручной сценарий

Вне REPL:

```bash
assistant invariants list --json
assistant chat --once --render-prompt --input "Как устроен Go MVP?" --json
assistant chat --once --input "предложи переписать MVP на Python" --json
```

Ожидаемо:

- `invariants list` показывает `stack.go` и другие default invariants;
- rendered prompt содержит `Invariant policy`, `id="invariants.active"`, `stack.go`;
- конфликтный запрос возвращает JSON error с `invariant_conflict`, `stack.go`, `mvp на python` и structured `violations`;
- provider chat call для конфликтного запроса не выполняется.

Внутри REPL:

```text
/invariants
/invariants add custom.no_beta --kind business --content "Do not propose beta stack" --forbid "beta stack"
/invariants
/exit
```

Ожидаемо:

- `/invariants` показывает active invariants;
- custom invariant сохраняется в `<storage_root>/invariants/project.jsonl`;
- custom invariant content is bounded and may be visible to the configured provider when rendered into prompts.

Критерий готовности дня 14: инварианты есть в отдельном storage, видны в prompt, конфликт deterministic блокируется, безопасный запрос продолжает обычный flow.

## 6. Быстрые Команды Для Диагностики

Эти команды можно запускать вне REPL, если нужно быстро посмотреть состояние:

```bash
assistant task status
assistant memory list short
assistant memory list work
assistant memory list long
assistant profiles list
assistant invariants list
assistant process audit --latest
```

Полезный smoke-test с реальным OpenRouter:

```bash
assistant chat --once --input "Ответь одним коротким предложением: ассистент подключен?"
```

Проверка prompt без вызова OpenRouter:

```bash
assistant chat --once --render-prompt --input "Продолжай задачу."
```

Prompt audit opt-in:

```bash
export ASSISTANT_PROMPT_AUDIT=1
# raw prompt text is still off by default; enable only deliberately:
export ASSISTANT_RAW_PROMPT_AUDIT=1
```

Ожидаемо:

- без `ASSISTANT_RAW_PROMPT_AUDIT=1` JSON output содержит `rendered_prompt_id`, но не raw prompt;
- с `--render-prompt` raw prompt выводится прямо в ответ команды;
- audit/prompt files can be purged with `assistant privacy purge --audit --yes`.

## 7. Автоматические Acceptance-Тесты

Автотесты не заменяют ручную проверку с вашим ключом, но подтверждают базовую логику дней 11-14:

```bash
env -u ASSISTANT_MODEL -u ASSISTANT_STORAGE_DIR go test ./tests -run 'TestDay11|TestDay12|TestDay13|TestDay14'
```

Ожидаемо:

- `TestDay11EndToEndMemoryProposalApplyInfluence` проходит;
- `TestDay12ProfilesChangePromptAndResponse` проходит;
- `TestDay13PauseResumeAfterRestartUsesWorkingMemory` проходит;
- `TestDay14InvariantsStoredPromptedAndConflictRefused` проходит;
- итоговый статус `ok`.

## 8. Финальный Чек-Лист

- Настройка готова, если `which assistant` указывает на `.assistant/bin/assistant`, `assistant init` проходит, а `assistant chat` отвечает через OpenRouter.
- День 11 готов, если `short`, `work`, `long` хранят разные данные и следующий ответ использует сохраненную память.
- День 12 готов, если `student`, `senior` и `tester` дают заметно разные ответы.
- День 13 готов, если задача проходит `planning -> execution -> validation -> done`, хранит шаг и ожидаемое действие, а pause/resume работает после перезапуска CLI.
- День 14 готов, если invariants хранятся отдельно, prompt содержит active invariants, conflict request получает `invariant_conflict`, а non-conflict flow не ломает дни 11-13.
