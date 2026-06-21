# Ambiguities

Task: implement full TUI P0 from `TUI-IMPLEMENTATION-PLAN.md` and adapt Day 15 manual test to run through TUI.
Task ID: `tui-implementation-plan-scope-lock`
Session ID: `2026-06-21T00-00-00Z-codex`
Status: `resolved`

## Critical Ambiguities

### `AMB-CRIT-001`

Category: `success-criteria`
Question:
Should this run implement the whole TUI P0 done definition from section 16, or only the smaller first implementation PR shape from section 15?
Why it matters:
The whole P0 includes default TUI, plan approvals, memory proposals, trusted evidence, applied artifacts, resume, and unit tests. The first PR shape is narrower: dependencies, backend adapter, minimal input/timeline/status TUI, explicit `--tui`/`--plain`, and default still plain.
Resolved by:
Direct user message.
Resolution:
Full TUI P0, plus Day 15 manual test adapted to TUI and run by Codex without errors.
Decision reference:
`decisions.md` / `Select full TUI P0 scope`.

### `AMB-CRIT-002`

Category: `dependency`
Question:
If implementation proceeds, is adding Charm dependencies (`bubbletea`, `lipgloss`, `bubbles`) approved in this run?
Why it matters:
`go.mod` currently has only Cobra as a direct runtime dependency. TUI implementation requires new external modules and network/package updates, which are a build-surface change.
Resolved by:
Scope contract confirmation required.
Resolution:
Contract includes Charm dependencies and package downloads as required for full P0. User confirmation of the contract will approve this build-surface change for this session.
Decision reference:
Pending contract confirmation.

### `AMB-CRIT-003`

Category: `public-interface`
Question:
Should interactive `assistant chat` become TUI by default in this run, or should this run only add explicit `--tui` while leaving default `runREPL()` plain?
Why it matters:
Changing default interactive behavior is the primary user-facing CLI behavior change and affects tests, docs/help text, fallback behavior, and compatibility.
Resolved by:
Direct user selected full P0.
Resolution:
Interactive `assistant chat` becomes TUI by default. `assistant chat --plain`, `assistant chat --once`, and `assistant chat --once --json` must remain supported.
Decision reference:
`decisions.md` / `Default TUI in interactive chat`.

## Deferable Ambiguities

### `AMB-DEF-001`

Category: `architecture`
Uncertainty:
Whether to extract `internal/render/**` in this implementation.
Conservative choice:
Allowed only if needed to avoid duplicated raw JSON rendering logic between CLI and TUI.
Alternative:
Keep rendering local to TUI and CLI separately.
Defer-log reference:
Pending execution.

### `AMB-DEF-002`

Category: `behavior`
Uncertainty:
Whether full diff preview is required.
Conservative choice:
P0 requires applied files and a diff placeholder only; do not fake a PatchSet/diff workflow before backend exists.
Alternative:
Build full PatchSet preview and apply workflow.
Defer-log reference:
Pending execution.

## Non-Blocking Ambiguities

### `AMB-NB-001`

Category: `testing`
Note:
Terminal escape sequence rendering should not be treated as product behavior.
Reason it does not affect implementation:
Plan already says to test update/render state and stable text snapshots, with styles disabled where needed.

## Interview Queue

- No unresolved critical ambiguity remains before contract confirmation.
