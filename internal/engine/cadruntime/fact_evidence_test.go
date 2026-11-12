package cadruntime

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/resourcebinding"
)

// Issue #51: controlled fact evidence is mapped by cadruntime.MapFactEvidence.
// Real FreeCAD participation is never required; the evidence is a contract
// fixture and the Engine owns every normalized field.

func factTestInventory(t *testing.T) resourcebinding.Inventory {
	t.Helper()
	inventory, err := resourcebinding.New([]resourcebinding.Binding{
		{ProjectID: "project-a", Kind: resourcebinding.ResourceKindModel, LogicalID: "widget", SourceCoordinate: "Widget.FCStd", MaterializedCoordinate: "source/Widget.FCStd"},
		{ProjectID: "project-a", Kind: resourcebinding.ResourceKindModel, LogicalID: "part", SourceCoordinate: "part.FCStd", MaterializedCoordinate: "resources/part.FCStd"},
		{ProjectID: "project-a", Kind: resourcebinding.ResourceKindModel, LogicalID: "alias", SourceCoordinate: "alias.FCStd", MaterializedCoordinate: "resources/alias.FCStd"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return inventory
}

func factTestRequest(bindings ...recordcontract.FactBinding) *recordcontract.FactRequest {
	return &recordcontract.FactRequest{SchemaVersion: recordcontract.FactRequestSchemaVersion, Bindings: bindings}
}

func factTestBinding(logicalID, name, property, valueKind, unit string) recordcontract.FactBinding {
	return recordcontract.FactBinding{
		Kind:        recordcontract.FactKindBoundProperty,
		Subject:     recordcontract.OccurrenceEntity{Resource: recordcontract.ReferenceResource{ProjectID: "project-a", Kind: resourcebinding.ResourceKindModel, LogicalID: logicalID}, NativeKind: "object", Name: name},
		Context:     []recordcontract.OccurrenceContextFrame{},
		Property:    property,
		ValueKind:   valueKind,
		Unit:        unit,
		Requirement: recordcontract.FactRequirementOptional,
	}
}

func factTestObserved(logicalID, name, document, property, valueKind, rawValue, unit string) FactRuntimeEvidenceFact {
	return FactRuntimeEvidenceFact{
		Kind:         recordcontract.FactKindBoundProperty,
		Subject:      FactRuntimeSubject{Entity: recordcontract.OccurrenceEntity{Resource: recordcontract.ReferenceResource{ProjectID: "project-a", Kind: resourcebinding.ResourceKindModel, LogicalID: logicalID}, NativeKind: "object", Name: name}, Document: document, NativeType: "PartDesign::Body"},
		Context:      []recordcontract.OccurrenceContextFrame{},
		Property:     property,
		ValueKind:    valueKind,
		Value:        json.RawMessage(rawValue),
		Unit:         unit,
		Availability: recordcontract.FactAvailabilityObserved,
	}
}

func factTestUnobserved(logicalID, name, document, property, availability, diagnostic string) FactRuntimeEvidenceFact {
	return FactRuntimeEvidenceFact{
		Kind:         recordcontract.FactKindBoundProperty,
		Subject:      FactRuntimeSubject{Entity: recordcontract.OccurrenceEntity{Resource: recordcontract.ReferenceResource{ProjectID: "project-a", Kind: resourcebinding.ResourceKindModel, LogicalID: logicalID}, NativeKind: "object", Name: name}, Document: document, NativeType: "PartDesign::Body"},
		Context:      []recordcontract.OccurrenceContextFrame{},
		Property:     property,
		Availability: availability,
		Diagnostic:   diagnostic,
	}
}

func factTestEvidence(facts ...FactRuntimeEvidenceFact) FactRuntimeEvidence {
	return FactRuntimeEvidence{SchemaVersion: recordcontract.FactEvidenceSchemaVersion, Kind: "native_fact_evidence", FreeCADVersion: "1.1.4", Facts: facts}
}

func factTestEvidenceBytes(t *testing.T, evidence FactRuntimeEvidence) []byte {
	t.Helper()
	content, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func mapFactEvidence(t *testing.T, request *recordcontract.FactRequest, content []byte) ([]recordcontract.ObservationFact, error) {
	t.Helper()
	return MapFactEvidence(FactMappingInput{
		Request:                request,
		Resources:              factTestInventory(t),
		ExpectedSourceDocument: "source/Widget.FCStd",
		Content:                content,
	})
}

func mustMapFactEvidence(t *testing.T, request *recordcontract.FactRequest, content []byte) []recordcontract.ObservationFact {
	t.Helper()
	facts, err := mapFactEvidence(t, request, content)
	if err != nil {
		t.Fatalf("MapFactEvidence returned error: %v", err)
	}
	return facts
}

func buildFactObservationRecord(t *testing.T, facts []recordcontract.ObservationFact) recordcontract.ObservationRecord {
	t.Helper()
	record, err := recordcontract.BuildObservationRecord(recordcontract.ObservationRecordInput{
		RecordKey:   "engine-run:fact-plan:observation",
		Provenance:  recordcontract.Provenance{Plan: recordcontract.PlanProvenance{PlanID: "fact-plan"}},
		Observation: recordcontract.ObservationSummary{Facts: facts},
	})
	if err != nil {
		t.Fatalf("normalized fact record is invalid: %v", err)
	}
	return record
}

func TestMapFactEvidenceObservedValues(t *testing.T) {
	tests := []struct {
		name      string
		property  string
		valueKind string
		unit      string
		rawValue  string
		wantRaw   string
	}{
		{name: "finite positive number", property: "Length", valueKind: recordcontract.FactValueKindNumber, unit: "mm", rawValue: "42", wantRaw: "42"},
		{name: "fractional number with unit", property: "Length", valueKind: recordcontract.FactValueKindNumber, unit: "mm", rawValue: "2.5", wantRaw: "2.5"},
		{name: "finite negative number", property: "Offset", valueKind: recordcontract.FactValueKindNumber, unit: "mm", rawValue: "-2.5", wantRaw: "-2.5"},
		{name: "zero is a genuine observed value", property: "Offset", valueKind: recordcontract.FactValueKindNumber, unit: "mm", rawValue: "0", wantRaw: "0"},
		{name: "integer", property: "InstanceCount", valueKind: recordcontract.FactValueKindInteger, unit: "", rawValue: "3", wantRaw: "3"},
		{name: "bound material scalar", property: "Material", valueKind: recordcontract.FactValueKindString, unit: "", rawValue: `"AL-6061"`, wantRaw: `"AL-6061"`},
		{name: "bound boolean scalar", property: "SuppressedHint", valueKind: recordcontract.FactValueKindBoolean, unit: "", rawValue: "false", wantRaw: "false"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := factTestRequest(factTestBinding("widget", "Body", tt.property, tt.valueKind, tt.unit))
			content := factTestEvidenceBytes(t, factTestEvidence(factTestObserved("widget", "Body", "source/Widget.FCStd", tt.property, tt.valueKind, tt.rawValue, tt.unit)))
			facts := mustMapFactEvidence(t, request, content)
			if len(facts) != 1 {
				t.Fatalf("facts = %#v, want exactly one", facts)
			}
			fact := facts[0]
			if fact.Kind != recordcontract.ObservationKindFact || fact.Key != recordcontract.FactKindBoundProperty {
				t.Fatalf("fact kind material = %#v", fact)
			}
			if fact.Value.Kind != tt.valueKind || fact.Value.Raw != tt.wantRaw {
				t.Fatalf("value = %#v, want %s %q", fact.Value, tt.valueKind, tt.wantRaw)
			}
			if fact.Unit != tt.unit || fact.Property != tt.property || fact.Availability != recordcontract.FactAvailabilityObserved {
				t.Fatalf("fact provenance/state changed: %#v", fact)
			}
			if fact.Subject.Name != "Body" || fact.Subject.NativeKind != "object" || fact.Subject.Resource == nil || *fact.Subject.Resource != (recordcontract.ReferenceResource{ProjectID: "project-a", Kind: resourcebinding.ResourceKindModel, LogicalID: "widget"}) {
				t.Fatalf("resource-qualified subject lost: %#v", fact.Subject)
			}
			if fact.Evidence.SourceKind != recordcontract.FactEvidenceSourceKind || fact.Evidence.SourceRef != recordcontract.FactEvidenceSourceRef || fact.Evidence.DigestSHA256 != fmt.Sprintf("%x", sha256.Sum256(content)) {
				t.Fatalf("raw evidence provenance lost: %#v", fact.Evidence)
			}
			if provenance := FactEvidenceProvenance(facts); provenance != fact.Evidence {
				t.Fatalf("shared fact evidence provenance = %#v, want %#v", provenance, fact.Evidence)
			}
			buildFactObservationRecord(t, facts)
		})
	}
}

func TestMapFactEvidenceNumericCanonicalizationCollapsesEquivalentEvidence(t *testing.T) {
	request := factTestRequest(factTestBinding("widget", "Body", "Length", recordcontract.FactValueKindNumber, "mm"))
	content := factTestEvidenceBytes(t, factTestEvidence(
		factTestObserved("widget", "Body", "source/Widget.FCStd", "Length", recordcontract.FactValueKindNumber, "42", "mm"),
		factTestObserved("widget", "Body", "source/Widget.FCStd", "Length", recordcontract.FactValueKindNumber, "42.0", "mm"),
		factTestObserved("widget", "Body", "source/Widget.FCStd", "Length", recordcontract.FactValueKindNumber, "4.2e1", "mm"),
	))
	facts := mustMapFactEvidence(t, request, content)
	if len(facts) != 1 || facts[0].Value.Raw != "42" {
		t.Fatalf("equivalent numeric evidence did not canonicalize: %#v", facts)
	}
	buildFactObservationRecord(t, facts)
}

func TestMapFactEvidenceUnits(t *testing.T) {
	t.Run("explicit accepted unit", func(t *testing.T) {
		facts := mustMapFactEvidence(t,
			factTestRequest(factTestBinding("widget", "Body", "Length", recordcontract.FactValueKindNumber, "mm")),
			factTestEvidenceBytes(t, factTestEvidence(factTestObserved("widget", "Body", "source/Widget.FCStd", "Length", recordcontract.FactValueKindNumber, "2.45", "mm"))),
		)
		if len(facts) != 1 || facts[0].Unit != "mm" || facts[0].Value.Raw != "2.45" {
			t.Fatalf("explicit unit not retained separately from the value: %#v", facts)
		}
	})

	t.Run("missing unit for unit-bearing fact", func(t *testing.T) {
		_, err := mapFactEvidence(t,
			factTestRequest(factTestBinding("widget", "Body", "Length", recordcontract.FactValueKindNumber, "mm")),
			factTestEvidenceBytes(t, factTestEvidence(factTestObserved("widget", "Body", "source/Widget.FCStd", "Length", recordcontract.FactValueKindNumber, "2.45", ""))),
		)
		if !errors.Is(err, recordcontract.ErrInvalidFact) {
			t.Fatalf("missing unit error = %v, want ErrInvalidFact", err)
		}
	})

	t.Run("incompatible unit", func(t *testing.T) {
		_, err := mapFactEvidence(t,
			factTestRequest(factTestBinding("widget", "Body", "Length", recordcontract.FactValueKindNumber, "mm")),
			factTestEvidenceBytes(t, factTestEvidence(factTestObserved("widget", "Body", "source/Widget.FCStd", "Length", recordcontract.FactValueKindNumber, "2.45", "cm"))),
		)
		if !errors.Is(err, recordcontract.ErrInvalidFact) {
			t.Fatalf("incompatible unit error = %v, want ErrInvalidFact", err)
		}
	})

	t.Run("unsolicited unit on unitless fact", func(t *testing.T) {
		_, err := mapFactEvidence(t,
			factTestRequest(factTestBinding("widget", "Body", "Length", recordcontract.FactValueKindNumber, "")),
			factTestEvidenceBytes(t, factTestEvidence(factTestObserved("widget", "Body", "source/Widget.FCStd", "Length", recordcontract.FactValueKindNumber, "2.45", "mm"))),
		)
		if !errors.Is(err, recordcontract.ErrInvalidFact) {
			t.Fatalf("unsolicited unit error = %v, want ErrInvalidFact", err)
		}
	})

	t.Run("unit forbidden on string values", func(t *testing.T) {
		_, err := mapFactEvidence(t,
			factTestRequest(factTestBinding("widget", "Body", "Material", recordcontract.FactValueKindString, "")),
			factTestEvidenceBytes(t, factTestEvidence(factTestObserved("widget", "Body", "source/Widget.FCStd", "Material", recordcontract.FactValueKindString, `"AL-6061"`, "mm"))),
		)
		if !errors.Is(err, recordcontract.ErrInvalidFact) {
			t.Fatalf("string unit error = %v, want ErrInvalidFact", err)
		}
	})

	t.Run("no opaque value and unit substitution", func(t *testing.T) {
		_, err := mapFactEvidence(t,
			factTestRequest(factTestBinding("widget", "Body", "Mass", recordcontract.FactValueKindNumber, "kg")),
			factTestEvidenceBytes(t, factTestEvidence(factTestObserved("widget", "Body", "source/Widget.FCStd", "Mass", recordcontract.FactValueKindNumber, `"2.45kg"`, "kg"))),
		)
		if !errors.Is(err, recordcontract.ErrInvalidFact) {
			t.Fatalf("opaque value and unit error = %v, want ErrInvalidFact", err)
		}
	})

	t.Run("equivalent repeated unit-bearing evidence normalizes stably", func(t *testing.T) {
		request := factTestRequest(factTestBinding("widget", "Body", "Length", recordcontract.FactValueKindNumber, "mm"))
		first := factTestEvidenceBytes(t, factTestEvidence(factTestObserved("widget", "Body", "source/Widget.FCStd", "Length", recordcontract.FactValueKindNumber, "42.0", "mm")))
		second := factTestEvidenceBytes(t, factTestEvidence(factTestObserved("widget", "Body", "source/Widget.FCStd", "Length", recordcontract.FactValueKindNumber, "4.2e1", " mm ")))
		firstFacts := mustMapFactEvidence(t, request, first)
		secondFacts := mustMapFactEvidence(t, request, second)
		if len(firstFacts) != 1 || len(secondFacts) != 1 {
			t.Fatalf("unit-bearing duplicates collapsed incorrectly: %#v %#v", firstFacts, secondFacts)
		}
		firstFacts[0].Evidence = recordcontract.ObservationEvidence{}
		secondFacts[0].Evidence = recordcontract.ObservationEvidence{}
		if !reflect.DeepEqual(firstFacts, secondFacts) {
			t.Fatalf("equivalent unit-bearing evidence normalized differently:\nfirst: %#v\nsecond: %#v", firstFacts, secondFacts)
		}
	})
}

func TestMapFactEvidenceSubjectQualification(t *testing.T) {
	t.Run("two resources with identical native names remain distinct", func(t *testing.T) {
		request := factTestRequest(
			factTestBinding("widget", "Body", "Length", recordcontract.FactValueKindNumber, "mm"),
			factTestBinding("part", "Body", "Length", recordcontract.FactValueKindNumber, "mm"),
		)
		content := factTestEvidenceBytes(t, factTestEvidence(
			factTestObserved("widget", "Body", "source/Widget.FCStd", "Length", recordcontract.FactValueKindNumber, "42", "mm"),
			factTestObserved("part", "Body", "resources/part.FCStd", "Length", recordcontract.FactValueKindNumber, "17", "mm"),
		))
		facts := mustMapFactEvidence(t, request, content)
		if len(facts) != 2 {
			t.Fatalf("facts = %#v, want two distinct resources", facts)
		}
		byResource := map[string]string{}
		for _, fact := range facts {
			if fact.Subject.Name != "Body" || fact.Subject.Resource == nil {
				t.Fatalf("subject lost: %#v", fact.Subject)
			}
			byResource[fact.Subject.Resource.LogicalID] = fact.Value.Raw
		}
		if byResource["widget"] != "42" || byResource["part"] != "17" {
			t.Fatalf("same native name in different resources collapsed: %#v", byResource)
		}
		buildFactObservationRecord(t, facts)
	})

	t.Run("semantic identity joins when supplied", func(t *testing.T) {
		binding := factTestBinding("widget", "Body", "Length", recordcontract.FactValueKindNumber, "mm")
		binding.Subject.SemanticID = "part/body"
		item := factTestObserved("widget", "Body", "source/Widget.FCStd", "Length", recordcontract.FactValueKindNumber, "42", "mm")
		item.Subject.Entity.SemanticID = "part/body"
		facts := mustMapFactEvidence(t, factTestRequest(binding), factTestEvidenceBytes(t, factTestEvidence(item)))
		if len(facts) != 1 || facts[0].Subject.SemanticID != "part/body" {
			t.Fatalf("semantic identity annotation lost: %#v", facts)
		}
		buildFactObservationRecord(t, facts)

		item.Subject.Entity.SemanticID = "part/other"
		_, err := mapFactEvidence(t, factTestRequest(binding), factTestEvidenceBytes(t, factTestEvidence(item)))
		if !errors.Is(err, recordcontract.ErrInvalidFact) {
			t.Fatalf("mismatched semantic identity error = %v, want ErrInvalidFact", err)
		}
	})

	t.Run("occurrence scope stays distinct from entity scope", func(t *testing.T) {
		entityBinding := factTestBinding("widget", "Body", "Length", recordcontract.FactValueKindNumber, "mm")
		scopedBinding := factTestBinding("widget", "Body", "Length", recordcontract.FactValueKindNumber, "mm")
		scopedBinding.Context = []recordcontract.OccurrenceContextFrame{{
			Parent: recordcontract.OccurrenceEntity{Resource: recordcontract.ReferenceResource{ProjectID: "project-a", Kind: resourcebinding.ResourceKindModel, LogicalID: "widget"}, NativeKind: "object", Name: "Assembly"},
			Member: "Link1",
		}}
		entityItem := factTestObserved("widget", "Body", "source/Widget.FCStd", "Length", recordcontract.FactValueKindNumber, "42", "mm")
		scopedItem := factTestObserved("widget", "Body", "source/Widget.FCStd", "Length", recordcontract.FactValueKindNumber, "42", "mm")
		scopedItem.Context = append([]recordcontract.OccurrenceContextFrame{}, scopedBinding.Context...)
		facts := mustMapFactEvidence(t, factTestRequest(entityBinding, scopedBinding), factTestEvidenceBytes(t, factTestEvidence(entityItem, scopedItem)))
		if len(facts) != 2 {
			t.Fatalf("occurrence scope collapsed into entity scope: %#v", facts)
		}
		scoped := 0
		for _, fact := range facts {
			if len(fact.Subject.Context) > 0 {
				scoped++
			}
		}
		if scoped != 1 {
			t.Fatalf("occurrence context not retained exactly once: %#v", facts)
		}
		buildFactObservationRecord(t, facts)
	})

	t.Run("evidence context must match the requested binding", func(t *testing.T) {
		binding := factTestBinding("widget", "Body", "Length", recordcontract.FactValueKindNumber, "mm")
		item := factTestObserved("widget", "Body", "source/Widget.FCStd", "Length", recordcontract.FactValueKindNumber, "42", "mm")
		item.Context = []recordcontract.OccurrenceContextFrame{{
			Parent: recordcontract.OccurrenceEntity{Resource: recordcontract.ReferenceResource{ProjectID: "project-a", Kind: resourcebinding.ResourceKindModel, LogicalID: "widget"}, NativeKind: "object", Name: "Assembly"},
			Member: "Link1",
		}}
		_, err := mapFactEvidence(t, factTestRequest(binding), factTestEvidenceBytes(t, factTestEvidence(item)))
		if !errors.Is(err, recordcontract.ErrInvalidFact) {
			t.Fatalf("context mismatch error = %v, want ErrInvalidFact", err)
		}
	})

	t.Run("materialized document need not equal selected source document", func(t *testing.T) {
		request := factTestRequest(factTestBinding("part", "Body", "Length", recordcontract.FactValueKindNumber, "mm"))
		content := factTestEvidenceBytes(t, factTestEvidence(factTestObserved("part", "Body", "resources/part.FCStd", "Length", recordcontract.FactValueKindNumber, "42", "mm")))
		facts := mustMapFactEvidence(t, request, content)
		if len(facts) != 1 || facts[0].Subject.Resource == nil || facts[0].Subject.Resource.LogicalID != "part" {
			t.Fatalf("inventory coordinate join failed: %#v", facts)
		}
	})

	t.Run("absolute operational paths never qualify a subject", func(t *testing.T) {
		for _, document := range []string{"/tmp/attempt/source/Widget.FCStd", "../Widget.FCStd", `C:/attempt/Widget.FCStd`} {
			t.Run(document, func(t *testing.T) {
				item := factTestObserved("widget", "Body", document, "Length", recordcontract.FactValueKindNumber, "42", "mm")
				_, err := mapFactEvidence(t,
					factTestRequest(factTestBinding("widget", "Body", "Length", recordcontract.FactValueKindNumber, "mm")),
					factTestEvidenceBytes(t, factTestEvidence(item)),
				)
				if err == nil {
					t.Fatal("absolute/traversing coordinate was accepted as fact identity")
				}
			})
		}
	})
}

func TestMapFactEvidenceAvailabilityStates(t *testing.T) {
	request := factTestRequest(factTestBinding("widget", "Body", "Material", recordcontract.FactValueKindString, ""))

	t.Run("observed", func(t *testing.T) {
		facts := mustMapFactEvidence(t, request, factTestEvidenceBytes(t, factTestEvidence(factTestObserved("widget", "Body", "source/Widget.FCStd", "Material", recordcontract.FactValueKindString, `"AL-6061"`, ""))))
		if facts[0].Availability != recordcontract.FactAvailabilityObserved || facts[0].Value.Raw != `"AL-6061"` {
			t.Fatalf("observed state lost: %#v", facts[0])
		}
	})

	for _, availability := range []string{recordcontract.FactAvailabilityUnavailable, recordcontract.FactAvailabilityUnsupported} {
		t.Run("optional "+availability, func(t *testing.T) {
			item := factTestUnobserved("widget", "Body", "source/Widget.FCStd", "Material", availability, "controlled native diagnostic")
			facts := mustMapFactEvidence(t, request, factTestEvidenceBytes(t, factTestEvidence(item)))
			if len(facts) != 1 || facts[0].Availability != availability {
				t.Fatalf("%s state lost: %#v", availability, facts)
			}
			if facts[0].Value != (recordcontract.ObservationValue{}) || facts[0].Unit != "" {
				t.Fatalf("%s substituted value material: %#v", availability, facts[0])
			}
			if facts[0].Value.Raw == "0" || facts[0].Value.Raw == "false" {
				t.Fatalf("%s became an implicit scalar: %#v", availability, facts[0])
			}
			buildFactObservationRecord(t, facts)
		})
	}

	t.Run("absent evidence file is explicit unavailability", func(t *testing.T) {
		optional := factTestBinding("widget", "Body", "Material", recordcontract.FactValueKindString, "")
		other := factTestBinding("part", "Body", "Length", recordcontract.FactValueKindNumber, "mm")
		facts, err := mapFactEvidence(t, factTestRequest(optional, other), nil)
		if err != nil {
			t.Fatalf("absent evidence returned error: %v", err)
		}
		if len(facts) != 2 {
			t.Fatalf("absent evidence omitted requested facts: %#v", facts)
		}
		for _, fact := range facts {
			if fact.Availability != recordcontract.FactAvailabilityUnavailable {
				t.Fatalf("absent evidence availability = %q, want unavailable", fact.Availability)
			}
			if fact.Value != (recordcontract.ObservationValue{}) || fact.Unit != "" || fact.Evidence != (recordcontract.ObservationEvidence{}) {
				t.Fatalf("absent evidence fabricated material: %#v", fact)
			}
		}
		buildFactObservationRecord(t, facts)
	})

	t.Run("required unavailable fails deterministically", func(t *testing.T) {
		required := factTestBinding("widget", "Body", "Material", recordcontract.FactValueKindString, "")
		required.Requirement = recordcontract.FactRequirementRequired
		if _, err := mapFactEvidence(t, factTestRequest(required), nil); !errors.Is(err, recordcontract.ErrRequiredFactUnavailable) {
			t.Fatalf("required unavailable error = %v, want ErrRequiredFactUnavailable", err)
		}
	})

	t.Run("required unsupported fails deterministically", func(t *testing.T) {
		required := factTestBinding("widget", "Body", "Material", recordcontract.FactValueKindString, "")
		required.Requirement = recordcontract.FactRequirementRequired
		item := factTestUnobserved("widget", "Body", "source/Widget.FCStd", "Material", recordcontract.FactAvailabilityUnsupported, "property mechanism unsupported")
		if _, err := mapFactEvidence(t, factTestRequest(required), factTestEvidenceBytes(t, factTestEvidence(item))); !errors.Is(err, recordcontract.ErrRequiredFactUnavailable) {
			t.Fatalf("required unsupported error = %v, want ErrRequiredFactUnavailable", err)
		}
	})

	t.Run("required observed succeeds", func(t *testing.T) {
		required := factTestBinding("widget", "Body", "Material", recordcontract.FactValueKindString, "")
		required.Requirement = recordcontract.FactRequirementRequired
		facts := mustMapFactEvidence(t, factTestRequest(required), factTestEvidenceBytes(t, factTestEvidence(factTestObserved("widget", "Body", "source/Widget.FCStd", "Material", recordcontract.FactValueKindString, `"Steel"`, ""))))
		if len(facts) != 1 || facts[0].Availability != recordcontract.FactAvailabilityObserved {
			t.Fatalf("required observed fact lost: %#v", facts)
		}
	})

	t.Run("present evidence must cover every requested binding", func(t *testing.T) {
		request := factTestRequest(
			factTestBinding("widget", "Body", "Length", recordcontract.FactValueKindNumber, "mm"),
			factTestBinding("part", "Body", "Length", recordcontract.FactValueKindNumber, "mm"),
		)
		content := factTestEvidenceBytes(t, factTestEvidence(factTestObserved("widget", "Body", "source/Widget.FCStd", "Length", recordcontract.FactValueKindNumber, "42", "mm")))
		if _, err := mapFactEvidence(t, request, content); !errors.Is(err, recordcontract.ErrInvalidFact) {
			t.Fatalf("coverage gap error = %v, want ErrInvalidFact", err)
		}
	})
}

func TestMapFactEvidenceDuplicateAndConflictSemantics(t *testing.T) {
	binding := factTestBinding("widget", "Body", "Length", recordcontract.FactValueKindNumber, "mm")
	request := factTestRequest(binding)

	t.Run("equivalent duplicates canonicalize", func(t *testing.T) {
		content := factTestEvidenceBytes(t, factTestEvidence(
			factTestObserved("widget", "Body", "source/Widget.FCStd", "Length", recordcontract.FactValueKindNumber, "42", "mm"),
			factTestObserved("widget", "Body", "source/Widget.FCStd", "Length", recordcontract.FactValueKindNumber, "42.0", "mm"),
		))
		facts := mustMapFactEvidence(t, request, content)
		if len(facts) != 1 || facts[0].Value.Raw != "42" {
			t.Fatalf("equivalent duplicates were not canonicalized: %#v", facts)
		}
	})

	t.Run("conflicting value fails", func(t *testing.T) {
		content := factTestEvidenceBytes(t, factTestEvidence(
			factTestObserved("widget", "Body", "source/Widget.FCStd", "Length", recordcontract.FactValueKindNumber, "42", "mm"),
			factTestObserved("widget", "Body", "source/Widget.FCStd", "Length", recordcontract.FactValueKindNumber, "43", "mm"),
		))
		if _, err := mapFactEvidence(t, request, content); !errors.Is(err, recordcontract.ErrInvalidFact) {
			t.Fatalf("conflicting value error = %v, want ErrInvalidFact", err)
		}
	})

	t.Run("conflicting unit fails", func(t *testing.T) {
		content := factTestEvidenceBytes(t, factTestEvidence(
			factTestObserved("widget", "Body", "source/Widget.FCStd", "Length", recordcontract.FactValueKindNumber, "42", "mm"),
			factTestObserved("widget", "Body", "source/Widget.FCStd", "Length", recordcontract.FactValueKindNumber, "42", "cm"),
		))
		if _, err := mapFactEvidence(t, request, content); !errors.Is(err, recordcontract.ErrInvalidFact) {
			t.Fatalf("conflicting unit error = %v, want ErrInvalidFact", err)
		}
	})

	t.Run("conflicting availability fails", func(t *testing.T) {
		content := factTestEvidenceBytes(t, factTestEvidence(
			factTestObserved("widget", "Body", "source/Widget.FCStd", "Length", recordcontract.FactValueKindNumber, "42", "mm"),
			factTestUnobserved("widget", "Body", "source/Widget.FCStd", "Length", recordcontract.FactAvailabilityUnavailable, "property unavailable"),
		))
		if _, err := mapFactEvidence(t, request, content); !errors.Is(err, recordcontract.ErrInvalidFact) {
			t.Fatalf("conflicting availability error = %v, want ErrInvalidFact", err)
		}
	})

	t.Run("same native name in different resources is not a conflict", func(t *testing.T) {
		request := factTestRequest(
			factTestBinding("widget", "Body", "Length", recordcontract.FactValueKindNumber, "mm"),
			factTestBinding("part", "Body", "Length", recordcontract.FactValueKindNumber, "mm"),
		)
		content := factTestEvidenceBytes(t, factTestEvidence(
			factTestObserved("widget", "Body", "source/Widget.FCStd", "Length", recordcontract.FactValueKindNumber, "42", "mm"),
			factTestObserved("part", "Body", "resources/part.FCStd", "Length", recordcontract.FactValueKindNumber, "42", "mm"),
		))
		facts := mustMapFactEvidence(t, request, content)
		if len(facts) != 2 {
			t.Fatalf("same name in different resources was treated as a conflict: %#v", facts)
		}
	})
}

func TestMapFactEvidenceDeterminismUnderShuffledEvidence(t *testing.T) {
	request := factTestRequest(
		factTestBinding("widget", "Body", "Length", recordcontract.FactValueKindNumber, "mm"),
		factTestBinding("widget", "Body", "Material", recordcontract.FactValueKindString, ""),
		factTestBinding("part", "Body", "Length", recordcontract.FactValueKindNumber, "mm"),
	)
	items := []FactRuntimeEvidenceFact{
		factTestObserved("widget", "Body", "source/Widget.FCStd", "Length", recordcontract.FactValueKindNumber, "42", "mm"),
		factTestObserved("widget", "Body", "source/Widget.FCStd", "Material", recordcontract.FactValueKindString, `"AL-6061"`, ""),
		factTestObserved("part", "Body", "resources/part.FCStd", "Length", recordcontract.FactValueKindNumber, "17", "mm"),
	}
	orders := [][]FactRuntimeEvidenceFact{
		{items[0], items[1], items[2]},
		{items[2], items[0], items[1]},
		{items[1], items[2], items[0]},
		{items[2], items[1], items[0]},
	}
	var want []string
	for i, order := range orders {
		facts := mustMapFactEvidence(t, request, factTestEvidenceBytes(t, factTestEvidence(order...)))
		got := make([]string, 0, len(facts))
		for _, fact := range facts {
			// Raw evidence bytes legitimately differ by order; normalized semantic
			// ordering and material must not.
			fact.Evidence = recordcontract.ObservationEvidence{}
			encoded, err := json.Marshal(fact)
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, string(encoded))
		}
		if i == 0 {
			want = got
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("evidence order %d changed normalized facts:\ngot:  %#v\nwant: %#v", i, got, want)
		}
	}
}

func TestMapFactEvidenceRejectsMalformedAndUnsolicitedEvidence(t *testing.T) {
	validBinding := factTestBinding("widget", "Body", "Length", recordcontract.FactValueKindNumber, "mm")
	valid := factTestEvidenceBytes(t, factTestEvidence(factTestObserved("widget", "Body", "source/Widget.FCStd", "Length", recordcontract.FactValueKindNumber, "42", "mm")))

	// mutateEvidence changes one decoded evidence item; every mutation must be
	// rejected by contract, never silently coerced.
	mutations := map[string]func(*FactRuntimeEvidence){
		"unsupported schema version": func(e *FactRuntimeEvidence) { e.SchemaVersion = "2.0" },
		"unexpected evidence kind":   func(e *FactRuntimeEvidence) { e.Kind = "native_occurrence_evidence" },
		"missing runtime version":    func(e *FactRuntimeEvidence) { e.FreeCADVersion = "" },
		"observed without value":     func(e *FactRuntimeEvidence) { e.Facts[0].Value = nil },
		"observed without value kind": func(e *FactRuntimeEvidence) {
			e.Facts[0].ValueKind = ""
		},
		"observed with diagnostic": func(e *FactRuntimeEvidence) { e.Facts[0].Diagnostic = "diagnostic" },
		"incompatible value kind":  func(e *FactRuntimeEvidence) { e.Facts[0].ValueKind = recordcontract.FactValueKindInteger },
		"missing unit":             func(e *FactRuntimeEvidence) { e.Facts[0].Unit = "" },
		"number is not finite":     func(e *FactRuntimeEvidence) { e.Facts[0].Value = json.RawMessage(`"Inf"`) },
		"number is NaN":            func(e *FactRuntimeEvidence) { e.Facts[0].Value = json.RawMessage(`"NaN"`) },
		"number is malformed":      func(e *FactRuntimeEvidence) { e.Facts[0].Value = json.RawMessage(`"4.2.1"`) },
		"number is opaque value and unit": func(e *FactRuntimeEvidence) {
			e.Facts[0].Value = json.RawMessage(`"2.45kg"`)
		},
		"number is null":       func(e *FactRuntimeEvidence) { e.Facts[0].Value = json.RawMessage(`null`) },
		"subject name changed": func(e *FactRuntimeEvidence) { e.Facts[0].Subject.Entity.Name = "Other" },
		"native type missing":  func(e *FactRuntimeEvidence) { e.Facts[0].Subject.NativeType = "" },
		"document is alias":    func(e *FactRuntimeEvidence) { e.Facts[0].Subject.Document = "resources/alias.FCStd" },
		"undeclared subject":   func(e *FactRuntimeEvidence) { e.Facts[0].Subject.Entity.Resource.LogicalID = "unknown" },
		"unsupported property": func(e *FactRuntimeEvidence) { e.Facts[0].Property = "Density" },
		"invalid availability": func(e *FactRuntimeEvidence) { e.Facts[0].Availability = "missing" },
		"unobserved with value": func(e *FactRuntimeEvidence) {
			e.Facts[0].Availability = recordcontract.FactAvailabilityUnavailable
		},
		"unobserved without diagnostic": func(e *FactRuntimeEvidence) {
			e.Facts[0].Availability = recordcontract.FactAvailabilityUnavailable
			e.Facts[0].Value = nil
			e.Facts[0].ValueKind = ""
			e.Facts[0].Unit = ""
		},
	}

	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			var evidence FactRuntimeEvidence
			if err := json.Unmarshal(valid, &evidence); err != nil {
				t.Fatal(err)
			}
			mutate(&evidence)
			_, err := mapFactEvidence(t, factTestRequest(validBinding), factTestEvidenceBytes(t, evidence))
			if err == nil {
				t.Fatal("invalid evidence was accepted")
			}
			if !errors.Is(err, recordcontract.ErrInvalidFact) && !errors.Is(err, resourcebinding.ErrInvalid) {
				t.Fatalf("error classification = %v, want ErrInvalidFact or resourcebinding.ErrInvalid", err)
			}
		})
	}

	t.Run("unconfined subject coordinates", func(t *testing.T) {
		for _, document := range []string{"/tmp/attempt/Widget.FCStd", "../Widget.FCStd", "C:/attempt/Widget.FCStd"} {
			item := factTestObserved("widget", "Body", document, "Length", recordcontract.FactValueKindNumber, "42", "mm")
			if _, err := mapFactEvidence(t, factTestRequest(validBinding), factTestEvidenceBytes(t, factTestEvidence(item))); err == nil {
				t.Fatalf("document %q was accepted as normalized fact identity", document)
			}
		}
	})

	t.Run("duplicate JSON field", func(t *testing.T) {
		duplicate := []byte(`{"schemaVersion":"1.0","schemaVersion":"1.0","kind":"native_fact_evidence","freecadVersion":"1.1.4","facts":[]}`)
		if _, err := mapFactEvidence(t, factTestRequest(validBinding), duplicate); !errors.Is(err, recordcontract.ErrInvalidFact) {
			t.Fatalf("duplicate JSON field error = %v, want ErrInvalidFact", err)
		}
	})

	t.Run("unknown JSON field", func(t *testing.T) {
		unknown := []byte(`{"schemaVersion":"1.0","kind":"native_fact_evidence","freecadVersion":"1.1.4","facts":[],"unexpected":true}`)
		if _, err := mapFactEvidence(t, factTestRequest(validBinding), unknown); !errors.Is(err, recordcontract.ErrInvalidFact) {
			t.Fatalf("unknown JSON field error = %v, want ErrInvalidFact", err)
		}
	})

	t.Run("trailing JSON", func(t *testing.T) {
		trailing := append(append([]byte(nil), valid...), []byte(` {}`)...)
		if _, err := mapFactEvidence(t, factTestRequest(validBinding), trailing); !errors.Is(err, recordcontract.ErrInvalidFact) {
			t.Fatalf("trailing JSON error = %v, want ErrInvalidFact", err)
		}
	})

	t.Run("unsolicited evidence without intent", func(t *testing.T) {
		if _, err := mapFactEvidence(t, nil, valid); !errors.Is(err, recordcontract.ErrInvalidFact) {
			t.Fatalf("unsolicited evidence error = %v, want ErrInvalidFact", err)
		}
	})

	t.Run("no intent and no evidence requests nothing", func(t *testing.T) {
		facts, err := mapFactEvidence(t, nil, nil)
		if err != nil || facts != nil {
			t.Fatalf("no intent produced evidence: %#v %v", facts, err)
		}
	})

	t.Run("undeclared binding resource", func(t *testing.T) {
		binding := factTestBinding("unknown", "Body", "Length", recordcontract.FactValueKindNumber, "mm")
		if _, err := mapFactEvidence(t, factTestRequest(binding), valid); !errors.Is(err, resourcebinding.ErrInvalid) {
			t.Fatalf("undeclared resource error = %v, want resourcebinding.ErrInvalid", err)
		}
	})
}

func TestMapFactEvidenceRuntimeVersionIsProvenanceOnly(t *testing.T) {
	request := factTestRequest(factTestBinding("widget", "Body", "Length", recordcontract.FactValueKindNumber, "mm"))
	first := factTestEvidence(factTestObserved("widget", "Body", "source/Widget.FCStd", "Length", recordcontract.FactValueKindNumber, "42", "mm"))
	second := first
	second.FreeCADVersion = "0.0.0-controlled"
	firstFacts := mustMapFactEvidence(t, request, factTestEvidenceBytes(t, first))
	secondFacts := mustMapFactEvidence(t, request, factTestEvidenceBytes(t, second))
	if len(firstFacts) != 1 || len(secondFacts) != 1 {
		t.Fatalf("facts = %#v %#v", firstFacts, secondFacts)
	}
	firstFacts[0].Evidence = recordcontract.ObservationEvidence{}
	secondFacts[0].Evidence = recordcontract.ObservationEvidence{}
	if !reflect.DeepEqual(firstFacts, secondFacts) {
		t.Fatalf("runtime version altered normalized facts: %#v %#v", firstFacts, secondFacts)
	}
}

func TestFactEvidenceProvenanceCoversUnobservedFactsFromPresentEvidence(t *testing.T) {
	request := factTestRequest(
		factTestBinding("widget", "Body", "Length", recordcontract.FactValueKindNumber, "mm"),
		factTestBinding("part", "Body", "Material", recordcontract.FactValueKindString, ""),
	)
	item := factTestUnobserved("widget", "Body", "source/Widget.FCStd", "Length", recordcontract.FactAvailabilityUnavailable, "not reported")
	content := factTestEvidenceBytes(t, factTestEvidence(item))
	// The second binding is not covered by the present evidence file, so this
	// mapping must fail rather than fabricate an unavailable fact.
	if _, err := mapFactEvidence(t, request, content); !errors.Is(err, recordcontract.ErrInvalidFact) {
		t.Fatalf("partial evidence error = %v, want ErrInvalidFact", err)
	}

	single := factTestRequest(factTestBinding("widget", "Body", "Length", recordcontract.FactValueKindNumber, "mm"))
	facts := mustMapFactEvidence(t, single, content)
	provenance := FactEvidenceProvenance(facts)
	if provenance.SourceRef != recordcontract.FactEvidenceSourceRef || provenance.DigestSHA256 != fmt.Sprintf("%x", sha256.Sum256(content)) {
		t.Fatalf("shared evidence provenance = %#v", provenance)
	}
	if strings.TrimSpace(provenance.SourceKind) == "" {
		t.Fatal("evidence source kind is required")
	}
}
