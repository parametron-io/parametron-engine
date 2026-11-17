package cadruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"parametron/internal/engine/adapter"
	"parametron/internal/engine/adapter/freecad"
	"parametron/internal/engine/integrity"
	"parametron/internal/engine/resourcebinding"
	"parametron/internal/engine/runtimecap"
)

var (
	integrityExecutionSource     = []byte("deterministic prepared source")
	integrityExecutionAdditional = []byte("additional baseline")
)

func sha256Text(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func integrityOperationByID(resources []integrity.ResourceOperation, logicalID string) integrity.ResourceOperation {
	for _, resource := range resources {
		if resource.LogicalID == logicalID {
			return resource
		}
	}
	return integrity.ResourceOperation{}
}

func declaredIntegrityExecutionRequest(t *testing.T) adapter.CADRuntimeOrchestrationRequest {
	t.Helper()
	req := validFreeCADRuntimeExecutionRequest(t)
	root := filepath.Dir(req.Manifest.Inputs.SourceModel)
	if err := os.WriteFile(filepath.Join(root, "additional.FCStd"), integrityExecutionAdditional, 0o644); err != nil {
		t.Fatal(err)
	}
	inventory, err := resourcebinding.New([]resourcebinding.Binding{
		{
			ProjectID: "p", Kind: resourcebinding.ResourceKindModel, LogicalID: "widget",
			SourceCoordinate: filepath.Base(req.Manifest.Inputs.SourceModel), SourceDigest: sha256Text(integrityExecutionSource),
		},
		{
			ProjectID: "p", Kind: resourcebinding.ResourceKindModel, LogicalID: "zunused",
			SourceCoordinate: "additional.FCStd", SourceDigest: sha256Text(integrityExecutionAdditional),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	req.Resources, err = resourcebinding.Select(inventory, "p", "widget", root)
	if err != nil {
		t.Fatal(err)
	}
	req.IntegrityRequestDir = filepath.Join(t.TempDir(), "integrity-root")
	return req
}

// integrityAwareRuntimeSuccess completes one aligned runtime invocation and reports
// integrity evidence derived from the Engine-authored request. It mutates the
// primary materialized resource after recording the pre-mutation baseline.
func integrityAwareRuntimeSuccess(t *testing.T, req runtimecap.Request, mode string) (*runtimecap.Result, error) {
	t.Helper()
	artifacts := task10ManifestArtifacts(t, req)
	task10WriteArtifactFiles(t, req, artifacts, "artifact")
	task10Result(t, req, artifacts, "succeeded")
	task10Observed(t, req, req.WorkingCopyDir, task10PreparedSourceDigest(t, req), false, 999)

	data, err := os.ReadFile(req.IntegrityRequestPath)
	if err != nil {
		t.Fatalf("read integrity request: %v", err)
	}
	var request FreeCADIntegrityRequest
	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatalf("decode integrity request: %v", err)
	}
	resources := make([]FreeCADIntegrityEvidenceResource, 0, len(request.Resources)+1)
	for _, requested := range request.Resources {
		path := filepath.Join(req.WorkingCopyDir, filepath.FromSlash(requested.MaterializedCoordinate))
		baseline, err := sha256File(path)
		if err != nil {
			t.Fatalf("hash materialized resource %q: %v", requested.LogicalID, err)
		}
		entry := FreeCADIntegrityEvidenceResource{
			LogicalID:              requested.LogicalID,
			MaterializedCoordinate: requested.MaterializedCoordinate,
			Status:                 FreeCADIntegrityStatusObserved,
			BaselineSHA256:         baseline,
		}
		if requested.ExpectsPersistedResult {
			if mode == "baseline_mismatch" {
				entry.BaselineSHA256 = strings.Repeat("0", 64)
			}
			if err := os.WriteFile(path, append([]byte{}, []byte("configured result bytes")...), 0o644); err != nil {
				t.Fatalf("persist primary result: %v", err)
			}
			persisted, err := sha256File(path)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "persisted_mismatch" {
				persisted = strings.Repeat("0", 64)
			}
			entry.PersistedResult = &FreeCADIntegrityPersistedResult{Coordinate: requested.MaterializedCoordinate, SHA256: persisted}
		}
		resources = append(resources, entry)
	}
	if mode == "evidence_unrequested" {
		resources = append(resources, FreeCADIntegrityEvidenceResource{
			LogicalID: "extra", MaterializedCoordinate: "source/Extra.FCStd",
			Status: FreeCADIntegrityStatusObserved, BaselineSHA256: strings.Repeat("0", 64),
		})
	}
	evidence, err := json.Marshal(FreeCADIntegrityEvidence{SchemaVersion: FreeCADIntegritySchemaVersion, Resources: resources})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(req.OutputDir, FreeCADIntegrityEvidenceFilename), evidence, 0o644); err != nil {
		t.Fatal(err)
	}
	return &runtimecap.Result{Command: runtimecap.Command{Path: req.RuntimeCommand, Args: []string{"execute"}}, Stdout: "integrity"}, nil
}

func TestInvokeAndValidateFreeCADRuntimeRejectsMaterializationMismatchBeforeInvocation(t *testing.T) {
	req := declaredIntegrityExecutionRequest(t)
	inventory, err := resourcebinding.New([]resourcebinding.Binding{
		{
			ProjectID: "p", Kind: resourcebinding.ResourceKindModel, LogicalID: "widget",
			SourceCoordinate: filepath.Base(req.Manifest.Inputs.SourceModel), SourceDigest: strings.Repeat("b", 64),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	req.Resources, err = resourcebinding.Select(inventory, "p", "widget", filepath.Dir(req.Manifest.Inputs.SourceModel))
	if err != nil {
		t.Fatal(err)
	}
	capability := &task10Capability{}

	_, err = InvokeAndValidateFreeCADRuntime(context.Background(), capability, req)
	runErr := requireTask10Error(t, err, FreeCADRuntimeRunStageMaterializationIntegrity)
	if capability.calls != 0 {
		t.Fatalf("runtime invoked %d times despite a materialization mismatch", capability.calls)
	}
	failure := integrity.AsFailure(runErr.Err)
	if failure == nil || failure.Code != integrity.CodeMaterializationMismatch {
		t.Fatalf("underlying failure = %#v, want materialization_content_mismatch", failure)
	}
	if failure.LogicalID != "widget" || failure.Coordinate != "source/Widget.FCStd" {
		t.Fatalf("failure context = %#v, want the logical resource and attempt-relative coordinate", failure)
	}
	if strings.Contains(failure.Error(), req.ProductDir) || strings.Contains(failure.Error(), os.TempDir()) {
		t.Fatalf("materialization failure leaked a host path: %q", failure.Error())
	}
}

func TestInvokeAndValidateFreeCADRuntimePublishesIntegrityRequestAndValidatesEvidence(t *testing.T) {
	req := declaredIntegrityExecutionRequest(t)
	capability := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		if got.IntegrityRequestPath == "" {
			t.Fatal("runtime did not receive an integrity request path")
		}
		return integrityAwareRuntimeSuccess(t, got, "success")
	}}
	sink := &freeCADRuntimeIntegritySink{}

	run, err := invokeAndValidateFreeCADRuntime(context.Background(), capability, req, sink)
	if err != nil {
		t.Fatalf("invokeAndValidateFreeCADRuntime returned error: %v", err)
	}
	if capability.calls != 1 {
		t.Fatalf("runtime invoked %d times, want once", capability.calls)
	}
	if got := capability.req.IntegrityRequestPath; got == "" {
		t.Fatal("IntegrityRequestPath was not transported")
	} else {
		want, err := FreeCADIntegrityRequestPath(req.IntegrityRequestDir, run.ObservationRequest.Manifest.Attempt.Identity.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("integrity request path = %q, want %q", got, want)
		}
		requestBytes, err := os.ReadFile(got)
		if err != nil {
			t.Fatalf("published integrity request is missing: %v", err)
		}
		request, err := ParseFreeCADIntegrityRequest(requestBytes)
		if err != nil {
			t.Fatalf("published integrity request is invalid: %v\n%s", err, requestBytes)
		}
		if len(request.Resources) != 2 {
			t.Fatalf("published request resources = %#v, want both declared resources", request.Resources)
		}
	}
	if sink.Operation == nil || len(sink.Operation.Resources) != 2 {
		t.Fatalf("sink operation = %#v, want both resources", sink.Operation)
	}
	primary := integrityOperationByID(sink.Operation.Resources, "widget")
	if primary.MaterializationStatus != integrity.StatusVerified ||
		primary.RuntimeBaselineStatus != integrity.StatusVerified ||
		primary.ResultStatus != integrity.StatusVerified {
		t.Fatalf("primary operation = %#v, want all stages verified", primary)
	}
	if primary.MaterializedDigest.String() != sha256Text(integrityExecutionSource) {
		t.Fatalf("materialized baseline digest = %q, want the source digest", primary.MaterializedDigest)
	}
	if primary.PersistedResultDigest == "" || primary.PersistedResultDigest == primary.MaterializedDigest {
		t.Fatalf("persisted result = %q, want a distinct configured-result digest", primary.PersistedResultDigest)
	}
	if primary.PersistedResultDigest.String() != sha256Text([]byte("configured result bytes")) {
		t.Fatalf("persisted result digest = %q, want the Engine-computed persisted bytes digest", primary.PersistedResultDigest)
	}
}

func TestInvokeAndValidateFreeCADRuntimeWithoutIntegrityRootOmitsTransport(t *testing.T) {
	req := declaredIntegrityExecutionRequest(t)
	req.IntegrityRequestDir = ""
	capability := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		if got.IntegrityRequestPath != "" {
			t.Fatalf("runtime received an integrity request path without an Engine root: %q", got.IntegrityRequestPath)
		}
		return task10Success(t, got, false)
	}}

	if _, err := InvokeAndValidateFreeCADRuntime(context.Background(), capability, req); err != nil {
		t.Fatalf("invokeAndValidateFreeCADRuntime returned error: %v", err)
	}
	if capability.calls != 1 {
		t.Fatalf("runtime invoked %d times, want once", capability.calls)
	}
}

func TestInvokeAndValidateFreeCADRuntimeClassifiesInvalidEvidence(t *testing.T) {
	tests := []struct {
		name string
		mode string
		code string
	}{
		{name: "runtime baseline mismatch", mode: "baseline_mismatch", code: integrity.CodeRuntimeBaselineMismatch},
		{name: "persisted result mismatch", mode: "persisted_mismatch", code: integrity.CodeResultMismatch},
		{name: "unrequested evidence", mode: "evidence_unrequested", code: integrity.CodeEvidenceUnrequested},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := declaredIntegrityExecutionRequest(t)
			capability := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
				return integrityAwareRuntimeSuccess(t, got, tc.mode)
			}}
			sink := &freeCADRuntimeIntegritySink{}

			_, err := invokeAndValidateFreeCADRuntime(context.Background(), capability, req, sink)
			runErr := requireTask10Error(t, err, FreeCADRuntimeRunStageIntegrityValidation)
			failure := integrity.AsFailure(runErr.Err)
			if failure == nil || failure.Code != tc.code {
				t.Fatalf("underlying failure = %#v, want %q", failure, tc.code)
			}
			if sink.Operation == nil {
				t.Fatal("rejected evidence did not report the reached identity chain")
			}
		})
	}
}

func TestInvokeAndValidateFreeCADRuntimeReportsMalformedEvidenceLoadStage(t *testing.T) {
	req := declaredIntegrityExecutionRequest(t)
	capability := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		result, err := task10Success(t, got, false)
		if err != nil {
			return result, err
		}
		if err := os.WriteFile(filepath.Join(got.OutputDir, FreeCADIntegrityEvidenceFilename), []byte(`{"schemaVersion":"9.9","resources":[]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		return result, nil
	}}

	_, err := invokeAndValidateFreeCADRuntime(context.Background(), capability, req, &freeCADRuntimeIntegritySink{})
	runErr := requireTask10Error(t, err, FreeCADRuntimeRunStageIntegrityEvidenceLoad)
	failure := integrity.AsFailure(runErr.Err)
	if failure == nil || failure.Code != integrity.CodeEvidenceSchemaUnsupported {
		t.Fatalf("underlying failure = %#v, want %q", failure, integrity.CodeEvidenceSchemaUnsupported)
	}
}

func TestIntegrityRequestEvidenceFixtureUsesDeclaredResources(t *testing.T) {
	// Guard the fixture itself: the test capability reads the Engine-authored
	// request, so the request must include exactly the declared resources.
	req := declaredIntegrityExecutionRequest(t)
	capability := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		return integrityAwareRuntimeSuccess(t, got, "success")
	}}
	if _, err := InvokeAndValidateFreeCADRuntime(context.Background(), capability, req); err != nil {
		t.Fatal(err)
	}
	freecadAttempt, err := freecad.ComputeFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(freecadAttempt.Resources.Bindings()) != 2 {
		t.Fatalf("declared resources = %#v, want two", freecadAttempt.Resources.Bindings())
	}
}
