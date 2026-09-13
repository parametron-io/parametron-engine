package recordcontract

import (
	"errors"
	"reflect"
	"testing"
)

func TestDeriveIdentityDeterministicForEquivalentNormalizedInput(t *testing.T) {
	input := validIdentityInput()

	first, err := DeriveIdentity(input)
	if err != nil {
		t.Fatalf("DeriveIdentity(valid) returned error: %v", err)
	}
	second, err := DeriveIdentity(input)
	if err != nil {
		t.Fatalf("DeriveIdentity(valid) second call returned error: %v", err)
	}
	if first != second {
		t.Fatalf("DeriveIdentity repeated calls = %#v then %#v, want same identity", first, second)
	}
	if first.Algorithm != IdentityAlgorithmSHA256CanonicalV1 {
		t.Fatalf("Algorithm = %q, want %q", first.Algorithm, IdentityAlgorithmSHA256CanonicalV1)
	}
	if first.Family != input.Family {
		t.Fatalf("Family = %q, want %q", first.Family, input.Family)
	}
	if first.Version != CurrentVersion {
		t.Fatalf("Version = %q, want %q", first.Version, CurrentVersion)
	}
	if !isLowerHex64(first.ID) {
		t.Fatalf("ID = %q, want lowercase 64-character SHA-256 hex", first.ID)
	}

	equivalent := IdentityInput{
		Family:    Family(" \t" + string(input.Family) + "\n"),
		Version:   " " + CurrentVersion + "\t",
		RecordKey: "\n" + input.RecordKey + " ",
		Provenance: Provenance{
			SourceRevision: SourceRevisionProvenance{
				RevisionID:   " " + input.Provenance.SourceRevision.RevisionID + " ",
				AssetID:      "\t" + input.Provenance.SourceRevision.AssetID,
				DigestSHA256: input.Provenance.SourceRevision.DigestSHA256 + "\n",
			},
			Inputs: []ProvenanceInput{
				input.Provenance.Inputs[1],
				{
					Kind:         " " + input.Provenance.Inputs[0].Kind + " ",
					Identity:     "\t" + input.Provenance.Inputs[0].Identity,
					DigestSHA256: input.Provenance.Inputs[0].DigestSHA256 + "\n",
				},
			},
			Plan: PlanProvenance{
				PlanID:   " " + input.Provenance.Plan.PlanID,
				PlanHash: input.Provenance.Plan.PlanHash + "\n",
			},
			Linkage: LinkageProvenance{
				ProductKey: " " + input.Provenance.Linkage.ProductKey + " ",
				JobID:      "\t" + input.Provenance.Linkage.JobID,
				StepRef:    input.Provenance.Linkage.StepRef + "\n",
			},
			Evidence: []EvidenceReference{
				input.Provenance.Evidence[1],
				{
					Kind:         " " + input.Provenance.Evidence[0].Kind,
					Ref:          input.Provenance.Evidence[0].Ref + " ",
					DigestSHA256: input.Provenance.Evidence[0].DigestSHA256 + "\n",
				},
			},
			Runtime: RuntimeProvenance{
				ToolID:    " " + input.Provenance.Runtime.ToolID,
				RuntimeID: input.Provenance.Runtime.RuntimeID + "\t",
				Adapter:   "\n" + input.Provenance.Runtime.Adapter + " ",
			},
		},
		Parts: []IdentityPart{
			input.Parts[1],
			{Key: " " + input.Parts[0].Key + " ", Value: "\t" + input.Parts[0].Value},
		},
	}

	equivalentIdentity, err := DeriveIdentity(equivalent)
	if err != nil {
		t.Fatalf("DeriveIdentity(equivalent) returned error: %v", err)
	}
	if equivalentIdentity != first {
		t.Fatalf("DeriveIdentity(equivalent) = %#v, want %#v", equivalentIdentity, first)
	}

	nilRepeated := IdentityInput{
		Family:    FamilyExecution,
		Version:   CurrentVersion,
		RecordKey: "minimal-record",
	}
	emptyRepeated := IdentityInput{
		Family:    FamilyExecution,
		Version:   CurrentVersion,
		RecordKey: "minimal-record",
		Provenance: Provenance{
			Inputs:   []ProvenanceInput{},
			Evidence: []EvidenceReference{},
		},
		Parts: []IdentityPart{},
	}
	nilIdentity, err := DeriveIdentity(nilRepeated)
	if err != nil {
		t.Fatalf("DeriveIdentity(nil repeated collections) returned error: %v", err)
	}
	emptyIdentity, err := DeriveIdentity(emptyRepeated)
	if err != nil {
		t.Fatalf("DeriveIdentity(empty repeated collections) returned error: %v", err)
	}
	if nilIdentity != emptyIdentity {
		t.Fatalf("nil and empty repeated collections identities differ: %#v != %#v", nilIdentity, emptyIdentity)
	}
}

func TestDeriveIdentityChangesWhenSemanticMaterialChanges(t *testing.T) {
	base := validIdentityInput()
	baseIdentity := mustDeriveIdentity(t, base)

	tests := []struct {
		name   string
		mutate func(*IdentityInput)
	}{
		{name: "family", mutate: func(input *IdentityInput) { input.Family = FamilyArtifact }},
		{name: "record key", mutate: func(input *IdentityInput) { input.RecordKey = "execution-record-2" }},
		{name: "source revision", mutate: func(input *IdentityInput) { input.Provenance.SourceRevision.RevisionID = "rev-2" }},
		{name: "input digest", mutate: func(input *IdentityInput) { input.Provenance.Inputs[0].DigestSHA256 = digestB() }},
		{name: "identity part value", mutate: func(input *IdentityInput) { input.Parts[0].Value = "plate" }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			changed := base
			changed.Provenance.Inputs = append([]ProvenanceInput(nil), base.Provenance.Inputs...)
			changed.Provenance.Evidence = append([]EvidenceReference(nil), base.Provenance.Evidence...)
			changed.Parts = append([]IdentityPart(nil), base.Parts...)
			tc.mutate(&changed)

			got := mustDeriveIdentity(t, changed)
			if got.ID == baseIdentity.ID {
				t.Fatalf("identity ID did not change after %s changed: %q", tc.name, got.ID)
			}
		})
	}
}

func TestDeriveIdentityValidationFailuresAreInspectable(t *testing.T) {
	tests := []struct {
		name        string
		input       IdentityInput
		wantErrors  []error
		allowFamily bool
	}{
		{
			name:       "empty family",
			input:      withIdentityInput(validIdentityInput(), func(input *IdentityInput) { input.Family = "" }),
			wantErrors: []error{ErrInvalidIdentity, ErrUnknownFamily},
		},
		{
			name:       "unknown family",
			input:      withIdentityInput(validIdentityInput(), func(input *IdentityInput) { input.Family = "unknown" }),
			wantErrors: []error{ErrInvalidIdentity, ErrUnknownFamily},
		},
		{
			name:       "unsupported version",
			input:      withIdentityInput(validIdentityInput(), func(input *IdentityInput) { input.Version = "1.1" }),
			wantErrors: []error{ErrInvalidIdentity, ErrInvalidVersion},
		},
		{
			name:       "malformed version",
			input:      withIdentityInput(validIdentityInput(), func(input *IdentityInput) { input.Version = "v1.0" }),
			wantErrors: []error{ErrInvalidIdentity, ErrInvalidVersion},
		},
		{
			name:       "empty record key",
			input:      withIdentityInput(validIdentityInput(), func(input *IdentityInput) { input.RecordKey = " \t\n" }),
			wantErrors: []error{ErrInvalidIdentity},
		},
		{
			name:       "empty identity part key",
			input:      withIdentityInput(validIdentityInput(), func(input *IdentityInput) { input.Parts[0].Key = " " }),
			wantErrors: []error{ErrInvalidIdentity},
		},
		{
			name:       "empty identity part value",
			input:      withIdentityInput(validIdentityInput(), func(input *IdentityInput) { input.Parts[0].Value = "\n" }),
			wantErrors: []error{ErrInvalidIdentity},
		},
		{
			name: "invalid provenance",
			input: withIdentityInput(validIdentityInput(), func(input *IdentityInput) {
				input.Provenance.Inputs[0].DigestSHA256 = "ABC"
			}),
			wantErrors: []error{ErrInvalidIdentity, ErrInvalidProvenance},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DeriveIdentity(tc.input)
			if err == nil {
				t.Fatal("DeriveIdentity returned nil error, want validation failure")
			}
			for _, want := range tc.wantErrors {
				if !errors.Is(err, want) {
					t.Fatalf("DeriveIdentity error = %v, want errors.Is(..., %v)", err, want)
				}
			}
		})
	}
}

func TestIdentityPreservesOwnershipVersioningFoundation(t *testing.T) {
	got := Definitions()
	if !reflect.DeepEqual(got, expectedDefinitions) {
		t.Fatalf("Definitions() = %#v, want %#v", got, expectedDefinitions)
	}
	if len(got) != 6 {
		t.Fatalf("Definitions() length = %d, want 6", len(got))
	}
	for _, def := range got {
		if _, ok := Lookup(def.Family); !ok {
			t.Fatalf("Lookup(%q) ok = false, want true", def.Family)
		}
		if err := ValidateVersion(CurrentVersion); err != nil {
			t.Fatalf("ValidateVersion(CurrentVersion) returned error: %v", err)
		}
		if err := ValidateOwnership(def.Ownership); err != nil {
			t.Fatalf("ValidateOwnership(%q default) returned error: %v", def.Family, err)
		}
	}
}

func validIdentityInput() IdentityInput {
	return IdentityInput{
		Family:    FamilyExecution,
		Version:   CurrentVersion,
		RecordKey: "execution-record-1",
		Provenance: Provenance{
			SourceRevision: SourceRevisionProvenance{
				RevisionID:   "rev-1",
				AssetID:      "asset-1",
				DigestSHA256: digestA(),
			},
			Inputs: []ProvenanceInput{
				{Kind: "parameter-table", Identity: "params-a", DigestSHA256: digestC()},
				{Kind: "dsl", Identity: "model-a", DigestSHA256: digestD()},
			},
			Plan: PlanProvenance{
				PlanID:   "plan-1",
				PlanHash: "plan-hash-1",
			},
			Linkage: LinkageProvenance{
				ProductKey: "product-1",
				JobID:      "job-1",
				StepRef:    "step-1",
			},
			Evidence: []EvidenceReference{
				{Kind: "log", Ref: "logs/run.log", DigestSHA256: digestE()},
				{Kind: "artifact", Ref: "out/model.step", DigestSHA256: digestF()},
			},
			Runtime: RuntimeProvenance{
				ToolID:    "freecad",
				RuntimeID: "freecad-1.1.1",
				Adapter:   "parametron-freecad",
			},
		},
		Parts: []IdentityPart{
			{Key: "shape", Value: "bracket"},
			{Key: "units", Value: "mm"},
		},
	}
}

func mustDeriveIdentity(t *testing.T, input IdentityInput) Identity {
	t.Helper()
	identity, err := DeriveIdentity(input)
	if err != nil {
		t.Fatalf("DeriveIdentity(%#v) returned error: %v", input, err)
	}
	return identity
}

func withIdentityInput(base IdentityInput, mutate func(*IdentityInput)) IdentityInput {
	base.Provenance.Inputs = append([]ProvenanceInput(nil), base.Provenance.Inputs...)
	base.Provenance.Evidence = append([]EvidenceReference(nil), base.Provenance.Evidence...)
	base.Parts = append([]IdentityPart(nil), base.Parts...)
	mutate(&base)
	return base
}
