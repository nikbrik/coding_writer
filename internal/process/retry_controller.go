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
	return &RetryController{MaxRetries: 2}
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
		case "task_paused", "task_done", "forbidden_action", "secret_blocked", "missing_task":
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
	b.WriteString("Regenerate the response using the required schema.\n")
	b.WriteString("Do not add new scope.")
	return b.String()
}
