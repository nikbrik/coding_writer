package process

import "github.com/nikbrik/coding_writer/internal/app"

// ExchangeResult is the outcome of a process-controlled chat exchange.
type ExchangeResult struct {
	Answer         string
	Model          string
	Messages       []app.ChatMessage
	RenderedPrompt string
	Proposal       *app.MemoryProposal
	Transition     *TransitionResult
	Warnings       []string
}
