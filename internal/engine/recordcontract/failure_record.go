package recordcontract

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// FailureClass is a normalized failure classification vocabulary value.
type FailureClass string

const (
	FailureClassValidation   FailureClass = "validation"
	FailureClassPlanning     FailureClass = "planning"
	FailureClassExecution    FailureClass = "execution"
	FailureClassAdapter      FailureClass = "adapter"
	FailureClassRuntime      FailureClass = "runtime"
	FailureClassExport       FailureClass = "export"
	FailureClassVerification FailureClass = "verification"
	FailureClassTimeout      FailureClass = "timeout"
	FailureClassCanceled     FailureClass = "canceled"
	FailureClassInternal     FailureClass = "internal"
)

// FailureSeverity is a normalized failure severity vocabulary value.
type FailureSeverity string

const (
	FailureSeverityError FailureSeverity = "error"
	FailureSeverityFatal FailureSeverity = "fatal"
)

// FailureStage is a normalized failure stage vocabulary value.
type FailureStage string

const (
	FailureStageValidation   FailureStage = "validation"
	FailureStagePlanning     FailureStage = "planning"
	FailureStageScheduling   FailureStage = "scheduling"
	FailureStageExecution    FailureStage = "execution"
	FailureStageAdapter      FailureStage = "adapter"
	FailureStageRuntime      FailureStage = "runtime"
	FailureStageExport       FailureStage = "export"
	FailureStageVerification FailureStage = "verification"
	FailureStagePackaging    FailureStage = "packaging"
)

// FailureLocation captures normalized failure location material.
type FailureLocation struct {
	Path      string `json:"path,omitempty"`
	Component string `json:"component,omitempty"`
	Field     string `json:"field,omitempty"`
}

// FailureLinkage captures normalized job/product/step linkage material.
type FailureLinkage struct {
	JobID      string `json:"jobId,omitempty"`
	ProductKey string `json:"productKey,omitempty"`
	StepRef    string `json:"stepRef,omitempty"`
}

// FailureEvidence captures normalized failure evidence references.
type FailureEvidence struct {
	SourceKind   string `json:"sourceKind,omitempty"`
	SourceRef    string `json:"sourceRef,omitempty"`
	DigestSHA256 string `json:"digestSha256,omitempty"`
}

// FailureSummary captures normalized failure payload material.
type FailureSummary struct {
	Class      FailureClass      `json:"class"`
	Severity   FailureSeverity   `json:"severity,omitempty"`
	Stage      FailureStage      `json:"stage,omitempty"`
	Code       string            `json:"code,omitempty"`
	Message    string            `json:"message"`
	RetryCount int               `json:"retryCount,omitempty"`
	Timeout    bool              `json:"timeout,omitempty"`
	Canceled   bool              `json:"canceled,omitempty"`
	OccurredAt *time.Time        `json:"occurredAt,omitempty"`
	Linkage    FailureLinkage    `json:"linkage"`
	Location   FailureLocation   `json:"location"`
	Evidence   []FailureEvidence `json:"evidence"`
}

// FailureRecord is the Engine-produced normalized failure record contract payload.
type FailureRecord struct {
	Family     Family         `json:"family"`
	Version    string         `json:"version"`
	RecordKey  string         `json:"recordKey"`
	Identity   Identity       `json:"identity"`
	Provenance Provenance     `json:"provenance"`
	Failure    FailureSummary `json:"failure"`
}

// FailureRecordInput carries caller material used to build a normalized failure record.
type FailureRecordInput struct {
	RecordKey  string
	Provenance Provenance
	Failure    FailureSummary
}

type canonicalFailureLocation struct {
	Path      string `json:"path,omitempty"`
	Component string `json:"component,omitempty"`
	Field     string `json:"field,omitempty"`
}

type canonicalFailureLinkage struct {
	JobID      string `json:"jobId,omitempty"`
	ProductKey string `json:"productKey,omitempty"`
	StepRef    string `json:"stepRef,omitempty"`
}

type canonicalFailureEvidence struct {
	SourceKind   string `json:"sourceKind,omitempty"`
	SourceRef    string `json:"sourceRef,omitempty"`
	DigestSHA256 string `json:"digestSha256,omitempty"`
}

type canonicalFailureSummary struct {
	Class      string                     `json:"class"`
	Severity   string                     `json:"severity,omitempty"`
	Stage      string                     `json:"stage,omitempty"`
	Code       string                     `json:"code,omitempty"`
	Message    string                     `json:"message"`
	RetryCount int                        `json:"retryCount,omitempty"`
	Timeout    bool                       `json:"timeout,omitempty"`
	Canceled   bool                       `json:"canceled,omitempty"`
	OccurredAt string                     `json:"occurredAt,omitempty"`
	Linkage    canonicalFailureLinkage    `json:"linkage"`
	Location   canonicalFailureLocation   `json:"location"`
	Evidence   []canonicalFailureEvidence `json:"evidence"`
}

// BuildFailureRecord normalizes input, derives identity, and returns a validated failure record.
func BuildFailureRecord(input FailureRecordInput) (FailureRecord, error) {
	normalized := normalizeFailureRecordInput(input)

	identity, err := deriveFailureIdentity(normalized)
	if err != nil {
		return FailureRecord{}, err
	}

	record := FailureRecord{
		Family:     FamilyFailure,
		Version:    CurrentVersion,
		RecordKey:  normalized.RecordKey,
		Identity:   identity,
		Provenance: normalized.Provenance,
		Failure:    normalized.Failure,
	}

	if err := ValidateFailureRecord(record); err != nil {
		return FailureRecord{}, err
	}

	return record, nil
}

// NormalizeFailureRecord returns a deterministic, copy-safe normalized failure record.
func NormalizeFailureRecord(record FailureRecord) FailureRecord {
	return FailureRecord{
		Family:     NormalizeFamily(record.Family),
		Version:    trimString(record.Version),
		RecordKey:  trimString(record.RecordKey),
		Identity:   record.Identity,
		Provenance: NormalizeProvenance(record.Provenance),
		Failure:    normalizeFailureSummary(record.Failure),
	}
}

// ValidateFailureRecord rejects malformed normalized failure record material.
func ValidateFailureRecord(record FailureRecord) error {
	normalized := NormalizeFailureRecord(record)

	if normalized.Family != FamilyFailure {
		return fmt.Errorf("%w: family must be failure", ErrInvalidFailureRecord)
	}
	if err := ValidateVersion(normalized.Version); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidFailureRecord, err)
	}
	if normalized.RecordKey == "" {
		return fmt.Errorf("%w: record key is required", ErrInvalidFailureRecord)
	}
	if err := ValidateProvenance(normalized.Provenance); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidFailureRecord, err)
	}
	if err := validateStoredIdentity(normalized.Identity); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidFailureRecord, err)
	}
	if normalized.Identity.Family != FamilyFailure {
		return fmt.Errorf("%w: identity family must be failure", ErrInvalidFailureRecord)
	}
	if normalized.Identity.Version != CurrentVersion {
		return fmt.Errorf("%w: identity version must be %q", ErrInvalidFailureRecord, CurrentVersion)
	}
	if err := validateFailureSummary(normalized.Failure); err != nil {
		return err
	}

	expectedIdentity, err := deriveFailureIdentity(normalizedFailureRecordInput{
		RecordKey:  normalized.RecordKey,
		Provenance: normalized.Provenance,
		Failure:    normalized.Failure,
	})
	if err != nil {
		return err
	}
	if normalized.Identity != expectedIdentity {
		return fmt.Errorf("%w: identity does not match derived identity", ErrInvalidFailureRecord)
	}

	return nil
}

type normalizedFailureRecordInput struct {
	RecordKey  string
	Provenance Provenance
	Failure    FailureSummary
}

func normalizeFailureRecordInput(input FailureRecordInput) normalizedFailureRecordInput {
	return normalizedFailureRecordInput{
		RecordKey:  trimString(input.RecordKey),
		Provenance: NormalizeProvenance(input.Provenance),
		Failure:    normalizeFailureSummary(input.Failure),
	}
}

func normalizeFailureSummary(summary FailureSummary) FailureSummary {
	return FailureSummary{
		Class:      NormalizeFailureClass(summary.Class),
		Severity:   NormalizeFailureSeverity(summary.Severity),
		Stage:      NormalizeFailureStage(summary.Stage),
		Code:       trimString(summary.Code),
		Message:    trimString(summary.Message),
		RetryCount: summary.RetryCount,
		Timeout:    summary.Timeout,
		Canceled:   summary.Canceled,
		OccurredAt: normalizeOptionalTime(summary.OccurredAt),
		Linkage:    normalizeFailureLinkage(summary.Linkage),
		Location:   normalizeFailureLocation(summary.Location),
		Evidence:   normalizeFailureEvidenceList(summary.Evidence),
	}
}

func normalizeFailureLinkage(linkage FailureLinkage) FailureLinkage {
	return FailureLinkage{
		JobID:      trimString(linkage.JobID),
		ProductKey: trimString(linkage.ProductKey),
		StepRef:    trimString(linkage.StepRef),
	}
}

func normalizeFailureLocation(location FailureLocation) FailureLocation {
	return FailureLocation{
		Path:      trimString(location.Path),
		Component: trimString(location.Component),
		Field:     trimString(location.Field),
	}
}

func normalizeFailureEvidence(evidence FailureEvidence) FailureEvidence {
	return FailureEvidence{
		SourceKind:   trimString(evidence.SourceKind),
		SourceRef:    trimString(evidence.SourceRef),
		DigestSHA256: trimString(evidence.DigestSHA256),
	}
}

func normalizeFailureEvidenceList(evidence []FailureEvidence) []FailureEvidence {
	if len(evidence) == 0 {
		return []FailureEvidence{}
	}

	out := make([]FailureEvidence, len(evidence))
	for i, item := range evidence {
		out[i] = normalizeFailureEvidence(item)
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

// NormalizeFailureClass trims and preserves unknown values for validation to reject.
func NormalizeFailureClass(class FailureClass) FailureClass {
	return FailureClass(strings.TrimSpace(string(class)))
}

// NormalizeFailureSeverity trims and preserves unknown values for validation to reject.
func NormalizeFailureSeverity(severity FailureSeverity) FailureSeverity {
	return FailureSeverity(strings.TrimSpace(string(severity)))
}

// NormalizeFailureStage trims and preserves unknown values for validation to reject.
func NormalizeFailureStage(stage FailureStage) FailureStage {
	return FailureStage(strings.TrimSpace(string(stage)))
}

func knownFailureClass(class FailureClass) bool {
	switch NormalizeFailureClass(class) {
	case FailureClassValidation, FailureClassPlanning, FailureClassExecution,
		FailureClassAdapter, FailureClassRuntime, FailureClassExport,
		FailureClassVerification, FailureClassTimeout, FailureClassCanceled,
		FailureClassInternal:
		return true
	default:
		return false
	}
}

func knownFailureSeverity(severity FailureSeverity) bool {
	switch NormalizeFailureSeverity(severity) {
	case FailureSeverityError, FailureSeverityFatal:
		return true
	default:
		return false
	}
}

func knownFailureStage(stage FailureStage) bool {
	switch NormalizeFailureStage(stage) {
	case FailureStageValidation, FailureStagePlanning, FailureStageScheduling,
		FailureStageExecution, FailureStageAdapter, FailureStageRuntime,
		FailureStageExport, FailureStageVerification, FailureStagePackaging:
		return true
	default:
		return false
	}
}

func validateFailureSummary(summary FailureSummary) error {
	if summary.Class == "" {
		return fmt.Errorf("%w: failure class is required", ErrInvalidFailureRecord)
	}
	if !knownFailureClass(summary.Class) {
		return fmt.Errorf("%w: failure class %q is invalid", ErrInvalidFailureRecord, summary.Class)
	}
	if summary.Message == "" {
		return fmt.Errorf("%w: failure message is required", ErrInvalidFailureRecord)
	}
	if summary.Severity != "" && !knownFailureSeverity(summary.Severity) {
		return fmt.Errorf("%w: failure severity %q is invalid", ErrInvalidFailureRecord, summary.Severity)
	}
	if summary.Stage != "" && !knownFailureStage(summary.Stage) {
		return fmt.Errorf("%w: failure stage %q is invalid", ErrInvalidFailureRecord, summary.Stage)
	}
	if summary.RetryCount < 0 {
		return fmt.Errorf("%w: failure retryCount must not be negative", ErrInvalidFailureRecord)
	}
	if err := validateFailureClassFlagConsistency(summary); err != nil {
		return err
	}

	seen := make(map[string]struct{}, len(summary.Evidence))
	for i, evidence := range summary.Evidence {
		if err := validateFailureEvidence(evidence, i); err != nil {
			return err
		}

		key, err := failureEvidenceDuplicateKey(evidence)
		if err != nil {
			return err
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("%w: evidence %d is a duplicate", ErrInvalidFailureRecord, i)
		}
		seen[key] = struct{}{}
	}

	return nil
}

func validateFailureClassFlagConsistency(summary FailureSummary) error {
	if summary.Timeout && summary.Canceled {
		return fmt.Errorf("%w: failure timeout and canceled flags are mutually exclusive", ErrInvalidFailureRecord)
	}

	class := NormalizeFailureClass(summary.Class)

	if class == FailureClassTimeout && !summary.Timeout {
		return fmt.Errorf("%w: failure class timeout requires timeout flag", ErrInvalidFailureRecord)
	}
	if class == FailureClassCanceled && !summary.Canceled {
		return fmt.Errorf("%w: failure class canceled requires canceled flag", ErrInvalidFailureRecord)
	}
	if summary.Timeout && class == FailureClassCanceled {
		return fmt.Errorf("%w: failure timeout flag is inconsistent with canceled class", ErrInvalidFailureRecord)
	}
	if summary.Canceled && class == FailureClassTimeout {
		return fmt.Errorf("%w: failure canceled flag is inconsistent with timeout class", ErrInvalidFailureRecord)
	}
	if summary.Timeout && class != FailureClassTimeout {
		return fmt.Errorf("%w: failure timeout flag requires timeout class", ErrInvalidFailureRecord)
	}
	if summary.Canceled && class != FailureClassCanceled {
		return fmt.Errorf("%w: failure canceled flag requires canceled class", ErrInvalidFailureRecord)
	}

	return nil
}

func validateFailureEvidence(evidence FailureEvidence, index int) error {
	if err := validateFailureDigestSHA256(
		fmt.Sprintf("evidence %d digestSha256", index),
		evidence.DigestSHA256,
	); err != nil {
		return err
	}
	if evidence.SourceKind != "" && evidence.SourceRef == "" {
		return fmt.Errorf("%w: evidence %d sourceRef is required when sourceKind is present", ErrInvalidFailureRecord, index)
	}
	if evidence.SourceRef != "" && evidence.SourceKind == "" {
		return fmt.Errorf("%w: evidence %d sourceKind is required when sourceRef is present", ErrInvalidFailureRecord, index)
	}
	return nil
}

func validateFailureDigestSHA256(field, digest string) error {
	if digest == "" {
		return nil
	}
	if len(digest) != 64 || !isLowerHex64(digest) {
		return fmt.Errorf("%w: %s must be a 64-character lowercase hexadecimal digest", ErrInvalidFailureRecord, field)
	}
	return nil
}

func deriveFailureIdentity(input normalizedFailureRecordInput) (Identity, error) {
	parts, err := failureIdentityParts(input)
	if err != nil {
		return Identity{}, err
	}

	identity, err := DeriveIdentity(IdentityInput{
		Family:     FamilyFailure,
		Version:    CurrentVersion,
		RecordKey:  input.RecordKey,
		Provenance: input.Provenance,
		Parts:      parts,
	})
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %w", ErrInvalidFailureRecord, err)
	}

	return identity, nil
}

func failureIdentityParts(input normalizedFailureRecordInput) ([]IdentityPart, error) {
	failureJSON, err := json.Marshal(canonicalFailureSummaryFromNormalized(input.Failure))
	if err != nil {
		return nil, fmt.Errorf("%w: marshal failure identity material: %v", ErrInvalidFailureRecord, err)
	}

	return []IdentityPart{
		{Key: "failure", Value: string(failureJSON)},
	}, nil
}

func canonicalFailureSummaryFromNormalized(summary FailureSummary) canonicalFailureSummary {
	return canonicalFailureSummary{
		Class:      string(summary.Class),
		Severity:   string(summary.Severity),
		Stage:      string(summary.Stage),
		Code:       summary.Code,
		Message:    summary.Message,
		RetryCount: summary.RetryCount,
		Timeout:    summary.Timeout,
		Canceled:   summary.Canceled,
		OccurredAt: canonicalTimeString(summary.OccurredAt),
		Linkage: canonicalFailureLinkage{
			JobID:      summary.Linkage.JobID,
			ProductKey: summary.Linkage.ProductKey,
			StepRef:    summary.Linkage.StepRef,
		},
		Location: canonicalFailureLocation{
			Path:      summary.Location.Path,
			Component: summary.Location.Component,
			Field:     summary.Location.Field,
		},
		Evidence: canonicalFailureEvidenceFromNormalized(summary.Evidence),
	}
}

func canonicalFailureEvidenceFromNormalized(evidence []FailureEvidence) []canonicalFailureEvidence {
	out := make([]canonicalFailureEvidence, len(evidence))
	for i, item := range evidence {
		out[i] = canonicalFailureEvidence{
			SourceKind:   item.SourceKind,
			SourceRef:    item.SourceRef,
			DigestSHA256: item.DigestSHA256,
		}
	}
	return out
}

func failureEvidenceDuplicateKey(evidence FailureEvidence) (string, error) {
	data, err := json.Marshal(canonicalFailureEvidence{
		SourceKind:   evidence.SourceKind,
		SourceRef:    evidence.SourceRef,
		DigestSHA256: evidence.DigestSHA256,
	})
	if err != nil {
		return "", fmt.Errorf("%w: marshal evidence duplicate key material: %v", ErrInvalidFailureRecord, err)
	}
	return string(data), nil
}
