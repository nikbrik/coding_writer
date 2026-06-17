package process

import (
	"strings"
	"testing"

	"github.com/nikbrik/coding_writer/internal/app"
)

func TestParseAnswerQuestionAllowsText(t *testing.T) {
	resp, err := Parse(app.StagePlanning, ActionAnswerQuestion, "just text")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Raw != "just text" {
		t.Fatalf("unexpected raw: %q", resp.Raw)
	}
}

func TestParseRejectsInvalidJSON(t *testing.T) {
	_, err := Parse(app.StagePlanning, ActionPlanTask, "not json")
	if err == nil {
		t.Fatal("expected error")
	}
	if app.AsError(err).Code != "invalid_json" {
		t.Fatalf("unexpected code: %v", err)
	}
}

func TestParseRejectsStageMismatch(t *testing.T) {
	raw := `{"stage":"execution","summary":"x"}`
	_, err := Parse(app.StagePlanning, ActionPlanTask, raw)
	if err == nil {
		t.Fatal("expected error")
	}
	if app.AsError(err).Code != "stage_mismatch" {
		t.Fatalf("unexpected code: %v", err)
	}
}

func TestParsePlanningOutput(t *testing.T) {
	raw := `{"stage":"planning","summary":"s","assumptions":["a"],"acceptance_criteria":["c"],"plan":["p"],"open_questions":[],"readiness":"needs_user_input"}`
	resp, err := Parse(app.StagePlanning, ActionPlanTask, raw)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Planning == nil || resp.Planning.Summary != "s" {
		t.Fatalf("unexpected parsed output: %+v", resp.Planning)
	}
}

func TestParseStripsMarkdownFence(t *testing.T) {
	raw := "```json\n{\"stage\":\"validation\",\"findings\":[],\"passed_checks\":[],\"missing_evidence\":[],\"residual_risks\":[],\"verdict\":\"ready_for_done\"}\n```"
	_, err := Parse(app.StageValidation, ActionReviewOutput, raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseStripsUppercaseMarkdownFence(t *testing.T) {
	raw := "```JSON\n{\"stage\":\"planning\",\"summary\":\"s\",\"assumptions\":[],\"acceptance_criteria\":[\"c\"],\"plan\":[\"p\"],\"open_questions\":[],\"readiness\":\"needs_user_input\"}\n```"
	_, err := Parse(app.StagePlanning, ActionPlanTask, raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseNoStageField(t *testing.T) {
	_, err := Parse(app.StageExecution, ActionExecutePlanStep, `{"summary":"x"}`)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(app.AsError(err).Code, "missing_stage") {
		t.Fatalf("unexpected code: %v", err)
	}
}

func TestParseRejectsUnknownFields(t *testing.T) {
	_, err := Parse(app.StagePlanning, ActionPlanTask, `{"stage":"planning","summary":"s","assumptions":[],"acceptance_criteria":["c"],"plan":["p"],"open_questions":[],"readiness":"needs_user_input","set_stage":"execution"}`)
	if err == nil || app.AsError(err).Code != "invalid_json" {
		t.Fatalf("want invalid_json, got %v", err)
	}
}

func TestParseRejectsTrailingJSON(t *testing.T) {
	_, err := Parse(app.StagePlanning, ActionPlanTask, `{"stage":"planning","summary":"s","assumptions":[],"acceptance_criteria":["c"],"plan":["p"],"open_questions":[],"readiness":"needs_user_input"} {"stage":"done"}`)
	if err == nil || app.AsError(err).Code != "invalid_json" {
		t.Fatalf("want invalid_json, got %v", err)
	}
}
