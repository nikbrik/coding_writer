---
description: Judge and arbiter for consensus reviews. Use as a subagent to aggregate expert artifacts, deduplicate findings, run consensus synthesis, and produce final verdicts.
mode: subagent
steps: 100
hidden: false
color: "#57606A"
permission:
  read: allow
  glob: allow
  grep: allow
  bash: ask
  edit:
    "artifacts/consensus/**": allow
    "*": ask
---
Load and follow `.agents/docs/consensus-roles/judge.md`.

Kilo adapter note:
- Write only the requested artifact path under `artifacts/consensus/**`.
