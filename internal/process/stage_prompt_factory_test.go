package process

import (
	"strings"
	"testing"

	"github.com/nikbrik/coding_writer/internal/app"
)

func TestStagePromptPlanningForCodeTasksAvoidsManualShellSteps(t *testing.T) {
	factory := NewStagePromptFactory(NewStagePolicyRegistry())
	prompt, err := factory.StagePrompt(app.StagePlanning, ActionPlanTask)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"plan user-visible deliverables and files",
		"not shell steps for the user",
		"Do not create standalone setup-only steps",
		"Combine directory/package scaffolding",
		`"mkdir"`,
		`"run command manually"`,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("planning prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestStagePromptExecutionTurnsFileCreationStepsIntoCodeArtifacts(t *testing.T) {
	factory := NewStagePromptFactory(NewStagePolicyRegistry())
	prompt, err := factory.StagePrompt(app.StageExecution, ActionExecutePlanStep)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"fenced code block or unified diff",
		"If the current plan item is setup-only",
		"next implementation or test artifact from the approved plan",
		"If the current plan item says to create a directory or file",
		"do not output shell commands",
		"output the file contents or unified diff",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("execution prompt missing %q:\n%s", want, prompt)
		}
	}
}
