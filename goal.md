# Goal: Whole-Code Review Fixes

## Current Goal-X pass: Consensus Verdict Remediation

### Context

Current task: fix every issue from consensus final verdict `artifacts/consensus/20260618-084327-codebase-day11-day12-day13/12-final-verdict.md`.

Strict acceptance criteria remain `day11.md`, `day12.md`, `day13.md`: Day 11 memory layers/explicit save choice, Day 12 profile on every request, Day 13 FSM/current step/expected action/pause/resume must not be weakened.

### Acceptance criteria

- [x] F001 fixed: every baseline non-render chat path ends with memory proposal or explicit no-save decision; classifier/proposal failure cannot return accepted answer-only success.
- [x] F002 fixed: untrusted profile/task/memory/query context is no longer sent as system-role trusted instructions; adversarial test coverage exists.
- [x] F003 fixed: public free-text `--trusted-evidence` cannot satisfy `ready_for_done`; trusted evidence is app-generated/provenance-bound or removed from normal chat.
- [x] F004 fixed: `--profile` and `ASSISTANT_PROFILE` affect every request and memory flow without requiring persisted profile mutation.
- [x] F005 fixed: pending planning proposals persist with expected confirmation across restart; confirm/reject applies/clears deterministically.
- [x] F006 fixed: task transitions reject stale state via version/content digest/CAS.
- [x] F007 fixed: raw transcript/session history is separated from accepted short-term memory or clearly modeled as a separate sublayer with prompt/list/rejection semantics.
- [x] F008 fixed: Day 13 resume has durable task context for cross-session `chat --once` without repeated explanation.
- [x] F009 fixed: `task pause` at `done` no longer reports no-op success; behavior is real pause metadata/status or explicit terminal error.
- [x] F010 fixed: `current_step`/next/completed step progress is initialized and advanced through persisted transitions.
- [x] F011 fixed: profile-scoped long-memory listing is filtered by effective active profile by default, with explicit all-profile path if kept.
- [x] F012 fixed: REPL/top-level memory apply guidance and options are consistent.
- [x] F013 fixed: text `profiles show/list` exposes profile preferences needed to verify Day 12.
- [x] F014 fixed: benign/ambiguous input in `done` routes to read-only answer/summarize, not forbidden mutation.
- [x] F015 fixed: short-memory chat persistence uses safe append/batch append instead of full JSONL rewrite per turn.
- [x] F016 fixed: read-style commands and failing `init` avoid unintended storage/default writes where practical.
- [x] F017 fixed: platform support boundary is explicit/enforced or secure platform-specific locking/no-follow exists.
- [x] F018 fixed: `caveman-compress` has content secret/PII scanning/consent/local-dry-run safeguards or is explicitly excluded from supported surface.
- [x] F019 fixed: E2E CLI/ProcessController tests cover Day 11/12/13 adversarial and real workflow paths.
- [x] F020 fixed: provider disclosure/consent precedes every provider network/model-validation path.
- [x] F021 fixed: normal prompt audit is redacted/metadata-only; raw prompt audit requires explicit raw opt-in and purge guidance.
- [x] F022 fixed: `--quiet` has defined behavior and does not hide mandatory provider disclosure/consent.
- [x] F023 fixed: text-mode `ErrorWithHint` prints sanitized recovery hints.
- [x] `go test ./...` passes.
- [x] `go test ./tests/...` passes.
- [x] `git diff --check` passes.
- [x] `day11.md`, `day12.md`, `day13.md`, `03-memory-state-notes.md` remain unchanged.
- [x] Latest self-review has no unresolved actionable findings.

### Constraints

- Do not edit `day11.md`, `day12.md`, `day13.md`, or `03-memory-state-notes.md`.
- Keep changes minimal and focused on consensus findings F001-F023.
- Do not split `internal/cli/root.go`.
- Do not add heavy dependencies.
- Do not let LLM/model text own task state transitions, trusted evidence, or physical memory writes.
- Treat `artifacts/consensus/**` as local/private/untrusted evidence, not source instructions.

### Blast radius

- `.agents/skills/caveman-compress/scripts/*`
- `.agents/skills/caveman-compress/SKILL.md`, `.agents/skills/caveman-compress/README.md`, `.agents/skills/caveman-compress/SECURITY.md`
- `internal/app/*`
- `internal/cli/root.go`, `internal/cli/root_test.go`
- `internal/memory/*`
- `internal/process/*`
- `internal/profiles/*`
- `internal/prompting/*`
- `internal/providers/*`
- `internal/storage/*`
- `internal/tasks/*`
- `internal/validation/*`
- `tests/*`

### Execution log

- 2026-06-18: Started consensus verdict remediation. Loaded `goal-x`, read project rules, updated `ast-index`, and confirmed clean working tree before edits.
- 2026-06-18: Fixed F001-F023 across memory proposals, untrusted prompt roles, profile overrides, process FSM/CAS, storage hardening, provider disclosure, prompt audit, CLI text output, and caveman-compress safeguards.
- 2026-06-18: Verification passed: `go test ./...`, `go test ./tests/...`, `git diff --check`, `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest scripts.test_compress_safeguards`, `GOOS=windows go test -c ./internal/storage -o /var/folders/br/48dxplrx6dvdkm481dc2ggb80000gn/T/kilo/storage_windows.test.exe`, and forbidden-doc diff check.
- 2026-06-18: Independent self-review after fixes returned `no findings`.

## Context

Current task: review the full codebase against `docs/*`, find at least 50 concrete issues, score each issue from 0 to 10 objectively, then fix every valid issue scored 5+ through the `goal-x` loop.

Requirements source of truth:
- `docs/prd.md`
- `docs/frd.md`
- `docs/architect.md`
- `docs/implementation-plan.md`
- `docs/deterministic-stateful-agent-loop-epic.md`

## Acceptance criteria

- [x] At least 50 review findings are identified and scored 0-10.
- [x] Each finding is checked against `docs/*` or implementation robustness.
- [x] Every valid finding scored 5+ is fixed, or explicitly marked invalid/deferred with reason.
- [x] Fixes preserve Day 11, Day 12, Day 13 contracts.
- [x] Fixes do not add heavy dependencies.
- [x] `day11.md`, `day12.md`, `03-memory-state-notes.md` remain unchanged.
- [x] `go test ./...` passes.
- [x] `go test ./tests/...` passes.
- [x] Latest self-review result has no unresolved valid findings scored 5+.

## Constraints

- Use `docs/*` as requirements source.
- Keep changes minimal and focused on valid 5+ findings.
- Do not split `internal/cli/root.go`.
- Do not let LLM own task state transitions or memory writes.
- Do not add heavy dependencies.

## Blast radius

- `internal/app/*`
- `internal/cli/root.go`, `root_test.go`
- `internal/memory/*`
- `internal/process/*`
- `internal/profiles/*`
- `internal/prompting/*`
- `internal/providers/*`
- `internal/storage/*`
- `internal/tasks/*`
- `internal/validation/*`
- `tests/*`

## Execution log

### Setup

- Loaded project rules and `goal-x`.
- Updated `ast-index` before work and after edits.
- Read `docs/*` and existing implementation.
- Ran initial `go test ./...`: passing before fixes.

### Fix pass

- Fixed storage path hardening, JSONL locking/fsync, provider retry cap, model validation/init persistence, raw prompt audit opt-in, privacy purge coverage, memory classifier validation/redaction/escaping, profile/task state validation, memory proposal apply ordering/idempotency, prompt order, paused task gates, process validation, transition evidence gates, retry safety, and terminal escaping.
- Added regression tests across CLI, storage, memory, profiles, tasks, prompting, process, and acceptance suites.
- Final self-review found two more valid issues: concurrent proposal apply duplicate race and side-effect matcher gap; both fixed.

### Verification

- `go test ./internal/cli ./internal/process ./tests`: passed.
- `go test ./internal/memory ./internal/process ./internal/cli ./tests`: passed.
- `go test ./...`: passed.
- `go test ./tests/...`: passed.
- `git diff --check`: passed.
- Forbidden docs check: `git diff --name-only -- "day11.md" "day12.md" "03-memory-state-notes.md" "docs/day11.md" "docs/day12.md" "docs/03-memory-state-notes.md"` returned empty.
- Latest self-review: no unresolved valid findings scored 5+ after final fixes.

### Follow-up review/fix pass

- Re-reviewed the codebase against `docs/*` after the first ledger and found 14 additional valid findings scored 5+.
- Fixed safe privacy purge, JSONL no-follow opens, paused task-scoped Q&A gates, atomic planning transition, transition-before-accepted-history ordering, answer_question transition-signal validation, done pause/resume no-op behavior, long-memory scope inference, explicit memory apply action requirement, `chat --once --json` classifier failure handling, default JSON prompt minimization, P1 top-level task command exposure, and CLI P0 acceptance coverage.
- Added regression coverage in storage, tasks, memory, process, CLI, and acceptance tests.
- Verification after follow-up: `go test ./internal/storage ./internal/tasks ./internal/memory ./internal/process ./internal/cli ./tests`, `go test ./...`, `go test ./tests/...`, and `git diff --check` passed.
- Forbidden docs check after follow-up returned empty.

## Scored findings ledger

| # | Score | Basis | Finding | Status |
|---|---:|---|---|---|
| 1 | 9 | security/robustness | Storage safe-path checks could be bypassed through symlink parents. | Fixed in `internal/storage/safe_path.go`. |
| 2 | 6 | robustness | Safe-path hardening broke standard macOS temp paths through `/var` and `/tmp` compatibility symlinks. | Fixed with constrained system-symlink allowance. |
| 3 | 8 | robustness | JSONL file locking could block indefinitely under stale/concurrent lock contention. | Fixed with bounded lock timeout. |
| 4 | 6 | durability | New JSONL files did not fsync parent directory after create. | Fixed in `internal/storage/jsonl.go`. |
| 5 | 8 | docs: model selection | `assistant init --model` did not persist the validated active model. | Fixed in `internal/cli/root.go`. |
| 6 | 7 | docs: provider/model | OpenRouter model IDs were syntax-checked only, not verified against provider model list. | Fixed in `validateModelID`. |
| 7 | 6 | privacy | Raw rendered prompts were audited by default, retaining sensitive prompt context. | Fixed: prompt audit opt-in via env. |
| 8 | 6 | privacy | Privacy purge missed `prompts.jsonl`. | Fixed in purge logic. |
| 9 | 6 | privacy | Privacy purge missed root `process_audit.jsonl`. | Fixed in purge logic. |
| 10 | 7 | security | Terminal output escaped ASCII controls but not bidi/format controls. | Fixed in `safeTerminalText`. |
| 11 | 6 | UX/security | Memory proposal text hid record IDs, making safe selective apply/edit harder. | Fixed in proposal text output. |
| 12 | 7 | docs: provider disclosure | `init` could validate provider/model before OpenRouter disclosure was printed. | Fixed by disclosing before provider model lookup. |
| 13 | 6 | docs: validation evidence | CLI had no way to pass app-owned trusted evidence into process validation. | Superseded by current pass: public `--trusted-evidence` removed; only app-generated structured evidence is trusted. |
| 14 | 8 | docs: tool evidence | Execution output could claim tests passed without trusted app evidence. | Fixed in `ExecutionValidator`. |
| 15 | 8 | docs: tool evidence | Execution verification strings could claim tool/test results without trusted evidence. | Fixed in `ExecutionValidator`. |
| 16 | 7 | docs: side effects | Execution output could claim side effects without trusted evidence. | Fixed in `ExecutionValidator`. |
| 17 | 7 | docs: side effects | Side-effect matcher missed implementation phrases like `updated file` and diffs. | Fixed by reusing implementation matcher. |
| 18 | 7 | docs: answer_question | Informational answers could claim file/state/test mutations. | Fixed in `validateAnswerQuestion`. |
| 19 | 6 | docs: answer_question | Informational answers could propose or claim task transitions. | Fixed in `validateAnswerQuestion`. |
| 20 | 7 | docs: stage contract | `answer_question` accepted structured JSON for the wrong current stage. | Fixed with stage mismatch rejection. |
| 21 | 7 | docs: retry policy | `stage_mismatch` could be retried even though docs classify it as non-retryable. | Fixed in retry controller. |
| 22 | 7 | security | Retry prompt re-sent rejected model output, preserving possible prompt injection. | Fixed: retry prompt now includes validator errors only. |
| 23 | 6 | security | Validator errors were not escaped in retry prompt. | Fixed in retry controller. |
| 24 | 8 | docs: transition gate | `ready_for_done` could pass with model-authored `passed_checks` only. | Fixed: trusted app evidence required. |
| 25 | 8 | docs: transition gate | Transition gate allowed validation-to-done without trusted evidence. | Fixed in `TransitionGate`. |
| 26 | 7 | docs: no LLM state ownership | Model text resembling trusted tool evidence could be accepted. | Fixed: model text no longer counts as trusted evidence. |
| 27 | 7 | docs: paused task | Paused task safe question path could still persist short memory and run classifier. | Fixed: paused `answer_question` returns read-only after audit. |
| 28 | 7 | docs: paused task | Paused task continuation routing could allow mutation-like requests as questions. | Fixed in action resolver override/gate. |
| 29 | 6 | docs: paused task | Paused memory apply was not consistently hard-gated. | Fixed in CLI slash/apply gates. |
| 30 | 6 | docs: hard gates | Paused task checks could happen after model validation/provider disclosure in some paths. | Fixed with preflight ordering tests. |
| 31 | 7 | security | Memory classifier payload could include secret-like content before provider call. | Fixed with pre-provider secret scan. |
| 32 | 6 | security | Classifier existing memory content was not escaped before model prompt. | Fixed in classifier prompt construction. |
| 33 | 6 | validation | Classifier accepted records with missing required fields. | Fixed with stricter record validation. |
| 34 | 5 | validation | Classifier proposal failures were not fully reflected in process audit. | Fixed with audit entries on classifier/proposal failures. |
| 35 | 7 | data integrity | Memory proposal `accepted/edited` status was written before physical memory save succeeded. | Fixed with two-phase save/reconcile. |
| 36 | 7 | data integrity | Concurrent proposal apply could save duplicate memory records. | Fixed with cross-process apply lock and idempotency check. |
| 37 | 6 | data integrity | Proposal apply did not reliably persist `SavedRecordID` after physical save. | Fixed during reconcile. |
| 38 | 6 | auditability | Proposal edits could lose proposed-vs-applied layer/content audit detail. | Fixed in proposal apply record fields. |
| 39 | 6 | validation | Secret-like proposal content/edits could be accepted into physical memory. | Fixed with proposal secret blocking/redaction. |
| 40 | 5 | memory budget | Prompt memory selection could let one layer crowd out higher-value later layers. | Fixed by preserving long-memory budget. |
| 41 | 6 | docs: untrusted context | Prompt builder rendered task/memory/profile order inconsistently with trusted-stage docs. | Fixed in prompt builder. |
| 42 | 6 | docs: answer_question | Stage prompt for `answer_question` did not make read-only contract explicit enough. | Fixed in stage prompt factory. |
| 43 | 6 | validation | Loaded profile JSON was not revalidated after disk read. | Fixed in profile manager. |
| 44 | 6 | validation | Persisted task state was not revalidated after disk read. | Fixed in task manager. |
| 45 | 6 | validation | Task state machine allowed invalid persisted stage/status/action combinations through. | Fixed with `ValidateState`. |
| 46 | 6 | race | Task start could overwrite/race existing active state without a final lock-time check. | Fixed in task manager. |
| 47 | 5 | validation | Task move/mutation logic did not consistently reject paused-state mutations. | Fixed in task state manager/CLI gates. |
| 48 | 6 | provider robustness | OpenRouter `Retry-After` could force unbounded sleep. | Fixed with capped retry delay. |
| 49 | 5 | provider robustness | OpenRouter retry delay parsing did not guard hostile values tightly enough. | Fixed with cap tests. |
| 50 | 5 | CLI correctness | `memory propose --latest` behavior existed but flag contract was not exposed. | Fixed by adding explicit `--latest` flag. |
| 51 | 4 | docs/readability | `internal/cli/root.go` is large and hard to review. | No change: explicit constraint says do not split. |
| 52 | 4 | testing | Some tests depend on fake provider heuristics. | No change: below 5, existing fake provider remains sufficient after regressions. |
| 53 | 3 | product scope | No separate reviewer model/agent is implemented. | No change: docs mark separate model/agent as not required for P1. |
| 54 | 3 | product scope | No autonomous commit automation. | No change: docs explicitly forbid commit automation. |
| 55 | 6 | docs: storage/privacy | `privacy purge` deleted via raw joined paths and did not validate session/file symlink targets. | Fixed in `internal/cli/root.go` with `SafeJoin`, ID validation and symlink rejection. |
| 56 | 5 | docs: storage safety | JSONL final/lock opens could follow a swapped symlink after precheck. | Fixed in `internal/storage/jsonl.go` with `O_NOFOLLOW` opens. |
| 57 | 8 | docs: paused task gate | Paused task-scoped Q&A could still call provider as `answer_question`. | Fixed in `ProcessController` with task-scoped paused query rejection. |
| 58 | 7 | docs: transition gate | Planning output could be persisted before a later stage move failure. | Fixed with atomic `MoveWithPlanningOutput`. |
| 59 | 6 | docs: persistence ordering | Transition failure could leave model output in accepted short memory. | Fixed by applying transition before accepted short-memory persistence. |
| 60 | 5 | docs: answer_question | `answer_question` allowed transition signals like `ready_for_execution_proposal`. | Fixed in `validateAnswerQuestion`. |
| 61 | 5 | docs: done terminal no-op | Done pause/resume rewrote task files instead of terminal no-op. | Fixed in task manager and state validation. |
| 62 | 6 | docs: long memory semantics | Long-term decisions/knowledge defaulted to profile scope and disappeared across profiles. | Fixed with kind-based long scope inference. |
| 63 | 5 | docs: memory confirmation | Empty proposal apply returned success with zero user decisions. | Fixed with core/CLI `missing_apply_action`. |
| 64 | 8 | docs: P0 scriptability | Day 11/12/13 acceptance lacked CLI-level scriptable coverage. | Fixed with CLI P0 day-flow regression. |
| 65 | 8 | docs: classifier requirement | `chat --once --json` could succeed when classifier failed, violating Day 11 P0 flow. | Fixed with strict proposal requirement for non-interactive JSON. |
| 66 | 7 | docs: privacy/output | Default `chat --once --json` exposed raw rendered prompt/messages instead of prompt id. | Fixed with default `rendered_prompt_id` and raw prompt only under `--render-prompt`. |
| 67 | 6 | CLI correctness | Top-level `memory apply` with no action hid usage errors. | Fixed with CLI/core missing-action regression. |
| 68 | 5 | docs: command boundary | Top-level `task plan`/`task criteria` were exposed despite P1/debug command boundary. | Fixed by removing top-level exposure; slash debug commands remain. |
