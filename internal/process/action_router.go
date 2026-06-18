package process

import (
	"strings"

	"github.com/nikbrik/coding_writer/internal/app"
)

// ResolveActionKind maps user input and current stage to a deterministic ActionKind.
func ResolveActionKind(input string, stage app.TaskStage, expectedAction app.ExpectedAction) ActionKind {
	normalized := strings.ToLower(strings.TrimSpace(input))
	if stage == "" {
		if isPlanningIntent(normalized) {
			return ActionPlanTask
		}
		return ActionAnswerQuestion
	}
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
		if isPlanningIntent(normalized) {
			return ActionPlanTask
		}
		if containsAny(normalized, []string{"продолжай", "continue", "выполняй", "execute next", "next step"}) {
			return ActionExecutePlanStep
		}
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
		if containsAny(normalized, []string{"summary", "summarize", "итог", "резюме", "final summary", "what was done", "status", "what changed", "что изменилось"}) {
			return ActionSummarizeDone
		}
		if isInformationalQuestion(normalized) {
			return ActionAnswerQuestion
		}
		if containsDoneMutationIntent(normalized) {
			return ActionExecutePlanStep
		}
		if looksLikeClarification(normalized) {
			return ActionAnswerQuestion
		}
		return ActionAnswerQuestion
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

func isInformationalQuestion(normalized string) bool {
	tokens := words(normalized)
	if len(tokens) == 0 {
		return false
	}
	switch tokens[0] {
	case "what", "how", "why", "which", "что", "как", "почему", "какой", "какие":
		return true
	}
	return false
}

func isConfirmation(normalized string) bool {
	for _, token := range words(normalized) {
		switch token {
		case "yes", "y", "approve", "approved", "confirm", "confirmed", "да", "ок", "подтверждаю", "согласен":
			return true
		}
	}
	return false
}

func containsDoneMutationIntent(normalized string) bool {
	if strings.Contains(normalized, "continue work") {
		return true
	}
	tokens := words(normalized)
	for i, token := range tokens {
		if token == "update" && i+1 < len(tokens) && tokens[i+1] == "me" {
			continue
		}
		switch token {
		case "реализуй", "implement", "execute", "edit", "change", "fix", "доделай", "доработай", "add", "update", "write", "create", "delete", "remove", "make", "build", "refactor", "rename", "modify", "создай", "добавь", "измени", "обнови", "удали", "исправь":
			return true
		}
	}
	return false
}

func words(s string) []string {
	replacer := strings.NewReplacer(".", " ", ",", " ", "!", " ", "?", " ", ";", " ", ":", " ", "\n", " ", "\t", " ", "(", " ", ")", " ", "[", " ", "]", " ")
	return strings.Fields(replacer.Replace(s))
}

func containsAny(s string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}
