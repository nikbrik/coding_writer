package process

import (
	"strings"

	"github.com/nikbrik/coding_writer/internal/app"
)

// ResolveActionKind maps user input and current stage to a deterministic ActionKind.
func ResolveActionKind(input string, stage app.TaskStage, expectedAction app.ExpectedAction) ActionKind {
	if stage == "" {
		return ActionAnswerQuestion
	}
	normalized := strings.ToLower(strings.TrimSpace(input))

	switch stage {
	case app.StagePlanning:
		if looksLikeClarification(normalized) {
			return ActionAskClarification
		}
		if containsAny(normalized, []string{"спланируй", "plan", "спланировать", "planning", "plan the"}) {
			return ActionPlanTask
		}
		if containsAny(normalized, []string{"готов", "ready", "execute", "реализуй", "implement", "proceed"}) {
			return ActionProposeTransition
		}
		return ActionAnswerQuestion
	case app.StageExecution:
		if looksLikeClarification(normalized) {
			return ActionAnswerQuestion
		}
		if containsAny(normalized, []string{"summarize", "summary", "готово", "ready for validation", "проверь", "validate"}) {
			return ActionSummarizeExecution
		}
		return ActionExecutePlanStep
	case app.StageValidation:
		if containsAny(normalized, []string{"verify", "criteria", "проверь критерии"}) {
			return ActionVerifyCriteria
		}
		return ActionReviewOutput
	case app.StageDone:
		return ActionAnswerQuestion
	default:
		return ActionAnswerQuestion
	}
}

func looksLikeClarification(normalized string) bool {
	return containsAny(normalized, []string{"?", "что", "как", "почему", "какой", "какие", "what", "how", "why", "which", "explain"})
}

func containsAny(s string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}
