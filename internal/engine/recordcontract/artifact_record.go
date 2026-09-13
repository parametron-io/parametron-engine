package recordcontract

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ArtifactClass is a normalized artifact class vocabulary value.
type ArtifactClass string

const (
	ArtifactClassExecutionOutput ArtifactClass = "execution_output"
	ArtifactClassVerified        ArtifactClass = "verified_artifact"
)

// ArtifactType is a normalized artifact type vocabulary value.
type ArtifactType string

const (
	ArtifactTypeSTEP    ArtifactType = "step"
	ArtifactTypeCSV     ArtifactType = "csv"
	ArtifactTypePDF     ArtifactType = "pdf"
	ArtifactTypeJSON    ArtifactType = "json"
	ArtifactTypeLog     ArtifactType = "log"
	ArtifactTypeUnknown ArtifactType = "unknown"
)

// ArtifactDeclaredOutput captures normalized declared output provenance material.
type ArtifactDeclaredOutput struct {
	OutputID   string `json:"outputId,omitempty"`
	OutputName string `json:"outputName,omitempty"`
	OutputType string `json:"outputType,omitempty"`
	Format     string `json:"format,omitempty"`
	ObjectRef  string `json:"objectRef,omitempty"`
}

// ArtifactLinkage captures normalized job/product/step linkage material.
type ArtifactLinkage struct {
	JobID      string `json:"jobId,omitempty"`
	ProductKey string `json:"productKey,omitempty"`
	StepRef    string `json:"stepRef,omitempty"`
}

// ArtifactEvidence captures normalized artifact evidence references.
type ArtifactEvidence struct {
	SourceKind   string `json:"sourceKind,omitempty"`
	SourceRef    string `json:"sourceRef,omitempty"`
	DigestSHA256 string `json:"digestSha256,omitempty"`
}

// ArtifactSummary captures normalized artifact payload material.
type ArtifactSummary struct {
	Class          ArtifactClass          `json:"class"`
	Type           ArtifactType           `json:"type"`
	URI            string                 `json:"uri,omitempty"`
	Path           string                 `json:"path,omitempty"`
	Filename       string                 `json:"filename,omitempty"`
	MimeType       string                 `json:"mimeType,omitempty"`
	SizeBytes      int64                  `json:"sizeBytes,omitempty"`
	ChecksumSHA256 string                 `json:"checksumSha256,omitempty"`
	DeclaredOutput ArtifactDeclaredOutput `json:"declaredOutput"`
	Linkage        ArtifactLinkage        `json:"linkage"`
	Evidence       ArtifactEvidence       `json:"evidence"`
}

// ArtifactRecord is the Engine-produced normalized artifact record contract payload.
type ArtifactRecord struct {
	Family     Family          `json:"family"`
	Version    string          `json:"version"`
	RecordKey  string          `json:"recordKey"`
	Identity   Identity        `json:"identity"`
	Provenance Provenance      `json:"provenance"`
	Artifact   ArtifactSummary `json:"artifact"`
}

// ArtifactRecordInput carries caller material used to build a normalized artifact record.
type ArtifactRecordInput struct {
	RecordKey  string
	Provenance Provenance
	Artifact   ArtifactSummary
}

type canonicalArtifactDeclaredOutput struct {
	OutputID   string `json:"outputId,omitempty"`
	OutputName string `json:"outputName,omitempty"`
	OutputType string `json:"outputType,omitempty"`
	Format     string `json:"format,omitempty"`
	ObjectRef  string `json:"objectRef,omitempty"`
}

type canonicalArtifactLinkage struct {
	JobID      string `json:"jobId,omitempty"`
	ProductKey string `json:"productKey,omitempty"`
	StepRef    string `json:"stepRef,omitempty"`
}

type canonicalArtifactEvidence struct {
	SourceKind   string `json:"sourceKind,omitempty"`
	SourceRef    string `json:"sourceRef,omitempty"`
	DigestSHA256 string `json:"digestSha256,omitempty"`
}

type canonicalArtifactSummary struct {
	Class          string                          `json:"class"`
	Type           string                          `json:"type"`
	URI            string                          `json:"uri,omitempty"`
	Path           string                          `json:"path,omitempty"`
	Filename       string                          `json:"filename,omitempty"`
	MimeType       string                          `json:"mimeType,omitempty"`
	SizeBytes      int64                           `json:"sizeBytes,omitempty"`
	ChecksumSHA256 string                          `json:"checksumSha256,omitempty"`
	DeclaredOutput canonicalArtifactDeclaredOutput `json:"declaredOutput"`
	Linkage        canonicalArtifactLinkage        `json:"linkage"`
	Evidence       canonicalArtifactEvidence       `json:"evidence"`
}

// BuildArtifactRecord normalizes input, derives identity, and returns a validated artifact record.
func BuildArtifactRecord(input ArtifactRecordInput) (ArtifactRecord, error) {
	normalized := normalizeArtifactRecordInput(input)

	identity, err := deriveArtifactIdentity(normalized)
	if err != nil {
		return ArtifactRecord{}, err
	}

	record := ArtifactRecord{
		Family:     FamilyArtifact,
		Version:    CurrentVersion,
		RecordKey:  normalized.RecordKey,
		Identity:   identity,
		Provenance: normalized.Provenance,
		Artifact:   normalized.Artifact,
	}

	if err := ValidateArtifactRecord(record); err != nil {
		return ArtifactRecord{}, err
	}

	return record, nil
}

// NormalizeArtifactRecord returns a deterministic, copy-safe normalized artifact record.
func NormalizeArtifactRecord(record ArtifactRecord) ArtifactRecord {
	return ArtifactRecord{
		Family:     NormalizeFamily(record.Family),
		Version:    trimString(record.Version),
		RecordKey:  trimString(record.RecordKey),
		Identity:   record.Identity,
		Provenance: NormalizeProvenance(record.Provenance),
		Artifact:   normalizeArtifactSummary(record.Artifact),
	}
}

// ValidateArtifactRecord rejects malformed normalized artifact record material.
func ValidateArtifactRecord(record ArtifactRecord) error {
	normalized := NormalizeArtifactRecord(record)

	if normalized.Family != FamilyArtifact {
		return fmt.Errorf("%w: family must be artifact", ErrInvalidArtifactRecord)
	}
	if err := ValidateVersion(normalized.Version); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidArtifactRecord, err)
	}
	if normalized.RecordKey == "" {
		return fmt.Errorf("%w: record key is required", ErrInvalidArtifactRecord)
	}
	if err := ValidateProvenance(normalized.Provenance); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidArtifactRecord, err)
	}
	if err := validateStoredIdentity(normalized.Identity); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidArtifactRecord, err)
	}
	if normalized.Identity.Family != FamilyArtifact {
		return fmt.Errorf("%w: identity family must be artifact", ErrInvalidArtifactRecord)
	}
	if normalized.Identity.Version != CurrentVersion {
		return fmt.Errorf("%w: identity version must be %q", ErrInvalidArtifactRecord, CurrentVersion)
	}
	if err := validateArtifactSummary(normalized.Artifact); err != nil {
		return err
	}

	expectedIdentity, err := deriveArtifactIdentity(normalizedArtifactRecordInput{
		RecordKey:  normalized.RecordKey,
		Provenance: normalized.Provenance,
		Artifact:   normalized.Artifact,
	})
	if err != nil {
		return err
	}
	if normalized.Identity != expectedIdentity {
		return fmt.Errorf("%w: identity does not match derived identity", ErrInvalidArtifactRecord)
	}

	return nil
}

type normalizedArtifactRecordInput struct {
	RecordKey  string
	Provenance Provenance
	Artifact   ArtifactSummary
}

func normalizeArtifactRecordInput(input ArtifactRecordInput) normalizedArtifactRecordInput {
	return normalizedArtifactRecordInput{
		RecordKey:  trimString(input.RecordKey),
		Provenance: NormalizeProvenance(input.Provenance),
		Artifact:   normalizeArtifactSummary(input.Artifact),
	}
}

func normalizeArtifactSummary(summary ArtifactSummary) ArtifactSummary {
	return ArtifactSummary{
		Class:          NormalizeArtifactClass(summary.Class),
		Type:           NormalizeArtifactType(summary.Type),
		URI:            trimString(summary.URI),
		Path:           trimString(summary.Path),
		Filename:       trimString(summary.Filename),
		MimeType:       trimString(summary.MimeType),
		SizeBytes:      summary.SizeBytes,
		ChecksumSHA256: trimString(summary.ChecksumSHA256),
		DeclaredOutput: normalizeArtifactDeclaredOutput(summary.DeclaredOutput),
		Linkage:        normalizeArtifactLinkage(summary.Linkage),
		Evidence:       normalizeArtifactEvidence(summary.Evidence),
	}
}

func normalizeArtifactDeclaredOutput(output ArtifactDeclaredOutput) ArtifactDeclaredOutput {
	return ArtifactDeclaredOutput{
		OutputID:   trimString(output.OutputID),
		OutputName: trimString(output.OutputName),
		OutputType: trimString(output.OutputType),
		Format:     trimString(output.Format),
		ObjectRef:  trimString(output.ObjectRef),
	}
}

func normalizeArtifactLinkage(linkage ArtifactLinkage) ArtifactLinkage {
	return ArtifactLinkage{
		JobID:      trimString(linkage.JobID),
		ProductKey: trimString(linkage.ProductKey),
		StepRef:    trimString(linkage.StepRef),
	}
}

func normalizeArtifactEvidence(evidence ArtifactEvidence) ArtifactEvidence {
	return ArtifactEvidence{
		SourceKind:   trimString(evidence.SourceKind),
		SourceRef:    trimString(evidence.SourceRef),
		DigestSHA256: trimString(evidence.DigestSHA256),
	}
}

// NormalizeArtifactClass trims and preserves unknown values for validation to reject.
func NormalizeArtifactClass(class ArtifactClass) ArtifactClass {
	return ArtifactClass(strings.TrimSpace(string(class)))
}

// NormalizeArtifactType trims and preserves unknown values for validation to reject.
func NormalizeArtifactType(typ ArtifactType) ArtifactType {
	return ArtifactType(strings.TrimSpace(string(typ)))
}

func knownArtifactClass(class ArtifactClass) bool {
	switch NormalizeArtifactClass(class) {
	case ArtifactClassExecutionOutput, ArtifactClassVerified:
		return true
	default:
		return false
	}
}

func knownArtifactType(typ ArtifactType) bool {
	switch NormalizeArtifactType(typ) {
	case ArtifactTypeSTEP, ArtifactTypeCSV, ArtifactTypePDF, ArtifactTypeJSON, ArtifactTypeLog:
		return true
	default:
		return false
	}
}

func hasArtifactLocator(summary ArtifactSummary) bool {
	return summary.URI != "" ||
		summary.Path != "" ||
		summary.Filename != "" ||
		summary.ChecksumSHA256 != "" ||
		summary.Evidence.SourceRef != ""
}

func hasUsefulArtifactEvidenceForUnknownType(summary ArtifactSummary) bool {
	if summary.ChecksumSHA256 != "" || summary.Evidence.DigestSHA256 != "" {
		return true
	}
	if summary.SizeBytes > 0 && (summary.URI != "" || summary.Path != "" || summary.Filename != "") {
		return true
	}
	return false
}

func validateArtifactSummary(summary ArtifactSummary) error {
	if summary.Class == "" {
		return fmt.Errorf("%w: artifact class is required", ErrInvalidArtifactRecord)
	}
	if !knownArtifactClass(summary.Class) {
		return fmt.Errorf("%w: artifact class %q is invalid", ErrInvalidArtifactRecord, summary.Class)
	}
	if summary.Type == "" {
		return fmt.Errorf("%w: artifact type is required", ErrInvalidArtifactRecord)
	}
	normalizedType := NormalizeArtifactType(summary.Type)
	if !knownArtifactType(normalizedType) && normalizedType != ArtifactTypeUnknown {
		return fmt.Errorf("%w: artifact type %q is invalid", ErrInvalidArtifactRecord, summary.Type)
	}
	if normalizedType == ArtifactTypeUnknown && !hasUsefulArtifactEvidenceForUnknownType(summary) {
		return fmt.Errorf("%w: artifact type unknown requires checksum, evidence digest, or locator with positive size", ErrInvalidArtifactRecord)
	}
	if summary.SizeBytes < 0 {
		return fmt.Errorf("%w: artifact sizeBytes must not be negative", ErrInvalidArtifactRecord)
	}
	if err := validateArtifactDigestSHA256("artifact.checksumSha256", summary.ChecksumSHA256); err != nil {
		return err
	}
	if err := validateArtifactDigestSHA256("artifact.evidence.digestSha256", summary.Evidence.DigestSHA256); err != nil {
		return err
	}
	if !hasArtifactLocator(summary) {
		return fmt.Errorf("%w: artifact requires at least one durable locator or evidence field", ErrInvalidArtifactRecord)
	}
	return nil
}

func validateArtifactDigestSHA256(field, digest string) error {
	if digest == "" {
		return nil
	}
	if len(digest) != 64 || !isLowerHex64(digest) {
		return fmt.Errorf("%w: %s must be a 64-character lowercase hexadecimal digest", ErrInvalidArtifactRecord, field)
	}
	return nil
}

func deriveArtifactIdentity(input normalizedArtifactRecordInput) (Identity, error) {
	parts, err := artifactIdentityParts(input)
	if err != nil {
		return Identity{}, err
	}

	identity, err := DeriveIdentity(IdentityInput{
		Family:     FamilyArtifact,
		Version:    CurrentVersion,
		RecordKey:  input.RecordKey,
		Provenance: input.Provenance,
		Parts:      parts,
	})
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %w", ErrInvalidArtifactRecord, err)
	}

	return identity, nil
}

func artifactIdentityParts(input normalizedArtifactRecordInput) ([]IdentityPart, error) {
	artifactJSON, err := json.Marshal(canonicalArtifactSummaryFromNormalized(input.Artifact))
	if err != nil {
		return nil, fmt.Errorf("%w: marshal artifact identity material: %v", ErrInvalidArtifactRecord, err)
	}

	return []IdentityPart{
		{Key: "artifact", Value: string(artifactJSON)},
	}, nil
}

func canonicalArtifactSummaryFromNormalized(summary ArtifactSummary) canonicalArtifactSummary {
	return canonicalArtifactSummary{
		Class:          string(summary.Class),
		Type:           string(summary.Type),
		URI:            summary.URI,
		Path:           summary.Path,
		Filename:       summary.Filename,
		MimeType:       summary.MimeType,
		SizeBytes:      summary.SizeBytes,
		ChecksumSHA256: summary.ChecksumSHA256,
		DeclaredOutput: canonicalArtifactDeclaredOutput{
			OutputID:   summary.DeclaredOutput.OutputID,
			OutputName: summary.DeclaredOutput.OutputName,
			OutputType: summary.DeclaredOutput.OutputType,
			Format:     summary.DeclaredOutput.Format,
			ObjectRef:  summary.DeclaredOutput.ObjectRef,
		},
		Linkage: canonicalArtifactLinkage{
			JobID:      summary.Linkage.JobID,
			ProductKey: summary.Linkage.ProductKey,
			StepRef:    summary.Linkage.StepRef,
		},
		Evidence: canonicalArtifactEvidence{
			SourceKind:   summary.Evidence.SourceKind,
			SourceRef:    summary.Evidence.SourceRef,
			DigestSHA256: summary.Evidence.DigestSHA256,
		},
	}
}
