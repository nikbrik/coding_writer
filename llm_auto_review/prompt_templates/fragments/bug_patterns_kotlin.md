# Kotlin / Android — типовые баг-паттерны (подсказки)

Каталог **не** заменяет truth tables, BVA, trace или инварианты. Используй пункт только если в diff или surrounding context есть **подтверждающий** признак. Без evidence в prompt/context — не создавай finding.

| # | Симптом в snippet | Что проверить |
|---|-------------------|---------------|
| 1 | `GlobalScope`, `CoroutineScope()` без lifecycle | Scope привязан к `viewModelScope` / `lifecycleScope` / structured concurrency; отмена при destroy |
| 2 | `launch` / `async` на `Dispatchers.Main` для IO/диска | Dispatcher: IO/Default для блокирующей работы; Main только для UI |
| 3 | `runBlocking` вне тестов | Блокировка потока в production path, ANR, deadlock с Main |
| 4 | `!!` на nullable без контракта | Заменить на `?.`, guard, или явный require с доказательством non-null |
| 5 | `if (isPremium)` → paywall / deny | Инверсия флага доступа; согласованность с `expectations` |
| 5b | `if (isPremium) navigate` + `else` curtain | Записать исход для true и false отдельно до PASS (guard) |
| 6 | `if (isEnabled)` → block / hide feature | Направление `isEmpty`/`isNotEmpty`, `!flag` перепутан |
| 7 | `isEmpty()` / `isNotEmpty()` в условии доступа | Инверсия предиката vs ожидаемая ветка |
| 8 | `in` / `!in` для allowlist/denylist | Перепутан список; default branch при «не в списке» |
| 9 | `else` отсутствует при 2+ флагах | Default path: куда попадает поток когда все флаги off |
| 10 | Ранний `return` в середине ветвления | Остальные сценарии из `expectations` всё ещё достижимы |
| 11 | Сравнение строк без `trim` / регистра | `.lowercase()`, `equals(ignoreCase=true)` с обеих сторон |
| 12 | `return true` в catch / on error | Ошибка маскируется как успех vs expectation |
| 13 | Общий `var` / `companion object` cache без sync | Гонка при concurrent read/write; thread-safety |
| 14 | `SharedFlow` / Channel как единственное UI state | Потеря последнего состояния; предпочтительно `StateFlow` для state |
| 15 | Flow chain без `.catch` / silent `catch {}` | Ошибка не доходит до UI/логики; cancellation |
| 16 | `collect` / `collectAsState` без lifecycle в Compose | `collectAsStateWithLifecycle`, scope effect |
| 17 | `LaunchedEffect` без стабильного key | Перезапуск при каждой recomposition, дубли запросов |
| 18 | Тяжёлая работа в composable `body` | Вынесено в `remember` / ViewModel / `LaunchedEffect` |
| 19 | ViewModel с `Context` / `Activity` | Утечка lifecycle; инъекция Application-level interface |
| 20 | Domain/UI импорт Android framework | Граница слоёв: UI → Domain → Data |
| 21 | UI напрямую использует network/DTO model | Маппинг в domain/presentation model |
| 22 | Feature flag только в одном месте ветвления | Консистентность: связанные router/gate проверяют тот же флаг |
| 23 | Navigation `navigate` / deep link route id | Destination vs expectation (wrong screen, pop вместо push) |
| 24 | `suspend` вызов без cancellation propagation | `ensureActive`, не глотать `CancellationException` |
| 25 | Off-by-one на `last`, `size-1`, range | BVA на краю коллекции совместно с trace при цикле |

## Few-shot (negative)

**BAD:** «Возможен GlobalScope» — без строки в snippet и без механизма поломки.

**GOOD:** `pattern: GlobalScope.launch in Repository.kt:42 -> expected viewModelScope-bound work, actual application-wide scope survives configuration change; evidence from snippet: GlobalScope.launch { repo.refresh() }`
