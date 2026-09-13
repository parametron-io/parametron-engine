package runtimecap

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/handoff"
	"parametron/internal/engine/job"
)

var _ ExecutableResolver = (*DefaultExecutableResolver)(nil)

func testExecutable(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\ntouch \"$SELECTION_MARKER\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func selectionError(t *testing.T, err error, kind, adapter, command string) *ExecutableSelectionError {
	t.Helper()
	var got *ExecutableSelectionError
	if !errors.As(err, &got) {
		t.Fatalf("error=%T %v", err, err)
	}
	if got.Kind != kind || got.Adapter != adapter || got.Command != command {
		t.Fatalf("selection error=%#v", got)
	}
	if !strings.Contains(err.Error(), kind) || (adapter != "" && !strings.Contains(err.Error(), adapter)) || (command != "" && !strings.Contains(err.Error(), command)) {
		t.Fatalf("unhelpful error: %v", err)
	}
	return got
}

func TestDefaultExecutableResolver_ImplementsExecutableResolver(t *testing.T) {
	if FreeCADAdapterID != "freecad" || DefaultFreeCADRuntimeExecutable != "parametron-freecad" || ExecutableSelectionSourceConfigured != "configured" || ExecutableSelectionSourceDefaultPATH != "default_path" {
		t.Fatal("public constants changed")
	}
}

func TestNewExecutableResolver_AcceptsNilAndEmptyConfiguration(t *testing.T) {
	dir := t.TempDir()
	want := testExecutable(t, dir, DefaultFreeCADRuntimeExecutable)
	t.Setenv("PATH", dir)
	var selections []ExecutableSelection
	for _, commands := range []map[string]string{nil, {}} {
		r, err := NewExecutableResolver(commands)
		if err != nil {
			t.Fatal(err)
		}
		got, err := r.Resolve(FreeCADAdapterID)
		if err != nil || got.Path != want {
			t.Fatalf("got=%#v err=%v", got, err)
		}
		selections = append(selections, got)
	}
	if !reflect.DeepEqual(selections[0], selections[1]) {
		t.Fatalf("selections differ: %v", selections)
	}
}

func TestExecutableResolver_ResolvesDefaultFreeCADRuntimeFromPATH(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")
	want := testExecutable(t, dir, DefaultFreeCADRuntimeExecutable)
	t.Setenv("PATH", dir)
	t.Setenv("SELECTION_MARKER", marker)
	r, _ := NewExecutableResolver(nil)
	got, err := r.Resolve(FreeCADAdapterID)
	if err != nil || got != (ExecutableSelection{Adapter: "freecad", Command: "parametron-freecad", Path: want, Source: ExecutableSelectionSourceDefaultPATH}) {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	assertUsableSelection(t, got)
	assertAbsent(t, marker)
}

func TestExecutableResolver_ConfiguredBareCommandOverridesDefault(t *testing.T) {
	dir := t.TempDir()
	want := testExecutable(t, dir, "configured-parametron-freecad")
	testExecutable(t, dir, DefaultFreeCADRuntimeExecutable)
	t.Setenv("PATH", dir)
	r, _ := NewExecutableResolver(map[string]string{"freecad": "configured-parametron-freecad"})
	got, err := r.Resolve("freecad")
	if err != nil || got.Command != "configured-parametron-freecad" || got.Path != want || got.Source != ExecutableSelectionSourceConfigured {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}

func TestExecutableResolver_ResolvesConfiguredAbsolutePath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "runtime wrappers")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := testExecutable(t, dir, "custom-wrapper")
	t.Setenv("PATH", t.TempDir())
	r, _ := NewExecutableResolver(map[string]string{"freecad": want})
	got, err := r.Resolve("freecad")
	if err != nil || got.Command != want || got.Path != want || got.Source != ExecutableSelectionSourceConfigured {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}

func TestExecutableResolver_CleansConfiguredAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	wrappers := filepath.Join(dir, "wrappers")
	if err := os.Mkdir(wrappers, 0o755); err != nil {
		t.Fatal(err)
	}
	want := testExecutable(t, wrappers, "parametron-freecad")
	command := filepath.Join(dir, "wrappers", "..", "wrappers", "parametron-freecad")
	r, _ := NewExecutableResolver(map[string]string{"freecad": command})
	got, err := r.Resolve("freecad")
	if err != nil || got.Path != want || got.Command != want {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}

func TestExecutableResolver_ResolvesExplicitNonFreeCADAdapter(t *testing.T) {
	path := testExecutable(t, t.TempDir(), "runtime")
	t.Setenv("PATH", t.TempDir())
	r, _ := NewExecutableResolver(map[string]string{"solidworks": path})
	got, err := r.Resolve("solidworks")
	if err != nil || got.Adapter != "solidworks" || got.Path != path {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}

func TestExecutableResolver_AcceptsCanonicalAdapterIDs(t *testing.T) {
	path := testExecutable(t, t.TempDir(), "runtime")
	for _, adapter := range []string{"freecad", "solidworks", "cad-runtime.v2", "adapter_1", "adapter-2", "a", "a1"} {
		t.Run(adapter, func(t *testing.T) {
			r, _ := NewExecutableResolver(map[string]string{adapter: path})
			got, err := r.Resolve(adapter)
			if err != nil || got.Adapter != adapter {
				t.Fatalf("got=%#v err=%v", got, err)
			}
		})
	}
}

func TestExecutableResolver_MissingConfiguredCommandDoesNotFallBack(t *testing.T) {
	testFailClosed(t, "missing-explicit-runtime", ExecutableSelectionErrorNotFound)
}
func TestExecutableResolver_MissingConfiguredAbsolutePathDoesNotFallBack(t *testing.T) {
	testFailClosed(t, filepath.Join(t.TempDir(), "missing"), ExecutableSelectionErrorNotFound)
}

func testFailClosed(t *testing.T, command, kind string) {
	t.Helper()
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")
	testExecutable(t, dir, DefaultFreeCADRuntimeExecutable)
	t.Setenv("PATH", dir)
	t.Setenv("SELECTION_MARKER", marker)
	r, _ := NewExecutableResolver(map[string]string{"freecad": command})
	got, err := r.Resolve("freecad")
	if got != (ExecutableSelection{}) {
		t.Fatalf("selection=%#v", got)
	}
	selectionError(t, err, kind, "freecad", command)
	assertAbsent(t, marker)
}

func TestExecutableResolver_InvalidConfiguredCommandDoesNotFallBack(t *testing.T) {
	for _, command := range []string{"", " ", "./parametron-freecad", "bin/parametron-freecad", "../bin/parametron-freecad", "parametron-freecad --debug", "parametron-freecad\n", "parametron-freecad\t", "parametron-freecad\x00"} {
		t.Run(strings.ReplaceAll(command, "/", "_"), func(t *testing.T) { testFailClosed(t, command, ExecutableSelectionErrorInvalidCommand) })
	}
}

func TestExecutableResolver_UnusableConfiguredTargetDoesNotFallBack(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "not-executable")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{dir, file} {
		t.Run(filepath.Base(target), func(t *testing.T) { testFailClosed(t, target, ExecutableSelectionErrorUnusable) })
	}
}

func TestExecutableResolver_RejectsUnconfiguredAdapter(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	r, _ := NewExecutableResolver(nil)
	_, err := r.Resolve("solidworks")
	selectionError(t, err, ExecutableSelectionErrorUnconfigured, "solidworks", "")
}

func TestExecutableResolver_RejectsInvalidAdapterIDs(t *testing.T) {
	for _, adapter := range []string{"", " ", " freecad", "freecad ", "FreeCAD", "FREECAD", "free cad", "../freecad", "freecad/runtime", "freecad\\runtime", ".freecad", "-freecad", "_freecad", "freecad\n", "freecad\t", "freecad\x00"} {
		t.Run(strings.ReplaceAll(adapter, "/", "_"), func(t *testing.T) {
			r, _ := NewExecutableResolver(nil)
			_, err := r.Resolve(adapter)
			selectionError(t, err, ExecutableSelectionErrorInvalidAdapter, adapter, "")
		})
	}
}

func TestExecutableResolver_AcceptsValidBareExecutableNames(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	for _, command := range []string{"parametron-freecad", "parametron_freecad", "parametron-freecad.v2", "runtime1"} {
		testExecutable(t, dir, command)
		t.Run(command, func(t *testing.T) {
			r, _ := NewExecutableResolver(map[string]string{"freecad": command})
			if _, err := r.Resolve("freecad"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestExecutableResolver_RejectsInvalidCommandReferences(t *testing.T) {
	for _, command := range []string{"", " ", "./parametron-freecad", "bin/parametron-freecad", "../parametron-freecad", "bin\\parametron-freecad", "parametron-freecad --debug", "parametron-freecad\n", "parametron-freecad\t", "parametron-freecad\x00"} {
		t.Run(strings.ReplaceAll(command, "/", "_"), func(t *testing.T) {
			r, _ := NewExecutableResolver(map[string]string{"freecad": command})
			_, err := r.Resolve("freecad")
			selectionError(t, err, ExecutableSelectionErrorInvalidCommand, "freecad", command)
		})
	}
}

func TestExecutableResolver_ReturnsAbsoluteCleanRegularExecutablePath(t *testing.T) {
	dir := t.TempDir()
	bare := testExecutable(t, dir, "bare")
	def := testExecutable(t, dir, DefaultFreeCADRuntimeExecutable)
	absolute := testExecutable(t, dir, "absolute")
	t.Setenv("PATH", dir)
	for name, commands := range map[string]map[string]string{"bare": {"freecad": "bare"}, "absolute": {"freecad": absolute}, "default": nil} {
		t.Run(name, func(t *testing.T) {
			r, _ := NewExecutableResolver(commands)
			got, err := r.Resolve("freecad")
			if err != nil {
				t.Fatal(err)
			}
			assertUsableSelection(t, got)
			if name == "bare" && got.Path != bare || name == "default" && got.Path != def {
				t.Fatalf("path=%q", got.Path)
			}
		})
	}
}

func TestExecutableResolver_ErrorKinds(t *testing.T) {
	dir := t.TempDir()
	nonexec := filepath.Join(dir, "nonexec")
	if err := os.WriteFile(nonexec, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	tests := []struct {
		adapter, command, kind string
		configured             bool
	}{{"Bad", "", ExecutableSelectionErrorInvalidAdapter, false}, {"solidworks", "", ExecutableSelectionErrorUnconfigured, false}, {"freecad", "", ExecutableSelectionErrorInvalidCommand, true}, {"freecad", "runtime --arg", ExecutableSelectionErrorInvalidCommand, true}, {"freecad", "missing", ExecutableSelectionErrorNotFound, true}, {"freecad", filepath.Join(dir, "missing"), ExecutableSelectionErrorNotFound, true}, {"freecad", dir, ExecutableSelectionErrorUnusable, true}, {"freecad", nonexec, ExecutableSelectionErrorUnusable, true}}
	for _, tt := range tests {
		t.Run(tt.kind+tt.command, func(t *testing.T) {
			var commands map[string]string
			if tt.configured {
				commands = map[string]string{tt.adapter: tt.command}
			}
			r, _ := NewExecutableResolver(commands)
			_, err := r.Resolve(tt.adapter)
			selectionError(t, err, tt.kind, tt.adapter, tt.command)
		})
	}
}

func TestExecutableSelectionError_UnwrapsUnderlyingFailure(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	r, _ := NewExecutableResolver(map[string]string{"freecad": "missing"})
	_, err := r.Resolve("freecad")
	got := selectionError(t, err, ExecutableSelectionErrorNotFound, "freecad", "missing")
	if got.Unwrap() == nil || errors.Unwrap(err) != got.Err {
		t.Fatalf("unwrap mismatch: %#v", got)
	}
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("underlying lookup error unavailable: %v", err)
	}
}

func TestExecutableSelectionError_NilReceiver(t *testing.T) {
	var err *ExecutableSelectionError
	if err.Error() != "" || err.Unwrap() != nil {
		t.Fatal("nil receiver behavior changed")
	}
}

func TestNewExecutableResolver_CopiesConfiguredCommands(t *testing.T) {
	a := testExecutable(t, t.TempDir(), "a")
	b := testExecutable(t, t.TempDir(), "b")
	commands := map[string]string{"freecad": a}
	r, _ := NewExecutableResolver(commands)
	commands["freecad"] = b
	delete(commands, "freecad")
	commands["solidworks"] = b
	for i := 0; i < 3; i++ {
		got, err := r.Resolve("freecad")
		if err != nil || got.Path != a {
			t.Fatalf("got=%#v err=%v", got, err)
		}
	}
	_, err := r.Resolve("solidworks")
	selectionError(t, err, ExecutableSelectionErrorUnconfigured, "solidworks", "")
}

func TestExecutableResolver_RepeatedResolutionIsDeterministic(t *testing.T) {
	path := testExecutable(t, t.TempDir(), "runtime")
	r, _ := NewExecutableResolver(map[string]string{"freecad": path})
	first, _ := r.Resolve("freecad")
	for i := 0; i < 5; i++ {
		got, err := r.Resolve("freecad")
		if err != nil || !reflect.DeepEqual(got, first) {
			t.Fatalf("got=%#v err=%v", got, err)
		}
	}
	bad, _ := NewExecutableResolver(map[string]string{"freecad": "bad command"})
	var context string
	for i := 0; i < 3; i++ {
		_, err := bad.Resolve("freecad")
		got := selectionError(t, err, ExecutableSelectionErrorInvalidCommand, "freecad", "bad command")
		if i == 0 {
			context = got.Error()
		} else if got.Error() != context {
			t.Fatalf("errors differ: %q %q", context, got.Error())
		}
	}
}

func TestExecutableResolver_InstancesDoNotShareMutableState(t *testing.T) {
	a := testExecutable(t, t.TempDir(), "a")
	b := testExecutable(t, t.TempDir(), "b")
	ca := map[string]string{"freecad": a}
	cb := map[string]string{"freecad": b}
	ra, _ := NewExecutableResolver(ca)
	rb, _ := NewExecutableResolver(cb)
	ca["freecad"] = b
	cb["freecad"] = a
	ga, _ := ra.Resolve("freecad")
	gb, _ := rb.Resolve("freecad")
	if ga.Path != a || gb.Path != b {
		t.Fatalf("a=%#v b=%#v", ga, gb)
	}
}

func TestExecutableResolver_DoesNotExecuteSelectedRuntime(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")
	absolute := testExecutable(t, dir, "absolute")
	testExecutable(t, dir, "bare")
	testExecutable(t, dir, DefaultFreeCADRuntimeExecutable)
	t.Setenv("PATH", dir)
	t.Setenv("SELECTION_MARKER", marker)
	for name, commands := range map[string]map[string]string{"absolute": {"freecad": absolute}, "bare": {"freecad": "bare"}, "default": nil} {
		t.Run(name, func(t *testing.T) {
			r, _ := NewExecutableResolver(commands)
			if _, err := r.Resolve("freecad"); err != nil {
				t.Fatal(err)
			}
			assertAbsent(t, marker)
		})
	}
}

func TestExecutableResolver_InvalidConfiguredCommandDoesNotExecuteDefault(t *testing.T) {
	testFailClosed(t, "missing-explicit", ExecutableSelectionErrorNotFound)
}

func identityFixture(t *testing.T) (*planner.ExecutionPlan, string, interface{}, string, []byte) {
	t.Helper()
	steps := []planner.Step{{Type: planner.StepWriteCSV, Payload: planner.WriteCSVPayload{ProductKey: "widget", Filename: "widget.csv", Headers: []string{"width"}, Values: []interface{}{10.0}}}, {Type: planner.StepWriteExportManifest, Payload: planner.WriteExportManifestPayload{ProductKey: "widget", ManifestFilename: planner.ExportManifestFilename, SchemaVersion: planner.ExportManifestSchemaVersion, Adapter: "freecad", Product: planner.ExportManifestProduct{ID: "widget"}, Values: map[string]interface{}{"width": 10.0}, Outputs: []planner.ExportManifestOutput{{Type: "step", Filename: "widget.step", Object: "Body"}}}}, {Type: planner.StepRunCADRuntime, Payload: planner.RunCADRuntimePayload{ProductKey: "widget", Adapter: "freecad", ManifestFilename: planner.ExportManifestFilename, ResultFilename: "result.json"}}}
	plan := &planner.ExecutionPlan{Steps: steps}
	hash, err := planner.ComputePlanHash(plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	cache, err := planner.ComputeStepCacheKeyContext(plan, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	j, err := job.New("widget", steps)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := handoff.FromJob(j)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return plan, hash, cache, j.ID, data
}

func TestExecutableResolver_SelectionDoesNotAffectEngineeringIdentity(t *testing.T) {
	plan, hash, cache, id, data := identityFixture(t)
	path := testExecutable(t, t.TempDir(), "runtime")
	r, _ := NewExecutableResolver(map[string]string{"freecad": path})
	selection, err := r.Resolve("freecad")
	if err != nil {
		t.Fatal(err)
	}
	afterPlan, afterHash, afterCache, afterID, afterData := identityFixture(t)
	if !reflect.DeepEqual(plan, afterPlan) || hash != afterHash || !reflect.DeepEqual(cache, afterCache) || id != afterID || !slices.Equal(data, afterData) {
		t.Fatal("selection changed engineering identity")
	}
	if strings.Contains(string(data), selection.Path) || strings.Contains(string(data), selection.Command) {
		t.Fatalf("operational selection leaked into handoff: %s", data)
	}
}

func TestExecutableResolver_DifferentOperationalCommandsDoNotAlterEngineeringIdentity(t *testing.T) {
	a := testExecutable(t, t.TempDir(), "a")
	b := testExecutable(t, t.TempDir(), "b")
	ra, _ := NewExecutableResolver(map[string]string{"freecad": a})
	rb, _ := NewExecutableResolver(map[string]string{"freecad": b})
	sa, _ := ra.Resolve("freecad")
	sb, _ := rb.Resolve("freecad")
	_, ha, ca, ia, pa := identityFixture(t)
	_, hb, cb, ib, pb := identityFixture(t)
	if sa.Path == sb.Path || ha != hb || !reflect.DeepEqual(ca, cb) || ia != ib || !slices.Equal(pa, pb) {
		t.Fatal("operational configuration altered logical identity")
	}
}

func TestExecutableResolver_DoesNotModifyRuntimecapRequest(t *testing.T) {
	path := testExecutable(t, t.TempDir(), "runtime")
	r, _ := NewExecutableResolver(map[string]string{"freecad": path})
	req := Request{RuntimeCommand: "caller-supplied-runtime", WorkingCopyDir: "/work", ManifestPath: "/work/manifest.json", ResultPath: "/work/result.json"}
	original := req
	if _, err := r.Resolve("freecad"); err != nil {
		t.Fatal(err)
	}
	cmd, err := BuildCommand(req)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"execute", "--working-copy", "/work", "--manifest", "/work/manifest.json", "--result", "/work/result.json"}
	if req != original || cmd.Path != "caller-supplied-runtime" || !slices.Equal(cmd.Args, want) {
		t.Fatalf("req=%#v cmd=%#v", req, cmd)
	}
}

func TestBuildCommand_RemainsIndependentOfExecutableResolver(t *testing.T) {
	TestExecutableResolver_DoesNotModifyRuntimecapRequest(t)
}

func assertUsableSelection(t *testing.T, selection ExecutableSelection) {
	t.Helper()
	if !filepath.IsAbs(selection.Path) || selection.Path != filepath.Clean(selection.Path) {
		t.Fatalf("path=%q", selection.Path)
	}
	info, err := os.Stat(selection.Path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		t.Fatalf("info=%v err=%v", info, err)
	}
}
func assertAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("marker exists or stat failed: %v", err)
	}
}
