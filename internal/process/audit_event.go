package process

import (
	"time"

	"github.com/nikbrik/coding_writer/internal/app"
)

// ProcessAuditEvent matches the epic audit schema.
type ProcessAuditEvent struct {
	ID              string        `json:"id"`
	TaskID          string        `json:"task_id"`
	SessionID       string        `json:"session_id"`
	Stage           app.TaskStage `json:"stage"`
	ActionKind      ActionKind    `json:"action_kind"`
	Decision        string        `json:"decision"`
	ValidatorErrors []string      `json:"validator_errors,omitempty"`
	TransitionFrom  string        `json:"transition_from,omitempty"`
	TransitionTo    string        `json:"transition_to,omitempty"`
	Model           string        `json:"model,omitempty"`
	CreatedAt       time.Time     `json:"created_at"`
}
