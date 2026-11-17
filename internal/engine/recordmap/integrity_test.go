package recordmap_test

import (
	"errors"
	"reflect"
	"testing"

	"parametron/internal/engine/integrity"
	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordmap"
)

const (
	mappedSourceDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	mappedResultDigest = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func mappedRunIntegrity() *integrity.RunIntegrity {
	return &integrity.RunIntegrity{
		Lock: integrity.LockStateVerified,
		Resources: []integrity.ResourceIntegrity{
			{
				LogicalID:                  "zeta",
				SourceCoordinate:           "models/zeta.FCStd",
				MaterializedCoordinate:     "resources/zeta/Zeta.FCStd",
				SourceDigest:               integrity.Digest(mappedSourceDigest),
				LockState:                  integrity.LockStateVerified,
				MaterializedBaselineDigest: integrity.Digest(mappedSourceDigest),
				MaterializationStatus:      integrity.StatusVerified,
				RuntimeBaselineDigest:      integrity.Digest(mappedSourceDigest),
				RuntimeBaselineStatus:      integrity.StatusVerified,
				ResultStatus:               integrity.StatusNotReported,
			},
			{
				LogicalID:                  "alpha",
				SourceCoordinate:           "models/alpha.FCStd",
				MaterializedCoordinate:     "source/Alpha.FCStd",
				PersistedResultCoordinate:  "source/Alpha.FCStd",
				SourceDigest:               integrity.Digest(mappedSourceDigest),
				LockState:                  integrity.LockStateVerified,
				MaterializedBaselineDigest: integrity.Digest(mappedSourceDigest),
				MaterializationStatus:      integrity.StatusVerified,
				RuntimeBaselineDigest:      integrity.Digest(mappedSourceDigest),
				RuntimeBaselineStatus:      integrity.StatusVerified,
				PersistedResultDigest:      integrity.Digest(mappedResultDigest),
				ResultStatus:               integrity.StatusVerified,
			},
		},
	}
}

func TestMapIntegrityProvenanceSeparatesAndOrdersIdentities(t *testing.T) {
	mapped, err := recordmap.MapIntegrityProvenance(mappedRunIntegrity())
	if err != nil {
		t.Fatalf("MapIntegrityProvenance returned error: %v", err)
	}
	if mapped == nil || mapped.Lock != recordcontract.LockVerificationVerified {
		t.Fatalf("mapped provenance = %#v, want verified lock state", mapped)
	}
	if len(mapped.Resources) != 2 || mapped.Resources[0].LogicalID != "alpha" || mapped.Resources[1].LogicalID != "zeta" {
		t.Fatalf("mapped resources = %#v, want logical ID order", mapped.Resources)
	}
	alpha := mapped.Resources[0]
	if alpha.SourceDigestSHA256 != mappedSourceDigest {
		t.Fatalf("source digest = %q, want %q", alpha.SourceDigestSHA256, mappedSourceDigest)
	}
	if alpha.BaselineDigestSHA256 != mappedSourceDigest || alpha.RuntimeBaselineDigestSHA256 != mappedSourceDigest {
		t.Fatalf("baseline identities not preserved: %#v", alpha)
	}
	if alpha.PersistedResultDigestSHA256 != mappedResultDigest {
		t.Fatalf("configured-result digest = %q, want %q", alpha.PersistedResultDigestSHA256, mappedResultDigest)
	}
	if alpha.PersistedResultDigestSHA256 == alpha.SourceDigestSHA256 {
		t.Fatal("configured-result digest replaced the source identity")
	}
	if alpha.LockState != recordcontract.LockVerificationVerified ||
		alpha.MaterializationStatus != recordcontract.IntegrityStatusVerified ||
		alpha.RuntimeBaselineStatus != recordcontract.IntegrityStatusVerified ||
		alpha.ResultStatus != recordcontract.IntegrityStatusVerified {
		t.Fatalf("mapped statuses = %#v, want all verified", alpha)
	}
	zeta := mapped.Resources[1]
	if zeta.ResultStatus != recordcontract.IntegrityStatusNotReported || zeta.PersistedResultDigestSHA256 != "" {
		t.Fatalf("zeta result = %#v, want no persisted result", zeta)
	}
}

func TestMapIntegrityProvenanceIsDeterministic(t *testing.T) {
	first, err := recordmap.MapIntegrityProvenance(mappedRunIntegrity())
	if err != nil {
		t.Fatal(err)
	}
	shuffled := mappedRunIntegrity()
	shuffled.Resources[0], shuffled.Resources[1] = shuffled.Resources[1], shuffled.Resources[0]
	second, err := recordmap.MapIntegrityProvenance(shuffled)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("equivalent evidence in different order mapped differently:\nfirst:  %#v\nsecond: %#v", first, second)
	}
}

func TestMapIntegrityProvenanceUnlockedWorkflow(t *testing.T) {
	run := mappedRunIntegrity()
	run.Lock = integrity.LockStateNotPerformed
	for index := range run.Resources {
		run.Resources[index].LockState = integrity.LockStateNotPerformed
	}
	mapped, err := recordmap.MapIntegrityProvenance(run)
	if err != nil {
		t.Fatal(err)
	}
	if mapped.Lock != recordcontract.LockVerificationNotPerformed {
		t.Fatalf("mapped lock = %q, want not_performed", mapped.Lock)
	}
	for _, resource := range mapped.Resources {
		if resource.LockState != recordcontract.LockVerificationNotPerformed {
			t.Fatalf("resource %q lock state = %q, want not_performed", resource.LogicalID, resource.LockState)
		}
	}
}

func TestMapIntegrityProvenanceNilAndInvalid(t *testing.T) {
	mapped, err := recordmap.MapIntegrityProvenance(nil)
	if err != nil || mapped != nil {
		t.Fatalf("MapIntegrityProvenance(nil) = %#v, %v; want nil without error", mapped, err)
	}

	invalid := mappedRunIntegrity()
	invalid.Resources[0].LogicalID = ""
	_, err = recordmap.MapIntegrityProvenance(invalid)
	if !errors.Is(err, recordmap.ErrInvalidIntegrityMapping) {
		t.Fatalf("error = %v, want ErrInvalidIntegrityMapping", err)
	}
}
