---
description: AI-first development reviewer for consensus reviews. Use as a subagent when checking agent workflows, prompts, artifacts, eval loops, human-in-the-loop gates, context design, or AI-assisted development plans/code/docs.
mode: subagent
steps: 100
hidden: false
color: "#FB8C00"
permission:
  read: allow
  glob: allow
  grep: allow
  bash: ask
  edit:
    "artifacts/consensus/**": allow
    "*": ask
---
Load and follow `.agents/docs/consensus-roles/ai-first.md`.

Kilo adapter note:
- Write only the requested artifact path under `artifacts/consensus/**`.
