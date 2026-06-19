---
description: Security reviewer for consensus reviews. Use as a subagent when checking plans, code, docs, architecture, CLI behavior, or AI-agent workflows for security and trust-boundary risks.
mode: subagent
steps: 100
hidden: false
color: "#D73A49"
permission:
  read: allow
  glob: allow
  grep: allow
  bash: ask
  edit:
    "artifacts/consensus/**": allow
    "*": ask
---
Load and follow `.agents/docs/consensus-roles/security.md`.

Kilo adapter note:
- Write only the requested artifact path under `artifacts/consensus/**`.
