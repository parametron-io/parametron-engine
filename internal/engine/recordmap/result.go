package recordmap

import (
	"fmt"
	"strings"

	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordpackage"
)

const (
	runtimeResultSchemaVersion = "1.0"
	runtimeResultEvidenceKind  = "runtime-result"
)

var runtimeResultEvidenceRef = recordpackage.RawRuntimeResultContractPath()

// RuntimeResult carries FreeCAD/runner result.json material supplied by the caller.
type RuntimeResult struct {
	SchemaVersion string                  `json:"schemaVersion,omitempty"`
	Status        string                  `json:"status"`
	Artifacts     []RuntimeResultArtifact `json:"artifacts,omitempty"`
	Error         *RuntimeResultError     `json:"error,omitempty"`
}

// RuntimeResultArtifact carries one runtime artifact entry from result.json.
type RuntimeResultArtifact struct {
	Type     string `json:"type"`
	Filename string `json:"filename"`
	Path     string `json:"path"`
}

// RuntimeResultError carries runtime failure material from result.json.
type RuntimeResultError struct {
	Classification string `json:"classification"`
	Message        string `json:"message"`
}

// RuntimeResultMappingLinkage carries caller-supplied deterministic linkage material.
// The mapper must not infer missing job/product/step identity from PDM or runtime paths.
type RuntimeResultMappingLinkage struct {
	JobID      string
	ProductKey string
	StepRef    string
}

// RuntimeResultMappingInput carries runtime result material and caller-supplied record identity.
type RuntimeResultMappingInput struct {
	Result RuntimeResult

	// Required stable key material supplied by the caller.
	RecordKey string

	// Optional base provenance supplied by the caller.
	Provenance recordcontract.Provenance

	// Optional deterministic linkage supplied by the caller.
	Linkage RuntimeResultMappingLinkage

	// Optional digest of raw/runtime/result.json.
	// If present, it must be a lowercase 64-character SHA-256 hex digest.
	EvidenceDigestSHA256 string
}

// RuntimeResultMappingOutput carries an optional normalized failure record.
type RuntimeResultMappingOutput struct {
	FailureRecord *recordcontract.FailureRecord
}

// MapRuntimeResult converts runtime result material into an optional normalized failure record.
func MapRuntimeResult(input RuntimeResultMappingInput) (RuntimeResultMappingOutput, error) {
	failureRecord, err := MapRuntimeResultToFailureRecord(input)
	if err != nil {
		return RuntimeResultMappingOutput{}, err
	}

	return RuntimeResultMappingOutput{
		FailureRecord: failureRecord,
	}, nil
}

// MapRuntimeResultToFailureRecord converts runtime result material into a normalized failure record.
// It returns nil when the runtime result status is successful.
func MapRuntimeResultToFailureRecord(input RuntimeResultMappingInput) (*recordcontract.FailureRecord, error) {
	if err := validateRuntimeResultMappingInput(input); err != nil {
		return nil, err
	}

	status, err := normalizeRuntimeResultStatus(input.Result.Status)
	if err != nil {
		return nil, err
	}
	if status == "success" {
		return nil, nil
	}

	summary, err := mapRuntimeResultFailureSummary(input)
	if err != nil {
		return nil, err
	}

	provenance, err := enrichRuntimeResultProvenance(input)
	if err != nil {
		return nil, err
	}

	record, err := recordcontract.BuildFailureRecord(recordcontract.FailureRecordInput{
		RecordKey:  strings.TrimSpace(input.RecordKey),
		Provenance: provenance,
		Failure:    summary,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: build failure record: %w", ErrInvalidRuntimeResultMapping, err)
	}

	return &record, nil
}

func validateRuntimeResultMappingInput(input RuntimeResultMappingInput) error {
	if strings.TrimSpace(input.RecordKey) == "" {
		return fmt.Errorf("%w: record key is required", ErrInvalidRuntimeResultMapping)
	}
	if err := validateRuntimeResultDigestSHA256("evidenceDigestSha256", input.EvidenceDigestSHA256); err != nil {
		return err
	}
	return validateRuntimeResultSchemaVersion(input.Result.SchemaVersion)
}

func validateRuntimeResultSchemaVersion(version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		return nil
	}
	if version != runtimeResultSchemaVersion {
		return fmt.Errorf("%w: unsupported runtime result schema version %q", ErrInvalidRuntimeResultMapping, version)
	}
	return nil
}

func normalizeRuntimeResultStatus(status string) (string, error) {
	status = strings.TrimSpace(status)
	switch status {
	case "success", "failure":
		return status, nil
	default:
		return "", fmt.Errorf("%w: unsupported runtime result status %q", ErrInvalidRuntimeResultMapping, status)
	}
}

func mapRuntimeResultFailureSummary(input RuntimeResultMappingInput) (recordcontract.FailureSummary, error) {
	if input.Result.Error == nil {
		return recordcontract.FailureSummary{}, fmt.Errorf("%w: failure error is required", ErrInvalidRuntimeResultMapping)
	}

	message := strings.TrimSpace(input.Result.Error.Message)
	if message == "" {
		return recordcontract.FailureSummary{}, fmt.Errorf("%w: failure message is required", ErrInvalidRuntimeResultMapping)
	}

	failureClass, stage, code := mapRuntimeResultFailureClassAndStage(input.Result.Error.Classification)
	digest := strings.TrimSpace(input.EvidenceDigestSHA256)

	return recordcontract.FailureSummary{
		Class:    failureClass,
		Severity: recordcontract.FailureSeverityError,
		Stage:    stage,
		Code:     code,
		Message:  message,
		Linkage:  runtimeResultLinkage(input.Linkage),
		Evidence: runtimeResultFailureEvidence(digest),
	}, nil
}

func mapRuntimeResultFailureClassAndStage(classification string) (recordcontract.FailureClass, recordcontract.FailureStage, string) {
	classification = strings.TrimSpace(classification)
	switch classification {
	case "preflight_validation_error":
		return recordcontract.FailureClassValidation, recordcontract.FailureStageValidation, classification
	case "working_copy_materialization_error":
		return recordcontract.FailureClassRuntime, recordcontract.FailureStageRuntime, classification
	case "document_open_error":
		return recordcontract.FailureClassAdapter, recordcontract.FailureStageAdapter, classification
	case "assembly_mutations_error", "part_mutations_error", "recompute_error":
		return recordcontract.FailureClassRuntime, recordcontract.FailureStageRuntime, classification
	case "bom_generation_error", "geometry_export_error", "drawing_update_error", "drawing_export_error":
		return recordcontract.FailureClassExport, recordcontract.FailureStageExport, classification
	case "internal_error":
		return recordcontract.FailureClassInternal, recordcontract.FailureStageRuntime, classification
	case "":
		return recordcontract.FailureClassRuntime, recordcontract.FailureStageRuntime, ""
	default:
		return recordcontract.FailureClassRuntime, recordcontract.FailureStageRuntime, classification
	}
}

func enrichRuntimeResultProvenance(input RuntimeResultMappingInput) (recordcontract.Provenance, error) {
	provenance := copyProvenance(input.Provenance)

	evidence, err := appendRuntimeResultEvidence(provenance.Evidence, strings.TrimSpace(input.EvidenceDigestSHA256))
	if err != nil {
		return recordcontract.Provenance{}, err
	}
	provenance.Evidence = evidence

	normalized := recordcontract.NormalizeProvenance(provenance)
	if err := recordcontract.ValidateProvenance(normalized); err != nil {
		return recordcontract.Provenance{}, fmt.Errorf("%w: validate provenance: %w", ErrInvalidRuntimeResultMapping, err)
	}

	return normalized, nil
}

func appendRuntimeResultEvidence(existing []recordcontract.EvidenceReference, digest string) ([]recordcontract.EvidenceReference, error) {
	out := copyEvidenceReferences(existing)
	for i, item := range out {
		if strings.TrimSpace(item.Kind) != runtimeResultEvidenceKind || strings.TrimSpace(item.Ref) != runtimeResultEvidenceRef {
			continue
		}
		existingDigest := strings.TrimSpace(item.DigestSHA256)
		if existingDigest != "" && digest != "" && existingDigest != digest {
			return nil, fmt.Errorf("%w: conflicting digest for runtime result evidence reference", ErrInvalidRuntimeResultMapping)
		}
		if existingDigest == "" && digest != "" {
			out[i].DigestSHA256 = digest
		}
		return out, nil
	}

	out = append(out, recordcontract.EvidenceReference{
		Kind:         runtimeResultEvidenceKind,
		Ref:          runtimeResultEvidenceRef,
		DigestSHA256: digest,
	})
	return out, nil
}

func runtimeResultFailureEvidence(digest string) []recordcontract.FailureEvidence {
	return []recordcontract.FailureEvidence{
		{
			SourceKind:   runtimeResultEvidenceKind,
			SourceRef:    runtimeResultEvidenceRef,
			DigestSHA256: digest,
		},
	}
}

func runtimeResultLinkage(linkage RuntimeResultMappingLinkage) recordcontract.FailureLinkage {
	return recordcontract.FailureLinkage{
		JobID:      strings.TrimSpace(linkage.JobID),
		ProductKey: strings.TrimSpace(linkage.ProductKey),
		StepRef:    strings.TrimSpace(linkage.StepRef),
	}
}

func validateRuntimeResultDigestSHA256(field, digest string) error {
	digest = strings.TrimSpace(digest)
	if digest == "" {
		return nil
	}
	if len(digest) != 64 || !isMappingLowerHex64(digest) {
		return fmt.Errorf("%w: %s must be a 64-character lowercase hexadecimal digest", ErrInvalidRuntimeResultMapping, field)
	}
	return nil
}
