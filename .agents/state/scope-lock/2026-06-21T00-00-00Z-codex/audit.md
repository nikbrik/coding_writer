# Audit

Task: implement or scope-lock TUI work from `TUI-IMPLEMENTATION-PLAN.md`.
Task ID: `tui-implementation-plan-scope-lock`
Session ID: `2026-06-21T00-00-00Z-codex`
Status: `complete`

## Files Read

- `AGENTS.md` - repo entrypoint, requires `.agents/rules/always.md`, repo-local policy, caveman ultra style, harness evolution.
- `.agents/rules/always.md` - canonical shared rules, protected lecture notes, adapter boundary.
- `.agents/rules/search.md` - ast-index first, rebuild once if missing.
- `.agents/rules/goal-loop.md` - explicit goal/protocol rules are source of truth.
- `.agents/rules/harness-evolution.md` - read learnings before substantial tasks, run evolve after meaningful tasks.
- `.agents/rules/validation.md` - no keyword/regex semantic validation for product decisions.
- `.agents/learnings/LEARNINGS.md` - product and validation constraints relevant to TUI/control-plane work.
- `.agents/learnings/ERRORS.md` - ast-index cache outside workspace and Go build cache sandbox notes.
- `.agents/skills/scope-lock/PREFLIGHT.md` - stage order, audit/ambiguity/contract gates.
- `.agents/skills/scope-lock/CHECKLIST.md` - required preflight questions.
- `.agents/skills/scope-lock/AGENTS.md` - role model and persisted outputs.
- `.agents/skills/scope-lock/GUARD.md` - deny-by-default, irreversible approval, defer behavior.
- `.agents/skills/scope-lock/HANDOFFS.md` - state handoff rules.
- `.agents/skills/scope-lock/VALIDATION.md` - final validation requirements.
- `.agents/skills/scope-lock/templates/*` - state templates copied into this session dir.
- `TUI-IMPLEMENTATION-PLAN.md` - requested TUI implementation plan and proposed phases.
- `go.mod` - current dependencies; only Cobra is direct runtime dependency.
- `internal/cli/root.go` - chat command, runtime composition, `runChatExchange`, `runREPL`, renderer, trusted verification.
- `internal/cli/root_test.go` - existing CLI, renderer, verification, and concurrency tests.
- `internal/app/models.go` - `TaskState`, planning proposal, microtask state, app config models.
- `internal/process/controller.go` - `ProcessController.RunExchange` deterministic process loop.
- `internal/process/audit_store.go` - persisted process audit timeline.
- `internal/memory/proposal_store.go` - memory proposal list/latest/apply APIs.

## Project Rules

- Repo-local `.agents` policy is canonical; runtime-specific adapters must stay thin.
- Use `ast-index` first for code search. Sandbox could not read cache; escalated `ast-index rebuild` succeeded.
- Do not change lecture note files unless explicitly asked.
- Do not add keyword/regex semantic routing for user intent, lifecycle decisions, acceptance, readiness, or verification selection.
- TUI must not own authoritative lifecycle state if existing `ProcessController`/task managers already own it.
- Significant task completion requires local harness evolution entrypoint after work.

## Existing Patterns

- `cmd/assistant/main.go` delegates to `internal/cli.Execute()`.
- `internal/cli` is the composition root for Cobra commands and runtime wiring.
- `runtime` aggregates config, profiles, tasks, memory, invariants, proposals, provider, prompt builder, `ProcessController`, policy/gate/audit objects.
- `runChatExchange()` serializes mutating turns with `withChatTurnLock()`.
- `runChatExchangeLocked()` preflights process state, validates provider/model, resolves trusted verification, calls `ProcessController.RunExchange()`, materializes artifacts, reloads task, and loads audit events.
- `runREPL()` is the current interactive fallback; it reads lines, handles slash commands, calls `runChatExchange()`, and prints `renderChatResult()`.
- Human rendering already avoids raw stage JSON through private renderer helpers in `internal/cli/root.go`.
- Tests use Go `testing`, temp storage dirs, fake providers, and direct package-private helpers from `internal/cli`.

## Similar Components

- `chatResult` already contains answer, model, proposal, transition, applied artifacts, warnings, task, and audit events.
- `TaskState` already exposes stage, expected action, status, plan, acceptance criteria, open questions, pending planning, validation evidence, history log, pause/resume fields.
- `AuditStore.Latest(limit)` can provide timeline data.
- `ProposalStore.LatestPending()` and `ProposalStore.Apply()` can back memory proposal UI.
- `TrustedEvidenceStore.Validate()` can back evidence display.
- `renderChatResult()` and related helpers are a reuse target for shared render extraction.

## Likely Touched Areas

- `go.mod` and `go.sum` - adding Charm dependencies if TUI implementation proceeds.
- `internal/cli/root.go` - chat flags, interactive/non-interactive branch, adapter hooks, possible renderer extraction.
- `internal/cli/root_test.go` - CLI behavior tests for `--tui`, `--plain`, `--once`, `--json`.
- `internal/cli/tui_adapter.go` - likely new adapter to expose runtime/chat backend inside package `cli`.
- `internal/tui/**` - likely new TUI package, model, messages, layout, components, tests.
- Optional `internal/render/**` - if renderer extraction is included in the contract.

## Protected Or Risky Areas

- `ProcessController.RunExchange()` lifecycle and semantic routing must not be duplicated in TUI.
- Trusted verification command selection must remain structured/approved and cannot become keyword/path heuristics.
- JSON `--once --json` behavior must remain unchanged.
- Existing `runREPL()` must remain a supported fallback.
- Direct artifact materialization exists today; TUI should display `AppliedArtifacts` and not invent PatchSet preview.
- New dependencies are a public/build surface change and require explicit scope confirmation.
- TUI default behavior changes the primary UX of `assistant chat`; confirm whether this run should make default switch now or later.

## Facts Learned From Code

- `chatOptions` currently has `Once`, `Input`, `RenderPrompt`, and `Verify`; no `TUI` or `Plain`.
- `chatCommand()` currently routes `--once` through `runChatExchange()` and otherwise always calls `runREPL()`.
- `runREPL()` uses `isInteractiveReader(in)` only for progress/failure behavior, not for choosing TUI.
- `go.mod` has no Bubble Tea, Lip Gloss, or Bubbles dependencies.
- `chatResult.Task` and `chatResult.AuditEvents` are not JSON-exported but are available in-process for TUI adapter use.
- `renderChatResult()` is private to `internal/cli`; TUI cannot import it without extraction or adapter rendering.
- `ProcessController.RunExchange()` owns secret blocking, preflight state resolution, prompt improvement, invariants, auto-start, lifecycle transitions, audit, trusted evidence transitions, and proposal generation.
- `TaskState.PendingPlanning` provides enough data to derive a planning approval UI.
- `ProposalStore` supports latest pending proposal and apply semantics; work memory apply requires task-state blocking context.
- `AuditStore` stores process events in `<storage_root>/process_audit.jsonl` and returns newest-last events.

## Questions Already Answered By Code

- TUI can call existing control-plane through an adapter; no separate lifecycle engine is needed.
- `internal/tui` must not import `internal/cli`; the dependency direction should be `internal/cli` imports `internal/tui` and passes a backend.
- Existing CLI tests can be extended in-package for package-private helpers.
- Raw stage JSON is already addressed in current human renderer and should be reused/extracted rather than rewritten from scratch.
- Memory proposal and trusted evidence data already have stores; TUI should read via backend methods, not direct component shell/file access.

## Open Audit Gaps

- Whether this run should implement the entire P0 done definition or the smaller first PR shape.
- Whether adding Charm dependencies is approved in this run, given restricted network and build surface change.
- Whether `assistant chat` should become TUI default immediately or only add explicit `--tui` plus `--plain` first.
