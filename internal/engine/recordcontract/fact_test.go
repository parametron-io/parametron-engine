package recordcontract

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/engine/resourcebinding"
)

// Issue #51: bounded resource-qualified engineering fact observations extend the
// existing ObservationRecord family. These tests derive the contract from the
// issue and the implemented contract surfaces; they never assume native FreeCAD
// participation.

const (
	factTestDigest = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func factTestResource(logicalID string) ReferenceResource {
	return ReferenceResource{ProjectID: "project-a", Kind: resourcebinding.ResourceKindModel, LogicalID: logicalID}
}

func factTestEntity(logicalID, name string) OccurrenceEntity {
	return OccurrenceEntity{Resource: factTestResource(logicalID), NativeKind: "object", Name: name}
}

func factTestBinding() FactBinding {
	return FactBinding{
		Kind:        FactKindBoundProperty,
		Subject:     factTestEntity("widget", "Body"),
		Context:     []OccurrenceContextFrame{},
		Property:    "Length",
		ValueKind:   FactValueKindNumber,
		Unit:        "mm",
		Requirement: FactRequirementOptional,
	}
}

func factTestEvidence() ObservationEvidence {
	return ObservationEvidence{SourceKind: FactEvidenceSourceKind, SourceRef: FactEvidenceSourceRef, DigestSHA256: factTestDigest}
}

func factTestObservationFact() ObservationFact {
	return ObservationFact{
		Kind:         ObservationKindFact,
		Subject:      ObservationSubject{Name: "Body", Resource: factTestResourcePointer("widget"), NativeKind: "object"},
		Key:          FactKindBoundProperty,
		Value:        ObservationValue{Kind: FactValueKindNumber, Raw: "42"},
		Property:     "Length",
		Unit:         "mm",
		Availability: FactAvailabilityObserved,
		Evidence:     factTestEvidence(),
	}
}

func factTestResourcePointer(logicalID string) *ReferenceResource {
	resource := factTestResource(logicalID)
	return &resource
}

func factTestRecordInput(facts ...ObservationFact) ObservationRecordInput {
	return ObservationRecordInput{
		RecordKey:   "engine-run:fact-plan:observation",
		Provenance:  Provenance{Plan: PlanProvenance{PlanID: "fact-plan", PlanHash: "fact-hash"}},
		Observation: ObservationSummary{Facts: facts},
	}
}

func buildFactRecord(t *testing.T, facts ...ObservationFact) (ObservationRecord, error) {
	t.Helper()
	return BuildObservationRecord(factTestRecordInput(facts...))
}

func TestCanonicalFactScalarAcceptsFiniteTypedValues(t *testing.T) {
	tests := []struct {
		name      string
		valueKind string
		raw       string
		want      string
	}{
		{name: "positive number", valueKind: FactValueKindNumber, raw: "42", want: "42"},
		{name: "integral decimal", valueKind: FactValueKindNumber, raw: "42.0", want: "42"},
		{name: "exponent", valueKind: FactValueKindNumber, raw: "4.2e1", want: "42"},
		{name: "negative number", valueKind: FactValueKindNumber, raw: "-2.5", want: "-2.5"},
		{name: "negative integral decimal", valueKind: FactValueKindNumber, raw: "-42.0", want: "-42"},
		{name: "zero", valueKind: FactValueKindNumber, raw: "0", want: "0"},
		{name: "zero decimal", valueKind: FactValueKindNumber, raw: "0.0", want: "0"},
		{name: "small fraction", valueKind: FactValueKindNumber, raw: "1e-3", want: "0.001"},
		{name: "surrounding whitespace", valueKind: FactValueKindNumber, raw: " 42 ", want: "42"},
		{name: "integer", valueKind: FactValueKindInteger, raw: "42", want: "42"},
		{name: "integer decimal", valueKind: FactValueKindInteger, raw: "42.0", want: "42"},
		{name: "negative integer", valueKind: FactValueKindInteger, raw: "-7", want: "-7"},
		{name: "integer zero", valueKind: FactValueKindInteger, raw: "0", want: "0"},
		{name: "string material", valueKind: FactValueKindString, raw: `"AL-6061"`, want: `"AL-6061"`},
		{name: "string with unicode", valueKind: FactValueKindString, raw: `"Stahl ä"`, want: `"Stahl ä"`},
		{name: "opaque string value is not a numeric fact", valueKind: FactValueKindString, raw: `"2.45kg"`, want: `"2.45kg"`},
		{name: "empty string is an explicit observed string", valueKind: FactValueKindString, raw: `""`, want: `""`},
		{name: "boolean true", valueKind: FactValueKindBoolean, raw: "true", want: "true"},
		{name: "boolean false", valueKind: FactValueKindBoolean, raw: "false", want: "false"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CanonicalFactScalar(tt.valueKind, tt.raw)
			if err != nil {
				t.Fatalf("CanonicalFactScalar(%q, %q) returned error: %v", tt.valueKind, tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("CanonicalFactScalar(%q, %q) = %q, want %q", tt.valueKind, tt.raw, got, tt.want)
			}
		})
	}
}

func TestCanonicalFactScalarCanonicalizesEquivalentNumbersToOneIdentity(t *testing.T) {
	lexemes := []string{"42", "42.0", "4.2e1", "42.000", "4.20e+1", "+42", "042"}
	for _, lexeme := range lexemes {
		t.Run(lexeme, func(t *testing.T) {
			got, err := CanonicalFactScalar(FactValueKindNumber, lexeme)
			if err != nil {
				t.Fatalf("CanonicalFactScalar(%q) returned error: %v", lexeme, err)
			}
			if got != "42" {
				t.Fatalf("CanonicalFactScalar(%q) = %q, want %q", lexeme, got, "42")
			}
		})
	}
}

func TestCanonicalFactScalarRejectsNonFiniteAndMalformedValues(t *testing.T) {
	tests := []struct {
		name      string
		valueKind string
		raw       string
	}{
		{name: "number NaN", valueKind: FactValueKindNumber, raw: "NaN"},
		{name: "number lowercase NaN", valueKind: FactValueKindNumber, raw: "nan"},
		{name: "number positive infinity", valueKind: FactValueKindNumber, raw: "+Inf"},
		{name: "number infinity", valueKind: FactValueKindNumber, raw: "Inf"},
		{name: "number negative infinity", valueKind: FactValueKindNumber, raw: "-Inf"},
		{name: "number infinity word", valueKind: FactValueKindNumber, raw: "Infinity"},
		{name: "number negative infinity word", valueKind: FactValueKindNumber, raw: "-Infinity"},
		{name: "number overflow", valueKind: FactValueKindNumber, raw: "1e999"},
		{name: "number negative overflow", valueKind: FactValueKindNumber, raw: "-1e999"},
		{name: "number hex", valueKind: FactValueKindNumber, raw: "0x10"},
		{name: "number separators", valueKind: FactValueKindNumber, raw: "1,000"},
		{name: "number two decimal points", valueKind: FactValueKindNumber, raw: "4.2.1"},
		{name: "number opaque value and unit", valueKind: FactValueKindNumber, raw: "2.45kg"},
		{name: "number empty", valueKind: FactValueKindNumber, raw: ""},
		{name: "number whitespace", valueKind: FactValueKindNumber, raw: "   "},
		{name: "number null", valueKind: FactValueKindNumber, raw: "null"},
		{name: "number boolean", valueKind: FactValueKindNumber, raw: "true"},
		{name: "number quoted", valueKind: FactValueKindNumber, raw: `"42"`},
		{name: "integer fractional", valueKind: FactValueKindInteger, raw: "42.5"},
		{name: "integer NaN", valueKind: FactValueKindInteger, raw: "NaN"},
		{name: "integer overflow", valueKind: FactValueKindInteger, raw: "9e999"},
		{name: "string unquoted", valueKind: FactValueKindString, raw: "material"},
		{name: "string non-canonical escape", valueKind: FactValueKindString, raw: `"a\u0041"`},
		{name: "string boolean", valueKind: FactValueKindString, raw: "true"},
		{name: "string missing", valueKind: FactValueKindString, raw: ""},
		{name: "boolean uppercase", valueKind: FactValueKindBoolean, raw: "TRUE"},
		{name: "boolean quoted", valueKind: FactValueKindBoolean, raw: `"true"`},
		{name: "boolean numeric", valueKind: FactValueKindBoolean, raw: "1"},
		{name: "unsupported value kind", valueKind: "float", raw: "42"},
		{name: "missing value kind", valueKind: "", raw: "42"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := CanonicalFactScalar(tt.valueKind, tt.raw); !errors.Is(err, ErrInvalidFact) {
				t.Fatalf("CanonicalFactScalar(%q, %q) error = %v, want ErrInvalidFact", tt.valueKind, tt.raw, err)
			}
		})
	}
}

func TestValidateFactBindingAcceptsBoundedContract(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*FactBinding)
	}{
		{name: "numeric without unit", mutate: func(b *FactBinding) { b.Unit = "" }},
		{name: "numeric with explicit unit", mutate: func(b *FactBinding) { b.Unit = "kg/m^3" }},
		{name: "integer", mutate: func(b *FactBinding) { b.ValueKind = FactValueKindInteger; b.Unit = "count" }},
		{name: "string", mutate: func(b *FactBinding) { b.ValueKind = FactValueKindString; b.Unit = "" }},
		{name: "boolean", mutate: func(b *FactBinding) { b.ValueKind = FactValueKindBoolean; b.Unit = "" }},
		{name: "required", mutate: func(b *FactBinding) { b.Requirement = FactRequirementRequired }},
		{name: "semantic annotation", mutate: func(b *FactBinding) { b.Subject.SemanticID = "part/body" }},
		{name: "property path", mutate: func(b *FactBinding) { b.Property = "Shape.Material" }},
		{name: "occurrence context", mutate: func(b *FactBinding) {
			b.Context = []OccurrenceContextFrame{{Parent: factTestEntity("widget", "Assembly"), Member: "Link1"}}
		}},
		{name: "nil context", mutate: func(b *FactBinding) { b.Context = nil }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binding := factTestBinding()
			tt.mutate(&binding)
			if err := ValidateFactBinding(binding); err != nil {
				t.Fatalf("ValidateFactBinding returned error: %v", err)
			}
		})
	}
}

func TestValidateFactBindingRejectsInvalidContract(t *testing.T) {
	factRejections := map[string]func(*FactBinding){
		"unsupported physical mass kind": func(b *FactBinding) { b.Kind = "physical_mass" },
		"missing property":               func(b *FactBinding) { b.Property = "" },
		"property with whitespace":       func(b *FactBinding) { b.Property = " Material " },
		"property too long":              func(b *FactBinding) { b.Property = strings.Repeat("p", 129) },
		"unsupported value kind":         func(b *FactBinding) { b.ValueKind = "float" },
		"unit on string value":           func(b *FactBinding) { b.ValueKind = FactValueKindString; b.Unit = "mm" },
		"unit on boolean value":          func(b *FactBinding) { b.ValueKind = FactValueKindBoolean; b.Unit = "mm" },
		"unit with whitespace":           func(b *FactBinding) { b.Unit = " mm " },
		"unit with newline":              func(b *FactBinding) { b.Unit = "mm\n" },
		"unit too long":                  func(b *FactBinding) { b.Unit = strings.Repeat("u", 33) },
		"invalid requirement":            func(b *FactBinding) { b.Requirement = "mandatory" },
		"context too large": func(b *FactBinding) {
			b.Context = make([]OccurrenceContextFrame, 17)
			for i := range b.Context {
				b.Context[i] = OccurrenceContextFrame{Parent: factTestEntity("widget", "Assembly"), Member: "Link1"}
			}
		},
	}
	for name, mutate := range factRejections {
		t.Run(name, func(t *testing.T) {
			binding := factTestBinding()
			mutate(&binding)
			if err := ValidateFactBinding(binding); !errors.Is(err, ErrInvalidFact) {
				t.Fatalf("ValidateFactBinding error = %v, want ErrInvalidFact", err)
			}
		})
	}

	// Subject qualification itself is owned by the #50 occurrence vocabulary.
	subjectRejections := map[string]func(*FactBinding){
		"missing resource":     func(b *FactBinding) { b.Subject.Resource = ReferenceResource{} },
		"unsupported resource": func(b *FactBinding) { b.Subject.Resource.Kind = resourcebinding.ResourceKindDSL },
		"missing native name":  func(b *FactBinding) { b.Subject.Name = "" },
		"unsupported native kind": func(b *FactBinding) {
			b.Subject.NativeKind = "PartDesign::Body"
		},
		"cyclic context": func(b *FactBinding) {
			b.Context = []OccurrenceContextFrame{{Parent: b.Subject, Member: "Link1"}}
		},
	}
	for name, mutate := range subjectRejections {
		t.Run(name, func(t *testing.T) {
			binding := factTestBinding()
			mutate(&binding)
			if err := ValidateFactBinding(binding); !errors.Is(err, ErrInvalidOccurrence) {
				t.Fatalf("ValidateFactBinding error = %v, want ErrInvalidOccurrence", err)
			}
		})
	}
}

func TestValidateFactRequestRejectsInvalidContract(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*FactRequest)
	}{
		{name: "unsupported schema version", mutate: func(r *FactRequest) { r.SchemaVersion = "2.0" }},
		{name: "blank schema version", mutate: func(r *FactRequest) { r.SchemaVersion = "" }},
		{name: "no bindings", mutate: func(r *FactRequest) { r.Bindings = nil }},
		{name: "too many bindings", mutate: func(r *FactRequest) {
			for len(r.Bindings) <= 32 {
				binding := factTestBinding()
				binding.Subject.Name = "Body" + string(rune('A'+len(r.Bindings)))
				r.Bindings = append(r.Bindings, binding)
			}
		}},
		{name: "equivalent duplicate binding", mutate: func(r *FactRequest) {
			r.Bindings = append(r.Bindings, CloneFactBinding(r.Bindings[0]))
		}},
		{name: "duplicate differs only by semantic annotation", mutate: func(r *FactRequest) {
			duplicate := CloneFactBinding(r.Bindings[0])
			duplicate.Subject.SemanticID = "part/body"
			r.Bindings = append(r.Bindings, duplicate)
		}},
		{name: "duplicate differs only by unit", mutate: func(r *FactRequest) {
			duplicate := CloneFactBinding(r.Bindings[0])
			duplicate.Unit = "cm"
			r.Bindings = append(r.Bindings, duplicate)
		}},
		{name: "duplicate differs only by value kind", mutate: func(r *FactRequest) {
			duplicate := CloneFactBinding(r.Bindings[0])
			duplicate.ValueKind = FactValueKindString
			duplicate.Unit = ""
			r.Bindings = append(r.Bindings, duplicate)
		}},
		{name: "invalid nested binding", mutate: func(r *FactRequest) { r.Bindings[0].Requirement = "sometimes" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := FactRequest{SchemaVersion: FactRequestSchemaVersion, Bindings: []FactBinding{factTestBinding()}}
			tt.mutate(&request)
			if err := ValidateFactRequest(request); !errors.Is(err, ErrInvalidFact) {
				t.Fatalf("ValidateFactRequest error = %v, want ErrInvalidFact", err)
			}
		})
	}

	valid := FactRequest{SchemaVersion: FactRequestSchemaVersion, Bindings: []FactBinding{factTestBinding()}}
	other := factTestBinding()
	other.Subject.Resource = factTestResource("part")
	other.Property = "Material"
	other.ValueKind = FactValueKindString
	other.Unit = ""
	valid.Bindings = append(valid.Bindings, other)
	if err := ValidateFactRequest(valid); err != nil {
		t.Fatalf("ValidateFactRequest(two distinct bindings) returned error: %v", err)
	}
}

func TestFactBindingKeyExcludesNonIdentityMaterial(t *testing.T) {
	base := factTestBinding()
	baseKey, err := FactBindingKey(base)
	if err != nil {
		t.Fatal(err)
	}

	// Semantic ID and value material are not fact identity; conflicting bindings
	// for one slot are rejected by ValidateFactRequest instead.
	annotated := CloneFactBinding(base)
	annotated.Subject.SemanticID = "part/body"
	annotated.Unit = "cm"
	annotated.ValueKind = FactValueKindInteger
	annotated.Requirement = FactRequirementRequired
	annotatedKey, err := FactBindingKey(annotated)
	if err != nil {
		t.Fatal(err)
	}
	if annotatedKey != baseKey {
		t.Fatalf("non-identity material changed the binding key:\nbase: %s\nannotated: %s", baseKey, annotatedKey)
	}

	distinct := map[string]func(*FactBinding){
		"resource": func(b *FactBinding) { b.Subject.Resource = factTestResource("part") },
		"native kind": func(b *FactBinding) {
			b.Subject.NativeKind = "object"
			b.Subject.Name = "Other"
		},
		"name":     func(b *FactBinding) { b.Subject.Name = "Other" },
		"property": func(b *FactBinding) { b.Property = "Material" },
		"kind":     func(b *FactBinding) { b.Kind = "other_fact" },
		"context": func(b *FactBinding) {
			b.Context = []OccurrenceContextFrame{{Parent: factTestEntity("widget", "Assembly"), Member: "Link1"}}
		},
	}
	for name, mutate := range distinct {
		t.Run(name, func(t *testing.T) {
			binding := CloneFactBinding(base)
			mutate(&binding)
			key, err := FactBindingKey(binding)
			if err != nil {
				t.Fatal(err)
			}
			if key == baseKey {
				t.Fatalf("%s did not change the binding key", name)
			}
		})
	}
}

func TestFactVocabularyIsClosed(t *testing.T) {
	if !KnownFactKind(FactKindBoundProperty) {
		t.Fatal("bound_property must be a known fact kind")
	}
	for _, kind := range []string{"", "physical_mass", "density", "quantity", "material_catalog"} {
		if KnownFactKind(kind) {
			t.Fatalf("KnownFactKind(%q) = true, want false", kind)
		}
	}
	for _, state := range []string{FactAvailabilityObserved, FactAvailabilityUnavailable, FactAvailabilityUnsupported} {
		if !KnownFactAvailability(state) {
			t.Fatalf("KnownFactAvailability(%q) = false, want true", state)
		}
	}
	for _, state := range []string{"", "missing", "skipped", "failed", "observed "} {
		if KnownFactAvailability(state) {
			t.Fatalf("KnownFactAvailability(%q) = true, want false", state)
		}
	}
}

func TestBuildObservationRecordFactKindPreservesQualifiedSubject(t *testing.T) {
	fact := factTestObservationFact()
	fact.Subject.SemanticID = "part/body"
	record, err := buildFactRecord(t, fact)
	if err != nil {
		t.Fatalf("BuildObservationRecord(fact) returned error: %v", err)
	}
	if record.Family != FamilyObservation {
		t.Fatalf("fact record family = %q, want the single observation family", record.Family)
	}
	if record.Observation.Occurrences != nil {
		t.Fatal("a fact-only record must not synthesize occurrences")
	}
	got := record.Observation.Facts[0]
	if got.Kind != ObservationKindFact || got.Key != FactKindBoundProperty {
		t.Fatalf("fact kind material changed: %#v", got)
	}
	if got.Subject.Resource == nil || *got.Subject.Resource != factTestResource("widget") {
		t.Fatalf("resource-qualified subject lost: %#v", got.Subject)
	}
	if got.Subject.Name != "Body" || got.Subject.NativeKind != "object" || got.Subject.SemanticID != "part/body" {
		t.Fatalf("native/semantic subject annotation lost: %#v", got.Subject)
	}
	if got.Property != "Length" || got.Unit != "mm" || got.Availability != FactAvailabilityObserved {
		t.Fatalf("fact provenance or value shape lost: %#v", got)
	}
	if got.Value != (ObservationValue{Kind: FactValueKindNumber, Raw: "42"}) {
		t.Fatalf("fact value = %#v, want canonical 42", got.Value)
	}
	if got.Evidence != factTestEvidence() {
		t.Fatalf("fact evidence = %#v, want canonical raw evidence provenance", got.Evidence)
	}
}

func TestBuildObservationRecordUnavailableFactIsExplicitAndValid(t *testing.T) {
	for _, availability := range []string{FactAvailabilityUnavailable, FactAvailabilityUnsupported} {
		t.Run(availability, func(t *testing.T) {
			fact := factTestObservationFact()
			fact.Value = ObservationValue{}
			fact.Unit = ""
			fact.Evidence = ObservationEvidence{}
			fact.Availability = availability
			record, err := buildFactRecord(t, fact)
			if err != nil {
				t.Fatalf("BuildObservationRecord(%s fact) returned error: %v", availability, err)
			}
			if len(record.Observation.Facts) != 1 {
				t.Fatalf("unavailable fact was omitted: %#v", record.Observation.Facts)
			}
			got := record.Observation.Facts[0]
			if got.Availability != availability || got.Value != (ObservationValue{}) || got.Unit != "" {
				t.Fatalf("unavailable fact substituted value material: %#v", got)
			}
		})
	}
}

func TestValidateObservationRecordRejectsInvalidFacts(t *testing.T) {
	tests := map[string]func(*ObservationFact){
		"missing resource qualification": func(f *ObservationFact) { f.Subject.Resource = nil },
		"unsupported resource kind": func(f *ObservationFact) {
			f.Subject.Resource = factTestResourcePointer("widget")
			f.Subject.Resource.Kind = resourcebinding.ResourceKindDSL
		},
		"missing native kind": func(f *ObservationFact) { f.Subject.NativeKind = "" },
		"unsupported native kind": func(f *ObservationFact) {
			f.Subject.NativeKind = "PartDesign::Body"
		},
		"missing native name":    func(f *ObservationFact) { f.Subject.Name = "" },
		"unsupported fact kind":  func(f *ObservationFact) { f.Key = "physical_mass" },
		"missing property":       func(f *ObservationFact) { f.Property = "" },
		"invalid availability":   func(f *ObservationFact) { f.Availability = "missing" },
		"observed without value": func(f *ObservationFact) { f.Value = ObservationValue{} },
		"observed without kind":  func(f *ObservationFact) { f.Value.Kind = "" },
		"non-canonical number":   func(f *ObservationFact) { f.Value = ObservationValue{Kind: FactValueKindNumber, Raw: "42.0"} },
		"non-finite number":      func(f *ObservationFact) { f.Value = ObservationValue{Kind: FactValueKindNumber, Raw: "Inf"} },
		"numeric opaque unit":    func(f *ObservationFact) { f.Value = ObservationValue{Kind: FactValueKindNumber, Raw: "2.45kg"} },
		"unit on string value":   func(f *ObservationFact) { f.Value = ObservationValue{Kind: FactValueKindString, Raw: `"steel"`} },
		"unit on boolean value":  func(f *ObservationFact) { f.Value = ObservationValue{Kind: FactValueKindBoolean, Raw: "true"} },
		"unobserved with value":  func(f *ObservationFact) { f.Availability = FactAvailabilityUnavailable },
		"unobserved with unit":   func(f *ObservationFact) { f.Value = ObservationValue{}; f.Availability = FactAvailabilityUnavailable },
		"observed without evidence": func(f *ObservationFact) {
			f.Evidence = ObservationEvidence{}
		},
		"non-canonical evidence source": func(f *ObservationFact) {
			f.Evidence.SourceRef = "raw/runtime/prm.fact-evidence.json"
		},
		"missing evidence digest": func(f *ObservationFact) { f.Evidence.DigestSHA256 = "" },
		"malformed evidence digest": func(f *ObservationFact) {
			f.Evidence.DigestSHA256 = "not-a-digest"
		},
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			fact := factTestObservationFact()
			mutate(&fact)
			if _, err := buildFactRecord(t, fact); !errors.Is(err, ErrInvalidObservationRecord) {
				t.Fatalf("BuildObservationRecord error = %v, want ErrInvalidObservationRecord", err)
			}
		})
	}
}

func TestBuildObservationRecordFactConflictSemantics(t *testing.T) {
	base := factTestObservationFact()

	changedValue := base
	changedValue.Value = ObservationValue{Kind: FactValueKindNumber, Raw: "43"}
	changedUnit := base
	changedUnit.Unit = "cm"
	changedAvailability := base
	changedAvailability.Value = ObservationValue{}
	changedAvailability.Unit = ""
	changedAvailability.Evidence = ObservationEvidence{}
	changedAvailability.Availability = FactAvailabilityUnavailable
	changedSemantic := base
	changedSemantic.Subject.SemanticID = "part/body"

	conflicts := map[string]ObservationFact{
		"conflicting value":        changedValue,
		"conflicting unit":         changedUnit,
		"conflicting availability": changedAvailability,
		"conflicting semantic identity is not a distinct fact": changedSemantic,
	}
	for name, other := range conflicts {
		t.Run(name, func(t *testing.T) {
			if _, err := buildFactRecord(t, base, other); !errors.Is(err, ErrInvalidObservationRecord) || !strings.Contains(err.Error(), "conflict") {
				t.Fatalf("BuildObservationRecord error = %v, want a deterministic conflict rejection", err)
			}
		})
	}

	if _, err := buildFactRecord(t, base, base); !errors.Is(err, ErrInvalidObservationRecord) {
		t.Fatalf("exact duplicate error = %v, want ErrInvalidObservationRecord", err)
	}

	sameNameOtherResource := base
	sameNameOtherResource.Subject.Resource = factTestResourcePointer("part")
	occurrenceScoped := base
	occurrenceScoped.Subject.ID = ""
	occurrenceScoped.Subject.Context = []OccurrenceContextFrame{{Parent: factTestEntity("widget", "Assembly"), Member: "Link1"}}
	record, err := buildFactRecord(t, base, sameNameOtherResource, occurrenceScoped)
	if err != nil {
		t.Fatalf("distinct subjects were treated as conflicting: %v", err)
	}
	if len(record.Observation.Facts) != 3 {
		t.Fatalf("distinct subjects collapsed: %#v", record.Observation.Facts)
	}
	seenResources := map[ReferenceResource]bool{}
	seenScoped := 0
	for _, fact := range record.Observation.Facts {
		if fact.Subject.Resource != nil {
			seenResources[*fact.Subject.Resource] = true
		}
		if len(fact.Subject.Context) > 0 {
			seenScoped++
		}
	}
	if !seenResources[factTestResource("widget")] || !seenResources[factTestResource("part")] || seenScoped != 1 {
		t.Fatalf("resource or occurrence scope collapsed: %#v", record.Observation.Facts)
	}
}

func TestBuildObservationRecordFactIdentityAndOrderingAreDeterministic(t *testing.T) {
	first := factTestObservationFact()
	second := factTestObservationFact()
	second.Subject.Name = "Cover"
	second.Subject.Resource = factTestResourcePointer("part")
	second.Property = "Material"
	second.Value = ObservationValue{Kind: FactValueKindString, Raw: `"AL-6061"`}
	second.Unit = ""
	third := factTestObservationFact()
	third.Subject.Name = "Body"
	third.Subject.Context = []OccurrenceContextFrame{{Parent: factTestEntity("widget", "Assembly"), Member: "Link1"}}
	third.Value = ObservationValue{Kind: FactValueKindNumber, Raw: "0"}

	orders := [][]ObservationFact{{first, second, third}, {third, first, second}, {second, third, first}}
	var want Identity
	var wantFacts []ObservationFact
	for i, facts := range orders {
		record, err := buildFactRecord(t, facts...)
		if err != nil {
			t.Fatalf("order %d: BuildObservationRecord returned error: %v", i, err)
		}
		if i == 0 {
			want = record.Identity
			wantFacts = record.Observation.Facts
			continue
		}
		if record.Identity != want {
			t.Fatalf("order %d changed identity: %#v vs %#v", i, record.Identity, want)
		}
		if !reflect.DeepEqual(record.Observation.Facts, wantFacts) {
			t.Fatalf("order %d changed normalized ordering", i)
		}
	}

	mutations := map[string]func(*ObservationFact){
		"value":        func(f *ObservationFact) { f.Value.Raw = "7" },
		"unit":         func(f *ObservationFact) { f.Unit = "cm" },
		"availability": func(f *ObservationFact) { f.Availability = FactAvailabilityUnsupported },
		"property":     func(f *ObservationFact) { f.Property = "Width" },
		"resource":     func(f *ObservationFact) { f.Subject.Resource = factTestResourcePointer("part") },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			fact := first
			mutate(&fact)
			if fact.Availability != FactAvailabilityObserved {
				fact.Value = ObservationValue{}
				fact.Unit = ""
				fact.Evidence = ObservationEvidence{}
			}
			record, err := buildFactRecord(t, fact)
			if err != nil {
				t.Fatalf("BuildObservationRecord returned error: %v", err)
			}
			if record.Identity == want {
				t.Fatalf("%s did not change record identity", name)
			}
		})
	}
}

// TestParameterObservationRecordIdentityMatchesPreChangeGolden is a permanent
// guard for established observation kinds. The golden record below was produced
// by the parent commit of the #51 implementation, so any change to existing-kind
// canonical identity material fails this test.
func TestParameterObservationRecordIdentityMatchesPreChangeGolden(t *testing.T) {
	goldenBytes, err := os.ReadFile("testdata/parameter-observation-record.golden.json")
	if err != nil {
		t.Fatalf("read golden record: %v", err)
	}
	record, err := BuildObservationRecord(goldenObservationRecordInput())
	if err != nil {
		t.Fatalf("BuildObservationRecord returned error: %v", err)
	}
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	if got := append(encoded, '\n'); string(got) != string(goldenBytes) {
		t.Fatalf("existing-kind observation record changed:\ngot:\n%s\nwant:\n%s", got, goldenBytes)
	}
	var golden ObservationRecord
	if err := json.Unmarshal(goldenBytes, &golden); err != nil {
		t.Fatalf("decode golden record: %v", err)
	}
	if record.Identity != golden.Identity {
		t.Fatalf("identity = %#v, golden = %#v", record.Identity, golden.Identity)
	}
	if err := ValidateObservationRecord(golden); err != nil {
		t.Fatalf("golden record is invalid: %v", err)
	}
}

func goldenObservationRecordInput() ObservationRecordInput {
	digest := strings.Repeat("a", 64)
	return ObservationRecordInput{
		RecordKey: "engine-run:golden-plan:observation",
		Provenance: Provenance{
			SourceRevision: SourceRevisionProvenance{RevisionID: "rev-golden"},
			Inputs:         []ProvenanceInput{{Kind: "dsl", Identity: "model-golden", DigestSHA256: strings.Repeat("1", 64)}},
			Plan:           PlanProvenance{PlanID: "plan-golden", PlanHash: "hash-golden"},
			Linkage:        LinkageProvenance{JobID: "job-golden", ProductKey: "widget", StepRef: "step-golden"},
			Evidence:       []EvidenceReference{{Kind: "observed", Ref: "raw/observed/prm.observed.json", DigestSHA256: digest}},
			Runtime:        RuntimeProvenance{ToolID: "parametron", RuntimeID: "runtime-golden", Adapter: "freecad"},
		},
		Observation: ObservationSummary{Facts: []ObservationFact{{
			Kind:    ObservationKindParameter,
			Subject: ObservationSubject{ID: "part/body", Name: "Body"},
			Key:     "length",
			Value:   ObservationValue{Kind: "number", Raw: "42.0"},
			Linkage: ObservationLinkage{JobID: "job-golden", ProductKey: "widget", StepRef: "step-golden"},
			Evidence: ObservationEvidence{
				SourceKind:   "observed",
				SourceRef:    "raw/observed/prm.observed.json",
				DigestSHA256: digest,
			},
		}}},
	}
}
