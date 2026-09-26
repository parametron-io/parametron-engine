package executor

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/adapter"
	"parametron/internal/engine/adapter/freecad"
	"parametron/internal/engine/artifact"
	"parametron/internal/engine/cadruntime"
	"parametron/internal/engine/handoff"
	"parametron/internal/engine/observed"
	"parametron/internal/engine/runtimecap"
	"parametron/internal/engine/verification"
)

type task12Adapter struct {
	mu        sync.Mutex
	requests  []adapter.CADRuntimeOrchestrationRequest
	reads     int
	events    []string
	run       cadruntime.FreeCADRuntimeVerifiedRun
	err       error
	ok        bool
	onCall    func(adapter.CADRuntimeOrchestrationRequest) (cadruntime.FreeCADRuntimeVerifiedRun, bool, error)
	onContext func(context.Context, adapter.CADRuntimeOrchestrationRequest) (cadruntime.FreeCADRuntimeVerifiedRun, bool, error)
}

func (a *task12Adapter) Run(context.Context, interfaceStep) error { return nil }

// interfaceStep is an alias which keeps the fake's Adapter method explicit below.
type interfaceStep = planner.Step

func (a *task12Adapter) OrchestrateCADRuntime(ctx context.Context, req adapter.CADRuntimeOrchestrationRequest) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.requests = append(a.requests, cloneCADRuntimeRequest(req))
	a.events = append(a.events, "orchestrate:"+strconv.Itoa(req.Attempt))
	if a.onCall != nil {
		a.run, a.ok, a.err = a.onCall(req)
	}
	if a.onContext != nil {
		a.run, a.ok, a.err = a.onContext(ctx, req)
	}
	return a.err
}

func (a *task12Adapter) CADRuntimeVerifiedRun() (cadruntime.FreeCADRuntimeVerifiedRun, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reads++
	a.events = append(a.events, "read:"+strconv.Itoa(a.run.Runtime.ObservationRequest.Manifest.Attempt.Identity.Attempt))
	return cloneTask12Run(a.run), a.ok
}

func cloneTask12Run(in cadruntime.FreeCADRuntimeVerifiedRun) cadruntime.FreeCADRuntimeVerifiedRun {
	out := in
	out.Runtime.Artifacts = append([]cadruntime.FreeCADRuntimeValidatedArtifact(nil), in.Runtime.Artifacts...)
	if in.Runtime.Result != nil {
		v := *in.Runtime.Result
		out.Runtime.Result = &v
	}
	if in.Verification != nil {
		v := *in.Verification
		out.Verification = &v
	}
	return out
}

func task12Request(root string, attempt int) adapter.CADRuntimeOrchestrationRequest {
	return adapter.CADRuntimeOrchestrationRequest{
		JobID: "job-12", ProductKey: "widget", StepID: "2", Attempt: attempt,
		Manifest:   planner.WriteExportManifestPayload{PlanHash: "plan-12"},
		CADRuntime: planner.RunCADRuntimePayload{Adapter: "freecad"},
	}
}

func task12Pass(root string, attempt int) cadruntime.FreeCADRuntimeVerifiedRun {
	working := filepath.Join(root, "attempt")
	result := filepath.Join(working, "result.json")
	return cadruntime.FreeCADRuntimeVerifiedRun{
		Runtime: cadruntime.FreeCADRuntimeRun{
			ObservationRequest: cadruntime.FreeCADRuntimeObservationRequestMaterialization{
				Manifest: freecad.FreeCADRuntimeManifestMaterialization{Attempt: freecad.FreeCADRuntimeAttempt{
					Identity: freecad.FreeCADRuntimeAttemptIdentity{
						ID: "attempt-12", JobID: "job-12", ProductKey: "widget", StepID: "2",
						Attempt: attempt, Adapter: "freecad", PlanHash: "plan-12",
					},
					Layout: freecad.FreeCADRuntimeWorkingCopyLayout{
						WorkingCopyDir: working, ResultPath: result, OutputDir: filepath.Join(working, "outputs"),
					},
				}},
			},
			ExecutionRequest: freecad.FreeCADRuntimeExecutionRequest{ResultPath: result, OutputDir: filepath.Join(working, "outputs")},
			Result:           &freecad.FreeCADRuntimeResult{Status: freecad.FreeCADRuntimeResultStatusSucceeded},
			Artifacts: []cadruntime.FreeCADRuntimeValidatedArtifact{
				{ID: "step", Format: "step", RelativePath: "outputs/widget.step", Path: filepath.Join(working, "outputs", "widget.step")},
				{ID: "csv", Format: "csv", RelativePath: "outputs/nested/table.csv", Path: filepath.Join(working, "outputs", "nested", "table.csv")},
				{ID: "pdf", Format: "pdf", RelativePath: "outputs/drawing.pdf", Path: filepath.Join(working, "outputs", "drawing.pdf")},
			},
		},
		Verification: &verification.Result{Status: verification.StatusPass, Failure: verification.FailureClassNone},
	}
}

func task12RunForRequest(root string, req adapter.CADRuntimeOrchestrationRequest) cadruntime.FreeCADRuntimeVerifiedRun {
	run := task12Pass(filepath.Join(root, strconv.Itoa(req.Attempt)), req.Attempt)
	attempt := &run.Runtime.ObservationRequest.Manifest.Attempt
	attempt.Identity = freecad.FreeCADRuntimeAttemptIdentity{
		ID: "attempt-" + strconv.Itoa(req.Attempt), JobID: req.JobID, ProductKey: req.ProductKey,
		StepID: req.StepID, Attempt: req.Attempt, Adapter: req.CADRuntime.Adapter, PlanHash: req.Manifest.PlanHash,
	}
	return run
}

func task12Execution(t *testing.T, a *task12Adapter) (*Executor, *handoff.Package) {
	t.Helper()
	pkg := alignedPackage(t, "freecad")
	productDir := t.TempDir()
	e := NewWithCADRuntimeConsumption(a, &cadResolver{selections: []runtimecap.ExecutableSelection{selection("freecad", "/opt/runtime")}}, productDir)
	return e, pkg
}

func consumeTask12(t *testing.T, mutate func(*adapter.CADRuntimeOrchestrationRequest, *cadruntime.FreeCADRuntimeVerifiedRun), orchestrationErr error) (*Executor, error) {
	t.Helper()
	root := t.TempDir()
	req := task12Request(root, 1)
	run := task12Pass(root, 1)
	if mutate != nil {
		mutate(&req, &run)
	}
	e := New(&legacyOnlyAdapter{})
	return e, e.consumeCADRuntimeResult(req, run, orchestrationErr)
}

func TestNewWithCADRuntimeConsumption_EnablesVerifiedRunConsumption(t *testing.T) {
	a := &task12Adapter{}
	e := NewWithCADRuntimeConsumption(a, &cadResolver{}, t.TempDir())
	if !e.cadRuntimeConsumption || e.cadRuntime == nil || e.CADRuntimeOutcome() != nil || a.reads != 0 {
		t.Fatalf("constructor state = %#v reads=%d", e, a.reads)
	}
}

func TestNewWithCADRuntimeOrchestration_PreservesOrchestrationOnlyBehavior(t *testing.T) {
	e := NewWithCADRuntimeOrchestration(&cadTestAdapter{}, &cadResolver{}, t.TempDir())
	if e.cadRuntimeConsumption || e.CADRuntimeOutcome() != nil {
		t.Fatalf("orchestration-only constructor enabled consumption")
	}
}

func TestExecutorCADRuntimeConsumption_RequiresVerifiedRunProvider(t *testing.T) {
	e := NewWithCADRuntimeConsumption(&cadTestAdapter{}, &cadResolver{selections: []runtimecap.ExecutableSelection{selection("freecad", "/opt/runtime")}}, t.TempDir())
	e.pkg = alignedPackage(t, "freecad")
	_, err := e.prepareCADRuntimeDispatch(e.pkg.Plan())
	var got *CADRuntimeConsumptionError
	if !errors.As(err, &got) || got.Stage != CADRuntimeConsumptionStageOutcomeUnavailable ||
		!errors.Is(err, ErrCADRuntimeVerifiedRunProviderUnsupported) || e.CADRuntimeOutcome() != nil {
		t.Fatalf("err=%v outcome=%#v", err, e.CADRuntimeOutcome())
	}
}

func TestExecutorCADRuntimeConsumption_ConsumesVerificationPass(t *testing.T) {
	e, err := consumeTask12(t, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := e.CADRuntimeOutcome()
	if got == nil || got.Verification != artifact.VerificationOutcomePassed || got.AttemptID != "attempt-12" ||
		e.ArtifactAcceptanceContext().VerificationOutcome != artifact.VerificationOutcomePassed {
		t.Fatalf("outcome=%#v acceptance=%#v", got, e.ArtifactAcceptanceContext())
	}
}

func TestExecutorCADRuntimeConsumption_ConsumesNoParameterPass(t *testing.T) {
	e, err := consumeTask12(t, nil, nil)
	if err != nil || e.CADRuntimeOutcome().Verification != artifact.VerificationOutcomePassed {
		t.Fatalf("err=%v outcome=%#v", err, e.CADRuntimeOutcome())
	}
}

func TestExecutorCADRuntimeConsumption_ConsumesParameterPass(t *testing.T) {
	e, err := consumeTask12(t, func(_ *adapter.CADRuntimeOrchestrationRequest, run *cadruntime.FreeCADRuntimeVerifiedRun) {
		run.Verification.Categories.Parameters = verification.CategoryResult{Enabled: true, Status: verification.CategoryStatusPass}
	}, nil)
	if err != nil || e.CADRuntimeOutcome().Verification != artifact.VerificationOutcomePassed {
		t.Fatalf("err=%v outcome=%#v", err, e.CADRuntimeOutcome())
	}
}

func TestExecutorCADRuntimeConsumption_PreservesArtifactOrder(t *testing.T) {
	e, err := consumeTask12(t, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := e.CADRuntimeOutcome().Artifacts
	if len(got) != 3 || got[0].Format != "step" || got[1].Format != "csv" || got[2].Format != "pdf" ||
		got[1].RelativePath != "outputs/nested/table.csv" {
		t.Fatalf("artifacts=%#v", got)
	}
}

func TestExecutorCADRuntimeConsumption_RejectsMismatchedOutcomeIdentity(t *testing.T) {
	cases := map[string]func(*adapter.CADRuntimeOrchestrationRequest, *cadruntime.FreeCADRuntimeVerifiedRun){
		"job": func(_ *adapter.CADRuntimeOrchestrationRequest, r *cadruntime.FreeCADRuntimeVerifiedRun) {
			r.Runtime.ObservationRequest.Manifest.Attempt.Identity.JobID = "other"
		},
		"product": func(_ *adapter.CADRuntimeOrchestrationRequest, r *cadruntime.FreeCADRuntimeVerifiedRun) {
			r.Runtime.ObservationRequest.Manifest.Attempt.Identity.ProductKey = "other"
		},
		"step": func(_ *adapter.CADRuntimeOrchestrationRequest, r *cadruntime.FreeCADRuntimeVerifiedRun) {
			r.Runtime.ObservationRequest.Manifest.Attempt.Identity.StepID = "9"
		},
		"attempt": func(_ *adapter.CADRuntimeOrchestrationRequest, r *cadruntime.FreeCADRuntimeVerifiedRun) {
			r.Runtime.ObservationRequest.Manifest.Attempt.Identity.Attempt = 2
		},
		"adapter": func(_ *adapter.CADRuntimeOrchestrationRequest, r *cadruntime.FreeCADRuntimeVerifiedRun) {
			r.Runtime.ObservationRequest.Manifest.Attempt.Identity.Adapter = "other"
		},
		"plan": func(_ *adapter.CADRuntimeOrchestrationRequest, r *cadruntime.FreeCADRuntimeVerifiedRun) {
			r.Runtime.ObservationRequest.Manifest.Attempt.Identity.PlanHash = "other"
		},
		"result": func(_ *adapter.CADRuntimeOrchestrationRequest, r *cadruntime.FreeCADRuntimeVerifiedRun) {
			r.Runtime.ExecutionRequest.ResultPath += ".other"
		},
		"working-copy": func(_ *adapter.CADRuntimeOrchestrationRequest, r *cadruntime.FreeCADRuntimeVerifiedRun) {
			r.Runtime.ObservationRequest.Manifest.Attempt.Layout.WorkingCopyDir = "relative"
		},
		"artifact-outside": func(_ *adapter.CADRuntimeOrchestrationRequest, r *cadruntime.FreeCADRuntimeVerifiedRun) {
			r.Runtime.Artifacts[0].Path = filepath.Join(filepath.Dir(r.Runtime.Artifacts[0].Path), "..", "..", "escape")
		},
		"artifact-noncanonical": func(_ *adapter.CADRuntimeOrchestrationRequest, r *cadruntime.FreeCADRuntimeVerifiedRun) {
			r.Runtime.Artifacts[0].Path += string(filepath.Separator) + ".."
		},
		"reference-traversal-wrong-path": func(_ *adapter.CADRuntimeOrchestrationRequest, r *cadruntime.FreeCADRuntimeVerifiedRun) {
			sibling := filepath.Join(filepath.Dir(r.Runtime.ObservationRequest.Manifest.Attempt.Layout.OutputDir), "sibling-outputs")
			r.Runtime.ReferenceTraversalPath = filepath.Join(sibling, cadruntime.FreeCADRuntimeReferenceTraversalFilename)
		},
		"reference-traversal-bytes-without-path": func(_ *adapter.CADRuntimeOrchestrationRequest, r *cadruntime.FreeCADRuntimeVerifiedRun) {
			r.Runtime.ReferenceTraversalJSON = []byte(`{"schemaVersion":"1.0"}`)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			e, err := consumeTask12(t, mutate, nil)
			var got *CADRuntimeConsumptionError
			if !errors.As(err, &got) || got.Stage != CADRuntimeConsumptionStageOutcomeValidation ||
				e.ArtifactAcceptanceContext().VerificationOutcome == artifact.VerificationOutcomePassed {
				t.Fatalf("err=%v outcome=%#v", err, e.CADRuntimeOutcome())
			}
		})
	}
}

func TestExecutorCADRuntimeConsumption_ConsumesReferenceTraversalWithAuthoritativePath(t *testing.T) {
	var wantPath string
	traversal := []byte(`{"schemaVersion":"1.0"}`)
	e, err := consumeTask12(t, func(_ *adapter.CADRuntimeOrchestrationRequest, r *cadruntime.FreeCADRuntimeVerifiedRun) {
		outputDir := r.Runtime.ObservationRequest.Manifest.Attempt.Layout.OutputDir
		wantPath = filepath.Join(outputDir, cadruntime.FreeCADRuntimeReferenceTraversalFilename)
		r.Runtime.ReferenceTraversalPath = wantPath
		r.Runtime.ReferenceTraversalJSON = traversal
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := e.CADRuntimeOutcome()
	if got.ReferenceTraversalPath != wantPath || !bytes.Equal(got.ReferenceTraversalJSON, traversal) {
		t.Fatalf("traversal not consumed: path=%q bytes=%q", got.ReferenceTraversalPath, got.ReferenceTraversalJSON)
	}
}

func TestExecutorCADRuntimeConsumption_PropagatesEvidenceReadingFields(t *testing.T) {
	var wantWorkingCopyDir, wantObservationRequestPath, wantObservedPath string
	e, err := consumeTask12(t, func(_ *adapter.CADRuntimeOrchestrationRequest, r *cadruntime.FreeCADRuntimeVerifiedRun) {
		wantWorkingCopyDir = r.Runtime.ObservationRequest.Manifest.Attempt.Layout.WorkingCopyDir
		r.Runtime.ExecutionRequest.WorkingCopyDir = wantWorkingCopyDir
		wantObservationRequestPath = filepath.Join(wantWorkingCopyDir, "prm.verification.json")
		r.Runtime.ObservationRequest.Path = wantObservationRequestPath
		r.Runtime.ExecutionRequest.ObservationRequestPath = wantObservationRequestPath
		wantObservedPath = filepath.Join(r.Runtime.ObservationRequest.Manifest.Attempt.Layout.OutputDir, cadruntime.FreeCADRuntimeObservedFilename)
		r.Runtime.ObservedPath = wantObservedPath
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := e.CADRuntimeOutcome()
	if wantWorkingCopyDir == "" || wantObservationRequestPath == "" || wantObservedPath == "" {
		t.Fatal("fixture must produce non-empty paths for this test to be meaningful")
	}
	if got.WorkingCopyDir != wantWorkingCopyDir {
		t.Fatalf("WorkingCopyDir = %q, want %q", got.WorkingCopyDir, wantWorkingCopyDir)
	}
	if got.ObservationRequestPath != wantObservationRequestPath {
		t.Fatalf("ObservationRequestPath = %q, want %q", got.ObservationRequestPath, wantObservationRequestPath)
	}
	if got.ObservedPath != wantObservedPath {
		t.Fatalf("ObservedPath = %q, want %q", got.ObservedPath, wantObservedPath)
	}
}

func TestExecutorCADRuntimeConsumption_AcceptsLegacyEmptyReferenceTraversal(t *testing.T) {
	e, err := consumeTask12(t, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := e.CADRuntimeOutcome()
	if got.ReferenceTraversalPath != "" || len(got.ReferenceTraversalJSON) != 0 {
		t.Fatalf("expected empty legacy-compatible traversal transport by default: %#v", got)
	}
}

func TestExecutorCADRuntimeConsumption_RejectsInvalidAttemptPaths(t *testing.T) {
	cases := map[string]func(*cadruntime.FreeCADRuntimeVerifiedRun){
		"output outside": func(r *cadruntime.FreeCADRuntimeVerifiedRun) {
			path := filepath.Join(filepath.Dir(r.Runtime.ObservationRequest.Manifest.Attempt.Layout.WorkingCopyDir), "outside")
			r.Runtime.ObservationRequest.Manifest.Attempt.Layout.OutputDir, r.Runtime.ExecutionRequest.OutputDir = path, path
		},
		"result outside": func(r *cadruntime.FreeCADRuntimeVerifiedRun) {
			outside := filepath.Join(filepath.Dir(r.Runtime.ObservationRequest.Manifest.Attempt.Layout.WorkingCopyDir), "result.json")
			r.Runtime.ExecutionRequest.ResultPath = outside
			r.Runtime.ObservationRequest.Manifest.Attempt.Layout.ResultPath = outside
		},
		"noncanonical output": func(r *cadruntime.FreeCADRuntimeVerifiedRun) {
			path := r.Runtime.ObservationRequest.Manifest.Attempt.Layout.OutputDir + string(filepath.Separator) + ".."
			r.Runtime.ObservationRequest.Manifest.Attempt.Layout.OutputDir, r.Runtime.ExecutionRequest.OutputDir = path, path
		},
		"noncanonical result": func(r *cadruntime.FreeCADRuntimeVerifiedRun) {
			path := r.Runtime.ExecutionRequest.ResultPath + string(filepath.Separator) + ".."
			r.Runtime.ExecutionRequest.ResultPath = path
			r.Runtime.ObservationRequest.Manifest.Attempt.Layout.ResultPath = path
		},
		"artifact resolves outside": func(r *cadruntime.FreeCADRuntimeVerifiedRun) {
			r.Runtime.Artifacts[0].Path = filepath.Join(filepath.Dir(r.Runtime.ObservationRequest.Manifest.Attempt.Layout.WorkingCopyDir), "escape.step")
		},
		"artifact lexical traversal": func(r *cadruntime.FreeCADRuntimeVerifiedRun) {
			r.Runtime.Artifacts[0].RelativePath = "outputs/../escape.step"
		},
		"artifact relative mismatch": func(r *cadruntime.FreeCADRuntimeVerifiedRun) {
			r.Runtime.Artifacts[0].RelativePath = "outputs/other.step"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			var original cadruntime.FreeCADRuntimeVerifiedRun
			a := &task12Adapter{onCall: func(req adapter.CADRuntimeOrchestrationRequest) (cadruntime.FreeCADRuntimeVerifiedRun, bool, error) {
				run := task12RunForRequest(root, req)
				mutate(&run)
				original = cloneTask12Run(run)
				return run, true, nil
			}}
			e, pkg := task12Execution(t, a)
			err := e.ExecutePackage(context.Background(), pkg)
			var consumptionErr *CADRuntimeConsumptionError
			if !errors.As(err, &consumptionErr) || consumptionErr.Stage != CADRuntimeConsumptionStageOutcomeValidation {
				t.Fatalf("err=%v", err)
			}
			if a.reads != 1 || len(a.requests) != 1 || !reflect.DeepEqual(a.run, original) {
				t.Fatalf("reads=%d requests=%d provider mutated=%v", a.reads, len(a.requests), !reflect.DeepEqual(a.run, original))
			}
			if e.ArtifactAcceptanceContext().VerificationOutcome == artifact.VerificationOutcomePassed ||
				e.ArtifactAcceptanceContext().FinalOutcome != artifact.FinalOutcomeFailed ||
				!isTerminalCADRuntimeConsumptionError(consumptionErr) {
				t.Fatalf("acceptance=%#v err=%v", e.ArtifactAcceptanceContext(), err)
			}
			if outcome := e.CADRuntimeOutcome(); outcome != nil && len(outcome.Artifacts) != 0 {
				t.Fatalf("projected artifacts=%#v", outcome.Artifacts)
			}
		})
	}
}

func TestExecutorCADRuntimeConsumption_RejectsUnavailableOutcome(t *testing.T) {
	req := task12Request(t.TempDir(), 1)
	e := New(&legacyOnlyAdapter{})
	err := e.consumptionError(CADRuntimeConsumptionStageOutcomeUnavailable, req, "", "", "", "", "", "", errors.New("no snapshot"))
	var got *CADRuntimeConsumptionError
	if !errors.As(err, &got) || got.Stage != CADRuntimeConsumptionStageOutcomeUnavailable {
		t.Fatal(err)
	}
}

func TestExecutorCADRuntimeConsumption_RejectsStaleAttemptOutcome(t *testing.T) {
	e, err := consumeTask12(t, func(_ *adapter.CADRuntimeOrchestrationRequest, r *cadruntime.FreeCADRuntimeVerifiedRun) {
		r.Runtime.ObservationRequest.Manifest.Attempt.Identity.Attempt = 0
	}, nil)
	if err == nil || e.ArtifactAcceptanceContext().VerificationOutcome == artifact.VerificationOutcomePassed {
		t.Fatalf("err=%v", err)
	}
}

func TestExecutorCADRuntimeConsumption_RejectsContradictoryPassOutcome(t *testing.T) {
	cases := map[string]func(*cadruntime.FreeCADRuntimeVerifiedRun){
		"nil verification":  func(r *cadruntime.FreeCADRuntimeVerifiedRun) { r.Verification = nil },
		"fail verification": func(r *cadruntime.FreeCADRuntimeVerifiedRun) { r.Verification.Status = verification.StatusFail },
		"failure class": func(r *cadruntime.FreeCADRuntimeVerifiedRun) {
			r.Verification.Failure = verification.FailureClassInternalError
		},
		"nil result": func(r *cadruntime.FreeCADRuntimeVerifiedRun) { r.Runtime.Result = nil },
		"failed result": func(r *cadruntime.FreeCADRuntimeVerifiedRun) {
			r.Runtime.Result.Status = freecad.FreeCADRuntimeResultStatusFailed
		},
		"missing result path": func(r *cadruntime.FreeCADRuntimeVerifiedRun) { r.Runtime.ExecutionRequest.ResultPath = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := consumeTask12(t, func(_ *adapter.CADRuntimeOrchestrationRequest, r *cadruntime.FreeCADRuntimeVerifiedRun) { mutate(r) }, nil)
			var got *CADRuntimeConsumptionError
			if !errors.As(err, &got) || got.Stage != CADRuntimeConsumptionStageOutcomeValidation {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestExecutorCADRuntimeConsumption_ConsumesVerificationFailure(t *testing.T) {
	class := verification.FailureClassParameterMismatch
	cause := &verification.VerifyError{Class: class, Message: "width mismatch"}
	verr := &cadruntime.FreeCADRuntimeVerificationError{Stage: cadruntime.FreeCADRuntimeVerificationStageVerificationComparison, AttemptID: "attempt-12", FailureClass: class, Err: cause}
	e, err := consumeTask12(t, func(_ *adapter.CADRuntimeOrchestrationRequest, r *cadruntime.FreeCADRuntimeVerifiedRun) {
		r.Verification = &verification.Result{Status: verification.StatusFail, Failure: class, Message: "width mismatch"}
	}, verr)
	var got *CADRuntimeConsumptionError
	if !errors.As(err, &got) || got.Stage != CADRuntimeConsumptionStageVerificationFailure || !errors.Is(err, cause) ||
		e.CADRuntimeOutcome().Verification != artifact.VerificationOutcomeFailed {
		t.Fatalf("err=%v outcome=%#v", err, e.CADRuntimeOutcome())
	}
}

func TestExecutorCADRuntimeConsumption_PreservesRequiredObservationFailure(t *testing.T) {
	testTask12VerificationClass(t, verification.FailureClassRequiredObservationMissing)
}

func TestExecutorCADRuntimeConsumption_PreservesVerificationClassifications(t *testing.T) {
	for _, class := range []verification.FailureClass{verification.FailureClassParameterMismatch, verification.FailureClassRequiredObservationMissing, verification.FailureClassMetadataMismatch, verification.FailureClassReferenceMismatch, verification.FailureClassContractInvalid, verification.FailureClassObservedInvalid} {
		t.Run(string(class), func(t *testing.T) { testTask12VerificationClass(t, class) })
	}
}

func testTask12VerificationClass(t *testing.T, class verification.FailureClass) {
	t.Helper()
	verr := &cadruntime.FreeCADRuntimeVerificationError{Stage: cadruntime.FreeCADRuntimeVerificationStageVerificationComparison, AttemptID: "attempt-12", FailureClass: class, Err: errors.New("comparison")}
	e, err := consumeTask12(t, func(_ *adapter.CADRuntimeOrchestrationRequest, r *cadruntime.FreeCADRuntimeVerifiedRun) {
		r.Verification = &verification.Result{Status: verification.StatusFail, Failure: class, Message: "comparison"}
	}, verr)
	if err == nil || e.CADRuntimeOutcome().VerificationClass != class {
		t.Fatalf("err=%v outcome=%#v", err, e.CADRuntimeOutcome())
	}
}

func TestExecutorCADRuntimeConsumption_ConsumesVerificationResultFailure(t *testing.T) {
	cause := errors.New("inconsistent verifier")
	verr := &cadruntime.FreeCADRuntimeVerificationError{Stage: cadruntime.FreeCADRuntimeVerificationStageVerificationResult, AttemptID: "attempt-12", Err: cause}
	e, err := consumeTask12(t, nil, verr)
	if !errors.Is(err, cause) || e.CADRuntimeOutcome().VerificationClass != verification.FailureClassInternalError ||
		e.CADRuntimeOutcome().Verification != artifact.VerificationOutcomeUnknown {
		t.Fatalf("err=%v outcome=%#v", err, e.CADRuntimeOutcome())
	}
}

func TestExecutorCADRuntimeConsumption_PreservesRuntimeNativeFailure(t *testing.T) {
	stage := "export"
	runErr := &cadruntime.FreeCADRuntimeRunError{Stage: cadruntime.FreeCADRuntimeRunStageRuntimeReportedFailure, Err: errors.New("exit")}
	verr := &cadruntime.FreeCADRuntimeVerificationError{Stage: cadruntime.FreeCADRuntimeVerificationStageRuntimeExecution, AttemptID: "attempt-12", Err: runErr}
	e, err := consumeTask12(t, func(_ *adapter.CADRuntimeOrchestrationRequest, r *cadruntime.FreeCADRuntimeVerifiedRun) {
		r.Verification = nil
		r.Runtime.Result = &freecad.FreeCADRuntimeResult{Status: freecad.FreeCADRuntimeResultStatusFailed, Failure: &freecad.FreeCADRuntimeResultFailure{Boundary: "freecad", Category: "runtime", Code: "export_failed", Message: "export failed", Stage: &stage}}
	}, verr)
	f := e.CADRuntimeOutcome().Failure
	if !errors.Is(err, runErr) || f == nil || f.Code != "export_failed" || f.Stage != stage || f.Boundary != "freecad" {
		t.Fatalf("err=%v failure=%#v", err, f)
	}
}

func TestExecutorCADRuntimeConsumption_NormalizesRuntimeInfrastructureFailure(t *testing.T) {
	runErr := &cadruntime.FreeCADRuntimeRunError{Stage: cadruntime.FreeCADRuntimeRunStageResultLoad, Err: errors.New("missing result")}
	verr := &cadruntime.FreeCADRuntimeVerificationError{Stage: cadruntime.FreeCADRuntimeVerificationStageRuntimeExecution, Err: runErr}
	e, err := consumeTask12(t, func(_ *adapter.CADRuntimeOrchestrationRequest, r *cadruntime.FreeCADRuntimeVerifiedRun) {
		r.Verification, r.Runtime.Result = nil, nil
	}, verr)
	f := e.CADRuntimeOutcome().Failure
	if !errors.Is(err, runErr) || f.Code != cadruntime.FreeCADRuntimeRunStageResultLoad || f.Boundary != "engine" {
		t.Fatalf("err=%v failure=%#v", err, f)
	}
}

func TestExecutorCADRuntimeConsumption_DoesNotRetryDeterministicFailures(t *testing.T) {
	type setup func(*cadruntime.FreeCADRuntimeVerifiedRun) (bool, error)
	runtimeFailure := func(stage string) setup {
		return func(run *cadruntime.FreeCADRuntimeVerifiedRun) (bool, error) {
			run.Runtime.Result, run.Verification, run.Runtime.Artifacts = nil, nil, nil
			runErr := &cadruntime.FreeCADRuntimeRunError{Stage: stage, Err: errors.New(stage)}
			return true, &cadruntime.FreeCADRuntimeVerificationError{Stage: cadruntime.FreeCADRuntimeVerificationStageRuntimeExecution, Err: runErr}
		}
	}
	cases := []struct {
		name, wantStage string
		setup           setup
	}{
		{"request materialization", CADRuntimeConsumptionStageRuntimeFailure, runtimeFailure(cadruntime.FreeCADRuntimeRunStageRequestMaterialization)},
		{"output preparation", CADRuntimeConsumptionStageRuntimeFailure, runtimeFailure(cadruntime.FreeCADRuntimeRunStageOutputPreparation)},
		{"result load", CADRuntimeConsumptionStageRuntimeFailure, runtimeFailure(cadruntime.FreeCADRuntimeRunStageResultLoad)},
		{"process result correlation", CADRuntimeConsumptionStageRuntimeFailure, runtimeFailure(cadruntime.FreeCADRuntimeRunStageProcessResultCorrelation)},
		{"artifact validation", CADRuntimeConsumptionStageRuntimeFailure, runtimeFailure(cadruntime.FreeCADRuntimeRunStageArtifactValidation)},
		{"observed load", CADRuntimeConsumptionStageRuntimeFailure, runtimeFailure(cadruntime.FreeCADRuntimeRunStageObservedLoad)},
		{"observed correlation", CADRuntimeConsumptionStageRuntimeFailure, runtimeFailure(cadruntime.FreeCADRuntimeRunStageObservedCorrelation)},
		{"provider unavailable", CADRuntimeConsumptionStageOutcomeUnavailable, func(_ *cadruntime.FreeCADRuntimeVerifiedRun) (bool, error) { return false, nil }},
		{"stale provider", CADRuntimeConsumptionStageOutcomeValidation, func(run *cadruntime.FreeCADRuntimeVerifiedRun) (bool, error) {
			run.Runtime.ObservationRequest.Manifest.Attempt.Identity.Attempt--
			return true, nil
		}},
		{"identity mismatch", CADRuntimeConsumptionStageOutcomeValidation, func(run *cadruntime.FreeCADRuntimeVerifiedRun) (bool, error) {
			run.Runtime.ObservationRequest.Manifest.Attempt.Identity.JobID = "other"
			return true, nil
		}},
		{"verification comparison", CADRuntimeConsumptionStageVerificationFailure, func(run *cadruntime.FreeCADRuntimeVerifiedRun) (bool, error) {
			class := verification.FailureClassParameterMismatch
			run.Verification = &verification.Result{Status: verification.StatusFail, Failure: class, Message: "mismatch"}
			return true, &cadruntime.FreeCADRuntimeVerificationError{Stage: cadruntime.FreeCADRuntimeVerificationStageVerificationComparison, FailureClass: class, Err: &verification.VerifyError{Class: class, Message: "mismatch"}}
		}},
		{"verification result", CADRuntimeConsumptionStageVerificationResult, func(_ *cadruntime.FreeCADRuntimeVerifiedRun) (bool, error) {
			return true, &cadruntime.FreeCADRuntimeVerificationError{Stage: cadruntime.FreeCADRuntimeVerificationStageVerificationResult, Err: errors.New("inconsistent")}
		}},
		{"artifact projection", CADRuntimeConsumptionStageArtifactProjection, func(run *cadruntime.FreeCADRuntimeVerifiedRun) (bool, error) {
			run.Runtime.Artifacts[0].Format = "iges"
			return true, nil
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			a := &task12Adapter{onCall: func(req adapter.CADRuntimeOrchestrationRequest) (cadruntime.FreeCADRuntimeVerifiedRun, bool, error) {
				run := task12RunForRequest(root, req)
				ok, err := tc.setup(&run)
				return run, ok, err
			}}
			e, pkg := task12Execution(t, a)
			err := e.ExecutePackage(context.Background(), pkg)
			var consumptionErr *CADRuntimeConsumptionError
			if !errors.As(err, &consumptionErr) || consumptionErr.Stage != tc.wantStage {
				t.Fatalf("err=%v", err)
			}
			if len(a.requests) != 1 || a.reads != 1 || a.requests[0].Attempt != 1 {
				t.Fatalf("requests=%#v reads=%d", a.requests, a.reads)
			}
			if e.ArtifactAcceptanceContext().FinalOutcome != artifact.FinalOutcomeFailed ||
				e.ArtifactAcceptanceContext().VerificationOutcome == artifact.VerificationOutcomePassed {
				t.Fatalf("acceptance=%#v", e.ArtifactAcceptanceContext())
			}
		})
	}
}

func TestExecutorCADRuntimeConsumption_RetriesEligibleProcessFailures(t *testing.T) {
	root := t.TempDir()
	a := &task12Adapter{onCall: func(req adapter.CADRuntimeOrchestrationRequest) (cadruntime.FreeCADRuntimeVerifiedRun, bool, error) {
		run := task12RunForRequest(root, req)
		switch req.Attempt {
		case 1:
			run.Runtime.Result, run.Verification, run.Runtime.Artifacts = nil, nil, nil
			runErr := &cadruntime.FreeCADRuntimeRunError{Stage: cadruntime.FreeCADRuntimeRunStageProcessInvocation, AttemptID: "attempt-1", Err: errors.New("start failed")}
			return run, true, &cadruntime.FreeCADRuntimeVerificationError{Stage: cadruntime.FreeCADRuntimeVerificationStageRuntimeExecution, AttemptID: "attempt-1", Err: runErr}
		case 2:
			stage := "execute"
			run.Verification, run.Runtime.Artifacts = nil, []cadruntime.FreeCADRuntimeValidatedArtifact{{ID: "stale", Format: "step", RelativePath: "outputs/stale.step", Path: filepath.Join(run.Runtime.ObservationRequest.Manifest.Attempt.Layout.WorkingCopyDir, "outputs", "stale.step")}}
			run.Runtime.Result = &freecad.FreeCADRuntimeResult{Status: freecad.FreeCADRuntimeResultStatusFailed, Failure: &freecad.FreeCADRuntimeResultFailure{Boundary: "freecad", Category: "runtime", Code: "transient", Message: "retry", Stage: &stage}}
			runErr := &cadruntime.FreeCADRuntimeRunError{Stage: cadruntime.FreeCADRuntimeRunStageRuntimeReportedFailure, AttemptID: "attempt-2", Err: errors.New("exit")}
			return run, true, &cadruntime.FreeCADRuntimeVerificationError{Stage: cadruntime.FreeCADRuntimeVerificationStageRuntimeExecution, AttemptID: "attempt-2", Err: runErr}
		default:
			return run, true, nil
		}
	}}
	e, pkg := task12Execution(t, a)
	if err := e.ExecutePackage(context.Background(), pkg); err != nil {
		t.Fatal(err)
	}
	if len(a.requests) != 3 || a.reads != 3 {
		t.Fatalf("requests=%d reads=%d", len(a.requests), a.reads)
	}
	for i, req := range a.requests {
		if req.Attempt != i+1 {
			t.Fatalf("request %d attempt=%d", i, req.Attempt)
		}
	}
	wantEvents := []string{"orchestrate:1", "read:1", "orchestrate:2", "read:2", "orchestrate:3", "read:3"}
	if !reflect.DeepEqual(a.events, wantEvents) {
		t.Fatalf("events=%v", a.events)
	}
	outcome := e.CADRuntimeOutcome()
	if outcome == nil || outcome.Attempt != 3 || outcome.AttemptID != "attempt-3" ||
		outcome.Verification != artifact.VerificationOutcomePassed ||
		e.ArtifactAcceptanceContext().FinalOutcome != artifact.FinalOutcomeSucceeded ||
		e.ArtifactAcceptanceContext().VerificationOutcome != artifact.VerificationOutcomePassed {
		t.Fatalf("outcome=%#v acceptance=%#v", outcome, e.ArtifactAcceptanceContext())
	}
	for _, item := range outcome.Artifacts {
		if strings.Contains(item.Path, "stale") || !strings.Contains(item.Path, string(filepath.Separator)+"3"+string(filepath.Separator)) {
			t.Fatalf("prior attempt artifact leaked: %#v", item)
		}
	}
}

func TestExecutorCADRuntimeConsumption_PreservesContextTermination(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cause    error
		timeout  bool
		canceled bool
	}{{"canceled", context.Canceled, false, true}, {"deadline", context.DeadlineExceeded, true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			a := &task12Adapter{onContext: func(_ context.Context, _ adapter.CADRuntimeOrchestrationRequest) (cadruntime.FreeCADRuntimeVerifiedRun, bool, error) {
				return cadruntime.FreeCADRuntimeVerifiedRun{}, false, tc.cause
			}}
			e, pkg := task12Execution(t, a)
			// Seed stale state and prove ExecutePackage clears it.
			e.cadRuntimeOutcome = &CADRuntimeOutcome{AttemptID: "stale"}
			err := e.ExecutePackage(context.Background(), pkg)
			var executionErr *ExecutionError
			if !errors.As(err, &executionErr) || !errors.Is(err, tc.cause) ||
				executionErr.Timeout != tc.timeout || executionErr.Canceled != tc.canceled {
				t.Fatalf("err=%v execution=%#v", err, executionErr)
			}
			if len(a.requests) != 1 || a.reads != 1 || e.CADRuntimeOutcome() != nil ||
				e.ArtifactAcceptanceContext().VerificationOutcome == artifact.VerificationOutcomePassed {
				t.Fatalf("requests=%d reads=%d outcome=%#v acceptance=%#v", len(a.requests), a.reads, e.CADRuntimeOutcome(), e.ArtifactAcceptanceContext())
			}
			_ = e.Execute(context.Background(), nil)
			if e.CADRuntimeOutcome() != nil {
				t.Fatal("subsequent execution retained outcome")
			}
		})
	}
}

func TestExecutorCADRuntimeOutcome_IsNilBeforeConsumption(t *testing.T) {
	if New(&legacyOnlyAdapter{}).CADRuntimeOutcome() != nil {
		t.Fatal("non-nil")
	}
}
func TestExecutorCADRuntimeOutcome_ResetsAtExecuteStart(t *testing.T) {
	e, _ := consumeTask12(t, nil, nil)
	_ = e.Execute(context.Background(), nil)
	if e.CADRuntimeOutcome() != nil {
		t.Fatal("outcome retained")
	}
}
func TestExecutorCADRuntimeOutcome_ResetsAtExecutePackageStart(t *testing.T) {
	e, _ := consumeTask12(t, nil, nil)
	_ = e.ExecutePackage(context.Background(), nil)
	if e.CADRuntimeOutcome() != nil {
		t.Fatal("outcome retained")
	}
}
func TestExecutorCADRuntimeOutcome_ExposesOnlyLatestCorrelatedAttempt(t *testing.T) {
	e, _ := consumeTask12(t, func(_ *adapter.CADRuntimeOrchestrationRequest, r *cadruntime.FreeCADRuntimeVerifiedRun) {
		outputDir := r.Runtime.ObservationRequest.Manifest.Attempt.Layout.OutputDir
		r.Runtime.ReferenceTraversalPath = filepath.Join(outputDir, cadruntime.FreeCADRuntimeReferenceTraversalFilename)
		r.Runtime.ReferenceTraversalJSON = []byte("traversal-A")
	}, nil)
	root := t.TempDir()
	req, run := task12Request(root, 2), task12Pass(root, 2)
	run.Runtime.ObservationRequest.Manifest.Attempt.Identity.ID = "attempt-13"
	run.Runtime.ReferenceTraversalPath = filepath.Join(run.Runtime.ObservationRequest.Manifest.Attempt.Layout.OutputDir, cadruntime.FreeCADRuntimeReferenceTraversalFilename)
	run.Runtime.ReferenceTraversalJSON = []byte("traversal-B")
	if err := e.consumeCADRuntimeResult(req, run, nil); err != nil {
		t.Fatal(err)
	}
	got := e.CADRuntimeOutcome()
	if got.Attempt != 2 || got.AttemptID != "attempt-13" || string(got.ReferenceTraversalJSON) != "traversal-B" {
		t.Fatal(got)
	}
}
func TestExecutorCADRuntimeConsumption_SequentialPackagesResetState(t *testing.T) {
	TestExecutorCADRuntimeOutcome_ResetsAtExecutePackageStart(t)
}
func TestExecutorCADRuntimeConsumption_ConcurrentExecutorsAreIsolated(t *testing.T) {
	var wg sync.WaitGroup
	outcomes := make(chan *CADRuntimeOutcome, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e, err := consumeTask12(t, nil, nil)
			if err != nil {
				t.Error(err)
				return
			}
			outcomes <- e.CADRuntimeOutcome()
		}()
	}
	wg.Wait()
	close(outcomes)
	for got := range outcomes {
		if got.JobID != "job-12" || got.AttemptID != "attempt-12" {
			t.Fatal(got)
		}
	}
}
func TestExecutorCADRuntimeOutcome_ReturnsDefensiveCopy(t *testing.T) {
	e, _ := consumeTask12(t, func(_ *adapter.CADRuntimeOrchestrationRequest, r *cadruntime.FreeCADRuntimeVerifiedRun) {
		outputDir := r.Runtime.ObservationRequest.Manifest.Attempt.Layout.OutputDir
		r.Runtime.ReferenceTraversalPath = filepath.Join(outputDir, cadruntime.FreeCADRuntimeReferenceTraversalFilename)
		r.Runtime.ReferenceTraversalJSON = []byte("traversal-bytes")
	}, nil)
	first := e.CADRuntimeOutcome()
	first.AttemptID = "mutated"
	first.Artifacts[0].Path = "mutated"
	if len(first.ReferenceTraversalJSON) == 0 {
		t.Fatal("fixture must produce non-empty traversal bytes for this test to be meaningful")
	}
	first.ReferenceTraversalJSON[0] = 'X'
	second := e.CADRuntimeOutcome()
	if second.AttemptID == "mutated" || second.Artifacts[0].Path == "mutated" || second.ReferenceTraversalJSON[0] == 'X' {
		t.Fatal(second)
	}
	// Repeated snapshots are independent of each other, not just of the source.
	third := e.CADRuntimeOutcome()
	second.ReferenceTraversalJSON[0] = 'Y'
	if third.ReferenceTraversalJSON[0] == 'Y' {
		t.Fatal("repeated CADRuntimeOutcome snapshots share backing storage")
	}
}
func TestExecutorCADRuntimeConsumption_IsDeterministic(t *testing.T) {
	a, _ := consumeTask12(t, nil, nil)
	b, _ := consumeTask12(t, nil, nil)
	ao, bo := a.CADRuntimeOutcome(), b.CADRuntimeOutcome()
	ao.ResultPath, bo.ResultPath = "", ""
	for i := range ao.Artifacts {
		ao.Artifacts[i].Path, bo.Artifacts[i].Path = "", ""
	}
	if !reflect.DeepEqual(ao, bo) {
		t.Fatalf("%#v != %#v", ao, bo)
	}
}

func TestCADRuntimeConsumptionError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("cause")
	err := &CADRuntimeConsumptionError{Stage: "runtime_failure", JobID: "j", ProductKey: "p", StepID: "2", Attempt: 3, AttemptID: "a", Classification: "c", Boundary: "b", Category: "cat", Code: "code", NativeStage: "stage", Err: cause}
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "attemptId=a") {
		t.Fatal(err)
	}
	var got *CADRuntimeConsumptionError
	if !errors.As(err, &got) {
		t.Fatal(err)
	}
}
func TestCADRuntimeConsumptionError_Stages(t *testing.T) {
	for _, stage := range []string{CADRuntimeConsumptionStageOutcomeUnavailable, CADRuntimeConsumptionStageOutcomeValidation, CADRuntimeConsumptionStageRuntimeFailure, CADRuntimeConsumptionStageVerificationFailure, CADRuntimeConsumptionStageVerificationResult, CADRuntimeConsumptionStageArtifactProjection} {
		if !strings.Contains((&CADRuntimeConsumptionError{Stage: stage}).Error(), stage) {
			t.Fatal(stage)
		}
	}
}
func TestCADRuntimeConsumptionError_PreservesCauses(t *testing.T) {
	type causeCase struct {
		name string
		root error
		err  error
		as   any
	}
	runtimeRoot := errors.New("runtime root")
	joinedA, joinedB := errors.New("result status"), errors.New("process exit")
	cases := []causeCase{
		{"verification error", verification.ErrParameterMismatch, &cadruntime.FreeCADRuntimeVerificationError{Stage: cadruntime.FreeCADRuntimeVerificationStageVerificationComparison, Err: &verification.VerifyError{Class: verification.FailureClassParameterMismatch}}, new(*cadruntime.FreeCADRuntimeVerificationError)},
		{"runtime error", runtimeRoot, &cadruntime.FreeCADRuntimeRunError{Stage: cadruntime.FreeCADRuntimeRunStageResultLoad, Err: runtimeRoot}, new(*cadruntime.FreeCADRuntimeRunError)},
		{"verify error", verification.ErrMetadataMismatch, &verification.VerifyError{Class: verification.FailureClassMetadataMismatch}, new(*verification.VerifyError)},
		{"parameter sentinel", verification.ErrParameterMismatch, &verification.VerifyError{Class: verification.FailureClassParameterMismatch}, new(*verification.VerifyError)},
		{"required observation sentinel", verification.ErrRequiredObservationMissing, &verification.VerifyError{Class: verification.FailureClassRequiredObservationMissing}, new(*verification.VerifyError)},
		{"metadata sentinel", verification.ErrMetadataMismatch, &verification.VerifyError{Class: verification.FailureClassMetadataMismatch}, new(*verification.VerifyError)},
		{"reference sentinel", verification.ErrReferenceMismatch, &verification.VerifyError{Class: verification.FailureClassReferenceMismatch}, new(*verification.VerifyError)},
		{"contract sentinel", verification.ErrContractInvalid, &verification.VerifyError{Class: verification.FailureClassContractInvalid}, new(*verification.VerifyError)},
		{"observed sentinel", verification.ErrObservedInvalid, &verification.VerifyError{Class: verification.FailureClassObservedInvalid}, new(*verification.VerifyError)},
		{"runtime start", runtimeRoot, &runtimecap.Error{Kind: runtimecap.ErrorStart, Message: "start", Err: runtimeRoot}, new(*runtimecap.Error)},
		{"runtime invocation", runtimeRoot, &runtimecap.Error{Kind: runtimecap.ErrorInvalid, Message: "invoke", Err: runtimeRoot}, new(*runtimecap.Error)},
		{"runtime exit", runtimeRoot, &runtimecap.Error{Kind: runtimecap.ErrorExit, Message: "exit", ExitCode: 2, Err: runtimeRoot}, new(*runtimecap.Error)},
		{"canceled", context.Canceled, context.Canceled, nil},
		{"deadline", context.DeadlineExceeded, context.DeadlineExceeded, nil},
		{"filesystem", os.ErrNotExist, &verification.FileError{Path: "/safe/missing", Err: os.ErrNotExist}, new(*verification.FileError)},
		{"result validation", freecad.ErrFreeCADRuntimeResultValidation, &freecad.FreeCADRuntimeResultValidationError{Message: "invalid"}, new(*freecad.FreeCADRuntimeResultValidationError)},
		{"observed load", os.ErrNotExist, &observed.FileError{Path: "/safe/observed.json", Err: os.ErrNotExist}, new(*observed.FileError)},
		{"observed validation", observed.ErrValidation, &observed.ValidationError{Message: "invalid"}, new(*observed.ValidationError)},
		{"joined consistency", joinedA, errors.Join(joinedA, joinedB), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := &CADRuntimeConsumptionError{Stage: CADRuntimeConsumptionStageRuntimeFailure, JobID: "job", ProductKey: "product", StepID: "2", Attempt: 1, Err: tc.err}
			if !errors.Is(err, tc.root) {
				t.Fatalf("root %v not preserved by %v", tc.root, err)
			}
			if tc.as != nil && !errors.As(err, tc.as) {
				t.Fatalf("typed cause %T not preserved", tc.as)
			}
			if err.Stage != CADRuntimeConsumptionStageRuntimeFailure || err.JobID != "job" || err.Attempt != 1 ||
				err.Error() != (&CADRuntimeConsumptionError{Stage: CADRuntimeConsumptionStageRuntimeFailure, JobID: "job", ProductKey: "product", StepID: "2", Attempt: 1, Err: tc.err}).Error() {
				t.Fatalf("fields/text changed: %#v", err)
			}
		})
	}
}
func TestCADRuntimeConsumptionError_DoesNotLeakRuntimeContent(t *testing.T) {
	sentinels := []string{"SOURCE_SECRET", "MANIFEST_SECRET", "OBSERVED_SECRET", "STDOUT_SECRET", "STDERR_SECRET"}
	err := (&CADRuntimeConsumptionError{Stage: CADRuntimeConsumptionStageOutcomeValidation, Err: errors.New("safe")}).Error()
	for _, sentinel := range sentinels {
		if strings.Contains(err, sentinel) {
			t.Fatal(err)
		}
	}
}

func TestCADRuntimeConsumption_DoesNotReinvokeRuntimeOrVerification(t *testing.T) {
	for _, name := range []string{"cad_runtime_consumption.go", "cad_runtime_orchestration.go", "executor.go"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"InvokeAndVerifyFreeCADRuntime", "InvokeAndValidateFreeCADRuntime", "verification.Verify(", "verification.LoadFile", "observed.LoadFile"} {
			if strings.Contains(string(data), forbidden) {
				t.Fatalf("%s references %s", name, forbidden)
			}
		}
	}
}
func TestCADRuntimeConsumption_DoesNotIntegrateRecordOrPDMState(t *testing.T) {
	data, err := os.ReadFile("cad_runtime_consumption.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"recordmap", "recordemit", "recordpackage", "PDM", "pdm"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("references %s", forbidden)
		}
	}
}
