package runtimecap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func writeFakeRuntime(t *testing.T, dir, name, script string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0755); err != nil {
		t.Fatalf("failed to write fake runtime %s: %v", name, err)
	}
	return path
}

func validExternalRequest(runtimeCommand, dir string) Request {
	return Request{
		RuntimeCommand: runtimeCommand,
		WorkingCopyDir: filepath.Join(dir, "work"),
		ManifestPath:   filepath.Join(dir, "work", "manifest.json"),
		ResultPath:     filepath.Join(dir, "work", "result.json"),
	}
}

func TestNormalizeRequest_TrimsObservationRequestPath(t *testing.T) {
	input := Request{
		RuntimeCommand:         "  parametron-freecad  ",
		WorkingCopyDir:         "  /work  ",
		ManifestPath:           "  /work/manifest.json  ",
		ResultPath:             "  /work/result.json  ",
		OutputDir:              "  /work/outputs  ",
		ObservationRequestPath: "  /work/request.json  ",
	}
	original := input

	normalized, err := NormalizeRequest(input)
	if err != nil {
		t.Fatalf("NormalizeRequest returned error: %v", err)
	}
	if normalized.ObservationRequestPath != "/work/request.json" {
		t.Fatalf("ObservationRequestPath: got %q, want %q", normalized.ObservationRequestPath, "/work/request.json")
	}
	if normalized.RuntimeCommand != "parametron-freecad" {
		t.Fatalf("RuntimeCommand: got %q, want %q", normalized.RuntimeCommand, "parametron-freecad")
	}
	if normalized.WorkingCopyDir != "/work" {
		t.Fatalf("WorkingCopyDir: got %q, want %q", normalized.WorkingCopyDir, "/work")
	}
	if normalized.ManifestPath != "/work/manifest.json" {
		t.Fatalf("ManifestPath: got %q, want %q", normalized.ManifestPath, "/work/manifest.json")
	}
	if normalized.ResultPath != "/work/result.json" {
		t.Fatalf("ResultPath: got %q, want %q", normalized.ResultPath, "/work/result.json")
	}
	if normalized.OutputDir != "/work/outputs" {
		t.Fatalf("OutputDir: got %q, want %q", normalized.OutputDir, "/work/outputs")
	}
	if input != original {
		t.Fatalf("NormalizeRequest mutated caller-owned input: got %#v, want %#v", input, original)
	}
}

func TestNormalizeRequest_OmittedObservationRequestPathRemainsEmpty(t *testing.T) {
	normalized, err := NormalizeRequest(Request{
		RuntimeCommand: "parametron-freecad",
		WorkingCopyDir: "/work",
		ManifestPath:   "/work/manifest.json",
		ResultPath:     "/work/result.json",
		OutputDir:      "/work/outputs",
	})
	if err != nil {
		t.Fatalf("NormalizeRequest returned error: %v", err)
	}
	if normalized.ObservationRequestPath != "" {
		t.Fatalf("ObservationRequestPath: got %q, want empty", normalized.ObservationRequestPath)
	}
}

func TestNormalizeRequest_RejectsWhitespaceOnlyObservationRequestPath(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "started")
	fake := writeFakeRuntime(t, dir, "parametron-freecad", `
touch "$RUNTIMECAP_MARKER"
exit 0
`)
	t.Setenv("RUNTIMECAP_MARKER", marker)

	req := validExternalRequest(fake, dir)
	req.OutputDir = filepath.Join(dir, "outputs")
	req.ObservationRequestPath = "   "

	result, err := (ExternalCapability{}).Invoke(context.Background(), req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if result != nil {
		t.Fatalf("expected nil result, got %#v", result)
	}
	var runtimeErr *Error
	if !errors.As(err, &runtimeErr) {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if runtimeErr.Kind != ErrorInvalid {
		t.Fatalf("error kind: got %q, want %q", runtimeErr.Kind, ErrorInvalid)
	}
	if !strings.Contains(err.Error(), "observation request") {
		t.Fatalf("error should mention observation request path, got: %v", err)
	}
	if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("subprocess should not start for whitespace-only observation request; marker stat error: %v", statErr)
	}
}

func TestNormalizeRequest_ObservationOutputDependencyMatrix(t *testing.T) {
	base := Request{
		RuntimeCommand: "parametron-freecad",
		WorkingCopyDir: "/work",
		ManifestPath:   "/work/manifest.json",
		ResultPath:     "/work/result.json",
	}
	tests := []struct {
		name                   string
		observationRequestPath string
		outputDir              string
		wantErr                string
	}{
		{name: "empty observation and empty output", observationRequestPath: "", outputDir: "", wantErr: ""},
		{name: "empty observation and nonempty output", observationRequestPath: "", outputDir: "/work/outputs", wantErr: ""},
		{name: "nonempty observation and empty output", observationRequestPath: "/work/request.json", outputDir: "", wantErr: "observation request requires an output directory"},
		{name: "nonempty observation and nonempty output", observationRequestPath: "/work/request.json", outputDir: "/work/outputs", wantErr: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := base
			req.ObservationRequestPath = tt.observationRequestPath
			req.OutputDir = tt.outputDir

			normalized, err := NormalizeRequest(req)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("NormalizeRequest returned error: %v", err)
				}
				if normalized.ObservationRequestPath != tt.observationRequestPath {
					t.Fatalf("ObservationRequestPath: got %q, want %q", normalized.ObservationRequestPath, tt.observationRequestPath)
				}
				if normalized.OutputDir != tt.outputDir {
					t.Fatalf("OutputDir: got %q, want %q", normalized.OutputDir, tt.outputDir)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error: got %q, want substring %q", err.Error(), tt.wantErr)
			}

			dir := t.TempDir()
			marker := filepath.Join(dir, "started")
			fake := writeFakeRuntime(t, dir, "parametron-freecad", `
touch "$RUNTIMECAP_MARKER"
exit 0
`)
			t.Setenv("RUNTIMECAP_MARKER", marker)
			invokeReq := validExternalRequest(fake, dir)
			invokeReq.ObservationRequestPath = tt.observationRequestPath
			invokeReq.OutputDir = tt.outputDir
			result, invokeErr := (ExternalCapability{}).Invoke(context.Background(), invokeReq)
			if invokeErr == nil {
				t.Fatal("expected Invoke error, got nil")
			}
			if result != nil {
				t.Fatalf("expected nil result, got %#v", result)
			}
			var runtimeErr *Error
			if !errors.As(invokeErr, &runtimeErr) {
				t.Fatalf("expected *Error, got %T: %v", invokeErr, invokeErr)
			}
			if runtimeErr.Kind != ErrorInvalid {
				t.Fatalf("error kind: got %q, want %q", runtimeErr.Kind, ErrorInvalid)
			}
			if !strings.Contains(invokeErr.Error(), "observation request requires an output directory") {
				t.Fatalf("error should identify observation/output dependency, got: %v", invokeErr)
			}
			if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("subprocess should not start; marker stat error: %v", statErr)
			}
		})
	}
}

func TestBuildCommand_ExecutionOnlyOmitsOptionalFlags(t *testing.T) {
	cmd, err := BuildCommand(Request{
		RuntimeCommand: "parametron-freecad",
		WorkingCopyDir: "/work",
		ManifestPath:   "/work/manifest.json",
		ResultPath:     "/work/result.json",
	})
	if err != nil {
		t.Fatalf("BuildCommand returned error: %v", err)
	}
	if cmd.Path != "parametron-freecad" {
		t.Fatalf("command path: got %q, want %q", cmd.Path, "parametron-freecad")
	}
	wantArgs := []string{
		"execute",
		"--working-copy", "/work",
		"--manifest", "/work/manifest.json",
		"--result", "/work/result.json",
	}
	if !slices.Equal(cmd.Args, wantArgs) {
		t.Fatalf("command args: got %v, want %v", cmd.Args, wantArgs)
	}
	for _, forbidden := range []string{"--output-dir", "--observation-request"} {
		if slices.Contains(cmd.Args, forbidden) {
			t.Fatalf("execution-only args must not contain %q: %v", forbidden, cmd.Args)
		}
	}
}

func TestBuildCommand_OutputDirOnlyOmitsObservationRequest(t *testing.T) {
	cmd, err := BuildCommand(Request{
		RuntimeCommand: "parametron-freecad",
		WorkingCopyDir: "/work",
		ManifestPath:   "/work/manifest.json",
		ResultPath:     "/work/result.json",
		OutputDir:      "/work/outputs",
	})
	if err != nil {
		t.Fatalf("BuildCommand returned error: %v", err)
	}
	wantArgs := []string{
		"execute",
		"--working-copy", "/work",
		"--manifest", "/work/manifest.json",
		"--result", "/work/result.json",
		"--output-dir", "/work/outputs",
	}
	if !slices.Equal(cmd.Args, wantArgs) {
		t.Fatalf("command args: got %v, want %v", cmd.Args, wantArgs)
	}
	if slices.Contains(cmd.Args, "--observation-request") {
		t.Fatalf("output-dir-only args must not contain --observation-request: %v", cmd.Args)
	}
}

func TestBuildCommand_WithObservationRequest(t *testing.T) {
	req := Request{
		RuntimeCommand:         "  parametron-freecad  ",
		WorkingCopyDir:         "  /work  ",
		ManifestPath:           "  /work/manifest.json  ",
		ResultPath:             "  /work/result.json  ",
		OutputDir:              "  /work/outputs  ",
		ObservationRequestPath: "  /work/request.json  ",
	}
	wantArgs := []string{
		"execute",
		"--working-copy", "/work",
		"--manifest", "/work/manifest.json",
		"--result", "/work/result.json",
		"--output-dir", "/work/outputs",
		"--observation-request", "/work/request.json",
	}

	first, err := BuildCommand(req)
	if err != nil {
		t.Fatalf("BuildCommand returned error: %v", err)
	}
	if first.Path != "parametron-freecad" {
		t.Fatalf("command path: got %q, want %q", first.Path, "parametron-freecad")
	}
	if !slices.Equal(first.Args, wantArgs) {
		t.Fatalf("command args: got %v, want %v", first.Args, wantArgs)
	}
	outputIdx := slices.Index(first.Args, "--output-dir")
	observationIdx := slices.Index(first.Args, "--observation-request")
	if outputIdx < 0 || observationIdx < 0 || outputIdx >= observationIdx {
		t.Fatalf("--output-dir must precede --observation-request in %v", first.Args)
	}

	second, err := BuildCommand(req)
	if err != nil {
		t.Fatalf("second BuildCommand returned error: %v", err)
	}
	if !slices.Equal(first.Args, second.Args) {
		t.Fatalf("repeated equivalent requests must produce identical args: first %v second %v", first.Args, second.Args)
	}
}

func TestBuildCommand_WithReferenceTraversalRequest(t *testing.T) {
	req := Request{RuntimeCommand: "/runtime", WorkingCopyDir: "/work", ManifestPath: "/work/manifest.json", ResultPath: "/work/result.json", OutputDir: "/work/outputs", ReferenceTraversalRequestPath: " /work/traversal.json "}
	got, err := BuildCommand(req)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"execute", "--working-copy", "/work", "--manifest", "/work/manifest.json", "--result", "/work/result.json", "--output-dir", "/work/outputs", "--reference-traversal-request", "/work/traversal.json"}
	if !slices.Equal(got.Args, want) {
		t.Fatalf("args=%v want=%v", got.Args, want)
	}
}

func TestNormalizeRequest_ReferenceTraversalRequiresOutputDirectory(t *testing.T) {
	_, err := NormalizeRequest(Request{RuntimeCommand: "/runtime", WorkingCopyDir: "/work", ManifestPath: "/work/manifest.json", ResultPath: "/work/result.json", ReferenceTraversalRequestPath: "/work/traversal.json"})
	if err == nil || !strings.Contains(err.Error(), "requires an output directory") {
		t.Fatalf("error=%v", err)
	}
}

func TestExternalCapability_InvokeBuildsAlignedCommandWithoutOutputDir(t *testing.T) {
	dir := t.TempDir()
	fake := writeFakeRuntime(t, dir, "parametron-freecad", `
printf '%s\n' "$@"
`)
	req := validExternalRequest(fake, dir)

	result, err := (ExternalCapability{}).Invoke(context.Background(), req)
	if err != nil {
		t.Fatalf("Invoke returned error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Command.Path != fake {
		t.Fatalf("command path: got %q, want %q", result.Command.Path, fake)
	}
	wantArgs := []string{
		"execute",
		"--working-copy", req.WorkingCopyDir,
		"--manifest", req.ManifestPath,
		"--result", req.ResultPath,
	}
	if !slices.Equal(result.Command.Args, wantArgs) {
		t.Fatalf("command args: got %v, want %v", result.Command.Args, wantArgs)
	}
	if gotStdout := strings.Fields(result.Stdout); !slices.Equal(gotStdout, wantArgs) {
		t.Fatalf("subprocess args: got %v, want %v", gotStdout, wantArgs)
	}
}

func TestExternalCapability_InvokeBuildsAlignedCommandWithOutputDir(t *testing.T) {
	dir := t.TempDir()
	fake := writeFakeRuntime(t, dir, "parametron-freecad", `
printf '%s\n' "$@"
`)
	req := validExternalRequest(fake, dir)
	req.OutputDir = filepath.Join(dir, "outputs")

	result, err := (ExternalCapability{}).Invoke(context.Background(), req)
	if err != nil {
		t.Fatalf("Invoke returned error: %v", err)
	}
	wantArgs := []string{
		"execute",
		"--working-copy", req.WorkingCopyDir,
		"--manifest", req.ManifestPath,
		"--result", req.ResultPath,
		"--output-dir", req.OutputDir,
	}
	if !slices.Equal(result.Command.Args, wantArgs) {
		t.Fatalf("command args: got %v, want %v", result.Command.Args, wantArgs)
	}
	if gotStdout := strings.Fields(result.Stdout); !slices.Equal(gotStdout, wantArgs) {
		t.Fatalf("subprocess args: got %v, want %v", gotStdout, wantArgs)
	}
}

func TestExternalCapability_InvokeBuildsAlignedCommandWithObservationRequest(t *testing.T) {
	dir := t.TempDir()
	callCountPath := filepath.Join(dir, "call-count")
	fake := writeFakeRuntime(t, dir, "parametron-freecad", `
echo 1 >> "$RUNTIMECAP_CALL_COUNT"
printf '%s\n' "$@"
echo "stdout-observation"
echo "stderr-observation" >&2
`)
	t.Setenv("RUNTIMECAP_CALL_COUNT", callCountPath)

	req := validExternalRequest(fake, dir)
	req.OutputDir = filepath.Join(dir, "outputs")
	// Nonexistent path must still reach the fake executable; filesystem validation is FreeCAD-owned.
	req.ObservationRequestPath = filepath.Join(dir, "missing", "request.json")

	result, err := (ExternalCapability{}).Invoke(context.Background(), req)
	if err != nil {
		t.Fatalf("Invoke returned error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Command.Path != fake {
		t.Fatalf("command path: got %q, want %q", result.Command.Path, fake)
	}
	wantArgs := []string{
		"execute",
		"--working-copy", req.WorkingCopyDir,
		"--manifest", req.ManifestPath,
		"--result", req.ResultPath,
		"--output-dir", req.OutputDir,
		"--observation-request", req.ObservationRequestPath,
	}
	if !slices.Equal(result.Command.Args, wantArgs) {
		t.Fatalf("command args: got %v, want %v", result.Command.Args, wantArgs)
	}
	if gotStdoutLines := strings.Split(strings.TrimSpace(result.Stdout), "\n"); !slices.Equal(gotStdoutLines[:len(wantArgs)], wantArgs) {
		t.Fatalf("subprocess args: got %v, want %v", gotStdoutLines[:len(wantArgs)], wantArgs)
	}
	if !strings.Contains(result.Stdout, "stdout-observation") {
		t.Fatalf("stdout not preserved: %q", result.Stdout)
	}
	if !strings.Contains(result.Stderr, "stderr-observation") {
		t.Fatalf("stderr not preserved: %q", result.Stderr)
	}
	countBytes, readErr := os.ReadFile(callCountPath)
	if readErr != nil {
		t.Fatalf("failed to read call count: %v", readErr)
	}
	if got := strings.Count(string(countBytes), "1"); got != 1 {
		t.Fatalf("expected runtime command invoked once, got %d", got)
	}
	if _, statErr := os.Stat(req.ObservationRequestPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("test must not create observation request file; stat error: %v", statErr)
	}
}

func TestExternalCapability_InvokeRejectsInvalidRequestsBeforeSubprocess(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "started")
	fake := writeFakeRuntime(t, dir, "parametron-freecad", `
touch "$RUNTIMECAP_MARKER"
exit 0
`)
	t.Setenv("RUNTIMECAP_MARKER", marker)

	valid := validExternalRequest(fake, dir)
	tests := []struct {
		name string
		req  Request
	}{
		{name: "empty runtime command", req: Request{WorkingCopyDir: valid.WorkingCopyDir, ManifestPath: valid.ManifestPath, ResultPath: valid.ResultPath}},
		{name: "empty working copy", req: Request{RuntimeCommand: valid.RuntimeCommand, ManifestPath: valid.ManifestPath, ResultPath: valid.ResultPath}},
		{name: "empty manifest", req: Request{RuntimeCommand: valid.RuntimeCommand, WorkingCopyDir: valid.WorkingCopyDir, ResultPath: valid.ResultPath}},
		{name: "empty result", req: Request{RuntimeCommand: valid.RuntimeCommand, WorkingCopyDir: valid.WorkingCopyDir, ManifestPath: valid.ManifestPath}},
		{name: "blank output dir", req: Request{RuntimeCommand: valid.RuntimeCommand, WorkingCopyDir: valid.WorkingCopyDir, ManifestPath: valid.ManifestPath, ResultPath: valid.ResultPath, OutputDir: " \t "}},
		{name: "blank observation request", req: Request{RuntimeCommand: valid.RuntimeCommand, WorkingCopyDir: valid.WorkingCopyDir, ManifestPath: valid.ManifestPath, ResultPath: valid.ResultPath, OutputDir: filepath.Join(dir, "outputs"), ObservationRequestPath: " \t "}},
		{name: "observation without output dir", req: Request{RuntimeCommand: valid.RuntimeCommand, WorkingCopyDir: valid.WorkingCopyDir, ManifestPath: valid.ManifestPath, ResultPath: valid.ResultPath, ObservationRequestPath: filepath.Join(dir, "request.json")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = os.Remove(marker)
			result, err := (ExternalCapability{}).Invoke(context.Background(), tt.req)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if result != nil {
				t.Fatalf("expected nil result, got %#v", result)
			}
			var runtimeErr *Error
			if !errors.As(err, &runtimeErr) {
				t.Fatalf("expected *Error, got %T: %v", err, err)
			}
			if runtimeErr.Kind != ErrorInvalid {
				t.Fatalf("error kind: got %q, want %q", runtimeErr.Kind, ErrorInvalid)
			}
			if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("subprocess should not start for invalid input; marker stat error: %v", statErr)
			}
		})
	}
}

func TestExternalCapability_InvokeRejectsNilContextBeforeSubprocess(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "started")
	fake := writeFakeRuntime(t, dir, "parametron-freecad", `
touch "$RUNTIMECAP_MARKER"
exit 0
`)
	t.Setenv("RUNTIMECAP_MARKER", marker)

	//nolint:staticcheck
	result, err := (ExternalCapability{}).Invoke(nil, validExternalRequest(fake, dir))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if result != nil {
		t.Fatalf("expected nil result, got %#v", result)
	}
	var runtimeErr *Error
	if !errors.As(err, &runtimeErr) {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if runtimeErr.Kind != ErrorInvalid {
		t.Fatalf("error kind: got %q, want %q", runtimeErr.Kind, ErrorInvalid)
	}
	if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("subprocess should not start for nil context; marker stat error: %v", statErr)
	}
}

func TestExternalCapability_InvokeFiltersLegacyEnvironment(t *testing.T) {
	dir := t.TempDir()
	fake := writeFakeRuntime(t, dir, "parametron-freecad", `
if [ -n "$PARAMETRON_MANIFEST" ]; then
    echo "PARAMETRON_MANIFEST leaked" >&2
    exit 99
fi
if [ -n "$PARAMETRON_OUT" ]; then
    echo "PARAMETRON_OUT leaked" >&2
    exit 99
fi
if [ -n "$PARAMETRON_OBSERVATION_REQUEST" ]; then
    echo "PARAMETRON_OBSERVATION_REQUEST leaked" >&2
    exit 99
fi
if [ -z "$RUNTIMECAP_UNRELATED" ]; then
    echo "unrelated env missing" >&2
    exit 99
fi
printf '%s\n' "$@"
echo "legacy-env-absent"
`)
	t.Setenv("PARAMETRON_MANIFEST", filepath.Join(dir, "legacy-manifest.json"))
	t.Setenv("PARAMETRON_OUT", filepath.Join(dir, "legacy-out"))
	t.Setenv("RUNTIMECAP_UNRELATED", "keep-me")

	req := validExternalRequest(fake, dir)
	req.OutputDir = filepath.Join(dir, "outputs")
	req.ObservationRequestPath = filepath.Join(dir, "missing-request.json")

	result, err := (ExternalCapability{}).Invoke(context.Background(), req)
	if err != nil {
		t.Fatalf("Invoke returned error: %v", err)
	}
	if !strings.Contains(result.Stdout, "legacy-env-absent") {
		t.Fatalf("expected child process to report absent legacy env, got stdout %q stderr %q", result.Stdout, result.Stderr)
	}
	wantArgs := []string{
		"execute",
		"--working-copy", req.WorkingCopyDir,
		"--manifest", req.ManifestPath,
		"--result", req.ResultPath,
		"--output-dir", req.OutputDir,
		"--observation-request", req.ObservationRequestPath,
	}
	gotArgs := strings.Split(strings.TrimSpace(result.Stdout), "\n")
	gotArgs = gotArgs[:len(wantArgs)]
	if !slices.Equal(gotArgs, wantArgs) {
		t.Fatalf("observation must be passed only via CLI args: got %v, want %v", gotArgs, wantArgs)
	}
	for _, forbidden := range []string{"PARAMETRON_OBSERVATION", "PARAMETRON_OUTPUT"} {
		if strings.Contains(result.Stdout, forbidden) || strings.Contains(result.Stderr, forbidden) {
			t.Fatalf("no new observation/output environment variable should be introduced; got stdout %q stderr %q", result.Stdout, result.Stderr)
		}
	}
}

func TestExternalCapability_InvokeNonZeroExitReturnsTypedError(t *testing.T) {
	dir := t.TempDir()
	fake := writeFakeRuntime(t, dir, "parametron-freecad", `
echo "stdout-nonzero"
echo "stderr-nonzero" >&2
exit 37
`)

	result, err := (ExternalCapability{}).Invoke(context.Background(), validExternalRequest(fake, dir))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if result == nil {
		t.Fatal("expected result with captured command and output")
	}
	var runtimeErr *Error
	if !errors.As(err, &runtimeErr) {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if runtimeErr.Kind != ErrorExit {
		t.Fatalf("error kind: got %q, want %q", runtimeErr.Kind, ErrorExit)
	}
	if runtimeErr.ExitCode != 37 {
		t.Fatalf("exit code: got %d, want 37", runtimeErr.ExitCode)
	}
	if !strings.Contains(runtimeErr.Stdout, "stdout-nonzero") {
		t.Fatalf("stdout not preserved: %q", runtimeErr.Stdout)
	}
	if !strings.Contains(runtimeErr.Stderr, "stderr-nonzero") {
		t.Fatalf("stderr not preserved: %q", runtimeErr.Stderr)
	}
}

func TestExternalCapability_InvokeObservationEnabledNonZeroExitPreservesErrorBehavior(t *testing.T) {
	dir := t.TempDir()
	fake := writeFakeRuntime(t, dir, "parametron-freecad", `
echo "stdout-observation-exit"
echo "stderr-observation-exit" >&2
exit 41
`)
	req := validExternalRequest(fake, dir)
	req.OutputDir = filepath.Join(dir, "outputs")
	req.ObservationRequestPath = filepath.Join(dir, "missing-request.json")

	result, err := (ExternalCapability{}).Invoke(context.Background(), req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if result == nil {
		t.Fatal("expected result with captured command and output")
	}
	wantArgs := []string{
		"execute",
		"--working-copy", req.WorkingCopyDir,
		"--manifest", req.ManifestPath,
		"--result", req.ResultPath,
		"--output-dir", req.OutputDir,
		"--observation-request", req.ObservationRequestPath,
	}
	if !slices.Equal(result.Command.Args, wantArgs) {
		t.Fatalf("command args: got %v, want %v", result.Command.Args, wantArgs)
	}
	var runtimeErr *Error
	if !errors.As(err, &runtimeErr) {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if runtimeErr.Kind != ErrorExit {
		t.Fatalf("error kind: got %q, want %q", runtimeErr.Kind, ErrorExit)
	}
	if runtimeErr.ExitCode != 41 {
		t.Fatalf("exit code: got %d, want 41", runtimeErr.ExitCode)
	}
	if !strings.Contains(runtimeErr.Stderr, "stderr-observation-exit") {
		t.Fatalf("stderr not preserved: %q", runtimeErr.Stderr)
	}
	if !strings.Contains(runtimeErr.Stdout, "stdout-observation-exit") {
		t.Fatalf("stdout not preserved: %q", runtimeErr.Stdout)
	}
}

func TestExternalCapability_InvokeStartFailureReturnsTypedError(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing-parametron-freecad")

	result, err := (ExternalCapability{}).Invoke(context.Background(), validExternalRequest(missing, dir))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if result == nil {
		t.Fatal("expected result with resolved command")
	}
	var runtimeErr *Error
	if !errors.As(err, &runtimeErr) {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if runtimeErr.Kind != ErrorStart {
		t.Fatalf("error kind: got %q, want %q", runtimeErr.Kind, ErrorStart)
	}
	if runtimeErr.ExitCode != -1 {
		t.Fatalf("exit code: got %d, want -1", runtimeErr.ExitCode)
	}
}

func TestExternalCapability_InvokeCancellationReturnsTypedError(t *testing.T) {
	dir := t.TempDir()
	fake := writeFakeRuntime(t, dir, "parametron-freecad", `
echo "started"
exec tail -f /dev/null
`)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result, err := (ExternalCapability{}).Invoke(ctx, validExternalRequest(fake, dir))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if result == nil {
		t.Fatal("expected result with captured command and output")
	}
	var runtimeErr *Error
	if !errors.As(err, &runtimeErr) {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if runtimeErr.Kind != ErrorCancelled {
		t.Fatalf("error kind: got %q, want %q", runtimeErr.Kind, ErrorCancelled)
	}
	if runtimeErr.Unwrap() == nil {
		t.Fatal("expected wrapped context error")
	}
}
