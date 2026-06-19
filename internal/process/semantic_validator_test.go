package process

import (
	"context"
	"strings"
	"testing"

	"github.com/nikbrik/coding_writer/internal/providers"
)

func TestSemanticValidationPromptAllowsReadOnlyProcedures(t *testing.T) {
	prompt := semanticValidationSystemPrompt()
	for _, needle := range []string{"read-only checklist", "procedure", "claims the assistant already performed", "explicit apply/approval", "will ask for confirmation", "deliverable is empty"} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("semantic validation prompt lost read-only procedure guidance: missing %q", needle)
		}
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

func TestInvariantValidationPromptAllowsNormalLifecycleRequests(t *testing.T) {
	prompt := invariantValidationSystemPrompt()
	for _, want := range []string{
		"normal user request asking the assistant to check, validate, review, continue, or finish",
		"application gate still owns the actual transition",
		"does not forbid a user from asking to complete an active task",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("invariant prompt missing %q:\n%s", want, prompt)
		}
	}
}
