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
	if expectedAction == app.ExpectedUserConfirmation && isConfirmation(normalized) {
		if stage == app.StagePlanning || stage == app.StageExecution || stage == app.StageValidation {
			return ActionProposeTransition
		}
	}

	switch stage {
	case app.StagePlanning:
		if isPlanningIntent(normalized) {
			return ActionPlanTask
		}
		if looksLikeClarification(normalized) {
			return ActionAnswerQuestion
		}
		if containsAny(normalized, []string{"готов", "ready", "execute", "реализуй", "implement", "proceed", "продолжай", "continue"}) {
			return ActionProposeTransition
		}
		if containsAny(normalized, []string{"уточни", "clarify", "ask clarification", "open question"}) {
			return ActionAskClarification
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
		if looksLikeClarification(normalized) {
			return ActionAnswerQuestion
		}
		return ActionReviewOutput
	case app.StageDone:
		if containsAny(normalized, []string{"summary", "summarize", "итог", "резюме", "final summary", "what was done"}) {
			return ActionSummarizeDone
		}
		if containsAny(normalized, []string{"реализуй", "implement", "execute", "edit", "change", "fix", "доделай", "add", "update", "write", "create", "delete", "remove", "make", "build", "создай", "добавь", "измени", "обнови", "удали", "исправь"}) {
			return ActionExecutePlanStep
		}
		if looksLikeClarification(normalized) {
			return ActionAnswerQuestion
		}
		return ActionExecutePlanStep
	default:
		return ActionAnswerQuestion
	}
}

func isPlanningIntent(normalized string) bool {
	if containsAny(normalized, []string{"спланируй", "спланировать", "plan the", "please plan"}) {
		return true
	}
	return strings.HasPrefix(normalized, "plan ") || normalized == "plan"
}

func looksLikeClarification(normalized string) bool {
	return containsAny(normalized, []string{"?", "что", "как", "почему", "какой", "какие", "объясни", "расскажи", "what", "how", "why", "which", "explain"})
}

func isConfirmation(normalized string) bool {
	replacer := strings.NewReplacer(".", " ", ",", " ", "!", " ", "?", " ", ";", " ", ":", " ", "\n", " ", "\t", " ")
	for _, token := range strings.Fields(replacer.Replace(normalized)) {
		switch token {
		case "yes", "y", "approve", "approved", "confirm", "confirmed", "да", "ок", "подтверждаю", "согласен":
			return true
		}
	}
	return false
}

func containsAny(s string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}
