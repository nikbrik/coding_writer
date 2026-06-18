package app

import "time"

type ChatRole string

const (
	RoleSystem    ChatRole = "system"
	RoleUser      ChatRole = "user"
	RoleAssistant ChatRole = "assistant"
)

type ChatMessage struct {
	ID        string    `json:"id,omitempty"`
	Role      ChatRole  `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type MemoryLayer string

const (
	LayerShort MemoryLayer = "short"
	LayerWork  MemoryLayer = "work"
	LayerLong  MemoryLayer = "long"
)

type MemoryRecord struct {
	ID               string      `json:"id"`
	Layer            MemoryLayer `json:"layer"`
	Kind             string      `json:"kind"`
	Content          string      `json:"content"`
	Source           string      `json:"source"`
	Scope            string      `json:"scope,omitempty"`
	ProfileID        string      `json:"profile_id,omitempty"`
	UserID           string      `json:"user_id,omitempty"`
	Tags             []string    `json:"tags,omitempty"`
	TaskID           string      `json:"task_id,omitempty"`
	SessionID        string      `json:"session_id,omitempty"`
	ProposalID       string      `json:"proposal_id,omitempty"`
	ProposalRecordID string      `json:"proposal_record_id,omitempty"`
	CreatedAt        time.Time   `json:"created_at"`
}

type ProposedMemoryLayer string

const (
	ProposedLayerShort  ProposedMemoryLayer = "short"
	ProposedLayerWork   ProposedMemoryLayer = "work"
	ProposedLayerLong   ProposedMemoryLayer = "long"
	ProposedLayerIgnore ProposedMemoryLayer = "ignore"
)

type ProposalRecordStatus string

const (
	ProposalPending  ProposalRecordStatus = "pending"
	ProposalAccepted ProposalRecordStatus = "accepted"
	ProposalEdited   ProposalRecordStatus = "edited"
	ProposalRejected ProposalRecordStatus = "rejected"
	ProposalBlocked  ProposalRecordStatus = "blocked"
)

type MemoryProposal struct {
	ID               string                 `json:"id"`
	SessionID        string                 `json:"session_id,omitempty"`
	SourceMessageIDs []string               `json:"source_message_ids"`
	Records          []ProposedMemoryRecord `json:"records"`
	Provider         string                 `json:"provider,omitempty"`
	Model            string                 `json:"model,omitempty"`
	TemplateHash     string                 `json:"template_hash,omitempty"`
	CreatedAt        time.Time              `json:"created_at"`
}

type ProposedMemoryRecord struct {
	ID             string               `json:"id"`
	Layer          ProposedMemoryLayer  `json:"layer"`
	Kind           string               `json:"kind"`
	Content        string               `json:"content"`
	Scope          string               `json:"scope,omitempty"`
	ProfileID      string               `json:"profile_id,omitempty"`
	UserID         string               `json:"user_id,omitempty"`
	Reason         string               `json:"reason"`
	Confidence     float64              `json:"confidence"`
	Status         ProposalRecordStatus `json:"status"`
	BlockReason    string               `json:"block_reason,omitempty"`
	AppliedLayer   ProposedMemoryLayer  `json:"applied_layer,omitempty"`
	AppliedContent string               `json:"applied_content,omitempty"`
	SavedRecordID  string               `json:"saved_record_id,omitempty"`
	AppliedAt      *time.Time           `json:"applied_at,omitempty"`
}

type UserProfile struct {
	ID             string            `json:"id"`
	DisplayName    string            `json:"display_name"`
	Style          map[string]string `json:"style"`
	ResponseFormat map[string]string `json:"response_format"`
	Constraints    []string          `json:"constraints"`
	DefaultModel   string            `json:"default_model,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

type TaskStage string

const (
	StagePlanning   TaskStage = "planning"
	StageExecution  TaskStage = "execution"
	StageValidation TaskStage = "validation"
	StageDone       TaskStage = "done"
)

type TaskStatus string

const (
	TaskStatusActive TaskStatus = "active"
	TaskStatusPaused TaskStatus = "paused"
)

type ExpectedAction string

const (
	ExpectedUserInput        ExpectedAction = "user_input"
	ExpectedLLMResponse      ExpectedAction = "llm_response"
	ExpectedUserConfirmation ExpectedAction = "user_confirmation"
	ExpectedNone             ExpectedAction = "none"
)

type TaskState struct {
	ID                 string                 `json:"id"`
	Title              string                 `json:"title"`
	Stage              TaskStage              `json:"stage"`
	CurrentStep        string                 `json:"current_step"`
	CompletedSteps     []string               `json:"completed_steps,omitempty"`
	ExpectedAction     ExpectedAction         `json:"expected_action"`
	Status             TaskStatus             `json:"status"`
	Objective          string                 `json:"objective"`
	AcceptanceCriteria []string               `json:"acceptance_criteria"`
	Plan               []string               `json:"plan"`
	Decisions          []string               `json:"decisions"`
	OpenQuestions      []string               `json:"open_questions"`
	PendingPlanning    *PlanningProposalState `json:"pending_planning,omitempty"`
	LastSessionID      string                 `json:"last_session_id,omitempty"`
	ValidationStatus   string                 `json:"validation_status,omitempty"`
	HistoryLog         []string               `json:"history_log,omitempty"`
	PausedAt           *time.Time             `json:"paused_at,omitempty"`
	ResumedAt          *time.Time             `json:"resumed_at,omitempty"`
	UpdatedAt          time.Time              `json:"updated_at"`
}

type PlanningProposalState struct {
	ID                 string    `json:"id"`
	Summary            string    `json:"summary"`
	AcceptanceCriteria []string  `json:"acceptance_criteria"`
	Plan               []string  `json:"plan"`
	OpenQuestions      []string  `json:"open_questions"`
	CreatedAt          time.Time `json:"created_at"`
}

type AppConfig struct {
	ActiveProfileID           string   `json:"active_profile_id,omitempty"`
	ActiveModel               string   `json:"active_model,omitempty"`
	StorageDir                string   `json:"storage_dir"`
	OpenRouterBaseURL         string   `json:"openrouter_base_url"`
	TrustedOpenRouterBaseURLs []string `json:"trusted_openrouter_base_urls,omitempty"`
	MemoryModel               string   `json:"memory_model,omitempty"`
}

type MemoryBundle struct {
	Short []MemoryRecord `json:"short"`
	Work  []MemoryRecord `json:"work"`
	Long  []MemoryRecord `json:"long"`
}
