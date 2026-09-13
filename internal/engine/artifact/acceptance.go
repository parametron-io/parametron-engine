package artifact

import (
	"errors"
	"fmt"
)

type FinalOutcome string

const (
	FinalOutcomeUnknown   FinalOutcome = "unknown"
	FinalOutcomeSucceeded FinalOutcome = "succeeded"
	FinalOutcomeFailed    FinalOutcome = "failed"
	FinalOutcomeCanceled  FinalOutcome = "canceled"
	FinalOutcomeTimedOut  FinalOutcome = "timed_out"
)

type VerificationOutcome string

const (
	VerificationOutcomeUnknown VerificationOutcome = "unknown"
	VerificationOutcomePassed  VerificationOutcome = "passed"
	VerificationOutcomeFailed  VerificationOutcome = "failed"
	VerificationOutcomeNotRun  VerificationOutcome = "not_run"
	VerificationOutcomeSkipped VerificationOutcome = "skipped"
)

type AcceptanceContext struct {
	FinalOutcome        FinalOutcome
	VerificationOutcome VerificationOutcome
}

var ErrArtifactAcceptanceRejected = errors.New("artifact acceptance rejected")

const (
	acceptanceReasonUnsupportedClass        = "artifact class is not supported"
	acceptanceReasonFinalOutcomeMustSucceed = `final runtime outcome must be "succeeded"`
	acceptanceReasonVerificationPassNeeded  = `explicit engine verification pass is required`
)

type AcceptanceError struct {
	Class  ArtifactClass
	Reason string
}

func (e *AcceptanceError) Error() string {
	if e == nil {
		return ErrArtifactAcceptanceRejected.Error()
	}
	return fmt.Sprintf("artifact registration rejected for class %q: %s", e.Class, e.Reason)
}

func (e *AcceptanceError) Unwrap() error {
	return ErrArtifactAcceptanceRejected
}

type BatchAcceptanceError struct {
	Index int
	Err   error
}

func (e *BatchAcceptanceError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return e.Err.Error()
}

func (e *BatchAcceptanceError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func AcceptForRegistration(item Artifact, ctx AcceptanceContext) error {
	ctx = normalizeAcceptanceContext(ctx)
	class := DefaultArtifactClass(item.Class)
	if !ValidArtifactClass(class) {
		return &AcceptanceError{
			Class:  class,
			Reason: acceptanceReasonUnsupportedClass,
		}
	}

	switch class {
	case ArtifactClassExecutionOutput:
		if ctx.FinalOutcome != FinalOutcomeSucceeded {
			return &AcceptanceError{
				Class:  class,
				Reason: acceptanceReasonFinalOutcomeMustSucceed,
			}
		}
		return nil
	case ArtifactClassVerified:
		if ctx.FinalOutcome != FinalOutcomeSucceeded {
			return &AcceptanceError{
				Class:  class,
				Reason: acceptanceReasonFinalOutcomeMustSucceed,
			}
		}
		if ctx.VerificationOutcome != VerificationOutcomePassed {
			return &AcceptanceError{
				Class:  class,
				Reason: acceptanceReasonVerificationPassNeeded,
			}
		}
		return nil
	default:
		return nil
	}
}

func AcceptBatchForRegistration(items []Artifact, ctx AcceptanceContext) error {
	for i := range items {
		if err := AcceptForRegistration(items[i], ctx); err != nil {
			return &BatchAcceptanceError{Index: i, Err: err}
		}
	}
	return nil
}

func normalizeAcceptanceContext(ctx AcceptanceContext) AcceptanceContext {
	if ctx.FinalOutcome == "" {
		ctx.FinalOutcome = FinalOutcomeUnknown
	}
	if ctx.VerificationOutcome == "" {
		ctx.VerificationOutcome = VerificationOutcomeUnknown
	}
	return ctx
}
