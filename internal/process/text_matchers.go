package process

import (
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
)

const trustedEvidencePrefix = "app:evidence:v1:"

func NewTrustedEvidence(source string, exitCode int, output string) string {
	source = strings.NewReplacer(";", "_", "=", "_", "\n", "_", "\r", "_").Replace(strings.TrimSpace(source))
	if source == "" {
		source = "tool"
	}
	digest := sha256.Sum256([]byte(output))
	return fmt.Sprintf("%ssource=%s;exit=%d;sha256=%x", trustedEvidencePrefix, source, exitCode, digest)
}

func containsImplementationClaim(text string) bool {
	lower := strings.ToLower(text)
	for _, needle := range []string{
		"implemented", "implementation completed", "changed file", "changed code", "fixed ", "fix applied", "patched", "wrote code", "updated file", "created file", "deleted file", "modified file", "diff --git", "+++ b/", "--- a/", "@@",
		"```go", "```diff", "```patch",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
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
		if isStructuredTrustedEvidence(item) {
			return true
		}
	}
	return false
}

func isStructuredTrustedEvidence(item string) bool {
	item = strings.TrimSpace(item)
	if !strings.HasPrefix(item, trustedEvidencePrefix) {
		return false
	}
	fields := map[string]string{}
	for _, part := range strings.Split(strings.TrimPrefix(item, trustedEvidencePrefix), ";") {
		key, value, ok := strings.Cut(part, "=")
		if !ok || strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			return false
		}
		fields[key] = value
	}
	if strings.TrimSpace(fields["source"]) == "" || len(fields["sha256"]) != 64 {
		return false
	}
	for _, r := range fields["sha256"] {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	exit, err := strconv.Atoi(fields["exit"])
	return err == nil && exit == 0
}

func containsSideEffectClaim(text string) bool {
	lower := strings.ToLower(text)
	for _, needle := range []string{
		"i edited", "i changed", "i updated", "i created", "i deleted", "i wrote", "i saved", "i persisted", "i committed", "i ran", "i executed",
		"updated file", "changed file", "created file", "deleted file", "modified file", "file edited", "file changed", "file updated", "memory saved", "state changed", "stage changed", "transitioned to", "applied patch", "ran tests", "tests passed", "all tests passed",
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
