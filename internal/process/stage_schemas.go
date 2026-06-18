package process

// PlanningOutput is the JSON schema for the planning stage.
type PlanningOutput struct {
	Stage              string   `json:"stage"`
	Summary            string   `json:"summary"`
	Assumptions        []string `json:"assumptions"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	Plan               []string `json:"plan"`
	OpenQuestions      []string `json:"open_questions"`
	Readiness          string   `json:"readiness"`
}

// ExecutionOutput is the JSON schema for the execution stage.
type ExecutionOutput struct {
	Stage            string   `json:"stage"`
	Summary          string   `json:"summary"`
	CurrentStep      string   `json:"current_step,omitempty"`
	CompletedSteps   []string `json:"completed_steps,omitempty"`
	NextStep         string   `json:"next_step,omitempty"`
	ChangedArtifacts []string `json:"changed_artifacts"`
	Verification     []string `json:"verification"`
	Blockers         []string `json:"blockers"`
	NextSignal       string   `json:"next_signal"`
}

// ValidationFinding is a single finding inside ValidationOutput.
type ValidationFinding struct {
	Severity string `json:"severity"`
	Location string `json:"location"`
	Problem  string `json:"problem"`
	Fix      string `json:"fix"`
}

// ValidationOutput is the JSON schema for the validation stage.
type ValidationOutput struct {
	Stage           string              `json:"stage"`
	Findings        []ValidationFinding `json:"findings"`
	PassedChecks    []string            `json:"passed_checks"`
	MissingEvidence []string            `json:"missing_evidence"`
	ResidualRisks   []string            `json:"residual_risks"`
	Verdict         string              `json:"verdict"`
}

// DoneOutput is the JSON schema for the done stage.
type DoneOutput struct {
	Stage                 string   `json:"stage"`
	Summary               string   `json:"summary"`
	AcceptanceStatus      []string `json:"acceptance_status"`
	ValidationEvidence    []string `json:"validation_evidence"`
	FollowUpTaskProposals []string `json:"follow_up_task_proposals"`
}
