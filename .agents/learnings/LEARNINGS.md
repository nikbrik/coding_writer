# Project Learnings

> База знаний проекта. Пополняется через `/evolve` после значимых задач.
> Правило: добавляй новые записи append-only. Единственное допустимое изменение
> существующей записи — обновить `Recurrence-Count` и добавить краткую evidence
> строку для того же `pattern-key`.

<!-- LEARNINGS:START -->
## 2026-06-19 | day15-live-demo-model
**Тип**: correction
**Модуль**: manual testing
**Приоритет**: HIGH
**Recurrence-Count**: 1

### Суть
Live пользовательские demo/scenario прогоны Day 15 должны идти через OpenRouter model `google/gemini-3.1-flash-lite`, как указано в `docs/manual-testing-demo.md`. Fake provider допустим только для deterministic CI/smoke, не как доказательство live user scenario.

### Когда важно
Когда запускается ручной пользовательский сценарий, live demo, запись видео или проверка OpenRouter dashboard calls.

### Применение
Перед live-прогоном выполнить `export ASSISTANT_MODEL="google/gemini-3.1-flash-lite"`, `unset ASSISTANT_PROVIDER`, `unset ASSISTANT_LLM_VALIDATION`; не подменять модель на `openai/gpt-4.1-mini`, DeepSeek или `fake/model`.

### Evidence
- 2026-06-19 пользователь указал, что live scenario должен тестироваться через Gemini 3.1 Flash Lite; попытка запустить `openai/gpt-4.1-mini` была остановлена.

---

## 2026-06-19 | real-mode-semantic-validation-only
**Тип**: correction
**Модуль**: process validation
**Приоритет**: HIGH
**Recurrence-Count**: 2

### Суть
В real/OpenRouter mode semantic decisions должны идти через LLM structured validator; keyword/regex heuristics допустимы только для hard safety или prefilter, не как fallback decision по смыслу.

### Когда важно
Когда меняется process controller, transition routing, execution validation или manual acceptance для real CLI.

### Применение
Не добавлять semantic validators вида `containsAny`/regex для product real-mode. Для смысловых проверок использовать out-of-band LLM referee со strict structured JSON output (`verdict`/`findings`), а локально оставлять schema/safety gates без trigger-word fallback для intent/readiness/acceptance.

### Evidence
- 2026-06-19 пользователь указал, что `autoVerificationIntent` с `containsAnyText` нарушает запрет keyword-based semantic validation; auto verification intent должен идти через semantic referee.

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
## 2026-06-19 | internal-llm-json-tolerant-final-json-strict
**Тип**: pattern
**Модуль**: internal LLM JSON parsing
**Приоритет**: MEDIUM
**Recurrence-Count**: 1

### Суть
Для internal LLM helper outputs использовать tolerant parsing и semantic revalidation; для final user-visible/stage schemas оставлять strict parsing.

### Когда важно
Когда меняются `PromptImprover`, `PlanningSwarm`, internal agent helpers или любые промежуточные LLM JSON outputs, которые не являются финальным публичным contract.

### Применение
Internal helper output можно чинить tolerant parser'ом, bounded retry и последующей semantic/schema revalidation. Final user-visible output, stage schemas, lifecycle decisions и persisted contracts должны оставаться strict JSON/schema и не принимать "почти валидный" ответ.

### Evidence
- 2026-06-19 Day 15 live/manual flow потребовал tolerant handling для internal helper JSON, но final validation/lifecycle contracts остались strict.

---
## 2026-06-19 | readme-feature-docs-architecture-first
**Тип**: pattern
**Модуль**: documentation
**Приоритет**: MEDIUM
**Recurrence-Count**: 1

### Суть
README для новой фичи должен сначала объяснять систему, компоненты, state flow и схемы; ссылки/commands идут после, не вместо архитектурного описания.

### Когда важно
Когда добавляется крупная фича, новая demo-дока, architecture/FRD/PRD update или README summary для пользовательской записи demo.

### Применение
Начинать с product/system overview: какие компоненты участвуют, кто владеет state, какие gates/agents/evidence есть, как выглядит happy path и failure path. Команды, ссылки на demo scripts и troubleshooting добавлять после этого как операционный слой.

### Evidence
- 2026-06-19 README/demo docs для Day 15 сначала были сведены к ссылкам/commands; пользователь потребовал консистентное описание системы со схемами.

---
<!-- LEARNINGS:END -->
