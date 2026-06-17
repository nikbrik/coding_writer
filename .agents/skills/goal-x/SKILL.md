---
name: goal-x
description: >
  Goal-driven execution loop for coding agents. Use this skill when the user defines
  a goal with explicit acceptance criteria (build, tests, specific behavior in code).
  Your job is to keep working in iterations until every acceptance criterion is
  satisfied and verified by commands/tests/logs, or until you are hard-blocked.
license: MIT
compatibility: kilocode, codex, pi
metadata:
  version: "0.1.0"
  author: nikita
---

# GOAL-X EXECUTION PROTOCOL

## 1. Goal source of truth

- Treat `goal.md` / `goal.mdc` in the project root or `.kilo/` as the **single source of truth**
  for the current task.
- If the goal file exists, read it first and do **not** start coding before you understand:
  - Context of the task.
  - Acceptance criteria (what must be true when we are done).
  - Constraints and forbidden changes.
  - Allowed blast radius (what files/modules you may touch).
- If no goal file exists:
  - Derive it from the user's description.
  - Create `goal.md` with sections: `Context`, `Acceptance criteria`, `Constraints`, `Blast radius`, `Open questions`.
  - Write acceptance criteria in a way that can be objectively checked (commands, tests, log messages, code conditions).

## 2. Acceptance criteria and checklist

- Convert acceptance criteria into a concrete checklist.
- Each checklist item must be:
  - Binary (done / not done).
  - Verifiable by a command, test, or clear inspection.
- Example items:
  - `go test ./...` passes.
  - `go build ./...` succeeds.
  - New feature flow X works as described in `docs/feature-X.md`.
  - Log `E/MyFeature` is no longer emitted during the scenario.

Always keep this checklist in mind and update it after each iteration.

## 3. Execution loop

Operate in a strict loop:

1. **Assess**
   - Re-evaluate which checklist items are failing or unknown.
   - Read relevant files and logs only as needed.
2. **Plan**
   - Propose the smallest next change that moves at least one failing item towards DONE.
   - Prefer incremental, low-risk changes over giant refactors.
3. **Act**
   - Apply the planned changes in code/config/docs.
   - Keep diffs minimal and focused on the current checklist item.
4. **Verify**
   - Run the minimal verification commands that can prove progress:
     - build commands (e.g. `go build ./...`),
     - test suites or targeted tests,
     - linters / static analyzers,
     - custom scripts mentioned in the goal.
   - Inspect outputs and logs.
5. **Update status**
   - Mark checklist items as done/failed/blocked based on evidence.
   - Append a short note to `goal.md` (or a dedicated log section) describing:
     - What you changed.
     - What commands you ran.
     - What passed/failed.

Repeat this loop until all checklist items are DONE or a hard blocker is reached.

## 4. Stopping conditions

You may consider the goal complete **only if**:

- Every acceptance criterion in the goal file is satisfied.
- There is objective evidence:
  - successful command outputs,
  - passing tests,
  - required logic present in the codebase.
- You can point the user to:
  - which files were changed,
  - how to run the same verification locally.

If any acceptance criterion is not satisfied or unverified, you must continue the loop.

If you hit a **hard blocker** (missing secrets, offline dependency, unknown command, broken environment):

- Stop the loop.
- Produce a `BLOCKED` report in the answer and, if possible, append it to `goal.md`:
  - What you tried.
  - Exact failing commands and outputs.
  - What you need from the user to proceed.

## 5. Communication style

- Minimize noise. Focus on:
  - current checklist status,
  - what you will do next,
  - what you just changed,
  - what the verification results are.
- Use sections:
  - `PLAN`
  - `ACTIONS`
  - `VERIFICATION`
  - `STATUS`
- Never claim "done" without explicitly restating each acceptance criterion and
  confirming how it was verified.

## 6. Safety and constraints

- Always respect project-level rules from `AGENTS.md`, `.kilo/*.md`, `.agents/skills/*`.
- Do **not** expand blast radius beyond what is allowed in the goal file.
- Do **not** introduce heavy new dependencies, tools, or services unless:
  - the goal explicitly allows it, or
  - there is no other reasonable solution and you clearly justify it.
- Prefer changes that:
  - integrate into existing architecture,
  - follow project coding standards,
  - keep behavior predictable and testable.

## 7. Compatibility and usage

- This skill follows the `.agents/skills` spec and can be loaded by any agent
  that supports skills (KiloCode, Codex, Pi, etc.).
- In KiloCode you can also use the dedicated `goal-runner` agent from `.kilo/agent/goal-runner.md`.
- To start a goal-driven session:
  1. Create or open `goal.md` in the project root.
  2. Fill `Context`, `Acceptance criteria`, `Constraints`, `Blast radius`.
  3. Select agent `goal-runner` (or invoke this skill) and describe the goal.

Your motto: **"No DONE without evidence."**
