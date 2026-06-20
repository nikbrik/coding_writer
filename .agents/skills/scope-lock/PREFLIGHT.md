# PREFLIGHT

Pre-flight exists to build shared understanding before execution. The agent must audit first, detect critical ambiguity second, ask questions third, and only then generate a scope contract.

## Stage 1: Codebase Audit

Run a silent audit before asking the user anything. Answer questions from the repository whenever possible.

Inspect:

- repo structure and package layout,
- project rules such as `AGENTS.md`, `CLAUDE.md`, `.agents/rules/*`, `.kilo/rules/*`, or equivalents,
- existing architecture patterns,
- similar components or prior implementations,
- public interfaces and command surfaces,
- data models, schemas, persisted state, and migrations,
- tests and test helpers,
- dependency files and build files,
- high-risk areas likely to be touched.

Write `.agents/state/scope-lock/<session-id>/audit.md` using the `templates/audit.md` structure.

Required audit content:

- task summary,
- relevant files read,
- existing patterns to follow,
- likely touched areas,
- protected or risky areas,
- facts learned from code,
- questions that code already answered.

Do not ask the user about facts the audit can answer.

## Stage 2: Ambiguity Pass

Run a dedicated ambiguity pass after the audit and before the interview. Detect only high-impact unknowns.

Check these categories:

- user intent and user personas,
- product behavior,
- happy path and edge cases,
- architecture and layering,
- reuse versus new code,
- data models and schema assumptions,
- dependency choices,
- public API or CLI changes,
- testing expectations,
- success criteria,
- explicit non-goals.

Write `.agents/state/scope-lock/<session-id>/ambiguities.md` using the `templates/ambiguities.md` structure, with each ambiguity classified:

- `CRITICAL`: must resolve before contract,
- `DEFERABLE`: can choose conservatively and surface later,
- `NON-BLOCKING`: document only.

Do not solve ambiguities in this pass.

## Stage 3: Grill Interview

Ask one question at a time. Resolve one branch before opening another.

Rules:

- ask only about `CRITICAL` ambiguities that code and prior answers did not resolve,
- prefer the highest-impact question first,
- present options when useful,
- recommend one option with concise reasoning,
- use `question`, `ask_followup_question`, `request_user_input`, or the platform equivalent when available,
- do not batch unrelated questions,
- stop after each question until the answer is received.

Record answers in `decisions.md` and update `handoff-state.json`.

When an answer approves an irreversible action, record trusted provenance: source type, source id or exact user message reference, approver, timestamp, exact approved action, and approved scope. A plain markdown note is not enough.

End the interview only when critical ambiguities are resolved and the scope can be locked.

## Stage 4: Scope Contract

Generate `scope-definition.md` from the template. It must contain:

- `MUST`,
- `ALLOWED`,
- `DEFER`,
- `FORBIDDEN`,
- `IRREVERSIBLE`,
- `ALLOWED_PATHS`,
- `FORBIDDEN_PATHS`,
- `DONE_CRITERIA`.

Show the contract to the user and request confirmation before coding. If confirmation is ambiguous, ask again. Do not treat silence as approval.

## Stage 5: Planning

Generate `plan.md` only after contract confirmation.

Every plan item must trace to at least one `MUST` item. No plan item may exist because it seems cleaner, interesting, or broadly useful.

Each plan item must include:

- linked contract item,
- expected files,
- exact allowed path coverage,
- expected verification.

## Pre-Execution Gate

Implementation may start only when all are true:

- audit is persisted,
- critical ambiguities are resolved,
- scope contract exists,
- user confirmed the contract,
- plan exists and maps to `MUST`,
- guard rules are loaded.
