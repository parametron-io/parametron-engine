package recordmap_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordmap"
	"parametron/internal/engine/report"
	"parametron/internal/engine/verification"
)

const (
	testRecordKey = "execution:run-1"
	testPlanHash  = "plan-hash-1"
)

func TestMapReportSuccessMapsExecutionRecordOnly(t *testing.T) {
	input := testMappingInput(successReport())

	got, err := recordmap.MapReport(input)
	if err != nil {
		t.Fatalf("MapReport(success) returned error: %v", err)
	}
	if err := recordcontract.ValidateExecutionRecord(got.ExecutionRecord); err != nil {
		t.Fatalf("ValidateExecutionRecord(mapped) returned error: %v", err)
	}
	if got.FailureRecord != nil {
		t.Fatalf("FailureRecord = %#v, want nil", got.FailureRecord)
	}

	execution := got.ExecutionRecord.Execution
	if execution.Outcome != recordcontract.ExecutionOutcomeSucceeded {
		t.Fatalf("Execution.Outcome = %q, want %q", execution.Outcome, recordcontract.ExecutionOutcomeSucceeded)
	}
	if execution.PlanHash != testPlanHash {
		t.Fatalf("Execution.PlanHash = %q, want %q", execution.PlanHash, testPlanHash)
	}
	if execution.Duration != 1250*time.Millisecond {
		t.Fatalf("Execution.Duration = %s, want 1.25s", execution.Duration)
	}
	if execution.StartedAt == nil || execution.EndedAt == nil {
		t.Fatalf("Execution timestamps = %#v/%#v, want both present", execution.StartedAt, execution.EndedAt)
	}

	if len(got.ExecutionRecord.Jobs) != 1 {
		t.Fatalf("Jobs length = %d, want 1", len(got.ExecutionRecord.Jobs))
	}
	job := got.ExecutionRecord.Jobs[0]
	if job.JobID != "job-a" || job.ProductKey != "product-a" {
		t.Fatalf("job summary = %#v, want job-a/product-a", job)
	}
	if len(job.Steps) != 2 {
		t.Fatalf("Steps length = %d, want 2", len(job.Steps))
	}
	if job.Steps[0].StepRef != "0:WriteCSV" || job.Steps[1].StepRef != "1:RunCADRuntime" {
		t.Fatalf("step refs = %#v, want deterministic <index>:<type>", job.Steps)
	}
	if !hasProvenanceEvidence(got.ExecutionRecord.Provenance, "report", "raw/prm.report.json") {
		t.Fatalf("Provenance.Evidence = %#v, want raw/prm.report.json evidence", got.ExecutionRecord.Provenance.Evidence)
	}

	failure, err := recordmap.MapReportToFailureRecord(input)
	if err != nil {
		t.Fatalf("MapReportToFailureRecord(success) returned error: %v", err)
	}
	if failure != nil {
		t.Fatalf("MapReportToFailureRecord(success) = %#v, want nil", failure)
	}
}

func TestMapReportStatusMapsExecutionOutcomes(t *testing.T) {
	tests := []struct {
		name       string
		status     report.Status
		want       recordcontract.ExecutionOutcome
		wantErr    bool
		errorValue *report.ErrorSummary
	}{
		{name: "success", status: report.StatusSuccess, want: recordcontract.ExecutionOutcomeSucceeded},
		{name: "failed", status: report.StatusFailed, want: recordcontract.ExecutionOutcomeFailed},
		{name: "canceled", status: report.StatusCanceled, want: recordcontract.ExecutionOutcomeCanceled},
		{name: "timeout", status: report.StatusTimeout, want: recordcontract.ExecutionOutcomeTimeout},
		{name: "unsupported", status: report.Status("interrupted"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rep := successReport()
			rep.Status = tt.status
			got, err := recordmap.MapReportToExecutionRecord(testMappingInput(rep))
			if tt.wantErr {
				assertInvalidMappingError(t, err)
				return
			}
			if err != nil {
				t.Fatalf("MapReportToExecutionRecord(%s) returned error: %v", tt.name, err)
			}
			if got.Execution.Outcome != tt.want {
				t.Fatalf("Execution.Outcome = %q, want %q", got.Execution.Outcome, tt.want)
			}
		})
	}
}

func TestMapReportStepStatusMapsExecutionStepOutcomes(t *testing.T) {
	tests := []struct {
		name   string
		status report.StepStatus
		want   recordcontract.ExecutionStepOutcome
	}{
		{name: "success", status: report.StepStatusSuccess, want: recordcontract.ExecutionStepOutcomeSucceeded},
		{name: "failed", status: report.StepStatusFailed, want: recordcontract.ExecutionStepOutcomeFailed},
		{name: "skipped", status: report.StepStatusSkipped, want: recordcontract.ExecutionStepOutcomeSkipped},
		{name: "canceled", status: report.StepStatusCanceled, want: recordcontract.ExecutionStepOutcomeCanceled},
		{name: "timeout", status: report.StepStatusTimeout, want: recordcontract.ExecutionStepOutcomeTimeout},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rep := successReport()
			rep.Jobs[0].Steps = []report.StepReport{step(0, planner.StepRunCADRuntime, tt.status, 10)}
			got, err := recordmap.MapReportToExecutionRecord(testMappingInput(rep))
			if err != nil {
				t.Fatalf("MapReportToExecutionRecord(%s) returned error: %v", tt.name, err)
			}
			if got.Jobs[0].Steps[0].Outcome != tt.want {
				t.Fatalf("Step.Outcome = %q, want %q", got.Jobs[0].Steps[0].Outcome, tt.want)
			}
		})
	}

	t.Run("unsupported status", func(t *testing.T) {
		rep := successReport()
		rep.Jobs[0].Steps = []report.StepReport{step(0, planner.StepRunCADRuntime, report.StepStatus("blocked"), 10)}
		_, err := recordmap.MapReportToExecutionRecord(testMappingInput(rep))
		assertInvalidMappingError(t, err)
	})

	t.Run("negative duration", func(t *testing.T) {
		rep := successReport()
		rep.Jobs[0].Steps = []report.StepReport{step(0, planner.StepRunCADRuntime, report.StepStatusSuccess, -1)}
		_, err := recordmap.MapReportToExecutionRecord(testMappingInput(rep))
		assertInvalidMappingError(t, err)
	})
}

func TestMapReportJobOutcomeDerivationIsDeterministic(t *testing.T) {
	tests := []struct {
		name          string
		reportStatus  report.Status
		stepStatuses  []report.StepStatus
		wantJobResult recordcontract.ExecutionOutcome
	}{
		{
			name:          "timeout wins over failed",
			reportStatus:  report.StatusFailed,
			stepStatuses:  []report.StepStatus{report.StepStatusFailed, report.StepStatusTimeout},
			wantJobResult: recordcontract.ExecutionOutcomeTimeout,
		},
		{
			name:          "canceled wins after timeout absent",
			reportStatus:  report.StatusFailed,
			stepStatuses:  []report.StepStatus{report.StepStatusFailed, report.StepStatusCanceled},
			wantJobResult: recordcontract.ExecutionOutcomeCanceled,
		},
		{
			name:          "failed wins after timeout and canceled absent",
			reportStatus:  report.StatusTimeout,
			stepStatuses:  []report.StepStatus{report.StepStatusSuccess, report.StepStatusFailed},
			wantJobResult: recordcontract.ExecutionOutcomeFailed,
		},
		{
			name:          "all relevant succeeded",
			reportStatus:  report.StatusFailed,
			stepStatuses:  []report.StepStatus{report.StepStatusSuccess, report.StepStatusSkipped},
			wantJobResult: recordcontract.ExecutionOutcomeSucceeded,
		},
		{
			name:          "fallback uses top-level outcome",
			reportStatus:  report.StatusCanceled,
			stepStatuses:  []report.StepStatus{report.StepStatusSkipped},
			wantJobResult: recordcontract.ExecutionOutcomeCanceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rep := successReport()
			rep.Status = tt.reportStatus
			rep.Jobs[0].Steps = stepsFromStatuses(tt.stepStatuses)
			got, err := recordmap.MapReportToExecutionRecord(testMappingInput(rep))
			if err != nil {
				t.Fatalf("MapReportToExecutionRecord(%s) returned error: %v", tt.name, err)
			}
			if got.Jobs[0].Outcome != tt.wantJobResult {
				t.Fatalf("Job.Outcome = %q, want %q", got.Jobs[0].Outcome, tt.wantJobResult)
			}
		})
	}
}

func TestMapReportRuntimeDurationValidation(t *testing.T) {
	tests := []struct {
		name       string
		durationMs int64
		want       time.Duration
		wantErr    bool
	}{
		{name: "positive", durationMs: 500, want: 500 * time.Millisecond},
		{name: "zero", durationMs: 0, want: 0},
		{name: "negative", durationMs: -1, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rep := successReport()
			rep.Runtime.DurationMs = tt.durationMs
			got, err := recordmap.MapReportToExecutionRecord(testMappingInput(rep))
			if tt.wantErr {
				assertInvalidMappingError(t, err)
				return
			}
			if err != nil {
				t.Fatalf("MapReportToExecutionRecord(%s) returned error: %v", tt.name, err)
			}
			if got.Execution.Duration != tt.want {
				t.Fatalf("Execution.Duration = %s, want %s", got.Execution.Duration, tt.want)
			}
		})
	}
}

func TestMapReportOmitFailureWithoutReportError(t *testing.T) {
	input := testMappingInput(successReport())

	output, err := recordmap.MapReport(input)
	if err != nil {
		t.Fatalf("MapReport(no error) returned error: %v", err)
	}
	if output.FailureRecord != nil {
		t.Fatalf("MapReport FailureRecord = %#v, want nil", output.FailureRecord)
	}

	failure, err := recordmap.MapReportToFailureRecord(input)
	if err != nil {
		t.Fatalf("MapReportToFailureRecord(no error) returned error: %v", err)
	}
	if failure != nil {
		t.Fatalf("MapReportToFailureRecord(no error) = %#v, want nil", failure)
	}
}

func TestMapReportErrorMapsValidFailureRecord(t *testing.T) {
	rep := failedReportWithError()

	output, err := recordmap.MapReport(testMappingInput(rep))
	if err != nil {
		t.Fatalf("MapReport(error) returned error: %v", err)
	}
	if output.FailureRecord == nil {
		t.Fatal("FailureRecord = nil, want non-nil")
	}
	failure := *output.FailureRecord
	if err := recordcontract.ValidateFailureRecord(failure); err != nil {
		t.Fatalf("ValidateFailureRecord(mapped) returned error: %v", err)
	}

	if failure.RecordKey != testRecordKey+":failure" {
		t.Fatalf("Failure.RecordKey = %q, want %q", failure.RecordKey, testRecordKey+":failure")
	}
	if failure.Failure.Message != "verification parameter mismatch" {
		t.Fatalf("Failure.Message = %q", failure.Failure.Message)
	}
	if failure.Failure.RetryCount != 2 {
		t.Fatalf("Failure.RetryCount = %d, want 2", failure.Failure.RetryCount)
	}
	if failure.Failure.Linkage.ProductKey != "product-a" {
		t.Fatalf("Failure.Linkage.ProductKey = %q, want product-a", failure.Failure.Linkage.ProductKey)
	}
	if failure.Failure.Linkage.StepRef != "1:RunCADRuntime" {
		t.Fatalf("Failure.Linkage.StepRef = %q, want 1:RunCADRuntime", failure.Failure.Linkage.StepRef)
	}
	if failure.Failure.Linkage.JobID != "job-a" {
		t.Fatalf("Failure.Linkage.JobID = %q, want unique matching job-a", failure.Failure.Linkage.JobID)
	}
	if failure.Failure.OccurredAt == nil || !failure.Failure.OccurredAt.Equal(rep.Runtime.EndedAt.UTC()) {
		t.Fatalf("Failure.OccurredAt = %#v, want runtime endedAt", failure.Failure.OccurredAt)
	}
	if failure.Failure.Class != recordcontract.FailureClassVerification {
		t.Fatalf("Failure.Class = %q, want %q", failure.Failure.Class, recordcontract.FailureClassVerification)
	}
	if failure.Failure.Stage != recordcontract.FailureStageVerification {
		t.Fatalf("Failure.Stage = %q, want %q", failure.Failure.Stage, recordcontract.FailureStageVerification)
	}
	if failure.Failure.Severity != recordcontract.FailureSeverityError {
		t.Fatalf("Failure.Severity = %q, want %q", failure.Failure.Severity, recordcontract.FailureSeverityError)
	}
	if failure.Failure.Code != string(verification.FailureClassParameterMismatch) {
		t.Fatalf("Failure.Code = %q, want verification classification", failure.Failure.Code)
	}
	if !hasFailureEvidence(failure.Failure.Evidence, "report", "raw/prm.report.json") {
		t.Fatalf("Failure.Evidence = %#v, want raw/prm.report.json evidence", failure.Failure.Evidence)
	}
}

func TestMapReportTimeoutAndCanceledErrorConsistency(t *testing.T) {
	tests := []struct {
		name     string
		timeout  bool
		canceled bool
		want     recordcontract.FailureClass
		wantErr  bool
	}{
		{name: "timeout", timeout: true, want: recordcontract.FailureClassTimeout},
		{name: "canceled", canceled: true, want: recordcontract.FailureClassCanceled},
		{name: "both rejected", timeout: true, canceled: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rep := failedReportWithError()
			rep.Error.Timeout = tt.timeout
			rep.Error.Canceled = tt.canceled
			failure, err := recordmap.MapReportToFailureRecord(testMappingInput(rep))
			if tt.wantErr {
				assertInvalidMappingError(t, err)
				return
			}
			if err != nil {
				t.Fatalf("MapReportToFailureRecord(%s) returned error: %v", tt.name, err)
			}
			if err := recordcontract.ValidateFailureRecord(*failure); err != nil {
				t.Fatalf("ValidateFailureRecord(%s) returned error: %v", tt.name, err)
			}
			if failure.Failure.Class != tt.want {
				t.Fatalf("Failure.Class = %q, want %q", failure.Failure.Class, tt.want)
			}
		})
	}
}

func TestMapReportAmbiguousFailureJobLinkageDoesNotGuess(t *testing.T) {
	tests := []struct {
		name string
		jobs []report.JobReport
	}{
		{
			name: "zero matches",
			jobs: []report.JobReport{
				{JobID: "job-b", ProductKey: "product-b", Steps: []report.StepReport{step(0, planner.StepRunCADRuntime, report.StepStatusFailed, 10)}},
			},
		},
		{
			name: "multiple matches",
			jobs: []report.JobReport{
				{JobID: "job-a", ProductKey: "product-a", Steps: []report.StepReport{step(0, planner.StepRunCADRuntime, report.StepStatusFailed, 10)}},
				{JobID: "job-a-2", ProductKey: "product-a", Steps: []report.StepReport{step(0, planner.StepRunCADRuntime, report.StepStatusFailed, 10)}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rep := failedReportWithError()
			rep.Jobs = tt.jobs
			failure, err := recordmap.MapReportToFailureRecord(testMappingInput(rep))
			if err != nil {
				t.Fatalf("MapReportToFailureRecord(%s) returned error: %v", tt.name, err)
			}
			if err := recordcontract.ValidateFailureRecord(*failure); err != nil {
				t.Fatalf("ValidateFailureRecord(%s) returned error: %v", tt.name, err)
			}
			if failure.Failure.Linkage.ProductKey != "product-a" {
				t.Fatalf("ProductKey = %q, want product-a", failure.Failure.Linkage.ProductKey)
			}
			if failure.Failure.Linkage.StepRef != "1:RunCADRuntime" {
				t.Fatalf("StepRef = %q, want 1:RunCADRuntime", failure.Failure.Linkage.StepRef)
			}
			if failure.Failure.Linkage.JobID != "" {
				t.Fatalf("JobID = %q, want blank when no unique product match exists", failure.Failure.Linkage.JobID)
			}
		})
	}
}

func TestMapReportInvalidInputWrapsInvalidReportMapping(t *testing.T) {
	tests := []struct {
		name   string
		input  recordmap.ReportMappingInput
		mapper func(recordmap.ReportMappingInput) error
	}{
		{
			name:  "blank record key",
			input: withInputMutation(testMappingInput(successReport()), func(input *recordmap.ReportMappingInput) { input.RecordKey = " " }),
			mapper: func(input recordmap.ReportMappingInput) error {
				_, err := recordmap.MapReport(input)
				return err
			},
		},
		{
			name:  "blank schema version",
			input: withReportMutation(successReport(), func(rep *report.Report) { rep.SchemaVersion = "" }),
			mapper: func(input recordmap.ReportMappingInput) error {
				_, err := recordmap.MapReportToExecutionRecord(input)
				return err
			},
		},
		{
			name:  "unsupported schema version",
			input: withReportMutation(successReport(), func(rep *report.Report) { rep.SchemaVersion = "2.0" }),
			mapper: func(input recordmap.ReportMappingInput) error {
				_, err := recordmap.MapReportToExecutionRecord(input)
				return err
			},
		},
		{
			name:  "unsupported report status",
			input: withReportMutation(successReport(), func(rep *report.Report) { rep.Status = report.Status("partial") }),
			mapper: func(input recordmap.ReportMappingInput) error {
				_, err := recordmap.MapReportToExecutionRecord(input)
				return err
			},
		},
		{
			name: "unsupported step status",
			input: withReportMutation(successReport(), func(rep *report.Report) {
				rep.Jobs[0].Steps[0].Status = report.StepStatus("pending")
			}),
			mapper: func(input recordmap.ReportMappingInput) error {
				_, err := recordmap.MapReportToExecutionRecord(input)
				return err
			},
		},
		{
			name:  "negative runtime duration",
			input: withReportMutation(successReport(), func(rep *report.Report) { rep.Runtime.DurationMs = -10 }),
			mapper: func(input recordmap.ReportMappingInput) error {
				_, err := recordmap.MapReportToExecutionRecord(input)
				return err
			},
		},
		{
			name: "negative step duration",
			input: withReportMutation(successReport(), func(rep *report.Report) {
				negative := int64(-10)
				rep.Jobs[0].Steps[0].DurationMs = &negative
			}),
			mapper: func(input recordmap.ReportMappingInput) error {
				_, err := recordmap.MapReportToExecutionRecord(input)
				return err
			},
		},
		{
			name: "blank failure message",
			input: withReportMutation(failedReportWithError(), func(rep *report.Report) {
				rep.Error.Message = " "
			}),
			mapper: func(input recordmap.ReportMappingInput) error {
				_, err := recordmap.MapReportToFailureRecord(input)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.mapper(tt.input)
			assertInvalidMappingError(t, err)
		})
	}
}

func TestMapReportPreservesCallerProvenanceAndRelatedRecords(t *testing.T) {
	input := testMappingInput(successReport())
	input.Provenance = recordcontract.Provenance{
		SourceRevision: recordcontract.SourceRevisionProvenance{RevisionID: "rev-1"},
		Plan:           recordcontract.PlanProvenance{PlanID: "plan-1", PlanHash: "caller-plan-hash"},
		Evidence: []recordcontract.EvidenceReference{
			{Kind: "log", Ref: "logs/run.log"},
		},
		Runtime: recordcontract.RuntimeProvenance{ToolID: "parametron", RuntimeID: "runtime-1", Adapter: "freecad"},
	}
	input.RelatedRecords = recordcontract.ExecutionRelatedRecords{
		ArtifactRecordIDs:     []string{"artifact-record-2", "artifact-record-1"},
		FailureRecordIDs:      []string{"failure-record-1"},
		ObservationRecordIDs:  []string{"observation-record-1"},
		ReferenceRecordIDs:    []string{"reference-record-1"},
		VerificationRecordIDs: []string{"verification-record-1"},
	}

	got, err := recordmap.MapReportToExecutionRecord(input)
	if err != nil {
		t.Fatalf("MapReportToExecutionRecord(provenance) returned error: %v", err)
	}
	if got.Provenance.SourceRevision.RevisionID != "rev-1" {
		t.Fatalf("SourceRevision.RevisionID = %q, want preserved rev-1", got.Provenance.SourceRevision.RevisionID)
	}
	if got.Provenance.Plan.PlanID != "plan-1" || got.Provenance.Plan.PlanHash != "caller-plan-hash" {
		t.Fatalf("Plan provenance = %#v, want caller plan material preserved", got.Provenance.Plan)
	}
	if got.Execution.PlanHash != testPlanHash {
		t.Fatalf("Execution.PlanHash = %q, want report plan hash %q", got.Execution.PlanHash, testPlanHash)
	}
	if !hasProvenanceEvidence(got.Provenance, "log", "logs/run.log") {
		t.Fatalf("Provenance.Evidence = %#v, want caller evidence preserved", got.Provenance.Evidence)
	}
	if !hasProvenanceEvidence(got.Provenance, "report", "raw/prm.report.json") {
		t.Fatalf("Provenance.Evidence = %#v, want raw report evidence added", got.Provenance.Evidence)
	}
	if !reflect.DeepEqual(got.RelatedRecords, recordcontract.NormalizeExecutionRecord(recordcontract.ExecutionRecord{RelatedRecords: input.RelatedRecords}).RelatedRecords) {
		t.Fatalf("RelatedRecords = %#v, want normalized pass-through of %#v", got.RelatedRecords, input.RelatedRecords)
	}

	noPlanInput := testMappingInput(successReport())
	noPlanRecord, err := recordmap.MapReportToExecutionRecord(noPlanInput)
	if err != nil {
		t.Fatalf("MapReportToExecutionRecord(no plan provenance) returned error: %v", err)
	}
	if noPlanRecord.Provenance.Plan.PlanHash != testPlanHash {
		t.Fatalf("Provenance.Plan.PlanHash = %q, want enriched report plan hash", noPlanRecord.Provenance.Plan.PlanHash)
	}
}

func TestMapReportDeterministicForRepeatedEquivalentMapping(t *testing.T) {
	input := testMappingInput(failedReportWithError())

	first, err := recordmap.MapReport(input)
	if err != nil {
		t.Fatalf("MapReport(first) returned error: %v", err)
	}
	second, err := recordmap.MapReport(input)
	if err != nil {
		t.Fatalf("MapReport(second) returned error: %v", err)
	}

	firstExecution := recordcontract.NormalizeExecutionRecord(first.ExecutionRecord)
	secondExecution := recordcontract.NormalizeExecutionRecord(second.ExecutionRecord)
	if !reflect.DeepEqual(firstExecution, secondExecution) {
		t.Fatalf("execution records differ across equivalent mappings:\nfirst:  %#v\nsecond: %#v", firstExecution, secondExecution)
	}
	if first.FailureRecord == nil || second.FailureRecord == nil {
		t.Fatalf("failure records = %#v/%#v, want both non-nil", first.FailureRecord, second.FailureRecord)
	}
	firstFailure := recordcontract.NormalizeFailureRecord(*first.FailureRecord)
	secondFailure := recordcontract.NormalizeFailureRecord(*second.FailureRecord)
	if !reflect.DeepEqual(firstFailure, secondFailure) {
		t.Fatalf("failure records differ across equivalent mappings:\nfirst:  %#v\nsecond: %#v", firstFailure, secondFailure)
	}
}

func successReport() report.Report {
	started := time.Date(2026, 6, 1, 9, 0, 0, 0, time.FixedZone("UTC+3", 3*60*60))
	ended := started.Add(1250 * time.Millisecond)

	return report.Report{
		SchemaVersion: report.SchemaVersion,
		Status:        report.StatusSuccess,
		PlanHash:      testPlanHash,
		Runtime: report.Runtime{
			StartedAt:  started,
			EndedAt:    ended,
			DurationMs: 1250,
		},
		Jobs: []report.JobReport{
			{
				JobID:      "job-a",
				ProductKey: "product-a",
				Steps: []report.StepReport{
					step(0, planner.StepWriteCSV, report.StepStatusSuccess, 250),
					step(1, planner.StepRunCADRuntime, report.StepStatusSuccess, 1000),
				},
			},
		},
	}
}

func failedReportWithError() report.Report {
	rep := successReport()
	rep.Status = report.StatusFailed
	rep.Jobs[0].Steps[1].Status = report.StepStatusFailed
	rep.Error = &report.ErrorSummary{
		Message:        "verification parameter mismatch",
		Classification: verification.FailureClassParameterMismatch,
		ProductID:      "product-a",
		StepID:         "1:RunCADRuntime",
		RetryCount:     2,
		Timeout:        false,
		Canceled:       false,
	}
	return rep
}

func testMappingInput(rep report.Report) recordmap.ReportMappingInput {
	return recordmap.ReportMappingInput{
		Report:    rep,
		RecordKey: testRecordKey,
	}
}

func withReportMutation(rep report.Report, mutate func(*report.Report)) recordmap.ReportMappingInput {
	mutate(&rep)
	return testMappingInput(rep)
}

func withInputMutation(input recordmap.ReportMappingInput, mutate func(*recordmap.ReportMappingInput)) recordmap.ReportMappingInput {
	mutate(&input)
	return input
}

func step(index int, stepType planner.StepType, status report.StepStatus, durationMs int64) report.StepReport {
	started := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC).Add(time.Duration(index) * time.Second)
	ended := started.Add(time.Duration(durationMs) * time.Millisecond)
	return report.StepReport{
		Index:      index,
		Type:       stepType,
		Status:     status,
		Attempts:   1,
		StartedAt:  &started,
		EndedAt:    &ended,
		DurationMs: &durationMs,
	}
}

func stepsFromStatuses(statuses []report.StepStatus) []report.StepReport {
	steps := make([]report.StepReport, 0, len(statuses))
	for index, status := range statuses {
		stepType := planner.StepRunCADRuntime
		if index == 0 {
			stepType = planner.StepWriteCSV
		}
		steps = append(steps, step(index, stepType, status, 10))
	}
	return steps
}

func hasProvenanceEvidence(provenance recordcontract.Provenance, kind, ref string) bool {
	for _, evidence := range provenance.Evidence {
		if evidence.Kind == kind && evidence.Ref == ref {
			return true
		}
	}
	return false
}

func hasFailureEvidence(evidence []recordcontract.FailureEvidence, kind, ref string) bool {
	for _, item := range evidence {
		if item.SourceKind == kind && item.SourceRef == ref {
			return true
		}
	}
	return false
}

func assertInvalidMappingError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil, want invalid report mapping error")
	}
	if !errors.Is(err, recordmap.ErrInvalidReportMapping) {
		t.Fatalf("error = %v, want errors.Is(..., ErrInvalidReportMapping)", err)
	}
}
