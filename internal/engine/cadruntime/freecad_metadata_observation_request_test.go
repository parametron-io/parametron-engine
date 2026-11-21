package cadruntime

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/verification"
)

// Issue #73 permanent contract: prm.verification.json requests post-execution
// observation for authored metadata mutations and keeps the working-copy
// expectation compatible. The derived contract uses the same native
// owner/property identity as the runtime manifest metadata mutation while
// retaining the semantic metadata ID.

func TestComposeFreeCADRuntimeObservationRequest_AuthoredMetadata(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	req.Manifest.PartMutations = &planner.ExportManifestMutationCollection{
		Metadata: []planner.ExportManifestMetadataMutation{
			{Object: "Body01", Property: "material", Value: "S235JR", ValueKind: "string"},
			{Object: "MountingBracket01", Property: "revision", Value: float64(7), ValueKind: "integer"},
		},
	}
	req.Manifest.Verification.ExpectedMetadata = []planner.VerificationExpectedMetadata{
		{ID: "meta.bracket.material", Key: "material", OwnerID: "Body01", Value: "S235JR", ValueKind: "string"},
		{ID: "meta.feature.revision", Key: "revision", OwnerID: "MountingBracket01", Value: float64(7), ValueKind: "integer"},
	}
	prepareObservationSource(t, req, []byte("prepared source bytes"))

	got, err := ComposeFreeCADRuntimeObservationRequest(req)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if !got.Contract.Observe.Metadata || !got.Contract.Checks.Metadata.Enabled {
		t.Fatalf("metadata observation disabled: %#v %#v", got.Contract.Observe, got.Contract.Checks)
	}
	if len(got.Contract.Expected.Metadata) != 3 {
		t.Fatalf("expected working-copy plus two authored expectations, got %#v", got.Contract.Expected.Metadata)
	}
	if got.Contract.Expected.Metadata[0].ID != "" || got.Contract.Expected.Metadata[0].Key != "working_copy_sha256" {
		t.Fatalf("working-copy expectation must remain first: %#v", got.Contract.Expected.Metadata[0])
	}

	byID := map[string]verification.ExpectedMetadata{}
	for _, entry := range got.Contract.Expected.Metadata[1:] {
		byID[entry.ID] = entry
	}
	material, ok := byID["meta.bracket.material"]
	if !ok {
		t.Fatalf("material expectation missing: %#v", got.Contract.Expected.Metadata)
	}
	if material.Key != "material" || material.OwnerID != "Body01" || string(material.Value) != `"S235JR"` || material.ValueKind != "string" {
		t.Fatalf("material expectation: %#v", material)
	}
	revision, ok := byID["meta.feature.revision"]
	if !ok {
		t.Fatalf("revision expectation missing: %#v", got.Contract.Expected.Metadata)
	}
	if revision.Key != "revision" || revision.OwnerID != "MountingBracket01" || string(revision.Value) != "7" || revision.ValueKind != "integer" {
		t.Fatalf("revision expectation: %#v", revision)
	}
	if !strings.Contains(string(got.JSON), `"id":"meta.bracket.material"`) || !strings.Contains(string(got.JSON), `"value":7`) {
		t.Fatalf("serialized verification request missing authored metadata: %s", got.JSON)
	}
}

func TestComposeFreeCADRuntimeObservationRequest_RejectsInvalidMetadataIntent(t *testing.T) {
	cases := []struct {
		name      string
		entries   []planner.VerificationExpectedMetadata
		wantField string
		wantErr   string
	}{
		{
			name:      "empty semantic id",
			entries:   []planner.VerificationExpectedMetadata{{ID: "", Key: "material", OwnerID: "Body01", Value: "S235JR", ValueKind: "string"}},
			wantField: "Manifest.Verification.ExpectedMetadata[0].ID",
			wantErr:   "must be non-empty",
		},
		{
			name:      "empty native key",
			entries:   []planner.VerificationExpectedMetadata{{ID: "meta.a", Key: "", OwnerID: "Body01", Value: "S235JR", ValueKind: "string"}},
			wantField: "Manifest.Verification.ExpectedMetadata[0].Key",
			wantErr:   "must be non-empty",
		},
		{
			name:      "empty native owner",
			entries:   []planner.VerificationExpectedMetadata{{ID: "meta.a", Key: "material", OwnerID: "", Value: "S235JR", ValueKind: "string"}},
			wantField: "Manifest.Verification.ExpectedMetadata[0].OwnerID",
			wantErr:   "must be non-empty",
		},
		{
			name:      "unsupported value kind",
			entries:   []planner.VerificationExpectedMetadata{{ID: "meta.a", Key: "material", OwnerID: "Body01", Value: "S235JR", ValueKind: "date"}},
			wantField: "Manifest.Verification.ExpectedMetadata[0].ValueKind",
			wantErr:   `must be one of "number", "integer", "string", or "boolean"`,
		},
		{
			name:      "value kind mismatch",
			entries:   []planner.VerificationExpectedMetadata{{ID: "meta.a", Key: "material", OwnerID: "Body01", Value: 42, ValueKind: "string"}},
			wantField: "Manifest.Verification.ExpectedMetadata[0].Value",
			wantErr:   `must be a string for valueKind "string"`,
		},
		{
			name: "duplicate semantic id",
			entries: []planner.VerificationExpectedMetadata{
				{ID: "meta.a", Key: "material", OwnerID: "Body01", Value: "X", ValueKind: "string"},
				{ID: "meta.a", Key: "other", OwnerID: "Body02", Value: "Y", ValueKind: "string"},
			},
			wantField: "Manifest.Verification.ExpectedMetadata[1].ID",
			wantErr:   "duplicates ExpectedMetadata[0].ID",
		},
		{
			name: "duplicate native owner and property",
			entries: []planner.VerificationExpectedMetadata{
				{ID: "meta.a", Key: "material", OwnerID: "Body01", Value: "X", ValueKind: "string"},
				{ID: "meta.b", Key: "material", OwnerID: "Body01", Value: "Y", ValueKind: "string"},
			},
			wantField: "Manifest.Verification.ExpectedMetadata[1]",
			wantErr:   `duplicates ExpectedMetadata[0] for native owner "Body01" and property "material"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validFreeCADObservationRequest(t)
			req.Manifest.Verification.ExpectedMetadata = tc.entries
			writeObservationHostSource(t, req, []byte("prepared source bytes"))

			_, err := ComposeFreeCADRuntimeObservationRequest(req)
			if err == nil {
				t.Fatal("expected metadata intent validation to fail")
			}
			got := requireObservationError(t, err, FreeCADRuntimeObservationRequestStageIntentValidation)
			if got.Field != tc.wantField {
				t.Fatalf("error field: got %q want %q", got.Field, tc.wantField)
			}
			if !strings.Contains(got.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, got)
			}
		})
	}
}

func TestWriteFreeCADRuntimeObservationRequest_AuthoredMetadataContract(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	req.Manifest.PartMutations = &planner.ExportManifestMutationCollection{
		Metadata: []planner.ExportManifestMetadataMutation{
			{Object: "Body01", Property: "material", Value: "S235JR", ValueKind: "string"},
		},
	}
	req.Manifest.Verification.ExpectedMetadata = []planner.VerificationExpectedMetadata{
		{ID: "meta.bracket.material", Key: "material", OwnerID: "Body01", Value: "S235JR", ValueKind: "string"},
	}
	prepareObservationSource(t, req, []byte("prepared source bytes"))

	got, err := WriteFreeCADRuntimeObservationRequest(req)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	data, err := os.ReadFile(got.Path)
	if err != nil {
		t.Fatalf("read verification request: %v", err)
	}
	parsed, err := verification.Parse(data)
	if err != nil {
		t.Fatalf("parse verification request: %v", err)
	}
	if err := verification.Validate(parsed); err != nil {
		t.Fatalf("validate verification request: %v", err)
	}

	var workingCopy *verification.ExpectedMetadata
	var authored []verification.ExpectedMetadata
	for index := range parsed.Expected.Metadata {
		entry := parsed.Expected.Metadata[index]
		if entry.ID == "" {
			workingCopy = &entry
			continue
		}
		authored = append(authored, entry)
	}
	if workingCopy == nil || workingCopy.Key != "working_copy_sha256" {
		t.Fatalf("working-copy expectation missing: %#v", parsed.Expected.Metadata)
	}
	if len(authored) != 1 {
		t.Fatalf("expected one authored metadata expectation, got %#v", authored)
	}
	if authored[0].ID != "meta.bracket.material" || authored[0].OwnerID != "Body01" || string(authored[0].Value) != `"S235JR"` {
		t.Fatalf("authored metadata expectation: %#v", authored[0])
	}
}

func TestValidateFreeCADVerificationContract_MetadataExpectationBoundary(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	req.Manifest.Verification.ExpectedMetadata = []planner.VerificationExpectedMetadata{
		{ID: "meta.bracket.material", Key: "material", OwnerID: "Body01", Value: "S235JR", ValueKind: "string"},
	}
	prepareObservationSource(t, req, []byte("prepared source bytes"))
	got, err := ComposeFreeCADRuntimeObservationRequest(req)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}

	base := got.Contract
	if field, err := validateFreeCADVerificationContract(&base); err != nil {
		t.Fatalf("expected authored metadata contract to pass: %s %v", field, err)
	}

	t.Run("missing working-copy expectation", func(t *testing.T) {
		contract := base
		contract.Expected.Metadata = []verification.ExpectedMetadata{contract.Expected.Metadata[1]}
		field, err := validateFreeCADVerificationContract(&contract)
		if err == nil || field != "metadata" {
			t.Fatalf("expected metadata field failure, got field=%q err=%v", field, err)
		}
	})

	t.Run("duplicate working-copy expectation", func(t *testing.T) {
		contract := base
		contract.Expected.Metadata = append(append([]verification.ExpectedMetadata(nil), base.Expected.Metadata...), base.Expected.Metadata[0])
		_, err := validateFreeCADVerificationContract(&contract)
		if err == nil || !strings.Contains(err.Error(), "duplicates expected.metadata[0].key") {
			t.Fatalf("expected duplicate working-copy failure, got %v", err)
		}
	})

	t.Run("authored expectation without owner", func(t *testing.T) {
		contract := base
		contract.Expected.Metadata = append([]verification.ExpectedMetadata(nil), base.Expected.Metadata...)
		contract.Expected.Metadata[1].OwnerID = ""
		_, err := validateFreeCADVerificationContract(&contract)
		if err == nil || !strings.Contains(err.Error(), "ownerId must be an exact native owner object selector") {
			t.Fatalf("expected ownerId failure, got %v", err)
		}
	})

	t.Run("duplicate native expectation", func(t *testing.T) {
		contract := base
		duplicate := contract.Expected.Metadata[1]
		duplicate.ID = "meta.bracket.material.other"
		contract.Expected.Metadata = append(append([]verification.ExpectedMetadata(nil), base.Expected.Metadata...), duplicate)
		_, err := validateFreeCADVerificationContract(&contract)
		if err == nil || !strings.Contains(err.Error(), "duplicates expected.metadata[1] for native owner") {
			t.Fatalf("expected native duplicate failure, got %v", err)
		}
	})
}

func TestComposeFreeCADRuntimeObservationRequest_AuthoredMetadataValueKindsRoundTrip(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	req.Manifest.Verification.ExpectedMetadata = []planner.VerificationExpectedMetadata{
		{ID: "meta.material", Key: "material", OwnerID: "Body01", Value: "S235JR", ValueKind: "string"},
		{ID: "meta.tagged", Key: "tagged", OwnerID: "Body01", Value: true, ValueKind: "boolean"},
		{ID: "meta.revision", Key: "revision", OwnerID: "Body01", Value: float64(7), ValueKind: "integer"},
		{ID: "meta.mass", Key: "mass", OwnerID: "Body01", Value: 12.5, ValueKind: "number"},
	}
	prepareObservationSource(t, req, []byte("prepared source bytes"))

	got, err := ComposeFreeCADRuntimeObservationRequest(req)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	byID := map[string]verification.ExpectedMetadata{}
	for _, entry := range got.Contract.Expected.Metadata {
		if entry.ID != "" {
			byID[entry.ID] = entry
		}
	}
	for id, want := range map[string]string{
		"meta.material": `"S235JR"`,
		"meta.tagged":   "true",
		"meta.revision": "7",
		"meta.mass":     "12.5",
	} {
		if got := string(byID[id].Value); got != want {
			t.Fatalf("%s value: got %s want %s", id, got, want)
		}
	}
	var decoded map[string]any
	if err := json.Unmarshal(got.JSON, &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
}
