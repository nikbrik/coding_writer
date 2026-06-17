package process

import (
	"time"

	"github.com/nikbrik/coding_writer/internal/app"
)

// ProcessAuditEvent matches the epic audit schema.
type ProcessAuditEvent struct {
	ID               string        `json:"id"`
	TaskID           string        `json:"task_id"`
	SessionID        string        `json:"session_id"`
	Stage            app.TaskStage `json:"stage"`
	ActionKind       ActionKind    `json:"action_kind"`
	Decision         string        `json:"decision"`
	ValidatorErrors  []string      `json:"validator_errors,omitempty"`
	ErrorCategory    string        `json:"error_category,omitempty"`
	ErrorCode        string        `json:"error_code,omitempty"`
	Reason           string        `json:"reason,omitempty"`
	RetryCount       int           `json:"retry_count,omitempty"`
	PromptPolicyID   string        `json:"prompt_policy_id,omitempty"`
	TransitionFrom   string        `json:"transition_from,omitempty"`
	TransitionTo     string        `json:"transition_to,omitempty"`
	TransitionReason string        `json:"transition_reason,omitempty"`
	Model            string        `json:"model,omitempty"`
	CreatedAt        time.Time     `json:"created_at"`
}
