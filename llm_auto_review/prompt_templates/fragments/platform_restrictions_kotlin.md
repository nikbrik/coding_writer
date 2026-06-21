# Platform restrictions — Android / Kotlin

- Не проверяй NPE/null/nullable как самостоятельный баг в Kotlin, если нет явного Java/platform type, unsafe cast, reflection, внешнего API-контракта или видимого `!!`/force-cast в изменении.
- Не оценивай imports, форматирование, написание констант и нейминг как баги.
- DI boilerplate, `@Inject constructor(...)`, Moshi/serialization boilerplate и generated-code замечай только если diff реально меняет поведение или контракт данных.
- Для Android/Kotlin релевантны lifecycle/coroutine/Flow/Compose/navigation риски: scope, cancellation, dispatcher, `StateFlow` vs event stream, `LaunchedEffect` keys, route/back stack.
