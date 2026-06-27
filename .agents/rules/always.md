# Always Rules

Load these canonical shared rule files before work:

- `.agents/rules/search.md`
- `.agents/rules/goal-loop.md`
- `.agents/rules/harness-evolution.md`
- `.agents/rules/validation.md`

## Search

- Canonical search policy lives in `.agents/rules/search.md`.

## Validation

- Canonical validation policy lives in `.agents/rules/validation.md`.

## User-Facing Surface

- User-facing functionality and homework demos default to the real `cw` TUI. Do not substitute `cw chat --once`, `cw mcp ...`, direct storage edits, smoke scripts, or other CLI-only paths as acceptance proof unless the user explicitly asks for CLI/debug/smoke.
- CLI commands are allowed for setup, deterministic tests, recovery, and diagnostics. The primary product proof must still go through TUI interactions such as normal text input and slash commands.

## Style

- Default reply style: caveman ultra.
- Terse fragments OK. Drop filler, pleasantries, hedging.
- Use arrows for cause/effect when clear: `X -> Y`.
- Keep technical terms, code, paths, commands, URLs, API names, commit keywords, and exact error strings unchanged.
- Match user language. Russian prompt -> Russian answer, same caveman ultra compression.
- Use normal prose for irreversible actions, security warnings, or when compression risks ambiguity.

## Scope

- Repo-local only. Do not change global Codex, Brew, MCP, Claude, Cursor, or shell config unless user explicitly asks.
- Keep `day11.md`, `day12.md`, and `03-memory-state-notes.md` unchanged unless user asks.
- Runtime-specific directories such as `.kilo/` should act as adapters over shared policy in `.agents/` whenever possible.
