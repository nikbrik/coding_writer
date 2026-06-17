# Goal: Реализация всех изменений из финального вердикта консенсуса

## Context

Финальный вердикт консенсуса (12-final-verdict.md) содержит 30 изменений:
- 11 обязательных (required changes)
- 19 рекомендуемых (recommended changes)

## Acceptance Criteria

### Обязательные изменения (11 шт.)

- [x] **1. F001**: Пользовательский запрос обёрнут в `<context_block trust="untrusted">` с `EscapeUntrusted`
- [x] **2. F002**: Retry-сообщение классификатора содержит полную JSON-схему
- [x] **3. F013**: Текст ошибки исправлен: убрана ссылка на `--non-interactive`
- [x] **4. F014**: Флаги `--reject` и `--edit` в `memory apply` стали `StringArrayVar`
- [x] **5. F004**: `LatestSessionID` валидирует имена директорий через `storage.ValidateID`
- [x] **6. F005**: Явные скобки в булевом выражении `purgePrivacyData`
- [x] **7. F007**: `DisallowUnknownFields` удалён из `ReadJSON`
- [x] **8. F030**: Trust-метка профиля: `trust="untrusted" priority="high"`
- [x] **9. F017**: `Temperature=0` для классификатора
- [x] **10. F016**: `ErrorWithHint` для `invalid_model`, `forbidden_transition`, `unknown_command`
- [x] **11. F015**: Парсинг пробелов в `/memory apply --edit` — реализован `splitShellTokens` с поддержкой кавычек

### Рекомендуемые изменения (19 шт.)

- [x] **12. F010**: Классификатор получает сводку существующей памяти (последние 5 на слой)
- [x] **13. F011**: Команда `memory proposals` для списка всех предложений
- [x] **14. F003**: Паттерны секретов расширены: AWS AKIA*, GCP service_account
- [ ] **15. F006**: `root.go` разбит на файлы — ОТЛОЖЕНО (крупный рефакторинг, не блокирует релиз)
- [x] **16. F009**: `LongTermKinds` — единый источник истины для типов long-term памяти
- [x] **17. F018**: FakeProvider поддерживает `ClassifierResponses []string` для последовательных ответов
- [x] **18. F019**: Память рендерится для промпта как `promptMemoryRecord{kind, content, time}`
- [x] **19. F012**: `HistoryLog []string` в TaskState, заполняется при смене стадии
- [x] **20. AI-NEW1**: Few-shot примеры в `classifierInstructions()`
- [x] **21. F024**: REPL `/profile create` принимает `--style`/`--format`/`--constraint`
- [x] **22. F025**: Первое `message_user` закреплено в `latestWithinBudget`
- [x] **23. F027**: Флаг `--quiet` подавляет provider disclosure
- [x] **24. F028**: `/help` обновлён: повторяемые флаги, `/clear` только short, `/proposals`
- [x] **25. F029**: Дефолты `profiles create` задокументированы в help-тексте флагов
- [x] **26. CLI8**: `--accept` поддерживает конкретные ID через `StringArrayVar`
- [x] **27. F020**: Пути в ошибках маскируются через `redactHomePath` (`~` вместо home dir)
- [x] **28. AI-NEW2**: Профиль рендерится как естественный язык (детерминированно, с сортировкой ключей)
- [x] **29. F022**: Отступы в `chatCommand` исправлены
- [x] **30. F023**: Кастомная `func min()` удалена, используется встроенная Go 1.22

### Верификация

- [x] `go build ./...` проходит без ошибок
- [x] `go test ./...` проходит без ошибок (все 10 пакетов)
- [x] Все существующие тесты проходят
- [x] Тесты обновлены для F030 (profiles/manager_test.go, prompting/builder_test.go)
- [x] Day11/12/13 требования не нарушены (подтверждено passing acceptance tests)

## Constraints

- Не менять структуру 3-слойной памяти (day11) ✅
- Не менять модель UserProfile (day12) ✅
- Не менять FSM задач и переходы (day13) ✅
- Не добавлять тяжёлые зависимости ✅
- Следовать существующему стилю кода ✅

## Execution Log

### Iteration 1 — Обязательные изменения (1-11)
- **Changed**: builder.go, classifier.go, json.go, paths.go, render.go, secrets.go, root.go, tasks/manager.go
- **Verified**: `go build ./...` OK, `go test ./...` — 2 test failures (trust label assertions)
- **Fixed**: profiles/manager_test.go, prompting/builder_test.go — обновлены assertions под новые trust-метки
- **Result**: все тесты проходят ✅

### Iteration 2 — Рекомендуемые изменения (12-30, кроме F006)
- **Changed**: fake.go, paths.go, manager.go, builder.go, render.go, classifier.go, root.go, errors.go, models.go, tasks/manager.go
- **Verified**: `go build ./...` — 1 compile error (redactHomePath signature), исправлен
- **Verified**: `go test ./...` — 2 test failures (profile render non-deterministic, day12 acceptance)
- **Fixed**: render.go — добавлена сортировка ключей мапы + Profile ID в рендер
- **Fixed**: fake.go — обновлены паттерны матчинга профилей под natural language формат
- **Result**: все тесты проходят ✅

### Not done
- **F006** (split root.go): крупный рефакторинг 1397-строчного файла. Не блокирует релиз, все остальные 29 изменений реализованы и протестированы.
