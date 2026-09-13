package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/artifact"
	"parametron/internal/engine/executor"
	"parametron/internal/engine/handoff"
	"parametron/internal/engine/scheduler"
	"parametron/internal/engine/verification"
)

const SchemaVersion = "1.0"

type Status string

const (
	StatusSuccess  Status = "success"
	StatusFailed   Status = "failed"
	StatusCanceled Status = "canceled"
	StatusTimeout  Status = "timeout"
)

type StepStatus string

const (
	StepStatusSuccess  StepStatus = "success"
	StepStatusFailed   StepStatus = "failed"
	StepStatusSkipped  StepStatus = "skipped"
	StepStatusCanceled StepStatus = "canceled"
	StepStatusTimeout  StepStatus = "timeout"
)

type Report struct {
	SchemaVersion string         `json:"schemaVersion"`
	Status        Status         `json:"status"`
	PlanHash      string         `json:"planHash"`
	DSLHash       string         `json:"dslHash"`
	Profile       *Profile       `json:"profile,omitempty"`
	Runtime       Runtime        `json:"runtime"`
	Jobs          []JobReport    `json:"jobs"`
	Artifacts     []ArtifactInfo `json:"artifacts"`
	Error         *ErrorSummary  `json:"error,omitempty"`
}

type Profile struct {
	Name             string         `json:"name"`
	ResolvedSettings map[string]any `json:"resolvedSettings"`
}

type Runtime struct {
	StartedAt          time.Time `json:"startedAt"`
	EndedAt            time.Time `json:"endedAt"`
	DurationMs         int64     `json:"durationMs"`
	WorkerCount        int       `json:"workerCount"`
	StepTimeoutSeconds int       `json:"stepTimeoutSeconds"`
	MaxRetries         int       `json:"maxRetries"`
}

type JobReport struct {
	JobID             string       `json:"jobId"`
	ProductKey        string       `json:"productKey"`
	ExpectedArtifacts []string     `json:"expectedArtifacts"`
	Steps             []StepReport `json:"steps"`
}

type StepReport struct {
	Index      int              `json:"index"`
	Type       planner.StepType `json:"type"`
	Status     StepStatus       `json:"status"`
	Attempts   int              `json:"attempts"`
	StartedAt  *time.Time       `json:"startedAt,omitempty"`
	EndedAt    *time.Time       `json:"endedAt,omitempty"`
	DurationMs *int64           `json:"durationMs,omitempty"`
}

type ArtifactInfo struct {
	ProductID      string `json:"productId,omitempty"`
	Type           string `json:"type"`
	Filename       string `json:"filename"`
	Path           string `json:"path"`
	ChecksumSHA256 string `json:"checksumSHA256,omitempty"`
	SizeBytes      int64  `json:"sizeBytes"`
}

type ErrorSummary struct {
	Message        string                    `json:"message"`
	Classification verification.FailureClass `json:"classification,omitempty"`
	ProductID      string                    `json:"productId,omitempty"`
	StepID         string                    `json:"stepId,omitempty"`
	RetryCount     int                       `json:"retryCount,omitempty"`
	Timeout        bool                      `json:"timeout"`
	Canceled       bool                      `json:"canceled"`
	Boundary       string                    `json:"boundary,omitempty"`
	Category       string                    `json:"category,omitempty"`
	Code           string                    `json:"code,omitempty"`
	Stage          string                    `json:"stage,omitempty"`
	AttemptID      string                    `json:"attemptId,omitempty"`
}

type BuildInput struct {
	PlanHash           string
	DSLHash            string
	ProfileName        string
	ProfileSettings    map[string]any
	Execution          scheduler.ExecutionResult
	Artifacts          []artifact.Artifact
	StartedAt          time.Time
	EndedAt            time.Time
	WorkerCount        int
	StepTimeoutSeconds int
	MaxRetries         int
	Err                error
}

func (p Profile) MarshalJSON() ([]byte, error) {
	var out bytes.Buffer
	out.WriteByte('{')

	nameBytes, err := json.Marshal(p.Name)
	if err != nil {
		return nil, fmt.Errorf("marshal profile name: %w", err)
	}
	out.WriteString(`"name":`)
	out.Write(nameBytes)
	out.WriteByte(',')
	out.WriteString(`"resolvedSettings":`)

	settingsBytes, err := marshalSortedMap(p.ResolvedSettings)
	if err != nil {
		return nil, err
	}
	out.Write(settingsBytes)
	out.WriteByte('}')
	return out.Bytes(), nil
}

func Build(input BuildInput) (Report, error) {
	startedAt := input.StartedAt.UTC()
	endedAt := input.EndedAt.UTC()
	if endedAt.Before(startedAt) {
		endedAt = startedAt
	}

	report := Report{
		SchemaVersion: SchemaVersion,
		Status:        statusFromError(input.Err),
		PlanHash:      input.PlanHash,
		DSLHash:       input.DSLHash,
		Runtime: Runtime{
			StartedAt:          startedAt,
			EndedAt:            endedAt,
			DurationMs:         endedAt.Sub(startedAt).Milliseconds(),
			WorkerCount:        input.WorkerCount,
			StepTimeoutSeconds: input.StepTimeoutSeconds,
			MaxRetries:         input.MaxRetries,
		},
		Jobs:      make([]JobReport, 0, len(input.Execution.Jobs)),
		Artifacts: buildArtifacts(input.Artifacts),
		Error:     buildErrorSummary(input.Err),
	}

	if input.ProfileName != "" || len(input.ProfileSettings) > 0 {
		report.Profile = &Profile{
			Name:             input.ProfileName,
			ResolvedSettings: cloneMap(input.ProfileSettings),
		}
	}

	for _, jobExecution := range input.Execution.Jobs {
		if jobExecution.Job == nil {
			continue
		}

		handoffPackage, err := handoff.FromJob(jobExecution.Job)
		if err != nil {
			return Report{}, fmt.Errorf("build handoff package for report job %q: %w", jobExecution.Job.ID, err)
		}

		jobReport := JobReport{
			JobID:             jobExecution.Job.ID,
			ProductKey:        jobExecution.Job.ProductKey,
			ExpectedArtifacts: append([]string(nil), handoffPackage.ExpectedArtifacts()...),
			Steps:             buildSteps(handoffPackage, jobExecution.States, jobExecution.Err),
		}
		report.Jobs = append(report.Jobs, jobReport)
	}

	return report, nil
}

func statusFromError(err error) Status {
	if err == nil {
		return StatusSuccess
	}

	var executionErr *executor.ExecutionError
	if errors.As(err, &executionErr) {
		if executionErr.Timeout {
			return StatusTimeout
		}
		if executionErr.Canceled {
			return StatusCanceled
		}
	}

	return StatusFailed
}

func buildSteps(pkg *handoff.Package, states []executor.ExecutionState, err error) []StepReport {
	steps := make([]StepReport, 0, len(pkg.Steps))
	var executionErr *executor.ExecutionError
	_ = errors.As(err, &executionErr)

	for index, snapshot := range pkg.Steps {
		stepReport := StepReport{
			Index:    index,
			Type:     snapshot.Type,
			Status:   StepStatusSkipped,
			Attempts: 0,
		}

		if index < len(states) {
			state := states[index]
			stepReport.Attempts = state.Attempts
			if !state.StartedAt.IsZero() {
				startedAt := state.StartedAt.UTC()
				stepReport.StartedAt = &startedAt
			}
			if !state.EndedAt.IsZero() {
				endedAt := state.EndedAt.UTC()
				stepReport.EndedAt = &endedAt
			}
			if stepReport.StartedAt != nil && stepReport.EndedAt != nil {
				duration := stepReport.EndedAt.Sub(*stepReport.StartedAt).Milliseconds()
				stepReport.DurationMs = &duration
			}
			stepReport.Status = mapStepStatus(index, state, executionErr)
		}

		steps = append(steps, stepReport)
	}

	return steps
}

func mapStepStatus(index int, state executor.ExecutionState, executionErr *executor.ExecutionError) StepStatus {
	if executionErr != nil {
		stepID := strconv.Itoa(index)
		if executionErr.StepID == stepID {
			if executionErr.Timeout {
				return StepStatusTimeout
			}
			if executionErr.Canceled {
				return StepStatusCanceled
			}
		}
	}

	switch state.Status {
	case executor.StepSuccess:
		return StepStatusSuccess
	case executor.StepFailed:
		return StepStatusFailed
	default:
		return StepStatusSkipped
	}
}

func buildArtifacts(items []artifact.Artifact) []ArtifactInfo {
	artifacts := make([]artifact.Artifact, len(items))
	copy(artifacts, items)
	sort.Slice(artifacts, func(i, j int) bool {
		if artifacts[i].ProductID != artifacts[j].ProductID {
			return artifacts[i].ProductID < artifacts[j].ProductID
		}
		if artifacts[i].StepID != artifacts[j].StepID {
			return artifacts[i].StepID < artifacts[j].StepID
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

	out := make([]ArtifactInfo, 0, len(artifacts))
	for _, item := range artifacts {
		out = append(out, ArtifactInfo{
			ProductID:      item.ProductID,
			Type:           string(item.Type),
			Filename:       item.Filename,
			Path:           item.Path,
			ChecksumSHA256: item.ChecksumSHA256,
			SizeBytes:      item.SizeBytes,
		})
	}
	return out
}

func buildErrorSummary(err error) *ErrorSummary {
	if err == nil {
		return nil
	}

	summary := &ErrorSummary{
		Message: err.Error(),
	}

	var executionErr *executor.ExecutionError
	if errors.As(err, &executionErr) {
		summary.Message = executionErr.Err.Error()
		summary.ProductID = executionErr.ProductID
		summary.StepID = executionErr.StepID
		summary.RetryCount = executionErr.RetryCount
		summary.Timeout = executionErr.Timeout
		summary.Canceled = executionErr.Canceled

		var verifyErr *verification.VerifyError
		if errors.As(executionErr.Err, &verifyErr) {
			summary.Message = verifyErr.Message
			summary.Classification = verifyErr.Class
		}
		var consumptionErr *executor.CADRuntimeConsumptionError
		if errors.As(executionErr.Err, &consumptionErr) {
			summary.Message = consumptionErr.Message
			if summary.Message == "" {
				summary.Message = consumptionErr.Error()
			}
			summary.Classification = verification.FailureClass(consumptionErr.Classification)
			summary.Boundary = consumptionErr.Boundary
			summary.Category = consumptionErr.Category
			summary.Code = consumptionErr.Code
			summary.Stage = consumptionErr.NativeStage
			summary.AttemptID = consumptionErr.AttemptID
		}
	}

	return summary
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func marshalSortedMap(values map[string]any) ([]byte, error) {
	if len(values) == 0 {
		return []byte(`{}`), nil
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var out bytes.Buffer
	out.WriteByte('{')
	for index, key := range keys {
		if index > 0 {
			out.WriteByte(',')
		}
		keyBytes, err := json.Marshal(key)
		if err != nil {
			return nil, fmt.Errorf("marshal profile key %q: %w", key, err)
		}
		valueBytes, err := json.Marshal(values[key])
		if err != nil {
			return nil, fmt.Errorf("marshal profile value for key %q: %w", key, err)
		}
		out.Write(keyBytes)
		out.WriteByte(':')
		out.Write(valueBytes)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}
