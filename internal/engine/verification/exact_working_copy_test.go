package verification

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"parametron/internal/authoring/planner"
)

func exactWorkingCopyManifest() planner.WriteExportManifestPayload {
	return planner.WriteExportManifestPayload{
		SchemaVersion: planner.ExportManifestSchemaVersion,
		ParameterAssignments: []planner.ExportManifestParameterAssignment{
			{Name: "width", Target: "Body.Width", Value: 10, Type: "number", Unit: "mm"},
		},
		Verification: planner.VerificationManifestIntent{
			ExpectedParameters: []planner.VerificationExpectedParameter{
				{ID: "width", Name: "Width", Value: 10, Type: "number", Unit: "mm"},
			},
			ObservationParameterLinks: []planner.VerificationObservationParameterLink{
				{ID: "width", Name: "Width", GroupName: "VarSet"},
			},
		},
	}
}

func writeExactWorkingCopy(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o640); err != nil {
		t.Fatal(err)
	}
}

func TestDeriveFromManifestAndWorkingCopy_UsesExactFile(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "legacy.FCStd")
	exact := filepath.Join(root, "attempt", "source", "Widget.FCStd")
	a, b := []byte("legacy bytes"), []byte("exact prepared bytes")
	writeExactWorkingCopy(t, legacy, a)
	writeExactWorkingCopy(t, exact, b)

	got, err := DeriveFromManifestAndWorkingCopy(exactWorkingCopyManifest(), exact)
	if err != nil {
		t.Fatal(err)
	}
	sumB, sumA := sha256.Sum256(b), sha256.Sum256(a)
	if got.Expected.Metadata[0].Key != "working_copy_sha256" || got.Expected.Metadata[0].Value != hex.EncodeToString(sumB[:]) {
		t.Fatalf("metadata=%#v", got.Expected.Metadata)
	}
	if got.Expected.Metadata[0].Value == hex.EncodeToString(sumA[:]) {
		t.Fatal("legacy file was hashed")
	}
	if got.Expected.References[0].Kind != "working_copy_path" || got.Expected.References[0].Name != exact {
		t.Fatalf("references=%#v", got.Expected.References)
	}
	if err := Validate(got); err != nil {
		t.Fatal(err)
	}
}

func TestDeriveFromManifestAndWorkingCopy_ValidatesPath(t *testing.T) {
	root := t.TempDir()
	clean := filepath.Join(root, "exact.FCStd")
	writeExactWorkingCopy(t, clean, []byte("must not be read through invalid spelling"))
	tests := []string{
		"", " ", "relative.FCStd", "./relative.FCStd",
		root + string(filepath.Separator) + "." + string(filepath.Separator) + "exact.FCStd",
		root + string(filepath.Separator) + "sub" + string(filepath.Separator) + ".." + string(filepath.Separator) + "exact.FCStd",
		" " + clean, clean + " ", clean + "\x00",
	}
	for _, path := range tests {
		t.Run(strings.ReplaceAll(path, string(filepath.Separator), "_"), func(t *testing.T) {
			_, err := DeriveFromManifestAndWorkingCopy(exactWorkingCopyManifest(), path)
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) || !errors.Is(err, ErrValidation) {
				t.Fatalf("error=%T %v", err, err)
			}
			var fileErr *FileError
			if errors.As(err, &fileErr) {
				t.Fatalf("filesystem reached: %v", err)
			}
		})
	}
}

func TestDeriveFromManifestAndWorkingCopy_FileErrors(t *testing.T) {
	for name, path := range map[string]string{
		"missing":   filepath.Join(t.TempDir(), "missing.FCStd"),
		"directory": t.TempDir(),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := DeriveFromManifestAndWorkingCopy(exactWorkingCopyManifest(), path)
			var fileErr *FileError
			if !errors.As(err, &fileErr) || !errors.Is(err, ErrIO) || fileErr.Path != path || fileErr.Err == nil {
				t.Fatalf("error=%T %#v", err, err)
			}
		})
	}
}

func TestDeriveFromManifestAndWorkingCopy_PreservesParameterSemantics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prepared.FCStd")
	writeExactWorkingCopy(t, path, []byte("prepared"))
	manifest := exactWorkingCopyManifest()
	manifest.ParameterAssignments = []planner.ExportManifestParameterAssignment{
		{Name: "z", Value: 2, Type: "number", Unit: "mm"},
		{Name: "a", Value: 1, Type: "number", Unit: "mm"},
	}
	manifest.Verification.ExpectedParameters = []planner.VerificationExpectedParameter{
		{ID: "z-id", Name: "Z", Value: 2, Type: "number", Unit: "mm"},
		{ID: "a-id", Name: "A", Value: 1, Type: "number", Unit: "mm"},
	}
	manifest.Verification.ObservationParameterLinks = []planner.VerificationObservationParameterLink{
		{ID: "z-id", Name: "ZProp", GroupName: "Vars"},
		{ID: "a-id", Name: "AProp", GroupName: "Dims"},
	}
	got, err := DeriveFromManifestAndWorkingCopy(manifest, path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Expected.Parameters[0].ID != "a-id" || got.ObservationContext.Parameters[0].ID != "a-id" {
		t.Fatalf("ordering drift: %#v %#v", got.Expected.Parameters, got.ObservationContext.Parameters)
	}
	if got.Observe.Components || !got.Observe.Parameters || !got.Observe.Metadata || !got.Observe.References ||
		got.Checks.Components.Enabled || !got.Checks.Parameters.Enabled || !got.Checks.Metadata.Enabled || !got.Checks.References.Enabled {
		t.Fatalf("category semantics drift: %#v %#v", got.Observe, got.Checks)
	}
}

func TestDeriveFromManifestAndWorkingCopy_NoParameters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prepared.FCStd")
	writeExactWorkingCopy(t, path, []byte("prepared"))
	manifest := exactWorkingCopyManifest()
	manifest.ParameterAssignments = nil
	manifest.Verification = planner.VerificationManifestIntent{}
	got, err := DeriveFromManifestAndWorkingCopy(manifest, path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Observe.Parameters || got.Checks.Parameters.Enabled || got.ObservationContext.Parameters == nil || len(got.ObservationContext.Parameters) != 0 {
		t.Fatalf("no-parameter semantics=%#v", got)
	}
	if !got.Observe.Metadata || !got.Observe.References || !got.Checks.Metadata.Enabled || !got.Checks.References.Enabled {
		t.Fatalf("metadata/reference disabled: %#v", got)
	}
}

func TestDeriveFromManifestAndWorkingCopy_ReturnsIndependentContracts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prepared.FCStd")
	writeExactWorkingCopy(t, path, []byte("prepared"))
	first, err := DeriveFromManifestAndWorkingCopy(exactWorkingCopyManifest(), path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := DeriveFromManifestAndWorkingCopy(exactWorkingCopyManifest(), path)
	if err != nil {
		t.Fatal(err)
	}
	first.Expected.Parameters[0].ID = "mutated"
	first.ObservationContext.Parameters[0].Name = "mutated"
	first.Expected.Metadata[0].Value = "mutated"
	first.Expected.References[0].Name = "/mutated"
	if second.Expected.Parameters[0].ID != "width" || second.ObservationContext.Parameters[0].Name != "Width" ||
		second.Expected.Metadata[0].Value == "mutated" || second.Expected.References[0].Name != path {
		t.Fatalf("contracts alias: %#v", second)
	}
}
