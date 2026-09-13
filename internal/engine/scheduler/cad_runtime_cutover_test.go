package scheduler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"parametron/internal/authoring/dsl"
	"parametron/internal/authoring/planner"
	"parametron/internal/engine/adapter"
	"parametron/internal/engine/adapter/freecad"
	"parametron/internal/engine/artifact"
	"parametron/internal/engine/cadruntime"
	"parametron/internal/engine/executor"
	"parametron/internal/engine/handoff"
	"parametron/internal/engine/runtimecap"
	"parametron/internal/engine/verification"
)

// --- Fakes ---

// schedulerFakeCADAdapter implements both adapter.Adapter and
// adapter.CADRuntimeOrchestrator so a single fake can observe which dispatch
// path (legacy Execute -> Run, or aligned ExecutePackage -> OrchestrateCADRuntime)
// the scheduler actually takes.
type schedulerFakeCADAdapter struct {
	mu               sync.Mutex
	runSteps         []planner.Step
	orchestrateCalls []adapter.CADRuntimeOrchestrationRequest
	orchestrateFn    func(ctx context.Context, req adapter.CADRuntimeOrchestrationRequest) error
	// traversal, when non-empty, is returned as reference traversal evidence
	// by CADRuntimeVerifiedRun for the most recently orchestrated request.
	traversal []byte
}

// CADRuntimeVerifiedRun derives an identity-matching passing verified run
// from the most recently orchestrated request, optionally carrying
// reference traversal evidence, so consumption-enabled executors under test
// can exercise Scheduler.JobExecution.CADRuntimeOutcome transport.
func (a *schedulerFakeCADAdapter) CADRuntimeVerifiedRun() (cadruntime.FreeCADRuntimeVerifiedRun, bool) {
	a.mu.Lock()
	if len(a.orchestrateCalls) == 0 {
		a.mu.Unlock()
		return cadruntime.FreeCADRuntimeVerifiedRun{}, false
	}
	req := a.orchestrateCalls[len(a.orchestrateCalls)-1]
	traversal := a.traversal
	a.mu.Unlock()

	attempt, err := freecad.ComputeFreeCADRuntimeAttempt(req)
	if err != nil {
		return cadruntime.FreeCADRuntimeVerifiedRun{}, false
	}
	run := cadruntime.FreeCADRuntimeRun{
		ObservationRequest: cadruntime.FreeCADRuntimeObservationRequestMaterialization{
			Manifest: freecad.FreeCADRuntimeManifestMaterialization{Attempt: attempt},
		},
		ExecutionRequest: freecad.FreeCADRuntimeExecutionRequest{ResultPath: attempt.Layout.ResultPath, OutputDir: attempt.Layout.OutputDir},
		Result:           &freecad.FreeCADRuntimeResult{Status: freecad.FreeCADRuntimeResultStatusSucceeded},
	}
	if len(traversal) > 0 {
		run.ReferenceTraversalPath = filepath.Join(attempt.Layout.OutputDir, cadruntime.FreeCADRuntimeReferenceTraversalFilename)
		run.ReferenceTraversalJSON = traversal
	}
	return cadruntime.FreeCADRuntimeVerifiedRun{
		Runtime:      run,
		Verification: &verification.Result{Status: verification.StatusPass, Failure: verification.FailureClassNone},
	}, true
}

func (a *schedulerFakeCADAdapter) Run(_ context.Context, step planner.Step) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.runSteps = append(a.runSteps, step)
	return nil
}

func (a *schedulerFakeCADAdapter) OrchestrateCADRuntime(ctx context.Context, req adapter.CADRuntimeOrchestrationRequest) error {
	a.mu.Lock()
	a.orchestrateCalls = append(a.orchestrateCalls, req)
	fn := a.orchestrateFn
	a.mu.Unlock()
	if fn != nil {
		return fn(ctx, req)
	}
	return nil
}

func (a *schedulerFakeCADAdapter) runCallCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.runSteps)
}

func (a *schedulerFakeCADAdapter) orchestrateCallCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.orchestrateCalls)
}

func (a *schedulerFakeCADAdapter) orchestrateRequests() []adapter.CADRuntimeOrchestrationRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]adapter.CADRuntimeOrchestrationRequest(nil), a.orchestrateCalls...)
}

type schedulerFakeResolver struct{}

func (schedulerFakeResolver) Resolve(adapterID string) (runtimecap.ExecutableSelection, error) {
	return runtimecap.ExecutableSelection{
		Adapter: adapterID,
		Command: "fake-freecad-runtime",
		Path:    "/fake/fake-freecad-runtime",
		Source:  runtimecap.ExecutableSelectionSourceConfigured,
	}, nil
}

var _ runtimecap.ExecutableResolver = schedulerFakeResolver{}

// alignedProductPlan builds a real planner-produced plan for one eligible
// FreeCAD product (WriteCSV, WriteExportManifest, RunCADRuntime).
func alignedProductPlan(t *testing.T, productName string) *planner.ExecutionPlan {
	t.Helper()
	dslContent := fmt.Sprintf(`dsl v1.0
product %s {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = ["step"]

    param label: string = "box"
}
`, productName)
	tmpFile, err := os.CreateTemp("", "scheduler_aligned_*.dsl")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.WriteString(dslContent); err != nil {
		t.Fatalf("failed to write temp DSL: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("failed to close temp DSL: %v", err)
	}
	ast, err := dsl.Parse(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to parse DSL: %v", err)
	}
	if err := dsl.Validate(ast); err != nil {
		t.Fatalf("failed to validate DSL: %v", err)
	}
	plan, err := planner.CreatePlan(ast, nil)
	if err != nil {
		t.Fatalf("failed to create plan: %v", err)
	}
	return plan
}

// ===================== Part G: scheduler dispatch =====================

func TestScheduler_AlignedJobUsesValidatedHandoffPackage(t *testing.T) {
	plan := alignedProductPlan(t, "Box")
	fakeAdp := &schedulerFakeCADAdapter{}
	rt := executor.NewWithCADRuntimeOrchestration(fakeAdp, schedulerFakeResolver{}, t.TempDir())
	s := New(rt)

	if err := s.Execute(context.Background(), plan); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fakeAdp.orchestrateCallCount() != 1 {
		t.Fatalf("expected exactly one OrchestrateCADRuntime call, got %d", fakeAdp.orchestrateCallCount())
	}
	for _, step := range fakeAdp.runSteps {
		if step.Type == planner.StepRunCADRuntime {
			t.Fatal("Execute (legacy Run path) must not be used for the RunCADRuntime step")
		}
	}
	req := fakeAdp.orchestrateRequests()[0]
	if req.ProductKey != "Box" {
		t.Fatalf("expected orchestration request for product Box, got %q", req.ProductKey)
	}
	if req.JobID == "" {
		t.Fatal("expected a non-empty job ID derived from the validated handoff package")
	}
}

func TestScheduler_LegacyJobUsesExecute(t *testing.T) {
	plan := productPlan("widget")
	fakeAdp := &schedulerFakeCADAdapter{}
	rt := executor.New(fakeAdp)
	s := New(rt)

	if err := s.Execute(context.Background(), plan); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fakeAdp.orchestrateCallCount() != 0 {
		t.Fatalf("expected ExecutePackage/OrchestrateCADRuntime not to be used for a legacy plan, calls=%d", fakeAdp.orchestrateCallCount())
	}
	if fakeAdp.runCallCount() != 2 {
		t.Fatalf("expected 2 legacy Run calls (WriteCSV, WriteExportManifest), got %d", fakeAdp.runCallCount())
	}
}

func TestScheduler_TransportsTerminalCADRuntimeOutcome(t *testing.T) {
	plan := alignedProductPlan(t, "Box")
	fakeAdp := &schedulerFakeCADAdapter{traversal: []byte(`{"schemaVersion":"2.0"}`)}
	rt := executor.NewWithCADRuntimeConsumption(fakeAdp, schedulerFakeResolver{}, t.TempDir())
	s := New(rt)

	result, err := s.ExecuteWithResult(context.Background(), plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Jobs) != 1 {
		t.Fatalf("expected exactly one job execution, got %d", len(result.Jobs))
	}
	outcome := result.Jobs[0].CADRuntimeOutcome
	if outcome == nil || outcome.Verification != artifact.VerificationOutcomePassed {
		t.Fatalf("outcome=%#v", outcome)
	}
	if outcome.ProductKey != "Box" || len(outcome.ReferenceTraversalJSON) == 0 || outcome.ReferenceTraversalPath == "" {
		t.Fatalf("outcome missing expected reference traversal transport: %#v", outcome)
	}
}

func TestScheduler_LegacyJobRetainsNilCADRuntimeOutcome(t *testing.T) {
	plan := productPlan("widget")
	fakeAdp := &schedulerFakeCADAdapter{}
	rt := executor.New(fakeAdp)
	s := New(rt)

	result, err := s.ExecuteWithResult(context.Background(), plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, job := range result.Jobs {
		if job.CADRuntimeOutcome != nil {
			t.Fatalf("expected nil CADRuntimeOutcome for a legacy (non-CAD-runtime) job: %#v", job.CADRuntimeOutcome)
		}
	}
}

func TestScheduler_MixedAlignedAndLegacyJobsUseCorrectDispatch(t *testing.T) {
	alignedPlan := alignedProductPlan(t, "AlignedProduct")
	legacyPlan := productPlan("LegacyProduct")
	combined := &planner.ExecutionPlan{}
	combined.Steps = append(combined.Steps, alignedPlan.Steps...)
	combined.Steps = append(combined.Steps, legacyPlan.Steps...)

	runRoot := t.TempDir()
	type factoryCall struct {
		productDir string
		adp        *schedulerFakeCADAdapter
	}
	var mu sync.Mutex
	var calls []factoryCall
	factory := func(productDir string) *executor.Executor {
		adp := &schedulerFakeCADAdapter{}
		mu.Lock()
		calls = append(calls, factoryCall{productDir: productDir, adp: adp})
		mu.Unlock()
		if strings.HasSuffix(productDir, "LegacyProduct") {
			// The legacy product's plan is not manifest-verification-aware
			// (matches every other legacy scheduler test in this package,
			// which never enables productDir-based runtime verification);
			// only the aligned product needs CAD-runtime orchestration wiring.
			return executor.New(adp)
		}
		return executor.NewWithCADRuntimeOrchestration(adp, schedulerFakeResolver{}, productDir)
	}
	s := NewWithProductDirs(runRoot, factory)

	result, err := s.ExecuteWithResult(context.Background(), combined)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(result.Jobs))
	}
	if result.Jobs[0].Job.ProductKey != "AlignedProduct" {
		t.Fatalf("expected first job to be AlignedProduct (plan order), got %q", result.Jobs[0].Job.ProductKey)
	}
	if result.Jobs[1].Job.ProductKey != "LegacyProduct" {
		t.Fatalf("expected second job to be LegacyProduct (plan order), got %q", result.Jobs[1].Job.ProductKey)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 2 {
		t.Fatalf("expected an independent Executor construction per product, got %d factory calls", len(calls))
	}
	if calls[0].productDir == calls[1].productDir {
		t.Fatal("expected distinct product directories (independent Executors) per product")
	}

	var alignedAdp, legacyAdp *schedulerFakeCADAdapter
	for _, c := range calls {
		if strings.HasSuffix(c.productDir, "AlignedProduct") {
			alignedAdp = c.adp
		}
		if strings.HasSuffix(c.productDir, "LegacyProduct") {
			legacyAdp = c.adp
		}
	}
	if alignedAdp == nil || legacyAdp == nil {
		t.Fatalf("could not correlate factory calls to products: %+v", calls)
	}
	if alignedAdp.orchestrateCallCount() != 1 {
		t.Fatalf("expected aligned product's own executor to receive exactly one orchestration call, got %d", alignedAdp.orchestrateCallCount())
	}
	if legacyAdp.orchestrateCallCount() != 0 {
		t.Fatalf("expected legacy product's executor never to receive an orchestration call, got %d", legacyAdp.orchestrateCallCount())
	}
	if alignedAdp.runCallCount() != 2 {
		t.Fatalf("expected the aligned executor's own Run calls to be WriteCSV+WriteExportManifest only, got %d", alignedAdp.runCallCount())
	}
	if legacyAdp.runCallCount() != 2 {
		t.Fatalf("expected the legacy executor's own Run calls to be WriteCSV+WriteExportManifest only, got %d", legacyAdp.runCallCount())
	}
}

func TestScheduler_InvalidAlignedHandoffFailsBeforeExecution(t *testing.T) {
	// A RunCADRuntime step with no accompanying WriteCSV/WriteExportManifest
	// step is a malformed handoff package (ErrMissingWriteCSV), which must be
	// caught before any adapter call is attempted.
	plan := &planner.ExecutionPlan{Steps: []planner.Step{
		{Type: planner.StepRunCADRuntime, Payload: planner.RunCADRuntimePayload{
			ProductKey: "widget", Adapter: "freecad",
			ManifestFilename: planner.ExportManifestFilename, ResultFilename: planner.FreeCADRuntimeResultFilename,
		}},
	}}
	fakeAdp := &schedulerFakeCADAdapter{}
	rt := executor.NewWithCADRuntimeOrchestration(fakeAdp, schedulerFakeResolver{}, t.TempDir())
	s := New(rt)

	err := s.Execute(context.Background(), plan)
	if err == nil {
		t.Fatal("expected a handoff validation error")
	}
	if !errors.Is(err, handoff.ErrMissingWriteCSV) {
		t.Fatalf("expected ErrMissingWriteCSV, got %v", err)
	}
	if fakeAdp.runCallCount() != 0 || fakeAdp.orchestrateCallCount() != 0 {
		t.Fatalf("expected no adapter call before handoff validation, run=%d orchestrate=%d", fakeAdp.runCallCount(), fakeAdp.orchestrateCallCount())
	}
}

func TestScheduler_AlignedCancellationPreservesState(t *testing.T) {
	plan := alignedProductPlan(t, "Box")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fakeAdp := &schedulerFakeCADAdapter{orchestrateFn: func(ctx context.Context, _ adapter.CADRuntimeOrchestrationRequest) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	rt := executor.NewWithCADRuntimeOrchestration(fakeAdp, schedulerFakeResolver{}, t.TempDir())
	s := New(rt)

	done := make(chan error, 1)
	go func() { done <- s.Execute(ctx, plan) }()
	time.Sleep(20 * time.Millisecond)
	cancel()

	var err error
	select {
	case err = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for cancellation to propagate")
	}
	if err == nil {
		t.Fatal("expected a cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected a wrapped context.Canceled, got %v", err)
	}
}

func TestScheduler_AlignedFirstErrorOrderingIsDeterministic(t *testing.T) {
	errFirst := errors.New("first-product-error")
	errSecond := errors.New("second-product-error")

	p0 := alignedProductPlan(t, "P0")
	p1 := alignedProductPlan(t, "P1")
	combined := &planner.ExecutionPlan{}
	combined.Steps = append(combined.Steps, p0.Steps...)
	combined.Steps = append(combined.Steps, p1.Steps...)

	fakeAdp := &schedulerFakeCADAdapter{orchestrateFn: func(_ context.Context, req adapter.CADRuntimeOrchestrationRequest) error {
		switch req.ProductKey {
		case "P0":
			time.Sleep(40 * time.Millisecond)
			return errFirst
		case "P1":
			return errSecond
		default:
			return nil
		}
	}}
	rt := executor.NewWithCADRuntimeOrchestration(fakeAdp, schedulerFakeResolver{}, t.TempDir())
	s := New(rt)

	err := s.Execute(context.Background(), combined)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, errFirst) {
		t.Fatalf("expected the first-product (index 0) error regardless of completion timing, got %v", err)
	}
}

func TestScheduler_AlignedWorkersDoNotSharePackagesOrExecutors(t *testing.T) {
	const productCount = 6
	combined := &planner.ExecutionPlan{}
	for i := 0; i < productCount; i++ {
		p := alignedProductPlan(t, fmt.Sprintf("Product%d", i))
		combined.Steps = append(combined.Steps, p.Steps...)
	}

	runRoot := t.TempDir()
	var mu sync.Mutex
	adaptersByProduct := make(map[string]*schedulerFakeCADAdapter)
	factory := func(productDir string) *executor.Executor {
		adp := &schedulerFakeCADAdapter{}
		mu.Lock()
		adaptersByProduct[productDir] = adp
		mu.Unlock()
		return executor.NewWithCADRuntimeOrchestration(adp, schedulerFakeResolver{}, productDir)
	}
	s := NewWithProductDirs(runRoot, factory)

	result, err := s.ExecuteWithResult(context.Background(), combined)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Jobs) != productCount {
		t.Fatalf("expected %d jobs, got %d", productCount, len(result.Jobs))
	}

	mu.Lock()
	defer mu.Unlock()
	if len(adaptersByProduct) != productCount {
		t.Fatalf("expected %d independent executors, got %d", productCount, len(adaptersByProduct))
	}
	for productDir, adp := range adaptersByProduct {
		expectedProduct := productDir[strings.LastIndex(productDir, string(os.PathSeparator))+1:]
		if adp.orchestrateCallCount() != 1 {
			t.Fatalf("product %q: expected exactly one orchestration call on its own executor, got %d", expectedProduct, adp.orchestrateCallCount())
		}
		req := adp.orchestrateRequests()[0]
		if req.ProductKey != expectedProduct {
			t.Fatalf("cross-worker leakage: executor for %q observed orchestration request for product %q", expectedProduct, req.ProductKey)
		}
		if req.ProductDir != productDir {
			t.Fatalf("cross-worker leakage: executor for %q observed product dir %q", expectedProduct, req.ProductDir)
		}
	}
}

// ===================== Part N: protected boundaries (scheduler-local) =====================

func TestTask13_SchedulerDoesNotInvokeRuntimeVerificationDirectly(t *testing.T) {
	data, err := os.ReadFile("scheduler.go")
	if err != nil {
		t.Fatalf("failed to read scheduler.go: %v", err)
	}
	src := string(data)
	forbidden := []string{"InvokeAndVerifyFreeCADRuntime", "InvokeAndValidateFreeCADRuntime", "verification.Verify", "cadruntime.", `"parametron/internal/engine/cadruntime"`, `"parametron/internal/engine/verification"`}
	for _, token := range forbidden {
		if strings.Contains(src, token) {
			t.Fatalf("scheduler.go must not reference %q; the wrapper owns Task 11 verification", token)
		}
	}
}
