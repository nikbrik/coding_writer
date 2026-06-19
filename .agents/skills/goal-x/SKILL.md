---
name: goal-x
description: 'Goal-driven execution loop for coding agents. Use this skill when the user defines a goal with explicit acceptance criteria (build, tests, specific behavior in code). Your job is to keep working in iterations until every acceptance criterion is satisfied, verified by commands/tests/logs, and clean after the latest iterative self-review / re-review loop, or until you are hard-blocked.'
license: MIT
compatibility: kilocode, codex, pi
metadata:
  version: "0.1.0"
  author: nikita
---

# GOAL-X EXECUTION PROTOCOL

## 1. Goal source of truth

- Use a session-scoped goal file named `goal_<session-id>.md` in the project root as the
  **single source of truth** for the current task.
  - `<session-id>` must be stable for this agent session/thread and filesystem-safe.
  - Good examples: `goal_2026-06-19T12-30-00Z.md`, `goal_codex_019edc43.md`.
- If a matching session goal file exists, read it first and do **not** start coding before you understand:
  - Context of the task.
  - Acceptance criteria (what must be true when we are done).
  - Constraints and forbidden changes.
  - Allowed blast radius (what files/modules you may touch).
- If no matching session goal file exists:
  - Derive it from the user's description.
  - Create `goal_<session-id>.md` with sections: `Context`, `Acceptance criteria`, `Constraints`, `Blast radius`, `Open questions`.
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
5. **Self-review**
   - Review the changed diff/files against:
     - acceptance criteria,
     - constraints / forbidden changes,
     - regression risk,
     - edge cases,
     - missing or weak evidence.
   - Emit findings as `severity`, `location`, `problem`, `fix`.
   - A valid finding must have a concrete fix or a clear reason it is a blocker.
6. **Convert findings**
   - If any actionable finding exists, treat it as a failing checklist item.
   - Fix findings in severity / impact order through the same goal loop.
   - Re-run verification after each fix batch.
   - Re-review until the latest review has no unresolved findings.
   - If a finding is out of scope, contradicts constraints, or cannot be fixed locally,
     report it as a blocker or explicit deferral; do not silently count it as clean.
7. **Update status**
   - Mark checklist items as done/failed/blocked based on evidence.
   - Append a short note to the session goal file (or a dedicated log section) describing:
     - What you changed.
     - What commands you ran.
     - What passed/failed.
     - Latest self-review result: `no findings` or unresolved findings/blocker.

Repeat this loop until all checklist items are DONE or a hard blocker is reached.

## 4. Stopping conditions

You may consider the goal complete **only if**:

- Every acceptance criterion in the goal file is satisfied.
- There is objective evidence:
  - successful command outputs,
  - passing tests,
  - required logic present in the codebase.
- The most recent self-review after the final verification produced no unresolved findings.
- You can point the user to:
  - which files were changed,
  - how to run the same verification locally.

If any acceptance criterion is not satisfied, unverified, or the latest self-review has
unresolved findings, you must continue the loop.

If you hit a **hard blocker** (missing secrets, offline dependency, unknown command, broken environment):

- Stop the loop.
- Produce a `BLOCKED` report in the answer and, if possible, append it to the session goal file:
  - What you tried.
  - Exact failing commands and outputs.
  - What you need from the user to proceed.
- Keep the session goal file while blocked so the next session can resume with evidence.

When the goal is complete:

- Delete the session goal file before the final response.
- Do not commit or leave completed `goal_<session-id>.md` files in the repository.
- If deletion fails, report the path and reason in the final response.

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
- Include the final self-review result: `no findings` or the unresolved
  findings/blocker that prevents DONE.

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
  1. Create or open `goal_<session-id>.md` in the project root.
  2. Fill `Context`, `Acceptance criteria`, `Constraints`, `Blast radius`.
  3. Select agent `goal-runner` (or invoke this skill) and describe the goal.

Your motto: **"No DONE without evidence."**
