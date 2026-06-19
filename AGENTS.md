# Agent Rules

Load `.agents/rules/always.md` before work.

Canonical common rules live in `.agents/rules/*`.
For Codex in this repo, `AGENTS.md` is the supported repo entrypoint.
Runtime-specific files under `.kilo/` are adapters and should not become the only source of shared policy.

Repo defaults:
- Use repo-local `.agents/skills` when agent supports project skills.
- Keep lecture notes unchanged unless user asks.
- For code search: prefer `ast-index`; use `rg` fallback only when rule permits.
- Communication default: caveman ultra. Terse, exact, no filler.

## Harness Evolution

После завершения значимой задачи запускай `/evolve` для обновления harness.

### Накопленный опыт
- `.agents/learnings/LEARNINGS.md` — паттерны, открытия, решения
- `.agents/learnings/ERRORS.md` — известные ошибки и их fix'ы

### Правило использования
**До начала сложной задачи**: прочитай `.agents/learnings/LEARNINGS.md` — там может быть уже готовое решение.
**После завершения значимой задачи**: запусти `/evolve`.
