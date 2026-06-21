# Platform rules — Android / Kotlin (default)

Объединённые правила для архитектурного ревью Android-проектов. Проекты на другом стеке: создайте `.llm-review/platform` с `ios` или положите свой `.llm-review/fragments/platform_rules.md`.

## Kotlin — логика и условия

- Не раздувай NPE без Java/platform types, reflection, unsafe cast.
- Проверяй инверсию условий, feature flags, согласованность веток.
- Для 2+ независимых флагов — truth/decision tables (см. logic reviewer).

## Android — архитектура

- Clean Architecture: UI → Domain → Data, без обратных зависимостей.
- ViewModel без Context/Activity; use cases без Android framework.
- Compose: state hoisting, `collectAsStateWithLifecycle`, без тяжёлой работы в composables.
- Coroutines: `viewModelScope`, Flow с `catch`, без `runBlocking` в production.
- DI: явные модули, корректные scope.

Полный пример для overlay в app-репо: `.llm-review.example/fragments/platform_rules.md.example` (Kotlin + Android + запреты из single-пресетов).
