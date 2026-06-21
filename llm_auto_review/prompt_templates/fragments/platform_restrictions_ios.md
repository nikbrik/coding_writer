# Platform restrictions — iOS / Swift

- Не применяй Android/JVM-only запреты к Swift: optional handling, `!`, implicitly unwrapped optional, `try?`, nil fallback и decode failure можно ревьюить, если есть evidence из diff.
- Apple SDK imports (`Foundation`, `UIKit`, `SwiftUI`, `Combine`) сами по себе не являются finding.
- Проверяй MainActor/UI-thread нарушения, retain cycles в closure/Task/Combine, потерю `Task` cancellation, silent `catch`/`try?`, Codable fallback/default-value regressions и неверный navigation stack/dismiss target.
- Swift boilerplate, property wrappers и Codable declarations оценивай только через конкретный сценарий поломки: actor isolation, state propagation, decoding contract, lifecycle or navigation behavior.
