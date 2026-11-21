package recordmap

import (
	"reflect"
	"testing"

	"parametron/internal/engine/observed"
	"parametron/internal/engine/recordcontract"
)

// Issue #74 permanent contract: normalized observation facts preserve the
// semantic metadata ID separately from the exact native owner/property
// identity, along with the typed observed value. The raw observed artifact
// remains the single evidence source; normalized facts are not a second store.

func observedMetadataIdentityFixture() observed.Observed {
	obs := observedMapObserved()
	obs.Observation.Metadata = append(obs.Observation.Metadata,
		observed.Metadata{
			ID:        "meta.bracket.material",
			Key:       "material",
			OwnerID:   "Body01",
			ValueKind: "string",
			Value:     observedMapValue(tJSON(`"S235JR"`)),
		},
		observed.Metadata{
			ID:        "meta.feature.revision",
			Key:       "revision",
			OwnerID:   "MountingBracket01",
			ValueKind: "integer",
			Value:     observedMapValue(tJSON(`7`)),
		},
	)
	return obs
}

func TestMapObserved_NormalizedMetadataPreservesSemanticAndNativeIdentity(t *testing.T) {
	input := observedMapInput(observedMetadataIdentityFixture())
	record, err := MapObservedToObservationRecord(input)
	if err != nil {
		t.Fatalf("MapObservedToObservationRecord: %v", err)
	}
	if err := recordcontract.ValidateObservationRecord(record); err != nil {
		t.Fatalf("ValidateObservationRecord: %v", err)
	}

	byID := map[string]recordcontract.ObservationFact{}
	for _, fact := range record.Observation.Facts {
		if fact.Kind == recordcontract.ObservationKindMetadata {
			byID[fact.Subject.ID] = fact
		}
	}

	material, ok := byID["meta.bracket.material"]
	if !ok {
		t.Fatalf("material metadata fact missing: %#v", record.Observation.Facts)
	}
	if material.Subject.OwnerID != "Body01" {
		t.Fatalf("native owner identity lost: %#v", material)
	}
	if material.Subject.Name != "material" || material.Key != "material" {
		t.Fatalf("native property identity lost: %#v", material)
	}
	if material.Subject.ID == material.Subject.OwnerID {
		t.Fatalf("semantic metadata ID must remain distinct from native owner: %#v", material)
	}
	if material.Value.Kind != "string" || material.Value.Raw != `"S235JR"` {
		t.Fatalf("typed metadata value lost: %#v", material.Value)
	}

	revision, ok := byID["meta.feature.revision"]
	if !ok {
		t.Fatalf("revision metadata fact missing: %#v", record.Observation.Facts)
	}
	if revision.Subject.OwnerID != "MountingBracket01" || revision.Subject.Name != "revision" || revision.Key != "revision" {
		t.Fatalf("revision native identity lost: %#v", revision)
	}
	if revision.Value.Kind != "integer" || revision.Value.Raw != "7" {
		t.Fatalf("revision typed value lost: %#v", revision.Value)
	}

	// Raw observed evidence remains the reference source for every normalized
	// fact; the normalized record is not an independent metadata store.
	if material.Evidence.SourceKind != observedEvidenceKind || material.Evidence.SourceRef != observedEvidenceRef {
		t.Fatalf("metadata fact lost raw evidence reference: %#v", material.Evidence)
	}
}

func TestMapObserved_MetadataIdentityIsOrderIndependent(t *testing.T) {
	first := observedMetadataIdentityFixture()
	second := observedMetadataIdentityFixture()
	second.Observation.Metadata = []observed.Metadata{
		second.Observation.Metadata[3],
		second.Observation.Metadata[2],
		second.Observation.Metadata[1],
		second.Observation.Metadata[0],
	}

	firstRecord, err := MapObservedToObservationRecord(observedMapInput(first))
	if err != nil {
		t.Fatalf("first map: %v", err)
	}
	secondRecord, err := MapObservedToObservationRecord(observedMapInput(second))
	if err != nil {
		t.Fatalf("second map: %v", err)
	}
	if !reflect.DeepEqual(firstRecord.Identity, secondRecord.Identity) {
		t.Fatalf("metadata observation identity depends on source order:\nfirst:  %#v\nsecond: %#v", firstRecord.Identity, secondRecord.Identity)
	}
	if !reflect.DeepEqual(firstRecord.Observation.Facts, secondRecord.Observation.Facts) {
		t.Fatalf("normalized metadata facts depend on source order:\nfirst:  %#v\nsecond: %#v", firstRecord.Observation.Facts, secondRecord.Observation.Facts)
	}
}
