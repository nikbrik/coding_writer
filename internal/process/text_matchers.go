package process

import "strings"

func containsImplementationClaim(text string) bool {
	lower := strings.ToLower(text)
	for _, needle := range []string{
		"implemented", "implementation completed", "changed file", "changed code", "fixed ", "fix applied", "patched", "wrote code", "updated file", "created file", "deleted file", "modified file", "diff --git", "+++ b/", "--- a/", "@@",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return strings.Contains(lower, "```")
}

func containsTestPassClaim(text string) bool {
	lower := strings.ToLower(text)
	for _, needle := range []string{"test passed", "tests passed", "all tests pass", "all tests passed", "passed tests"} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func isExplicitNotRun(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "not run") || strings.Contains(lower, "not executed") || strings.Contains(lower, "не запуск")
}

func hasTrustedToolEvidenceText(text string) bool {
	return false
}

func hasTrustedEvidence(evidence []string) bool {
	for _, item := range evidence {
		if strings.TrimSpace(item) != "" {
			return true
		}
	}
	return false
}

func containsSideEffectClaim(text string) bool {
	if containsImplementationClaim(text) {
		return true
	}
	lower := strings.ToLower(text)
	for _, needle := range []string{
		"i edited", "i changed", "i updated", "i created", "i deleted", "i wrote", "i saved", "i persisted", "i committed", "i ran", "i executed",
		"file edited", "file changed", "file updated", "memory saved", "state changed", "stage changed", "transitioned to", "applied patch", "ran tests", "tests passed", "all tests passed",
		"я изменил", "я обновил", "я создал", "я удалил", "я сохранил", "запустил тест", "тесты прошли", "перевёл задачу", "сменил stage",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func containsMutationCommand(text string) bool {
	lower := strings.ToLower(text)
	for _, needle := range []string{
		"please implement", "can you implement", "implement ", "edit file", "change file", "write file", "create file", "delete file", "update file", "modify config", "refactor ", "rename file",
		"реализуй", "измени", "создай", "удали", "исправь", "доделай",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}
