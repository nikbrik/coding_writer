package process

import "github.com/nikbrik/coding_writer/internal/app"

// PromptBuildInput is the data needed to build a trusted + untrusted prompt.
type PromptBuildInput struct {
	Profile    app.UserProfile
	Task       *app.TaskState
	Memory     app.MemoryBundle
	Invariants []app.Invariant
	Query      string
	Stage      app.TaskStage
	ActionKind ActionKind
}

// PromptBuilder builds chat messages from a prompt input.
type PromptBuilder interface {
	Build(input PromptBuildInput) ([]app.ChatMessage, error)
}
