# iOS / Swift — типовые баг-паттерны (подсказки)

Каталог **не** заменяет truth tables, BVA, trace или инварианты. Используй пункт только если в diff или surrounding context есть **подтверждающий** признак. Без evidence в prompt/context — не создавай finding.

| # | Симптом в snippet | Что проверить |
|---|-------------------|---------------|
| 1 | UI update вне `@MainActor` / main queue | `@MainActor`, `DispatchQueue.main.async`, SwiftUI state на main |
| 2 | Closure без `[weak self]` / `unowned` на `self` | Retain cycle в ViewController / coordinator |
| 3 | `!` force unwrap optional | `guard let`, `if let`, default vs crash path |
| 4 | `Codable` optional vs required key mismatch | Decode failure, silent nil, versioning полей |
| 5 | `if isPremium` → paywall / lock | Инверсия entitlement vs `expectations` |
| 6 | Feature flag / experiment только в одном месте | Согласованность router и gate |
| 7 | `Task { }` без отмены при dismiss | `task.cancel()`, `onDisappear`, weak lifecycle |
| 8 | Shared `var` / static cache без isolation | Actor, `@MainActor`, lock для mutable state |
| 9 | `async` race: check-then-act без await barrier | TOCTOU; serial queue / actor |
| 10 | Navigation `pop` / `dismiss` vs expected screen | Stack depth, deep link destination |
| 11 | `switch` на enum без `default` / нового case | Исчерпывающность при добавлении case |
| 12 | `onAppear` / `task` дублирует side effect | Идемпотентность, debounce, stable id |
| 13 | Тяжёлая работа в SwiftUI `body` | Вынести в model / `.task` off main |
| 14 | Delegate `strong` reference to parent | `weak var delegate` |
| 15 | Combine / async error swallowed | `catch`, `Result`, пользовательский feedback |
| 16 | `NotificationCenter` observer без `removeObserver` | Leak / duplicate handling |
| 17 | Network / decode на main thread | Background queue + main для UI |
| 18 | Keychain / sensitive data в UserDefaults | Хранение и доступ по контракту |
| 19 | Deep link / universal link route | Target screen vs expectation |
| 20 | `viewDidLoad` vs SwiftUI `.onAppear` дубль | Двойной fetch / double navigation |
| 21 | Timer / `DispatchSource` не invalidated | Invalidate on deinit / disappear |
| 22 | `UIImage` / large data sync on main | Async load, placeholder state |
| 23 | `Codable` `decodeIfPresent` vs business required | Nil path ломает invariant |
| 24 | State machine: event без valid `from` | Transition table vs `transition:` evidence |
| 25 | Off-by-one в `indices.last`, slice | BVA на краю массива |

## Few-shot (negative)

**BAD:** «Может быть retain cycle» — без closure/`self` в snippet.

**GOOD:** `pattern: closure captures self strongly in ProfileVC.swift:88 -> expected [weak self], actual strong capture; evidence: Button action { self.reload() } without weak`
