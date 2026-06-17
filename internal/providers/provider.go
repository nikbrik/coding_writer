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
)

type CompletionRequest struct {
	Purpose     CompletionPurpose `json:"purpose"`
	Model       string            `json:"model"`
	Messages    []app.ChatMessage `json:"messages"`
	JSONMode    bool              `json:"json_mode"`
	Temperature *float64          `json:"temperature,omitempty"`
}

type CompletionResponse struct {
	Message      app.ChatMessage `json:"message"`
	ProviderID   string          `json:"provider_id,omitempty"`
	Model        string          `json:"model"`
	RetryCount   int             `json:"retry_count"`
	UsageSummary string          `json:"usage_summary,omitempty"`
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
