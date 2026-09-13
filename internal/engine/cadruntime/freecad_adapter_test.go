package cadruntime

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/adapter/freecad"
	"parametron/internal/engine/observed"
	"parametron/internal/engine/runtimecap"
	"parametron/internal/engine/verification"
)

// --- Shared fakes ---

type wrapperFakeDelegate struct {
	mu    sync.Mutex
	calls int
	steps []planner.Step
	err   error
}

func (d *wrapperFakeDelegate) Run(_ context.Context, step planner.Step) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	d.steps = append(d.steps, step)
	return d.err
}

func (d *wrapperFakeDelegate) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

// sequencedCapability returns a distinct behavior per call index, used to
// simulate a single wrapper instance running multiple attempts.
type sequencedCapability struct {
	mu    sync.Mutex
	calls int
	steps []func(context.Context, runtimecap.Request) (*runtimecap.Result, error)
}

func (s *sequencedCapability) Invoke(ctx context.Context, req runtimecap.Request) (*runtimecap.Result, error) {
	s.mu.Lock()
	idx := s.calls
	s.calls++
	s.mu.Unlock()
	if idx >= len(s.steps) {
		return nil, fmt.Errorf("unexpected invocation %d", idx)
	}
	return s.steps[idx](ctx, req)
}

// ===================== Part D: wrapper construction and delegation =====================

func TestNewFreeCADRuntimeAdapter_ValidatesDependencies(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	layout := task10Layout(t, req)
	fake := verify11Capability(t, verify11PassBuild(req, layout))
	delegate := &wrapperFakeDelegate{}

	if _, err := NewFreeCADRuntimeAdapter(nil, fake); err == nil {
		t.Fatal("expected error for nil delegate")
	}
	if fake.calls != 0 {
		t.Fatalf("nil-delegate construction touched capability: calls=%d", fake.calls)
	}

	if _, err := NewFreeCADRuntimeAdapter(delegate, nil); err == nil {
		t.Fatal("expected error for nil capability")
	}
	if delegate.callCount() != 0 {
		t.Fatalf("nil-capability construction touched delegate: calls=%d", delegate.callCount())
	}

	wrapper, err := NewFreeCADRuntimeAdapter(delegate, fake)
	if err != nil {
		t.Fatalf("unexpected error for valid construction: %v", err)
	}
	if wrapper == nil {
		t.Fatal("expected non-nil wrapper")
	}
	if fake.calls != 0 {
		t.Fatalf("valid construction resolved an executable/invoked capability: calls=%d", fake.calls)
	}
	if delegate.callCount() != 0 {
		t.Fatalf("valid construction touched delegate: calls=%d", delegate.callCount())
	}
	if _, ok := wrapper.CADRuntimeVerifiedRun(); ok {
		t.Fatal("expected no verified-run snapshot immediately after construction")
	}
}

func TestFreeCADRuntimeAdapter_RunDelegatesNonCADRuntimeStepExactlyOnce(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	layout := task10Layout(t, req)
	fake := verify11Capability(t, verify11PassBuild(req, layout))

	steps := []planner.Step{
		{Type: planner.StepWriteCSV, Payload: planner.WriteCSVPayload{ProductKey: "widget", Filename: "widget.csv"}},
		{Type: planner.StepWriteExportManifest, Payload: planner.WriteExportManifestPayload{ProductKey: "widget", ManifestFilename: planner.ExportManifestFilename}},
	}
	for _, step := range steps {
		delegate := &wrapperFakeDelegate{}
		wrapper, err := NewFreeCADRuntimeAdapter(delegate, fake)
		if err != nil {
			t.Fatalf("construct: %v", err)
		}
		if err := wrapper.Run(context.Background(), step); err != nil {
			t.Fatalf("Run(%s): unexpected error: %v", step.Type, err)
		}
		if delegate.callCount() != 1 {
			t.Fatalf("Run(%s): expected exactly one delegate call, got %d", step.Type, delegate.callCount())
		}
		if len(delegate.steps) != 1 || !reflect.DeepEqual(delegate.steps[0], step) {
			t.Fatalf("Run(%s): step not preserved exactly: got %+v", step.Type, delegate.steps)
		}
	}
	if fake.calls != 0 {
		t.Fatalf("Run must never invoke the aligned capability, calls=%d", fake.calls)
	}
}

func TestFreeCADRuntimeAdapter_RunDoesNotInterpretRunCADRuntime(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	layout := task10Layout(t, req)
	fake := verify11Capability(t, verify11PassBuild(req, layout))
	delegate := &wrapperFakeDelegate{}
	wrapper, err := NewFreeCADRuntimeAdapter(delegate, fake)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}

	step := planner.Step{Type: planner.StepRunCADRuntime, Payload: planner.RunCADRuntimePayload{
		ProductKey: "widget", Adapter: "freecad", ManifestFilename: planner.ExportManifestFilename, ResultFilename: planner.FreeCADRuntimeResultFilename,
	}}
	if err := wrapper.Run(context.Background(), step); err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	if delegate.callCount() != 1 {
		t.Fatalf("expected Run to delegate the RunCADRuntime step to the legacy adapter unconditionally, delegate calls=%d", delegate.callCount())
	}
	if fake.calls != 0 {
		t.Fatalf("Run must not invoke the aligned capability even for a RunCADRuntime step, calls=%d", fake.calls)
	}
	if _, ok := wrapper.CADRuntimeVerifiedRun(); ok {
		t.Fatal("Run must not populate a verified-run snapshot")
	}
}

// ===================== Part E: wrapper orchestration =====================

func TestFreeCADRuntimeAdapter_OrchestrateRetainsSuccessfulVerifiedRun(t *testing.T) {
	req := verify11NoParamRequest(t)
	layout := task10Layout(t, req)
	fake := verify11Capability(t, verify11PassBuild(req, layout))
	delegate := &wrapperFakeDelegate{}
	wrapper, err := NewFreeCADRuntimeAdapter(delegate, fake)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}

	if err := wrapper.OrchestrateCADRuntime(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.calls != 1 {
		t.Fatalf("expected exactly one Task 11 invocation, got %d", fake.calls)
	}
	if delegate.callCount() != 0 {
		t.Fatalf("expected no delegate.Run call during orchestration, got %d", delegate.callCount())
	}
	run, ok := wrapper.CADRuntimeVerifiedRun()
	if !ok {
		t.Fatal("expected a verified-run snapshot")
	}
	if run.Verification == nil || run.Verification.Status != verification.StatusPass {
		t.Fatalf("verification=%#v", run.Verification)
	}
	if run.Runtime.Process == nil || run.Runtime.Result == nil || run.Runtime.Observed == nil {
		t.Fatal("expected complete Task 10/11 state retained")
	}
}

func TestFreeCADRuntimeAdapter_OrchestrateRetainsRuntimeFailureState(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		return &runtimecap.Result{}, nil // missing result.json
	}}
	wrapper, err := NewFreeCADRuntimeAdapter(&wrapperFakeDelegate{}, fake)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}

	err = wrapper.OrchestrateCADRuntime(context.Background(), req)
	if err == nil {
		t.Fatal("expected a runtime failure error")
	}
	run, ok := wrapper.CADRuntimeVerifiedRun()
	if !ok {
		t.Fatal("expected partial state to remain available after a runtime failure")
	}
	if run.Runtime.ObservationRequest.Path == "" {
		t.Fatal("expected partial Task 9/10 state to be retained")
	}
	if run.Verification != nil {
		t.Fatalf("did not expect a verification result after a runtime failure: %#v", run.Verification)
	}
}

func TestFreeCADRuntimeAdapter_OrchestrateRetainsVerificationFailureState(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	layout := task10Layout(t, req)
	fake := verify11Capability(t, verify11ParameterMismatchBuild(req, layout))
	wrapper, err := NewFreeCADRuntimeAdapter(&wrapperFakeDelegate{}, fake)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}

	err = wrapper.OrchestrateCADRuntime(context.Background(), req)
	if err == nil {
		t.Fatal("expected a verification failure error")
	}
	verr := &FreeCADRuntimeVerificationError{}
	if !asVerificationError(err, &verr) || verr.Stage != FreeCADRuntimeVerificationStageVerificationComparison {
		t.Fatalf("expected a verification-comparison stage error, got: %v", err)
	}
	if verr.FailureClass != verification.FailureClassParameterMismatch {
		t.Fatalf("expected parameter mismatch class, got %q", verr.FailureClass)
	}
	run, ok := wrapper.CADRuntimeVerifiedRun()
	if !ok {
		t.Fatal("expected state to remain available after a verification failure")
	}
	if run.Verification == nil || run.Verification.Status != verification.StatusFail || run.Verification.Failure != verification.FailureClassParameterMismatch {
		t.Fatalf("verification result not retained: %#v", run.Verification)
	}
}

func asVerificationError(err error, target **FreeCADRuntimeVerificationError) bool {
	for err != nil {
		if v, ok := err.(*FreeCADRuntimeVerificationError); ok {
			*target = v
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func TestFreeCADRuntimeAdapter_OrchestrateClearsPriorSnapshot(t *testing.T) {
	req1 := verify11NoParamRequest(t)
	layout1 := task10Layout(t, req1)
	pass1 := verify11Capability(t, verify11PassBuild(req1, layout1))

	req2 := verify11NoParamRequest(t)
	layout2 := task10Layout(t, req2)
	pass2 := verify11Capability(t, verify11PassBuild(req2, layout2))

	entered := make(chan struct{})
	release := make(chan struct{})
	seq := &sequencedCapability{steps: []func(context.Context, runtimecap.Request) (*runtimecap.Result, error){
		pass1.fn,
		func(ctx context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
			close(entered)
			<-release
			return pass2.fn(ctx, got)
		},
	}}

	wrapper, err := NewFreeCADRuntimeAdapter(&wrapperFakeDelegate{}, seq)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}

	if err := wrapper.OrchestrateCADRuntime(context.Background(), req1); err != nil {
		t.Fatalf("first attempt: %v", err)
	}
	firstRun, ok := wrapper.CADRuntimeVerifiedRun()
	if !ok || firstRun.Runtime.ObservationRequest.Path == "" {
		t.Fatal("expected a populated first snapshot")
	}

	done := make(chan error, 1)
	go func() {
		done <- wrapper.OrchestrateCADRuntime(context.Background(), req2)
	}()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for second attempt to start")
	}

	if _, ok := wrapper.CADRuntimeVerifiedRun(); ok {
		t.Fatal("expected the first snapshot to be cleared/unavailable during the second attempt")
	}

	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("second attempt: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for second attempt to complete")
	}

	secondRun, ok := wrapper.CADRuntimeVerifiedRun()
	if !ok {
		t.Fatal("expected a second snapshot")
	}
	if secondRun.Runtime.ObservationRequest.Path == firstRun.Runtime.ObservationRequest.Path {
		t.Fatal("expected the second snapshot to reflect the second attempt, not the stale first one")
	}
}

func TestFreeCADRuntimeAdapter_DoesNotRetryInternally(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		return nil, fmt.Errorf("boom")
	}}
	wrapper, err := NewFreeCADRuntimeAdapter(&wrapperFakeDelegate{}, fake)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	if err := wrapper.OrchestrateCADRuntime(context.Background(), req); err == nil {
		t.Fatal("expected an error")
	}
	if fake.calls != 1 {
		t.Fatalf("expected exactly one capability invocation (no internal retry), got %d", fake.calls)
	}
}

// ===================== Part F: provider lifecycle and copying =====================

func TestFreeCADRuntimeAdapter_ProviderAvailabilityLifecycle(t *testing.T) {
	req := verify11NoParamRequest(t)
	layout := task10Layout(t, req)
	entered := make(chan struct{})
	release := make(chan struct{})
	blocking := &task10Capability{fn: func(ctx context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		close(entered)
		<-release
		return verify11Capability(t, verify11PassBuild(req, layout)).fn(ctx, got)
	}}
	wrapper, err := NewFreeCADRuntimeAdapter(&wrapperFakeDelegate{}, blocking)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}

	if _, ok := wrapper.CADRuntimeVerifiedRun(); ok {
		t.Fatal("expected unavailable before any attempt")
	}

	done := make(chan error, 1)
	go func() { done <- wrapper.OrchestrateCADRuntime(context.Background(), req) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for attempt to start")
	}
	if _, ok := wrapper.CADRuntimeVerifiedRun(); ok {
		t.Fatal("expected unavailable after clear and before completion")
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for attempt to complete")
	}
	if _, ok := wrapper.CADRuntimeVerifiedRun(); !ok {
		t.Fatal("expected available after pass")
	}

	failReq := validFreeCADRuntimeExecutionRequest(t)
	failFake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		return &runtimecap.Result{}, nil
	}}
	wrapper2, err := NewFreeCADRuntimeAdapter(&wrapperFakeDelegate{}, failFake)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	if err := wrapper2.OrchestrateCADRuntime(context.Background(), failReq); err == nil {
		t.Fatal("expected failure")
	}
	if _, ok := wrapper2.CADRuntimeVerifiedRun(); !ok {
		t.Fatal("expected available after failure with retained state")
	}
}

func TestFreeCADRuntimeAdapter_ProviderReturnsDefensiveCopy(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	layout := task10Layout(t, req)
	fake := verify11Capability(t, verify11PassBuild(req, layout))
	wrapper, err := NewFreeCADRuntimeAdapter(&wrapperFakeDelegate{}, fake)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	if err := wrapper.OrchestrateCADRuntime(context.Background(), req); err != nil {
		t.Fatalf("orchestrate: %v", err)
	}

	run, ok := wrapper.CADRuntimeVerifiedRun()
	if !ok {
		t.Fatal("expected a snapshot")
	}

	if run.Runtime.Process == nil || len(run.Runtime.Process.Command.Args) == 0 {
		t.Fatal("fixture must produce a non-empty process command for this test to be meaningful")
	}
	run.Runtime.Process.Command.Args[0] = "mutated"
	run.Runtime.Process.Stdout = "mutated"

	if run.Runtime.Result == nil || len(run.Runtime.Result.Artifacts) == 0 {
		t.Fatal("fixture must produce at least one artifact for this test to be meaningful")
	}
	run.Runtime.Result.Artifacts[0].Path = "mutated"
	run.Runtime.Result.Status = "mutated"

	if run.Runtime.Observed == nil || len(run.Runtime.Observed.Observation.Parameters) == 0 {
		t.Fatal("fixture must produce observed parameters for this test to be meaningful")
	}
	run.Runtime.Observed.WorkingCopy.SHA256 = "mutated"
	run.Runtime.Observed.Observation.Parameters[0].ID = "mutated"
	run.Runtime.Observed.Observation.Metadata = append(run.Runtime.Observed.Observation.Metadata, run.Runtime.Observed.Observation.Metadata[0])

	run.Runtime.Artifacts = append(run.Runtime.Artifacts, FreeCADRuntimeValidatedArtifact{ID: "mutated"})

	if len(run.Runtime.ObservationRequest.JSON) == 0 || len(run.Runtime.ObservationRequest.Manifest.JSON) == 0 {
		t.Fatal("fixture must produce non-empty JSON snapshots for this test to be meaningful")
	}
	run.Runtime.ObservationRequest.JSON[0] = 0xFF
	run.Runtime.ObservationRequest.Manifest.JSON[0] = 0xFF
	if len(run.Runtime.ObservationRequest.Manifest.Manifest.ParameterAssignments) > 0 {
		run.Runtime.ObservationRequest.Manifest.Manifest.ParameterAssignments[0].Value = "mutated"
	}
	run.Runtime.ObservationRequest.Manifest.Manifest.Outputs = append(run.Runtime.ObservationRequest.Manifest.Manifest.Outputs, freecad.FreeCADRuntimeManifestOutput{ID: "mutated"})
	run.Runtime.ObservationRequest.Contract.Expected.Parameters = append(run.Runtime.ObservationRequest.Contract.Expected.Parameters, verification.ExpectedParameter{Name: "mutated"})
	run.Runtime.ObservationRequest.Contract.ObservationContext.Parameters = append(run.Runtime.ObservationRequest.Contract.ObservationContext.Parameters, verification.ObservedParameterBinding{})

	if run.Verification != nil {
		run.Verification.Status = "mutated"
		run.Verification.Failure = "mutated"
	}

	again, ok := wrapper.CADRuntimeVerifiedRun()
	if !ok {
		t.Fatal("expected the snapshot to still be available")
	}
	if again.Runtime.Process.Command.Args[0] == "mutated" || again.Runtime.Process.Stdout == "mutated" {
		t.Fatal("process mutation leaked into stored state")
	}
	if again.Runtime.Result.Artifacts[0].Path == "mutated" || again.Runtime.Result.Status == "mutated" {
		t.Fatal("result mutation leaked into stored state")
	}
	if again.Runtime.Observed.WorkingCopy.SHA256 == "mutated" || again.Runtime.Observed.Observation.Parameters[0].ID == "mutated" {
		t.Fatal("observed mutation leaked into stored state")
	}
	if len(again.Runtime.Observed.Observation.Metadata) != len(run.Runtime.Observed.Observation.Metadata)-1 {
		t.Fatal("observed metadata slice append leaked into stored state")
	}
	if len(again.Runtime.Artifacts) != 1 {
		t.Fatalf("artifact slice append leaked into stored state: %d", len(again.Runtime.Artifacts))
	}
	if again.Runtime.ObservationRequest.JSON[0] == 0xFF || again.Runtime.ObservationRequest.Manifest.JSON[0] == 0xFF {
		t.Fatal("raw JSON mutation leaked into stored state")
	}
	if len(again.Runtime.ObservationRequest.Manifest.Manifest.ParameterAssignments) > 0 && again.Runtime.ObservationRequest.Manifest.Manifest.ParameterAssignments[0].Value == "mutated" {
		t.Fatal("parameter assignment mutation leaked into stored state")
	}
	if len(again.Runtime.ObservationRequest.Manifest.Manifest.Outputs) != len(run.Runtime.ObservationRequest.Manifest.Manifest.Outputs)-1 {
		t.Fatal("manifest output slice append leaked into stored state")
	}
	if len(again.Runtime.ObservationRequest.Contract.Expected.Parameters) != len(run.Runtime.ObservationRequest.Contract.Expected.Parameters)-1 {
		t.Fatal("expected-parameters slice append leaked into stored state")
	}
	if len(again.Runtime.ObservationRequest.Contract.ObservationContext.Parameters) != len(run.Runtime.ObservationRequest.Contract.ObservationContext.Parameters)-1 {
		t.Fatal("observation-context parameters slice append leaked into stored state")
	}
	if again.Verification.Status == "mutated" || again.Verification.Failure == "mutated" {
		t.Fatal("verification result mutation leaked into stored state")
	}
}

func TestFreeCADRuntimeAdapter_RepeatedProviderReadsAreIndependent(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	layout := task10Layout(t, req)
	fake := verify11Capability(t, verify11PassBuild(req, layout))
	wrapper, err := NewFreeCADRuntimeAdapter(&wrapperFakeDelegate{}, fake)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	if err := wrapper.OrchestrateCADRuntime(context.Background(), req); err != nil {
		t.Fatalf("orchestrate: %v", err)
	}

	first, ok := wrapper.CADRuntimeVerifiedRun()
	if !ok {
		t.Fatal("expected snapshot")
	}
	second, ok := wrapper.CADRuntimeVerifiedRun()
	if !ok {
		t.Fatal("expected snapshot")
	}
	if &first.Runtime == &second.Runtime {
		t.Fatal("expected independent struct copies")
	}
	if len(first.Runtime.Artifacts) > 0 && len(second.Runtime.Artifacts) > 0 {
		first.Runtime.Artifacts[0].ID = "mutated-first-read"
		if second.Runtime.Artifacts[0].ID == "mutated-first-read" {
			t.Fatal("mutating one read leaked into a separate read")
		}
	}
}

func TestFreeCADRuntimeAdapter_ProviderCopiesReferenceTraversalDefensively(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	layout := task10Layout(t, req)
	traversal := []byte(`{"schemaVersion":"2.0","kind":"reference-traversal","boundary":"internal","operation":"resolve","status":"succeeded","sourceDocument":"Widget.FCStd","nodes":[],"edges":[],"diagnostics":[]}`)
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		artifacts := task10ManifestArtifacts(t, got)
		task10WriteArtifactFiles(t, got, artifacts, "artifact")
		task10Result(t, got, artifacts, "succeeded")
		digest := task10PreparedSourceDigest(t, got)
		obs := verify11BuildPassObserved(t, req, layout, got, digest)
		data, err := observed.CanonicalJSON(obs)
		if err != nil {
			t.Fatal(err)
		}
		task10RawObserved(t, got, data)
		task10WriteReferenceTraversal(t, got, traversal)
		return &runtimecap.Result{
			Command: runtimecap.Command{Path: got.RuntimeCommand, Args: []string{"execute"}},
			Stdout:  "run-stdout", Stderr: "run-stderr",
		}, nil
	}}
	wrapper, err := NewFreeCADRuntimeAdapter(&wrapperFakeDelegate{}, fake)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	if err := wrapper.OrchestrateCADRuntime(context.Background(), req); err != nil {
		t.Fatalf("orchestrate: %v", err)
	}

	run, ok := wrapper.CADRuntimeVerifiedRun()
	if !ok {
		t.Fatal("expected a snapshot")
	}
	if !bytes.Equal(run.Runtime.ReferenceTraversalJSON, traversal) {
		t.Fatalf("traversal bytes not preserved: got=%q want=%q", run.Runtime.ReferenceTraversalJSON, traversal)
	}

	// Mutating the returned snapshot bytes must not leak into a subsequent read.
	run.Runtime.ReferenceTraversalJSON[0] = 'X'
	again, ok := wrapper.CADRuntimeVerifiedRun()
	if !ok {
		t.Fatal("expected a snapshot")
	}
	if !bytes.Equal(again.Runtime.ReferenceTraversalJSON, traversal) {
		t.Fatalf("mutation of a returned snapshot leaked into stored state: %q", again.Runtime.ReferenceTraversalJSON)
	}
}

func TestFreeCADRuntimeAdapter_ConcurrentProviderReadsAreRaceSafe(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	layout := task10Layout(t, req)
	fake := verify11Capability(t, verify11PassBuild(req, layout))
	wrapper, err := NewFreeCADRuntimeAdapter(&wrapperFakeDelegate{}, fake)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	if err := wrapper.OrchestrateCADRuntime(context.Background(), req); err != nil {
		t.Fatalf("orchestrate: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			run, ok := wrapper.CADRuntimeVerifiedRun()
			if ok && len(run.Runtime.Artifacts) > 0 {
				run.Runtime.Artifacts[0].ID = "race-mutation"
			}
		}()
	}
	wg.Wait()
}

func TestFreeCADRuntimeAdapter_SerializesConcurrentOrchestration(t *testing.T) {
	// Both concurrent calls share the same request/fixture: orchestrateMu
	// serializes them, so no two invocations ever touch the working copy at
	// once, and it is therefore safe (and simpler) to reuse one fixture
	// rather than correlate call order to a specific request.
	req := verify11NoParamRequest(t)
	layout := task10Layout(t, req)

	var inFlight int32
	var overlapDetected int32
	fake := &task10Capability{fn: func(ctx context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		if atomic.AddInt32(&inFlight, 1) != 1 {
			atomic.StoreInt32(&overlapDetected, 1)
		}
		time.Sleep(10 * time.Millisecond)
		result, err := verify11Capability(t, verify11PassBuild(req, layout)).fn(ctx, got)
		atomic.AddInt32(&inFlight, -1)
		return result, err
	}}

	wrapper, err := NewFreeCADRuntimeAdapter(&wrapperFakeDelegate{}, fake)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		errs <- wrapper.OrchestrateCADRuntime(context.Background(), req)
	}()
	go func() {
		defer wg.Done()
		errs <- wrapper.OrchestrateCADRuntime(context.Background(), req)
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if atomic.LoadInt32(&overlapDetected) != 0 {
		t.Fatal("expected concurrent orchestration calls to be serialized, but capability invocations overlapped")
	}
}

func TestFreeCADRuntimeAdapter_SeparateInstancesAreIsolated(t *testing.T) {
	req1 := validFreeCADRuntimeExecutionRequest(t)
	layout1 := task10Layout(t, req1)
	fake1 := verify11Capability(t, verify11PassBuild(req1, layout1))
	wrapper1, err := NewFreeCADRuntimeAdapter(&wrapperFakeDelegate{}, fake1)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}

	req2 := validFreeCADRuntimeExecutionRequest(t)
	layout2 := task10Layout(t, req2)
	fake2 := verify11Capability(t, verify11PassBuild(req2, layout2))
	wrapper2, err := NewFreeCADRuntimeAdapter(&wrapperFakeDelegate{}, fake2)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}

	if err := wrapper1.OrchestrateCADRuntime(context.Background(), req1); err != nil {
		t.Fatalf("wrapper1 orchestrate: %v", err)
	}
	if _, ok := wrapper2.CADRuntimeVerifiedRun(); ok {
		t.Fatal("expected wrapper2 to remain unaffected by wrapper1's orchestration")
	}
	if fake2.calls != 0 {
		t.Fatalf("expected wrapper2's capability to remain uninvoked, calls=%d", fake2.calls)
	}
}

// ===================== Part N: protected boundaries (wrapper-local) =====================

func TestTask13_AlignedWrapperHasSingleVerificationInvocation(t *testing.T) {
	data, err := os.ReadFile("freecad_adapter.go")
	if err != nil {
		t.Fatalf("failed to read freecad_adapter.go: %v", err)
	}
	src := string(data)
	if n := strings.Count(src, "InvokeAndVerifyFreeCADRuntime("); n != 1 {
		t.Fatalf("expected exactly one InvokeAndVerifyFreeCADRuntime call site in the wrapper, found %d", n)
	}
	if strings.Contains(src, "InvokeAndValidateFreeCADRuntime(") {
		t.Fatal("wrapper must not call InvokeAndValidateFreeCADRuntime directly; it must go through InvokeAndVerifyFreeCADRuntime")
	}
	if strings.Contains(src, "verification.Verify(") {
		t.Fatal("wrapper must not call verification.Verify directly; it must go through InvokeAndVerifyFreeCADRuntime")
	}
}

func TestTask13_BaseAdapterDoesNotDependOnCADRuntimePackage(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "adapter", "adapter.go"))
	if err != nil {
		t.Fatalf("failed to read adapter.go: %v", err)
	}
	if strings.Contains(string(data), "cadruntime") {
		t.Fatal("base Adapter interface must not depend on the cadruntime package")
	}
}
