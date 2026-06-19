---
description: Run post-task harness evolution as a dry-run proposal, then apply only explicitly approved updates.
---

Use the `evolve` skill for:

`$ARGUMENTS`

Default behavior:

- `/evolve` and `/evolve --dry-run` produce a proposal only. Do not edit files.
- `/evolve --apply` may edit files only after the user selects proposal IDs and confirms diffs.
- `apply RULE-001 LEARNING-002`, `apply all`, `skip type:skill`, and `edit ID` are valid follow-up controls.

Follow the skill workflow exactly:

1. Audit `AGENTS.md`, `.kilo/kilo.jsonc`, `.kilo/skills/*/SKILL.md`, `.kilo/rules/*.md`, `.kilo/learnings/*`, and legacy `.kilo/skill/` if present.
2. Extract session signals, classify them, filter duplicates, and redact sensitive data before proposal output.
3. Emit at most five stable proposal IDs, ordered by priority.
4. In apply mode, show diffs before writes and never change `AGENTS.md` without explicit confirmation.
5. After writes, append `.kilo/learnings/HARNESS_CHANGELOG.md`.
6. Run `node scripts/validate-kilo-harness.mjs` and report the result.

Path policy:

- `.kilo/skills` is the configured active project-skill path.
- `.kilo/skill` is legacy content. Do not move or edit it unless the user explicitly asks for migration.
