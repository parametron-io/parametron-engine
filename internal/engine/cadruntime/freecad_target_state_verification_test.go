package cadruntime

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/adapter"
	"parametron/internal/engine/adapter/freecad"
	"parametron/internal/engine/observed"
	"parametron/internal/engine/runtimecap"
	"parametron/internal/engine/verification"
)

// Issue #18: runtime-boundary integration of target-state verification. The
// fake capability plays the external runtime; no FreeCAD is involved.

func tsRuntimeRequest(t *testing.T) adapter.CADRuntimeOrchestrationRequest {
	t.Helper()
	req := verify11NoParamRequest(t)
	req.Manifest.AssemblyMutations = &planner.ExportManifestMutationCollection{
		Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Sketch", Suppressed: true}},
		Deletion:    []planner.ExportManifestDeletionMutation{{Object: "Gone"}},
	}
	req.Manifest.PartMutations = &planner.ExportManifestMutationCollection{
		Visibility: []planner.ExportManifestVisibilityMutation{{Object: "Body", Visible: false}},
	}
	return req
}

func tsTrue() *bool  { v := true; return &v }
func tsFalse() *bool { v := false; return &v }

// tsPassEvidence is evidence that satisfies tsRuntimeRequest's mutation intent.
func tsPassEvidence() *observed.TargetStateObservation {
	return &observed.TargetStateObservation{
		Suppression: []observed.BooleanTargetEvidence{{Destination: "assembly", Object: "Sketch", Status: observed.BooleanEvidenceStatusObserved, Value: tsTrue()}},
		Visibility:  []observed.BooleanTargetEvidence{{Destination: "part", Object: "Body", Status: observed.BooleanEvidenceStatusObserved, Value: tsFalse()}},
		Existence:   []observed.ExistenceTargetEvidence{{Destination: "assembly", Object: "Gone", Status: observed.ExistenceEvidenceStatusAbsent}},
	}
}

func tsBuild(req adapter.CADRuntimeOrchestrationRequest, layout freecad.FreeCADRuntimeWorkingCopyLayout, mutate func(*observed.TargetStateObservation)) verify11Build {
	return func(t *testing.T, got runtimecap.Request, digest string) *observed.Observed {
		obs := verify11BuildPassObserved(t, req, layout, got, digest)
		evidence := tsPassEvidence()
		if mutate != nil {
			mutate(evidence)
		}
		obs.Observation.TargetState = evidence
		return obs
	}
}

func TestInvokeAndVerifyFreeCADRuntime_TargetStatePasses(t *testing.T) {
	req := tsRuntimeRequest(t)
	layout := task10Layout(t, req)
	fake := verify11Capability(t, tsBuild(req, layout, nil))

	run, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
	if err != nil {
		t.Fatalf("InvokeAndVerifyFreeCADRuntime: %v", err)
	}
	if run.Verification == nil || run.Verification.Status != verification.StatusPass || !run.Verification.Categories.TargetState.Enabled ||
		run.Verification.Categories.TargetState.Status != verification.CategoryStatusPass {
		t.Fatalf("verification=%#v", run.Verification)
	}
	expected := run.Runtime.ObservationRequest.Contract.Expected.TargetState
	if expected == nil || len(expected.Suppression) != 1 || !expected.Suppression[0].Value ||
		len(expected.Visibility) != 1 || expected.Visibility[0].Value || len(expected.Existence) != 1 {
		t.Fatalf("expected target state=%+v", expected)
	}
	if fake.calls != 1 {
		t.Fatalf("calls=%d", fake.calls)
	}
}

func TestInvokeAndVerifyFreeCADRuntime_TargetStateFailuresAreVerificationFailures(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*observed.TargetStateObservation)
		wantClass verification.FailureClass
		sentinel  error
	}{
		{"suppression mismatch", func(e *observed.TargetStateObservation) { e.Suppression[0].Value = tsFalse() },
			verification.FailureClassTargetStateMismatch, verification.ErrTargetStateMismatch},
		{"visibility mismatch", func(e *observed.TargetStateObservation) { e.Visibility[0].Value = tsTrue() },
			verification.FailureClassTargetStateMismatch, verification.ErrTargetStateMismatch},
		{"existence exists", func(e *observed.TargetStateObservation) { e.Existence[0].Status = observed.ExistenceEvidenceStatusExists },
			verification.FailureClassTargetStateMismatch, verification.ErrTargetStateMismatch},
		{"target missing", func(e *observed.TargetStateObservation) {
			e.Suppression[0] = observed.BooleanTargetEvidence{Destination: "assembly", Object: "Sketch", Status: observed.BooleanEvidenceStatusTargetMissing}
		}, verification.FailureClassTargetMissing, verification.ErrTargetMissing},
		{"native evidence unavailable", func(e *observed.TargetStateObservation) {
			e.Existence[0].Status = observed.ExistenceEvidenceStatusUnavailable
		}, verification.FailureClassNativeEvidenceUnavailable, verification.ErrNativeEvidenceUnavailable},
		{"required entry omitted", func(e *observed.TargetStateObservation) { e.Visibility = []observed.BooleanTargetEvidence{} },
			verification.FailureClassRequiredObservationMissing, verification.ErrRequiredObservationMissing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tsRuntimeRequest(t)
			layout := task10Layout(t, req)
			fake := verify11Capability(t, tsBuild(req, layout, tt.mutate))

			run, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
			verr := requireTask11Error(t, err, FreeCADRuntimeVerificationStageVerificationComparison)
			if verr.FailureClass != tt.wantClass || !errors.Is(err, tt.sentinel) {
				t.Fatalf("class=%q err=%v", verr.FailureClass, err)
			}
			if want := run.Runtime.ObservationRequest.Manifest.Attempt.Identity.ID; want == "" || verr.AttemptID != want {
				t.Fatalf("attempt id=%q want %q", verr.AttemptID, want)
			}
			var typed *verification.VerifyError
			if !errors.As(err, &typed) || typed.Class != tt.wantClass {
				t.Fatalf("cause=%v", err)
			}
			// It stays a verification failure; it is not a CAD-native runtime failure.
			var runErr *FreeCADRuntimeRunError
			if errors.As(err, &runErr) {
				t.Fatalf("target-state failure reclassified as a runtime failure: %v", runErr)
			}
			if verr.Stage == FreeCADRuntimeVerificationStageRuntimeExecution {
				t.Fatalf("stage=%q", verr.Stage)
			}
			for _, other := range []error{verification.ErrObservedInvalid, verification.ErrContractInvalid} {
				if errors.Is(err, other) {
					t.Fatalf("matches %v", other)
				}
			}
			if run.Verification == nil || run.Verification.Status != verification.StatusFail || run.Verification.Failure != tt.wantClass ||
				run.Verification.Categories.TargetState.Status != verification.CategoryStatusFail {
				t.Fatalf("verification=%#v", run.Verification)
			}
		})
	}
}

func TestInvokeAndVerifyFreeCADRuntime_ObservedEvidenceDecidesNotRequestedIntent(t *testing.T) {
	req := tsRuntimeRequest(t)
	if !req.Manifest.AssemblyMutations.Suppression[0].Suppressed || req.Manifest.PartMutations.Visibility[0].Visible {
		t.Fatal("precondition: intent is suppressed=true and visible=false")
	}
	layout := task10Layout(t, req)
	// Runtime reports the opposite of both requests.
	fake := verify11Capability(t, tsBuild(req, layout, func(e *observed.TargetStateObservation) {
		e.Suppression[0].Value = tsFalse()
		e.Visibility[0].Value = tsTrue()
	}))
	_, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
	verr := requireTask11Error(t, err, FreeCADRuntimeVerificationStageVerificationComparison)
	if verr.FailureClass != verification.FailureClassTargetStateMismatch {
		t.Fatalf("class=%q", verr.FailureClass)
	}
}

func TestInvokeAndVerifyFreeCADRuntime_RuntimeFailureShortCircuitsTargetStateVerification(t *testing.T) {
	req := tsRuntimeRequest(t)
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		return &runtimecap.Result{}, nil // no result.json
	}}
	run, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
	verr := requireTask11Error(t, err, FreeCADRuntimeVerificationStageRuntimeExecution)
	if verr.FailureClass != verification.FailureClassNone {
		t.Fatalf("runtime failure carries verification class %q", verr.FailureClass)
	}
	for _, sentinel := range []error{verification.ErrTargetStateMismatch, verification.ErrTargetMissing, verification.ErrNativeEvidenceUnavailable, verification.ErrRequiredObservationMissing} {
		if errors.Is(err, sentinel) {
			t.Fatalf("runtime failure surfaced as %v", sentinel)
		}
	}
	if run.Verification != nil {
		t.Fatalf("semantic verification ran despite runtime failure: %#v", run.Verification)
	}
	if run.Runtime.ObservationRequest.Contract.Expected.TargetState == nil {
		t.Fatal("expected target state must remain attached to the retained request state")
	}
}

func TestVerifyFreeCADRuntimeRun_TargetStateDoesNotRewriteRawEvidence(t *testing.T) {
	req := tsRuntimeRequest(t)
	layout := task10Layout(t, req)
	fake := verify11Capability(t, tsBuild(req, layout, func(e *observed.TargetStateObservation) { e.Suppression[0].Value = tsFalse() }))
	runtimeRun, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	if err != nil {
		t.Fatal(err)
	}
	observedBefore, err := os.ReadFile(runtimeRun.ObservedPath)
	if err != nil {
		t.Fatal(err)
	}
	requestBefore := append([]byte(nil), runtimeRun.ObservationRequest.JSON...)
	observedCanonicalBefore, err := observed.CanonicalJSON(runtimeRun.Observed)
	if err != nil {
		t.Fatal(err)
	}
	contractCanonicalBefore, err := verification.CanonicalJSON(&runtimeRun.ObservationRequest.Contract)
	if err != nil {
		t.Fatal(err)
	}
	expectedBefore := copyVerificationContract(runtimeRun.ObservationRequest.Contract).Expected.TargetState

	if _, err := verifyFreeCADRuntimeRun(runtimeRun); err == nil {
		t.Fatal("expected verification failure")
	}
	observedAfter, err := os.ReadFile(runtimeRun.ObservedPath)
	if err != nil || string(observedBefore) != string(observedAfter) {
		t.Fatalf("raw observed file changed: %v", err)
	}
	observedCanonicalAfter, err := observed.CanonicalJSON(runtimeRun.Observed)
	if err != nil || string(observedCanonicalBefore) != string(observedCanonicalAfter) {
		t.Fatalf("in-memory observed evidence rewritten: %v", err)
	}
	contractCanonicalAfter, err := verification.CanonicalJSON(&runtimeRun.ObservationRequest.Contract)
	if err != nil || string(contractCanonicalBefore) != string(contractCanonicalAfter) ||
		!reflect.DeepEqual(expectedBefore, runtimeRun.ObservationRequest.Contract.Expected.TargetState) ||
		string(requestBefore) != string(runtimeRun.ObservationRequest.JSON) {
		t.Fatal("in-memory contract/request rewritten by verification")
	}
	if *runtimeRun.Observed.Observation.TargetState.Suppression[0].Value != false {
		t.Fatal("observed evidence was normalized toward the requested value")
	}
}

func TestFreeCADRuntimeVerification_TargetStateResultConsistency(t *testing.T) {
	req := tsRuntimeRequest(t)
	layout := task10Layout(t, req)
	baseRun := verify11ValidRuntimeRun(t, req, layout)

	pass := func(target verification.CategoryResult) *verification.Result {
		return &verification.Result{Status: verification.StatusPass, Message: "verification passed", Categories: verification.CategoryResults{
			Components:  verification.CategoryResult{Status: verification.CategoryStatusSkipped},
			Parameters:  verification.CategoryResult{Status: verification.CategoryStatusSkipped},
			Metadata:    verification.CategoryResult{Enabled: true, Status: verification.CategoryStatusPass},
			References:  verification.CategoryResult{Enabled: true, Status: verification.CategoryStatusPass},
			TargetState: target,
		}}
	}

	t.Run("valid target-state pass is accepted", func(t *testing.T) {
		verified, err := validateFreeCADRuntimeVerificationResult(baseRun, pass(verification.CategoryResult{Enabled: true, Status: verification.CategoryStatusPass}), nil)
		if err != nil || verified.Verification == nil || verified.Verification.Status != verification.StatusPass {
			t.Fatalf("verified=%#v err=%v", verified.Verification, err)
		}
	})
	t.Run("disabled target-state category is ignored", func(t *testing.T) {
		if _, err := validateFreeCADRuntimeVerificationResult(baseRun, pass(verification.CategoryResult{Status: verification.CategoryStatusSkipped}), nil); err != nil {
			t.Fatal(err)
		}
	})
	for _, status := range []verification.CategoryStatus{verification.CategoryStatusFail, verification.CategoryStatusSkipped} {
		t.Run("pass with enabled target-state category "+string(status)+" is rejected", func(t *testing.T) {
			_, err := validateFreeCADRuntimeVerificationResult(baseRun, pass(verification.CategoryResult{Enabled: true, Status: status}), nil)
			requireTask11Error(t, err, FreeCADRuntimeVerificationStageVerificationResult)
		})
	}

	classes := []struct {
		class    verification.FailureClass
		sentinel error
	}{
		{verification.FailureClassTargetStateMismatch, verification.ErrTargetStateMismatch},
		{verification.FailureClassTargetMissing, verification.ErrTargetMissing},
		{verification.FailureClassNativeEvidenceUnavailable, verification.ErrNativeEvidenceUnavailable},
	}
	for _, tt := range classes {
		t.Run("fail "+string(tt.class), func(t *testing.T) {
			verifyErr := &verification.VerifyError{Class: tt.class, Message: "target"}
			result := &verification.Result{Status: verification.StatusFail, Failure: tt.class, Message: "target"}
			verified, err := validateFreeCADRuntimeVerificationResult(baseRun, result, verifyErr)
			verr := requireTask11Error(t, err, FreeCADRuntimeVerificationStageVerificationComparison)
			if verr.FailureClass != tt.class || !errors.Is(err, tt.sentinel) || verified.Verification == nil || verified.Verification.Failure != tt.class {
				t.Fatalf("verr=%+v err=%v", verr, err)
			}

			// A result whose class differs from the typed error is an inconsistency, not a target-state failure.
			mismatched := &verification.Result{Status: verification.StatusFail, Failure: verification.FailureClassTargetStateMismatch, Message: "x"}
			other := &verification.VerifyError{Class: verification.FailureClassTargetMissing, Message: "x"}
			if tt.class == verification.FailureClassTargetMissing {
				other = &verification.VerifyError{Class: verification.FailureClassNativeEvidenceUnavailable, Message: "x"}
			}
			_, err = validateFreeCADRuntimeVerificationResult(baseRun, mismatched, other)
			requireTask11Error(t, err, FreeCADRuntimeVerificationStageVerificationResult)
		})
	}
}

// ===================== Expected-state deep copy =====================

func tsExpectedContract() verification.Contract {
	return verification.Contract{
		Observe: verification.Observe{TargetState: true},
		ObservationContext: verification.ObservationContext{TargetState: &verification.TargetStateRequest{
			Suppression: []verification.TargetIdentity{{Destination: "assembly", Object: "S"}},
			Visibility:  []verification.TargetIdentity{{Destination: "part", Object: "V"}},
			Existence:   []verification.TargetIdentity{{Destination: "part", Object: "E"}},
		}},
		Expected: verification.Expected{TargetState: &verification.ExpectedTargetState{
			Suppression: []verification.ExpectedBooleanTargetState{{Destination: "assembly", Object: "S", Value: true}},
			Visibility:  []verification.ExpectedBooleanTargetState{{Destination: "part", Object: "V", Value: false}},
			Existence:   []verification.ExpectedExistenceTargetState{{Destination: "part", Object: "E", Status: observed.ExistenceEvidenceStatusAbsent}},
		}},
	}
}

func TestCopyVerificationContract_ExpectedTargetStateDoesNotAlias(t *testing.T) {
	src := tsExpectedContract()
	copied := copyVerificationContract(src)
	if copied.Expected.TargetState == src.Expected.TargetState {
		t.Fatal("expected target-state pointer must be copied")
	}
	if !reflect.DeepEqual(copied.Expected.TargetState, src.Expected.TargetState) {
		t.Fatalf("copy differs: %+v vs %+v", copied.Expected.TargetState, src.Expected.TargetState)
	}

	// Mutating the copy must not change the source.
	copied.Expected.TargetState.Suppression[0].Value = false
	copied.Expected.TargetState.Suppression[0].Object = "mutated"
	copied.Expected.TargetState.Visibility[0].Value = true
	copied.Expected.TargetState.Existence[0].Status = observed.ExistenceEvidenceStatusExists
	copied.Expected.TargetState.Existence = append(copied.Expected.TargetState.Existence, verification.ExpectedExistenceTargetState{})
	want := tsExpectedContract().Expected.TargetState
	if !reflect.DeepEqual(src.Expected.TargetState, want) {
		t.Fatalf("copy mutation leaked into source: %+v", src.Expected.TargetState)
	}

	// Mutating the source must not change a fresh copy.
	fresh := copyVerificationContract(src)
	src.Expected.TargetState.Suppression[0].Value = false
	src.Expected.TargetState.Visibility[0].Object = "src-mutated"
	src.Expected.TargetState.Existence[0].Destination = "assembly"
	if !reflect.DeepEqual(fresh.Expected.TargetState, want) {
		t.Fatalf("source mutation leaked into copy: %+v", fresh.Expected.TargetState)
	}
}

func TestCopyVerificationContract_ExpectedTargetStateAbsentStaysAbsent(t *testing.T) {
	if got := copyVerificationContract(verification.Contract{}); got.Expected.TargetState != nil {
		t.Fatalf("copy fabricated expected target state: %+v", got.Expected.TargetState)
	}
}

func TestDeepCopyVerifiedRun_ExpectedTargetStateIsIndependent(t *testing.T) {
	run := FreeCADRuntimeVerifiedRun{}
	run.Runtime.ObservationRequest.Contract = tsExpectedContract()
	copied := deepCopyVerifiedRun(run)

	copied.Runtime.ObservationRequest.Contract.Expected.TargetState.Suppression[0].Value = false
	copied.Runtime.ObservationRequest.Contract.Expected.TargetState.Visibility[0].Value = true
	copied.Runtime.ObservationRequest.Contract.Expected.TargetState.Existence[0].Object = "mutated"
	if !reflect.DeepEqual(run.Runtime.ObservationRequest.Contract.Expected.TargetState, tsExpectedContract().Expected.TargetState) {
		t.Fatalf("copy mutation leaked into stored run: %+v", run.Runtime.ObservationRequest.Contract.Expected.TargetState)
	}

	run.Runtime.ObservationRequest.Contract.Expected.TargetState.Suppression[0].Object = "stored-mutated"
	if copied.Runtime.ObservationRequest.Contract.Expected.TargetState.Suppression[0].Object != "S" {
		t.Fatal("stored-run mutation leaked into the copy")
	}
}

func TestInvokeAndVerifyFreeCADRuntime_ReturnedRunDoesNotAliasExpectedTargetState(t *testing.T) {
	req := tsRuntimeRequest(t)
	layout := task10Layout(t, req)
	fake := verify11Capability(t, tsBuild(req, layout, nil))
	first, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := *first.Runtime.ObservationRequest.Contract.Expected.TargetState
	snapshot.Suppression = append([]verification.ExpectedBooleanTargetState(nil), snapshot.Suppression...)

	// A second invocation with the same request must be unaffected by mutation of the first result.
	first.Runtime.ObservationRequest.Contract.Expected.TargetState.Suppression[0].Value = false
	second, err := InvokeAndVerifyFreeCADRuntime(context.Background(), verify11Capability(t, tsBuild(req, layout, nil)), req)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(second.Runtime.ObservationRequest.Contract.Expected.TargetState.Suppression, snapshot.Suppression) {
		t.Fatalf("expected state shared across invocations: %+v", second.Runtime.ObservationRequest.Contract.Expected.TargetState)
	}
}
