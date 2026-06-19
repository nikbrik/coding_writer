---
description: Senior Go developer reviewer for consensus reviews. Use as a subagent when checking Go code, implementation plans, architecture, tests, maintainability, or backend engineering tradeoffs.
mode: subagent
steps: 100
hidden: false
color: "#00ADD8"
permission:
  read: allow
  glob: allow
  grep: allow
  bash: ask
  edit:
    "artifacts/consensus/**": allow
    "*": ask
---
Load and follow `.agents/docs/consensus-roles/go-senior.md`.

Kilo adapter note:
- Write only the requested artifact path under `artifacts/consensus/**`.
