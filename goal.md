# Goal: real CLI behavior, LLM validation, manual test coverage

## Context

The project must behave like a real CLI assistant application, not like a demo
surface that only passes scripted Day 11-14 examples. Current risks from live
testing: deterministic phrase gates and regex validators can make behavior feel
demo-only. The user wants real-app behavior plus documentation with enough manual
scenarios for future agents to run.

## Acceptance criteria

- The CLI has a documented answer to "real app vs demo": current behavior,
  remaining limits, and how to run real mode are clear in docs.
- Runtime validation for real provider flows uses an LLM referee outside the
  main assistant dialogue where semantic judgment is needed, while deterministic
  gates remain only for hard local safety and storage rules.
- LLM validation is integrated into process flow without sending secrets, with
  typed audit evidence and tests proving:
  - semantic approval can allow valid outputs;
  - semantic rejection blocks invalid outputs;
  - classifier/validator failures do not silently mutate task state;
  - fake mode remains deterministic for CI.
- Exact phrase checks are not the sole mechanism for user intent transitions in
  real mode; user-facing transition commands still work but are backed by a
  general intent mechanism or LLM referee path.
- Manual testing documentation is expanded with non-demo, agent-runnable cases:
  happy paths, edge cases, recovery, restart, LLM validation, provider failures,
  safety/invariant conflicts, profile/memory/task interactions, and negative
  cases. The document should be broad but not bloated.
- `go test ./...` passes.
- Latest self-review has no unresolved findings.

## Constraints

- Do not modify `day11.md`, `day12.md`, or `03-memory-state-notes.md`.
- Do not print or persist `OPENROUTER_API_KEY`.
- Keep repo-local changes only.
- Prefer small, integrated changes over large rewrites.

## Blast radius

- `internal/process`, `internal/cli`, `internal/providers`, `internal/app`,
  `tests`, and focused docs under `docs/`.
- `goal.md` status log.

## Open questions

- None. Use conservative defaults: fake mode stays deterministic; OpenRouter real
  mode can use LLM validation by default unless explicitly disabled.

## Log

- Started new goal on 2026-06-18.
- Added provider `PurposeValidator`, fake validator responses, and `process.SemanticValidator` for out-of-band intent/output validation.
- Split validation into structural hard checks and semantic LLM checks. Fake mode keeps deterministic validators; OpenRouter mode enables semantic validation by default unless `ASSISTANT_LLM_VALIDATION=off`.
- Hooked semantic intent into process routing so real mode is not limited to exact demo phrases; stage policy still blocks forbidden actions.
- Mapped semantic transition signals to stage actions, so approvals/review-ready intents work by meaning even if `action_kind` is generic.
- Hooked semantic output validation before task/memory persistence and transitions, with tests for pass, reject, invalid referee output, and fake deterministic fallback.
- Added typed audit decisions for the LLM referee: `semantic_intent_call` and `semantic_output_call`.
- Hardened classifier handling so an unknown memory `kind` is coerced to `other` instead of hiding a safe chat answer.
- Hardened fake validator defaults so forced fake semantic mode remains deterministic for intent and output checks.
- Expanded provider disclosure to mention semantic validation payload.
- Added `docs/manual-testing-real-cli.md` with real-app answer, validation model, and 20 agent-runnable manual cases.
- Verification: `go test ./...` passed; `go build -o .assistant/bin/assistant ./cmd/assistant` passed; `git diff --check` passed.
- Live verification: `deepseek/deepseek-v4-flash` via OpenRouter returned `ok: true`; process audit recorded `semantic_intent_call`, chat `provider_call`, `semantic_output_call`, `accepted`, and classifier `provider_call`.
- Index verification: `ast-index update` passed outside sandbox; `ast-index stats` sees 87 files / 1336 symbols.
- Latest self-review: no unresolved findings.
- Full manual verification: ran all 20 cases from `docs/manual-testing-real-cli.md` with real OpenRouter `deepseek/deepseek-v4-flash`; final run `manual-live-20260618-194037` passed 20/20.
- Fixes from manual runs: relaxed semantic validator to judge side effects/FSM safety rather than ordinary programming facts, preserved no-task answer intent, removed bare `sk-` false-positive invariant matcher, rerouted active-task classifier requirements to work memory, stabilized done/semantic-ready manual setup, and added `доработай` as done-stage mutation intent.
