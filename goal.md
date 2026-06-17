# Goal: Deterministic Process Controller Review Fixes

## Context

Current task: finish the deterministic stateful agent loop feature and fix review findings that are valid against `docs/deterministic-stateful-agent-loop-epic.md`.

Chosen transition/persistence contract from docs:
- Parse/validate candidate output first.
- TransitionGate precheck blocks invalid transition proposals before accepted memory persistence.
- Accepted output is persisted to short memory before stage mutation.
- Transition apply re-checks current task identity/stage/status before mutating state.
- Transition apply failure is audited and returned before classifier proposal flow.

## Acceptance Criteria

- [x] Hard gates and provider calls are audited where applicable.
- [x] Secret input is blocked before provider/model validation.
- [x] Rejected outputs are not saved as accepted assistant memory.
- [x] Accepted short exchange saves user+assistant atomically.
- [x] TransitionGate validates preconditions before accepted memory persistence and re-checks current task before mutation.
- [x] Classifier proposal flow runs only after accepted output and transition handling.
- [x] Validators enforce required fields, enums, missing evidence, fake tool/test claims, and selected action constraints.
- [x] Done-stage mutation requests fail closed before provider call.
- [x] Memory proposal secret handling scans reason fields.
- [x] `go build ./...` passes.
- [x] `go test ./...` passes.
- [x] `go test ./tests/...` passes.
- [x] Latest self-review result: no unresolved findings under the selected docs-backed contract.

## Constraints

- Keep Day 11/12/13 contracts.
- Do not add heavy dependencies.
- Do not split `root.go`.
- Do not let LLM own task state transitions or memory writes.

## Blast Radius

- `internal/process/*`
- `internal/cli/root.go`, `root_test.go`
- `internal/memory/*`
- `internal/tasks/manager.go`
- `internal/prompting/builder_test.go`
- `tests/process_acceptance_test.go`

## Execution Log

### Review Fix Consolidation

Changed:
- Added stricter parser/validator/action-router/TransitionGate checks.
- Added audited preflight and provider-call audit paths.
- Added atomic short exchange save.
- Added task planning output persistence before planning->execution move.
- Added current task re-check before transition apply.
- Hardened memory proposal secret sanitization.

Verified:
- `go build ./...` OK.
- `go test ./...` OK.
- `go test ./tests/...` OK.

Self-review:
- No unresolved findings under docs-backed contract.
- Explicit non-issue: accepted output persistence before transition apply is intentional per docs target flow; transition apply failure is operational failure, audited as `transition_failed`, returned before classifier proposal.
