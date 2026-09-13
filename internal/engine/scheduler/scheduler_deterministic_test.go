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

func buildMixedLatencyPlan() *planner.ExecutionPlan {
	return productPlan("p0", "p1", "p2", "p3", "p4", "p5")
}

func TestScheduler_DeterministicOrder(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	plan := buildMixedLatencyPlan()

	expectedErr := errors.New("err-p0")
	errorByProduct := map[string]error{
		"p0.json": expectedErr,
		"p1.json": errors.New("err-p1"),
		"p2.json": errors.New("err-p2"),
		"p3.json": errors.New("err-p3"),
		"p4.json": errors.New("err-p4"),
		"p5.json": errors.New("err-p5"),
	}
	latencyByProduct := map[string]time.Duration{
		"p0.json": 80 * time.Millisecond,
		"p1.json": 5 * time.Millisecond,
		"p2.json": 30 * time.Millisecond,
		"p3.json": 1 * time.Millisecond,
		"p4.json": 15 * time.Millisecond,
		"p5.json": 2 * time.Millisecond,
	}

	for run := 0; run < 25; run++ {
		adp := &fakeAdapter{
			runFn: func(_ context.Context, _ int, step planner.Step) error {
				payload, ok := step.Payload.(planner.WriteExportManifestPayload)
				if !ok {
					return nil
				}

				latency, ok := latencyByProduct[payload.ManifestFilename]
				if !ok {
					return fmt.Errorf("missing latency for %s", payload.ManifestFilename)
				}
				time.Sleep(latency)

				err, ok := errorByProduct[payload.ManifestFilename]
				if !ok {
					return fmt.Errorf("missing error for %s", payload.ManifestFilename)
				}
				return err
			},
		}
		s := New(executor.New(adp))

		err := s.Execute(ctx, plan)
		if err == nil {
			t.Fatalf("run %d: expected error, got nil", run)
		}
		if !errors.Is(err, expectedErr) {
			t.Fatalf("run %d: non-deterministic aggregated order, expected %v got %v", run, expectedErr, err)
		}
	}
}
