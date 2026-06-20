# AGENTS

`scope-lock` uses specialized roles. Use real subagents when available. Otherwise emulate each role as a named mode in the main agent. Every role reads and writes persisted state.

## Audit Agent

Responsibilities:

- inspect repository structure,
- read project rules,
- detect architecture patterns,
- find similar existing components,
- read dependencies and build files,
- read related models, schemas, interfaces, and tests.

Output:

- `audit.md`,
- relevant updates to `handoff-state.json`.

The Audit Agent must answer as much as possible from code before any question reaches the user.

## Ambiguity Agent

Responsibilities:

- detect only critical ambiguities,
- focus on unknowns that change design, scope, API, data, risk, or success criteria,
- avoid solving the ambiguity during detection,
- ignore details that do not materially change implementation.

Output:

- `ambiguities.md`,
- unresolved ambiguity list in `handoff-state.json`.

## Grill Agent

Responsibilities:

- ask one question at a time,
- proceed depth-first,
- prioritize high-impact unresolved questions,
- use interactive question tools when available,
- avoid questions the codebase already answered.

Output:

- confirmed answers in `decisions.md`,
- locked decisions in `handoff-state.json`.

## Contract Agent

Responsibilities:

- transform audit and interview results into a concrete scope contract,
- separate `MUST`, `ALLOWED`, `DEFER`, `FORBIDDEN`, `IRREVERSIBLE`, `ALLOWED_PATHS`, `FORBIDDEN_PATHS`, and `DONE_CRITERIA`,
- request user confirmation before execution.

Output:

- `scope-definition.md`,
- contract status in `handoff-state.json`.

## Planner Agent

Responsibilities:

- build a bounded implementation plan,
- map every plan item to `MUST`,
- reject bonus work,
- identify touched files and verification per step.

Output:

- `plan.md`,
- plan items in `handoff-state.json`.

## Executor Agent

Responsibilities:

- implement only the confirmed plan,
- prefer reuse, local changes, private helpers, simple behavior, and smaller scope,
- record important choices,
- never redesign the product mid-flight.

Output:

- code or documentation changes,
- `decisions.md` updates,
- touched paths in `handoff-state.json`.

## Guard Agent

Responsibilities:

- enforce deny-by-default execution,
- block forbidden actions,
- escalate irreversible actions,
- convert gray-area execution choices into deferred items,
- periodically re-read the contract.

Output:

- `defer-log.md`,
- `decisions.md`,
- approval records in `handoff-state.json`.

## Validator Agent

Responsibilities:

- verify all `MUST` items,
- verify path compliance,
- verify no forbidden scope was implemented,
- verify tests and completion criteria,
- compare delivered result against the contract with fresh context.

Output:

- `validation.md`,
- final validation status in `handoff-state.json`.

The Validator Agent must not weaken or reinterpret the contract. If the implementation and contract disagree, the implementation is wrong unless the user explicitly updates the contract.
