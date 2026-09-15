package freecad

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/adapter"
)

func validFreeCADRuntimeManifestRequest(t *testing.T) adapter.CADRuntimeOrchestrationRequest {
	t.Helper()
	req := validFreeCADRuntimeAttemptRequest(t)
	req.Manifest.SchemaVersion = planner.ExportManifestSchemaVersion
	req.Manifest.SourceDocument = "stale/caller-value.FCStd"
	req.Manifest.ParameterAssignments = []planner.ExportManifestParameterAssignment{{Name: "width", Target: "Body.Width", Value: 10, Type: "number", Unit: "mm"}}
	req.Manifest.Outputs = []planner.ExportManifestOutput{{Type: "step", Filename: "outputs/Widget.step", Object: "Body"}}
	return req
}

func requireRuntimeManifestError(t *testing.T, err error, stage string) *FreeCADRuntimeManifestError {
	t.Helper()
	var manifestErr *FreeCADRuntimeManifestError
	if !errors.As(err, &manifestErr) {
		t.Fatalf("error %T %v is not FreeCADRuntimeManifestError", err, err)
	}
	if manifestErr.Stage != stage {
		t.Fatalf("stage=%q want %q: %v", manifestErr.Stage, stage, err)
	}
	return manifestErr
}

func manifestTempFiles(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".export-manifest-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

func TestComposeFreeCADRuntimeManifest_Valid(t *testing.T) {
	req := validFreeCADRuntimeManifestRequest(t)
	wantAttempt, err := ComputeFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ComposeFreeCADRuntimeManifest(req)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Attempt, wantAttempt) || got.Manifest.SchemaVersion != planner.ExportManifestSchemaVersion || got.Manifest.SourceDocument != wantAttempt.Layout.SourceDocument {
		t.Fatalf("unexpected materialization: %#v", got)
	}
	if len(got.JSON) == 0 || len(got.Manifest.ParameterAssignments) != 1 || len(got.Manifest.Outputs) != 1 {
		t.Fatalf("incomplete materialization: %#v", got)
	}
	assertPathAbsent(t, req.ProductDir)
}

func TestComposeFreeCADRuntimeManifest_UsesAttemptSourceDocument(t *testing.T) {
	req := validFreeCADRuntimeManifestRequest(t)
	host := req.Manifest.Inputs.SourceModel
	got, err := ComposeFreeCADRuntimeManifest(req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Manifest.SourceDocument != "source/Widget.FCStd" || !bytes.Contains(got.JSON, []byte(`"sourceDocument": "source/Widget.FCStd"`)) {
		t.Fatalf("authoritative source missing: %s", got.JSON)
	}
	if bytes.Contains(got.JSON, []byte(host)) || bytes.Contains(got.JSON, []byte(req.Manifest.SourceDocument)) {
		t.Fatalf("caller/host source leaked: %s", got.JSON)
	}
}

func TestComposeFreeCADRuntimeManifest_IgnoresCallerSourceDocument(t *testing.T) {
	var baseline FreeCADRuntimeManifestMaterialization
	for i, value := range []string{"", "stale.FCStd", "../escape.FCStd", "/host/absolute.FCStd"} {
		req := validFreeCADRuntimeManifestRequest(t)
		req.Manifest.SourceDocument = value
		got, err := ComposeFreeCADRuntimeManifest(req)
		if err != nil {
			t.Fatalf("value %q: %v", value, err)
		}
		if i == 0 {
			baseline = got
			continue
		}
		// Temp roots differ between fixtures, but logical source projection does not.
		if got.Manifest.SourceDocument != baseline.Manifest.SourceDocument || !bytes.Equal(got.JSON, baseline.JSON) {
			t.Fatalf("caller source %q affected native output", value)
		}
	}
}

func TestComposeFreeCADRuntimeManifest_ValidatesSchemaVersion(t *testing.T) {
	for _, version := range []string{"", " ", " 1.0", "1.0 ", "2.0", "1"} {
		t.Run(strings.ReplaceAll(version, " ", "space"), func(t *testing.T) {
			req := validFreeCADRuntimeManifestRequest(t)
			req.Manifest.SchemaVersion = version
			err := func() error { _, err := ComposeFreeCADRuntimeManifest(req); return err }()
			manifestErr := requireRuntimeManifestError(t, err, FreeCADRuntimeManifestStageManifestValidation)
			if manifestErr.Field != "Manifest.SchemaVersion" || manifestErr.AttemptID == "" || manifestErr.Path == "" {
				t.Fatalf("missing error context: %#v", manifestErr)
			}
			assertPathAbsent(t, req.ProductDir)
		})
	}
}

func TestComposeFreeCADRuntimeManifest_EmitsExactNativeShape(t *testing.T) {
	got, err := ComposeFreeCADRuntimeManifest(validFreeCADRuntimeManifestRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(got.JSON, &top); err != nil {
		t.Fatal(err)
	}
	assertKeys := func(label string, got map[string]json.RawMessage, want ...string) {
		t.Helper()
		keys := make([]string, 0, len(got))
		for key := range got {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		sort.Strings(want)
		if !reflect.DeepEqual(keys, want) {
			t.Fatalf("%s keys=%v want %v", label, keys, want)
		}
	}
	assertKeys("top", top, "schemaVersion", "sourceDocument", "parameterAssignments", "outputs")
	var assignments []map[string]json.RawMessage
	var outputs []map[string]json.RawMessage
	if err := json.Unmarshal(top["parameterAssignments"], &assignments); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(top["outputs"], &outputs); err != nil {
		t.Fatal(err)
	}
	assertKeys("assignment", assignments[0], "target", "value", "valueKind")
	assertKeys("output", outputs[0], "id", "format", "path")
	for _, forbidden := range []string{"planHash", "adapter", "product", "inputs", "sourceModel", "values", "name", "unit", "filename", "object", "verification", "productKey", "AttemptID", "WorkingCopyDir", "Executable"} {
		if _, exists := top[forbidden]; exists {
			t.Fatalf("Engine-only top-level key %q emitted", forbidden)
		}
	}
}

func TestComposeFreeCADRuntimeManifest_PreservesNativeProjectionSemantics(t *testing.T) {
	req := validFreeCADRuntimeManifestRequest(t)
	req.Manifest.ParameterAssignments = []planner.ExportManifestParameterAssignment{
		{Name: "width", Target: "Body.Width", Value: 10, Type: "number", Unit: "mm"},
		{Name: "height", Target: "Body.Height", Value: 20, Type: "number", Unit: "mm"},
	}
	req.Manifest.Outputs = []planner.ExportManifestOutput{
		{Type: "step", Filename: "outputs/Widget.step", Object: "ExactBody"},
		{Type: "pdf", Filename: "outputs/drawing sheet.pdf"},
	}
	got, err := ComposeFreeCADRuntimeManifest(req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Manifest.ParameterAssignments[0].Target != "Body.Width" || got.Manifest.ParameterAssignments[1].Target != "Body.Height" {
		t.Fatalf("assignment order changed: %#v", got.Manifest.ParameterAssignments)
	}
	wantOutputs := []FreeCADRuntimeManifestOutput{{ID: "ExactBody", Format: "step", Path: "outputs/Widget.step"}, {ID: "drawing_sheet-pdf", Format: "pdf", Path: "outputs/drawing sheet.pdf"}}
	if !reflect.DeepEqual(got.Manifest.Outputs, wantOutputs) {
		t.Fatalf("outputs=%#v want %#v", got.Manifest.Outputs, wantOutputs)
	}
}

func TestComposeFreeCADRuntimeManifest_IsDeterministic(t *testing.T) {
	req := validFreeCADRuntimeManifestRequest(t)
	first, err := ComposeFreeCADRuntimeManifest(req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ComposeFreeCADRuntimeManifest(cloneFreeCADRuntimeAttemptRequest(req))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Attempt, second.Attempt) || !reflect.DeepEqual(first.Manifest, second.Manifest) || !bytes.Equal(first.JSON, second.JSON) {
		t.Fatal("equivalent compositions differ")
	}
	if !json.Valid(first.JSON) || !bytes.HasSuffix(first.JSON, []byte("\n")) || bytes.HasSuffix(first.JSON, []byte("\n\n")) {
		t.Fatalf("invalid deterministic JSON framing: %q", first.JSON)
	}
}

func TestComposeFreeCADRuntimeManifest_DoesNotMutateRequest(t *testing.T) {
	req := validFreeCADRuntimeManifestRequest(t)
	enrichFreeCADRuntimeAttemptRequest(&req)
	req.Manifest.Outputs[0].Filename = "outputs/Widget.step"
	req.Manifest.SourceDocument = "stale/caller.FCStd"
	want := cloneFreeCADRuntimeAttemptRequest(req)
	for i := 0; i < 2; i++ {
		if _, err := ComposeFreeCADRuntimeManifest(req); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(req, want) {
		t.Fatalf("request mutated\n got: %#v\nwant: %#v", req, want)
	}
}

func TestComposeFreeCADRuntimeManifest_HasNoFilesystemOrProcessSideEffects(t *testing.T) {
	req := validFreeCADRuntimeManifestRequest(t)
	marker := filepath.Join(t.TempDir(), "executed")
	req.Executable.Command, req.Executable.Path = marker, marker
	attempt, err := ComputeFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ComposeFreeCADRuntimeManifest(req); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{req.ProductDir, attempt.Layout.WorkingRoot, attempt.Layout.SourceDocumentPath, attempt.Layout.OutputDir, attempt.Layout.ManifestPath, attempt.Layout.ResultPath, marker} {
		assertPathAbsent(t, path)
	}
}

func TestComposeFreeCADRuntimeManifest_WrapsAttemptComputationFailure(t *testing.T) {
	req := validFreeCADRuntimeManifestRequest(t)
	req.JobID = ""
	_, err := ComposeFreeCADRuntimeManifest(req)
	requireRuntimeManifestError(t, err, FreeCADRuntimeManifestStageAttemptComputation)
	var attemptErr *FreeCADRuntimeAttemptError
	if !errors.As(err, &attemptErr) {
		t.Fatalf("attempt cause lost: %v", err)
	}
	var requestErr *adapter.CADRuntimeOrchestrationRequestError
	if !errors.As(err, &requestErr) {
		t.Fatalf("request cause lost: %v", err)
	}
}

func TestComposeFreeCADRuntimeManifest_WrapsProjectionFailure(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*adapter.CADRuntimeOrchestrationRequest)
	}{
		{"missing target", func(r *adapter.CADRuntimeOrchestrationRequest) { r.Manifest.ParameterAssignments[0].Target = "" }},
		{"malformed target", func(r *adapter.CADRuntimeOrchestrationRequest) {
			r.Manifest.ParameterAssignments[0].Target = "Body.Bad.Target"
		}},
		{"unsupported unit", func(r *adapter.CADRuntimeOrchestrationRequest) { r.Manifest.ParameterAssignments[0].Unit = "cm" }},
		{"nonfinite", func(r *adapter.CADRuntimeOrchestrationRequest) { r.Manifest.ParameterAssignments[0].Value = math.NaN() }},
		{"root output", func(r *adapter.CADRuntimeOrchestrationRequest) { r.Manifest.Outputs[0].Filename = "Widget.step" }},
		{"traversal", func(r *adapter.CADRuntimeOrchestrationRequest) {
			r.Manifest.Outputs[0].Filename = "outputs/../Widget.step"
		}},
		{"missing selector", func(r *adapter.CADRuntimeOrchestrationRequest) { r.Manifest.Outputs[0].Object = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validFreeCADRuntimeManifestRequest(t)
			tt.mutate(&req)
			_, err := ComposeFreeCADRuntimeManifest(req)
			manifestErr := requireRuntimeManifestError(t, err, FreeCADRuntimeManifestStageManifestProjection)
			var projectionErr *FreeCADRuntimeManifestProjectionError
			if !errors.As(err, &projectionErr) {
				t.Fatalf("projection cause lost: %v", err)
			}
			if manifestErr.Field != projectionErr.Field {
				t.Fatalf("field=%q projection=%q", manifestErr.Field, projectionErr.Field)
			}
			assertPathAbsent(t, req.ProductDir)
		})
	}
}

// TestComposeFreeCADRuntimeManifest_ValidWithEmptyOutputs is the positive
// counterpart split out of former TestComposeFreeCADRuntimeManifest_WrapsProjectionFailure/"empty
// outputs": a nil Manifest.Outputs composes successfully end to end
// (attempt computation, schema validation, projection, and serialization),
// producing "outputs": [] and preserving parameterAssignments/
// sourceDocument, with no filesystem or process side effects.
func TestComposeFreeCADRuntimeManifest_ValidWithEmptyOutputs(t *testing.T) {
	req := validFreeCADRuntimeManifestRequest(t)
	req.Manifest.Outputs = nil

	got, err := ComposeFreeCADRuntimeManifest(req)
	if err != nil {
		t.Fatalf("unexpected error composing native-only manifest: %v", err)
	}
	if got.Manifest.Outputs == nil {
		t.Fatal("expected a non-nil empty Outputs slice")
	}
	if len(got.Manifest.Outputs) != 0 {
		t.Fatalf("expected zero outputs, got %+v", got.Manifest.Outputs)
	}
	if !bytes.Contains(got.JSON, []byte(`"outputs": []`)) {
		t.Fatalf("expected serialized outputs as empty array, got: %s", got.JSON)
	}
	if len(got.Manifest.ParameterAssignments) != 1 {
		t.Fatalf("expected parameterAssignments to survive, got %+v", got.Manifest.ParameterAssignments)
	}
	if got.Manifest.SourceDocument == "" {
		t.Fatal("expected sourceDocument to remain populated")
	}
	assertPathAbsent(t, req.ProductDir)
}

// TestComposeFreeCADRuntimeManifest_EmptyOutputsIsByteDeterministic extends
// TestComposeFreeCADRuntimeManifest_IsDeterministic to the native-only
// (empty Outputs) shape specifically: repeated composition from equivalent
// logical input produces byte-identical manifest JSON, with "outputs": []
// represented identically each time and parameter assignment ordering
// remaining canonical.
func TestComposeFreeCADRuntimeManifest_EmptyOutputsIsByteDeterministic(t *testing.T) {
	req := validFreeCADRuntimeManifestRequest(t)
	req.Manifest.Outputs = nil

	first, err := ComposeFreeCADRuntimeManifest(req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ComposeFreeCADRuntimeManifest(cloneFreeCADRuntimeAttemptRequest(req))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.JSON, second.JSON) {
		t.Fatalf("equivalent native-only compositions produced different bytes:\n%s\nvs\n%s", first.JSON, second.JSON)
	}
	if !reflect.DeepEqual(first.Manifest, second.Manifest) {
		t.Fatalf("equivalent native-only manifests differ: %#v vs %#v", first.Manifest, second.Manifest)
	}
}

func TestFreeCADRuntimeManifestError_SerializationStageContract(t *testing.T) {
	cause := errors.New("serialize")
	err := &FreeCADRuntimeManifestError{Stage: FreeCADRuntimeManifestStageManifestSerialization, Err: cause}
	if err.Stage != "manifest_serialization" || !errors.Is(err, cause) || !strings.Contains(err.Error(), "manifest_serialization") {
		t.Fatalf("serialization contract broken: %v", err)
	}
}

func TestWriteFreeCADRuntimeManifest_PreparesAndWritesManifest(t *testing.T) {
	req := validFreeCADRuntimeManifestRequest(t)
	source := []byte("freecad-source")
	writeAttemptSource(t, req, source)
	pure, err := ComposeFreeCADRuntimeManifest(req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := WriteFreeCADRuntimeManifest(req)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Attempt, pure.Attempt) || !reflect.DeepEqual(got.Manifest, pure.Manifest) || !bytes.Equal(got.JSON, pure.JSON) {
		t.Fatal("write changed composition")
	}
	preparedSource, err := os.ReadFile(got.Attempt.Layout.SourceDocumentPath)
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := os.ReadFile(got.Attempt.Layout.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(preparedSource, source) || !bytes.Equal(manifestBytes, got.JSON) {
		t.Fatal("prepared/written bytes differ")
	}
	for _, dir := range []string{got.Attempt.Layout.WorkingCopyDir, got.Attempt.Layout.SourceDir, got.Attempt.Layout.OutputDir} {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatalf("directory %q: %v", dir, err)
		}
	}
	assertLaterRuntimeFilesAbsent(t, got.Attempt)
}

func TestWriteFreeCADRuntimeManifest_CompositionFailureHasNoPreparationSideEffects(t *testing.T) {
	req := validFreeCADRuntimeManifestRequest(t)
	// A missing parameter assignment target (not an empty Outputs list,
	// which is now a valid native-only projection -- see
	// TestComposeFreeCADRuntimeManifest_ValidWithEmptyOutputs) still fails
	// deterministically at the manifest_projection stage.
	req.Manifest.ParameterAssignments[0].Target = ""
	attempt, err := ComputeFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	_, err = WriteFreeCADRuntimeManifest(req)
	requireRuntimeManifestError(t, err, FreeCADRuntimeManifestStageManifestProjection)
	for _, path := range []string{req.ProductDir, attempt.Layout.SourceDir, attempt.Layout.OutputDir, attempt.Layout.ManifestPath} {
		assertPathAbsent(t, path)
	}
}

func TestWriteFreeCADRuntimeManifest_PreparationFailureWritesNoManifest(t *testing.T) {
	req := validFreeCADRuntimeManifestRequest(t)
	pure, err := ComposeFreeCADRuntimeManifest(req)
	if err != nil {
		t.Fatal(err)
	}
	_, err = WriteFreeCADRuntimeManifest(req)
	requireRuntimeManifestError(t, err, FreeCADRuntimeManifestStageAttemptPreparation)
	var attemptErr *FreeCADRuntimeAttemptError
	if !errors.As(err, &attemptErr) {
		t.Fatalf("attempt cause lost: %v", err)
	}
	assertPathAbsent(t, pure.Attempt.Layout.ManifestPath)
	assertLaterRuntimeFilesAbsent(t, pure.Attempt)
}

func TestWriteFreeCADRuntimeManifest_RepeatedWriteIsDeterministic(t *testing.T) {
	req := validFreeCADRuntimeManifestRequest(t)
	writeAttemptSource(t, req, []byte("source"))
	first, err := WriteFreeCADRuntimeManifest(req)
	if err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(first.Attempt.Layout.OutputDir, "keep.txt")
	if err := os.WriteFile(unrelated, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := WriteFreeCADRuntimeManifest(req)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.ReadFile(second.Attempt.Layout.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if first.Attempt.Identity.ID != second.Attempt.Identity.ID || !bytes.Equal(first.JSON, second.JSON) || !bytes.Equal(file, second.JSON) {
		t.Fatal("repeated write differs")
	}
	if data, err := os.ReadFile(unrelated); err != nil || string(data) != "keep" {
		t.Fatalf("unrelated output damaged: %q %v", data, err)
	}
	if files := manifestTempFiles(t, second.Attempt.Layout.WorkingCopyDir); len(files) != 0 {
		t.Fatalf("temp files remain: %v", files)
	}
}

func TestWriteFreeCADRuntimeManifest_ReplacesExistingRegularManifest(t *testing.T) {
	req := validFreeCADRuntimeManifestRequest(t)
	writeAttemptSource(t, req, []byte("source"))
	attempt, err := PrepareFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(attempt.Layout.ManifestPath, []byte("sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := WriteFreeCADRuntimeManifest(req)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(attempt.Layout.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(attempt.Layout.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, got.JSON) || !info.Mode().IsRegular() || bytes.Contains(data, []byte("sentinel")) {
		t.Fatal("manifest was not atomically replaced")
	}
	if files := manifestTempFiles(t, attempt.Layout.WorkingCopyDir); len(files) != 0 {
		t.Fatalf("temp files remain: %v", files)
	}
}

func TestWriteFreeCADRuntimeManifest_ReturnedJSONDoesNotAliasWrittenFile(t *testing.T) {
	req := validFreeCADRuntimeManifestRequest(t)
	writeAttemptSource(t, req, []byte("source"))
	got, err := WriteFreeCADRuntimeManifest(req)
	if err != nil {
		t.Fatal(err)
	}
	want := append([]byte(nil), got.JSON...)
	for i := range got.JSON {
		got.JSON[i] ^= 0xff
	}
	data, err := os.ReadFile(got.Attempt.Layout.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, want) {
		t.Fatal("returned JSON aliases file state")
	}
}

func TestWriteFreeCADRuntimeManifest_DoesNotMutateRequest(t *testing.T) {
	req := validFreeCADRuntimeManifestRequest(t)
	enrichFreeCADRuntimeAttemptRequest(&req)
	req.Manifest.Outputs[0].Filename = "outputs/Widget.step"
	req.Manifest.SourceDocument = "stale.FCStd"
	writeAttemptSource(t, req, []byte("source"))
	want := cloneFreeCADRuntimeAttemptRequest(req)
	if _, err := WriteFreeCADRuntimeManifest(req); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(req, want) {
		t.Fatal("write mutated request")
	}
}

func TestWriteFreeCADRuntimeManifest_PreservesExistingManifestOnCompositionFailure(t *testing.T) {
	req := validFreeCADRuntimeManifestRequest(t)
	writeAttemptSource(t, req, []byte("source"))
	first, err := WriteFreeCADRuntimeManifest(req)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(first.Attempt.Layout.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	req.Manifest.Outputs[0].Filename = "Widget.step"
	_, err = WriteFreeCADRuntimeManifest(req)
	requireRuntimeManifestError(t, err, FreeCADRuntimeManifestStageManifestProjection)
	got, err := os.ReadFile(first.Attempt.Layout.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("existing manifest changed")
	}
	if files := manifestTempFiles(t, first.Attempt.Layout.WorkingCopyDir); len(files) != 0 {
		t.Fatalf("temp files remain: %v", files)
	}
}

func TestWriteFreeCADRuntimeManifest_CleansTemporaryFileAfterFinalizationFailure(t *testing.T) {
	req := validFreeCADRuntimeManifestRequest(t)
	writeAttemptSource(t, req, []byte("source"))
	attempt, err := PrepareFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(attempt.Layout.ManifestPath, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = WriteFreeCADRuntimeManifest(req)
	manifestErr := requireRuntimeManifestError(t, err, FreeCADRuntimeManifestStageManifestWrite)
	var linkErr *os.LinkError
	if !errors.As(err, &linkErr) {
		t.Fatalf("filesystem cause lost: %v", err)
	}
	if manifestErr.AttemptID != attempt.Identity.ID || manifestErr.Path != attempt.Layout.ManifestPath {
		t.Fatalf("write context lost: %#v", manifestErr)
	}
	info, statErr := os.Stat(attempt.Layout.ManifestPath)
	if statErr != nil || !info.IsDir() {
		t.Fatalf("destination conflict damaged: %v", statErr)
	}
	if files := manifestTempFiles(t, attempt.Layout.WorkingCopyDir); len(files) != 0 {
		t.Fatalf("temp files remain: %v", files)
	}
	assertLaterRuntimeFilesAbsent(t, attempt)
}

func TestWriteFreeCADRuntimeManifest_WriteFailureDoesNotCreatePartialRegularFile(t *testing.T) {
	req := validFreeCADRuntimeManifestRequest(t)
	writeAttemptSource(t, req, []byte("source"))
	attempt, err := PrepareFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(attempt.Layout.ManifestPath, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = WriteFreeCADRuntimeManifest(req)
	requireRuntimeManifestError(t, err, FreeCADRuntimeManifestStageManifestWrite)
	info, err := os.Stat(attempt.Layout.ManifestPath)
	if err != nil || info.Mode().IsRegular() {
		t.Fatalf("partial regular file created: %#v %v", info, err)
	}
}

func TestFreeCADRuntimeManifestError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("sentinel")
	err := &FreeCADRuntimeManifestError{Stage: FreeCADRuntimeManifestStageManifestWrite, Field: "Manifest", AttemptID: "attempt-1", Path: "/work/manifest.json", Err: cause}
	for _, want := range []string{"manifest_write", "Manifest", "attempt-1", "/work/manifest.json"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Error missing %q: %v", want, err)
		}
	}
	if !errors.Is(err, cause) {
		t.Fatal("sentinel not unwrapped")
	}
	var got *FreeCADRuntimeManifestError
	if !errors.As(err, &got) || got != err {
		t.Fatal("typed error not discoverable")
	}
}

func TestFreeCADRuntimeManifest_ErrorStages(t *testing.T) {
	base := func(t *testing.T) adapter.CADRuntimeOrchestrationRequest {
		r := validFreeCADRuntimeManifestRequest(t)
		return r
	}
	tests := []struct {
		name, stage string
		run         func(*testing.T, adapter.CADRuntimeOrchestrationRequest) error
	}{
		{"attempt", FreeCADRuntimeManifestStageAttemptComputation, func(_ *testing.T, r adapter.CADRuntimeOrchestrationRequest) error {
			r.ProductDir = "relative"
			_, err := ComposeFreeCADRuntimeManifest(r)
			return err
		}},
		{"schema", FreeCADRuntimeManifestStageManifestValidation, func(_ *testing.T, r adapter.CADRuntimeOrchestrationRequest) error {
			r.Manifest.SchemaVersion = "2.0"
			_, err := ComposeFreeCADRuntimeManifest(r)
			return err
		}},
		{"projection", FreeCADRuntimeManifestStageManifestProjection, func(_ *testing.T, r adapter.CADRuntimeOrchestrationRequest) error {
			// A nil Outputs slice is a valid native-only projection (see
			// TestComposeFreeCADRuntimeManifest_ValidWithEmptyOutputs); a
			// missing parameter assignment target still fails at this stage.
			r.Manifest.ParameterAssignments[0].Target = ""
			_, err := ComposeFreeCADRuntimeManifest(r)
			return err
		}},
		{"preparation", FreeCADRuntimeManifestStageAttemptPreparation, func(_ *testing.T, r adapter.CADRuntimeOrchestrationRequest) error {
			_, err := WriteFreeCADRuntimeManifest(r)
			return err
		}},
		{"write", FreeCADRuntimeManifestStageManifestWrite, func(t *testing.T, r adapter.CADRuntimeOrchestrationRequest) error {
			writeAttemptSource(t, r, []byte("source"))
			a, err := PrepareFreeCADRuntimeAttempt(r)
			if err != nil {
				return err
			}
			if err := os.Mkdir(a.Layout.ManifestPath, 0o755); err != nil {
				return err
			}
			_, err = WriteFreeCADRuntimeManifest(r)
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { requireRuntimeManifestError(t, tt.run(t, base(t)), tt.stage) })
	}
}

func TestFreeCADRuntimeManifestError_PreservesUnderlyingCauses(t *testing.T) {
	// Attempt/request and projection causes are covered independently here; the
	// finalization conflict supplies a stable filesystem cause without permissions.
	req := validFreeCADRuntimeManifestRequest(t)
	req.JobID = ""
	_, err := ComposeFreeCADRuntimeManifest(req)
	var attemptErr *FreeCADRuntimeAttemptError
	var requestErr *adapter.CADRuntimeOrchestrationRequestError
	if !errors.As(err, &attemptErr) || !errors.As(err, &requestErr) {
		t.Fatalf("Task 7/6 causes lost: %v", err)
	}
	req = validFreeCADRuntimeManifestRequest(t)
	// A nil Outputs slice is a valid native-only projection (see
	// TestComposeFreeCADRuntimeManifest_ValidWithEmptyOutputs); a missing
	// parameter assignment target still fails projection and preserves its cause.
	req.Manifest.ParameterAssignments[0].Target = ""
	_, err = ComposeFreeCADRuntimeManifest(req)
	var projectionErr *FreeCADRuntimeManifestProjectionError
	if !errors.As(err, &projectionErr) {
		t.Fatalf("projection cause lost: %v", err)
	}
	req = validFreeCADRuntimeManifestRequest(t)
	writeAttemptSource(t, req, []byte("source"))
	a, err := PrepareFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(a.Layout.ManifestPath, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = WriteFreeCADRuntimeManifest(req)
	var linkErr *os.LinkError
	if !errors.As(err, &linkErr) {
		t.Fatalf("filesystem cause lost: %v", err)
	}
}

func assertLaterRuntimeFilesAbsent(t *testing.T, attempt FreeCADRuntimeAttempt) {
	t.Helper()
	for _, path := range []string{
		attempt.Layout.ResultPath,
		filepath.Join(attempt.Layout.WorkingCopyDir, "observation_request.json"),
		filepath.Join(attempt.Layout.WorkingCopyDir, "observation-request.json"),
		filepath.Join(attempt.Layout.WorkingCopyDir, "prm.observed.json"),
		filepath.Join(attempt.Layout.OutputDir, "Widget.step"),
		filepath.Join(attempt.Layout.OutputDir, "drawing.pdf"),
	} {
		assertPathAbsent(t, path)
	}
}

func TestWriteFreeCADRuntimeManifest_DoesNotMaterializeTask9OrTask10Files(t *testing.T) {
	req := validFreeCADRuntimeManifestRequest(t)
	req.Manifest.Outputs = append(req.Manifest.Outputs, planner.ExportManifestOutput{Type: "pdf", Filename: "outputs/drawing.pdf"})
	writeAttemptSource(t, req, []byte("source"))
	got, err := WriteFreeCADRuntimeManifest(req)
	if err != nil {
		t.Fatal(err)
	}
	assertLaterRuntimeFilesAbsent(t, got.Attempt)
	if info, err := os.Stat(got.Attempt.Layout.OutputDir); err != nil || !info.IsDir() {
		t.Fatalf("outputs directory absent: %v", err)
	}
}

func TestFreeCADRuntimeManifest_DoesNotExecuteSelectedRuntime(t *testing.T) {
	req := validFreeCADRuntimeManifestRequest(t)
	writeAttemptSource(t, req, []byte("source"))
	marker := filepath.Join(t.TempDir(), "runtime-executed")
	script := filepath.Join(t.TempDir(), "runtime.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	req.Executable.Command, req.Executable.Path = script, script
	if _, err := ComposeFreeCADRuntimeManifest(req); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteFreeCADRuntimeManifest(req); err != nil {
		t.Fatal(err)
	}
	assertPathAbsent(t, marker)
}

func TestFreeCADRuntimeManifest_DoesNotAlterLogicalEngineeringIdentity(t *testing.T) {
	req := validFreeCADRuntimeManifestRequest(t)
	enrichFreeCADRuntimeAttemptRequest(&req)
	req.Manifest.Outputs[0].Filename = "outputs/Widget.step"
	writeAttemptSource(t, req, []byte("source"))
	want := cloneFreeCADRuntimeAttemptRequest(req)
	logicalBefore, err := json.Marshal(struct {
		JobID, ProductKey, StepID string
		Manifest                  planner.WriteExportManifestPayload
	}{req.JobID, req.ProductKey, req.StepID, req.Manifest})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ComposeFreeCADRuntimeManifest(req); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteFreeCADRuntimeManifest(req); err != nil {
		t.Fatal(err)
	}
	logicalAfter, err := json.Marshal(struct {
		JobID, ProductKey, StepID string
		Manifest                  planner.WriteExportManifestPayload
	}{req.JobID, req.ProductKey, req.StepID, req.Manifest})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(logicalBefore, logicalAfter) || !reflect.DeepEqual(req, want) {
		t.Fatal("logical engineering identity changed")
	}
	for _, forbidden := range []string{"WorkingCopyDir", "ManifestPath", "ResultPath", req.Executable.Path} {
		if strings.Contains(string(logicalAfter), forbidden) {
			t.Fatalf("attempt/runtime detail %q entered logical payload", forbidden)
		}
	}
}
