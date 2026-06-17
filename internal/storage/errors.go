package storage

import "fmt"

type Error struct {
	Code string
	Path string
	Err  error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Path != "" {
		return fmt.Sprintf("storage %s: %s", e.Code, e.Path)
	}
	return fmt.Sprintf("storage %s", e.Code)
}

func (e *Error) Unwrap() error { return e.Err }

func errStorage(code, path string, err error) error {
	return &Error{Code: code, Path: path, Err: err}
}
