# Android архитектурные правила

## Слои архитектуры
Соблюдай Clean Architecture / многослойную архитектуру:
- **UI / Presentation** — Compose-экраны, ViewModel, UI-состояние
- **Domain** — use cases, бизнес-логика, модели (без зависимостей на фреймворк)
- **Data** — репозитории, источники данных (сеть, БД), DTO/маппинг

Правило зависимостей: UI → Domain → Data. Обратные зависимости запрещены.

## Что проверять
- Нарушение слоёв: UI не должен напрямую работать с data layer.
- ViewModel не должна иметь ссылок на Context, Activity, Fragment.
- Use case не должен зависеть от Android-фреймворка.
- DI-код должен быть изолирован в di-модулях.

## Compose
- State должен быть поднят (state hoisting) — данные приходят сверху, события уходят наверх.
- Нет тяжёлых вычислений в Compose-функциях — используй `remember`, `derivedStateOf`, `LaunchedEffect`.
- Коллекшены из Flow подписываются через `collectAsState()` или `collectAsStateWithLifecycle()`.
- `SideEffect` / `LaunchedEffect` используются осмысленно, без утечек.

## Coroutines / Flow
- `viewModelScope.launch` для ViewModel. Без глобальных скоупов.
- Flow-цепи не должны быть оборваны без `catch`.
- `StateFlow` / `SharedFlow` используются правильно: `StateFlow` для состояния, `SharedFlow` для одноразовых событий.
- Нет блокирующих вызовов (runBlocking) в production-коде.

## DI
- Модули должны быть явными, без反射.
- Scope (Singleton, FragmentScoped и т.д.) должен быть корректен.
