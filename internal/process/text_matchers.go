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
	fields, ok := structuredTrustedEvidenceFields(item)
	if !ok {
		return false
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

func structuredTrustedEvidenceFields(item string) (map[string]string, bool) {
	item = strings.TrimSpace(item)
	if !strings.HasPrefix(item, trustedEvidencePrefix) {
		return nil, false
	}
	fields := map[string]string{}
	for _, part := range strings.Split(strings.TrimPrefix(item, trustedEvidencePrefix), ";") {
		key, value, ok := strings.Cut(part, "=")
		if !ok || strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			return nil, false
		}
		fields[key] = value
	}
	return fields, true
}

func trustedEvidenceSatisfiesAcceptanceCriteria(criteria []string, evidence []string) bool {
	if !hasTrustedEvidence(evidence) {
		return false
	}
	requiredSources := requiredTrustedEvidenceSources(criteria)
	if len(requiredSources) == 0 {
		return true
	}
	sources := trustedEvidenceSources(evidence)
	for _, required := range requiredSources {
		if !hasEvidenceSourcePrefix(sources, required) {
			return false
		}
	}
	return true
}

func requiredTrustedEvidenceSources(criteria []string) []string {
	required := map[string]bool{}
	for _, criterion := range criteria {
		lower := strings.ToLower(criterion)
		switch {
		case strings.Contains(lower, "go test") || strings.Contains(lower, "tests pass") || strings.Contains(lower, "tests passed") || strings.Contains(lower, "тесты"):
			required["go test"] = true
		case strings.Contains(lower, "go vet"):
			required["go vet"] = true
		case strings.Contains(lower, "git diff --check"):
			required["git diff --check"] = true
		}
	}
	out := make([]string, 0, len(required))
	for source := range required {
		out = append(out, source)
	}
	return out
}

func trustedEvidenceSources(evidence []string) []string {
	var sources []string
	for _, item := range evidence {
		if !isStructuredTrustedEvidence(item) {
			continue
		}
		fields, _ := structuredTrustedEvidenceFields(item)
		sources = append(sources, strings.ToLower(strings.TrimSpace(fields["source"])))
	}
	return sources
}

func hasEvidenceSourcePrefix(sources []string, required string) bool {
	for _, source := range sources {
		if source == required || strings.HasPrefix(source, required+" ") {
			return true
		}
	}
	return false
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
