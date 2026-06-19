package process

import (
	"strings"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/validation"
)

// RetryController decides whether a validation failure is fixable and how many
// correction attempts remain.
type RetryController struct {
	MaxRetries int
}

func NewRetryController() *RetryController {
	return &RetryController{MaxRetries: 4}
}

// ShouldRetry returns true only for fixable schema/stage violations.
func (r *RetryController) ShouldRetry(err error) bool {
	if err == nil {
		return false
	}
	appErr := app.AsError(err)
	switch appErr.Code {
	case "invalid_json", "missing_stage":
		return true
	case "stage_mismatch":
		return false
	}
	// Hard gates and security blocks must not retry.
	if appErr.Category == app.CategoryValidation {
		switch appErr.Code {
		case "task_paused", "task_done", "forbidden_action", "secret_blocked", "missing_task", "invariant_conflict":
			return false
		}
	}
	return false
}

// CorrectionPrompt builds a trusted correction prompt from validator errors.
func (r *RetryController) CorrectionPrompt(validatorErrors []string) string {
	var b strings.Builder
	b.WriteString("Your previous output violated the trusted stage contract.\n")
	b.WriteString("Validator errors:\n")
	b.WriteString("<trusted_validator_errors>\n")
	for _, e := range validatorErrors {
		b.WriteString("- ")
		b.WriteString(validation.EscapeUntrusted(e))
		b.WriteString("\n")
	}
	b.WriteString("</trusted_validator_errors>\n")
	lower := strings.ToLower(strings.Join(validatorErrors, "\n"))
	if strings.Contains(lower, "ready_for_validation requires changed artifacts and verification evidence") || strings.Contains(lower, "ready_for_validation requires trusted evidence") {
		b.WriteString("Because trusted file/tool evidence was not provided, do not use next_signal=ready_for_validation. Set next_signal=continue_execution and provide the next code deliverable in a fenced code block or unified diff. Use deliverable format like \"### path/to/file.go\\n```go\\npackage name\\n...\\n```\". Keep changed_artifacts empty and verification=[\"not run\"].\n")
	}
	if strings.Contains(lower, "execution deliverable") {
		b.WriteString("For execution, the deliverable field must contain concrete code in a fenced code block or a unified diff, for example \"### path/to/file.go\\n```go\\npackage name\\n...\\n```\". Do not return only metadata.\n")
	}
	if strings.Contains(lower, "llm_validator:false_claim") || strings.Contains(lower, "llm_validator:unauthorized_claim") || strings.Contains(lower, "llm_validator:missing_trusted_evidence") || strings.Contains(lower, "llm_validator:no_trusted_evidence") {
		b.WriteString("Without trusted_evidence, describe the code as prepared/proposed in chat only. Do not claim files were created, changed, implemented, verified, or tested. Keep changed_artifacts empty and verification=[\"not run\"].\n")
	}
	b.WriteString("Regenerate the response using the required schema.\n")
	b.WriteString("Do not add new scope.")
	return b.String()
}
