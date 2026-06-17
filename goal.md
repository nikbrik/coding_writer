# Goal: Deterministic Stateful Agent Loop (Process Controller)

## Context

Implement the `ProcessController` layer so the assistant becomes process-deterministic: for the same persisted task state, selected action and validator configuration, the application makes the same allow/block/transition decisions regardless of model wording.

Source of truth:
- `docs/deterministic-stateful-agent-loop-epic.md`
- `.kilo/plans/deterministic-stateful-agent-loop-epic.md`

The recent commit only updated documentation; no process-control code exists yet. The current chat loop in `internal/cli/root.go` builds a prompt and calls the provider immediately. We are adding a new `internal/process` package and wiring it into the existing `runtime` struct without splitting `root.go` (F006 is deferred).

## Acceptance Criteria

- [x] Phase 1 — Policy and Types
  - `internal/process` defines `ActionKind`, `StagePolicy`, `StagePolicyRegistry`, stage schemas and P0 permission constants.
  - Every canonical stage returns a non-nil policy; unknown stage fails closed with typed `validation` error.
  - `paused` is not represented as a stage policy.
- [x] Phase 2 — StagePromptFactory
  - Trusted base + process + stage + tool prompts are inserted before untrusted profile/task/memory/user blocks.
  - Profile/task/memory/user blocks keep `trust="untrusted"` and `EscapeUntrusted`.
  - Existing Day 12 profile-difference test still passes.
- [x] Phase 3 — ProcessController Hard Gates
  - `ProcessController.RunExchange` replaces the direct provider call in `runChatExchange`.
  - Paused task normal chat returns `task_paused` error without calling provider.
  - Done task mutation request is blocked before provider call.
  - Forbidden action for current stage is blocked before provider call.
- [x] Phase 4 — Structured Output Parser
  - JSON-first parsing for controlled actions; text fallback only for `answer_question`.
  - Parsed `stage` must match current stage; parser never mutates task state.
- [x] Phase 5 — Response Validators
  - Stage-specific validators reject implementation in planning, fake test claims in execution, fixes/features in validation, mutation in done.
  - Validation `ready_for_done` is blocked by blocker/high findings or missing evidence.
- [x] Phase 6 — RetryController
  - Fixable schema/stage violations retry up to 2 times.
  - Hard gate/security violations do not retry.
  - Failed response is not saved as accepted assistant short memory.
- [x] Phase 7 — TransitionGate
  - Only `TransitionGate` applies chat-driven stage transitions through `tasks.Manager.Move`.
  - LLM cannot directly move stage; forbidden transition preserves task state.
- [x] Phase 8 — Audit Log
  - `ProcessAuditStore` appends events to `<storage_root>/process_audit.jsonl`.
  - Every provider call, rejected output and transition is recorded.
  - CLI `process audit [--latest]` (or `/process audit`) inspects events without provider call.
- [x] Phase 9 — Integration Tests
  - `tests/process_acceptance_test.go` covers planning/execution/validation/done, pause gate, wrong stage, retry, transition gate.
  - Day 11/12/13 acceptance tests keep passing.
- [x] Final verification
  - `go build ./...` succeeds.
  - `go test ./...` passes.
  - `go test ./tests/...` passes.

## Constraints

- Do not change Day 11/12/13 acceptance contracts.
- `paused` stays `TaskStatus`; `done` stays `stage=done + expected_action=none`.
- LLM does not mutate task state or memory directly.
- P0 side effects remain forbidden: no LLM-owned file edits, shell execution or git commits.
- Follow existing error conventions (`*app.Error`, `CategoryValidation`, etc.), file locks, atomic JSON writes and `EscapeUntrusted`.
- Wire into existing `runtime` struct in `root.go` without major refactor.

## Blast Radius

- New package `internal/process` and its tests.
- `internal/prompting/builder.go` and `builder_test.go`.
- `internal/providers/fake.go` (multi-response chat support).
- `internal/cli/root.go` (runtime struct, `runChatExchange`, new `process` command).
- New test file `tests/process_acceptance_test.go`.

## Execution Log

### Iteration 0 — Goal and discovery
- Read plan, epic, `root.go`, `builder.go`, `manager.go`, `fake.go`, `models.go`, tests.
- Updated `goal.md` with checklist and constraints.
- Next: Phase 1 Policy and Types.

### Iteration 1 — Process controller implementation
- Added `internal/process` with action kinds, stage policies, schemas, trusted stage prompts, parser, validators, retry controller, transition gate and audit store.
- Refactored `prompting.Builder` to insert trusted process/stage/tool prompts before untrusted profile/task/memory/user blocks.
- Extended `providers.FakeProvider` with sequential `ChatResponses` for retry tests.
- Replaced direct chat provider flow in `root.go` with `ProcessController.RunExchange` and added `assistant process audit [--latest]` plus `/process audit`.
- Added process acceptance tests for planning rejection, validation role, paused hard gate, retry block and validation-to-done transition.
- Verified: `go build ./...` OK, `go test ./...` OK, `go test ./tests/...` OK.
- Smoke: `ASSISTANT_FAKE_PROVIDER=1 go run ./cmd/assistant --storage-dir <tmp> --model fake/model chat --once --input "спланируй MVP"` OK.
- Smoke: `ASSISTANT_FAKE_PROVIDER=1 go run ./cmd/assistant --storage-dir <tmp> --model fake/model chat --once --input "реализуй шаг 1"` OK.

### Iteration 2 — Review fixes
- Fixed `answer_question` validation so text fallback runs only common validators, not stage schema validators.
- Added process preflight hard gates before CLI model/provider validation, preserving `task_paused`/`task_done`/`forbidden_action` ordering.
- Added `MemoryModel` to `ProcessController` and restored classifier use of configured memory model.
- Strengthened planning validation to reject code patches/implementation claims outside `summary`.
- Added regression tests for all four review findings.
- Verified: `go test ./internal/process/...` OK, `go test ./internal/cli/...` OK, `go build ./...` OK, `go test ./...` OK, `go test ./tests/...` OK.
- Smoke: active task normal chat with fake provider now succeeds.
- Smoke: paused task with invalid model returns `task_paused` before model validation.

### Iteration 3 — Second review fixes
- Fixed execution-stage question routing so ordinary questions resolve to `answer_question`, not `execute_plan_step`.
- Made stage prompts action-aware: `answer_question` no longer requests stage JSON schema.
- Moved `accepted` audit after transition gate; transition precondition errors now audit as `rejected` and do not write accepted events.
- Added regression tests for all three findings.
- Verified: `go test ./internal/process/...` OK, `go test ./internal/prompting/...` OK, `go test ./internal/cli/...` OK, `go build ./...` OK, `go test ./...` OK, `go test ./tests/...` OK.
