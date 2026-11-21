package recordcontract

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// Issue #74 permanent contract: the observation record keeps semantic metadata
// identity and native owner/property identity distinct, serializes both, and
// orders metadata facts deterministically when only the native owner differs.

func metadataOwnerObservationFact(ownerID string) ObservationFact {
	return ObservationFact{
		Kind: ObservationKindMetadata,
		Subject: ObservationSubject{
			ID:      "meta.bracket.material",
			Name:    "material",
			OwnerID: ownerID,
		},
		Key:   "material",
		Value: ObservationValue{Kind: "string", Raw: `"S235JR"`},
		Linkage: ObservationLinkage{
			JobID:      "job-a",
			ProductKey: "product-a",
			StepRef:    "observe",
		},
		Evidence: ObservationEvidence{
			SourceKind:   "observed",
			SourceRef:    "raw/observed/prm.observed.json",
			DigestSHA256: digestC(),
		},
	}
}

func TestObservationRecord_MetadataSubjectPreservesSemanticAndNativeIdentity(t *testing.T) {
	input := validObservationRecordInput()
	input.Observation.Facts = []ObservationFact{metadataOwnerObservationFact("Body01")}

	record, err := BuildObservationRecord(input)
	if err != nil {
		t.Fatalf("BuildObservationRecord: %v", err)
	}
	if len(record.Observation.Facts) != 1 {
		t.Fatalf("facts: %#v", record.Observation.Facts)
	}
	fact := record.Observation.Facts[0]
	if fact.Subject.ID != "meta.bracket.material" {
		t.Fatalf("semantic metadata ID lost: %#v", fact.Subject)
	}
	if fact.Subject.OwnerID != "Body01" {
		t.Fatalf("native owner identity lost: %#v", fact.Subject)
	}
	if fact.Subject.Name != "material" || fact.Key != "material" {
		t.Fatalf("native property identity lost: %#v", fact)
	}
	if fact.Subject.ID == fact.Subject.OwnerID {
		t.Fatalf("semantic and native identity must remain distinct: %#v", fact.Subject)
	}
	if fact.Value.Kind != "string" || fact.Value.Raw != `"S235JR"` {
		t.Fatalf("typed metadata value lost: %#v", fact.Value)
	}

	payload, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, fragment := range []string{
		`"id":"meta.bracket.material"`,
		`"ownerId":"Body01"`,
		`"name":"material"`,
		`"key":"material"`,
		`"kind":"string"`,
		`"raw":"\"S235JR\""`,
		`"sourceRef":"raw/observed/prm.observed.json"`,
	} {
		if !strings.Contains(string(payload), fragment) {
			t.Fatalf("observation record JSON missing %q: %s", fragment, payload)
		}
	}
}

func TestObservationRecord_MetadataOwnerIdentityDeterministic(t *testing.T) {
	first := validObservationRecordInput()
	first.Observation.Facts = []ObservationFact{
		metadataOwnerObservationFact("Body02"),
		metadataOwnerObservationFact("Body01"),
	}
	second := validObservationRecordInput()
	second.Observation.Facts = []ObservationFact{
		metadataOwnerObservationFact("Body01"),
		metadataOwnerObservationFact("Body02"),
	}

	firstRecord, err := BuildObservationRecord(first)
	if err != nil {
		t.Fatalf("first build: %v", err)
	}
	secondRecord, err := BuildObservationRecord(second)
	if err != nil {
		t.Fatalf("second build: %v", err)
	}
	if !reflect.DeepEqual(firstRecord, secondRecord) {
		t.Fatalf("metadata owner identity is order dependent:\nfirst:  %#v\nsecond: %#v", firstRecord, secondRecord)
	}
	if len(firstRecord.Observation.Facts) != 2 {
		t.Fatalf("facts: %#v", firstRecord.Observation.Facts)
	}
	if firstRecord.Observation.Facts[0].Subject.OwnerID != "Body01" ||
		firstRecord.Observation.Facts[1].Subject.OwnerID != "Body02" {
		t.Fatalf("metadata owner ordering is not canonical: %#v", firstRecord.Observation.Facts)
	}
}
