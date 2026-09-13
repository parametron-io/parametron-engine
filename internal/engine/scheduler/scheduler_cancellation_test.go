package scheduler

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/executor"
)

func buildSlowPlan(productCount int) *planner.ExecutionPlan {
	names := make([]string, 0, productCount)
	for i := 0; i < productCount; i++ {
		names = append(names, fmt.Sprintf("slow-%03d", i))
	}
	return productPlan(names...)
}

func TestScheduler_CancellationUnderLoad(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	plan := buildSlowPlan(200)

	adp := &fakeAdapter{
		runFn: func(ctx context.Context, _ int, _ planner.Step) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(25 * time.Millisecond):
				return nil
			}
		},
	}
	s := New(executor.New(adp))

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := s.Execute(ctx, plan)
	if err == nil {
		t.Fatalf("expected cancellation error")
	}

	var execErr *executor.ExecutionError
	if !errors.As(err, &execErr) {
		t.Fatalf("expected executor.ExecutionError, got %T (%v)", err, err)
	}
	if !execErr.Canceled {
		t.Fatalf("expected cancellation classification, got %+v", execErr)
	}
}
