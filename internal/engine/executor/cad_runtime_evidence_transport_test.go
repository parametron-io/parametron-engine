package executor

import (
	"reflect"
	"strings"
	"testing"

	"parametron/internal/engine/adapter"
	"parametron/internal/engine/artifact"
	"parametron/internal/engine/cadruntime"
	"parametron/internal/engine/observed"
	"parametron/internal/engine/verification"
)

// Issue #1: a verified CAD runtime run's already-interpreted typed values
// (observed evidence and the in-memory verification result) are carried on
// CADRuntimeOutcome so normal-run emission can map them.

func mustObservedValue(t *testing.T, raw string) observed.Value {
	t.Helper()
	value, err := observed.NewValue([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func transportObserved(t *testing.T) *observed.Observed {
	t.Helper()
	suppressed := true
	return &observed.Observed{
		SchemaVersion: observed.SchemaVersion,
		WorkingCopy:   observed.WorkingCopy{Path: "/work/widget.FCStd", SHA256: strings.Repeat("a", 64)},
		Observation: observed.Observation{
			Parameters: []observed.Parameter{{ID: "p1", Name: "width", GroupID: "g1", Value: mustObservedValue(t, `12.5`), ValueKind: "number"}},
			Metadata:   []observed.Metadata{{ID: "m1", Key: "revision", OwnerID: "c1", Value: mustObservedValue(t, `"B"`), ValueKind: "string"}},
			References: []observed.Reference{{Kind: "external", Name: "lib"}},
			Components: []observed.Component{{ID: "c1", Kind: observed.ComponentKindPart, Name: "Panel"}},
			TargetState: &observed.TargetStateObservation{
				Suppression: []observed.BooleanTargetEvidence{{Destination: "part", Object: "Sketch", Status: observed.BooleanEvidenceStatusObserved, Value: &suppressed}},
				Visibility:  []observed.BooleanTargetEvidence{},
				Existence:   []observed.ExistenceTargetEvidence{{Destination: "part", Object: "Old", Status: observed.ExistenceEvidenceStatusAbsent}},
			},
		},
	}
}

func consumeWithObserved(t *testing.T, obs *observed.Observed) *Executor {
	t.Helper()
	e, err := consumeTask12(t, func(_ *adapter.CADRuntimeOrchestrationRequest, r *cadruntime.FreeCADRuntimeVerifiedRun) {
		r.Runtime.Observed = obs
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestExecutorCADRuntimeConsumption_CarriesTypedObservedAndVerificationResult(t *testing.T) {
	obs := transportObserved(t)
	e := consumeWithObserved(t, obs)

	got := e.CADRuntimeOutcome()
	if got == nil || got.Verification != artifact.VerificationOutcomePassed {
		t.Fatalf("outcome = %#v, want a passed outcome", got)
	}
	if !reflect.DeepEqual(got.Observed, obs) {
		t.Fatalf("outcome observed = %#v, want %#v", got.Observed, obs)
	}
	want := &verification.Result{Status: verification.StatusPass, Failure: verification.FailureClassNone}
	if !reflect.DeepEqual(got.VerificationResult, want) {
		t.Fatalf("outcome verification result = %#v, want %#v", got.VerificationResult, want)
	}
}

func TestExecutorCADRuntimeConsumption_PreservesNonNilEmptyObservedCollections(t *testing.T) {
	// The observed contract requires every collection to be present; an empty
	// array must not collapse to nil (which would re-serialize as null).
	obs := &observed.Observed{
		SchemaVersion: observed.SchemaVersion,
		WorkingCopy:   observed.WorkingCopy{Path: "/work/widget.FCStd", SHA256: strings.Repeat("a", 64)},
		Observation: observed.Observation{
			Parameters: []observed.Parameter{}, Metadata: []observed.Metadata{},
			References: []observed.Reference{}, Components: []observed.Component{},
			TargetState: &observed.TargetStateObservation{
				Suppression: []observed.BooleanTargetEvidence{}, Visibility: []observed.BooleanTargetEvidence{}, Existence: []observed.ExistenceTargetEvidence{},
			},
		},
	}
	got := consumeWithObserved(t, obs).CADRuntimeOutcome().Observed
	if got == nil {
		t.Fatal("observed evidence was not carried")
	}
	for name, isNil := range map[string]bool{
		"parameters":              got.Observation.Parameters == nil,
		"metadata":                got.Observation.Metadata == nil,
		"references":              got.Observation.References == nil,
		"components":              got.Observation.Components == nil,
		"targetState.suppression": got.Observation.TargetState.Suppression == nil,
		"targetState.visibility":  got.Observation.TargetState.Visibility == nil,
		"targetState.existence":   got.Observation.TargetState.Existence == nil,
	} {
		if isNil {
			t.Errorf("%s collapsed from a non-nil empty collection to nil", name)
		}
	}
	if !reflect.DeepEqual(got, obs) {
		t.Fatalf("observed = %#v, want %#v", got, obs)
	}
}

func TestExecutorCADRuntimeConsumption_AbsentTargetStateAndObservedStayAbsent(t *testing.T) {
	obs := transportObserved(t)
	obs.Observation.TargetState = nil
	if got := consumeWithObserved(t, obs).CADRuntimeOutcome().Observed; got == nil || got.Observation.TargetState != nil {
		t.Fatalf("absent target-state evidence must stay nil, got %#v", got)
	}
	if got := consumeWithObserved(t, nil).CADRuntimeOutcome().Observed; got != nil {
		t.Fatalf("absent observed evidence must stay nil, got %#v", got)
	}
}

func TestExecutorCADRuntimeConsumption_TypedEvidenceIsIsolatedFromSourceAndReaders(t *testing.T) {
	obs := transportObserved(t)
	e := consumeWithObserved(t, obs)

	// Mutating the source after consumption does not reach the outcome.
	*obs.Observation.TargetState.Suppression[0].Value = false
	obs.Observation.Parameters[0].Name = "mutated"
	obs.Observation.References[0].Name = "mutated"
	first := e.CADRuntimeOutcome()
	if !*first.Observed.Observation.TargetState.Suppression[0].Value || first.Observed.Observation.Parameters[0].Name != "width" || first.Observed.Observation.References[0].Name != "lib" {
		t.Fatalf("outcome aliases the source observed value: %#v", first.Observed)
	}

	// Mutating a returned outcome does not reach the executor's stored state.
	*first.Observed.Observation.TargetState.Suppression[0].Value = false
	first.Observed.Observation.Components[0].Name = "mutated"
	first.VerificationResult.Message = "mutated"
	second := e.CADRuntimeOutcome()
	if !*second.Observed.Observation.TargetState.Suppression[0].Value || second.Observed.Observation.Components[0].Name != "Panel" || second.VerificationResult.Message != "" {
		t.Fatalf("CADRuntimeOutcome() shares state between calls: observed=%#v result=%#v", second.Observed, second.VerificationResult)
	}
}

// The aligned adapter hands the executor a deep copy of the verified run whose
// empty observed collections may have collapsed to nil. Every collection is
// required by the observed contract, so the outcome must restore non-nil empty
// collections that pass observed.Validate for downstream record mapping.
func TestExecutorCADRuntimeConsumption_NormalizesNilObservedCollectionsToValidEmpty(t *testing.T) {
	obs := &observed.Observed{
		SchemaVersion: observed.SchemaVersion,
		WorkingCopy:   observed.WorkingCopy{Path: "/work/widget.FCStd", SHA256: strings.Repeat("a", 64)},
		Observation:   observed.Observation{TargetState: &observed.TargetStateObservation{}},
	}
	got := consumeWithObserved(t, obs).CADRuntimeOutcome().Observed
	if got == nil {
		t.Fatal("observed evidence was not carried")
	}
	if err := observed.Validate(got); err != nil {
		t.Fatalf("carried observed evidence fails observed.Validate: %v", err)
	}
	if got.Observation.TargetState == nil {
		t.Fatal("present target-state evidence must stay present")
	}
}
