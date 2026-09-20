package cadruntime

import (
	"bytes"
	"testing"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/observed"
	"parametron/internal/engine/verification"
)

func targetStateObservationRequest(t *testing.T, assembly, part *planner.ExportManifestMutationCollection) FreeCADRuntimeObservationRequestMaterialization {
	t.Helper()
	req := validFreeCADObservationRequest(t)
	req.Manifest.AssemblyMutations = assembly
	req.Manifest.PartMutations = part
	prepareObservationSource(t, req, []byte("prepared"))
	got, err := ComposeFreeCADRuntimeObservationRequest(req)
	if err != nil {
		t.Fatalf("ComposeFreeCADRuntimeObservationRequest: %v", err)
	}
	return got
}

func TestComposeFreeCADRuntimeObservationRequest_TargetStateAbsentWithoutTargetMutations(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	prepareObservationSource(t, req, []byte("prepared"))
	got, err := ComposeFreeCADRuntimeObservationRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Contract.Observe.TargetState || got.Contract.ObservationContext.TargetState != nil {
		t.Fatalf("no target mutations must yield no target-state request: %+v", got.Contract)
	}
	if bytes.Contains(got.JSON, []byte("targetState")) {
		t.Fatalf("request JSON must omit targetState: %s", got.JSON)
	}
}

func TestComposeFreeCADRuntimeObservationRequest_DerivesTargetStateFromCanonicalMutationIntent(t *testing.T) {
	got := targetStateObservationRequest(t,
		&planner.ExportManifestMutationCollection{
			Suppression: []planner.ExportManifestSuppressionMutation{{Object: "AsmSketch", Suppressed: true}},
			Deletion:    []planner.ExportManifestDeletionMutation{{Object: "AsmGone"}},
		},
		&planner.ExportManifestMutationCollection{
			Visibility:  []planner.ExportManifestVisibilityMutation{{Object: "PartPad", Visible: false}, {Object: "PartBody", Visible: true}},
			Suppression: []planner.ExportManifestSuppressionMutation{{Object: "PartSketch", Suppressed: false}},
		})
	if !got.Contract.Observe.TargetState {
		t.Fatal("observe.targetState must be true")
	}
	want := `"targetState":{"suppression":[{"destination":"assembly","object":"AsmSketch"},{"destination":"part","object":"PartSketch"}],"visibility":[{"destination":"part","object":"PartBody"},{"destination":"part","object":"PartPad"}],"existence":[{"destination":"assembly","object":"AsmGone"}]}`
	if !bytes.Contains(got.JSON, []byte(want)) {
		t.Fatalf("unexpected request\nwant fragment: %s\ngot: %s", want, got.JSON)
	}
	for _, forbidden := range []string{`"suppressed"`, `"visible"`, `"status"`} {
		if bytes.Contains(got.JSON, []byte(forbidden)) {
			t.Fatalf("requested intent leaked into request as %s: %s", forbidden, got.JSON)
		}
	}
	parsed, err := verification.Parse(bytes.TrimSuffix(got.JSON, []byte("\n")))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	canonical, err := verification.CanonicalJSON(parsed)
	if err != nil || !bytes.Equal(append(canonical, '\n'), got.JSON) {
		t.Fatalf("round trip: %v\n%s\n%s", err, canonical, got.JSON)
	}
}

func TestComposeFreeCADRuntimeObservationRequest_TargetStateIsDeterministicAcrossMutationOrder(t *testing.T) {
	a := targetStateObservationRequest(t,
		&planner.ExportManifestMutationCollection{
			Visibility: []planner.ExportManifestVisibilityMutation{{Object: "A", Visible: true}, {Object: "B", Visible: false}},
		},
		&planner.ExportManifestMutationCollection{
			Deletion: []planner.ExportManifestDeletionMutation{{Object: "X"}, {Object: "Y"}},
		})
	b := targetStateObservationRequest(t,
		&planner.ExportManifestMutationCollection{
			Visibility: []planner.ExportManifestVisibilityMutation{{Object: "B", Visible: false}, {Object: "A", Visible: true}},
		},
		&planner.ExportManifestMutationCollection{
			Deletion: []planner.ExportManifestDeletionMutation{{Object: "Y"}, {Object: "X"}},
		})
	// Both requests derive from independent temp roots; compare only the target-state request.
	fragment := func(data []byte) string {
		start := bytes.Index(data, []byte(`"targetState":{"suppression"`))
		end := bytes.Index(data, []byte(`},"expected"`))
		if start < 0 || end < start {
			t.Fatalf("no target-state fragment: %s", data)
		}
		return string(data[start:end])
	}
	if fa, fb := fragment(a.JSON), fragment(b.JSON); fa != fb {
		t.Fatalf("target-state request must be order independent\nA: %s\nB: %s", fa, fb)
	}
}

func TestCopyVerificationContract_TargetStateRequestDoesNotAlias(t *testing.T) {
	src := verification.Contract{
		Observe: verification.Observe{TargetState: true},
		ObservationContext: verification.ObservationContext{TargetState: &verification.TargetStateRequest{
			Suppression: []verification.TargetIdentity{{Destination: "assembly", Object: "S"}},
			Visibility:  []verification.TargetIdentity{{Destination: "part", Object: "V"}},
			Existence:   []verification.TargetIdentity{{Destination: "part", Object: "E"}},
		}},
	}
	copied := copyVerificationContract(src)
	if copied.ObservationContext.TargetState == src.ObservationContext.TargetState {
		t.Fatal("TargetState pointer must be copied")
	}

	// Mutating the copy must not change the source.
	copied.ObservationContext.TargetState.Suppression[0].Object = "mutated"
	copied.ObservationContext.TargetState.Visibility[0].Object = "mutated"
	copied.ObservationContext.TargetState.Existence[0].Object = "mutated"
	copied.ObservationContext.TargetState.Suppression = append(copied.ObservationContext.TargetState.Suppression, verification.TargetIdentity{})
	if got := src.ObservationContext.TargetState; got.Suppression[0].Object != "S" || got.Visibility[0].Object != "V" || got.Existence[0].Object != "E" || len(got.Suppression) != 1 {
		t.Fatalf("copy mutation leaked into source: %+v", *got)
	}

	// Mutating the source must not change a fresh copy.
	fresh := copyVerificationContract(src)
	src.ObservationContext.TargetState.Suppression[0].Object = "src-mutated"
	src.ObservationContext.TargetState.Visibility[0].Destination = "src-mutated"
	src.ObservationContext.TargetState.Existence[0].Object = "src-mutated"
	if got := fresh.ObservationContext.TargetState; got.Suppression[0].Object != "S" || got.Visibility[0].Destination != "part" || got.Existence[0].Object != "E" {
		t.Fatalf("source mutation leaked into copy: %+v", *got)
	}
}

func TestCopyVerificationContract_AbsentTargetStateStaysAbsent(t *testing.T) {
	copied := copyVerificationContract(verification.Contract{})
	if copied.ObservationContext.TargetState != nil || copied.Observe.TargetState {
		t.Fatalf("absent target state must remain absent: %+v", copied)
	}
}

func TestCopyObserved_TargetStateEvidenceDoesNotAlias(t *testing.T) {
	yes, no := true, false
	src := &observed.Observed{
		SchemaVersion: observed.SchemaVersion,
		Observation: observed.Observation{
			TargetState: &observed.TargetStateObservation{
				Suppression: []observed.BooleanTargetEvidence{{Destination: "part", Object: "S", Status: observed.BooleanEvidenceStatusObserved, Value: &yes}},
				Visibility:  []observed.BooleanTargetEvidence{{Destination: "part", Object: "V", Status: observed.BooleanEvidenceStatusObserved, Value: &no}},
				Existence:   []observed.ExistenceTargetEvidence{{Destination: "part", Object: "E", Status: observed.ExistenceEvidenceStatusExists}},
			},
		},
	}
	copied := copyObserved(src)
	if copied.Observation.TargetState == src.Observation.TargetState {
		t.Fatal("TargetState pointer must be copied")
	}
	if copied.Observation.TargetState.Suppression[0].Value == src.Observation.TargetState.Suppression[0].Value ||
		copied.Observation.TargetState.Visibility[0].Value == src.Observation.TargetState.Visibility[0].Value {
		t.Fatal("*bool evidence values must be deep-copied")
	}
	if *copied.Observation.TargetState.Suppression[0].Value != true || *copied.Observation.TargetState.Visibility[0].Value != false {
		t.Fatal("copied *bool values must equal the source")
	}

	// Mutating the copy must not change the source.
	*copied.Observation.TargetState.Suppression[0].Value = false
	*copied.Observation.TargetState.Visibility[0].Value = true
	copied.Observation.TargetState.Suppression[0].Status = "mutated"
	copied.Observation.TargetState.Visibility[0].Object = "mutated"
	copied.Observation.TargetState.Existence[0].Status = "mutated"
	copied.Observation.TargetState.Existence = append(copied.Observation.TargetState.Existence, observed.ExistenceTargetEvidence{})
	ts := src.Observation.TargetState
	if !yes || no || ts.Suppression[0].Status != observed.BooleanEvidenceStatusObserved || ts.Visibility[0].Object != "V" ||
		ts.Existence[0].Status != observed.ExistenceEvidenceStatusExists || len(ts.Existence) != 1 {
		t.Fatalf("copy mutation leaked into source: %+v yes=%v no=%v", *ts, yes, no)
	}

	// Mutating the source must not change a fresh copy.
	fresh := copyObserved(src)
	yes, no = false, true
	ts.Suppression[0].Object = "src-mutated"
	ts.Existence[0].Object = "src-mutated"
	fts := fresh.Observation.TargetState
	if !*fts.Suppression[0].Value || *fts.Visibility[0].Value || fts.Suppression[0].Object != "S" || fts.Existence[0].Object != "E" {
		t.Fatalf("source mutation leaked into copy: %+v", *fts)
	}
}

func TestCopyObserved_NonObservedEvidenceKeepsNilValue(t *testing.T) {
	src := &observed.Observed{Observation: observed.Observation{TargetState: &observed.TargetStateObservation{
		Suppression: []observed.BooleanTargetEvidence{
			{Destination: "part", Object: "M", Status: observed.BooleanEvidenceStatusTargetMissing},
			{Destination: "part", Object: "U", Status: observed.BooleanEvidenceStatusUnavailable},
		},
		Visibility: []observed.BooleanTargetEvidence{},
		Existence:  []observed.ExistenceTargetEvidence{},
	}}}
	copied := copyObserved(src)
	for _, entry := range copied.Observation.TargetState.Suppression {
		if entry.Value != nil {
			t.Fatalf("copy must not fabricate a value for %s: %v", entry.Status, *entry.Value)
		}
	}
}

func TestCopyObserved_AbsentTargetStateStaysAbsent(t *testing.T) {
	if copied := copyObserved(&observed.Observed{}); copied.Observation.TargetState != nil {
		t.Fatalf("absent target state must remain nil: %+v", copied.Observation.TargetState)
	}
}

func TestDeepCopyVerifiedRun_TargetStateIsIndependent(t *testing.T) {
	yes := true
	run := FreeCADRuntimeVerifiedRun{}
	run.Runtime.ObservationRequest.Contract = verification.Contract{
		Observe: verification.Observe{TargetState: true},
		ObservationContext: verification.ObservationContext{TargetState: &verification.TargetStateRequest{
			Suppression: []verification.TargetIdentity{{Destination: "part", Object: "S"}},
			Visibility:  []verification.TargetIdentity{},
			Existence:   []verification.TargetIdentity{},
		}},
	}
	run.Runtime.Observed = &observed.Observed{Observation: observed.Observation{TargetState: &observed.TargetStateObservation{
		Suppression: []observed.BooleanTargetEvidence{{Destination: "part", Object: "S", Status: observed.BooleanEvidenceStatusObserved, Value: &yes}},
		Visibility:  []observed.BooleanTargetEvidence{},
		Existence:   []observed.ExistenceTargetEvidence{},
	}}}
	copied := deepCopyVerifiedRun(run)
	copied.Runtime.ObservationRequest.Contract.ObservationContext.TargetState.Suppression[0].Object = "mutated"
	*copied.Runtime.Observed.Observation.TargetState.Suppression[0].Value = false
	if run.Runtime.ObservationRequest.Contract.ObservationContext.TargetState.Suppression[0].Object != "S" || !yes {
		t.Fatal("deepCopyVerifiedRun leaked target-state mutation into the stored run")
	}
}
