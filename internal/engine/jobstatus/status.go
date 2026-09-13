package jobstatus

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"parametron/internal/engine/executor"
	"parametron/internal/engine/job"
)

const SchemaVersion = "1.0"

type State string

const (
	StateQueued    State = "queued"
	StateRunning   State = "running"
	StateSucceeded State = "succeeded"
	StateFailed    State = "failed"
	StateCanceled  State = "canceled"
)

var (
	ErrNilJob            = errors.New("job status job is nil")
	ErrMissingJobID      = errors.New("job status job id is missing")
	ErrMissingProductKey = errors.New("job status product key is missing")
	ErrMissingFailure    = errors.New("job status failure error is missing")
	errInvalidTimeOrder  = errors.New("job status timestamps are inconsistent")
)

type InvalidTransitionError struct {
	From State
	To   State
}

func (e *InvalidTransitionError) Error() string {
	return fmt.Sprintf("invalid job status transition from %q to %q", e.From, e.To)
}

type Failure struct {
	Message        string `json:"message"`
	ProductID      string `json:"productId"`
	StepID         string `json:"stepId,omitempty"`
	RetryCount     int    `json:"retryCount"`
	Timeout        bool   `json:"timeout"`
	Canceled       bool   `json:"canceled"`
	Classification string `json:"classification,omitempty"`
	Boundary       string `json:"boundary,omitempty"`
	Category       string `json:"category,omitempty"`
	Code           string `json:"code,omitempty"`
	Stage          string `json:"stage,omitempty"`
	AttemptID      string `json:"attemptId,omitempty"`
}

type Status struct {
	SchemaVersion string     `json:"schemaVersion"`
	JobID         string     `json:"jobId"`
	ProductKey    string     `json:"productKey"`
	State         State      `json:"state"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	StartedAt     *time.Time `json:"startedAt,omitempty"`
	EndedAt       *time.Time `json:"endedAt,omitempty"`
	Error         *Failure   `json:"error,omitempty"`
}

func New(j *job.Job, now time.Time) (*Status, error) {
	if j == nil {
		return nil, ErrNilJob
	}
	if strings.TrimSpace(j.ID) == "" {
		return nil, ErrMissingJobID
	}
	if strings.TrimSpace(j.ProductKey) == "" {
		return nil, ErrMissingProductKey
	}

	at := now.UTC()
	return &Status{
		SchemaVersion: SchemaVersion,
		JobID:         j.ID,
		ProductKey:    j.ProductKey,
		State:         StateQueued,
		CreatedAt:     at,
		UpdatedAt:     at,
	}, nil
}

func (s State) IsTerminal() bool {
	switch s {
	case StateSucceeded, StateFailed, StateCanceled:
		return true
	default:
		return false
	}
}

func (s *Status) Start(now time.Time) error {
	return s.transition(StateRunning, now, nil)
}

func (s *Status) Succeed(now time.Time) error {
	return s.transition(StateSucceeded, now, nil)
}

func (s *Status) Fail(now time.Time, err error) error {
	if err == nil {
		return ErrMissingFailure
	}
	return s.transition(StateFailed, now, FailureFromError(s.ProductKey, err))
}

func (s *Status) Cancel(now time.Time, err error) error {
	return s.transition(StateCanceled, now, CanceledFailureFromError(s.ProductKey, err))
}

func FailureFromError(productKey string, err error) *Failure {
	if err == nil {
		return nil
	}

	failure := &Failure{
		Message:   err.Error(),
		ProductID: productKey,
	}

	var executionErr *executor.ExecutionError
	if errors.As(err, &executionErr) {
		failure.Message = executionErr.Err.Error()
		failure.ProductID = executionErr.ProductID
		failure.StepID = executionErr.StepID
		failure.RetryCount = executionErr.RetryCount
		failure.Timeout = executionErr.Timeout
		failure.Canceled = executionErr.Canceled
	}
	var consumptionErr *executor.CADRuntimeConsumptionError
	if errors.As(err, &consumptionErr) {
		failure.Message = consumptionErr.Message
		if failure.Message == "" {
			failure.Message = consumptionErr.Error()
		}
		failure.Classification = consumptionErr.Classification
		failure.Boundary = consumptionErr.Boundary
		failure.Category = consumptionErr.Category
		failure.Code = consumptionErr.Code
		failure.Stage = consumptionErr.NativeStage
		failure.AttemptID = consumptionErr.AttemptID
	}

	if failure.ProductID == "" {
		failure.ProductID = productKey
	}

	return failure
}

func CanceledFailureFromError(productKey string, err error) *Failure {
	failure := FailureFromError(productKey, err)
	if failure == nil {
		failure = &Failure{
			Message:   "job canceled",
			ProductID: productKey,
		}
	}
	failure.Canceled = true
	if failure.ProductID == "" {
		failure.ProductID = productKey
	}
	return failure
}

func (s *Status) transition(next State, now time.Time, failure *Failure) error {
	if s == nil {
		return ErrNilJob
	}
	if !canTransition(s.State, next) {
		return &InvalidTransitionError{From: s.State, To: next}
	}

	at := now.UTC()
	if at.Before(s.CreatedAt) {
		return errInvalidTimeOrder
	}

	s.State = next
	s.UpdatedAt = at

	switch next {
	case StateRunning:
		s.StartedAt = timePtr(at)
		s.EndedAt = nil
		s.Error = nil
	case StateSucceeded:
		s.EndedAt = timePtr(at)
		s.Error = nil
	case StateFailed, StateCanceled:
		s.EndedAt = timePtr(at)
		s.Error = failure
	}

	return nil
}

func canTransition(from, to State) bool {
	switch from {
	case StateQueued:
		return to == StateRunning || to == StateCanceled
	case StateRunning:
		return to == StateSucceeded || to == StateFailed || to == StateCanceled
	default:
		return false
	}
}

func timePtr(value time.Time) *time.Time {
	utc := value.UTC()
	return &utc
}
