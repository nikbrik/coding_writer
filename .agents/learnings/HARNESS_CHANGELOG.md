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
<!-- HARNESS-CHANGELOG:END -->
