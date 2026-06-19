---
name: evolve
description: Post-task harness evolution. Analyzes the current session, emits dry-run proposals with stable IDs, deduplicates learnings by pattern-key, and applies only explicitly approved updates to AGENTS.md, .kilo/rules/, .kilo/skills/, and .kilo/learnings/. Invoke after significant tasks with /evolve or "обнови harness".
---

# evolve — Harness Evolution Skill

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
- Активный project-skill path: `.kilo/skills`.
- Legacy path: `.kilo/skill`. Аудируй для понимания, но не двигай и не редактируй без явного запроса на миграцию.
- `name` в frontmatter должен совпадать с именем директории.

---

## ФАЗА 0 — Аудит harness

Прочитай, если существует:

```text
AGENTS.md
.kilo/kilo.jsonc
.kilo/skills/*/SKILL.md        # только frontmatter
.kilo/skill/*/SKILL.md         # legacy, только frontmatter
.kilo/rules/*.md
.kilo/learnings/LEARNINGS.md
.kilo/learnings/ERRORS.md
.kilo/learnings/HARNESS_CHANGELOG.md
scripts/validate-kilo-harness.mjs
```

Зафиксируй внутри:
- существующие skill names и paths;
- `skills.paths` из `.kilo/kilo.jsonc`;
- загружаемые rules из `instructions`;
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

## ФАЗА 3 — Классификация

Scope:
- `project` → `.kilo/rules/[category].md`, `AGENTS.md`, `.kilo/skills/`, `.kilo/learnings/`
- `global` → только предложи; не пиши в `~/.kilo/*` в этом repo-local harness
- `task` → отсеять

Тип изменения:
- `rule` → короткое правило в `.kilo/rules/[category].md` или `AGENTS.md`
- `skill` → новый `.kilo/skills/[name]/SKILL.md`
- `learning` → `.kilo/learnings/LEARNINGS.md`
- `error` → `.kilo/learnings/ERRORS.md`
- `promote` → existing learning с `Recurrence-Count >= 3` становится rule proposal

Promotion в `AGENTS.md` или `.kilo/rules/*`, если:
- signal встретился 3+ раза, или
- пользователь сказал "всегда", "никогда", "запомни", или
- касается критического поведения: git, filesystem, architecture, API contracts, secrets.

---

## ФАЗА 4 — Dedupe и recurrence

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

## ФАЗА 5 — Proposal format

Выводи machine-readable блок и краткое human summary:

```text
══════════════════════════════════════════════════
HARNESS EVOLUTION PROPOSAL | KiloCode
Task: [кратко]
Date: [YYYY-MM-DD]
Mode: dry-run
Signals: [N] | Proposals: [M]
══════════════════════════════════════════════════

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
target: .kilo/learnings/LEARNINGS.md
pattern-key: [kebab-case]
recurrence-count: [N]
summary: [1 строка]

[PROMOTE-001]
priority: HIGH
action: promote
target: .kilo/rules/[category].md
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
- prefix by type: `RULE`, `SKILL`, `LEARNING`, `ERROR`, `PROMOTE`;
- number from 001 in displayed order per type;
- keep IDs stable while user edits the current proposal.

---

## ФАЗА 6 — Apply rules

Before any write:
1. Re-read target file.
2. Show diff for each target.
3. For `AGENTS.md`, require explicit `y`.
4. Apply only selected IDs.
5. Never delete existing learnings/errors.
6. For new skills, validate no duplicate `name`.
7. Append changelog entry after successful apply.

Changelog target: `.kilo/learnings/HARNESS_CHANGELOG.md`

Changelog template:

```markdown
## [YYYY-MM-DD] | [task-summary]
**Applied**: RULE-001, LEARNING-001
**Files**: AGENTS.md, .kilo/learnings/LEARNINGS.md
**Reason**: [1 sentence]

---
```

After writes:
- run `node scripts/validate-kilo-harness.mjs`;
- run `ast-index update` when available;
- report created/updated counts and validation result.

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
