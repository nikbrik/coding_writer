# Goal: Fix F001 Execution Progress Validation

## Context

Implement `.kilo/plans/fix-f001-execution-progress-validation.md`.

Consensus F001 high bug: untrusted model output can put side-effect/test/tool claims into execution progress fields (`current_step`, `next_step`, `completed_steps`) and persist them into durable Day 13 task state without trusted evidence.

## Acceptance criteria

- `validateExecution` rejects side-effect/test/tool claims in `current_step`, `next_step`, and `completed_steps` when no trusted evidence is supplied.
- Benign execution progress fields still persist through `ProcessController`.
- A controller-level regression test proves invalid progress claims return `validation_failed` and do not mutate task state.
- `go test ./internal/process` passes.
- `go test ./...` passes.
- `day11.md`, `day12.md`, `day13.md`, and `03-memory-state-notes.md` remain unchanged.

## Constraints

- Follow project rules in `AGENTS.md` and `.agents/rules/always.md`.
- Use ast-index first for code search.
- Keep changes minimal and scoped to the plan.
- Do not change SSOT files: `day11.md`, `day12.md`, `day13.md`, `03-memory-state-notes.md`.
- Do not add dependencies or new services.

## Blast radius

Allowed source files:

- `internal/process/execution_validator.go`
- `internal/process/validators_test.go`
- `internal/process/controller_test.go`

Allowed tracking file:

- `goal.md`

## Open questions

- None.

## Status log

- 2026-06-18T13:09:29+03:00: Goal created from user request and `.kilo/plans/fix-f001-execution-progress-validation.md`. Implementation pending.
- 2026-06-18T13:09:29+03:00: Added regression tests in `internal/process/validators_test.go` and `internal/process/controller_test.go`. Pre-fix `go test ./internal/process` failed on the new F001 cases, confirming the bug.
- 2026-06-18T13:09:29+03:00: Updated `internal/process/execution_validator.go` to include `current_step`, `next_step`, and `completed_steps` in execution claim validation.
- 2026-06-18T13:09:29+03:00: Ran `gofmt -w internal/process/execution_validator.go internal/process/validators_test.go internal/process/controller_test.go`.
- 2026-06-18T13:09:29+03:00: `go test ./internal/process` passed.
- 2026-06-18T13:09:29+03:00: `go test ./...` passed.
- 2026-06-18T13:09:29+03:00: `git diff --check` passed.
- 2026-06-18T13:09:29+03:00: Local diff review found no issues, then independent review found one high follow-up: terse progress claims like `ran go test ./...`, `go test ./... passed`, and `tool_result` were still not rejected.
- 2026-06-18T13:09:29+03:00: Fixed the follow-up by adding progress-specific tool/test claim detection in `internal/process/execution_validator.go` and regression tests in `internal/process/validators_test.go`.
- 2026-06-18T13:09:29+03:00: Re-ran `gofmt`, `go test ./internal/process`, and `go test ./...`; all passed.
- 2026-06-18T13:09:29+03:00: Latest independent re-review result: no findings.
