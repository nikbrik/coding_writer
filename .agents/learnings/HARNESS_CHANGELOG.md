# Harness Changelog

> История изменений harness. Пополняется через `/evolve --apply`.
> Правило: только append-only записи между маркерами.

<!-- HARNESS-CHANGELOG:START -->
## 2026-06-19 | semantic intent routing rule
**Applied**: RULE-001, LEARNING-001
**Files**: .agents/rules/validation.md, .agents/learnings/LEARNINGS.md
**Reason**: User correction exposed `autoVerificationIntent` keyword routing; product free-text intent/readiness decisions must use structured semantic validation.

---
## 2026-06-19 | live scenario provider rule
**Applied**: RULE-001
**Files**: .agents/rules/validation.md
**Reason**: Prevent fake-provider or wrong-model substitutions when the user requests a real live/manual scenario.

---
## 2026-06-19 | semantic validation rule
**Applied**: RULE-001
**Files**: .agents/rules/always.md, .agents/rules/validation.md
**Reason**: Made semantic product validation policy explicit: no keyword/substring/simple-regex final validators for meaning-based decisions.

---
## 2026-06-19 | real CLI validation and manual suite
**Applied**: LEARNING-001, LEARNING-002, ERROR-001, ERROR-002
**Files**: .kilo/learnings/LEARNINGS.md, .kilo/learnings/ERRORS.md
**Reason**: Captured LLM-validation, live manual rerun, Go cache, and memory apply lessons from the Gemini manual test pass.

---
## 2026-06-19 | evolve dry-run apply all
**Applied**: ERROR-001, LEARNING-001, LEARNING-002, LEARNING-003
**Files**: .kilo/learnings/ERRORS.md, .kilo/learnings/LEARNINGS.md
**Reason**: Captured repeatable lessons from the harness evolution implementation and `/evolve` dry-run.

---
## 2026-06-19 | day15 one-repl manual proof
**Applied**: LEARNING-001
**Files**: .agents/learnings/LEARNINGS.md
**Reason**: Captured the Day 15 primary demo contract: one REPL session plus app-owned trusted verification through approved commands or VerificationResolver.

---
## 2026-06-19 | product north star
**Applied**: LEARNING-001
**Files**: .agents/learnings/LEARNINGS.md
**Reason**: Captured the product target as a Claude Code / Codex CLI-class coding agent, not a generic chat/debug utility.

---
## 2026-06-19 | verification resolver architecture
**Applied**: RULE-001, LEARNING-001
**Files**: .agents/rules/validation.md, .agents/learnings/LEARNINGS.md
**Reason**: Captured the rule that verification commands must come from exact approved commands or structured planners, not language/path heuristics.

---
## 2026-06-19 | day15 demo single source
**Applied**: LEARNING-001
**Files**: .agents/learnings/LEARNINGS.md
**Reason**: Captured that Day 15 demo must live only in the common demo doc and use a real LeetCode-style coding task.

---
## 2026-06-19 | day15 live verification variance
**Applied**: LEARNING-001
**Files**: .agents/learnings/LEARNINGS.md
**Reason**: Captured live-only Day 15 fixes for approved-plan command inference and reviewer variance under app-owned trusted evidence.

---
## 2026-06-20 | evolve root-cause bug gate
**Applied**: VALIDATOR-001, PRODUCT-BUG-001
**Files**: .agents/docs/evolve.md, scripts/validate-kilo-harness.mjs, BUGS/README.md, BUGS/app-session-task-overlapping-turns.md
**Reason**: Product defects must be tracked as bugs and harness evolution must preserve root-cause, bug, validator, and Harness Value Test guardrails.

---
## 2026-06-26 | MCP TUI manual proof
**Applied**: LEARNING-001, ERROR-001, LEARNING-002
**Files**: .agents/learnings/LEARNINGS.md, .agents/learnings/ERRORS.md
**Reason**: Captured MCP/TUI demo lessons: requested UI surface must be the proof surface, external tool slash commands need timeouts, and MCP results must expose user-visible business fields.

---
## 2026-06-26 | Day 18 scheduled MCP demo
**Applied**: LEARNING-001, ERROR-001, ERROR-002
**Files**: .agents/learnings/LEARNINGS.md, .agents/learnings/ERRORS.md
**Reason**: Captured Day 18 lessons: scheduled MCP demos need producer/consumer ownership split, Go cache sandbox rerun evidence, and same-package helper processes for stdio tests.

---
## 2026-06-26 | Day 18 LLM agent demo
**Applied**: LEARNING-001
**Files**: .agents/learnings/LEARNINGS.md
**Reason**: Captured that strict "agent 24/7" scheduled MCP demos require a real LLM loop over MCP aggregates, not only polling output.

---
## 2026-06-27 | Day 19 MCP composition cleanup
**Applied**: LEARNING-001, PROMOTE-001, LEARNING-002, LEARNING-003
**Files**: .agents/learnings/LEARNINGS.md, .agents/rules/harness-evolution.md
**Reason**: Captured MCP stdio demo ownership, multi-repo commit boundaries, and local review cache hygiene.

---
## 2026-06-27 | Day 19 TUI repair follow-up
**Applied**: VALIDATOR-001, ERROR-001, LEARNING-001
**Files**: scripts/validate-kilo-harness.mjs, .agents/learnings/ERRORS.md, .agents/learnings/LEARNINGS.md
**Reason**: Captured unsafe OpenRouter placeholder export docs, stale MCP test-helper config failure, and reinforced LLM-only semantic validation decisions.

---
## 2026-06-28 | versioning and cw build guardrails
**Applied**: VALIDATOR-001, ERROR-001
**Files**: scripts/validate-kilo-harness.mjs, .agents/learnings/ERRORS.md
**Reason**: Enforced versioning rule wiring and captured Go cache behavior for `scripts/build-cw.sh`.

---
<!-- HARNESS-CHANGELOG:END -->
