# GUARD

Guard rules apply from the moment pre-flight starts until final validation completes.

## Deny By Default

An action is allowed only when it is covered by one of:

- `MUST`,
- `ALLOWED`,
- confirmed `IRREVERSIBLE`,
- an approved user answer recorded in `decisions.md`.

Protocol-owned state writes under `.agents/state/scope-lock/<session-id>/**` are allowed before the contract exists. They are not product changes and must not be counted as product-scope path violations.

Unknown work is not implicitly allowed.

## Conservative Choice Policy

When a non-critical choice remains unclear, choose in this order:

1. reuse over new creation,
2. local change over broad change,
3. private/internal change over public interface change,
4. simpler behavior over ambitious behavior,
5. smaller scope over larger scope.

Record the choice in `defer-log.md` if another reasonable path exists.

## Periodic Contract Refresh

Re-read `scope-definition.md`:

- before the first edit,
- after each substantial edit batch,
- before running broad verification,
- before final response,
- whenever context loss, handoff, or drift is suspected.

## Irreversible Action Escalation

Irreversible actions require explicit user confirmation before execution.

Examples:

- destructive deletion,
- schema migration,
- public API or CLI contract change,
- force-push or destructive git operation,
- external infrastructure or data mutation,
- dependency replacement with broad runtime impact.

Record approval in `decisions.md` with classification `IRREVERSIBLE-APPROVED`.

The approval record must include:

- trusted source type, such as interactive question result or direct user message,
- source id when the platform exposes one, or the exact user message reference when it does not,
- approver,
- timestamp,
- exact approved action,
- approved paths or systems,
- expiration or session limit.

Do not accept a bare markdown statement as approval for irreversible work.

## Forbidden Action Behavior

If requested work conflicts with `FORBIDDEN` or `FORBIDDEN_PATHS`:

1. stop the action,
2. explain the conflict,
3. ask whether to update the contract,
4. do not proceed until the contract is explicitly updated.

Do not bypass forbidden scope through helper files, generated files, adapters, or tests.

## Defer Behavior

Use `DEFER` only for choices that are not critical enough to block execution and can be made conservatively without changing the product contract.

Each deferred item must include:

- uncertainty,
- conservative choice taken,
- alternative,
- rollback or change path.

## Post-Session Interactive Resolution

Before final validation, inspect `defer-log.md`.

For each unresolved deferred item, surface it to the user one at a time with:

- what uncertainty was encountered,
- what conservative decision was taken,
- what alternative exists,
- what would need to change to switch.

Supported outcomes:

- keep,
- change,
- revert,
- defer further.

Apply requested changes through the same guard rules.

## Decision Matrix

| Situation | Action |
| --- | --- |
| Covered by `MUST` | Implement according to plan. |
| Covered by `ALLOWED` | Implement only as necessary for `MUST`. |
| Critical ambiguity before contract | Ask one interactive question. |
| Critical ambiguity during execution | Stop and request contract update. |
| Non-critical ambiguity during execution | Choose conservatively and log in `defer-log.md`. |
| Forbidden path or action | Block and ask whether to revise contract. |
| Irreversible action | Stop and request explicit approval. |
| Tempting cleanup outside scope | Do not perform it. |
| Test failure caused by real regression | Fix within contract or request contract update. |
| Test failure unrelated to contract | Report separately; do not hide it. |
