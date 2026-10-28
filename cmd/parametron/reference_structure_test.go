package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordmap"
	"parametron/internal/engine/recordpackage"
	"parametron/internal/engine/resourcebinding"
)

// Exercise the normal CLI path with the finite declared inventory, including an
// aliased source resource and an unused resource. The external runtime only
// supplies native evidence; resource identity and normalization belong to Engine.
func TestContractPackage_ResourceQualifiedReferencesOptionA(t *testing.T) {
	contractPackageEnv(t)
	var previous *recordcontract.ReferenceRecord
	var previousKeys []string
	var previousRaw []byte
	for iteration := 0; iteration < 2; iteration++ {
		root := writeContractPackageFixture(t, nil)
		addDeclaredModels(t, root)
		targetCoordinate := fmt.Sprintf("resources/%x/input/box.FCStd", sha256.Sum256([]byte("alias")))
		property, mechanism := "Link", "App::PropertyXLink"
		traversal := recordmap.ReferenceTraversalRuntimeEvidence{
			SchemaVersion: "1.0", Kind: "raw_reference_traversal", Boundary: "reference_traversal_entrypoint", Operation: "reference_traversal", Status: "succeeded", SourceDocument: "source/box.FCStd",
			Nodes: []recordmap.ReferenceTraversalRuntimeNode{
				{ID: "doc", Kind: "document", State: "resolved", DocumentPath: "source/box.FCStd"},
				{ID: "source", Kind: "object", State: "resolved", DocumentPath: "source/box.FCStd", ObjectName: stringPointer("Part")},
				{ID: "target", Kind: "object", State: "resolved", DocumentPath: targetCoordinate, ObjectName: stringPointer("Part")},
			},
			Edges: []recordmap.ReferenceTraversalRuntimeEdge{
				{Source: "source", Target: "target", Kind: "external_document_reference", State: "resolved", SourceProperty: &property, ReferenceMechanism: &mechanism},
				{Source: "source", Target: "target", Kind: "external_document_reference", State: "resolved", SourceProperty: stringPointer("Support"), ReferenceMechanism: &mechanism},
				{Source: "source", Target: "target", Kind: "external_document_reference", State: "resolved", SourceProperty: &property, ReferenceMechanism: stringPointer("App::PropertyLink")},
			},
			Diagnostics: []recordmap.ReferenceTraversalRuntimeDiagnostic{},
		}
		if iteration == 1 {
			slices.Reverse(traversal.Nodes)
			slices.Reverse(traversal.Edges)
			for n := range traversal.Nodes {
				traversal.Nodes[n].Sequence = 100 + n
			}
			for n := range traversal.Edges {
				traversal.Edges[n].Sequence = 200 + n
			}
		}
		raw, err := json.MarshalIndent(traversal, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("PARAMETRON_TASK13_RUNTIME_TRAVERSAL_JSON", string(raw))
		run := runContractPackage(t, root, filepath.Join(t.TempDir(), "out"), "success", nil)
		if run.err != nil || len(run.invocations) != 1 {
			t.Fatalf("normal execution: %v; invocations=%d", run.err, len(run.invocations))
		}
		outcome := singleCADRuntimeOutcome(t, run.result.Execution)
		if outcome.SourceDocument != traversal.SourceDocument || len(outcome.Resources.Bindings()) != 3 {
			t.Fatal("completed inventory or selected source lost")
		}
		selected, err := outcome.Resources.LookupCoordinate(outcome.SourceDocument, true)
		if err != nil || selected.LogicalID != "box_model" {
			t.Fatalf("selected source aliased: %+v %v", selected, err)
		}
		target, err := outcome.Resources.LookupCoordinate(targetCoordinate, true)
		if err != nil || target.LogicalID != "alias" {
			t.Fatalf("additional materialization missing: %+v %v", target, err)
		}
		files := readRecordPackageFiles(t, recordPackageRoot(run.result.RunRoot))
		if !bytes.Equal(files[recordpackage.RawRuntimeReferenceTraversalContractPath()], raw) || !bytes.Equal(outcome.ReferenceTraversalJSON, raw) {
			t.Fatal("exact raw evidence changed")
		}
		var record recordcontract.ReferenceRecord
		decodePackageRecord(t, files, recordpackage.MustRecordContractPath("reference"), &record)
		if err = recordcontract.ValidateReferenceRecord(record); err != nil {
			t.Fatal(err)
		}
		if record.Reference.Completeness != "complete" || record.Reference.Source.Resource.LogicalID != "box_model" || len(record.Reference.Edges) != 3 {
			t.Fatalf("incomplete normalized structure: %+v", record.Reference)
		}
		digest := sha256HexOf(raw)
		keys := []string{}
		for _, e := range record.Reference.Edges {
			wantSource := recordcontract.ReferenceResource{ProjectID: selected.ProjectID, Kind: resourcebinding.ResourceKindModel, LogicalID: "box_model"}
			wantTarget := recordcontract.ReferenceResource{ProjectID: target.ProjectID, Kind: resourcebinding.ResourceKindModel, LogicalID: "alias"}
			if e.Source.Resource == nil || *e.Source.Resource != wantSource || e.Target.Resource == nil || *e.Target.Resource != wantTarget || e.Source.Name != "Part" || e.Target.Name != "Part" || e.Role != "" || e.Source.Path != "" || e.Target.Path != "" || e.SourceProperty == nil || e.ReferenceMechanism == nil {
				t.Fatalf("resource/discriminator collapse: %+v", e)
			}
			if e.Evidence.DigestSHA256 != digest {
				t.Fatal("incorrect exact-byte digest")
			}
			key, err := recordcontract.ReferenceRelationshipKey(e)
			if err != nil {
				t.Fatal(err)
			}
			keys = append(keys, key)
		}
		if !slices.IsSorted(keys) || len(slices.Compact(slices.Clone(keys))) != 3 {
			t.Fatal("relationships collapsed or ordering unstable")
		}
		foundDigest := false
		for _, e := range record.Provenance.Evidence {
			if e.Ref == recordpackage.RawRuntimeReferenceTraversalContractPath() && e.DigestSHA256 == digest {
				foundDigest = true
			}
		}
		if !foundDigest {
			t.Fatal("raw provenance lost")
		}
		// Existing record families and package layout remain additive.
		for _, family := range []string{"execution", "observation", "verification", "reference"} {
			assertPackageFileExists(t, recordPackageRoot(run.result.RunRoot), recordpackage.MustRecordContractPath(recordcontract.Family(family)))
		}
		if previous != nil {
			if !reflect.DeepEqual(previousKeys, keys) {
				t.Fatal("relocation/enumeration changed semantic relationship order")
			}
			if previous.Identity == record.Identity || bytes.Equal(previousRaw, raw) {
				t.Fatal("Option A evidence-sensitive record identity lost")
			}
		}
		previous = &record
		previousKeys = keys
		previousRaw = raw
	}
}

func stringPointer(s string) *string { return &s }

func TestContractPackage_EmptyReferenceCompleteness(t *testing.T) {
	for _, status := range []string{"succeeded", "partial", "failed"} {
		t.Run(status, func(t *testing.T) {
			contractPackageEnv(t)
			root := writeContractPackageFixture(t, nil)
			traversal := recordmap.ReferenceTraversalRuntimeEvidence{SchemaVersion: "1.0", Kind: "raw_reference_traversal", Boundary: "reference_traversal_entrypoint", Operation: "reference_traversal", Status: status, SourceDocument: "source/box.FCStd", Nodes: []recordmap.ReferenceTraversalRuntimeNode{{ID: "doc", Kind: "document", State: "resolved", DocumentPath: "source/box.FCStd"}}, Edges: []recordmap.ReferenceTraversalRuntimeEdge{}, Diagnostics: []recordmap.ReferenceTraversalRuntimeDiagnostic{}}
			if status != "succeeded" {
				traversal.Diagnostics = append(traversal.Diagnostics, recordmap.ReferenceTraversalRuntimeDiagnostic{Severity: "warning", Code: "traversal_incomplete", Message: "required traversal work unavailable"})
			}
			if status == "failed" {
				traversal.Nodes = []recordmap.ReferenceTraversalRuntimeNode{}
			}
			raw, err := json.Marshal(traversal)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("PARAMETRON_TASK13_RUNTIME_TRAVERSAL_JSON", string(raw))
			run := runContractPackage(t, root, filepath.Join(t.TempDir(), "out"), "success", nil)
			if run.err != nil {
				t.Fatal(run.err)
			}
			files := readRecordPackageFiles(t, recordPackageRoot(run.result.RunRoot))
			var record recordcontract.ReferenceRecord
			decodePackageRecord(t, files, recordpackage.MustRecordContractPath("reference"), &record)
			if err = recordcontract.ValidateReferenceRecord(record); err != nil {
				t.Fatal(err)
			}
			want := map[string]string{"succeeded": "complete", "partial": "incomplete", "failed": "unavailable"}[status]
			if record.Reference.Completeness != want || record.Reference.Status != status || len(record.Reference.Edges) != 0 || record.Reference.Source.Resource.LogicalID != "box_model" {
				t.Fatalf("empty scope lost: %+v", record.Reference)
			}
			if !bytes.Equal(files[recordpackage.RawRuntimeReferenceTraversalContractPath()], raw) {
				t.Fatal("raw bytes lost")
			}
			found := false
			for _, e := range record.Provenance.Evidence {
				if e.Ref == recordpackage.RawRuntimeReferenceTraversalContractPath() && e.DigestSHA256 == sha256HexOf(raw) {
					found = true
				}
			}
			if !found {
				t.Fatal("empty traversal raw digest lost")
			}
		})
	}
}
