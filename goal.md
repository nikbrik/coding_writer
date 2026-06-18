# Goal: Day14 Invariants

## Context

Implement `.kilo/plans/day14-invariants.md` fully. Close Day14 mandatory acceptance without regressing Day11, Day12, Day13, or the deterministic process loop documented under `docs/*`.

Day14 requires a separate invariant layer: defaults stored outside dialog, active invariants rendered into prompts, deterministic input/output conflict blocking, explicit refusal that names the conflicting invariant, CLI/REPL inspection/add surface, deterministic tests without live OpenRouter, and docs traceability.

## Acceptance criteria

- Invariants are stored separately from dialogue under `<storage_root>/invariants/project.jsonl`.
- Active invariants are explicitly rendered in the assistant prompt with stable marker `Invariant policy` and `id="invariants.active"`.
- User requests and assistant/provider outputs that violate active invariants are blocked deterministically.
- Conflict refusals include `invariant_conflict`, the conflicting invariant ID, and conflict evidence.
- Input-side invariant conflicts do not call the provider.
- Output-side invariant conflicts are rejected before accepted persistence and before memory classifier flow; bounded retry may be used for fixable output.
- Day11 memory proposal/apply/influence tests still pass.
- Day12 profile prompt/response tests still pass.
- Day13 pause/resume/FSM tests still pass.
- Day14 deterministic acceptance test covers separate storage, prompt accounting, conflict refusal, provider-not-called behavior, and non-conflicting flow.
- CLI supports `assistant invariants list --json` and `assistant invariants add <id> --kind <kind> --content <text> [--severity block] [--forbid <term>...] [--json]`.
- REPL supports `/invariants` and `/invariants add <id> --kind <kind> --content <text> --forbid <term>` minimally.
- Docs updated in existing docs only: `docs/implementation-plan.md`, `docs/frd.md`, `docs/prd.md`, `docs/architect.md`, `docs/manual-testing-day11-13.md`.
- Verification commands pass: `go test ./...` and `env -u ASSISTANT_MODEL -u ASSISTANT_STORAGE_DIR go test ./tests -run 'TestDay11|TestDay12|TestDay13|TestDay14'`.
- Final self-review has no unresolved findings.

## Constraints

- Do not modify `day11.md`, `day12.md`, `day13.md`, or `03-memory-state-notes.md`.
- Do not use live OpenRouter in tests.
- Keep changes minimal and integrated with existing architecture.
- No heavy new dependencies, tools, or services.
- Use ast-index first for code search and update it after edits.
- Keep lecture notes unchanged unless explicitly requested.

## Blast radius

Allowed files/modules:

- New `internal/invariants` package.
- Shared app models where needed, likely `internal/app/models.go`.
- Runtime/init/config wiring.
- Prompt builder and process controller integration.
- CLI root and REPL command handling.
- Tests listed in `.kilo/plans/day14-invariants.md` or adjacent existing tests.
- Existing docs listed in acceptance criteria.
- This `goal.md` execution log.

Forbidden unless explicitly needed by local integration:

- `day11.md`, `day12.md`, `day13.md`, `03-memory-state-notes.md`.
- Global/user configuration outside repo.

## Open questions

None. Follow `.kilo/plans/day14-invariants.md` as implementation plan.

## Execution log

- 2026-06-18: Created goal from `.kilo/plans/day14-invariants.md`. Initial ast-index update: up to date.
- 2026-06-18: Implemented `internal/invariants` manager with JSONL storage, defaults, add/list/render/check, secret/ID/severity validation, and deterministic conflict evidence.
- 2026-06-18: Added shared `app.Invariant` and `app.InvariantViolation`; added `invariants/` to storage tree.
- 2026-06-18: Wired runtime/init/process/prompt: defaults persisted by `assistant init`, active invariants rendered with `Invariant policy` and `id="invariants.active"`, input conflicts fail before provider, output conflicts fail before accepted persistence/memory classifier.
- 2026-06-18: Added CLI `assistant invariants list/add` and REPL `/invariants` plus `/invariants add`.
- 2026-06-18: Added tests for manager, prompt order/visibility, process input/output blocking, CLI JSON list/add, and Day14 acceptance.
- 2026-06-18: Updated allowed docs: `docs/implementation-plan.md`, `docs/frd.md`, `docs/prd.md`, `docs/architect.md`, `docs/manual-testing-day11-13.md`.
- 2026-06-18: Verification passed: `go test ./...`; `env -u ASSISTANT_MODEL -u ASSISTANT_STORAGE_DIR go test ./tests -run 'TestDay11|TestDay12|TestDay13|TestDay14'`; `ast-index update`.
- 2026-06-18: Self-review finding: FRD prompt-order row too generic for Day14 marker/source. Fixed. Re-ran required verification. Latest self-review result: no findings.
- 2026-06-18: Fixed consensus findings F001-F011: runtime default seeding/fail-closed storage paths, REPL `/invariants` list/add/help, hard-gated output invariant conflicts with no retry/classifier path, source-aware invariant prompt rendering, count/content/term limits, unsupported `RequiredTerms` rejection, structured JSON violations, context-aware preflight, and redacted audit evidence.
- 2026-06-18: Updated allowed docs for literal matching contract, invariant priority, provider visibility, JSON violations, and no-retry output conflicts. Verification passed: `go test ./internal/invariants ./internal/process ./internal/cli`; `go test ./...`; `env -u ASSISTANT_MODEL -u ASSISTANT_STORAGE_DIR go test ./tests -run 'TestDay11|TestDay12|TestDay13|TestDay14'`; `ast-index update`. Latest self-review result: no findings.
- 2026-06-18: Local uncommitted review found 4 issues. Fixed all: process/stage gates now precede invariant conflicts, default seeding no-ops on complete stores instead of rewriting on every read, unused `Manager.Render` removed, unused `Preflight` wrapper removed. Added regression for process gate priority. Verification passed: `go test ./internal/invariants ./internal/process ./internal/cli`; `go test ./...`; `env -u ASSISTANT_MODEL -u ASSISTANT_STORAGE_DIR go test ./tests -run 'TestDay11|TestDay12|TestDay13|TestDay14'`; `ast-index update`. Latest self-review result: no findings.
