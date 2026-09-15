package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/adapter"
	"parametron/internal/engine/artifact"
	"parametron/internal/engine/cadruntime"
	"parametron/internal/engine/executor"
	"parametron/internal/engine/handoff"
	"parametron/internal/engine/job"
	"parametron/internal/engine/jobstatus"
	"parametron/internal/engine/verification"
)

// --- Controlled aligned runtime executable (ported from cmd/parametron's
// implementation-stage helper of the same shape) for genuine default-factory
// end-to-end tests. Never touches FreeCAD or the legacy bridge. ---

func apiInstallControlledRuntime(t *testing.T, mode string) string {
	t.Helper()
	pythonPath, err := exec.LookPath("python3")
	if err != nil {
		t.Fatalf("resolve python3 for controlled aligned runtime: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "parametron-freecad")
	script := `#!` + pythonPath + `
import hashlib
import json
import os
import sys

def arg(name):
    i = sys.argv.index(name)
    return sys.argv[i + 1]

working = os.path.abspath(arg("--working-copy"))
manifest_path = arg("--manifest")
result_path = arg("--result")
output_dir = arg("--output-dir")
request_path = arg("--observation-request")
mode = os.environ.get("PARAMETRON_API_RUNTIME_MODE", "success")

with open(manifest_path, "r", encoding="utf-8") as handle:
    manifest = json.load(handle)
with open(request_path, "r", encoding="utf-8") as handle:
    contract = json.load(handle)

if mode == "failure":
    result = {
        "schemaVersion": "1.0", "status": "failed",
        "failure": {"boundary": "freecad", "category": "runtime", "code": "controlled_failure", "message": "intentional controlled failure", "stage": "execute"},
    }
    with open(result_path, "w", encoding="utf-8") as handle:
        json.dump(result, handle, sort_keys=True, separators=(",", ":"))
    sys.exit(17)

artifacts = []
for output in manifest.get("outputs", []):
    relative = output["path"]
    target = os.path.join(working, *relative.split("/"))
    os.makedirs(os.path.dirname(target), exist_ok=True)
    with open(target, "w", encoding="utf-8") as handle:
        handle.write(output["format"] + "\n")
    artifacts.append({"id": output["id"], "format": output["format"], "path": relative})

source = manifest["sourceDocument"]
source_path = source if os.path.isabs(source) else os.path.join(working, source)
with open(source_path, "rb") as handle:
    digest = hashlib.sha256(handle.read()).hexdigest()

expected = contract.get("expected", {})
bindings = {item["id"]: item for item in contract.get("observationContext", {}).get("parameters", [])}
parameters = []
for item in expected.get("parameters", []):
    binding = bindings.get(item["id"], {})
    parameters.append({"id": item["id"], "name": binding.get("name", item.get("name", item["id"])), "value": item["value"], "valueKind": "number"})
metadata = []
for item in expected.get("metadata", []):
    metadata.append({"id": item.get("id", item["key"]), "key": item["key"], "value": digest if item["key"] == "working_copy_sha256" else item["value"], "valueKind": item.get("valueKind", "string")})
references = [{"kind": item["kind"], "name": item["name"]} for item in expected.get("references", [])]
components = [{"id": item["id"], "kind": item["kind"], "name": item["name"]} for item in expected.get("components", [])]
observed = {
    "schemaVersion": "1.0",
    "workingCopy": {"path": working, "sha256": digest},
    "observation": {"parameters": parameters, "metadata": metadata, "references": references, "components": components},
}
with open(os.path.join(output_dir, "prm.observed.json"), "w", encoding="utf-8") as handle:
    json.dump(observed, handle, sort_keys=True, separators=(",", ":"))

result = {"schemaVersion": "1.0", "status": "succeeded", "artifacts": artifacts}
with open(result_path, "w", encoding="utf-8") as handle:
    json.dump(result, handle, sort_keys=True, separators=(",", ":"))
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write controlled aligned runtime: %v", err)
	}
	t.Setenv("PARAMETRON_FREECAD_RUNTIME", path)
	t.Setenv("PARAMETRON_API_RUNTIME_MODE", mode)
	return path
}

// apiAlignedPackage builds a real handoff package whose manifest carries a
// genuine absolute source_model, matching Task 9/10's attempt-materialization
// contract, suitable for a real (controlled-executable) aligned run through
// the default executor factory.
func apiAlignedPackage(t *testing.T, product string, sourceModel string) *handoff.Package {
	t.Helper()
	steps := []planner.Step{
		{Type: planner.StepWriteCSV, Payload: planner.WriteCSVPayload{ProductKey: product, Filename: product + ".csv", Headers: []string{"width"}, Values: []any{42.0}}},
		{Type: planner.StepWriteExportManifest, Payload: planner.WriteExportManifestPayload{
			ProductKey: product, ManifestFilename: planner.ExportManifestFilename, SchemaVersion: planner.ExportManifestSchemaVersion,
			PlanHash: "plan-" + product, Adapter: "freecad", Product: planner.ExportManifestProduct{ID: product},
			Inputs:         planner.ExportManifestInputs{SourceModel: sourceModel},
			SourceDocument: "source/" + filepath.Base(sourceModel),
			Values:         map[string]any{"width": 42.0},
			Outputs:        []planner.ExportManifestOutput{{Type: "step", Filename: "outputs/" + product + ".step", Object: "Body"}},
		}},
		{Type: planner.StepRunCADRuntime, Payload: planner.RunCADRuntimePayload{ProductKey: product, Adapter: "freecad", ManifestFilename: planner.ExportManifestFilename, ResultFilename: "result.json"}},
	}
	j, err := job.New(product, steps)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := handoff.FromJob(j)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func apiAlignedEnvelope(pkg *handoff.Package) jobSubmissionRequest {
	req := packageEnvelopeFromPackage(pkg)
	req.Handoff.CADRuntime = cloneCADRuntimePtr(pkg.CADRuntime)
	// packageEnvelopeFromPackage does not round-trip SourceDocument; carry it
	// through explicitly so the submitted payload's canonical identity
	// (recomputed server-side) matches pkg.JobID exactly.
	req.Handoff.Manifest.SourceDocument = pkg.Manifest.SourceDocument
	for i := range pkg.Steps {
		if pkg.Steps[i].CADRuntime != nil {
			req.Handoff.Steps[i].CADRuntime = cloneCADRuntimePtr(pkg.Steps[i].CADRuntime)
		}
		if pkg.Steps[i].Manifest != nil && req.Handoff.Steps[i].Manifest != nil {
			req.Handoff.Steps[i].Manifest.SourceDocument = pkg.Steps[i].Manifest.SourceDocument
		}
	}
	return req
}

func apiSubmitJob(t *testing.T, handler http.Handler, pkg *handoff.Package) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/job", strings.NewReader(mustSubmissionJSON(t, apiAlignedEnvelope(pkg))))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST /job status=%d body=%q", rec.Code, rec.Body.String())
	}
	var response jobSubmissionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response.JobID
}

func writeAPISourceModel(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("fixture-model-"+name), 0o644); err != nil {
		t.Fatalf("write source model: %v", err)
	}
	return path
}

// ===================== Part J: default API factory =====================

func TestDefaultExecutorFactory_CreatesAlignedRuntimeExecutor(t *testing.T) {
	store := artifact.NewFileSystemStore(t.TempDir())
	factory, err := defaultExecutorFactory(store, store.BaseDir())
	if err != nil {
		t.Fatalf("defaultExecutorFactory: %v", err)
	}
	pkg := task12AlignedPackage(t, "widget")
	exec := factory(pkg)
	concrete, ok := exec.(*executor.Executor)
	if !ok {
		t.Fatalf("executor type = %T, want *executor.Executor", exec)
	}
	value := reflect.ValueOf(concrete).Elem()
	if !value.FieldByName("cadRuntimeConsumption").Bool() {
		t.Fatal("expected the default factory to enable Task 12 aligned-runtime consumption")
	}
	adapterField := value.FieldByName("adapter")
	if adapterField.IsNil() {
		t.Fatal("expected a configured adapter")
	}
	adapterType := adapterField.Elem().Type().String()
	if adapterType != "*cadruntime.FreeCADRuntimeAdapter" {
		t.Fatalf("expected the default factory to wrap the base adapter in *cadruntime.FreeCADRuntimeAdapter, got %s", adapterType)
	}
}

func TestRuntime_DefaultFactoryExecutesAlignedHandoff(t *testing.T) {
	root := t.TempDir()
	store := artifact.NewFileSystemStore(root)
	apiInstallControlledRuntime(t, "success")

	handler := newHandler(HandlerOptions{ArtifactStore: store, RunRoot: root, WorkerCount: 1, PollInterval: time.Millisecond})
	cancel, wait := startHandlerRuntime(t, handler)
	defer func() { cancel(); wait() }()

	sourceModel := writeAPISourceModel(t, "widget.FCStd")
	pkg := apiAlignedPackage(t, "widget", sourceModel)
	jobID := apiSubmitJob(t, handler, pkg)

	status := waitForTerminalJobState(t, handler, jobID)
	if status.State != jobstatus.StateSucceeded {
		t.Fatalf("expected the default-factory aligned job to succeed, got %#v", status)
	}
	artifacts := mustGetJobArtifacts(t, handler, jobID)
	if len(artifacts.Artifacts) == 0 {
		t.Fatal("expected registered artifacts for a successful aligned job")
	}
	_, runReport := mustReadRuntimeReport(t, root, pkg)
	if runReport.Status != "success" {
		t.Fatalf("expected a successful runtime report, got %q", runReport.Status)
	}
}

func TestRuntime_DefaultFactoryRegistersAlignedArtifactsExactlyOnce(t *testing.T) {
	h := newTask12APIHarness(t, nil)
	pkg := task12AlignedPackage(t, "exactly-once")
	jobID := submitTask12APIJob(t, h.handler, pkg)
	if status := waitForTerminalJobState(t, h.handler, jobID); status.State != jobstatus.StateSucceeded {
		t.Fatalf("expected success, got %#v", status)
	}

	first := mustGetJobArtifacts(t, h.handler, jobID)
	second := mustGetJobArtifacts(t, h.handler, jobID)
	if len(first.Artifacts) == 0 {
		t.Fatal("expected registered artifacts")
	}
	if len(first.Artifacts) != len(second.Artifacts) {
		t.Fatalf("expected stable artifact count across repeated reads: %d vs %d", len(first.Artifacts), len(second.Artifacts))
	}
	seen := make(map[string]int)
	for _, a := range first.Artifacts {
		seen[string(a.Type)+"|"+a.Filename+"|"+string(a.Class)]++
	}
	for key, count := range seen {
		if count != 1 {
			t.Fatalf("duplicate artifact registration for %q: count=%d", key, count)
		}
	}
}

func TestRuntime_DefaultFactoryDoesNotExposeArtifactsBeforeCompletion(t *testing.T) {
	h := newTask12APIHarness(t, map[string]string{"blocked-product": "blocked"})
	pkg := task12AlignedPackage(t, "blocked-product")
	jobID := submitTask12APIJob(t, h.handler, pkg)

	_ = waitForJobState(t, h.handler, jobID, jobstatus.StateRunning)
	a := h.waitAdapter(t, jobID)
	select {
	case <-a.started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for orchestration to start")
	}

	if got := mustGetJobArtifacts(t, h.handler, jobID); len(got.Artifacts) != 0 {
		t.Fatalf("expected no artifacts visible before completion, got %d", len(got.Artifacts))
	}
	if status := mustGetJobStatus(t, h.handler, jobID); status.State.IsTerminal() {
		t.Fatal("expected the job to still be non-terminal while blocked")
	}

	close(a.release)
	if status := waitForTerminalJobState(t, h.handler, jobID); status.State != jobstatus.StateSucceeded {
		t.Fatalf("expected eventual success, got %#v", status)
	}
	if got := mustGetJobArtifacts(t, h.handler, jobID); len(got.Artifacts) == 0 {
		t.Fatal("expected artifacts to become visible after completion")
	}
}

func TestRuntime_DefaultFactoryAlignedFailuresExposeNoArtifacts(t *testing.T) {
	cases := []struct {
		name string
		mode string
	}{
		{"runtime-failure", "runtime-failure"},
		{"verification-failure", "verification-failure"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			h := newTask12APIHarness(t, map[string]string{"failing-product": tt.mode})
			pkg := task12AlignedPackage(t, "failing-product")
			jobID := submitTask12APIJob(t, h.handler, pkg)
			status := waitForTerminalJobState(t, h.handler, jobID)
			if status.State != jobstatus.StateFailed {
				t.Fatalf("expected failure, got %#v", status)
			}
			if got := mustGetJobArtifacts(t, h.handler, jobID); len(got.Artifacts) != 0 {
				t.Fatalf("expected no artifacts after a %s failure, got %d", tt.mode, len(got.Artifacts))
			}
		})
	}

	t.Run("cancellation", func(t *testing.T) {
		h := newTask12APIHarness(t, map[string]string{"cancel-product": "blocked"})
		pkg := task12AlignedPackage(t, "cancel-product")
		jobID := submitTask12APIJob(t, h.handler, pkg)
		_ = waitForJobState(t, h.handler, jobID, jobstatus.StateRunning)
		a := h.waitAdapter(t, jobID)
		select {
		case <-a.started:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for orchestration to start")
		}
		h.cancel()
		status := waitForTerminalJobState(t, h.handler, jobID)
		if status.State != jobstatus.StateFailed && status.State != jobstatus.StateCanceled {
			t.Fatalf("expected a terminal failure/cancellation state, got %#v", status)
		}
		if got := mustGetJobArtifacts(t, h.handler, jobID); len(got.Artifacts) != 0 {
			t.Fatalf("expected no artifacts after cancellation, got %d", len(got.Artifacts))
		}
	})

	// malformed_result / artifact_missing / observed_mismatch are exhaustively
	// covered at the Task 10/11 boundary (internal/engine/cadruntime); at the
	// Executor/API boundary they surface as the same runtime-failure or
	// verification-failure consumption classes already exercised above, so a
	// third distinct API-level code path is not exercised by production code.
}

func TestRuntime_ExecutorFactoryOverrideRemainsSupported(t *testing.T) {
	var called int32
	store := artifact.NewFileSystemStore(t.TempDir())
	handler := newHandler(HandlerOptions{
		ArtifactStore: store, RunRoot: store.BaseDir(), WorkerCount: 1, PollInterval: time.Millisecond,
		ExecutorFactory: func(pkg *handoff.Package) packageExecutor {
			atomic.AddInt32(&called, 1)
			return &legacyOnlyAdapterExecutor{}
		},
	})
	cancel, wait := startHandlerRuntime(t, handler)
	defer func() { cancel(); wait() }()

	jobID := submitTask12APIJob(t, handler, task12AlignedPackage(t, "override-product"))
	_ = waitForTerminalJobState(t, handler, jobID)
	if atomic.LoadInt32(&called) != 1 {
		t.Fatalf("expected the overriding factory to be used exactly once, got %d", called)
	}
}

// legacyOnlyAdapterExecutor is a minimal packageExecutor stand-in used only to
// prove HandlerOptions.ExecutorFactory overrides the default factory.
type legacyOnlyAdapterExecutor struct{}

func (e *legacyOnlyAdapterExecutor) ExecutePackage(context.Context, *handoff.Package) error {
	return errors.New("override executor invoked")
}

func TestRuntime_DefaultFactoryCreatesFreshAlignedStatePerJob(t *testing.T) {
	store := artifact.NewFileSystemStore(t.TempDir())
	factory, err := defaultExecutorFactory(store, store.BaseDir())
	if err != nil {
		t.Fatalf("defaultExecutorFactory: %v", err)
	}

	execA := factory(task12AlignedPackage(t, "product-a"))
	execB := factory(task12AlignedPackage(t, "product-b"))

	concreteA, okA := execA.(*executor.Executor)
	concreteB, okB := execB.(*executor.Executor)
	if !okA || !okB {
		t.Fatalf("unexpected executor types: %T, %T", execA, execB)
	}
	if concreteA == concreteB {
		t.Fatal("expected distinct Executor instances per job")
	}
	valueA := reflect.ValueOf(concreteA).Elem().FieldByName("adapter")
	valueB := reflect.ValueOf(concreteB).Elem().FieldByName("adapter")
	if valueA.Elem().Pointer() == valueB.Elem().Pointer() {
		t.Fatal("expected distinct aligned wrapper (and thus distinct provider state) per job")
	}
	dirA := reflect.ValueOf(concreteA).Elem().FieldByName("productDir").String()
	dirB := reflect.ValueOf(concreteB).Elem().FieldByName("productDir").String()
	if dirA == dirB {
		t.Fatal("expected distinct product directories per job")
	}
}

// ===================== Part K: API report and status publication =====================

func TestRuntime_TerminalStatusIsPublishedAfterReport(t *testing.T) {
	h := newTask12APIHarness(t, map[string]string{
		"terminal-success": "", "terminal-runtime-fail": "runtime-failure", "terminal-verify-fail": "verification-failure",
	})
	cases := []struct {
		product   string
		wantState jobstatus.State
	}{
		{"terminal-success", jobstatus.StateSucceeded},
		{"terminal-runtime-fail", jobstatus.StateFailed},
		{"terminal-verify-fail", jobstatus.StateFailed},
	}
	for _, tt := range cases {
		t.Run(tt.product, func(t *testing.T) {
			pkg := task12AlignedPackage(t, tt.product)
			jobID := submitTask12APIJob(t, h.handler, pkg)
			status := waitForTerminalJobState(t, h.handler, jobID)
			if status.State != tt.wantState {
				t.Fatalf("state=%q want=%q", status.State, tt.wantState)
			}
			// The instant terminal status is observable, the report file must
			// already exist and be complete (report is written before the
			// terminal status transition is committed).
			data, runReport := mustReadRuntimeReport(t, h.runRoot, pkg)
			if len(data) == 0 || runReport.SchemaVersion == "" {
				t.Fatalf("expected a complete report immediately at terminal status, got %q", data)
			}
			if runReport.Jobs[0].ProductKey != tt.product {
				t.Fatalf("report product mismatch: %+v", runReport.Jobs)
			}
		})
	}
}

func TestRuntime_ConcurrentAlignedReportsArePublishedBeforeTerminalStatus(t *testing.T) {
	const jobCount = 8
	modes := make(map[string]string, jobCount)
	products := make([]string, jobCount)
	for i := 0; i < jobCount; i++ {
		product := "concurrent-report-" + strconv.Itoa(i)
		products[i] = product
		if i%2 == 0 {
			modes[product] = ""
		} else {
			modes[product] = "verification-failure"
		}
	}
	h := newTask12APIHarness(t, modes)

	var wg sync.WaitGroup
	for _, product := range products {
		wg.Add(1)
		go func(product string) {
			defer wg.Done()
			pkg := task12AlignedPackage(t, product)
			jobID := submitTask12APIJob(t, h.handler, pkg)
			status := waitForTerminalJobState(t, h.handler, jobID)
			if status.State != jobstatus.StateSucceeded && status.State != jobstatus.StateFailed {
				t.Errorf("product %s: unexpected terminal state %q", product, status.State)
				return
			}
			if _, runReport := mustReadRuntimeReport(t, h.runRoot, pkg); runReport.SchemaVersion == "" {
				t.Errorf("product %s: report not complete at terminal-status observation time", product)
			}
		}(product)
	}
	wg.Wait()
}

func TestRuntime_DefaultFactoryPreservesAlignedFailureFields(t *testing.T) {
	h := newTask12APIHarness(t, map[string]string{"structured-failure": "verification-failure"})
	pkg := task12AlignedPackage(t, "structured-failure")
	jobID := submitTask12APIJob(t, h.handler, pkg)
	status := waitForTerminalJobState(t, h.handler, jobID)
	if status.State != jobstatus.StateFailed {
		t.Fatalf("expected failure, got %#v", status)
	}
	if status.Error == nil {
		t.Fatal("expected a structured failure payload")
	}
	f := status.Error
	if f.Classification != string(verification.FailureClassParameterMismatch) {
		t.Fatalf("classification=%q", f.Classification)
	}
	if f.Boundary != "engine" || f.Category != "verification" {
		t.Fatalf("boundary=%q category=%q", f.Boundary, f.Category)
	}
	if f.Code != string(verification.FailureClassParameterMismatch) {
		t.Fatalf("code=%q", f.Code)
	}
	if f.Stage != cadruntime.FreeCADRuntimeVerificationStageVerificationComparison {
		t.Fatalf("stage=%q", f.Stage)
	}
	if f.AttemptID == "" {
		t.Fatal("expected a non-empty attempt ID")
	}
}

// ===================== Part L: retry and cancellation =====================

// retryAdapter fails the first failCount orchestration attempts with an
// eligible (process-invocation) runtime error, then succeeds; or always
// fails with a deterministic verification error; or blocks until canceled.
type retryAdapter struct {
	mu         sync.Mutex
	productDir string
	failCount  int
	attempts   int32
	terminal   bool
	block      chan struct{}
	run        cadruntime.FreeCADRuntimeVerifiedRun
	ok         bool
}

func (a *retryAdapter) Run(_ context.Context, step planner.Step) error {
	var path string
	switch payload := step.Payload.(type) {
	case planner.WriteCSVPayload:
		path = filepath.Join(a.productDir, payload.Filename)
	case planner.WriteExportManifestPayload:
		path = filepath.Join(a.productDir, payload.ManifestFilename)
	default:
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte("retry\n"), 0o644)
}

func (a *retryAdapter) OrchestrateCADRuntime(ctx context.Context, req adapter.CADRuntimeOrchestrationRequest) error {
	n := atomic.AddInt32(&a.attempts, 1)
	working := filepath.Join(a.productDir, "_working", "attempt-"+strconv.Itoa(int(n)))
	output := filepath.Join(working, "outputs")
	resultPath := filepath.Join(working, req.CADRuntime.ResultFilename)
	run := task12APIRun(req, working, resultPath, output)

	if a.block != nil {
		select {
		case <-a.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if a.terminal {
		class := verification.FailureClassParameterMismatch
		run.Verification = &verification.Result{Status: verification.StatusFail, Failure: class}
		a.mu.Lock()
		a.run, a.ok = run, true
		a.mu.Unlock()
		return &cadruntime.FreeCADRuntimeVerificationError{
			Stage:     cadruntime.FreeCADRuntimeVerificationStageVerificationComparison,
			AttemptID: run.Runtime.ObservationRequest.Manifest.Attempt.Identity.ID, FailureClass: class,
			Err: &verification.VerifyError{Class: class, Message: "deterministic mismatch"},
		}
	}

	if int(n) <= a.failCount {
		run.Verification = nil
		run.Runtime.Result = nil
		run.Runtime.Artifacts = nil
		runErr := &cadruntime.FreeCADRuntimeRunError{
			Stage:     cadruntime.FreeCADRuntimeRunStageProcessInvocation,
			AttemptID: run.Runtime.ObservationRequest.Manifest.Attempt.Identity.ID,
			Err:       errors.New("transient process failure"),
		}
		a.mu.Lock()
		a.run, a.ok = run, true
		a.mu.Unlock()
		return &cadruntime.FreeCADRuntimeVerificationError{
			Stage:     cadruntime.FreeCADRuntimeVerificationStageRuntimeExecution,
			AttemptID: runErr.AttemptID, Err: runErr,
		}
	}

	if err := os.MkdirAll(output, 0o755); err != nil {
		return err
	}
	for _, item := range run.Runtime.Artifacts {
		if err := os.MkdirAll(filepath.Dir(item.Path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(item.Path, []byte(item.ID+"\n"), 0o644); err != nil {
			return err
		}
	}
	if err := os.WriteFile(resultPath, []byte(`{"schemaVersion":"1.0","status":"succeeded","artifacts":[]}`), 0o644); err != nil {
		return err
	}
	_ = os.WriteFile(filepath.Join(working, "prm.observed.json"), []byte("{}"), 0o644)
	a.mu.Lock()
	a.run, a.ok = run, true
	a.mu.Unlock()
	return nil
}

func (a *retryAdapter) CADRuntimeVerifiedRun() (cadruntime.FreeCADRuntimeVerifiedRun, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.run, a.ok
}

func newRetryHarness(t *testing.T, a *retryAdapter) http.Handler {
	t.Helper()
	root := t.TempDir()
	store := artifact.NewFileSystemStore(root)
	a.productDir = runtimeProductDir(root, task12AlignedPackage(t, "retry-product"))
	handler := newHandler(HandlerOptions{
		ArtifactStore: store, RunRoot: root, WorkerCount: 1, PollInterval: time.Millisecond,
		ExecutorFactory: func(pkg *handoff.Package) packageExecutor {
			return executor.NewWithCADRuntimeConsumption(a, task12APIResolver{}, a.productDir)
		},
	})
	cancel, wait := startHandlerRuntime(t, handler)
	t.Cleanup(func() { cancel(); wait() })
	return handler
}

func TestNormalAlignedRuntime_RetriesEligibleFailures(t *testing.T) {
	a := &retryAdapter{failCount: 2}
	handler := newRetryHarness(t, a)
	pkg := task12AlignedPackage(t, "retry-product")
	jobID := apiSubmitJob(t, handler, pkg)

	status := waitForTerminalJobState(t, handler, jobID)
	if status.State != jobstatus.StateSucceeded {
		t.Fatalf("expected eventual success after eligible retries, got %#v", status)
	}
	if got := atomic.LoadInt32(&a.attempts); got != 3 {
		t.Fatalf("expected exactly 3 attempts (2 failures + 1 success), got %d", got)
	}
}

func TestNormalAlignedRuntime_DoesNotRetryDeterministicFailures(t *testing.T) {
	a := &retryAdapter{terminal: true}
	handler := newRetryHarness(t, a)
	pkg := task12AlignedPackage(t, "retry-product")
	jobID := apiSubmitJob(t, handler, pkg)

	status := waitForTerminalJobState(t, handler, jobID)
	if status.State != jobstatus.StateFailed {
		t.Fatalf("expected failure, got %#v", status)
	}
	if got := atomic.LoadInt32(&a.attempts); got != 1 {
		t.Fatalf("expected exactly one attempt for a deterministic verification failure, got %d", got)
	}
}

func TestNormalAlignedRuntime_DoesNotRetryCancellation(t *testing.T) {
	// The API job runner's background context has no cancellation hook
	// exposed to tests (only process shutdown), so this exercises the same
	// normal aligned-runtime path (Executor.ExecutePackage, the shared
	// engine the API/CLI/scheduler all drive) with a genuinely cancelable
	// context.
	root := t.TempDir()
	productDir := filepath.Join(root, "widget")
	if err := os.MkdirAll(productDir, 0o755); err != nil {
		t.Fatal(err)
	}
	a := &retryAdapter{block: make(chan struct{}), productDir: productDir}
	exec := executor.NewWithCADRuntimeConsumption(a, task12APIResolver{}, productDir)
	pkg := task12AlignedPackage(t, "widget")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- exec.ExecutePackage(ctx, pkg) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&a.attempts) == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if atomic.LoadInt32(&a.attempts) == 0 {
		t.Fatal("timed out waiting for the first attempt to start")
	}
	cancel()

	var err error
	select {
	case err = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for cancellation to propagate")
	}
	if err == nil {
		t.Fatal("expected a cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		var execErr *executor.ExecutionError
		if !errors.As(err, &execErr) || !execErr.Canceled {
			t.Fatalf("expected a cancellation error, got: %v", err)
		}
	}
	if got := atomic.LoadInt32(&a.attempts); got != 1 {
		t.Fatalf("expected exactly one attempt after cancellation (no retry), got %d", got)
	}
}

// ===================== Part M: determinism =====================

func TestTask13_NormalAlignedExecutionIsDeterministic(t *testing.T) {
	run := func() (jobstatus.Status, []byte) {
		h := newTask12APIHarness(t, nil)
		pkg := task12AlignedPackage(t, "determinism-product")
		jobID := submitTask12APIJob(t, h.handler, pkg)
		status := waitForTerminalJobState(t, h.handler, jobID)
		data, _ := mustReadRuntimeReport(t, h.runRoot, pkg)
		return status, data
	}
	status1, report1 := run()
	status2, report2 := run()

	if status1.State != status2.State || status1.ProductKey != status2.ProductKey {
		t.Fatalf("status not stable across equivalent runs: %#v vs %#v", status1, status2)
	}
	var normalized1, normalized2 map[string]any
	if err := json.Unmarshal(report1, &normalized1); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(report2, &normalized2); err != nil {
		t.Fatal(err)
	}
	stripGeneratedTimingFields(normalized1)
	stripGeneratedTimingFields(normalized2)
	delete(normalized1, "planHash")
	delete(normalized2, "planHash")
	b1, _ := json.Marshal(normalized1)
	b2, _ := json.Marshal(normalized2)
	if string(b1) != string(b2) {
		t.Fatalf("normalized report content differs across equivalent runs:\n%s\nvs\n%s", b1, b2)
	}
}

// stripGeneratedTimingFields recursively removes non-logical, run-to-run
// varying timing keys (timestamps and measured durations) from a decoded
// JSON document, in place.
func stripGeneratedTimingFields(value any) {
	switch v := value.(type) {
	case map[string]any:
		for _, key := range []string{"startedAt", "endedAt", "durationMs", "createdAt", "updatedAt"} {
			delete(v, key)
		}
		for _, nested := range v {
			stripGeneratedTimingFields(nested)
		}
	case []any:
		for _, item := range v {
			stripGeneratedTimingFields(item)
		}
	}
}

// ===================== Part N: protected boundaries =====================

func TestTask13_OuterLayersDoNotInvokeRuntimeVerificationDirectly(t *testing.T) {
	sources := []string{
		"runtime.go",
		filepath.Join("..", "executor", "cad_runtime_orchestration.go"),
		filepath.Join("..", "scheduler", "scheduler.go"),
		filepath.Join("..", "..", "..", "cmd", "parametron", "cli_helpers.go"),
	}
	forbidden := []string{"InvokeAndVerifyFreeCADRuntime(", "InvokeAndValidateFreeCADRuntime(", "verification.Verify("}
	for _, src := range sources {
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("failed to read %q: %v", src, err)
		}
		text := string(data)
		for _, token := range forbidden {
			if strings.Contains(text, token) {
				t.Fatalf("%s must not call %s directly; only the cadruntime wrapper may", src, token)
			}
		}
	}
}

func TestTask13_AlignedNormalPathDoesNotInvokeLegacyBridge(t *testing.T) {
	data, err := os.ReadFile("runtime.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"parametron/internal/engine/bridge"`) {
		t.Fatal("the API's default aligned dispatch path must not import the legacy observation bridge")
	}
}

func TestTask13_DoesNotIntroduceRecordOrPDMCoupling(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	forbidden := []string{
		`"parametron/internal/engine/recordcontract"`,
		`"parametron/internal/engine/recordmap"`,
		`"parametron/internal/engine/recordemit"`,
		`"parametron/internal/engine/recordpackage"`,
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, token := range forbidden {
			if strings.Contains(text, token) {
				t.Fatalf("%s must not import %s; Task 13 does not introduce record/PDM coupling", entry.Name(), token)
			}
		}
	}
}
