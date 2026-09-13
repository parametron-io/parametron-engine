package executor

import "fmt"

// ExecutionError describes a structured runtime execution failure.
type ExecutionError struct {
	ProductID  string
	StepID     string
	Err        error
	RetryCount int
	Timeout    bool
	Canceled   bool
}

// Error returns a human-readable execution error message.
func (e *ExecutionError) Error() string {
	return fmt.Sprintf("product %s step %s failed: %v", e.ProductID, e.StepID, e.Err)
}

// Unwrap returns the underlying cause.
func (e *ExecutionError) Unwrap() error {
	return e.Err
}
