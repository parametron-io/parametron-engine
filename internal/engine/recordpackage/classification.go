package recordpackage

import (
	"fmt"
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

// RawRuntimeOccurrenceEvidenceContractPath returns the canonical raw occurrence
// evidence contract path.
func RawRuntimeOccurrenceEvidenceContractPath() string {
	return mustJoinContractPath(RawEvidenceDirectoryName, rawRuntimeDirectoryName, "prm.occurrence-evidence.json")
}

// RawRuntimeIntegrityEvidenceContractPath returns the canonical raw runtime
// integrity evidence contract path.
func RawRuntimeIntegrityEvidenceContractPath() string {
	return mustJoinContractPath(RawEvidenceDirectoryName, rawRuntimeDirectoryName, "prm.integrity-evidence.json")
}

// RawFactEvidenceContractPath returns the canonical raw fact evidence contract path.
func RawFactEvidenceContractPath() string {
	return mustJoinContractPath(RawEvidenceDirectoryName, "fact", "prm.fact-evidence.json")
}

// RawHandoffDirectoryContractPath returns the canonical raw handoff directory contract path.
func RawHandoffDirectoryContractPath() string {
	return mustJoinContractPath(RawEvidenceDirectoryName, rawHandoffDirectoryName)
}

// OutcomeRawEvidenceContractPath returns the outcome-scoped package path for one
// raw evidence file. The canonical path keeps its family-relative shape under
// raw/outcomes/<outcomeKey>/ so the association remains explicit.
func OutcomeRawEvidenceContractPath(outcomeKey, canonicalPath string) (string, error) {
	outcomeKey, err := NormalizeOutcomeKey(outcomeKey)
	if err != nil {
		return "", err
	}
	if outcomeKey == "" {
		return "", fmt.Errorf("%w: outcome key is required", ErrInvalidLayoutPath)
	}
	relative, ok := outcomeRawEvidenceRelative(canonicalPath)
	if !ok {
		return "", fmt.Errorf("%w: contract path %q has no outcome-scoped form", ErrInvalidLayoutPath, canonicalPath)
	}
	return JoinContractPath(RawEvidenceDirectoryName, "outcomes", outcomeKey, relative)
}

func outcomeRawEvidenceRelative(canonicalPath string) (string, bool) {
	relative, ok := map[string]string{
		RawObservedContractPath():                  rawObservedDirectoryName + "/" + rawObservedFileName,
		RawVerificationContractPath():              rawVerificationDirectoryName + "/" + rawVerificationFileName,
		RawRuntimeResultContractPath():             rawRuntimeDirectoryName + "/" + rawRuntimeResultFileName,
		RawRuntimeReferenceTraversalContractPath(): rawRuntimeDirectoryName + "/" + rawRuntimeReferenceTraversalFileName,
		RawRuntimeOccurrenceEvidenceContractPath(): rawRuntimeDirectoryName + "/" + "prm.occurrence-evidence.json",
		RawRuntimeIntegrityEvidenceContractPath():  rawRuntimeDirectoryName + "/" + "prm.integrity-evidence.json",
		RawFactEvidenceContractPath():              "fact/prm.fact-evidence.json",
	}[canonicalPath]
	return relative, ok
}

func isOutcomeRawEvidenceContractPath(path string) bool {
	parts := strings.Split(path, "/")
	if len(parts) < 5 || parts[0] != RawEvidenceDirectoryName || parts[1] != "outcomes" {
		return false
	}
	if key, err := NormalizeOutcomeKey(parts[2]); err != nil || key == "" {
		return false
	}
	canonical := append([]string{parts[0]}, parts[3:]...)
	_, ok := outcomeRawEvidenceRelative(strings.Join(canonical, "/"))
	return ok
}

// EntryForContractPath classifies a known package contract path.
// Unsafe paths are not treated as valid entries.
func EntryForContractPath(path string) (Entry, bool) {
	if err := ValidateContractPath(path); err != nil {
		return Entry{}, false
	}
	if family, ok := outcomeRecordFamilyForContractPath(path); ok {
		return Entry{ContractPath: path, Kind: EntryKindFile, Role: EntryRoleNormalizedRecord, Family: family}, true
	}
	if isOutcomeRawEvidenceContractPath(path) {
		return Entry{ContractPath: path, Kind: EntryKindFile, Role: EntryRoleRawEvidence}, true
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
	if _, ok := outcomeRecordFamilyForContractPath(path); ok {
		return true
	}
	entry, ok := entryForValidatedContractPath(path)
	if !ok {
		return false
	}
	return entry.Role == EntryRoleNormalizedRecord && entry.Kind == EntryKindFile
}

func outcomeRecordFamilyForContractPath(path string) (recordcontract.Family, bool) {
	parts := strings.Split(path, "/")
	if len(parts) != 4 || parts[0] != RecordsDirectoryName || parts[1] != OutcomeRecordsDirectoryName || parts[2] == "" {
		return "", false
	}
	if key, err := NormalizeOutcomeKey(parts[2]); err != nil || key == "" {
		return "", false
	}
	for _, def := range recordcontract.Definitions() {
		if def.Family == recordcontract.FamilyArtifact {
			continue
		}
		if def.FileName == parts[3] {
			return def.Family, true
		}
	}
	return "", false
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
	if isOutcomeRawEvidenceContractPath(path) {
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
	if isOutcomeRawEvidenceContractPath(path) {
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
	case RawRuntimeResultContractPath(), RawRuntimeReferenceTraversalContractPath(), RawRuntimeOccurrenceEvidenceContractPath(), RawRuntimeIntegrityEvidenceContractPath():
		return true
	}
	if !isOutcomeRawEvidenceContractPath(path) {
		return false
	}
	parts := strings.Split(path, "/")
	canonical := append([]string{parts[0]}, parts[3:]...)
	switch strings.Join(canonical, "/") {
	case RawRuntimeResultContractPath(), RawRuntimeReferenceTraversalContractPath(), RawRuntimeOccurrenceEvidenceContractPath(), RawRuntimeIntegrityEvidenceContractPath():
		return true
	default:
		return false
	}
}

func entryForValidatedContractPath(path string) (Entry, bool) {
	// Optional occurrence, fact, and integrity evidence extend classification
	// without changing the established core layout returned for runs that
	// request none of them.
	if path == RawRuntimeOccurrenceEvidenceContractPath() || path == RawFactEvidenceContractPath() ||
		path == RawRuntimeIntegrityEvidenceContractPath() {
		return Entry{ContractPath: path, Kind: EntryKindFile, Role: EntryRoleRawEvidence}, true
	}
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
