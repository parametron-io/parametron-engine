package recordpackage_test

import (
	"testing"

	"parametron/internal/engine/recordpackage"
)

// Issue #51: raw fact evidence is packaged under its own canonical path while
// remaining classified as raw evidence, not as a normalized record family.
func TestRawFactEvidenceContractPathAndClassification(t *testing.T) {
	t.Parallel()

	path := recordpackage.RawFactEvidenceContractPath()
	if path != "raw/fact/prm.fact-evidence.json" {
		t.Fatalf("RawFactEvidenceContractPath() = %q, want %q", path, "raw/fact/prm.fact-evidence.json")
	}
	if err := recordpackage.ValidateContractPath(path); err != nil {
		t.Fatalf("ValidateContractPath(%q) returned error: %v", path, err)
	}

	entry, ok := recordpackage.EntryForContractPath(path)
	if !ok {
		t.Fatalf("EntryForContractPath(%q) ok = false, want true", path)
	}
	if entry.Kind != recordpackage.EntryKindFile || entry.Role != recordpackage.EntryRoleRawEvidence || entry.Family != "" || entry.ContractPath != path {
		t.Fatalf("fact evidence entry = %#v, want a file-role raw evidence entry", entry)
	}

	if !recordpackage.IsRawEvidenceContractPath(path) {
		t.Fatalf("IsRawEvidenceContractPath(%q) = false, want true", path)
	}
	if !recordpackage.IsRawEvidenceFileContractPath(path) {
		t.Fatalf("IsRawEvidenceFileContractPath(%q) = false, want true", path)
	}
	if recordpackage.IsNormalizedRecordContractPath(path) {
		t.Fatalf("IsNormalizedRecordContractPath(%q) = true, want false", path)
	}
	if recordpackage.IsRawRuntimeEvidenceContractPath(path) {
		t.Fatalf("IsRawRuntimeEvidenceContractPath(%q) = true, want false (fact evidence is not runtime evidence)", path)
	}
	if recordpackage.IsRawHandoffRuntimeProvenanceContractPath(path) || recordpackage.IsRawHandoffEvidenceFileContractPath(path) {
		t.Fatalf("fact evidence was classified as handoff evidence: %q", path)
	}
}

func TestRawFactEvidenceRejectsUnknownAndUnsafePaths(t *testing.T) {
	t.Parallel()

	path := recordpackage.RawFactEvidenceContractPath()
	for _, candidate := range []string{
		"/" + path,
		"raw/fact/../fact/prm.fact-evidence.json",
		"raw/fact/extra/prm.fact-evidence.json",
		"raw/fact/prm.fact-request.json",
		"raw/runtime/prm.fact-evidence.json",
		"raw\\fact\\prm.fact-evidence.json",
	} {
		if candidate == path {
			continue
		}
		if _, ok := recordpackage.EntryForContractPath(candidate); ok {
			t.Fatalf("EntryForContractPath(%q) accepted a non-canonical path", candidate)
		}
		if recordpackage.IsRawEvidenceFileContractPath(candidate) {
			t.Fatalf("IsRawEvidenceFileContractPath(%q) = true, want false", candidate)
		}
	}
}
