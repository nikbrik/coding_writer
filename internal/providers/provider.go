package providers

import (
	"context"
	"time"

	"github.com/nikbrik/coding_writer/internal/app"
)

type CompletionPurpose string

const (
	PurposeChat       CompletionPurpose = "chat"
	PurposeClassifier CompletionPurpose = "classifier"
	PurposeValidator  CompletionPurpose = "validator"
)

type CompletionRequest struct {
	Purpose           CompletionPurpose `json:"purpose"`
	Model             string            `json:"model"`
	Messages          []app.ChatMessage `json:"messages"`
	JSONMode          bool              `json:"json_mode"`
	Temperature       *float64          `json:"temperature,omitempty"`
	Tools             []ToolDefinition  `json:"tools,omitempty"`
	ToolChoice        any               `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool             `json:"parallel_tool_calls,omitempty"`
}

type CompletionResponse struct {
	Message      app.ChatMessage    `json:"message"`
	ToolCalls    []app.ChatToolCall `json:"tool_calls,omitempty"`
	ProviderID   string             `json:"provider_id,omitempty"`
	Model        string             `json:"model"`
	RetryCount   int                `json:"retry_count"`
	UsageSummary string             `json:"usage_summary,omitempty"`
}

type ToolDefinition struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

type LLMProvider interface {
	ListModels(ctx context.Context) ([]string, error)
	Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}

func newAssistantMessage(content, model, providerID string) CompletionResponse {
	return CompletionResponse{
		Message:    app.ChatMessage{ID: app.NewID("msg"), Role: app.RoleAssistant, Content: content, CreatedAt: time.Now().UTC()},
		Model:      model,
		ProviderID: providerID,
	}
}
