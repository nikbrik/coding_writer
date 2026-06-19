---
name: evolve
description: Compatibility adapter for shared evolve harness evolution. Loads .agents/docs/evolve.md and keeps Kilo-specific runtime audit notes for /evolve, dry-run proposals, approved applies, and validation.
---

# evolve - Kilo compatibility adapter

Active shared skill path is `.agents/skills/evolve/SKILL.md`; `.kilo/kilo.jsonc` should point `skills.paths` at `.agents/skills`.

Load and follow `.agents/docs/evolve.md` as the canonical evolve protocol.

Kilo-specific adapter notes:

- Shared skill path: `.agents/skills`
- Shared learnings path: `.agents/learnings`
- Legacy Kilo skill path: `.kilo/skill`
- Kilo audit surface includes `.kilo/kilo.jsonc`, `.kilo/agent/*`, `.kilo/command/*`, `.kilo/skills/*`, and legacy `.kilo/skill/*`
- `AGENTS.md` remains guarded: show diff and require explicit confirmation before changing it
- After apply-mode writes, run `node scripts/validate-kilo-harness.mjs`
