---
description: Run multi-agent consensus review for a plan, docs, code, architecture, or arbitrary target.
---

Use the `consensus-orchestrator` skill to run a full multi-agent consensus review for:

`$ARGUMENTS`

If `$ARGUMENTS` is empty, infer the target from the current conversation. If no target can be inferred, ask one short question.

Follow the skill workflow exactly:

1. Create `Artifacts/consensus/<run-id>/`.
2. Run Round 1 expert reviews with the five consensus expert agents.
3. Run judge aggregation.
4. Run Round 2 cross-responses.
5. Run final judge verdict.
6. Return only a concise summary and artifact paths.

Do not apply source changes unless the user explicitly asks after the verdict.

Examples:

```text
/consensus docs/prd.md
/consensus review current implementation plan in .kilo/plans/foo.md
/consensus code changes in current branch
```
