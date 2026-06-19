package process

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/providers"
)

type PromptImprovementInput struct {
	SessionID string
	Original  string
	Task      *app.TaskState
}

type PromptImprovementResult struct {
	Original            string   `json:"original"`
	Improved            string   `json:"improved_prompt"`
	PreservedIntent     bool     `json:"preserved_intent"`
	AddedRequirements   []string `json:"added_requirements"`
	RemovedRequirements []string `json:"removed_requirements"`
	Clarifications      []string `json:"clarifications"`
	Rationale           string   `json:"rationale"`
	Model               string   `json:"model"`
}

type PromptImprover struct {
	Provider providers.LLMProvider
	Model    string
}

func (p *PromptImprover) Improve(ctx context.Context, input PromptImprovementInput) (PromptImprovementResult, error) {
	if p == nil || p.Provider == nil {
		return PromptImprovementResult{}, app.NewError(app.CategoryInternal, "missing_prompt_improver", "prompt improver is required", nil)
	}
	payload, err := json.Marshal(map[string]any{
		"session_id": input.SessionID,
		"original":   input.Original,
		"task":       input.Task,
	})
	if err != nil {
		return PromptImprovementResult{}, app.NewError(app.CategoryInternal, "prompt_improvement_encode", err.Error(), err)
	}
	system := `You improve user prompts for an internal orchestrator.
Return strict JSON with keys improved_prompt, preserved_intent, added_requirements, removed_requirements, clarifications, rationale.
preserved_intent must be JSON boolean true or false, not a string.
added_requirements, removed_requirements, and clarifications must be JSON arrays of strings.
Preserve intent exactly. Do not add files, technologies, constraints, acceptance criteria, or completion claims.
If the prompt is already clear, keep it nearly unchanged.`
	res, err := p.Provider.Complete(ctx, providers.CompletionRequest{
		Purpose:  providers.PurposeValidator,
		Model:    p.Model,
		JSONMode: true,
		Messages: []app.ChatMessage{
			{ID: app.NewID("msg"), Role: app.RoleSystem, Content: system},
			{ID: app.NewID("msg"), Role: app.RoleUser, Content: string(payload)},
		},
	})
	if err != nil {
		return PromptImprovementResult{}, err
	}
	var out PromptImprovementResult
	if err := decodePromptImprovementJSON(res.Message.Content, &out); err != nil {
		return PromptImprovementResult{}, app.NewError(app.CategoryValidation, "prompt_improvement_failed", app.AsError(err).Message, err)
	}
	out.Original = input.Original
	out.Model = res.Model
	if !out.PreservedIntent || strings.TrimSpace(out.Improved) == "" {
		return PromptImprovementResult{}, app.NewError(app.CategoryValidation, "prompt_improvement_failed", "prompt improvement changed user intent", nil)
	}
	return out, nil
}

func decodePromptImprovementJSON(raw string, out *PromptImprovementResult) error {
	type promptImprovementWire struct {
		Improved            string          `json:"improved_prompt"`
		PreservedIntentRaw  json.RawMessage `json:"preserved_intent"`
		AddedRequirements   json.RawMessage `json:"added_requirements"`
		RemovedRequirements json.RawMessage `json:"removed_requirements"`
		Clarifications      json.RawMessage `json:"clarifications"`
		Rationale           string          `json:"rationale"`
	}
	var wire promptImprovementWire
	if err := decodeSemanticJSON(raw, &wire); err != nil {
		return err
	}
	preserved, err := parseFlexibleBool(wire.PreservedIntentRaw)
	if err != nil {
		return app.NewError(app.CategoryValidation, "invalid_json", "preserved_intent must be boolean", err)
	}
	added, err := parseFlexibleStringSlice(wire.AddedRequirements)
	if err != nil {
		return app.NewError(app.CategoryValidation, "invalid_json", "added_requirements must be an array of strings", err)
	}
	removed, err := parseFlexibleStringSlice(wire.RemovedRequirements)
	if err != nil {
		return app.NewError(app.CategoryValidation, "invalid_json", "removed_requirements must be an array of strings", err)
	}
	clarifications, err := parseFlexibleStringSlice(wire.Clarifications)
	if err != nil {
		return app.NewError(app.CategoryValidation, "invalid_json", "clarifications must be an array of strings", err)
	}
	*out = PromptImprovementResult{
		Improved:            wire.Improved,
		PreservedIntent:     preserved,
		AddedRequirements:   added,
		RemovedRequirements: removed,
		Clarifications:      clarifications,
		Rationale:           wire.Rationale,
	}
	return nil
}

func parseFlexibleBool(raw json.RawMessage) (bool, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return true, nil
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return b, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "yes":
		return true, nil
	case "false", "no":
		return false, nil
	default:
		return false, fmt.Errorf("invalid bool string %q", s)
	}
}

func parseFlexibleStringSlice(raw json.RawMessage) ([]string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err == nil {
		return values, nil
	}
	var single string
	if err := json.Unmarshal(raw, &single); err != nil {
		return nil, err
	}
	single = strings.TrimSpace(single)
	if single == "" || strings.EqualFold(single, "none") || strings.EqualFold(single, "null") || strings.EqualFold(single, "n/a") {
		return nil, nil
	}
	return []string{single}, nil
}
