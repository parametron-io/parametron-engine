package executor

import "time"

// StepStatus represents the lifecycle state of an execution step.
type StepStatus string

const (
	StepPending StepStatus = "pending"
	StepRunning StepStatus = "running"
	StepSuccess StepStatus = "success"
	StepFailed  StepStatus = "failed"
)

// ExecutionState stores in-memory execution state for a single step.
type ExecutionState struct {
	ProductID string
	StepID    string
	Status    StepStatus
	StartedAt time.Time
	EndedAt   time.Time
	Attempts  int
}

// Clone returns a copy with timestamps normalized to UTC.
func (s ExecutionState) Clone() ExecutionState {
	s.StartedAt = s.StartedAt.UTC()
	s.EndedAt = s.EndedAt.UTC()
	return s
}
