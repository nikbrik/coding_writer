---
name: evolve
description: Post-task harness evolution. Analyzes the current session, emits dry-run proposals with stable IDs, deduplicates learnings by pattern-key, and applies only explicitly approved updates to shared rules, skills, learnings, and runtime adapters.
---

# evolve

Load and follow `.agents/docs/evolve.md` as the canonical evolve protocol.

Runtime adapter notes:

- Shared skill path: `.agents/skills`
- Shared learnings path: `.agents/learnings`
- Runtime adapters may exist under `.kilo/*` and other agent-specific directories
- `AGENTS.md` remains guarded: show diff and require explicit confirmation before changing it
- After apply-mode writes, run the local harness validator when available
