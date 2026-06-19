package main

import "testing"

func TestIsValid(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"empty", "", true},
		{"single invalid char", "x", false},
		{"simple match", "()", true},
		{"multiple types", "()[]{}", true},
		{"mismatch", "(]", false},
		{"wrong order", "([)]", false},
		{"nested", "{[]}", true},
		{"unmatched open", "(((", false},
		{"unmatched close", "]", false},
		{"extra opening", "[", false},
		{"deeply nested", "((()))", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValid(tt.input); got != tt.expected {
				t.Errorf("IsValid(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}
