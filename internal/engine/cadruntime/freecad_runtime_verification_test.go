package cadruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/adapter"
	"parametron/internal/engine/adapter/freecad"
	"parametron/internal/engine/observed"
	"parametron/internal/engine/runtimecap"
	"parametron/internal/engine/verification"
)

// --- Request fixtures ---

// verify11NoParamRequest is the Task 9/10 fixture with no expected
// parameters: the aligned contract disables parameter observation/checks.
func verify11NoParamRequest(t *testing.T) adapter.CADRuntimeOrchestrationRequest {
	t.Helper()
	req := validFreeCADRuntimeExecutionRequest(t)
	req.Manifest.ParameterAssignments = nil
	req.Manifest.Verification = planner.VerificationManifestIntent{}
	return req
}

// verify11MultiParamRequest is the Task 9/10 fixture with multiple explicit
// expected parameters and a distinct attempt number.
func verify11MultiParamRequest(t *testing.T) adapter.CADRuntimeOrchestrationRequest {
	t.Helper()
	req := validFreeCADRuntimeExecutionRequest(t)
	req.Attempt = 3
	req.Manifest.ParameterAssignments = []planner.ExportManifestParameterAssignment{
		{Name: "width", Target: "Body.Width", Value: 10, Type: "number", Unit: "mm"},
		{Name: "height", Target: "Body.Height", Value: 20, Type: "number", Unit: "mm"},
	}
	req.Manifest.Verification.ExpectedParameters = []planner.VerificationExpectedParameter{
		{ID: "width", Name: "Width", Value: 10, Type: "number", Unit: "mm"},
		{ID: "height", Name: "Height", Value: 20, Type: "number", Unit: "mm"},
	}
	req.Manifest.Verification.ObservationParameterLinks = []planner.VerificationObservationParameterLink{
		{ID: "width", Name: "Width", GroupName: "VarSet"},
		{ID: "height", Name: "Height", GroupName: "VarSet"},
	}
	return req
}

// --- Observed fixture helpers ---

func verify11Value(t *testing.T, value float64) observed.Value {
	t.Helper()
	v, err := observed.NewValue(json.RawMessage(strconv.FormatFloat(value, 'f', -1, 64)))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func verify11Parameter(t *testing.T, id string, value float64) observed.Parameter {
	t.Helper()
	return observed.Parameter{ID: id, Name: id, GroupID: "VarSet", Value: verify11Value(t, value), ValueKind: "number"}
}

func verify11Metadata(t *testing.T, digest string) observed.Metadata {
	t.Helper()
	raw, err := json.Marshal(digest)
	if err != nil {
		t.Fatal(err)
	}
	v, err := observed.NewValue(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	return observed.Metadata{ID: "working-copy-sha256", Key: "working_copy_sha256", Value: v, ValueKind: "string"}
}

// verify11BuildPassObserved builds the exact aligned Observed state that
// matches req's derived verification contract: matching top-level
// working-copy identity, matching working_copy_sha256 metadata, matching
// working_copy_path reference, and observed parameters mirroring every
// explicit expected parameter.
func verify11BuildPassObserved(
	t *testing.T,
	req adapter.CADRuntimeOrchestrationRequest,
	layout freecad.FreeCADRuntimeWorkingCopyLayout,
	got runtimecap.Request,
	digest string,
) *observed.Observed {
	t.Helper()
	params := make([]observed.Parameter, len(req.Manifest.Verification.ExpectedParameters))
	for i, expected := range req.Manifest.Verification.ExpectedParameters {
		params[i] = verify11Parameter(t, expected.ID, expected.Value)
	}
	return &observed.Observed{
		SchemaVersion: observed.SchemaVersion,
		WorkingCopy:   observed.WorkingCopy{Path: got.WorkingCopyDir, SHA256: digest},
		Observation: observed.Observation{
			Parameters: params,
			Metadata:   []observed.Metadata{verify11Metadata(t, digest)},
			References: []observed.Reference{{Kind: "working_copy_path", Name: layout.WorkingCopyDir}},
			Components: []observed.Component{},
		},
	}
}

// --- Scenario builders: functions producing the Task 10 observed-output
// closure for one verification outcome. ---

type verify11Build func(t *testing.T, got runtimecap.Request, digest string) *observed.Observed

func verify11PassBuild(req adapter.CADRuntimeOrchestrationRequest, layout freecad.FreeCADRuntimeWorkingCopyLayout) verify11Build {
	return func(t *testing.T, got runtimecap.Request, digest string) *observed.Observed {
		return verify11BuildPassObserved(t, req, layout, got, digest)
	}
}

func verify11ParameterMismatchBuild(req adapter.CADRuntimeOrchestrationRequest, layout freecad.FreeCADRuntimeWorkingCopyLayout) verify11Build {
	return func(t *testing.T, got runtimecap.Request, digest string) *observed.Observed {
		obs := verify11BuildPassObserved(t, req, layout, got, digest)
		obs.Observation.Parameters[0].Value = verify11Value(t, 4242)
		return obs
	}
}

func verify11MissingParameterBuild(req adapter.CADRuntimeOrchestrationRequest, layout freecad.FreeCADRuntimeWorkingCopyLayout) verify11Build {
	return func(t *testing.T, got runtimecap.Request, digest string) *observed.Observed {
		obs := verify11BuildPassObserved(t, req, layout, got, digest)
		obs.Observation.Parameters = []observed.Parameter{}
		return obs
	}
}

func verify11MetadataMismatchBuild(req adapter.CADRuntimeOrchestrationRequest, layout freecad.FreeCADRuntimeWorkingCopyLayout) verify11Build {
	return func(t *testing.T, got runtimecap.Request, digest string) *observed.Observed {
		obs := verify11BuildPassObserved(t, req, layout, got, digest)
		obs.Observation.Metadata[0] = verify11Metadata(t, strings.Repeat("0", 64))
		return obs
	}
}

func verify11ReferenceMismatchBuild(req adapter.CADRuntimeOrchestrationRequest, layout freecad.FreeCADRuntimeWorkingCopyLayout) verify11Build {
	return func(t *testing.T, got runtimecap.Request, digest string) *observed.Observed {
		obs := verify11BuildPassObserved(t, req, layout, got, digest)
		obs.Observation.References[0] = observed.Reference{Kind: "working_copy_path", Name: "/wrong/reference/path"}
		return obs
	}
}

func verify11ParameterAndMetadataMismatchBuild(req adapter.CADRuntimeOrchestrationRequest, layout freecad.FreeCADRuntimeWorkingCopyLayout) verify11Build {
	return func(t *testing.T, got runtimecap.Request, digest string) *observed.Observed {
		obs := verify11BuildPassObserved(t, req, layout, got, digest)
		obs.Observation.Parameters[0].Value = verify11Value(t, 4242)
		obs.Observation.Metadata[0] = verify11Metadata(t, strings.Repeat("0", 64))
		return obs
	}
}

func verify11MissingParameterAndReferenceMismatchBuild(req adapter.CADRuntimeOrchestrationRequest, layout freecad.FreeCADRuntimeWorkingCopyLayout) verify11Build {
	return func(t *testing.T, got runtimecap.Request, digest string) *observed.Observed {
		obs := verify11BuildPassObserved(t, req, layout, got, digest)
		obs.Observation.Parameters = []observed.Parameter{}
		obs.Observation.References[0] = observed.Reference{Kind: "working_copy_path", Name: "/wrong/reference/path"}
		return obs
	}
}

func verify11MetadataAndReferenceMismatchBuild(req adapter.CADRuntimeOrchestrationRequest, layout freecad.FreeCADRuntimeWorkingCopyLayout) verify11Build {
	return func(t *testing.T, got runtimecap.Request, digest string) *observed.Observed {
		obs := verify11BuildPassObserved(t, req, layout, got, digest)
		obs.Observation.Metadata[0] = verify11Metadata(t, strings.Repeat("0", 64))
		obs.Observation.References[0] = observed.Reference{Kind: "working_copy_path", Name: "/wrong/reference/path"}
		return obs
	}
}

// --- Fake runtime capability ---

// verify11Capability returns a fake runtimecap.Capability that writes
// declared artifacts, a succeeded result.json, and the observed output
// produced by build, mirroring a real aligned runtime invocation.
func verify11Capability(t *testing.T, build verify11Build) *task10Capability {
	t.Helper()
	return &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		artifacts := task10ManifestArtifacts(t, got)
		task10WriteArtifactFiles(t, got, artifacts, "artifact")
		task10Result(t, got, artifacts, "succeeded")
		digest := task10PreparedSourceDigest(t, got)
		obs := build(t, got, digest)
		data, err := observed.CanonicalJSON(obs)
		if err != nil {
			t.Fatal(err)
		}
		task10RawObserved(t, got, data)
		return &runtimecap.Result{
			Command: runtimecap.Command{Path: got.RuntimeCommand, Args: []string{"execute"}},
			Stdout:  "run-stdout",
			Stderr:  "run-stderr",
		}, nil
	}}
}

// verify11ValidRuntimeRun produces the raw Task 10 state for a matching-pass
// observed output, without performing Task 11 comparison.
func verify11ValidRuntimeRun(t *testing.T, req adapter.CADRuntimeOrchestrationRequest, layout freecad.FreeCADRuntimeWorkingCopyLayout) FreeCADRuntimeRun {
	t.Helper()
	fake := verify11Capability(t, verify11PassBuild(req, layout))
	run, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func requireTask11Error(t *testing.T, err error, stage string) *FreeCADRuntimeVerificationError {
	t.Helper()
	var got *FreeCADRuntimeVerificationError
	if !errors.As(err, &got) || got.Stage != stage {
		t.Fatalf("error=%T %v typed=%#v want stage %q", err, err, got, stage)
	}
	return got
}

// ===================== Part A: verification pass behavior =====================

func TestInvokeAndVerifyFreeCADRuntime_NoParameterPass(t *testing.T) {
	req := verify11NoParamRequest(t)
	layout := task10Layout(t, req)
	fake := verify11Capability(t, verify11PassBuild(req, layout))

	run, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
	if err != nil {
		t.Fatal(err)
	}
	if run.Verification == nil || run.Verification.Status != verification.StatusPass || run.Verification.Failure != verification.FailureClassNone {
		t.Fatalf("verification=%#v", run.Verification)
	}
	cats := run.Verification.Categories
	if cats.Parameters.Enabled || cats.Parameters.Status != verification.CategoryStatusSkipped {
		t.Fatalf("parameters category=%#v", cats.Parameters)
	}
	if !cats.Metadata.Enabled || cats.Metadata.Status != verification.CategoryStatusPass {
		t.Fatalf("metadata category=%#v", cats.Metadata)
	}
	if !cats.References.Enabled || cats.References.Status != verification.CategoryStatusPass {
		t.Fatalf("references category=%#v", cats.References)
	}
	if cats.Components.Enabled || cats.Components.Status != verification.CategoryStatusSkipped {
		t.Fatalf("components category=%#v", cats.Components)
	}
	if run.Runtime.Process == nil || run.Runtime.Result == nil || run.Runtime.Observed == nil || len(run.Runtime.Artifacts) != 1 {
		t.Fatalf("incomplete runtime state: %#v", run.Runtime)
	}
	if fake.calls != 1 {
		t.Fatalf("calls=%d", fake.calls)
	}
}

// TestInvokeAndVerifyFreeCADRuntime_ZeroDerivedArtifactsPass proves that
// Engine verification -- parameter/configuration expected state plus
// observation comparison -- can pass with zero derived artifacts. It must
// not depend on artifact count: this test combines the no-expected
// -parameters fixture with a native-only (zero manifest outputs) request to
// prove verification categories are decided purely by parameter/metadata/
// reference/component comparison, never by len(Runtime.Artifacts).
func TestInvokeAndVerifyFreeCADRuntime_ZeroDerivedArtifactsPass(t *testing.T) {
	req := verify11NoParamRequest(t)
	req.Manifest.Outputs = []planner.ExportManifestOutput{}
	layout := task10Layout(t, req)
	fake := verify11Capability(t, verify11PassBuild(req, layout))

	run, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
	if err != nil {
		t.Fatal(err)
	}
	if run.Runtime.Artifacts == nil {
		t.Fatal("expected a non-nil empty Artifacts slice")
	}
	if len(run.Runtime.Artifacts) != 0 {
		t.Fatalf("expected zero derived artifacts, got %+v", run.Runtime.Artifacts)
	}
	if run.Verification == nil || run.Verification.Status != verification.StatusPass || run.Verification.Failure != verification.FailureClassNone {
		t.Fatalf("expected verification to pass despite zero derived artifacts, got %#v", run.Verification)
	}
	cats := run.Verification.Categories
	if !cats.Metadata.Enabled || cats.Metadata.Status != verification.CategoryStatusPass {
		t.Fatalf("metadata category=%#v", cats.Metadata)
	}
	if !cats.References.Enabled || cats.References.Status != verification.CategoryStatusPass {
		t.Fatalf("references category=%#v", cats.References)
	}
}

func TestInvokeAndVerifyFreeCADRuntime_ExplicitParameterPass(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	want := cloneFreeCADObservationRequest(req)
	layout := task10Layout(t, req)
	fake := verify11Capability(t, verify11PassBuild(req, layout))

	run, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
	if err != nil {
		t.Fatal(err)
	}
	cats := run.Verification.Categories
	if !cats.Parameters.Enabled || cats.Parameters.Status != verification.CategoryStatusPass {
		t.Fatalf("parameters category=%#v", cats.Parameters)
	}
	if !cats.Metadata.Enabled || cats.Metadata.Status != verification.CategoryStatusPass {
		t.Fatalf("metadata category=%#v", cats.Metadata)
	}
	if !cats.References.Enabled || cats.References.Status != verification.CategoryStatusPass {
		t.Fatalf("references category=%#v", cats.References)
	}
	if run.Verification.Status != verification.StatusPass {
		t.Fatalf("status=%q", run.Verification.Status)
	}
	if run.Runtime.Process == nil || run.Runtime.Result == nil || run.Runtime.Observed == nil {
		t.Fatalf("incomplete runtime state: %#v", run.Runtime)
	}
	// No name/group identity inference: matching relies solely on ID.
	if !reflect.DeepEqual(req, want) {
		t.Fatal("request mutated")
	}
}

func TestInvokeAndVerifyFreeCADRuntime_MultipleParametersPass(t *testing.T) {
	req := verify11MultiParamRequest(t)
	layout := task10Layout(t, req)
	fake := verify11Capability(t, verify11PassBuild(req, layout))

	run, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
	if err != nil {
		t.Fatal(err)
	}
	cats := run.Verification.Categories
	if !cats.Parameters.Enabled || cats.Parameters.Status != verification.CategoryStatusPass {
		t.Fatalf("parameters category=%#v", cats.Parameters)
	}
	if run.Verification.Status != verification.StatusPass {
		t.Fatalf("status=%q", run.Verification.Status)
	}

	// Repeated equivalent independent run is stable.
	req2 := verify11MultiParamRequest(t)
	layout2 := task10Layout(t, req2)
	fake2 := verify11Capability(t, verify11PassBuild(req2, layout2))
	run2, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake2, req2)
	if err != nil {
		t.Fatal(err)
	}
	if run2.Verification.Status != verification.StatusPass || run2.Verification.Categories.Parameters.Status != verification.CategoryStatusPass {
		t.Fatalf("second run=%#v", run2.Verification)
	}
}

// ===================== Part B: expected verification failures =====================

func TestInvokeAndVerifyFreeCADRuntime_ReturnsParameterMismatch(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	layout := task10Layout(t, req)
	fake := verify11Capability(t, verify11ParameterMismatchBuild(req, layout))

	run, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
	verr := requireTask11Error(t, err, FreeCADRuntimeVerificationStageVerificationComparison)
	if verr.FailureClass != verification.FailureClassParameterMismatch {
		t.Fatalf("class=%q", verr.FailureClass)
	}
	var typed *verification.VerifyError
	if !errors.As(err, &typed) || !errors.Is(err, verification.ErrParameterMismatch) {
		t.Fatalf("sentinel missing: %v", err)
	}
	if run.Verification == nil || run.Verification.Status != verification.StatusFail || run.Verification.Failure != verification.FailureClassParameterMismatch {
		t.Fatalf("verification=%#v", run.Verification)
	}
	if run.Runtime.Process == nil || run.Runtime.Result == nil || run.Runtime.Observed == nil {
		t.Fatalf("runtime state lost: %#v", run.Runtime)
	}
	if fake.calls != 1 {
		t.Fatalf("calls=%d", fake.calls)
	}
}

func TestInvokeAndVerifyFreeCADRuntime_ReturnsRequiredObservationMissing(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	layout := task10Layout(t, req)
	fake := verify11Capability(t, verify11MissingParameterBuild(req, layout))

	run, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
	verr := requireTask11Error(t, err, FreeCADRuntimeVerificationStageVerificationComparison)
	if verr.FailureClass != verification.FailureClassRequiredObservationMissing {
		t.Fatalf("class=%q", verr.FailureClass)
	}
	if !errors.Is(err, verification.ErrRequiredObservationMissing) {
		t.Fatalf("sentinel missing: %v", err)
	}
	if run.Verification == nil || run.Verification.Status != verification.StatusFail || run.Verification.Failure != verification.FailureClassRequiredObservationMissing {
		t.Fatalf("verification=%#v", run.Verification)
	}
	if fake.calls != 1 {
		t.Fatalf("calls=%d", fake.calls)
	}
}

func TestInvokeAndVerifyFreeCADRuntime_ReturnsMetadataMismatch(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	layout := task10Layout(t, req)
	fake := verify11Capability(t, verify11MetadataMismatchBuild(req, layout))

	run, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
	verr := requireTask11Error(t, err, FreeCADRuntimeVerificationStageVerificationComparison)
	if verr.FailureClass != verification.FailureClassMetadataMismatch {
		t.Fatalf("class=%q", verr.FailureClass)
	}
	if !errors.Is(err, verification.ErrMetadataMismatch) {
		t.Fatalf("sentinel missing: %v", err)
	}
	// Task 10 top-level SHA correlation stays valid: this proves Task 10
	// identity correlation does not replace Engine verification.
	if run.Runtime.Observed.WorkingCopy.SHA256 == "" {
		t.Fatal("top-level working-copy identity lost")
	}
	if run.Verification == nil || run.Verification.Status != verification.StatusFail || run.Verification.Failure != verification.FailureClassMetadataMismatch {
		t.Fatalf("verification=%#v", run.Verification)
	}
	if fake.calls != 1 {
		t.Fatalf("calls=%d", fake.calls)
	}
}

func TestInvokeAndVerifyFreeCADRuntime_ReturnsReferenceMismatch(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	layout := task10Layout(t, req)
	fake := verify11Capability(t, verify11ReferenceMismatchBuild(req, layout))

	run, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
	verr := requireTask11Error(t, err, FreeCADRuntimeVerificationStageVerificationComparison)
	if verr.FailureClass != verification.FailureClassReferenceMismatch {
		t.Fatalf("class=%q", verr.FailureClass)
	}
	if !errors.Is(err, verification.ErrReferenceMismatch) {
		t.Fatalf("sentinel missing: %v", err)
	}
	if run.Runtime.Observed.WorkingCopy.Path != layout.WorkingCopyDir {
		t.Fatal("top-level working-copy path lost")
	}
	if run.Verification == nil || run.Verification.Status != verification.StatusFail || run.Verification.Failure != verification.FailureClassReferenceMismatch {
		t.Fatalf("verification=%#v", run.Verification)
	}
	if fake.calls != 1 {
		t.Fatalf("calls=%d", fake.calls)
	}
}

func TestInvokeAndVerifyFreeCADRuntime_RejectsWrongWorkingCopyReferences(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	layout := task10Layout(t, req)
	cases := map[string]string{
		"prepared source": layout.SourceDocumentPath,
		"parent root":     filepath.Dir(layout.WorkingCopyDir),
		"another attempt": layout.WorkingCopyDir + "-attempt-000002",
		"non-canonical":   layout.WorkingCopyDir + string(filepath.Separator) + ".." + string(filepath.Separator) + filepath.Base(layout.WorkingCopyDir),
		"relative":        "relative/attempt",
	}
	for name, reference := range cases {
		t.Run(name, func(t *testing.T) {
			build := func(t *testing.T, got runtimecap.Request, digest string) *observed.Observed {
				value := verify11BuildPassObserved(t, req, layout, got, digest)
				value.Observation.References[0].Name = reference
				return value
			}
			fake := verify11Capability(t, build)
			run, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
			if err == nil {
				t.Fatalf("reference %q unexpectedly passed: %#v", reference, run.Verification)
			}
			if fake.calls != 1 {
				t.Fatalf("calls=%d", fake.calls)
			}
		})
	}
}

func TestInvokeAndVerifyFreeCADRuntime_VerificationFailureRetainsRuntimeState(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	layout := task10Layout(t, req)
	fake := verify11Capability(t, verify11ParameterMismatchBuild(req, layout))

	run, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
	if err == nil {
		t.Fatal("expected verification failure")
	}
	rt := run.Runtime
	if rt.Process == nil || rt.Process.Command.Path == "" || rt.Process.Stdout != "run-stdout" || rt.Process.Stderr != "run-stderr" {
		t.Fatalf("process state lost: %#v", rt.Process)
	}
	if rt.Result == nil || rt.Result.Status != freecad.FreeCADRuntimeResultStatusSucceeded {
		t.Fatalf("result state lost: %#v", rt.Result)
	}
	if rt.Observed == nil {
		t.Fatal("observed state lost")
	}
	if rt.ObservedPath == "" {
		t.Fatal("observed path lost")
	}
	if len(rt.Artifacts) != 1 {
		t.Fatalf("artifacts lost: %#v", rt.Artifacts)
	}
	if rt.ObservationRequest.Path == "" || len(rt.ObservationRequest.JSON) == 0 {
		t.Fatal("observation request lost")
	}
	if run.Verification == nil || run.Verification.Status != verification.StatusFail {
		t.Fatalf("verification fail result lost: %#v", run.Verification)
	}
}

// ===================== Part C: failure ordering =====================

func TestInvokeAndVerifyFreeCADRuntime_PreservesVerificationFailureOrdering(t *testing.T) {
	tests := []struct {
		name      string
		reqFn     func(t *testing.T) adapter.CADRuntimeOrchestrationRequest
		buildFn   func(req adapter.CADRuntimeOrchestrationRequest, layout freecad.FreeCADRuntimeWorkingCopyLayout) verify11Build
		wantClass verification.FailureClass
	}{
		{"parameter and metadata mismatch", validFreeCADRuntimeExecutionRequest, verify11ParameterAndMetadataMismatchBuild, verification.FailureClassParameterMismatch},
		{"missing parameter and reference mismatch", validFreeCADRuntimeExecutionRequest, verify11MissingParameterAndReferenceMismatchBuild, verification.FailureClassRequiredObservationMissing},
		{"disabled parameters, metadata and reference mismatch", verify11NoParamRequest, verify11MetadataAndReferenceMismatchBuild, verification.FailureClassMetadataMismatch},
		{"matching parameters, metadata and reference mismatch", validFreeCADRuntimeExecutionRequest, verify11MetadataAndReferenceMismatchBuild, verification.FailureClassMetadataMismatch},
		{"matching parameters and metadata, reference mismatch", validFreeCADRuntimeExecutionRequest, verify11ReferenceMismatchBuild, verification.FailureClassReferenceMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := func() (FreeCADRuntimeVerifiedRun, error) {
				req := tt.reqFn(t)
				layout := task10Layout(t, req)
				fake := verify11Capability(t, tt.buildFn(req, layout))
				return InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
			}
			result1, err1 := run()
			verr1 := requireTask11Error(t, err1, FreeCADRuntimeVerificationStageVerificationComparison)
			if verr1.FailureClass != tt.wantClass {
				t.Fatalf("class=%q want=%q", verr1.FailureClass, tt.wantClass)
			}
			if result1.Verification == nil || result1.Verification.Failure != tt.wantClass || result1.Verification.Status != verification.StatusFail {
				t.Fatalf("verification=%#v", result1.Verification)
			}

			// Independent equivalent run: deterministic repeated ordering.
			result2, err2 := run()
			verr2 := requireTask11Error(t, err2, FreeCADRuntimeVerificationStageVerificationComparison)
			if verr2.FailureClass != tt.wantClass || result2.Verification == nil || result2.Verification.Failure != tt.wantClass {
				t.Fatalf("repeat class=%q verification=%#v", verr2.FailureClass, result2.Verification)
			}
		})
	}
}

// ===================== Part D: Task 10 short-circuit behavior =====================

func TestInvokeAndVerifyFreeCADRuntime_RuntimeFailureShortCircuitsVerification(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		return &runtimecap.Result{}, nil // missing result.json: no retry
	}}
	run, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
	requireTask11Error(t, err, FreeCADRuntimeVerificationStageRuntimeExecution)
	var runErr *FreeCADRuntimeRunError
	if !errors.As(err, &runErr) || runErr.Stage != FreeCADRuntimeRunStageResultLoad {
		t.Fatalf("task 10 cause lost: %v", err)
	}
	if run.Verification != nil {
		t.Fatalf("verification unexpectedly produced: %#v", run.Verification)
	}
	if run.Runtime.ObservationRequest.Path == "" {
		t.Fatalf("partial runtime state lost: %#v", run.Runtime)
	}
	if fake.calls != 1 {
		t.Fatalf("calls=%d", fake.calls)
	}
}

func TestInvokeAndVerifyFreeCADRuntime_ArtifactFailureShortCircuitsVerification(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		// Result declares success, but the artifact file is never written.
		task10Result(t, got, task10ManifestArtifacts(t, got), "succeeded")
		return &runtimecap.Result{}, nil
	}}
	run, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
	requireTask11Error(t, err, FreeCADRuntimeVerificationStageRuntimeExecution)
	if run.Verification != nil {
		t.Fatalf("verification unexpectedly produced: %#v", run.Verification)
	}
	if run.Runtime.Result == nil || run.Runtime.Artifacts != nil {
		t.Fatalf("run=%#v", run.Runtime)
	}
	if fake.calls != 1 {
		t.Fatalf("calls=%d", fake.calls)
	}
}

func TestInvokeAndVerifyFreeCADRuntime_ObservedFailureShortCircuitsVerification(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		artifacts := task10ManifestArtifacts(t, got)
		task10WriteArtifactFiles(t, got, artifacts, "artifact")
		task10Result(t, got, artifacts, "succeeded")
		task10RawObserved(t, got, []byte("{malformed"))
		return &runtimecap.Result{}, nil
	}}
	run, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
	requireTask11Error(t, err, FreeCADRuntimeVerificationStageRuntimeExecution)
	if run.Verification != nil {
		t.Fatalf("verification unexpectedly produced: %#v", run.Verification)
	}
	if run.Runtime.Observed != nil {
		t.Fatalf("run=%#v", run.Runtime)
	}
	if fake.calls != 1 {
		t.Fatalf("calls=%d", fake.calls)
	}
}

func TestInvokeAndVerifyFreeCADRuntime_PreservesCancellation(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	ctx, cancel := context.WithCancel(context.Background())
	fake := &task10Capability{fn: func(inner context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		cancel()
		return &runtimecap.Result{}, &runtimecap.Error{Kind: runtimecap.ErrorCancelled, Err: inner.Err()}
	}}
	run, err := InvokeAndVerifyFreeCADRuntime(ctx, fake, req)
	requireTask11Error(t, err, FreeCADRuntimeVerificationStageRuntimeExecution)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if run.Verification != nil {
		t.Fatalf("verification unexpectedly produced: %#v", run.Verification)
	}
	if fake.calls != 1 {
		t.Fatalf("calls=%d", fake.calls)
	}
}

// ===================== Part E: controlled external aligned execution =====================

func TestInvokeAndVerifyFreeCADRuntime_SucceedsWithExternalCapability(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	sum := sha256.Sum256([]byte("deterministic prepared source"))
	digest := hex.EncodeToString(sum[:])
	layout := task10Layout(t, req)
	observationRequestPath := filepath.Join(layout.WorkingCopyDir, FreeCADRuntimeObservationRequestFilename)
	artifactPath := filepath.Join(layout.WorkingCopyDir, "outputs", "Widget.step")
	observedPath := filepath.Join(layout.OutputDir, FreeCADRuntimeObservedFilename)

	observedValue := verify11BuildPassObserved(t, req, layout, runtimecap.Request{WorkingCopyDir: layout.WorkingCopyDir}, digest)
	observedJSON, err := observed.CanonicalJSON(observedValue)
	if err != nil {
		t.Fatal(err)
	}
	resultJSON := `{"schemaVersion":"1.0","status":"succeeded","artifacts":[{"id":"Body","format":"step","path":"outputs/Widget.step"}]}`

	control := t.TempDir()
	countPath := filepath.Join(control, "count")
	badPath := filepath.Join(control, "bad")
	script := fmt.Sprintf(`echo run >> %[1]q
check() { if [ "$1" != "$2" ]; then echo "$1 want $2" >> %[2]q; exit 9; fi; }
check "$#" 13
check "$1" execute
check "$2" --working-copy
check "$3" %[3]q
check "$4" --manifest
check "$5" %[4]q
check "$6" --result
check "$7" %[5]q
check "$8" --output-dir
check "$9" %[6]q
check "${10}" --observation-request
check "${11}" %[7]q
check "${12}" --reference-traversal-request
check "${13}" %[12]q
if [ -n "$PARAMETRON_MANIFEST" ] || [ -n "$PARAMETRON_OUT" ]; then echo legacy-env >> %[2]q; exit 9; fi
printf external-artifact > %[8]q
cat > %[5]q <<'RESULT_JSON'
%[9]s
RESULT_JSON
cat > %[10]q <<'OBSERVED_JSON'
%[11]s
OBSERVED_JSON
printf external-stdout
printf external-stderr >&2
exit 0
`, countPath, badPath, layout.WorkingCopyDir, layout.ManifestPath, layout.ResultPath, layout.OutputDir,
		observationRequestPath, artifactPath, resultJSON, observedPath, string(observedJSON), filepath.Join(layout.WorkingCopyDir, FreeCADReferenceTraversalRequestFilename))
	runtimePath := filepath.Join(control, "task11-runtime")
	if err := os.WriteFile(runtimePath, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	req.Executable.Command, req.Executable.Path = runtimePath, runtimePath

	// capability == nil reaches the default external runtime capability.
	run, err := InvokeAndVerifyFreeCADRuntime(context.Background(), nil, req)
	if data, readErr := os.ReadFile(badPath); readErr == nil {
		t.Fatalf("runtime invocation mismatch: %s", data)
	}
	if err != nil {
		t.Fatal(err)
	}
	count, err := os.ReadFile(countPath)
	if err != nil || string(count) != "run\n" {
		t.Fatalf("invocations=%q %v", count, err)
	}
	wantArgs := []string{"execute", "--working-copy", layout.WorkingCopyDir, "--manifest", layout.ManifestPath,
		"--result", layout.ResultPath, "--output-dir", layout.OutputDir, "--observation-request", observationRequestPath,
		"--reference-traversal-request", filepath.Join(layout.WorkingCopyDir, FreeCADReferenceTraversalRequestFilename)}
	if run.Runtime.Process == nil || run.Runtime.Process.Command.Path != runtimePath || !reflect.DeepEqual(run.Runtime.Process.Command.Args, wantArgs) {
		t.Fatalf("shell-unmediated command mismatch: %#v", run.Runtime.Process)
	}
	if run.Runtime.Process.Stdout != "external-stdout" || run.Runtime.Process.Stderr != "external-stderr" {
		t.Fatalf("stdout/stderr=%#v", run.Runtime.Process)
	}
	if run.Verification == nil || run.Verification.Status != verification.StatusPass || run.Verification.Failure != verification.FailureClassNone {
		t.Fatalf("verification=%#v", run.Verification)
	}
	if !run.Verification.Categories.Parameters.Enabled || run.Verification.Categories.Parameters.Status != verification.CategoryStatusPass {
		t.Fatalf("parameters=%#v", run.Verification.Categories.Parameters)
	}
	if !run.Verification.Categories.Metadata.Enabled || run.Verification.Categories.Metadata.Status != verification.CategoryStatusPass {
		t.Fatalf("metadata=%#v", run.Verification.Categories.Metadata)
	}
	if !run.Verification.Categories.References.Enabled || run.Verification.Categories.References.Status != verification.CategoryStatusPass {
		t.Fatalf("references=%#v", run.Verification.Categories.References)
	}
}

// ===================== Part F: in-memory comparison helper =====================

func TestVerifyFreeCADRuntimeRun_UsesOnlyInMemoryState(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	layout := task10Layout(t, req)
	run := verify11ValidRuntimeRun(t, req, layout)

	if err := os.Remove(run.ObservationRequest.Path); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(run.ObservedPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(run.ExecutionRequest.ResultPath); err != nil {
		t.Fatal(err)
	}
	for _, artifact := range run.Artifacts {
		if err := os.Remove(artifact.Path); err != nil {
			t.Fatal(err)
		}
	}

	verified, err := verifyFreeCADRuntimeRun(run)
	if err != nil {
		t.Fatalf("verification failed after file deletion: %v", err)
	}
	if verified.Verification == nil || verified.Verification.Status != verification.StatusPass {
		t.Fatalf("verification=%#v", verified.Verification)
	}
}

func TestVerifyFreeCADRuntimeRun_DoesNotInvokeRuntime(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	layout := task10Layout(t, req)
	run := verify11ValidRuntimeRun(t, req, layout)
	before := task10ProductFiles(t, req.ProductDir)

	// verifyFreeCADRuntimeRun's signature accepts no capability: it cannot
	// invoke a runtime process. The product tree confirms no new files or
	// process side effects occur during comparison.
	if _, err := verifyFreeCADRuntimeRun(run); err != nil {
		t.Fatal(err)
	}
	after := task10ProductFiles(t, req.ProductDir)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("product tree changed by comparison helper: before=%v after=%v", before, after)
	}
}

func TestVerifyFreeCADRuntimeRun_ReturnsContractInvalid(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	layout := task10Layout(t, req)
	run := verify11ValidRuntimeRun(t, req, layout)
	run.ObservationRequest.Contract.SchemaVersion = "9.9"

	verified, err := verifyFreeCADRuntimeRun(run)
	verr := requireTask11Error(t, err, FreeCADRuntimeVerificationStageVerificationComparison)
	if verr.FailureClass != verification.FailureClassContractInvalid {
		t.Fatalf("failure class=%q", verr.FailureClass)
	}
	if !errors.Is(err, verification.ErrContractInvalid) {
		t.Fatalf("sentinel missing: %v", err)
	}
	if verified.Verification == nil || verified.Verification.Status != verification.StatusFail || verified.Verification.Failure != verification.FailureClassContractInvalid {
		t.Fatalf("verification=%#v", verified.Verification)
	}
}

func TestVerifyFreeCADRuntimeRun_ReturnsObservedArtifactInvalid(t *testing.T) {
	tests := []struct {
		name         string
		edit         func(*FreeCADRuntimeRun)
		wantClass    verification.FailureClass
		wantSentinel error
	}{
		{"nil observed pointer", func(r *FreeCADRuntimeRun) {
			r.Observed = nil
		}, verification.FailureClassObservedInvalid, verification.ErrObservedInvalid},
		{"nil components field", func(r *FreeCADRuntimeRun) {
			r.Observed.Observation.Components = nil
		}, verification.FailureClassObservedInvalid, verification.ErrObservedInvalid},
		{"duplicate observed parameter", func(r *FreeCADRuntimeRun) {
			r.Observed.Observation.Parameters = append(r.Observed.Observation.Parameters, r.Observed.Observation.Parameters[0])
		}, verification.FailureClassParameterMismatch, verification.ErrParameterMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validFreeCADRuntimeExecutionRequest(t)
			layout := task10Layout(t, req)
			run := verify11ValidRuntimeRun(t, req, layout)
			tt.edit(&run)

			verified, err := verifyFreeCADRuntimeRun(run)
			verr := requireTask11Error(t, err, FreeCADRuntimeVerificationStageVerificationComparison)
			if verr.FailureClass != tt.wantClass || !errors.Is(err, tt.wantSentinel) {
				t.Fatalf("class=%q err=%v", verr.FailureClass, err)
			}
			if verified.Verification == nil || verified.Verification.Status != verification.StatusFail {
				t.Fatalf("verification=%#v", verified.Verification)
			}
		})
	}
}

// ===================== Part G: result/error-pair consistency =====================

func TestFreeCADRuntimeVerification_ResultConsistency(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	layout := task10Layout(t, req)
	baseRun := verify11ValidRuntimeRun(t, req, layout)

	passResult := &verification.Result{Status: verification.StatusPass, Message: "verification passed"}
	failResult := &verification.Result{Status: verification.StatusFail, Failure: verification.FailureClassParameterMismatch, Message: "mismatch"}
	verifyErr := &verification.VerifyError{Class: verification.FailureClassParameterMismatch, Message: "mismatch"}

	tests := []struct {
		name   string
		result *verification.Result
		err    error
	}{
		{"nil result nil error", nil, nil},
		{"fail result nil error", failResult, nil},
		{"pass result nonempty failure", &verification.Result{Status: verification.StatusPass, Failure: verification.FailureClassMetadataMismatch}, nil},
		{"unknown status nil error", &verification.Result{Status: verification.Status("unknown")}, nil},
		{"verifyerror nil result", nil, verifyErr},
		{"verifyerror pass result", passResult, verifyErr},
		{"verifyerror class differs from result", &verification.Result{Status: verification.StatusFail, Failure: verification.FailureClassMetadataMismatch}, verifyErr},
		{"non verifyerror with inconsistent result", nil, errors.New("opaque comparison failure")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateFreeCADRuntimeVerificationResult(baseRun, tt.result, tt.err)
			requireTask11Error(t, err, FreeCADRuntimeVerificationStageVerificationResult)
			if tt.err != nil && !errors.Is(err, tt.err) {
				t.Fatalf("original cause lost: %v", err)
			}
		})
	}
}

func TestFreeCADRuntimeVerification_ValidResultPairs(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	layout := task10Layout(t, req)
	baseRun := verify11ValidRuntimeRun(t, req, layout)

	t.Run("valid pass", func(t *testing.T) {
		result := &verification.Result{Status: verification.StatusPass, Message: "verification passed", Categories: verification.CategoryResults{
			Components: verification.CategoryResult{Status: verification.CategoryStatusSkipped, Message: "component verification is disabled"},
			Parameters: verification.CategoryResult{Enabled: true, Status: verification.CategoryStatusPass, Message: "verified 1 parameters"},
			Metadata:   verification.CategoryResult{Enabled: true, Status: verification.CategoryStatusPass, Message: "verified 1 metadata entries"},
			References: verification.CategoryResult{Enabled: true, Status: verification.CategoryStatusPass, Message: "verified 1 reference entries"},
		}}
		verified, err := validateFreeCADRuntimeVerificationResult(baseRun, result, nil)
		if err != nil {
			t.Fatalf("valid pass reclassified: %v", err)
		}
		if verified.Verification == nil || verified.Verification.Status != verification.StatusPass {
			t.Fatalf("verification=%#v", verified.Verification)
		}
	})
	t.Run("valid fail", func(t *testing.T) {
		result := &verification.Result{Status: verification.StatusFail, Failure: verification.FailureClassParameterMismatch, Message: "mismatch"}
		verifyErr := &verification.VerifyError{Class: verification.FailureClassParameterMismatch, Message: "mismatch"}
		verified, err := validateFreeCADRuntimeVerificationResult(baseRun, result, verifyErr)
		verr := requireTask11Error(t, err, FreeCADRuntimeVerificationStageVerificationComparison)
		if verr.FailureClass != verification.FailureClassParameterMismatch || !errors.Is(err, verifyErr) {
			t.Fatalf("valid fail reclassified: %v", err)
		}
		if verified.Verification == nil || verified.Verification.Status != verification.StatusFail {
			t.Fatalf("verification=%#v", verified.Verification)
		}
	})
}

// ===================== Part H: result-copy defensiveness =====================

func TestInvokeAndVerifyFreeCADRuntime_ReturnsIndependentVerificationResults(t *testing.T) {
	build := func(t *testing.T) FreeCADRuntimeVerifiedRun {
		req := validFreeCADRuntimeExecutionRequest(t)
		layout := task10Layout(t, req)
		fake := verify11Capability(t, verify11PassBuild(req, layout))
		run, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
		if err != nil {
			t.Fatal(err)
		}
		return run
	}
	first := build(t)
	second := build(t)
	if first.Verification == second.Verification {
		t.Fatal("verification result pointers alias")
	}
	if !reflect.DeepEqual(first.Verification, second.Verification) {
		t.Fatalf("verification content differs: %#v vs %#v", first.Verification, second.Verification)
	}
	first.Verification.Status = "mutated"
	first.Verification.Failure = "mutated"
	first.Verification.Categories.Parameters.Message = "mutated"
	if second.Verification.Status == "mutated" || second.Verification.Failure == "mutated" || second.Verification.Categories.Parameters.Message == "mutated" {
		t.Fatal("mutation leaked into independent result")
	}
}

func TestCopyFreeCADRuntimeVerificationResult(t *testing.T) {
	if got := copyFreeCADRuntimeVerificationResult(nil); got != nil {
		t.Fatalf("nil input=%#v", got)
	}

	pass := &verification.Result{Status: verification.StatusPass, Message: "verification passed", Categories: verification.CategoryResults{
		Components: verification.CategoryResult{Status: verification.CategoryStatusSkipped, Message: "disabled"},
		Parameters: verification.CategoryResult{Enabled: true, Status: verification.CategoryStatusPass, Message: "ok"},
		Metadata:   verification.CategoryResult{Enabled: true, Status: verification.CategoryStatusPass, Message: "ok"},
		References: verification.CategoryResult{Enabled: true, Status: verification.CategoryStatusPass, Message: "ok"},
	}}
	gotPass := copyFreeCADRuntimeVerificationResult(pass)
	if gotPass == pass {
		t.Fatal("copy aliases input pointer")
	}
	if !reflect.DeepEqual(*gotPass, *pass) {
		t.Fatalf("copy=%#v want=%#v", *gotPass, *pass)
	}
	pass.Status = "mutated"
	pass.Categories.Parameters.Message = "mutated"
	if gotPass.Status == "mutated" || gotPass.Categories.Parameters.Message == "mutated" {
		t.Fatal("copy aliases input state")
	}

	fail := &verification.Result{Status: verification.StatusFail, Failure: verification.FailureClassMetadataMismatch, Message: "mismatch", Categories: verification.CategoryResults{
		Metadata: verification.CategoryResult{Enabled: true, Status: verification.CategoryStatusFail, Message: "mismatch"},
	}}
	gotFail := copyFreeCADRuntimeVerificationResult(fail)
	if gotFail == fail {
		t.Fatal("copy aliases input pointer")
	}
	if !reflect.DeepEqual(*gotFail, *fail) {
		t.Fatalf("fail copy=%#v want=%#v", *gotFail, *fail)
	}
}

// ===================== Part I: input and runtime-state immutability =====================

func TestInvokeAndVerifyFreeCADRuntime_DoesNotMutateInputs(t *testing.T) {
	t.Run("pass", func(t *testing.T) {
		req := validFreeCADRuntimeExecutionRequest(t)
		wantReq := cloneFreeCADObservationRequest(req)
		layout := task10Layout(t, req)
		before, err := freecad.ComputeFreeCADRuntimeAttempt(req)
		if err != nil {
			t.Fatal(err)
		}
		fake := verify11Capability(t, verify11PassBuild(req, layout))
		if _, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req); err != nil {
			t.Fatal(err)
		}
		after, err := freecad.ComputeFreeCADRuntimeAttempt(req)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(req, wantReq) || !reflect.DeepEqual(before.Identity, after.Identity) {
			t.Fatal("request or logical identity mutated")
		}
	})
	t.Run("failure", func(t *testing.T) {
		req := validFreeCADRuntimeExecutionRequest(t)
		wantReq := cloneFreeCADObservationRequest(req)
		layout := task10Layout(t, req)
		before, err := freecad.ComputeFreeCADRuntimeAttempt(req)
		if err != nil {
			t.Fatal(err)
		}
		fake := verify11Capability(t, verify11ParameterMismatchBuild(req, layout))
		if _, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req); err == nil {
			t.Fatal("expected verification failure")
		}
		after, err := freecad.ComputeFreeCADRuntimeAttempt(req)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(req, wantReq) || !reflect.DeepEqual(before.Identity, after.Identity) {
			t.Fatal("request or logical identity mutated")
		}
	})
}

func TestVerifyFreeCADRuntimeRun_DoesNotMutateRuntimeState(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	layout := task10Layout(t, req)
	run := verify11ValidRuntimeRun(t, req, layout)
	before := fmt.Sprintf("%#v", run)

	if _, err := verifyFreeCADRuntimeRun(run); err != nil {
		t.Fatal(err)
	}
	after := fmt.Sprintf("%#v", run)
	if before != after {
		t.Fatal("runtime state mutated by comparison helper")
	}
}

// ===================== Part J: determinism =====================

func TestVerifyFreeCADRuntimeRun_IsDeterministic(t *testing.T) {
	tests := []struct {
		name    string
		reqFn   func(t *testing.T) adapter.CADRuntimeOrchestrationRequest
		buildFn func(req adapter.CADRuntimeOrchestrationRequest, layout freecad.FreeCADRuntimeWorkingCopyLayout) verify11Build
	}{
		{"no-parameter pass", verify11NoParamRequest, verify11PassBuild},
		{"explicit-parameter pass", validFreeCADRuntimeExecutionRequest, verify11PassBuild},
		{"parameter failure", validFreeCADRuntimeExecutionRequest, verify11ParameterMismatchBuild},
		{"metadata failure", validFreeCADRuntimeExecutionRequest, verify11MetadataMismatchBuild},
		{"reference failure", validFreeCADRuntimeExecutionRequest, verify11ReferenceMismatchBuild},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.reqFn(t)
			layout := task10Layout(t, req)
			fake := verify11Capability(t, tt.buildFn(req, layout))
			runtimeRun, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
			if err != nil {
				t.Fatal(err)
			}

			result1, err1 := verifyFreeCADRuntimeRun(runtimeRun)
			result2, err2 := verifyFreeCADRuntimeRun(runtimeRun)
			if (err1 == nil) != (err2 == nil) {
				t.Fatalf("error presence not deterministic: %v vs %v", err1, err2)
			}
			var stage1, stage2 string
			var class1, class2 verification.FailureClass
			var verr1, verr2 *FreeCADRuntimeVerificationError
			if errors.As(err1, &verr1) {
				stage1, class1 = verr1.Stage, verr1.FailureClass
			}
			if errors.As(err2, &verr2) {
				stage2, class2 = verr2.Stage, verr2.FailureClass
			}
			if stage1 != stage2 || class1 != class2 {
				t.Fatalf("stage/class not deterministic: %q/%q vs %q/%q", stage1, class1, stage2, class2)
			}
			if !reflect.DeepEqual(result1.Verification, result2.Verification) {
				t.Fatalf("verification result not deterministic: %#v vs %#v", result1.Verification, result2.Verification)
			}
		})
	}
}

// ===================== Part K: result and raw-output preservation =====================

func TestInvokeAndVerifyFreeCADRuntime_DoesNotRewriteRuntimeResult(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	layout := task10Layout(t, req)
	fake := verify11Capability(t, verify11ParameterMismatchBuild(req, layout))
	runtimeRun, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(layout.ResultPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifyFreeCADRuntimeRun(runtimeRun); err == nil {
		t.Fatal("expected verification failure")
	}
	after, err := os.ReadFile(layout.ResultPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("result.json changed: before=%q after=%q", before, after)
	}
}

func TestInvokeAndVerifyFreeCADRuntime_PreservesRawOutputsAfterVerificationFailure(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	layout := task10Layout(t, req)
	fake := verify11Capability(t, verify11ParameterMismatchBuild(req, layout))

	run, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
	if err == nil {
		t.Fatal("expected verification failure")
	}
	data, readErr := os.ReadFile(run.Runtime.Artifacts[0].Path)
	if readErr != nil || string(data) != "artifact:"+run.Runtime.Artifacts[0].ID {
		t.Fatalf("artifact changed: %q %v", data, readErr)
	}
	manifestData, readErr := os.ReadFile(layout.ManifestPath)
	if readErr != nil || len(manifestData) == 0 {
		t.Fatalf("manifest missing: %v", readErr)
	}
	observedData, readErr := os.ReadFile(run.Runtime.ObservedPath)
	if readErr != nil || len(observedData) == 0 {
		t.Fatalf("observed missing: %v", readErr)
	}
	requestData, readErr := os.ReadFile(run.Runtime.ObservationRequest.Path)
	if readErr != nil || !bytes.Equal(requestData, run.Runtime.ObservationRequest.JSON) {
		t.Fatalf("observation request changed: %v", readErr)
	}
	sourceData, readErr := os.ReadFile(layout.SourceDocumentPath)
	if readErr != nil || string(sourceData) != "deterministic prepared source" {
		t.Fatalf("prepared source changed: %q %v", sourceData, readErr)
	}
	// Process output remains inspectable and is not copied into error text.
	if run.Runtime.Process == nil || run.Runtime.Process.Stdout != "run-stdout" || run.Runtime.Process.Stderr != "run-stderr" {
		t.Fatalf("process output lost: %#v", run.Runtime.Process)
	}
	if strings.Contains(err.Error(), "run-stdout") || strings.Contains(err.Error(), "run-stderr") {
		t.Fatalf("error text leaks process output: %v", err)
	}
}

// ===================== Part L: legacy bridge bypass =====================

func TestInvokeAndVerifyFreeCADRuntime_BypassesLegacyObservationBridge(t *testing.T) {
	t.Setenv("PARAMETRON_MANIFEST", "/legacy/manifest.json")
	t.Setenv("PARAMETRON_OUT", "/legacy/out")

	req := validFreeCADRuntimeExecutionRequest(t)
	layout := task10Layout(t, req)

	control := t.TempDir()
	legacyObserverMarker := filepath.Join(control, "legacy-observe.marker")
	legacyObserver := filepath.Join(control, "legacy-observe.sh")
	if err := os.WriteFile(legacyObserver, []byte("#!/bin/sh\necho invoked > "+legacyObserverMarker+"\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := WriteFreeCADRuntimeObservationRequest(req); err != nil {
		t.Fatal(err)
	}
	rootLevelSentinel := filepath.Join(layout.WorkingCopyDir, FreeCADRuntimeObservedFilename)
	if err := os.WriteFile(rootLevelSentinel, []byte("{legacy-root-sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}

	fake := verify11Capability(t, verify11PassBuild(req, layout))
	run, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
	if err != nil {
		t.Fatal(err)
	}
	if run.Verification == nil || run.Verification.Status != verification.StatusPass {
		t.Fatalf("verification=%#v", run.Verification)
	}
	if _, statErr := os.Stat(legacyObserverMarker); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("legacy observer executable was invoked")
	}
	data, err := os.ReadFile(rootLevelSentinel)
	if err != nil || string(data) != "{legacy-root-sentinel" {
		t.Fatalf("root-level legacy sentinel consumed: %q %v", data, err)
	}
	if run.Runtime.ObservedPath != filepath.Join(layout.OutputDir, FreeCADRuntimeObservedFilename) {
		t.Fatalf("observed path=%q", run.Runtime.ObservedPath)
	}
	if fake.calls != 1 {
		t.Fatalf("calls=%d", fake.calls)
	}
}

func TestFreeCADRuntimeVerification_HasNoLegacyBridgeDependency(t *testing.T) {
	data, err := os.ReadFile("freecad_runtime_verification.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, forbidden := range []string{
		"engine/bridge", "bridge.", "ObserveInput", "ComputeWorkingCopyPath",
		"observe.py", "verification.LoadFile", "observed.LoadFile",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("forbidden dependency %q present", forbidden)
		}
	}
}

// ===================== Part M: no Task 12 integration =====================

func TestFreeCADRuntimeVerification_DoesNotIntegrateTask12Surfaces(t *testing.T) {
	data, err := os.ReadFile("freecad_runtime_verification.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, forbidden := range []string{
		"engine/executor", "artifact.AcceptanceContext", "integrateVerificationFailureResult",
		"RecordExisting", "RecordExistingBatch", "recordmap", "recordemit", "recordpackage",
		"engine/report", "engine/scheduler", "engine/api",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("Task 12 integration %q present", forbidden)
		}
	}

	// The returned type carries only the raw runtime state and the Engine
	// verification result: no artifact acceptance, report, or record state.
	verifiedType := reflect.TypeOf(FreeCADRuntimeVerifiedRun{})
	wantFields := []string{"Runtime", "Verification"}
	if verifiedType.NumField() != len(wantFields) {
		t.Fatalf("verified run type grew beyond raw runtime + verification state: %v", verifiedType)
	}
	for i, name := range wantFields {
		if verifiedType.Field(i).Name != name {
			t.Fatalf("field %d=%q want %q", i, verifiedType.Field(i).Name, name)
		}
	}
}

// ===================== Part N: error contract =====================

func TestFreeCADRuntimeVerificationError_ErrorAndUnwrap(t *testing.T) {
	sentinel := errors.New("sentinel")
	err := &FreeCADRuntimeVerificationError{
		Stage:        FreeCADRuntimeVerificationStageVerificationComparison,
		AttemptID:    "attempt-1",
		FailureClass: verification.FailureClassParameterMismatch,
		Err:          sentinel,
	}
	for _, want := range []string{FreeCADRuntimeVerificationStageVerificationComparison, "attempt-1", string(verification.FailureClassParameterMismatch), "sentinel"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q lacks %q", err, want)
		}
	}
	var typed *FreeCADRuntimeVerificationError
	if !errors.Is(err, sentinel) || !errors.As(err, &typed) {
		t.Fatalf("unwrap/as failed: %v", err)
	}
}

func TestFreeCADRuntimeVerification_ErrorStages(t *testing.T) {
	tests := []struct {
		name  string
		stage string
		run   func(t *testing.T) error
	}{
		{"task 10 failure", FreeCADRuntimeVerificationStageRuntimeExecution, func(t *testing.T) error {
			req := validFreeCADRuntimeExecutionRequest(t)
			fake := &task10Capability{fn: func(context.Context, runtimecap.Request) (*runtimecap.Result, error) {
				return &runtimecap.Result{}, nil
			}}
			_, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
			return err
		}},
		{"expected verification mismatch", FreeCADRuntimeVerificationStageVerificationComparison, func(t *testing.T) error {
			req := validFreeCADRuntimeExecutionRequest(t)
			layout := task10Layout(t, req)
			fake := verify11Capability(t, verify11ParameterMismatchBuild(req, layout))
			_, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
			return err
		}},
		{"invalid verification contract", FreeCADRuntimeVerificationStageVerificationComparison, func(t *testing.T) error {
			req := validFreeCADRuntimeExecutionRequest(t)
			layout := task10Layout(t, req)
			run := verify11ValidRuntimeRun(t, req, layout)
			run.ObservationRequest.Contract.SchemaVersion = "9.9"
			_, err := verifyFreeCADRuntimeRun(run)
			return err
		}},
		{"invalid observed state", FreeCADRuntimeVerificationStageVerificationComparison, func(t *testing.T) error {
			req := validFreeCADRuntimeExecutionRequest(t)
			layout := task10Layout(t, req)
			run := verify11ValidRuntimeRun(t, req, layout)
			run.Observed = nil
			_, err := verifyFreeCADRuntimeRun(run)
			return err
		}},
		{"inconsistent verification result pair", FreeCADRuntimeVerificationStageVerificationResult, func(t *testing.T) error {
			req := validFreeCADRuntimeExecutionRequest(t)
			layout := task10Layout(t, req)
			run := verify11ValidRuntimeRun(t, req, layout)
			_, err := validateFreeCADRuntimeVerificationResult(run, nil, nil)
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireTask11Error(t, tt.run(t), tt.stage)
		})
	}
}

func TestFreeCADRuntimeVerificationError_PreservesCauses(t *testing.T) {
	t.Run("task 10 run error", func(t *testing.T) {
		req := validFreeCADRuntimeExecutionRequest(t)
		fake := &task10Capability{fn: func(context.Context, runtimecap.Request) (*runtimecap.Result, error) {
			return &runtimecap.Result{}, nil
		}}
		_, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
		var runErr *FreeCADRuntimeRunError
		if !errors.As(err, &runErr) {
			t.Fatalf("task 10 error lost: %v", err)
		}
	})
	t.Run("runtimecap root cause", func(t *testing.T) {
		req := validFreeCADRuntimeExecutionRequest(t)
		root := errors.New("capability root")
		fake := &task10Capability{fn: func(context.Context, runtimecap.Request) (*runtimecap.Result, error) {
			return nil, &runtimecap.Error{Kind: runtimecap.ErrorStart, Err: root}
		}}
		_, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
		if !errors.Is(err, root) {
			t.Fatalf("runtimecap root cause lost: %v", err)
		}
	})
	t.Run("cancellation", func(t *testing.T) {
		req := validFreeCADRuntimeExecutionRequest(t)
		ctx, cancel := context.WithCancel(context.Background())
		fake := &task10Capability{fn: func(inner context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
			cancel()
			return &runtimecap.Result{}, &runtimecap.Error{Kind: runtimecap.ErrorCancelled, Err: inner.Err()}
		}}
		_, err := InvokeAndVerifyFreeCADRuntime(ctx, fake, req)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation lost: %v", err)
		}
	})
	t.Run("parameter mismatch sentinel", func(t *testing.T) {
		req := validFreeCADRuntimeExecutionRequest(t)
		layout := task10Layout(t, req)
		fake := verify11Capability(t, verify11ParameterMismatchBuild(req, layout))
		_, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
		if !errors.Is(err, verification.ErrParameterMismatch) {
			t.Fatalf("parameter mismatch sentinel lost: %v", err)
		}
	})
	t.Run("required observation missing sentinel", func(t *testing.T) {
		req := validFreeCADRuntimeExecutionRequest(t)
		layout := task10Layout(t, req)
		fake := verify11Capability(t, verify11MissingParameterBuild(req, layout))
		_, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
		if !errors.Is(err, verification.ErrRequiredObservationMissing) {
			t.Fatalf("required observation missing sentinel lost: %v", err)
		}
	})
	t.Run("metadata mismatch sentinel", func(t *testing.T) {
		req := validFreeCADRuntimeExecutionRequest(t)
		layout := task10Layout(t, req)
		fake := verify11Capability(t, verify11MetadataMismatchBuild(req, layout))
		_, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
		if !errors.Is(err, verification.ErrMetadataMismatch) {
			t.Fatalf("metadata mismatch sentinel lost: %v", err)
		}
	})
	t.Run("reference mismatch sentinel", func(t *testing.T) {
		req := validFreeCADRuntimeExecutionRequest(t)
		layout := task10Layout(t, req)
		fake := verify11Capability(t, verify11ReferenceMismatchBuild(req, layout))
		_, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
		if !errors.Is(err, verification.ErrReferenceMismatch) {
			t.Fatalf("reference mismatch sentinel lost: %v", err)
		}
	})
	t.Run("contract invalid sentinel", func(t *testing.T) {
		req := validFreeCADRuntimeExecutionRequest(t)
		layout := task10Layout(t, req)
		run := verify11ValidRuntimeRun(t, req, layout)
		run.ObservationRequest.Contract.SchemaVersion = "9.9"
		_, err := verifyFreeCADRuntimeRun(run)
		if !errors.Is(err, verification.ErrContractInvalid) {
			t.Fatalf("contract invalid sentinel lost: %v", err)
		}
	})
	t.Run("observed invalid sentinel", func(t *testing.T) {
		req := validFreeCADRuntimeExecutionRequest(t)
		layout := task10Layout(t, req)
		run := verify11ValidRuntimeRun(t, req, layout)
		run.Observed = nil
		_, err := verifyFreeCADRuntimeRun(run)
		if !errors.Is(err, verification.ErrObservedInvalid) {
			t.Fatalf("observed invalid sentinel lost: %v", err)
		}
	})
	t.Run("verify error survives", func(t *testing.T) {
		req := validFreeCADRuntimeExecutionRequest(t)
		layout := task10Layout(t, req)
		fake := verify11Capability(t, verify11ParameterMismatchBuild(req, layout))
		_, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
		var typed *verification.VerifyError
		if !errors.As(err, &typed) {
			t.Fatalf("VerifyError lost: %v", err)
		}
	})
	t.Run("joined inconsistency causes", func(t *testing.T) {
		req := validFreeCADRuntimeExecutionRequest(t)
		layout := task10Layout(t, req)
		run := verify11ValidRuntimeRun(t, req, layout)
		root := errors.New("opaque root")
		_, err := validateFreeCADRuntimeVerificationResult(run, nil, root)
		if !errors.Is(err, root) {
			t.Fatalf("joined cause lost: %v", err)
		}
	})
}

func TestFreeCADRuntimeVerificationError_DoesNotLeakRuntimeContent(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	layout := task10Layout(t, req)
	sourceSentinel := "SOURCE-SENTINEL-VALUE"
	writeObservationHostSource(t, req, []byte(sourceSentinel))
	t.Setenv("PARAMETRON_SENTINEL_ENV", "ENV-SENTINEL-VALUE")

	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		artifacts := task10ManifestArtifacts(t, got)
		for _, a := range artifacts {
			path := filepath.Join(got.WorkingCopyDir, filepath.FromSlash(a.Path))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("ARTIFACT-SENTINEL-VALUE"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		task10Result(t, got, artifacts, "succeeded")
		digest := task10PreparedSourceDigest(t, got)
		obs := verify11BuildPassObserved(t, req, layout, got, digest)
		obs.Observation.Parameters[0].Value = verify11Value(t, 4242)
		data, err := observed.CanonicalJSON(obs)
		if err != nil {
			t.Fatal(err)
		}
		task10RawObserved(t, got, data)
		return &runtimecap.Result{
			Command: runtimecap.Command{Path: got.RuntimeCommand, Args: []string{"execute"}},
			Stdout:  "STDOUT-SENTINEL-VALUE",
			Stderr:  "STDERR-SENTINEL-VALUE",
		}, nil
	}}

	_, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
	if err == nil {
		t.Fatal("expected verification failure")
	}
	for _, leaked := range []string{sourceSentinel, "ARTIFACT-SENTINEL-VALUE", "STDOUT-SENTINEL-VALUE", "STDERR-SENTINEL-VALUE", "ENV-SENTINEL-VALUE"} {
		if strings.Contains(err.Error(), leaked) {
			t.Fatalf("error leaks runtime content %q: %v", leaked, err)
		}
	}
}

// ===================== Part O: attempt context =====================

func TestFreeCADRuntimeVerificationError_UsesExistingAttemptIdentity(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	layout := task10Layout(t, req)
	fake := verify11Capability(t, verify11ParameterMismatchBuild(req, layout))
	_, err := InvokeAndVerifyFreeCADRuntime(context.Background(), fake, req)
	var verr *FreeCADRuntimeVerificationError
	if !errors.As(err, &verr) {
		t.Fatalf("error missing: %v", err)
	}
	attempt, attemptErr := freecad.ComputeFreeCADRuntimeAttempt(req)
	if attemptErr != nil {
		t.Fatal(attemptErr)
	}
	if verr.AttemptID == "" || verr.AttemptID != attempt.Identity.ID {
		t.Fatalf("attempt id=%q want=%q", verr.AttemptID, attempt.Identity.ID)
	}

	// A pre-materialization Task 10 failure leaves the attempt ID empty: no
	// new identity is derived by Task 11.
	badReq := validFreeCADRuntimeExecutionRequest(t)
	badReq.Manifest.Verification.ObservationParameterLinks = nil
	fakeBad := &task10Capability{fn: func(context.Context, runtimecap.Request) (*runtimecap.Result, error) {
		return &runtimecap.Result{}, nil
	}}
	_, badErr := InvokeAndVerifyFreeCADRuntime(context.Background(), fakeBad, badReq)
	var badVerr *FreeCADRuntimeVerificationError
	if !errors.As(badErr, &badVerr) || badVerr.AttemptID != "" || fakeBad.calls != 0 {
		t.Fatalf("expected empty attempt id and no invocation on pre-materialization failure: err=%#v calls=%d", badVerr, fakeBad.calls)
	}
}
