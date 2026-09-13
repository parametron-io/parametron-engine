package tableloader

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"parametron/internal/engine/table"
)

func TestLoadFiles_SingleValidTable(t *testing.T) {
	path := writeTableJSON(t, "fasteners.json", validTableJSON("fastener_catalog", "M8x20", "8"))

	loaded, err := LoadFiles(map[string]string{"fasteners": path})
	if err != nil {
		t.Fatalf("LoadFiles returned error: %v", err)
	}

	if len(loaded) != 1 {
		t.Fatalf("expected 1 table, got %d", len(loaded))
	}
	if _, ok := loaded["fasteners"]; !ok {
		t.Fatalf("expected table keyed by logical ID")
	}
	if loaded["fasteners"].Name != "fastener_catalog" {
		t.Fatalf("unexpected embedded table name: %q", loaded["fasteners"].Name)
	}
}

func TestLoadFiles_MultipleValidTablesDeterministicAcrossMapOrder(t *testing.T) {
	fastenersPath := writeTableJSON(t, "fasteners.json", validTableJSON("fastener_catalog", "M8x20", "8"))
	labelsPath := writeTableJSON(t, "labels.json", validTableJSON("label_catalog", "A", "1"))

	first, err := LoadFiles(map[string]string{
		"fasteners": fastenersPath,
		"labels":    labelsPath,
	})
	if err != nil {
		t.Fatalf("first LoadFiles returned error: %v", err)
	}

	second, err := LoadFiles(map[string]string{
		"labels":    labelsPath,
		"fasteners": fastenersPath,
	})
	if err != nil {
		t.Fatalf("second LoadFiles returned error: %v", err)
	}

	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("expected two loaded tables, got %d and %d", len(first), len(second))
	}
	assertSameFingerprint(t, first["fasteners"], second["fasteners"])
	assertSameFingerprint(t, first["labels"], second["labels"])
}

func TestLoadFiles_RejectsMalformedInputs(t *testing.T) {
	path := writeTableJSON(t, "fasteners.json", validTableJSON("fastener_catalog", "M8x20", "8"))

	testCases := []struct {
		name          string
		inputs        map[string]string
		errorContains string
	}{
		{
			name:          "empty logical id",
			inputs:        map[string]string{"": path},
			errorContains: "logical ID must not be empty",
		},
		{
			name:          "logical id with surrounding whitespace",
			inputs:        map[string]string{" fasteners ": path},
			errorContains: "must not have leading or trailing whitespace",
		},
		{
			name:          "empty path",
			inputs:        map[string]string{"fasteners": ""},
			errorContains: "path must not be empty",
		},
		{
			name:          "path with surrounding whitespace",
			inputs:        map[string]string{"fasteners": " " + path + " "},
			errorContains: "path must not have leading or trailing whitespace",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := LoadFiles(tt.inputs); err == nil || !strings.Contains(err.Error(), tt.errorContains) {
				t.Fatalf("expected error containing %q, got %v", tt.errorContains, err)
			}
		})
	}
}

func TestLoadFiles_MissingFilePreservesIOClassification(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")

	_, err := LoadFiles(map[string]string{"fasteners": path})
	if err == nil {
		t.Fatal("expected LoadFiles to fail")
	}
	if !errors.Is(err, table.ErrIO) {
		t.Fatalf("expected ErrIO, got %v", err)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got %v", err)
	}
}

func TestLoadFiles_InvalidJSONPreservesDecodeClassification(t *testing.T) {
	path := writeTableJSON(t, "invalid.json", `{"schemaVersion":"1.0"`)

	_, err := LoadFiles(map[string]string{"fasteners": path})
	if err == nil {
		t.Fatal("expected LoadFiles to fail")
	}
	if !errors.Is(err, table.ErrDecode) {
		t.Fatalf("expected ErrDecode, got %v", err)
	}
	if errors.Is(err, table.ErrValidation) {
		t.Fatalf("expected decode error, got validation error: %v", err)
	}
}

func TestLoadFiles_InvalidSchemaPreservesValidationClassification(t *testing.T) {
	path := writeTableJSON(t, "invalid-schema.json", `{
  "schemaVersion": "1.0",
  "name": "fastener_catalog",
  "keyColumn": "code",
  "columns": [],
  "rows": []
}`)

	_, err := LoadFiles(map[string]string{"fasteners": path})
	if err == nil {
		t.Fatal("expected LoadFiles to fail")
	}
	if !errors.Is(err, table.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestLoadFiles_NoSilentOverwriteForDistinctLogicalIDs(t *testing.T) {
	path := writeTableJSON(t, "fasteners.json", validTableJSON("fastener_catalog", "M8x20", "8"))

	loaded, err := LoadFiles(map[string]string{
		"fasteners_a": path,
		"fasteners_b": path,
	})
	if err != nil {
		t.Fatalf("LoadFiles returned error: %v", err)
	}

	if len(loaded) != 2 {
		t.Fatalf("expected 2 tables, got %d", len(loaded))
	}
	if loaded["fasteners_a"] == loaded["fasteners_b"] {
		t.Fatal("expected independent table instances")
	}

	loaded["fasteners_a"].Name = "mutated"
	if loaded["fasteners_b"].Name != "fastener_catalog" {
		t.Fatalf("unexpected overwrite across logical IDs: %q", loaded["fasteners_b"].Name)
	}
}

func writeTableJSON(t *testing.T, name, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write table fixture: %v", err)
	}
	return path
}

func validTableJSON(name, key, diameter string) string {
	return `{
  "schemaVersion": "1.0",
  "name": "` + name + `",
  "keyColumn": "sku",
  "columns": [
    {"name": "sku", "type": "string", "required": true},
    {"name": "diameter", "type": "number", "required": true}
  ],
  "rows": [
    {"sku": "` + key + `", "diameter": ` + diameter + `}
  ]
}`
}

func assertSameFingerprint(t *testing.T, left *table.Table, right *table.Table) {
	t.Helper()

	leftFingerprint, err := left.Fingerprint()
	if err != nil {
		t.Fatalf("left Fingerprint returned error: %v", err)
	}
	rightFingerprint, err := right.Fingerprint()
	if err != nil {
		t.Fatalf("right Fingerprint returned error: %v", err)
	}
	if leftFingerprint != rightFingerprint {
		t.Fatalf("expected equal fingerprints, got %q and %q", leftFingerprint, rightFingerprint)
	}
}
