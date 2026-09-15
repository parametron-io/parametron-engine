package recordmap

import (
	"fmt"
	"strings"

	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordpackage"
	"parametron/internal/engine/verification"
)

const (
	verificationEvidenceKind = "verification"
)

var verificationEvidenceRef = recordpackage.RawVerificationContractPath()

// VerificationMappingLinkage carries caller-supplied deterministic linkage material.
// The mapper must not infer missing job/product/step identity from PDM or runtime paths.
type VerificationMappingLinkage struct {
	JobID      string
	ProductKey string
	StepRef    string
}

// VerificationMappingInput carries verification result material and caller-supplied record identity.
type VerificationMappingInput struct {
	Result verification.Result

	// Required stable key material supplied by the caller.
	// Do not invent source revision, PDM identity, job identity, product identity, or runtime path identity.
	RecordKey string

	// Optional base provenance supplied by the caller.
	// Preserve it and append raw verification evidence.
	Provenance recordcontract.Provenance

	// Optional deterministic linkage supplied by the caller.
	Linkage VerificationMappingLinkage

	// Optional digest of raw/verification/prm.verification.json request evidence.
	// If present, it must be a lowercase 64-character SHA-256 hex digest.
	EvidenceDigestSHA256 string
}

// VerificationMappingOutput carries a normalized verification record.
type VerificationMappingOutput struct {
	VerificationRecord recordcontract.VerificationRecord
}

// MapVerification converts verification result material into a normalized verification record.
func MapVerification(input VerificationMappingInput) (VerificationMappingOutput, error) {
	record, err := MapVerificationToVerificationRecord(input)
	if err != nil {
		return VerificationMappingOutput{}, err
	}

	return VerificationMappingOutput{
		VerificationRecord: record,
	}, nil
}

// MapVerificationToVerificationRecord converts verification result material into a normalized verification record.
func MapVerificationToVerificationRecord(input VerificationMappingInput) (recordcontract.VerificationRecord, error) {
	if err := validateVerificationMappingInput(input); err != nil {
		return recordcontract.VerificationRecord{}, err
	}

	provenance, err := enrichVerificationProvenance(input)
	if err != nil {
		return recordcontract.VerificationRecord{}, err
	}

	summary, err := mapVerificationSummary(input)
	if err != nil {
		return recordcontract.VerificationRecord{}, err
	}

	record, err := recordcontract.BuildVerificationRecord(recordcontract.VerificationRecordInput{
		RecordKey:    strings.TrimSpace(input.RecordKey),
		Provenance:   provenance,
		Verification: summary,
	})
	if err != nil {
		return recordcontract.VerificationRecord{}, fmt.Errorf("%w: build verification record: %w", ErrInvalidVerificationMapping, err)
	}

	return record, nil
}

func validateVerificationMappingInput(input VerificationMappingInput) error {
	if strings.TrimSpace(input.RecordKey) == "" {
		return fmt.Errorf("%w: record key is required", ErrInvalidVerificationMapping)
	}
	if err := validateVerificationDigestSHA256("evidenceDigestSha256", input.EvidenceDigestSHA256); err != nil {
		return err
	}
	if _, err := mapVerificationOutcome(input.Result.Status); err != nil {
		return err
	}
	return nil
}

func mapVerificationSummary(input VerificationMappingInput) (recordcontract.VerificationSummary, error) {
	outcome, err := mapVerificationOutcome(input.Result.Status)
	if err != nil {
		return recordcontract.VerificationSummary{}, err
	}

	digest := strings.TrimSpace(input.EvidenceDigestSHA256)
	evidence := verificationRecordEvidence(digest)

	failureClass, err := mapTopLevelVerificationFailureClass(outcome, input.Result.Failure)
	if err != nil {
		return recordcontract.VerificationSummary{}, err
	}

	categories, err := mapVerificationCategoryResults(input.Result, digest)
	if err != nil {
		return recordcontract.VerificationSummary{}, err
	}

	return recordcontract.VerificationSummary{
		Outcome:      outcome,
		Message:      strings.TrimSpace(input.Result.Message),
		FailureClass: failureClass,
		Linkage:      verificationLinkage(input.Linkage),
		Categories:   categories,
		Evidence:     evidence,
	}, nil
}

func mapVerificationCategoryResults(result verification.Result, digest string) ([]recordcontract.VerificationCategoryResult, error) {
	ordered := []struct {
		category recordcontract.VerificationCategory
		item     verification.CategoryResult
	}{
		{recordcontract.VerificationCategoryComponents, result.Categories.Components},
		{recordcontract.VerificationCategoryParameters, result.Categories.Parameters},
		{recordcontract.VerificationCategoryMetadata, result.Categories.Metadata},
		{recordcontract.VerificationCategoryReferences, result.Categories.References},
	}

	out := make([]recordcontract.VerificationCategoryResult, 0, len(ordered))
	for _, entry := range ordered {
		mapped, err := mapVerificationCategoryResult(entry.category, entry.item, digest, result)
		if err != nil {
			return nil, err
		}
		out = append(out, mapped)
	}

	return out, nil
}

func mapVerificationCategoryResult(
	category recordcontract.VerificationCategory,
	result verification.CategoryResult,
	digest string,
	topResult verification.Result,
) (recordcontract.VerificationCategoryResult, error) {
	outcome, err := mapCategoryOutcome(result.Status)
	if err != nil {
		return recordcontract.VerificationCategoryResult{}, err
	}

	failureClass, err := mapCategoryVerificationFailureClass(category, outcome, result, topResult)
	if err != nil {
		return recordcontract.VerificationCategoryResult{}, err
	}

	return recordcontract.VerificationCategoryResult{
		Category:     category,
		Enabled:      result.Enabled,
		Outcome:      outcome,
		Message:      strings.TrimSpace(result.Message),
		FailureClass: failureClass,
		Evidence:     verificationRecordEvidence(digest),
	}, nil
}

func mapTopLevelVerificationFailureClass(
	outcome recordcontract.VerificationOutcome,
	failure verification.FailureClass,
) (recordcontract.VerificationFailureClass, error) {
	if outcome != recordcontract.VerificationOutcomeFail {
		return "", nil
	}

	mapped, err := mapVerificationFailureClass(failure)
	if err != nil {
		return "", err
	}
	if mapped == "" {
		return recordcontract.VerificationFailureClassInternalError, nil
	}
	return mapped, nil
}

func mapCategoryVerificationFailureClass(
	category recordcontract.VerificationCategory,
	outcome recordcontract.VerificationOutcome,
	result verification.CategoryResult,
	topResult verification.Result,
) (recordcontract.VerificationFailureClass, error) {
	if outcome != recordcontract.VerificationOutcomeFail {
		return "", nil
	}

	failureClass := result.FailureClass()
	if failureClass == verification.FailureClassNone {
		if isFirstEnabledFailedCategory(topResult, category) {
			failureClass = topResult.Failure
		}
	}

	mapped, err := mapVerificationFailureClass(failureClass)
	if err != nil {
		return "", err
	}
	if mapped == "" {
		return recordcontract.VerificationFailureClassInternalError, nil
	}
	return mapped, nil
}

func isFirstEnabledFailedCategory(result verification.Result, category recordcontract.VerificationCategory) bool {
	ordered := []struct {
		category recordcontract.VerificationCategory
		item     verification.CategoryResult
	}{
		{recordcontract.VerificationCategoryComponents, result.Categories.Components},
		{recordcontract.VerificationCategoryParameters, result.Categories.Parameters},
		{recordcontract.VerificationCategoryMetadata, result.Categories.Metadata},
		{recordcontract.VerificationCategoryReferences, result.Categories.References},
	}

	for _, entry := range ordered {
		if !entry.item.Enabled || entry.item.Status != verification.CategoryStatusFail {
			continue
		}
		return entry.category == category
	}

	return false
}

func mapVerificationOutcome(status verification.Status) (recordcontract.VerificationOutcome, error) {
	switch status {
	case verification.StatusPass:
		return recordcontract.VerificationOutcomePass, nil
	case verification.StatusFail:
		return recordcontract.VerificationOutcomeFail, nil
	default:
		return "", fmt.Errorf("%w: unsupported verification status %q", ErrInvalidVerificationMapping, status)
	}
}

func mapCategoryOutcome(status verification.CategoryStatus) (recordcontract.VerificationOutcome, error) {
	switch status {
	case verification.CategoryStatusPass:
		return recordcontract.VerificationOutcomePass, nil
	case verification.CategoryStatusFail:
		return recordcontract.VerificationOutcomeFail, nil
	case verification.CategoryStatusSkipped:
		return recordcontract.VerificationOutcomeSkipped, nil
	default:
		return "", fmt.Errorf("%w: unsupported verification category status %q", ErrInvalidVerificationMapping, status)
	}
}

func mapVerificationFailureClass(class verification.FailureClass) (recordcontract.VerificationFailureClass, error) {
	switch class {
	case verification.FailureClassNone:
		return "", nil
	case verification.FailureClassContractInvalid:
		return recordcontract.VerificationFailureClassContractInvalid, nil
	case verification.FailureClassObservedInvalid:
		return recordcontract.VerificationFailureClassObservedInvalid, nil
	case verification.FailureClassComponentMismatch:
		return recordcontract.VerificationFailureClassComponentMismatch, nil
	case verification.FailureClassParameterMismatch:
		return recordcontract.VerificationFailureClassParameterMismatch, nil
	case verification.FailureClassMetadataMismatch:
		return recordcontract.VerificationFailureClassMetadataMismatch, nil
	case verification.FailureClassReferenceMismatch:
		return recordcontract.VerificationFailureClassReferenceMismatch, nil
	case verification.FailureClassRequiredObservationMissing:
		return recordcontract.VerificationFailureClassRequiredObservationMissing, nil
	case verification.FailureClassInternalError:
		return recordcontract.VerificationFailureClassInternalError, nil
	default:
		return "", fmt.Errorf("%w: unsupported verification failure class %q", ErrInvalidVerificationMapping, class)
	}
}

func enrichVerificationProvenance(input VerificationMappingInput) (recordcontract.Provenance, error) {
	provenance := copyProvenance(input.Provenance)

	evidence, err := appendVerificationEvidence(provenance.Evidence, strings.TrimSpace(input.EvidenceDigestSHA256))
	if err != nil {
		return recordcontract.Provenance{}, err
	}
	provenance.Evidence = evidence

	normalized := recordcontract.NormalizeProvenance(provenance)
	if err := recordcontract.ValidateProvenance(normalized); err != nil {
		return recordcontract.Provenance{}, fmt.Errorf("%w: validate provenance: %w", ErrInvalidVerificationMapping, err)
	}

	return normalized, nil
}

func appendVerificationEvidence(existing []recordcontract.EvidenceReference, digest string) ([]recordcontract.EvidenceReference, error) {
	out := copyEvidenceReferences(existing)
	for i, item := range out {
		if strings.TrimSpace(item.Kind) != verificationEvidenceKind || strings.TrimSpace(item.Ref) != verificationEvidenceRef {
			continue
		}
		existingDigest := strings.TrimSpace(item.DigestSHA256)
		if existingDigest != "" && digest != "" && existingDigest != digest {
			return nil, fmt.Errorf("%w: conflicting digest for verification evidence reference", ErrInvalidVerificationMapping)
		}
		if existingDigest == "" && digest != "" {
			out[i].DigestSHA256 = digest
		}
		return out, nil
	}

	out = append(out, recordcontract.EvidenceReference{
		Kind:         verificationEvidenceKind,
		Ref:          verificationEvidenceRef,
		DigestSHA256: digest,
	})
	return out, nil
}

func verificationRecordEvidence(digest string) []recordcontract.VerificationEvidence {
	return []recordcontract.VerificationEvidence{
		{
			SourceKind:   verificationEvidenceKind,
			SourceRef:    verificationEvidenceRef,
			DigestSHA256: digest,
		},
	}
}

func verificationLinkage(linkage VerificationMappingLinkage) recordcontract.VerificationLinkage {
	return recordcontract.VerificationLinkage{
		JobID:      strings.TrimSpace(linkage.JobID),
		ProductKey: strings.TrimSpace(linkage.ProductKey),
		StepRef:    strings.TrimSpace(linkage.StepRef),
	}
}

func validateVerificationDigestSHA256(field, digest string) error {
	digest = strings.TrimSpace(digest)
	if digest == "" {
		return nil
	}
	if len(digest) != 64 || !isMappingLowerHex64(digest) {
		return fmt.Errorf("%w: %s must be a 64-character lowercase hexadecimal digest", ErrInvalidVerificationMapping, field)
	}
	return nil
}
