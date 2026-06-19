# Project Learnings

> База знаний проекта. Пополняется через `/evolve` после значимых задач.
> Правило: добавляй новые записи append-only. Единственное допустимое изменение
> существующей записи — обновить `Recurrence-Count` и добавить краткую evidence
> строку для того же `pattern-key`.

<!-- LEARNINGS:START -->
## 2026-06-19 | real-mode-semantic-validation-only
**Тип**: correction
**Модуль**: process validation
**Приоритет**: HIGH
**Recurrence-Count**: 1

### Суть
В real/OpenRouter mode semantic decisions должны идти через LLM structured validator; keyword/regex heuristics допустимы только для hard safety или fake fallback.

### Когда важно
Когда меняется process controller, transition routing, execution validation или manual acceptance для real CLI.

### Применение
Не добавлять semantic validators вида `containsAny`/regex для product real-mode. Для смысловых проверок использовать out-of-band LLM referee со strict structured JSON output (`verdict`/`findings`), а локально оставлять schema/safety gates.

---

## 2026-06-19 | manual-suite-rerun-failed-only
**Тип**: pattern
**Модуль**: manual testing
**Приоритет**: HIGH
**Recurrence-Count**: 1

### Суть
После полного live manual suite перезапускать только failed/ambiguous cases; отделять product bug от checker/model-language variance.

### Когда важно
Когда manual suite использует live OpenRouter models и стоит дорого/долго.

### Применение
Сохранять evidence в run-scoped dirs, читать логи упавших кейсов, чинить реальные bugs, а затем rerun только failed/ambiguous cases. Если checker ищет конкретный язык/формулировку, сверять фактическое состояние storage/audit.

---
## 2026-06-19 | goal-x-refresh-stale-goal
**Тип**: correction
**Модуль**: goal-x
**Приоритет**: HIGH
**Recurrence-Count**: 1

### Суть
При `goal-x` existing `goal.md` может относиться к прошлой задаче; если контекст не совпадает с текущим user request, goal нужно заменить или обновить перед implementation.

### Когда важно
Когда пользователь вызывает `goal-x`, а `goal.md` уже существует в repo.

### Применение
Сначала прочитать `goal.md`; если он stale, записать текущие context, acceptance criteria, constraints, blast radius и log для новой задачи.

### Evidence
- 2026-06-19 текущий `goal.md` был про CLI/manual-suite, а задача была про harness evolution.

---

## 2026-06-19 | harness-validator-frontmatter-edge-cases
**Тип**: pattern
**Модуль**: harness validator
**Приоритет**: MEDIUM
**Recurrence-Count**: 1

### Суть
Harness validator должен возвращать понятные errors на malformed frontmatter и ловить multiline/continuation `description`.

### Когда важно
Когда меняется validator или добавляются новые `.kilo/skills/*/SKILL.md`.

### Применение
Проверять missing fences, duplicate names, directory/name mismatch, YAML block scalars и indented continuation lines после `description`.

### Evidence
- 2026-06-19 self-review нашёл и исправил malformed-frontmatter fallback и continuation handling.

---

## 2026-06-19 | patch-smaller-after-context-drift
**Тип**: workaround
**Модуль**: apply_patch workflow
**Приоритет**: LOW
**Recurrence-Count**: 1

### Суть
Если большой `apply_patch` не находит expected lines, нужно re-read exact context и разбить patch на меньшие независимые куски.

### Когда важно
Когда multi-file patch меняет документы, которые могли чуть отличаться от ожидаемого текста.

### Применение
Сначала перечитать failing target range, затем применить smaller patches по файлам или секциям.

### Evidence
- 2026-06-19 multi-file patch для learnings/errors/evolve skill не применился из-за небольшой разницы текста.

---
<!-- LEARNINGS:END -->
