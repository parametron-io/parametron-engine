package recordmap

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/report"
	"parametron/internal/engine/verification"
)

const (
	reportEvidenceKind     = "report"
	reportEvidenceRef      = "raw/report.json"
	failureRecordKeySuffix = ":failure"
)

// ReportMappingInput carries operational report material and caller-supplied record identity.
type ReportMappingInput struct {
	Report report.Report

	// Required stable key material supplied by the caller.
	// Do not invent source revision or PDM identity.
	RecordKey string

	// Optional base provenance supplied by the caller.
	// The mapper may enrich plan provenance and evidence references from report fields,
	// but must not fabricate unavailable PDM/source revision data.
	Provenance recordcontract.Provenance

	// Optional related records supplied by caller.
	RelatedRecords recordcontract.ExecutionRelatedRecords
}

// ReportMappingOutput carries normalized execution and optional failure records.
type ReportMappingOutput struct {
	ExecutionRecord recordcontract.ExecutionRecord
	FailureRecord   *recordcontract.FailureRecord
}

// MapReport converts an operational report into normalized execution and failure records.
func MapReport(input ReportMappingInput) (ReportMappingOutput, error) {
	executionRecord, err := MapReportToExecutionRecord(input)
	if err != nil {
		return ReportMappingOutput{}, err
	}

	failureRecord, err := MapReportToFailureRecord(input)
	if err != nil {
		return ReportMappingOutput{}, err
	}

	return ReportMappingOutput{
		ExecutionRecord: executionRecord,
		FailureRecord:   failureRecord,
	}, nil
}

// MapReportToExecutionRecord converts an operational report into a normalized execution record.
func MapReportToExecutionRecord(input ReportMappingInput) (recordcontract.ExecutionRecord, error) {
	if err := validateReportMappingInput(input); err != nil {
		return recordcontract.ExecutionRecord{}, err
	}

	outcome, err := mapReportStatus(input.Report.Status)
	if err != nil {
		return recordcontract.ExecutionRecord{}, err
	}

	runtimeDuration, err := durationMsToDuration(input.Report.Runtime.DurationMs)
	if err != nil {
		return recordcontract.ExecutionRecord{}, fmt.Errorf("%w: runtime duration: %w", ErrInvalidReportMapping, err)
	}

	jobs, err := mapJobs(input.Report.Jobs, outcome)
	if err != nil {
		return recordcontract.ExecutionRecord{}, err
	}

	execution := recordcontract.ExecutionSummary{
		Outcome:  outcome,
		PlanHash: strings.TrimSpace(input.Report.PlanHash),
		Duration: runtimeDuration,
	}
	if startedAt := optionalTime(input.Report.Runtime.StartedAt); startedAt != nil {
		execution.StartedAt = startedAt
	}
	if endedAt := optionalTime(input.Report.Runtime.EndedAt); endedAt != nil {
		execution.EndedAt = endedAt
	}

	record, err := recordcontract.BuildExecutionRecord(recordcontract.ExecutionRecordInput{
		RecordKey:      strings.TrimSpace(input.RecordKey),
		Provenance:     enrichProvenance(input.Provenance, input.Report.PlanHash),
		Execution:      execution,
		Jobs:           jobs,
		RelatedRecords: input.RelatedRecords,
	})
	if err != nil {
		return recordcontract.ExecutionRecord{}, fmt.Errorf("%w: build execution record: %w", ErrInvalidReportMapping, err)
	}

	return record, nil
}

// MapReportToFailureRecord converts report error material into a normalized failure record.
// It returns nil when the report has no error.
func MapReportToFailureRecord(input ReportMappingInput) (*recordcontract.FailureRecord, error) {
	if err := validateReportMappingInput(input); err != nil {
		return nil, err
	}

	if input.Report.Error == nil {
		return nil, nil
	}

	summary, err := mapFailureSummary(input.Report)
	if err != nil {
		return nil, err
	}

	recordKey := strings.TrimSpace(input.RecordKey) + failureRecordKeySuffix
	record, err := recordcontract.BuildFailureRecord(recordcontract.FailureRecordInput{
		RecordKey:  recordKey,
		Provenance: enrichProvenance(input.Provenance, input.Report.PlanHash),
		Failure:    summary,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: build failure record: %w", ErrInvalidReportMapping, err)
	}

	return &record, nil
}

func validateReportMappingInput(input ReportMappingInput) error {
	if strings.TrimSpace(input.RecordKey) == "" {
		return fmt.Errorf("%w: record key is required", ErrInvalidReportMapping)
	}
	if strings.TrimSpace(input.Report.SchemaVersion) == "" {
		return fmt.Errorf("%w: report schema version is required", ErrInvalidReportMapping)
	}
	if input.Report.SchemaVersion != report.SchemaVersion {
		return fmt.Errorf("%w: unsupported report schema version %q", ErrInvalidReportMapping, input.Report.SchemaVersion)
	}
	return nil
}

func mapReportStatus(status report.Status) (recordcontract.ExecutionOutcome, error) {
	switch status {
	case report.StatusSuccess:
		return recordcontract.ExecutionOutcomeSucceeded, nil
	case report.StatusFailed:
		return recordcontract.ExecutionOutcomeFailed, nil
	case report.StatusCanceled:
		return recordcontract.ExecutionOutcomeCanceled, nil
	case report.StatusTimeout:
		return recordcontract.ExecutionOutcomeTimeout, nil
	default:
		return "", fmt.Errorf("%w: unsupported report status %q", ErrInvalidReportMapping, status)
	}
}

func mapJobs(jobs []report.JobReport, reportOutcome recordcontract.ExecutionOutcome) ([]recordcontract.ExecutionJobSummary, error) {
	if len(jobs) == 0 {
		return []recordcontract.ExecutionJobSummary{}, nil
	}

	out := make([]recordcontract.ExecutionJobSummary, 0, len(jobs))
	for _, job := range jobs {
		steps, err := mapSteps(job.Steps)
		if err != nil {
			return nil, err
		}

		jobOutcome := deriveJobOutcome(job.Steps, reportOutcome)

		out = append(out, recordcontract.ExecutionJobSummary{
			JobID:      job.JobID,
			ProductKey: job.ProductKey,
			Outcome:    jobOutcome,
			Steps:      steps,
		})
	}

	return out, nil
}

func mapSteps(steps []report.StepReport) ([]recordcontract.ExecutionStepSummary, error) {
	if len(steps) == 0 {
		return []recordcontract.ExecutionStepSummary{}, nil
	}

	out := make([]recordcontract.ExecutionStepSummary, 0, len(steps))
	for _, step := range steps {
		outcome, err := mapStepStatus(step.Status)
		if err != nil {
			return nil, err
		}

		summary := recordcontract.ExecutionStepSummary{
			Index:    step.Index,
			StepRef:  stepRef(step),
			Outcome:  outcome,
			Attempts: step.Attempts,
		}
		if startedAt := optionalTimePtr(step.StartedAt); startedAt != nil {
			summary.StartedAt = startedAt
		}
		if endedAt := optionalTimePtr(step.EndedAt); endedAt != nil {
			summary.EndedAt = endedAt
		}
		if step.DurationMs != nil {
			duration, err := durationMsToDuration(*step.DurationMs)
			if err != nil {
				return nil, fmt.Errorf("%w: job step %d duration: %w", ErrInvalidReportMapping, step.Index, err)
			}
			summary.Duration = duration
		}

		out = append(out, summary)
	}

	return out, nil
}

func stepRef(step report.StepReport) string {
	stepType := strings.TrimSpace(string(step.Type))
	if stepType == "" {
		return strconv.Itoa(step.Index)
	}
	return fmt.Sprintf("%d:%s", step.Index, stepType)
}

func deriveJobOutcome(steps []report.StepReport, fallback recordcontract.ExecutionOutcome) recordcontract.ExecutionOutcome {
	hasCanceled := false
	hasFailed := false
	for _, step := range steps {
		switch step.Status {
		case report.StepStatusTimeout:
			return recordcontract.ExecutionOutcomeTimeout
		case report.StepStatusCanceled:
			hasCanceled = true
		case report.StepStatusFailed:
			hasFailed = true
		}
	}
	if hasCanceled {
		return recordcontract.ExecutionOutcomeCanceled
	}
	if hasFailed {
		return recordcontract.ExecutionOutcomeFailed
	}

	hasNonSkipped := false
	allNonSkippedSucceeded := true
	for _, step := range steps {
		if step.Status == report.StepStatusSkipped {
			continue
		}
		hasNonSkipped = true
		if step.Status != report.StepStatusSuccess {
			allNonSkippedSucceeded = false
			break
		}
	}

	if hasNonSkipped && allNonSkippedSucceeded {
		return recordcontract.ExecutionOutcomeSucceeded
	}

	return fallback
}

func mapStepStatus(status report.StepStatus) (recordcontract.ExecutionStepOutcome, error) {
	switch status {
	case report.StepStatusSuccess:
		return recordcontract.ExecutionStepOutcomeSucceeded, nil
	case report.StepStatusFailed:
		return recordcontract.ExecutionStepOutcomeFailed, nil
	case report.StepStatusSkipped:
		return recordcontract.ExecutionStepOutcomeSkipped, nil
	case report.StepStatusCanceled:
		return recordcontract.ExecutionStepOutcomeCanceled, nil
	case report.StepStatusTimeout:
		return recordcontract.ExecutionStepOutcomeTimeout, nil
	default:
		return "", fmt.Errorf("%w: unsupported step status %q", ErrInvalidReportMapping, status)
	}
}

func mapFailureSummary(rep report.Report) (recordcontract.FailureSummary, error) {
	errSummary := rep.Error
	if errSummary == nil {
		return recordcontract.FailureSummary{}, fmt.Errorf("%w: failure summary is required", ErrInvalidReportMapping)
	}

	message := strings.TrimSpace(errSummary.Message)
	if message == "" {
		return recordcontract.FailureSummary{}, fmt.Errorf("%w: failure message is required", ErrInvalidReportMapping)
	}

	failureClass, stage := mapFailureClassAndStage(errSummary)

	summary := recordcontract.FailureSummary{
		Class:      failureClass,
		Severity:   recordcontract.FailureSeverityError,
		Stage:      stage,
		Code:       failureCode(errSummary),
		Message:    message,
		RetryCount: errSummary.RetryCount,
		Timeout:    errSummary.Timeout,
		Canceled:   errSummary.Canceled,
		Linkage: recordcontract.FailureLinkage{
			JobID:      resolveFailureJobID(rep.Jobs, errSummary.ProductID),
			ProductKey: strings.TrimSpace(errSummary.ProductID),
			StepRef:    strings.TrimSpace(errSummary.StepID),
		},
		Evidence: []recordcontract.FailureEvidence{
			{
				SourceKind: reportEvidenceKind,
				SourceRef:  reportEvidenceRef,
			},
		},
	}

	if endedAt := optionalTime(rep.Runtime.EndedAt); endedAt != nil {
		summary.OccurredAt = endedAt
	}

	return summary, nil
}

func mapFailureClassAndStage(errSummary *report.ErrorSummary) (recordcontract.FailureClass, recordcontract.FailureStage) {
	if errSummary.Timeout {
		return recordcontract.FailureClassTimeout, recordcontract.FailureStageExecution
	}
	if errSummary.Canceled {
		return recordcontract.FailureClassCanceled, recordcontract.FailureStageExecution
	}

	if class, stage, ok := mapVerificationClassification(errSummary.Classification); ok {
		return class, stage
	}

	return recordcontract.FailureClassExecution, recordcontract.FailureStageExecution
}

func mapVerificationClassification(classification verification.FailureClass) (recordcontract.FailureClass, recordcontract.FailureStage, bool) {
	switch classification {
	case verification.FailureClassNone:
		return "", "", false
	case verification.FailureClassContractInvalid,
		verification.FailureClassObservedInvalid,
		verification.FailureClassComponentMismatch,
		verification.FailureClassParameterMismatch,
		verification.FailureClassMetadataMismatch,
		verification.FailureClassReferenceMismatch,
		verification.FailureClassRequiredObservationMissing:
		return recordcontract.FailureClassVerification, recordcontract.FailureStageVerification, true
	case verification.FailureClassInternalError:
		return recordcontract.FailureClassInternal, recordcontract.FailureStageVerification, true
	default:
		return "", "", false
	}
}

func failureCode(errSummary *report.ErrorSummary) string {
	return strings.TrimSpace(string(errSummary.Classification))
}

func resolveFailureJobID(jobs []report.JobReport, productID string) string {
	productID = strings.TrimSpace(productID)
	if productID == "" {
		return ""
	}

	var matchedJobID string
	matchCount := 0
	for _, job := range jobs {
		if job.ProductKey == productID {
			matchedJobID = job.JobID
			matchCount++
		}
	}

	if matchCount == 1 {
		return matchedJobID
	}

	return ""
}

func enrichProvenance(base recordcontract.Provenance, planHash string) recordcontract.Provenance {
	provenance := base
	if strings.TrimSpace(provenance.Plan.PlanHash) == "" {
		provenance.Plan.PlanHash = strings.TrimSpace(planHash)
	}
	provenance.Evidence = appendReportEvidence(provenance.Evidence)
	return provenance
}

func appendReportEvidence(existing []recordcontract.EvidenceReference) []recordcontract.EvidenceReference {
	for _, item := range existing {
		if item.Kind == reportEvidenceKind && item.Ref == reportEvidenceRef {
			return existing
		}
	}

	out := make([]recordcontract.EvidenceReference, len(existing), len(existing)+1)
	copy(out, existing)
	out = append(out, recordcontract.EvidenceReference{
		Kind: reportEvidenceKind,
		Ref:  reportEvidenceRef,
	})
	return out
}

func durationMsToDuration(ms int64) (time.Duration, error) {
	if ms < 0 {
		return 0, fmt.Errorf("negative duration %dms", ms)
	}
	return time.Duration(ms) * time.Millisecond, nil
}

func optionalTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func optionalTimePtr(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}
	utc := value.UTC()
	return &utc
}
