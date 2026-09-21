package recordcontract

import (
	"errors"
	"reflect"
	"testing"
)

// Issue #18: normalized target-state vocabulary inside the existing
// observation and verification record families.

func targetStateObservationFact(destination, object, key, raw string) ObservationFact {
	return ObservationFact{
		Kind:    ObservationKindTargetState,
		Subject: ObservationSubject{ID: destination, Name: object},
		Key:     key,
		Value:   ObservationValue{Kind: "target_state_evidence", Raw: raw},
		Linkage: ObservationLinkage{JobID: "job-a", ProductKey: "product-a", StepRef: "observe"},
		Evidence: ObservationEvidence{
			SourceKind:   "observed",
			SourceRef:    "raw/observed/prm.observed.json",
			DigestSHA256: digestB(),
		},
	}
}

func targetStateObservationInput(facts ...ObservationFact) ObservationRecordInput {
	input := validObservationRecordInput()
	input.Observation.Facts = facts
	return input
}

func mixedTargetStateObservationFacts() []ObservationFact {
	return []ObservationFact{
		targetStateObservationFact("part", "Sketch", "suppression", `{"status":"observed","value":true}`),
		targetStateObservationFact("assembly", "Body", "visibility", `{"status":"observed","value":false}`),
		targetStateObservationFact("assembly", "Gone", "existence", `{"status":"absent"}`),
		targetStateObservationFact("part", "Pad", "suppression", `{"status":"target_missing"}`),
		targetStateObservationFact("part", "Pad", "visibility", `{"status":"unavailable"}`),
		targetStateObservationFact("part", "Old", "existence", `{"status":"exists"}`),
	}
}

func TestObservationRecordAcceptsTargetStateFacts(t *testing.T) {
	record := mustBuildObservationRecord(t, targetStateObservationInput(mixedTargetStateObservationFacts()...))
	if err := ValidateObservationRecord(record); err != nil {
		t.Fatalf("ValidateObservationRecord: %v", err)
	}
	if len(record.Observation.Facts) != 6 {
		t.Fatalf("facts=%d", len(record.Observation.Facts))
	}
	for _, fact := range record.Observation.Facts {
		if fact.Kind != ObservationKindTargetState || fact.Value.Kind != "target_state_evidence" {
			t.Fatalf("fact=%+v", fact)
		}
	}

	// Kind is trimmed like the other observation kinds; near-miss spellings stay unknown.
	spaced := targetStateObservationFact("part", "Pad", "existence", `{"status":"absent"}`)
	spaced.Kind = " target_state "
	if _, err := BuildObservationRecord(targetStateObservationInput(spaced)); err != nil {
		t.Fatalf("trimmed kind rejected: %v", err)
	}
	for _, kind := range []ObservationKind{"target-state", "targetState", "TARGET_STATE"} {
		fact := targetStateObservationFact("part", "Pad", "existence", `{"status":"absent"}`)
		fact.Kind = kind
		if _, err := BuildObservationRecord(targetStateObservationInput(fact)); !errors.Is(err, ErrInvalidObservationRecord) {
			t.Fatalf("kind %q accepted: %v", kind, err)
		}
	}
}

func TestObservationRecordRejectsInvalidTargetStateFacts(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ObservationFact)
	}{
		{"missing destination", func(f *ObservationFact) { f.Subject.ID = " " }},
		{"missing object", func(f *ObservationFact) { f.Subject.Name = "\t" }},
		{"missing destination and object", func(f *ObservationFact) { f.Subject = ObservationSubject{} }},
		{"only group identity", func(f *ObservationFact) { f.Subject = ObservationSubject{GroupID: "g", OwnerID: "o"} }},
		{"empty family key", func(f *ObservationFact) { f.Key = " " }},
		{"unsupported family key", func(f *ObservationFact) { f.Key = "deletion" }},
		{"family key case differs", func(f *ObservationFact) { f.Key = "Suppression" }},
		{"family key plural", func(f *ObservationFact) { f.Key = "visibilities" }},
		{"missing value kind", func(f *ObservationFact) { f.Value.Kind = " " }},
		{"wrong value kind", func(f *ObservationFact) { f.Value.Kind = "string" }},
		{"missing value raw", func(f *ObservationFact) { f.Value.Raw = "\n" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fact := targetStateObservationFact("part", "Pad", "suppression", `{"status":"observed","value":true}`)
			tt.mutate(&fact)
			if _, err := BuildObservationRecord(targetStateObservationInput(fact)); !errors.Is(err, ErrInvalidObservationRecord) {
				t.Fatalf("BuildObservationRecord err=%v, want ErrInvalidObservationRecord", err)
			}

			// A stored, already-built record that is mutated into the same shape is rejected too.
			valid := mustBuildObservationRecord(t, targetStateObservationInput(
				targetStateObservationFact("part", "Pad", "suppression", `{"status":"observed","value":true}`)))
			record := cloneObservationRecord(valid)
			tt.mutate(&record.Observation.Facts[0])
			if err := ValidateObservationRecord(record); !errors.Is(err, ErrInvalidObservationRecord) {
				t.Fatalf("ValidateObservationRecord err=%v, want ErrInvalidObservationRecord", err)
			}
		})
	}
}

func TestObservationRecordTargetStateOrderingAndIdentityAreDeterministic(t *testing.T) {
	facts := mixedTargetStateObservationFacts()
	base := mustBuildObservationRecord(t, targetStateObservationInput(facts...))

	reversed := make([]ObservationFact, len(facts))
	for i, fact := range facts {
		reversed[len(facts)-1-i] = fact
	}
	other := mustBuildObservationRecord(t, targetStateObservationInput(reversed...))
	if !reflect.DeepEqual(base.Observation.Facts, other.Observation.Facts) || base.Identity != other.Identity {
		t.Fatalf("fact ordering leaked into the record\nbase:  %+v\nother: %+v", base.Observation.Facts, other.Observation.Facts)
	}
	// Canonical order: destination, then object, then key.
	for i := 1; i < len(base.Observation.Facts); i++ {
		prev, cur := base.Observation.Facts[i-1], base.Observation.Facts[i]
		if prev.Subject.ID > cur.Subject.ID || (prev.Subject.ID == cur.Subject.ID && prev.Subject.Name > cur.Subject.Name) ||
			(prev.Subject.ID == cur.Subject.ID && prev.Subject.Name == cur.Subject.Name && prev.Key > cur.Key) {
			t.Fatalf("facts not canonically ordered at %d: %+v then %+v", i, prev, cur)
		}
	}

	t.Run("input facts are not reordered or mutated", func(t *testing.T) {
		input := targetStateObservationInput(reversed...)
		before := append([]ObservationFact(nil), input.Observation.Facts...)
		mustBuildObservationRecord(t, input)
		if !reflect.DeepEqual(before, input.Observation.Facts) {
			t.Fatalf("BuildObservationRecord mutated its input")
		}
	})

	tests := []struct {
		name   string
		mutate func(*ObservationFact)
	}{
		{"boolean value", func(f *ObservationFact) { f.Value.Raw = `{"status":"observed","value":false}` }},
		{"status", func(f *ObservationFact) { f.Value.Raw = `{"status":"unavailable"}` }},
		{"destination", func(f *ObservationFact) { f.Subject.ID = "assembly" }},
		{"object", func(f *ObservationFact) { f.Subject.Name = "Other" }},
		{"family", func(f *ObservationFact) { f.Key = "visibility" }},
		{"evidence digest", func(f *ObservationFact) { f.Evidence.DigestSHA256 = digestC() }},
	}
	for _, tt := range tests {
		t.Run("identity changes with "+tt.name, func(t *testing.T) {
			changed := append([]ObservationFact(nil), facts...)
			tt.mutate(&changed[0])
			got := mustBuildObservationRecord(t, targetStateObservationInput(changed...))
			if got.Identity.ID == base.Identity.ID {
				t.Fatalf("identity did not change with %s", tt.name)
			}
		})
	}
}

// ===================== Verification record =====================

func targetStateVerificationCategory(outcome VerificationOutcome, class VerificationFailureClass, message string) VerificationCategoryResult {
	return VerificationCategoryResult{
		Category:     VerificationCategoryTargetState,
		Enabled:      true,
		Outcome:      outcome,
		Message:      message,
		FailureClass: class,
		Evidence: []VerificationEvidence{
			{SourceKind: "verification", SourceRef: "raw/verification/prm.verification.json", DigestSHA256: digestA()},
			{SourceKind: "observed", SourceRef: "raw/observed/prm.observed.json", DigestSHA256: digestB()},
		},
	}
}

func targetStateVerificationInput(category VerificationCategoryResult, topOutcome VerificationOutcome, topClass VerificationFailureClass) VerificationRecordInput {
	input := validVerificationRecordInput()
	input.Verification.Outcome = topOutcome
	input.Verification.FailureClass = topClass
	if topOutcome != VerificationOutcomeFail {
		input.Verification.Message = ""
	}
	input.Verification.Categories = []VerificationCategoryResult{
		{Category: VerificationCategoryComponents, Enabled: false, Outcome: VerificationOutcomeSkipped},
		{Category: VerificationCategoryParameters, Enabled: false, Outcome: VerificationOutcomeSkipped},
		{Category: VerificationCategoryMetadata, Enabled: true, Outcome: VerificationOutcomePass},
		{Category: VerificationCategoryReferences, Enabled: true, Outcome: VerificationOutcomePass},
		category,
	}
	return input
}

func TestVerificationRecordAcceptsTargetStateVocabulary(t *testing.T) {
	if string(VerificationCategoryTargetState) != "target_state" ||
		string(VerificationFailureClassTargetStateMismatch) != "target_state_mismatch" ||
		string(VerificationFailureClassTargetMissing) != "target_missing" ||
		string(VerificationFailureClassNativeEvidenceUnavailable) != "native_evidence_unavailable" {
		t.Fatal("target-state vocabulary strings changed")
	}

	classes := []VerificationFailureClass{
		VerificationFailureClassTargetStateMismatch,
		VerificationFailureClassTargetMissing,
		VerificationFailureClassNativeEvidenceUnavailable,
		VerificationFailureClassRequiredObservationMissing,
	}
	for _, class := range classes {
		t.Run(string(class), func(t *testing.T) {
			input := targetStateVerificationInput(targetStateVerificationCategory(VerificationOutcomeFail, class, "failed"), VerificationOutcomeFail, class)
			record := mustBuildVerificationRecord(t, input)
			if err := ValidateVerificationRecord(record); err != nil {
				t.Fatalf("ValidateVerificationRecord: %v", err)
			}
			if record.Verification.FailureClass != class {
				t.Fatalf("top-level class=%q", record.Verification.FailureClass)
			}
		})
	}
	t.Run("pass", func(t *testing.T) {
		input := targetStateVerificationInput(targetStateVerificationCategory(VerificationOutcomePass, "", ""), VerificationOutcomePass, "")
		if err := ValidateVerificationRecord(mustBuildVerificationRecord(t, input)); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("disabled category is skipped", func(t *testing.T) {
		category := VerificationCategoryResult{Category: VerificationCategoryTargetState, Enabled: false, Outcome: VerificationOutcomeSkipped}
		input := targetStateVerificationInput(category, VerificationOutcomePass, "")
		if err := ValidateVerificationRecord(mustBuildVerificationRecord(t, input)); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("trimmed vocabulary", func(t *testing.T) {
		if NormalizeVerificationCategory(" target_state ") != VerificationCategoryTargetState ||
			NormalizeVerificationFailureClass(" target_missing ") != VerificationFailureClassTargetMissing {
			t.Fatal("normalizers must trim target-state vocabulary")
		}
	})
}

func TestVerificationRecordRejectsInvalidTargetStateVocabulary(t *testing.T) {
	failing := func(mutate func(*VerificationRecordInput)) VerificationRecordInput {
		input := targetStateVerificationInput(targetStateVerificationCategory(VerificationOutcomeFail, VerificationFailureClassTargetStateMismatch, "mismatch"),
			VerificationOutcomeFail, VerificationFailureClassTargetStateMismatch)
		mutate(&input)
		return input
	}
	tests := []struct {
		name  string
		input VerificationRecordInput
	}{
		{"unknown category spelling target-state", failing(func(i *VerificationRecordInput) { i.Verification.Categories[4].Category = "target-state" })},
		{"unknown category spelling targetState", failing(func(i *VerificationRecordInput) { i.Verification.Categories[4].Category = "targetState" })},
		{"unknown category suppression", failing(func(i *VerificationRecordInput) { i.Verification.Categories[4].Category = "suppression" })},
		{"unknown category failure class", failing(func(i *VerificationRecordInput) { i.Verification.Categories[4].FailureClass = "target_state_missing" })},
		{"unknown top-level failure class", failing(func(i *VerificationRecordInput) { i.Verification.FailureClass = "target_unavailable" })},
		{"failed target-state category without class", failing(func(i *VerificationRecordInput) { i.Verification.Categories[4].FailureClass = "" })},
		{"failed target-state category without message", failing(func(i *VerificationRecordInput) { i.Verification.Categories[4].Message = "" })},
		{"passed target-state category with class", failing(func(i *VerificationRecordInput) {
			i.Verification.Categories[4].Outcome = VerificationOutcomePass
			i.Verification.Categories[4].Message = ""
		})},
		{"disabled target-state category not skipped", failing(func(i *VerificationRecordInput) {
			i.Verification.Categories[4].Enabled = false
			i.Verification.Categories[4].FailureClass = ""
			i.Verification.Categories[4].Message = ""
		})},
		{"duplicate target-state category", failing(func(i *VerificationRecordInput) {
			i.Verification.Categories = append(i.Verification.Categories, i.Verification.Categories[4])
		})},
		{"target-state evidence digest not sha256", failing(func(i *VerificationRecordInput) { i.Verification.Categories[4].Evidence[1].DigestSHA256 = "xyz" })},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := BuildVerificationRecord(tt.input); !errors.Is(err, ErrInvalidVerificationRecord) {
				t.Fatalf("BuildVerificationRecord err=%v, want ErrInvalidVerificationRecord", err)
			}
		})
	}
}

func TestVerificationRecordTargetStateNormalizationIsDeterministicAndCopySafe(t *testing.T) {
	input := targetStateVerificationInput(targetStateVerificationCategory(VerificationOutcomeFail, VerificationFailureClassTargetMissing, "missing"),
		VerificationOutcomeFail, VerificationFailureClassTargetMissing)
	base := mustBuildVerificationRecord(t, input)

	var categories []VerificationCategory
	for _, category := range base.Verification.Categories {
		categories = append(categories, category.Category)
	}
	// Canonical record ordering is lexical by category vocabulary value.
	wantOrder := []VerificationCategory{
		VerificationCategoryComponents, VerificationCategoryMetadata, VerificationCategoryParameters,
		VerificationCategoryReferences, VerificationCategoryTargetState,
	}
	if !reflect.DeepEqual(categories, wantOrder) {
		t.Fatalf("category order=%v want %v", categories, wantOrder)
	}

	reordered := cloneVerificationRecordInput(input)
	src := input.Verification.Categories
	swapped := src[4]
	swapped.Evidence = []VerificationEvidence{src[4].Evidence[1], src[4].Evidence[0]}
	reordered.Verification.Categories = []VerificationCategoryResult{swapped, src[3], src[2], src[1], src[0]}
	got := mustBuildVerificationRecord(t, reordered)
	if !reflect.DeepEqual(got, base) {
		t.Fatalf("category/evidence input order changed the record\nbase: %+v\ngot:  %+v", base, got)
	}

	// Building must not mutate the caller's category slice or its evidence.
	before := cloneVerificationRecordInput(reordered)
	for i := range before.Verification.Categories {
		before.Verification.Categories[i].Evidence = append([]VerificationEvidence(nil), reordered.Verification.Categories[i].Evidence...)
	}
	mustBuildVerificationRecord(t, reordered)
	if !reflect.DeepEqual(before.Verification.Categories, reordered.Verification.Categories) {
		t.Fatal("BuildVerificationRecord mutated input categories")
	}

	// Mutating the built record's target-state evidence never reaches the input.
	got.Verification.Categories[4].Evidence[0].SourceRef = "changed"
	if reordered.Verification.Categories[0].Evidence[0].SourceRef == "changed" || input.Verification.Categories[4].Evidence[0].SourceRef == "changed" {
		t.Fatal("record aliases input evidence")
	}
}

func TestVerificationRecordIdentityTracksTargetStateMaterial(t *testing.T) {
	base := targetStateVerificationInput(targetStateVerificationCategory(VerificationOutcomeFail, VerificationFailureClassTargetStateMismatch, "mismatch"),
		VerificationOutcomeFail, VerificationFailureClassTargetStateMismatch)
	baseRecord := mustBuildVerificationRecord(t, base)

	if again := mustBuildVerificationRecord(t, cloneVerificationRecordInput(base)); again.Identity != baseRecord.Identity {
		t.Fatal("identity is not stable for equivalent input")
	}

	tests := []struct {
		name   string
		mutate func(*VerificationRecordInput)
	}{
		{"target-state failure class", func(i *VerificationRecordInput) {
			i.Verification.Categories[4].FailureClass = VerificationFailureClassNativeEvidenceUnavailable
		}},
		{"target-state message", func(i *VerificationRecordInput) { i.Verification.Categories[4].Message = "different" }},
		{"target-state outcome", func(i *VerificationRecordInput) {
			i.Verification.Categories[4].Outcome = VerificationOutcomePass
			i.Verification.Categories[4].Message = ""
			i.Verification.Categories[4].FailureClass = ""
		}},
		{"observed evidence digest", func(i *VerificationRecordInput) { i.Verification.Categories[4].Evidence[1].DigestSHA256 = digestC() }},
		{"observed evidence removed", func(i *VerificationRecordInput) {
			i.Verification.Categories[4].Evidence = i.Verification.Categories[4].Evidence[:1]
		}},
		{"target-state category removed", func(i *VerificationRecordInput) {
			i.Verification.Categories = i.Verification.Categories[:4]
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changed := cloneVerificationRecordInput(base)
			changed.Verification.Categories = append([]VerificationCategoryResult(nil), base.Verification.Categories...)
			changed.Verification.Categories[4].Evidence = append([]VerificationEvidence(nil), base.Verification.Categories[4].Evidence...)
			tt.mutate(&changed)
			got, err := BuildVerificationRecord(changed)
			if err != nil {
				t.Fatalf("BuildVerificationRecord: %v", err)
			}
			if got.Identity.ID == baseRecord.Identity.ID {
				t.Fatalf("identity did not change with %s", tt.name)
			}
		})
	}
}
