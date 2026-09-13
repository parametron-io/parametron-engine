package recordcontract

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// VerificationOutcome is a normalized verification outcome vocabulary value.
type VerificationOutcome string

const (
	VerificationOutcomePass    VerificationOutcome = "pass"
	VerificationOutcomeFail    VerificationOutcome = "fail"
	VerificationOutcomeSkipped VerificationOutcome = "skipped"
)

// VerificationCategory is a normalized verification category vocabulary value.
type VerificationCategory string

const (
	VerificationCategoryComponents VerificationCategory = "components"
	VerificationCategoryParameters VerificationCategory = "parameters"
	VerificationCategoryMetadata   VerificationCategory = "metadata"
	VerificationCategoryReferences VerificationCategory = "references"
)

// VerificationFailureClass is a normalized verification failure classification vocabulary value.
type VerificationFailureClass string

const (
	VerificationFailureClassContractInvalid            VerificationFailureClass = "verification_contract_invalid"
	VerificationFailureClassObservedInvalid            VerificationFailureClass = "observed_artifact_invalid"
	VerificationFailureClassComponentMismatch          VerificationFailureClass = "component_mismatch"
	VerificationFailureClassParameterMismatch          VerificationFailureClass = "parameter_mismatch"
	VerificationFailureClassMetadataMismatch           VerificationFailureClass = "metadata_mismatch"
	VerificationFailureClassReferenceMismatch          VerificationFailureClass = "reference_mismatch"
	VerificationFailureClassRequiredObservationMissing VerificationFailureClass = "required_observation_missing"
	VerificationFailureClassInternalError              VerificationFailureClass = "internal_verification_error"
)

// VerificationLinkage captures normalized job/product/step linkage material.
type VerificationLinkage struct {
	JobID      string `json:"jobId,omitempty"`
	ProductKey string `json:"productKey,omitempty"`
	StepRef    string `json:"stepRef,omitempty"`
}

// VerificationEvidence captures normalized verification evidence references.
type VerificationEvidence struct {
	SourceKind   string `json:"sourceKind,omitempty"`
	SourceRef    string `json:"sourceRef,omitempty"`
	DigestSHA256 string `json:"digestSha256,omitempty"`
}

// VerificationCategoryResult captures one normalized verification category outcome.
type VerificationCategoryResult struct {
	Category     VerificationCategory     `json:"category"`
	Enabled      bool                     `json:"enabled"`
	Outcome      VerificationOutcome      `json:"outcome"`
	Message      string                   `json:"message,omitempty"`
	FailureClass VerificationFailureClass `json:"failureClass,omitempty"`
	Evidence     []VerificationEvidence   `json:"evidence"`
}

// VerificationSummary captures normalized verification payload material.
type VerificationSummary struct {
	Outcome      VerificationOutcome          `json:"outcome"`
	Message      string                       `json:"message,omitempty"`
	FailureClass VerificationFailureClass     `json:"failureClass,omitempty"`
	Linkage      VerificationLinkage          `json:"linkage"`
	Categories   []VerificationCategoryResult `json:"categories"`
	Evidence     []VerificationEvidence       `json:"evidence"`
}

// VerificationRecord is the Engine-produced normalized verification record contract payload.
type VerificationRecord struct {
	Family       Family              `json:"family"`
	Version      string              `json:"version"`
	RecordKey    string              `json:"recordKey"`
	Identity     Identity            `json:"identity"`
	Provenance   Provenance          `json:"provenance"`
	Verification VerificationSummary `json:"verification"`
}

// VerificationRecordInput carries caller material used to build a normalized verification record.
type VerificationRecordInput struct {
	RecordKey    string
	Provenance   Provenance
	Verification VerificationSummary
}

type canonicalVerificationLinkage struct {
	JobID      string `json:"jobId,omitempty"`
	ProductKey string `json:"productKey,omitempty"`
	StepRef    string `json:"stepRef,omitempty"`
}

type canonicalVerificationEvidence struct {
	SourceKind   string `json:"sourceKind,omitempty"`
	SourceRef    string `json:"sourceRef,omitempty"`
	DigestSHA256 string `json:"digestSha256,omitempty"`
}

type canonicalVerificationCategoryResult struct {
	Category     string                          `json:"category"`
	Enabled      bool                            `json:"enabled"`
	Outcome      string                          `json:"outcome"`
	Message      string                          `json:"message,omitempty"`
	FailureClass string                          `json:"failureClass,omitempty"`
	Evidence     []canonicalVerificationEvidence `json:"evidence"`
}

type canonicalVerificationSummary struct {
	Outcome      string                                `json:"outcome"`
	Message      string                                `json:"message,omitempty"`
	FailureClass string                                `json:"failureClass,omitempty"`
	Linkage      canonicalVerificationLinkage          `json:"linkage"`
	Categories   []canonicalVerificationCategoryResult `json:"categories"`
	Evidence     []canonicalVerificationEvidence       `json:"evidence"`
}

// BuildVerificationRecord normalizes input, derives identity, and returns a validated verification record.
func BuildVerificationRecord(input VerificationRecordInput) (VerificationRecord, error) {
	normalized := normalizeVerificationRecordInput(input)

	identity, err := deriveVerificationIdentity(normalized)
	if err != nil {
		return VerificationRecord{}, err
	}

	record := VerificationRecord{
		Family:       FamilyVerification,
		Version:      CurrentVersion,
		RecordKey:    normalized.RecordKey,
		Identity:     identity,
		Provenance:   normalized.Provenance,
		Verification: normalized.Verification,
	}

	if err := ValidateVerificationRecord(record); err != nil {
		return VerificationRecord{}, err
	}

	return record, nil
}

// NormalizeVerificationRecord returns a deterministic, copy-safe normalized verification record.
func NormalizeVerificationRecord(record VerificationRecord) VerificationRecord {
	return VerificationRecord{
		Family:       NormalizeFamily(record.Family),
		Version:      trimString(record.Version),
		RecordKey:    trimString(record.RecordKey),
		Identity:     record.Identity,
		Provenance:   NormalizeProvenance(record.Provenance),
		Verification: normalizeVerificationSummary(record.Verification),
	}
}

// ValidateVerificationRecord rejects malformed normalized verification record material.
func ValidateVerificationRecord(record VerificationRecord) error {
	normalized := NormalizeVerificationRecord(record)

	if normalized.Family != FamilyVerification {
		return fmt.Errorf("%w: family must be verification", ErrInvalidVerificationRecord)
	}
	if err := ValidateVersion(normalized.Version); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidVerificationRecord, err)
	}
	if normalized.RecordKey == "" {
		return fmt.Errorf("%w: record key is required", ErrInvalidVerificationRecord)
	}
	if err := ValidateProvenance(normalized.Provenance); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidVerificationRecord, err)
	}
	if err := validateStoredIdentity(normalized.Identity); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidVerificationRecord, err)
	}
	if normalized.Identity.Family != FamilyVerification {
		return fmt.Errorf("%w: identity family must be verification", ErrInvalidVerificationRecord)
	}
	if normalized.Identity.Version != CurrentVersion {
		return fmt.Errorf("%w: identity version must be %q", ErrInvalidVerificationRecord, CurrentVersion)
	}
	if err := validateVerificationSummary(normalized.Verification); err != nil {
		return err
	}

	expectedIdentity, err := deriveVerificationIdentity(normalizedVerificationRecordInput{
		RecordKey:    normalized.RecordKey,
		Provenance:   normalized.Provenance,
		Verification: normalized.Verification,
	})
	if err != nil {
		return err
	}
	if normalized.Identity != expectedIdentity {
		return fmt.Errorf("%w: identity does not match derived identity", ErrInvalidVerificationRecord)
	}

	return nil
}

// NormalizeVerificationOutcome trims and preserves unknown values for validation to reject.
func NormalizeVerificationOutcome(outcome VerificationOutcome) VerificationOutcome {
	return VerificationOutcome(strings.TrimSpace(string(outcome)))
}

// NormalizeVerificationCategory trims and preserves unknown values for validation to reject.
func NormalizeVerificationCategory(category VerificationCategory) VerificationCategory {
	return VerificationCategory(strings.TrimSpace(string(category)))
}

// NormalizeVerificationFailureClass trims and preserves unknown values for validation to reject.
func NormalizeVerificationFailureClass(class VerificationFailureClass) VerificationFailureClass {
	return VerificationFailureClass(strings.TrimSpace(string(class)))
}

type normalizedVerificationRecordInput struct {
	RecordKey    string
	Provenance   Provenance
	Verification VerificationSummary
}

func normalizeVerificationRecordInput(input VerificationRecordInput) normalizedVerificationRecordInput {
	return normalizedVerificationRecordInput{
		RecordKey:    trimString(input.RecordKey),
		Provenance:   NormalizeProvenance(input.Provenance),
		Verification: normalizeVerificationSummary(input.Verification),
	}
}

func normalizeVerificationSummary(summary VerificationSummary) VerificationSummary {
	return VerificationSummary{
		Outcome:      NormalizeVerificationOutcome(summary.Outcome),
		Message:      trimString(summary.Message),
		FailureClass: NormalizeVerificationFailureClass(summary.FailureClass),
		Linkage:      normalizeVerificationLinkage(summary.Linkage),
		Categories:   normalizeVerificationCategoryResults(summary.Categories),
		Evidence:     normalizeVerificationEvidenceList(summary.Evidence),
	}
}

func normalizeVerificationLinkage(linkage VerificationLinkage) VerificationLinkage {
	return VerificationLinkage{
		JobID:      trimString(linkage.JobID),
		ProductKey: trimString(linkage.ProductKey),
		StepRef:    trimString(linkage.StepRef),
	}
}

func normalizeVerificationEvidence(evidence VerificationEvidence) VerificationEvidence {
	return VerificationEvidence{
		SourceKind:   trimString(evidence.SourceKind),
		SourceRef:    trimString(evidence.SourceRef),
		DigestSHA256: trimString(evidence.DigestSHA256),
	}
}

func normalizeVerificationEvidenceList(evidence []VerificationEvidence) []VerificationEvidence {
	if len(evidence) == 0 {
		return []VerificationEvidence{}
	}

	out := make([]VerificationEvidence, len(evidence))
	for i, item := range evidence {
		out[i] = normalizeVerificationEvidence(item)
	}

	sort.Slice(out, func(i, j int) bool {
		left := out[i]
		right := out[j]
		if left.SourceKind != right.SourceKind {
			return left.SourceKind < right.SourceKind
		}
		if left.SourceRef != right.SourceRef {
			return left.SourceRef < right.SourceRef
		}
		return left.DigestSHA256 < right.DigestSHA256
	})

	return out
}

func normalizeVerificationCategoryResult(result VerificationCategoryResult) VerificationCategoryResult {
	return VerificationCategoryResult{
		Category:     NormalizeVerificationCategory(result.Category),
		Enabled:      result.Enabled,
		Outcome:      NormalizeVerificationOutcome(result.Outcome),
		Message:      trimString(result.Message),
		FailureClass: NormalizeVerificationFailureClass(result.FailureClass),
		Evidence:     normalizeVerificationEvidenceList(result.Evidence),
	}
}

func normalizeVerificationCategoryResults(results []VerificationCategoryResult) []VerificationCategoryResult {
	if len(results) == 0 {
		return []VerificationCategoryResult{}
	}

	out := make([]VerificationCategoryResult, len(results))
	for i, result := range results {
		out[i] = normalizeVerificationCategoryResult(result)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Category < out[j].Category
	})

	return out
}

func knownVerificationOutcome(outcome VerificationOutcome) bool {
	switch NormalizeVerificationOutcome(outcome) {
	case VerificationOutcomePass, VerificationOutcomeFail, VerificationOutcomeSkipped:
		return true
	default:
		return false
	}
}

func knownVerificationCategory(category VerificationCategory) bool {
	switch NormalizeVerificationCategory(category) {
	case VerificationCategoryComponents, VerificationCategoryParameters,
		VerificationCategoryMetadata, VerificationCategoryReferences:
		return true
	default:
		return false
	}
}

func knownVerificationFailureClass(class VerificationFailureClass) bool {
	switch NormalizeVerificationFailureClass(class) {
	case VerificationFailureClassContractInvalid, VerificationFailureClassObservedInvalid,
		VerificationFailureClassComponentMismatch, VerificationFailureClassParameterMismatch,
		VerificationFailureClassMetadataMismatch, VerificationFailureClassReferenceMismatch,
		VerificationFailureClassRequiredObservationMissing, VerificationFailureClassInternalError:
		return true
	default:
		return false
	}
}

func validateVerificationSummary(summary VerificationSummary) error {
	if summary.Outcome == "" {
		return fmt.Errorf("%w: verification outcome is required", ErrInvalidVerificationRecord)
	}
	if !knownVerificationOutcome(summary.Outcome) {
		return fmt.Errorf("%w: verification outcome %q is invalid", ErrInvalidVerificationRecord, summary.Outcome)
	}
	if err := validateVerificationOutcomeFailureClassConsistency(
		"verification",
		summary.Outcome,
		summary.FailureClass,
	); err != nil {
		return err
	}
	if err := validateVerificationOutcomeMessageConsistency(
		"verification",
		summary.Outcome,
		summary.Message,
	); err != nil {
		return err
	}
	if err := validateVerificationEvidenceList(summary.Evidence, "verification evidence"); err != nil {
		return err
	}

	seen := make(map[string]struct{}, len(summary.Categories))
	for i, category := range summary.Categories {
		if err := validateVerificationCategoryResult(category, i); err != nil {
			return err
		}

		key := string(category.Category)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("%w: category %d is a duplicate", ErrInvalidVerificationRecord, i)
		}
		seen[key] = struct{}{}
	}

	return nil
}

func validateVerificationCategoryResult(result VerificationCategoryResult, index int) error {
	prefix := fmt.Sprintf("category %d", index)

	if result.Category == "" {
		return fmt.Errorf("%w: %s category is required", ErrInvalidVerificationRecord, prefix)
	}
	if !knownVerificationCategory(result.Category) {
		return fmt.Errorf("%w: %s category %q is invalid", ErrInvalidVerificationRecord, prefix, result.Category)
	}
	if result.Outcome == "" {
		return fmt.Errorf("%w: %s outcome is required", ErrInvalidVerificationRecord, prefix)
	}
	if !knownVerificationOutcome(result.Outcome) {
		return fmt.Errorf("%w: %s outcome %q is invalid", ErrInvalidVerificationRecord, prefix, result.Outcome)
	}
	if !result.Enabled && result.Outcome != VerificationOutcomeSkipped {
		return fmt.Errorf("%w: %s disabled categories must use skipped outcome", ErrInvalidVerificationRecord, prefix)
	}
	if err := validateVerificationOutcomeFailureClassConsistency(
		prefix,
		result.Outcome,
		result.FailureClass,
	); err != nil {
		return err
	}
	if err := validateVerificationCategoryOutcomeMessageConsistency(result, prefix); err != nil {
		return err
	}
	if err := validateVerificationEvidenceList(result.Evidence, fmt.Sprintf("%s evidence", prefix)); err != nil {
		return err
	}

	return nil
}

func validateVerificationOutcomeFailureClassConsistency(scope string, outcome VerificationOutcome, failureClass VerificationFailureClass) error {
	switch NormalizeVerificationOutcome(outcome) {
	case VerificationOutcomePass:
		if failureClass != "" {
			return fmt.Errorf("%w: %s failure class must be empty for pass outcome", ErrInvalidVerificationRecord, scope)
		}
	case VerificationOutcomeFail:
		if failureClass == "" {
			return fmt.Errorf("%w: %s failure class is required for fail outcome", ErrInvalidVerificationRecord, scope)
		}
		if !knownVerificationFailureClass(failureClass) {
			return fmt.Errorf("%w: %s failure class %q is invalid", ErrInvalidVerificationRecord, scope, failureClass)
		}
	case VerificationOutcomeSkipped:
		if failureClass != "" && !knownVerificationFailureClass(failureClass) {
			return fmt.Errorf("%w: %s failure class %q is invalid", ErrInvalidVerificationRecord, scope, failureClass)
		}
	}

	return nil
}

func validateVerificationOutcomeMessageConsistency(scope string, outcome VerificationOutcome, message string) error {
	if NormalizeVerificationOutcome(outcome) == VerificationOutcomeFail && message == "" {
		return fmt.Errorf("%w: %s message is required for fail outcome", ErrInvalidVerificationRecord, scope)
	}
	return nil
}

func validateVerificationCategoryOutcomeMessageConsistency(result VerificationCategoryResult, scope string) error {
	outcome := NormalizeVerificationOutcome(result.Outcome)
	if !result.Enabled {
		return nil
	}
	if outcome == VerificationOutcomeFail && result.Message == "" {
		return fmt.Errorf("%w: %s message is required for enabled failed categories", ErrInvalidVerificationRecord, scope)
	}
	return nil
}

func validateVerificationEvidenceList(evidence []VerificationEvidence, scope string) error {
	seen := make(map[string]struct{}, len(evidence))
	for i, item := range evidence {
		if err := validateVerificationEvidence(item, scope, i); err != nil {
			return err
		}

		key, err := verificationEvidenceDuplicateKey(item)
		if err != nil {
			return err
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("%w: %s %d is a duplicate", ErrInvalidVerificationRecord, scope, i)
		}
		seen[key] = struct{}{}
	}

	return nil
}

func validateVerificationEvidence(evidence VerificationEvidence, scope string, index int) error {
	field := fmt.Sprintf("%s %d digestSha256", scope, index)
	if err := validateVerificationDigestSHA256(field, evidence.DigestSHA256); err != nil {
		return err
	}
	if evidence.SourceKind != "" && evidence.SourceRef == "" {
		return fmt.Errorf("%w: %s %d sourceRef is required when sourceKind is present", ErrInvalidVerificationRecord, scope, index)
	}
	if evidence.SourceRef != "" && evidence.SourceKind == "" {
		return fmt.Errorf("%w: %s %d sourceKind is required when sourceRef is present", ErrInvalidVerificationRecord, scope, index)
	}
	return nil
}

func validateVerificationDigestSHA256(field, digest string) error {
	if digest == "" {
		return nil
	}
	if len(digest) != 64 || !isLowerHex64(digest) {
		return fmt.Errorf("%w: %s must be a 64-character lowercase hexadecimal digest", ErrInvalidVerificationRecord, field)
	}
	return nil
}

func deriveVerificationIdentity(input normalizedVerificationRecordInput) (Identity, error) {
	parts, err := verificationIdentityParts(input)
	if err != nil {
		return Identity{}, err
	}

	identity, err := DeriveIdentity(IdentityInput{
		Family:     FamilyVerification,
		Version:    CurrentVersion,
		RecordKey:  input.RecordKey,
		Provenance: input.Provenance,
		Parts:      parts,
	})
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %w", ErrInvalidVerificationRecord, err)
	}

	return identity, nil
}

func verificationIdentityParts(input normalizedVerificationRecordInput) ([]IdentityPart, error) {
	verificationJSON, err := json.Marshal(canonicalVerificationSummaryFromNormalized(input.Verification))
	if err != nil {
		return nil, fmt.Errorf("%w: marshal verification identity material: %v", ErrInvalidVerificationRecord, err)
	}

	return []IdentityPart{
		{Key: "verification", Value: string(verificationJSON)},
	}, nil
}

func canonicalVerificationSummaryFromNormalized(summary VerificationSummary) canonicalVerificationSummary {
	return canonicalVerificationSummary{
		Outcome:      string(summary.Outcome),
		Message:      summary.Message,
		FailureClass: string(summary.FailureClass),
		Linkage: canonicalVerificationLinkage{
			JobID:      summary.Linkage.JobID,
			ProductKey: summary.Linkage.ProductKey,
			StepRef:    summary.Linkage.StepRef,
		},
		Categories: canonicalVerificationCategoryResultsFromNormalized(summary.Categories),
		Evidence:   canonicalVerificationEvidenceFromNormalized(summary.Evidence),
	}
}

func canonicalVerificationCategoryResultsFromNormalized(results []VerificationCategoryResult) []canonicalVerificationCategoryResult {
	out := make([]canonicalVerificationCategoryResult, len(results))
	for i, result := range results {
		out[i] = canonicalVerificationCategoryResult{
			Category:     string(result.Category),
			Enabled:      result.Enabled,
			Outcome:      string(result.Outcome),
			Message:      result.Message,
			FailureClass: string(result.FailureClass),
			Evidence:     canonicalVerificationEvidenceFromNormalized(result.Evidence),
		}
	}
	return out
}

func canonicalVerificationEvidenceFromNormalized(evidence []VerificationEvidence) []canonicalVerificationEvidence {
	out := make([]canonicalVerificationEvidence, len(evidence))
	for i, item := range evidence {
		out[i] = canonicalVerificationEvidence{
			SourceKind:   item.SourceKind,
			SourceRef:    item.SourceRef,
			DigestSHA256: item.DigestSHA256,
		}
	}
	return out
}

func verificationEvidenceDuplicateKey(evidence VerificationEvidence) (string, error) {
	data, err := json.Marshal(canonicalVerificationEvidence{
		SourceKind:   evidence.SourceKind,
		SourceRef:    evidence.SourceRef,
		DigestSHA256: evidence.DigestSHA256,
	})
	if err != nil {
		return "", fmt.Errorf("%w: marshal evidence duplicate key material: %v", ErrInvalidVerificationRecord, err)
	}
	return string(data), nil
}
