package runtimecap

import "fmt"

const (
	ErrorInvalid   = "invalid_invocation"
	ErrorStart     = "start_failure"
	ErrorExit      = "non_zero_exit"
	ErrorCancelled = "cancelled"
)

// Error is a typed runtime capability invocation failure.
type Error struct {
	Kind     string
	Message  string
	ExitCode int
	Stdout   string
	Stderr   string
	Err      error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Err)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
