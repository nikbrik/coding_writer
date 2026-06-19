---
description: CLI utility architect reviewer for consensus reviews. Use as a subagent when checking command-line tools, developer UX, config behavior, scripts, automation plans, or CLI-related docs/code.
mode: subagent
steps: 100
hidden: false
color: "#2DA44E"
permission:
  read: allow
  glob: allow
  grep: allow
  bash: ask
  edit:
    "artifacts/consensus/**": allow
    "*": ask
---
Load and follow `.agents/docs/consensus-roles/cli-architect.md`.

Kilo adapter note:
- Write only the requested artifact path under `artifacts/consensus/**`.
