package executor

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"parametron/internal/authoring/planner"
)

type testContextKey string

type recordingAdapter struct {
	calls      []planner.Step
	lastCtx    context.Context
	failAt     int
	failAlways bool
	failWith   error
	onRun      func(callIndex int)
	runFn      func(ctx context.Context, callIndex int, step planner.Step) error
}

func (a *recordingAdapter) Run(ctx context.Context, step planner.Step) error {
	a.lastCtx = ctx
	a.calls = append(a.calls, step)
	callIndex := len(a.calls) - 1
	if a.runFn != nil {
		return a.runFn(ctx, callIndex, step)
	}
	if a.onRun != nil {
		a.onRun(callIndex)
	}

	if a.failWith != nil && (a.failAlways || callIndex == a.failAt) {
		return a.failWith
	}

	return nil
}

func TestExecutorExecute_SequentialAndContextPropagation(t *testing.T) {
	ctx := context.WithValue(context.Background(), testContextKey("k"), "v")
	plan := &planner.ExecutionPlan{
		Steps: []planner.Step{
			{Type: planner.StepWriteCSV},
			{Type: planner.StepWriteExportManifest},
		},
	}

	adp := &recordingAdapter{}
	exec := New(adp)

	if err := exec.Execute(ctx, plan); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(adp.calls) != 2 {
		t.Fatalf("expected 2 adapter calls, got %d", len(adp.calls))
	}
	if adp.calls[0].Type != planner.StepWriteCSV || adp.calls[1].Type != planner.StepWriteExportManifest {
		t.Fatalf("steps were not executed in order: %+v", adp.calls)
	}
	if adp.lastCtx == nil {
		t.Fatal("executor did not provide adapter context")
	}
	if got := adp.lastCtx.Value(testContextKey("k")); got != "v" {
		t.Fatalf("executor did not propagate context values, got %v", got)
	}
}

func TestExecutorExecute_StepStateTransition_Success(t *testing.T) {
	plan := &planner.ExecutionPlan{
		Steps: []planner.Step{
			{
				Type: planner.StepWriteCSV,
				Payload: planner.WriteCSVPayload{
					Filename: "widget.csv",
				},
			},
		},
	}

	adp := &recordingAdapter{}
	exec := New(adp)

	if err := exec.Execute(context.Background(), plan); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(exec.states) != 1 {
		t.Fatalf("expected 1 execution state, got %d", len(exec.states))
	}
	state := exec.states[0]
	if state.ProductID != "widget" {
		t.Fatalf("expected product id widget, got %q", state.ProductID)
	}
	if state.StepID != "0" {
		t.Fatalf("expected step id 0, got %q", state.StepID)
	}
	if state.Status != StepSuccess {
		t.Fatalf("expected status %q, got %q", StepSuccess, state.Status)
	}
	if state.Attempts != 1 {
		t.Fatalf("expected attempts 1, got %d", state.Attempts)
	}
	if state.StartedAt.IsZero() {
		t.Fatal("expected StartedAt to be set")
	}
	if state.EndedAt.IsZero() {
		t.Fatal("expected EndedAt to be set")
	}
}

func TestExecutorExecute_PropagatesExplicitProductKeyToStates(t *testing.T) {
	plan := &planner.ExecutionPlan{
		Steps: []planner.Step{
			{
				Type: planner.StepWriteCSV,
				Payload: planner.WriteCSVPayload{
					ProductKey: "A",
					Filename:   "Dev_A_hash_1.csv",
				},
			},
			{
				Type: planner.StepWriteExportManifest,
				Payload: planner.WriteExportManifestPayload{
					ProductKey:       "A",
					ManifestFilename: planner.ExportManifestFilename,
				},
			},
		},
	}

	adp := &recordingAdapter{}
	exec := New(adp)

	if err := exec.Execute(context.Background(), plan); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	got := []string{exec.states[0].ProductID, exec.states[1].ProductID}
	want := []string{"A", "A"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected propagated product ids: got=%v want=%v", got, want)
	}
}

func TestExecutorExecute_StepStateTransition_Failed(t *testing.T) {
	expectedErr := errors.New("boom")
	plan := &planner.ExecutionPlan{
		Steps: []planner.Step{
			{
				Type: planner.StepWriteCSV,
				Payload: planner.WriteCSVPayload{
					Filename: "widget.csv",
				},
			},
			{
				Type:    planner.StepWriteExportManifest,
				Payload: planner.WriteExportManifestPayload{},
			},
		},
	}

	adp := &recordingAdapter{
		failAlways: true,
		failWith:   expectedErr,
	}
	exec := New(adp)

	err := exec.Execute(context.Background(), plan)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if len(exec.states) != 2 {
		t.Fatalf("expected 2 execution states, got %d", len(exec.states))
	}

	failed := exec.states[0]
	if failed.Status != StepFailed {
		t.Fatalf("expected first step status %q, got %q", StepFailed, failed.Status)
	}
	if failed.Attempts != defaultMaxRetries+1 {
		t.Fatalf("expected first step attempts %d, got %d", defaultMaxRetries+1, failed.Attempts)
	}
	if failed.StartedAt.IsZero() || failed.EndedAt.IsZero() {
		t.Fatal("expected failed step timestamps to be set")
	}

	pending := exec.states[1]
	if pending.Status != StepPending {
		t.Fatalf("expected second step status %q, got %q", StepPending, pending.Status)
	}
	if pending.Attempts != 0 {
		t.Fatalf("expected second step attempts 0, got %d", pending.Attempts)
	}
}

func TestExecutorExecute_StepStateTimestampOrder(t *testing.T) {
	plan := &planner.ExecutionPlan{
		Steps: []planner.Step{
			{
				Type: planner.StepWriteCSV,
				Payload: planner.WriteCSVPayload{
					Filename: "widget.csv",
				},
			},
		},
	}
	exec := New(&recordingAdapter{})

	if err := exec.Execute(context.Background(), plan); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	state := exec.states[0]
	if !state.StartedAt.Before(state.EndedAt) {
		t.Fatalf("expected StartedAt < EndedAt, got StartedAt=%v EndedAt=%v", state.StartedAt, state.EndedAt)
	}
}

func TestExecutorExecute_CanceledBeforeFirstStep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	plan := &planner.ExecutionPlan{
		Steps: []planner.Step{
			{
				Type: planner.StepWriteCSV,
				Payload: planner.WriteCSVPayload{
					Filename: "widget.csv",
				},
			},
		},
	}

	adp := &recordingAdapter{}
	exec := New(adp)

	err := exec.Execute(ctx, plan)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var execErr *ExecutionError
	if !errors.As(err, &execErr) {
		t.Fatalf("expected errors.As to resolve *ExecutionError, got %T", err)
	}
	if !execErr.Canceled {
		t.Fatal("expected canceled execution error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected wrapped context.Canceled, got %v", err)
	}
	if len(adp.calls) != 0 {
		t.Fatalf("expected no adapter calls, got %d", len(adp.calls))
	}
	if exec.states[0].Status != StepFailed {
		t.Fatalf("expected first step status %q, got %q", StepFailed, exec.states[0].Status)
	}
}

func TestExecutorExecute_CanceledBetweenSteps(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	plan := &planner.ExecutionPlan{
		Steps: []planner.Step{
			{
				Type: planner.StepWriteCSV,
				Payload: planner.WriteCSVPayload{
					Filename: "widget.csv",
				},
			},
			{
				Type:    planner.StepWriteExportManifest,
				Payload: planner.WriteExportManifestPayload{},
			},
		},
	}

	adp := &recordingAdapter{
		onRun: func(callIndex int) {
			if callIndex == 0 {
				cancel()
			}
		},
	}
	exec := New(adp)

	err := exec.Execute(ctx, plan)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var execErr *ExecutionError
	if !errors.As(err, &execErr) {
		t.Fatalf("expected errors.As to resolve *ExecutionError, got %T", err)
	}
	if !execErr.Canceled {
		t.Fatal("expected canceled execution error")
	}
	if len(adp.calls) != 1 {
		t.Fatalf("expected first step only, got %d calls", len(adp.calls))
	}
	if exec.states[0].Status != StepSuccess {
		t.Fatalf("expected first step status %q, got %q", StepSuccess, exec.states[0].Status)
	}
	if exec.states[1].Status != StepFailed {
		t.Fatalf("expected second step status %q, got %q", StepFailed, exec.states[1].Status)
	}
}

func TestExecutorExecute_AdapterReturnsContextCanceled(t *testing.T) {
	plan := &planner.ExecutionPlan{
		Steps: []planner.Step{
			{
				Type: planner.StepWriteCSV,
				Payload: planner.WriteCSVPayload{
					Filename: "widget.csv",
				},
			},
		},
	}

	adp := &recordingAdapter{
		failAt:   0,
		failWith: context.Canceled,
	}
	exec := New(adp)

	err := exec.Execute(context.Background(), plan)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var execErr *ExecutionError
	if !errors.As(err, &execErr) {
		t.Fatalf("expected errors.As to resolve *ExecutionError, got %T", err)
	}
	if !execErr.Canceled {
		t.Fatal("expected canceled execution error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected wrapped context.Canceled, got %v", err)
	}
	if exec.states[0].Status != StepFailed {
		t.Fatalf("expected first step status %q, got %q", StepFailed, exec.states[0].Status)
	}
	if exec.states[0].Attempts != 1 {
		t.Fatalf("expected first step attempts 1, got %d", exec.states[0].Attempts)
	}
	if execErr.RetryCount != 1 {
		t.Fatalf("expected retry count 1, got %d", execErr.RetryCount)
	}
}

func TestExecutorExecute_StepCompletesBeforeTimeout(t *testing.T) {
	plan := &planner.ExecutionPlan{
		Steps: []planner.Step{
			{
				Type: planner.StepWriteCSV,
				Payload: planner.WriteCSVPayload{
					Filename: "widget.csv",
				},
			},
		},
	}

	adp := &recordingAdapter{
		runFn: func(ctx context.Context, _ int, _ planner.Step) error {
			select {
			case <-time.After(5 * time.Millisecond):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
	exec := New(adp)

	if err := exec.Execute(context.Background(), plan); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(exec.states) != 1 {
		t.Fatalf("expected 1 execution state, got %d", len(exec.states))
	}
	if exec.states[0].Status != StepSuccess {
		t.Fatalf("expected status %q, got %q", StepSuccess, exec.states[0].Status)
	}
}

func TestExecutorExecute_StepExceedsTimeout(t *testing.T) {
	plan := &planner.ExecutionPlan{
		Steps: []planner.Step{
			{
				Type: planner.StepWriteCSV,
				Payload: planner.WriteCSVPayload{
					Filename: "widget.csv",
				},
			},
		},
	}

	adp := &recordingAdapter{
		runFn: func(ctx context.Context, _ int, _ planner.Step) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	exec := New(adp)

	parentCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := exec.Execute(parentCtx, plan)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}

	var execErr *ExecutionError
	if !errors.As(err, &execErr) {
		t.Fatalf("expected errors.As to resolve *ExecutionError, got %T", err)
	}
	if !execErr.Timeout {
		t.Fatal("expected timeout execution error")
	}
	if execErr.Canceled {
		t.Fatal("expected canceled=false for timeout")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected wrapped context.DeadlineExceeded, got %v", err)
	}
	if execErr.RetryCount != 1 {
		t.Fatalf("expected retry count 1 for timeout, got %d", execErr.RetryCount)
	}
	if len(adp.calls) != 1 {
		t.Fatalf("expected no retry on timeout, got %d calls", len(adp.calls))
	}
	if exec.states[0].Status != StepFailed {
		t.Fatalf("expected first step status %q, got %q", StepFailed, exec.states[0].Status)
	}
	if exec.states[0].EndedAt.IsZero() {
		t.Fatal("expected first step EndedAt to be set")
	}
	if exec.states[0].Attempts != 1 {
		t.Fatalf("expected first step attempts 1, got %d", exec.states[0].Attempts)
	}
}

func TestExecutorExecute_ReturnsStepError(t *testing.T) {
	expectedErr := errors.New("boom")
	plan := &planner.ExecutionPlan{
		Steps: []planner.Step{
			{
				Type: planner.StepWriteCSV,
				Payload: planner.WriteCSVPayload{
					Filename: "widget.csv",
				},
			},
			{
				Type:    planner.StepWriteExportManifest,
				Payload: planner.WriteExportManifestPayload{Product: planner.ExportManifestProduct{ID: "widget"}},
			},
		},
	}

	adp := &recordingAdapter{
		runFn: func(_ context.Context, callIndex int, _ planner.Step) error {
			if callIndex >= 1 {
				return expectedErr
			}
			return nil
		},
	}
	exec := New(adp)

	err := exec.Execute(context.Background(), plan)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected wrapped error %v, got %v", expectedErr, err)
	}
	var execErr *ExecutionError
	if !errors.As(err, &execErr) {
		t.Fatalf("expected errors.As to resolve *ExecutionError, got %T", err)
	}
	if execErr.ProductID != "widget" {
		t.Fatalf("expected product id widget, got %q", execErr.ProductID)
	}
	if execErr.StepID != "1" {
		t.Fatalf("expected step id 1, got %q", execErr.StepID)
	}
	if execErr.Unwrap() != expectedErr {
		t.Fatalf("expected Unwrap to return original error %v, got %v", expectedErr, execErr.Unwrap())
	}
	if execErr.RetryCount != defaultMaxRetries+1 {
		t.Fatalf("expected retry count %d, got %d", defaultMaxRetries+1, execErr.RetryCount)
	}
	if len(adp.calls) != 1+(defaultMaxRetries+1) {
		t.Fatalf("expected retries on second step, got %d calls", len(adp.calls))
	}
}

func TestExecutorExecute_StepFailsOnceThenSucceeds(t *testing.T) {
	plan := &planner.ExecutionPlan{
		Steps: []planner.Step{
			{
				Type: planner.StepWriteCSV,
				Payload: planner.WriteCSVPayload{
					Filename: "widget.csv",
				},
			},
		},
	}

	expectedErr := errors.New("flaky")
	adp := &recordingAdapter{
		runFn: func(_ context.Context, callIndex int, _ planner.Step) error {
			if callIndex == 0 {
				return expectedErr
			}
			return nil
		},
	}
	exec := New(adp)

	if err := exec.Execute(context.Background(), plan); err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}
	if len(adp.calls) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(adp.calls))
	}
	if exec.states[0].Status != StepSuccess {
		t.Fatalf("expected step status %q, got %q", StepSuccess, exec.states[0].Status)
	}
	if exec.states[0].Attempts != 2 {
		t.Fatalf("expected attempts 2, got %d", exec.states[0].Attempts)
	}
}

func TestExecutorExecute_StepFailsAllRetries(t *testing.T) {
	plan := &planner.ExecutionPlan{
		Steps: []planner.Step{
			{
				Type: planner.StepWriteCSV,
				Payload: planner.WriteCSVPayload{
					Filename: "widget.csv",
				},
			},
		},
	}

	expectedErr := errors.New("boom")
	adp := &recordingAdapter{
		runFn: func(_ context.Context, _ int, _ planner.Step) error {
			return expectedErr
		},
	}
	exec := New(adp)

	err := exec.Execute(context.Background(), plan)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var execErr *ExecutionError
	if !errors.As(err, &execErr) {
		t.Fatalf("expected errors.As to resolve *ExecutionError, got %T", err)
	}
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected wrapped error %v, got %v", expectedErr, err)
	}
	if execErr.RetryCount != defaultMaxRetries+1 {
		t.Fatalf("expected retry count %d, got %d", defaultMaxRetries+1, execErr.RetryCount)
	}
	if len(adp.calls) != defaultMaxRetries+1 {
		t.Fatalf("expected %d attempts, got %d", defaultMaxRetries+1, len(adp.calls))
	}
	if exec.states[0].Status != StepFailed {
		t.Fatalf("expected step status %q, got %q", StepFailed, exec.states[0].Status)
	}
	if exec.states[0].Attempts != defaultMaxRetries+1 {
		t.Fatalf("expected attempts %d, got %d", defaultMaxRetries+1, exec.states[0].Attempts)
	}
}

func TestExecutionError_ErrorAndUnwrap(t *testing.T) {
	root := errors.New("root")
	err := &ExecutionError{
		ProductID: "gear",
		StepID:    "3",
		Err:       root,
	}

	if err.Error() != "product gear step 3 failed: root" {
		t.Fatalf("unexpected error string: %q", err.Error())
	}
	if err.Unwrap() != root {
		t.Fatalf("expected Unwrap to return root, got %v", err.Unwrap())
	}
}

func TestExecutorExecute_NilPlan(t *testing.T) {
	exec := New(&recordingAdapter{})
	if err := exec.Execute(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil plan, got nil")
	}
}
