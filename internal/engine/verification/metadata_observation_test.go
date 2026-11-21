package verification

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/observed"
)

// Issue #73 permanent contract: authored metadata mutations produce expected
// metadata entries in prm.verification.json. Each entry carries the semantic
// metadata ID separately from the exact native owner/property identity, a typed
// value, and a value kind. The working-copy digest expectation remains present
// and compatible.

func authoredMetadataManifest(t *testing.T) (planner.WriteExportManifestPayload, string) {
	t.Helper()
	root := t.TempDir()
	workingCopy := filepath.Join(root, "attempt", "source", "Widget.FCStd")
	if err := os.MkdirAll(filepath.Dir(workingCopy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workingCopy, []byte("prepared bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	return planner.WriteExportManifestPayload{
		SchemaVersion: planner.ExportManifestSchemaVersion,
		Verification: planner.VerificationManifestIntent{
			ExpectedMetadata: []planner.VerificationExpectedMetadata{
				{ID: "meta.feature.tagged", Key: "tagged", OwnerID: "MountingBracket01", Value: true, ValueKind: "boolean"},
				{ID: "meta.root.mass", Key: "mass", OwnerID: "Assembly01", Value: 12.5, ValueKind: "number"},
				{ID: "meta.feature.revision", Key: "revision", OwnerID: "MountingBracket01", Value: float64(7), ValueKind: "integer"},
				{ID: "meta.bracket.material", Key: "material", OwnerID: "Body01", Value: "S235JR", ValueKind: "string"},
			},
		},
	}, workingCopy
}

func mustDeriveMetadataContract(t *testing.T, manifest planner.WriteExportManifestPayload, workingCopy string) *Contract {
	t.Helper()
	contract, err := DeriveFromManifestAndWorkingCopy(manifest, workingCopy)
	if err != nil {
		t.Fatalf("DeriveFromManifestAndWorkingCopy: %v", err)
	}
	return contract
}

func matchingMetadataObserved(t *testing.T, contract *Contract) *observed.Observed {
	t.Helper()
	var workingCopy ExpectedMetadata
	entries := make([]observed.Metadata, 0, len(contract.Expected.Metadata))
	for _, entry := range contract.Expected.Metadata {
		if entry.ID == "" {
			workingCopy = entry
			entries = append(entries, observed.Metadata{
				ID:        "working-copy:sha256",
				Key:       entry.Key,
				Value:     mustObservedRaw(t, entry.Value),
				ValueKind: "string",
			})
			continue
		}
		entries = append(entries, observed.Metadata{
			ID:        entry.ID,
			Key:       entry.Key,
			OwnerID:   entry.OwnerID,
			Value:     mustObservedRaw(t, entry.Value),
			ValueKind: entry.ValueKind,
		})
	}
	digest, err := workingCopy.StringValue()
	if err != nil {
		t.Fatalf("working-copy digest: %v", err)
	}
	return &observed.Observed{
		SchemaVersion: observed.SchemaVersion,
		WorkingCopy: observed.WorkingCopy{
			Path:   contract.Expected.References[0].Name,
			SHA256: digest,
		},
		Observation: observed.Observation{
			Parameters: []observed.Parameter{},
			Metadata:   entries,
			References: []observed.Reference{{
				Kind: contract.Expected.References[0].Kind,
				Name: contract.Expected.References[0].Name,
			}},
			Components: []observed.Component{},
		},
	}
}

func mustObservedRaw(t *testing.T, raw json.RawMessage) observed.Value {
	t.Helper()
	value, err := observed.NewValue(raw)
	if err != nil {
		t.Fatalf("observed value: %v", err)
	}
	return value
}

func findMetadataExpectation(t *testing.T, contract *Contract, id string) ExpectedMetadata {
	t.Helper()
	for _, entry := range contract.Expected.Metadata {
		if entry.ID == id {
			return entry
		}
	}
	t.Fatalf("metadata expectation %q not found", id)
	return ExpectedMetadata{}
}

func findObservedMetadata(t *testing.T, obs *observed.Observed, id string) *observed.Metadata {
	t.Helper()
	for index := range obs.Observation.Metadata {
		if obs.Observation.Metadata[index].ID == id {
			return &obs.Observation.Metadata[index]
		}
	}
	t.Fatalf("observed metadata %q not found", id)
	return nil
}

func TestDeriveFromManifestAndWorkingCopy_AuthoredMetadataExpectations(t *testing.T) {
	manifest, workingCopy := authoredMetadataManifest(t)
	contract := mustDeriveMetadataContract(t, manifest, workingCopy)

	if err := Validate(contract); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(contract.Expected.Metadata) != 5 {
		t.Fatalf("expected working-copy plus four authored entries, got %#v", contract.Expected.Metadata)
	}
	if contract.Expected.Metadata[0].ID != "" || contract.Expected.Metadata[0].Key != "working_copy_sha256" {
		t.Fatalf("working-copy expectation must remain first: %#v", contract.Expected.Metadata[0])
	}

	wantIDs := []string{"", "meta.bracket.material", "meta.feature.revision", "meta.feature.tagged", "meta.root.mass"}
	for index, want := range wantIDs {
		if contract.Expected.Metadata[index].ID != want {
			t.Fatalf("metadata order[%d]: got %q want %q", index, contract.Expected.Metadata[index].ID, want)
		}
	}

	material := findMetadataExpectation(t, contract, "meta.bracket.material")
	if material.Key != "material" || material.OwnerID != "Body01" || material.ValueKind != "string" {
		t.Fatalf("material expectation: %#v", material)
	}
	if string(material.Value) != `"S235JR"` {
		t.Fatalf("material value: %s", material.Value)
	}
	revision := findMetadataExpectation(t, contract, "meta.feature.revision")
	if string(revision.Value) != "7" || revision.ValueKind != "integer" {
		t.Fatalf("revision expectation: %#v", revision)
	}
	tagged := findMetadataExpectation(t, contract, "meta.feature.tagged")
	if string(tagged.Value) != "true" || tagged.ValueKind != "boolean" {
		t.Fatalf("tagged expectation: %#v", tagged)
	}
	mass := findMetadataExpectation(t, contract, "meta.root.mass")
	if string(mass.Value) != "12.5" || mass.ValueKind != "number" {
		t.Fatalf("mass expectation: %#v", mass)
	}

	first, err := CanonicalJSON(contract)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CanonicalJSON(contract)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("canonical JSON changed across calls:\nfirst:  %s\nsecond: %s", first, second)
	}
	for _, fragment := range []string{
		`"id":"meta.bracket.material"`,
		`"key":"material"`,
		`"ownerId":"Body01"`,
		`"value":"S235JR"`,
		`"valueKind":"string"`,
		`"value":7`,
		`"valueKind":"integer"`,
		`"value":true`,
		`"valueKind":"boolean"`,
		`"value":12.5`,
		`"valueKind":"number"`,
		`"key":"working_copy_sha256"`,
	} {
		if !strings.Contains(string(first), fragment) {
			t.Fatalf("canonical JSON missing %q: %s", fragment, first)
		}
	}
}

func TestVerify_AuthoredMetadataPass(t *testing.T) {
	manifest, workingCopy := authoredMetadataManifest(t)
	contract := mustDeriveMetadataContract(t, manifest, workingCopy)

	result, err := Verify(contract, matchingMetadataObserved(t, contract))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if result.Status != StatusPass {
		t.Fatalf("expected pass, got %#v", result)
	}
	if result.Categories.Metadata.Status != CategoryStatusPass {
		t.Fatalf("metadata category: %#v", result.Categories.Metadata)
	}
}

func TestVerify_AuthoredMetadataDeterministicFailures(t *testing.T) {
	cases := []struct {
		name    string
		tweak   func(t *testing.T, contract *Contract, obs *observed.Observed)
		want    FailureClass
		message string
	}{
		{
			name: "missing observation",
			tweak: func(t *testing.T, _ *Contract, obs *observed.Observed) {
				filtered := obs.Observation.Metadata[:0]
				for _, entry := range obs.Observation.Metadata {
					if entry.ID != "meta.feature.revision" {
						filtered = append(filtered, entry)
					}
				}
				obs.Observation.Metadata = filtered
			},
			want:    FailureClassRequiredObservationMissing,
			message: `required observed metadata "meta.feature.revision" is missing`,
		},
		{
			name: "value mismatch",
			tweak: func(t *testing.T, _ *Contract, obs *observed.Observed) {
				entry := findObservedMetadata(t, obs, "meta.feature.revision")
				entry.Value = mustObservedRaw(t, json.RawMessage("8"))
			},
			want:    FailureClassMetadataMismatch,
			message: `observed metadata "meta.feature.revision" mismatch: want 7, got 8`,
		},
		{
			name: "value-kind mismatch",
			tweak: func(t *testing.T, _ *Contract, obs *observed.Observed) {
				entry := findObservedMetadata(t, obs, "meta.feature.revision")
				entry.ValueKind = "string"
				entry.Value = mustObservedRaw(t, json.RawMessage(`"7"`))
			},
			want:    FailureClassMetadataMismatch,
			message: `observed metadata "meta.feature.revision" valueKind mismatch: want "integer", got "string"`,
		},
		{
			name: "observed value inconsistent with its kind",
			tweak: func(t *testing.T, _ *Contract, obs *observed.Observed) {
				entry := findObservedMetadata(t, obs, "meta.feature.revision")
				entry.Value = mustObservedRaw(t, json.RawMessage(`"7"`))
			},
			want:    FailureClassMetadataMismatch,
			message: `observed metadata "meta.feature.revision" value must be a number for valueKind "integer"`,
		},
		{
			name: "owner mismatch",
			tweak: func(t *testing.T, _ *Contract, obs *observed.Observed) {
				findObservedMetadata(t, obs, "meta.bracket.material").OwnerID = "Body99"
			},
			want:    FailureClassMetadataMismatch,
			message: `observed metadata "meta.bracket.material" ownerId mismatch: want "Body01", got "Body99"`,
		},
		{
			name: "key mismatch",
			tweak: func(t *testing.T, _ *Contract, obs *observed.Observed) {
				findObservedMetadata(t, obs, "meta.root.mass").Key = "weight"
			},
			want:    FailureClassMetadataMismatch,
			message: `observed metadata "meta.root.mass" key mismatch: want "mass", got "weight"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manifest, workingCopy := authoredMetadataManifest(t)
			contract := mustDeriveMetadataContract(t, manifest, workingCopy)

			firstObs := matchingMetadataObserved(t, contract)
			tc.tweak(t, contract, firstObs)
			firstResult, firstErr := Verify(contract, firstObs)
			if firstErr == nil {
				t.Fatal("expected Verify to fail")
			}
			if firstResult.Failure != tc.want {
				t.Fatalf("failure class: got %q want %q (%v)", firstResult.Failure, tc.want, firstErr)
			}
			if !strings.Contains(firstResult.Message, tc.message) {
				t.Fatalf("failure message: got %q want containing %q", firstResult.Message, tc.message)
			}

			secondObs := matchingMetadataObserved(t, contract)
			tc.tweak(t, contract, secondObs)
			secondResult, secondErr := Verify(contract, secondObs)
			if secondErr == nil || secondResult.Failure != firstResult.Failure || secondResult.Message != firstResult.Message {
				t.Fatalf("failure is not deterministic:\nfirst:  %#v %v\nsecond: %#v %v", firstResult, firstErr, secondResult, secondErr)
			}
		})
	}
}

func TestDeriveFromManifestAndWorkingCopy_RejectsMetadataConflicts(t *testing.T) {
	baseEntry := planner.VerificationExpectedMetadata{ID: "meta.a", Key: "material", OwnerID: "Body01", Value: "S235JR", ValueKind: "string"}

	cases := []struct {
		name    string
		entries []planner.VerificationExpectedMetadata
		wantErr string
	}{
		{
			name: "duplicate semantic ID",
			entries: []planner.VerificationExpectedMetadata{
				baseEntry,
				{ID: "meta.a", Key: "other", OwnerID: "Body02", Value: "X", ValueKind: "string"},
			},
			wantErr: "duplicates",
		},
		{
			name: "duplicate native owner and property",
			entries: []planner.VerificationExpectedMetadata{
				baseEntry,
				{ID: "meta.b", Key: "material", OwnerID: "Body01", Value: "X", ValueKind: "string"},
			},
			wantErr: "native owner \"Body01\" and property \"material\"",
		},
		{
			name:    "empty owner",
			entries: []planner.VerificationExpectedMetadata{{ID: "meta.a", Key: "material", OwnerID: "", Value: "S235JR", ValueKind: "string"}},
			wantErr: "ownerId must be a non-blank exact native identity",
		},
		{
			name:    "unsupported kind",
			entries: []planner.VerificationExpectedMetadata{{ID: "meta.a", Key: "material", OwnerID: "Body01", Value: "S235JR", ValueKind: "date"}},
			wantErr: "valueKind must be one of",
		},
		{
			name:    "value kind mismatch",
			entries: []planner.VerificationExpectedMetadata{{ID: "meta.a", Key: "material", OwnerID: "Body01", Value: 42, ValueKind: "string"}},
			wantErr: `must be a string for valueKind "string"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			workingCopy := filepath.Join(root, "source", "Widget.FCStd")
			if err := os.MkdirAll(filepath.Dir(workingCopy), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(workingCopy, []byte("prepared"), 0o644); err != nil {
				t.Fatal(err)
			}
			manifest := planner.WriteExportManifestPayload{
				SchemaVersion: planner.ExportManifestSchemaVersion,
				Verification:  planner.VerificationManifestIntent{ExpectedMetadata: tc.entries},
			}
			_, err := DeriveFromManifestAndWorkingCopy(manifest, workingCopy)
			if err == nil {
				t.Fatal("expected derivation to fail")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("expected ValidationError, got %T: %v", err, err)
			}
		})
	}
}

func TestValidate_RejectsConflictingNativeMetadataExpectations(t *testing.T) {
	contract := validContract()
	contract.Expected.Metadata = []ExpectedMetadata{
		{Key: "working_copy_sha256", Value: json.RawMessage(`"` + strings.Repeat("a", 64) + `"`)},
		{ID: "meta.a", Key: "material", OwnerID: "Body01", Value: json.RawMessage(`"S235JR"`), ValueKind: "string"},
		{ID: "meta.b", Key: "material", OwnerID: "Body01", Value: json.RawMessage(`"S355"`), ValueKind: "string"},
	}
	err := Validate(contract)
	if err == nil {
		t.Fatal("expected conflicting native metadata expectations to fail validation")
	}
	if !strings.Contains(err.Error(), `duplicates expected.metadata[1] for native owner "Body01" and property "material"`) {
		t.Fatalf("unexpected validation error: %v", err)
	}
}
