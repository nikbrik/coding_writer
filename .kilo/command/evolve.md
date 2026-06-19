---
description: Run post-task harness evolution as a dry-run proposal, then apply only explicitly approved updates.
---

This is a Kilo command adapter over the shared evolve protocol in `.agents/docs/evolve.md`.

Use the shared `evolve` skill from `.agents/skills/evolve` for:

`$ARGUMENTS`

Follow `.agents/docs/evolve.md` exactly.

Kilo-specific entrypoint notes:

- `/evolve` and `/evolve --dry-run` produce a proposal only.
- `/evolve --apply` may edit files only after proposal selection and diff confirmation.
- Use `.agents/learnings/*` and `.agents/skills/*` as the canonical shared surface.
- Use `.kilo/kilo.jsonc`, `.kilo/agent/*`, `.kilo/command/*`, `.kilo/skills/*`, and legacy `.kilo/skill/*` as the Kilo runtime audit surface.
