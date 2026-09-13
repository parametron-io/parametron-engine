//go:build stress
// +build stress

package scheduler

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/executor"
)

func buildTestPlan(productCount int) *planner.ExecutionPlan {
	steps := make([]planner.Step, 0, productCount*2)
	for i := 0; i < productCount; i++ {
		csv := fmt.Sprintf("stress-%04d.csv", i)
		manifest := fmt.Sprintf("stress-%04d.json", i)
		steps = append(steps,
			planner.Step{Type: planner.StepWriteCSV, Payload: planner.WriteCSVPayload{Filename: csv}},
			planner.Step{Type: planner.StepWriteExportManifest, Payload: planner.WriteExportManifestPayload{ManifestFilename: manifest}},
		)
	}
	return &planner.ExecutionPlan{Steps: steps}
}

func TestScheduler_ParallelStress(t *testing.T) {
	t.Parallel()

	const productCount = 1000

	ctx := context.Background()
	plan := buildTestPlan(productCount)

	adp := &fakeAdapter{
		runFn: func(_ context.Context, _ int, _ planner.Step) error {
			return nil
		},
	}
	s := New(executor.New(adp))

	before := runtime.NumGoroutine()

	if err := s.Execute(ctx, plan); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	deadline := time.Now().Add(1 * time.Second)
	for {
		after := runtime.NumGoroutine()
		if after <= before+5 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("possible goroutine leak: before=%d after=%d", before, after)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
