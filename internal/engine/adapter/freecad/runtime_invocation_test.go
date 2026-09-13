package freecad

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"parametron/internal/engine/runtimecap"
)

// writeFakeRuntime writes an executable shell script to dir/name and returns its path.
func writeFakeRuntime(t *testing.T, dir, name, script string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0755); err != nil {
		t.Fatalf("failed to write fake runtime %s: %v", name, err)
	}
	return path
}

type fakeRuntimeCapability struct {
	called bool
	calls  int
	ctx    context.Context
	req    runtimecap.Request
	result *runtimecap.Result
	err    error
}

func (f *fakeRuntimeCapability) Invoke(ctx context.Context, req runtimecap.Request) (*runtimecap.Result, error) {
	f.called = true
	f.calls++
	f.ctx = ctx
	f.req = req
	return f.result, f.err
}

func TestInvokeFreeCADRuntime_ExportedWrapperDelegates(t *testing.T) {
	ctx := context.WithValue(context.Background(), struct{}{}, "marker")
	req := FreeCADRuntimeExecutionRequest{
		RuntimeCommand: "/runtime", WorkingCopyDir: "/work", ManifestPath: "/work/manifest.json",
		ResultPath: "/work/result.json", OutputDir: "/work/outputs", ObservationRequestPath: "/work/observe.json",
	}
	want := &runtimecap.Result{
		Command: runtimecap.Command{Path: "/runtime", Args: []string{"execute", "--marker"}},
		Stdout:  "stdout", Stderr: "stderr",
	}
	fake := &fakeRuntimeCapability{result: want}
	got, err := InvokeFreeCADRuntime(fake, ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 1 || fake.ctx != ctx {
		t.Fatalf("calls=%d context forwarded=%v", fake.calls, fake.ctx == ctx)
	}
	wantReq := runtimecap.Request{
		RuntimeCommand: req.RuntimeCommand, WorkingCopyDir: req.WorkingCopyDir, ManifestPath: req.ManifestPath,
		ResultPath: req.ResultPath, OutputDir: req.OutputDir, ObservationRequestPath: req.ObservationRequestPath,
	}
	if fake.req != wantReq || got.Command.Path != want.Command.Path ||
		strings.Join(got.Command.Args, "\x00") != strings.Join(want.Command.Args, "\x00") ||
		got.Stdout != want.Stdout || got.Stderr != want.Stderr {
		t.Fatalf("forwarding/conversion mismatch: request=%#v result=%#v", fake.req, got)
	}
}

func TestInvokeFreeCADRuntime_ExportedWrapperPreservesErrors(t *testing.T) {
	root := errors.New("root")
	for _, kind := range []string{runtimecap.ErrorInvalid, runtimecap.ErrorStart, runtimecap.ErrorExit, runtimecap.ErrorCancelled} {
		t.Run(kind, func(t *testing.T) {
			rcResult := &runtimecap.Result{Command: runtimecap.Command{Path: "/runtime"}, Stdout: "out", Stderr: "err"}
			fake := &fakeRuntimeCapability{result: rcResult, err: &runtimecap.Error{
				Kind: kind, ExitCode: 19, Stdout: "out", Stderr: "err", Err: root,
			}}
			got, err := InvokeFreeCADRuntime(fake, context.Background(), FreeCADRuntimeExecutionRequest{
				RuntimeCommand: "/runtime", WorkingCopyDir: "/work", ManifestPath: "/work/m", ResultPath: "/work/r",
			})
			var execErr *FreeCADRuntimeExecutionError
			if !errors.As(err, &execErr) || execErr.Kind != kind || execErr.ExitCode != 19 ||
				execErr.Stdout != "out" || execErr.Stderr != "err" || !errors.Is(err, root) {
				t.Fatalf("conversion failed: result=%#v error=%T %v", got, err, err)
			}
			if got == nil {
				t.Fatal("existing private path retains capability result")
			}
		})
	}
}

func TestInvokeFreeCADRuntime_NilCapabilityUsesExternalRuntime(t *testing.T) {
	dir := t.TempDir()
	runtime := writeFakeRuntime(t, dir, "runtime", `
printf '%s\n' "$@" > "$3/args.txt"
printf external-stdout
printf external-stderr >&2
`)
	req := FreeCADRuntimeExecutionRequest{
		RuntimeCommand: runtime, WorkingCopyDir: dir, ManifestPath: filepath.Join(dir, "manifest.json"),
		ResultPath: filepath.Join(dir, "result.json"), OutputDir: filepath.Join(dir, "outputs"),
		ObservationRequestPath: filepath.Join(dir, "observe.json"),
	}
	got, err := InvokeFreeCADRuntime(nil, context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Stdout != "external-stdout" || got.Stderr != "external-stderr" {
		t.Fatalf("output=%#v", got)
	}
	data, err := os.ReadFile(filepath.Join(dir, "args.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{"execute", "--working-copy", dir, "--manifest", req.ManifestPath,
		"--result", req.ResultPath, "--output-dir", req.OutputDir, "--observation-request", req.ObservationRequestPath}, "\n") + "\n"
	if string(data) != want {
		t.Fatalf("args=%q want %q", data, want)
	}
}

// --- Request validation ---

func TestBuildFreeCADRuntimeExecutionCommand_RejectsEmptyRuntimeCommand(t *testing.T) {
	_, err := buildFreeCADRuntimeExecutionCommand(FreeCADRuntimeExecutionRequest{
		WorkingCopyDir: "/tmp/work",
		ManifestPath:   "/tmp/work/manifest.json",
		ResultPath:     "/tmp/work/result.json",
	})
	if err == nil {
		t.Fatal("expected error for empty runtime command, got nil")
	}
	if !strings.Contains(err.Error(), "runtime command") {
		t.Fatalf("expected error to mention runtime command, got: %v", err)
	}
}

func TestBuildFreeCADRuntimeExecutionCommand_RejectsEmptyWorkingCopyDir(t *testing.T) {
	_, err := buildFreeCADRuntimeExecutionCommand(FreeCADRuntimeExecutionRequest{
		RuntimeCommand: "parametron-freecad",
		ManifestPath:   "/tmp/work/manifest.json",
		ResultPath:     "/tmp/work/result.json",
	})
	if err == nil {
		t.Fatal("expected error for empty working copy dir, got nil")
	}
	if !strings.Contains(err.Error(), "working copy") {
		t.Fatalf("expected error to mention working copy, got: %v", err)
	}
}

func TestBuildFreeCADRuntimeExecutionCommand_RejectsEmptyManifestPath(t *testing.T) {
	_, err := buildFreeCADRuntimeExecutionCommand(FreeCADRuntimeExecutionRequest{
		RuntimeCommand: "parametron-freecad",
		WorkingCopyDir: "/tmp/work",
		ResultPath:     "/tmp/work/result.json",
	})
	if err == nil {
		t.Fatal("expected error for empty manifest path, got nil")
	}
	if !strings.Contains(err.Error(), "manifest") {
		t.Fatalf("expected error to mention manifest, got: %v", err)
	}
}

func TestBuildFreeCADRuntimeExecutionCommand_RejectsEmptyResultPath(t *testing.T) {
	_, err := buildFreeCADRuntimeExecutionCommand(FreeCADRuntimeExecutionRequest{
		RuntimeCommand: "parametron-freecad",
		WorkingCopyDir: "/tmp/work",
		ManifestPath:   "/tmp/work/manifest.json",
	})
	if err == nil {
		t.Fatal("expected error for empty result path, got nil")
	}
	if !strings.Contains(err.Error(), "result") {
		t.Fatalf("expected error to mention result path, got: %v", err)
	}
}

func TestExecuteFreeCADRuntime_RejectsNilContext(t *testing.T) {
	req := FreeCADRuntimeExecutionRequest{
		RuntimeCommand: "parametron-freecad",
		WorkingCopyDir: "/tmp/work",
		ManifestPath:   "/tmp/work/manifest.json",
		ResultPath:     "/tmp/work/result.json",
	}
	//nolint:staticcheck
	result, err := executeFreeCADRuntime(nil, req)
	if err == nil {
		t.Fatal("expected error for nil context, got nil")
	}
	if result != nil {
		t.Fatalf("expected nil result for nil context, got non-nil")
	}
	var execErr *FreeCADRuntimeExecutionError
	if !errors.As(err, &execErr) {
		t.Fatalf("expected *FreeCADRuntimeExecutionError, got %T: %v", err, err)
	}
	if execErr.Kind != FreeCADRuntimeExecutionErrorInvalid {
		t.Fatalf("expected kind %q, got %q", FreeCADRuntimeExecutionErrorInvalid, execErr.Kind)
	}
}

func TestExecuteFreeCADRuntime_InvalidRequestFailsBeforeSubprocess(t *testing.T) {
	result, err := executeFreeCADRuntime(context.Background(), FreeCADRuntimeExecutionRequest{})
	if err == nil {
		t.Fatal("expected error for empty request, got nil")
	}
	if result != nil {
		t.Fatalf("expected nil result for invalid request, got non-nil")
	}
	var execErr *FreeCADRuntimeExecutionError
	if !errors.As(err, &execErr) {
		t.Fatalf("expected *FreeCADRuntimeExecutionError, got %T: %v", err, err)
	}
	if execErr.Kind != FreeCADRuntimeExecutionErrorInvalid {
		t.Fatalf("expected kind %q, got %q", FreeCADRuntimeExecutionErrorInvalid, execErr.Kind)
	}
}

// --- Deterministic command construction ---

func TestBuildFreeCADRuntimeExecutionCommand_WithoutOutputDir(t *testing.T) {
	req := FreeCADRuntimeExecutionRequest{
		RuntimeCommand: "parametron-freecad",
		WorkingCopyDir: "/tmp/work",
		ManifestPath:   "/tmp/work/export_manifest_v1.json",
		ResultPath:     "/tmp/work/result.json",
	}
	cmd, err := buildFreeCADRuntimeExecutionCommand(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cmd.Path != "parametron-freecad" {
		t.Fatalf("expected executable %q, got %q", "parametron-freecad", cmd.Path)
	}

	wantArgs := []string{
		"execute",
		"--working-copy", "/tmp/work",
		"--manifest", "/tmp/work/export_manifest_v1.json",
		"--result", "/tmp/work/result.json",
	}
	if len(cmd.Args) != len(wantArgs) {
		t.Fatalf("expected %d args, got %d: %v", len(wantArgs), len(cmd.Args), cmd.Args)
	}
	for i, want := range wantArgs {
		if cmd.Args[i] != want {
			t.Fatalf("args[%d]: expected %q, got %q", i, want, cmd.Args[i])
		}
	}

	argStr := strings.Join(cmd.Args, " ")
	for _, forbidden := range []string{"freecad_runner.py", "bridge/observe.py", "PARAMETRON_MANIFEST", "PARAMETRON_OUT"} {
		if strings.Contains(argStr, forbidden) {
			t.Fatalf("aligned command args must not contain %q, got: %s", forbidden, argStr)
		}
	}
}

func TestBuildFreeCADRuntimeExecutionCommand_WithOutputDir(t *testing.T) {
	req := FreeCADRuntimeExecutionRequest{
		RuntimeCommand: "parametron-freecad",
		WorkingCopyDir: "/tmp/work",
		ManifestPath:   "/tmp/work/export_manifest_v1.json",
		ResultPath:     "/tmp/work/result.json",
		OutputDir:      "/tmp/work/outputs",
	}
	cmd, err := buildFreeCADRuntimeExecutionCommand(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantArgs := []string{
		"execute",
		"--working-copy", "/tmp/work",
		"--manifest", "/tmp/work/export_manifest_v1.json",
		"--result", "/tmp/work/result.json",
		"--output-dir", "/tmp/work/outputs",
	}
	if len(cmd.Args) != len(wantArgs) {
		t.Fatalf("expected %d args, got %d: %v", len(wantArgs), len(cmd.Args), cmd.Args)
	}
	for i, want := range wantArgs {
		if cmd.Args[i] != want {
			t.Fatalf("args[%d]: expected %q, got %q", i, want, cmd.Args[i])
		}
	}

	argStr := strings.Join(cmd.Args, " ")
	for _, forbidden := range []string{"freecad_runner.py", "bridge/observe.py", "PARAMETRON_MANIFEST", "PARAMETRON_OUT", "--observation-request"} {
		if strings.Contains(argStr, forbidden) {
			t.Fatalf("aligned command args must not contain %q, got: %s", forbidden, argStr)
		}
	}
}

func TestBuildFreeCADRuntimeExecutionCommand_WithObservationRequest(t *testing.T) {
	req := FreeCADRuntimeExecutionRequest{
		RuntimeCommand:         "  parametron-freecad  ",
		WorkingCopyDir:         "  /tmp/work  ",
		ManifestPath:           "  /tmp/work/export_manifest_v1.json  ",
		ResultPath:             "  /tmp/work/result.json  ",
		OutputDir:              "  /tmp/work/outputs  ",
		ObservationRequestPath: "  /tmp/work/request.json  ",
	}
	cmd, err := buildFreeCADRuntimeExecutionCommand(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantArgs := []string{
		"execute",
		"--working-copy", "/tmp/work",
		"--manifest", "/tmp/work/export_manifest_v1.json",
		"--result", "/tmp/work/result.json",
		"--output-dir", "/tmp/work/outputs",
		"--observation-request", "/tmp/work/request.json",
	}
	if cmd.Path != "parametron-freecad" {
		t.Fatalf("expected executable %q, got %q", "parametron-freecad", cmd.Path)
	}
	if len(cmd.Args) != len(wantArgs) {
		t.Fatalf("expected %d args, got %d: %v", len(wantArgs), len(cmd.Args), cmd.Args)
	}
	for i, want := range wantArgs {
		if cmd.Args[i] != want {
			t.Fatalf("args[%d]: expected %q, got %q", i, want, cmd.Args[i])
		}
	}
}

func TestFreeCADRuntimeExecutionErrorKindsMatchRuntimeCapabilityKinds(t *testing.T) {
	tests := []struct {
		name       string
		freeCAD    string
		runtimecap string
	}{
		{name: "invalid", freeCAD: FreeCADRuntimeExecutionErrorInvalid, runtimecap: runtimecap.ErrorInvalid},
		{name: "start", freeCAD: FreeCADRuntimeExecutionErrorStart, runtimecap: runtimecap.ErrorStart},
		{name: "exit", freeCAD: FreeCADRuntimeExecutionErrorExit, runtimecap: runtimecap.ErrorExit},
		{name: "cancelled", freeCAD: FreeCADRuntimeExecutionErrorCancelled, runtimecap: runtimecap.ErrorCancelled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.freeCAD != tt.runtimecap {
				t.Fatalf("FreeCAD error kind: got %q, want %q", tt.freeCAD, tt.runtimecap)
			}
		})
	}
}

func TestInvokeFreeCADRuntime_ForwardsObservationRequestPathUnchanged(t *testing.T) {
	ctx := context.Background()
	req := FreeCADRuntimeExecutionRequest{
		RuntimeCommand:         "parametron-freecad",
		WorkingCopyDir:         "/tmp/work",
		ManifestPath:           "/tmp/work/manifest.json",
		ResultPath:             "/tmp/work/result.json",
		OutputDir:              "/tmp/work/outputs",
		ObservationRequestPath: "  /tmp/work/request.json  ",
	}
	fake := &fakeRuntimeCapability{
		result: &runtimecap.Result{
			Command: runtimecap.Command{
				Path: "parametron-freecad",
				Args: []string{"execute"},
			},
			Stdout: "delegated-stdout",
			Stderr: "delegated-stderr",
		},
	}

	result, err := invokeFreeCADRuntime(fake, ctx, req)
	if err != nil {
		t.Fatalf("invokeFreeCADRuntime returned error: %v", err)
	}
	if !fake.called {
		t.Fatal("expected provided runtime capability to be invoked")
	}
	wantReq := runtimecap.Request{
		RuntimeCommand:         req.RuntimeCommand,
		WorkingCopyDir:         req.WorkingCopyDir,
		ManifestPath:           req.ManifestPath,
		ResultPath:             req.ResultPath,
		OutputDir:              req.OutputDir,
		ObservationRequestPath: "  /tmp/work/request.json  ",
	}
	if fake.req != wantReq {
		t.Fatalf("adapter must forward ObservationRequestPath unchanged; got %#v, want %#v", fake.req, wantReq)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Stdout != "delegated-stdout" || result.Stderr != "delegated-stderr" {
		t.Fatalf("result conversion changed stdout/stderr: %#v", result)
	}
}

func TestInvokeFreeCADRuntime_ExecutionOnlyPreservesEmptyObservationField(t *testing.T) {
	req := FreeCADRuntimeExecutionRequest{
		RuntimeCommand: "parametron-freecad",
		WorkingCopyDir: "/tmp/work",
		ManifestPath:   "/tmp/work/manifest.json",
		ResultPath:     "/tmp/work/result.json",
	}
	fake := &fakeRuntimeCapability{
		result: &runtimecap.Result{
			Command: runtimecap.Command{Path: "parametron-freecad", Args: []string{"execute"}},
		},
	}

	_, err := invokeFreeCADRuntime(fake, context.Background(), req)
	if err != nil {
		t.Fatalf("invokeFreeCADRuntime returned error: %v", err)
	}
	if fake.req.ObservationRequestPath != "" {
		t.Fatalf("execution-only request must preserve empty observation field, got %q", fake.req.ObservationRequestPath)
	}
	if fake.req.OutputDir != "" {
		t.Fatalf("execution-only request must preserve empty output dir, got %q", fake.req.OutputDir)
	}
}

func TestInvokeFreeCADRuntime_DelegatesToProvidedRuntimeCapability(t *testing.T) {
	ctx := context.Background()
	req := FreeCADRuntimeExecutionRequest{
		RuntimeCommand: "parametron-freecad",
		WorkingCopyDir: "/tmp/work",
		ManifestPath:   "/tmp/work/manifest.json",
		ResultPath:     "/tmp/work/result.json",
		OutputDir:      "/tmp/work/outputs",
	}
	fake := &fakeRuntimeCapability{
		result: &runtimecap.Result{
			Command: runtimecap.Command{
				Path: "parametron-freecad",
				Args: []string{
					"execute",
					"--working-copy", req.WorkingCopyDir,
					"--manifest", req.ManifestPath,
					"--result", req.ResultPath,
					"--output-dir", req.OutputDir,
				},
			},
			Stdout: "runtime stdout",
			Stderr: "runtime stderr",
		},
	}

	result, err := invokeFreeCADRuntime(fake, ctx, req)
	if err != nil {
		t.Fatalf("invokeFreeCADRuntime returned error: %v", err)
	}
	if !fake.called {
		t.Fatal("expected provided runtime capability to be invoked")
	}
	if fake.ctx != ctx {
		t.Fatal("expected runtime capability to receive original context")
	}
	wantReq := runtimecap.Request{
		RuntimeCommand: req.RuntimeCommand,
		WorkingCopyDir: req.WorkingCopyDir,
		ManifestPath:   req.ManifestPath,
		ResultPath:     req.ResultPath,
		OutputDir:      req.OutputDir,
	}
	if fake.req != wantReq {
		t.Fatalf("runtime capability request: got %#v, want %#v", fake.req, wantReq)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Command.Path != fake.result.Command.Path {
		t.Fatalf("command path: got %q, want %q", result.Command.Path, fake.result.Command.Path)
	}
	if strings.Join(result.Command.Args, "\x00") != strings.Join(fake.result.Command.Args, "\x00") {
		t.Fatalf("command args: got %v, want %v", result.Command.Args, fake.result.Command.Args)
	}
	if result.Stdout != "runtime stdout" {
		t.Fatalf("stdout: got %q", result.Stdout)
	}
	if result.Stderr != "runtime stderr" {
		t.Fatalf("stderr: got %q", result.Stderr)
	}
}

func TestInvokeFreeCADRuntime_ObservationEnabledResultConversion(t *testing.T) {
	req := FreeCADRuntimeExecutionRequest{
		RuntimeCommand:         "parametron-freecad",
		WorkingCopyDir:         "/tmp/work",
		ManifestPath:           "/tmp/work/manifest.json",
		ResultPath:             "/tmp/work/result.json",
		OutputDir:              "/tmp/work/outputs",
		ObservationRequestPath: "/tmp/work/request.json",
	}
	wantArgs := []string{
		"execute",
		"--working-copy", req.WorkingCopyDir,
		"--manifest", req.ManifestPath,
		"--result", req.ResultPath,
		"--output-dir", req.OutputDir,
		"--observation-request", req.ObservationRequestPath,
	}
	fake := &fakeRuntimeCapability{
		result: &runtimecap.Result{
			Command: runtimecap.Command{
				Path: "parametron-freecad",
				Args: wantArgs,
			},
			Stdout: "observation-stdout",
			Stderr: "observation-stderr",
		},
	}

	result, err := invokeFreeCADRuntime(fake, context.Background(), req)
	if err != nil {
		t.Fatalf("invokeFreeCADRuntime returned error: %v", err)
	}
	if fake.req.ObservationRequestPath != req.ObservationRequestPath || fake.req.OutputDir != req.OutputDir {
		t.Fatalf("OutputDir and ObservationRequestPath must reach capability together: got %#v", fake.req)
	}
	if result.Command.Path != "parametron-freecad" {
		t.Fatalf("command path: got %q", result.Command.Path)
	}
	if strings.Join(result.Command.Args, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("command args: got %v, want %v", result.Command.Args, wantArgs)
	}
	if result.Stdout != "observation-stdout" {
		t.Fatalf("stdout: got %q", result.Stdout)
	}
	if result.Stderr != "observation-stderr" {
		t.Fatalf("stderr: got %q", result.Stderr)
	}
}

func TestInvokeFreeCADRuntime_ConvertsRuntimeCapabilityErrors(t *testing.T) {
	rootCause := errors.New("runtime root cause")
	tests := []struct {
		name        string
		runtimeKind string
		wantKind    string
		wantMessage string
		exitCode    int
	}{
		{name: "invalid", runtimeKind: runtimecap.ErrorInvalid, wantKind: FreeCADRuntimeExecutionErrorInvalid, wantMessage: "invalid FreeCAD runtime execution request", exitCode: -1},
		{name: "start", runtimeKind: runtimecap.ErrorStart, wantKind: FreeCADRuntimeExecutionErrorStart, wantMessage: "failed to start FreeCAD runtime execution", exitCode: -1},
		{name: "exit", runtimeKind: runtimecap.ErrorExit, wantKind: FreeCADRuntimeExecutionErrorExit, wantMessage: "FreeCAD runtime execution failed", exitCode: 17},
		{name: "cancelled", runtimeKind: runtimecap.ErrorCancelled, wantKind: FreeCADRuntimeExecutionErrorCancelled, wantMessage: "FreeCAD runtime execution cancelled", exitCode: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeRuntimeCapability{
				result: &runtimecap.Result{
					Command: runtimecap.Command{Path: "parametron-freecad", Args: []string{"execute"}},
					Stdout:  "stdout-" + tt.name,
					Stderr:  "stderr-" + tt.name,
				},
				err: &runtimecap.Error{
					Kind:     tt.runtimeKind,
					Message:  "runtime message",
					ExitCode: tt.exitCode,
					Stdout:   "stdout-" + tt.name,
					Stderr:   "stderr-" + tt.name,
					Err:      rootCause,
				},
			}

			result, err := invokeFreeCADRuntime(fake, context.Background(), FreeCADRuntimeExecutionRequest{
				RuntimeCommand: "parametron-freecad",
				WorkingCopyDir: "/tmp/work",
				ManifestPath:   "/tmp/work/manifest.json",
				ResultPath:     "/tmp/work/result.json",
			})
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if result == nil {
				t.Fatal("expected result to be preserved")
			}
			var execErr *FreeCADRuntimeExecutionError
			if !errors.As(err, &execErr) {
				t.Fatalf("expected *FreeCADRuntimeExecutionError, got %T: %v", err, err)
			}
			if execErr.Kind != tt.wantKind {
				t.Fatalf("kind: got %q, want %q", execErr.Kind, tt.wantKind)
			}
			if execErr.Message != tt.wantMessage {
				t.Fatalf("message: got %q, want %q", execErr.Message, tt.wantMessage)
			}
			if execErr.ExitCode != tt.exitCode {
				t.Fatalf("exit code: got %d, want %d", execErr.ExitCode, tt.exitCode)
			}
			if execErr.Stdout != "stdout-"+tt.name {
				t.Fatalf("stdout: got %q", execErr.Stdout)
			}
			if execErr.Stderr != "stderr-"+tt.name {
				t.Fatalf("stderr: got %q", execErr.Stderr)
			}
			if !errors.Is(err, rootCause) {
				t.Fatalf("expected error to wrap root cause")
			}
		})
	}
}

func TestInvokeFreeCADRuntime_ObservationEnabledErrorConversion(t *testing.T) {
	rootCause := errors.New("observation runtime failure")
	wantArgs := []string{
		"execute",
		"--working-copy", "/tmp/work",
		"--manifest", "/tmp/work/manifest.json",
		"--result", "/tmp/work/result.json",
		"--output-dir", "/tmp/work/outputs",
		"--observation-request", "/tmp/work/request.json",
	}
	fake := &fakeRuntimeCapability{
		result: &runtimecap.Result{
			Command: runtimecap.Command{Path: "parametron-freecad", Args: wantArgs},
			Stdout:  "observation-error-stdout",
			Stderr:  "observation-error-stderr",
		},
		err: &runtimecap.Error{
			Kind:     runtimecap.ErrorExit,
			Message:  "runtime capability invocation failed",
			ExitCode: 23,
			Stdout:   "observation-error-stdout",
			Stderr:   "observation-error-stderr",
			Err:      rootCause,
		},
	}

	result, err := invokeFreeCADRuntime(fake, context.Background(), FreeCADRuntimeExecutionRequest{
		RuntimeCommand:         "parametron-freecad",
		WorkingCopyDir:         "/tmp/work",
		ManifestPath:           "/tmp/work/manifest.json",
		ResultPath:             "/tmp/work/result.json",
		OutputDir:              "/tmp/work/outputs",
		ObservationRequestPath: "/tmp/work/request.json",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if result == nil {
		t.Fatal("expected result to be preserved")
	}
	if strings.Join(result.Command.Args, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("command args: got %v, want %v", result.Command.Args, wantArgs)
	}
	var execErr *FreeCADRuntimeExecutionError
	if !errors.As(err, &execErr) {
		t.Fatalf("expected *FreeCADRuntimeExecutionError, got %T: %v", err, err)
	}
	if execErr.Kind != FreeCADRuntimeExecutionErrorExit {
		t.Fatalf("kind: got %q, want %q", execErr.Kind, FreeCADRuntimeExecutionErrorExit)
	}
	if execErr.Kind != runtimecap.ErrorExit {
		t.Fatalf("underlying generic runtime error kind must remain %q, got %q", runtimecap.ErrorExit, execErr.Kind)
	}
	if execErr.ExitCode != 23 {
		t.Fatalf("exit code: got %d, want 23", execErr.ExitCode)
	}
	if execErr.Stdout != "observation-error-stdout" || execErr.Stderr != "observation-error-stderr" {
		t.Fatalf("stdout/stderr not preserved: %#v", execErr)
	}
	if !errors.Is(err, rootCause) {
		t.Fatalf("expected errors.Is to preserve root cause")
	}
	if strings.Contains(err.Error(), "verification") || strings.Contains(err.Error(), "observed") {
		t.Fatalf("adapter must not reinterpret observation as verification/observed error: %v", err)
	}
}

// --- Subprocess success with env filtering ---

func TestExecuteFreeCADRuntime_SuccessWithFakeRuntime(t *testing.T) {
	dir := t.TempDir()
	fakeRuntime := writeFakeRuntime(t, dir, "parametron-freecad", `
if [ -n "$PARAMETRON_MANIFEST" ]; then
    echo "FAIL: PARAMETRON_MANIFEST leaked to aligned subprocess" >&2
    exit 99
fi
if [ -n "$PARAMETRON_OUT" ]; then
    echo "FAIL: PARAMETRON_OUT leaked to aligned subprocess" >&2
    exit 99
fi
echo "stdout-marker"
echo "stderr-marker" >&2
exit 0
`)

	t.Setenv("PARAMETRON_MANIFEST", "/should/not/reach/subprocess/manifest.json")
	t.Setenv("PARAMETRON_OUT", "/should/not/reach/subprocess/out")

	result, err := executeFreeCADRuntime(context.Background(), FreeCADRuntimeExecutionRequest{
		RuntimeCommand: fakeRuntime,
		WorkingCopyDir: dir,
		ManifestPath:   filepath.Join(dir, "manifest.json"),
		ResultPath:     filepath.Join(dir, "result.json"),
	})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !strings.Contains(result.Stdout, "stdout-marker") {
		t.Fatalf("expected stdout-marker in captured stdout, got: %q", result.Stdout)
	}
	if !strings.Contains(result.Stderr, "stderr-marker") {
		t.Fatalf("expected stderr-marker in captured stderr, got: %q", result.Stderr)
	}
}

// --- Non-zero exit ---

func TestExecuteFreeCADRuntime_NonZeroExitReturnsTypedError(t *testing.T) {
	dir := t.TempDir()
	fakeRuntime := writeFakeRuntime(t, dir, "parametron-freecad", `
echo "stdout-nonzero"
echo "stderr-nonzero" >&2
exit 42
`)

	_, err := executeFreeCADRuntime(context.Background(), FreeCADRuntimeExecutionRequest{
		RuntimeCommand: fakeRuntime,
		WorkingCopyDir: dir,
		ManifestPath:   filepath.Join(dir, "manifest.json"),
		ResultPath:     filepath.Join(dir, "result.json"),
	})
	if err == nil {
		t.Fatal("expected error for non-zero exit, got nil")
	}

	var execErr *FreeCADRuntimeExecutionError
	if !errors.As(err, &execErr) {
		t.Fatalf("expected *FreeCADRuntimeExecutionError, got %T: %v", err, err)
	}
	if execErr.Kind != FreeCADRuntimeExecutionErrorExit {
		t.Fatalf("expected kind %q, got %q", FreeCADRuntimeExecutionErrorExit, execErr.Kind)
	}
	if execErr.ExitCode != 42 {
		t.Fatalf("expected exit code 42, got %d", execErr.ExitCode)
	}
	if !strings.Contains(execErr.Stdout, "stdout-nonzero") {
		t.Fatalf("expected stdout preserved in error, got: %q", execErr.Stdout)
	}
	if !strings.Contains(execErr.Stderr, "stderr-nonzero") {
		t.Fatalf("expected stderr preserved in error, got: %q", execErr.Stderr)
	}
}

// --- Start failure ---

func TestExecuteFreeCADRuntime_StartFailureReturnsTypedError(t *testing.T) {
	_, err := executeFreeCADRuntime(context.Background(), FreeCADRuntimeExecutionRequest{
		RuntimeCommand: filepath.Join(t.TempDir(), "nonexistent-parametron-freecad-xyz"),
		WorkingCopyDir: "/tmp/work",
		ManifestPath:   "/tmp/work/manifest.json",
		ResultPath:     "/tmp/work/result.json",
	})
	if err == nil {
		t.Fatal("expected error for non-existent runtime, got nil")
	}

	var execErr *FreeCADRuntimeExecutionError
	if !errors.As(err, &execErr) {
		t.Fatalf("expected *FreeCADRuntimeExecutionError, got %T: %v", err, err)
	}
	if execErr.Kind != FreeCADRuntimeExecutionErrorStart {
		t.Fatalf("expected kind %q, got %q", FreeCADRuntimeExecutionErrorStart, execErr.Kind)
	}
	if execErr.Unwrap() == nil {
		t.Fatal("expected Unwrap to return root cause, got nil")
	}
	if strings.Contains(err.Error(), "freecad_runner.py") {
		t.Fatalf("start failure must not mention freecad_runner.py, got: %v", err)
	}
}

// --- Context cancellation ---

func TestExecuteFreeCADRuntime_CancellationReturnsTypedError(t *testing.T) {
	dir := t.TempDir()
	fakeRuntime := writeFakeRuntime(t, dir, "parametron-freecad", `
sleep 5
exit 0
`)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := executeFreeCADRuntime(ctx, FreeCADRuntimeExecutionRequest{
		RuntimeCommand: fakeRuntime,
		WorkingCopyDir: dir,
		ManifestPath:   filepath.Join(dir, "manifest.json"),
		ResultPath:     filepath.Join(dir, "result.json"),
	})
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}

	var execErr *FreeCADRuntimeExecutionError
	if !errors.As(err, &execErr) {
		t.Fatalf("expected *FreeCADRuntimeExecutionError, got %T: %v", err, err)
	}
	if execErr.Kind != FreeCADRuntimeExecutionErrorCancelled {
		t.Fatalf("expected kind %q, got %q", FreeCADRuntimeExecutionErrorCancelled, execErr.Kind)
	}
	if strings.Contains(err.Error(), "freecad_runner.py") {
		t.Fatalf("cancellation error must not mention freecad_runner.py, got: %v", err)
	}
}

// --- Adapter method wiring ---

func TestFreeCADAdapter_RunFreeCADRuntimeExecution_DelegatesToAlignedBoundary(t *testing.T) {
	dir := t.TempDir()
	fakeRuntime := writeFakeRuntime(t, dir, "parametron-freecad", `
echo "adapter-wiring-stdout"
exit 0
`)

	adp := NewFreeCADAdapter(dir)
	result, err := adp.runFreeCADRuntimeExecution(context.Background(), FreeCADRuntimeExecutionRequest{
		RuntimeCommand: fakeRuntime,
		WorkingCopyDir: dir,
		ManifestPath:   filepath.Join(dir, "manifest.json"),
		ResultPath:     filepath.Join(dir, "result.json"),
	})
	if err != nil {
		t.Fatalf("expected success from adapter method, got error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result from adapter method")
	}
	if !strings.Contains(result.Stdout, "adapter-wiring-stdout") {
		t.Fatalf("expected adapter-wiring-stdout in result stdout, got: %q", result.Stdout)
	}
}

func TestFreeCADAdapter_RunFreeCADRuntimeExecution_DoesNotInjectLegacyEnvVars(t *testing.T) {
	dir := t.TempDir()
	fakeRuntime := writeFakeRuntime(t, dir, "parametron-freecad", `
if [ -n "$PARAMETRON_MANIFEST" ]; then
    echo "FAIL: PARAMETRON_MANIFEST injected by adapter" >&2
    exit 1
fi
if [ -n "$PARAMETRON_OUT" ]; then
    echo "FAIL: PARAMETRON_OUT injected by adapter" >&2
    exit 1
fi
echo "no-legacy-env"
exit 0
`)

	t.Setenv("PARAMETRON_MANIFEST", "/injected/by/parent/manifest.json")
	t.Setenv("PARAMETRON_OUT", "/injected/by/parent/out")

	adp := NewFreeCADAdapter(dir)
	result, err := adp.runFreeCADRuntimeExecution(context.Background(), FreeCADRuntimeExecutionRequest{
		RuntimeCommand: fakeRuntime,
		WorkingCopyDir: dir,
		ManifestPath:   filepath.Join(dir, "manifest.json"),
		ResultPath:     filepath.Join(dir, "result.json"),
	})
	if err != nil {
		t.Fatalf("adapter method must not inject legacy env vars; got error: %v\nstderr: %v", err, func() string {
			if e, ok := err.(*FreeCADRuntimeExecutionError); ok {
				return e.Stderr
			}
			return ""
		}())
	}
	if result == nil || !strings.Contains(result.Stdout, "no-legacy-env") {
		t.Fatalf("expected no-legacy-env in result stdout, got: %v", result)
	}
}

func TestFreeCADAdapter_RunFreeCADRuntimeExecution_DoesNotRouteThroughRunPython(t *testing.T) {
	dir := t.TempDir()
	// The fake runtime name and invocation prove it is not freecad_runner.py or bridge/observe.py.
	fakeRuntime := writeFakeRuntime(t, dir, "parametron-freecad", `
# Fail if invoked as freecad_runner.py or bridge/observe.py.
case "$0" in
    *freecad_runner.py*|*bridge/observe.py*)
        echo "FAIL: routed through legacy script" >&2
        exit 1 ;;
esac
echo "aligned-path-ok"
exit 0
`)

	adp := NewFreeCADAdapter(dir)
	result, err := adp.runFreeCADRuntimeExecution(context.Background(), FreeCADRuntimeExecutionRequest{
		RuntimeCommand: fakeRuntime,
		WorkingCopyDir: dir,
		ManifestPath:   filepath.Join(dir, "manifest.json"),
		ResultPath:     filepath.Join(dir, "result.json"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || !strings.Contains(result.Stdout, "aligned-path-ok") {
		t.Fatalf("expected aligned-path-ok in result stdout, got: %v", result)
	}
}

// --- Env filtering unit test ---

func TestFreeCADRuntimeExecutionEnv_FiltersLegacyEnvVars(t *testing.T) {
	input := []string{
		"PATH=/usr/bin",
		"PARAMETRON_MANIFEST=/some/manifest.json",
		"HOME=/root",
		"PARAMETRON_OUT=/some/out",
		"TERM=xterm",
	}
	filtered := freeCADRuntimeExecutionEnv(input)

	for _, entry := range filtered {
		if strings.HasPrefix(entry, "PARAMETRON_MANIFEST=") {
			t.Fatalf("PARAMETRON_MANIFEST must be filtered, found in: %v", filtered)
		}
		if strings.HasPrefix(entry, "PARAMETRON_OUT=") {
			t.Fatalf("PARAMETRON_OUT must be filtered, found in: %v", filtered)
		}
	}

	envMap := make(map[string]string, len(filtered))
	for _, entry := range filtered {
		if parts := strings.SplitN(entry, "=", 2); len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}
	for _, key := range []string{"PATH", "HOME", "TERM"} {
		if _, ok := envMap[key]; !ok {
			t.Fatalf("expected %s to be preserved in filtered env, got: %v", key, filtered)
		}
	}
}

func TestFreeCADRuntimeExecutionEnv_PreservesNonLegacyVarsUnchanged(t *testing.T) {
	input := []string{
		"PARAMETRON_FREECAD_BIN=/usr/bin/freecadcmd",
		"PARAMETRON_OTHER=value",
		"PATH=/usr/local/bin:/usr/bin",
	}
	filtered := freeCADRuntimeExecutionEnv(input)

	// Only PARAMETRON_MANIFEST and PARAMETRON_OUT should be removed; others are preserved.
	if len(filtered) != len(input) {
		t.Fatalf("expected no entries removed, got filtered len %d from input len %d: %v", len(filtered), len(input), filtered)
	}
}
