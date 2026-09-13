package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/adapter"
	"parametron/internal/engine/adapter/freecad"
	"parametron/internal/engine/artifact"
	"parametron/internal/engine/cadruntime"
	"parametron/internal/engine/executor"
	"parametron/internal/engine/handoff"
	"parametron/internal/engine/job"
	"parametron/internal/engine/jobstatus"
	"parametron/internal/engine/runtimecap"
	"parametron/internal/engine/verification"
)

type task12APIResolver struct{}

func (task12APIResolver) Resolve(id string) (runtimecap.ExecutableSelection, error) {
	return runtimecap.ExecutableSelection{Adapter: id, Command: "/opt/runtime", Path: "/opt/runtime", Source: runtimecap.ExecutableSelectionSourceConfigured}, nil
}

type task12APIAdapter struct {
	mu         sync.Mutex
	productDir string
	requests   []adapter.CADRuntimeOrchestrationRequest
	reads      int
	run        cadruntime.FreeCADRuntimeVerifiedRun
	ok         bool
	err        error
	started    chan struct{}
	release    chan struct{}
	mode       string
}

func (a *task12APIAdapter) Run(_ context.Context, step planner.Step) error {
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
	return os.WriteFile(path, []byte("task12\n"), 0o644)
}

func (a *task12APIAdapter) OrchestrateCADRuntime(ctx context.Context, req adapter.CADRuntimeOrchestrationRequest) error {
	a.mu.Lock()
	a.requests = append(a.requests, req)
	working := filepath.Join(a.productDir, "_working", "attempt-"+strconv.Itoa(req.Attempt))
	output := filepath.Join(working, "outputs")
	resultPath := filepath.Join(working, req.CADRuntime.ResultFilename)
	run := task12APIRun(req, working, resultPath, output)
	mode, started, release := a.mode, a.started, a.release
	a.run, a.ok, a.err = run, true, nil
	if mode == "verification-failure" {
		class := verification.FailureClassParameterMismatch
		a.run.Verification = &verification.Result{Status: verification.StatusFail, Failure: class, Message: "width mismatch"}
		a.err = &cadruntime.FreeCADRuntimeVerificationError{Stage: cadruntime.FreeCADRuntimeVerificationStageVerificationComparison, AttemptID: run.Runtime.ObservationRequest.Manifest.Attempt.Identity.ID, FailureClass: class, Err: &verification.VerifyError{Class: class, Message: "width mismatch"}}
	}
	if mode == "runtime-failure" {
		stage := "execute"
		a.run.Verification, a.run.Runtime.Artifacts = nil, nil
		a.run.Runtime.Result = &freecad.FreeCADRuntimeResult{Status: freecad.FreeCADRuntimeResultStatusFailed, Failure: &freecad.FreeCADRuntimeResultFailure{Boundary: "freecad", Category: "runtime", Code: "execute_failed", Message: "runtime failed", Stage: &stage}}
		runErr := &cadruntime.FreeCADRuntimeRunError{Stage: cadruntime.FreeCADRuntimeRunStageResultLoad, AttemptID: run.Runtime.ObservationRequest.Manifest.Attempt.Identity.ID, Err: errors.New("runtime failed")}
		a.err = &cadruntime.FreeCADRuntimeVerificationError{Stage: cadruntime.FreeCADRuntimeVerificationStageRuntimeExecution, AttemptID: runErr.AttemptID, Err: runErr}
	}
	a.mu.Unlock()
	if started != nil {
		select {
		case started <- struct{}{}:
		default:
		}
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if mode == "" {
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
		_ = os.WriteFile(filepath.Join(output, "unrelated.bin"), []byte("ignore"), 0o644)
		_ = os.WriteFile(filepath.Join(working, "parametron.observed.json"), []byte("{}"), 0o644)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.err
}

func (a *task12APIAdapter) CADRuntimeVerifiedRun() (cadruntime.FreeCADRuntimeVerifiedRun, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reads++
	return a.run, a.ok
}

func task12APIRun(req adapter.CADRuntimeOrchestrationRequest, working, resultPath, output string) cadruntime.FreeCADRuntimeVerifiedRun {
	identity := freecad.FreeCADRuntimeAttemptIdentity{ID: "attempt-" + req.JobID + "-" + strconv.Itoa(req.Attempt), JobID: req.JobID, ProductKey: req.ProductKey, StepID: req.StepID, Attempt: req.Attempt, Adapter: req.CADRuntime.Adapter, PlanHash: req.Manifest.PlanHash}
	return cadruntime.FreeCADRuntimeVerifiedRun{
		Runtime: cadruntime.FreeCADRuntimeRun{
			ObservationRequest: cadruntime.FreeCADRuntimeObservationRequestMaterialization{Manifest: freecad.FreeCADRuntimeManifestMaterialization{Attempt: freecad.FreeCADRuntimeAttempt{Identity: identity, Layout: freecad.FreeCADRuntimeWorkingCopyLayout{WorkingCopyDir: working, ResultPath: resultPath, OutputDir: output}}}},
			ExecutionRequest:   freecad.FreeCADRuntimeExecutionRequest{ResultPath: resultPath, OutputDir: output},
			Result:             &freecad.FreeCADRuntimeResult{Status: freecad.FreeCADRuntimeResultStatusSucceeded},
			Artifacts: []cadruntime.FreeCADRuntimeValidatedArtifact{
				{ID: "step", Format: "step", RelativePath: "outputs/nested/model.step", Path: filepath.Join(output, "nested", "model.step")},
				{ID: "csv", Format: "csv", RelativePath: "outputs/table.csv", Path: filepath.Join(output, "table.csv")},
				{ID: "pdf", Format: "pdf", RelativePath: "outputs/drawing.pdf", Path: filepath.Join(output, "drawing.pdf")},
			},
		},
		Verification: &verification.Result{Status: verification.StatusPass},
	}
}

func task12AlignedPackage(t *testing.T, product string) *handoff.Package {
	t.Helper()
	steps := []planner.Step{
		{Type: planner.StepWriteCSV, Payload: planner.WriteCSVPayload{ProductKey: product, Filename: product + ".csv", Headers: []string{"width"}, Values: []any{42}}},
		{Type: planner.StepWriteExportManifest, Payload: planner.WriteExportManifestPayload{ProductKey: product, ManifestFilename: planner.ExportManifestFilename, SchemaVersion: planner.ExportManifestSchemaVersion, PlanHash: "plan-" + product, Adapter: "freecad", Product: planner.ExportManifestProduct{ID: product}, Values: map[string]any{"width": 42}, Outputs: []planner.ExportManifestOutput{{Type: "step", Filename: product + ".step", Object: "Body"}}}},
		{Type: planner.StepRunCADRuntime, Payload: planner.RunCADRuntimePayload{ProductKey: product, Adapter: "freecad", ManifestFilename: planner.ExportManifestFilename, ResultFilename: "aligned-result.json"}},
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

func task12Envelope(pkg *handoff.Package) jobSubmissionRequest {
	req := packageEnvelopeFromPackage(pkg)
	req.Handoff.CADRuntime = cloneCADRuntimePtr(pkg.CADRuntime)
	for i := range pkg.Steps {
		if pkg.Steps[i].CADRuntime != nil {
			req.Handoff.Steps[i].CADRuntime = cloneCADRuntimePtr(pkg.Steps[i].CADRuntime)
		}
	}
	return req
}

type task12APIHarness struct {
	handler   http.Handler
	runRoot   string
	store     *artifact.FileSystemStore
	mu        sync.RWMutex
	adapters  map[string]*task12APIAdapter
	published chan struct{}
	cancel    context.CancelFunc
	wait      func()
}

func newTask12APIHarness(t *testing.T, modes map[string]string) *task12APIHarness {
	t.Helper()
	root := t.TempDir()
	store := artifact.NewFileSystemStore(root)
	h := &task12APIHarness{runRoot: root, store: store, adapters: make(map[string]*task12APIAdapter), published: make(chan struct{}, 1)}
	handler := newHandler(HandlerOptions{
		ArtifactStore: store, RunRoot: root, WorkerCount: 2,
		ExecutorFactory: func(pkg *handoff.Package) packageExecutor {
			mode := modes[pkg.ProductKey]
			a := &task12APIAdapter{productDir: runtimeProductDir(root, pkg), mode: mode}
			if mode == "blocked" {
				a.mode = ""
				a.started = make(chan struct{}, 1)
				a.release = make(chan struct{})
			}
			h.mu.Lock()
			h.adapters[pkg.JobID] = a
			h.mu.Unlock()
			select {
			case h.published <- struct{}{}:
			default:
			}
			return executor.NewWithCADRuntimeConsumption(a, task12APIResolver{}, a.productDir)
		},
	})
	h.handler = handler
	h.cancel, h.wait = startHandlerRuntime(t, handler)
	t.Cleanup(func() { h.cancel(); h.wait() })
	return h
}

func (h *task12APIHarness) adapter(jobID string) *task12APIAdapter {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.adapters[jobID]
}

func (h *task12APIHarness) waitAdapter(t *testing.T, jobID string) *task12APIAdapter {
	t.Helper()
	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()
	for {
		if a := h.adapter(jobID); a != nil {
			return a
		}
		select {
		case <-h.published:
		case <-timeout.C:
			t.Fatalf("adapter for job %q was not published", jobID)
		}
	}
}

func (a *task12APIAdapter) counts() (int, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.requests), a.reads
}

func submitTask12APIJob(t *testing.T, handler http.Handler, pkg *handoff.Package) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/job", strings.NewReader(mustSubmissionJSON(t, task12Envelope(pkg))))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST status=%d body=%q", rec.Code, rec.Body.String())
	}
	var response jobSubmissionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response.JobID
}

func task12APIPackage() *handoff.Package {
	cad := planner.RunCADRuntimePayload{ProductKey: "widget", Adapter: "freecad", ManifestFilename: planner.ExportManifestFilename, ResultFilename: "aligned-result.json"}
	return &handoff.Package{
		JobID: "job-12", ProductKey: "widget", CADRuntime: &cad,
		Steps: []handoff.StepSnapshot{{Type: planner.StepRunCADRuntime, CADRuntime: &cad}},
	}
}

func task12APIOutcome() *executor.CADRuntimeOutcome {
	return &executor.CADRuntimeOutcome{
		AttemptID: "attempt-12", JobID: "job-12", ProductKey: "widget", StepID: "0", Attempt: 1,
		Adapter: "freecad", ResultPath: "/tmp/attempt/aligned-result.json",
		Verification: artifact.VerificationOutcomePassed,
		Artifacts: []executor.CADRuntimeArtifactOutcome{
			{RuntimeID: "step", Format: "step", RelativePath: "outputs/nested/widget.step", Path: "/tmp/attempt/outputs/nested/widget.step"},
			{RuntimeID: "csv", Format: "csv", RelativePath: "outputs/table.csv", Path: "/tmp/attempt/outputs/table.csv"},
			{RuntimeID: "pdf", Format: "pdf", RelativePath: "outputs/drawing.pdf", Path: "/tmp/attempt/outputs/drawing.pdf"},
		},
	}
}

func TestJobSubmission_AcceptsAlignedCADRuntimePayload(t *testing.T) {
	pkg := task12APIPackage()
	payload := &handoffPackagePayload{JobID: pkg.JobID, ProductKey: pkg.ProductKey, CADRuntime: cloneCADRuntimePtr(pkg.CADRuntime)}
	got := payload.toPackage()
	if !reflect.DeepEqual(got.CADRuntime, pkg.CADRuntime) {
		t.Fatalf("%#v", got.CADRuntime)
	}
}
func TestJobSubmission_AcceptsStepCADRuntimePayload(t *testing.T) {
	pkg := task12APIPackage()
	payload := &handoffPackagePayload{CADRuntime: cloneCADRuntimePtr(pkg.CADRuntime), Steps: []stepSnapshotPayload{{Type: planner.StepRunCADRuntime, CADRuntime: cloneCADRuntimePtr(pkg.CADRuntime)}}}
	got := payload.toPackage()
	if len(got.Steps) != 1 || !reflect.DeepEqual(got.Steps[0].CADRuntime, got.CADRuntime) {
		t.Fatalf("%#v", got.Steps)
	}
}
func TestJobSubmission_PreservesCADRuntimePayloadThroughClone(t *testing.T) {
	pkg := clonePackage(task12APIPackage())
	pkg.CADRuntime.Adapter = "mutated"
	if task12APIPackage().CADRuntime.Adapter != "freecad" {
		t.Fatal("alias")
	}
}
func TestJobSubmission_RejectsRunnerField(t *testing.T) {
	pkg := task12AlignedPackage(t, "no-runner")
	cases := map[string]string{
		"top level":  `{"handoff":{"runner":{"script":"runner.py"}}}`,
		"step level": `{"handoff":{"steps":[{"type":"RunPython","runner":{"script":"runner.py"}}]}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			store := newInMemorySubmissionStore()
			queue := newInMemoryJobQueue().(*inMemoryJobQueue)
			created := 0
			handler := newHandler(HandlerOptions{SubmissionStore: store, JobQueue: queue, ExecutorFactory: func(*handoff.Package) packageExecutor { created++; return nil }})
			req := httptest.NewRequest(http.MethodPost, "/job", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest || queue.Len() != 0 || created != 0 {
				t.Fatalf("status=%d body=%q queue=%d created=%d", rec.Code, rec.Body.String(), queue.Len(), created)
			}
			if _, ok, err := store.GetStatus(pkg.JobID); err != nil || ok {
				t.Fatalf("persisted rejected job: ok=%v err=%v", ok, err)
			}
		})
	}
}
func TestJobSubmission_RejectsUnknownCADRuntimeFields(t *testing.T) {
	var req jobSubmissionRequest
	dec := json.NewDecoder(strings.NewReader(`{"handoff":{"cadRuntime":{"adapter":"freecad","unknown":true}}}`))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err == nil {
		t.Fatal("unknown field accepted")
	}
}
func TestJobSubmission_RejectsOperationalCADRuntimeState(t *testing.T) {
	for _, field := range []string{"executablePath", "command", "attemptId", "result", "observed", "verification", "acceptance", "providerState"} {
		t.Run(field, func(t *testing.T) {
			var req jobSubmissionRequest
			dec := json.NewDecoder(strings.NewReader(`{"handoff":{"cadRuntime":{"` + field + `":"forbidden"}}}`))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&req); err == nil {
				t.Fatal("operational field accepted")
			}
		})
	}
}
func TestJobSubmission_CADRuntimePayloadPreservesCanonicalJobIdentity(t *testing.T) {
	pkg := task12APIPackage()
	got := (&handoffPackagePayload{JobID: pkg.JobID, ProductKey: pkg.ProductKey, CADRuntime: cloneCADRuntimePtr(pkg.CADRuntime)}).toPackage()
	if got.JobID != pkg.JobID || got.ProductKey != pkg.ProductKey {
		t.Fatalf("%#v", got)
	}
}

func TestRuntime_AlignedCADRuntimeSuccess(t *testing.T) {
	h := newTask12APIHarness(t, nil)
	pkg := task12AlignedPackage(t, "aligned-success")
	jobID := submitTask12APIJob(t, h.handler, pkg)
	status := waitForTerminalJobState(t, h.handler, jobID)
	if status.State != jobstatus.StateSucceeded || status.Error != nil {
		t.Fatalf("status=%#v", status)
	}
	a := h.adapter(jobID)
	if a == nil {
		t.Fatal("missing adapter")
	}
	requests, reads := a.counts()
	if requests != 1 || reads != 1 {
		t.Fatalf("adapter=%#v", a)
	}
	response := mustGetJobArtifacts(t, h.handler, jobID)
	if len(response.Artifacts) != 9 {
		t.Fatalf("artifacts=%#v", response.Artifacts)
	}
	data, runReport := mustReadRuntimeReport(t, h.runRoot, pkg)
	if runReport.Status != "success" || len(runReport.Artifacts) != 9 || len(data) == 0 {
		t.Fatalf("report=%#v", runReport)
	}
	for _, item := range response.Artifacts {
		for _, forbidden := range []string{"unrelated", "observed", "verification"} {
			if strings.Contains(item.Filename, forbidden) {
				t.Fatalf("raw/unrelated artifact registered: %#v", item)
			}
		}
	}
}
func TestRuntime_AlignedCADRuntimeRegistersExpectedArtifacts(t *testing.T) {
	records, err := alignedCompletionArtifactRecords(task12APIPackage(), task12APIOutcome())
	if err != nil {
		t.Fatal(err)
	}
	if records[0].meta.Type != artifact.ArtifactTypeJSON || records[0].meta.Class != artifact.ArtifactClassExecutionOutput {
		t.Fatal(records[0])
	}
	for i := 1; i < len(records); i += 2 {
		if records[i].meta.Class != artifact.ArtifactClassExecutionOutput || records[i+1].meta.Class != artifact.ArtifactClassVerified {
			t.Fatal(records[i:])
		}
	}
}
func TestRuntime_AlignedCADRuntimeMapsArtifactFormats(t *testing.T) {
	records, _ := alignedCompletionArtifactRecords(task12APIPackage(), task12APIOutcome())
	want := []artifact.ArtifactType{artifact.ArtifactTypeJSON, artifact.ArtifactTypeSTEP, artifact.ArtifactTypeSTEP, artifact.ArtifactTypeCSV, artifact.ArtifactTypeCSV, artifact.ArtifactTypePDF, artifact.ArtifactTypePDF}
	for i := range want {
		if records[i].meta.Type != want[i] {
			t.Fatalf("%d: %q", i, records[i].meta.Type)
		}
	}
}
func TestRuntime_AlignedCADRuntimePreservesNestedArtifactFilenames(t *testing.T) {
	records, _ := alignedCompletionArtifactRecords(task12APIPackage(), task12APIOutcome())
	if records[1].meta.Filename != "outputs/nested/widget.step" {
		t.Fatal(records[1])
	}
}
func TestRuntime_AlignedCADRuntimeUsesDeclaredResultFilename(t *testing.T) {
	records, err := alignedCompletionArtifactRecords(task12APIPackage(), task12APIOutcome())
	if err != nil || records[0].meta.Filename != "aligned-result.json" {
		t.Fatalf("%#v %v", records, err)
	}
}
func TestRuntime_AlignedCADRuntimeDoesNotRegisterUnrelatedFiles(t *testing.T) {
	records, _ := alignedCompletionArtifactRecords(task12APIPackage(), task12APIOutcome())
	for _, r := range records {
		if strings.Contains(r.meta.Filename, "unrelated") {
			t.Fatal(r)
		}
	}
}
func TestRuntime_AlignedCADRuntimeDoesNotRegisterRawEvidence(t *testing.T) {
	records, _ := alignedCompletionArtifactRecords(task12APIPackage(), task12APIOutcome())
	for _, r := range records {
		for _, forbidden := range []string{"manifest", "verification", "observed", "source"} {
			if strings.Contains(r.meta.Filename, forbidden) {
				t.Fatal(r)
			}
		}
	}
}
func TestJobArtifacts_AlignedCADRuntimeSuccess(t *testing.T) {
	h := newTask12APIHarness(t, nil)
	pkg := task12AlignedPackage(t, "artifact-success")
	jobID := submitTask12APIJob(t, h.handler, pkg)
	if status := waitForTerminalJobState(t, h.handler, jobID); status.State != jobstatus.StateSucceeded {
		t.Fatal(status)
	}
	got := mustGetJobArtifacts(t, h.handler, jobID)
	if len(got.Artifacts) != 9 {
		t.Fatalf("%#v", got.Artifacts)
	}
	for _, item := range got.Artifacts {
		if item.JobID != jobID || item.ProductID != pkg.ProductKey {
			t.Fatal(item)
		}
	}
}
func TestJobArtifacts_AlignedCADRuntimeNotVisibleBeforeCompletion(t *testing.T) {
	h := newTask12APIHarness(t, map[string]string{"blocked": "blocked"})
	pkg := task12AlignedPackage(t, "blocked")
	jobID := submitTask12APIJob(t, h.handler, pkg)
	_ = waitForJobState(t, h.handler, jobID, jobstatus.StateRunning)
	a := h.adapter(jobID)
	select {
	case <-a.started:
	case <-time.After(2 * time.Second):
		t.Fatal("orchestrator did not block")
	}
	if got := mustGetJobArtifacts(t, h.handler, jobID); len(got.Artifacts) != 0 {
		t.Fatal(got.Artifacts)
	}
	items, err := h.store.List()
	if err != nil || len(items) != 0 {
		t.Fatalf("store=%#v err=%v", items, err)
	}
	close(a.release)
	if status := waitForTerminalJobState(t, h.handler, jobID); status.State != jobstatus.StateSucceeded {
		t.Fatal(status)
	}
	if got := mustGetJobArtifacts(t, h.handler, jobID); len(got.Artifacts) != 9 {
		t.Fatal(got.Artifacts)
	}
}
func TestJobArtifacts_AlignedVerificationFailureIsEmpty(t *testing.T) {
	h := newTask12APIHarness(t, map[string]string{"verify-fail": "verification-failure"})
	pkg := task12AlignedPackage(t, "verify-fail")
	jobID := submitTask12APIJob(t, h.handler, pkg)
	status := waitForTerminalJobState(t, h.handler, jobID)
	if status.State != jobstatus.StateFailed || status.Error == nil || status.Error.Classification != string(verification.FailureClassParameterMismatch) {
		t.Fatal(status)
	}
	if got := mustGetJobArtifacts(t, h.handler, jobID); len(got.Artifacts) != 0 {
		t.Fatal(got.Artifacts)
	}
	items, _ := h.store.List()
	if len(items) != 0 {
		t.Fatal(items)
	}
}
func TestJobArtifacts_AlignedRuntimeFailureIsEmpty(t *testing.T) {
	h := newTask12APIHarness(t, map[string]string{"runtime-fail": "runtime-failure"})
	pkg := task12AlignedPackage(t, "runtime-fail")
	jobID := submitTask12APIJob(t, h.handler, pkg)
	status := waitForTerminalJobState(t, h.handler, jobID)
	if status.State != jobstatus.StateFailed || status.Error == nil || status.Error.Code != "execute_failed" {
		t.Fatal(status)
	}
	if got := mustGetJobArtifacts(t, h.handler, jobID); len(got.Artifacts) != 0 {
		t.Fatal(got.Artifacts)
	}
	items, _ := h.store.List()
	if len(items) != 0 {
		t.Fatal(items)
	}
}
func TestJobArtifacts_AlignedCADRuntimeOrderingIsDeterministic(t *testing.T) {
	a, _ := alignedCompletionArtifactRecords(task12APIPackage(), task12APIOutcome())
	b, _ := alignedCompletionArtifactRecords(task12APIPackage(), task12APIOutcome())
	if !reflect.DeepEqual(a, b) {
		t.Fatal("non-deterministic")
	}
}
func TestRuntime_AlignedCADRuntimeRegistrationIsAtomic(t *testing.T) {
	root := t.TempDir()
	base := artifact.NewFileSystemStore(root)
	unrelatedPath := filepath.Join(root, "unrelated.csv")
	if err := os.WriteFile(unrelatedPath, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	unrelated, err := base.RecordExisting(context.Background(), unrelatedPath, artifact.Artifact{JobID: "unrelated-job", ProductID: "other", StepID: "0", Type: artifact.ArtifactTypeCSV, Filename: "unrelated.csv"})
	if err != nil {
		t.Fatal(err)
	}
	store := newControlledCompletionArtifactStore(base)
	store.FailOnCall(4, errors.New("atomic registration failure"))
	adapters := make(map[string]*task12APIAdapter)
	handler := newHandler(HandlerOptions{ArtifactStore: store, RunRoot: root, ExecutorFactory: func(pkg *handoff.Package) packageExecutor {
		a := &task12APIAdapter{productDir: runtimeProductDir(root, pkg)}
		adapters[pkg.JobID] = a
		return executor.NewWithCADRuntimeConsumption(a, task12APIResolver{}, a.productDir)
	}})
	cancel, wait := startHandlerRuntime(t, handler)
	defer func() { cancel(); wait() }()
	pkg := task12AlignedPackage(t, "atomic-fail")
	jobID := submitTask12APIJob(t, handler, pkg)
	status := waitForTerminalJobState(t, handler, jobID)
	if status.State != jobstatus.StateFailed || status.Error == nil || !strings.Contains(status.Error.Message, "atomic registration failure") {
		t.Fatal(status)
	}
	if got := mustGetJobArtifacts(t, handler, jobID); len(got.Artifacts) != 0 {
		t.Fatal(got.Artifacts)
	}
	items, err := store.List()
	if err != nil || len(items) != 1 || items[0].ID != unrelated.ID {
		t.Fatalf("items=%#v err=%v", items, err)
	}
	if store.RecordCalls() != 4 {
		t.Fatalf("mutation attempts=%d", store.RecordCalls())
	}
	_, runReport := mustReadRuntimeReport(t, root, pkg)
	if runReport.Status != "failed" || runReport.Error == nil || !strings.Contains(runReport.Error.Message, "atomic registration failure") {
		t.Fatal(runReport)
	}
}
func TestRuntime_AlignedVerificationFailurePopulatesJobStatus(t *testing.T) {
	testTask12APIFailure(t, "parameter_mismatch", "engine", "verification", "parameter_mismatch", "verification_comparison")
}
func TestRuntime_AlignedVerificationResultFailurePopulatesJobStatus(t *testing.T) {
	testTask12APIFailure(t, "internal_verification_error", "engine", "verification", "internal_verification_error", "verification_result")
}
func TestRuntime_AlignedRuntimeNativeFailurePopulatesJobStatus(t *testing.T) {
	testTask12APIFailure(t, "export_failed", "freecad", "runtime", "export_failed", "export")
}
func TestRuntime_AlignedRuntimeInfrastructureFailurePopulatesJobStatus(t *testing.T) {
	testTask12APIFailure(t, "result_load", "engine", "runtime", "result_load", "result_load")
}
func testTask12APIFailure(t *testing.T, class, boundary, category, code, stage string) {
	t.Helper()
	f := jobstatus.FailureFromError("widget", &executor.ExecutionError{ProductID: "widget", StepID: "2", Err: &executor.CADRuntimeConsumptionError{
		Classification: class, Boundary: boundary, Category: category, Code: code, NativeStage: stage, AttemptID: "attempt-12", Message: "safe",
	}})
	if f.Classification != class || f.Boundary != boundary || f.Category != category || f.Code != code || f.Stage != stage || f.AttemptID != "attempt-12" {
		t.Fatal(f)
	}
}
func TestRuntime_LegacyFailureOmitsAlignedStructuredFields(t *testing.T) {
	f := jobstatus.FailureFromError("widget", errors.New("legacy"))
	if f.Classification != "" || f.Boundary != "" || f.AttemptID != "" {
		t.Fatal(f)
	}
}
func TestRuntime_AlignedFailureStatusJSONIsDeterministic(t *testing.T) {
	f := &jobstatus.Failure{Message: "safe", ProductID: "widget", Classification: "export_failed", AttemptID: "attempt-12"}
	a, _ := json.Marshal(f)
	b, _ := json.Marshal(f)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("non-deterministic")
	}
}
func TestRuntime_ConcurrentAlignedJobsRemainIsolated(t *testing.T) {
	h := newTask12APIHarness(t, map[string]string{"concurrent-a": "blocked", "concurrent-b": "blocked"})
	firstPkg, secondPkg := task12AlignedPackage(t, "concurrent-a"), task12AlignedPackage(t, "concurrent-b")
	firstID := submitTask12APIJob(t, h.handler, firstPkg)
	secondID := submitTask12APIJob(t, h.handler, secondPkg)
	_ = waitForJobState(t, h.handler, firstID, jobstatus.StateRunning)
	_ = waitForJobState(t, h.handler, secondID, jobstatus.StateRunning)
	first, second := h.waitAdapter(t, firstID), h.waitAdapter(t, secondID)
	select {
	case <-first.started:
	case <-time.After(2 * time.Second):
		t.Fatal("first not started")
	}
	select {
	case <-second.started:
	case <-time.After(2 * time.Second):
		t.Fatal("second not started")
	}
	close(first.release)
	if status := waitForTerminalJobState(t, h.handler, firstID); status.State != jobstatus.StateSucceeded {
		t.Fatal(status)
	}
	if status := mustGetJobStatus(t, h.handler, secondID); status.State != jobstatus.StateRunning {
		t.Fatalf("second finalized with first: %#v", status)
	}
	if got := mustGetJobArtifacts(t, h.handler, secondID); len(got.Artifacts) != 0 {
		t.Fatal(got.Artifacts)
	}
	close(second.release)
	if status := waitForTerminalJobState(t, h.handler, secondID); status.State != jobstatus.StateSucceeded {
		t.Fatal(status)
	}
	for _, tc := range []struct {
		id      string
		pkg     *handoff.Package
		adapter *task12APIAdapter
	}{{firstID, firstPkg, first}, {secondID, secondPkg, second}} {
		got := mustGetJobArtifacts(t, h.handler, tc.id)
		requests, reads := tc.adapter.counts()
		if len(got.Artifacts) != 9 || reads != 1 || requests != 1 {
			t.Fatalf("%s artifacts=%d adapter=%#v", tc.id, len(got.Artifacts), tc.adapter)
		}
		for _, item := range got.Artifacts {
			if item.JobID != tc.id || item.ProductID != tc.pkg.ProductKey || strings.Contains(item.Path, map[bool]string{true: secondPkg.ProductKey, false: firstPkg.ProductKey}[tc.id == firstID]) {
				t.Fatalf("cross-job artifact: %#v", item)
			}
		}
		_, runReport := mustReadRuntimeReport(t, h.runRoot, tc.pkg)
		if runReport.Status != "success" || len(runReport.Jobs) != 1 || runReport.Jobs[0].ProductKey != tc.pkg.ProductKey {
			t.Fatal(runReport)
		}
	}
}
func TestDefaultExecutorFactory_IsAlignedCapable(t *testing.T) {
	factory, err := defaultExecutorFactory(nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	exec := factory(task12APIPackage())
	err = exec.ExecutePackage(context.Background(), task12APIPackage())
	var selectionErr *runtimecap.ExecutableSelectionError
	if !errors.As(err, &selectionErr) {
		t.Fatalf("default factory did not reach aligned executable selection: %v", err)
	}
}
