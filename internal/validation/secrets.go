package validation

import (
	"regexp"
	"strings"
)

type SecretFinding struct {
	Type string `json:"type"`
}

var secretPatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	{"openrouter_api_key", regexp.MustCompile(`(?i)OPENROUTER_API_KEY\s*=\s*[^\s]+`)},
	{"bearer_token", regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._~+/=-]{12,}`)},
	{"sk_token", regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{12,}\b`)},
	{"password", regexp.MustCompile(`(?i)\b(password|passwd|token|secret|api[_-]?key)\s*[:=]\s*[^\s]+`)},
}

func DetectSecrets(text string) []SecretFinding {
	if text == "" {
		return nil
	}
	var findings []SecretFinding
	seen := map[string]bool{}
	for _, pattern := range secretPatterns {
		if pattern.re.MatchString(text) && !seen[pattern.name] {
			findings = append(findings, SecretFinding{Type: pattern.name})
			seen[pattern.name] = true
		}
	}
	return findings
}

func HasSecret(text string) bool { return len(DetectSecrets(text)) > 0 }

func RedactText(text string) (string, []SecretFinding) {
	findings := DetectSecrets(text)
	redacted := text
	for _, pattern := range secretPatterns {
		redacted = pattern.re.ReplaceAllString(redacted, "[REDACTED_SECRET]")
	}
	return redacted, findings
}

func FindingTypes(findings []SecretFinding) string {
	if len(findings) == 0 {
		return ""
	}
	parts := make([]string, 0, len(findings))
	for _, finding := range findings {
		parts = append(parts, finding.Type)
	}
	return strings.Join(parts, ",")
}
