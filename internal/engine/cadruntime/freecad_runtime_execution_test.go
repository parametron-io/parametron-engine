package cadruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"testing"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/adapter"
	"parametron/internal/engine/adapter/freecad"
	"parametron/internal/engine/observed"
	"parametron/internal/engine/runtimecap"
)

type task10Capability struct {
	calls int
	ctx   context.Context
	req   runtimecap.Request
	fn    func(context.Context, runtimecap.Request) (*runtimecap.Result, error)
}

func (f *task10Capability) Invoke(ctx context.Context, req runtimecap.Request) (*runtimecap.Result, error) {
	f.calls++
	f.ctx, f.req = ctx, req
	return f.fn(ctx, req)
}

func validFreeCADRuntimeExecutionRequest(t *testing.T) adapter.CADRuntimeOrchestrationRequest {
	t.Helper()
	req := validFreeCADObservationRequest(t)
	writeObservationHostSource(t, req, []byte("deterministic prepared source"))
	return req
}

func task10Result(t *testing.T, req runtimecap.Request, artifacts []freecad.FreeCADRuntimeResultArtifact, status string) {
	t.Helper()
	rawArtifacts := make([]map[string]string, len(artifacts))
	for i, artifact := range artifacts {
		rawArtifacts[i] = map[string]string{"id": artifact.ID, "format": artifact.Format, "path": artifact.Path}
	}
	var failure any
	if status == string(freecad.FreeCADRuntimeResultStatusFailed) {
		failure = map[string]any{"boundary": "runtime", "category": "execution", "code": "failed", "message": "controlled failure", "stage": nil}
	}
	payload := map[string]any{"schemaVersion": freecad.FreeCADRuntimeResultSchemaVersion, "status": status}
	if status == string(freecad.FreeCADRuntimeResultStatusFailed) {
		payload["failure"] = failure
	} else {
		payload["artifacts"] = rawArtifacts
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(req.ResultPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func task10BuildObserved(t *testing.T, path, digest, parameterName string, value float64) *observed.Observed {
	t.Helper()
	parameterValue, err := observed.NewValue(json.RawMessage(fmt.Sprintf("%g", value)))
	if err != nil {
		t.Fatal(err)
	}
	return &observed.Observed{
		SchemaVersion: observed.SchemaVersion,
		WorkingCopy:   observed.WorkingCopy{Path: path, SHA256: digest},
		Observation: observed.Observation{
			Parameters: []observed.Parameter{{ID: "width", Name: parameterName, GroupID: "VarSet", Value: parameterValue, ValueKind: "number"}},
			Metadata:   []observed.Metadata{}, References: []observed.Reference{}, Components: []observed.Component{},
		},
	}
}

func task10WriteObserved(t *testing.T, req runtimecap.Request, got *observed.Observed, sparse bool) {
	t.Helper()
	data, err := observed.CanonicalJSON(got)
	if err != nil {
		t.Fatal(err)
	}
	if sparse {
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatal(err)
		}
		observation := raw["observation"].(map[string]any)
		delete(observation, "metadata")
		delete(observation, "references")
		delete(observation, "components")
		data, err = json.Marshal(raw)
		if err != nil {
			t.Fatal(err)
		}
	}
	task10RawObserved(t, req, data)
}

func task10RawObserved(t *testing.T, req runtimecap.Request, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(req.OutputDir, FreeCADRuntimeObservedFilename), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func task10Observed(t *testing.T, req runtimecap.Request, path, digest string, sparse bool, value float64) {
	t.Helper()
	task10WriteObserved(t, req, task10BuildObserved(t, path, digest, "Width", value), sparse)
}

func task10PreparedSourceDigest(t *testing.T, req runtimecap.Request) string {
	t.Helper()
	digest, err := sha256File(filepath.Join(req.WorkingCopyDir, "source", "Widget.FCStd"))
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

// task10ManifestArtifacts reads the projected native manifest the runtime was
// given and returns its output declarations in declaration order.
func task10ManifestArtifacts(t *testing.T, req runtimecap.Request) []freecad.FreeCADRuntimeResultArtifact {
	t.Helper()
	data, err := os.ReadFile(req.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Outputs []struct {
			ID     string `json:"id"`
			Format string `json:"format"`
			Path   string `json:"path"`
		} `json:"outputs"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	artifacts := make([]freecad.FreeCADRuntimeResultArtifact, len(manifest.Outputs))
	for i, output := range manifest.Outputs {
		artifacts[i] = freecad.FreeCADRuntimeResultArtifact{ID: output.ID, Format: output.Format, Path: output.Path}
	}
	return artifacts
}

func task10WriteArtifactFiles(t *testing.T, req runtimecap.Request, artifacts []freecad.FreeCADRuntimeResultArtifact, marker string) {
	t.Helper()
	for _, entry := range artifacts {
		path := filepath.Join(req.WorkingCopyDir, filepath.FromSlash(entry.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(marker+":"+entry.ID), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func task10SuccessMarked(t *testing.T, req runtimecap.Request, sparse bool, marker string, value float64) (*runtimecap.Result, error) {
	t.Helper()
	artifacts := task10ManifestArtifacts(t, req)
	task10WriteArtifactFiles(t, req, artifacts, marker)
	task10Result(t, req, artifacts, "succeeded")
	task10Observed(t, req, req.WorkingCopyDir, task10PreparedSourceDigest(t, req), sparse, value)
	return &runtimecap.Result{Command: runtimecap.Command{Path: req.RuntimeCommand, Args: []string{"execute"}}, Stdout: "out:" + marker, Stderr: "err:" + marker}, nil
}

func task10Success(t *testing.T, req runtimecap.Request, sparse bool) (*runtimecap.Result, error) {
	t.Helper()
	return task10SuccessMarked(t, req, sparse, "artifact", 999)
}

func multiOutputFreeCADRuntimeExecutionRequest(t *testing.T) adapter.CADRuntimeOrchestrationRequest {
	t.Helper()
	req := validFreeCADRuntimeExecutionRequest(t)
	req.Manifest.Outputs = []planner.ExportManifestOutput{
		{Type: "step", Filename: "outputs/Widget.step", Object: "Body"},
		{Type: "pdf", Filename: "outputs/Widget.pdf"},
		{Type: "csv", Filename: "outputs/Widget.csv", Columns: []string{"name"}},
	}
	return req
}

func task10Layout(t *testing.T, req adapter.CADRuntimeOrchestrationRequest) freecad.FreeCADRuntimeWorkingCopyLayout {
	t.Helper()
	attempt, err := freecad.ComputeFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	return attempt.Layout
}

func task10ProductFiles(t *testing.T, productDir string) []string {
	t.Helper()
	var files []string
	err := filepath.Walk(productDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsDir() {
			return nil
		}
		relative, err := filepath.Rel(productDir, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	return files
}

func requireTask10Error(t *testing.T, err error, stage string) *FreeCADRuntimeRunError {
	t.Helper()
	var got *FreeCADRuntimeRunError
	if !errors.As(err, &got) || got.Stage != stage {
		t.Fatalf("error=%T %v typed=%#v want stage %q", err, err, got, stage)
	}
	return got
}

func TestInvokeAndValidateFreeCADRuntime_MapsExactExecutionRequest(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	before := cloneFreeCADObservationRequest(req)
	ctx := context.WithValue(context.Background(), struct{}{}, "ctx")
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		return task10Success(t, got, false)
	}}
	run, err := InvokeAndValidateFreeCADRuntime(ctx, fake, req)
	if err != nil {
		t.Fatal(err)
	}
	layout := run.ObservationRequest.Manifest.Attempt.Layout
	want := runtimecap.Request{RuntimeCommand: req.Executable.Path, WorkingCopyDir: layout.WorkingCopyDir,
		ManifestPath: layout.ManifestPath, ResultPath: layout.ResultPath, OutputDir: layout.OutputDir,
		ObservationRequestPath: run.ObservationRequest.Path, ReferenceTraversalRequestPath: run.ReferenceTraversalRequest.Path}
	if fake.calls != 1 || fake.ctx != ctx || fake.req != want || !reflect.DeepEqual(req, before) {
		t.Fatalf("calls=%d request=%#v want=%#v mutated=%v", fake.calls, fake.req, want, !reflect.DeepEqual(req, before))
	}
}

func TestInvokeAndValidateFreeCADRuntime_SucceedsWithFakeCapability(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		return task10Success(t, got, false)
	}}
	run, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	if err != nil {
		t.Fatal(err)
	}
	if run.Process == nil || run.Result == nil || run.Observed == nil || len(run.Artifacts) != 1 ||
		run.ObservedPath != filepath.Join(run.ExecutionRequest.OutputDir, FreeCADRuntimeObservedFilename) ||
		run.Artifacts[0].ID != "Body" || run.Artifacts[0].Format != "step" {
		t.Fatalf("incomplete run: %#v", run)
	}
	// Deliberately mismatched parameter value proves Task 10 performs no verification comparison.
	if string(run.Observed.Observation.Parameters[0].Value.Raw()) != "999" {
		t.Fatalf("observed value changed: %#v", run.Observed)
	}
}

func TestInvokeAndValidateFreeCADRuntime_InvokesExactlyOnce(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		return &runtimecap.Result{}, nil // missing result: no retry
	}}
	_, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	requireTask10Error(t, err, FreeCADRuntimeRunStageResultLoad)
	if fake.calls != 1 {
		t.Fatalf("calls=%d", fake.calls)
	}
}

func TestInvokeAndValidateFreeCADRuntime_RejectsNilContextBeforeSideEffects(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		return task10Success(t, got, false)
	}}
	//nolint:staticcheck
	_, err := InvokeAndValidateFreeCADRuntime(nil, fake, req)
	requireTask10Error(t, err, FreeCADRuntimeRunStageExecutionRequest)
	if fake.calls != 0 {
		t.Fatal("capability invoked")
	}
	if _, statErr := os.Stat(req.ProductDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("product dir side effect: %v", statErr)
	}
}

func TestInvokeAndValidateFreeCADRuntime_RequestMaterializationFailure(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	req.Manifest.Verification.ObservationParameterLinks = nil
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		return task10Success(t, got, false)
	}}
	_, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	requireTask10Error(t, err, FreeCADRuntimeRunStageRequestMaterialization)
	if fake.calls != 0 {
		t.Fatal("capability invoked")
	}
}

func TestInvokeAndValidateFreeCADRuntime_RemovesOwnedStaleRegularFiles(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	mat, err := WriteFreeCADRuntimeObservationRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{mat.Manifest.Attempt.Layout.ResultPath, filepath.Join(mat.Manifest.Attempt.Layout.OutputDir, FreeCADRuntimeObservedFilename),
		filepath.Join(mat.Manifest.Attempt.Layout.OutputDir, FreeCADRuntimeReferenceTraversalFilename),
		filepath.Join(mat.Manifest.Attempt.Layout.WorkingCopyDir, "outputs", "Widget.step")}
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("stale"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		for _, path := range paths {
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("owned stale output remains: %s: %v", path, err)
			}
		}
		return task10Success(t, got, false)
	}}
	if _, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req); err != nil {
		t.Fatal(err)
	}
}

func TestInvokeAndValidateFreeCADRuntime_PreservesUnrelatedOutputFiles(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	mat, err := WriteFreeCADRuntimeObservationRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(mat.Manifest.Attempt.Layout.OutputDir, "unrelated.bin")
	if err := os.WriteFile(unrelated, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		return task10Success(t, got, false)
	}}
	run, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(unrelated)
	if string(data) != "keep" || len(run.Artifacts) != 1 {
		t.Fatalf("unrelated=%q artifacts=%#v", data, run.Artifacts)
	}
}

func TestInvokeAndValidateFreeCADRuntime_RejectsOwnedOutputSymlinks(t *testing.T) {
	for _, which := range []string{"result", "observed", "reference-traversal", "artifact"} {
		t.Run(which, func(t *testing.T) {
			req := validFreeCADRuntimeExecutionRequest(t)
			mat, err := WriteFreeCADRuntimeObservationRequest(req)
			if err != nil {
				t.Fatal(err)
			}
			layout := mat.Manifest.Attempt.Layout
			path := map[string]string{"result": layout.ResultPath, "observed": filepath.Join(layout.OutputDir, FreeCADRuntimeObservedFilename),
				"reference-traversal": filepath.Join(layout.OutputDir, FreeCADRuntimeReferenceTraversalFilename),
				"artifact":            filepath.Join(layout.WorkingCopyDir, "outputs", "Widget.step")}[which]
			target := filepath.Join(t.TempDir(), "target")
			if err := os.WriteFile(target, []byte("safe"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Skipf("symlink unsupported: %v", err)
			}
			fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
				return task10Success(t, got, false)
			}}
			_, err = InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
			runErr := requireTask10Error(t, err, FreeCADRuntimeRunStageOutputPreparation)
			if fake.calls != 0 || runErr.Path != path {
				t.Fatalf("calls=%d error=%#v", fake.calls, runErr)
			}
			data, _ := os.ReadFile(target)
			if string(data) != "safe" {
				t.Fatalf("target modified: %q", data)
			}
		})
	}
}

func TestInvokeAndValidateFreeCADRuntime_RejectsNonRegularOwnedOutputs(t *testing.T) {
	for _, kind := range []string{"directory", "fifo"} {
		for _, which := range []string{"result", "observed", "reference-traversal", "artifact"} {
			t.Run(kind+"_"+which, func(t *testing.T) {
				req := validFreeCADRuntimeExecutionRequest(t)
				mat, err := WriteFreeCADRuntimeObservationRequest(req)
				if err != nil {
					t.Fatal(err)
				}
				layout := mat.Manifest.Attempt.Layout
				path := map[string]string{"result": layout.ResultPath, "observed": filepath.Join(layout.OutputDir, FreeCADRuntimeObservedFilename),
					"reference-traversal": filepath.Join(layout.OutputDir, FreeCADRuntimeReferenceTraversalFilename),
					"artifact":            filepath.Join(layout.WorkingCopyDir, "outputs", "Widget.step")}[which]
				switch kind {
				case "directory":
					if err := os.MkdirAll(path, 0o755); err != nil {
						t.Fatal(err)
					}
				case "fifo":
					if err := syscall.Mkfifo(path, 0o600); err != nil {
						t.Skipf("fifo unsupported: %v", err)
					}
				}
				fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
					return task10Success(t, got, false)
				}}
				_, err = InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
				runErr := requireTask10Error(t, err, FreeCADRuntimeRunStageOutputPreparation)
				if fake.calls != 0 {
					t.Fatal("process started")
				}
				if runErr.Path != path {
					t.Fatalf("failing path=%q want %q", runErr.Path, path)
				}
				// The conflicting non-regular object is rejected, not deleted.
				if _, statErr := os.Lstat(path); statErr != nil {
					t.Fatalf("conflicting object removed: %v", statErr)
				}
			})
		}
	}
}

func TestInvokeAndValidateFreeCADRuntime_ExitZeroValidatesResult(t *testing.T) {
	for _, payload := range []string{"{", `{"schemaVersion":"9","status":"succeeded","artifacts":[],"failure":null}`} {
		t.Run(payload[:1], func(t *testing.T) {
			req := validFreeCADRuntimeExecutionRequest(t)
			fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
				if err := os.WriteFile(got.ResultPath, []byte(payload), 0o600); err != nil {
					t.Fatal(err)
				}
				return &runtimecap.Result{}, nil
			}}
			_, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
			requireTask10Error(t, err, FreeCADRuntimeRunStageResultLoad)
		})
	}
}

func TestInvokeAndValidateFreeCADRuntime_ReturnsRuntimeReportedFailure(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	root := errors.New("exit root")
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		task10Result(t, got, []freecad.FreeCADRuntimeResultArtifact{}, "failed")
		result := &runtimecap.Result{Command: runtimecap.Command{Path: got.RuntimeCommand}, Stdout: "partial", Stderr: "failure"}
		return result, &runtimecap.Error{Kind: runtimecap.ErrorExit, ExitCode: 7, Stdout: result.Stdout, Stderr: result.Stderr, Err: root}
	}}
	run, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	got := requireTask10Error(t, err, FreeCADRuntimeRunStageRuntimeReportedFailure)
	if run.Process == nil || run.Result == nil || got.ProcessKind != runtimecap.ErrorExit || got.ExitCode != 7 || !errors.Is(err, root) {
		t.Fatalf("run=%#v error=%#v", run, got)
	}
}

func TestInvokeAndValidateFreeCADRuntime_NonZeroMissingResultPreservesBothCauses(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	root := errors.New("exit root")
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		return &runtimecap.Result{}, &runtimecap.Error{Kind: runtimecap.ErrorExit, ExitCode: 8, Err: root}
	}}
	_, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	requireTask10Error(t, err, FreeCADRuntimeRunStageResultLoad)
	if !errors.Is(err, root) || !errors.Is(err, freecad.ErrFreeCADRuntimeResultIO) {
		t.Fatalf("joined causes lost: %v", err)
	}
}

func TestInvokeAndValidateFreeCADRuntime_RejectsArtifactDeclarationMismatch(t *testing.T) {
	tests := []struct {
		name string
		edit func([]freecad.FreeCADRuntimeResultArtifact) []freecad.FreeCADRuntimeResultArtifact
	}{
		{"missing declaration", func(a []freecad.FreeCADRuntimeResultArtifact) []freecad.FreeCADRuntimeResultArtifact {
			return a[:len(a)-1]
		}},
		{"extra declaration", func(a []freecad.FreeCADRuntimeResultArtifact) []freecad.FreeCADRuntimeResultArtifact {
			return append(a, freecad.FreeCADRuntimeResultArtifact{ID: "extra", Format: "step", Path: "outputs/Extra.step"})
		}},
		{"reordered declarations", func(a []freecad.FreeCADRuntimeResultArtifact) []freecad.FreeCADRuntimeResultArtifact {
			a[0], a[1] = a[1], a[0]
			return a
		}},
		{"changed id", func(a []freecad.FreeCADRuntimeResultArtifact) []freecad.FreeCADRuntimeResultArtifact {
			a[0].ID = "changed"
			return a
		}},
		{"changed format", func(a []freecad.FreeCADRuntimeResultArtifact) []freecad.FreeCADRuntimeResultArtifact {
			a[0].Format = "csv"
			return a
		}},
		{"changed relative path", func(a []freecad.FreeCADRuntimeResultArtifact) []freecad.FreeCADRuntimeResultArtifact {
			a[0].Path = "outputs/Other.step"
			return a
		}},
		{"duplicate declaration replacing expected", func(a []freecad.FreeCADRuntimeResultArtifact) []freecad.FreeCADRuntimeResultArtifact {
			a[1] = a[0]
			return a
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := multiOutputFreeCADRuntimeExecutionRequest(t)
			fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
				expected := task10ManifestArtifacts(t, got)
				task10WriteArtifactFiles(t, got, expected, "artifact")
				task10Result(t, got, tt.edit(expected), "succeeded")
				return &runtimecap.Result{}, nil
			}}
			_, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
			requireTask10Error(t, err, FreeCADRuntimeRunStageArtifactValidation)
		})
	}
}

// nativeOnlyFreeCADRuntimeExecutionRequest builds a runtime execution
// request with zero declared manifest outputs, mirroring authoring-only
// outputs=["none"] planning at the runtime-orchestration boundary.
func nativeOnlyFreeCADRuntimeExecutionRequest(t *testing.T) adapter.CADRuntimeOrchestrationRequest {
	t.Helper()
	req := validFreeCADRuntimeExecutionRequest(t)
	req.Manifest.Outputs = []planner.ExportManifestOutput{}
	return req
}

// TestInvokeAndValidateFreeCADRuntime_ZeroVsZeroArtifactCorrelationSucceeds
// proves the correlation guard trivially and correctly succeeds when both
// the manifest's declared outputs and the runtime result's artifacts are
// zero-length: this is the runtime-facing counterpart to native-only
// planning, and it must not be treated as a validation-skip shortcut --
// declared-vs-observed correlation still runs, it simply has nothing to
// compare and legitimately passes.
func TestInvokeAndValidateFreeCADRuntime_ZeroVsZeroArtifactCorrelationSucceeds(t *testing.T) {
	req := nativeOnlyFreeCADRuntimeExecutionRequest(t)
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		return task10Success(t, got, false)
	}}
	run, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	if err != nil {
		t.Fatalf("expected zero-vs-zero correlation to succeed, got error: %v", err)
	}
	if run.Artifacts == nil {
		t.Fatal("expected a non-nil empty Artifacts slice")
	}
	if len(run.Artifacts) != 0 {
		t.Fatalf("expected zero validated artifacts, got %+v", run.Artifacts)
	}
}

// TestInvokeAndValidateFreeCADRuntime_ZeroManifestOutputsRejectsNonZeroResultArtifacts
// proves the manifest=[] vs result=[non-empty] mismatch remains rejected:
// zero declared outputs must not be treated as "skip validation" for
// whatever the runtime happens to report.
func TestInvokeAndValidateFreeCADRuntime_ZeroManifestOutputsRejectsNonZeroResultArtifacts(t *testing.T) {
	req := nativeOnlyFreeCADRuntimeExecutionRequest(t)
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		task10Result(t, got, []freecad.FreeCADRuntimeResultArtifact{{ID: "unexpected", Format: "step", Path: "outputs/Unexpected.step"}}, "succeeded")
		return &runtimecap.Result{}, nil
	}}
	_, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	requireTask10Error(t, err, FreeCADRuntimeRunStageArtifactValidation)
}

// TestInvokeAndValidateFreeCADRuntime_NonZeroManifestOutputsRejectsZeroResultArtifacts
// proves the mirrored mismatch: a manifest declaring a real output must
// reject a runtime result reporting zero artifacts.
func TestInvokeAndValidateFreeCADRuntime_NonZeroManifestOutputsRejectsZeroResultArtifacts(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		task10Result(t, got, []freecad.FreeCADRuntimeResultArtifact{}, "succeeded")
		return &runtimecap.Result{}, nil
	}}
	_, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	requireTask10Error(t, err, FreeCADRuntimeRunStageArtifactValidation)
}

func TestInvokeAndValidateFreeCADRuntime_RequiresObservedOutput(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		path := filepath.Join(got.OutputDir, "Widget.step")
		if err := os.WriteFile(path, []byte("artifact"), 0o600); err != nil {
			t.Fatal(err)
		}
		task10Result(t, got, []freecad.FreeCADRuntimeResultArtifact{{ID: "Body", Format: "step", Path: "outputs/Widget.step"}}, "succeeded")
		return &runtimecap.Result{}, nil
	}}
	_, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	requireTask10Error(t, err, FreeCADRuntimeRunStageObservedLoad)
}

func TestInvokeAndValidateFreeCADRuntime_CorrelatesObservedWorkingCopyPath(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		result, err := task10Success(t, got, false)
		digest, _ := sha256File(filepath.Join(got.WorkingCopyDir, "source", "Widget.FCStd"))
		task10Observed(t, got, filepath.Join(t.TempDir(), "other"), digest, false, 10)
		return result, err
	}}
	_, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	requireTask10Error(t, err, FreeCADRuntimeRunStageObservedCorrelation)
}

func TestInvokeAndValidateFreeCADRuntime_AcceptsSparseAlignedObservedOutput(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		return task10Success(t, got, true)
	}}
	run, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	if err != nil {
		t.Fatal(err)
	}
	if run.Observed.Observation.Metadata == nil || run.Observed.Observation.References == nil || run.Observed.Observation.Components == nil {
		t.Fatalf("sparse categories not normalized: %#v", run.Observed.Observation)
	}
}

func TestInvokeAndValidateFreeCADRuntime_RepeatedRunUsesFreshOwnedOutputs(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	layout := task10Layout(t, req)
	unrelated := filepath.Join(layout.OutputDir, "unrelated.bin")

	if _, err := WriteFreeCADRuntimeObservationRequest(req); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unrelated, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	firstFake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		return task10SuccessMarked(t, got, false, "one", 111)
	}}
	first, err := InvokeAndValidateFreeCADRuntime(context.Background(), firstFake, req)
	if err != nil {
		t.Fatal(err)
	}

	ownedPaths := []string{first.ExecutionRequest.ResultPath, first.ObservedPath, first.Artifacts[0].Path}
	secondFake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		// Every first-run owned output must be gone before run two writes.
		for _, path := range ownedPaths {
			if _, statErr := os.Lstat(path); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("first-run owned output remains before second run: %s: %v", path, statErr)
			}
		}
		return task10SuccessMarked(t, got, false, "two", 222)
	}}
	second, err := InvokeAndValidateFreeCADRuntime(context.Background(), secondFake, req)
	if err != nil {
		t.Fatal(err)
	}
	if firstFake.calls != 1 || secondFake.calls != 1 {
		t.Fatalf("calls=%d/%d", firstFake.calls, secondFake.calls)
	}
	if string(second.Observed.Observation.Parameters[0].Value.Raw()) != "222" {
		t.Fatalf("second observed state is not from run two: %#v", second.Observed)
	}
	if second.Process.Stdout != "out:two" || second.Result == nil || second.Result.Status != freecad.FreeCADRuntimeResultStatusSucceeded {
		t.Fatalf("second returned process/result is not from run two: %#v", second.Process)
	}
	artifactData, err := os.ReadFile(second.Artifacts[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(artifactData) != "two:Body" {
		t.Fatalf("second artifact content is not from run two: %q", artifactData)
	}
	// Unrelated output files survive both runs with unchanged bytes.
	data, err := os.ReadFile(unrelated)
	if err != nil || string(data) != "keep" {
		t.Fatalf("unrelated file changed: %q %v", data, err)
	}
}

// --- Reference traversal evidence collection ---

func task10WriteReferenceTraversal(t *testing.T, req runtimecap.Request, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(req.OutputDir, FreeCADRuntimeReferenceTraversalFilename), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestInvokeAndValidateFreeCADRuntime_ReferenceTraversalPathIsAuthoritative(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	layout := task10Layout(t, req)
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		result, err := task10Success(t, got, false)
		task10WriteReferenceTraversal(t, got, []byte(`{"schemaVersion":"2.0"}`))
		return result, err
	}}
	run, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(layout.OutputDir, FreeCADRuntimeReferenceTraversalFilename)
	if run.ReferenceTraversalPath != want {
		t.Fatalf("ReferenceTraversalPath=%q want %q", run.ReferenceTraversalPath, want)
	}
	if run.ExecutionRequest.OutputDir != layout.OutputDir {
		t.Fatalf("execution request output dir=%q is not the authoritative output dir %q", run.ExecutionRequest.OutputDir, layout.OutputDir)
	}
}

func TestInvokeAndValidateFreeCADRuntime_MissingReferenceTraversalIsCompatible(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		// The controlled runtime succeeds without producing traversal output.
		return task10Success(t, got, false)
	}}
	run, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(run.ReferenceTraversalJSON) != 0 {
		t.Fatalf("expected no traversal evidence, got %q", run.ReferenceTraversalJSON)
	}
	if run.ReferenceTraversalPath == "" {
		t.Fatal("authoritative traversal path must still be recorded even when absent")
	}
	if len(run.Artifacts) != 1 {
		t.Fatalf("normal success/artifact behavior must be preserved: %#v", run.Artifacts)
	}
}

func TestInvokeAndValidateFreeCADRuntime_StaleReferenceTraversalNotAcceptedWithoutFreshOutput(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	mat, err := WriteFreeCADRuntimeObservationRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	traversalPath := filepath.Join(mat.Manifest.Attempt.Layout.OutputDir, FreeCADRuntimeReferenceTraversalFilename)
	if err := os.MkdirAll(filepath.Dir(traversalPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(traversalPath, []byte(`{"stale":true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		if _, statErr := os.Lstat(traversalPath); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("stale traversal output remains at invocation time: %v", statErr)
		}
		// The fresh invocation does not reproduce a traversal file.
		return task10Success(t, got, false)
	}}
	run, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(run.ReferenceTraversalJSON) != 0 {
		t.Fatalf("stale traversal evidence was accepted: %q", run.ReferenceTraversalJSON)
	}
	if _, statErr := os.Lstat(traversalPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("stale traversal file was not removed before invocation")
	}
}

func TestInvokeAndValidateFreeCADRuntime_StaleReferenceTraversalReplacedByFreshOutput(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	mat, err := WriteFreeCADRuntimeObservationRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	traversalPath := filepath.Join(mat.Manifest.Attempt.Layout.OutputDir, FreeCADRuntimeReferenceTraversalFilename)
	if err := os.MkdirAll(filepath.Dir(traversalPath), 0o755); err != nil {
		t.Fatal(err)
	}
	stale := []byte(`{"stale":true}`)
	if err := os.WriteFile(traversalPath, stale, 0o600); err != nil {
		t.Fatal(err)
	}

	fresh := []byte(`{"schemaVersion":"2.0","kind":"reference-traversal","boundary":"internal","operation":"resolve","status":"succeeded","sourceDocument":"Widget.FCStd","nodes":[],"edges":[],"diagnostics":[]}`)
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		result, err := task10Success(t, got, false)
		task10WriteReferenceTraversal(t, got, fresh)
		return result, err
	}}
	run, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(run.ReferenceTraversalJSON, fresh) {
		t.Fatalf("returned bytes=%q want fresh bytes=%q", run.ReferenceTraversalJSON, fresh)
	}
	if bytes.Equal(run.ReferenceTraversalJSON, stale) {
		t.Fatal("stale traversal bytes leaked into the returned run")
	}
}

func TestInvokeAndValidateFreeCADRuntime_RejectsUnsafeFreshReferenceTraversalOutput(t *testing.T) {
	tests := []struct {
		name  string
		write func(t *testing.T, path string)
	}{
		{"symlink", func(t *testing.T, path string) {
			target := filepath.Join(t.TempDir(), "target")
			if err := os.WriteFile(target, []byte("safe"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Skipf("symlink unsupported: %v", err)
			}
		}},
		{"directory", func(t *testing.T, path string) {
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{"empty", func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validFreeCADRuntimeExecutionRequest(t)
			layout := task10Layout(t, req)
			traversalPath := filepath.Join(layout.OutputDir, FreeCADRuntimeReferenceTraversalFilename)
			fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
				result, err := task10Success(t, got, false)
				tt.write(t, traversalPath)
				return result, err
			}}
			_, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
			requireTask10Error(t, err, FreeCADRuntimeRunStageReferenceTraversalLoad)
		})
	}
}

func TestInvokeAndValidateFreeCADRuntime_RejectsUnreadableReferenceTraversalOutput(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("unreadable-file semantics do not apply when running as root")
	}
	req := validFreeCADRuntimeExecutionRequest(t)
	layout := task10Layout(t, req)
	traversalPath := filepath.Join(layout.OutputDir, FreeCADRuntimeReferenceTraversalFilename)
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		result, err := task10Success(t, got, false)
		if err != nil {
			return result, err
		}
		if writeErr := os.WriteFile(traversalPath, []byte(`{"schemaVersion":"2.0"}`), 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
		if chmodErr := os.Chmod(traversalPath, 0o000); chmodErr != nil {
			t.Fatal(chmodErr)
		}
		return result, err
	}}
	_, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	requireTask10Error(t, err, FreeCADRuntimeRunStageReferenceTraversalLoad)
	_ = os.Chmod(traversalPath, 0o600)
}

func TestInvokeAndValidateFreeCADRuntime_PreservesExactReferenceTraversalBytes(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	raw := []byte("{\n  \"schemaVersion\": \"2.0\",\n  \"status\":   \"succeeded\",\n  \"nodes\": [],\n  \"edges\": []\n}\n")
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		result, err := task10Success(t, got, false)
		task10WriteReferenceTraversal(t, got, raw)
		return result, err
	}}
	run, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(run.ReferenceTraversalJSON, raw) {
		t.Fatalf("bytes not preserved exactly:\ngot:  %q\nwant: %q", run.ReferenceTraversalJSON, raw)
	}
}

func TestInvokeAndValidateFreeCADRuntime_PreservesPartialRuntimeEvidence(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	var partial string
	root := errors.New("exit")
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		partial = filepath.Join(got.OutputDir, "partial.bin")
		if err := os.WriteFile(partial, []byte("evidence"), 0o600); err != nil {
			t.Fatal(err)
		}
		return &runtimecap.Result{}, &runtimecap.Error{Kind: runtimecap.ErrorExit, ExitCode: 2, Err: root}
	}}
	_, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	requireTask10Error(t, err, FreeCADRuntimeRunStageResultLoad)
	data, readErr := os.ReadFile(partial)
	if readErr != nil || string(data) != "evidence" {
		t.Fatalf("evidence rolled back: %q %v", data, readErr)
	}
}

func TestFreeCADRuntimeRunError_ErrorAndUnwrap(t *testing.T) {
	sentinel := errors.New("sentinel")
	err := &FreeCADRuntimeRunError{Stage: "stage", Field: "field", AttemptID: "attempt", Path: "/safe/path",
		ProcessKind: "exit", ExitCode: 17, Err: sentinel}
	for _, want := range []string{"stage", "field", "attempt", "/safe/path", "exit", "17"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q lacks %q", err, want)
		}
	}
	var typed *FreeCADRuntimeRunError
	if !errors.Is(err, sentinel) || !errors.As(err, &typed) {
		t.Fatalf("unwrap/as failed: %v", err)
	}
}

func TestInvokeAndValidateFreeCADRuntime_DoesNotUseLegacyBridge(t *testing.T) {
	data, err := os.ReadFile("freecad_runtime_execution.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"verification.Verify", "observed.LinkToSemantic", "bridge.", "RunPython", "OrchestrateCADRuntime", "RecordExisting"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("later/legacy integration %q present", forbidden)
		}
	}

	// Behavioral proof: legacy env bridge variables and a legacy root-level
	// observed marker are ignored by the aligned Task 10 path.
	t.Setenv("PARAMETRON_MANIFEST", "/legacy/manifest.json")
	t.Setenv("PARAMETRON_OUT", "/legacy/out")
	req := validFreeCADRuntimeExecutionRequest(t)
	layout := task10Layout(t, req)
	if _, err := WriteFreeCADRuntimeObservationRequest(req); err != nil {
		t.Fatal(err)
	}
	legacyMarker := filepath.Join(layout.WorkingCopyDir, FreeCADRuntimeObservedFilename)
	if err := os.WriteFile(legacyMarker, []byte("{legacy-bridge-marker"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		return task10Success(t, got, false)
	}}
	run, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	if err != nil {
		t.Fatal(err)
	}
	if run.ObservedPath != filepath.Join(layout.OutputDir, FreeCADRuntimeObservedFilename) {
		t.Fatalf("observed path=%q", run.ObservedPath)
	}
	marker, err := os.ReadFile(legacyMarker)
	if err != nil || string(marker) != "{legacy-bridge-marker" {
		t.Fatalf("legacy marker consumed: %q %v", marker, err)
	}
}

// --- Controlled external Task 10 execution ---

func TestInvokeAndValidateFreeCADRuntime_SucceedsWithExternalCapability(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	sum := sha256.Sum256([]byte("deterministic prepared source"))
	digest := hex.EncodeToString(sum[:])
	layout := task10Layout(t, req)
	observationRequestPath := filepath.Join(layout.WorkingCopyDir, FreeCADRuntimeObservationRequestFilename)
	artifactPath := filepath.Join(layout.WorkingCopyDir, "outputs", "Widget.step")
	observedPath := filepath.Join(layout.OutputDir, FreeCADRuntimeObservedFilename)

	observedJSON, err := observed.CanonicalJSON(task10BuildObserved(t, layout.WorkingCopyDir, digest, "Width", 999))
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
	runtimePath := filepath.Join(control, "task10-runtime")
	if err := os.WriteFile(runtimePath, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	req.Executable.Command, req.Executable.Path = runtimePath, runtimePath

	// capability == nil reaches the default external runtime capability.
	run, err := InvokeAndValidateFreeCADRuntime(context.Background(), nil, req)
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
	if run.Process == nil || run.Process.Command.Path != runtimePath || !reflect.DeepEqual(run.Process.Command.Args, wantArgs) {
		t.Fatalf("shell-unmediated command mismatch: %#v", run.Process)
	}
	if run.Process.Stdout != "external-stdout" || run.Process.Stderr != "external-stderr" {
		t.Fatalf("stdout/stderr=%#v", run.Process)
	}
	if run.Result == nil || run.Result.Status != freecad.FreeCADRuntimeResultStatusSucceeded || run.Result.Failure != nil {
		t.Fatalf("result=%#v", run.Result)
	}
	wantArtifacts := []FreeCADRuntimeValidatedArtifact{{ID: "Body", Format: "step", RelativePath: "outputs/Widget.step", Path: artifactPath}}
	if !reflect.DeepEqual(run.Artifacts, wantArtifacts) {
		t.Fatalf("artifacts=%#v", run.Artifacts)
	}
	if run.ObservedPath != observedPath || run.Observed == nil ||
		run.Observed.WorkingCopy.Path != layout.WorkingCopyDir || run.Observed.WorkingCopy.SHA256 != digest ||
		string(run.Observed.Observation.Parameters[0].Value.Raw()) != "999" {
		t.Fatalf("observed=%#v path=%q", run.Observed, run.ObservedPath)
	}
	data, err := os.ReadFile(artifactPath)
	if err != nil || string(data) != "external-artifact" {
		t.Fatalf("artifact=%q %v", data, err)
	}
}

// --- Execution-request consistency stage ---

func TestValidateFreeCADRuntimeExecutionRequest_RejectsInconsistentRequests(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	mat, err := WriteFreeCADRuntimeObservationRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	layout := mat.Manifest.Attempt.Layout
	base := freecad.FreeCADRuntimeExecutionRequest{
		RuntimeCommand:                req.Executable.Path,
		WorkingCopyDir:                layout.WorkingCopyDir,
		ManifestPath:                  layout.ManifestPath,
		ResultPath:                    layout.ResultPath,
		OutputDir:                     layout.OutputDir,
		ObservationRequestPath:        mat.Path,
		ReferenceTraversalRequestPath: filepath.Join(layout.WorkingCopyDir, FreeCADReferenceTraversalRequestFilename),
	}
	if _, _, field, err := validateFreeCADRuntimeExecutionRequest(base, req.Executable.Path, mat); err != nil || field != "" {
		t.Fatalf("baseline invalid: field=%q err=%v", field, err)
	}
	tests := []struct {
		field string
		edit  func(*freecad.FreeCADRuntimeExecutionRequest)
	}{
		{"RuntimeCommand", func(r *freecad.FreeCADRuntimeExecutionRequest) { r.RuntimeCommand = "/other-runtime" }},
		{"WorkingCopyDir", func(r *freecad.FreeCADRuntimeExecutionRequest) {
			r.WorkingCopyDir = filepath.Dir(layout.WorkingCopyDir)
		}},
		{"ManifestPath", func(r *freecad.FreeCADRuntimeExecutionRequest) {
			r.ManifestPath = filepath.Join(layout.WorkingCopyDir, "other_manifest.json")
		}},
		{"ResultPath", func(r *freecad.FreeCADRuntimeExecutionRequest) {
			r.ResultPath = filepath.Join(layout.WorkingCopyDir, "other_result.json")
		}},
		{"OutputDir", func(r *freecad.FreeCADRuntimeExecutionRequest) {
			r.OutputDir = filepath.Join(layout.WorkingCopyDir, "alt-outputs")
		}},
		{"ObservationRequestPath", func(r *freecad.FreeCADRuntimeExecutionRequest) {
			r.ObservationRequestPath = filepath.Join(layout.WorkingCopyDir, "other_request.json")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			mutated := base
			tt.edit(&mutated)
			_, _, field, err := validateFreeCADRuntimeExecutionRequest(mutated, req.Executable.Path, mat)
			if err == nil || field != tt.field {
				t.Fatalf("field=%q err=%v", field, err)
			}
		})
	}
}

func TestValidateFreeCADRuntimeExecutionRequest_DefensivePathBoundaries(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "task10-defensive")
	makeLayout := func() freecad.FreeCADRuntimeWorkingCopyLayout {
		wcd := filepath.Join(root, "wc")
		return freecad.FreeCADRuntimeWorkingCopyLayout{
			WorkingCopyDir:        wcd,
			SourceDocumentPath:    filepath.Join(wcd, "source", "Widget.FCStd"),
			ManifestPath:          filepath.Join(wcd, "runtime_manifest.json"),
			ResultPath:            filepath.Join(wcd, "cad_result.json"),
			OutputDir:             filepath.Join(wcd, "outputs"),
			OutputDirRelativePath: "outputs",
		}
	}
	tests := []struct {
		name      string
		layout    func() freecad.FreeCADRuntimeWorkingCopyLayout
		outputs   []freecad.FreeCADRuntimeManifestOutput
		wantField string
		wantText  string
	}{
		{"traversal", makeLayout,
			[]freecad.FreeCADRuntimeManifestOutput{{ID: "a", Format: "step", Path: "../escape.step"}},
			"Manifest.Outputs[0].Path", "unsafe segment"},
		{"non-canonical traversal", makeLayout,
			[]freecad.FreeCADRuntimeManifestOutput{{ID: "a", Format: "step", Path: "outputs/../escape.step"}},
			"Manifest.Outputs[0].Path", "canonical slash-separated"},
		{"absolute outside working copy", makeLayout,
			[]freecad.FreeCADRuntimeManifestOutput{{ID: "a", Format: "step", Path: "/escape.step"}},
			"Manifest.Outputs[0].Path", "canonical slash-separated"},
		{"outside output dir", makeLayout,
			[]freecad.FreeCADRuntimeManifestOutput{{ID: "a", Format: "step", Path: "elsewhere/escape.step"}},
			"Manifest.Outputs[0].Path", "strictly contained"},
		{"collision with result path", func() freecad.FreeCADRuntimeWorkingCopyLayout {
			layout := makeLayout()
			layout.ResultPath = filepath.Join(layout.OutputDir, "cad_result.json")
			return layout
		}, []freecad.FreeCADRuntimeManifestOutput{{ID: "a", Format: "step", Path: "outputs/cad_result.json"}},
			"Manifest.Outputs[0].Path", "collides with ResultPath"},
		{"collision with observed path", makeLayout,
			[]freecad.FreeCADRuntimeManifestOutput{{ID: "a", Format: "step", Path: "outputs/" + FreeCADRuntimeObservedFilename}},
			"Manifest.Outputs[0].Path", "collides with ObservedPath"},
		{"collision with reference traversal path", makeLayout,
			[]freecad.FreeCADRuntimeManifestOutput{{ID: "a", Format: "step", Path: "outputs/" + FreeCADRuntimeReferenceTraversalFilename}},
			"Manifest.Outputs[0].Path", "collides with ReferenceTraversalPath"},
		{"duplicate artifact path", makeLayout,
			[]freecad.FreeCADRuntimeManifestOutput{
				{ID: "a", Format: "step", Path: "outputs/Widget.step"},
				{ID: "b", Format: "step", Path: "outputs/Widget.step"},
			},
			"Manifest.Outputs[1].Path", "duplicates"},
		{"authoritative path collision", func() freecad.FreeCADRuntimeWorkingCopyLayout {
			layout := makeLayout()
			layout.ResultPath = layout.ManifestPath
			return layout
		}, []freecad.FreeCADRuntimeManifestOutput{{ID: "a", Format: "step", Path: "outputs/Widget.step"}},
			"Paths", "collide"},
		{"output dir outside working copy", func() freecad.FreeCADRuntimeWorkingCopyLayout {
			layout := makeLayout()
			layout.OutputDir = filepath.Join(root, "outputs")
			return layout
		}, []freecad.FreeCADRuntimeManifestOutput{{ID: "a", Format: "step", Path: "outputs/Widget.step"}},
			"OutputDir", "strictly contained"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			layout := tt.layout()
			mat := FreeCADRuntimeObservationRequestMaterialization{
				Manifest: freecad.FreeCADRuntimeManifestMaterialization{
					Attempt:  freecad.FreeCADRuntimeAttempt{Layout: layout},
					Manifest: freecad.FreeCADRuntimeExportManifest{Outputs: tt.outputs},
				},
				Path: filepath.Join(layout.WorkingCopyDir, FreeCADRuntimeObservationRequestFilename),
			}
			execReq := freecad.FreeCADRuntimeExecutionRequest{
				RuntimeCommand:                "/runtime",
				WorkingCopyDir:                layout.WorkingCopyDir,
				ManifestPath:                  layout.ManifestPath,
				ResultPath:                    layout.ResultPath,
				OutputDir:                     layout.OutputDir,
				ObservationRequestPath:        mat.Path,
				ReferenceTraversalRequestPath: filepath.Join(layout.WorkingCopyDir, FreeCADReferenceTraversalRequestFilename),
			}
			_, _, field, err := validateFreeCADRuntimeExecutionRequest(execReq, "/runtime", mat)
			if err == nil || field != tt.wantField || !strings.Contains(err.Error(), tt.wantText) {
				t.Fatalf("field=%q err=%v", field, err)
			}
		})
	}
}

// --- Filesystem safety ---

func TestInvokeAndValidateFreeCADRuntime_RejectsSymlinkedWorkingCopyRoot(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	mat, err := WriteFreeCADRuntimeObservationRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	layout := mat.Manifest.Attempt.Layout
	external := filepath.Join(t.TempDir(), "external-root")
	if err := os.Rename(layout.WorkingCopyDir, external); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, layout.WorkingCopyDir); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	externalBefore := task10ProductFiles(t, external)
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		return task10Success(t, got, false)
	}}
	_, err = InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	runErr := requireTask10Error(t, err, FreeCADRuntimeRunStageOutputPreparation)
	if fake.calls != 0 {
		t.Fatal("capability invoked despite symlinked working-copy root")
	}
	if !strings.Contains(runErr.Error(), "symlink") {
		t.Fatalf("error=%v", runErr)
	}
	// The external target is not traversed, modified, or recursively removed.
	if !reflect.DeepEqual(task10ProductFiles(t, external), externalBefore) {
		t.Fatalf("external target changed: %v want %v", task10ProductFiles(t, external), externalBefore)
	}
	if info, statErr := os.Lstat(layout.WorkingCopyDir); statErr != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink removed: %v %v", statErr, info)
	}
}

func TestInvokeAndValidateFreeCADRuntime_RejectsSymlinkedOutputPathComponents(t *testing.T) {
	nestedRequest := func(t *testing.T) adapter.CADRuntimeOrchestrationRequest {
		req := validFreeCADRuntimeExecutionRequest(t)
		req.Manifest.Outputs = []planner.ExportManifestOutput{{Type: "step", Filename: "outputs/nested/Widget.step", Object: "Body"}}
		return req
	}

	t.Run("stale artifact cleanup", func(t *testing.T) {
		req := nestedRequest(t)
		mat, err := WriteFreeCADRuntimeObservationRequest(req)
		if err != nil {
			t.Fatal(err)
		}
		layout := mat.Manifest.Attempt.Layout
		external := t.TempDir()
		stale := filepath.Join(external, "Widget.step")
		if err := os.WriteFile(stale, []byte("safe"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(external, filepath.Join(layout.OutputDir, "nested")); err != nil {
			t.Skipf("symlink unsupported: %v", err)
		}
		fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
			return task10Success(t, got, false)
		}}
		_, err = InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
		requireTask10Error(t, err, FreeCADRuntimeRunStageOutputPreparation)
		if fake.calls != 0 {
			t.Fatal("capability invoked despite symlinked path component")
		}
		data, readErr := os.ReadFile(stale)
		if readErr != nil || string(data) != "safe" {
			t.Fatalf("external target read or removed: %q %v", data, readErr)
		}
	})

	t.Run("post-process artifact validation", func(t *testing.T) {
		req := nestedRequest(t)
		external := t.TempDir()
		fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
			if err := os.WriteFile(filepath.Join(external, "Widget.step"), []byte("safe"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(external, filepath.Join(got.OutputDir, "nested")); err != nil {
				t.Skipf("symlink unsupported: %v", err)
			}
			task10Result(t, got, task10ManifestArtifacts(t, got), "succeeded")
			return &runtimecap.Result{Stdout: "raw"}, nil
		}}
		run, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
		requireTask10Error(t, err, FreeCADRuntimeRunStageArtifactValidation)
		// Raw outputs are preserved for inspection; the target is not followed.
		if run.Process == nil || run.Process.Stdout != "raw" || run.Result == nil {
			t.Fatalf("raw process/result state lost: %#v", run)
		}
		data, readErr := os.ReadFile(filepath.Join(external, "Widget.step"))
		if readErr != nil || string(data) != "safe" {
			t.Fatalf("external target changed: %q %v", data, readErr)
		}
		if _, statErr := os.ReadFile(run.ExecutionRequest.ResultPath); statErr != nil {
			t.Fatalf("raw result removed: %v", statErr)
		}
	})

	t.Run("post-process observed load", func(t *testing.T) {
		// A symlinked component between the working copy and the observed file is
		// only constructible against the package-private loader because every
		// public run also validates artifacts under the same output directory.
		root := t.TempDir()
		external := t.TempDir()
		digest := strings.Repeat("ab", 32)
		data, err := observed.CanonicalJSON(task10BuildObserved(t, root, digest, "Width", 1))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(external, FreeCADRuntimeObservedFilename), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(external, filepath.Join(root, "outputs")); err != nil {
			t.Skipf("symlink unsupported: %v", err)
		}
		observedPath := filepath.Join(root, "outputs", FreeCADRuntimeObservedFilename)
		_, err = loadRegularFreeCADRuntimeObserved(root, observedPath)
		if err == nil || !errors.Is(err, observed.ErrIO) || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("symlinked component followed: %v", err)
		}
		after, readErr := os.ReadFile(filepath.Join(external, FreeCADRuntimeObservedFilename))
		if readErr != nil || !bytes.Equal(after, data) {
			t.Fatalf("external observed target changed: %v", readErr)
		}
	})
}

// --- Process/result correlation matrix ---

func TestInvokeAndValidateFreeCADRuntime_RejectsFailedResultAfterSuccessfulProcess(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		task10Result(t, got, []freecad.FreeCADRuntimeResultArtifact{}, "failed")
		return &runtimecap.Result{Stdout: "zero-exit"}, nil
	}}
	run, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	got := requireTask10Error(t, err, FreeCADRuntimeRunStageProcessResultCorrelation)
	if run.Process == nil || run.Process.Stdout != "zero-exit" ||
		run.Result == nil || run.Result.Status != freecad.FreeCADRuntimeResultStatusFailed ||
		got.ProcessKind != "" || got.ExitCode != 0 {
		t.Fatalf("run=%#v error=%#v", run, got)
	}
}

func TestInvokeAndValidateFreeCADRuntime_RejectsSucceededResultAfterNonZeroExit(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	root := errors.New("exit root")
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		task10Result(t, got, []freecad.FreeCADRuntimeResultArtifact{}, "succeeded")
		return &runtimecap.Result{}, &runtimecap.Error{Kind: runtimecap.ErrorExit, ExitCode: 5, Err: root}
	}}
	run, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	got := requireTask10Error(t, err, FreeCADRuntimeRunStageProcessResultCorrelation)
	if got.ProcessKind != runtimecap.ErrorExit || got.ExitCode != 5 || !errors.Is(err, root) {
		t.Fatalf("process error lost: %#v %v", got, err)
	}
	if run.Result == nil || run.Result.Status != freecad.FreeCADRuntimeResultStatusSucceeded {
		t.Fatalf("succeeded result not inspectable: %#v", run.Result)
	}
}

func TestInvokeAndValidateFreeCADRuntime_NonZeroExitInvalidResultMatrix(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		decode  bool
	}{
		{"malformed json", "{", true},
		{"unknown field", `{"schemaVersion":"1.0","status":"succeeded","artifacts":[],"bogus":1}`, false},
		{"unsupported schema", `{"schemaVersion":"9.9","status":"succeeded","artifacts":[]}`, false},
		{"invalid status", `{"schemaVersion":"1.0","status":"partial","artifacts":[]}`, false},
		{"invalid shape", `{"schemaVersion":"1.0","status":"failed","failure":null}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validFreeCADRuntimeExecutionRequest(t)
			root := errors.New("exit root")
			fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
				if err := os.WriteFile(got.ResultPath, []byte(tt.payload), 0o600); err != nil {
					t.Fatal(err)
				}
				return &runtimecap.Result{}, &runtimecap.Error{Kind: runtimecap.ErrorExit, ExitCode: 3, Err: root}
			}}
			_, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
			got := requireTask10Error(t, err, FreeCADRuntimeRunStageResultLoad)
			if got.ProcessKind != runtimecap.ErrorExit || got.ExitCode != 3 || !errors.Is(err, root) {
				t.Fatalf("process cause lost: %#v %v", got, err)
			}
			if tt.decode {
				var decodeErr *freecad.FreeCADRuntimeResultDecodeError
				if !errors.As(err, &decodeErr) || !errors.Is(err, freecad.ErrFreeCADRuntimeResultDecode) {
					t.Fatalf("decode cause lost: %v", err)
				}
			} else {
				var validationErr *freecad.FreeCADRuntimeResultValidationError
				if !errors.As(err, &validationErr) || !errors.Is(err, freecad.ErrFreeCADRuntimeResultValidation) {
					t.Fatalf("validation cause lost: %v", err)
				}
			}
		})
	}
}

// --- Task 10 process-invocation failure matrix ---

func TestInvokeAndValidateFreeCADRuntime_ProcessInvocationFailures(t *testing.T) {
	tests := []struct {
		name     string
		err      func(root error) error
		wantKind string
	}{
		{"invalid invocation", func(root error) error {
			return &runtimecap.Error{Kind: runtimecap.ErrorInvalid, Err: root}
		}, runtimecap.ErrorInvalid},
		{"start failure", func(root error) error {
			return &runtimecap.Error{Kind: runtimecap.ErrorStart, ExitCode: -1, Err: root}
		}, runtimecap.ErrorStart},
		{"unknown capability error", func(root error) error { return root }, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validFreeCADRuntimeExecutionRequest(t)
			root := errors.New("capability root")
			fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
				return nil, tt.err(root)
			}}
			_, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
			got := requireTask10Error(t, err, FreeCADRuntimeRunStageProcessInvocation)
			if got.ProcessKind != tt.wantKind || !errors.Is(err, root) || fake.calls != 1 {
				t.Fatalf("calls=%d error=%#v cause=%v", fake.calls, got, err)
			}
		})
	}
}

func TestInvokeAndValidateFreeCADRuntime_PreservesCancellation(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	ctx, cancel := context.WithCancel(context.Background())
	fake := &task10Capability{fn: func(inner context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		cancel()
		return &runtimecap.Result{}, &runtimecap.Error{Kind: runtimecap.ErrorCancelled, Err: inner.Err()}
	}}
	run, err := InvokeAndValidateFreeCADRuntime(ctx, fake, req)
	got := requireTask10Error(t, err, FreeCADRuntimeRunStageProcessInvocation)
	if got.ProcessKind != runtimecap.ErrorCancelled || !errors.Is(err, context.Canceled) || fake.calls != 1 {
		t.Fatalf("calls=%d error=%#v cause=%v", fake.calls, got, err)
	}
	// Task 9 inputs remain; no result is required.
	layout := run.ObservationRequest.Manifest.Attempt.Layout
	for _, path := range []string{layout.SourceDocumentPath, layout.ManifestPath, run.ObservationRequest.Path} {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("Task 9 input lost: %v", statErr)
		}
	}
	if run.Result != nil {
		t.Fatalf("result unexpectedly loaded: %#v", run.Result)
	}
}

// --- Artifact validation ---

func TestInvokeAndValidateFreeCADRuntime_ValidatesArtifactsInDeclarationOrder(t *testing.T) {
	req := multiOutputFreeCADRuntimeExecutionRequest(t)
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		return task10Success(t, got, false)
	}}
	run, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	if err != nil {
		t.Fatal(err)
	}
	layout := run.ObservationRequest.Manifest.Attempt.Layout
	want := []FreeCADRuntimeValidatedArtifact{
		{ID: "Body", Format: "step", RelativePath: "outputs/Widget.step", Path: filepath.Join(layout.WorkingCopyDir, "outputs", "Widget.step")},
		{ID: "Widget-pdf", Format: "pdf", RelativePath: "outputs/Widget.pdf", Path: filepath.Join(layout.WorkingCopyDir, "outputs", "Widget.pdf")},
		{ID: "Widget-csv", Format: "csv", RelativePath: "outputs/Widget.csv", Path: filepath.Join(layout.WorkingCopyDir, "outputs", "Widget.csv")},
	}
	if !reflect.DeepEqual(run.Artifacts, want) {
		t.Fatalf("artifacts=%#v want=%#v", run.Artifacts, want)
	}
	// The returned order and metadata mirror the projected native manifest.
	outputs := run.ObservationRequest.Manifest.Manifest.Outputs
	if len(outputs) != len(run.Artifacts) {
		t.Fatalf("outputs=%d artifacts=%d", len(outputs), len(run.Artifacts))
	}
	for i, output := range outputs {
		got := run.Artifacts[i]
		if got.ID != output.ID || got.Format != output.Format || got.RelativePath != output.Path ||
			got.Path != filepath.Join(layout.WorkingCopyDir, filepath.FromSlash(output.Path)) {
			t.Fatalf("artifacts[%d]=%#v output=%#v", i, got, output)
		}
	}
}

func TestInvokeAndValidateFreeCADRuntime_RequiresDeclaredArtifactFiles(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		// Exact declaration, but the artifact file is never written.
		task10Result(t, got, task10ManifestArtifacts(t, got), "succeeded")
		return &runtimecap.Result{}, nil
	}}
	run, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	requireTask10Error(t, err, FreeCADRuntimeRunStageArtifactValidation)
	if run.Result == nil || run.Artifacts != nil {
		t.Fatalf("run=%#v", run)
	}
}

func TestInvokeAndValidateFreeCADRuntime_RejectsUnsafeArtifactObjects(t *testing.T) {
	external := t.TempDir()
	target := filepath.Join(external, "target.step")
	if err := os.WriteFile(target, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		place func(t *testing.T, path string)
	}{
		{"symlink", func(t *testing.T, path string) {
			if err := os.Symlink(target, path); err != nil {
				t.Skipf("symlink unsupported: %v", err)
			}
		}},
		{"directory", func(t *testing.T, path string) {
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{"fifo", func(t *testing.T, path string) {
			if err := syscall.Mkfifo(path, 0o600); err != nil {
				t.Skipf("fifo unsupported: %v", err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validFreeCADRuntimeExecutionRequest(t)
			var artifactPath string
			fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
				expected := task10ManifestArtifacts(t, got)
				artifactPath = filepath.Join(got.WorkingCopyDir, filepath.FromSlash(expected[0].Path))
				tt.place(t, artifactPath)
				task10Result(t, got, expected, "succeeded")
				return &runtimecap.Result{Stdout: "raw"}, nil
			}}
			run, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
			requireTask10Error(t, err, FreeCADRuntimeRunStageArtifactValidation)
			if run.Process == nil || run.Process.Stdout != "raw" || run.Result == nil || run.Artifacts != nil {
				t.Fatalf("run=%#v", run)
			}
			// The unsafe object is retained for inspection and never followed.
			if _, statErr := os.Lstat(artifactPath); statErr != nil {
				t.Fatalf("unsafe artifact object removed: %v", statErr)
			}
			data, readErr := os.ReadFile(target)
			if readErr != nil || string(data) != "safe" {
				t.Fatalf("external target changed: %q %v", data, readErr)
			}
		})
	}
}

func TestInvokeAndValidateFreeCADRuntime_ReturnsValidationOnlyArtifacts(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		return task10Success(t, got, false)
	}}
	run, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	if err != nil {
		t.Fatal(err)
	}
	// The artifact state is validation metadata only: no registration IDs,
	// URLs, acceptance flags, or record entries exist on the returned type.
	artifactType := reflect.TypeOf(FreeCADRuntimeValidatedArtifact{})
	wantFields := []string{"ID", "Format", "RelativePath", "Path"}
	if artifactType.NumField() != len(wantFields) {
		t.Fatalf("validated artifact grew beyond validation metadata: %v", artifactType)
	}
	for i, name := range wantFields {
		if artifactType.Field(i).Name != name || artifactType.Field(i).Type.Kind() != reflect.String {
			t.Fatalf("field %d=%v want %s string", i, artifactType.Field(i), name)
		}
	}
	// No store mutation, copying, repackaging, or report/record output: the
	// product directory holds exactly the working-copy files.
	layout := run.ObservationRequest.Manifest.Attempt.Layout
	prefix := "_working/" + layout.WorkingCopyID + "/"
	want := []string{
		prefix + "cad_result.json",
		prefix + "outputs/" + FreeCADRuntimeObservedFilename,
		prefix + "outputs/Widget.step",
		prefix + FreeCADRuntimeObservationRequestFilename,
		prefix + FreeCADReferenceTraversalRequestFilename,
		prefix + "runtime_manifest.json",
		prefix + "source/Widget.FCStd",
	}
	sort.Strings(want)
	got := task10ProductFiles(t, req.ProductDir)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("product files=%v want=%v", got, want)
	}
}

// --- Observed loading and correlation ---

func TestInvokeAndValidateFreeCADRuntime_LoadsObservedFromOutputDirectory(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	mat, err := WriteFreeCADRuntimeObservationRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	layout := mat.Manifest.Attempt.Layout
	sentinel := filepath.Join(layout.WorkingCopyDir, FreeCADRuntimeObservedFilename)
	if err := os.WriteFile(sentinel, []byte("{malformed sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		return task10Success(t, got, false)
	}}
	run, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	if err != nil {
		t.Fatal(err)
	}
	if run.ObservedPath != filepath.Join(layout.OutputDir, FreeCADRuntimeObservedFilename) {
		t.Fatalf("observed path=%q", run.ObservedPath)
	}
	data, readErr := os.ReadFile(sentinel)
	if readErr != nil || string(data) != "{malformed sentinel" {
		t.Fatalf("root-level sentinel consumed: %q %v", data, readErr)
	}
}

func TestInvokeAndValidateFreeCADRuntime_ValidatesObservedContract(t *testing.T) {
	digest := strings.Repeat("ab", 32)
	tests := []struct {
		name   string
		raw    func(workingCopyDir string) string
		decode bool
	}{
		{"malformed json", func(string) string { return "{" }, true},
		{"unknown top-level field", func(wcd string) string {
			return fmt.Sprintf(`{"schemaVersion":"1.0","workingCopy":{"path":%q,"sha256":%q},"observation":{"parameters":[],"metadata":[],"references":[],"components":[]},"extra":1}`, wcd, digest)
		}, false},
		{"unsupported schema version", func(wcd string) string {
			return fmt.Sprintf(`{"schemaVersion":"9.9","workingCopy":{"path":%q,"sha256":%q},"observation":{"parameters":[],"metadata":[],"references":[],"components":[]}}`, wcd, digest)
		}, false},
		{"invalid working-copy scalar", func(wcd string) string {
			return fmt.Sprintf(`{"schemaVersion":"1.0","workingCopy":{"path":%q,"sha256":"NOT-A-SHA"},"observation":{"parameters":[],"metadata":[],"references":[],"components":[]}}`, wcd)
		}, false},
		{"invalid parameter entry", func(wcd string) string {
			return fmt.Sprintf(`{"schemaVersion":"1.0","workingCopy":{"path":%q,"sha256":%q},"observation":{"parameters":[{"id":"p","name":"n","value":1,"valueKind":"weird"}],"metadata":[],"references":[],"components":[]}}`, wcd, digest)
		}, false},
		{"invalid metadata entry", func(wcd string) string {
			return fmt.Sprintf(`{"schemaVersion":"1.0","workingCopy":{"path":%q,"sha256":%q},"observation":{"parameters":[],"metadata":[{"id":"m","key":"","value":1,"valueKind":"number"}],"references":[],"components":[]}}`, wcd, digest)
		}, false},
		{"invalid reference entry", func(wcd string) string {
			return fmt.Sprintf(`{"schemaVersion":"1.0","workingCopy":{"path":%q,"sha256":%q},"observation":{"parameters":[],"metadata":[],"references":[{"kind":"","name":"x"}],"components":[]}}`, wcd, digest)
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validFreeCADRuntimeExecutionRequest(t)
			fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
				result, err := task10Success(t, got, false)
				task10RawObserved(t, got, []byte(tt.raw(got.WorkingCopyDir)))
				return result, err
			}}
			_, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
			requireTask10Error(t, err, FreeCADRuntimeRunStageObservedLoad)
			if tt.decode {
				var decodeErr *observed.DecodeError
				if !errors.As(err, &decodeErr) || !errors.Is(err, observed.ErrDecode) {
					t.Fatalf("observed decode cause lost: %v", err)
				}
			} else {
				var validationErr *observed.ValidationError
				if !errors.As(err, &validationErr) || !errors.Is(err, observed.ErrValidation) {
					t.Fatalf("observed validation cause lost: %v", err)
				}
			}
		})
	}
}

func TestInvokeAndValidateFreeCADRuntime_RejectsUnsafeObservedObjects(t *testing.T) {
	external := t.TempDir()
	target := filepath.Join(external, "observed-target.json")
	tests := []struct {
		name  string
		place func(t *testing.T, got runtimecap.Request, path string)
	}{
		{"symlink", func(t *testing.T, got runtimecap.Request, path string) {
			// The target is itself a valid observed contract: only a traversal
			// would make the run succeed.
			data, err := observed.CanonicalJSON(task10BuildObserved(t, got.WorkingCopyDir, task10PreparedSourceDigest(t, got), "Width", 1))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Skipf("symlink unsupported: %v", err)
			}
		}},
		{"directory", func(t *testing.T, _ runtimecap.Request, path string) {
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validFreeCADRuntimeExecutionRequest(t)
			var observedPath string
			fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
				expected := task10ManifestArtifacts(t, got)
				task10WriteArtifactFiles(t, got, expected, "artifact")
				task10Result(t, got, expected, "succeeded")
				observedPath = filepath.Join(got.OutputDir, FreeCADRuntimeObservedFilename)
				tt.place(t, got, observedPath)
				return &runtimecap.Result{Stdout: "raw"}, nil
			}}
			run, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
			requireTask10Error(t, err, FreeCADRuntimeRunStageObservedLoad)
			// Result and artifacts remain available for inspection.
			if run.Process == nil || run.Result == nil || len(run.Artifacts) != 1 || run.Observed != nil {
				t.Fatalf("run=%#v", run)
			}
			if _, statErr := os.Lstat(observedPath); statErr != nil {
				t.Fatalf("raw observed object removed: %v", statErr)
			}
		})
	}
}

func TestInvokeAndValidateFreeCADRuntime_CorrelatesObservedPreparedSourceSHA(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	other := sha256.Sum256([]byte("another prepared source"))
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		result, err := task10Success(t, got, false)
		// Structurally valid observed output with the correct working-copy path
		// but a different valid lowercase SHA-256.
		task10Observed(t, got, got.WorkingCopyDir, hex.EncodeToString(other[:]), false, 999)
		return result, err
	}}
	run, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	got := requireTask10Error(t, err, FreeCADRuntimeRunStageObservedCorrelation)
	if got.Field != "Observed.WorkingCopy.SHA256" || run.Observed == nil {
		t.Fatalf("error=%#v run=%#v", got, run)
	}
}

// TestInvokeAndValidateFreeCADRuntime_UsesPreInvocationPreparedSourceDigestForObservedCorrelation
// proves native persistence is compatible with observed correlation: the
// runtime is free to mutate, recompute, and save the working-copy document
// during invocation (prepared source A -> persisted source B, A != B) as
// long as the reported workingCopy.sha256 correlates to the pre-invocation
// prepared-source identity A. This replaces the prior stale expectation that
// observed correlation was authoritative against whatever bytes happened to
// be on disk after the runtime finished.
func TestInvokeAndValidateFreeCADRuntime_UsesPreInvocationPreparedSourceDigestForObservedCorrelation(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	modified := []byte("modified during invocation")
	var preRuntimeDigest, postRuntimeDigest string
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		sourcePath := filepath.Join(got.WorkingCopyDir, "source", "Widget.FCStd")
		preRuntimeDigest = task10PreparedSourceDigest(t, got)
		artifacts := task10ManifestArtifacts(t, got)
		task10WriteArtifactFiles(t, got, artifacts, "artifact")
		task10Result(t, got, artifacts, "succeeded")
		// Native mutation, recompute, and document.save() during invocation:
		// the persisted source becomes B, distinct from prepared source A.
		if writeErr := os.WriteFile(sourcePath, modified, 0o640); writeErr != nil {
			t.Fatal(writeErr)
		}
		postRuntimeDigest = task10PreparedSourceDigest(t, got)
		// Observed correctly reports the pre-invocation prepared-source
		// identity A, not the post-save persisted identity B.
		task10Observed(t, got, got.WorkingCopyDir, preRuntimeDigest, false, 999)
		return &runtimecap.Result{Command: runtimecap.Command{Path: got.RuntimeCommand, Args: []string{"execute"}}, Stdout: "out:artifact", Stderr: "err:artifact"}, nil
	}}
	run, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	if err != nil {
		t.Fatalf("legitimate native persistence rejected: %v", err)
	}
	if preRuntimeDigest == "" || postRuntimeDigest == "" || preRuntimeDigest == postRuntimeDigest {
		t.Fatalf("mutation did not change persisted source digest: pre=%q post=%q", preRuntimeDigest, postRuntimeDigest)
	}
	if run.Observed.WorkingCopy.SHA256 != preRuntimeDigest {
		t.Fatalf("observed correlation did not use pre-invocation prepared-source digest: got %q want %q", run.Observed.WorkingCopy.SHA256, preRuntimeDigest)
	}
	finalDigest, err := sha256File(filepath.Join(run.ExecutionRequest.WorkingCopyDir, "source", "Widget.FCStd"))
	if err != nil {
		t.Fatal(err)
	}
	if finalDigest != postRuntimeDigest {
		t.Fatalf("persisted source digest was not preserved: got %q want %q", finalDigest, postRuntimeDigest)
	}
}

// TestInvokeAndValidateFreeCADRuntime_RejectsPostSavePreparedSourceDigestForObservedCorrelation
// proves observed correlation has exactly one valid meaning: the
// pre-invocation prepared-source identity A. Reporting the post-save
// persisted identity B -- even though B is a real, legitimately-produced
// digest of the runtime's own saved document -- must still fail, since
// accepting either A or B would weaken correlation to an unordered set
// membership check instead of a strict single-value identity check.
func TestInvokeAndValidateFreeCADRuntime_RejectsPostSavePreparedSourceDigestForObservedCorrelation(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	modified := []byte("modified during invocation")
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		sourcePath := filepath.Join(got.WorkingCopyDir, "source", "Widget.FCStd")
		preRuntimeDigest := task10PreparedSourceDigest(t, got)
		artifacts := task10ManifestArtifacts(t, got)
		task10WriteArtifactFiles(t, got, artifacts, "artifact")
		task10Result(t, got, artifacts, "succeeded")
		if writeErr := os.WriteFile(sourcePath, modified, 0o640); writeErr != nil {
			t.Fatal(writeErr)
		}
		postRuntimeDigest := task10PreparedSourceDigest(t, got)
		if postRuntimeDigest == preRuntimeDigest {
			t.Fatal("mutation did not change persisted source digest")
		}
		// Observed incorrectly reports the post-save persisted identity B
		// instead of the pre-invocation prepared-source identity A.
		task10Observed(t, got, got.WorkingCopyDir, postRuntimeDigest, false, 999)
		return &runtimecap.Result{Command: runtimecap.Command{Path: got.RuntimeCommand, Args: []string{"execute"}}, Stdout: "out:artifact", Stderr: "err:artifact"}, nil
	}}
	_, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	got := requireTask10Error(t, err, FreeCADRuntimeRunStageObservedCorrelation)
	if got.Field != "Observed.WorkingCopy.SHA256" {
		t.Fatalf("error=%#v", got)
	}
}

// --- Determinism, isolation, and evidence ---

func TestInvokeAndValidateFreeCADRuntime_AttemptsAreIsolated(t *testing.T) {
	success := func() *task10Capability {
		return &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
			return task10Success(t, got, false)
		}}
	}
	req1 := validFreeCADRuntimeExecutionRequest(t)
	first, err := InvokeAndValidateFreeCADRuntime(context.Background(), success(), req1)
	if err != nil {
		t.Fatal(err)
	}

	req2 := cloneFreeCADObservationRequest(req1)
	req2.Attempt = 2
	// Attempt 1 outputs exist, but a runtime that writes nothing for attempt 2
	// cannot be satisfied by them.
	noop := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		return &runtimecap.Result{}, nil
	}}
	_, err = InvokeAndValidateFreeCADRuntime(context.Background(), noop, req2)
	requireTask10Error(t, err, FreeCADRuntimeRunStageResultLoad)

	second, err := InvokeAndValidateFreeCADRuntime(context.Background(), success(), req2)
	if err != nil {
		t.Fatal(err)
	}

	firstLayout := first.ObservationRequest.Manifest.Attempt.Layout
	secondLayout := second.ObservationRequest.Manifest.Attempt.Layout
	pairs := [][2]string{
		{first.ObservationRequest.Manifest.Attempt.Identity.ID, second.ObservationRequest.Manifest.Attempt.Identity.ID},
		{firstLayout.WorkingCopyDir, secondLayout.WorkingCopyDir},
		{firstLayout.SourceDocumentPath, secondLayout.SourceDocumentPath},
		{firstLayout.ManifestPath, secondLayout.ManifestPath},
		{first.ObservationRequest.Path, second.ObservationRequest.Path},
		{firstLayout.ResultPath, secondLayout.ResultPath},
		{first.ObservedPath, second.ObservedPath},
		{first.Artifacts[0].Path, second.Artifacts[0].Path},
	}
	for i, pair := range pairs {
		if pair[0] == "" || pair[0] == pair[1] {
			t.Fatalf("attempt state %d not isolated: %q == %q", i, pair[0], pair[1])
		}
	}
}

func TestInvokeAndValidateFreeCADRuntime_EquivalentRunsReturnEquivalentState(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		return task10Success(t, got, false)
	}}
	first, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, cloneFreeCADObservationRequest(req))
	if err != nil {
		t.Fatal(err)
	}
	// Every returned field, including the execution request, command argument
	// order, decoded result, normalized observed structure, observed path, and
	// validated artifact order, is deterministic for equivalent runs.
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("equivalent runs diverged:\n%#v\n%#v", first, second)
	}
}

func TestInvokeAndValidateFreeCADRuntime_DoesNotRollbackInvalidRuntimeOutputs(t *testing.T) {
	t.Run("artifact validation failure", func(t *testing.T) {
		req := validFreeCADRuntimeExecutionRequest(t)
		var artifactPath string
		fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
			expected := task10ManifestArtifacts(t, got)
			task10WriteArtifactFiles(t, got, expected, "artifact")
			task10Observed(t, got, got.WorkingCopyDir, task10PreparedSourceDigest(t, got), false, 999)
			artifactPath = filepath.Join(got.WorkingCopyDir, filepath.FromSlash(expected[0].Path))
			mismatched := append([]freecad.FreeCADRuntimeResultArtifact(nil), expected...)
			mismatched[0].ID = "changed"
			task10Result(t, got, mismatched, "succeeded")
			return &runtimecap.Result{Stdout: "evidence-out", Stderr: "evidence-err"}, nil
		}}
		run, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
		requireTask10Error(t, err, FreeCADRuntimeRunStageArtifactValidation)
		for _, path := range []string{run.ExecutionRequest.ResultPath, artifactPath, run.ObservedPath} {
			if _, statErr := os.Stat(path); statErr != nil {
				t.Fatalf("runtime evidence rolled back: %v", statErr)
			}
		}
		if run.Result == nil || run.Process == nil || run.Process.Stdout != "evidence-out" || run.Process.Stderr != "evidence-err" {
			t.Fatalf("run=%#v", run)
		}
	})

	t.Run("observed validation failure", func(t *testing.T) {
		req := validFreeCADRuntimeExecutionRequest(t)
		fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
			result, err := task10Success(t, got, false)
			task10RawObserved(t, got, []byte("{invalid observed"))
			return result, err
		}}
		run, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
		requireTask10Error(t, err, FreeCADRuntimeRunStageObservedLoad)
		for _, path := range []string{run.ExecutionRequest.ResultPath, run.Artifacts[0].Path, run.ObservedPath} {
			if _, statErr := os.Stat(path); statErr != nil {
				t.Fatalf("runtime evidence rolled back: %v", statErr)
			}
		}
		data, readErr := os.ReadFile(run.ObservedPath)
		if readErr != nil || string(data) != "{invalid observed" {
			t.Fatalf("raw observed object changed: %q %v", data, readErr)
		}
		if run.Result == nil || len(run.Artifacts) != 1 || run.Process == nil {
			t.Fatalf("run=%#v", run)
		}
	})
}

// --- Error stages and causes ---

func TestFreeCADRuntimeRun_ErrorStages(t *testing.T) {
	successFake := func() *task10Capability {
		return &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
			return task10Success(t, got, false)
		}}
	}
	tests := []struct {
		stage string
		run   func(t *testing.T) error
	}{
		{FreeCADRuntimeRunStageRequestMaterialization, func(t *testing.T) error {
			req := validFreeCADRuntimeExecutionRequest(t)
			req.Manifest.Verification.ObservationParameterLinks = nil
			_, err := InvokeAndValidateFreeCADRuntime(context.Background(), successFake(), req)
			return err
		}},
		{FreeCADRuntimeRunStageExecutionRequest, func(t *testing.T) error {
			req := validFreeCADRuntimeExecutionRequest(t)
			//nolint:staticcheck
			_, err := InvokeAndValidateFreeCADRuntime(nil, successFake(), req)
			return err
		}},
		{FreeCADRuntimeRunStageOutputPreparation, func(t *testing.T) error {
			req := validFreeCADRuntimeExecutionRequest(t)
			mat, err := WriteFreeCADRuntimeObservationRequest(req)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(mat.Manifest.Attempt.Layout.ResultPath, 0o755); err != nil {
				t.Fatal(err)
			}
			_, err = InvokeAndValidateFreeCADRuntime(context.Background(), successFake(), req)
			return err
		}},
		{FreeCADRuntimeRunStageProcessInvocation, func(t *testing.T) error {
			req := validFreeCADRuntimeExecutionRequest(t)
			fake := &task10Capability{fn: func(context.Context, runtimecap.Request) (*runtimecap.Result, error) {
				return nil, &runtimecap.Error{Kind: runtimecap.ErrorStart, Err: errors.New("start")}
			}}
			_, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
			return err
		}},
		{FreeCADRuntimeRunStageResultLoad, func(t *testing.T) error {
			req := validFreeCADRuntimeExecutionRequest(t)
			fake := &task10Capability{fn: func(context.Context, runtimecap.Request) (*runtimecap.Result, error) {
				return &runtimecap.Result{}, nil
			}}
			_, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
			return err
		}},
		{FreeCADRuntimeRunStageProcessResultCorrelation, func(t *testing.T) error {
			req := validFreeCADRuntimeExecutionRequest(t)
			fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
				task10Result(t, got, []freecad.FreeCADRuntimeResultArtifact{}, "failed")
				return &runtimecap.Result{}, nil
			}}
			_, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
			return err
		}},
		{FreeCADRuntimeRunStageRuntimeReportedFailure, func(t *testing.T) error {
			req := validFreeCADRuntimeExecutionRequest(t)
			fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
				task10Result(t, got, []freecad.FreeCADRuntimeResultArtifact{}, "failed")
				return &runtimecap.Result{}, &runtimecap.Error{Kind: runtimecap.ErrorExit, ExitCode: 4, Err: errors.New("exit")}
			}}
			_, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
			return err
		}},
		{FreeCADRuntimeRunStageArtifactValidation, func(t *testing.T) error {
			req := validFreeCADRuntimeExecutionRequest(t)
			fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
				expected := task10ManifestArtifacts(t, got)
				task10WriteArtifactFiles(t, got, expected, "artifact")
				expected[0].ID = "changed"
				task10Result(t, got, expected, "succeeded")
				return &runtimecap.Result{}, nil
			}}
			_, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
			return err
		}},
		{FreeCADRuntimeRunStageObservedLoad, func(t *testing.T) error {
			req := validFreeCADRuntimeExecutionRequest(t)
			fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
				expected := task10ManifestArtifacts(t, got)
				task10WriteArtifactFiles(t, got, expected, "artifact")
				task10Result(t, got, expected, "succeeded")
				return &runtimecap.Result{}, nil
			}}
			_, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
			return err
		}},
		{FreeCADRuntimeRunStageObservedCorrelation, func(t *testing.T) error {
			req := validFreeCADRuntimeExecutionRequest(t)
			other := sha256.Sum256([]byte("other"))
			fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
				result, err := task10Success(t, got, false)
				task10Observed(t, got, got.WorkingCopyDir, hex.EncodeToString(other[:]), false, 999)
				return result, err
			}}
			_, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
			return err
		}},
	}
	stages := map[string]bool{
		FreeCADRuntimeRunStageRequestMaterialization:   false,
		FreeCADRuntimeRunStageExecutionRequest:         false,
		FreeCADRuntimeRunStageOutputPreparation:        false,
		FreeCADRuntimeRunStageProcessInvocation:        false,
		FreeCADRuntimeRunStageResultLoad:               false,
		FreeCADRuntimeRunStageProcessResultCorrelation: false,
		FreeCADRuntimeRunStageRuntimeReportedFailure:   false,
		FreeCADRuntimeRunStageArtifactValidation:       false,
		FreeCADRuntimeRunStageObservedLoad:             false,
		FreeCADRuntimeRunStageObservedCorrelation:      false,
	}
	for _, tt := range tests {
		t.Run(tt.stage, func(t *testing.T) {
			requireTask10Error(t, tt.run(t), tt.stage)
		})
		stages[tt.stage] = true
	}
	for stage, covered := range stages {
		if !covered {
			t.Fatalf("exported stage %q has no reachable scenario", stage)
		}
	}
}

func TestFreeCADRuntimeRunError_PreservesCauses(t *testing.T) {
	t.Run("task 9 observation-request cause", func(t *testing.T) {
		req := validFreeCADRuntimeExecutionRequest(t)
		req.Manifest.Verification.ObservationParameterLinks = nil
		_, err := InvokeAndValidateFreeCADRuntime(context.Background(), &task10Capability{fn: func(context.Context, runtimecap.Request) (*runtimecap.Result, error) {
			return &runtimecap.Result{}, nil
		}}, req)
		var observationErr *FreeCADRuntimeObservationRequestError
		if !errors.As(err, &observationErr) {
			t.Fatalf("Task 9 cause lost: %v", err)
		}
	})

	t.Run("task 8 and attempt causes through task 9", func(t *testing.T) {
		req := validFreeCADObservationRequest(t) // host source never written
		_, err := InvokeAndValidateFreeCADRuntime(context.Background(), &task10Capability{fn: func(context.Context, runtimecap.Request) (*runtimecap.Result, error) {
			return &runtimecap.Result{}, nil
		}}, req)
		requireTask10Error(t, err, FreeCADRuntimeRunStageRequestMaterialization)
		var manifestErr *freecad.FreeCADRuntimeManifestError
		var attemptErr *freecad.FreeCADRuntimeAttemptError
		if !errors.As(err, &manifestErr) || !errors.As(err, &attemptErr) {
			t.Fatalf("Task 8/attempt causes lost: %v", err)
		}
	})

	t.Run("adapter and runtimecap causes", func(t *testing.T) {
		req := validFreeCADRuntimeExecutionRequest(t)
		root := errors.New("runtimecap root")
		fake := &task10Capability{fn: func(context.Context, runtimecap.Request) (*runtimecap.Result, error) {
			return nil, &runtimecap.Error{Kind: runtimecap.ErrorStart, Err: root}
		}}
		_, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
		var executionErr *freecad.FreeCADRuntimeExecutionError
		if !errors.As(err, &executionErr) || !errors.Is(err, root) {
			t.Fatalf("adapter/runtimecap causes lost: %v", err)
		}
	})

	t.Run("cancellation cause", func(t *testing.T) {
		req := validFreeCADRuntimeExecutionRequest(t)
		fake := &task10Capability{fn: func(context.Context, runtimecap.Request) (*runtimecap.Result, error) {
			return nil, &runtimecap.Error{Kind: runtimecap.ErrorCancelled, Err: context.Canceled}
		}}
		_, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation cause lost: %v", err)
		}
	})

	t.Run("result file cause", func(t *testing.T) {
		req := validFreeCADRuntimeExecutionRequest(t)
		fake := &task10Capability{fn: func(context.Context, runtimecap.Request) (*runtimecap.Result, error) {
			return &runtimecap.Result{}, nil
		}}
		_, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
		var fileErr *freecad.FreeCADRuntimeResultFileError
		if !errors.As(err, &fileErr) || !errors.Is(err, freecad.ErrFreeCADRuntimeResultIO) || !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("result file cause lost: %v", err)
		}
	})

	t.Run("result decode cause", func(t *testing.T) {
		req := validFreeCADRuntimeExecutionRequest(t)
		fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
			if err := os.WriteFile(got.ResultPath, []byte("{"), 0o600); err != nil {
				t.Fatal(err)
			}
			return &runtimecap.Result{}, nil
		}}
		_, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
		var decodeErr *freecad.FreeCADRuntimeResultDecodeError
		if !errors.As(err, &decodeErr) || !errors.Is(err, freecad.ErrFreeCADRuntimeResultDecode) {
			t.Fatalf("result decode cause lost: %v", err)
		}
	})

	t.Run("result validation cause", func(t *testing.T) {
		req := validFreeCADRuntimeExecutionRequest(t)
		fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
			if err := os.WriteFile(got.ResultPath, []byte(`{"schemaVersion":"9","status":"succeeded","artifacts":[]}`), 0o600); err != nil {
				t.Fatal(err)
			}
			return &runtimecap.Result{}, nil
		}}
		_, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
		var validationErr *freecad.FreeCADRuntimeResultValidationError
		if !errors.As(err, &validationErr) || !errors.Is(err, freecad.ErrFreeCADRuntimeResultValidation) {
			t.Fatalf("result validation cause lost: %v", err)
		}
	})

	t.Run("observed file cause", func(t *testing.T) {
		req := validFreeCADRuntimeExecutionRequest(t)
		fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
			expected := task10ManifestArtifacts(t, got)
			task10WriteArtifactFiles(t, got, expected, "artifact")
			task10Result(t, got, expected, "succeeded")
			return &runtimecap.Result{}, nil
		}}
		_, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
		var fileErr *observed.FileError
		if !errors.As(err, &fileErr) || !errors.Is(err, observed.ErrIO) || !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("observed file cause lost: %v", err)
		}
	})

	t.Run("observed decode cause", func(t *testing.T) {
		req := validFreeCADRuntimeExecutionRequest(t)
		fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
			result, err := task10Success(t, got, false)
			task10RawObserved(t, got, []byte("{"))
			return result, err
		}}
		_, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
		var decodeErr *observed.DecodeError
		if !errors.As(err, &decodeErr) || !errors.Is(err, observed.ErrDecode) {
			t.Fatalf("observed decode cause lost: %v", err)
		}
	})

	t.Run("observed validation cause", func(t *testing.T) {
		req := validFreeCADRuntimeExecutionRequest(t)
		fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
			result, err := task10Success(t, got, false)
			task10RawObserved(t, got, []byte(`{"schemaVersion":"9.9"}`))
			return result, err
		}}
		_, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
		var validationErr *observed.ValidationError
		if !errors.As(err, &validationErr) || !errors.Is(err, observed.ErrValidation) {
			t.Fatalf("observed validation cause lost: %v", err)
		}
	})

	t.Run("filesystem cleanup cause", func(t *testing.T) {
		req := validFreeCADRuntimeExecutionRequest(t)
		req.Manifest.Outputs = []planner.ExportManifestOutput{{Type: "step", Filename: "outputs/nested/Widget.step", Object: "Body"}}
		mat, err := WriteFreeCADRuntimeObservationRequest(req)
		if err != nil {
			t.Fatal(err)
		}
		// A regular file where a path directory is expected surfaces the real
		// filesystem cause through the cleanup stage.
		if err := os.WriteFile(filepath.Join(mat.Manifest.Attempt.Layout.OutputDir, "nested"), []byte("file"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err = InvokeAndValidateFreeCADRuntime(context.Background(), &task10Capability{fn: func(context.Context, runtimecap.Request) (*runtimecap.Result, error) {
			return &runtimecap.Result{}, nil
		}}, req)
		requireTask10Error(t, err, FreeCADRuntimeRunStageOutputPreparation)
		var pathErr *fs.PathError
		if !errors.As(err, &pathErr) || !errors.Is(err, syscall.ENOTDIR) {
			t.Fatalf("filesystem cause lost: %v", err)
		}
	})

	t.Run("joined process and result causes", func(t *testing.T) {
		req := validFreeCADRuntimeExecutionRequest(t)
		root := errors.New("exit root")
		fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
			if err := os.WriteFile(got.ResultPath, []byte("{"), 0o600); err != nil {
				t.Fatal(err)
			}
			return &runtimecap.Result{}, &runtimecap.Error{Kind: runtimecap.ErrorExit, ExitCode: 2, Err: root}
		}}
		_, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
		var decodeErr *freecad.FreeCADRuntimeResultDecodeError
		var executionErr *freecad.FreeCADRuntimeExecutionError
		if !errors.Is(err, root) || !errors.As(err, &decodeErr) || !errors.As(err, &executionErr) {
			t.Fatalf("joined causes lost: %v", err)
		}
	})
}

func TestFreeCADRuntimeRunError_DoesNotLeakContent(t *testing.T) {
	const (
		sourceSentinel   = "SRC_BYTES_SENTINEL"
		manifestSentinel = "Width_MANIFEST_SENTINEL"
		requestSentinel  = "GROUP_OBSREQ_SENTINEL"
		observedSentinel = "OBSERVED_NAME_SENTINEL"
		envSentinel      = "ENV_SECRET_SENTINEL_VALUE"
	)
	t.Setenv("PARAMETRON_SECRET", envSentinel)
	req := validFreeCADRuntimeExecutionRequest(t)
	writeObservationHostSource(t, req, []byte(sourceSentinel))
	req.Manifest.ParameterAssignments[0].Target = "Body." + manifestSentinel
	req.Manifest.Verification.ObservationParameterLinks[0].GroupName = requestSentinel

	other := sha256.Sum256([]byte("different bytes"))
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		expected := task10ManifestArtifacts(t, got)
		task10WriteArtifactFiles(t, got, expected, "artifact")
		task10Result(t, got, expected, "succeeded")
		task10WriteObserved(t, got, task10BuildObserved(t, got.WorkingCopyDir, hex.EncodeToString(other[:]), observedSentinel, 999), false)
		return &runtimecap.Result{Stdout: sourceSentinel, Stderr: sourceSentinel},
			nil
	}}
	run, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	got := requireTask10Error(t, err, FreeCADRuntimeRunStageObservedCorrelation)
	text := got.Error()
	for _, sentinel := range []string{sourceSentinel, manifestSentinel, requestSentinel, observedSentinel, envSentinel} {
		if strings.Contains(text, sentinel) {
			t.Fatalf("error text leaks %q: %s", sentinel, text)
		}
	}
	// Useful stage, field, attempt, and path context remains.
	attemptID := run.ObservationRequest.Manifest.Attempt.Identity.ID
	for _, want := range []string{FreeCADRuntimeRunStageObservedCorrelation, "Observed.WorkingCopy.SHA256", attemptID, run.ObservedPath} {
		if !strings.Contains(text, want) {
			t.Fatalf("error text lacks context %q: %s", want, text)
		}
	}

	// Process failure context stays inspectable without echoing captured output.
	req2 := validFreeCADRuntimeExecutionRequest(t)
	failing := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		task10Result(t, got, []freecad.FreeCADRuntimeResultArtifact{}, "failed")
		return &runtimecap.Result{Stdout: sourceSentinel, Stderr: sourceSentinel},
			&runtimecap.Error{Kind: runtimecap.ErrorExit, ExitCode: 7, Stdout: sourceSentinel, Stderr: sourceSentinel, Err: errors.New("exit status 7")}
	}}
	_, err = InvokeAndValidateFreeCADRuntime(context.Background(), failing, req2)
	processErr := requireTask10Error(t, err, FreeCADRuntimeRunStageRuntimeReportedFailure)
	processText := processErr.Error()
	if strings.Contains(processText, sourceSentinel) {
		t.Fatalf("error text leaks captured output: %s", processText)
	}
	for _, want := range []string{"non_zero_exit", "exit=7"} {
		if !strings.Contains(processText, want) {
			t.Fatalf("error text lacks process context %q: %s", want, processText)
		}
	}
}

// --- Logical identity and boundary non-integration ---

func TestInvokeAndValidateFreeCADRuntime_DoesNotAlterLogicalEngineeringIdentity(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	want := cloneFreeCADObservationRequest(req)
	before, err := freecad.ComputeFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		return task10Success(t, got, false)
	}}
	run, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	if err != nil {
		t.Fatal(err)
	}
	after, err := freecad.ComputeFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(req, want) || !reflect.DeepEqual(before.Identity, after.Identity) {
		t.Fatal("logical identity changed by invocation")
	}
	// The complete logical request payload (plan hash, cache/step context, job
	// ID, and handoff-facing planner payloads) contains no operational Task 10
	// output state.
	logical, err := json.Marshal(struct {
		JobID, ProductKey, StepID, PlanHash string
		CSV                                 planner.WriteCSVPayload
		Manifest                            planner.WriteExportManifestPayload
		CADRuntime                          planner.RunCADRuntimePayload
	}{req.JobID, req.ProductKey, req.StepID, req.Manifest.PlanHash, req.CSV, req.Manifest, req.CADRuntime})
	if err != nil {
		t.Fatal(err)
	}
	resultJSON, err := os.ReadFile(run.ExecutionRequest.ResultPath)
	if err != nil {
		t.Fatal(err)
	}
	observedJSON, err := os.ReadFile(run.ObservedPath)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := []string{
		run.ExecutionRequest.ResultPath,
		run.ObservedPath,
		run.Artifacts[0].Path,
		run.Process.Stdout,
		run.Process.Stderr,
		string(resultJSON),
		string(observedJSON),
		string(run.ObservationRequest.JSON),
	}
	for _, value := range forbidden {
		if value != "" && bytes.Contains(logical, []byte(value)) {
			t.Fatalf("operational state entered logical identity: %q in %s", value, logical)
		}
	}
}

func TestInvokeAndValidateFreeCADRuntime_ReturnedStateIsDefensive(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	want := cloneFreeCADObservationRequest(req)
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		return task10Success(t, got, false)
	}}
	first, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, cloneFreeCADObservationRequest(req))
	if err != nil {
		t.Fatal(err)
	}
	persisted := map[string][]byte{}
	for _, path := range []string{first.ExecutionRequest.ResultPath, first.ObservedPath, first.Artifacts[0].Path} {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		persisted[path] = data
	}

	// Mutate every returned slice and entry.
	first.Artifacts[0].ID = "mutated"
	first.Artifacts = append(first.Artifacts, FreeCADRuntimeValidatedArtifact{ID: "extra"})
	first.Process.Command.Args = append(first.Process.Command.Args, "--mutated")
	if len(first.Process.Command.Args) > 0 {
		first.Process.Command.Args[0] = "mutated"
	}
	first.Result.Artifacts[0].ID = "mutated"
	first.Result.Artifacts = append(first.Result.Artifacts, freecad.FreeCADRuntimeResultArtifact{ID: "extra"})
	first.Observed.Observation.Parameters[0].Name = "mutated"
	first.Observed.Observation.Parameters = append(first.Observed.Observation.Parameters, observed.Parameter{ID: "extra"})
	first.Observed.Observation.Metadata = append(first.Observed.Observation.Metadata, observed.Metadata{ID: "extra"})
	first.Observed.Observation.References = append(first.Observed.Observation.References, observed.Reference{Kind: "extra"})

	// The caller request, persisted files, and independent equivalent runs are
	// all unaffected.
	if !reflect.DeepEqual(req, want) {
		t.Fatal("caller request changed by returned-state mutation")
	}
	for path, before := range persisted {
		after, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Equal(after, before) {
			t.Fatalf("persisted state changed: %s %v", path, readErr)
		}
	}
	again, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, cloneFreeCADObservationRequest(req))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reference, again) {
		t.Fatalf("independent equivalent run affected:\n%#v\n%#v", reference, again)
	}
}

func TestInvokeAndValidateFreeCADRuntime_DoesNotProduceVerificationDecision(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		// The observed parameter value (999) deliberately mismatches the
		// expected manifest value (10) while runtime identity stays valid.
		return task10Success(t, got, false)
	}}
	run, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	if err != nil {
		t.Fatal(err)
	}
	if string(run.Observed.Observation.Parameters[0].Value.Raw()) != "999" {
		t.Fatalf("observed mismatch not preserved: %#v", run.Observed)
	}
	// The returned state carries no verification result, pass/fail field, or
	// accepted/rejected state. ReferenceTraversalPath/ReferenceTraversalJSON
	// are approved raw evidence transport fields, not verification decisions.
	runType := reflect.TypeOf(run)
	wantFields := []string{
		"ObservationRequest", "ReferenceTraversalRequest", "ExecutionRequest", "Process", "Result",
		"Observed", "ObservedPath", "ReferenceTraversalPath", "ReferenceTraversalJSON", "Artifacts",
	}
	if runType.NumField() != len(wantFields) {
		t.Fatalf("run type grew beyond raw runtime state: %v", runType)
	}
	for i, name := range wantFields {
		field := runType.Field(i)
		if field.Name != name {
			t.Fatalf("field %d=%q want %q", i, field.Name, name)
		}
		if typeName := field.Type.String(); strings.Contains(typeName, "verification.") {
			t.Fatalf("verification package state present: %s", typeName)
		}
	}
	// The two approved raw evidence transport fields must stay plain raw
	// bytes/path material, not grow into a verification decision shape.
	if field, _ := runType.FieldByName("ReferenceTraversalPath"); field.Type.Kind() != reflect.String {
		t.Fatalf("ReferenceTraversalPath is no longer a raw path string: %s", field.Type)
	}
	if field, _ := runType.FieldByName("ReferenceTraversalJSON"); field.Type.String() != "[]uint8" {
		t.Fatalf("ReferenceTraversalJSON is no longer raw bytes: %s", field.Type)
	}
	// No verification output file is produced anywhere in the product tree.
	for _, file := range task10ProductFiles(t, req.ProductDir) {
		if strings.Contains(file, "verification") && !strings.HasSuffix(file, FreeCADRuntimeObservationRequestFilename) {
			t.Fatalf("verification output file produced: %s", file)
		}
	}
	// Source boundary: Task 10 production never calls verification.Verify.
	source, err := os.ReadFile("freecad_runtime_execution.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(source), "verification.") {
		t.Fatal("Task 10 production references the verification package")
	}
}

func TestInvokeAndValidateFreeCADRuntime_DoesNotIntegrateLaterStages(t *testing.T) {
	req := validFreeCADRuntimeExecutionRequest(t)
	fake := &task10Capability{fn: func(_ context.Context, got runtimecap.Request) (*runtimecap.Result, error) {
		return task10Success(t, got, false)
	}}
	run, err := InvokeAndValidateFreeCADRuntime(context.Background(), fake, req)
	if err != nil {
		t.Fatal(err)
	}
	// No artifact registry/store, report, API/scheduler, or record-package
	// output exists: only the working-copy files are ever produced.
	layout := run.ObservationRequest.Manifest.Attempt.Layout
	prefix := "_working/" + layout.WorkingCopyID + "/"
	for _, file := range task10ProductFiles(t, req.ProductDir) {
		if !strings.HasPrefix(file, prefix) {
			t.Fatalf("output outside attempt working copy: %s", file)
		}
	}
	// Task 10 production integrates no later-stage packages.
	source, err := os.ReadFile("freecad_runtime_execution.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"engine/artifact", "engine/report", "engine/api", "engine/scheduler",
		"engine/executor", "engine/job", "engine/handoff",
		"recordcontract", "recordmap", "recordemit", "recordpackage",
		"authoring/planner", "engine/verification", "engine/bridge",
	} {
		if strings.Contains(string(source), forbidden) {
			t.Fatalf("later-stage integration %q present", forbidden)
		}
	}
}
