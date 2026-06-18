# Ручное тестирование дней 11-13

Документ описывает, как вручную подключить OpenRouter API key, запустить CLI-ассистента и проверить требования дней 11, 12 и 13.

## 1. Подключение ключа OpenRouter

### Что нужно заранее

- Go 1.22 или новее.
- Терминал в корне проекта `coding_writer`.
- Аккаунт OpenRouter.
- API key OpenRouter.
- ID модели OpenRouter, например `openai/gpt-4.1-mini`, `anthropic/claude-3.5-sonnet` или другая доступная модель.

Важно:

- ключ передается только через переменную окружения `OPENROUTER_API_KEY`;
- ключ не надо писать в prompt, профиль, память, название задачи или config;
- реальные запросы к OpenRouter могут тратить деньги;
- для чистого ручного теста лучше использовать отдельное временное хранилище.

### Шаг 1. Открыть терминал в проекте

```bash
pwd
```

Ожидаемо:

- путь заканчивается на `coding_writer`.

### Шаг 2. Указать API key

```bash
export OPENROUTER_API_KEY="ваш_ключ_OpenRouter"
```

Проверить, что переменная задана:

```bash
test -n "$OPENROUTER_API_KEY" && echo "OPENROUTER_API_KEY set"
```

Ожидаемо:

- вывод: `OPENROUTER_API_KEY set`.

Не выводите сам ключ командой `echo $OPENROUTER_API_KEY`, чтобы случайно не показать его в логах или скриншотах.

### Шаг 3. Выбрать модель

```bash
export ASSISTANT_MODEL="openai/gpt-4.1-mini"
```

Если хотите другую модель, замените значение на ID модели из OpenRouter.

Например, если вы хотите использовать DeepSeek:

```bash
export ASSISTANT_MODEL="deepseek/deepseek-v4-flash"
```

### Шаг 4. Создать отдельное хранилище для ручного теста

```bash
export ASSISTANT_STORAGE_DIR="$(mktemp -d)"
```

Зачем это нужно:

- все данные теста попадут во временную папку;
- реальные пользовательские данные не будут затронуты;
- тест можно безопасно повторять с нуля.

### Шаг 5. Проверить сборку проекта

Если вы уже задали `ASSISTANT_MODEL` и `ASSISTANT_STORAGE_DIR`, запускайте тесты так:

```bash
env -u ASSISTANT_MODEL -u ASSISTANT_STORAGE_DIR go test ./...
```

Ожидаемо:

- команда завершается без ошибок;
- в конце нет `FAIL`.

Почему так: `ASSISTANT_MODEL` нужен для ручного запуска ассистента, но некоторые unit-тесты специально проверяют сценарий без выбранной модели.

### Шаг 6. Инициализировать ассистента

```bash
go run ./cmd/assistant init --model "$ASSISTANT_MODEL"
```

Ожидаемо:

- вывод начинается с `initialized`;
- ошибок нет;
- созданы стандартные профили `student` и `senior`;
- ключ OpenRouter не печатается в выводе.

Если ошибка `OPENROUTER_API_KEY is required`, значит переменная окружения не задана в этом терминале.

Если ошибка про модель, проверьте, что модель доступна в вашем аккаунте OpenRouter.

### Шаг 7. Проверить первый реальный запрос

```bash
go run ./cmd/assistant chat --once --input "Ответь одним коротким предложением: ассистент подключен?"
```

Ожидаемо:

- ассистент отвечает обычным текстом;
- нет ошибки `missing_api_key`;
- нет ошибки модели;
- в stderr может быть предупреждение provider disclosure: это нормально, оно объясняет, что prompt отправляется внешнему провайдеру.

### Шаг 8. Быстро проверить профили

```bash
go run ./cmd/assistant profiles list
```

Ожидаемо:

- есть профиль `student`;
- есть профиль `senior`;
- один из профилей помечен как активный.

## 2. День 11. Модель памяти ассистента

### Требования дня 11 простыми словами

Нужно проверить, что у ассистента есть 3 разных слоя памяти:

- `short` - краткосрочная память текущего диалога;
- `work` - рабочая память текущей задачи;
- `long` - долговременная память профиля, предпочтений и знаний.

Также нужно проверить:

- разные типы памяти хранятся отдельно;
- ассистент явно предлагает, что сохранить и куда;
- пользователь явно применяет предложение памяти;
- память влияет на следующий ответ;
- мусорные или неважные данные не сохраняются как полезная память.

### Кейс 11.1. Создать задачу для рабочей памяти

```bash
go run ./cmd/assistant task start "CLI assistant MVP"
go run ./cmd/assistant task move execution
```

Ожидаемо:

- первая команда показывает задачу со стадией `planning`;
- вторая команда показывает стадию `execution`;
- задача стала текущей.

### Кейс 11.2. Выполнить запрос, из которого можно извлечь память

```bash
go run ./cmd/assistant chat --once --input "Спланируй модуль памяти. Требование текущей задачи: CLI должен поддерживать выбор модели OpenRouter. Мое стабильное предпочтение: отвечай коротко на русском. Случайная фраза для игнорирования: сегодня на улице облачно."
```

Ожидаемо:

- ассистент отвечает без ошибки;
- ответ связан с модулем памяти;
- после запроса появляется предложение памяти или его можно получить следующей командой.

### Кейс 11.3. Посмотреть предложения памяти

```bash
go run ./cmd/assistant memory proposals
```

Ожидаемо:

- есть pending-предложение;
- в предложении есть записи с разными слоями;
- требование про OpenRouter должно быть предложено в `work` или близком рабочем слое;
- предпочтение про короткие ответы на русском должно быть предложено в `long` или близком долговременном слое;
- текущий контекст диалога может быть предложен в `short`;
- случайная фраза про погоду не должна попадать в полезную память или должна быть помечена как `ignore`.

Реальный LLM может формулировать записи иначе. Оценивайте смысл, а не точное совпадение текста.

### Кейс 11.4. Явно применить предложение памяти

```bash
go run ./cmd/assistant memory apply --accept all
```

Ожидаемо:

- вывод вида `saved N records`, где `N` больше нуля;
- сохраняются только принятые полезные записи;
- записи `ignore`, если они были, не становятся настоящей памятью.

### Кейс 11.5. Проверить краткосрочную память `short`

```bash
go run ./cmd/assistant memory list short
```

Ожидаемо:

- есть контекст текущего диалога, если LLM предложил сохранить его в `short`;
- это не должно быть постоянным пользовательским предпочтением;
- это не должно быть главным требованием всей задачи.

### Кейс 11.6. Проверить рабочую память `work`

```bash
go run ./cmd/assistant memory list work
```

Ожидаемо:

- есть требование текущей задачи про выбор модели OpenRouter;
- запись привязана к текущей задаче;
- это не просто пересказ всего диалога.

### Кейс 11.7. Проверить долговременную память `long`

```bash
go run ./cmd/assistant memory list long
```

Ожидаемо:

- есть предпочтение пользователя отвечать коротко на русском;
- запись выглядит как долговременное предпочтение, а не как шаг текущей задачи.

### Кейс 11.8. Проверить, что мусор не попал в память

Выполнить все три команды:

```bash
go run ./cmd/assistant memory list short
go run ./cmd/assistant memory list work
go run ./cmd/assistant memory list long
```

Ожидаемо:

- случайная фраза `сегодня на улице облачно` не сохранена как полезная память;
- нет сохраненного слоя `ignore`;
- если LLM все-таки предложил сохранить мусор, это дефект классификации памяти.

### Кейс 11.9. Проверить влияние памяти на следующий ответ

```bash
go run ./cmd/assistant chat --once --input "Продолжай задачу. Не повторяй исходные требования, просто используй сохраненный контекст."
```

Ожидаемо:

- ассистент продолжает тему текущей задачи;
- ответ учитывает требование про выбор модели OpenRouter;
- стиль ответа учитывает предпочтение коротко на русском;
- ассистент не просит заново объяснить задачу.

### Кейс 11.10. Проверить prompt без вызова провайдера

```bash
go run ./cmd/assistant chat --once --render-prompt --input "Продолжай задачу."
```

Ожидаемо:

- команда не вызывает OpenRouter;
- в выводе видны блоки профиля, задачи и памяти;
- в памяти видны сохраненные данные из `short`, `work`, `long`.

## 3. День 12. Персонализация ассистента

### Требования дня 12 простыми словами

Нужно проверить, что ассистент использует профиль пользователя:

- профиль существует;
- в профиле есть стиль, формат ответа и ограничения;
- профиль подключается к каждому запросу;
- разные профили дают разные prompt и заметно разные ответы;
- ассистент учитывает профиль автоматически, без повторного объяснения в каждом запросе.

### Кейс 12.1. Посмотреть профиль `student`

```bash
go run ./cmd/assistant profiles show student --json
```

Ожидаемо:

- `id` равен `student`;
- в `style` есть русский язык и учебный тон;
- в `response_format` есть пошаговая структура;
- в `constraints` есть требование объяснять термины.

### Кейс 12.2. Посмотреть профиль `senior`

```bash
go run ./cmd/assistant profiles show senior --json
```

Ожидаемо:

- `id` равен `senior`;
- в `style` есть русский язык и прямой тон;
- в `response_format` есть краткая структура;
- в `constraints` есть фокус на рисках и решениях.

### Кейс 12.3. Проверить ответ с профилем `student`

```bash
go run ./cmd/assistant profiles set student
go run ./cmd/assistant chat --once --input "Объясни архитектуру memory layers."
```

Ожидаемо:

- активный профиль стал `student`;
- ответ подробнее, чем для senior;
- есть объяснение шагами или с примерами;
- ассистент пишет по-русски.

### Кейс 12.4. Проверить ответ с профилем `senior`

```bash
go run ./cmd/assistant profiles set senior
go run ./cmd/assistant chat --once --input "Объясни архитектуру memory layers."
```

Ожидаемо:

- активный профиль стал `senior`;
- ответ короче и прямее, чем для student;
- больше фокуса на рисках, решениях или trade-offs;
- ассистент пишет по-русски.

### Кейс 12.5. Сравнить prompt для разных профилей

```bash
go run ./cmd/assistant profiles set student
go run ./cmd/assistant chat --once --render-prompt --input "Объясни архитектуру memory layers."
go run ./cmd/assistant profiles set senior
go run ./cmd/assistant chat --once --render-prompt --input "Объясни архитектуру memory layers."
```

Ожидаемо:

- оба prompt содержат блок активного профиля;
- в первом prompt есть `student`;
- во втором prompt есть `senior`;
- prompt отличаются между собой;
- это доказывает, что профиль подключается к каждому запросу.

### Кейс 12.6. Создать свой профиль и проверить его подключение

```bash
go run ./cmd/assistant profiles create tester --display-name "Manual Tester" --style language=ru --style tone=checklist --format structure=checklist --constraint "answer as checklist"
go run ./cmd/assistant profiles set tester
go run ./cmd/assistant profiles show --json
go run ./cmd/assistant chat --once --render-prompt --input "Как проверить память?"
```

Ожидаемо:

- профиль `tester` создан;
- активный профиль стал `tester`;
- `profiles show --json` показывает стиль, формат и ограничения профиля;
- render-prompt содержит данные профиля `tester`.

### Кейс 12.7. Проверить реальный ответ с новым профилем

```bash
go run ./cmd/assistant chat --once --input "Как проверить память?"
```

Ожидаемо:

- ответ похож на чек-лист;
- ассистент учитывает профиль `tester` без дополнительного объяснения в запросе.

## 4. День 13. Состояние задачи как конечный автомат

### Требования дня 13 простыми словами

Нужно проверить, что задача хранит формальное состояние:

- этап задачи: `planning`, `execution`, `validation`, `done`;
- текущий шаг;
- ожидаемое действие;
- можно поставить задачу на паузу на любом этапе;
- можно продолжить без повторных объяснений;
- после перезапуска CLI состояние не теряется.

### Кейс 13.1. Создать новую задачу и проверить начальное состояние

Для чистоты можно использовать новое хранилище, но ключ и модель оставить теми же:

```bash
export ASSISTANT_STORAGE_DIR="$(mktemp -d)"
go run ./cmd/assistant init --model "$ASSISTANT_MODEL"
go run ./cmd/assistant task start "Проверка конечного автомата"
go run ./cmd/assistant task status
```

Ожидаемо:

- задача создана;
- стадия началась с `planning`;
- статус `active`;
- в выводе есть список допустимых следующих стадий.

### Кейс 13.2. Задать текущий шаг и ожидаемое действие

```bash
go run ./cmd/assistant task step "реализовать MemoryManager"
go run ./cmd/assistant task expect llm_response
go run ./cmd/assistant task status
```

Ожидаемо:

- `current_step` или текстовый вывод содержит `реализовать MemoryManager`;
- `expected_action` равен `llm_response`;
- эти поля сохраняются в состоянии задачи.

### Кейс 13.3. Пройти переходы автомата

```bash
go run ./cmd/assistant task move execution
go run ./cmd/assistant task status
go run ./cmd/assistant task move validation
go run ./cmd/assistant task status
go run ./cmd/assistant task move done
go run ./cmd/assistant task status
```

Ожидаемо:

- после первой команды стадия `execution`;
- после второй команды стадия `validation`;
- после третьей команды стадия `done`;
- переходы идут по цепочке `planning -> execution -> validation -> done`.

### Кейс 13.4. Проверить запрет неправильного перехода

```bash
export ASSISTANT_STORAGE_DIR="$(mktemp -d)"
go run ./cmd/assistant init --model "$ASSISTANT_MODEL"
go run ./cmd/assistant task start "Проверка неверного перехода"
go run ./cmd/assistant task move done
```

Ожидаемо:

- команда `task move done` из `planning` завершается ошибкой;
- состояние задачи не становится `done`;
- это подтверждает, что переходы контролируются, а не выставляются произвольно.

### Кейс 13.5. Проверить паузу на этапе `planning`

```bash
export ASSISTANT_STORAGE_DIR="$(mktemp -d)"
go run ./cmd/assistant init --model "$ASSISTANT_MODEL"
go run ./cmd/assistant task start "Пауза на planning"
go run ./cmd/assistant task pause
go run ./cmd/assistant task status
go run ./cmd/assistant task resume
go run ./cmd/assistant task status
```

Ожидаемо:

- после `pause` статус `paused`;
- стадия остается `planning`;
- после `resume` статус снова `active`;
- стадия не сбрасывается.

### Кейс 13.6. Проверить паузу на этапе `execution`

```bash
export ASSISTANT_STORAGE_DIR="$(mktemp -d)"
go run ./cmd/assistant init --model "$ASSISTANT_MODEL"
go run ./cmd/assistant task start "Пауза на execution"
go run ./cmd/assistant task step "реализовать MemoryManager"
go run ./cmd/assistant task expect llm_response
go run ./cmd/assistant task move execution
go run ./cmd/assistant task pause
go run ./cmd/assistant task status
go run ./cmd/assistant task resume
go run ./cmd/assistant task status
```

Ожидаемо:

- после `pause` статус `paused`;
- стадия остается `execution`;
- текущий шаг остается `реализовать MemoryManager`;
- ожидаемое действие остается `llm_response`;
- после `resume` статус снова `active`.

### Кейс 13.7. Проверить паузу на этапе `validation`

```bash
export ASSISTANT_STORAGE_DIR="$(mktemp -d)"
go run ./cmd/assistant init --model "$ASSISTANT_MODEL"
go run ./cmd/assistant task start "Пауза на validation"
go run ./cmd/assistant task move execution
go run ./cmd/assistant task move validation
go run ./cmd/assistant task pause
go run ./cmd/assistant task status
go run ./cmd/assistant task resume
go run ./cmd/assistant task status
```

Ожидаемо:

- после `pause` статус `paused`;
- стадия остается `validation`;
- после `resume` статус снова `active`;
- стадия остается `validation`.

### Кейс 13.8. Проверить продолжение после перезапуска CLI

```bash
export ASSISTANT_STORAGE_DIR="$(mktemp -d)"
go run ./cmd/assistant init --model "$ASSISTANT_MODEL"
go run ./cmd/assistant task start "Продолжение после перезапуска"
go run ./cmd/assistant task step "реализовать MemoryManager"
go run ./cmd/assistant task expect llm_response
go run ./cmd/assistant task move execution
go run ./cmd/assistant task pause
echo "$ASSISTANT_STORAGE_DIR"
```

Скопируйте значение `ASSISTANT_STORAGE_DIR`, откройте новый терминал и задайте там те же переменные:

```bash
export OPENROUTER_API_KEY="ваш_ключ_OpenRouter"
export ASSISTANT_MODEL="openai/gpt-4.1-mini"
export ASSISTANT_STORAGE_DIR="путь_из_предыдущего_терминала"
```

Затем продолжите:

```bash
go run ./cmd/assistant task resume
go run ./cmd/assistant task status
go run ./cmd/assistant chat --once --input "Продолжай задачу. Не проси заново объяснить контекст."
```

Ожидаемо:

- после `resume` стадия остается `execution`;
- текущий шаг остается `реализовать MemoryManager`;
- ожидаемое действие остается `llm_response`;
- ассистент продолжает задачу без повторного объяснения с нуля.

### Кейс 13.9. Проверить, что рабочая память подхватывается после resume

Сначала создайте состояние и сохраните рабочую память через обычный диалог дня 11:

```bash
export ASSISTANT_STORAGE_DIR="$(mktemp -d)"
go run ./cmd/assistant init --model "$ASSISTANT_MODEL"
go run ./cmd/assistant task start "Проверка resume с рабочей памятью"
go run ./cmd/assistant task step "реализовать MemoryManager"
go run ./cmd/assistant task expect llm_response
go run ./cmd/assistant task move execution
go run ./cmd/assistant chat --once --input "Спланируй модуль памяти. Требование текущей задачи: CLI должен поддерживать выбор модели OpenRouter. Мое стабильное предпочтение: отвечай коротко на русском."
go run ./cmd/assistant memory apply --accept all
go run ./cmd/assistant task pause
go run ./cmd/assistant task resume
go run ./cmd/assistant chat --once --render-prompt --input "Продолжай задачу."
```

Ожидаемо:

- render-prompt содержит стадию `execution`;
- render-prompt содержит шаг `реализовать MemoryManager`;
- render-prompt содержит рабочую память про выбор модели OpenRouter;
- значит ассистент продолжает с контекстом, а не с пустого места.

## 5. Общая проверка через автоматические acceptance-тесты

После ручных кейсов полезно запустить целевые тесты:

```bash
go test ./tests -run 'TestDay11|TestDay12|TestDay13'
```

Ожидаемо:

- `TestDay11EndToEndMemoryProposalApplyInfluence` проходит;
- `TestDay12ProfilesChangePromptAndResponse` проходит;
- `TestDay13PauseResumeAfterRestartUsesWorkingMemory` проходит;
- итоговый статус `ok`.

Эти тесты используют тестовый runtime и не заменяют ручную проверку с вашим OpenRouter key.

## 6. Краткий чек-лист готовности

- Подключение готово, если `init` и первый `chat --once` проходят с вашим `OPENROUTER_API_KEY` и выбранной моделью.
- День 11 готов, если `short`, `work`, `long` хранят разные данные, мусор не сохраняется, а следующий ответ использует сохраненную память.
- День 12 готов, если `student`, `senior` и кастомный профиль дают разные prompt и заметно разные ответы, а активный профиль автоматически попадает в каждый запрос.
- День 13 готов, если задача проходит стадии `planning -> execution -> validation -> done`, хранит текущий шаг и ожидаемое действие, пауза/продолжение не теряют контекст даже после нового запуска CLI.
