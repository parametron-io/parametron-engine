package recordpackage

import (
	"strings"

	"parametron/internal/engine/recordcontract"
)

// RawReportContractPath returns the canonical raw report evidence contract path.
func RawReportContractPath() string {
	return mustJoinContractPath(RawEvidenceDirectoryName, rawReportFileName)
}

// RawMetadataContractPath returns the canonical raw metadata evidence contract path.
func RawMetadataContractPath() string {
	return mustJoinContractPath(RawEvidenceDirectoryName, rawMetadataFileName)
}

// RawArtifactStoreManifestContractPath returns the canonical raw artifact-store manifest contract path.
func RawArtifactStoreManifestContractPath() string {
	return mustJoinContractPath(RawEvidenceDirectoryName, rawArtifactStoreDirectoryName, rawArtifactStoreManifestName)
}

// RawObservedContractPath returns the canonical raw observed evidence contract path.
func RawObservedContractPath() string {
	return mustJoinContractPath(RawEvidenceDirectoryName, rawObservedDirectoryName, rawObservedFileName)
}

// RawVerificationContractPath returns the canonical raw verification evidence contract path.
func RawVerificationContractPath() string {
	return mustJoinContractPath(RawEvidenceDirectoryName, rawVerificationDirectoryName, rawVerificationFileName)
}

// RawRuntimeResultContractPath returns the canonical raw runtime result evidence contract path.
func RawRuntimeResultContractPath() string {
	return mustJoinContractPath(RawEvidenceDirectoryName, rawRuntimeDirectoryName, rawRuntimeResultFileName)
}

// RawRuntimeReferenceTraversalContractPath returns the canonical raw runtime
// reference traversal evidence contract path.
func RawRuntimeReferenceTraversalContractPath() string {
	return mustJoinContractPath(RawEvidenceDirectoryName, rawRuntimeDirectoryName, rawRuntimeReferenceTraversalFileName)
}

// RawHandoffDirectoryContractPath returns the canonical raw handoff directory contract path.
func RawHandoffDirectoryContractPath() string {
	return mustJoinContractPath(RawEvidenceDirectoryName, rawHandoffDirectoryName)
}

// EntryForContractPath classifies a known package contract path.
// Unsafe paths are not treated as valid entries.
func EntryForContractPath(path string) (Entry, bool) {
	if err := ValidateContractPath(path); err != nil {
		return Entry{}, false
	}
	entry, ok := entryForValidatedContractPath(path)
	if !ok {
		return Entry{}, false
	}
	return copyEntry(entry), true
}

// IsNormalizedRecordContractPath reports whether path is a registered normalized record file path.
func IsNormalizedRecordContractPath(path string) bool {
	if err := ValidateContractPath(path); err != nil {
		return false
	}
	if isArtifactRecordContractPath(path) {
		return true
	}
	entry, ok := entryForValidatedContractPath(path)
	if !ok {
		return false
	}
	return entry.Role == EntryRoleNormalizedRecord && entry.Kind == EntryKindFile
}

func isArtifactRecordContractPath(contractPath string) bool {
	parts := strings.Split(contractPath, "/")
	if len(parts) != 4 || parts[0] != RecordsDirectoryName || parts[1] != ArtifactRecordsDirectoryName || parts[2] == "" {
		return false
	}
	fileName, ok := recordcontract.FileNameForFamily(recordcontract.FamilyArtifact)
	return ok && parts[3] == fileName
}

// IsRawHandoffRuntimeProvenanceContractPath reports whether path is the raw handoff
// runtime/provenance root directory or a safe descendant under raw/handoff/.
func IsRawHandoffRuntimeProvenanceContractPath(path string) bool {
	if err := ValidateContractPath(path); err != nil {
		return false
	}
	handoffRoot := RawHandoffDirectoryContractPath()
	if path == handoffRoot {
		return true
	}
	return strings.HasPrefix(path, handoffRoot+"/")
}

// IsRawHandoffEvidenceFileContractPath reports whether path is a safe handoff package
// file under raw/handoff/. The raw/handoff directory itself is excluded.
func IsRawHandoffEvidenceFileContractPath(path string) bool {
	if err := ValidateContractPath(path); err != nil {
		return false
	}
	handoffRoot := RawHandoffDirectoryContractPath()
	if path == handoffRoot {
		return false
	}
	return strings.HasPrefix(path, handoffRoot+"/")
}

// IsRawEvidenceContractPath reports whether path is a canonical raw evidence file or directory path.
func IsRawEvidenceContractPath(path string) bool {
	if err := ValidateContractPath(path); err != nil {
		return false
	}
	if IsRawHandoffRuntimeProvenanceContractPath(path) {
		return true
	}
	entry, ok := entryForValidatedContractPath(path)
	if !ok {
		return false
	}
	return entry.Role == EntryRoleRawEvidence
}

// IsRawEvidenceFileContractPath reports whether path is a canonical raw evidence file path.
func IsRawEvidenceFileContractPath(path string) bool {
	if err := ValidateContractPath(path); err != nil {
		return false
	}
	if IsRawHandoffEvidenceFileContractPath(path) {
		return true
	}
	entry, ok := entryForValidatedContractPath(path)
	if !ok {
		return false
	}
	return entry.Role == EntryRoleRawEvidence && entry.Kind == EntryKindFile
}

// IsRawRuntimeEvidenceContractPath reports whether path is a canonical raw runtime evidence path.
func IsRawRuntimeEvidenceContractPath(path string) bool {
	if err := ValidateContractPath(path); err != nil {
		return false
	}
	switch path {
	case RawRuntimeResultContractPath(), RawRuntimeReferenceTraversalContractPath():
		return true
	default:
		return false
	}
}

func entryForValidatedContractPath(path string) (Entry, bool) {
	for _, entry := range Entries() {
		if entry.ContractPath == path {
			return entry, true
		}
	}
	return Entry{}, false
}

func copyEntry(entry Entry) Entry {
	return Entry{
		ContractPath: entry.ContractPath,
		Kind:         entry.Kind,
		Role:         entry.Role,
		Family:       entry.Family,
	}
}
