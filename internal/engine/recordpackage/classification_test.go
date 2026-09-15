package recordpackage_test

import (
	"reflect"
	"testing"

	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordpackage"
)

func TestCanonicalRawEvidencePathHelpers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"report", recordpackage.RawReportContractPath(), "raw/prm.report.json"},
		{"metadata", recordpackage.RawMetadataContractPath(), "raw/prm.metadata.json"},
		{"artifact store manifest", recordpackage.RawArtifactStoreManifestContractPath(), "raw/artifact-store/manifest.json"},
		{"observed", recordpackage.RawObservedContractPath(), "raw/observed/prm.observed.json"},
		{"verification", recordpackage.RawVerificationContractPath(), "raw/verification/prm.verification.json"},
		{"runtime result", recordpackage.RawRuntimeResultContractPath(), "raw/runtime/prm.result.json"},
		{"reference traversal", recordpackage.RawRuntimeReferenceTraversalContractPath(), "raw/runtime/prm.reference-traversal.json"},
		{"handoff directory", recordpackage.RawHandoffDirectoryContractPath(), "raw/handoff"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("path = %q, want %q", tt.got, tt.want)
			}
		})
	}
}

func TestRawRuntimeReferenceTraversalContractPath(t *testing.T) {
	t.Parallel()

	got := recordpackage.RawRuntimeReferenceTraversalContractPath()
	if got != "raw/runtime/prm.reference-traversal.json" {
		t.Fatalf("RawRuntimeReferenceTraversalContractPath() = %q, want %q",
			got, "raw/runtime/prm.reference-traversal.json")
	}
	if err := recordpackage.ValidateContractPath(got); err != nil {
		t.Fatalf("RawRuntimeReferenceTraversalContractPath() = %q; ValidateContractPath returned error: %v", got, err)
	}
}

func TestRawRuntimeReferenceTraversalEntryAndClassification(t *testing.T) {
	t.Parallel()

	path := recordpackage.RawRuntimeReferenceTraversalContractPath()

	entry, ok := recordpackage.EntryForContractPath(path)
	if !ok {
		t.Fatalf("EntryForContractPath(%q) ok = false, want true", path)
	}
	if entry.Kind != recordpackage.EntryKindFile {
		t.Fatalf("entry.Kind = %q, want %q", entry.Kind, recordpackage.EntryKindFile)
	}
	if entry.Role != recordpackage.EntryRoleRawEvidence {
		t.Fatalf("entry.Role = %q, want %q", entry.Role, recordpackage.EntryRoleRawEvidence)
	}
	if entry.Family != "" {
		t.Fatalf("entry.Family = %q, want empty (traversal evidence has no normalized record family)", entry.Family)
	}
	if entry.ContractPath != path {
		t.Fatalf("entry.ContractPath = %q, want %q", entry.ContractPath, path)
	}

	if !recordpackage.IsRawEvidenceContractPath(path) {
		t.Fatalf("IsRawEvidenceContractPath(%q) = false, want true", path)
	}
	if !recordpackage.IsRawEvidenceFileContractPath(path) {
		t.Fatalf("IsRawEvidenceFileContractPath(%q) = false, want true", path)
	}
	if !recordpackage.IsRawRuntimeEvidenceContractPath(path) {
		t.Fatalf("IsRawRuntimeEvidenceContractPath(%q) = false, want true", path)
	}
	if recordpackage.IsNormalizedRecordContractPath(path) {
		t.Fatalf("IsNormalizedRecordContractPath(%q) = true, want false", path)
	}
	if recordpackage.IsRawHandoffRuntimeProvenanceContractPath(path) {
		t.Fatalf("IsRawHandoffRuntimeProvenanceContractPath(%q) = true, want false", path)
	}
	if recordpackage.IsRawHandoffEvidenceFileContractPath(path) {
		t.Fatalf("IsRawHandoffEvidenceFileContractPath(%q) = true, want false", path)
	}
}

func TestEntryForContractPathClassifiesRegisteredEntries(t *testing.T) {
	t.Parallel()

	for _, want := range append(recordpackage.RecordEntries(), recordpackage.RawEvidenceEntries()...) {
		got, ok := recordpackage.EntryForContractPath(want.ContractPath)
		if !ok {
			t.Fatalf("EntryForContractPath(%q) ok = false, want true", want.ContractPath)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("EntryForContractPath(%q) = %#v, want %#v", want.ContractPath, got, want)
		}

		got.ContractPath = "mutated"
		got.Kind = recordpackage.EntryKindDirectory
		got.Role = recordpackage.EntryRoleArtifactPayload
		got.Family = recordcontract.Family("mutated")

		again, ok := recordpackage.EntryForContractPath(want.ContractPath)
		if !ok {
			t.Fatalf("EntryForContractPath(%q) after mutation ok = false, want true", want.ContractPath)
		}
		if !reflect.DeepEqual(again, want) {
			t.Fatalf("EntryForContractPath(%q) returned mutated entry %#v, want %#v", want.ContractPath, again, want)
		}
	}
}

func TestEntryForContractPathRejectsUnknownAndUnsafePaths(t *testing.T) {
	t.Parallel()

	for _, path := range append([]string{
		"raw/runtime/other.json",
		"artifacts/files/example.step",
		"records/not-registered.json",
		"raw/handoff/package.json",
		"raw/handoff/product-a/manifest.json",
	}, unsafeClassificationPaths()...) {
		t.Run(path, func(t *testing.T) {
			if entry, ok := recordpackage.EntryForContractPath(path); ok {
				t.Fatalf("EntryForContractPath(%q) = %#v, true; want false", path, entry)
			}
		})
	}
}

func TestNormalizedRecordContractPathClassification(t *testing.T) {
	t.Parallel()

	for _, entry := range recordpackage.RecordEntries() {
		if !recordpackage.IsNormalizedRecordContractPath(entry.ContractPath) {
			t.Fatalf("IsNormalizedRecordContractPath(%q) = false, want true", entry.ContractPath)
		}
	}

	for _, path := range []string{
		"raw/runtime/prm.result.json",
		"raw/runtime/prm.reference-traversal.json",
		"raw/prm.report.json",
		"raw/prm.metadata.json",
		"raw/observed/prm.observed.json",
		"raw/verification/prm.verification.json",
		"raw/artifact-store/manifest.json",
		"raw/handoff",
		"raw/handoff/package.json",
		"artifacts/files/example.step",
	} {
		if recordpackage.IsNormalizedRecordContractPath(path) {
			t.Fatalf("IsNormalizedRecordContractPath(%q) = true, want false", path)
		}
	}
}

func TestRawHandoffRuntimeProvenanceContractPathClassification(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"raw/handoff",
		"raw/handoff/package.json",
		"raw/handoff/manifest.json",
		"raw/handoff/product-a/manifest.json",
		"raw/handoff/product-a/files/input.json",
	} {
		if !recordpackage.IsRawHandoffRuntimeProvenanceContractPath(path) {
			t.Fatalf("IsRawHandoffRuntimeProvenanceContractPath(%q) = false, want true", path)
		}
	}

	for _, path := range handoffNonRuntimeProvenancePaths() {
		if recordpackage.IsRawHandoffRuntimeProvenanceContractPath(path) {
			t.Fatalf("IsRawHandoffRuntimeProvenanceContractPath(%q) = true, want false", path)
		}
	}
}

func TestRawHandoffEvidenceFileContractPathClassification(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"raw/handoff/package.json",
		"raw/handoff/manifest.json",
		"raw/handoff/product-a/manifest.json",
		"raw/handoff/product-a/files/input.json",
	} {
		if !recordpackage.IsRawHandoffEvidenceFileContractPath(path) {
			t.Fatalf("IsRawHandoffEvidenceFileContractPath(%q) = false, want true", path)
		}
	}

	for _, path := range append([]string{"raw/handoff"}, handoffNonRuntimeProvenancePaths()...) {
		if recordpackage.IsRawHandoffEvidenceFileContractPath(path) {
			t.Fatalf("IsRawHandoffEvidenceFileContractPath(%q) = true, want false", path)
		}
	}
}

func TestRawEvidenceContractPathClassification(t *testing.T) {
	t.Parallel()

	for _, entry := range recordpackage.RawEvidenceEntries() {
		if !recordpackage.IsRawEvidenceContractPath(entry.ContractPath) {
			t.Fatalf("IsRawEvidenceContractPath(%q) = false, want true", entry.ContractPath)
		}
	}

	for _, path := range []string{
		"raw/handoff/package.json",
		"raw/handoff/product-a/manifest.json",
	} {
		if !recordpackage.IsRawEvidenceContractPath(path) {
			t.Fatalf("IsRawEvidenceContractPath(%q) = false, want true", path)
		}
	}

	for _, entry := range recordpackage.RecordEntries() {
		if recordpackage.IsRawEvidenceContractPath(entry.ContractPath) {
			t.Fatalf("IsRawEvidenceContractPath(%q) = true for normalized record, want false", entry.ContractPath)
		}
	}

	for _, path := range append([]string{
		"artifacts/files/example.step",
		"raw/runtime/other.json",
		"raw/unknown.json",
	}, unsafeClassificationPaths()...) {
		if recordpackage.IsRawEvidenceContractPath(path) {
			t.Fatalf("IsRawEvidenceContractPath(%q) = true, want false", path)
		}
	}
}

func TestRawEvidenceFileContractPathClassification(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"raw/prm.report.json",
		"raw/prm.metadata.json",
		"raw/artifact-store/manifest.json",
		"raw/observed/prm.observed.json",
		"raw/verification/prm.verification.json",
		"raw/runtime/prm.result.json",
		"raw/runtime/prm.reference-traversal.json",
		"raw/handoff/package.json",
		"raw/handoff/product-a/manifest.json",
	} {
		if !recordpackage.IsRawEvidenceFileContractPath(path) {
			t.Fatalf("IsRawEvidenceFileContractPath(%q) = false, want true", path)
		}
	}

	for _, path := range []string{"raw/handoff"} {
		if recordpackage.IsRawEvidenceFileContractPath(path) {
			t.Fatalf("IsRawEvidenceFileContractPath(%q) = true for directory, want false", path)
		}
	}
	for _, entry := range recordpackage.RecordEntries() {
		if recordpackage.IsRawEvidenceFileContractPath(entry.ContractPath) {
			t.Fatalf("IsRawEvidenceFileContractPath(%q) = true for normalized record, want false", entry.ContractPath)
		}
	}
	for _, path := range append([]string{
		"artifacts/files/example.step",
		"raw/runtime/other.json",
		"raw/unknown.json",
	}, unsafeClassificationPaths()...) {
		if recordpackage.IsRawEvidenceFileContractPath(path) {
			t.Fatalf("IsRawEvidenceFileContractPath(%q) = true, want false", path)
		}
	}
}

func TestRawRuntimeEvidenceContractPathClassification(t *testing.T) {
	t.Parallel()

	// IsRawRuntimeEvidenceContractPath is an explicit allowlist: only these two paths are accepted.
	for _, path := range []string{
		"raw/runtime/prm.result.json",
		"raw/runtime/prm.reference-traversal.json",
	} {
		if !recordpackage.IsRawRuntimeEvidenceContractPath(path) {
			t.Fatalf("IsRawRuntimeEvidenceContractPath(%q) = false, want true", path)
		}
	}

	// All other paths are rejected, including near-misses and unsafe/non-canonical variants.
	for _, path := range []string{
		"records/parametron.failure-record.json",
		"records/parametron.reference-record.json",
		"artifacts/files/example.step",
		"raw/prm.report.json",
		"raw/prm.metadata.json",
		"raw/observed/prm.observed.json",
		"raw/verification/prm.verification.json",
		"raw/artifact-store/manifest.json",
		"raw/handoff",
		"raw/handoff/example.json",
		"raw/runtime",                                         // directory itself
		"raw/runtime/other.json",                              // arbitrary descendant
		"raw/runtime/reference-traversal.json",                // missing prm. prefix
		"raw/runtime/prm.reference-traversal.json/extra",      // descendant of traversal path
		"raw/runtime/../runtime/prm.reference-traversal.json", // traversal via ..
	} {
		if recordpackage.IsRawRuntimeEvidenceContractPath(path) {
			t.Fatalf("IsRawRuntimeEvidenceContractPath(%q) = true, want false", path)
		}
	}
}

func TestClassificationHelpersRejectUnsafePaths(t *testing.T) {
	t.Parallel()

	for _, path := range unsafeClassificationPaths() {
		t.Run(path, func(t *testing.T) {
			if entry, ok := recordpackage.EntryForContractPath(path); ok {
				t.Fatalf("EntryForContractPath(%q) = %#v, true; want false", path, entry)
			}
			if recordpackage.IsNormalizedRecordContractPath(path) {
				t.Fatalf("IsNormalizedRecordContractPath(%q) = true, want false", path)
			}
			if recordpackage.IsRawEvidenceContractPath(path) {
				t.Fatalf("IsRawEvidenceContractPath(%q) = true, want false", path)
			}
			if recordpackage.IsRawEvidenceFileContractPath(path) {
				t.Fatalf("IsRawEvidenceFileContractPath(%q) = true, want false", path)
			}
			if recordpackage.IsRawRuntimeEvidenceContractPath(path) {
				t.Fatalf("IsRawRuntimeEvidenceContractPath(%q) = true, want false", path)
			}
		})
	}
}

func unsafeClassificationPaths() []string {
	return []string{
		"",
		"/raw/runtime/prm.result.json",
		"../raw/runtime/prm.result.json",
		"raw/../runtime/result.json",
		`raw\runtime\result.json`,
		"raw//runtime/result.json",
		"raw/runtime/prm.result.json/..",
	}
}

func handoffNonRuntimeProvenancePaths() []string {
	return []string{
		"",
		"/raw/handoff/package.json",
		"../raw/handoff/package.json",
		"raw/handoff/../package.json",
		"raw/handoff/./package.json",
		"raw//handoff/package.json",
		`raw\handoff\package.json`,
		"records/parametron.execution-record.json",
		"artifacts/files/output.step",
		"raw/runtime/prm.result.json",
		"raw/runtime/other.json",
		"raw/unknown.json",
	}
}
