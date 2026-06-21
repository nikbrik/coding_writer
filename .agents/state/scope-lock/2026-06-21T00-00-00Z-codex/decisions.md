# Decisions

Task: implement full TUI P0 from `TUI-IMPLEMENTATION-PLAN.md` and adapt Day 15 manual test to run through TUI.
Task ID: `tui-implementation-plan-scope-lock`

## Decision Log

### Decision entry

Timestamp: `2026-06-20T21:06:52Z`
Action title: `Select full TUI P0 scope`

Classification: `MUST`

What was done:

- Resolved `AMB-CRIT-001` to full TUI P0, not the smaller first PR shape.
- Added explicit acceptance goal: by the end of the task, Day 15 manual test must be adapted and run through TUI without errors by Codex.

Why:

- Direct user answer selected full P0 and stated final acceptance target.

Alternatives considered:

- First PR shape with explicit `--tui` and default plain REPL was rejected by user scope.

Evidence:

- Direct user message: `full P0, цель - в конце задачи day15 ручной тест должен прогоняться через tui без ошибок тобою. сам тест тоже придётся адаптировать`

Trusted approval provenance:

- Source type: `direct-user-message`
- Source id or exact user-message reference: `full P0, цель - в конце задачи day15 ручной тест должен прогоняться через tui без ошибок тобою. сам тест тоже придётся адаптировать`
- Approver: `user`
- Timestamp: `2026-06-20T21:06:52Z`
- Exact approved action: `Use full TUI P0 as scope and include Day 15 TUI manual test adaptation and successful run as done criteria.`
- Approved scope: `TUI P0 plus Day 15 TUI manual test adaptation.`
- Session limit or expiration: `This scope-lock session only.`

### Decision entry

Timestamp: `2026-06-20T21:06:52Z`
Action title: `Default TUI in interactive chat`

Classification: `MUST`

What was done:

- Resolved `AMB-CRIT-003`: interactive `assistant chat` must start TUI by default by the end of this run.

Why:

- Full P0 in `TUI-IMPLEMENTATION-PLAN.md` section 16 includes `interactive assistant chat opens TUI by default`.

Alternatives considered:

- Leave default plain and only add `--tui`; rejected because it is first PR shape, not full P0.

Evidence:

- `TUI-IMPLEMENTATION-PLAN.md` section 16.
- Direct user selected full P0.

Trusted approval provenance:

- Source type: `direct-user-message`
- Source id or exact user-message reference: `full P0, цель - в конце задачи day15 ручной тест должен прогоняться через tui без ошибок тобою. сам тест тоже придётся адаптировать`
- Approver: `user`
- Timestamp: `2026-06-20T21:06:52Z`
- Exact approved action: `Make interactive chat use TUI by default, with plain fallback retained.`
- Approved scope: `assistant chat interactive behavior only; --once and --json unchanged.`
- Session limit or expiration: `This scope-lock session only.`

### Decision entry

Timestamp: `2026-06-20T21:06:52Z`
Action title: `Correct public entrypoint to cw/codingwriter`

Classification: `MUST`

What was done:

- Invalidated the earlier `assistant chat` wording in the contract.
- Re-scoped the public happy path to `cw`/`codingwriter` as the product entrypoint.
- Kept legacy `assistant` compatibility only where explicitly allowed by the revised contract.

Why:

- Direct user correction: the agreed entrypoint is `codingwriter` or `cw`, not `assistant chat`.
- `docs/tui-frd.md` already names `cw`/`codingwriter` as the canonical TUI entrypoint.

Alternatives considered:

- Keep `assistant chat` as primary TUI command; rejected by direct user correction.

Evidence:

- Direct user message: `стоп блять, мы договаривались не assistant chat, а codingwriter или cw как точка входа`
- `docs/tui-frd.md` section `TUI-FR-016. Единая команда cw / codingwriter`.

Trusted approval provenance:

- Source type: `direct-user-message`
- Source id or exact user-message reference: `стоп блять, мы договаривались не assistant chat, а codingwriter или cw как точка входа`
- Approver: `user`
- Timestamp: `2026-06-20T21:06:52Z`
- Exact approved action: `Use cw/codingwriter as canonical public entrypoint instead of assistant chat.`
- Approved scope: `Public CLI/TUI entrypoint, scripts, docs, tests, and build artifacts needed for TUI P0.`
- Session limit or expiration: `This scope-lock session only.`

### Decision entry

Timestamp: `2026-06-20T21:06:52Z`
Action title: `Keep existing app lifecycle logic`

Classification: `MUST`

What was done:

- Clarified that TUI must not duplicate authoritative lifecycle logic in UI components.
- Clarified that existing application lifecycle/control-plane logic must remain in the app/backend.
- If new lifecycle behavior is needed for TUI P0, it must be added to backend/control-plane next to existing logic and exposed through an adapter, not implemented as a replacement inside TUI.

Why:

- Direct user clarification: old application logic must remain.
- Existing plan/FRD require TUI to display state and collect input/approvals while authoritative mutations stay in runtime managers and process gates.

Alternatives considered:

- Treat TUI as a separate lifecycle engine; rejected because it would create duplicate sources of truth.
- Rewrite old lifecycle logic into TUI; rejected by direct user clarification.

Evidence:

- Direct user message: `да, но старая логика в приложении должна остаться`

Trusted approval provenance:

- Source type: `direct-user-message`
- Source id or exact user-message reference: `да, но старая логика в приложении должна остаться`
- Approver: `user`
- Timestamp: `2026-06-20T21:06:52Z`
- Exact approved action: `Preserve existing app lifecycle/control-plane logic; TUI may call it through adapter and backend may be extended only without replacing old logic.`
- Approved scope: `Lifecycle/control-plane ownership and TUI adapter boundary for this TUI P0 session.`
- Session limit or expiration: `This scope-lock session only.`

### Decision entry

Timestamp: `2026-06-20T21:06:52Z`
Action title: `Claude Code/Codex class TUI UX`

Classification: `MUST`

What was done:

- Added a product UX invariant: the TUI should feel like a terminal coding agent in the class of Claude Code / Codex, not a debug chat REPL with panels.

Why:

- Direct user clarification asked whether the UX goal is Claude Code/Codex-like and then asked to add it.

Alternatives considered:

- Minimal chat UI with status panels; rejected because it underspecifies the expected product class.

Evidence:

- Direct user message: `Правильно ли я понял, что мы делаем UI/UX похожим на claude code/ codex? Тебе это понятно?`
- Direct user message: `добавь`

Trusted approval provenance:

- Source type: `direct-user-message`
- Source id or exact user-message reference: `добавь`
- Approver: `user`
- Timestamp: `2026-06-20T21:06:52Z`
- Exact approved action: `Add Claude Code/Codex class terminal coding agent UX invariant to the TUI P0 contract.`
- Approved scope: `UX target and acceptance interpretation for this TUI P0 session.`
- Session limit or expiration: `This scope-lock session only.`

### Decision entry

Timestamp: `2026-06-20T21:18:08Z`
Action title: `Go directive follows Charm dependency resolution`

Classification: `ALLOWED`

What was done:

- `go mod tidy` raised the module `go` directive from `1.22` to `1.24.2` after adding the confirmed Charm TUI dependencies.
- A manual attempt to keep `go 1.22` made `go test` fail with `updates to go.mod needed; to update it: go mod tidy`, so the tidy result was kept.

Why:

- Confirmed contract allowed adding Bubble Tea, Lip Gloss and Bubbles dependencies. The Go directive update is a dependency-resolution consequence, not an unrelated toolchain refactor.

Alternatives considered:

- Pin older dependency versions and keep `go 1.22`; attempted earlier with older Charm versions, but compatible Bubbles/Bubble Tea constraints still required a newer resolved module graph.

Evidence:

- Command: `go test ./internal/tui ./internal/cli`
- Error after forcing `go 1.22`: `go: updates to go.mod needed; to update it: go mod tidy`

Trusted approval provenance:

- Source type: `trusted-tool-result`
- Source id or exact user-message reference: `go test ./internal/tui ./internal/cli`
- Approver: `not-applicable`
- Timestamp: `2026-06-20T21:18:08Z`
- Exact approved action: `Keep go.mod tidy result required by confirmed TUI dependencies.`
- Approved scope: `go.mod/go.sum dependency metadata for this TUI P0 session.`
- Session limit or expiration: `This scope-lock session only.`
