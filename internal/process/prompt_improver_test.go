package process

import "testing"

func TestDecodePromptImprovementAcceptsStringBool(t *testing.T) {
	var out PromptImprovementResult
	err := decodePromptImprovementJSON(`{
		"improved_prompt":"check package",
		"preserved_intent":"true",
		"added_requirements":"none",
		"removed_requirements":null,
		"clarifications":"",
		"rationale":"same intent"
	}`, &out)
	if err != nil {
		t.Fatalf("decodePromptImprovementJSON returned error: %v", err)
	}
	if !out.PreservedIntent || out.Improved != "check package" {
		t.Fatalf("unexpected decoded result: %+v", out)
	}
}

func TestDecodePromptImprovementAllowsMissingPreservedIntent(t *testing.T) {
	var out PromptImprovementResult
	err := decodePromptImprovementJSON(`{
		"improved_prompt":"check package",
		"added_requirements":[],
		"removed_requirements":[],
		"clarifications":[],
		"rationale":"same intent"
	}`, &out)
	if err != nil {
		t.Fatalf("decodePromptImprovementJSON returned error: %v", err)
	}
	if !out.PreservedIntent {
		t.Fatalf("missing preserved_intent should fall back to true for semantic revalidation: %+v", out)
	}
}
