package process

import "strings"

func hasNonEmpty(items []string) bool {
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			return true
		}
	}
	return false
}

func hasAnyNonEmpty(items ...[]string) bool {
	for _, group := range items {
		if hasNonEmpty(group) {
			return true
		}
	}
	return false
}
