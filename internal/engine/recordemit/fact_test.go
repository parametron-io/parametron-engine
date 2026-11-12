package recordemit_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"parametron/internal/engine/cadruntime"
	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordemit"
	"parametron/internal/engine/recordpackage"
	"parametron/internal/engine/resourcebinding"
)

// Issue #51: normalized engineering facts enter the existing observation record
// family and raw fact evidence is packaged with digest provenance. These tests
// drive recordemit.EmitRunPackage and inspect the written package rather than
// only exercising mapping helpers.

func factEmissionEntity(logicalID, name string) recordcontract.OccurrenceEntity {
	return recordcontract.OccurrenceEntity{
		Resource:   recordcontract.ReferenceResource{ProjectID: "project-a", Kind: resourcebinding.ResourceKindModel, LogicalID: logicalID},
		NativeKind: "object",
		Name:       name,
	}
}

func factEmissionBinding(entity recordcontract.OccurrenceEntity, property, valueKind, unit string) recordcontract.FactBinding {
	return recordcontract.FactBinding{
		Kind:        recordcontract.FactKindBoundProperty,
		Subject:     entity,
		Context:     []recordcontract.OccurrenceContextFrame{},
		Property:    property,
		ValueKind:   valueKind,
		Unit:        unit,
		Requirement: recordcontract.FactRequirementOptional,
	}
}

func factEmissionInventory(t *testing.T) resourcebinding.Inventory {
	t.Helper()
	inventory, err := resourcebinding.New([]resourcebinding.Binding{
		{ProjectID: "project-a", Kind: resourcebinding.ResourceKindModel, LogicalID: "widget", SourceCoordinate: "models/Widget.FCStd", MaterializedCoordinate: "Widget.FCStd"},
		{ProjectID: "project-a", Kind: resourcebinding.ResourceKindModel, LogicalID: "part", SourceCoordinate: "models/Part.FCStd", MaterializedCoordinate: "resources/part.FCStd"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return inventory
}

func factEmissionRequest() *recordcontract.FactRequest {
	scoped := factEmissionBinding(factEmissionEntity("widget", "Body"), "Length", recordcontract.FactValueKindNumber, "mm")
	scoped.Context = []recordcontract.OccurrenceContextFrame{{Parent: factEmissionEntity("widget", "Assembly"), Member: "Link1"}}
	return &recordcontract.FactRequest{
		SchemaVersion: recordcontract.FactRequestSchemaVersion,
		Bindings: []recordcontract.FactBinding{
			factEmissionBinding(factEmissionEntity("widget", "Body"), "Length", recordcontract.FactValueKindNumber, "mm"),
			factEmissionBinding(factEmissionEntity("part", "Body"), "Material", recordcontract.FactValueKindString, ""),
			scoped,
		},
	}
}

func factEmissionObserved(entity recordcontract.OccurrenceEntity, document, property, valueKind, rawValue, unit string) cadruntime.FactRuntimeEvidenceFact {
	return cadruntime.FactRuntimeEvidenceFact{
		Kind:         recordcontract.FactKindBoundProperty,
		Subject:      cadruntime.FactRuntimeSubject{Entity: entity, Document: document, NativeType: "PartDesign::Body"},
		Context:      []recordcontract.OccurrenceContextFrame{},
		Property:     property,
		ValueKind:    valueKind,
		Value:        json.RawMessage(rawValue),
		Unit:         unit,
		Availability: recordcontract.FactAvailabilityObserved,
	}
}

func factEmissionEvidence() cadruntime.FactRuntimeEvidence {
	scoped := factEmissionObserved(factEmissionEntity("widget", "Body"), "Widget.FCStd", "Length", recordcontract.FactValueKindNumber, "17", "mm")
	scoped.Context = []recordcontract.OccurrenceContextFrame{{Parent: factEmissionEntity("widget", "Assembly"), Member: "Link1"}}
	return cadruntime.FactRuntimeEvidence{
		SchemaVersion:  recordcontract.FactEvidenceSchemaVersion,
		Kind:           "native_fact_evidence",
		FreeCADVersion: "1.1.4",
		Facts: []cadruntime.FactRuntimeEvidenceFact{
			factEmissionObserved(factEmissionEntity("widget", "Body"), "Widget.FCStd", "Length", recordcontract.FactValueKindNumber, "42.0", "mm"),
			factEmissionObserved(factEmissionEntity("part", "Body"), "resources/part.FCStd", "Material", recordcontract.FactValueKindString, `"AL-6061"`, ""),
			scoped,
		},
	}
}

// factEmissionRaw marshals with indentation so tests can prove that recordemit
// packages the exact supplied bytes rather than a re-marshaled form.
func factEmissionRaw(t *testing.T) []byte {
	t.Helper()
	raw, err := json.MarshalIndent(factEmissionEvidence(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, '\n')
}

func factEmissionEvidenceInput(t *testing.T) *recordemit.FactRunEvidence {
	t.Helper()
	return &recordemit.FactRunEvidence{Input: cadruntime.FactMappingInput{
		Request:                factEmissionRequest(),
		Resources:              factEmissionInventory(t),
		ExpectedSourceDocument: "models/Widget.FCStd",
		Content:                factEmissionRaw(t),
	}}
}

func TestEmitRunPackageFactMergesIntoExistingObservationFamily(t *testing.T) {
	material := newRuntimeMaterial(t, true)
	cad := material.evidence()
	cad.Fact = factEmissionEvidenceInput(t)
	occurrence, occurrenceEvidence := occurrenceEmissionFixture(t)
	occurrenceRaw, err := json.MarshalIndent(occurrenceEvidence, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	occurrence.Input.Content = append(occurrenceRaw, '\n')
	cad.Occurrence = occurrence
	rawFact := factEmissionRaw(t)

	planHash := "fact-merge-plan"
	_, files := emitAndReadPackage(t, recordemit.RunEmitInput{
		PlanHash:   planHash,
		Report:     syntheticSuccessReport(planHash),
		Metadata:   syntheticMetadata(planHash),
		CADRuntime: cad,
	})

	if entries := manifestRecordsOf(t, files, "observation"); len(entries) != 1 {
		t.Fatalf("observation family entries = %#v, want exactly one merged record", entries)
	}
	record := decodeObservation(t, files)
	if record.Observation.Occurrences == nil || len(record.Observation.Occurrences.Occurrences) != 2 {
		t.Fatalf("occurrence surface lost during fact merge: %#v", record.Observation.Occurrences)
	}
	if len(record.Observation.Occurrences.Quantities) != 1 || record.Observation.Occurrences.Quantities[0].Quantity != 2 || record.Observation.Occurrences.Quantities[0].Target.Name != "ExactPart" {
		t.Fatalf("quantity rollup changed during fact merge: %#v", record.Observation.Occurrences.Quantities)
	}

	existingKinds := map[recordcontract.ObservationKind]int{}
	var factKinds []recordcontract.ObservationFact
	for _, fact := range record.Observation.Facts {
		if fact.Kind == recordcontract.ObservationKindFact {
			factKinds = append(factKinds, fact)
			continue
		}
		existingKinds[fact.Kind]++
	}
	if len(existingKinds) == 0 || existingKinds[recordcontract.ObservationKindMetadata] == 0 {
		t.Fatalf("existing observation kinds were replaced: %#v", existingKinds)
	}
	if len(factKinds) != 3 {
		t.Fatalf("fact kinds = %#v, want three", factKinds)
	}

	byKey := map[string]recordcontract.ObservationFact{}
	for _, fact := range factKinds {
		if fact.Subject.Resource == nil {
			t.Fatalf("fact subject lost resource qualification: %#v", fact)
		}
		key := fact.Subject.Resource.LogicalID + "/" + fact.Property
		if len(fact.Subject.Context) > 0 {
			key += "/occurrence"
		}
		byKey[key] = fact
	}
	widgetLength, ok := byKey["widget/Length"]
	if !ok || widgetLength.Value.Raw != "42" || widgetLength.Unit != "mm" || widgetLength.Availability != recordcontract.FactAvailabilityObserved {
		t.Fatalf("widget length fact = %#v, want canonical 42 mm", widgetLength)
	}
	partMaterial, ok := byKey["part/Material"]
	if !ok || partMaterial.Value.Raw != `"AL-6061"` || partMaterial.Unit != "" {
		t.Fatalf("part material fact = %#v, want the bound scalar string", partMaterial)
	}
	scoped, ok := byKey["widget/Length/occurrence"]
	if !ok || scoped.Value.Raw != "17" || len(scoped.Subject.Context) != 1 || scoped.Subject.Context[0].Member != "Link1" {
		t.Fatalf("occurrence-scoped fact = %#v, want a distinct occurrence scope", scoped)
	}
	if widgetLength.Subject.Name != "Body" || partMaterial.Subject.Name != "Body" {
		t.Fatal("same native name in different resources collapsed")
	}

	if !bytes.Equal(files[recordpackage.RawFactEvidenceContractPath()], rawFact) {
		t.Fatal("exact raw fact evidence bytes were not packaged")
	}
	if entry, ok := recordpackage.EntryForContractPath(recordpackage.RawFactEvidenceContractPath()); !ok || entry.Role != recordpackage.EntryRoleRawEvidence {
		t.Fatalf("raw fact evidence classification = %#v, %v", entry, ok)
	}
	digest, found := evidenceDigest(record.Provenance.Evidence, recordcontract.FactEvidenceSourceKind, recordcontract.FactEvidenceSourceRef)
	if !found || digest != sha256Hex(rawFact) {
		t.Fatalf("raw fact evidence digest provenance = %q, found = %t", digest, found)
	}
	for _, fact := range factKinds {
		if fact.Evidence.SourceKind != recordcontract.FactEvidenceSourceKind || fact.Evidence.SourceRef != recordcontract.FactEvidenceSourceRef || fact.Evidence.DigestSHA256 != sha256Hex(rawFact) {
			t.Fatalf("normalized fact evidence provenance = %#v", fact.Evidence)
		}
	}
}

func TestEmitRunPackageFactDoesNotAlterExistingFamilies(t *testing.T) {
	material := newRuntimeMaterial(t, true)
	traversal := &recordemit.ReferenceTraversalRunEvidence{
		Resources:              traversalInventory(t),
		ExpectedSourceDocument: "Widget.FCStd",
		Content:                referenceTraversalJSON(validReferenceTraversalEdgeJSON()),
	}
	emit := func(withFact bool) map[string][]byte {
		cad := material.evidence()
		if withFact {
			cad.Fact = factEmissionEvidenceInput(t)
		}
		planHash := "fact-regression-plan"
		_, files := emitAndReadPackage(t, recordemit.RunEmitInput{
			PlanHash:           planHash,
			Report:             syntheticSuccessReport(planHash),
			Metadata:           syntheticMetadata(planHash),
			CADRuntime:         cad,
			ReferenceTraversal: traversal,
		})
		return files
	}

	without := emit(false)
	with := emit(true)

	for _, family := range []recordcontract.Family{recordcontract.FamilyVerification, recordcontract.FamilyReference, recordcontract.FamilyFailure, recordcontract.FamilyExecution} {
		path := recordpackage.MustRecordContractPath(family)
		if !bytes.Equal(without[path], with[path]) {
			t.Fatalf("%s record changed when facts were requested", family)
		}
	}
	if bytes.Equal(without[recordpackage.MustRecordContractPath("observation")], with[recordpackage.MustRecordContractPath("observation")]) {
		t.Fatal("fact observation material did not change the observation record")
	}

	withoutRecord := decodeObservation(t, without)
	withRecord := decodeObservation(t, with)
	if !reflect.DeepEqual(existingFacts(withoutRecord), existingFacts(withRecord)) {
		t.Fatal("existing observation facts changed when facts were requested")
	}
	if len(existingFacts(withRecord)) == 0 {
		t.Fatal("fixture must retain at least one existing observation fact")
	}
	targetStates := 0
	for _, fact := range existingFacts(withRecord) {
		if fact.Kind == recordcontract.ObservationKindTargetState {
			targetStates++
		}
	}
	if targetStates == 0 {
		t.Fatal("target-state observation material changed when facts were requested")
	}
}

func existingFacts(record recordcontract.ObservationRecord) []recordcontract.ObservationFact {
	var out []recordcontract.ObservationFact
	for _, fact := range record.Observation.Facts {
		if fact.Kind != recordcontract.ObservationKindFact {
			out = append(out, fact)
		}
	}
	return out
}

func TestEmitRunPackageFactAvailabilityAndFailures(t *testing.T) {
	emit := func(fact *recordemit.FactRunEvidence) error {
		cad := newRuntimeMaterial(t, false).evidence()
		cad.Fact = fact
		planHash := "fact-availability-plan"
		return recordemit.EmitRunPackage(recordemit.RunEmitInput{
			RunRoot:    t.TempDir(),
			PlanHash:   planHash,
			Report:     syntheticSuccessReport(planHash),
			Metadata:   syntheticMetadata(planHash),
			CADRuntime: cad,
		})
	}

	t.Run("absent evidence emits explicit unavailable facts", func(t *testing.T) {
		request := factEmissionRequest()
		runRoot := t.TempDir()
		cad := newRuntimeMaterial(t, false).evidence()
		cad.Fact = &recordemit.FactRunEvidence{Input: cadruntime.FactMappingInput{Request: request, Resources: factEmissionInventory(t)}}
		planHash := "fact-availability-plan"
		_, files := emitAndReadPackage(t, recordemit.RunEmitInput{RunRoot: runRoot, PlanHash: planHash, Report: syntheticSuccessReport(planHash), Metadata: syntheticMetadata(planHash), CADRuntime: cad})
		record := decodeObservation(t, files)
		count := 0
		for _, fact := range record.Observation.Facts {
			if fact.Kind != recordcontract.ObservationKindFact {
				continue
			}
			count++
			if fact.Availability != recordcontract.FactAvailabilityUnavailable {
				t.Fatalf("absent evidence availability = %q, want unavailable", fact.Availability)
			}
			if fact.Value != (recordcontract.ObservationValue{}) || fact.Unit != "" || fact.Evidence != (recordcontract.ObservationEvidence{}) {
				t.Fatalf("absent evidence fabricated material: %#v", fact)
			}
		}
		if count != len(request.Bindings) {
			t.Fatalf("unavailable facts = %d, want %d", count, len(request.Bindings))
		}
		if _, ok := files[recordpackage.RawFactEvidenceContractPath()]; ok {
			t.Fatal("absent evidence fabricated a raw fact evidence file")
		}
	})

	t.Run("required unavailable fails", func(t *testing.T) {
		request := factEmissionRequest()
		request.Bindings[0].Requirement = recordcontract.FactRequirementRequired
		err := emit(&recordemit.FactRunEvidence{Input: cadruntime.FactMappingInput{Request: request, Resources: factEmissionInventory(t)}})
		if !errors.Is(err, recordcontract.ErrRequiredFactUnavailable) {
			t.Fatalf("EmitRunPackage error = %v, want ErrRequiredFactUnavailable", err)
		}
	})

	t.Run("unsolicited evidence fails", func(t *testing.T) {
		err := emit(&recordemit.FactRunEvidence{Input: cadruntime.FactMappingInput{
			Resources: factEmissionInventory(t),
			Content:   factEmissionRaw(t),
		}})
		if !errors.Is(err, recordcontract.ErrInvalidFact) {
			t.Fatalf("EmitRunPackage error = %v, want ErrInvalidFact", err)
		}
	})

	t.Run("partial evidence coverage fails", func(t *testing.T) {
		partial := factEmissionEvidence()
		partial.Facts = partial.Facts[:1]
		partialRaw, err := json.Marshal(partial)
		if err != nil {
			t.Fatal(err)
		}
		err = emit(&recordemit.FactRunEvidence{Input: cadruntime.FactMappingInput{
			Request:   factEmissionRequest(),
			Resources: factEmissionInventory(t),
			Content:   partialRaw,
		}})
		if !errors.Is(err, recordcontract.ErrInvalidFact) {
			t.Fatalf("EmitRunPackage error = %v, want ErrInvalidFact", err)
		}
	})
}

func TestEmitRunPackageFactIsDeterministicAndRelocationIndependent(t *testing.T) {
	planHash := "fact-determinism-plan"
	material := newRuntimeMaterial(t, false)
	emit := func(runRoot string, content []byte) (map[string][]byte, recordcontract.ObservationRecord) {
		cad := material.evidence()
		cad.Fact = &recordemit.FactRunEvidence{Input: cadruntime.FactMappingInput{
			Request:                factEmissionRequest(),
			Resources:              factEmissionInventory(t),
			ExpectedSourceDocument: "models/Widget.FCStd",
			Content:                content,
		}}
		_, files := emitAndReadPackage(t, recordemit.RunEmitInput{
			RunRoot:    runRoot,
			PlanHash:   planHash,
			Report:     syntheticSuccessReport(planHash),
			Metadata:   syntheticMetadata(planHash),
			CADRuntime: cad,
		})
		return files, decodeObservation(t, files)
	}

	raw := factEmissionRaw(t)
	firstFiles, firstRecord := emit(t.TempDir(), raw)
	secondFiles, secondRecord := emit(t.TempDir(), raw)
	if firstRecord.Identity != secondRecord.Identity {
		t.Fatalf("relocation changed fact observation identity:\nfirst:  %#v\nsecond: %#v", firstRecord.Identity, secondRecord.Identity)
	}
	if !bytes.Equal(firstFiles[recordpackage.MustRecordContractPath("observation")], secondFiles[recordpackage.MustRecordContractPath("observation")]) {
		t.Fatal("relocation changed the observation record bytes")
	}
	if !bytes.Equal(firstFiles[recordpackage.RawFactEvidenceContractPath()], secondFiles[recordpackage.RawFactEvidenceContractPath()]) {
		t.Fatal("relocation changed raw fact evidence bytes")
	}
	if !bytes.Equal(firstFiles[recordpackage.PackageManifestContractPath()], secondFiles[recordpackage.PackageManifestContractPath()]) {
		t.Fatal("relocation changed package manifest entries")
	}

	reversed := factEmissionEvidence()
	for i, j := 0, len(reversed.Facts)-1; i < j; i, j = i+1, j-1 {
		reversed.Facts[i], reversed.Facts[j] = reversed.Facts[j], reversed.Facts[i]
	}
	reversedRaw, err := json.MarshalIndent(reversed, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	_, reversedRecord := emit(t.TempDir(), append(reversedRaw, '\n'))
	if !reflect.DeepEqual(factFactsWithoutEvidence(firstRecord), factFactsWithoutEvidence(reversedRecord)) {
		t.Fatalf("shuffled raw evidence changed normalized fact ordering:\nfirst:    %#v\nreversed: %#v", factFactsWithoutEvidence(firstRecord), factFactsWithoutEvidence(reversedRecord))
	}
}

func factFactsWithoutEvidence(record recordcontract.ObservationRecord) []recordcontract.ObservationFact {
	var out []recordcontract.ObservationFact
	for _, fact := range record.Observation.Facts {
		if fact.Kind != recordcontract.ObservationKindFact {
			continue
		}
		fact.Evidence = recordcontract.ObservationEvidence{}
		out = append(out, fact)
	}
	return out
}
