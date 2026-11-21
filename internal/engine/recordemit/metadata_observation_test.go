package recordemit_test

import (
	"bytes"
	"testing"

	"parametron/internal/engine/observed"
	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordemit"
	"parametron/internal/engine/recordpackage"
	"parametron/internal/engine/verification"
)

// Issue #74 permanent contract: authored metadata observed during CAD runtime
// execution is emitted through the existing normalized observation record and
// record-package surface. Semantic metadata identity stays distinct from the
// native owner/property identity, and the exact raw observed artifact remains
// separate evidence rather than a second normalized store.

func mustObservedMetadataValue(t *testing.T, raw string) observed.Value {
	t.Helper()
	value, err := observed.NewValue([]byte(raw))
	if err != nil {
		t.Fatalf("observed value %q: %v", raw, err)
	}
	return value
}

func TestEmitRunPackage_NormalizedMetadataPreservesSemanticAndNativeIdentity(t *testing.T) {
	material := newRuntimeMaterialWith(t, false, func(obs *observed.Observed) {
		obs.Observation.Metadata = append(obs.Observation.Metadata,
			observed.Metadata{
				ID:        "meta.bracket.material",
				Key:       "material",
				OwnerID:   "Body01",
				ValueKind: "string",
				Value:     mustObservedMetadataValue(t, `"S235JR"`),
			},
			observed.Metadata{
				ID:        "meta.feature.revision",
				Key:       "revision",
				OwnerID:   "MountingBracket01",
				ValueKind: "integer",
				Value:     mustObservedMetadataValue(t, `7`),
			},
		)
	}, verification.StatusPass)

	planHash := "normalized-metadata-plan"
	_, files := emitAndReadPackage(t, recordemit.RunEmitInput{
		PlanHash:   planHash,
		Report:     syntheticSuccessReport(planHash),
		Metadata:   syntheticMetadata(planHash),
		CADRuntime: material.evidence(),
	})

	record := decodeObservation(t, files)
	if record.Version != recordcontract.CurrentVersion {
		t.Fatalf("observation record version: got %q want %q", record.Version, recordcontract.CurrentVersion)
	}
	if record.Family != recordcontract.FamilyObservation {
		t.Fatalf("observation record family: got %q", record.Family)
	}
	byID := map[string]recordcontract.ObservationFact{}
	for _, fact := range record.Observation.Facts {
		if fact.Kind == recordcontract.ObservationKindMetadata {
			byID[fact.Subject.ID] = fact
		}
	}

	materialFact, ok := byID["meta.bracket.material"]
	if !ok {
		t.Fatalf("material metadata fact missing: %#v", record.Observation.Facts)
	}
	if materialFact.Subject.OwnerID != "Body01" {
		t.Fatalf("native owner identity lost: %#v", materialFact)
	}
	if materialFact.Subject.Name != "material" || materialFact.Key != "material" {
		t.Fatalf("native property identity lost: %#v", materialFact)
	}
	if materialFact.Subject.ID == materialFact.Subject.OwnerID {
		t.Fatalf("semantic metadata ID must remain distinct from native owner: %#v", materialFact)
	}
	if materialFact.Value.Kind != "string" || materialFact.Value.Raw != `"S235JR"` {
		t.Fatalf("typed metadata value lost: %#v", materialFact.Value)
	}

	revisionFact, ok := byID["meta.feature.revision"]
	if !ok {
		t.Fatalf("revision metadata fact missing: %#v", record.Observation.Facts)
	}
	if revisionFact.Subject.OwnerID != "MountingBracket01" || revisionFact.Subject.Name != "revision" || revisionFact.Key != "revision" {
		t.Fatalf("revision native identity lost: %#v", revisionFact)
	}
	if revisionFact.Value.Kind != "integer" || revisionFact.Value.Raw != "7" {
		t.Fatalf("revision typed value lost: %#v", revisionFact.Value)
	}

	// Every normalized metadata fact cites the exact raw observed evidence.
	wantDigest := sha256Hex(material.observedRaw)
	if materialFact.Evidence.SourceKind != "observed" || materialFact.Evidence.SourceRef != recordpackage.RawObservedContractPath() {
		t.Fatalf("metadata fact evidence reference: %#v", materialFact.Evidence)
	}
	if materialFact.Evidence.DigestSHA256 != wantDigest {
		t.Fatalf("metadata fact evidence digest: got %q want %q", materialFact.Evidence.DigestSHA256, wantDigest)
	}

	// Raw observed evidence remains byte-exact and separate from the normalized
	// record; the normalized record is not a second metadata store.
	raw, ok := files[recordpackage.RawObservedContractPath()]
	if !ok {
		t.Fatal("raw observed evidence was not emitted")
	}
	if !bytes.Equal(raw, material.observedRaw) {
		t.Fatalf("raw observed evidence changed:\nwant: %s\ngot:  %s", material.observedRaw, raw)
	}
	observationJSON, ok := files[recordpackage.MustRecordContractPath("observation")]
	if !ok {
		t.Fatal("observation record was not emitted")
	}
	for _, forbidden := range []string{`"prm.observed.json"`, `"rawObserved"`, `"bridgeOutput"`} {
		if bytes.Contains(observationJSON, []byte(forbidden)) {
			t.Fatalf("normalized observation record embedded raw observed payload field %q: %s", forbidden, observationJSON)
		}
	}
}
