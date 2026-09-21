package recordmap

import (
	"fmt"
	"strings"
	"time"

	"parametron/internal/engine/executor"
	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordpackage"
)

const cadRuntimeResultEvidenceKind = "runtime-result"

var cadRuntimeResultEvidenceRef = recordpackage.RawRuntimeResultContractPath()

type CADRuntimeFailureMappingInput struct {
	Failure              executor.CADRuntimeFailureOutcome
	RecordKey            string
	Provenance           recordcontract.Provenance
	Linkage              recordcontract.FailureLinkage
	EvidenceDigestSHA256 string
	// RetryCount and OccurredAt are Engine-owned operational context supplied by
	// the caller (from the run report); they are never derived from the runtime.
	RetryCount int
	OccurredAt *time.Time
}

// MapCADRuntimeFailure maps already interpreted, adapter-neutral CAD runtime
// failure semantics. Adapter-native classification must happen before this
// boundary.
func MapCADRuntimeFailure(input CADRuntimeFailureMappingInput) (recordcontract.FailureRecord, error) {
	if err := validateCADRuntimeFailureMappingInput(input); err != nil {
		return recordcontract.FailureRecord{}, err
	}
	digest := strings.TrimSpace(input.EvidenceDigestSHA256)
	provenance, err := enrichCADRuntimeFailureProvenance(input.Provenance, digest)
	if err != nil {
		return recordcontract.FailureRecord{}, err
	}
	record, err := recordcontract.BuildFailureRecord(recordcontract.FailureRecordInput{
		RecordKey:  strings.TrimSpace(input.RecordKey) + failureRecordKeySuffix,
		Provenance: provenance,
		Failure: recordcontract.FailureSummary{
			Class:      recordcontract.FailureClass(strings.TrimSpace(input.Failure.Class)),
			Severity:   recordcontract.FailureSeverityError,
			Stage:      recordcontract.FailureStage(strings.TrimSpace(input.Failure.SemanticStage)),
			Code:       strings.TrimSpace(input.Failure.Code),
			Message:    strings.TrimSpace(input.Failure.Message),
			RetryCount: input.RetryCount,
			OccurredAt: optionalTimePtr(input.OccurredAt),
			Linkage: recordcontract.FailureLinkage{
				JobID:      strings.TrimSpace(input.Linkage.JobID),
				ProductKey: strings.TrimSpace(input.Linkage.ProductKey),
				StepRef:    strings.TrimSpace(input.Linkage.StepRef),
			},
			Evidence: []recordcontract.FailureEvidence{{
				SourceKind:   cadRuntimeResultEvidenceKind,
				SourceRef:    cadRuntimeResultEvidenceRef,
				DigestSHA256: digest,
			}},
		},
	})
	if err != nil {
		return recordcontract.FailureRecord{}, fmt.Errorf("%w: build failure record: %w", ErrInvalidCADRuntimeFailureMapping, err)
	}
	return record, nil
}

func validateCADRuntimeFailureMappingInput(input CADRuntimeFailureMappingInput) error {
	if strings.TrimSpace(input.RecordKey) == "" {
		return fmt.Errorf("%w: record key is required", ErrInvalidCADRuntimeFailureMapping)
	}
	if !input.Failure.RuntimeNative {
		return fmt.Errorf("%w: failure is not runtime-native", ErrInvalidCADRuntimeFailureMapping)
	}
	if strings.TrimSpace(input.Failure.Class) == "" || strings.TrimSpace(input.Failure.SemanticStage) == "" {
		return fmt.Errorf("%w: semantic class and stage are required", ErrInvalidCADRuntimeFailureMapping)
	}
	if strings.TrimSpace(input.Failure.Code) == "" {
		return fmt.Errorf("%w: native failure code is required", ErrInvalidCADRuntimeFailureMapping)
	}
	if strings.TrimSpace(input.Failure.Message) == "" {
		return fmt.Errorf("%w: native failure message is required", ErrInvalidCADRuntimeFailureMapping)
	}
	return validateCADRuntimeFailureDigest(input.EvidenceDigestSHA256)
}

func validateCADRuntimeFailureDigest(digest string) error {
	digest = strings.TrimSpace(digest)
	if digest == "" {
		return nil
	}
	if len(digest) != 64 || !isMappingLowerHex64(digest) {
		return fmt.Errorf("%w: evidenceDigestSha256 must be a 64-character lowercase hexadecimal digest", ErrInvalidCADRuntimeFailureMapping)
	}
	return nil
}

func enrichCADRuntimeFailureProvenance(base recordcontract.Provenance, digest string) (recordcontract.Provenance, error) {
	provenance := copyProvenance(base)
	for i, item := range provenance.Evidence {
		if strings.TrimSpace(item.Kind) != cadRuntimeResultEvidenceKind || strings.TrimSpace(item.Ref) != cadRuntimeResultEvidenceRef {
			continue
		}
		existingDigest := strings.TrimSpace(item.DigestSHA256)
		if existingDigest != "" && digest != "" && existingDigest != digest {
			return recordcontract.Provenance{}, fmt.Errorf("%w: conflicting digest for runtime result evidence reference", ErrInvalidCADRuntimeFailureMapping)
		}
		if existingDigest == "" && digest != "" {
			provenance.Evidence[i].DigestSHA256 = digest
		}
		return validateCADRuntimeFailureProvenance(provenance)
	}
	provenance.Evidence = append(provenance.Evidence, recordcontract.EvidenceReference{
		Kind: cadRuntimeResultEvidenceKind, Ref: cadRuntimeResultEvidenceRef, DigestSHA256: digest,
	})
	return validateCADRuntimeFailureProvenance(provenance)
}

func validateCADRuntimeFailureProvenance(provenance recordcontract.Provenance) (recordcontract.Provenance, error) {
	normalized := recordcontract.NormalizeProvenance(provenance)
	if err := recordcontract.ValidateProvenance(normalized); err != nil {
		return recordcontract.Provenance{}, fmt.Errorf("%w: validate provenance: %w", ErrInvalidCADRuntimeFailureMapping, err)
	}
	return normalized, nil
}
