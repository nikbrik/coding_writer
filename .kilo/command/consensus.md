---
description: Run multi-agent consensus review for a plan, docs, code, architecture, or arbitrary target.
---

This is a Kilo adapter over the shared consensus protocol in `.agents/docs/consensus-orchestrator.md`.

Use the shared `consensus-orchestrator` skill from `.agents/skills/consensus-orchestrator` to run a full multi-agent consensus review for:

`$ARGUMENTS`

If `$ARGUMENTS` is empty, infer the target from the current conversation. If no target can be inferred, ask one short question.

Follow `.agents/docs/consensus-orchestrator.md` exactly.

Kilo-specific entrypoint notes:

- Use the shared `consensus-orchestrator` skill and `.kilo/agent/*` reviewer roles when available.
- Keep artifact paths under `artifacts/consensus/**`.
- Stay read-only unless the user explicitly asks for source edits after the verdict.

Examples:

```text
/consensus diff HEAD~1..HEAD
/consensus staged changes
/consensus unstaged changes
/consensus code changes in current branch
/consensus PR #123
/consensus file .kilo/plans/foo.md
/consensus docs/prd.md
/consensus review current implementation plan in .kilo/plans/foo.md
/consensus pasted diff below
```
