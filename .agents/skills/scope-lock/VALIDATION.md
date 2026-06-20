# VALIDATION

Validation proves that execution matched the confirmed contract. It is not a summary pass.

## Required Checks

The Validator Agent must check:

- all `MUST` items are complete,
- all `DONE_CRITERIA` items have evidence,
- all changed paths are within `ALLOWED_PATHS`,
- protocol-owned state paths under `.agents/state/scope-lock/<session-id>/**` are excluded from product-scope path violations,
- no `FORBIDDEN_PATHS` were touched,
- no `FORBIDDEN` scope was implemented,
- irreversible actions have explicit approvals,
- tests or checks required by the contract were run,
- tests were not weakened to pass,
- deferred items were surfaced and resolved or consciously left deferred,
- no unresolved critical ambiguity remains hidden,
- delivered behavior still matches the intended product.

## Required Questions

The validator must answer:

- Did we build what the user asked for?
- Did we avoid building what the user did not ask for?
- Did any design choice materially exceed the agreed product scope?
- Did implementation follow existing architecture rather than inventing a new one?
- Did verification evidence prove completion rather than only absence of obvious errors?

## Path Compliance

Compare the actual diff against:

- `ALLOWED_PATHS`,
- `FORBIDDEN_PATHS`,
- plan expected files,
- protected files from project rules.

Any unexpected path is a finding unless it is explicitly approved and added to the contract.

## Contract Compliance

For every `MUST`, record:

- implementation evidence,
- verification evidence,
- remaining risk.

For every `FORBIDDEN`, record whether the diff avoided it.

For every irreversible item, record approval source.

Approval source must be trusted provenance, not only free-form markdown. If trusted provenance is missing, validation fails.

## Finding Format

Use this format:

```text
severity: blocker|high|medium|low|note
location: file or contract section
problem: concrete mismatch or risk
fix: exact required action, contract update, or blocker reason
```

Only call validation clean when there are no unresolved findings.

## Final Validation Output

Write `validation.md` with:

- contract status,
- changed paths,
- verification commands,
- evidence summary,
- deferred-item resolution,
- findings,
- final verdict: `PASS`, `BLOCKED`, or `FAIL`.

`PASS` is allowed only when the implementation and contract match.

Use the `templates/validation.md` structure when writing the validation artifact.
