package recordmap

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"parametron/internal/engine/artifact"
	"parametron/internal/engine/recordcontract"
)

const (
	artifactStoreManifestEvidenceKind = "artifact-store-manifest"
	artifactStoreManifestEvidenceRef  = "raw/artifact-store/manifest.json"
)

// ArtifactMappingInput carries artifact-store records and caller-supplied identity material.
type ArtifactMappingInput struct {
	Artifacts []artifact.Artifact

	// Required stable key material supplied by the caller.
	// Do not invent source revision or PDM identity.
	RecordKeyPrefix string

	// Optional base provenance supplied by the caller.
	// The mapper may enrich evidence references from artifact-store manifest data,
	// but must not fabricate unavailable PDM/source revision data.
	Provenance recordcontract.Provenance
}

// ArtifactManifestMappingInput carries artifact-store manifest data and caller identity material.
type ArtifactManifestMappingInput struct {
	Manifest artifact.Manifest

	// Required stable key material supplied by the caller.
	RecordKeyPrefix string

	// Optional base provenance supplied by the caller.
	Provenance recordcontract.Provenance
}

// ArtifactMappingOutput carries normalized artifact records produced by the mapper.
type ArtifactMappingOutput struct {
	ArtifactRecords []recordcontract.ArtifactRecord
}

// MapArtifactStoreRecords maps artifact-store records into normalized artifact record surfaces.
func MapArtifactStoreRecords(input ArtifactMappingInput) (ArtifactMappingOutput, error) {
	records, err := MapArtifactStoreRecordsToArtifactRecords(input)
	if err != nil {
		return ArtifactMappingOutput{}, err
	}
	return ArtifactMappingOutput{ArtifactRecords: records}, nil
}

// MapArtifactStoreManifest maps artifact-store manifest entries into normalized artifact record surfaces.
func MapArtifactStoreManifest(input ArtifactManifestMappingInput) (ArtifactMappingOutput, error) {
	records, err := MapArtifactStoreManifestToArtifactRecords(input)
	if err != nil {
		return ArtifactMappingOutput{}, err
	}
	return ArtifactMappingOutput{ArtifactRecords: records}, nil
}

// MapArtifactStoreRecordsToArtifactRecords maps artifact-store records into validated artifact records.
func MapArtifactStoreRecordsToArtifactRecords(input ArtifactMappingInput) ([]recordcontract.ArtifactRecord, error) {
	prefix, err := validateRecordKeyPrefix(input.RecordKeyPrefix)
	if err != nil {
		return nil, err
	}

	if len(input.Artifacts) == 0 {
		return []recordcontract.ArtifactRecord{}, nil
	}

	provenance, err := enrichProvenanceWithManifestEvidence(input.Provenance)
	if err != nil {
		return nil, fmt.Errorf("%w: provenance: %v", ErrInvalidArtifactMapping, err)
	}

	artifacts := append([]artifact.Artifact(nil), input.Artifacts...)
	sortArtifactsForMapping(artifacts)

	records := make([]recordcontract.ArtifactRecord, 0, len(artifacts))
	for index, item := range artifacts {
		record, err := mapArtifactStoreRecord(prefix, provenance, item, index)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}

	sortArtifactRecords(records)
	return records, nil
}

// MapArtifactStoreManifestToArtifactRecords maps manifest artifacts into validated artifact records.
func MapArtifactStoreManifestToArtifactRecords(input ArtifactManifestMappingInput) ([]recordcontract.ArtifactRecord, error) {
	return MapArtifactStoreRecordsToArtifactRecords(ArtifactMappingInput{
		Artifacts:       input.Manifest.Artifacts,
		RecordKeyPrefix: input.RecordKeyPrefix,
		Provenance:      input.Provenance,
	})
}

func mapArtifactStoreRecord(
	prefix string,
	provenance recordcontract.Provenance,
	item artifact.Artifact,
	index int,
) (recordcontract.ArtifactRecord, error) {
	ident := artifactIdentifier(item, index)

	if err := validateArtifactStoreEntry(item); err != nil {
		return recordcontract.ArtifactRecord{}, fmt.Errorf("%w: artifact %s: %v", ErrInvalidArtifactMapping, ident, err)
	}

	artifactClass, err := mapArtifactClass(item.Class)
	if err != nil {
		return recordcontract.ArtifactRecord{}, fmt.Errorf("%w: artifact %s: %v", ErrInvalidArtifactMapping, ident, err)
	}

	artifactType, err := mapArtifactType(item.Type)
	if err != nil {
		return recordcontract.ArtifactRecord{}, fmt.Errorf("%w: artifact %s: %v", ErrInvalidArtifactMapping, ident, err)
	}

	summary := recordcontract.ArtifactSummary{
		Class:          artifactClass,
		Type:           artifactType,
		Path:           strings.TrimSpace(item.Path),
		Filename:       strings.TrimSpace(item.Filename),
		MimeType:       strings.TrimSpace(item.MimeType),
		SizeBytes:      item.SizeBytes,
		ChecksumSHA256: strings.TrimSpace(item.ChecksumSHA256),
		Linkage: recordcontract.ArtifactLinkage{
			JobID:      strings.TrimSpace(item.JobID),
			ProductKey: strings.TrimSpace(item.ProductID),
			StepRef:    strings.TrimSpace(item.StepID),
		},
		Evidence: recordcontract.ArtifactEvidence{
			SourceKind: artifactStoreManifestEvidenceKind,
			SourceRef:  artifactStoreManifestEvidenceRef,
		},
	}

	recordKey := deriveArtifactRecordKey(prefix, item)

	record, err := recordcontract.BuildArtifactRecord(recordcontract.ArtifactRecordInput{
		RecordKey:  recordKey,
		Provenance: provenance,
		Artifact:   summary,
	})
	if err != nil {
		return recordcontract.ArtifactRecord{}, fmt.Errorf("%w: artifact %s: %v", ErrInvalidArtifactMapping, ident, err)
	}

	return record, nil
}

func validateRecordKeyPrefix(prefix string) (string, error) {
	trimmed := strings.TrimSpace(prefix)
	if trimmed == "" {
		return "", fmt.Errorf("%w: record key prefix is required", ErrInvalidArtifactMapping)
	}
	return trimmed, nil
}

func validateArtifactStoreEntry(item artifact.Artifact) error {
	if item.SizeBytes < 0 {
		return fmt.Errorf("sizeBytes must not be negative")
	}
	if err := validateChecksumSHA256(item.ChecksumSHA256); err != nil {
		return err
	}
	return nil
}

func validateChecksumSHA256(checksum string) error {
	checksum = strings.TrimSpace(checksum)
	if checksum == "" {
		return nil
	}
	if len(checksum) != 64 {
		return fmt.Errorf("checksumSHA256 must be a 64-character lowercase hexadecimal digest")
	}
	for _, r := range checksum {
		if !unicode.IsDigit(r) && (r < 'a' || r > 'f') {
			return fmt.Errorf("checksumSHA256 must be a 64-character lowercase hexadecimal digest")
		}
	}
	return nil
}

func mapArtifactClass(class artifact.ArtifactClass) (recordcontract.ArtifactClass, error) {
	normalized := artifact.DefaultArtifactClass(class)
	if !artifact.ValidArtifactClass(normalized) {
		return "", fmt.Errorf("unsupported artifact class %q", class)
	}

	switch normalized {
	case artifact.ArtifactClassExecutionOutput:
		return recordcontract.ArtifactClassExecutionOutput, nil
	case artifact.ArtifactClassVerified:
		return recordcontract.ArtifactClassVerified, nil
	default:
		return "", fmt.Errorf("unsupported artifact class %q", class)
	}
}

func mapArtifactType(typ artifact.ArtifactType) (recordcontract.ArtifactType, error) {
	normalized := strings.TrimSpace(string(typ))
	if normalized == "" {
		return recordcontract.ArtifactTypeUnknown, nil
	}

	switch artifact.ArtifactType(normalized) {
	case artifact.ArtifactTypeSTEP:
		return recordcontract.ArtifactTypeSTEP, nil
	case artifact.ArtifactTypeCSV:
		return recordcontract.ArtifactTypeCSV, nil
	case artifact.ArtifactTypePDF:
		return recordcontract.ArtifactTypePDF, nil
	case artifact.ArtifactTypeJSON:
		return recordcontract.ArtifactTypeJSON, nil
	case artifact.ArtifactTypeLog:
		return recordcontract.ArtifactTypeLog, nil
	case artifact.ArtifactTypeUnknown:
		return recordcontract.ArtifactTypeUnknown, nil
	default:
		return "", fmt.Errorf("unsupported artifact type %q", typ)
	}
}

func deriveArtifactRecordKey(prefix string, item artifact.Artifact) string {
	identity := strings.TrimSpace(item.ID)
	if identity == "" {
		class := artifact.DefaultArtifactClass(item.Class)
		typ := item.Type
		if strings.TrimSpace(string(typ)) == "" {
			typ = artifact.ArtifactTypeUnknown
		}
		identity = artifact.DeterministicID(
			strings.TrimSpace(item.JobID),
			strings.TrimSpace(item.ProductID),
			strings.TrimSpace(item.StepID),
			class,
			typ,
			strings.TrimSpace(item.Filename),
			strings.TrimSpace(item.ChecksumSHA256),
		)
	}
	return prefix + ":artifact:" + identity
}

func enrichProvenanceWithManifestEvidence(provenance recordcontract.Provenance) (recordcontract.Provenance, error) {
	out := recordcontract.NormalizeProvenance(provenance)
	out.Inputs = append([]recordcontract.ProvenanceInput(nil), out.Inputs...)
	out.Evidence = append([]recordcontract.EvidenceReference(nil), out.Evidence...)

	if !hasManifestEvidenceReference(out.Evidence) {
		out.Evidence = append(out.Evidence, recordcontract.EvidenceReference{
			Kind: artifactStoreManifestEvidenceKind,
			Ref:  artifactStoreManifestEvidenceRef,
		})
	}

	out = recordcontract.NormalizeProvenance(out)
	if err := recordcontract.ValidateProvenance(out); err != nil {
		return recordcontract.Provenance{}, err
	}
	return out, nil
}

func hasManifestEvidenceReference(evidence []recordcontract.EvidenceReference) bool {
	for _, item := range evidence {
		if item.Kind == artifactStoreManifestEvidenceKind && item.Ref == artifactStoreManifestEvidenceRef {
			return true
		}
	}
	return false
}

func artifactIdentifier(item artifact.Artifact, index int) string {
	if id := strings.TrimSpace(item.ID); id != "" {
		return "id=" + id
	}
	return fmt.Sprintf("index=%d", index)
}

func sortArtifactsForMapping(artifacts []artifact.Artifact) {
	sort.Slice(artifacts, func(i, j int) bool {
		if artifacts[i].JobID != artifacts[j].JobID {
			return artifacts[i].JobID < artifacts[j].JobID
		}
		if artifacts[i].ProductID != artifacts[j].ProductID {
			return artifacts[i].ProductID < artifacts[j].ProductID
		}
		if artifacts[i].StepID != artifacts[j].StepID {
			return artifacts[i].StepID < artifacts[j].StepID
		}
		if artifacts[i].Class != artifacts[j].Class {
			return artifacts[i].Class < artifacts[j].Class
		}
		if artifacts[i].Path != artifacts[j].Path {
			return artifacts[i].Path < artifacts[j].Path
		}
		if artifacts[i].Filename != artifacts[j].Filename {
			return artifacts[i].Filename < artifacts[j].Filename
		}
		if artifacts[i].Type != artifacts[j].Type {
			return artifacts[i].Type < artifacts[j].Type
		}
		if artifacts[i].ChecksumSHA256 != artifacts[j].ChecksumSHA256 {
			return artifacts[i].ChecksumSHA256 < artifacts[j].ChecksumSHA256
		}
		return artifacts[i].ID < artifacts[j].ID
	})
}

func sortArtifactRecords(records []recordcontract.ArtifactRecord) {
	sort.Slice(records, func(i, j int) bool {
		left := records[i].Artifact
		right := records[j].Artifact

		if left.Linkage.JobID != right.Linkage.JobID {
			return left.Linkage.JobID < right.Linkage.JobID
		}
		if left.Linkage.ProductKey != right.Linkage.ProductKey {
			return left.Linkage.ProductKey < right.Linkage.ProductKey
		}
		if left.Linkage.StepRef != right.Linkage.StepRef {
			return left.Linkage.StepRef < right.Linkage.StepRef
		}
		if left.Class != right.Class {
			return left.Class < right.Class
		}
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		if left.Filename != right.Filename {
			return left.Filename < right.Filename
		}
		if left.Type != right.Type {
			return left.Type < right.Type
		}
		if left.ChecksumSHA256 != right.ChecksumSHA256 {
			return left.ChecksumSHA256 < right.ChecksumSHA256
		}
		return records[i].RecordKey < records[j].RecordKey
	})
}
