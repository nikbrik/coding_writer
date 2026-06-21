# Scope Definition

Task: implement full TUI P0 with `cw`/`codingwriter` as the canonical entrypoint and adapt Day 15 manual test to run through TUI.
Task ID: `tui-implementation-plan-scope-lock`
Status: `confirmed`
Confirmed By: direct user message `confirmed`.
Confirmed At: `2026-06-20T21:18:08Z`.

## MUST

- `[MUST-001]` Add a TUI backend boundary that lets `internal/cli` expose existing runtime/chat/task/audit/memory/evidence behavior to TUI without moving lifecycle logic into UI components.
- `[MUST-002]` Add Bubble Tea based TUI package with input, timeline, status, plan/criteria, planning swarm, approvals, evidence, memory proposal, applied files/diff placeholder, warnings/errors, and resume surface needed for P0.
- `[MUST-003]` Add `cw`/`codingwriter` as the canonical public entrypoint. Interactive `cw` must launch TUI by default when stdin/stdout are interactive; add `cw --tui` and `cw --plain`.
- `[MUST-004]` Preserve old plain REPL behavior under `cw --plain`; support one-shot mode as `cw --once`, `cw --json --once`, and `cw --render-prompt --once` without TUI.
- `[MUST-005]` TUI must call the existing backend/control plane for exchanges, planning approval/rejection, memory proposal apply/reject, task pause/resume, audit, and trusted evidence. TUI must not duplicate authoritative lifecycle logic in UI components. If TUI P0 needs new lifecycle behavior, add it to backend/control-plane next to the existing app logic and expose it through an adapter.
- `[MUST-012]` Existing application lifecycle/control-plane behavior must remain. Do not replace or remove the old `ProcessController`/task/gate flow while adding TUI.
- `[MUST-006]` TUI must show Day 15 lifecycle visibly: planning, planning swarm, pending plan approval, execution, applied artifacts, trusted evidence, validation/reviewer, transition to done, and final `expected_action=none`.
- `[MUST-007]` TUI must avoid raw stage JSON as the main UX. Stage answers and audit events must render as readable summaries/panels.
- `[MUST-008]` Adapt the Day 15 manual/demo script(s) and docs needed so the scripted/manual acceptance path runs through `cw`/`codingwriter` TUI, not plain REPL or `assistant chat`.
- `[MUST-009]` Codex must run the adapted Day 15 TUI manual test by the end and it must pass without errors, or report a concrete blocker if live external requirements make it impossible.
- `[MUST-010]` Add focused automated tests for TUI model/components/backend adapter/CLI routing sufficient to protect P0 behavior.
- `[MUST-011]` Run repo-relevant verification after implementation, including Go tests and the adapted Day 15 TUI manual test.
- `[MUST-013]` UX must target a terminal coding agent in the class of Claude Code / Codex: top-level TUI, task-first workflow, natural language input, visible plan/progress/evidence/files, inline approvals, and no raw debug-console feel in the happy path.

## ALLOWED

- `[ALLOW-001]` Add required Charm dependencies: `github.com/charmbracelet/bubbletea`, `github.com/charmbracelet/lipgloss`, and `github.com/charmbracelet/bubbles`, plus transitive `go.sum` updates.
- `[ALLOW-002]` Add small test-only helper dependencies only if necessary for PTY/TUI tests and justified by the implementation.
- `[ALLOW-003]` Extract shared rendering helpers from `internal/cli/root.go` into a new internal package only if needed to prevent duplicated raw JSON rendering and keep CLI/TUI output consistent.
- `[ALLOW-004]` Add a small PTY/expect-style helper script or Go test helper for the Day 15 TUI manual test.
- `[ALLOW-005]` Update help text and docs directly related to TUI and Day 15 manual testing.
- `[ALLOW-006]` Update existing tests that assert old interactive default, but only to reflect the new TUI default while preserving plain fallback.
- `[ALLOW-007]` Use fake provider for deterministic automated smoke tests; use live OpenRouter only where the existing manual scenario requires live proof and credentials are available.
- `[ALLOW-008]` Keep legacy `assistant` binary/command compatibility only as a non-primary compatibility path, if doing so reduces migration risk.

## DEFER

- `[DEFER-001]` Full PatchSet/diff preview and apply workflow. P0 shows applied files and an honest diff placeholder if backend PatchSet does not exist.
- `[DEFER-002]` General-purpose repo search/read tools in TUI. P0 uses existing backend surfaces only.
- `[DEFER-003]` Persistent TUI session ledger beyond current task/audit based resume.
- `[DEFER-004]` Clipboard integration and advanced editing of memory proposal records.

## FORBIDDEN

- `[FORBID-001]` Do not duplicate or move lifecycle ownership from `ProcessController`, `LifecycleGate`, `TransitionGate`, task manager, proposal store, or trusted evidence store into TUI components.
- `[FORBID-002]` Do not add keyword/regex/substring semantic routing for planning approval, readiness, validation, done, verification command selection, invariant decisions, or user intent.
- `[FORBID-003]` Do not leave `assistant chat` as the documented primary happy path for TUI P0.
- `[FORBID-004]` Do not silently write memory without user-visible approval/action.
- `[FORBID-005]` Do not add arbitrary shell/tool execution inside TUI components.
- `[FORBID-006]` Do not fake PatchSet/diff preview behavior that backend does not provide.
- `[FORBID-007]` Do not change lecture note files unless user explicitly asks.
- `[FORBID-008]` Do not use fake provider as final evidence for a live-only manual scenario unless live credentials/model are unavailable; if so, report the blocker.
- `[FORBID-009]` Do not implement only a `chat` subcommand TUI and call it done; the accepted product entrypoint is top-level `cw`/`codingwriter`.
- `[FORBID-010]` Do not delete, bypass, or rewrite away the existing app lifecycle/control-plane logic as part of TUI implementation.

## IRREVERSIBLE

- `[IRREV-001]` None currently classified as irreversible destructive action.
- `[IRREV-002]` External package download/network access for Go modules requires command escalation and is approved only after this contract is confirmed.
- `[IRREV-003]` Destructive cleanup in scripts may only remove explicitly safe Day 15 storage/scratch paths already guarded by path checks.

## ALLOWED_PATHS

- `go.mod` - add TUI dependencies.
- `go.sum` - dependency checksums.
- `internal/cli/**` - CLI flags, routing, backend adapter, tests, renderer extraction if needed.
- `cmd/**` - add/rename command entrypoints for `cw`/`codingwriter` while preserving required compatibility.
- `internal/tui/**` - new TUI implementation and tests.
- `internal/render/**` - optional shared renderer package.
- `internal/process/**` - only small exported/read-only helpers for existing audit/evidence data if needed; no lifecycle rewrite.
- `internal/memory/**` - only small adapter-support helpers if needed; no storage contract rewrite.
- `internal/app/**` - only DTO/view helper additions if needed.
- `scripts/day15-demo.sh` - adapt scripted Day 15 demo to TUI.
- `scripts/manual-day15-user-flow.sh` - adapt deterministic Day 15 smoke to TUI.
- `scripts/day15-tui-driver.py` - PTY helper for scripted Day 15 TUI smoke.
- `.codingwriter/**` - local build artifact/storage path for the new product command, if scripts need it.
- `docs/tui-frd.md` - update if implementation/acceptance details require alignment.
- `docs/manual-testing-demo.md` - update Day 15 TUI flow.
- `docs/manual-testing-real-cli.md` - update Day 15 preflight/smoke command if script changes.
- `README.md` - update primary happy path entrypoint wording.
- `docs/prd.md` - update primary TUI entrypoint wording.
- `docs/frd.md` - update primary command surface wording.
- `tests/**` - update or add acceptance coverage tied to TUI/manual flow.
- `manual_scratch/day15_contains_duplicate/**` - generated/cleaned demo target as part of Day 15 script run.
- `.agents/state/scope-lock/2026-06-21T00-00-00Z-codex/**` - protocol-owned state.

## FORBIDDEN_PATHS

- `day11.md` - protected lecture note.
- `day12.md` - protected lecture note.
- `03-memory-state-notes.md` - protected lecture note.
- `.kilo/**` - runtime adapter area; no TUI implementation should require changes here.
- Global Codex/Brew/MCP/Claude/Cursor/shell config paths - out of repo scope.

## DONE_CRITERIA

- `[DONE-001]` `cw` in an interactive terminal starts TUI by default; `cw --plain` starts old REPL fallback.
- `[DONE-002]` `cw --json --once` works without TUI and preserves the existing JSON one-shot semantics.
- `[DONE-003]` Non-interactive `cw --tui` returns typed CLI error with hint to use `--plain`.
- `[DONE-004]` TUI lets user send normal messages and receives backend responses through existing `runChatExchange`/`ProcessController` path.
- `[DONE-005]` TUI displays task status, stage, expected action, current step, plan/criteria, pending approvals, audit-derived events, warnings/errors, memory proposal, evidence, applied files, and resume state.
- `[DONE-006]` Planning approval/rejection and memory proposal accept/reject actions go through backend managers/control plane and are visible in TUI.
- `[DONE-007]` TUI Day 15 flow can complete planning -> execution -> validation -> done without raw stage JSON as primary UX.
- `[DONE-008]` Day 15 manual/demo script(s) are adapted to drive `cw`/`codingwriter` TUI through a terminal/PTY rather than pipe-only plain REPL.
- `[DONE-009]` Codex runs the adapted Day 15 TUI manual test and captures passing evidence: final task state `done`, `expected_action=none`, `validation_status=ready_for_done`, audit roles/decisions present, transcript or captured screen has TUI evidence.
- `[DONE-010]` Automated tests for TUI/CLI routing pass.
- `[DONE-011]` Repo verification command(s) pass, or any failure is clearly unrelated and reported with evidence.
- `[DONE-012]` Final validation confirms changed paths stay within `ALLOWED_PATHS`, no `FORBIDDEN` behavior was implemented, and no forbidden path was touched.
- `[DONE-013]` Final self-review checks that the delivered TUI reads as a coding-agent workspace, not merely a chat REPL with status panels: `cw` opens directly into the task UI, Day 15 can be driven by natural language, and plan/progress/evidence/files/approvals are visible without raw JSON.

## Confirmation Record

Confirmed by direct user message `confirmed` at `2026-06-20T21:18:08Z`.
