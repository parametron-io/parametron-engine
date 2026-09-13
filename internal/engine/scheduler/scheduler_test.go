package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/executor"
	"parametron/internal/engine/job"
)

type testContextKey string

type fakeAdapter struct {
	mu         sync.Mutex
	calls      []planner.Step
	lastCtx    context.Context
	failAt     int
	failAlways bool
	failWith   error
	runFn      func(ctx context.Context, callIndex int, step planner.Step) error
}

func (a *fakeAdapter) Run(ctx context.Context, step planner.Step) error {
	a.mu.Lock()
	a.lastCtx = ctx
	a.calls = append(a.calls, step)
	callIndex := len(a.calls) - 1
	runFn := a.runFn
	failWith := a.failWith
	failAlways := a.failAlways
	failAt := a.failAt
	a.mu.Unlock()

	if runFn != nil {
		return runFn(ctx, callIndex, step)
	}

	if failWith != nil && (failAlways || callIndex == failAt) {
		return failWith
	}

	return nil
}

func (a *fakeAdapter) callCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.calls)
}

func productPlan(names ...string) *planner.ExecutionPlan {
	steps := make([]planner.Step, 0, len(names)*2)
	for _, name := range names {
		csv := fmt.Sprintf("%s.csv", name)
		steps = append(steps,
			planner.Step{Type: planner.StepWriteCSV, Payload: planner.WriteCSVPayload{ProductKey: name, Filename: csv}},
			planner.Step{Type: planner.StepWriteExportManifest, Payload: planner.WriteExportManifestPayload{ProductKey: name, ManifestFilename: fmt.Sprintf("%s.json", name)}},
		)
	}
	return &planner.ExecutionPlan{Steps: steps}
}

func TestSchedulerExecute_SequentialAndContextPropagation(t *testing.T) {
	ctx := context.WithValue(context.Background(), testContextKey("k"), "v")
	plan := productPlan("widget")

	adp := &fakeAdapter{}
	rt := executor.New(adp)
	s := New(rt)

	if err := s.Execute(ctx, plan); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if got := adp.callCount(); got != 2 {
		t.Fatalf("expected 2 adapter calls, got %d", got)
	}
	if adp.lastCtx == nil {
		t.Fatal("scheduler/runtime did not provide adapter context")
	}
	if got := adp.lastCtx.Value(testContextKey("k")); got != "v" {
		t.Fatalf("scheduler/runtime did not propagate context values, got %v", got)
	}
}

func TestSchedulerExecute_PropagatesErrorDeterministicallyByProductIndex(t *testing.T) {
	errFirst := errors.New("first-product-error")
	errSecond := errors.New("second-product-error")
	plan := productPlan("p0", "p1")

	adp := &fakeAdapter{
		runFn: func(_ context.Context, _ int, step planner.Step) error {
			payload, ok := step.Payload.(planner.WriteExportManifestPayload)
			if !ok {
				return nil
			}
			switch payload.ManifestFilename {
			case "p0.json":
				time.Sleep(40 * time.Millisecond)
				return errFirst
			case "p1.json":
				return errSecond
			default:
				return nil
			}
		},
	}

	rt := executor.New(adp)
	s := New(rt)

	err := s.Execute(context.Background(), plan)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, errFirst) {
		t.Fatalf("expected first product error %v, got %v", errFirst, err)
	}
}

func TestSchedulerExecute_MultipleProductsExecutedInParallel(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping parallel timing test in short mode")
	}

	plan := productPlan("p0", "p1", "p2", "p3")

	var mu sync.Mutex
	concurrent := 0
	maxConcurrent := 0

	adp := &fakeAdapter{
		runFn: func(_ context.Context, _ int, _ planner.Step) error {
			mu.Lock()
			concurrent++
			if concurrent > maxConcurrent {
				maxConcurrent = concurrent
			}
			mu.Unlock()

			time.Sleep(20 * time.Millisecond)

			mu.Lock()
			concurrent--
			mu.Unlock()
			return nil
		},
	}

	rt := executor.New(adp)
	s := New(rt)

	if err := s.Execute(context.Background(), plan); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if maxConcurrent < 2 {
		t.Fatalf("expected parallel execution (maxConcurrent >= 2), got %d", maxConcurrent)
	}
}

func TestSchedulerExecute_CancellationStopsSchedulingNewJobs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const totalProducts = 200
	productNames := make([]string, 0, totalProducts)
	for i := 0; i < totalProducts; i++ {
		productNames = append(productNames, fmt.Sprintf("p%03d", i))
	}
	plan := productPlan(productNames...)

	var mu sync.Mutex
	startedProducts := make(map[string]struct{})
	var cancelOnce sync.Once

	adp := &fakeAdapter{
		runFn: func(_ context.Context, _ int, step planner.Step) error {
			if payload, ok := step.Payload.(planner.WriteCSVPayload); ok {
				productID := payload.Filename[:len(payload.Filename)-len(".csv")]
				mu.Lock()
				startedProducts[productID] = struct{}{}
				mu.Unlock()

				cancelOnce.Do(func() {
					cancel()
				})
			}

			time.Sleep(15 * time.Millisecond)
			return nil
		},
	}

	rt := executor.New(adp)
	s := New(rt)

	err := s.Execute(ctx, plan)
	if err == nil {
		t.Fatal("expected cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected wrapped context.Canceled, got %v", err)
	}

	mu.Lock()
	started := len(startedProducts)
	mu.Unlock()

	if started >= totalProducts {
		t.Fatalf("expected cancellation to stop scheduling new jobs, started=%d total=%d", started, totalProducts)
	}
}

func TestSplitPlan_UsesProductKeyWhenPresent(t *testing.T) {
	plan := &planner.ExecutionPlan{
		Steps: []planner.Step{
			{
				Type: planner.StepWriteCSV,
				Payload: planner.WriteCSVPayload{
					ProductKey: "A",
					Filename:   "shared.csv",
				},
			},
			{
				Type: planner.StepRunCADRuntime,
				Payload: planner.RunCADRuntimePayload{
					ProductKey:       "A",
					Adapter:          "freecad",
					ManifestFilename: planner.ExportManifestFilename,
					ResultFilename:   planner.FreeCADRuntimeResultFilename,
				},
			},
			{
				Type: planner.StepWriteCSV,
				Payload: planner.WriteCSVPayload{
					ProductKey: "B",
					Filename:   "shared.csv",
				},
			},
			{
				Type: planner.StepRunCADRuntime,
				Payload: planner.RunCADRuntimePayload{
					ProductKey:       "B",
					Adapter:          "freecad",
					ManifestFilename: planner.ExportManifestFilename,
					ResultFilename:   planner.FreeCADRuntimeResultFilename,
				},
			},
		},
	}

	jobs, err := job.SplitPlan(plan)
	if err != nil {
		t.Fatalf("SplitPlan returned error: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}
}

func TestSplitPlan_KeepExportManifestWithSameProduct(t *testing.T) {
	plan := &planner.ExecutionPlan{
		Steps: []planner.Step{
			{
				Type: planner.StepWriteCSV,
				Payload: planner.WriteCSVPayload{
					ProductKey: "Wheel",
					Filename:   "Wheel.csv",
				},
			},
			{
				Type: planner.StepWriteExportManifest,
				Payload: planner.WriteExportManifestPayload{
					ProductKey:       "Wheel",
					ManifestFilename: planner.ExportManifestFilename,
					SchemaVersion:    planner.ExportManifestSchemaVersion,
					PlanHash:         "hash",
					Product:          planner.ExportManifestProduct{ID: "Wheel"},
					Inputs:           planner.ExportManifestInputs{},
					Outputs:          []planner.ExportManifestOutput{{Type: "step", Filename: "Wheel.step"}},
				},
			},
			{
				Type: planner.StepRunCADRuntime,
				Payload: planner.RunCADRuntimePayload{
					ProductKey:       "Wheel",
					Adapter:          "freecad",
					ManifestFilename: planner.ExportManifestFilename,
					ResultFilename:   planner.FreeCADRuntimeResultFilename,
				},
			},
		},
	}

	jobs, err := job.SplitPlan(plan)
	if err != nil {
		t.Fatalf("SplitPlan returned error: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if len(jobs[0].Steps()) != 3 {
		t.Fatalf("expected 3 steps in job, got %d", len(jobs[0].Steps()))
	}
}
