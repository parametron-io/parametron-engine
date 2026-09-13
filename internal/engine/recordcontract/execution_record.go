package recordcontract

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ExecutionOutcome is a normalized terminal execution-level outcome.
type ExecutionOutcome string

const (
	ExecutionOutcomeSucceeded ExecutionOutcome = "succeeded"
	ExecutionOutcomeFailed    ExecutionOutcome = "failed"
	ExecutionOutcomeCanceled  ExecutionOutcome = "canceled"
	ExecutionOutcomeTimeout   ExecutionOutcome = "timeout"
)

// ExecutionStepOutcome is a normalized terminal step-level outcome.
type ExecutionStepOutcome string

const (
	ExecutionStepOutcomeSucceeded ExecutionStepOutcome = "succeeded"
	ExecutionStepOutcomeFailed    ExecutionStepOutcome = "failed"
	ExecutionStepOutcomeSkipped   ExecutionStepOutcome = "skipped"
	ExecutionStepOutcomeCanceled  ExecutionStepOutcome = "canceled"
	ExecutionStepOutcomeTimeout   ExecutionStepOutcome = "timeout"
)

// ExecutionSummary captures normalized execution-level outcome, plan identity, and timing.
type ExecutionSummary struct {
	Outcome   ExecutionOutcome `json:"outcome"`
	PlanID    string           `json:"planId,omitempty"`
	PlanHash  string           `json:"planHash,omitempty"`
	StartedAt *time.Time       `json:"startedAt,omitempty"`
	EndedAt   *time.Time       `json:"endedAt,omitempty"`
	Duration  time.Duration    `json:"durationNs,omitempty"`
}

// ExecutionStepSummary captures normalized per-step execution summary material.
type ExecutionStepSummary struct {
	Index     int                  `json:"index"`
	StepRef   string               `json:"stepRef"`
	Outcome   ExecutionStepOutcome `json:"outcome"`
	Attempts  int                  `json:"attempts,omitempty"`
	StartedAt *time.Time           `json:"startedAt,omitempty"`
	EndedAt   *time.Time           `json:"endedAt,omitempty"`
	Duration  time.Duration        `json:"durationNs,omitempty"`
}

// ExecutionJobSummary captures normalized per-job execution summary material.
type ExecutionJobSummary struct {
	JobID      string                 `json:"jobId"`
	ProductKey string                 `json:"productKey"`
	Outcome    ExecutionOutcome       `json:"outcome"`
	Steps      []ExecutionStepSummary `json:"steps"`
}

// ExecutionRelatedRecords links related Engine-produced record identities by ID only.
type ExecutionRelatedRecords struct {
	ArtifactRecordIDs     []string `json:"artifactRecordIds,omitempty"`
	FailureRecordIDs      []string `json:"failureRecordIds,omitempty"`
	ObservationRecordIDs  []string `json:"observationRecordIds,omitempty"`
	ReferenceRecordIDs    []string `json:"referenceRecordIds,omitempty"`
	VerificationRecordIDs []string `json:"verificationRecordIds,omitempty"`
}

// ExecutionRecord is the Engine-produced normalized execution record contract payload.
type ExecutionRecord struct {
	Family         Family                  `json:"family"`
	Version        string                  `json:"version"`
	RecordKey      string                  `json:"recordKey"`
	Identity       Identity                `json:"identity"`
	Provenance     Provenance              `json:"provenance"`
	Execution      ExecutionSummary        `json:"execution"`
	Jobs           []ExecutionJobSummary   `json:"jobs"`
	RelatedRecords ExecutionRelatedRecords `json:"relatedRecords"`
}

// ExecutionRecordInput carries caller material used to build a normalized execution record.
type ExecutionRecordInput struct {
	RecordKey      string
	Provenance     Provenance
	Execution      ExecutionSummary
	Jobs           []ExecutionJobSummary
	RelatedRecords ExecutionRelatedRecords
}

type canonicalExecutionSummary struct {
	Outcome   string `json:"outcome"`
	PlanID    string `json:"planId,omitempty"`
	PlanHash  string `json:"planHash,omitempty"`
	StartedAt string `json:"startedAt,omitempty"`
	EndedAt   string `json:"endedAt,omitempty"`
	Duration  int64  `json:"durationNs,omitempty"`
}

type canonicalExecutionStepSummary struct {
	Index     int    `json:"index"`
	StepRef   string `json:"stepRef"`
	Outcome   string `json:"outcome"`
	Attempts  int    `json:"attempts,omitempty"`
	StartedAt string `json:"startedAt,omitempty"`
	EndedAt   string `json:"endedAt,omitempty"`
	Duration  int64  `json:"durationNs,omitempty"`
}

type canonicalExecutionJobSummary struct {
	JobID      string                          `json:"jobId"`
	ProductKey string                          `json:"productKey"`
	Outcome    string                          `json:"outcome"`
	Steps      []canonicalExecutionStepSummary `json:"steps"`
}

type canonicalExecutionRelatedRecords struct {
	ArtifactRecordIDs     []string `json:"artifactRecordIds,omitempty"`
	FailureRecordIDs      []string `json:"failureRecordIds,omitempty"`
	ObservationRecordIDs  []string `json:"observationRecordIds,omitempty"`
	ReferenceRecordIDs    []string `json:"referenceRecordIds,omitempty"`
	VerificationRecordIDs []string `json:"verificationRecordIds,omitempty"`
}

// BuildExecutionRecord normalizes input, derives identity, and returns a validated execution record.
func BuildExecutionRecord(input ExecutionRecordInput) (ExecutionRecord, error) {
	normalized := normalizeExecutionRecordInput(input)

	identity, err := deriveExecutionIdentity(normalized)
	if err != nil {
		return ExecutionRecord{}, err
	}

	record := ExecutionRecord{
		Family:         FamilyExecution,
		Version:        CurrentVersion,
		RecordKey:      normalized.RecordKey,
		Identity:       identity,
		Provenance:     normalized.Provenance,
		Execution:      normalized.Execution,
		Jobs:           normalized.Jobs,
		RelatedRecords: normalized.RelatedRecords,
	}

	if err := ValidateExecutionRecord(record); err != nil {
		return ExecutionRecord{}, err
	}

	return record, nil
}

// NormalizeExecutionRecord returns a deterministic, copy-safe normalized execution record.
func NormalizeExecutionRecord(record ExecutionRecord) ExecutionRecord {
	return ExecutionRecord{
		Family:         NormalizeFamily(record.Family),
		Version:        trimString(record.Version),
		RecordKey:      trimString(record.RecordKey),
		Identity:       record.Identity,
		Provenance:     NormalizeProvenance(record.Provenance),
		Execution:      normalizeExecutionSummary(record.Execution),
		Jobs:           normalizeExecutionJobs(record.Jobs),
		RelatedRecords: normalizeExecutionRelatedRecords(record.RelatedRecords),
	}
}

// ValidateExecutionRecord rejects malformed normalized execution record material.
func ValidateExecutionRecord(record ExecutionRecord) error {
	normalized := NormalizeExecutionRecord(record)

	if normalized.Family != FamilyExecution {
		return fmt.Errorf("%w: family must be execution", ErrInvalidExecutionRecord)
	}
	if err := ValidateVersion(normalized.Version); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidExecutionRecord, err)
	}
	if normalized.RecordKey == "" {
		return fmt.Errorf("%w: record key is required", ErrInvalidExecutionRecord)
	}
	if err := ValidateProvenance(normalized.Provenance); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidExecutionRecord, err)
	}
	if err := validateStoredIdentity(normalized.Identity); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidExecutionRecord, err)
	}
	if normalized.Identity.Family != FamilyExecution {
		return fmt.Errorf("%w: identity family must be execution", ErrInvalidExecutionRecord)
	}
	if normalized.Identity.Version != CurrentVersion {
		return fmt.Errorf("%w: identity version must be %q", ErrInvalidExecutionRecord, CurrentVersion)
	}
	if err := validateExecutionSummary(normalized.Execution); err != nil {
		return err
	}
	for i, job := range normalized.Jobs {
		if err := validateExecutionJobSummary(job, i); err != nil {
			return err
		}
	}
	if err := validateExecutionRelatedRecords(normalized.RelatedRecords); err != nil {
		return err
	}

	expectedIdentity, err := deriveExecutionIdentity(normalizedExecutionRecordInput{
		RecordKey:      normalized.RecordKey,
		Provenance:     normalized.Provenance,
		Execution:      normalized.Execution,
		Jobs:           normalized.Jobs,
		RelatedRecords: normalized.RelatedRecords,
	})
	if err != nil {
		return err
	}
	if normalized.Identity != expectedIdentity {
		return fmt.Errorf("%w: identity does not match derived identity", ErrInvalidExecutionRecord)
	}

	return nil
}

type normalizedExecutionRecordInput struct {
	RecordKey      string
	Provenance     Provenance
	Execution      ExecutionSummary
	Jobs           []ExecutionJobSummary
	RelatedRecords ExecutionRelatedRecords
}

func normalizeExecutionRecordInput(input ExecutionRecordInput) normalizedExecutionRecordInput {
	return normalizedExecutionRecordInput{
		RecordKey:      trimString(input.RecordKey),
		Provenance:     NormalizeProvenance(input.Provenance),
		Execution:      normalizeExecutionSummary(input.Execution),
		Jobs:           normalizeExecutionJobs(input.Jobs),
		RelatedRecords: normalizeExecutionRelatedRecords(input.RelatedRecords),
	}
}

func normalizeExecutionSummary(summary ExecutionSummary) ExecutionSummary {
	return ExecutionSummary{
		Outcome:   NormalizeExecutionOutcome(summary.Outcome),
		PlanID:    trimString(summary.PlanID),
		PlanHash:  trimString(summary.PlanHash),
		StartedAt: normalizeOptionalTime(summary.StartedAt),
		EndedAt:   normalizeOptionalTime(summary.EndedAt),
		Duration:  summary.Duration,
	}
}

func normalizeExecutionJobs(jobs []ExecutionJobSummary) []ExecutionJobSummary {
	if len(jobs) == 0 {
		return []ExecutionJobSummary{}
	}

	out := make([]ExecutionJobSummary, len(jobs))
	for i, job := range jobs {
		out[i] = ExecutionJobSummary{
			JobID:      trimString(job.JobID),
			ProductKey: trimString(job.ProductKey),
			Outcome:    NormalizeExecutionOutcome(job.Outcome),
			Steps:      normalizeExecutionSteps(job.Steps),
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].JobID != out[j].JobID {
			return out[i].JobID < out[j].JobID
		}
		return out[i].ProductKey < out[j].ProductKey
	})

	return out
}

func normalizeExecutionSteps(steps []ExecutionStepSummary) []ExecutionStepSummary {
	if len(steps) == 0 {
		return []ExecutionStepSummary{}
	}

	out := make([]ExecutionStepSummary, len(steps))
	for i, step := range steps {
		out[i] = ExecutionStepSummary{
			Index:     step.Index,
			StepRef:   trimString(step.StepRef),
			Outcome:   NormalizeExecutionStepOutcome(step.Outcome),
			Attempts:  step.Attempts,
			StartedAt: normalizeOptionalTime(step.StartedAt),
			EndedAt:   normalizeOptionalTime(step.EndedAt),
			Duration:  step.Duration,
		}
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Index < out[j].Index
	})

	return out
}

func normalizeExecutionRelatedRecords(related ExecutionRelatedRecords) ExecutionRelatedRecords {
	return ExecutionRelatedRecords{
		ArtifactRecordIDs:     normalizeRelatedRecordIDs(related.ArtifactRecordIDs),
		FailureRecordIDs:      normalizeRelatedRecordIDs(related.FailureRecordIDs),
		ObservationRecordIDs:  normalizeRelatedRecordIDs(related.ObservationRecordIDs),
		ReferenceRecordIDs:    normalizeRelatedRecordIDs(related.ReferenceRecordIDs),
		VerificationRecordIDs: normalizeRelatedRecordIDs(related.VerificationRecordIDs),
	}
}

func normalizeRelatedRecordIDs(ids []string) []string {
	if len(ids) == 0 {
		return []string{}
	}

	trimmed := make([]string, 0, len(ids))
	for _, id := range ids {
		trimmed = append(trimmed, trimString(id))
	}

	sort.Strings(trimmed)

	out := make([]string, 0, len(trimmed))
	var previous string
	for i, id := range trimmed {
		if id == "" {
			continue
		}
		if i == 0 || id != previous {
			out = append(out, id)
			previous = id
		}
	}

	return out
}

func normalizeOptionalTime(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}
	utc := value.UTC()
	return &utc
}

// NormalizeExecutionOutcome trims and preserves unknown values for validation to reject.
func NormalizeExecutionOutcome(outcome ExecutionOutcome) ExecutionOutcome {
	return ExecutionOutcome(strings.TrimSpace(string(outcome)))
}

// NormalizeExecutionStepOutcome trims and preserves unknown values for validation to reject.
func NormalizeExecutionStepOutcome(outcome ExecutionStepOutcome) ExecutionStepOutcome {
	return ExecutionStepOutcome(strings.TrimSpace(string(outcome)))
}

func knownExecutionOutcome(outcome ExecutionOutcome) bool {
	switch NormalizeExecutionOutcome(outcome) {
	case ExecutionOutcomeSucceeded, ExecutionOutcomeFailed, ExecutionOutcomeCanceled, ExecutionOutcomeTimeout:
		return true
	default:
		return false
	}
}

func knownExecutionStepOutcome(outcome ExecutionStepOutcome) bool {
	switch NormalizeExecutionStepOutcome(outcome) {
	case ExecutionStepOutcomeSucceeded, ExecutionStepOutcomeFailed, ExecutionStepOutcomeSkipped,
		ExecutionStepOutcomeCanceled, ExecutionStepOutcomeTimeout:
		return true
	default:
		return false
	}
}

func validateStoredIdentity(identity Identity) error {
	if identity.ID == "" {
		return fmt.Errorf("%w: identity id is required", ErrInvalidIdentity)
	}
	if !isLowerHex64(identity.ID) {
		return fmt.Errorf("%w: identity id must be a 64-character lowercase hexadecimal digest", ErrInvalidIdentity)
	}
	if identity.Algorithm != IdentityAlgorithmSHA256CanonicalV1 {
		return fmt.Errorf("%w: identity algorithm must be %q", ErrInvalidIdentity, IdentityAlgorithmSHA256CanonicalV1)
	}
	if err := ValidateFamily(identity.Family); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidIdentity, err)
	}
	if err := ValidateVersion(identity.Version); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidIdentity, err)
	}
	return nil
}

func validateExecutionSummary(summary ExecutionSummary) error {
	if summary.Outcome == "" {
		return fmt.Errorf("%w: execution outcome is required", ErrInvalidExecutionRecord)
	}
	if !knownExecutionOutcome(summary.Outcome) {
		return fmt.Errorf("%w: execution outcome %q is invalid", ErrInvalidExecutionRecord, summary.Outcome)
	}
	if summary.Duration < 0 {
		return fmt.Errorf("%w: execution duration must not be negative", ErrInvalidExecutionRecord)
	}
	return validateOptionalTiming("execution", summary.StartedAt, summary.EndedAt, summary.Duration)
}

func validateExecutionJobSummary(job ExecutionJobSummary, index int) error {
	if job.JobID == "" {
		return fmt.Errorf("%w: job %d jobId is required", ErrInvalidExecutionRecord, index)
	}
	if job.ProductKey == "" {
		return fmt.Errorf("%w: job %d productKey is required", ErrInvalidExecutionRecord, index)
	}
	if job.Outcome == "" {
		return fmt.Errorf("%w: job %d outcome is required", ErrInvalidExecutionRecord, index)
	}
	if !knownExecutionOutcome(job.Outcome) {
		return fmt.Errorf("%w: job %d outcome %q is invalid", ErrInvalidExecutionRecord, index, job.Outcome)
	}

	seenIndexes := make(map[int]struct{}, len(job.Steps))
	for stepIndex, step := range job.Steps {
		if err := validateExecutionStepSummary(step, index, stepIndex); err != nil {
			return err
		}
		if _, exists := seenIndexes[step.Index]; exists {
			return fmt.Errorf("%w: job %d step index %d is duplicated", ErrInvalidExecutionRecord, index, step.Index)
		}
		seenIndexes[step.Index] = struct{}{}
	}

	return nil
}

func validateExecutionStepSummary(step ExecutionStepSummary, jobIndex, stepIndex int) error {
	if step.Index < 0 {
		return fmt.Errorf("%w: job %d step %d index must not be negative", ErrInvalidExecutionRecord, jobIndex, stepIndex)
	}
	if step.StepRef == "" {
		return fmt.Errorf("%w: job %d step %d stepRef is required", ErrInvalidExecutionRecord, jobIndex, stepIndex)
	}
	if step.Outcome == "" {
		return fmt.Errorf("%w: job %d step %d outcome is required", ErrInvalidExecutionRecord, jobIndex, stepIndex)
	}
	if !knownExecutionStepOutcome(step.Outcome) {
		return fmt.Errorf("%w: job %d step %d outcome %q is invalid", ErrInvalidExecutionRecord, jobIndex, stepIndex, step.Outcome)
	}
	if step.Attempts < 0 {
		return fmt.Errorf("%w: job %d step %d attempts must not be negative", ErrInvalidExecutionRecord, jobIndex, stepIndex)
	}
	if step.Duration < 0 {
		return fmt.Errorf("%w: job %d step %d duration must not be negative", ErrInvalidExecutionRecord, jobIndex, stepIndex)
	}
	return validateOptionalTiming(fmt.Sprintf("job %d step %d", jobIndex, stepIndex), step.StartedAt, step.EndedAt, step.Duration)
}

func validateOptionalTiming(scope string, startedAt, endedAt *time.Time, duration time.Duration) error {
	if startedAt != nil && endedAt != nil && endedAt.Before(*startedAt) {
		return fmt.Errorf("%w: %s endedAt must not be before startedAt", ErrInvalidExecutionRecord, scope)
	}
	if startedAt != nil && endedAt != nil && duration < 0 {
		return fmt.Errorf("%w: %s duration must not be negative", ErrInvalidExecutionRecord, scope)
	}
	return nil
}

func validateExecutionRelatedRecords(related ExecutionRelatedRecords) error {
	fields := []struct {
		name string
		ids  []string
	}{
		{name: "artifactRecordIds", ids: related.ArtifactRecordIDs},
		{name: "failureRecordIds", ids: related.FailureRecordIDs},
		{name: "observationRecordIds", ids: related.ObservationRecordIDs},
		{name: "referenceRecordIds", ids: related.ReferenceRecordIDs},
		{name: "verificationRecordIds", ids: related.VerificationRecordIDs},
	}

	for _, field := range fields {
		for i, id := range field.ids {
			if id == "" {
				return fmt.Errorf("%w: %s[%d] must not be blank", ErrInvalidExecutionRecord, field.name, i)
			}
		}
	}

	return nil
}

func deriveExecutionIdentity(input normalizedExecutionRecordInput) (Identity, error) {
	parts, err := executionIdentityParts(input)
	if err != nil {
		return Identity{}, err
	}

	identity, err := DeriveIdentity(IdentityInput{
		Family:     FamilyExecution,
		Version:    CurrentVersion,
		RecordKey:  input.RecordKey,
		Provenance: input.Provenance,
		Parts:      parts,
	})
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %w", ErrInvalidExecutionRecord, err)
	}

	return identity, nil
}

func executionIdentityParts(input normalizedExecutionRecordInput) ([]IdentityPart, error) {
	executionJSON, err := json.Marshal(canonicalExecutionSummaryFromNormalized(input.Execution))
	if err != nil {
		return nil, fmt.Errorf("%w: marshal execution summary identity material: %v", ErrInvalidExecutionRecord, err)
	}

	jobsJSON, err := json.Marshal(canonicalExecutionJobsFromNormalized(input.Jobs))
	if err != nil {
		return nil, fmt.Errorf("%w: marshal jobs identity material: %v", ErrInvalidExecutionRecord, err)
	}

	relatedJSON, err := json.Marshal(canonicalExecutionRelatedRecordsFromNormalized(input.RelatedRecords))
	if err != nil {
		return nil, fmt.Errorf("%w: marshal related records identity material: %v", ErrInvalidExecutionRecord, err)
	}

	return []IdentityPart{
		{Key: "execution", Value: string(executionJSON)},
		{Key: "jobs", Value: string(jobsJSON)},
		{Key: "relatedRecords", Value: string(relatedJSON)},
	}, nil
}

func canonicalExecutionSummaryFromNormalized(summary ExecutionSummary) canonicalExecutionSummary {
	return canonicalExecutionSummary{
		Outcome:   string(summary.Outcome),
		PlanID:    summary.PlanID,
		PlanHash:  summary.PlanHash,
		StartedAt: canonicalTimeString(summary.StartedAt),
		EndedAt:   canonicalTimeString(summary.EndedAt),
		Duration:  int64(summary.Duration),
	}
}

func canonicalExecutionJobsFromNormalized(jobs []ExecutionJobSummary) []canonicalExecutionJobSummary {
	out := make([]canonicalExecutionJobSummary, len(jobs))
	for i, job := range jobs {
		out[i] = canonicalExecutionJobSummary{
			JobID:      job.JobID,
			ProductKey: job.ProductKey,
			Outcome:    string(job.Outcome),
			Steps:      canonicalExecutionStepsFromNormalized(job.Steps),
		}
	}
	return out
}

func canonicalExecutionStepsFromNormalized(steps []ExecutionStepSummary) []canonicalExecutionStepSummary {
	out := make([]canonicalExecutionStepSummary, len(steps))
	for i, step := range steps {
		out[i] = canonicalExecutionStepSummary{
			Index:     step.Index,
			StepRef:   step.StepRef,
			Outcome:   string(step.Outcome),
			Attempts:  step.Attempts,
			StartedAt: canonicalTimeString(step.StartedAt),
			EndedAt:   canonicalTimeString(step.EndedAt),
			Duration:  int64(step.Duration),
		}
	}
	return out
}

func canonicalExecutionRelatedRecordsFromNormalized(related ExecutionRelatedRecords) canonicalExecutionRelatedRecords {
	return canonicalExecutionRelatedRecords{
		ArtifactRecordIDs:     copyStringSlice(related.ArtifactRecordIDs),
		FailureRecordIDs:      copyStringSlice(related.FailureRecordIDs),
		ObservationRecordIDs:  copyStringSlice(related.ObservationRecordIDs),
		ReferenceRecordIDs:    copyStringSlice(related.ReferenceRecordIDs),
		VerificationRecordIDs: copyStringSlice(related.VerificationRecordIDs),
	}
}

func canonicalTimeString(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func copyStringSlice(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}
