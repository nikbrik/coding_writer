# CHECKLIST

Use this checklist during pre-flight and validation. It is not passive documentation. Record relevant answers in `audit.md`, `ambiguities.md`, `scope-definition.md`, or `decisions.md`.

## 1. Understanding The Task

- Why does this task exist?
- Who is the user or operator?
- What is the happy path?
- Which edge cases matter?
- What are the concrete success criteria?
- What is explicitly not part of the task?
- What would make the delivered result the wrong product?

## 2. Architecture

- Which layer owns the change?
- Which existing pattern should this follow?
- Which interfaces or responsibilities are affected?
- Are there sync, async, lifecycle, or ordering concerns?
- Are there backward-compatibility concerns?
- Does dependency direction remain clean?
- Does the change preserve runtime-specific adapter boundaries?

## 3. Existing Code

- Does similar functionality already exist?
- What should be reused?
- What is the impact radius of touched files?
- Which existing tests cover this behavior?
- Are there known-issue comments or documented error hotspots near the change?
- Are generated files or vendored files involved?

## 4. Data And State

- Which data models are affected?
- What validates the data?
- How are null, empty, invalid, and error states handled?
- Are there schema, migration, compatibility, or persistence implications?
- Who owns state updates?
- What state must persist across handoffs or context loss?

## 5. Edge Cases And Failures

- What invalid input must be handled?
- What empty states must be handled?
- Are auth or permission states relevant?
- Are race conditions or concurrency relevant?
- What dependent systems can fail?
- What is the conservative fallback?

## 6. Dependencies

- Is a new library actually needed?
- Can the existing stack solve the need?
- Are there version, license, build, or conflict implications?
- Does a dependency change affect runtime packaging?
- Is the dependency allowed by the contract?

## 7. Testing

- Which tests are required?
- Which manual checks are required?
- Which scenarios matter most?
- How will evidence be captured?
- What must not be weakened to make tests pass?
- Are semantic decisions validated with an appropriate semantic method instead of trigger words?

## 8. Irreversible Actions

- Is any file deletion destructive?
- Is any migration, force operation, public API change, or external action irreversible?
- What approval is required?
- What rollback or mitigation exists?
- Where is the approval recorded?

## 9. Scope Boundaries

- What are the explicit non-goals?
- Which files and modules are forbidden?
- Which opportunistic improvements must be rejected?
- Which refactors are local and necessary?
- Which refactors are unrelated and forbidden?
- What should be deferred instead of silently expanded?

## Checklist Output

Before execution, the agent must be able to point to persisted answers for:

- product intent,
- scope boundaries,
- allowed paths,
- forbidden paths,
- done criteria,
- unresolved deferred items,
- irreversible approvals.
