# Goal: LeetCode-like Day 11-14 Manual Demo Cases

## Context

User wants the mandatory Day 11-14 manual demo cases to look like real CLI-agent usage, not mechanical feature probes. Each day should make the assistant solve a small LeetCode-style task to completion while still proving that day's product requirement.

## Acceptance Criteria

- `docs/manual-testing-day11-14.md` defines four normal-user demo scenarios for Day 11, Day 12, Day 13, and Day 14.
- Each scenario uses a small algorithm task and requires a complete solution: code, examples/edge cases, complexity, and verification path.
- Day 11 still proves memory layers and explicit memory apply.
- Day 12 still proves profile-driven personalization.
- Day 13 still proves task FSM, pause/resume, restart, validation, and done transition.
- Day 14 still proves invariant storage, prompt inclusion, conflict refusal, and safe flow.
- `docs/manual-testing-real-cli.md` references the updated algorithmic demo cases.

## Constraints

- Do not change `day11.md`, `day12.md`, or `03-memory-state-notes.md`.
- Do not pretend the CLI edits files. If verification needs executable code, document it as agent verification using a scratch package from the emitted deliverable.
- Keep demo user flow natural: no `/task start`, `/task move`, `/task step`, or long `assistant chat --once --input ... --json` as the visible demo path.

## Blast Radius

- Documentation only:
  - `docs/manual-testing-day11-14.md`
  - `docs/manual-testing-real-cli.md`
  - `goal.md`

## Open Questions

- None.

## Log

- Updated `docs/manual-testing-day11-14.md`: four canonical Day 11-14 demos now solve LeetCode-style tasks (`Two Sum`, `Valid Parentheses`, `Merge Sorted Arrays`, `Best Time to Buy and Sell Stock`) and require code/tests/complexity plus trusted verification.
- Updated `docs/manual-testing-real-cli.md`: first four demo case summaries now point to the new algorithmic scenarios.
- Verification:
  - `git diff --check` passed.
  - `go test ./tests -run 'TestDay11|TestDay12|TestDay13|TestDay14'` passed.
  - Live OpenRouter manual run passed under `.assistant/manual-runs/leetcode-20260619-122306`.
  - `go test ./manual_scratch/...` passed.
  - `go test ./...` passed.
- Live run notes:
  - Day 11 found a bad duplicate-case expectation in model-generated Two Sum tests; fixed by validating any correct pair.
  - Day 13 found a nil-vs-empty slice issue in the mutation test harness; fixed by copying with `append([]int{}, src...)`.
  - Day 14 recovery prompt needed an explicit "do not claim tests ran" instruction; docs were updated to the passing wording.
- Self-review: no unresolved findings. Supplemental regression matrix still contains older `MemoryManager`/`MVP` cases intentionally; canonical Day 11-14 demos no longer use them.
