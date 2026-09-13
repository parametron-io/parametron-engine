package recordmap

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/engine/observed"
	"parametron/internal/engine/recordcontract"
)

func TestMapObservedToObservationRecordMapsObservedFacts(t *testing.T) {
	input := observedMapInput(observedMapObserved())

	record, err := MapObservedToObservationRecord(input)
	if err != nil {
		t.Fatalf("MapObservedToObservationRecord returned error: %v", err)
	}
	if record.Family != recordcontract.FamilyObservation {
		t.Fatalf("Family = %q, want %q", record.Family, recordcontract.FamilyObservation)
	}
	if record.Version != recordcontract.CurrentVersion {
		t.Fatalf("Version = %q, want %q", record.Version, recordcontract.CurrentVersion)
	}
	if err := recordcontract.ValidateObservationRecord(record); err != nil {
		t.Fatalf("ValidateObservationRecord(mapped) returned error: %v", err)
	}

	kinds := make(map[recordcontract.ObservationKind]bool)
	for _, fact := range record.Observation.Facts {
		kinds[fact.Kind] = true
		if fact.Evidence.SourceKind != observedEvidenceKind {
			t.Fatalf("fact evidence source kind = %q, want %q in %#v", fact.Evidence.SourceKind, observedEvidenceKind, fact)
		}
		if fact.Evidence.SourceRef != observedEvidenceRef {
			t.Fatalf("fact evidence source ref = %q, want %q in %#v", fact.Evidence.SourceRef, observedEvidenceRef, fact)
		}
		if fact.Evidence.DigestSHA256 != observedMapDigestA() {
			t.Fatalf("fact evidence digest = %q, want %q", fact.Evidence.DigestSHA256, observedMapDigestA())
		}
		if fact.Linkage.JobID != input.Linkage.JobID || fact.Linkage.ProductKey != input.Linkage.ProductKey || fact.Linkage.StepRef != input.Linkage.StepRef {
			t.Fatalf("fact linkage = %#v, want caller linkage %#v", fact.Linkage, input.Linkage)
		}
		if fact.Subject.ID == observedEvidenceRef || fact.Subject.Name == observedEvidenceRef || fact.Key == observedEvidenceRef || fact.Value.Raw == observedEvidenceRef {
			t.Fatalf("raw observed JSON ref was mapped as normalized fact payload: %#v", fact)
		}
	}
	for _, kind := range []recordcontract.ObservationKind{
		recordcontract.ObservationKindComponent,
		recordcontract.ObservationKindParameter,
		recordcontract.ObservationKindMetadata,
		recordcontract.ObservationKindReference,
	} {
		if !kinds[kind] {
			t.Fatalf("observation facts missing kind %q in %#v", kind, record.Observation.Facts)
		}
	}

	if record.Provenance.Runtime != input.Provenance.Runtime {
		t.Fatalf("runtime provenance = %#v, want caller runtime %#v", record.Provenance.Runtime, input.Provenance.Runtime)
	}
	if record.Provenance.Plan != input.Provenance.Plan {
		t.Fatalf("plan provenance = %#v, want caller plan %#v", record.Provenance.Plan, input.Provenance.Plan)
	}
	if !observedMapHasEvidence(record.Provenance.Evidence, "runtime", "raw/runtime.json", observedMapDigestB()) {
		t.Fatalf("caller evidence not preserved: %#v", record.Provenance.Evidence)
	}
	if !observedMapHasEvidence(record.Provenance.Evidence, observedEvidenceKind, observedEvidenceRef, observedMapDigestA()) {
		t.Fatalf("observed evidence not appended: %#v", record.Provenance.Evidence)
	}
	if observedMapHasInputIdentity(record.Provenance.Inputs, observedEvidenceRef) {
		t.Fatalf("raw observed JSON was represented as provenance input: %#v", record.Provenance.Inputs)
	}
}

func TestMapObservedToReferenceRecordMapsObservedReferences(t *testing.T) {
	input := observedMapInput(observedMapObserved())

	record, err := MapObservedToReferenceRecord(input)
	if err != nil {
		t.Fatalf("MapObservedToReferenceRecord returned error: %v", err)
	}
	if record == nil {
		t.Fatal("MapObservedToReferenceRecord returned nil record, want reference record")
	}
	if err := recordcontract.ValidateReferenceRecord(*record); err != nil {
		t.Fatalf("ValidateReferenceRecord(mapped) returned error: %v", err)
	}
	if len(record.Reference.Edges) != len(input.Observed.Observation.References) {
		t.Fatalf("len(Edges) = %d, want %d", len(record.Reference.Edges), len(input.Observed.Observation.References))
	}

	observedRefs := make(map[string]string)
	for _, ref := range input.Observed.Observation.References {
		observedRefs[ref.Name] = ref.Kind
	}
	for _, edge := range record.Reference.Edges {
		if edge.Source.Path != input.Observed.WorkingCopy.Path {
			t.Fatalf("edge source path = %q, want %q", edge.Source.Path, input.Observed.WorkingCopy.Path)
		}
		if edge.Source.DigestSHA256 != input.Observed.WorkingCopy.SHA256 {
			t.Fatalf("edge source digest = %q, want %q", edge.Source.DigestSHA256, input.Observed.WorkingCopy.SHA256)
		}
		wantRole, ok := observedRefs[edge.Target.Name]
		if !ok {
			t.Fatalf("edge target name %q not found in observed references", edge.Target.Name)
		}
		if edge.Role != wantRole {
			t.Fatalf("edge role = %q, want observed kind %q", edge.Role, wantRole)
		}
		if edge.Resolution != recordcontract.ReferenceResolutionResolved {
			t.Fatalf("edge resolution = %q, want %q", edge.Resolution, recordcontract.ReferenceResolutionResolved)
		}
		if edge.Evidence.SourceKind != observedEvidenceKind || edge.Evidence.SourceRef != observedEvidenceRef {
			t.Fatalf("edge evidence = %#v, want observed raw evidence", edge.Evidence)
		}
		if edge.Evidence.DigestSHA256 != observedMapDigestA() {
			t.Fatalf("edge evidence digest = %q, want %q", edge.Evidence.DigestSHA256, observedMapDigestA())
		}
		if edge.Source.Path == observedEvidenceRef || edge.Target.Name == observedEvidenceRef {
			t.Fatalf("raw observed JSON ref was mapped as reference endpoint payload: %#v", edge)
		}
	}

	repeated, err := MapObservedToReferenceRecord(input)
	if err != nil {
		t.Fatalf("MapObservedToReferenceRecord repeated call returned error: %v", err)
	}
	if !reflect.DeepEqual(record, repeated) {
		t.Fatalf("reference record is not deterministic:\nfirst:  %#v\nsecond: %#v", record, repeated)
	}
}

func TestMapObservedReturnsObservationAndReferenceRecords(t *testing.T) {
	output, err := MapObserved(observedMapInput(observedMapObserved()))
	if err != nil {
		t.Fatalf("MapObserved returned error: %v", err)
	}
	if err := recordcontract.ValidateObservationRecord(output.ObservationRecord); err != nil {
		t.Fatalf("ValidateObservationRecord(mapped) returned error: %v", err)
	}
	if output.ReferenceRecord == nil {
		t.Fatal("ReferenceRecord = nil, want mapped reference record")
	}
	if err := recordcontract.ValidateReferenceRecord(*output.ReferenceRecord); err != nil {
		t.Fatalf("ValidateReferenceRecord(mapped) returned error: %v", err)
	}
}

func TestMapObservedEmptyReferences(t *testing.T) {
	obs := observedMapObserved()
	obs.Observation.References = []observed.Reference{}
	input := observedMapInput(obs)

	observationRecord, err := MapObservedToObservationRecord(input)
	if err != nil {
		t.Fatalf("MapObservedToObservationRecord returned error: %v", err)
	}
	if err := recordcontract.ValidateObservationRecord(observationRecord); err != nil {
		t.Fatalf("ValidateObservationRecord(mapped) returned error: %v", err)
	}
	if observedMapHasObservationKind(observationRecord, recordcontract.ObservationKindReference) {
		t.Fatalf("observation record contains reference fact with empty observed references: %#v", observationRecord.Observation.Facts)
	}

	referenceRecord, err := MapObservedToReferenceRecord(input)
	if err != nil {
		t.Fatalf("MapObservedToReferenceRecord returned error: %v", err)
	}
	if referenceRecord != nil {
		t.Fatalf("MapObservedToReferenceRecord = %#v, want nil", referenceRecord)
	}

	output, err := MapObserved(input)
	if err != nil {
		t.Fatalf("MapObserved returned error: %v", err)
	}
	if output.ReferenceRecord != nil {
		t.Fatalf("MapObserved ReferenceRecord = %#v, want nil", output.ReferenceRecord)
	}
}

func TestMapObservedInvalidInputWrapsErrInvalidObservedMapping(t *testing.T) {
	tests := []struct {
		name   string
		input  ObservedMappingInput
		mutate func(*ObservedMappingInput)
	}{
		{name: "missing record key", mutate: func(input *ObservedMappingInput) { input.RecordKey = " " }},
		{name: "invalid observed payload", mutate: func(input *ObservedMappingInput) { input.Observed.WorkingCopy.Path = "relative/path.FCStd" }},
		{name: "uppercase evidence digest", mutate: func(input *ObservedMappingInput) { input.EvidenceDigestSHA256 = strings.ToUpper(observedMapDigestA()) }},
		{name: "malformed evidence digest", mutate: func(input *ObservedMappingInput) { input.EvidenceDigestSHA256 = "abc123" }},
		{name: "blank reference record key", mutate: func(input *ObservedMappingInput) { input.ReferenceRecordKey = " \t" }},
		{name: "conflicting observed evidence digest", mutate: func(input *ObservedMappingInput) {
			input.EvidenceDigestSHA256 = observedMapDigestA()
			input.Provenance.Evidence = append(input.Provenance.Evidence, recordcontract.EvidenceReference{
				Kind:         observedEvidenceKind,
				Ref:          observedEvidenceRef,
				DigestSHA256: observedMapDigestC(),
			})
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := observedMapInput(observedMapObserved())
			if tc.input.RecordKey != "" {
				input = tc.input
			}
			if tc.mutate != nil {
				tc.mutate(&input)
			}

			_, err := MapObservedToObservationRecord(input)
			if !errors.Is(err, ErrInvalidObservedMapping) {
				t.Fatalf("MapObservedToObservationRecord error = %v, want errors.Is(..., ErrInvalidObservedMapping)", err)
			}
		})
	}
}

func TestMapObservedDoesNotDuplicateExistingObservedEvidence(t *testing.T) {
	input := observedMapInput(observedMapObserved())
	input.Provenance.Evidence = append(input.Provenance.Evidence, recordcontract.EvidenceReference{
		Kind:         observedEvidenceKind,
		Ref:          observedEvidenceRef,
		DigestSHA256: observedMapDigestA(),
	})

	record, err := MapObservedToObservationRecord(input)
	if err != nil {
		t.Fatalf("MapObservedToObservationRecord returned error: %v", err)
	}
	if err := recordcontract.ValidateObservationRecord(record); err != nil {
		t.Fatalf("ValidateObservationRecord(mapped) returned error: %v", err)
	}
	if count := observedMapCountEvidence(record.Provenance.Evidence, observedEvidenceKind, observedEvidenceRef); count != 1 {
		t.Fatalf("observed evidence count = %d, want 1 in %#v", count, record.Provenance.Evidence)
	}
}

func TestMapObservedDeterministicAcrossInputOrder(t *testing.T) {
	firstInput := observedMapInput(observedMapObserved())
	secondInput := observedMapInput(observedMapObservedReordered())

	firstObservation, err := MapObservedToObservationRecord(firstInput)
	if err != nil {
		t.Fatalf("MapObservedToObservationRecord(first) returned error: %v", err)
	}
	secondObservation, err := MapObservedToObservationRecord(secondInput)
	if err != nil {
		t.Fatalf("MapObservedToObservationRecord(second) returned error: %v", err)
	}
	if !reflect.DeepEqual(firstObservation, secondObservation) {
		t.Fatalf("observation records differ across input order:\nfirst:  %#v\nsecond: %#v", firstObservation, secondObservation)
	}

	firstReference, err := MapObservedToReferenceRecord(firstInput)
	if err != nil {
		t.Fatalf("MapObservedToReferenceRecord(first) returned error: %v", err)
	}
	secondReference, err := MapObservedToReferenceRecord(secondInput)
	if err != nil {
		t.Fatalf("MapObservedToReferenceRecord(second) returned error: %v", err)
	}
	if !reflect.DeepEqual(firstReference, secondReference) {
		t.Fatalf("reference records differ across input order:\nfirst:  %#v\nsecond: %#v", firstReference, secondReference)
	}

	repeatedObservation, err := MapObservedToObservationRecord(firstInput)
	if err != nil {
		t.Fatalf("MapObservedToObservationRecord(repeated) returned error: %v", err)
	}
	if !reflect.DeepEqual(firstObservation, repeatedObservation) {
		t.Fatalf("observation record differs across repeated calls:\nfirst:   %#v\nrepeated:%#v", firstObservation, repeatedObservation)
	}
	repeatedReference, err := MapObservedToReferenceRecord(firstInput)
	if err != nil {
		t.Fatalf("MapObservedToReferenceRecord(repeated) returned error: %v", err)
	}
	if !reflect.DeepEqual(firstReference, repeatedReference) {
		t.Fatalf("reference record differs across repeated calls:\nfirst:   %#v\nrepeated:%#v", firstReference, repeatedReference)
	}
}

func TestMapObservedCopySafetyNoMutation(t *testing.T) {
	input := observedMapInput(observedMapObserved())
	input.Provenance.Evidence = append(input.Provenance.Evidence, recordcontract.EvidenceReference{
		Kind: observedEvidenceKind,
		Ref:  observedEvidenceRef,
	})
	originalCallerEvidence := append([]recordcontract.EvidenceReference(nil), input.Provenance.Evidence...)

	observationRecord, err := MapObservedToObservationRecord(input)
	if err != nil {
		t.Fatalf("MapObservedToObservationRecord returned error: %v", err)
	}
	referenceRecord, err := MapObservedToReferenceRecord(input)
	if err != nil {
		t.Fatalf("MapObservedToReferenceRecord returned error: %v", err)
	}
	if !reflect.DeepEqual(input.Provenance.Evidence, originalCallerEvidence) {
		t.Fatalf("caller provenance evidence mutated:\nbefore: %#v\nafter:  %#v", originalCallerEvidence, input.Provenance.Evidence)
	}

	observationBeforeMutation := observationRecord
	referenceBeforeMutation := *referenceRecord
	input.Observed.WorkingCopy.Path = "/mutated/model.FCStd"
	input.Observed.WorkingCopy.SHA256 = observedMapDigestC()
	input.Observed.Observation.Components[0].Name = "mutated"
	input.Observed.Observation.Parameters[0].Name = "mutated"
	input.Observed.Observation.Metadata[0].Key = "mutated"
	input.Observed.Observation.References[0].Name = "mutated"
	input.Provenance.Evidence[0].Ref = "mutated"

	if !reflect.DeepEqual(observationRecord, observationBeforeMutation) {
		t.Fatalf("previously returned observation record changed after input mutation:\nbefore: %#v\nafter:  %#v", observationBeforeMutation, observationRecord)
	}
	if !reflect.DeepEqual(*referenceRecord, referenceBeforeMutation) {
		t.Fatalf("previously returned reference record changed after input mutation:\nbefore: %#v\nafter:  %#v", referenceBeforeMutation, *referenceRecord)
	}

	freshObservation, err := MapObservedToObservationRecord(observedMapInput(observedMapObserved()))
	if err != nil {
		t.Fatalf("MapObservedToObservationRecord(fresh) returned error: %v", err)
	}
	if !reflect.DeepEqual(observationBeforeMutation, freshObservation) {
		t.Fatalf("fresh equivalent observation record differs:\noriginal: %#v\nfresh:    %#v", observationBeforeMutation, freshObservation)
	}
}

func observedMapInput(obs observed.Observed) ObservedMappingInput {
	return ObservedMappingInput{
		Observed:             obs,
		RecordKey:            "run-123:observed",
		ReferenceRecordKey:   "run-123:observed:references",
		EvidenceDigestSHA256: observedMapDigestA(),
		Provenance:           observedMapProvenance(),
		Linkage:              ObservedMappingLinkage{JobID: "job-123", ProductKey: "product-abc", StepRef: "observe"},
	}
}

func observedMapObserved() observed.Observed {
	return observed.Observed{
		SchemaVersion: observed.SchemaVersion,
		WorkingCopy: observed.WorkingCopy{
			Path:   "/workspace/project/model.FCStd",
			SHA256: observedMapDigestB(),
		},
		Observation: observed.Observation{
			Components: []observed.Component{
				{ID: "component-root", Kind: observed.ComponentKindAssembly, Name: "Root"},
				{ID: "component-panel", Kind: observed.ComponentKindPart, Name: "Panel", ParentID: "component-root"},
			},
			Parameters: []observed.Parameter{
				{ID: "parameter-width", Name: "width", GroupID: "dimensions", ValueKind: "number", Value: observedMapValue(tJSON(`125.5`))},
				{ID: "parameter-enabled", Name: "enabled", ValueKind: "boolean", Value: observedMapValue(tJSON(`true`))},
			},
			Metadata: []observed.Metadata{
				{ID: "metadata-material", Key: "material", OwnerID: "component-panel", ValueKind: "string", Value: observedMapValue(tJSON(`"aluminum"`))},
				{ID: "metadata-revision", Key: "revision", ValueKind: "integer", Value: observedMapValue(tJSON(`7`))},
			},
			References: []observed.Reference{
				{Kind: "external", Name: "fastener-library"},
				{Kind: "document", Name: "spec-sheet"},
			},
		},
	}
}

func observedMapObservedReordered() observed.Observed {
	obs := observedMapObserved()
	obs.Observation.Components = []observed.Component{obs.Observation.Components[1], obs.Observation.Components[0]}
	obs.Observation.Parameters = []observed.Parameter{obs.Observation.Parameters[1], obs.Observation.Parameters[0]}
	obs.Observation.Metadata = []observed.Metadata{obs.Observation.Metadata[1], obs.Observation.Metadata[0]}
	obs.Observation.References = []observed.Reference{obs.Observation.References[1], obs.Observation.References[0]}
	return obs
}

func observedMapProvenance() recordcontract.Provenance {
	return recordcontract.Provenance{
		Inputs: []recordcontract.ProvenanceInput{
			{Kind: "dsl", Identity: "project/main.pmtn", DigestSHA256: observedMapDigestC()},
		},
		Plan: recordcontract.PlanProvenance{PlanID: "plan-123"},
		Linkage: recordcontract.LinkageProvenance{
			ProductKey: "product-abc",
			JobID:      "job-123",
			StepRef:    "observe",
		},
		Evidence: []recordcontract.EvidenceReference{
			{Kind: "runtime", Ref: "raw/runtime.json", DigestSHA256: observedMapDigestB()},
		},
		Runtime: recordcontract.RuntimeProvenance{
			ToolID:    "parametron",
			RuntimeID: "local",
			Adapter:   "freecad",
		},
	}
}

func observedMapValue(raw json.RawMessage) observed.Value {
	value, err := observed.NewValue(raw)
	if err != nil {
		panic(err)
	}
	return value
}

func tJSON(raw string) json.RawMessage {
	return json.RawMessage(raw)
}

func observedMapDigestA() string {
	return strings.Repeat("a", 64)
}

func observedMapDigestB() string {
	return strings.Repeat("b", 64)
}

func observedMapDigestC() string {
	return strings.Repeat("c", 64)
}

func observedMapHasEvidence(evidence []recordcontract.EvidenceReference, kind, ref, digest string) bool {
	for _, item := range evidence {
		if item.Kind == kind && item.Ref == ref && item.DigestSHA256 == digest {
			return true
		}
	}
	return false
}

func observedMapCountEvidence(evidence []recordcontract.EvidenceReference, kind, ref string) int {
	count := 0
	for _, item := range evidence {
		if item.Kind == kind && item.Ref == ref {
			count++
		}
	}
	return count
}

func observedMapHasInputIdentity(inputs []recordcontract.ProvenanceInput, identity string) bool {
	for _, input := range inputs {
		if input.Identity == identity {
			return true
		}
	}
	return false
}

func observedMapHasObservationKind(record recordcontract.ObservationRecord, kind recordcontract.ObservationKind) bool {
	for _, fact := range record.Observation.Facts {
		if fact.Kind == kind {
			return true
		}
	}
	return false
}
