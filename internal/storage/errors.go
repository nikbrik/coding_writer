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
	detail := ""
	if e.Err != nil {
		detail = ": " + e.Err.Error()
	}
	if e.Path != "" {
		return fmt.Sprintf("storage %s: %s%s", e.Code, e.Path, detail)
	}
	return fmt.Sprintf("storage %s%s", e.Code, detail)
}

func (e *Error) Unwrap() error { return e.Err }

func errStorage(code, path string, err error) error {
	return &Error{Code: code, Path: path, Err: err}
}
