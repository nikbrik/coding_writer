# evolve — Harness Evolution Skill

Главная цель `evolve` — улучшать agent harness как систему: правила,
скиллы, проверки, adapters, learnings и known errors должны делать будущую
работу агента надежнее. `evolve` не должен превращать баги продукта,
пробелы документации или слабую архитектуру приложения в инструкции
"обходить проблему осторожнее".

## Когда использовать
- После завершения большой фичи, рефакторинга, сложной отладки
- Когда агент был поправлен пользователем 2+ раза за сессию
- Когда найден нетривиальный workaround или неочевидное решение
- По запросу: `/evolve`, `/evolve --dry-run`, `/evolve --apply`, "evolve harness", "обнови harness", "что мы узнали"

## Режимы
- `/evolve` и `/evolve --dry-run` — только proposal, без записи файлов.
- `/evolve --apply` — применяй только выбранные proposal IDs после показа diff.
- Follow-up controls: `apply all`, `apply RULE-001 LEARNING-002`, `skip type:skill`, `edit ID`, `n`.

Если ввод неоднозначен, оставайся в dry-run и не меняй файлы.

## Путь скиллов
- Активный shared project-skill path: `.agents/skills`.
- Runtime adapter paths may exist, for example `.kilo/skills` and legacy `.kilo/skill`. Аудируй для понимания, но не двигай и не редактируй без явного запроса на миграцию.
- `name` в frontmatter должен совпадать с именем директории.

---

## ФАЗА 0 — Аудит harness

Прочитай, если существует:

```text
AGENTS.md
.agents/rules/*.md
.agents/skills/*/SKILL.md       # только frontmatter
.agents/learnings/LEARNINGS.md
.agents/learnings/ERRORS.md
.agents/learnings/HARNESS_CHANGELOG.md
.kilo/kilo.jsonc
.kilo/agent/*.md                # runtime adapters
.kilo/command/*.md              # runtime adapters
.kilo/skills/*/SKILL.md         # runtime adapters, только frontmatter
.kilo/skill/*/SKILL.md          # legacy runtime adapters, только frontmatter
.kilo/rules/*.md
scripts/validate-kilo-harness.mjs
```

Зафиксируй внутри:
- существующие skill names и paths;
- `skills.paths` из runtime configs, включая `.kilo/kilo.jsonc`;
- загружаемые rules из runtime `instructions`;
- existing `pattern-key` и `Recurrence-Count` в learnings;
- existing known errors;
- есть ли validator и changelog markers.

---

## ФАЗА 1 — Сбор сигналов

Просмотри историю текущего разговора. Ищи сигналы пяти типов:

**Тип A — Коррекции пользователя** [HIGH]
- "нет", "не так", "стоп", "не используй X", "всегда делай Y"
- Пользователь отменил действие агента и объяснил почему
- Одна и та же правка применена 2+ раз подряд

**Тип B — Нетривиальные решения** [HIGH]
- Решение потребовало 3+ итераций или смены подхода
- Найден workaround для проблемы инструмента/библиотеки/окружения
- Обнаружена неочевидная зависимость между компонентами

**Тип C — Паттерны проекта** [MEDIUM]
- Повторяющаяся последовательность действий 3+ раз
- Соглашение проекта отсутствует в AGENTS.md/rules
- Архитектурное решение с объяснённым "почему"

**Тип D — Провалы с причинами** [MEDIUM]
- Команда упала: команда, симптом, причина, fix
- Тест провалился неожиданным образом
- Инструмент повёл себя иначе чем ожидалось

**Тип E — Новые находки** [LOW]
- Более эффективный способ делать стандартное действие
- Полезная команда/утилита для проекта
- Паттерн, достаточно самостоятельный для отдельного skill

---

## ФАЗА 2 — Фильтры

Отсеивай:
- разовые детали без повторяемого паттерна;
- уже существующие rules/skills/learnings без нового evidence;
- внешние сервисные сбои вне зоны контроля;
- расплывчатые советы без проверяемого действия;
- sensitive data: tokens, API keys, private URLs, emails, credential paths, full secret-like values.

Если signal содержит secret-like data:
- редактируй значение до `[REDACTED]`;
- не сохраняй raw command args с секретами;
- не цитируй private pasted content целиком.

Anti-bloat:
- показывай максимум 5 proposals за run;
- сортируй HIGH → MEDIUM → LOW;
- LOW показывай только если переиспользуемость очевидна;
- один `pattern-key` → один proposal.

---

## ФАЗА 3 — Root cause gate

Перед выбором типа proposal классифицируй root cause каждого сигнала:

- `product_bug` → дефект приложения, UX, state machine, API contract,
  verifier, persistence, execution, concurrency, security или tests.
- `docs_gap` → пользовательская/product документация неполная, вводит в
  заблуждение или не описывает expected behavior.
- `agent_process_error` → агент нарушил уже существующее правило, skill,
  acceptance criteria или explicit user instruction.
- `harness_gap` → в harness нет правила, skill, validator, adapter,
  checklist или memory, которые должны были направить агента правильно.
- `environment_issue` → локальная среда, сеть, sandbox, provider outage,
  missing secret или tool limitation.

Критическое правило:

- Не кодируй `product_bug` или `docs_gap` как агентское правило вида
  "обходить X", "ждать Y", "не трогать Z", если правильный fix должен быть
  в продукте или документации.
- Для `product_bug` основной proposal должен быть `PRODUCT-BUG`.
- Для `docs_gap` основной proposal должен быть `DOC-BUG`.
- `LEARNING`/`ERROR` для workaround допустим только как вторичный proposal и
  обязан ссылаться на основной `PRODUCT-BUG`/`DOC-BUG`.
- Если root cause смешанный, раздели proposals: продуктовый bug отдельно,
  harness improvement отдельно.

Пример:

- Плохо: `LEARNING: demo harness должен ждать обновления state`.
- Хорошо: `PRODUCT-BUG: app lacks per-session turn serialization`.
- Допустимо дополнительно: `ERROR: demo harness exposed race before product
  serialization existed`.

---

## ФАЗА 4 — Harness improvement design

Для каждого сигнала, который действительно относится к harness, продумай
долгосрочное улучшение, а не только запись в память.

Задай вопросы:

- Какой guardrail предотвратит этот класс ошибок в будущем?
- Достаточно ли learning, или нужен validator/test/checklist/skill/rule?
- Можно ли автоматически обнаружить нарушение до того, как пользователь его
  увидит?
- Как proposal будет проверяться: validator, grep-safe invariant, test,
  checklist или review gate?
- Не маскирует ли proposal баг продукта вместо того, чтобы поднять его как
  bug?

Предпочитай сильные harness changes:

- `validator` → новая/усиленная проверка harness, schemas, skill metadata,
  forbidden patterns, proposal quality.
- `skill` → repeatable workflow с clear trigger, steps, verification.
- `rule` → короткое обязательное правило для критичного поведения.
- `learning` → reusable pattern, если автоматизировать пока рано.
- `error` → known failure с repro/fix, не как замена product fix.
- `product-bug` / `doc-bug` → backlog item в `BUGS/` с repro и acceptance criteria.

Каждый non-bug proposal должен проходить Harness Value Test:

```text
Does this make future agent work more correct, safer, faster, or easier to
verify without hiding a product/documentation defect?
```

Если ответ "нет" или "только помогает обходить баг", proposal должен стать
`PRODUCT-BUG`, `DOC-BUG` или быть отброшен.

---

## ФАЗА 5 — Классификация

Scope:
- `project` → `.agents/rules/[category].md`, `AGENTS.md`, `.agents/skills/`, `.agents/learnings/`, runtime adapters when needed
- `global` → только предложи; не пиши в global agent configs (`~/.kilo/*`, `~/.codex/*`, etc.) в этом repo-local harness
- `task` → отсеять

Тип изменения:
- `rule` → короткое правило в `.agents/rules/[category].md` или `AGENTS.md`
- `skill` → новый `.agents/skills/[name]/SKILL.md`
- `learning` → `.agents/learnings/LEARNINGS.md`
- `error` → `.agents/learnings/ERRORS.md`
- `validator` → новая или измененная автоматическая проверка harness
- `promote` → existing learning с `Recurrence-Count >= 3` становится rule proposal
- `product-bug` → дефект продукта в `BUGS/` или issue tracker
- `doc-bug` → дефект документации в `BUGS/` или docs backlog

Promotion в `AGENTS.md` или `.agents/rules/*`, если:
- signal встретился 3+ раза, или
- пользователь сказал "всегда", "никогда", "запомни", или
- касается критического поведения: git, filesystem, architecture, API contracts, secrets.

---

## ФАЗА 6 — Dedupe и recurrence

Для каждого learning/error вычисли `pattern-key`:
- lowercase kebab-case;
- стабильный смысловой ключ, не дата;
- не включай имена секретов, токены, случайные IDs.

Если `pattern-key` уже есть в `LEARNINGS.md`:
- не создавай новую дублирующую запись;
- proposal должен быть `LEARNING-### action:update`;
- увеличь `Recurrence-Count`;
- добавь одну короткую `Evidence:` строку к existing entry.

Если похожая ошибка уже есть в `ERRORS.md`:
- proposal должен быть `ERROR-### action:update`;
- обнови `Статус` только если новый evidence меняет состояние;
- добавь краткий evidence/fix note.

Если `Recurrence-Count` после update станет `>= 3`, создай отдельный `PROMOTE-###` proposal.

---

## ФАЗА 7 — Proposal format

Выводи machine-readable блок и краткое human summary:

```text
══════════════════════════════════════════════════
HARNESS EVOLUTION PROPOSAL | Agent Harness
Task: [кратко]
Date: [YYYY-MM-DD]
Mode: dry-run
Signals: [N] | Proposals: [M]
══════════════════════════════════════════════════

Root causes:
  product_bug: [N]
  docs_gap: [N]
  agent_process_error: [N]
  harness_gap: [N]
  environment_issue: [N]

[RULE-001]
priority: HIGH
action: append
target: AGENTS.md
section: Harness Evolution
pattern-key: [kebab-case]
summary: [1 строка]
source: [коротко]

[LEARNING-001]
priority: MEDIUM
action: create | update
target: .agents/learnings/LEARNINGS.md
pattern-key: [kebab-case]
recurrence-count: [N]
summary: [1 строка]
harness-value: [почему это улучшает harness, а не обходит баг]

[VALIDATOR-001]
priority: HIGH
action: create | update
target: scripts/validate-kilo-harness.mjs
pattern-key: [kebab-case]
summary: [1 строка]
verifies: [какой invariant/check будет ловиться автоматически]

[PRODUCT-BUG-001]
priority: HIGH
action: create
target: BUGS/[pattern-key].md
pattern-key: [kebab-case]
summary: [1 строка]
symptom: [что видит пользователь]
expected: [что должно быть]
repro: [короткий repro без секретов]
acceptance: [как понять, что баг исправлен]

[DOC-BUG-001]
priority: MEDIUM
action: create
target: BUGS/[pattern-key].md
pattern-key: [kebab-case]
summary: [1 строка]
missing-doc: [что должно быть описано]
acceptance: [как понять, что документация исправлена]

[PROMOTE-001]
priority: HIGH
action: promote
target: .agents/rules/[category].md
from-pattern-key: [kebab-case]
summary: [1 строка]

Apply controls:
  apply all
  apply RULE-001 LEARNING-001
  skip type:skill
  edit RULE-001
  n
```

Stable IDs:
- prefix by type: `RULE`, `SKILL`, `LEARNING`, `ERROR`, `VALIDATOR`, `PRODUCT-BUG`, `DOC-BUG`, `PROMOTE`;
- number from 001 in displayed order per type;
- keep IDs stable while user edits the current proposal.

---

## ФАЗА 8 — Apply rules

Before any write:
1. Re-read target file.
2. Show diff for each target.
3. For `AGENTS.md`, require explicit `y`.
4. Apply only selected IDs.
5. Never delete existing learnings/errors.
6. For new skills, validate no duplicate `name`.
7. For `PRODUCT-BUG`/`DOC-BUG`, create target file if missing with stable
   append-only sections and enough repro/acceptance detail to act on.
8. For `VALIDATOR`, include at least one self-check or explain why existing
   validator coverage is sufficient.
9. Append changelog entry after successful apply.

Changelog target: `.agents/learnings/HARNESS_CHANGELOG.md`

Changelog template:

```markdown
## [YYYY-MM-DD] | [task-summary]
**Applied**: RULE-001, LEARNING-001
**Files**: AGENTS.md, .agents/learnings/LEARNINGS.md
**Reason**: [1 sentence]

---
```

After writes:
- run `node scripts/validate-kilo-harness.mjs` when available;
- run `ast-index update` when available;
- report created/updated counts and validation result.

Apply-mode quality gate:

- Do not apply a proposal that fails the Harness Value Test.
- Do not apply a workaround-only learning when the root cause is an open
  `product_bug`/`docs_gap` and no corresponding bug proposal exists.
- If user says `apply all`, still skip proposals that violate these gates and
  report why they were not applied.

---

## Шаблон нового скилла

```markdown
---
name: [skill-name]
description: [ОДНА строка до 1024 символов — что делает, когда применять, триггерные слова]
metadata:
  extracted-from-session: [YYYY-MM-DD]
  pattern-key: [kebab-case]
  recurrence-count: 1
---

# [Название]

## Проблема
[Что происходит, симптомы, когда возникает]

## Когда применять
- [Конкретная ситуация 1]
- [Конкретная ситуация 2]

## Решение

### Шаг 1
[Действие]

### Шаг 2
[Действие]

## Проверка
[Как убедиться что сработало]

## Ограничения
[Где этот подход не работает]
```

Критические правила:
- `name`: lowercase letters, digits, hyphens, max 64 chars;
- directory name = `name`;
- `description`: one line, max 1024 chars, no YAML block scalar.

## Шаблон LEARNINGS.md

```markdown
## [YYYY-MM-DD] | [pattern-key]
**Тип**: correction | pattern | discovery | workaround
**Модуль**: [component/tool]
**Приоритет**: HIGH | MEDIUM | LOW
**Recurrence-Count**: 1

### Суть
[1-2 предложения]

### Когда важно
[условия]

### Применение
[конкретное действие]

### Evidence
- [YYYY-MM-DD] [short redacted evidence]

---
```

## Шаблон ERRORS.md

```markdown
## [YYYY-MM-DD] | [command-or-component]
**Pattern-Key**: [pattern-key]
**Команда**: `[redacted command]`
**Симптом**: [что видно]
**Причина**: [почему]
**Fix**: [что сработало]
**Статус**: resolved | recurring | workaround

### Evidence
- [YYYY-MM-DD] [short redacted evidence]

---
```
