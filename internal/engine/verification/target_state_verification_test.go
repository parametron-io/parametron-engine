package verification

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/observed"
)

// Issue #18: Engine consumes canonical target-state evidence. These tests
// derive the Engine-owned expected state from real mutation intent and
// compare it with explicit actual evidence.

type tsMutations = planner.ExportManifestMutationCollection

func tsBool(destination, object string, value bool) observed.BooleanTargetEvidence {
	return observed.BooleanTargetEvidence{Destination: destination, Object: object, Status: observed.BooleanEvidenceStatusObserved, Value: &value}
}

func tsBoolStatus(destination, object, status string) observed.BooleanTargetEvidence {
	return observed.BooleanTargetEvidence{Destination: destination, Object: object, Status: status}
}

func tsExists(destination, object, status string) observed.ExistenceTargetEvidence {
	return observed.ExistenceTargetEvidence{Destination: destination, Object: object, Status: status}
}

func tsEvidence(suppression, visibility []observed.BooleanTargetEvidence, existence []observed.ExistenceTargetEvidence) *observed.TargetStateObservation {
	if suppression == nil {
		suppression = []observed.BooleanTargetEvidence{}
	}
	if visibility == nil {
		visibility = []observed.BooleanTargetEvidence{}
	}
	if existence == nil {
		existence = []observed.ExistenceTargetEvidence{}
	}
	return &observed.TargetStateObservation{Suppression: suppression, Visibility: visibility, Existence: existence}
}

// tsObserved returns otherwise-matching observed evidence carrying evidence.
func tsObserved(contract *Contract, evidence *observed.TargetStateObservation) *observed.Observed {
	obs := matchingObserved(contract)
	obs.Observation.TargetState = evidence
	return obs
}

func tsSuppressionContract(t *testing.T, destination string, suppressed bool) *Contract {
	t.Helper()
	collection := &tsMutations{Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Feature", Suppressed: suppressed}}}
	if destination == TargetDestinationPart {
		return deriveTargetStateContract(t, nil, collection)
	}
	return deriveTargetStateContract(t, collection, nil)
}

func tsVisibilityContract(t *testing.T, destination string, visible bool) *Contract {
	t.Helper()
	collection := &tsMutations{Visibility: []planner.ExportManifestVisibilityMutation{{Object: "Feature", Visible: visible}}}
	if destination == TargetDestinationPart {
		return deriveTargetStateContract(t, nil, collection)
	}
	return deriveTargetStateContract(t, collection, nil)
}

func tsDeletionContract(t *testing.T, destination string) *Contract {
	t.Helper()
	collection := &tsMutations{Deletion: []planner.ExportManifestDeletionMutation{{Object: "Feature"}}}
	if destination == TargetDestinationPart {
		return deriveTargetStateContract(t, nil, collection)
	}
	return deriveTargetStateContract(t, collection, nil)
}

// tsRequireFailure asserts a Verify failure with the exact class, sentinel,
// message fragment, and that the target-state category carries the failure.
func tsRequireFailure(t *testing.T, contract *Contract, obs *observed.Observed, class FailureClass, sentinel error, messageSub string) *Result {
	t.Helper()
	result, err := Verify(contract, obs)
	if err == nil {
		t.Fatalf("Verify passed, want %s failure; result=%+v", class, result)
	}
	if result == nil || result.Status != StatusFail || result.Failure != class {
		t.Fatalf("result=%+v, want fail with class %s (err=%v)", result, class, err)
	}
	var verifyErr *VerifyError
	if !errors.As(err, &verifyErr) || verifyErr.Class != class {
		t.Fatalf("err=%T %v, want *VerifyError class %s", err, err, class)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("errors.Is(%v, %v) = false", err, sentinel)
	}
	for _, other := range []error{ErrTargetStateMismatch, ErrTargetMissing, ErrNativeEvidenceUnavailable, ErrRequiredObservationMissing, ErrObservedInvalid, ErrContractInvalid} {
		if other != sentinel && errors.Is(err, other) {
			t.Fatalf("errors.Is(%v, %v) = true; classes must stay distinct", err, other)
		}
	}
	if !strings.Contains(result.Message, messageSub) || result.Message != err.Error() {
		t.Fatalf("message=%q err=%q, want fragment %q", result.Message, err.Error(), messageSub)
	}
	return result
}

func tsRequirePass(t *testing.T, contract *Contract, obs *observed.Observed) *Result {
	t.Helper()
	result, err := Verify(contract, obs)
	if err != nil {
		t.Fatalf("Verify returned error: %v (result=%+v)", err, result)
	}
	if result.Status != StatusPass || result.Failure != FailureClassNone {
		t.Fatalf("result=%+v, want pass", result)
	}
	if !result.Categories.TargetState.Enabled || result.Categories.TargetState.Status != CategoryStatusPass {
		t.Fatalf("target-state category=%+v, want enabled pass", result.Categories.TargetState)
	}
	return result
}

// ===================== Expected-state derivation =====================

func mixedTargetStateMutations() (*tsMutations, *tsMutations) {
	assembly := &tsMutations{
		Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Sketch", Suppressed: true}, {Object: "Pad", Suppressed: false}},
		Visibility:  []planner.ExportManifestVisibilityMutation{{Object: "Body", Visible: false}, {Object: "Axis", Visible: true}},
		Deletion:    []planner.ExportManifestDeletionMutation{{Object: "Zeta"}, {Object: "Alpha"}},
	}
	part := &tsMutations{
		Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Sketch", Suppressed: false}},
		Visibility:  []planner.ExportManifestVisibilityMutation{{Object: "Body", Visible: true}},
		Deletion:    []planner.ExportManifestDeletionMutation{{Object: "Fillet"}},
	}
	return assembly, part
}

func TestExpectedTargetState_DerivedFromMutationValuesAcrossFamiliesAndDestinations(t *testing.T) {
	assembly, part := mixedTargetStateMutations()
	contract := deriveTargetStateContract(t, assembly, part)

	want := &ExpectedTargetState{
		Suppression: []ExpectedBooleanTargetState{
			{Destination: "assembly", Object: "Pad", Value: false},
			{Destination: "assembly", Object: "Sketch", Value: true},
			{Destination: "part", Object: "Sketch", Value: false},
		},
		Visibility: []ExpectedBooleanTargetState{
			{Destination: "assembly", Object: "Axis", Value: true},
			{Destination: "assembly", Object: "Body", Value: false},
			{Destination: "part", Object: "Body", Value: true},
		},
		Existence: []ExpectedExistenceTargetState{
			{Destination: "assembly", Object: "Alpha", Status: observed.ExistenceEvidenceStatusAbsent},
			{Destination: "assembly", Object: "Zeta", Status: observed.ExistenceEvidenceStatusAbsent},
			{Destination: "part", Object: "Fillet", Status: observed.ExistenceEvidenceStatusAbsent},
		},
	}
	if !reflect.DeepEqual(contract.Expected.TargetState, want) {
		t.Fatalf("expected target state\nwant: %+v\ngot:  %+v", want, contract.Expected.TargetState)
	}
	if err := Validate(contract); err != nil {
		t.Fatalf("derived contract must validate: %v", err)
	}
}

func TestExpectedTargetState_BothDirectionsAndDestinationsIndividually(t *testing.T) {
	for _, destination := range []string{TargetDestinationAssembly, TargetDestinationPart} {
		for _, value := range []bool{true, false} {
			suppression := tsSuppressionContract(t, destination, value).Expected.TargetState
			if len(suppression.Suppression) != 1 || len(suppression.Visibility) != 0 || len(suppression.Existence) != 0 ||
				suppression.Suppression[0] != (ExpectedBooleanTargetState{Destination: destination, Object: "Feature", Value: value}) {
				t.Fatalf("suppression %s/%t derived %+v", destination, value, suppression)
			}
			visibility := tsVisibilityContract(t, destination, value).Expected.TargetState
			if len(visibility.Visibility) != 1 || len(visibility.Suppression) != 0 || len(visibility.Existence) != 0 ||
				visibility.Visibility[0] != (ExpectedBooleanTargetState{Destination: destination, Object: "Feature", Value: value}) {
				t.Fatalf("visibility %s/%t derived %+v", destination, value, visibility)
			}
		}
		deletion := tsDeletionContract(t, destination).Expected.TargetState
		if len(deletion.Existence) != 1 || len(deletion.Suppression) != 0 || len(deletion.Visibility) != 0 ||
			deletion.Existence[0] != (ExpectedExistenceTargetState{Destination: destination, Object: "Feature", Status: observed.ExistenceEvidenceStatusAbsent}) {
			t.Fatalf("deletion %s derived %+v", destination, deletion)
		}
	}
}

func TestExpectedTargetState_OrderingIsIndependentOfMutationInputOrder(t *testing.T) {
	assembly, part := mixedTargetStateMutations()
	forward := deriveTargetStateContract(t, assembly, part)

	reversedAssembly := &tsMutations{
		Suppression: []planner.ExportManifestSuppressionMutation{assembly.Suppression[1], assembly.Suppression[0]},
		Visibility:  []planner.ExportManifestVisibilityMutation{assembly.Visibility[1], assembly.Visibility[0]},
		Deletion:    []planner.ExportManifestDeletionMutation{assembly.Deletion[1], assembly.Deletion[0]},
	}
	reversed := deriveTargetStateContract(t, reversedAssembly, part)
	if !reflect.DeepEqual(forward.Expected.TargetState, reversed.Expected.TargetState) {
		t.Fatalf("expected state depends on mutation order\nforward:  %+v\nreversed: %+v", forward.Expected.TargetState, reversed.Expected.TargetState)
	}
}

func TestExpectedTargetState_ValuesComeFromMutationsNotRequestIdentities(t *testing.T) {
	build := func(suppressed, visible bool) *Contract {
		return deriveTargetStateContract(t, &tsMutations{
			Suppression: []planner.ExportManifestSuppressionMutation{{Object: "S", Suppressed: suppressed}},
			Visibility:  []planner.ExportManifestVisibilityMutation{{Object: "V", Visible: visible}},
		}, nil)
	}
	a, b := build(true, true), build(false, false)

	// The identity-only request is identical...
	if !reflect.DeepEqual(a.ObservationContext.TargetState, b.ObservationContext.TargetState) {
		t.Fatalf("request identities must not depend on intended values: %+v vs %+v", a.ObservationContext.TargetState, b.ObservationContext.TargetState)
	}
	// ...while the Engine-owned expectations follow the mutation values.
	if a.Expected.TargetState.Suppression[0].Value != true || b.Expected.TargetState.Suppression[0].Value != false ||
		a.Expected.TargetState.Visibility[0].Value != true || b.Expected.TargetState.Visibility[0].Value != false {
		t.Fatalf("expected values must follow mutations: %+v vs %+v", a.Expected.TargetState, b.Expected.TargetState)
	}
}

func TestExpectedTargetState_AbsentWithoutTargetMutations(t *testing.T) {
	if got := deriveTargetStateContract(t, nil, nil).Expected.TargetState; got != nil {
		t.Fatalf("no mutations must not derive expected target state: %+v", got)
	}
	propertyOnly := &tsMutations{Properties: []planner.ExportManifestPropertyMutation{{Object: "Box", Property: "Label", Value: "x"}}}
	if got := deriveTargetStateContract(t, propertyOnly, nil).Expected.TargetState; got != nil {
		t.Fatalf("property-only mutations must not derive expected target state: %+v", got)
	}
	if got := deriveTargetStateContract(t, &tsMutations{}, &tsMutations{}).Expected.TargetState; got != nil {
		t.Fatalf("empty collections must not derive expected target state: %+v", got)
	}
}

// ===================== In-memory expectations vs serialized request =====================

func TestExpectedTargetState_StaysOutOfSerializedRequest(t *testing.T) {
	assembly, part := mixedTargetStateMutations()
	contract := deriveTargetStateContract(t, assembly, part)
	canonical := mustCanonical(t, contract)

	var doc map[string]json.RawMessage
	if err := json.Unmarshal(canonical, &doc); err != nil {
		t.Fatalf("canonical JSON: %v", err)
	}
	var expected map[string]json.RawMessage
	if err := json.Unmarshal(doc["expected"], &expected); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(expected))
	for key := range expected {
		keys = append(keys, key)
	}
	if len(keys) != 4 || expected["components"] == nil || expected["parameters"] == nil || expected["metadata"] == nil || expected["references"] == nil {
		t.Fatalf("expected section gained fields: %v", keys)
	}

	var context struct {
		TargetState map[string][]map[string]any `json:"targetState"`
	}
	if err := json.Unmarshal(doc["observationContext"], &context); err != nil {
		t.Fatal(err)
	}
	if len(context.TargetState) != 3 {
		t.Fatalf("request families: %v", context.TargetState)
	}
	for family, entries := range context.TargetState {
		for _, entry := range entries {
			if len(entry) != 2 || entry["destination"] == nil || entry["object"] == nil {
				t.Fatalf("%s request entry is not identity-only: %v", family, entry)
			}
		}
	}
	for _, forbidden := range []string{`"suppressed"`, `"visible"`, `"absent"`, `"status"`} {
		if strings.Contains(string(canonical), forbidden) {
			t.Fatalf("canonical request leaked %s: %s", forbidden, canonical)
		}
	}
}

func TestExpectedTargetState_RoundTripDoesNotFabricateExpectations(t *testing.T) {
	assembly, part := mixedTargetStateMutations()
	derived := deriveTargetStateContract(t, assembly, part)
	canonical := mustCanonical(t, derived)

	parsed, err := Parse(canonical)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Expected.TargetState != nil {
		t.Fatalf("Parse fabricated expected target state: %+v", parsed.Expected.TargetState)
	}
	if !parsed.Observe.TargetState || !reflect.DeepEqual(parsed.ObservationContext.TargetState, derived.ObservationContext.TargetState) {
		t.Fatalf("Parse lost the identity request: %+v", parsed.ObservationContext.TargetState)
	}
	if got := mustCanonical(t, parsed); string(got) != string(canonical) {
		t.Fatalf("round trip changed bytes\nwant: %s\ngot:  %s", canonical, got)
	}

	// A request-only contract has no Engine expectation, so Engine must refuse
	// to verify it rather than inventing values or silently passing.
	obs := tsObserved(parsed, tsEvidence(nil, nil, nil))
	result, verifyErr := Verify(parsed, obs)
	if verifyErr == nil || result.Failure != FailureClassContractInvalid || !errors.Is(verifyErr, ErrContractInvalid) {
		t.Fatalf("request-only target-state contract: result=%+v err=%v", result, verifyErr)
	}
	if result.Categories.TargetState.Status == CategoryStatusPass {
		t.Fatalf("target-state category must not pass: %+v", result.Categories.TargetState)
	}
}

// ===================== Expected/request identity consistency =====================

func TestExpectedTargetState_RejectsIdentitiesInconsistentWithRequest(t *testing.T) {
	assembly, part := mixedTargetStateMutations()
	tests := []struct {
		name   string
		mutate func(*Contract)
	}{
		{"different object in suppression", func(c *Contract) { c.Expected.TargetState.Suppression[0].Object = "Other" }},
		{"different destination in visibility", func(c *Contract) { c.Expected.TargetState.Visibility[0].Destination = "part" }},
		{"different object in existence", func(c *Contract) { c.Expected.TargetState.Existence[2].Object = "Other" }},
		{"missing suppression identity", func(c *Contract) {
			c.Expected.TargetState.Suppression = c.Expected.TargetState.Suppression[1:]
		}},
		{"missing existence identity", func(c *Contract) {
			c.Expected.TargetState.Existence = c.Expected.TargetState.Existence[:2]
		}},
		{"extra visibility identity", func(c *Contract) {
			c.Expected.TargetState.Visibility = append(c.Expected.TargetState.Visibility, ExpectedBooleanTargetState{Destination: "part", Object: "Extra", Value: true})
		}},
		{"extra existence identity", func(c *Contract) {
			c.Expected.TargetState.Existence = append(c.Expected.TargetState.Existence, ExpectedExistenceTargetState{Destination: "part", Object: "Zz", Status: observed.ExistenceEvidenceStatusAbsent})
		}},
		{"family mismatch suppression to visibility", func(c *Contract) {
			// Sketch is requested under suppression only; expecting it as visibility must not validate.
			c.Expected.TargetState.Visibility[0] = ExpectedBooleanTargetState{Destination: "assembly", Object: "Sketch", Value: true}
		}},
		{"expected without request", func(c *Contract) {
			c.Observe.TargetState = false
			c.ObservationContext.TargetState = nil
		}},
		{"nil expected collection", func(c *Contract) { c.Expected.TargetState.Existence = nil }},
		{"unsupported existence status", func(c *Contract) { c.Expected.TargetState.Existence[0].Status = observed.ExistenceEvidenceStatusExists }},
		{"duplicate expected identity", func(c *Contract) {
			c.Expected.TargetState.Suppression[1] = c.Expected.TargetState.Suppression[0]
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contract := deriveTargetStateContract(t, assembly, part)
			tt.mutate(contract)

			err := Validate(contract)
			if err == nil || !errors.Is(err, ErrValidation) {
				t.Fatalf("Validate = %v, want ErrValidation", err)
			}

			// The failure is a contract failure, never a later comparison outcome,
			// even when the evidence itself is perfectly consistent.
			obs := tsObserved(contract, tsEvidence(nil, nil, nil))
			result, verifyErr := Verify(contract, obs)
			if verifyErr == nil || result.Failure != FailureClassContractInvalid || !errors.Is(verifyErr, ErrContractInvalid) {
				t.Fatalf("Verify: result=%+v err=%v, want contract_invalid", result, verifyErr)
			}
		})
	}
}

func TestExpectedTargetState_EquivalentIdentitySetsAreValidRegardlessOfRequestOrdering(t *testing.T) {
	assembly, part := mixedTargetStateMutations()
	contract := deriveTargetStateContract(t, assembly, part)
	request := contract.ObservationContext.TargetState
	for _, family := range [][]TargetIdentity{request.Suppression, request.Visibility, request.Existence} {
		for i, j := 0, len(family)-1; i < j; i, j = i+1, j-1 {
			family[i], family[j] = family[j], family[i]
		}
	}
	if err := Validate(contract); err != nil {
		t.Fatalf("reordered-but-equivalent request must validate: %v", err)
	}
}

// ===================== Boolean families: suppression + visibility =====================

type booleanFamilyCase struct {
	family   string
	contract func(*testing.T, string, bool) *Contract
	evidence func(suppressionOrVisibility []observed.BooleanTargetEvidence) *observed.TargetStateObservation
}

func booleanFamilies() []booleanFamilyCase {
	return []booleanFamilyCase{
		{"suppression", tsSuppressionContract, func(e []observed.BooleanTargetEvidence) *observed.TargetStateObservation { return tsEvidence(e, nil, nil) }},
		{"visibility", tsVisibilityContract, func(e []observed.BooleanTargetEvidence) *observed.TargetStateObservation { return tsEvidence(nil, e, nil) }},
	}
}

func TestVerifyTargetState_BooleanFamiliesClassifyObservedEvidence(t *testing.T) {
	for _, family := range booleanFamilies() {
		for _, destination := range []string{TargetDestinationAssembly, TargetDestinationPart} {
			for _, want := range []bool{true, false} {
				name := family.family + "/" + destination + "/expected=" + map[bool]string{true: "true", false: "false"}[want]
				t.Run(name+"/matching value passes", func(t *testing.T) {
					contract := family.contract(t, destination, want)
					tsRequirePass(t, contract, tsObserved(contract, family.evidence([]observed.BooleanTargetEvidence{tsBool(destination, "Feature", want)})))
				})
				t.Run(name+"/different observed value is target_state_mismatch", func(t *testing.T) {
					contract := family.contract(t, destination, want)
					obs := tsObserved(contract, family.evidence([]observed.BooleanTargetEvidence{tsBool(destination, "Feature", !want)}))
					result := tsRequireFailure(t, contract, obs, FailureClassTargetStateMismatch, ErrTargetStateMismatch, family.family+" mismatch")
					if result.Categories.TargetState.Status != CategoryStatusFail || result.Categories.Metadata.Status != CategoryStatusPass || result.Categories.References.Status != CategoryStatusPass {
						t.Fatalf("categories=%+v", result.Categories)
					}
				})
				t.Run(name+"/explicit target_missing", func(t *testing.T) {
					contract := family.contract(t, destination, want)
					obs := tsObserved(contract, family.evidence([]observed.BooleanTargetEvidence{tsBoolStatus(destination, "Feature", observed.BooleanEvidenceStatusTargetMissing)}))
					tsRequireFailure(t, contract, obs, FailureClassTargetMissing, ErrTargetMissing, "is missing")
				})
				t.Run(name+"/explicit unavailable", func(t *testing.T) {
					contract := family.contract(t, destination, want)
					obs := tsObserved(contract, family.evidence([]observed.BooleanTargetEvidence{tsBoolStatus(destination, "Feature", observed.BooleanEvidenceStatusUnavailable)}))
					tsRequireFailure(t, contract, obs, FailureClassNativeEvidenceUnavailable, ErrNativeEvidenceUnavailable, "unavailable")
				})
				t.Run(name+"/omitted entry", func(t *testing.T) {
					contract := family.contract(t, destination, want)
					obs := tsObserved(contract, family.evidence(nil))
					tsRequireFailure(t, contract, obs, FailureClassRequiredObservationMissing, ErrRequiredObservationMissing, family.family+" evidence")
				})
				t.Run(name+"/entire targetState omitted", func(t *testing.T) {
					contract := family.contract(t, destination, want)
					obs := tsObserved(contract, nil)
					tsRequireFailure(t, contract, obs, FailureClassRequiredObservationMissing, ErrRequiredObservationMissing, "target-state observation is missing")
				})
			}
		}
	}
}

func TestVerifyTargetState_SuppressionAndVisibilityAreIndependent(t *testing.T) {
	contract := deriveTargetStateContract(t, &tsMutations{
		Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Feature", Suppressed: true}},
		Visibility:  []planner.ExportManifestVisibilityMutation{{Object: "Feature", Visible: false}},
	}, nil)

	tsRequirePass(t, contract, tsObserved(contract, tsEvidence(
		[]observed.BooleanTargetEvidence{tsBool("assembly", "Feature", true)},
		[]observed.BooleanTargetEvidence{tsBool("assembly", "Feature", false)}, nil)))

	// Visibility evidence cannot stand in for suppression evidence, even for the
	// same object and the same wanted boolean.
	tsRequireFailure(t, contract, tsObserved(contract, tsEvidence(
		nil,
		[]observed.BooleanTargetEvidence{tsBool("assembly", "Feature", true), tsBool("part", "Feature", true)}, nil)),
		FailureClassRequiredObservationMissing, ErrRequiredObservationMissing, "suppression evidence")

	// Suppression evidence cannot stand in for visibility evidence.
	tsRequireFailure(t, contract, tsObserved(contract, tsEvidence(
		[]observed.BooleanTargetEvidence{tsBool("assembly", "Feature", true)}, nil, nil)),
		FailureClassRequiredObservationMissing, ErrRequiredObservationMissing, "visibility evidence")

	// A correct suppression does not mask a wrong visibility.
	tsRequireFailure(t, contract, tsObserved(contract, tsEvidence(
		[]observed.BooleanTargetEvidence{tsBool("assembly", "Feature", true)},
		[]observed.BooleanTargetEvidence{tsBool("assembly", "Feature", true)}, nil)),
		FailureClassTargetStateMismatch, ErrTargetStateMismatch, "visibility mismatch")
}

// ===================== Deletion / existence =====================

func TestVerifyTargetState_ExistenceClassifiesObservedEvidence(t *testing.T) {
	for _, destination := range []string{TargetDestinationAssembly, TargetDestinationPart} {
		contract := tsDeletionContract(t, destination)
		evidence := func(status string) *observed.Observed {
			return tsObserved(contract, tsEvidence(nil, nil, []observed.ExistenceTargetEvidence{tsExists(destination, "Feature", status)}))
		}

		t.Run(destination+"/absent passes", func(t *testing.T) {
			tsRequirePass(t, contract, evidence(observed.ExistenceEvidenceStatusAbsent))
		})
		t.Run(destination+"/exists is target_state_mismatch", func(t *testing.T) {
			tsRequireFailure(t, contract, evidence(observed.ExistenceEvidenceStatusExists), FailureClassTargetStateMismatch, ErrTargetStateMismatch, "existence mismatch")
		})
		t.Run(destination+"/unavailable is native_evidence_unavailable", func(t *testing.T) {
			tsRequireFailure(t, contract, evidence(observed.ExistenceEvidenceStatusUnavailable), FailureClassNativeEvidenceUnavailable, ErrNativeEvidenceUnavailable, "unavailable")
		})
		t.Run(destination+"/omitted entry is required_observation_missing", func(t *testing.T) {
			// Silence about a deleted object must never be read as successful deletion.
			tsRequireFailure(t, contract, tsObserved(contract, tsEvidence(nil, nil, nil)), FailureClassRequiredObservationMissing, ErrRequiredObservationMissing, "existence evidence")
		})
		t.Run(destination+"/entire targetState omitted", func(t *testing.T) {
			tsRequireFailure(t, contract, tsObserved(contract, nil), FailureClassRequiredObservationMissing, ErrRequiredObservationMissing, "target-state observation is missing")
		})
	}
}

func TestVerifyTargetState_AbsentIsNotCollapsedIntoMissingEvidence(t *testing.T) {
	contract := tsDeletionContract(t, TargetDestinationPart)
	absent := tsObserved(contract, tsEvidence(nil, nil, []observed.ExistenceTargetEvidence{tsExists("part", "Feature", observed.ExistenceEvidenceStatusAbsent)}))
	omitted := tsObserved(contract, tsEvidence(nil, nil, nil))

	if result, err := Verify(contract, absent); err != nil || result.Status != StatusPass {
		t.Fatalf("explicit absent: result=%+v err=%v", result, err)
	}
	if result, err := Verify(contract, omitted); err == nil || result.Failure != FailureClassRequiredObservationMissing {
		t.Fatalf("omitted existence: result=%+v err=%v", result, err)
	}
}

// ===================== Requested value vs observed evidence =====================

func TestVerifyTargetState_ObservedEvidenceControlsNotRequestedValue(t *testing.T) {
	t.Run("requested visible=false, observed visible=true", func(t *testing.T) {
		contract := tsVisibilityContract(t, TargetDestinationAssembly, false)
		if contract.Expected.TargetState.Visibility[0].Value {
			t.Fatal("precondition: requested mutation is visible=false")
		}
		obs := tsObserved(contract, tsEvidence(nil, []observed.BooleanTargetEvidence{tsBool("assembly", "Feature", true)}, nil))
		tsRequireFailure(t, contract, obs, FailureClassTargetStateMismatch, ErrTargetStateMismatch, "want false, got true")
	})
	t.Run("requested suppressed=true, observed suppressed=false", func(t *testing.T) {
		contract := tsSuppressionContract(t, TargetDestinationPart, true)
		if !contract.Expected.TargetState.Suppression[0].Value {
			t.Fatal("precondition: requested mutation is suppressed=true")
		}
		obs := tsObserved(contract, tsEvidence([]observed.BooleanTargetEvidence{tsBool("part", "Feature", false)}, nil, nil))
		tsRequireFailure(t, contract, obs, FailureClassTargetStateMismatch, ErrTargetStateMismatch, "want true, got false")
	})
	t.Run("requested deletion, observed exists", func(t *testing.T) {
		contract := tsDeletionContract(t, TargetDestinationAssembly)
		obs := tsObserved(contract, tsEvidence(nil, nil, []observed.ExistenceTargetEvidence{tsExists("assembly", "Feature", observed.ExistenceEvidenceStatusExists)}))
		tsRequireFailure(t, contract, obs, FailureClassTargetStateMismatch, ErrTargetStateMismatch, "want \"absent\", got \"exists\"")
	})
	t.Run("observed equals requested only when the evidence says so", func(t *testing.T) {
		contract := tsSuppressionContract(t, TargetDestinationPart, true)
		tsRequirePass(t, contract, tsObserved(contract, tsEvidence([]observed.BooleanTargetEvidence{tsBool("part", "Feature", true)}, nil, nil)))
	})
}

// ===================== Exact destination + object matching =====================

func TestVerifyTargetState_MatchesByExactDestinationAndObject(t *testing.T) {
	contract := deriveTargetStateContract(t,
		&tsMutations{Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: true}}},
		&tsMutations{Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: false}}})

	t.Run("same object name in different destinations selects the right evidence regardless of position", func(t *testing.T) {
		tsRequirePass(t, contract, tsObserved(contract, tsEvidence([]observed.BooleanTargetEvidence{
			tsBool("part", "Pad", false), tsBool("assembly", "Pad", true),
		}, nil, nil)))
		tsRequirePass(t, contract, tsObserved(contract, tsEvidence([]observed.BooleanTargetEvidence{
			tsBool("assembly", "Pad", true), tsBool("part", "Pad", false),
		}, nil, nil)))
	})
	t.Run("values swapped between destinations fail on the exact identity", func(t *testing.T) {
		result := tsRequireFailure(t, contract, tsObserved(contract, tsEvidence([]observed.BooleanTargetEvidence{
			tsBool("assembly", "Pad", false), tsBool("part", "Pad", true),
		}, nil, nil)), FailureClassTargetStateMismatch, ErrTargetStateMismatch, `"assembly/Pad"`)
		_ = result
	})
	t.Run("destination alone does not match", func(t *testing.T) {
		tsRequireFailure(t, contract, tsObserved(contract, tsEvidence([]observed.BooleanTargetEvidence{
			tsBool("assembly", "Other", true), tsBool("part", "Pad", false),
		}, nil, nil)), FailureClassRequiredObservationMissing, ErrRequiredObservationMissing, `"assembly/Pad"`)
	})
	t.Run("object alone does not match", func(t *testing.T) {
		tsRequireFailure(t, contract, tsObserved(contract, tsEvidence([]observed.BooleanTargetEvidence{
			tsBool("assembly", "Pad", true),
		}, nil, nil)), FailureClassRequiredObservationMissing, ErrRequiredObservationMissing, `"part/Pad"`)
	})
	t.Run("case-normalized and near names do not match", func(t *testing.T) {
		for _, near := range []string{"pad", "PAD", "Pad2", "Pa", "Pad_"} {
			tsRequireFailure(t, contract, tsObserved(contract, tsEvidence([]observed.BooleanTargetEvidence{
				tsBool("assembly", near, true), tsBool("part", "Pad", false),
			}, nil, nil)), FailureClassRequiredObservationMissing, ErrRequiredObservationMissing, `"assembly/Pad"`)
		}
	})
	t.Run("collection position does not match", func(t *testing.T) {
		// The only entry sits at index 0 and has the wanted value, but for another target.
		tsRequireFailure(t, contract, tsObserved(contract, tsEvidence([]observed.BooleanTargetEvidence{
			tsBool("part", "Elsewhere", true), tsBool("part", "Pad", false),
		}, nil, nil)), FailureClassRequiredObservationMissing, ErrRequiredObservationMissing, `"assembly/Pad"`)
	})
	t.Run("same identity in another family does not satisfy", func(t *testing.T) {
		tsRequireFailure(t, contract, tsObserved(contract, tsEvidence(nil,
			[]observed.BooleanTargetEvidence{tsBool("assembly", "Pad", true), tsBool("part", "Pad", false)}, nil)),
			FailureClassRequiredObservationMissing, ErrRequiredObservationMissing, "suppression evidence")
	})
	t.Run("existence is matched by exact identity too", func(t *testing.T) {
		deletion := deriveTargetStateContract(t, &tsMutations{Deletion: []planner.ExportManifestDeletionMutation{{Object: "Gone"}}}, nil)
		tsRequireFailure(t, deletion, tsObserved(deletion, tsEvidence(nil, nil, []observed.ExistenceTargetEvidence{
			tsExists("part", "Gone", observed.ExistenceEvidenceStatusAbsent),
			tsExists("assembly", "gone", observed.ExistenceEvidenceStatusAbsent),
		})), FailureClassRequiredObservationMissing, ErrRequiredObservationMissing, `"assembly/Gone"`)
	})
}

// ===================== Invalid evidence precedence =====================

func TestVerifyTargetState_InvalidEvidenceIsContractFailureBeforeComparison(t *testing.T) {
	contract := deriveTargetStateContract(t, &tsMutations{
		Suppression: []planner.ExportManifestSuppressionMutation{{Object: "A", Suppressed: true}, {Object: "B", Suppressed: true}},
		Visibility:  []planner.ExportManifestVisibilityMutation{{Object: "V", Visible: true}},
		Deletion:    []planner.ExportManifestDeletionMutation{{Object: "D"}},
	}, nil)

	tests := []struct {
		name string
		ts   *observed.TargetStateObservation
	}{
		{"observed status without value", tsEvidence([]observed.BooleanTargetEvidence{tsBoolStatus("assembly", "A", observed.BooleanEvidenceStatusObserved)}, nil, nil)},
		{"target_missing with a value", tsEvidence([]observed.BooleanTargetEvidence{func() observed.BooleanTargetEvidence {
			e := tsBool("assembly", "A", true)
			e.Status = observed.BooleanEvidenceStatusTargetMissing
			return e
		}()}, nil, nil)},
		{"unavailable with a value", tsEvidence([]observed.BooleanTargetEvidence{func() observed.BooleanTargetEvidence {
			e := tsBool("assembly", "A", true)
			e.Status = observed.BooleanEvidenceStatusUnavailable
			return e
		}()}, nil, nil)},
		{"unknown boolean status", tsEvidence([]observed.BooleanTargetEvidence{tsBoolStatus("assembly", "A", "maybe")}, nil, nil)},
		{"unknown existence status", tsEvidence(nil, nil, []observed.ExistenceTargetEvidence{tsExists("assembly", "D", "gone")})},
		{"unsupported destination", tsEvidence([]observed.BooleanTargetEvidence{tsBool("Assembly", "A", true)}, nil, nil)},
		{"blank object", tsEvidence(nil, []observed.BooleanTargetEvidence{tsBool("assembly", " ", true)}, nil)},
		{"padded object", tsEvidence([]observed.BooleanTargetEvidence{tsBool("assembly", "A ", true)}, nil, nil)},
		{"duplicate identity", tsEvidence([]observed.BooleanTargetEvidence{tsBool("assembly", "A", true), tsBool("assembly", "A", false)}, nil, nil)},
		{"duplicate existence identity", tsEvidence(nil, nil, []observed.ExistenceTargetEvidence{
			tsExists("assembly", "D", observed.ExistenceEvidenceStatusAbsent), tsExists("assembly", "D", observed.ExistenceEvidenceStatusAbsent)})},
		{"nil suppression collection", &observed.TargetStateObservation{Visibility: []observed.BooleanTargetEvidence{}, Existence: []observed.ExistenceTargetEvidence{}}},
		{"nil existence collection", &observed.TargetStateObservation{Suppression: []observed.BooleanTargetEvidence{}, Visibility: []observed.BooleanTargetEvidence{}}},
		{"invalid entry hidden behind an earlier mismatch", tsEvidence([]observed.BooleanTargetEvidence{
			tsBool("assembly", "A", false), tsBoolStatus("assembly", "B", observed.BooleanEvidenceStatusObserved)}, nil, nil)},
		{"invalid entry hidden behind a missing entry", tsEvidence([]observed.BooleanTargetEvidence{tsBoolStatus("assembly", "Z", "bogus")}, nil, nil)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obs := tsObserved(contract, tt.ts)
			result, err := Verify(contract, obs)
			if err == nil {
				t.Fatalf("Verify passed on invalid evidence: %+v", result)
			}
			if result.Failure != FailureClassObservedInvalid || !errors.Is(err, ErrObservedInvalid) {
				t.Fatalf("result=%+v err=%v, want observed_artifact_invalid", result, err)
			}
			for _, other := range []error{ErrTargetStateMismatch, ErrTargetMissing, ErrNativeEvidenceUnavailable, ErrRequiredObservationMissing} {
				if errors.Is(err, other) {
					t.Fatalf("invalid evidence collapsed into %v", other)
				}
			}
			if result.Categories.TargetState.Status == CategoryStatusPass || result.Categories.TargetState.Status == CategoryStatusFail {
				t.Fatalf("invalid evidence must not be semantically compared: %+v", result.Categories.TargetState)
			}
			if !strings.Contains(result.Message, "observed artifact invalid") {
				t.Fatalf("message=%q", result.Message)
			}
		})
	}
}

func TestVerifyTargetState_DoesNotRepairMalformedEvidence(t *testing.T) {
	contract := tsSuppressionContract(t, TargetDestinationAssembly, true)
	evidence := tsEvidence([]observed.BooleanTargetEvidence{tsBoolStatus("assembly", "Feature", observed.BooleanEvidenceStatusObserved)}, nil, nil)
	obs := tsObserved(contract, evidence)
	before := *evidence
	before.Suppression = append([]observed.BooleanTargetEvidence(nil), evidence.Suppression...)

	if _, err := Verify(contract, obs); !errors.Is(err, ErrObservedInvalid) {
		t.Fatalf("err=%v", err)
	}
	if !reflect.DeepEqual(*obs.Observation.TargetState, before) || obs.Observation.TargetState.Suppression[0].Value != nil {
		t.Fatalf("verification rewrote evidence: %+v", obs.Observation.TargetState)
	}
}

// ===================== Failure taxonomy =====================

func TestFailureTaxonomy_TargetStateClassesAreDistinctMachineReadableValues(t *testing.T) {
	classes := map[FailureClass]error{
		FailureClassTargetStateMismatch:        ErrTargetStateMismatch,
		FailureClassTargetMissing:              ErrTargetMissing,
		FailureClassNativeEvidenceUnavailable:  ErrNativeEvidenceUnavailable,
		FailureClassRequiredObservationMissing: ErrRequiredObservationMissing,
		FailureClassObservedInvalid:            ErrObservedInvalid,
	}
	wantStrings := map[FailureClass]string{
		FailureClassTargetStateMismatch:        "target_state_mismatch",
		FailureClassTargetMissing:              "target_missing",
		FailureClassNativeEvidenceUnavailable:  "native_evidence_unavailable",
		FailureClassRequiredObservationMissing: "required_observation_missing",
		FailureClassObservedInvalid:            "observed_artifact_invalid",
	}
	for class, sentinel := range classes {
		if string(class) != wantStrings[class] {
			t.Fatalf("class %q, want %q", class, wantStrings[class])
		}
		for _, cause := range []error{nil, errors.New("root cause")} {
			err := &VerifyError{Class: class, Message: "m", Err: cause}
			for otherClass, otherSentinel := range classes {
				if got, want := errors.Is(err, otherSentinel), otherClass == class; got != want {
					t.Fatalf("class %s cause=%v: errors.Is(%v)=%t, want %t", class, cause, otherSentinel, got, want)
				}
			}
			if cause != nil && !errors.Is(err, cause) {
				t.Fatalf("class %s lost wrapped cause", class)
			}
			if !errors.Is(err, sentinel) {
				t.Fatalf("class %s does not match its sentinel", class)
			}
		}
	}
}

// ===================== Failure ordering and determinism =====================

func TestVerifyTargetState_EarlierCategoriesRemainAuthoritative(t *testing.T) {
	newContract := func() *Contract {
		contract := deriveTargetStateContract(t, &tsMutations{Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Feature", Suppressed: true}}}, nil)
		return contract
	}
	failing := func(contract *Contract) *observed.Observed {
		return tsObserved(contract, tsEvidence([]observed.BooleanTargetEvidence{tsBool("assembly", "Feature", false)}, nil, nil))
	}

	t.Run("metadata beats target state", func(t *testing.T) {
		contract := newContract()
		obs := failing(contract)
		obs.Observation.Metadata[0].Value = mustObservedMetadataValue(strings.Repeat("e", 64))
		result := tsRequireFailureNoCategoryCheck(t, contract, obs, FailureClassMetadataMismatch, ErrMetadataMismatch)
		if result.Categories.TargetState.Status != CategoryStatusFail || result.Categories.TargetState.FailureClass() != FailureClassTargetStateMismatch {
			t.Fatalf("target-state failure must still be recorded: %+v", result.Categories.TargetState)
		}
	})
	t.Run("references beat target state", func(t *testing.T) {
		contract := newContract()
		obs := failing(contract)
		obs.Observation.References[0].Name = "/tmp/wrong.FCStd"
		tsRequireFailureNoCategoryCheck(t, contract, obs, FailureClassReferenceMismatch, ErrReferenceMismatch)
	})
	t.Run("parameters beat target state", func(t *testing.T) {
		contract := deriveTargetStateContract(t, &tsMutations{Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Feature", Suppressed: true}}}, nil)
		contract.Observe.Parameters = true
		contract.Checks.Parameters.Enabled = true
		contract.Expected.Parameters = []ExpectedParameter{{ID: "par.a", Name: "a", Type: "number", Unit: "mm", Value: 1}}
		contract.ObservationContext.Parameters = []ObservedParameterBinding{{ID: "par.a", Name: "a", GroupName: "g"}}
		obs := failing(contract)
		obs.Observation.Parameters = matchingObservedParameters(contract)
		obs.Observation.Parameters[0].Value = mustObservedScalarValue("2")
		tsRequireFailureNoCategoryCheck(t, contract, obs, FailureClassParameterMismatch, ErrParameterMismatch)
	})
	t.Run("components beat target state", func(t *testing.T) {
		contract := newContract()
		contract.Observe.Components = true
		contract.Checks.Components.Enabled = true
		contract.Expected.Components = []ExpectedComponent{{ID: "cmp.root", Kind: observed.ComponentKindAssembly, Name: "Widget"}}
		obs := failing(contract)
		obs.Observation.Components = matchingObservedComponents(contract)
		obs.Observation.Components[0].Name = "Wrong"
		tsRequireFailureNoCategoryCheck(t, contract, obs, FailureClassComponentMismatch, ErrComponentMismatch)
	})
	t.Run("target state fails alone when earlier categories pass", func(t *testing.T) {
		contract := newContract()
		tsRequireFailure(t, contract, failing(contract), FailureClassTargetStateMismatch, ErrTargetStateMismatch, "suppression mismatch")
	})
}

func tsRequireFailureNoCategoryCheck(t *testing.T, contract *Contract, obs *observed.Observed, class FailureClass, sentinel error) *Result {
	t.Helper()
	result, err := Verify(contract, obs)
	if err == nil || result.Failure != class || !errors.Is(err, sentinel) {
		t.Fatalf("result=%+v err=%v, want %s", result, err, class)
	}
	return result
}

func TestVerifyTargetState_FirstFailureOrderIsFamilyThenSortedIdentity(t *testing.T) {
	contract := deriveTargetStateContract(t,
		&tsMutations{
			Suppression: []planner.ExportManifestSuppressionMutation{{Object: "A", Suppressed: true}},
			Visibility:  []planner.ExportManifestVisibilityMutation{{Object: "A", Visible: true}},
			Deletion:    []planner.ExportManifestDeletionMutation{{Object: "A"}},
		},
		&tsMutations{
			Suppression: []planner.ExportManifestSuppressionMutation{{Object: "B", Suppressed: true}},
		})
	good := func() *observed.TargetStateObservation {
		return tsEvidence(
			[]observed.BooleanTargetEvidence{tsBool("assembly", "A", true), tsBool("part", "B", true)},
			[]observed.BooleanTargetEvidence{tsBool("assembly", "A", true)},
			[]observed.ExistenceTargetEvidence{tsExists("assembly", "A", observed.ExistenceEvidenceStatusAbsent)})
	}
	tsRequirePass(t, contract, tsObserved(contract, good()))

	t.Run("suppression failure beats visibility and existence", func(t *testing.T) {
		ev := good()
		ev.Suppression[0] = tsBoolStatus("assembly", "A", observed.BooleanEvidenceStatusTargetMissing)
		ev.Visibility[0] = tsBoolStatus("assembly", "A", observed.BooleanEvidenceStatusUnavailable)
		ev.Existence[0] = tsExists("assembly", "A", observed.ExistenceEvidenceStatusExists)
		tsRequireFailure(t, contract, tsObserved(contract, ev), FailureClassTargetMissing, ErrTargetMissing, "assembly/A")
	})
	t.Run("visibility failure beats existence", func(t *testing.T) {
		ev := good()
		ev.Visibility[0] = tsBoolStatus("assembly", "A", observed.BooleanEvidenceStatusUnavailable)
		ev.Existence[0] = tsExists("assembly", "A", observed.ExistenceEvidenceStatusExists)
		tsRequireFailure(t, contract, tsObserved(contract, ev), FailureClassNativeEvidenceUnavailable, ErrNativeEvidenceUnavailable, "visibility")
	})
	t.Run("existence failure surfaces last", func(t *testing.T) {
		ev := good()
		ev.Existence[0] = tsExists("assembly", "A", observed.ExistenceEvidenceStatusExists)
		tsRequireFailure(t, contract, tsObserved(contract, ev), FailureClassTargetStateMismatch, ErrTargetStateMismatch, "existence mismatch")
	})
	t.Run("within a family the sorted-first identity wins irrespective of evidence order or class", func(t *testing.T) {
		for _, reversed := range []bool{false, true} {
			ev := good()
			ev.Suppression = []observed.BooleanTargetEvidence{
				tsBoolStatus("assembly", "A", observed.BooleanEvidenceStatusUnavailable), // assembly/A sorts first
				tsBool("part", "B", false),                                                 // later mismatch
			}
			if reversed {
				ev.Suppression[0], ev.Suppression[1] = ev.Suppression[1], ev.Suppression[0]
			}
			tsRequireFailure(t, contract, tsObserved(contract, ev), FailureClassNativeEvidenceUnavailable, ErrNativeEvidenceUnavailable, "assembly/A")
		}
	})
	t.Run("missing entry for the sorted-first identity beats a later mismatch", func(t *testing.T) {
		ev := good()
		ev.Suppression = []observed.BooleanTargetEvidence{tsBool("part", "B", false)}
		tsRequireFailure(t, contract, tsObserved(contract, ev), FailureClassRequiredObservationMissing, ErrRequiredObservationMissing, "assembly/A")
	})
}

func TestVerifyTargetState_ResultIsDeterministicAcrossRunsAndEvidenceOrdering(t *testing.T) {
	contract := deriveTargetStateContract(t, &tsMutations{
		Suppression: []planner.ExportManifestSuppressionMutation{{Object: "A", Suppressed: true}, {Object: "B", Suppressed: false}, {Object: "C", Suppressed: true}},
		Visibility:  []planner.ExportManifestVisibilityMutation{{Object: "A", Visible: false}},
	}, nil)
	build := func(reverse bool) *observed.Observed {
		suppression := []observed.BooleanTargetEvidence{
			tsBool("assembly", "A", true), tsBool("assembly", "B", true) /* mismatch */, tsBoolStatus("assembly", "C", observed.BooleanEvidenceStatusUnavailable),
		}
		if reverse {
			suppression[0], suppression[2] = suppression[2], suppression[0]
		}
		return tsObserved(contract, tsEvidence(suppression, []observed.BooleanTargetEvidence{tsBool("assembly", "A", false)}, nil))
	}

	baseline, baselineErr := Verify(contract, build(false))
	if baselineErr == nil || baseline.Failure != FailureClassTargetStateMismatch || !strings.Contains(baseline.Message, "assembly/B") {
		t.Fatalf("baseline=%+v err=%v", baseline, baselineErr)
	}
	for i := 0; i < 25; i++ {
		for _, reverse := range []bool{false, true} {
			got, err := Verify(contract, build(reverse))
			if err == nil || !reflect.DeepEqual(got, baseline) || err.Error() != baselineErr.Error() {
				t.Fatalf("run %d reverse=%t diverged\nwant: %+v\ngot:  %+v (%v)", i, reverse, baseline, got, err)
			}
		}
	}

	// Passing outcomes are deterministic too.
	pass := tsObserved(contract, tsEvidence(
		[]observed.BooleanTargetEvidence{tsBool("assembly", "C", true), tsBool("assembly", "B", false), tsBool("assembly", "A", true)},
		[]observed.BooleanTargetEvidence{tsBool("assembly", "A", false)}, nil))
	first := tsRequirePass(t, contract, pass)
	for i := 0; i < 10; i++ {
		if again := tsRequirePass(t, contract, pass); !reflect.DeepEqual(first, again) {
			t.Fatalf("pass result unstable: %+v vs %+v", first, again)
		}
	}
	if first.Categories.TargetState.Message != "verified 4 target-state entries" {
		t.Fatalf("message=%q", first.Categories.TargetState.Message)
	}
}

func TestVerifyTargetState_DoesNotMutateInputs(t *testing.T) {
	assembly, part := mixedTargetStateMutations()
	contract := deriveTargetStateContract(t, assembly, part)
	obs := tsObserved(contract, tsEvidence(
		[]observed.BooleanTargetEvidence{tsBool("assembly", "Pad", true)},
		[]observed.BooleanTargetEvidence{tsBool("part", "Body", false)}, nil))
	contractBefore := mustCanonical(t, contract)
	expectedBefore := *contract.Expected.TargetState
	obsBefore, err := observed.CanonicalJSON(obs)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Verify(contract, obs); err == nil {
		t.Fatal("expected failure")
	}
	obsAfter, err := observed.CanonicalJSON(obs)
	if err != nil || string(obsAfter) != string(obsBefore) {
		t.Fatalf("observed evidence rewritten: %s vs %s (%v)", obsBefore, obsAfter, err)
	}
	if string(mustCanonical(t, contract)) != string(contractBefore) || !reflect.DeepEqual(*contract.Expected.TargetState, expectedBefore) {
		t.Fatal("contract rewritten by verification")
	}
}

// ===================== No-target-state regression =====================

func TestVerifyTargetState_AbsentRequestKeepsExistingBehavior(t *testing.T) {
	contract := validContract()
	if contract.Observe.TargetState || contract.ObservationContext.TargetState != nil || contract.Expected.TargetState != nil {
		t.Fatalf("precondition: %+v", contract)
	}
	canonical := string(mustCanonical(t, contract))
	if canonical != targetStateNoRequestCanonical {
		t.Fatalf("canonical bytes changed: %s", canonical)
	}

	pass, err := Verify(contract, matchingObserved(contract))
	if err != nil || pass.Status != StatusPass {
		t.Fatalf("pass=%+v err=%v", pass, err)
	}
	if pass.Categories.TargetState.Enabled || pass.Categories.TargetState.Status != CategoryStatusSkipped {
		t.Fatalf("target-state category must stay disabled: %+v", pass.Categories.TargetState)
	}

	// Target-state evidence in the observed artifact is inert without a request,
	// even when it would be a mismatch or otherwise unusable.
	inert := matchingObserved(contract)
	inert.Observation.TargetState = tsEvidence([]observed.BooleanTargetEvidence{tsBoolStatus("assembly", "X", observed.BooleanEvidenceStatusTargetMissing)}, nil, nil)
	if result, err := Verify(contract, inert); err != nil || result.Status != StatusPass || result.Categories.TargetState.Enabled {
		t.Fatalf("unrequested evidence changed the outcome: %+v err=%v", result, err)
	}

	// Existing failures keep their class and never pick up target-state classes.
	failing := matchingObserved(contract)
	failing.Observation.Metadata[0].Value = mustObservedMetadataValue(strings.Repeat("b", 64))
	result, err := Verify(contract, failing)
	if err == nil || result.Failure != FailureClassMetadataMismatch || !errors.Is(err, ErrMetadataMismatch) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if result.Categories.TargetState.Enabled || result.Categories.TargetState.Status != CategoryStatusSkipped {
		t.Fatalf("target-state category leaked into ordinary failure: %+v", result.Categories.TargetState)
	}
	for _, sentinel := range []error{ErrTargetStateMismatch, ErrTargetMissing, ErrNativeEvidenceUnavailable} {
		if errors.Is(err, sentinel) {
			t.Fatalf("ordinary failure matches %v", sentinel)
		}
	}

	// Required observations that are absent still classify as before.
	missing := matchingObserved(contract)
	missing.Observation.Metadata = []observed.Metadata{}
	if result, err := Verify(contract, missing); err == nil || result.Failure != FailureClassRequiredObservationMissing || !strings.Contains(result.Message, "metadata") {
		t.Fatalf("result=%+v err=%v", result, err)
	}

	// Invalid-contract and invalid-observed handling is unchanged for ordinary contracts.
	if result, err := Verify(contract, nil); err == nil || result.Failure != FailureClassObservedInvalid {
		t.Fatalf("nil observed: %+v %v", result, err)
	}
	if result.Categories.TargetState.Enabled {
		t.Fatalf("invalid-observed result enabled target state: %+v", result.Categories)
	}
}
