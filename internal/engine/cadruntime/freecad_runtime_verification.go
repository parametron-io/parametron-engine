package cadruntime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"parametron/internal/engine/adapter"
	"parametron/internal/engine/runtimecap"
	"parametron/internal/engine/verification"
)

const (
	FreeCADRuntimeVerificationStageRuntimeExecution       = "runtime_execution"
	FreeCADRuntimeVerificationStageVerificationComparison = "verification_comparison"
	FreeCADRuntimeVerificationStageVerificationResult     = "verification_result"
)

// FreeCADRuntimeVerifiedRun retains the complete raw Task 10 state and the
// Engine-owned expected-versus-observed verification result.
type FreeCADRuntimeVerifiedRun struct {
	Runtime      FreeCADRuntimeRun
	Verification *verification.Result
}

// FreeCADRuntimeVerificationError identifies whether aligned runtime execution,
// verification comparison, or verification result validation failed.
type FreeCADRuntimeVerificationError struct {
	Stage        string
	AttemptID    string
	FailureClass verification.FailureClass
	Err          error
}

func (e *FreeCADRuntimeVerificationError) Error() string {
	if e == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("freecad runtime verification")
	if e.Stage != "" {
		b.WriteString(": ")
		b.WriteString(e.Stage)
	}
	if e.AttemptID != "" {
		b.WriteString(": attempt=")
		b.WriteString(e.AttemptID)
	}
	if e.FailureClass != verification.FailureClassNone {
		b.WriteString(": failure=")
		b.WriteString(string(e.FailureClass))
	}
	if e.Err != nil {
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

func (e *FreeCADRuntimeVerificationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// InvokeAndVerifyFreeCADRuntime invokes and validates the aligned runtime once,
// then compares Task 9's in-memory contract with Task 10's in-memory observed
// state.
func InvokeAndVerifyFreeCADRuntime(
	ctx context.Context,
	capability runtimecap.Capability,
	req adapter.CADRuntimeOrchestrationRequest,
) (FreeCADRuntimeVerifiedRun, error) {
	runtimeRun, err := InvokeAndValidateFreeCADRuntime(ctx, capability, req)
	if err != nil {
		return FreeCADRuntimeVerifiedRun{Runtime: runtimeRun}, &FreeCADRuntimeVerificationError{
			Stage:     FreeCADRuntimeVerificationStageRuntimeExecution,
			AttemptID: freeCADRuntimeVerificationAttemptID(runtimeRun),
			Err:       err,
		}
	}
	return verifyFreeCADRuntimeRun(runtimeRun)
}

// verifyFreeCADRuntimeRun performs only the Engine-owned in-memory comparison.
func verifyFreeCADRuntimeRun(runtimeRun FreeCADRuntimeRun) (FreeCADRuntimeVerifiedRun, error) {
	result, err := verification.Verify(
		&runtimeRun.ObservationRequest.Contract,
		runtimeRun.Observed,
	)
	return validateFreeCADRuntimeVerificationResult(runtimeRun, result, err)
}

func validateFreeCADRuntimeVerificationResult(
	runtimeRun FreeCADRuntimeRun,
	result *verification.Result,
	verifyErr error,
) (FreeCADRuntimeVerifiedRun, error) {
	verified := FreeCADRuntimeVerifiedRun{
		Runtime:      runtimeRun,
		Verification: copyFreeCADRuntimeVerificationResult(result),
	}
	attemptID := freeCADRuntimeVerificationAttemptID(runtimeRun)

	if verifyErr == nil {
		switch {
		case result == nil:
			return verified, freeCADRuntimeVerificationResultError(attemptID, nil,
				errors.New("verification authority returned a nil result without an error"))
		case result.Status != verification.StatusPass:
			return verified, freeCADRuntimeVerificationResultError(attemptID, nil,
				fmt.Errorf("verification authority returned status %q without an error", result.Status))
		case result.Failure != verification.FailureClassNone:
			return verified, freeCADRuntimeVerificationResultError(attemptID, nil,
				fmt.Errorf("verification authority returned pass with failure class %q", result.Failure))
		case !freeCADRuntimeVerificationCategoriesPassed(result.Categories):
			return verified, freeCADRuntimeVerificationResultError(attemptID, nil,
				errors.New("verification authority returned pass with an enabled category not passed"))
		default:
			return verified, nil
		}
	}

	var typedErr *verification.VerifyError
	if !errors.As(verifyErr, &typedErr) {
		return verified, freeCADRuntimeVerificationResultError(attemptID, verifyErr,
			errors.New("verification authority returned a non-VerifyError comparison error"))
	}

	switch {
	case result == nil:
		return verified, freeCADRuntimeVerificationResultError(attemptID, verifyErr,
			errors.New("verification authority returned a verification error without a result"))
	case result.Status != verification.StatusFail:
		return verified, freeCADRuntimeVerificationResultError(attemptID, verifyErr,
			fmt.Errorf("verification authority returned status %q with a verification error", result.Status))
	case result.Failure != typedErr.Class:
		return verified, freeCADRuntimeVerificationResultError(attemptID, verifyErr,
			fmt.Errorf("verification result failure %q differs from verification error class %q", result.Failure, typedErr.Class))
	default:
		return verified, &FreeCADRuntimeVerificationError{
			Stage:        FreeCADRuntimeVerificationStageVerificationComparison,
			AttemptID:    attemptID,
			FailureClass: typedErr.Class,
			Err:          verifyErr,
		}
	}
}

func copyFreeCADRuntimeVerificationResult(result *verification.Result) *verification.Result {
	if result == nil {
		return nil
	}
	copied := *result
	return &copied
}

func freeCADRuntimeVerificationAttemptID(runtimeRun FreeCADRuntimeRun) string {
	return runtimeRun.ObservationRequest.Manifest.Attempt.Identity.ID
}

func freeCADRuntimeVerificationCategoriesPassed(categories verification.CategoryResults) bool {
	for _, category := range []verification.CategoryResult{
		categories.Components,
		categories.Parameters,
		categories.Metadata,
		categories.References,
		categories.TargetState,
	} {
		if category.Enabled && category.Status != verification.CategoryStatusPass {
			return false
		}
	}
	return true
}

func freeCADRuntimeVerificationResultError(
	attemptID string,
	cause error,
	inconsistency error,
) *FreeCADRuntimeVerificationError {
	err := inconsistency
	if cause != nil {
		err = errors.Join(cause, inconsistency)
	}
	return &FreeCADRuntimeVerificationError{
		Stage:     FreeCADRuntimeVerificationStageVerificationResult,
		AttemptID: attemptID,
		Err:       err,
	}
}
