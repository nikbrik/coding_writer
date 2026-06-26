package process

import (
	"context"
	"time"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/providers"
)

const maxPrimaryToolCalls = 4

type ToolRunner interface {
	Tools(ctx context.Context) ([]providers.ToolDefinition, error)
	Run(ctx context.Context, call app.ChatToolCall) (app.ChatMessage, error)
}

func ToolResultMessage(call app.ChatToolCall, content string) app.ChatMessage {
	return app.ChatMessage{
		ID:         app.NewID("msg"),
		Role:       app.ChatRole("tool"),
		Content:    content,
		ToolCallID: call.ID,
		CreatedAt:  time.Now().UTC(),
	}
}
