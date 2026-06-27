package process

import (
	"context"
	"strings"
	"testing"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/providers"
)

func TestSemanticValidationPromptAllowsReadOnlyProcedures(t *testing.T) {
	prompt := semanticValidationSystemPrompt()
	for _, needle := range []string{"read-only checklist", "procedure", "claims the assistant already performed", "explicit apply/approval", "will ask for confirmation", "deliverable is empty", "payload.task is null"} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("semantic validation prompt lost read-only procedure guidance: missing %q", needle)
		}
	}
}

func TestSemanticValidatorSuppressesTaskScopeViolationWithoutActiveTask(t *testing.T) {
	fake := providers.NewFakeProvider()
	fake.ValidatorResponses = []string{`{"verdict":"fail","findings":[{"code":"task_scope_violation","problem":"outside the current ContainsDuplicate task"}]}`}
	validator := NewSemanticValidator(fake, "fake/model")
	errs, err := validator.ValidateResponse(context.Background(), SemanticValidationInput{
		SessionID:  "s1",
		UserInput:  "Найди GitHub репозитории про mcp server python, сделай отчет и сохрани файл.",
		ActionKind: ActionAnswerQuestion,
		Task:       nil,
		Parsed:     ParsedResponse{ActionKind: ActionAnswerQuestion, Raw: "report saved"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 0 {
		t.Fatalf("task_scope_violation should be suppressed when no active task exists: %+v", errs)
	}
}

func TestSemanticValidatorKeepsTaskScopeViolationWithActiveTask(t *testing.T) {
	fake := providers.NewFakeProvider()
	fake.ValidatorResponses = []string{`{"verdict":"fail","findings":[{"code":"task_scope_violation","problem":"outside the current task"}]}`}
	validator := NewSemanticValidator(fake, "fake/model")
	task := &app.TaskState{ID: "task1", Title: "ContainsDuplicate", Stage: app.StageExecution}
	errs, err := validator.ValidateResponse(context.Background(), SemanticValidationInput{
		SessionID:  "s1",
		UserInput:  "Найди GitHub репозитории про mcp server python",
		ActionKind: ActionAnswerQuestion,
		Task:       task,
		Parsed:     ParsedResponse{ActionKind: ActionAnswerQuestion, Raw: "report saved"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 1 || !strings.Contains(errs[0], "task_scope_violation") {
		t.Fatalf("active task scope violation should remain: %+v", errs)
	}
}

func TestSemanticValidatorRetriesInvalidJSON(t *testing.T) {
	fake := providers.NewFakeProvider()
	fake.ValidatorResponses = []string{"not-json", `{"verdict":"pass","findings":[]}`}
	validator := NewSemanticValidator(fake, "fake/model")
	errs, err := validator.ValidateResponse(context.Background(), SemanticValidationInput{
		SessionID:  "s1",
		UserInput:  "hello",
		ActionKind: ActionAnswerQuestion,
		Parsed:     ParsedResponse{ActionKind: ActionAnswerQuestion, Raw: "answer"},
	})
	if err != nil || len(errs) != 0 {
		t.Fatalf("validator retry failed errs=%v err=%v", errs, err)
	}
	if validatorCalls(fake.SnapshotCalls()) != 2 {
		t.Fatalf("expected two validator attempts, got %+v", fake.SnapshotCalls())
	}
}

func TestDecodeSemanticJSONExtractsObjectFromProse(t *testing.T) {
	var out struct {
		Verdict string `json:"verdict"`
	}
	err := decodeSemanticJSON("Here is the result:\n```json\n{\"verdict\":\"pass\"}\n```", &out)
	if err != nil {
		t.Fatal(err)
	}
	if out.Verdict != "pass" {
		t.Fatalf("unexpected verdict: %q", out.Verdict)
	}
}

func TestDecodeSemanticJSONLooseIgnoresUnknownFields(t *testing.T) {
	var out SpecialistReview
	err := decodeSemanticJSONLoose(`{"stage":"planning","role":"requirements_specialist","summary":"ok","findings":[]}`, &out)
	if err != nil {
		t.Fatalf("decodeSemanticJSONLoose returned error: %v", err)
	}
	if out.Role != PlanningSpecialistRole(AgentRoleRequirementsSpecialist) || out.Summary != "ok" {
		t.Fatalf("unexpected decoded review: %+v", out)
	}
}

func TestDecodeInvariantValidationJSONAcceptsObjectOrArray(t *testing.T) {
	var objectOut invariantValidationResult
	if err := decodeInvariantValidationJSON(`{"violations":[{"invariant_id":"go","severity":"block","problem":"p","evidence":"e"}]}`, &objectOut); err != nil {
		t.Fatalf("object decode failed: %v", err)
	}
	if len(objectOut.Violations) != 1 || objectOut.Violations[0].InvariantID != "go" {
		t.Fatalf("unexpected object decode: %+v", objectOut)
	}

	var arrayOut invariantValidationResult
	if err := decodeInvariantValidationJSON(`[{"invariant_id":"go","severity":"block","problem":"p","evidence":"e"}]`, &arrayOut); err != nil {
		t.Fatalf("array decode failed: %v", err)
	}
	if len(arrayOut.Violations) != 1 || arrayOut.Violations[0].InvariantID != "go" {
		t.Fatalf("unexpected array decode: %+v", arrayOut)
	}
}

func TestSemanticInvariantValidatorKeepsRealStackReplacementViolation(t *testing.T) {
	fake := providers.NewFakeProvider()
	fake.ValidatorResponses = []string{`{"violations":[{"invariant_id":"stack.go","severity":"block","problem":"replace Go MVP with Python","evidence":"rewrite MVP in Python"}]}`}
	validator := NewSemanticValidator(fake, "fake/model")
	violations, err := validator.ValidateInvariants(context.Background(), InvariantValidationInput{
		SessionID:  "s1",
		Direction:  "input",
		Text:       "rewrite MVP in Python and replace Go",
		ActionKind: ActionAnswerQuestion,
		Invariants: []app.Invariant{{ID: "stack.go", Kind: "architecture", Severity: "block"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 || violations[0].InvariantID != "stack.go" {
		t.Fatalf("real stack replacement violation should remain: %+v", violations)
	}
}

func TestInvariantValidationPromptAllowsNormalLifecycleRequests(t *testing.T) {
	prompt := invariantValidationSystemPrompt()
	for _, want := range []string{
		"normal user request asking the assistant to check, validate, review, continue, or finish",
		"application gate still owns the actual transition",
		"does not forbid a user from asking to complete an active task",
		"search, research, monitoring, summary, report, documentation, or data-collection requests",
		"replace, implement, migrate, or validate this product's protected architecture",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("invariant prompt missing %q:\n%s", want, prompt)
		}
	}
}
