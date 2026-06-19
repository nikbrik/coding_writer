## Context

Исправить Day 15 manual/demo contract: основной пользовательский сценарий не должен быть набором `assistant chat --once --input ...` команд. Пользователь один раз запускает `assistant chat`, затем общается внутри REPL обычными сообщениями; приложение автономно ведет state, verification and lifecycle.

## Acceptance criteria

- Product auto verification intent in real/provider-backed mode is decided only by `SemanticValidator.ResolveIntent` strict JSON signal, not `strings.Contains`, `containsAny`, regex phrase lists, or trigger words.
- No keyword/substring fallback decides auto verification intent; no semantic validator means no automatic verification intent.
- Shared validation rule explicitly forbids free-text intent/readiness/approval/completion/acceptance product decisions via substring/regex helpers, even under non-validator names.
- `assistant chat` and `assistant chat --once --input <text>` output readable human transcript sections by default.
- TTY color path highlights sections/labels; non-TTY output contains no ANSI escapes.
- `--json` remains stable machine-readable mode for tests/scripts and does not become the primary Day 15 demo path.
- Docs describe final Day 15 contract: chat-first, app-owned state, semantic validation, readable UI, live Gemini proof, fake script only regression smoke.
- General demo doc shows the only Day 15 scenario: one `assistant chat` session first, then user messages inside the chat, not five separate `--once` invocations and not a duplicate focused Day15 file.
- Deterministic Day 15 regression script drives one `assistant chat` process with user messages on stdin and stores a human transcript; post-run machine assertions are separate.
- Manual test proves the one-session REPL flow reaches `stage=done`, `expected_action=none`, `validation_status=ready_for_done`, with planning specialists, orchestrator, executor and reviewer in audit.
- Relevant Go tests pass.
- Harness evolution records the new mandatory rule/learning.
- Latest self-review has no unresolved findings.

## Constraints

- Не менять `day11.md`, `day12.md`, `03-memory-state-notes.md`.
- Не подменять live manual test fake provider'ом или другой моделью.
- Не требовать от пользователя точные verification commands в chat.
- Не добавлять keyword/regex semantic product validators; при обнаружении заменить на semantic referee or objective hard gate.
- Сохранять machine JSON output для automation.
- Учитывать уже существующие dirty changes, не откатывать чужое.

## Blast radius

- `.agents/rules/validation.md`
- `.agents/learnings/LEARNINGS.md`
- `.agents/learnings/HARNESS_CHANGELOG.md`
- `docs/prd.md`
- `docs/frd.md`
- `docs/architect.md`
- `README.md`
- `internal/cli`
- `internal/process` only if output data needs small support changes
- `scripts/manual-day15-user-flow.sh` only if scenario needs human-output coverage
- `docs/manual-testing-demo.md`
- `DAY15_IMPLEMENTATION_PLAN.md`
- `docs/implementation-status-regression-plan.md`
- tests touching CLI output
- `goal.md`

## Open questions

- Нет.

## Log

- 2026-06-19: goal refreshed for UI/UX PRD + implementation + live Day 15 manual test.
- 2026-06-19: added PRD CLI chat UX/UI contract; implemented human transcript renderer for chat/task output while preserving `--json`.
- 2026-06-19: fixed Day 15 chat lifecycle gaps found by real manual testing: natural task start routing, prompt-improver fallback, package-specific auto verification, app-owned trusted-evidence validation review/done, and reviewer-agent audit.
- 2026-06-19: verified `GOCACHE=/private/tmp/coding_writer_gocache go test ./internal/process ./internal/cli ./tests`.
- 2026-06-19: verified `GOCACHE=/private/tmp/coding_writer_gocache go test ./...`.
- 2026-06-19: live manual Day 15 passed through OpenRouter model `google/gemini-3.1-flash-lite` with no fake provider, no `--json`, no `--verify`, no `/task move|step|expect`, and no user-supplied exact test command. Evidence: `/var/folders/br/48dxplrx6dvdkm481dc2ggb80000gn/T/coding_writer_day15_ui_live_RHV1Ac`; final state `done`, `validation_status=ready_for_done`, audit roles include planning specialists, orchestrator, executor, and reviewer.
- 2026-06-19: synced README, PRD, FRD, architecture, regression/status docs, manual demo docs and Day 15 plan with the final chat-first UX/UI and live Day 15 proof contract.
- 2026-06-19: goal refreshed for final review fixes: semantic auto verification, UI highlight verification, docs sync and evolve.
- 2026-06-19: replaced product auto verification keyword gate with semantic intent signal and removed CLI keyword fallback. Added UI color renderer test, fake negation test and persisted-evidence validation review test.
- 2026-06-19: verified `GOCACHE=/private/tmp/coding_writer_gocache go test ./internal/cli ./internal/process ./tests`, `GOCACHE=/private/tmp/coding_writer_gocache go test ./...`, `GOCACHE=/private/tmp/coding_writer_gocache bash scripts/manual-day15-user-flow.sh`, and real human CLI output without `--json`.
- 2026-06-19: final self-review for touched scope: no unresolved findings.
- 2026-06-19: goal refreshed for REPL-only Day 15 manual scenario; stale five-command `assistant chat --once --input ...` demo flow must be removed from primary Day 15 docs/script.
- 2026-06-19: fixed one-REPL Day 15 flow: deterministic script now drives a single `assistant chat` stdin session; app-owned post-approval trusted verification runs allowlisted `go test` from approved plan/criteria; human transcript no longer leaks raw stage JSON.
- 2026-06-19: live Gemini proof passed with OpenRouter model `google/gemini-3.1-flash-lite`, one `assistant chat` REPL process, no fake provider, no `--json`, no `--verify`, no `/task move|step|expect`, no user-supplied exact command. Evidence: `/var/folders/br/48dxplrx6dvdkm481dc2ggb80000gn/T/coding_writer_day15_repl_live_HZs4Ep`, final state `stage=done`, `expected_action=none`, `validation_status=ready_for_done`, events=102.
- 2026-06-19: verified `GOCACHE=/private/tmp/coding_writer_gocache go test ./internal/cli ./internal/process ./tests`, `GOCACHE=/private/tmp/coding_writer_gocache bash scripts/manual-day15-user-flow.sh`, `GOCACHE=/private/tmp/coding_writer_gocache go test ./...`, `git diff --check`, `git diff --cached --check`, `node scripts/validate-kilo-harness.mjs`, and `ast-index update`.
- 2026-06-19: evolve applied `day15-primary-demo-one-repl-app-owned-verification` learning and changelog entry; harness validation stayed green.
- 2026-06-19: removed duplicate Day 15 manual doc source; moved canonical scenario to `docs/manual-testing-demo.md` only and changed Day 15 demo/smoke task to LeetCode-style Contains Duplicate.
