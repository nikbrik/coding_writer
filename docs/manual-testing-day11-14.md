# Ручное тестирование Day 11-14

Цель: записать 4 отдельных видео, где каждое видео доказывает выполнение условий одного дня: Day 11, Day 12, Day 13, Day 14.

Формат: перед каждым видео используется отдельное `ASSISTANT_STORAGE_DIR`, чтобы сценарии не зависели друг от друга и на записи было видно чистое состояние.

## 0. Общая Подготовка Перед Записью

Выполнить один раз перед серией видео:

```bash
export CW_ROOT="/Users/nikita/code/coding_writer"
cd "$CW_ROOT"
mkdir -p "$CW_ROOT/.assistant/bin"
go build -o "$CW_ROOT/.assistant/bin/assistant" ./cmd/assistant
export PATH="$CW_ROOT/.assistant/bin:$PATH"
which assistant
assistant --help
```

Ожидаемо:

- `which assistant` показывает `$CW_ROOT/.assistant/bin/assistant`;
- `assistant --help` показывает CLI справку;
- дальше в видео используется команда `assistant`, без `go run`.

Выбрать режим provider.

Live mode через OpenRouter:

```bash
export OPENROUTER_API_KEY="ваш_ключ_OpenRouter"
export ASSISTANT_MODEL="deepseek/deepseek-v4-flash"
unset ASSISTANT_PROVIDER
test -n "$OPENROUTER_API_KEY" && echo "OPENROUTER_API_KEY set"
```

Deterministic fake mode для безопасной записи без ключа:

```bash
export ASSISTANT_PROVIDER=fake
export ASSISTANT_MODEL="fake/model"
unset OPENROUTER_API_KEY
```

Проверить автотесты до записи:

```bash
env -u ASSISTANT_MODEL -u ASSISTANT_STORAGE_DIR go test ./tests -run 'TestDay11|TestDay12|TestDay13|TestDay14'
```

Ожидаемо:

- `TestDay11EndToEndMemoryProposalApplyInfluence` проходит;
- `TestDay12ProfilesChangePromptAndResponse` проходит;
- `TestDay13PauseResumeAfterRestartUsesWorkingMemory` проходит;
- `TestDay14InvariantsStoredPromptedAndConflictRefused` проходит;
- итоговый статус `ok`.

Правила записи:

- каждое видео начинать с команды `export ASSISTANT_STORAGE_DIR=...`;
- показывать `assistant init --model "$ASSISTANT_MODEL"`;
- не показывать реальный `OPENROUTER_API_KEY`;
- в конце каждого видео показать короткий критерий готовности через CLI output.

## 1. Видео Day 11. Memory Layers

### Что Должно Быть Видно На Видео

- Есть 3 слоя памяти: `short`, `work`, `long`.
- Разные типы данных попадают в разные слои.
- Сохранение памяти выполняется явно через user action.
- Следующий ответ учитывает сохраненную память.
- Случайная мусорная фраза не становится полезной памятью.

### Сценарий Записи

Подготовить чистое хранилище:

```bash
export ASSISTANT_STORAGE_DIR="$CW_ROOT/.assistant/storage/video-day11"
test "$ASSISTANT_STORAGE_DIR" = "$CW_ROOT/.assistant/storage/video-day11" && rm -rf "$ASSISTANT_STORAGE_DIR"
mkdir -p "$ASSISTANT_STORAGE_DIR"
assistant init --model "$ASSISTANT_MODEL"
```

Запустить REPL:

```bash
assistant chat
```

Ввести в чате:

```text
Спланируй модуль памяти. Требование текущей задачи: CLI должен поддерживать выбор модели OpenRouter. Мое стабильное предпочтение: отвечай коротко на русском. Случайная фраза для игнорирования: сегодня на улице облачно.
/memory propose
/memory apply --accept all
/memory short
/memory work
/memory long
Продолжай задачу. Не повторяй исходные требования, просто используй сохраненный контекст.
/exit
```

Что проговорить или выделить на записи:

- первый обычный запрос сам создает задачу и переводит процесс в `planning`, без ручных `/task start` и `/task move`;
- `/memory propose` показывает предложения, что сохранить;
- `/memory apply --accept all` является явным применением памяти;
- `/memory short` относится к текущему диалогу;
- `/memory work` содержит требование текущей задачи про OpenRouter;
- `/memory long` содержит стабильное предпочтение отвечать коротко на русском;
- фраза про погоду не нужна для задачи и не должна выглядеть как полезная память;
- последний ответ использует сохраненный контекст без повторного ввода требований.

Финальная проверка вне REPL:

```bash
assistant memory list short
assistant memory list work
assistant memory list long
```

Критерий готовности Day 11: на видео видно раздельное хранение `short/work/long`, явное применение памяти и влияние памяти на следующий ответ.

## 2. Видео Day 12. Personalization Profiles

### Что Должно Быть Видно На Видео

- Есть профили `student` и `senior`.
- Активный профиль подключается к запросу автоматически.
- Разные профили меняют стиль ответа.
- Можно создать пользовательский профиль со style, format и constraint.

### Сценарий Записи

Подготовить чистое хранилище:

```bash
export ASSISTANT_STORAGE_DIR="$CW_ROOT/.assistant/storage/video-day12"
test "$ASSISTANT_STORAGE_DIR" = "$CW_ROOT/.assistant/storage/video-day12" && rm -rf "$ASSISTANT_STORAGE_DIR"
mkdir -p "$ASSISTANT_STORAGE_DIR"
assistant init --model "$ASSISTANT_MODEL"
assistant profiles list
```

Показать настройки дефолтных профилей:

```bash
assistant profiles show student --json
assistant profiles show senior --json
```

Запустить REPL:

```bash
assistant chat
```

Ввести в чате:

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

Что проговорить или выделить на записи:

- ответ `student` должен быть более учебным и объясняющим;
- ответ `senior` должен быть короче, прямее, с фокусом на решение и риски;
- профиль `tester` создан из CLI прямо во время сценария;
- ответ `tester` похож на чек-лист;
- пользователь не повторяет стиль в каждом запросе, стиль берется из активного профиля.

Финальная проверка вне REPL:

```bash
assistant profiles list
assistant profiles show tester --json
assistant chat --once --render-prompt --input "Как проверить память?"
```

Ожидаемо:

- `tester` есть в списке профилей;
- JSON профиля содержит `style`, `response_format`, `constraints`;
- rendered prompt показывает, что профиль попадает в prompt автоматически.

Критерий готовности Day 12: на видео видно, что разные профили автоматически меняют ответ без повторного описания предпочтений в запросе.

## 3. Видео Day 13. Task State FSM

### Что Должно Быть Видно На Видео

- У задачи есть stage: `planning`, `execution`, `validation`, `done`.
- У задачи есть `current_step`.
- У задачи есть `expected_action`.
- Можно поставить задачу на паузу на этапе работы.
- После выхода из CLI и нового запуска состояние сохраняется.
- Можно продолжить без повторного объяснения контекста.

### Сценарий Записи

Подготовить чистое хранилище:

```bash
export ASSISTANT_STORAGE_DIR="$CW_ROOT/.assistant/storage/video-day13"
test "$ASSISTANT_STORAGE_DIR" = "$CW_ROOT/.assistant/storage/video-day13" && rm -rf "$ASSISTANT_STORAGE_DIR"
mkdir -p "$ASSISTANT_STORAGE_DIR"
assistant init --model "$ASSISTANT_MODEL"
```

Запустить REPL:

```bash
assistant chat
```

Ввести в чате первую часть:

```text
Спланируй задачу: реализовать MemoryManager с сохранением состояния после перезапуска.
/task status
Продолжай задачу.
/task status
/task pause
/task status
/exit
```

Что должно быть видно перед перезапуском:

- обычная фраза сама создает задачу и stage `planning`;
- `Продолжай задачу` подтверждает готовый план и переводит stage в `execution`;
- `current_step` берется из плана, без ручного `/task step`;
- `expected_action` становится `llm_response`, без ручного `/task expect`;
- после `/task pause` задача имеет paused status, но stage не теряется.

Запустить CLI заново в том же terminal:

```bash
assistant chat
```

Ввести в чате вторую часть:

```text
/task status
/task resume
/task status
Продолжай задачу. Не проси заново объяснить контекст.
Готово к проверке.
/task pause
/task status
/task resume
/task status
/exit
```

Показать завершение с trusted verification evidence вне REPL:

```bash
assistant chat --once --verify "go version" --input "Проверь и заверши" --json
assistant task status
```

Что проговорить или выделить на записи:

- после нового запуска состояние восстановилось из storage;
- `/task resume` возвращает задачу в active state;
- ассистент продолжает задачу без повторного описания после restart;
- `Готово к проверке` переводит stage `execution -> validation`, без ручного `/task move validation`;
- pause/resume работает не только в `execution`, но и в `validation`;
- `--verify` запускает проверочную команду и передает trusted evidence, после чего validation gate сам переводит `validation -> done`;
- ручного `/task move done` в сценарии нет;
- финальный `/task status` показывает `done` и `expected_action: none`.

Финальная проверка вне REPL:

```bash
assistant task status
```

Критерий готовности Day 13: на видео видно формальное состояние задачи, pause/resume, restart CLI и продолжение без повторного объяснения.

## 4. Видео Day 14. Invariants

### Что Должно Быть Видно На Видео

- Инварианты хранятся отдельно от диалога.
- Active invariants попадают в prompt.
- Конфликтный запрос блокируется до provider call.
- Отказ объясняет invariant ID и evidence.
- Безопасный запрос проходит обычный flow.

### Сценарий Записи

Подготовить чистое хранилище:

```bash
export ASSISTANT_STORAGE_DIR="$CW_ROOT/.assistant/storage/video-day14"
test "$ASSISTANT_STORAGE_DIR" = "$CW_ROOT/.assistant/storage/video-day14" && rm -rf "$ASSISTANT_STORAGE_DIR"
mkdir -p "$ASSISTANT_STORAGE_DIR"
assistant init --model "$ASSISTANT_MODEL"
```

Показать отдельное хранилище инвариантов:

```bash
assistant invariants list --json
```

Ожидаемо:

- в списке есть default invariants;
- среди них есть `stack.go`, `memory.layers`, `security.no_secrets`.

Показать, что active invariants попадают в prompt:

```bash
assistant chat --once --render-prompt --input "Как устроен Go MVP?" --json
```

Ожидаемо:

- output содержит rendered prompt;
- в prompt видны `Invariant policy`, `id="invariants.active"`, `stack.go`;

Показать, что безопасный запрос проходит обычный flow:

```bash
assistant chat --once --input "объясни Go MVP" --json
```

Ожидаемо:

- команда возвращает успешный JSON response;
- error `invariant_conflict` отсутствует;
- provider flow не блокируется, потому что запрос не нарушает invariant.

Показать конфликт и отказ:

```bash
assistant chat --once --input "предложи переписать MVP на Python" --json
```

Ожидаемо:

- команда возвращает JSON error;
- error type содержит `invariant_conflict`;
- в JSON есть invariant ID `stack.go`;
- в evidence есть конфликтный фрагмент вроде `mvp на python`;
- provider call для конфликтного запроса не выполняется.

Показать управление инвариантами в REPL:

```bash
assistant chat
```

Ввести в чате:

```text
/invariants
/invariants add custom.no_beta --kind business --content "Do not propose beta stack" --forbid "beta stack"
/invariants
/exit
```

Проверить файл storage:

```bash
ls "$ASSISTANT_STORAGE_DIR/invariants"
assistant invariants list --json
```

Ожидаемо:

- `/invariants` показывает active invariants;
- `custom.no_beta` добавлен и сохраняется отдельно от диалога;
- файл storage находится в `$ASSISTANT_STORAGE_DIR/invariants/project.jsonl`;
- content инварианта может попадать в prompt, поэтому туда нельзя писать секреты.

Критерий готовности Day 14: на видео видно отдельное хранение invariants, попадание в prompt, deterministic refusal при конфликте и обычный flow для safe request.

## 5. Короткий Финальный Чек-Лист

- Day 11: `short/work/long` разделены, память применяется явно, следующий ответ ее учитывает.
- Day 12: `student/senior/tester` дают разные ответы, активный профиль попадает в prompt автоматически.
- Day 13: task FSM хранит stage, current step, expected action, переживает pause/resume и restart CLI.
- Day 14: invariants хранятся отдельно, видны в prompt, конфликт получает `invariant_conflict` с ID и evidence.
