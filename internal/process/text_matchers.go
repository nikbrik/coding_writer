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
	lower := strings.ToLower(text)
	if strings.Contains(lower, "no tool evidence") || strings.Contains(lower, "without tool evidence") || strings.Contains(lower, "missing tool evidence") {
		return false
	}
	return strings.Contains(lower, "tool evidence:") || strings.Contains(lower, "trusted tool evidence:")
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
