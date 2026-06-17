package process

import "strings"

func normalizedSeverity(severity string) string {
	return strings.ToLower(strings.TrimSpace(severity))
}

func isBlockerOrHighSeverity(severity string) bool {
	s := normalizedSeverity(severity)
	return s == "blocker" || s == "high"
}

func isKnownFindingSeverity(severity string) bool {
	switch normalizedSeverity(severity) {
	case "blocker", "high", "medium", "low":
		return true
	default:
		return false
	}
}
