package recordcontract

import (
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"testing"
)

func TestBuildReferenceRecordDerivesIdentity(t *testing.T) {
	record, err := BuildReferenceRecord(validReferenceRecordInput())
	if err != nil {
		t.Fatalf("BuildReferenceRecord(valid) returned error: %v", err)
	}

	if record.Family != FamilyReference {
		t.Fatalf("Family = %q, want %q", record.Family, FamilyReference)
	}
	if record.Version != CurrentVersion {
		t.Fatalf("Version = %q, want %q", record.Version, CurrentVersion)
	}
	if record.RecordKey != "reference-record-1" {
		t.Fatalf("RecordKey = %q, want trimmed reference-record-1", record.RecordKey)
	}
	if record.Identity.ID == "" || !isLowerHex64(record.Identity.ID) {
		t.Fatalf("Identity.ID = %q, want non-empty lowercase SHA-256 hex", record.Identity.ID)
	}
	if record.Identity.Family != FamilyReference {
		t.Fatalf("Identity.Family = %q, want %q", record.Identity.Family, FamilyReference)
	}
	if record.Identity.Version != CurrentVersion {
		t.Fatalf("Identity.Version = %q, want %q", record.Identity.Version, CurrentVersion)
	}
	if record.Provenance.SourceRevision.RevisionID != "rev-1" {
		t.Fatalf("Provenance.SourceRevision.RevisionID = %q, want normalized rev-1", record.Provenance.SourceRevision.RevisionID)
	}
	if got := record.Reference.Edges; len(got) != 2 {
		t.Fatalf("Reference.Edges length = %d, want 2", len(got))
	}
	if got := record.Reference.Edges[0]; got.Kind != ReferenceKindComponent || got.Resolution != ReferenceResolutionResolved || got.Source.ID != "assembly/root" || got.Target.ID != "part/body" {
		t.Fatalf("first reference edge not normalized deterministically: %#v", got)
	}
	if err := ValidateReferenceRecord(record); err != nil {
		t.Fatalf("ValidateReferenceRecord(built) returned error: %v", err)
	}
}

func TestNormalizeReferenceRecordDeterministic(t *testing.T) {
	first, err := BuildReferenceRecord(validReferenceRecordInput())
	if err != nil {
		t.Fatalf("BuildReferenceRecord(first) returned error: %v", err)
	}
	second, err := BuildReferenceRecord(equivalentReferenceRecordInput())
	if err != nil {
		t.Fatalf("BuildReferenceRecord(second) returned error: %v", err)
	}

	if first.Identity != second.Identity {
		t.Fatalf("equivalent identities differ:\nfirst:  %#v\nsecond: %#v", first.Identity, second.Identity)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("equivalent records differ:\nfirst:  %#v\nsecond: %#v", first, second)
	}
	if first.Reference.Edges == nil {
		t.Fatal("Reference.Edges = nil, want empty or populated non-nil slice after normalization")
	}
	if got := first.Reference.Edges[0]; got.Kind != ReferenceKindComponent || got.Resolution != ReferenceResolutionResolved {
		t.Fatalf("first edge = %#v, want sorted normalized component/resolved edge", got)
	}

	normalized := NormalizeReferenceRecord(ReferenceRecord{
		Family:     Family(" \t" + string(FamilyReference) + "\n"),
		Version:    " " + CurrentVersion + "\t",
		RecordKey:  "\nreference-record-1 ",
		Identity:   first.Identity,
		Provenance: equivalentReferenceRecordInput().Provenance,
		Reference:  equivalentReferenceRecordInput().Reference,
	})
	if !reflect.DeepEqual(normalized.Provenance, first.Provenance) {
		t.Fatalf("NormalizeReferenceRecord provenance = %#v, want %#v", normalized.Provenance, first.Provenance)
	}
	if !reflect.DeepEqual(normalized.Reference, first.Reference) {
		t.Fatalf("NormalizeReferenceRecord reference = %#v, want %#v", normalized.Reference, first.Reference)
	}

	empty := NormalizeReferenceRecord(ReferenceRecord{})
	if empty.Reference.Edges == nil {
		t.Fatal("NormalizeReferenceRecord empty Edges = nil, want empty non-nil slice")
	}
}

func TestReferenceRecordIdentityChangesWithSemanticMaterial(t *testing.T) {
	base := validReferenceRecordInput()
	baseRecord := mustBuildReferenceRecord(t, base)

	tests := []struct {
		name   string
		mutate func(*ReferenceRecordInput)
	}{
		{name: "reference kind", mutate: func(input *ReferenceRecordInput) { input.Reference.Edges[0].Kind = ReferenceKindDocument }},
		{name: "source endpoint", mutate: func(input *ReferenceRecordInput) { input.Reference.Edges[0].Source.ID = "assembly/root-v2" }},
		{name: "target endpoint", mutate: func(input *ReferenceRecordInput) { input.Reference.Edges[0].Target.ID = "part/body-v2" }},
		{name: "role", mutate: func(input *ReferenceRecordInput) { input.Reference.Edges[0].Role = "derived-from" }},
		{name: "resolution", mutate: func(input *ReferenceRecordInput) { input.Reference.Edges[0].Resolution = ReferenceResolutionResolved }},
		{name: "linkage", mutate: func(input *ReferenceRecordInput) { input.Reference.Edges[0].Linkage.StepRef = "resolve-v2" }},
		{name: "evidence", mutate: func(input *ReferenceRecordInput) { input.Reference.Edges[0].Evidence.DigestSHA256 = digestA() }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			changed := cloneReferenceRecordInput(base)
			tc.mutate(&changed)

			got, err := BuildReferenceRecord(changed)
			if err != nil {
				t.Fatalf("BuildReferenceRecord(changed) returned error: %v", err)
			}
			if got.Identity.ID == baseRecord.Identity.ID {
				t.Fatalf("identity ID did not change after %s changed: %q", tc.name, got.Identity.ID)
			}
		})
	}
}

func TestValidateReferenceRecordRejectsInvalidRecords(t *testing.T) {
	valid := mustBuildReferenceRecord(t, validReferenceRecordInput())

	tests := []struct {
		name   string
		mutate func(*ReferenceRecord)
	}{
		{name: "wrong family", mutate: func(record *ReferenceRecord) { record.Family = FamilyArtifact }},
		{name: "unsupported version", mutate: func(record *ReferenceRecord) { record.Version = "1.1" }},
		{name: "missing record key", mutate: func(record *ReferenceRecord) { record.RecordKey = " " }},
		{name: "invalid provenance", mutate: func(record *ReferenceRecord) { record.Provenance.Inputs[0].DigestSHA256 = upperDigestA() }},
		{name: "missing identity", mutate: func(record *ReferenceRecord) { record.Identity = Identity{} }},
		{name: "identity mismatch", mutate: func(record *ReferenceRecord) { record.Reference.Edges[0].Role = "mutated" }},
		{name: "empty edge list", mutate: func(record *ReferenceRecord) { record.Reference.Edges = nil }},
		{name: "unsupported reference kind", mutate: func(record *ReferenceRecord) { record.Reference.Edges[0].Kind = "parameter" }},
		{name: "unsupported resolution state", mutate: func(record *ReferenceRecord) { record.Reference.Edges[0].Resolution = "partial" }},
		{name: "missing source endpoint identity", mutate: func(record *ReferenceRecord) {
			record.Reference.Edges[0].Source = ReferenceEndpoint{RevisionID: "rev-1", DigestSHA256: digestA()}
		}},
		{name: "missing target endpoint identity", mutate: func(record *ReferenceRecord) {
			record.Reference.Edges[0].Target = ReferenceEndpoint{RevisionID: "rev-2", DigestSHA256: digestB()}
		}},
		{name: "invalid source endpoint SHA-256 digest", mutate: func(record *ReferenceRecord) { record.Reference.Edges[0].Source.DigestSHA256 = upperDigestA() }},
		{name: "invalid target endpoint SHA-256 digest", mutate: func(record *ReferenceRecord) { record.Reference.Edges[0].Target.DigestSHA256 = "abc123" }},
		{name: "invalid evidence SHA-256 digest", mutate: func(record *ReferenceRecord) { record.Reference.Edges[0].Evidence.DigestSHA256 = "abc123" }},
		{name: "evidence sourceKind without sourceRef", mutate: func(record *ReferenceRecord) { record.Reference.Edges[0].Evidence.SourceRef = " " }},
		{name: "evidence sourceRef without sourceKind", mutate: func(record *ReferenceRecord) { record.Reference.Edges[0].Evidence.SourceKind = "\t" }},
		{name: "duplicate normalized edges", mutate: func(record *ReferenceRecord) {
			record.Reference.Edges = []ReferenceEdge{componentReferenceEdge(), equivalentComponentReferenceEdge()}
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			record := cloneReferenceRecord(valid)
			tc.mutate(&record)

			if err := ValidateReferenceRecord(record); !errors.Is(err, ErrInvalidReferenceRecord) {
				t.Fatalf("ValidateReferenceRecord error = %v, want ErrInvalidReferenceRecord", err)
			}
		})
	}
}

func TestReferenceRecordValidationErrorsAreInspectable(t *testing.T) {
	record := mustBuildReferenceRecord(t, validReferenceRecordInput())

	tests := []struct {
		name       string
		record     ReferenceRecord
		wantErrors []error
	}{
		{
			name: "generic reference validation failure",
			record: withReferenceRecord(record, func(record *ReferenceRecord) {
				record.Reference.Edges[0].Kind = "parameter"
			}),
			wantErrors: []error{ErrInvalidReferenceRecord},
		},
		{
			name: "invalid version",
			record: withReferenceRecord(record, func(record *ReferenceRecord) {
				record.Version = "2.0"
			}),
			wantErrors: []error{ErrInvalidReferenceRecord, ErrInvalidVersion},
		},
		{
			name: "invalid provenance",
			record: withReferenceRecord(record, func(record *ReferenceRecord) {
				record.Provenance.Inputs[0].DigestSHA256 = "abc123"
			}),
			wantErrors: []error{ErrInvalidReferenceRecord, ErrInvalidProvenance},
		},
		{
			name: "invalid identity",
			record: withReferenceRecord(record, func(record *ReferenceRecord) {
				record.Identity.ID = "not-a-digest"
			}),
			wantErrors: []error{ErrInvalidReferenceRecord, ErrInvalidIdentity},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateReferenceRecord(tc.record)
			if err == nil {
				t.Fatal("ValidateReferenceRecord returned nil error, want validation failure")
			}
			for _, want := range tc.wantErrors {
				if !errors.Is(err, want) {
					t.Fatalf("ValidateReferenceRecord error = %v, want errors.Is(..., %v)", err, want)
				}
			}
		})
	}
}

func TestReferenceRecordNormalizationIsCopySafe(t *testing.T) {
	input := validReferenceRecordInput()
	built, err := BuildReferenceRecord(input)
	if err != nil {
		t.Fatalf("BuildReferenceRecord(valid) returned error: %v", err)
	}

	input.Reference.Edges[0].Source.ID = "mutated-source"
	input.Reference.Edges[0].Target.ID = "mutated-target"
	input.Reference.Edges[0].Evidence.SourceRef = "mutated-evidence"
	input.Provenance.Inputs[0].Identity = "mutated-input"
	if built.Reference.Edges[0].Source.ID != "assembly/root" {
		t.Fatalf("built edges changed after input mutation: %#v", built.Reference.Edges)
	}
	if built.Reference.Edges[0].Target.ID != "part/body" {
		t.Fatalf("built target changed after input mutation: %#v", built.Reference.Edges)
	}
	if built.Reference.Edges[0].Evidence.SourceRef != "runtime://references/body" {
		t.Fatalf("built evidence changed after input mutation: %#v", built.Reference.Edges)
	}
	if built.Provenance.Inputs[0].Identity != "model-a" {
		t.Fatalf("built provenance inputs changed after input mutation: %#v", built.Provenance.Inputs)
	}

	normalizedInput := ReferenceRecord{
		Provenance: Provenance{
			Inputs: []ProvenanceInput{{Kind: "dsl", Identity: "model-a"}},
		},
		Reference: ReferenceSummary{
			Edges: []ReferenceEdge{componentReferenceEdge()},
		},
	}
	normalized := NormalizeReferenceRecord(normalizedInput)
	normalizedInput.Provenance.Inputs[0].Identity = "mutated-input"
	normalizedInput.Reference.Edges[0].Source.ID = "mutated-source"
	normalizedInput.Reference.Edges[0].Evidence.SourceRef = "mutated-evidence"
	if normalized.Provenance.Inputs[0].Identity != "model-a" {
		t.Fatalf("normalized provenance inputs changed after input mutation: %#v", normalized.Provenance.Inputs)
	}
	if normalized.Reference.Edges[0].Source.ID != "assembly/root" {
		t.Fatalf("normalized edge source changed after input mutation: %#v", normalized.Reference.Edges)
	}
	if normalized.Reference.Edges[0].Evidence.SourceRef != "runtime://references/body" {
		t.Fatalf("normalized edge evidence changed after input mutation: %#v", normalized.Reference.Edges)
	}
}

func TestReferenceRecordJSONRootFields(t *testing.T) {
	record := mustBuildReferenceRecord(t, validReferenceRecordInput())

	payload, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("json.Marshal(ReferenceRecord) returned error: %v", err)
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(payload, &root); err != nil {
		t.Fatalf("json.Unmarshal(root) returned error: %v", err)
	}
	wantFields := []string{"family", "version", "recordKey", "identity", "provenance", "reference"}
	if len(root) != len(wantFields) {
		t.Fatalf("root JSON fields = %#v, want only %v", sortedJSONKeys(root), wantFields)
	}
	for _, field := range wantFields {
		if _, ok := root[field]; !ok {
			t.Fatalf("root JSON field %q missing from %s", field, payload)
		}
	}
}

func TestReferenceRecordSeparatesRawRuntimeOutput(t *testing.T) {
	record := mustBuildReferenceRecord(t, validReferenceRecordInput())

	payload, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("json.Marshal(ReferenceRecord) returned error: %v", err)
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(payload, &root); err != nil {
		t.Fatalf("json.Unmarshal(root) returned error: %v", err)
	}
	for _, field := range []string{"parametron.observed.json", "result.json", "raw", "runtimeOutput", "freecadResult", "observed"} {
		if _, ok := root[field]; ok {
			t.Fatalf("raw runtime output root field %q should not be exposed in reference contract JSON: %s", field, payload)
		}
	}

	var reference map[string]json.RawMessage
	if err := json.Unmarshal(root["reference"], &reference); err != nil {
		t.Fatalf("json.Unmarshal(reference) returned error: %v", err)
	}
	if _, ok := reference["edges"]; !ok {
		t.Fatalf("reference JSON field %q missing from %s", "edges", payload)
	}
}

func validReferenceRecordInput() ReferenceRecordInput {
	return ReferenceRecordInput{
		RecordKey: " reference-record-1 ",
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
				StepRef:    " resolve ",
			},
			Evidence: []EvidenceReference{
				{Kind: " log ", Ref: " logs/run.log ", DigestSHA256: digestE()},
				{Kind: " artifact ", Ref: " out/references.json ", DigestSHA256: digestF()},
			},
			Runtime: RuntimeProvenance{
				ToolID:    " parametron ",
				RuntimeID: " runtime-1 ",
				Adapter:   " freecad ",
			},
		},
		Reference: ReferenceSummary{
			Edges: []ReferenceEdge{
				externalReferenceEdge(),
				componentReferenceEdge(),
			},
		},
	}
}

func equivalentReferenceRecordInput() ReferenceRecordInput {
	input := validReferenceRecordInput()
	input.RecordKey = "\treference-record-1\n"
	input.Provenance.Inputs = []ProvenanceInput{
		{Kind: "dsl", Identity: "model-a", DigestSHA256: " " + digestD() + " "},
		{Kind: "parameter-table", Identity: "params-a", DigestSHA256: digestC()},
	}
	input.Provenance.Evidence = []EvidenceReference{
		{Kind: "artifact", Ref: "out/references.json", DigestSHA256: digestF()},
		{Kind: "log", Ref: "logs/run.log", DigestSHA256: digestE()},
	}
	input.Reference.Edges = []ReferenceEdge{
		withReferenceEdge(equivalentComponentReferenceEdge(), func(edge *ReferenceEdge) {
			edge.Kind = " component "
			edge.Resolution = "\tresolved\n"
		}),
		withReferenceEdge(equivalentExternalReferenceEdge(), func(edge *ReferenceEdge) {
			edge.Kind = "\texternal"
			edge.Resolution = " unresolved "
		}),
	}
	return input
}

func componentReferenceEdge() ReferenceEdge {
	return ReferenceEdge{
		Kind: ReferenceKindComponent,
		Source: ReferenceEndpoint{
			ID:           " assembly/root ",
			Name:         " Root Assembly ",
			Path:         " assemblies/root.FCStd ",
			AssetID:      " asset-root ",
			RevisionID:   " rev-1 ",
			DigestSHA256: " " + digestA() + " ",
		},
		Target: ReferenceEndpoint{
			ID:           " part/body ",
			Name:         " Body ",
			Path:         " parts/body.FCStd ",
			AssetID:      " asset-body ",
			RevisionID:   " rev-2 ",
			DigestSHA256: digestB(),
		},
		Role:       " contains ",
		Resolution: ReferenceResolutionResolved,
		Linkage: ReferenceLinkage{
			JobID:      " job-a ",
			ProductKey: " product-a ",
			StepRef:    " resolve ",
		},
		Evidence: ReferenceEvidence{
			SourceKind:   " runtime ",
			SourceRef:    " runtime://references/body ",
			DigestSHA256: digestC(),
		},
	}
}

func externalReferenceEdge() ReferenceEdge {
	return ReferenceEdge{
		Kind: ReferenceKindExternal,
		Source: ReferenceEndpoint{
			ID:      " drawing/main ",
			Name:    " Main Drawing ",
			Path:    " drawings/main.FCStd ",
			AssetID: " asset-drawing ",
		},
		Target: ReferenceEndpoint{
			ID:      " supplier/spec ",
			Name:    " Supplier Spec ",
			Path:    " https://example.invalid/spec.pdf ",
			AssetID: " supplier-spec ",
		},
		Role:       " cites ",
		Resolution: ReferenceResolutionUnresolved,
		Linkage: ReferenceLinkage{
			JobID:      " job-a ",
			ProductKey: " product-a ",
			StepRef:    " resolve ",
		},
		Evidence: ReferenceEvidence{
			SourceKind:   " runtime ",
			SourceRef:    " runtime://references/spec ",
			DigestSHA256: digestD(),
		},
	}
}

func equivalentComponentReferenceEdge() ReferenceEdge {
	edge := componentReferenceEdge()
	edge.Source.ID = "\tassembly/root\n"
	edge.Source.Name = "Root Assembly"
	edge.Source.Path = "\nassemblies/root.FCStd "
	edge.Source.AssetID = " asset-root"
	edge.Source.RevisionID = "rev-1 "
	edge.Source.DigestSHA256 = "\t" + digestA()
	edge.Target.ID = " part/body "
	edge.Target.Name = "\tBody\n"
	edge.Target.Path = "parts/body.FCStd"
	edge.Target.AssetID = "asset-body "
	edge.Target.RevisionID = "\trev-2"
	edge.Target.DigestSHA256 = " " + digestB() + " "
	edge.Role = "\tcontains\n"
	edge.Linkage.JobID = "job-a"
	edge.Linkage.ProductKey = "\tproduct-a "
	edge.Linkage.StepRef = "resolve\n"
	edge.Evidence.SourceKind = "\truntime"
	edge.Evidence.SourceRef = "runtime://references/body "
	edge.Evidence.DigestSHA256 = " " + digestC()
	return edge
}

func equivalentExternalReferenceEdge() ReferenceEdge {
	edge := externalReferenceEdge()
	edge.Source.ID = " drawing/main "
	edge.Source.Name = "\tMain Drawing"
	edge.Source.Path = "drawings/main.FCStd\n"
	edge.Source.AssetID = "\tasset-drawing "
	edge.Target.ID = "supplier/spec "
	edge.Target.Name = " Supplier Spec "
	edge.Target.Path = "\thttps://example.invalid/spec.pdf"
	edge.Target.AssetID = " supplier-spec\n"
	edge.Role = " cites "
	edge.Linkage.JobID = "\tjob-a"
	edge.Linkage.ProductKey = "product-a "
	edge.Linkage.StepRef = " resolve "
	edge.Evidence.SourceKind = "runtime "
	edge.Evidence.SourceRef = "\truntime://references/spec"
	edge.Evidence.DigestSHA256 = digestD() + " "
	return edge
}

func mustBuildReferenceRecord(t *testing.T, input ReferenceRecordInput) ReferenceRecord {
	t.Helper()
	record, err := BuildReferenceRecord(input)
	if err != nil {
		t.Fatalf("BuildReferenceRecord(%#v) returned error: %v", input, err)
	}
	return record
}

func withReferenceRecord(base ReferenceRecord, mutate func(*ReferenceRecord)) ReferenceRecord {
	base = cloneReferenceRecord(base)
	mutate(&base)
	return base
}

func withReferenceEdge(base ReferenceEdge, mutate func(*ReferenceEdge)) ReferenceEdge {
	mutate(&base)
	return base
}

func cloneReferenceRecord(record ReferenceRecord) ReferenceRecord {
	record.Provenance.Inputs = append([]ProvenanceInput(nil), record.Provenance.Inputs...)
	record.Provenance.Evidence = append([]EvidenceReference(nil), record.Provenance.Evidence...)
	record.Reference.Edges = append([]ReferenceEdge(nil), record.Reference.Edges...)
	return record
}

func cloneReferenceRecordInput(input ReferenceRecordInput) ReferenceRecordInput {
	input.Provenance.Inputs = append([]ProvenanceInput(nil), input.Provenance.Inputs...)
	input.Provenance.Evidence = append([]EvidenceReference(nil), input.Provenance.Evidence...)
	input.Reference.Edges = append([]ReferenceEdge(nil), input.Reference.Edges...)
	return input
}

func sortedJSONKeys(root map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(root))
	for key := range root {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
