package recordcontract

import (
	"reflect"
	"strings"
	"testing"
)

const (
	provenanceSourceDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	provenanceResultDigest = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	provenanceBaseline     = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func validIntegrityProvenance() *IntegrityProvenance {
	return &IntegrityProvenance{
		Lock: LockVerificationVerified,
		Resources: []ResourceProvenance{
			{
				LogicalID:                   "zeta",
				SourceCoordinate:            "models/zeta.FCStd",
				MaterializedCoordinate:      "resources/zeta/Zeta.FCStd",
				SourceDigestSHA256:          provenanceSourceDigest,
				LockState:                   LockVerificationVerified,
				BaselineDigestSHA256:        provenanceBaseline,
				MaterializationStatus:       IntegrityStatusVerified,
				RuntimeBaselineDigestSHA256: provenanceBaseline,
				RuntimeBaselineStatus:       IntegrityStatusVerified,
				ResultStatus:                IntegrityStatusNotReported,
			},
			{
				LogicalID:                   "alpha",
				SourceCoordinate:            "models/alpha.FCStd",
				MaterializedCoordinate:      "source/Alpha.FCStd",
				PersistedResultCoordinate:   "source/Alpha.FCStd",
				SourceDigestSHA256:          provenanceSourceDigest,
				LockState:                   LockVerificationVerified,
				BaselineDigestSHA256:        provenanceBaseline,
				MaterializationStatus:       IntegrityStatusVerified,
				RuntimeBaselineDigestSHA256: provenanceBaseline,
				RuntimeBaselineStatus:       IntegrityStatusVerified,
				PersistedResultDigestSHA256: provenanceResultDigest,
				ResultStatus:                IntegrityStatusVerified,
			},
		},
	}
}

func TestNormalizeProvenanceIntegrityOrdersAndSeparatesIdentities(t *testing.T) {
	normalized := NormalizeProvenance(Provenance{Integrity: validIntegrityProvenance()})
	if normalized.Integrity == nil {
		t.Fatal("NormalizeProvenance dropped integrity provenance")
	}
	if len(normalized.Integrity.Resources) != 2 {
		t.Fatalf("integrity resources = %d, want 2", len(normalized.Integrity.Resources))
	}
	if normalized.Integrity.Resources[0].LogicalID != "alpha" || normalized.Integrity.Resources[1].LogicalID != "zeta" {
		t.Fatalf("integrity resource order = %#v, want logical ID order", normalized.Integrity.Resources)
	}
	alpha := normalized.Integrity.Resources[0]
	if alpha.SourceDigestSHA256 != alpha.BaselineDigestSHA256 || alpha.BaselineDigestSHA256 != alpha.RuntimeBaselineDigestSHA256 {
		t.Fatalf("source/baseline identities are not separately preserved: %#v", alpha)
	}
	if alpha.PersistedResultDigestSHA256 != provenanceResultDigest || alpha.PersistedResultDigestSHA256 == alpha.SourceDigestSHA256 {
		t.Fatalf("configured-result digest not separately preserved: %#v", alpha)
	}

	if err := ValidateProvenance(normalized); err != nil {
		t.Fatalf("ValidateProvenance(normalized integrity) returned error: %v", err)
	}

	repeated := NormalizeProvenance(normalized)
	if !reflect.DeepEqual(repeated, normalized) {
		t.Fatalf("repeated normalization differs:\nfirst:  %#v\nsecond: %#v", normalized, repeated)
	}

	withoutIntegrity := NormalizeProvenance(Provenance{})
	if withoutIntegrity.Integrity != nil {
		t.Fatalf("empty provenance gained integrity material: %#v", withoutIntegrity.Integrity)
	}
	if err := ValidateProvenance(withoutIntegrity); err != nil {
		t.Fatalf("ValidateProvenance(without integrity) returned error: %v", err)
	}
}

func TestValidateProvenanceIntegrityRejectsInvalidStates(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*IntegrityProvenance)
		want   string
	}{
		{
			name:   "unknown lock state",
			mutate: func(p *IntegrityProvenance) { p.Lock = LockVerificationState("guessed") },
			want:   "not a known lock verification state",
		},
		{
			name:   "missing logical ID",
			mutate: func(p *IntegrityProvenance) { p.Resources[0].LogicalID = "" },
			want:   "logicalId is required",
		},
		{
			name: "duplicate logical ID",
			mutate: func(p *IntegrityProvenance) {
				p.Resources[1].LogicalID = p.Resources[0].LogicalID
			},
			want: "duplicates another resource identity",
		},
		{
			name:   "missing source coordinate",
			mutate: func(p *IntegrityProvenance) { p.Resources[0].SourceCoordinate = "" },
			want:   "sourceCoordinate is required",
		},
		{
			name:   "unknown lock state per resource",
			mutate: func(p *IntegrityProvenance) { p.Resources[0].LockState = LockVerificationState("guessed") },
			want:   "not a known lock verification state",
		},
		{
			name:   "unknown materialization status",
			mutate: func(p *IntegrityProvenance) { p.Resources[0].MaterializationStatus = IntegrityStatus("guessed") },
			want:   "not a known integrity status",
		},
		{
			name:   "unknown runtime baseline status",
			mutate: func(p *IntegrityProvenance) { p.Resources[0].RuntimeBaselineStatus = IntegrityStatus("guessed") },
			want:   "not a known integrity status",
		},
		{
			name:   "unknown result status",
			mutate: func(p *IntegrityProvenance) { p.Resources[0].ResultStatus = IntegrityStatus("guessed") },
			want:   "not a known integrity status",
		},
		{
			name:   "verified materialization without coordinate",
			mutate: func(p *IntegrityProvenance) { p.Resources[0].MaterializedCoordinate = "" },
			want:   "materializedCoordinate is required",
		},
		{
			name:   "verified result without coordinate",
			mutate: func(p *IntegrityProvenance) { p.Resources[1].PersistedResultCoordinate = "" },
			want:   "persistedResultCoordinate is required",
		},
		{
			name: "non-canonical source coordinate",
			mutate: func(p *IntegrityProvenance) {
				p.Resources[0].SourceCoordinate = "../models/zeta.FCStd"
			},
			want: "must be a canonical relative coordinate",
		},
		{
			name: "non-canonical source digest",
			mutate: func(p *IntegrityProvenance) {
				p.Resources[0].SourceDigestSHA256 = strings.ToUpper(provenanceSourceDigest)
			},
			want: "sourceDigestSha256 must be a 64-character lowercase hexadecimal digest",
		},
		{
			name:   "non-canonical baseline digest",
			mutate: func(p *IntegrityProvenance) { p.Resources[0].BaselineDigestSHA256 = "short" },
			want:   "baselineDigestSha256 must be a 64-character lowercase hexadecimal digest",
		},
		{
			name:   "non-canonical runtime baseline digest",
			mutate: func(p *IntegrityProvenance) { p.Resources[0].RuntimeBaselineDigestSHA256 = "short" },
			want:   "runtimeBaselineDigestSha256 must be a 64-character lowercase hexadecimal digest",
		},
		{
			name:   "non-canonical result digest",
			mutate: func(p *IntegrityProvenance) { p.Resources[1].PersistedResultDigestSHA256 = "short" },
			want:   "persistedResultDigestSha256 must be a 64-character lowercase hexadecimal digest",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			provenance := validIntegrityProvenance()
			tc.mutate(provenance)
			err := ValidateProvenance(Provenance{Integrity: provenance})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateProvenance = %v, want error containing %q", err, tc.want)
			}
		})
	}
}

func TestDeriveIdentityTreatsIntegrityAsSeparateProvenanceMaterial(t *testing.T) {
	base := IdentityInput{
		Family:     FamilyExecution,
		Version:    "1.0",
		RecordKey:  "execution:run-1",
		Provenance: Provenance{Integrity: validIntegrityProvenance()},
	}
	first, err := DeriveIdentity(base)
	if err != nil {
		t.Fatalf("DeriveIdentity returned error: %v", err)
	}
	second, err := DeriveIdentity(base)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("equivalent integrity provenance derived different identities: %#v / %#v", first, second)
	}

	resultChanged := base
	integrity := validIntegrityProvenance()
	integrity.Resources[1].PersistedResultDigestSHA256 = strings.Repeat("c", 64)
	resultChanged.Provenance = Provenance{Integrity: integrity}
	changed, err := DeriveIdentity(resultChanged)
	if err != nil {
		t.Fatal(err)
	}
	if changed.ID == first.ID {
		t.Fatal("changing only the configured-result digest did not change provenance identity")
	}

	withoutIntegrity := base
	withoutIntegrity.Provenance = Provenance{}
	plain, err := DeriveIdentity(withoutIntegrity)
	if err != nil {
		t.Fatal(err)
	}
	if plain.ID == first.ID {
		t.Fatal("integrity provenance did not participate in record identity")
	}
}
