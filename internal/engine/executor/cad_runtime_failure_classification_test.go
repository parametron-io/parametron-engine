package executor

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"parametron/internal/engine/adapter"
	"parametron/internal/engine/adapter/freecad"
	"parametron/internal/engine/artifact"
	"parametron/internal/engine/cadruntime"
	"parametron/internal/engine/verification"
)

// The executor is the boundary where adapter-native (FreeCAD) failure
// vocabulary is translated into the Engine-owned semantic class/stage that the
// adapter-neutral record mapper consumes.

func strPtr(s string) *string { return &s }

func nativeRuntimeFailureRun(failure *freecad.FreeCADRuntimeResultFailure) func(*adapter.CADRuntimeOrchestrationRequest, *cadruntime.FreeCADRuntimeVerifiedRun) {
	return func(_ *adapter.CADRuntimeOrchestrationRequest, r *cadruntime.FreeCADRuntimeVerifiedRun) {
		r.Verification = nil
		r.Runtime.Result = &freecad.FreeCADRuntimeResult{Status: freecad.FreeCADRuntimeResultStatusFailed, Failure: failure}
	}
}

func nativeRuntimeFailureError() error {
	runErr := &cadruntime.FreeCADRuntimeRunError{Stage: cadruntime.FreeCADRuntimeRunStageRuntimeReportedFailure, Err: errors.New("exit")}
	return &cadruntime.FreeCADRuntimeVerificationError{Stage: cadruntime.FreeCADRuntimeVerificationStageRuntimeExecution, AttemptID: "attempt-12", Err: runErr}
}

func consumeNativeFailure(t *testing.T, failure *freecad.FreeCADRuntimeResultFailure) *CADRuntimeFailureOutcome {
	t.Helper()
	e, err := consumeTask12(t, nativeRuntimeFailureRun(failure), nativeRuntimeFailureError())
	var consumptionErr *CADRuntimeConsumptionError
	if !errors.As(err, &consumptionErr) || consumptionErr.Stage != CADRuntimeConsumptionStageRuntimeFailure {
		t.Fatalf("err = %v, want runtime_failure consumption error", err)
	}
	outcome := e.CADRuntimeOutcome()
	if outcome == nil || outcome.Failure == nil {
		t.Fatalf("outcome = %#v, want failure", outcome)
	}
	return outcome.Failure
}

func TestCADRuntimeFailureClassification_CurrentFreeCADVocabulary(t *testing.T) {
	type row struct {
		name            string
		category, code  string
		stage           *string
		wantClass       string
		wantSemanticStg string
	}
	rows := []row{
		// validation
		{"category arguments", "arguments", "x", nil, "validation", "validation"},
		{"code invalid_arguments", "runtime", "invalid_arguments", nil, "validation", "validation"},
		{"stage argument_validation", "runtime", "x", strPtr("argument_validation"), "validation", "validation"},
		{"stage manifest_loading", "runtime", "x", strPtr("manifest_loading"), "validation", "validation"},
		{"stage manifest_compatibility", "runtime", "x", strPtr("manifest_compatibility"), "validation", "validation"},
		{"stage manifest_validation", "runtime", "x", strPtr("manifest_validation"), "validation", "validation"},
		{"stage source_document_resolution", "runtime", "x", strPtr("source_document_resolution"), "validation", "validation"},
		// adapter
		{"category freecad_unavailable", "freecad_unavailable", "x", nil, "adapter", "adapter"},
		{"code freecad_unavailable", "runtime", "freecad_unavailable", nil, "adapter", "adapter"},
		{"stage freecad_resolution", "runtime", "x", strPtr("freecad_resolution"), "adapter", "adapter"},
		{"stage document_open", "runtime", "x", strPtr("document_open"), "adapter", "adapter"},
		// export
		{"stage artifact_export", "runtime", "x", strPtr("artifact_export"), "export", "export"},
		// observation / traversal
		{"category observation", "observation", "x", nil, "adapter", "adapter"},
		{"code observation_failure", "runtime", "observation_failure", nil, "adapter", "adapter"},
		{"stage observation", "runtime", "x", strPtr("observation"), "adapter", "adapter"},
		{"stage observation_output", "runtime", "x", strPtr("observation_output"), "adapter", "adapter"},
		{"stage reference_traversal", "runtime", "x", strPtr("reference_traversal"), "adapter", "adapter"},
		{"stage reference_traversal_output_containment", "runtime", "x", strPtr("reference_traversal_output_containment"), "adapter", "adapter"},
		{"stage reference_traversal_output_write", "runtime", "x", strPtr("reference_traversal_output_write"), "adapter", "adapter"},
		// runtime
		{"stage parameter_assignment", "runtime", "x", strPtr("parameter_assignment"), "runtime", "runtime"},
		{"stage recompute", "runtime", "x", strPtr("recompute"), "runtime", "runtime"},
		{"stage document_save", "runtime", "x", strPtr("document_save"), "runtime", "runtime"},
		{"stage result_write", "runtime", "x", strPtr("result_write"), "runtime", "runtime"},
		{"stage unknown", "runtime", "x", strPtr("unknown"), "runtime", "runtime"},
		{"absent stage", "runtime", "x", nil, "runtime", "runtime"},
		{"unknown future stage", "runtime", "x", strPtr("future_stage_v9"), "runtime", "runtime"},
		{"unknown future category and code", "future_category", "future_code", strPtr("future_stage_v9"), "runtime", "runtime"},
		{"broad execution category", "execution", "x", strPtr("execute"), "runtime", "runtime"},
		// precedence: the first matching group wins, in the order
		// validation > adapter > export > observation.
		{"export stage beats broad execution category", "execution", "x", strPtr("artifact_export"), "export", "export"},
		{"export stage beats observation category", "observation", "x", strPtr("artifact_export"), "export", "export"},
		{"export stage beats observation code", "runtime", "observation_failure", strPtr("artifact_export"), "export", "export"},
		{"validation category beats export stage", "arguments", "x", strPtr("artifact_export"), "validation", "validation"},
		{"validation stage beats freecad_unavailable category", "freecad_unavailable", "x", strPtr("manifest_loading"), "validation", "validation"},
		{"adapter category beats export stage", "freecad_unavailable", "x", strPtr("artifact_export"), "adapter", "adapter"},
		{"document_open stage beats observation category", "observation", "x", strPtr("document_open"), "adapter", "adapter"},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			got := consumeNativeFailure(t, &freecad.FreeCADRuntimeResultFailure{
				Boundary: "freecad", Category: tc.category, Code: tc.code, Message: "native message", Stage: tc.stage,
			})
			if !got.RuntimeNative {
				t.Fatal("aligned native failure not marked runtime-native")
			}
			if got.Class != tc.wantClass || got.SemanticStage != tc.wantSemanticStg {
				t.Fatalf("class/stage = %q/%q, want %q/%q", got.Class, got.SemanticStage, tc.wantClass, tc.wantSemanticStg)
			}
			// Native material is carried through unchanged for raw-evidence
			// correlation and operator errors.
			wantStage := ""
			if tc.stage != nil {
				wantStage = *tc.stage
			}
			if got.Boundary != "freecad" || got.Category != tc.category || got.Code != tc.code ||
				got.Stage != wantStage || got.Message != "native message" {
				t.Fatalf("native fields not preserved: %#v", got)
			}
		})
	}
}

func TestCADRuntimeFailureClassification_IsDeterministic(t *testing.T) {
	failure := &freecad.FreeCADRuntimeResultFailure{Boundary: "freecad", Category: "observation", Code: "c", Message: "m", Stage: strPtr("artifact_export")}
	first := *consumeNativeFailure(t, failure)
	for i := 0; i < 5; i++ {
		if got := *consumeNativeFailure(t, failure); got != first {
			t.Fatalf("classification changed between identical inputs: %#v vs %#v", got, first)
		}
	}
}

func TestClassifyFreeCADRuntimeFailure_NilFallsBackToRuntime(t *testing.T) {
	if class, stage := classifyFreeCADRuntimeFailure(nil); class != "runtime" || stage != "runtime" {
		t.Fatalf("nil failure = %q/%q, want runtime/runtime", class, stage)
	}
}

// Only a validated aligned status=failed result with a native failure receives
// the runtime-native marker.
func TestCADRuntimeFailureRuntimeNativeMarkerEligibility(t *testing.T) {
	nativeFailure := func() *freecad.FreeCADRuntimeResultFailure {
		return &freecad.FreeCADRuntimeResultFailure{Boundary: "freecad", Category: "runtime", Code: "boom", Message: "boom", Stage: strPtr("recompute")}
	}
	runtimeExec := func(stage string) error {
		runErr := &cadruntime.FreeCADRuntimeRunError{Stage: stage, Err: errors.New(stage)}
		return &cadruntime.FreeCADRuntimeVerificationError{Stage: cadruntime.FreeCADRuntimeVerificationStageRuntimeExecution, Err: runErr}
	}

	t.Run("eligible native failure", func(t *testing.T) {
		e, _ := consumeTask12(t, nativeRuntimeFailureRun(nativeFailure()), nativeRuntimeFailureError())
		if f := e.CADRuntimeOutcome().Failure; f == nil || !f.RuntimeNative || f.Class == "" || f.SemanticStage == "" {
			t.Fatalf("failure = %#v", f)
		}
	})

	ineligible := []struct {
		name   string
		mutate func(*adapter.CADRuntimeOrchestrationRequest, *cadruntime.FreeCADRuntimeVerifiedRun)
		err    error
	}{
		{"verification mismatch after successful runtime", func(_ *adapter.CADRuntimeOrchestrationRequest, r *cadruntime.FreeCADRuntimeVerifiedRun) {
			r.Verification = &verification.Result{Status: verification.StatusFail, Failure: verification.FailureClassParameterMismatch, Message: "mismatch"}
		}, &cadruntime.FreeCADRuntimeVerificationError{
			Stage: cadruntime.FreeCADRuntimeVerificationStageVerificationComparison, FailureClass: verification.FailureClassParameterMismatch,
			Err: &verification.VerifyError{Class: verification.FailureClassParameterMismatch, Message: "mismatch"},
		}},
		{"verification result failure", nil, &cadruntime.FreeCADRuntimeVerificationError{
			Stage: cadruntime.FreeCADRuntimeVerificationStageVerificationResult, Err: errors.New("inconsistent"),
		}},
		{"invalid observed evidence", func(_ *adapter.CADRuntimeOrchestrationRequest, r *cadruntime.FreeCADRuntimeVerifiedRun) {
			r.Verification, r.Runtime.Result = nil, nil
		}, runtimeExec(cadruntime.FreeCADRuntimeRunStageObservedLoad)},
		{"missing result", func(_ *adapter.CADRuntimeOrchestrationRequest, r *cadruntime.FreeCADRuntimeVerifiedRun) {
			r.Verification, r.Runtime.Result = nil, nil
		}, runtimeExec(cadruntime.FreeCADRuntimeRunStageResultLoad)},
		{"malformed result", func(_ *adapter.CADRuntimeOrchestrationRequest, r *cadruntime.FreeCADRuntimeVerifiedRun) {
			r.Verification, r.Runtime.Result = nil, nil
		}, runtimeExec(cadruntime.FreeCADRuntimeRunStageProcessResultCorrelation)},
		{"process invocation failure", func(_ *adapter.CADRuntimeOrchestrationRequest, r *cadruntime.FreeCADRuntimeVerifiedRun) {
			r.Verification, r.Runtime.Result = nil, nil
		}, runtimeExec(cadruntime.FreeCADRuntimeRunStageProcessInvocation)},
		{"failed status without failure body", func(_ *adapter.CADRuntimeOrchestrationRequest, r *cadruntime.FreeCADRuntimeVerifiedRun) {
			r.Verification = nil
			r.Runtime.Result = &freecad.FreeCADRuntimeResult{Status: freecad.FreeCADRuntimeResultStatusFailed}
		}, runtimeExec(cadruntime.FreeCADRuntimeRunStageRuntimeReportedFailure)},
		{"failure body on non-failed status", func(_ *adapter.CADRuntimeOrchestrationRequest, r *cadruntime.FreeCADRuntimeVerifiedRun) {
			r.Verification = nil
			r.Runtime.Result = &freecad.FreeCADRuntimeResult{Status: freecad.FreeCADRuntimeResultStatusSucceeded, Failure: nativeFailure()}
		}, runtimeExec(cadruntime.FreeCADRuntimeRunStageResultLoad)},
		{"non-native orchestration failure", nil, errors.New("adapter transport down")},
	}
	for _, tc := range ineligible {
		t.Run(tc.name, func(t *testing.T) {
			e, err := consumeTask12(t, tc.mutate, tc.err)
			if err == nil {
				t.Fatal("expected a consumption error")
			}
			outcome := e.CADRuntimeOutcome()
			if outcome != nil && outcome.Failure != nil && (outcome.Failure.RuntimeNative || outcome.Failure.Class != "" || outcome.Failure.SemanticStage != "") {
				t.Fatalf("ineligible failure carries runtime-native semantics: %#v", outcome.Failure)
			}
		})
	}
}

// Retries: only the terminal attempt's outcome is exposed, so the normalized
// failure always comes from the final attempt, never an earlier one.
func TestCADRuntimeFailure_TerminalAttemptOutcomeWinsAfterRetries(t *testing.T) {
	root := t.TempDir()
	attemptFailure := func(attempt int) *freecad.FreeCADRuntimeResultFailure {
		switch attempt {
		case 1:
			return &freecad.FreeCADRuntimeResultFailure{Boundary: "freecad", Category: "arguments", Code: "first_failure", Message: "first", Stage: strPtr("argument_validation")}
		case 2:
			return &freecad.FreeCADRuntimeResultFailure{Boundary: "freecad", Category: "runtime", Code: "second_failure", Message: "second", Stage: strPtr("artifact_export")}
		default:
			return &freecad.FreeCADRuntimeResultFailure{Boundary: "freecad", Category: "runtime", Code: "terminal_failure", Message: "terminal", Stage: strPtr("recompute")}
		}
	}
	a := &task12Adapter{onCall: func(req adapter.CADRuntimeOrchestrationRequest) (cadruntime.FreeCADRuntimeVerifiedRun, bool, error) {
		run := task12RunForRequest(root, req)
		nativeRuntimeFailureRun(attemptFailure(req.Attempt))(nil, &run)
		return run, true, nativeRuntimeFailureError()
	}}
	e, pkg := task12Execution(t, a)
	err := e.ExecutePackage(context.Background(), pkg)
	var execErr *ExecutionError
	if !errors.As(err, &execErr) || execErr.RetryCount != len(a.requests) || len(a.requests) != 3 {
		t.Fatalf("err = %v, requests = %d", err, len(a.requests))
	}
	outcome := e.CADRuntimeOutcome()
	if outcome == nil || outcome.Attempt != 3 || outcome.AttemptID != "attempt-3" || outcome.Failure == nil {
		t.Fatalf("outcome = %#v", outcome)
	}
	f := outcome.Failure
	if !f.RuntimeNative || f.Code != "terminal_failure" || f.Message != "terminal" || f.Class != "runtime" || f.SemanticStage != "runtime" {
		t.Fatalf("terminal failure = %#v, want the third attempt's runtime failure", f)
	}
	// The result path is the terminal attempt's own working copy.
	if want := filepath.Join(root, "3"); !strings.HasPrefix(outcome.ResultPath, want+string(filepath.Separator)) {
		t.Fatalf("ResultPath = %q, want under %q", outcome.ResultPath, want)
	}
	if e.ArtifactAcceptanceContext().FinalOutcome != artifact.FinalOutcomeFailed {
		t.Fatalf("acceptance = %#v", e.ArtifactAcceptanceContext())
	}
}
