package executor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/adapter"
	"parametron/internal/engine/adapter/freecad"
	"parametron/internal/engine/handoff"
	"parametron/internal/engine/job"
	"parametron/internal/engine/runtimecap"
)

type cadTestAdapter struct {
	mu       sync.Mutex
	runs     []planner.Step
	requests []adapter.CADRuntimeOrchestrationRequest
	order    []string
	errors   []error
	started  chan struct{}
	release  chan struct{}
	mutate   bool
}

func (a *cadTestAdapter) Run(_ context.Context, s planner.Step) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.runs = append(a.runs, s)
	a.order = append(a.order, "Run("+string(s.Type)+")")
	if s.Type == planner.StepRunCADRuntime {
		return errors.New("CAD runtime reached Run")
	}
	return nil
}
func (a *cadTestAdapter) OrchestrateCADRuntime(ctx context.Context, r adapter.CADRuntimeOrchestrationRequest) error {
	a.mu.Lock()
	a.requests = append(a.requests, cloneCADRuntimeRequest(r))
	a.order = append(a.order, "OrchestrateCADRuntime")
	n := len(a.requests)
	started, release, mutate := a.started, a.release, a.mutate
	var err error
	if n <= len(a.errors) {
		err = a.errors[n-1]
	}
	a.mu.Unlock()
	if started != nil {
		select {
		case started <- struct{}{}:
		default:
			{
			}
		}
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if mutate {
		if len(r.CSV.Headers) > 0 {
			r.CSV.Headers[0] = "mutated"
		}
		if r.Manifest.Values != nil {
			r.Manifest.Values["width"] = "mutated"
		}
		r.CADRuntime.Adapter = "mutated"
	}
	return err
}

func cloneCADRuntimeRequest(r adapter.CADRuntimeOrchestrationRequest) adapter.CADRuntimeOrchestrationRequest {
	r.CSV = cloneWriteCSVPayload(r.CSV)
	r.Manifest = cloneManifestPayload(r.Manifest)
	r.CADRuntime = cloneRunCADRuntimePayload(r.CADRuntime)
	return r
}

type cadResolver struct {
	mu         sync.Mutex
	calls      []string
	selections []runtimecap.ExecutableSelection
	err        error
}

func (r *cadResolver) Resolve(id string) (runtimecap.ExecutableSelection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, id)
	if r.err != nil {
		return runtimecap.ExecutableSelection{}, r.err
	}
	i := len(r.calls) - 1
	if i >= len(r.selections) {
		i = len(r.selections) - 1
	}
	return r.selections[i], nil
}
func selection(id, path string) runtimecap.ExecutableSelection {
	return runtimecap.ExecutableSelection{Adapter: id, Command: path, Path: path, Source: runtimecap.ExecutableSelectionSourceConfigured}
}

type legacyOnlyAdapter struct{ runs []planner.Step }

func (a *legacyOnlyAdapter) Run(_ context.Context, s planner.Step) error {
	a.runs = append(a.runs, s)
	return nil
}

func alignedPackage(t *testing.T, id string) *handoff.Package {
	t.Helper()
	steps := []planner.Step{
		{Type: planner.StepWriteCSV, Payload: planner.WriteCSVPayload{ProductKey: "widget", Filename: "widget.csv", Headers: []string{"width", "label"}, Values: []any{10.0, "A"}}},
		{Type: planner.StepWriteExportManifest, Payload: planner.WriteExportManifestPayload{ProductKey: "widget", ManifestFilename: planner.ExportManifestFilename, SchemaVersion: planner.ExportManifestSchemaVersion, Adapter: id, Product: planner.ExportManifestProduct{ID: "widget"}, Values: map[string]any{"width": 10.0}, ParameterAssignments: []planner.ExportManifestParameterAssignment{{Name: "width", Value: 10.0, Type: "float", Unit: "mm"}}, Outputs: []planner.ExportManifestOutput{{Type: "step", Filename: "widget.step", Object: "Body"}}, Verification: planner.VerificationManifestIntent{ExpectedParameters: []planner.VerificationExpectedParameter{{ID: "width", Name: "width", Value: 10.0, Type: "float", Unit: "mm"}}}}},
		{Type: planner.StepRunCADRuntime, Payload: planner.RunCADRuntimePayload{ProductKey: "widget", Adapter: id, ManifestFilename: planner.ExportManifestFilename, ResultFilename: "result.json"}}}
	j, err := job.New("widget", steps)
	if err != nil {
		t.Fatal(err)
	}
	p, err := handoff.FromJob(j)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func newCADExecutor(t *testing.T, a adapter.Adapter, r runtimecap.ExecutableResolver) (*Executor, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "not-created")
	return NewWithCADRuntimeOrchestration(a, r, dir), dir
}
func dispatchError(t *testing.T, err error, stage string) *CADRuntimeDispatchError {
	t.Helper()
	var d *CADRuntimeDispatchError
	if !errors.As(err, &d) || d.Stage != stage {
		t.Fatalf("err=%v dispatch=%#v want stage=%s", err, d, stage)
	}
	return d
}

func TestNewWithCADRuntimeOrchestration_InjectsDependencies(t *testing.T) {
	a := &cadTestAdapter{}
	r := &cadResolver{selections: []runtimecap.ExecutableSelection{selection("freecad", "/opt/runtime")}}
	e, dir := newCADExecutor(t, a, r)
	if e.adapter != a || e.cadRuntime.resolver != r || e.productDir != dir || e.strategy == nil || len(r.calls) != 0 {
		t.Fatalf("constructor dependencies not retained: %#v", e)
	}
}

func TestExecutorConstructors_LegacyPlansDoNotRequireCADRuntimeDependencies(t *testing.T) {
	constructors := map[string]func(*legacyOnlyAdapter) *Executor{"New": func(a *legacyOnlyAdapter) *Executor { return New(a) }, "NewWithProductDir": func(a *legacyOnlyAdapter) *Executor { return NewWithProductDir(a, "") }, "NewWithStrategy": func(a *legacyOnlyAdapter) *Executor { return NewWithStrategy(a, nil) }}
	plan := &planner.ExecutionPlan{Steps: []planner.Step{{Type: planner.StepWriteCSV}}}
	for name, ctor := range constructors {
		t.Run(name, func(t *testing.T) {
			a := &legacyOnlyAdapter{}
			if err := ctor(a).Execute(context.Background(), plan); err != nil {
				t.Fatal(err)
			}
			if len(a.runs) != 1 {
				t.Fatal(a.runs)
			}
		})
	}
}

func TestExecutorExecute_RunCADRuntimeRequiresHandoffPackage(t *testing.T) {
	a := &cadTestAdapter{}
	r := &cadResolver{selections: []runtimecap.ExecutableSelection{selection("freecad", "/opt/runtime")}}
	e, dir := newCADExecutor(t, a, r)
	err := e.Execute(context.Background(), alignedPackage(t, "freecad").Plan())
	dispatchError(t, err, cadRuntimeDispatchStagePreflight)
	if !errors.Is(err, ErrCADRuntimeHandoffRequired) || len(r.calls) > 0 || len(a.runs) > 0 || len(a.requests) > 0 {
		t.Fatalf("unexpected side effects: %v", err)
	}
	if _, stat := os.Stat(dir); !errors.Is(stat, os.ErrNotExist) {
		t.Fatal(stat)
	}
}

func TestExecutorExecutePackage_CADRuntimeRequiresResolver(t *testing.T) {
	a := &cadTestAdapter{}
	e := NewWithProductDir(a, t.TempDir())
	err := e.ExecutePackage(context.Background(), alignedPackage(t, "freecad"))
	dispatchError(t, err, cadRuntimeDispatchStagePreflight)
	if !errors.Is(err, ErrCADRuntimeResolverNotConfigured) || len(a.runs)+len(a.requests) != 0 {
		t.Fatal(err)
	}
	s := e.States()
	if s[2].Attempts != 0 || !s[2].StartedAt.IsZero() {
		t.Fatalf("state=%#v", s[2])
	}
	var x *ExecutionError
	if !errors.As(err, &x) || x.RetryCount != 0 {
		t.Fatal(err)
	}
}

func TestExecutorExecutePackage_CADRuntimeRequiresOrchestratorCapability(t *testing.T) {
	a := &legacyOnlyAdapter{}
	r := &cadResolver{selections: []runtimecap.ExecutableSelection{selection("freecad", "/opt/runtime")}}
	e, _ := newCADExecutor(t, a, r)
	err := e.ExecutePackage(context.Background(), alignedPackage(t, "freecad"))
	dispatchError(t, err, cadRuntimeDispatchStagePreflight)
	if !errors.Is(err, ErrCADRuntimeOrchestratorUnsupported) || len(r.calls) != 0 || len(a.runs) != 0 {
		t.Fatal(err)
	}
}

func TestExecutorExecutePackage_CADRuntimeSelectionFailureStopsBeforeSideEffects(t *testing.T) {
	cause := &runtimecap.ExecutableSelectionError{Kind: runtimecap.ExecutableSelectionErrorNotFound, Adapter: "freecad", Command: "missing", Err: os.ErrNotExist}
	a := &cadTestAdapter{}
	r := &cadResolver{err: cause}
	e, dir := newCADExecutor(t, a, r)
	err := e.ExecutePackage(context.Background(), alignedPackage(t, "freecad"))
	dispatchError(t, err, cadRuntimeDispatchStageExecutableSelection)
	var got *runtimecap.ExecutableSelectionError
	if !errors.As(err, &got) || len(r.calls) != 1 || len(a.runs)+len(a.requests) != 0 {
		t.Fatal(err)
	}
	if _, x := os.Stat(dir); !errors.Is(x, os.ErrNotExist) {
		t.Fatal(x)
	}
}

func TestExecutorExecutePackage_CADRuntimeRequestValidationFailureStopsBeforeSideEffects(t *testing.T) {
	a := &cadTestAdapter{}
	r := &cadResolver{selections: []runtimecap.ExecutableSelection{{Adapter: "freecad", Command: "relative", Path: "relative", Source: runtimecap.ExecutableSelectionSourceConfigured}}}
	e, _ := newCADExecutor(t, a, r)
	err := e.ExecutePackage(context.Background(), alignedPackage(t, "freecad"))
	dispatchError(t, err, cadRuntimeDispatchStageRequestValidation)
	var requestErr *adapter.CADRuntimeOrchestrationRequestError
	if !errors.As(err, &requestErr) || len(a.runs)+len(a.requests) != 0 || e.States()[2].Attempts != 0 {
		t.Fatal(err)
	}
}

func TestExecutorCADRuntimePreflight_RejectsPackageStepMismatch(t *testing.T) {
	for _, kind := range []string{"missing", "different", "wrong-payload"} {
		t.Run(kind, func(t *testing.T) {
			p := alignedPackage(t, "freecad")
			a := &cadTestAdapter{}
			r := &cadResolver{selections: []runtimecap.ExecutableSelection{selection("freecad", "/opt/runtime")}}
			e, _ := newCADExecutor(t, a, r)
			e.pkg = cloneHandoffPackage(p)
			plan := p.Plan()
			switch kind {
			case "missing":
				e.pkg.CADRuntime = nil
			case "different":
				e.pkg.CADRuntime.ResultFilename = "other.json"
			case "wrong-payload":
				plan.Steps[2].Payload = planner.WriteCSVPayload{}
			}
			_, err := e.prepareCADRuntimeDispatch(plan)
			dispatchError(t, err, cadRuntimeDispatchStagePreflight)
			if len(r.calls) != 0 || len(a.runs)+len(a.requests) != 0 {
				t.Fatal("side effects")
			}
		})
	}
}

func TestExecutorExecutePackage_CADRuntimeDispatchOrder(t *testing.T) {
	a := &cadTestAdapter{}
	r := &cadResolver{selections: []runtimecap.ExecutableSelection{selection("freecad", "/opt/runtime")}}
	e, _ := newCADExecutor(t, a, r)
	if err := e.ExecutePackage(context.Background(), alignedPackage(t, "freecad")); err != nil {
		t.Fatal(err)
	}
	want := []string{"Run(WriteCSV)", "Run(WriteExportManifest)", "OrchestrateCADRuntime"}
	if !reflect.DeepEqual(a.order, want) || len(r.calls) != 1 {
		t.Fatalf("order=%v resolver=%v", a.order, r.calls)
	}
}
func TestExecutorExecutePackage_RunCADRuntimeNeverReachesAdapterRun(t *testing.T) {
	TestExecutorExecutePackage_CADRuntimeDispatchOrder(t)
}

func TestExecutorExecutePackage_CADRuntimeRequestFields(t *testing.T) {
	p := alignedPackage(t, "freecad")
	a := &cadTestAdapter{}
	sel := selection("freecad", "/opt/runtime")
	r := &cadResolver{selections: []runtimecap.ExecutableSelection{sel}}
	e, dir := newCADExecutor(t, a, r)
	if err := e.ExecutePackage(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	got := a.requests[0]
	if got.JobID != p.JobID || got.ProductKey != "widget" || got.ProductDir != dir || got.StepID != "2" || got.Attempt != 1 || !reflect.DeepEqual(got.CSV, p.CSV) || !reflect.DeepEqual(got.Manifest, p.Manifest) || got.CADRuntime != *p.CADRuntime || got.Executable != sel {
		t.Fatalf("request=%#v", got)
	}
}
func TestExecutorExecutePackage_CADRuntimeDispatchIsAdapterNeutral(t *testing.T) {
	a := &cadTestAdapter{}
	r := &cadResolver{selections: []runtimecap.ExecutableSelection{selection("solidworks", "/opt/solidworks")}}
	e, _ := newCADExecutor(t, a, r)
	if err := e.ExecutePackage(context.Background(), alignedPackage(t, "solidworks")); err != nil {
		t.Fatal(err)
	}
}

func TestExecutorExecutePackage_ResolvesExecutableOnceAcrossRetries(t *testing.T) {
	sentinel := errors.New("retry")
	a := &cadTestAdapter{errors: []error{sentinel, sentinel, nil}}
	r := &cadResolver{selections: []runtimecap.ExecutableSelection{selection("freecad", "/opt/first"), selection("freecad", "/opt/second")}}
	e, _ := newCADExecutor(t, a, r)
	if err := e.ExecutePackage(context.Background(), alignedPackage(t, "freecad")); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 1 || len(a.requests) != 3 || len(a.runs) != 2 {
		t.Fatalf("resolve=%d requests=%d runs=%d", len(r.calls), len(a.requests), len(a.runs))
	}
	for i, q := range a.requests {
		if q.Attempt != i+1 || q.Executable.Path != "/opt/first" {
			t.Fatal(a.requests)
		}
	}
	if s := e.States()[2]; s.Status != StepSuccess || s.Attempts != 3 {
		t.Fatalf("state=%#v", s)
	}
}
func TestExecutorExecutePackage_ReusesExecutableSelectionAcrossRetries(t *testing.T) {
	TestExecutorExecutePackage_ResolvesExecutableOnceAcrossRetries(t)
}

func TestExecutorExecutePackage_ResolvesOncePerPackageExecution(t *testing.T) {
	a := &cadTestAdapter{}
	r := &cadResolver{selections: []runtimecap.ExecutableSelection{selection("freecad", "/opt/one"), selection("freecad", "/opt/two")}}
	e, _ := newCADExecutor(t, a, r)
	p := alignedPackage(t, "freecad")
	if err := e.ExecutePackage(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if err := e.ExecutePackage(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 2 || a.requests[0].Executable.Path != "/opt/one" || a.requests[1].Executable.Path != "/opt/two" {
		t.Fatal(a.requests)
	}
}

func TestExecutorExecutePackage_CADRuntimeAdapterFailureAfterRetries(t *testing.T) {
	sentinel := errors.New("adapter sentinel")
	a := &cadTestAdapter{errors: []error{sentinel, sentinel, sentinel}}
	r := &cadResolver{selections: []runtimecap.ExecutableSelection{selection("freecad", "/opt/runtime")}}
	e, _ := newCADExecutor(t, a, r)
	err := e.ExecutePackage(context.Background(), alignedPackage(t, "freecad"))
	d := dispatchError(t, err, cadRuntimeDispatchStageAdapterOrchestration)
	var x *ExecutionError
	if !errors.As(err, &x) || x.RetryCount != 3 || d.Attempt != 3 || d.JobID == "" || d.ProductKey != "widget" || d.Adapter != "freecad" || d.StepID != "2" || !errors.Is(err, sentinel) {
		t.Fatalf("err=%v", err)
	}
}

func TestCADRuntimeDispatchError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("cause")
	d := &CADRuntimeDispatchError{Stage: "adapter_orchestration", JobID: "job", ProductKey: "widget", Adapter: "freecad", StepID: "2", Attempt: 3, Err: cause}
	x := &ExecutionError{Err: d}
	for _, s := range []string{"adapter_orchestration", "job", "widget", "freecad", "step=2", "attempt=3"} {
		if !strings.Contains(x.Error(), s) {
			t.Fatalf("missing %s: %v", s, x)
		}
	}
	var got *CADRuntimeDispatchError
	if !errors.As(x, &got) || !errors.Is(x, cause) {
		t.Fatal(x)
	}
}

func TestExecutorExecutePackage_CADRuntimeCancellation(t *testing.T) {
	started := make(chan struct{}, 1)
	a := &cadTestAdapter{started: started, release: make(chan struct{})}
	r := &cadResolver{selections: []runtimecap.ExecutableSelection{selection("freecad", "/opt/runtime")}}
	e, _ := newCADExecutor(t, a, r)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.ExecutePackage(ctx, alignedPackage(t, "freecad")) }()
	<-started
	cancel()
	err := <-done
	var x *ExecutionError
	if !errors.As(err, &x) || !x.Canceled || x.Timeout || !errors.Is(err, context.Canceled) || len(a.requests) != 1 {
		t.Fatalf("err=%#v", x)
	}
}
func TestExecutorExecutePackage_CADRuntimeDeadlineExceeded(t *testing.T) {
	a := &cadTestAdapter{errors: []error{context.DeadlineExceeded}}
	r := &cadResolver{selections: []runtimecap.ExecutableSelection{selection("freecad", "/opt/runtime")}}
	e, _ := newCADExecutor(t, a, r)
	err := e.ExecutePackage(context.Background(), alignedPackage(t, "freecad"))
	var x *ExecutionError
	if !errors.As(err, &x) || !x.Timeout || x.Canceled || len(a.requests) != 1 || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%#v", x)
	}
}
func TestExecutorExecutePackage_CADRuntimeContextCanceledBeforeSteps(t *testing.T) {
	a := &cadTestAdapter{}
	r := &cadResolver{selections: []runtimecap.ExecutableSelection{selection("freecad", "/opt/runtime")}}
	e, _ := newCADExecutor(t, a, r)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := e.ExecutePackage(ctx, alignedPackage(t, "freecad"))
	var x *ExecutionError
	if !errors.As(err, &x) || !x.Canceled || len(a.runs)+len(a.requests) != 0 {
		t.Fatal(err)
	}
	if len(r.calls) != 1 {
		t.Fatalf("live ordering resolves during preflight: %v", r.calls)
	}
}

func TestCloneHandoffPackage_PreservesCADRuntime(t *testing.T) {
	p := alignedPackage(t, "freecad")
	c := cloneHandoffPackage(p)
	if c.CADRuntime == p.CADRuntime || c.Steps[2].CADRuntime == p.Steps[2].CADRuntime || *c.CADRuntime != *p.CADRuntime || *c.Steps[2].CADRuntime != *p.Steps[2].CADRuntime {
		t.Fatal("CAD runtime clone")
	}
	c.CSV.Headers[0] = "x"
	c.Manifest.Values["width"] = "x"
	if p.CSV.Headers[0] == "x" || p.Manifest.Values["width"] == "x" {
		t.Fatal("mutable state aliased")
	}
}

func TestExecutorExecutePackage_CADRuntimePackageSnapshotIsDefensive(t *testing.T) {
	p := alignedPackage(t, "freecad")
	a := &cadTestAdapter{}
	r := &cadResolver{selections: []runtimecap.ExecutableSelection{selection("freecad", "/opt/runtime")}}
	e, _ := newCADExecutor(t, a, r)
	if err := e.ExecutePackage(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	p.CADRuntime.Adapter = "changed"
	p.Steps[2].CADRuntime.ResultFilename = "changed"
	p.CSV.Headers[0] = "changed"
	p.Manifest.Values["width"] = "changed"
	got := e.Package()
	if got.CADRuntime.Adapter != "freecad" || got.CADRuntime.ResultFilename != "result.json" || got.CSV.Headers[0] != "width" || got.Manifest.Values["width"] == "changed" {
		t.Fatalf("snapshot=%#v", got)
	}
}

func TestExecutorExecutePackage_ClonesCADRuntimePackageBeforeDispatch(t *testing.T) {
	p := alignedPackage(t, "freecad")
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	a := &cadTestAdapter{started: started, release: release}
	r := &cadResolver{selections: []runtimecap.ExecutableSelection{selection("freecad", "/opt/runtime")}}
	e, _ := newCADExecutor(t, a, r)
	done := make(chan error, 1)
	go func() { done <- e.ExecutePackage(context.Background(), p) }()
	<-started
	p.CSV.Headers[0] = "changed"
	p.CADRuntime.Adapter = "changed"
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if a.requests[0].CSV.Headers[0] != "width" || e.Package().CADRuntime.Adapter != "freecad" {
		t.Fatal("caller mutation leaked")
	}
}

func TestExecutorExecutePackage_CADRuntimeRequestMutationDoesNotAffectStoredPackage(t *testing.T) {
	a := &cadTestAdapter{mutate: true}
	r := &cadResolver{selections: []runtimecap.ExecutableSelection{selection("freecad", "/opt/runtime")}}
	e, _ := newCADExecutor(t, a, r)
	if err := e.ExecutePackage(context.Background(), alignedPackage(t, "freecad")); err != nil {
		t.Fatal(err)
	}
	p := e.Package()
	if p.CSV.Headers[0] != "width" || p.Manifest.Values["width"] == "mutated" || p.CADRuntime.Adapter != "freecad" {
		t.Fatal("request mutation leaked")
	}
}

func TestExecutorPackage_CADRuntimeSnapshotsAreIndependent(t *testing.T) {
	a := &cadTestAdapter{}
	r := &cadResolver{selections: []runtimecap.ExecutableSelection{selection("freecad", "/opt/runtime")}}
	e, _ := newCADExecutor(t, a, r)
	if err := e.ExecutePackage(context.Background(), alignedPackage(t, "freecad")); err != nil {
		t.Fatal(err)
	}
	one := e.Package()
	one.CSV.Headers[0] = "changed"
	one.CADRuntime.Adapter = "changed"
	two := e.Package()
	if two.CSV.Headers[0] != "width" || two.CADRuntime.Adapter != "freecad" {
		t.Fatal("Package snapshots alias")
	}
}

func TestExecutorExecutePackage_CADRuntimeRetryRequestsAreDefensive(t *testing.T) {
	sentinel := errors.New("retry")
	a := &cadTestAdapter{mutate: true, errors: []error{sentinel, sentinel, nil}}
	r := &cadResolver{selections: []runtimecap.ExecutableSelection{selection("freecad", "/opt/runtime")}}
	e, _ := newCADExecutor(t, a, r)
	if err := e.ExecutePackage(context.Background(), alignedPackage(t, "freecad")); err != nil {
		t.Fatal(err)
	}
	for _, q := range a.requests {
		if q.CSV.Headers[0] != "width" || q.Manifest.Values["width"] == "mutated" || q.CADRuntime.Adapter != "freecad" {
			t.Fatal(a.requests)
		}
	}
}

func TestExecutorExecutePackage_CADRuntimeDoesNotInvokeLegacyObservationBridge(t *testing.T) {
	a := &cadTestAdapter{}
	r := &cadResolver{selections: []runtimecap.ExecutableSelection{selection("freecad", "/opt/runtime")}}
	e, dir := newCADExecutor(t, a, r)
	if err := e.ExecutePackage(context.Background(), alignedPackage(t, "freecad")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"parametron.verification.json", "parametron.observed.json", "result.json", "widget.step", "observation_request.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unexpected %s: %v", name, err)
		}
	}
}
func TestExecutorExecutePackage_CADRuntimeDoesNotRunAlignedVerification(t *testing.T) {
	TestExecutorExecutePackage_CADRuntimeDoesNotInvokeLegacyObservationBridge(t)
}
func TestExecutorExecutePackage_CADRuntimeRegistrationRequiresConsumedOutcome(t *testing.T) {
	step := planner.Step{Type: planner.StepRunCADRuntime, Payload: planner.RunCADRuntimePayload{}}
	e := New(&legacyOnlyAdapter{})
	if err := e.registerArtifact(context.Background(), step, 2); err == nil {
		t.Fatal("expected missing consumed outcome to fail registration")
	}
}

func TestExecutorExecutePackage_CADRuntimeContractDoesNotMaterializeRuntimeFiles(t *testing.T) {
	a := &cadTestAdapter{}
	r := &cadResolver{selections: []runtimecap.ExecutableSelection{selection("freecad", "/opt/runtime")}}
	e, dir := newCADExecutor(t, a, r)
	if err := e.ExecutePackage(context.Background(), alignedPackage(t, "freecad")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Task 6 materialized product dir: %v", err)
	}
}
func TestExecutorExecutePackage_CADRuntimeContractDoesNotStartExecutable(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "marker")
	path := filepath.Join(root, "runtime-that-would-create-marker")
	a := &cadTestAdapter{}
	r := &cadResolver{selections: []runtimecap.ExecutableSelection{selection("freecad", path)}}
	e, _ := newCADExecutor(t, a, r)
	if err := e.ExecutePackage(context.Background(), alignedPackage(t, "freecad")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("executable was started")
	}
}

func TestExecutorExecutePackage_CADRuntimeRequestsAreDeterministic(t *testing.T) {
	run := func() (adapter.CADRuntimeOrchestrationRequest, []string, *handoff.Package) {
		a := &cadTestAdapter{}
		r := &cadResolver{selections: []runtimecap.ExecutableSelection{selection("freecad", "/opt/runtime")}}
		e := NewWithCADRuntimeOrchestration(a, r, "/products/widget")
		if err := e.ExecutePackage(context.Background(), alignedPackage(t, "freecad")); err != nil {
			t.Fatal(err)
		}
		return a.requests[0], a.order, e.Package()
	}
	q1, o1, p1 := run()
	q2, o2, p2 := run()
	if !reflect.DeepEqual(q1, q2) || !reflect.DeepEqual(o1, o2) || !reflect.DeepEqual(p1, p2) {
		t.Fatal("nondeterministic orchestration")
	}
}

func TestExecutorCADRuntimeOrchestration_DoesNotAlterEngineeringIdentity(t *testing.T) {
	p := alignedPackage(t, "freecad")
	before, _ := json.Marshal(p)
	jobID := p.JobID
	a := &cadTestAdapter{}
	r := &cadResolver{selections: []runtimecap.ExecutableSelection{selection("freecad", "/secret/operational/path")}}
	e, _ := newCADExecutor(t, a, r)
	if err := e.ExecutePackage(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(e.Package())
	if string(before) != string(after) || e.Package().JobID != jobID || strings.Contains(string(after), "/secret/operational/path") {
		t.Fatalf("identity changed\nbefore=%s\nafter=%s", before, after)
	}
}

func TestExecutorExecutePackage_ManifestOnlyBehaviorUnchanged(t *testing.T) {
	a := &legacyOnlyAdapter{}
	p := &planner.ExecutionPlan{Steps: []planner.Step{{Type: planner.StepWriteCSV}, {Type: planner.StepWriteExportManifest}}}
	if err := New(a).Execute(context.Background(), p); err != nil || len(a.runs) != 2 {
		t.Fatalf("err=%v runs=%v", err, a.runs)
	}
}
func TestExecutorExecutePackage_ProductionFreeCADAdapterDoesNotClaimAlignedSuccess(t *testing.T) {
	a := freecad.NewFreeCADAdapter(t.TempDir())
	r := &cadResolver{selections: []runtimecap.ExecutableSelection{selection("freecad", "/opt/runtime")}}
	e, _ := newCADExecutor(t, a, r)
	err := e.ExecutePackage(context.Background(), alignedPackage(t, "freecad"))
	if !errors.Is(err, ErrCADRuntimeOrchestratorUnsupported) || len(r.calls) != 0 {
		t.Fatal(err)
	}
}
