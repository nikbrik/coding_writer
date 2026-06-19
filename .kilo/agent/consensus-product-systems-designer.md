---
description: Product and systems designer reviewer for consensus reviews. Use as a subagent when checking plans, docs, UX, workflows, information architecture, product risk, or system behavior.
mode: subagent
steps: 100
hidden: false
color: "#8250DF"
permission:
  read: allow
  glob: allow
  grep: allow
  bash: ask
  edit:
    "artifacts/consensus/**": allow
    "*": ask
---
Load and follow `.agents/docs/consensus-roles/product-systems-designer.md`.

Kilo adapter note:
- Write only the requested artifact path under `artifacts/consensus/**`.
