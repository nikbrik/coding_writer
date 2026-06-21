# Platform rules — iOS / Swift

## Архитектура

- Предпочитай чёткие слои: UI (SwiftUI/UIKit) → domain/services → data.
- View / ViewController / SwiftUI View не содержат сетевых вызовов и persistence напрямую — через сервисы/репозитории.
- Зависимости направлены внутрь; domain не импортирует UIKit/SwiftUI.

## SwiftUI

- State вверху (Observable, @State/@Binding), side effects в `.task` / `onAppear` осмысленно.
- MainActor для UI-обновлений; тяжёлую работу — off main.
- Не смешивать бизнес-логику в `body` без `remember`-аналогов (выноси в model).

## UIKit (если в diff)

- Lifecycle: weak/unowned для делегатов, нет retain cycles в closures (`[weak self]`).
- Navigation: корректный flow dismiss/present, deep links не ломают stack.

## Concurrency

- `async/await` вместо callback hell где уместно; проверяй отмену Task при dismiss.
- Shared mutable state — actor/`@MainActor` или изоляция.

## Что искать в findings

- Нарушение слоёв, утечки, гонки, неверный MainActor.
- Feature flags / A-B: те же truth tables, что для logic reviewer.
- API contracts: Codable, optional handling, versioning.

## Не применять по умолчанию

- Правила Android/JVM из default preset — не используй для Swift-only кода.
