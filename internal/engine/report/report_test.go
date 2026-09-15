package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/artifact"
	"parametron/internal/engine/executor"
	"parametron/internal/engine/job"
	"parametron/internal/engine/scheduler"
	"parametron/internal/engine/verification"
)

func TestBuild_DeterministicJSON(t *testing.T) {
	input := testBuildInput(t)

	firstReport, err := Build(input)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	secondReport, err := Build(input)
	if err != nil {
		t.Fatalf("second Build returned error: %v", err)
	}

	firstJSON, err := json.Marshal(firstReport)
	if err != nil {
		t.Fatalf("marshal first report: %v", err)
	}
	secondJSON, err := json.Marshal(secondReport)
	if err != nil {
		t.Fatalf("marshal second report: %v", err)
	}

	if string(firstJSON) != string(secondJSON) {
		t.Fatal("report JSON must be identical across repeated builds")
	}
}

func TestBuild_JobOrderingStability(t *testing.T) {
	input := testBuildInput(t)

	report, err := Build(input)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	if len(report.Jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(report.Jobs))
	}
	if report.Jobs[0].ProductKey != "alpha" || report.Jobs[1].ProductKey != "beta" {
		t.Fatalf("unexpected job order: %+v", report.Jobs)
	}
}

func TestBuild_StepOrderingStability(t *testing.T) {
	input := testBuildInput(t)

	report, err := Build(input)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	steps := report.Jobs[0].Steps
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(steps))
	}

	got := []planner.StepType{steps[0].Type, steps[1].Type, steps[2].Type}
	want := []planner.StepType{planner.StepWriteCSV, planner.StepWriteExportManifest, planner.StepRunCADRuntime}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected step order: got=%v want=%v", got, want)
		}
	}
}

func TestBuild_MapInsertionOrderStability(t *testing.T) {
	base := testBuildInput(t)

	left := base
	left.ProfileSettings = map[string]any{
		"zeta":  true,
		"alpha": "value",
	}

	right := base
	right.ProfileSettings = map[string]any{
		"alpha": "value",
		"zeta":  true,
	}

	leftReport, err := Build(left)
	if err != nil {
		t.Fatalf("Build(left) returned error: %v", err)
	}
	rightReport, err := Build(right)
	if err != nil {
		t.Fatalf("Build(right) returned error: %v", err)
	}

	leftJSON, err := json.Marshal(leftReport)
	if err != nil {
		t.Fatalf("marshal left report: %v", err)
	}
	rightJSON, err := json.Marshal(rightReport)
	if err != nil {
		t.Fatalf("marshal right report: %v", err)
	}

	if string(leftJSON) != string(rightJSON) {
		t.Fatalf("profile map insertion order changed report JSON:\nleft=%s\nright=%s", leftJSON, rightJSON)
	}
}

func TestBuild_ArtifactOrderingStability(t *testing.T) {
	input := testBuildInput(t)

	report, err := Build(input)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	if len(report.Artifacts) != 3 {
		t.Fatalf("expected 3 artifacts, got %d", len(report.Artifacts))
	}

	got := []string{
		report.Artifacts[0].Path,
		report.Artifacts[1].Path,
		report.Artifacts[2].Path,
	}
	want := []string{
		"products/alpha/alpha.csv",
		"products/alpha/alpha.step",
		"products/beta/" + planner.ExportManifestFilename,
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected artifact order: got=%v want=%v", got, want)
		}
	}
}

func TestBuild_FailureCasePreservesExecutionError(t *testing.T) {
	alphaJob := mustJob(t, "alpha", true)
	betaJob := mustJob(t, "beta", false)
	failure := &executor.ExecutionError{
		ProductID:  "alpha",
		StepID:     "2",
		Err:        errors.New("runner timed out"),
		RetryCount: 1,
		Timeout:    true,
	}

	report, err := Build(BuildInput{
		PlanHash:           "plan-hash",
		DSLHash:            "dsl-hash",
		ProfileName:        "Prod",
		ProfileSettings:    map[string]any{"adapter": "freecad"},
		Execution:          scheduler.ExecutionResult{Jobs: []scheduler.JobExecution{{ProductIndex: 0, Job: alphaJob, States: timedOutStates(), Err: failure}, {ProductIndex: 1, Job: betaJob}}},
		StartedAt:          fixedStart(),
		EndedAt:            fixedEnd(),
		WorkerCount:        2,
		StepTimeoutSeconds: 30,
		MaxRetries:         2,
		Err:                failure,
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	if report.Status != StatusTimeout {
		t.Fatalf("expected timeout status, got %q", report.Status)
	}
	if report.Error == nil {
		t.Fatal("expected structured error summary")
	}
	if report.Error.Message != "runner timed out" {
		t.Fatalf("unexpected error message: %+v", report.Error)
	}
	if report.Error.ProductID != "alpha" || report.Error.StepID != "2" || report.Error.RetryCount != 1 || !report.Error.Timeout {
		t.Fatalf("execution error fields were not preserved: %+v", report.Error)
	}
	if report.Jobs[0].Steps[2].Status != StepStatusTimeout {
		t.Fatalf("expected failing step timeout status, got %q", report.Jobs[0].Steps[2].Status)
	}
	if report.Jobs[1].Steps[0].Status != StepStatusSkipped {
		t.Fatalf("expected unscheduled job steps to be skipped, got %q", report.Jobs[1].Steps[0].Status)
	}
}

func TestBuild_VerificationFailureIsSurfacedDistinctly(t *testing.T) {
	alphaJob := mustJob(t, "alpha", true)
	verifyFailure := &executor.ExecutionError{
		ProductID:  "alpha",
		StepID:     "2",
		RetryCount: 1,
		Err: fmt.Errorf("verify observed execution state: %w", &verification.VerifyError{
			Class:   verification.FailureClassMetadataMismatch,
			Message: `observed metadata "working_copy_sha256" mismatch: want "aaa", got "bbb"`,
		}),
	}

	report, err := Build(BuildInput{
		PlanHash:           "plan-hash",
		DSLHash:            "dsl-hash",
		Execution:          scheduler.ExecutionResult{Jobs: []scheduler.JobExecution{{ProductIndex: 0, Job: alphaJob, States: timedOutStates(), Err: verifyFailure}}},
		StartedAt:          fixedStart(),
		EndedAt:            fixedEnd(),
		WorkerCount:        1,
		StepTimeoutSeconds: 30,
		MaxRetries:         2,
		Err:                verifyFailure,
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	if report.Status != StatusFailed {
		t.Fatalf("expected failed report status, got %q", report.Status)
	}
	if report.Error == nil {
		t.Fatal("expected structured error summary")
	}
	if report.Error.Classification != verification.FailureClassMetadataMismatch {
		t.Fatalf("expected verification classification, got %+v", report.Error)
	}
	if report.Error.Message != `observed metadata "working_copy_sha256" mismatch: want "aaa", got "bbb"` {
		t.Fatalf("unexpected verification message: %+v", report.Error)
	}
	if report.Error.Timeout || report.Error.Canceled {
		t.Fatalf("expected verification failure to stay distinct from timeout/cancel, got %+v", report.Error)
	}
}

func TestBuild_VerificationFailureJSONIsDeterministic(t *testing.T) {
	alphaJob := mustJob(t, "alpha", true)
	verifyFailure := &executor.ExecutionError{
		ProductID:  "alpha",
		StepID:     "2",
		RetryCount: 1,
		Err: fmt.Errorf("verify observed execution state: %w", &verification.VerifyError{
			Class:   verification.FailureClassReferenceMismatch,
			Message: `observed reference "working_copy_path" mismatch: want "/tmp/a.FCStd", got "/tmp/b.FCStd"`,
		}),
	}

	input := BuildInput{
		PlanHash:           "plan-hash",
		DSLHash:            "dsl-hash",
		Execution:          scheduler.ExecutionResult{Jobs: []scheduler.JobExecution{{ProductIndex: 0, Job: alphaJob, States: timedOutStates(), Err: verifyFailure}}},
		StartedAt:          fixedStart(),
		EndedAt:            fixedEnd(),
		WorkerCount:        1,
		StepTimeoutSeconds: 30,
		MaxRetries:         2,
		Err:                verifyFailure,
	}

	first, err := Build(input)
	if err != nil {
		t.Fatalf("Build(first) returned error: %v", err)
	}
	second, err := Build(input)
	if err != nil {
		t.Fatalf("Build(second) returned error: %v", err)
	}

	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal first report: %v", err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("marshal second report: %v", err)
	}
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatalf("expected identical verification failure JSON\nfirst:  %s\nsecond: %s", firstJSON, secondJSON)
	}
}

func TestWrite_CreatesReportJSON(t *testing.T) {
	runRoot := t.TempDir()
	report, err := Build(testBuildInput(t))
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	if err := Write(runRoot, report); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(runRoot, FileName))
	if err != nil {
		t.Fatalf("expected %s to exist: %v", FileName, err)
	}
	if !strings.Contains(string(data), `"schemaVersion": "1.0"`) {
		t.Fatalf("expected schemaVersion in %s, got:\n%s", FileName, string(data))
	}
	if !strings.Contains(string(data), `"status": "success"`) {
		t.Fatalf("expected success status in %s, got:\n%s", FileName, string(data))
	}
	if !strings.Contains(string(data), `"planHash": "plan-hash"`) {
		t.Fatalf("expected planHash in %s, got:\n%s", FileName, string(data))
	}
	if !strings.Contains(string(data), `"jobs": [`) {
		t.Fatalf("expected jobs in %s, got:\n%s", FileName, string(data))
	}

	if _, err := os.Stat(filepath.Join(runRoot, "report.json")); !os.IsNotExist(err) {
		t.Fatalf("expected legacy report.json to not be written, stat err: %v", err)
	}
}

func TestWrite_OnDiskBytesDeterministic(t *testing.T) {
	input := testBuildInput(t)

	// Create two distinct output directories
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	// Build reports
	report1, err := Build(input)
	if err != nil {
		t.Fatalf("Build 1 failed: %v", err)
	}
	report2, err := Build(input)
	if err != nil {
		t.Fatalf("Build 2 failed: %v", err)
	}

	// Write to disk
	if err := Write(dir1, report1); err != nil {
		t.Fatalf("Write 1 failed: %v", err)
	}
	if err := Write(dir2, report2); err != nil {
		t.Fatalf("Write 2 failed: %v", err)
	}

	// Read bytes
	path1 := filepath.Join(dir1, FileName)
	path2 := filepath.Join(dir2, FileName)

	bytes1, err := os.ReadFile(path1)
	if err != nil {
		t.Fatalf("read 1 failed: %v", err)
	}
	bytes2, err := os.ReadFile(path2)
	if err != nil {
		t.Fatalf("read 2 failed: %v", err)
	}

	// Strict byte comparison
	if !bytes.Equal(bytes1, bytes2) {
		t.Fatal("report.json bytes differ between identical runs")
	}
}

func testBuildInput(t *testing.T) BuildInput {
	t.Helper()

	alphaJob := mustJob(t, "alpha", true)
	betaJob := mustJob(t, "beta", false)

	return BuildInput{
		PlanHash:        "plan-hash",
		DSLHash:         "dsl-hash",
		ProfileName:     "Prod",
		ProfileSettings: map[string]any{"adapter": "freecad", "retries": 2},
		Execution: scheduler.ExecutionResult{
			Jobs: []scheduler.JobExecution{
				{
					ProductIndex: 0,
					Job:          alphaJob,
					States: []executor.ExecutionState{
						successState("alpha", "0", fixedStart(), fixedStart().Add(10*time.Millisecond), 1),
						successState("alpha", "1", fixedStart().Add(20*time.Millisecond), fixedStart().Add(30*time.Millisecond), 1),
						successState("alpha", "2", fixedStart().Add(40*time.Millisecond), fixedStart().Add(50*time.Millisecond), 1),
					},
				},
				{
					ProductIndex: 1,
					Job:          betaJob,
					States: []executor.ExecutionState{
						successState("beta", "0", fixedStart().Add(60*time.Millisecond), fixedStart().Add(70*time.Millisecond), 1),
						successState("beta", "1", fixedStart().Add(80*time.Millisecond), fixedStart().Add(90*time.Millisecond), 1),
					},
				},
			},
		},
		Artifacts: []artifact.Artifact{
			{
				ID:             "b",
				Type:           artifact.ArtifactTypeSTEP,
				ProductID:      "alpha",
				StepID:         "2",
				Filename:       "alpha.step",
				Path:           "products/alpha/alpha.step",
				ChecksumSHA256: "bbb",
				SizeBytes:      20,
			},
			{
				ID:             "c",
				Type:           artifact.ArtifactTypeJSON,
				ProductID:      "beta",
				StepID:         "1",
				Filename:       planner.ExportManifestFilename,
				Path:           "products/beta/" + planner.ExportManifestFilename,
				ChecksumSHA256: "ccc",
				SizeBytes:      30,
			},
			{
				ID:             "a",
				Type:           artifact.ArtifactTypeCSV,
				ProductID:      "alpha",
				StepID:         "0",
				Filename:       "alpha.csv",
				Path:           "products/alpha/alpha.csv",
				ChecksumSHA256: "aaa",
				SizeBytes:      10,
			},
		},
		StartedAt:          fixedStart(),
		EndedAt:            fixedEnd(),
		WorkerCount:        2,
		StepTimeoutSeconds: 30,
		MaxRetries:         2,
	}
}

func mustJob(t *testing.T, productKey string, includeRunner bool) *job.Job {
	t.Helper()

	steps := []planner.Step{
		{
			Type: planner.StepWriteCSV,
			Payload: planner.WriteCSVPayload{
				ProductKey: productKey,
				Filename:   productKey + ".csv",
				Headers:    []string{"width"},
				Values:     []any{100},
			},
		},
		{
			Type: planner.StepWriteExportManifest,
			Payload: planner.WriteExportManifestPayload{
				ProductKey:       productKey,
				ManifestFilename: planner.ExportManifestFilename,
				SchemaVersion:    planner.ExportManifestSchemaVersion,
				PlanHash:         "plan-hash",
				Adapter:          "freecad",
				Product:          planner.ExportManifestProduct{ID: productKey},
				Inputs:           planner.ExportManifestInputs{SourceModel: "input/model.FCStd"},
				Values:           map[string]interface{}{"width": 100},
				Outputs: []planner.ExportManifestOutput{
					{Type: "step", Filename: productKey + ".step", Object: "Body"},
				},
			},
		},
	}
	if includeRunner {
		steps = append(steps, planner.Step{
			Type: planner.StepRunCADRuntime,
			Payload: planner.RunCADRuntimePayload{
				ProductKey:       productKey,
				Adapter:          "freecad",
				ManifestFilename: planner.ExportManifestFilename,
				ResultFilename:   planner.FreeCADRuntimeResultFilename,
			},
		})
	}

	j, err := job.New(productKey, steps)
	if err != nil {
		t.Fatalf("job.New returned error: %v", err)
	}
	return j
}

func successState(productID, stepID string, startedAt, endedAt time.Time, attempts int) executor.ExecutionState {
	return executor.ExecutionState{
		ProductID: productID,
		StepID:    stepID,
		Status:    executor.StepSuccess,
		StartedAt: startedAt,
		EndedAt:   endedAt,
		Attempts:  attempts,
	}
}

func timedOutStates() []executor.ExecutionState {
	start := fixedStart()
	return []executor.ExecutionState{
		successState("alpha", "0", start, start.Add(10*time.Millisecond), 1),
		successState("alpha", "1", start.Add(20*time.Millisecond), start.Add(30*time.Millisecond), 1),
		{
			ProductID: "alpha",
			StepID:    "2",
			Status:    executor.StepFailed,
			StartedAt: start.Add(40 * time.Millisecond),
			EndedAt:   start.Add(70 * time.Millisecond),
			Attempts:  1,
		},
	}
}

func fixedStart() time.Time {
	return time.Date(2026, 3, 16, 12, 0, 0, 0, time.UTC)
}

func fixedEnd() time.Time {
	return fixedStart().Add(100 * time.Millisecond)
}
