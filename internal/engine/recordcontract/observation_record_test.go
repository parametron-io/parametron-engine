package recordcontract

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestBuildObservationRecordDerivesNormalizedIdentity(t *testing.T) {
	record, err := BuildObservationRecord(validObservationRecordInput())
	if err != nil {
		t.Fatalf("BuildObservationRecord(valid) returned error: %v", err)
	}

	if record.Family != FamilyObservation {
		t.Fatalf("Family = %q, want %q", record.Family, FamilyObservation)
	}
	if record.Version != CurrentVersion {
		t.Fatalf("Version = %q, want %q", record.Version, CurrentVersion)
	}
	if record.RecordKey != "observation-record-1" {
		t.Fatalf("RecordKey = %q, want trimmed observation-record-1", record.RecordKey)
	}
	if record.Provenance.SourceRevision.RevisionID != "rev-1" {
		t.Fatalf("Provenance.SourceRevision.RevisionID = %q, want normalized rev-1", record.Provenance.SourceRevision.RevisionID)
	}
	if got := record.Observation.Facts; len(got) != 4 {
		t.Fatalf("Facts length = %d, want 4", len(got))
	}
	if got := record.Observation.Facts[0].Kind; got != ObservationKindComponent {
		t.Fatalf("Facts[0].Kind = %q, want deterministic first component", got)
	}
	if record.Identity.ID == "" || !isLowerHex64(record.Identity.ID) {
		t.Fatalf("Identity.ID = %q, want non-empty lowercase SHA-256 hex", record.Identity.ID)
	}
	if record.Identity.Algorithm != IdentityAlgorithmSHA256CanonicalV1 {
		t.Fatalf("Identity.Algorithm = %q, want %q", record.Identity.Algorithm, IdentityAlgorithmSHA256CanonicalV1)
	}
	if record.Identity.Family != FamilyObservation {
		t.Fatalf("Identity.Family = %q, want %q", record.Identity.Family, FamilyObservation)
	}
	if record.Identity.Version != CurrentVersion {
		t.Fatalf("Identity.Version = %q, want %q", record.Identity.Version, CurrentVersion)
	}
	if err := ValidateObservationRecord(record); err != nil {
		t.Fatalf("ValidateObservationRecord(built) returned error: %v", err)
	}

	equivalent, err := BuildObservationRecord(equivalentObservationRecordInput())
	if err != nil {
		t.Fatalf("BuildObservationRecord(equivalent) returned error: %v", err)
	}
	if equivalent.Identity != record.Identity {
		t.Fatalf("equivalent identities differ:\nfirst:  %#v\nsecond: %#v", record.Identity, equivalent.Identity)
	}
	if !reflect.DeepEqual(equivalent, record) {
		t.Fatalf("equivalent records differ:\nfirst:  %#v\nsecond: %#v", record, equivalent)
	}
}

func TestNormalizeObservationRecordCanonicalizesAndCopies(t *testing.T) {
	built := mustBuildObservationRecord(t, validObservationRecordInput())
	input := ObservationRecord{
		Family:    Family(" \t" + string(FamilyObservation) + "\n"),
		Version:   " " + CurrentVersion + "\t",
		RecordKey: "\nobservation-record-1 ",
		Identity:  built.Identity,
		Provenance: Provenance{
			SourceRevision: SourceRevisionProvenance{RevisionID: " rev-1 "},
			Inputs: []ProvenanceInput{
				{Kind: " parameter-table ", Identity: " params-a ", DigestSHA256: digestC()},
				{Kind: "\tdsl", Identity: "model-a\n", DigestSHA256: digestD()},
			},
			Plan:    PlanProvenance{PlanID: " plan-1 ", PlanHash: " plan-hash-1 "},
			Linkage: LinkageProvenance{ProductKey: " product-a ", JobID: " job-a ", StepRef: " observe "},
			Evidence: []EvidenceReference{
				{Kind: " log ", Ref: "logs/run.log ", DigestSHA256: digestE()},
				{Kind: " artifact ", Ref: " out/observed.json ", DigestSHA256: digestF()},
			},
			Runtime: RuntimeProvenance{ToolID: " parametron ", RuntimeID: " runtime-1 ", Adapter: " freecad "},
		},
		Observation: ObservationSummary{
			Facts: []ObservationFact{
				referenceObservationFact(),
				parameterObservationFact(),
				componentObservationFact(),
				metadataObservationFact(),
			},
		},
	}

	normalized := NormalizeObservationRecord(input)

	if normalized.Family != FamilyObservation || normalized.Version != CurrentVersion || normalized.RecordKey != "observation-record-1" {
		t.Fatalf("root fields not normalized: %#v", normalized)
	}
	if got := normalized.Provenance.Inputs; got[0].Kind != "dsl" || got[1].Kind != "parameter-table" {
		t.Fatalf("provenance inputs not normalized deterministically: %#v", got)
	}
	if got := normalized.Observation.Facts; got[0].Kind != ObservationKindComponent || got[1].Kind != ObservationKindMetadata || got[2].Kind != ObservationKindParameter || got[3].Kind != ObservationKindReference {
		t.Fatalf("facts not sorted by semantic ordering: %#v", got)
	}
	if got := normalized.Observation.Facts[2]; got.Subject.ID != "part/body" || got.Key != "length" || got.Value.Raw != "42.0" || got.Linkage.StepRef != "observe" || got.Evidence.SourceRef != "runtime://observations/length" {
		t.Fatalf("fact fields not trimmed recursively: %#v", got)
	}

	empty := NormalizeObservationRecord(ObservationRecord{})
	if empty.Provenance.Inputs == nil || empty.Provenance.Evidence == nil || empty.Observation.Facts == nil {
		t.Fatalf("empty repeated collections should be non-nil: %#v", empty)
	}

	input.Provenance.Inputs[0].Identity = "mutated-input"
	input.Provenance.Evidence[0].Ref = "mutated-evidence"
	input.Observation.Facts[0].Subject.ID = "mutated-subject"
	if normalized.Provenance.Inputs[0].Identity != "model-a" {
		t.Fatalf("normalized provenance inputs changed after original mutation: %#v", normalized.Provenance.Inputs)
	}
	if normalized.Provenance.Evidence[0].Ref != "out/observed.json" {
		t.Fatalf("normalized provenance evidence changed after original mutation: %#v", normalized.Provenance.Evidence)
	}
	if normalized.Observation.Facts[3].Subject.ID != "part/body" {
		t.Fatalf("normalized facts changed after original mutation: %#v", normalized.Observation.Facts)
	}

	first := NormalizeObservationRecord(ObservationRecord{Observation: ObservationSummary{Facts: []ObservationFact{parameterObservationFact()}}})
	second := NormalizeObservationRecord(ObservationRecord{Observation: ObservationSummary{Facts: []ObservationFact{parameterObservationFact()}}})
	first.Observation.Facts[0].Subject.ID = "changed"
	if second.Observation.Facts[0].Subject.ID != "part/body" {
		t.Fatalf("separately normalized facts share mutable storage: %#v", second.Observation.Facts)
	}
	if input.Observation.Facts[2].Subject.ID != " part/body " {
		t.Fatalf("mutating normalized result changed original input: %#v", input.Observation.Facts)
	}
}

func TestObservationRecordIdentityChangesWithSemanticMaterial(t *testing.T) {
	base := validObservationRecordInput()
	baseRecord := mustBuildObservationRecord(t, base)

	tests := []struct {
		name   string
		mutate func(*ObservationRecordInput)
	}{
		{name: "fact kind", mutate: func(input *ObservationRecordInput) { input.Observation.Facts[0].Kind = ObservationKindMetadata }},
		{name: "subject identity", mutate: func(input *ObservationRecordInput) { input.Observation.Facts[0].Subject.ID = "part/body-v2" }},
		{name: "key", mutate: func(input *ObservationRecordInput) { input.Observation.Facts[0].Key = "width" }},
		{name: "value kind", mutate: func(input *ObservationRecordInput) { input.Observation.Facts[0].Value.Kind = "integer" }},
		{name: "value raw", mutate: func(input *ObservationRecordInput) { input.Observation.Facts[0].Value.Raw = "43.0" }},
		{name: "linkage", mutate: func(input *ObservationRecordInput) { input.Observation.Facts[0].Linkage.StepRef = "reobserve" }},
		{name: "evidence digest", mutate: func(input *ObservationRecordInput) { input.Observation.Facts[0].Evidence.DigestSHA256 = digestA() }},
		{name: "evidence source ref", mutate: func(input *ObservationRecordInput) {
			input.Observation.Facts[0].Evidence.SourceRef = "runtime://observations/length-v2"
		}},
		{name: "provenance", mutate: func(input *ObservationRecordInput) { input.Provenance.SourceRevision.RevisionID = "rev-2" }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			changed := cloneObservationRecordInput(base)
			tc.mutate(&changed)

			got, err := BuildObservationRecord(changed)
			if err != nil {
				t.Fatalf("BuildObservationRecord(changed) returned error: %v", err)
			}
			if got.Identity.ID == baseRecord.Identity.ID {
				t.Fatalf("identity ID did not change after %s changed: %q", tc.name, got.Identity.ID)
			}
		})
	}

	equivalent := mustBuildObservationRecord(t, equivalentObservationRecordInput())
	if equivalent.Identity != baseRecord.Identity {
		t.Fatalf("identity changed for order/whitespace-only input:\nbase: %#v\nequiv:%#v", baseRecord.Identity, equivalent.Identity)
	}
}

func TestValidateObservationRecordAcceptsSupportedContract(t *testing.T) {
	record := mustBuildObservationRecord(t, validObservationRecordInput())
	if err := ValidateObservationRecord(record); err != nil {
		t.Fatalf("ValidateObservationRecord(built) returned error: %v", err)
	}

	for _, kind := range []ObservationKind{
		ObservationKindParameter,
		ObservationKindMetadata,
		ObservationKindReference,
		ObservationKindComponent,
	} {
		t.Run(string(kind), func(t *testing.T) {
			input := validObservationRecordInput()
			input.Observation.Facts = []ObservationFact{validObservationFactForKind(kind)}
			record := mustBuildObservationRecord(t, input)
			if err := ValidateObservationRecord(record); err != nil {
				t.Fatalf("ValidateObservationRecord(%s) returned error: %v", kind, err)
			}
		})
	}
}

func TestValidateObservationRecordRejectsInvalidRecords(t *testing.T) {
	valid := mustBuildObservationRecord(t, validObservationRecordInput())

	tests := []struct {
		name   string
		mutate func(*ObservationRecord)
	}{
		{name: "wrong family", mutate: func(record *ObservationRecord) { record.Family = FamilyArtifact }},
		{name: "unsupported version", mutate: func(record *ObservationRecord) { record.Version = "1.1" }},
		{name: "missing record key", mutate: func(record *ObservationRecord) { record.RecordKey = " " }},
		{name: "invalid provenance", mutate: func(record *ObservationRecord) { record.Provenance.Inputs[0].DigestSHA256 = upperDigestA() }},
		{name: "empty observation fact list", mutate: func(record *ObservationRecord) { record.Observation.Facts = nil }},
		{name: "unsupported observation kind", mutate: func(record *ObservationRecord) { record.Observation.Facts[0].Kind = "dimension" }},
		{name: "missing subject identity", mutate: func(record *ObservationRecord) {
			record.Observation.Facts[0].Subject.ID = " "
			record.Observation.Facts[0].Subject.Name = "\t"
		}},
		{name: "parameter missing value kind", mutate: func(record *ObservationRecord) {
			record.Observation.Facts = []ObservationFact{parameterObservationFact()}
			record.Observation.Facts[0].Value.Kind = " "
		}},
		{name: "parameter missing value raw", mutate: func(record *ObservationRecord) {
			record.Observation.Facts = []ObservationFact{parameterObservationFact()}
			record.Observation.Facts[0].Value.Raw = "\n"
		}},
		{name: "metadata missing key", mutate: func(record *ObservationRecord) {
			record.Observation.Facts = []ObservationFact{metadataObservationFact()}
			record.Observation.Facts[0].Key = " "
		}},
		{name: "metadata missing value kind", mutate: func(record *ObservationRecord) {
			record.Observation.Facts = []ObservationFact{metadataObservationFact()}
			record.Observation.Facts[0].Value.Kind = " "
		}},
		{name: "metadata missing value raw", mutate: func(record *ObservationRecord) {
			record.Observation.Facts = []ObservationFact{metadataObservationFact()}
			record.Observation.Facts[0].Value.Raw = " "
		}},
		{name: "reference missing key and value raw", mutate: func(record *ObservationRecord) {
			record.Observation.Facts = []ObservationFact{referenceObservationFact()}
			record.Observation.Facts[0].Key = " "
			record.Observation.Facts[0].Value.Raw = "\t"
		}},
		{name: "invalid evidence digest length", mutate: func(record *ObservationRecord) { record.Observation.Facts[0].Evidence.DigestSHA256 = "abc123" }},
		{name: "invalid evidence digest casing", mutate: func(record *ObservationRecord) { record.Observation.Facts[0].Evidence.DigestSHA256 = upperDigestA() }},
		{name: "invalid evidence digest non hex", mutate: func(record *ObservationRecord) {
			record.Observation.Facts[0].Evidence.DigestSHA256 = "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"
		}},
		{name: "evidence source kind without source ref", mutate: func(record *ObservationRecord) { record.Observation.Facts[0].Evidence.SourceRef = " " }},
		{name: "evidence source ref without source kind", mutate: func(record *ObservationRecord) { record.Observation.Facts[0].Evidence.SourceKind = "\t" }},
		{name: "duplicate normalized facts", mutate: func(record *ObservationRecord) {
			record.Observation.Facts = []ObservationFact{parameterObservationFact(), parameterObservationFact()}
		}},
		{name: "identity mismatch after payload mutation", mutate: func(record *ObservationRecord) { record.Observation.Facts[0].Value.Raw = "43.0" }},
		{name: "missing identity", mutate: func(record *ObservationRecord) { record.Identity = Identity{} }},
		{name: "malformed stored identity", mutate: func(record *ObservationRecord) { record.Identity.ID = "not-a-digest" }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			record := cloneObservationRecord(valid)
			tc.mutate(&record)

			if err := ValidateObservationRecord(record); !errors.Is(err, ErrInvalidObservationRecord) {
				t.Fatalf("ValidateObservationRecord error = %v, want ErrInvalidObservationRecord", err)
			}
		})
	}
}

func TestObservationRecordValidationErrorsAreInspectable(t *testing.T) {
	record := mustBuildObservationRecord(t, validObservationRecordInput())

	tests := []struct {
		name       string
		record     ObservationRecord
		wantErrors []error
	}{
		{
			name: "direct observation validation failure",
			record: withObservationRecord(record, func(record *ObservationRecord) {
				record.Observation.Facts[0].Kind = "dimension"
			}),
			wantErrors: []error{ErrInvalidObservationRecord},
		},
		{
			name: "invalid provenance",
			record: withObservationRecord(record, func(record *ObservationRecord) {
				record.Provenance.Inputs[0].DigestSHA256 = "abc123"
			}),
			wantErrors: []error{ErrInvalidObservationRecord, ErrInvalidProvenance},
		},
		{
			name: "invalid identity",
			record: withObservationRecord(record, func(record *ObservationRecord) {
				record.Identity.ID = "not-a-digest"
			}),
			wantErrors: []error{ErrInvalidObservationRecord, ErrInvalidIdentity},
		},
		{
			name: "invalid version",
			record: withObservationRecord(record, func(record *ObservationRecord) {
				record.Version = "2.0"
			}),
			wantErrors: []error{ErrInvalidObservationRecord, ErrInvalidVersion},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateObservationRecord(tc.record)
			if err == nil {
				t.Fatal("ValidateObservationRecord returned nil error, want validation failure")
			}
			for _, want := range tc.wantErrors {
				if !errors.Is(err, want) {
					t.Fatalf("ValidateObservationRecord error = %v, want errors.Is(..., %v)", err, want)
				}
			}
		})
	}
}

func TestNormalizeObservationKind(t *testing.T) {
	tests := []struct {
		name string
		in   ObservationKind
		want ObservationKind
	}{
		{name: "parameter", in: " parameter ", want: ObservationKindParameter},
		{name: "metadata", in: "\tmetadata\n", want: ObservationKindMetadata},
		{name: "reference", in: " reference", want: ObservationKindReference},
		{name: "component", in: "component ", want: ObservationKindComponent},
		{name: "unknown preserved", in: " dimension ", want: "dimension"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeObservationKind(tc.in); got != tc.want {
				t.Fatalf("NormalizeObservationKind(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestObservationRecordJSONContractRootFields(t *testing.T) {
	record := mustBuildObservationRecord(t, validObservationRecordInput())

	payload, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("json.Marshal(ObservationRecord) returned error: %v", err)
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(payload, &root); err != nil {
		t.Fatalf("json.Unmarshal(root) returned error: %v", err)
	}
	for _, field := range []string{"family", "version", "recordKey", "identity", "provenance", "observation"} {
		if _, ok := root[field]; !ok {
			t.Fatalf("root JSON field %q missing from %s", field, payload)
		}
	}
	for _, field := range []string{"parametron.observed.json", "observed", "rawObserved", "bridgeOutput"} {
		if _, ok := root[field]; ok {
			t.Fatalf("raw runtime output root field %q should not be exposed in observation contract JSON: %s", field, payload)
		}
	}

	var observation map[string]json.RawMessage
	if err := json.Unmarshal(root["observation"], &observation); err != nil {
		t.Fatalf("json.Unmarshal(observation) returned error: %v", err)
	}
	if _, ok := observation["facts"]; !ok {
		t.Fatalf("observation JSON field %q missing from %s", "facts", payload)
	}
}

func validObservationRecordInput() ObservationRecordInput {
	return ObservationRecordInput{
		RecordKey: " observation-record-1 ",
		Provenance: Provenance{
			SourceRevision: SourceRevisionProvenance{
				RevisionID:   " rev-1 ",
				AssetID:      " asset-1 ",
				DigestSHA256: digestA(),
			},
			Inputs: []ProvenanceInput{
				{Kind: " parameter-table ", Identity: " params-a ", DigestSHA256: digestC()},
				{Kind: " dsl ", Identity: " model-a ", DigestSHA256: digestD()},
			},
			Plan: PlanProvenance{
				PlanID:   " plan-1 ",
				PlanHash: " plan-hash-1 ",
			},
			Linkage: LinkageProvenance{
				ProductKey: " product-a ",
				JobID:      " job-a ",
				StepRef:    " observe ",
			},
			Evidence: []EvidenceReference{
				{Kind: " log ", Ref: " logs/run.log ", DigestSHA256: digestE()},
				{Kind: " artifact ", Ref: " out/observed.json ", DigestSHA256: digestF()},
			},
			Runtime: RuntimeProvenance{
				ToolID:    " parametron ",
				RuntimeID: " runtime-1 ",
				Adapter:   " freecad ",
			},
		},
		Observation: ObservationSummary{
			Facts: []ObservationFact{
				parameterObservationFact(),
				metadataObservationFact(),
				referenceObservationFact(),
				componentObservationFact(),
			},
		},
	}
}

func equivalentObservationRecordInput() ObservationRecordInput {
	input := validObservationRecordInput()
	input.RecordKey = "\tobservation-record-1\n"
	input.Provenance.Inputs = []ProvenanceInput{
		{Kind: "dsl", Identity: "model-a", DigestSHA256: " " + digestD() + " "},
		{Kind: "parameter-table", Identity: "params-a", DigestSHA256: digestC()},
	}
	input.Provenance.Evidence = []EvidenceReference{
		{Kind: "artifact", Ref: "out/observed.json", DigestSHA256: digestF()},
		{Kind: "log", Ref: "logs/run.log", DigestSHA256: digestE()},
	}
	input.Observation.Facts = []ObservationFact{
		withObservationFact(referenceObservationFact(), func(fact *ObservationFact) {
			fact.Kind = "\treference\n"
			fact.Subject.ID = " part/body "
			fact.Key = " source-model "
			fact.Value.Raw = "\tmodel.FCStd "
			fact.Linkage.StepRef = " observe\n"
			fact.Evidence.DigestSHA256 = "\t" + digestE() + " "
		}),
		withObservationFact(componentObservationFact(), func(fact *ObservationFact) {
			fact.Kind = " component "
			fact.Subject.Name = "\tBody\n"
			fact.Linkage.ProductKey = " product-a\n"
		}),
		withObservationFact(metadataObservationFact(), func(fact *ObservationFact) {
			fact.Kind = "\nmetadata "
			fact.Key = "\tmaterial "
			fact.Value.Kind = " string\n"
			fact.Value.Raw = " aluminum "
		}),
		withObservationFact(parameterObservationFact(), func(fact *ObservationFact) {
			fact.Kind = " parameter "
			fact.Subject.ID = "\tpart/body\n"
			fact.Key = " length "
			fact.Value.Kind = "\tnumber "
			fact.Value.Raw = " 42.0\n"
		}),
	}
	return input
}

func parameterObservationFact() ObservationFact {
	return ObservationFact{
		Kind: ObservationKindParameter,
		Subject: ObservationSubject{
			ID:       " part/body ",
			Name:     " Body ",
			GroupID:  " group-a ",
			OwnerID:  " owner-a ",
			ParentID: " assembly/root ",
		},
		Key:   " length ",
		Value: ObservationValue{Kind: " number ", Raw: " 42.0 "},
		Linkage: ObservationLinkage{
			JobID:      " job-a ",
			ProductKey: " product-a ",
			StepRef:    " observe ",
		},
		Evidence: ObservationEvidence{
			SourceKind:   " runtime ",
			SourceRef:    " runtime://observations/length ",
			DigestSHA256: " " + digestB() + " ",
		},
	}
}

func metadataObservationFact() ObservationFact {
	return ObservationFact{
		Kind:    ObservationKindMetadata,
		Subject: ObservationSubject{ID: " part/body ", Name: " Body "},
		Key:     " material ",
		Value:   ObservationValue{Kind: " string ", Raw: " aluminum "},
		Linkage: ObservationLinkage{
			JobID:      " job-a ",
			ProductKey: " product-a ",
			StepRef:    " observe ",
		},
		Evidence: ObservationEvidence{
			SourceKind:   " runtime ",
			SourceRef:    " runtime://observations/material ",
			DigestSHA256: digestC(),
		},
	}
}

func referenceObservationFact() ObservationFact {
	return ObservationFact{
		Kind:    ObservationKindReference,
		Subject: ObservationSubject{ID: " part/body ", Name: " Body "},
		Key:     " source-model ",
		Value:   ObservationValue{Kind: " path ", Raw: " model.FCStd "},
		Linkage: ObservationLinkage{
			JobID:      " job-a ",
			ProductKey: " product-a ",
			StepRef:    " observe ",
		},
		Evidence: ObservationEvidence{
			SourceKind:   " runtime ",
			SourceRef:    " runtime://observations/source-model ",
			DigestSHA256: digestE(),
		},
	}
}

func componentObservationFact() ObservationFact {
	return ObservationFact{
		Kind:    ObservationKindComponent,
		Subject: ObservationSubject{ID: " part/body ", Name: " Body "},
		Linkage: ObservationLinkage{
			JobID:      " job-a ",
			ProductKey: " product-a ",
			StepRef:    " observe ",
		},
		Evidence: ObservationEvidence{
			SourceKind:   " runtime ",
			SourceRef:    " runtime://observations/component/body ",
			DigestSHA256: digestF(),
		},
	}
}

func validObservationFactForKind(kind ObservationKind) ObservationFact {
	switch kind {
	case ObservationKindParameter:
		return parameterObservationFact()
	case ObservationKindMetadata:
		return metadataObservationFact()
	case ObservationKindReference:
		return referenceObservationFact()
	case ObservationKindComponent:
		return componentObservationFact()
	default:
		return ObservationFact{Kind: kind}
	}
}

func mustBuildObservationRecord(t *testing.T, input ObservationRecordInput) ObservationRecord {
	t.Helper()
	record, err := BuildObservationRecord(input)
	if err != nil {
		t.Fatalf("BuildObservationRecord(%#v) returned error: %v", input, err)
	}
	return record
}

func withObservationRecord(base ObservationRecord, mutate func(*ObservationRecord)) ObservationRecord {
	base = cloneObservationRecord(base)
	mutate(&base)
	return base
}

func withObservationFact(base ObservationFact, mutate func(*ObservationFact)) ObservationFact {
	mutate(&base)
	return base
}

func cloneObservationRecord(record ObservationRecord) ObservationRecord {
	record.Provenance.Inputs = append([]ProvenanceInput(nil), record.Provenance.Inputs...)
	record.Provenance.Evidence = append([]EvidenceReference(nil), record.Provenance.Evidence...)
	record.Observation.Facts = append([]ObservationFact(nil), record.Observation.Facts...)
	return record
}

func cloneObservationRecordInput(input ObservationRecordInput) ObservationRecordInput {
	input.Provenance.Inputs = append([]ProvenanceInput(nil), input.Provenance.Inputs...)
	input.Provenance.Evidence = append([]EvidenceReference(nil), input.Provenance.Evidence...)
	input.Observation.Facts = append([]ObservationFact(nil), input.Observation.Facts...)
	return input
}
