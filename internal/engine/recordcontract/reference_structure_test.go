package recordcontract

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"parametron/internal/engine/resourcebinding"
)

func nativeReferenceInput() ReferenceRecordInput {
	resource := ReferenceResource{ProjectID: "project", Kind: resourcebinding.ResourceKindModel, LogicalID: "model"}
	source := ReferenceEndpoint{Resource: &resource, NativeKind: "object", Qualification: "declared", Resolution: ReferenceResolutionResolved, Name: "Source"}
	target := source
	target.Name = "Part"
	doc := source
	doc.NativeKind = "document"
	doc.Name = ""
	p, m := "Support", "App::PropertyLink"
	return ReferenceRecordInput{RecordKey: "reference", Reference: ReferenceSummary{Status: "succeeded", Completeness: "complete", Source: &doc, Edges: []ReferenceEdge{{NativeKind: "document_internal_reference", Kind: ReferenceKindComponent, Source: source, Target: target, SourceProperty: &p, ReferenceMechanism: &m, Resolution: ReferenceResolutionResolved}}}}
}

func TestNativeReferenceRelationshipKeyContract(t *testing.T) {
	base := nativeReferenceInput().Reference.Edges[0]
	key, err := ReferenceRelationshipKey(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*ReferenceEdge)
		equal  bool
	}{
		{"resolution excluded", func(e *ReferenceEdge) {
			e.Resolution = ReferenceResolutionUnresolved
			e.Target.Resolution = ReferenceResolutionUnresolved
		}, true},
		{"provenance excluded", func(e *ReferenceEdge) {
			e.Evidence = ReferenceEvidence{SourceKind: "raw", SourceRef: "raw/other.json"}
			e.Linkage.JobID = "other"
		}, true},
		{"property identity", func(e *ReferenceEdge) { p := "Link"; e.SourceProperty = &p }, false},
		{"mechanism identity", func(e *ReferenceEdge) { m := "App::PropertyXLink"; e.ReferenceMechanism = &m }, false},
		{"null property", func(e *ReferenceEdge) { e.SourceProperty = nil }, false},
		{"null mechanism", func(e *ReferenceEdge) { e.ReferenceMechanism = nil }, false},
		{"native name", func(e *ReferenceEdge) { e.Target.Name = "Other" }, false},
		{"logical resource", func(e *ReferenceEdge) {
			r := *e.Source.Resource
			r.LogicalID = "other"
			e.Source.Resource = &r
			e.Target.Resource = &r
		}, false},
		{"project qualification", func(e *ReferenceEdge) {
			r := *e.Source.Resource
			r.ProjectID = "other"
			e.Source.Resource = &r
			e.Target.Resource = &r
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			edge := base
			tc.change(&edge)
			got, err := ReferenceRelationshipKey(edge)
			if err != nil {
				t.Fatal(err)
			}
			if (got == key) != tc.equal {
				t.Fatal("incorrect relationship identity boundary")
			}
		})
	}
}

func TestNativeReferenceRecordRejectsInvalidContract(t *testing.T) {
	for name, mutate := range map[string]func(*ReferenceRecordInput){
		"fabricated role": func(i *ReferenceRecordInput) { i.Reference.Edges[0].Role = "assembly-child" },
		"empty property":  func(i *ReferenceRecordInput) { s := ""; i.Reference.Edges[0].SourceProperty = &s },
		"empty mechanism": func(i *ReferenceRecordInput) { s := ""; i.Reference.Edges[0].ReferenceMechanism = &s },
		"invalid resource kind": func(i *ReferenceRecordInput) {
			i.Reference.Edges[0].Target.Resource.Kind = resourcebinding.ResourceKindTable
		},
		"absolute unqualified coordinate": func(i *ReferenceRecordInput) {
			e := &i.Reference.Edges[0]
			e.NativeKind = "external_document_reference"
			e.Kind = ReferenceKindExternal
			e.Target.Resource = nil
			e.Target.Qualification = "undeclared"
			e.Target.Path = "/tmp/model.FCStd"
			i.Reference.Completeness = "incomplete"
		},
		"partial claims complete":     func(i *ReferenceRecordInput) { i.Reference.Status = "partial" },
		"unsupported claims complete": func(i *ReferenceRecordInput) { i.Reference.Unsupported = true },
		"failed retains graph":        func(i *ReferenceRecordInput) { i.Reference.Status = "failed"; i.Reference.Completeness = "unavailable" },
		"duplicate identity state conflict": func(i *ReferenceRecordInput) {
			e := i.Reference.Edges[0]
			e.Resolution = ReferenceResolutionUnresolved
			i.Reference.Edges = append(i.Reference.Edges, e)
		},
	} {
		t.Run(name, func(t *testing.T) {
			input := nativeReferenceInput()
			mutate(&input)
			_, err := BuildReferenceRecord(input)
			if !errors.Is(err, ErrInvalidReferenceRecord) {
				t.Fatalf("expected invalid reference record, got %v", err)
			}
		})
	}
}

func TestNativeReferenceRecordSerializationCopyAndSharedIdentity(t *testing.T) {
	input := nativeReferenceInput()
	record, err := BuildReferenceRecord(input)
	if err != nil {
		t.Fatal(err)
	}
	before, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	// Explicitly verify the shared identity derivation and canonical preimage.
	parts, err := referenceIdentityParts(normalizeReferenceRecordInput(input))
	if err != nil {
		t.Fatal(err)
	}
	want, err := DeriveIdentity(IdentityInput{Family: FamilyReference, Version: CurrentVersion, RecordKey: record.RecordKey, Provenance: record.Provenance, Parts: parts})
	if err != nil {
		t.Fatal(err)
	}
	if record.Identity != want {
		t.Fatal("reference record bypassed shared identity contract")
	}
	*input.Reference.Edges[0].SourceProperty = "mutated"
	input.Reference.Edges[0].Source.Resource.LogicalID = "mutated"
	input.Reference.Source.Resource.ProjectID = "mutated"
	after, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("builder retained caller-owned native fields")
	}
	var decoded ReferenceRecord
	if err = json.Unmarshal(before, &decoded); err != nil {
		t.Fatal(err)
	}
	if err = ValidateReferenceRecord(decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, record) {
		t.Fatal("native fields lost during roundtrip")
	}
}
