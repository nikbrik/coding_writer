# Plan

Task: implement full TUI P0 with `cw`/`codingwriter` entrypoint and Day 15 TUI manual test.
Task ID: `tui-implementation-plan-scope-lock`
Session ID: `2026-06-21T00-00-00Z-codex`
Status: `active`

## Plan Items

### PLAN-001: dependencies and entrypoint

Linked contract items: `MUST-002`, `MUST-003`, `MUST-004`, `MUST-013`
Expected files: `go.mod`, `go.sum`, `cmd/cw/main.go`, `cmd/codingwriter/main.go`, `internal/cli/root.go`, `internal/cli/root_test.go`
Allowed path coverage: `go.mod`, `go.sum`, `cmd/**`, `internal/cli/**`
Verification: `go test ./internal/cli`

Work:
- Add Charm dependencies.
- Add `cw` and `codingwriter` command entrypoints.
- Add top-level `cw` flags: `--tui`, `--plain`, `--once`, `--input`, `--render-prompt`, `--verify`.
- Keep legacy `assistant chat` compatibility.

### PLAN-002: backend adapter

Linked contract items: `MUST-001`, `MUST-005`, `MUST-012`
Expected files: `internal/cli/tui_adapter.go`, `internal/tui/adapter.go`
Allowed path coverage: `internal/cli/**`, `internal/tui/**`
Verification: adapter/unit tests with fake provider/temp storage.

Work:
- Define TUI backend interface and DTOs.
- Wrap existing runtime, `runChatExchange`, task/audit/proposal/evidence APIs.
- Keep lifecycle changes in existing backend/control-plane.

### PLAN-003: TUI model and components

Linked contract items: `MUST-002`, `MUST-006`, `MUST-007`, `MUST-013`
Expected files: `internal/tui/**`
Allowed path coverage: `internal/tui/**`
Verification: TUI unit tests with fake backend.

Work:
- Implement Bubble Tea model, input, timeline, status, panels, keymap, layout.
- Render stage/audit/proposal/evidence as readable summaries.
- Support inline approvals and memory proposal actions through backend.
- Implement resume surface from current task/audit.

### PLAN-004: CLI routing and compatibility tests

Linked contract items: `MUST-003`, `MUST-004`, `MUST-010`
Expected files: `internal/cli/root.go`, `internal/cli/root_test.go`, possibly `internal/tui/*_test.go`
Allowed path coverage: `internal/cli/**`, `internal/tui/**`
Verification: `go test ./internal/cli ./internal/tui`

Work:
- Route interactive `cw` to TUI by default.
- Route `cw --plain` to plain REPL.
- Route one-shot JSON/text without TUI.
- Reject non-interactive `cw --tui` with typed error.

### PLAN-005: Day 15 TUI test adaptation

Linked contract items: `MUST-008`, `MUST-009`, `MUST-011`
Expected files: `scripts/day15-demo.sh`, `scripts/manual-day15-user-flow.sh`, `docs/manual-testing-demo.md`, `docs/manual-testing-real-cli.md`
Allowed path coverage: `scripts/day15-demo.sh`, `scripts/manual-day15-user-flow.sh`, `docs/manual-testing-demo.md`, `docs/manual-testing-real-cli.md`
Verification: run adapted Day 15 TUI manual smoke.

Work:
- Build/use `.codingwriter/bin/cw`.
- Drive TUI via PTY/expect-style helper.
- Assert final task state, audit roles/decisions, and captured TUI evidence.
- Update docs to name `cw`/TUI as canonical Day 15 path.

### PLAN-006: full verification and validation

Linked contract items: `MUST-011`, `DONE-001` through `DONE-013`
Expected files: validation state only unless fixes are needed.
Allowed path coverage: all allowed paths from contract.
Verification: `go test ./...`, adapted Day 15 TUI manual test, final diff/path review.

Work:
- Run focused tests, then broader tests.
- Run Day 15 TUI manual test.
- Inspect diff for forbidden paths and scope drift.
- Write `validation.md`.
