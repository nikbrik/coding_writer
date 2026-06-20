# Final prompt for implementing `scope-lock`

Create the final production version of a skill named `scope-lock` for my coding-agent harness.

The goal of this skill is to prevent two classes of failure:
1. The agent invents missing details instead of asking or flagging them.
2. The agent drifts outside the agreed product scope and builds the wrong thing.

This skill must combine the strongest ideas from:
- `grill-me` style interviewing: depth-first, one question at a time, shared understanding first.
- ambiguity-detection style thinking: explicitly search for critical ambiguities before execution.
- scope/permission systems like agent-scope-skill: explicit boundaries, allowed paths, forbidden paths, and execution discipline.
- multi-agent decomposition: use specialized subagents/roles where appropriate instead of forcing one monolithic agent to do everything.

This is not just a prompt. This is an execution protocol.

---

## High-level architecture

Implement `scope-lock` as a multi-stage, multi-role workflow with one persistent shared state.

The workflow has these stages:

1. **Codebase audit**
2. **Ambiguity pass**
3. **Grill interview**
4. **Scope contract generation**
5. **Planning**
6. **Execution with guardrails**
7. **Post-session interactive resolution**
8. **Final validation**

Each stage must operate on explicit persisted state on disk, not only chat memory.

---

## Core design principles

Follow these principles exactly:

### 1. Shared understanding before execution
The skill must not begin implementation before it has:
- read the relevant codebase and project rules,
- identified critical ambiguities,
- asked clarifying questions,
- generated a confirmed scope contract.

### 2. No silent assumption-making
If something materially changes architecture, user experience, data shape, dependencies, public interfaces, or the product surface, it must never be silently assumed.

### 3. Deny by default
If an action is not clearly in scope, it is not implicitly allowed.
Unknown items must become either:
- an explicit question during pre-flight, or
- a deferred decision during execution, or
- a blocked action if forbidden or irreversible.

### 4. Execution must be bounded
After pre-flight, the execution agent writes code only against the confirmed contract.
It must not redesign the product mid-flight.

### 5. Interactivity must be native
The user must not be required to inspect logs manually.
Whenever user input is needed, the agent must use the platform's interactive question mechanism (`question`, `ask_followup_question`, or equivalent).

### 6. State must survive drift
All important decisions must be written to files so the workflow survives long sessions, context loss, and handoffs between agents.

---

## Required directory layout

Create a universal source of truth under `.agents`, and thin platform-specific entrypoints for Kilo and Codex.

```text
.agents/skills/scope-lock/
├── SKILL.md
├── PREFLIGHT.md
├── CHECKLIST.md
├── AGENTS.md
├── GUARD.md
├── HANDOFFS.md
├── VALIDATION.md
└── templates/
    ├── scope-definition.md
    ├── decisions.md
    ├── defer-log.md
    ├── plan.md
    └── handoff-state.json

.kilocode/skills/scope-lock/
└── SKILL.md

.kilo/skills/scope-lock/
└── SKILL.md

.codex/skills/scope-lock/
└── SKILL.md
```

The universal implementation lives in `.agents/skills/scope-lock/`.
The Kilo/Codex entrypoints must be thin wrappers that point to the universal implementation.

---

## Required workflow roles

Design the skill as if it contains these specialized roles or subagents.
If the harness supports true subagents, use them.
If not, emulate them as explicit modes with the same responsibilities.

### 1. Audit Agent
Responsibilities:
- Inspect the repository structure.
- Read project rules (`AGENTS.md`, `CLAUDE.md`, `.kilocode/rules`, or equivalents).
- Detect existing architecture patterns.
- Find similar existing components.
- Read dependencies and build files.
- Read models, schemas, interfaces, and tests related to the task.

Output:
- structured audit summary written to persistent state.

### 2. Ambiguity Agent
Responsibilities:
- Detect only critical ambiguities.
- Focus on ambiguities that materially change design, scope, APIs, data, risk, or success criteria.
- Do not solve them yet.
- Produce a list of unresolved critical ambiguities.

Output:
- structured ambiguity list.

### 3. Grill Agent
Responsibilities:
- Ask questions one at a time, depth-first.
- Resolve one branch before opening too many others.
- Prefer high-impact questions first.
- Use interactive question tools, not plain-text chatting when tooling exists.
- Avoid asking questions that the codebase already answers.

Output:
- confirmed answers and locked decisions.

### 4. Contract Agent
Responsibilities:
- Transform the audit and interview outputs into a concrete scope contract.
- Write `scope-definition.md`.
- Explicitly separate `MUST`, `ALLOWED`, `DEFER`, `FORBIDDEN`, `IRREVERSIBLE`, `ALLOWED_PATHS`, `FORBIDDEN_PATHS`, and `DONE_CRITERIA`.

Output:
- a contract that the user confirms before coding starts.

### 5. Planner Agent
Responsibilities:
- Build a bounded implementation plan from the confirmed contract.
- Break work into small tasks that map directly to `MUST` items.
- Prevent “bonus work” not required by the contract.

Output:
- `plan.md`.

### 6. Executor Agent
Responsibilities:
- Implement only what is in the plan and scope contract.
- Use conservative choices when ambiguity remains.
- Record important decisions.
- Never silently expand product scope.

Output:
- code changes plus decision log updates.

### 7. Guard Agent
Responsibilities:
- Enforce scope during execution.
- Block forbidden actions.
- Escalate irreversible actions to the user.
- Convert unresolved gray-area decisions into deferred items.
- Re-read the scope contract periodically to fight drift.

Output:
- updates to `defer-log.md`, decisions, and interactive confirmations when needed.

### 8. Validator Agent
Responsibilities:
- Verify that all `MUST` items are done.
- Verify that forbidden areas were not changed.
- Verify test expectations and completion criteria.
- Verify the delivered result still matches the contract and not an agent-invented variant.

Output:
- final validation result.

---

## Stage-by-stage behavior

## Stage 1 — Codebase audit

Before asking the user anything, perform a silent audit.

The audit must check:
- repo structure,
- project rules,
- architectural style,
- existing components similar to the requested feature,
- public interfaces,
- data models and schemas,
- test structure,
- dependency files,
- high-risk areas likely to be touched.

The audit must answer as many questions as possible from code instead of asking the user.

Persist the results.

---

## Stage 2 — Ambiguity pass

Before grilling, run a dedicated ambiguity detection pass.

The ambiguity pass must look for high-impact unknowns in these categories:
- user intent and user personas,
- product behavior,
- happy path and edge cases,
- architecture and layering,
- existing code reuse vs new code,
- data models and schema assumptions,
- dependencies,
- API and public interface changes,
- testing expectations,
- success criteria,
- out-of-scope and not-to-do items.

Important:
- this pass detects ambiguity; it does not resolve it,
- it should focus on critical ambiguities, not trivial details,
- it should explicitly ignore missing details that do not materially change design.

Persist the ambiguity list.

---

## Stage 3 — Grill interview

This stage must behave like the best parts of `grill-me`.

Rules:
- ask one question at a time,
- depth-first, not shallow batching,
- ask only if the answer is not already recoverable from code or prior answers,
- prefer the most consequential unresolved question,
- whenever possible, present options and recommend one with reasoning,
- use the interactive question tool rather than expecting the user to read logs.

The interview ends only when:
- critical ambiguities are resolved,
- the product and technical intent are jointly understood,
- scope boundaries are clear enough to lock a contract.

---

## Stage 4 — Scope contract generation

After the grill interview, generate `scope-definition.md`.

It must include these sections:

### MUST
Items that are mandatory. The task is incomplete if any are missing.

### ALLOWED
Changes the agent may make without further approval because they are clearly incidental to MUST items.
Examples:
- private helpers inside a MUST component,
- local refactors that do not change public behavior,
- minor wiring necessary to complete MUST.

### DEFER
Gray-area decisions that can be implemented conservatively during execution and then brought back to the user interactively afterward.

### FORBIDDEN
Explicit no-go areas.
Examples:
- unrelated refactors,
- new features not requested,
- touching protected modules,
- opportunistic UX redesign.

### IRREVERSIBLE
Actions that always require explicit user confirmation even mid-session.
Examples:
- destructive file deletion,
- schema migrations,
- changing public APIs,
- force-push style destructive operations,
- irreversible infrastructure or data actions.

### ALLOWED_PATHS
Filesystem and modules that are expected to change.

### FORBIDDEN_PATHS
Places the agent must not touch.

### DONE_CRITERIA
Concrete completion checklist.

The contract must be shown to the user and confirmed before execution.

---

## Stage 5 — Planning

After the contract is confirmed, generate `plan.md`.

Rules for the plan:
- every plan item must trace back to one or more `MUST` items,
- no plan item may exist only because it “seems cleaner” or “would be nice to have”,
- tasks should be small enough to verify independently,
- the plan should be ordered to reduce risk and surface mistakes early,
- each step should identify likely touched files and expected verification.

---

## Stage 6 — Execution with guardrails

During execution, the agent must behave quietly and predictably.

### Execution rules
- Do not keep re-opening planning discussions unless necessary.
- Follow the plan.
- Re-read the contract periodically.
- Record important decisions in `decisions.md`.
- If you encounter unresolved ambiguity that is not critical enough to hard-stop, choose the most conservative option and record it in `defer-log.md`.

### Conservative choice policy
When a choice is unclear, prefer in this order:
1. reuse over new creation,
2. local change over broad change,
3. private/internal change over public interface change,
4. simpler behavior over more ambitious behavior,
5. smaller scope over larger scope.

### Forbidden behavior
Never do the following silently:
- invent product requirements,
- add a new library without the contract allowing it,
- redesign UX outside scope,
- refactor unrelated modules,
- change public contracts because it feels cleaner,
- weaken tests so they pass.

### Irreversible behavior
When an irreversible action is required:
- stop,
- ask the user interactively,
- do not proceed without confirmation.

---

## Stage 7 — Post-session interactive resolution

At the end of execution, the user must not be asked to inspect raw logs manually.

If `defer-log.md` contains entries:
- the agent must surface them interactively one by one using the question tool,
- each item must present:
  - what uncertainty was encountered,
  - what conservative decision was taken,
  - what the alternative would be,
  - what would need to change to switch to the alternative.

Supported user outcomes:
- keep,
- change,
- revert,
- defer further.

The agent then applies any requested changes.

---

## Stage 8 — Final validation

Before declaring completion, validate:
- all `MUST` items are complete,
- no `FORBIDDEN` scope was implemented,
- no forbidden paths were modified,
- all irreversible actions were explicitly confirmed,
- the implementation still matches the intended product,
- relevant tests or checks were run when expected,
- no unresolved critical ambiguity remains hidden.

The final message should summarize:
- what was completed,
- what was deferred and how it was resolved,
- any remaining consciously unimplemented items.

---

## Required persistent files

Implement these files and keep them updated.

### `templates/scope-definition.md`
Must contain a structured template with:
- task name,
- status,
- MUST,
- ALLOWED,
- DEFER,
- FORBIDDEN,
- IRREVERSIBLE,
- ALLOWED_PATHS,
- FORBIDDEN_PATHS,
- DONE_CRITERIA.

### `templates/decisions.md`
Decision log template.
Each entry should contain:
- action title,
- classification (`MUST`, `ALLOWED`, `DEFER`, `IRREVERSIBLE-APPROVED`),
- what was done,
- why,
- alternatives considered.

### `templates/defer-log.md`
Deferred decision template.
Each entry should contain:
- identifier,
- location/context,
- unresolved uncertainty,
- conservative choice taken,
- alternative,
- rollback/change path.

### `templates/plan.md`
Implementation plan template.
Each entry should map to contract items.

### `templates/handoff-state.json`
Structured machine-readable shared state.
At minimum it must include:
- task id,
- current stage,
- contract status,
- unresolved ambiguities,
- locked decisions,
- deferred items,
- plan items,
- touched paths,
- irreversible approvals,
- validation status.

---

## Required checklist content

Create `CHECKLIST.md` as a universal engineering pre-implementation checklist.
It must be technology-agnostic and include at least these areas:

1. Understanding the task
- why this exists,
- who the user is,
- happy path,
- edge cases,
- success criteria,
- what is explicitly not part of the task.

2. Architecture
- target layer,
- pattern alignment with existing code,
- interfaces and responsibilities,
- sync vs async concerns,
- compatibility concerns,
- dependency direction.

3. Existing code
- whether similar functionality already exists,
- what should be reused,
- impact radius of touched files,
- existing tests,
- TODO/FIXME hotspots.

4. Data and state
- data models,
- validation,
- null/empty/error handling,
- schema implications,
- persistence/state ownership.

5. Edge cases and failures
- invalid input,
- empty states,
- auth/permission states where relevant,
- race conditions or concurrency,
- dependent system failures.

6. Dependencies
- whether new libraries are actually needed,
- version/conflict implications,
- whether the existing stack already solves the need.

7. Testing
- what tests are required,
- what scenarios matter,
- how to avoid weakening tests.

8. Irreversible actions
- detect them,
- define rollback or mitigation.

9. Scope boundaries
- explicit non-goals,
- no opportunistic improvements,
- no unrelated cleanup.

The checklist is not only documentation. The agent must actively use it during pre-flight.

---

## Required guard rules

Create `GUARD.md` with explicit rules for in-flight execution.

It must include:
- deny-by-default principle,
- conservative-choice policy,
- periodic contract refresh,
- irreversible action escalation,
- forbidden action behavior,
- defer behavior,
- post-session interactive resolution behavior.

Also include a simple decision matrix mapping situations to actions.

---

## Required handoff rules

Create `HANDOFFS.md` describing how subagents exchange state.

The rules must include:
- all handoffs must read from and write to persisted state,
- no agent may weaken or reinterpret the confirmed contract,
- every agent inherits the same scope and boundaries,
- ambiguities found later must either become deferred items or interactive escalations,
- validator must operate with fresh context and independently compare implementation against the contract.

---

## Required validation rules

Create `VALIDATION.md` for the validator role.

It must check:
- contract compliance,
- path compliance,
- done criteria,
- hidden scope expansion,
- irreversible approvals,
- test discipline,
- unresolved critical ambiguity.

Also require the validator to answer:
- Did we build what the user asked for?
- Did we avoid building what the user did not ask for?
- Did any design choice materially exceed the agreed product scope?

---

## Platform interaction requirements

The skill must be designed for:
- universal source directory: `.agents`
- Kilo Code entrypoint: `.kilocode/` and `.kilo/`
- Codex entrypoint: `.codex/`

Interactive questioning requirements:
- during pre-flight, use question tool / ask_followup_question / equivalent,
- during post-session, use the same mechanism for deferred items,
- never require the user to manually inspect markdown logs to continue the workflow.

---

## Quality bar

The finished skill must be stronger than a simple scope skill and more execution-safe than plain `grill-me`.

That means:
- as good as `grill-me` at discovering hidden uncertainty,
- at least as disciplined as ambiguity-detection at isolating critical ambiguities,
- broader than agent-scope-skill because it covers not only permissions but the full delivery lifecycle,
- compatible with single-agent harnesses but designed to take advantage of multi-agent orchestration when available.

---

## Final instruction

Implement all files in markdown, ready to use.
Do not leave placeholders like “TODO” or “fill later”.
Write concrete, production-usable content.

The end result should feel like a real protocol for disciplined agentic software delivery, not just a generic prompt.
