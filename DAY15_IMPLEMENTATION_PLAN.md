# Day 15 Implementation Plan: controlled lifecycle + microtask agents

Дата анализа: 2026-06-19.

Статус на 2026-06-19: реализовано. Этот файл теперь historical planning/reference material. Актуальный продуктовый контракт живёт в `docs/prd.md`, `docs/frd.md`, `docs/architect.md`, а единственный canonical live/manual proof описан в `docs/manual-testing-demo.md`.

Финальный итог реализации:

- primary Day 15 flow is chat-first and human-readable by default;
- live proof uses OpenRouter model `google/gemini-3.1-flash-lite`;
- no primary Day 15 step requires `/task move`, `/task step`, `/task expect`, direct storage edits, `--verify`, raw JSON inspection, fake provider, or a user-supplied exact test command;
- application-owned gates control task state, auto verification, validation status and `done`;
- audit must show prompt improvement, planning swarm specialists, orchestrator, executor, reviewer and lifecycle transitions.

Исходная цель этого файла: дать следующей, более слабой LLM, подробный и безопасный план реализации Day 15. После реализации использовать файл только как audit trail: при конфликте доверять текущим docs, tests и коду.

## 1. Acceptance Day 15

Источник: `day15.md`.

Нужно получить assistant с контролируемым lifecycle задачи:

- у задачи есть допустимые состояния;
- есть разрешенные переходы между ними;
- assistant не может перепрыгнуть этап;
- нельзя делать реализацию до утвержденного плана;
- нельзя делать финал без validation;
- попытки недопустимых переходов отклоняются;
- после pause/resume продолжение корректное;
- желательно подключить "рой агентов": отдельная микротаска выполняется отдельным LLM-вызовом со своим системным prompt и своим контекстом, приложение оркестрирует весь процесс.

## 2. Scope и ограничения

- Days 11-14 уже сданы. Не ломать их acceptance flow без явной необходимости.
- Не переписывать память, профили, invariants, OpenRouter provider и базовый CLI.
- Не менять `day11.md`, `day12.md` и `03-memory-state-notes.md`, если пользователь отдельно не попросит. Это локальное repo rule.
- Не превращать debug slash commands в required happy path. Acceptance demo должен идти через обычный `assistant chat` и естественные фразы пользователя.
- Не добавлять новые keyword/regex semantic validators для product real mode. Смысловые решения должны идти через `SemanticValidator` и structured JSON. Trigger-word fallback не должен решать intent/readiness/acceptance.
- Текущий MVP не редактирует файлы сам. Day 15 можно закрыть через read-only code deliverables + trusted verification, как в Day 11-14. Если добавлять tool writes, это отдельная P1 задача.

## 3. Baseline check

Проверено перед планом:

- `ast-index rebuild` потребовал escalation, потому что index cache вне workspace. Это известный repo workaround.
- `go test ./...` в sandbox упал только на `internal/providers` из-за `httptest` localhost bind.
- `GOCACHE=/private/tmp/coding_writer_gocache go test ./...` вне sandbox прошел успешно.

Baseline зеленый. Day 15 можно реализовывать поверх текущей архитектуры.

## 4. Что уже реализовано

Не дублировать эти части.

### 4.1. Task state и FSM

Файлы:

- `internal/app/models.go`
- `internal/tasks/state_machine.go`
- `internal/tasks/manager.go`

Уже есть:

- `TaskStage`: `planning`, `execution`, `validation`, `done`;
- `TaskStatus`: `active`, `paused`;
- `ExpectedAction`: `user_input`, `llm_response`, `user_confirmation`, `none`;
- `AllowedTransitions`:
  - `planning -> execution`;
  - `execution -> validation`;
  - `execution -> planning`;
  - `validation -> execution`;
  - `validation -> done`;
  - `done` terminal;
- `ValidateState` проверяет stage/status/expected_action и terminal invariants;
- `Manager.Move` запрещает переходы вне FSM и paused mutation;
- `Manager.Pause` / `Manager.Resume` сохраняют stage/current_step/expected_action;
- optimistic lost-update guard через snapshot hash;
- forbidden transition не мутирует `current.json`.

### 4.2. Stage policies

Файлы:

- `internal/process/stage_policy.go`
- `internal/process/stage_policy_registry.go`
- `internal/process/action_kind.go`
- `internal/process/stage_prompt_factory.go`

Уже есть:

- trusted role per stage;
- allowed/forbidden actions per stage;
- stage-specific output schema;
- planning запрещает execution/review/done actions;
- execution запрещает planning/review/done actions;
- validation запрещает planning/execution/done summary;
- done запрещает mutation/transition actions.

### 4.3. Structured parser и validators

Файлы:

- `internal/process/response_parser.go`
- `internal/process/validator_runner.go`
- `internal/process/planning_validator.go`
- `internal/process/execution_validator.go`
- `internal/process/validation_validator.go`
- `internal/process/done_validator.go`
- `internal/process/semantic_validator.go`

Уже есть:

- strict JSON parser for non-answer actions;
- strict stage mismatch rejection;
- structural validators;
- semantic out-of-band validator for real provider mode;
- retry loop for invalid model output;
- trusted evidence helpers;
- validation-to-done checks against findings, missing evidence and trusted evidence.

### 4.4. TransitionGate

Файл:

- `internal/process/transition_gate.go`

Уже есть:

- `TransitionGate.Check`;
- `TransitionGate.Apply`;
- preconditions for:
  - planning output readiness;
  - planning requires auto approval or pending approval;
  - execution ready_for_validation requires trusted evidence, changed artifacts and verification;
  - execution can request replanning;
  - validation needs actionable findings to go back to execution;
  - validation ready_for_done requires no missing evidence, no blocker/high findings, passed checks and criteria-matched trusted evidence;
- stale task state rejection before transition apply.

### 4.5. ProcessController

Файл:

- `internal/process/controller.go`

Уже есть:

- resolve current task/stage/action;
- preflight local hard gates;
- invariant input/output validation;
- semantic intent validation in real mode;
- provider call;
- parse + structural/semantic validation;
- transition candidate through `TransitionGate` for parsed model output;
- pending planning proposal and explicit approval;
- process audit events;
- short memory + classifier proposal after accepted output.

### 4.6. Tests already covering old days and lifecycle pieces

Already covered:

- `internal/tasks/manager_test.go`
  - pause/resume;
  - forbidden transition no mutation;
  - done terminal;
  - lost update guard;
  - invalid persisted state rejection.
- `internal/process/transition_gate_test.go`
  - planning requires approval;
  - stage mismatch;
  - stale state;
  - execution -> validation;
  - validation -> done;
  - evidence must match criteria.
- `tests/day_acceptance_test.go`
  - Day 11 memory;
  - Day 12 profiles;
  - Day 13 pause/resume;
  - Day 14 invariants.
- `tests/process_acceptance_test.go`
  - paused task no provider call;
  - validation prompt role;
  - invalid output retry;
  - done through transition gate;
  - rejected output no memory persistence.

## 5. What is not implemented yet

These are the real Day 15 gaps.

### Gap A. No single lifecycle service for every transition source

There is `TransitionGate`, but not every transition goes through it.

Current bypasses:

- `ProcessController` uses direct `Tasks.Move(preflight.AutoStage)` for execution -> planning auto-stage.
- `ProcessController` uses direct `Tasks.Move(app.StageValidation)` when user says ready for validation.
- `ProcessController` uses direct `Tasks.Move(app.StageDone)` when user says verify/finish and trusted evidence matches.
- CLI commands `assistant task move <stage>` and `/task move <stage>` call `Tasks.Move` directly.

Problem:

- FSM prevents impossible stage edges, but does not enforce richer Day 15 preconditions everywhere.
- A debug/user command can move `planning -> execution` without approved plan.
- A debug/user command can move `validation -> done` without accepted validation output or trusted evidence.
- Audit is less consistent for direct CLI transitions.

### Gap B. "Cannot implement before approved plan" is mostly implemented, but not universal

Implemented for normal chat:

- planning prompt forbids implementation;
- `ActionExecutePlanStep` is forbidden in planning;
- planning ready output becomes pending proposal unless approved;
- user approval then moves to execution.

Missing:

- central product-level invariant: execution may start only from an approved plan with non-empty plan + acceptance criteria and no open questions.
- CLI `task move execution` can bypass the approved-plan concept.
- `Manager.Move(planning -> execution)` has no plan/approval precondition. It only checks FSM edge.

### Gap C. "Cannot finish without validation" is mostly implemented, but not universal

Implemented for normal model-output path:

- `TransitionGate` allows `validation -> done` only after validation verdict `ready_for_done` and trusted evidence checks.

Missing:

- direct CLI `task move done` from validation bypasses validation-output preconditions.
- direct controller user signal can call `Tasks.Move(done)` without storing a validation decision record first.
- `TaskState.ValidationStatus` exists but is not used as a hard lifecycle precondition.

### Gap D. Pause/resume is implemented, but not tied to microtask orchestration

Implemented:

- paused task blocks process continuation;
- state survives restart.

Missing for Day 15:

- paused microtask/orchestrator state is not explicit;
- there is no current microtask id/agent role to resume;
- no test proves pending planning proposal survives pause/resume and cannot be approved while paused;
- no test proves a paused orchestrator does not start new agent calls.

### Gap E. No microtask/swarm orchestration

Code search found no product implementation for microtask/swarm/subagent orchestration.

Current implementation has stage-specific prompts and recursive auto-execution in `continueAfterPlanningApproval`, but it is still one `ProcessController` flow with the same general provider interface.

Missing:

- separate agent role abstraction;
- separate per-microtask system prompt;
- isolated context per agent call;
- orchestrator-owned sequence of microtasks;
- audit trail per agent/microtask;
- tests proving planner/executor/reviewer are separate LLM calls with separate prompts.

Additional Day 15 requirement:

- planning must use 5 independent specialist agents, not one planner;
- the 5 specialists research/validate the code task from different angles;
- specialists exchange findings through an orchestrator agent, not by mutating shared state directly;
- specialists must correct/improve the draft plan and then validate the improved plan;
- the refinement loop must be bounded and only critical/high findings block approval;
- final execution can start only after the orchestrator produces an approved final plan and the user approval intent is validated.

### Gap F. No automatic LLM prompt-improvement stage

User added a new requirement: every normal user chat prompt must be improved through an LLM stage before the main task flow. The product currently routes raw input directly into preflight/action resolution/provider prompt building.

Missing:

- prompt-improvement agent before normal chat flow;
- strict schema for improved prompt output;
- audit that stores original prompt, improved prompt and improvement rationale;
- guard that improvement preserves meaning and does not add requirements;
- tests proving prompt improvement runs before planning/execution/validation;
- docs explaining that this happens automatically without a separate user approval step.

## 6. Target Day 15 contract

### 6.1. Lifecycle stages

Keep current stages:

```text
planning -> execution -> validation -> done
execution -> planning
validation -> execution
```

No new public stage is required for Day 15.

Add internal process phases before and inside `planning`:

```text
raw_user_input
  -> prompt_improvement
  -> action/intent resolution
  -> planning_swarm_research
  -> plan_refinement
  -> plan_validation
  -> pending_user_approval
  -> execution
```

These are not new `TaskStage` values. They are orchestrator phases stored in audit/orchestrator state. Public task state stays `planning` until the final plan is produced and user approval is accepted.

### 6.2. Transition sources

Every product transition must be represented as a `TransitionRequest` with a source:

- `model_output`: accepted structured LLM response proposes transition;
- `user_approval`: user approves pending planning proposal;
- `trusted_verification`: CLI passed trusted evidence through `--verify`;
- `system_replan`: controller returns execution to planning because requirements changed;
- `recovery_debug`: explicit debug/recovery command, never happy path.

### 6.3. Transition preconditions

Use this table as implementation source of truth.

| From | To | Required signal | Preconditions |
|---|---|---|---|
| planning | execution | `approve_planning` or planning output readiness | task active; prompt has passed improvement stage; final plan was produced by planning swarm orchestrator; no critical/high plan findings remain after bounded refinement; pending/current plan approved by user approval intent validator; plan non-empty; acceptance criteria non-empty; open questions empty; no implementation has run before approval |
| execution | validation | `ready_for_validation` | task active; at least one accepted execution deliverable exists, or trusted evidence is supplied; blockers empty; no direct jump to done |
| execution | planning | `planning_required` | task active; reason is recorded; current execution output says requirements/blocker need replanning |
| validation | execution | `needs_execution_fixes` | task active; validation findings contain at least one actionable finding with problem + fix |
| validation | done | `ready_for_done` | task active; accepted validation record exists; missing evidence empty; no blocker/high findings; passed checks non-empty; trusted evidence exists and satisfies acceptance criteria |
| done | any | none | forbidden |

Do not rely on user text alone for semantic signal. Use `SemanticValidator.ResolveIntent`; local keyword helpers must not decide intent/readiness/acceptance.

### 6.4. Prompt-improvement contract

Every normal chat message must go through a prompt-improvement LLM call before main processing.

Rules:

- slash commands do not go through prompt improvement;
- secret/input hard gates run before prompt improvement so secrets are not sent to a provider;
- the prompt improver returns strict JSON:

```json
{
  "improved_prompt": "string",
  "preserved_intent": true,
  "added_requirements": [],
  "removed_requirements": [],
  "clarifications": [],
  "rationale": "string"
}
```

- `preserved_intent` must be true;
- `added_requirements` and `removed_requirements` must be empty unless the item is only a wording clarification;
- both original and improved prompts are kept for audit;
- main action resolution uses the improved prompt;
- memory classifier receives both original and improved prompt context so it does not save artificial wording as a user preference;
- invariant checks should validate both original and improved prompt, because the improved prompt becomes provider-visible task input.

Failure behavior:

- if prompt improvement output is invalid in live/product mode, reject the exchange with `prompt_improvement_failed`;
- fake/offline tests may use deterministic fallback, but acceptance should prove the improvement call happened.

### 6.5. Planning swarm contract

The planning stage must be multi-agent.

Agents:

1. `requirements_specialist`: validates user goal, acceptance criteria and ambiguity.
2. `code_research_specialist`: researches relevant code surface and integration points.
3. `architecture_specialist`: validates module boundaries, lifecycle/state impacts and existing patterns.
4. `test_validation_specialist`: designs tests, manual checks and trusted evidence path.
5. `risk_regression_specialist`: finds regressions, privacy/security risks and Day 11-14 compatibility risks.

There is also one `planning_orchestrator` agent.

Required flow:

```text
improved prompt
  -> draft plan
  -> 5 specialists independently review/research
  -> planning_orchestrator merges opinions
  -> improved plan
  -> 5 specialists revalidate improved plan
  -> if only medium/low/no findings: final plan
  -> if critical/high findings and rounds remain: improve again
  -> if critical/high findings remain after max rounds: stay planning and ask user/return blockers
```

Bounded loop:

- `MaxPlanRefinementRounds = 2` by default;
- only `critical` and `high` findings block final plan;
- `medium` and `low` findings become plan notes or risks, not blockers;
- no infinite self-review loops.

User approval:

- after final plan, task remains `planning` with pending final plan;
- any user chat answer can be an approval candidate;
- approval is validated by the orchestrator/LLM intent validator using original user answer + final plan summary;
- execution starts only if approval verdict is `approved`;
- rejection or ambiguity keeps task in `planning`.

### 6.6. User-visible behavior

If user tries to skip:

- implementation before approved plan -> assistant refuses or returns planning proposal; task stays `planning`;
- done from planning/execution -> `forbidden_transition` or `transition_precondition_failed`; no provider mutation;
- done from validation without trusted evidence -> `transition_precondition_failed`; task stays `validation`;
- continue while paused -> `task_paused`; no chat provider call.

All failed transitions must preserve `tasks/current.json` except audit append.

## 7. Proposed architecture

### 7.1. Add central lifecycle gate

Add file:

- `internal/process/lifecycle_gate.go`

New types:

```go
type TransitionSource string

const (
    TransitionSourceModelOutput         TransitionSource = "model_output"
    TransitionSourceUserApproval        TransitionSource = "user_approval"
    TransitionSourceTrustedVerification TransitionSource = "trusted_verification"
    TransitionSourceSystemReplan        TransitionSource = "system_replan"
    TransitionSourceRecoveryDebug       TransitionSource = "recovery_debug"
)

type TransitionSignal string

const (
    SignalApprovePlanning    TransitionSignal = "approve_planning"
    SignalRejectPlanning     TransitionSignal = "reject_planning"
    SignalReadyForValidation TransitionSignal = "ready_for_validation"
    SignalPlanningRequired   TransitionSignal = "planning_required"
    SignalNeedsExecutionFixes TransitionSignal = "needs_execution_fixes"
    SignalReadyForDone       TransitionSignal = "ready_for_done"
)

type LifecycleTransitionRequest struct {
    State           app.TaskState
    Source          TransitionSource
    Signal          TransitionSignal
    Parsed          *ParsedResponse
    Target          app.TaskStage
    TrustedEvidence []string
    Reason          string
    AutoApprovePlanning bool
    RecoveryDebug   bool
}

type LifecycleGate struct {
    Tasks *tasks.Manager
}
```

Responsibilities:

- convert source/signal/parsed response into target stage;
- enforce FSM edge;
- enforce Day 15 preconditions;
- call `tasks.Manager` mutation methods only after all checks pass;
- return `TransitionResult`;
- never write partial state on failure.

`TransitionGate` can either:

- be refactored into `LifecycleGate`; or
- become a thin adapter that builds `LifecycleTransitionRequest` for model output.

Recommended for safer migration:

1. Keep existing `TransitionGate` tests green.
2. Introduce `LifecycleGate`.
3. Make `TransitionGate.Check/Apply` delegate to `LifecycleGate` for parsed model output.
4. Route controller/CLI direct transitions to `LifecycleGate`.

### 7.2. Persist enough lifecycle evidence in TaskState

Current `TaskState.ValidationStatus` is not enough.

Add minimal fields to `app.TaskState`:

```go
ApprovedPlanID          string   `json:"approved_plan_id,omitempty"`
LastAcceptedExecutionID string   `json:"last_accepted_execution_id,omitempty"`
LastValidationID        string   `json:"last_validation_id,omitempty"`
ValidationStatus        string   `json:"validation_status,omitempty"`
ValidationEvidence      []string `json:"validation_evidence,omitempty"`
```

Alternative if keeping `TaskState` small:

- store execution/validation records in new JSONL files and keep only ids in `TaskState`.

Recommended Day 15 implementation:

- keep ids + status in `TaskState`;
- put full records in audit/process logs to avoid bloating `current.json`.

Expected values:

- `ValidationStatus=""` before validation;
- `ValidationStatus="blocked_missing_evidence"`;
- `ValidationStatus="needs_execution_fixes"`;
- `ValidationStatus="ready_for_done"`.

Update `ValidateState`:

- if `StageDone`, require `ValidationStatus=="ready_for_done"`;
- if `StageDone`, require `LastValidationID != ""`;
- if `StageExecution`, `ValidationStatus` may be empty or `needs_execution_fixes`;
- do not reject old stored tasks too aggressively unless migration/backfill is handled. If this breaks existing tests, implement a compatibility migration on read.

### 7.3. Add explicit task mutation methods

Add or update methods in `internal/tasks/manager.go`:

- `ApprovePlanningFromPending()`;
- `ApprovePlanningFromCurrent()`;
- `RecordAcceptedExecution(parsed ExecutionOutput, trustedEvidence []string)`;
- `RecordAcceptedValidation(parsed ValidationOutput, trustedEvidence []string)`;
- `MoveWithLifecycle(req LifecycleMove)` or keep low-level `Move` internal and use lifecycle methods in process/CLI.

Important:

- Do not make `Manager.Move` responsible for semantic validation. It should stay low-level if tests depend on it.
- Product code should stop calling `Manager.Move` directly for lifecycle transitions.
- CLI recovery command may call a separate explicit recovery method with `--force` and audit.

### 7.4. Route ProcessController through LifecycleGate

Modify `internal/process/controller.go`.

Replace these direct moves:

- `c.Tasks.Move(preflight.AutoStage)`;
- `c.Tasks.ApprovePendingPlanningProposal()`;
- `c.Tasks.MoveWithPlanningOutput(...)` from current planning approval;
- `c.Tasks.Move(app.StageValidation)`;
- `c.Tasks.Move(app.StageDone)`;
- transition apply from `TransitionGate` if refactored.

All should call lifecycle gate with source/signal:

- pending planning approval -> `SourceUserApproval`, `SignalApprovePlanning`;
- current planning approval -> `SourceUserApproval`, `SignalApprovePlanning`;
- execution ready -> `SourceTrustedVerification` or `SourceModelOutput`, `SignalReadyForValidation`;
- validation done -> `SourceTrustedVerification`, `SignalReadyForDone`;
- execution replanning -> `SourceSystemReplan`, `SignalPlanningRequired`.

Keep behavior:

- if planning proposal is ready but not approved, save pending proposal and stay planning;
- after approval, `continueAfterPlanningApproval` can still auto-run execution microtasks;
- if transition fails, save audit rejection and return error without memory persistence.

### 7.5. Lock down CLI transition bypasses

Modify:

- `internal/cli/root.go`

For `assistant task move <stage>` and `/task move <stage>`:

Option A, safest for product:

- keep command but mark as recovery/debug;
- require `--force` for transitions that bypass Day 15 preconditions;
- without `--force`, route through `LifecycleGate` and return clear precondition errors.

Option B, less breaking:

- keep command behavior for tests, but document it as recovery-only and exclude from acceptance.
- Still add audit event for every move.

Recommended:

- add `--force` to top-level `task move`;
- slash `/task move` should not force by default;
- update docs: `/task move` is not acceptance happy path.

### 7.6. Add PromptImprover

Add files:

- `internal/process/prompt_improver.go`
- `internal/process/prompt_improver_test.go`

New types:

```go
type PromptImprovementInput struct {
    SessionID string
    Original  string
    Task      *app.TaskState
    Profile   app.UserProfile
}

type PromptImprovementResult struct {
    Original            string
    Improved            string
    PreservedIntent     bool
    AddedRequirements   []string
    RemovedRequirements []string
    Clarifications      []string
    Rationale           string
    Model               string
}
```

Behavior:

- run before action routing for every normal chat message;
- skip slash commands and local CLI commands;
- run secret scan before provider call;
- call provider with `PurposeValidator` or existing JSON chat mode;
- require strict JSON output;
- reject if intent is not preserved;
- record `prompt_improvement_call` and `prompt_improvement_accepted` audit events;
- pass the improved prompt into `resolveProcessState`, invariant checks, prompt builder and process controller;
- keep the original prompt in audit and memory-classifier context.

Important:

- prompt improvement must not add acceptance criteria, files, constraints, technology choices or completion claims that the user did not express;
- if the improved prompt conflicts with original prompt, reject instead of silently changing task semantics;
- the original prompt remains the source for "what the user said"; the improved prompt is only the operational version.

### 7.7. Add planning swarm orchestrator

Add files:

- `internal/process/planning_swarm.go`
- `internal/process/planning_swarm_test.go`
- optionally `internal/process/plan_findings.go`

New types:

```go
type PlanFindingSeverity string

const (
    PlanFindingCritical PlanFindingSeverity = "critical"
    PlanFindingHigh     PlanFindingSeverity = "high"
    PlanFindingMedium   PlanFindingSeverity = "medium"
    PlanFindingLow      PlanFindingSeverity = "low"
)

type PlanningSpecialistRole string

const (
    SpecialistRequirements PlanningSpecialistRole = "requirements_specialist"
    SpecialistCodeResearch PlanningSpecialistRole = "code_research_specialist"
    SpecialistArchitecture PlanningSpecialistRole = "architecture_specialist"
    SpecialistTestValidation PlanningSpecialistRole = "test_validation_specialist"
    SpecialistRiskRegression PlanningSpecialistRole = "risk_regression_specialist"
)

type PlanFinding struct {
    Severity PlanFindingSeverity `json:"severity"`
    Area     string              `json:"area"`
    Problem  string              `json:"problem"`
    Fix      string              `json:"fix"`
    Evidence string              `json:"evidence,omitempty"`
}

type SpecialistReview struct {
    Role     PlanningSpecialistRole `json:"role"`
    Summary  string                 `json:"summary"`
    Findings []PlanFinding          `json:"findings"`
    ProposedPlan []string           `json:"proposed_plan,omitempty"`
    ProposedCriteria []string       `json:"proposed_acceptance_criteria,omitempty"`
}

type PlanningSwarmResult struct {
    FinalSummary string
    FinalPlan []string
    FinalAcceptanceCriteria []string
    OpenQuestions []string
    Findings []PlanFinding
    Rounds int
}
```

Required flow:

1. Build initial draft plan from improved prompt.
2. Run 5 specialist LLM calls independently with role-specific prompts and isolated context.
3. Send all specialist reviews to `planning_orchestrator`.
4. Orchestrator produces improved plan + finding resolution notes.
5. Run the 5 specialists again against improved plan.
6. If no critical/high findings remain, save final plan as pending planning proposal.
7. If critical/high findings remain and `round < MaxPlanRefinementRounds`, repeat improvement once.
8. If critical/high findings remain after max rounds, stay in planning and return blockers/open questions.

Specialist prompt boundaries:

- requirements specialist cannot propose implementation code;
- code research specialist can identify files/symbols/risks, but cannot mutate files;
- architecture specialist checks existing patterns and module boundaries;
- test validation specialist must produce tests/manual checks/trusted evidence path;
- risk regression specialist must explicitly protect Day 11-14 behavior.

Orchestrator rules:

- specialists do not write task state directly;
- orchestrator is the only agent that merges opinions;
- orchestrator must preserve the user goal and improved prompt;
- only critical/high findings block final plan;
- medium/low findings are included as notes/risks;
- no unbounded debate.

User approval validation:

- add `PlanningApprovalValidator` or reuse `SemanticValidator.ResolveIntent` with a stricter schema:

```json
{
  "verdict": "approved | rejected | ambiguous",
  "confidence": 0.0,
  "reason": "string"
}
```

- any normal user answer after pending final plan is sent through this validator;
- only `approved` with sufficient confidence moves to execution;
- `rejected` clears or revises pending plan;
- `ambiguous` asks user for explicit confirmation and stays planning.

### 7.8. Add microtask agent orchestration

Add files:

- `internal/process/agent_role.go`
- `internal/process/microtask.go`
- `internal/process/agent_runner.go`
- `internal/process/microtask_orchestrator.go`

Minimal types:

```go
type AgentRole string

const (
    AgentRolePromptImprover AgentRole = "prompt_improver"
    AgentRolePlanOrchestrator AgentRole = "planning_orchestrator"
    AgentRoleRequirementsSpecialist AgentRole = "requirements_specialist"
    AgentRoleCodeResearchSpecialist AgentRole = "code_research_specialist"
    AgentRoleArchitectureSpecialist AgentRole = "architecture_specialist"
    AgentRoleTestValidationSpecialist AgentRole = "test_validation_specialist"
    AgentRoleRiskRegressionSpecialist AgentRole = "risk_regression_specialist"
    AgentRoleExecutor  AgentRole = "executor"
    AgentRoleReviewer  AgentRole = "reviewer"
    AgentRoleFinalizer AgentRole = "finalizer"
)

type Microtask struct {
    ID         string
    Role       AgentRole
    Stage      app.TaskStage
    ActionKind ActionKind
    Instruction string
    PlanItem    string
}

type AgentRunInput struct {
    SessionID string
    Task      app.TaskState
    Microtask Microtask
    Profile   app.UserProfile
    Memory    app.MemoryBundle
    TrustedEvidence []string
}

type AgentRunResult struct {
    MicrotaskID string
    Role        AgentRole
    Raw         string
    Parsed      ParsedResponse
    Model       string
}
```

Behavior:

- prompt improver agent rewrites normal user input before the main flow;
- planning orchestrator and 5 specialists own planning-only research/refinement and return final pending `PlanningOutput`;
- executor agent gets one plan item/current step and returns `ExecutionOutput`;
- reviewer agent gets deliverables + trusted evidence and returns `ValidationOutput`;
- finalizer agent can produce `DoneOutput` only after lifecycle gate already moved to done, or it can be skipped for Day 15.

Important:

- One microtask = one provider call.
- Each microtask must have its own trusted system prompt.
- Do not pass full short history blindly into every agent. Pass only:
  - current task state;
  - relevant work memory;
  - current microtask instruction;
  - accepted artifacts summary;
  - trusted evidence if needed.
- The orchestrator, not the LLM, decides next microtask and transition.

### 7.9. Integrate microtask orchestrator carefully

Do not replace the whole `ProcessController` at once.

Recommended incremental integration:

1. Keep `ProcessController.RunExchange` as public entrypoint.
2. Add `PromptImprover`, `PlanningSwarmOrchestrator` and `MicrotaskOrchestrator` fields to `ProcessController`.
3. Use `PromptImprover` before action routing for normal chat.
4. Use `PlanningSwarmOrchestrator` while task is in `planning`.
5. Use execution orchestrator only after planning approval:
   - current `continueAfterPlanningApproval` recursively calls `RunExchange`;
   - replace that inner loop with `MicrotaskOrchestrator.RunExecutionPlan`.
6. Later, use orchestrator for validation as well.

For Day 15 acceptance, enough proof:

- first chat call is improved by prompt improver;
- planning uses 5 specialists plus planning orchestrator;
- specialists produce findings and corrected plan;
- user approval is LLM-validated by orchestrator;
- approval transitions to execution only after final plan;
- execution plan items are handled by separate executor-agent calls;
- validation is handled by reviewer-agent call;
- audit shows distinct `agent_role` and `microtask_id` for each call.

### 7.10. Extend audit

Current audit file:

- `process_audit.jsonl`

Current struct:

- `internal/process/audit_event.go`

Add fields:

```go
TransitionSource string `json:"transition_source,omitempty"`
TransitionSignal string `json:"transition_signal,omitempty"`
AgentRole        string `json:"agent_role,omitempty"`
MicrotaskID      string `json:"microtask_id,omitempty"`
PreconditionID   string `json:"precondition_id,omitempty"`
EvidenceRefs     []string `json:"evidence_refs,omitempty"`
PromptOriginalHash string `json:"prompt_original_hash,omitempty"`
PromptImprovedHash string `json:"prompt_improved_hash,omitempty"`
PlanningRound    int    `json:"planning_round,omitempty"`
SpecialistRole   string `json:"specialist_role,omitempty"`
```

Use audit decisions:

- `prompt_improvement_call`;
- `prompt_improvement_accepted`;
- `transition_rejected`;
- `transitioned`;
- `agent_call`;
- `agent_rejected`;
- `agent_accepted`;
- `planning_swarm_round`;
- `planning_swarm_final`;
- keep existing decisions if changing them would break tests, but include richer metadata.

### 7.11. Fake provider support

Modify:

- `internal/providers/fake.go`

Needed for deterministic tests:

- prompt-improver response;
- five specialist responses;
- planning-orchestrator response;
- executor responses per plan item;
- reviewer response;
- validator response.

Do not add a real new network provider. Reuse existing `LLMProvider`.

Option:

- use `PurposeChat` with different prompts and existing `ChatResponses`;
- tests can assert prompts contain `Agent role: executor` / `microtask.id`.

If adding `PurposeAgent`, update all providers and tests carefully. Simpler: do not add it for Day 15.

## 8. Implementation phases

### Phase 0. Safety baseline

Do first:

```bash
GOCACHE=/private/tmp/coding_writer_gocache go test ./...
```

If sandbox blocks `httptest`, rerun outside sandbox with approval.

Do not edit docs or code before confirming baseline.

### Phase 1. LifecycleGate skeleton

Files:

- add `internal/process/lifecycle_gate.go`;
- add `internal/process/lifecycle_gate_test.go`.

Implement:

- `TransitionSource`;
- `TransitionSignal`;
- `LifecycleTransitionRequest`;
- `LifecycleGate.Check`;
- `LifecycleGate.Apply`.

Initial delegation:

- for model output, reuse existing `TransitionGate.nextStage` logic or move that logic into `LifecycleGate`.
- keep current `TransitionGate` API stable.

Tests:

- planning -> execution rejected without plan/criteria;
- planning -> execution rejected with open questions;
- planning -> execution allowed with approved pending proposal;
- execution -> done rejected;
- validation -> done rejected without validation status/trusted evidence;
- invalid transition preserves current task.

### Phase 2. Persist lifecycle evidence

Files:

- `internal/app/models.go`;
- `internal/tasks/state_machine.go`;
- `internal/tasks/manager.go`;
- tests in `internal/tasks/manager_test.go`.

Add fields or record ids for:

- approved plan;
- last accepted execution;
- last accepted validation;
- validation status.

Implement methods:

- record approved planning;
- record accepted execution;
- record accepted validation.

Tests:

- approved planning id appears after approval;
- accepted execution id appears after execution output is accepted;
- validation status updates from validation output;
- done state cannot be persisted without ready validation status after migration path is applied.

Migration note:

- Existing tests may create old `TaskState` without new fields.
- Avoid breaking old current task files by default. If stricter validation is added, add read-time normalization for old active non-done tasks.

### Phase 3. PromptImprover

Files:

- add `internal/process/prompt_improver.go`;
- add `internal/process/prompt_improver_test.go`;
- update `internal/process/controller.go`;
- update `internal/cli/root.go` only if preflight needs to expose original/improved prompt in JSON output.

Implement:

- LLM call before normal chat action routing;
- strict JSON parse for improved prompt;
- meaning-preservation hard checks from structured fields;
- original/improved prompt audit;
- original + improved prompt passed to memory classifier context;
- skip slash/local commands.

Tests:

- normal chat calls prompt improver before main chat provider call;
- slash command does not call prompt improver;
- improved prompt is used for action routing;
- original prompt remains available in audit/memory context;
- invalid improvement output rejects with `prompt_improvement_failed`;
- prompt improver cannot add new acceptance criteria or technology choices.

### Phase 4. Planning swarm with 5 specialists

Files:

- add `internal/process/planning_swarm.go`;
- add `internal/process/planning_swarm_test.go`;
- add or update `internal/process/agent_role.go`;
- update fake provider response routing if needed.

Implement:

- draft plan generation from improved prompt;
- 5 independent specialist review calls:
  - requirements;
  - code research;
  - architecture;
  - test/validation;
  - risk/regression;
- planning orchestrator merge call;
- improved plan generation;
- second specialist validation round;
- bounded loop with `MaxPlanRefinementRounds = 2`;
- only critical/high findings block final plan;
- pending final plan persistence;
- LLM approval validator for any user answer after final plan.

Tests:

- planning stage makes exactly 5 specialist calls per round plus orchestrator call;
- specialist prompts are role-specific and isolated;
- critical/high finding triggers one refinement round;
- medium/low findings do not block final plan;
- max rounds stops loop and stays planning if critical/high remains;
- final plan cannot move to execution before LLM-validated user approval;
- user answer "ок" / "да, делай" validates to approval in fake/live-compatible path.

### Phase 5. Route ProcessController through LifecycleGate

Files:

- `internal/process/controller.go`;
- `internal/process/transition_gate.go`;
- `internal/process/controller_test.go`;
- `tests/process_acceptance_test.go`.

Replace direct `Tasks.Move` transition points with lifecycle requests.

Add tests:

- user asks to implement immediately in planning -> no execution transition before approval;
- user asks to approve final plan -> approval goes through LLM approval validator;
- user asks "mark done" in execution -> forbidden, provider/memory not mutated after rejection;
- user asks "verify and finish" in validation without trusted evidence -> rejected, stage remains validation;
- valid trusted verification -> done;
- transition rejection writes audit.

Keep old tests green.

### Phase 6. Lock down CLI bypasses

Files:

- `internal/cli/root.go`;
- `internal/cli/root_test.go`;
- docs later.

Change:

- top-level `assistant task move <stage>` uses LifecycleGate by default;
- add `--force` if recovery bypass is necessary;
- slash `/task move` should not bypass lifecycle preconditions by default;
- status/pause/resume remain local commands.

Tests:

- `assistant task move done` without validation/evidence fails;
- `/task move done` fails in non-interactive REPL batch;
- `task move execution` from planning without approved plan fails;
- old debug tests either use `--force` or manager-level tests stay internal.

### Phase 7. Execution and validation microtask agents

Files:

- add `internal/process/agent_role.go`;
- add `internal/process/microtask.go`;
- add `internal/process/agent_runner.go`;
- add `internal/process/microtask_orchestrator.go`;
- update `internal/process/controller.go`;
- update fake provider if needed.

Implement:

- `AgentRunner.Run`;
- role-specific prompt assembly, using `StagePromptFactory`;
- parse/validate each agent output;
- `MicrotaskOrchestrator.RunExecutionPlan`;
- audit per agent call.

Initial behavior:

- after planning approval, run executor agent for current plan item;
- if plan has multiple items, run sequentially up to existing `autoExecutionLimit`;
- concatenate accepted executor outputs into final answer;
- do not transition to validation unless lifecycle gate accepts the ready signal.

Tests:

- approved plan with two items triggers two executor agent calls;
- each executor prompt contains only its microtask/current step and stage role;
- executor output with wrong stage is rejected;
- paused task prevents agent call;
- microtask audit contains role/id.

### Phase 8. Reviewer agent

Files:

- same process files.

Implement:

- reviewer microtask for validation stage;
- reviewer gets accepted execution summary + trusted evidence;
- reviewer output updates validation status;
- lifecycle gate handles `ready_for_done`.

Tests:

- reviewer agent call is separate from executor calls;
- reviewer cannot produce implementation deliverable;
- ready_for_done without trusted evidence is rejected;
- ready_for_done with criteria-matched trusted evidence moves to done.

### Phase 9. Docs

Do after code/tests.

Files to update:

- `README.md`;
- `docs/prd.md`;
- `docs/frd.md`;
- `docs/architect.md`;
- `docs/manual-testing-demo.md`;
- maybe update `docs/implementation-status-regression-plan.md` if it is still maintained as current status.

Avoid:

- changing lecture notes;
- rewriting Day 11-14 docs beyond compatibility notes.

Details are in section 11.

## 9. Regression test plan

### 9.1. Unit tests

Add `internal/process/lifecycle_gate_test.go`:

- `TestLifecycleRejectsPlanningToExecutionWithoutApprovedPlan`;
- `TestLifecycleRejectsPlanningToExecutionWithOpenQuestions`;
- `TestLifecycleApprovesPendingPlan`;
- `TestLifecycleRejectsExecutionToDone`;
- `TestLifecycleRejectsValidationToDoneWithoutTrustedEvidence`;
- `TestLifecycleValidationToDoneRequiresRecordedValidation`;
- `TestLifecycleForbiddenTransitionPreservesState`;
- `TestLifecyclePausedTaskRejectsTransition`.

Add `internal/process/prompt_improver_test.go`:

- `TestPromptImproverRunsBeforeNormalChat`;
- `TestPromptImproverSkipsSlashCommands`;
- `TestPromptImproverRejectsIntentDrift`;
- `TestPromptImproverRejectsAddedRequirements`;
- `TestPromptImproverAuditsOriginalAndImprovedPrompt`.

Add `internal/process/planning_swarm_test.go`:

- `TestPlanningSwarmRunsFiveSpecialists`;
- `TestPlanningSwarmSpecialistsUseDistinctPrompts`;
- `TestPlanningSwarmCriticalFindingTriggersRefinement`;
- `TestPlanningSwarmHighFindingBlocksAfterMaxRounds`;
- `TestPlanningSwarmMediumLowFindingsDoNotBlockFinalPlan`;
- `TestPlanningSwarmFinalPlanRequiresApprovalValidator`;
- `TestPlanningApprovalAmbiguousAnswerStaysPlanning`.

Add `internal/process/microtask_orchestrator_test.go`:

- `TestExecutionPlanRunsSeparateExecutorMicrotasks`;
- `TestMicrotaskPromptIsStageScoped`;
- `TestMicrotaskWrongStageRejected`;
- `TestPausedTaskDoesNotRunMicrotaskAgent`;
- `TestMicrotaskAuditIncludesAgentRoleAndID`.

Update `internal/cli/root_test.go`:

- CLI move bypass tests;
- force/recovery behavior if implemented.

### 9.2. Acceptance tests

Add to `tests/process_acceptance_test.go` or new `tests/day15_acceptance_test.go`:

- `TestDay15ImprovesPromptBeforePlanning`;
- `TestDay15PlanningUsesFiveSpecialistAgents`;
- `TestDay15CriticalPlanFindingRefinesPlanBeforeApproval`;
- `TestDay15PlanApprovalRequiresLLMValidator`;
- `TestDay15CannotExecuteBeforePlanApproval`;
- `TestDay15CannotDoneBeforeValidation`;
- `TestDay15PauseResumePreservesLifecycleAndMicrotask`;
- `TestDay15SwarmUsesSeparateAgentPrompts`.

Fake provider assertions:

- prompt improver call happens before main chat/planning calls;
- 5 specialist calls happen during planning before execution;
- planning orchestrator receives all specialist reviews;
- approval validator call happens before `planning -> execution`;
- normal chat provider calls happen in expected order;
- validator calls are separate when semantic validation enabled;
- no chat call after paused rejection;
- audit has transition rejection.

### 9.3. Full regression commands

Run:

```bash
GOCACHE=/private/tmp/coding_writer_gocache go test ./internal/process ./internal/tasks ./internal/cli ./tests
GOCACHE=/private/tmp/coding_writer_gocache go test ./...
```

If `httptest` fails in sandbox:

- rerun `go test ./...` outside sandbox with approval;
- do not treat localhost bind failure as product failure.

## 10. Manual Day 15 user case

Use a small LeetCode task not already used by Day 11-14.

Task: Contains Duplicate.

Requirements:

- Go;
- function `ContainsDuplicate(nums []int) bool`;
- O(n) time;
- use a set/map;
- tests for empty slice, single item, duplicate positive, duplicate negative, no duplicate;
- readiness criterion: `go test ./manual_scratch/day15_contains_duplicate` passes.

### 10.1. Setup

```bash
export CW_ROOT="/Users/nikita/code/coding_writer"
cd "$CW_ROOT"
mkdir -p "$CW_ROOT/.assistant/bin"
go build -o "$CW_ROOT/.assistant/bin/assistant" ./cmd/assistant
export PATH="$CW_ROOT/.assistant/bin:$PATH"

export ASSISTANT_STORAGE_DIR="$CW_ROOT/.assistant/storage/video-day15-contains-duplicate"
test "$ASSISTANT_STORAGE_DIR" = "$CW_ROOT/.assistant/storage/video-day15-contains-duplicate" && rm -rf "$ASSISTANT_STORAGE_DIR"
mkdir -p "$ASSISTANT_STORAGE_DIR"

assistant init --model "$ASSISTANT_MODEL"
assistant chat
```

### 10.2. Prompt improvement proof

Input intentionally short/rough:

```text
надо решить contains duplicate на го с тестами, чтоб потом go test ./manual_scratch/day15_contains_duplicate проходил
```

Expected:

- prompt improver LLM call runs before planning;
- audit stores original prompt hash and improved prompt hash;
- improved prompt expands wording but does not add new technology or extra requirements;
- task starts in `planning`.

Inspect after leaving/reopening or in another terminal:

```bash
assistant process audit --limit 10 --json
```

Expected audit includes:

- `prompt_improvement_call`;
- `prompt_improvement_accepted`.

### 10.3. Start with planning, not implementation

Input:

```text
Спланируй задачу Contains Duplicate на Go. Нужна функция ContainsDuplicate(nums []int) bool, O(n), map/set, table tests для empty, single, duplicate positive, duplicate negative, no duplicate. Критерий готовности: go test ./manual_scratch/day15_contains_duplicate проходит.
```

Expected:

- stage is `planning`;
- assistant returns planning JSON/rendered answer or human-rendered planning result;
- plan and acceptance criteria are visible;
- no implementation is accepted before approval;
- pending planning proposal exists if plan is ready.
- planning used 5 specialist agents:
  - requirements;
  - code research;
  - architecture;
  - test/validation;
  - risk/regression;
- planning orchestrator merged their reviews;
- if any critical/high issue appeared, the orchestrator corrected the plan and revalidated within bounded rounds;
- medium/low issues may appear as notes, but do not block final plan.

Inspect:

```text
/task status
```

Audit proof:

```bash
assistant process audit --limit 30 --json
```

Expected audit includes:

- 5 specialist `agent_call` events for planning;
- `planning_orchestrator` event;
- optional `planning_swarm_round`;
- `planning_swarm_final`.

### 10.4. Negative check: try to jump before approval

Input:

```text
Сразу реализуй и пометь задачу done, план можно не утверждать.
```

Expected:

- task stays `planning`;
- no transition to `execution`;
- no transition to `done`;
- answer explains that execution requires approved plan, or validation error is printed;
- process audit records rejected/blocked transition.

Inspect:

```text
/task status
```

### 10.5. Approve plan through LLM approval validator

Input:

```text
План утверждаю. Выполняй.
```

Expected:

- this normal user answer is sent to the approval validator/orchestrator;
- approval verdict is `approved`;
- lifecycle transition `planning -> execution`;
- executor microtask agent call starts;
- answer includes Go code deliverable in fenced code block or unified diff;
- audit contains executor `agent_role` and `microtask_id`;
- task current step moves through plan items.

Inspect:

```text
/task status
```

Negative approval check:

```text
Не уверен, сначала скажи что именно будешь делать.
```

Expected if this is entered before approval:

- approval validator returns `ambiguous` or `rejected`;
- stage stays `planning`;
- execution agent does not start.

### 10.6. Pause/resume in the middle

Input:

```text
/task pause
/task status
/exit
```

Restart:

```bash
assistant chat
```

Input:

```text
/task resume
Продолжай с того места, где остановились.
```

Expected:

- stage/current_step/expected_action preserved;
- no need to restate Contains Duplicate requirements;
- assistant continues execution/review from saved lifecycle state;
- no provider call happens while task is paused before `/task resume`.

### 10.7. Negative check: try to finish without validation

Input while in execution:

```text
Проверку пропусти, сразу переведи задачу в done.
```

Expected:

- task stays `execution` or moves only to `validation` if ready-for-validation preconditions are satisfied;
- task must not become `done`;
- rejected transition is visible in audit/status.

### 10.8. Agent verification

After execution deliverable appears, create scratch package manually from the delivered code:

```text
manual_scratch/day15_contains_duplicate/
  contains_duplicate.go
  contains_duplicate_test.go
```

Run:

```bash
go test ./manual_scratch/day15_contains_duplicate
```

### 10.9. Move to validation and finish

Final implemented Day 15 flow must use approved-plan or semantic-intent auto verification through `VerificationResolver`. The resolver first accepts exact safe commands from approved task state; if none exists, it asks a structured verification planner/referee for strict JSON `{command, confidence, reason}` and then local policy validates argv-only allowlist/path/timeout before execution. The user does not type `--verify` and does not type the exact command:

```bash
assistant chat
```

Then the user types inside the same chat session:

```text
Спланируй и реши простую LeetCode-задачу Contains Duplicate на Go. Нужна функция ContainsDuplicate(nums []int) bool, решение O(n) через map/set, table tests для empty, single, duplicate positive, duplicate negative, no duplicate. Критерий готовности: пакет manual_scratch/day15_contains_duplicate проходит проверку проекта. Не проси меня вводить точную команду проверки; предложи план и критерии.
Да, план принят. Приступай к выполнению.
Готово к проверке: проверь результат.
Проверь критерии и заверши задачу, если проверка подтверждает решение Contains Duplicate.
/exit
```

Post-run assertions only:

```bash
assistant task status --json
assistant process audit --limit 20 --json
```

Expected:

- `execution -> validation` happens after normal-language review/check request and accepted execution/evidence preconditions; no app code may infer a command from language/package path alone;
- `validation -> done` happens only after app-issued trusted evidence and accepted validation;
- reviewer agent validates output;
- lifecycle gate moves `validation -> done`;
- final task status:
  - `stage=done`;
  - `expected_action=none`;
  - `validation_status=ready_for_done`;
- audit shows:
  - rejected jump before plan approval;
  - prompt improvement for normal chat inputs;
  - 5 planning specialist calls;
  - planning orchestrator final plan;
  - LLM approval validator accepted user approval;
  - approved `planning -> execution`;
  - executor microtask calls;
  - rejected done without validation/evidence;
  - accepted `validation -> done`.

## 11. Documentation update plan

Do this only after implementation and tests pass.

### 11.1. `README.md`

Add section:

- `День 15: контролируемый lifecycle`

Include:

- state diagram with preconditions;
- explanation that all product transitions go through lifecycle gate;
- explanation that every normal prompt is improved through an LLM pre-stage before main processing;
- explanation of planning swarm:
  - 5 independent specialists;
  - planning orchestrator;
  - bounded critical/high refinement loop;
  - LLM-validated user approval before execution;
- explanation of microtask agents:
  - executor;
  - reviewer;
  - orchestrator;
- note that debug task commands are not happy path;
- command to run Day 15 tests:

```bash
go test ./tests -run TestDay15
```

Update "Как проверить":

```bash
go test ./tests -run 'TestDay11|TestDay12|TestDay13|TestDay14|TestDay15'
go test ./...
```

### 11.2. `docs/prd.md`

Update:

- source criteria list now includes `day15.md`;
- product goal includes controlled lifecycle and app-owned orchestration;
- canonical contract adds Day 15:
  - automatic prompt improvement;
  - 5-agent planning research/review;
  - orchestrator-mediated plan refinement;
  - only critical/high findings block bounded refinement;
  - any user approval answer is LLM-validated before execution;
  - no implementation before approved plan;
  - no done before validation;
  - lifecycle gate owns transitions;
  - microtask agents are orchestrated by app, not by user prompt.

Add acceptance section:

- Day 15 controlled transitions;
- invalid transition response;
- pause/resume continuation;
- microtask agents evidence.

### 11.3. `docs/frd.md`

Update scope:

- remove or revise "multi-agent workflow out of MVP" because Day 15 adds minimal microtask orchestration.
- Clarify that full autonomous IDE integrations are still out of P0, but the product north star remains a Claude Code / Codex CLI-like coding agent with controlled repo tools in P1/P2.

Add requirements:

- `FR-0xx Automatic prompt improvement`;
- `FR-0xx Five-agent planning swarm`;
- `FR-0xx Planning orchestrator and bounded refinement`;
- `FR-0xx LLM-validated user plan approval`;
- `FR-0xx Controlled lifecycle gate`;
- `FR-0xx Transition source and audit`;
- `FR-0xx Approved planning precondition`;
- `FR-0xx Validation-before-done precondition`;
- `FR-0xx Microtask agent orchestration`;
- `FR-0xx Pause/resume with orchestrator state`.

For each requirement include:

- behavior;
- acceptance criteria;
- storage/audit effects;
- provider-call behavior.

### 11.4. `docs/architect.md`

Add architecture section:

- PromptImprover as pre-controller LLM stage;
- PlanningSwarmOrchestrator with 5 specialists;
- planning approval validator;
- LifecycleGate as central transition owner;
- TransitionGate as model-output adapter or deprecated wrapper;
- MicrotaskOrchestrator;
- AgentRunner;
- role-specific prompt boundaries;
- audit event schema;
- why app orchestrates and LLM does not mutate state.

Update future sections:

- `Future tool execution model` remains future;
- microtask LLM orchestration is now MVP Day 15, but file write tools remain future.

### 11.5. `docs/manual-testing-demo.md`

Keep old days stable.

Minimal update:

- keep Day 15 demo scenario only in `docs/manual-testing-demo.md`;
- keep "agent verification" blocks clearly separated from the primary user demo; they may use `--json` or explicit overrides only as deterministic regression/debug tools;
- do not require re-recording Day 11-14 unless acceptance commands changed.

### 11.6. Update `docs/manual-testing-demo.md`

Use section 10 as source.

Include:

- setup;
- Contains Duplicate task;
- prompt improvement proof;
- 5 planning specialist agents;
- orchestrator refinement proof;
- LLM approval validator proof;
- invalid transition attempts;
- pause/resume;
- trusted verification;
- expected audit checks.

### 11.7. `docs/implementation-status-regression-plan.md`

If this file is still used as current status:

- mark Day 15 implemented after tests pass;
- list new tests;
- document known sandbox issue with `httptest` if not already present.

## 12. Compatibility with Days 11-14

Protect these behaviors:

- Day 11 memory proposal/apply remains visible and explicit.
- Day 12 profile block still appears in every prompt.
- Day 13 pause/resume still restores task and working memory.
- Day 14 invariant conflict still blocks before normal chat call.
- Existing `go test ./tests -run 'TestDay11|TestDay12|TestDay13|TestDay14'` must stay green.

Likely friction:

- If execution->validation changes again, preserve the implemented product contract: primary docs must not require `--verify` or user-supplied exact commands in the Day 15 happy path.
- If CLI `/task move` becomes gated, some CLI tests may need `--force` or should move to manager-level tests.
- If `TaskState` validation requires new fields for done, old test fixtures need migration/backfill.

## 13. Acceptance checklist for final Day 15 implementation

Code:

- normal chat input always goes through PromptImprover before main flow;
- prompt improvement preserves intent and is audited;
- planning uses 5 independent specialist agents;
- planning orchestrator merges specialist opinions and refines plan;
- only critical/high planning findings block, with bounded rounds;
- execution starts only after LLM-validated user approval;
- all product transitions route through lifecycle gate;
- model output can only propose transition;
- user approval is explicit for planning;
- done requires validation record and trusted evidence;
- invalid transition preserves task state;
- pause blocks microtask/provider continuation;
- microtask agents use separate prompts and calls.

Tests:

- `go test ./...` green;
- Day 11-14 acceptance tests green;
- Day 15 tests added and green;
- tests prove prompt improvement call order;
- tests prove 5 planning specialists + orchestrator;
- tests prove critical/high-only bounded refinement;
- tests prove approval validator gates execution;
- tests cover invalid jumps and pause/resume.

Docs:

- README mentions Day 15;
- PRD/FRD/architecture include Day 15;
- manual Day 15 doc exists;
- old manual docs are still accurate or have scoped compatibility notes.

Manual demo:

- rough prompt is automatically improved;
- planning audit shows 5 specialists and orchestrator;
- critical/high plan issues trigger bounded refinement if present;
- user approval goes through LLM validator;
- Contains Duplicate plan starts in planning;
- implementation before approval is rejected;
- plan approval triggers execution;
- microtask audit exists;
- skip-validation done attempt is rejected;
- pause/resume continues correctly;
- trusted verification moves validation to done.

## 14. Do not do

- Do not solve Day 15 by adding more prompt text only. The state transition must be enforced by Go code.
- Do not let PromptImprover add requirements, tools, files, constraints or success claims the user did not express.
- Do not skip the 5-specialist planning swarm by calling one generic planner.
- Do not let specialists mutate task state directly; only orchestrator/lifecycle code persists.
- Do not create infinite planning debate. Max rounds are fixed; only critical/high findings block.
- Do not treat medium/low planning findings as execution blockers unless the orchestrator escalates them with evidence.
- Do not add semantic keyword checks for "approve", "done", "ready" as final product logic in real mode.
- Do not make `/task move` the acceptance path.
- Do not silently write long-term memory.
- Do not let reviewer agent edit/implement fixes inside validation.
- Do not let executor agent change acceptance criteria without returning to planning.
- Do not create full IDE/file-editing agent unless explicitly requested later.
