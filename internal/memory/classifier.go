package memory

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/providers"
	"github.com/nikbrik/coding_writer/internal/validation"
)

type Classifier struct {
	Provider providers.LLMProvider
}

type ClassificationInput struct {
	SessionID         string
	UserMessageID     string
	AssistantMessageID string
	UserMessage       string
	AssistantMessage  string
	Profile           app.UserProfile
	Task              *app.TaskState
	Model             string
}

func NewClassifier(provider providers.LLMProvider) *Classifier { return &Classifier{Provider: provider} }

func (c *Classifier) Propose(ctx context.Context, input ClassificationInput) (app.MemoryProposal, error) {
	if c.Provider == nil {
		return app.MemoryProposal{}, app.NewError(app.CategoryClassifier, "missing_provider", "classifier provider missing", nil)
	}
	messages := []app.ChatMessage{{
		ID:        app.NewID("msg"),
		Role:      app.RoleSystem,
		Content:   classifierInstructions(),
		CreatedAt: time.Now().UTC(),
	}, {
		ID:        app.NewID("msg"),
		Role:      app.RoleUser,
		Content:   classifierInputText(input),
		CreatedAt: time.Now().UTC(),
	}}
	res, err := c.Provider.Complete(ctx, providers.CompletionRequest{Purpose: providers.PurposeClassifier, Model: input.Model, Messages: messages, JSONMode: true})
	if err != nil {
		return app.MemoryProposal{}, err
	}
	proposal, err := parseProposal(res.Message.Content)
	if err != nil {
		return proposal, err
	}
	proposal.ID = app.NewID("proposal")
	proposal.SessionID = input.SessionID
	proposal.SourceMessageIDs = []string{input.UserMessageID, input.AssistantMessageID}
	proposal.Provider = res.ProviderID
	proposal.Model = res.Model
	proposal.TemplateHash = "p0-memory-classifier-v1"
	proposal.CreatedAt = time.Now().UTC()
	return proposal, nil
}

type classifierJSON struct {
	Records []struct {
		Layer      string  `json:"layer"`
		Kind       string  `json:"kind"`
		Content    string  `json:"content"`
		Reason     string  `json:"reason"`
		Confidence float64 `json:"confidence"`
	} `json:"records"`
}

func parseProposal(content string) (app.MemoryProposal, error) {
	var parsed classifierJSON
	dec := json.NewDecoder(strings.NewReader(content))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&parsed); err != nil {
		return app.MemoryProposal{}, app.NewError(app.CategoryClassifier, "invalid_json", "classifier returned invalid JSON", err)
	}
	proposal := app.MemoryProposal{Records: make([]app.ProposedMemoryRecord, 0, len(parsed.Records))}
	for _, item := range parsed.Records {
		layer := app.ProposedMemoryLayer(strings.ToLower(strings.TrimSpace(item.Layer)))
		switch layer {
		case app.ProposedLayerShort, app.ProposedLayerWork, app.ProposedLayerLong, app.ProposedLayerIgnore:
		default:
			return proposal, app.NewError(app.CategoryClassifier, "unknown_layer", "classifier returned unknown memory layer", nil)
		}
		confidence := item.Confidence
		if confidence < 0 {
			confidence = 0
		}
		if confidence > 1 {
			confidence = 1
		}
		record := app.ProposedMemoryRecord{
			ID:         app.NewID("pmem"),
			Layer:      layer,
			Kind:       strings.TrimSpace(item.Kind),
			Content:    strings.TrimSpace(item.Content),
			Reason:     strings.TrimSpace(item.Reason),
			Confidence: confidence,
			Status:     app.ProposalPending,
		}
		if record.Kind == "" {
			record.Kind = "other"
		}
		if findings := validation.DetectSecrets(record.Content); len(findings) > 0 {
			record.Content = "[REDACTED_SECRET]"
			record.Status = app.ProposalBlocked
			record.BlockReason = "secret detected: " + validation.FindingTypes(findings)
		}
		proposal.Records = append(proposal.Records, record)
	}
	return proposal, nil
}

func classifierInstructions() string {
	return `You are the memory classifier for a CLI assistant.
Return strict JSON only: {"records":[{"layer":"short|work|long|ignore","kind":"preference|requirement|decision|constraint|context|smalltalk|other","content":"...","reason":"...","confidence":0.0}]}.
Memory layers: short=current session, work=current task, long=stable preferences/decisions/constraints/knowledge, ignore=noise/duplicates/secrets.
Never save secrets. Prefer ignore when unsure.`
}

func classifierInputText(input ClassificationInput) string {
	task := "none"
	if input.Task != nil {
		data, _ := json.Marshal(input.Task)
		task = string(data)
	}
	profile, _ := json.Marshal(input.Profile)
	return "Active profile: " + string(profile) + "\nTask state: " + task + "\nLatest user message: " + input.UserMessage + "\nLatest assistant response: " + input.AssistantMessage
}
