---
name: consensus-orchestrator
description: Multi-agent consensus review orchestrator for KiloCode. Use when the user asks for consensus, консенсус, multi-agent review, review by all agents, проверку всеми агентами, or wants a plan, documentation, code, architecture, CLI design, security posture, AI-first workflow, PR, or arbitrary target reviewed by specialized agents with artifacts and a final judge verdict.
---

# Consensus Orchestrator

Load and follow `.agents/docs/consensus-orchestrator.md` as the canonical consensus protocol.

Runtime adapter notes:

- Shared protocol path: `.agents/docs/consensus-orchestrator.md`
- Runtime reviewer adapters may exist under `.kilo/agent/*` and other agent-specific directories
- Keep artifacts under `artifacts/consensus/**`
- Default mode remains read-only review
