package app

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

type ErrorCategory string

const (
	CategoryInternal   ErrorCategory = "internal"
	CategoryCLI        ErrorCategory = "cli"
	CategoryValidation ErrorCategory = "validation"
	CategoryStorage    ErrorCategory = "storage"
	CategoryProvider   ErrorCategory = "provider"
	CategoryClassifier ErrorCategory = "classifier"
)

type Error struct {
	Category   ErrorCategory        `json:"category"`
	Code       string               `json:"code"`
	Message    string               `json:"message"`
	Hint       string               `json:"hint,omitempty"`
	Path       string               `json:"path,omitempty"`
	Violations []InvariantViolation `json:"violations,omitempty"`
	Err        error                `json:"-"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Path != "" {
		return fmt.Sprintf("%s: %s: %s", e.Category, e.Code, redactHomePath(e.Path))
	}
	return fmt.Sprintf("%s: %s: %s", e.Category, e.Code, e.Message)
}

func redactHomePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}

func (e *Error) Unwrap() error { return e.Err }

func NewError(category ErrorCategory, code, message string, err error) *Error {
	return &Error{Category: category, Code: code, Message: message, Err: err}
}

func ErrorWithHint(category ErrorCategory, code, message, hint string, err error) *Error {
	return &Error{Category: category, Code: code, Message: message, Hint: hint, Err: err}
}

func AsError(err error) *Error {
	if err == nil {
		return nil
	}
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr
	}
	return &Error{Category: CategoryInternal, Code: "unexpected", Message: err.Error(), Err: err}
}

func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	switch AsError(err).Category {
	case CategoryCLI:
		return 2
	case CategoryValidation:
		return 3
	case CategoryStorage:
		return 4
	case CategoryProvider:
		return 5
	case CategoryClassifier:
		return 6
	default:
		return 1
	}
}
