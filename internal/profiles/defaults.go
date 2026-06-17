package profiles

import (
	"time"

	"github.com/nikbrik/coding_writer/internal/app"
)

func DefaultProfiles(now time.Time) []app.UserProfile {
	return []app.UserProfile{
		{
			ID:          "student",
			DisplayName: "Student profile",
			Style: map[string]string{
				"language":        "ru",
				"detail":          "high",
				"tone":            "teacher",
				"prefer_steps":    "true",
				"prefer_examples": "true",
			},
			ResponseFormat: map[string]string{
				"structure": "step-by-step",
				"examples":  "include",
			},
			Constraints: []string{"explain terms", "show reasoning"},
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:          "senior",
			DisplayName: "Senior engineer profile",
			Style: map[string]string{
				"language":         "ru",
				"detail":           "low",
				"tone":             "direct",
				"prefer_steps":     "false",
				"prefer_tradeoffs": "true",
			},
			ResponseFormat: map[string]string{
				"structure": "concise",
				"focus":     "risks and decisions",
			},
			Constraints: []string{"be concise", "focus risks and decisions"},
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}
}
