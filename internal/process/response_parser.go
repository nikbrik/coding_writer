package process

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/nikbrik/coding_writer/internal/app"
)

// ParsedResponse holds a structured candidate without mutating task state.
type ParsedResponse struct {
	Stage           app.TaskStage
	ActionKind      ActionKind
	Raw             string
	TrustedEvidence []string
	Planning        *PlanningOutput
	Execution       *ExecutionOutput
	Validation      *ValidationOutput
	Done            *DoneOutput
}

// Parse converts raw provider output into a structured candidate.
func Parse(stage app.TaskStage, action ActionKind, raw string) (ParsedResponse, error) {
	raw = strings.TrimSpace(raw)
	if action == ActionAnswerQuestion {
		cleaned := stripMarkdownFences(raw)
		if looksLikeJSON(cleaned) {
			var stageField struct {
				Stage string `json:"stage"`
			}
			if err := json.Unmarshal([]byte(cleaned), &stageField); err == nil && stageField.Stage != "" && app.TaskStage(stageField.Stage) != stage {
				return ParsedResponse{}, app.ErrorWithHint(app.CategoryValidation, "stage_mismatch", "response stage does not match current stage", "expected "+string(stage)+", got "+stageField.Stage, nil)
			}
		}
		return ParsedResponse{Stage: stage, ActionKind: action, Raw: raw}, nil
	}

	cleaned := stripMarkdownFences(raw)
	if cleaned == "" || !looksLikeJSON(cleaned) {
		return ParsedResponse{}, app.NewError(app.CategoryValidation, "invalid_json", "response is not valid JSON", nil)
	}

	var stageField struct {
		Stage string `json:"stage"`
	}
	if err := json.Unmarshal([]byte(cleaned), &stageField); err != nil {
		return ParsedResponse{}, app.NewError(app.CategoryValidation, "invalid_json", "failed to parse response JSON: "+err.Error(), nil)
	}
	if stageField.Stage == "" {
		return ParsedResponse{}, app.NewError(app.CategoryValidation, "missing_stage", "response JSON missing stage field", nil)
	}
	if app.TaskStage(stageField.Stage) != stage {
		return ParsedResponse{}, app.ErrorWithHint(app.CategoryValidation, "stage_mismatch", "response stage does not match current stage", "expected "+string(stage)+", got "+stageField.Stage, nil)
	}

	resp := ParsedResponse{Stage: stage, ActionKind: action, Raw: cleaned}
	switch stage {
	case app.StagePlanning:
		var out PlanningOutput
		if err := decodeStrict(cleaned, &out); err != nil {
			return ParsedResponse{}, app.NewError(app.CategoryValidation, "invalid_json", "failed to parse planning schema: "+err.Error(), nil)
		}
		resp.Planning = &out
	case app.StageExecution:
		var out ExecutionOutput
		if err := decodeStrict(cleaned, &out); err != nil {
			return ParsedResponse{}, app.NewError(app.CategoryValidation, "invalid_json", "failed to parse execution schema: "+err.Error(), nil)
		}
		resp.Execution = &out
	case app.StageValidation:
		var out ValidationOutput
		if err := decodeStrict(cleaned, &out); err != nil {
			return ParsedResponse{}, app.NewError(app.CategoryValidation, "invalid_json", "failed to parse validation schema: "+err.Error(), nil)
		}
		resp.Validation = &out
	case app.StageDone:
		var out DoneOutput
		if err := decodeStrict(cleaned, &out); err != nil {
			return ParsedResponse{}, app.NewError(app.CategoryValidation, "invalid_json", "failed to parse done schema: "+err.Error(), nil)
		}
		resp.Done = &out
	default:
		return ParsedResponse{}, app.NewError(app.CategoryValidation, "unknown_stage", "no parser for stage", nil)
	}
	return resp, nil
}

func decodeStrict(raw string, out any) error {
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return app.NewError(app.CategoryValidation, "invalid_json", "response JSON has trailing data", err)
	}
	return nil
}

func stripMarkdownFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		if firstLine, rest, ok := strings.Cut(s, "\n"); ok && !looksLikeJSON(strings.TrimSpace(firstLine)) {
			s = rest
		}
		s = strings.TrimSpace(s)
		if strings.HasSuffix(s, "```") {
			s = strings.TrimSuffix(s, "```")
		}
		return strings.TrimSpace(s)
	}
	return s
}

func looksLikeJSON(s string) bool {
	return strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[")
}
