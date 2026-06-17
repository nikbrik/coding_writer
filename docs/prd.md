# PRD: минимальный CLI code assistant

## 1. Контекст

Проект недели: перейти от обычного prompt/response-бота к минимальному stateful-ассистенту. Ассистент должен работать через CLI, подключаться к OpenRouter, позволять выбрать модель в интерфейсе и явно управлять памятью.

`day11.md`, `day12.md` и `day13.md` считаются жёсткими критериями приёмки. Лекция `03-memory-state-notes.md` задаёт архитектурные принципы: memory layers, персонализация, task state machine, инварианты, prompt builder, контролируемые переходы и сохранение состояния.

## 2. Цель продукта

Сделать простой CLI code assistant, который:

- принимает запросы пользователя в терминале;
- использует OpenRouter API для вызова LLM;
- даёт выбрать модель при запуске или через команду интерфейса;
- хранит память в отдельных слоях;
- явно решает, какие данные куда сохранять, через отдельный LLM memory-classification step;
- подключает профиль пользователя к каждому запросу;
- демонстрирует разные ответы для разных профилей;
- ведёт текущую задачу как конечный автомат: этап, текущий шаг, ожидаемое действие.

## 3. MVP

MVP должен быть небольшим, но архитектурно правильным. Главная ценность не в большом количестве инструментов, а в явной модели памяти и персонализации.

Минимальный сценарий:

1. Пользователь запускает CLI.
2. CLI проверяет OpenRouter API key.
3. CLI предлагает выбрать или ввести модель.
4. CLI предлагает выбрать профиль пользователя или создать новый.
5. Пользователь задаёт вопрос по коду или архитектуре.
6. Prompt builder собирает prompt из системных правил, профиля, рабочей памяти, краткосрочной истории и релевантной долговременной памяти.
7. LLM отвечает.
8. После ответа ассистент запускает отдельный memory-classification prompt через OpenRouter.
9. LLM выделяет факты из диалога и предлагает слой памяти для каждого факта: `short`, `work`, `long` или `ignore`.
10. CLI показывает пользователю классификацию памяти.
11. Пользователь подтверждает сохранение или правит выбранные слои.
12. Приложение сохраняет факты в физически разные хранилища.
13. При следующем запросе ассистент использует выбранные слои памяти.

## 4. Пользовательские сценарии

### 4.1. Первый запуск

Пользователь запускает ассистента без конфигурации.

Ожидаемое поведение:

- ассистент проверяет `OPENROUTER_API_KEY`;
- если ключа нет, объясняет, как задать `OPENROUTER_API_KEY` через environment; hidden input не входит в P0;
- ключ не сохраняется в репозиторий;
- ассистент до первого provider call показывает, какие категории данных будут отправляться в OpenRouter;
- ассистент показывает список моделей OpenRouter или даёт ввести model id вручную;
- ассистент создаёт первый профиль пользователя через короткое интервью.

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
Memory proposal:
1. [work] Требование: CLI должен поддерживать выбор модели OpenRouter.
2. [long] Пользователь предпочитает короткие ответы на русском.
3. [short] В этом диалоге обсуждаем memory layers.
4. [ignore] Пользователь сказал "спасибо".

Save? [y]es / [e]dit / [n]o
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

## 4.5. Canonical contract

PRD, FRD и architecture должны ссылаться на один canonical contract для Day 11, Day 12 и Day 13.

### Task state

- `stage`: `planning`, `execution`, `validation`, `done`.
- `status`: `active`, `paused`.
- `expected_action`: `user_input`, `llm_response`, `user_confirmation`, `none`.
- `tool_result` не входит в P0, потому что MVP не выполняет tools; его можно вернуть только в P1 вместе с явным tool flow.
- terminal completion фиксируется через `stage=done` и `expected_action=none`; `status=done` не используется в MVP.

### Commands

- Top-level commands и slash commands должны описывать один и тот же canonical command tree.
- Для P0 обязательны только команды, нужные для Day 11/12/13 demo path и deterministic smoke tests.
- Canonical command matrix должен указывать P0/P1 статус, JSON/stdout/stderr поведение, LLM-call behavior и non-interactive эквиваленты для smoke tests.

### Memory layers

- `short`, `work`, `long` are the only physical storage layers.
- `ignore` exists only in the proposal/audit layer.

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

### Definition of ready for implementation

- canonical contract согласован;
- P0 vertical slice определён;
- provider/storage/privacy/test rules описаны;
- no open high-severity contract gaps.

## 5. Критерии приёмки Day 11

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

## 6. Критерии приёмки Day 12

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

## 7. Критерии приёмки Day 13

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

## 8. Memory layers

### 8.1. Short-term memory

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

### 8.2. Working memory

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

- пользователь явно пишет `/task start`, `/task step`, `/task expect`, `/save work ...` или подтверждает LLM proposal `[work]`;
- ассистент выделяет требования, acceptance criteria, решения и open questions отдельным memory-classification prompt;
- ассистент не сохраняет рабочие факты silently;
- при завершении задачи часть working memory можно перенести в long-term decisions.

### 8.3. Long-term memory

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

## 9. LLM memory classification

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

## 10. Персонализация

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

## 11. CLI interface

Минимальные команды запуска:

```text
assistant init
assistant chat
assistant chat --once --input <text> --json
assistant chat --profile student --model openai/gpt-4.1-mini
assistant profiles
assistant memory
assistant task status --json
assistant privacy
```

Команды внутри диалога:

```text
/help                         Показать команды
/model                        Выбрать модель
/profile                      Переключить профиль
/profile create               Создать профиль
/task start <title>           Начать рабочую задачу
/task status                  Показать состояние задачи
/task step <text>             Установить текущий шаг задачи
/task expect <action>         Установить ожидаемое действие
/task move <stage>            Перейти в другой этап
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
/privacy                      Показать privacy/provider/storage summary
/clear short                  Очистить краткосрочный слой текущей сессии
/exit                         Завершить CLI
```

P0 command boundary:

- `assistant task status --json` является P0 для smoke tests;
- остальные top-level `assistant task ...` subcommands и slash-команды `/task plan`, `/task criteria`, `/task decision`, `/task done`, `/task stage` являются P1/debug, пока canonical command matrix не включит их во все docs/tests.

## 12. OpenRouter requirements

Требования:

- поддержать `OPENROUTER_API_KEY` из environment;
- для MVP считать env-only policy канонической; hidden input или local key file не должны стать default path без отдельного threat model;
- не сохранять ключ в git, config, profiles, memory files, transcripts или audit trail;
- проверять chat/classifier payload локальным pre-provider secret scanner до отправки в OpenRouter;
- дать выбрать модель из списка или ввести model id;
- хранить выбранную модель в локальном config;
- позволять сменить модель через `/model`;
- использовать OpenRouter не только для основного ответа, но и для memory-classification prompt;
- явно сообщать пользователю, какие категории данных отправляются во внешний provider;
- custom base URL только как explicit opt-in за пределами P0.
- raw transcripts и raw proposal content должны быть opt-in; по умолчанию audit хранит минимальные/редактированные данные и поддерживает purge/retention policy.

Минимальная интеграция:

- endpoint: `https://openrouter.ai/api/v1/chat/completions`;
- model id передаётся в поле `model`;
- prompt отправляется как chat messages;
- ошибки API показываются в CLI без падения приложения.

## 13. Prompt builder

Prompt builder должен собирать контекст слоями, а не добавлять всё подряд.

Базовый порядок блоков:

1. System role ассистента.
2. Правила безопасности и запрет сохранять секреты.
3. Активный профиль пользователя.
4. Инварианты профиля и проекта.
5. Task state: stage, current step, expected action, allowed transitions.
6. Working memory текущей задачи.
7. Релевантная long-term memory.
8. Short-term history текущего диалога.
9. Текущий запрос пользователя.

Важно:

- профиль подключается к каждому запросу;
- short-term history ограничивается размером окна;
- long-term memory подмешивается выборочно;
- task state имеет больший приоритет, чем рабочая память и случайная история диалога;
- агент должен выполнять работу, соответствующую текущему stage и expected action.

Контракт безопасности:

- profile, memory, task state, transcripts и classifier output являются untrusted data;
- такие блоки должны быть serialized/quoted/tagged как данные, а не инструкции;
- canonical prompt schema должен задавать block id, block type, source, trust label, escaping rules и порядок блоков;
- system/application/security policy всегда выше пользовательского и сохранённого контекста.

## 14. State machine

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

Пример task state:

```json
{
  "stage": "planning",
  "current_step": "сформировать архитектурный план MVP",
  "expected_action": "user_confirmation",
  "status": "active"
}
```

## 15. Инварианты

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

Для MVP инварианты можно проверять простыми правилами и предупреждениями. Позже их можно вынести в deterministic invariant checker.

## 16. Non-goals MVP

В MVP не нужно делать:

- полноценный IDE agent;
- автоматическое редактирование файлов;
- RAG по всему репозиторию;
- vector database;
- multi-agent orchestration;
- web UI;
- сложную авторизацию;
- автоматическую долговременную память без подтверждения;
- silent memory writes без LLM proposal и без audit trail.

## 17. Метрики готовности

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
- задачу можно поставить на паузу и продолжить без повторного объяснения;
- API key не попадает в репозиторий и memory files;
- есть понятный путь расширения к invariant checker и validation loop.

## 18. Test and eval matrix

Реализация должна иметь детерминированный набор проверок, который подтверждает Day 11/12/13 без reliance on live LLM eyeballing.

Минимум:

- fake provider fixtures;
- golden prompt rendering tests;
- storage failure/corruption tests;
- provider timeout/error tests;
- pre-provider secret leak tests for chat and classifier payloads;
- duplicate proposal apply tests;
- forbidden transition tests;
- race/locking/atomic-write/symlink/path-safety tests for file storage;
- non-interactive CLI JSON/exit-code smoke tests;
- prompt-injection/redaction tests for profile/memory/task/transcripts.

Каждый обязательный Day 11/12/13 критерий должен иметь либо автоматический тест, либо явно отмеченный manual demo proof.

## 18. Roadmap недели

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
