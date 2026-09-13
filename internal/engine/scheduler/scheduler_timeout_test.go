package scheduler

import (
	"context"
	"errors"
	"testing"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/executor"
)

func buildTimeoutPlan() *planner.ExecutionPlan {
	return productPlan("timeout")
}

func TestScheduler_TimeoutNotRetryable(t *testing.T) {
	ctx := context.Background()
	plan := buildTimeoutPlan()

	adp := &fakeAdapter{
		runFn: func(_ context.Context, _ int, step planner.Step) error {
			if step.Type == planner.StepWriteExportManifest {
				return context.DeadlineExceeded
			}
			return nil
		},
	}
	s := New(executor.New(adp))

	err := s.Execute(ctx, plan)
	if err == nil {
		t.Fatalf("expected timeout error")
	}

	var execErr *executor.ExecutionError
	if !errors.As(err, &execErr) {
		t.Fatalf("expected executor.ExecutionError, got %T (%v)", err, err)
	}

	if !execErr.Timeout {
		t.Fatalf("expected timeout classification, got %+v", execErr)
	}

	// RetryCount currently represents attempts (not extra retries).
	// Timeout should stop immediately on first attempt.
	if execErr.RetryCount != 1 {
		t.Fatalf("timeout must not retry, expected attempts=1 got %d", execErr.RetryCount)
	}

	if got := adp.callCount(); got != 2 {
		t.Fatalf("expected exactly 2 adapter calls (WriteCSV + WriteExportManifest), got %d", got)
	}
}
