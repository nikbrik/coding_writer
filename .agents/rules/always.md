# Always Rules

## Search

- Use `ast-index` first for code search: files, symbols, classes, usages, refs, imports, deps, dependents, project map.
- Check index state before relying on it. If missing/stale, run `ast-index rebuild` once, then `ast-index update` after edits.
- Do not duplicate successful `ast-index` results with `rg`.
- Use `rg` only for regex, plain-text prose, unsupported file types, string literals, comments, or empty `ast-index` results.
- For large code files, use `ast-index outline` before full read.

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
