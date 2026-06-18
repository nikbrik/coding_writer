# Agent Rules

Load `.agents/rules/always.md` before work.

Repo defaults:
- Use repo-local `.agents/skills` when agent supports project skills.
- Keep lecture notes unchanged unless user asks.
- For code search: prefer `ast-index`; use `rg` fallback only when rule permits.
- Communication default: caveman ultra. Terse, exact, no filler.

## Harness Evolution

После завершения значимой задачи запускай `/evolve` для обновления harness.

### Накопленный опыт
- `.kilo/learnings/LEARNINGS.md` — паттерны, открытия, решения
- `.kilo/learnings/ERRORS.md` — известные ошибки и их fix'ы

### Правило использования
**До начала сложной задачи**: прочитай `.kilo/learnings/LEARNINGS.md` — там может быть уже готовое решение.
**После завершения значимой задачи**: запусти `/evolve`.
