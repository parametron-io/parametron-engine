package recordemit_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"

	"parametron/internal/engine/cadruntime"
	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordemit"
	"parametron/internal/engine/recordpackage"
)

func occurrenceEmissionFixture(t *testing.T) (*recordemit.OccurrenceRunEvidence, cadruntime.OccurrenceRuntimeEvidence) {
	t.Helper()
	parent := recordcontract.OccurrenceEntity{Resource: recordcontract.ReferenceResource{ProjectID: "project-a", Kind: "model", LogicalID: "widget"}, NativeKind: "object", Name: "Assembly"}
	request := &recordcontract.OccurrenceRequest{SchemaVersion: "1.0", Requirement: "optional", Scope: recordcontract.OccurrenceScope{Parent: parent, Context: []recordcontract.OccurrenceContextFrame{}, Mechanism: recordcontract.OccurrenceDirectMemberMechanism}}
	evidence := cadruntime.OccurrenceRuntimeEvidence{SchemaVersion: "1.1", Kind: "native_occurrence_evidence", FreeCADVersion: "1.1.4", Scope: request.Scope, Parent: &cadruntime.OccurrenceNativeEntity{Entity: parent, Document: "Widget.FCStd", NativeType: "Assembly::AssemblyObject"}, Completeness: "complete", InspectionCompleted: true, Group: []string{"Second", "First"}, Members: []cadruntime.OccurrenceRuntimeMember{}, Diagnostics: []string{}}
	for _, name := range evidence.Group {
		valid, zero := true, 0
		target := parent
		target.Name = "ExactPart"
		evidence.Members = append(evidence.Members, cadruntime.OccurrenceRuntimeMember{Name: name, Parent: parent, NativeType: "App::Link", ElementCount: &zero, MembershipCorroborated: true, Classification: "occurrence", Resolution: "resolved", NativeStateValid: &valid, Target: &cadruntime.OccurrenceNativeEntity{Entity: target, Document: "Widget.FCStd", NativeType: "App::Part"}})
	}
	return &recordemit.OccurrenceRunEvidence{Input: cadruntime.OccurrenceMappingInput{Request: request, Resources: traversalInventory(t), ExpectedSourceDocument: "Widget.FCStd"}}, evidence
}

func TestEmitOccurrencePreservesRawProvenanceAndIndependentReferences(t *testing.T) {
	var previous *recordcontract.ObservationRecord
	for iteration := 0; iteration < 3; iteration++ {
		occurrence, evidence := occurrenceEmissionFixture(t)
		if iteration == 1 {
			slices.Reverse(evidence.Members)
			slices.Reverse(evidence.Group)
		}
		raw, err := json.MarshalIndent(evidence, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		raw = append(raw, '\n')
		occurrence.Input.Content = raw
		material := newRuntimeMaterial(t, false)
		cad := material.evidence()
		cad.Occurrence = occurrence
		_, files := emitAndReadPackage(t, recordemit.RunEmitInput{PlanHash: "occurrence-plan", Report: syntheticSuccessReport("occurrence-plan"), Metadata: syntheticMetadata("occurrence-plan"), CADRuntime: cad, ReferenceTraversal: &recordemit.ReferenceTraversalRunEvidence{Resources: traversalInventory(t), ExpectedSourceDocument: "Widget.FCStd", Content: referenceTraversalJSON(validReferenceTraversalEdgeJSON())}})
		if !bytes.Equal(files["raw/runtime/prm.occurrence-evidence.json"], raw) {
			t.Fatal("exact occurrence bytes not packaged")
		}
		var record recordcontract.ObservationRecord
		if err := json.Unmarshal(files[recordpackage.MustRecordContractPath("observation")], &record); err != nil {
			t.Fatal(err)
		}
		if err := recordcontract.ValidateObservationRecord(record); err != nil {
			t.Fatal(err)
		}
		summary := record.Observation.Occurrences
		if summary == nil || len(summary.Occurrences) != 2 || len(summary.Quantities) != 1 || summary.Quantities[0].Quantity != 2 || summary.Quantities[0].Target.Name != "ExactPart" || summary.Evidence.DigestSHA256 != sha256Hex(raw) || len(record.Observation.Facts) == 0 {
			t.Fatalf("occurrence or ordinary facts lost: %+v", record)
		}
		found := false
		for _, ref := range record.Provenance.Evidence {
			if ref.Kind == "occurrence-evidence" && ref.Ref == "raw/runtime/prm.occurrence-evidence.json" && ref.DigestSHA256 == sha256Hex(raw) {
				found = true
			}
		}
		if !found {
			t.Fatal("exact raw digest absent from provenance")
		}
		var reference recordcontract.ReferenceRecord
		if err := json.Unmarshal(files[recordpackage.MustRecordContractPath("reference")], &reference); err != nil {
			t.Fatal(err)
		}
		if err := recordcontract.ValidateReferenceRecord(reference); err != nil {
			t.Fatal(err)
		}
		if len(reference.Reference.Edges) != 1 {
			t.Fatal("generic relationships changed")
		}
		if previous != nil {
			if !reflect.DeepEqual(previous.Observation.Occurrences.Occurrences, summary.Occurrences) || !reflect.DeepEqual(previous.Observation.Occurrences.Quantities, summary.Quantities) {
				t.Fatal("ordering changed normalized semantics")
			}
			if previous.Identity == record.Identity {
				t.Fatal("different exact raw provenance did not change record identity")
			}
		}
		previous = &record
	}
}

func TestEmitOccurrenceOptionalOmissionAndRequiredFailure(t *testing.T) {
	for _, mode := range []string{"optional", "required", "malformed", "no request"} {
		t.Run(mode, func(t *testing.T) {
			occurrence, _ := occurrenceEmissionFixture(t)
			cad := newRuntimeMaterial(t, false).evidence()
			cad.Occurrence = occurrence
			if mode == "required" {
				occurrence.Input.Request.Requirement = "complete"
			}
			if mode == "malformed" {
				occurrence.Input.Content = []byte(`{`)
			}
			if mode == "no request" {
				cad.Occurrence = nil
			}
			input := recordemit.RunEmitInput{RunRoot: t.TempDir(), PlanHash: "optional-plan", Report: syntheticSuccessReport("optional-plan"), CADRuntime: cad}
			if mode == "required" || mode == "malformed" {
				err := recordemit.EmitRunPackage(input)
				want := recordcontract.ErrInvalidOccurrence
				if mode == "required" {
					want = recordcontract.ErrOccurrenceCompletenessRequired
				}
				if !errors.Is(err, want) {
					t.Fatalf("wrong failure: %v", err)
				}
				return
			}
			_, files := emitAndReadPackage(t, input)
			if _, ok := files["raw/runtime/prm.occurrence-evidence.json"]; ok {
				t.Fatal("fabricated raw evidence")
			}
			var record recordcontract.ObservationRecord
			if err := json.Unmarshal(files[recordpackage.MustRecordContractPath("observation")], &record); err != nil {
				t.Fatal(err)
			}
			if mode == "no request" {
				if record.Observation.Occurrences != nil {
					t.Fatal("synthesized occurrences")
				}
			} else if s := record.Observation.Occurrences; s == nil || s.Completeness != "unavailable" || s.Quantities != nil {
				t.Fatal("omission became zero quantity")
			}
		})
	}
}
