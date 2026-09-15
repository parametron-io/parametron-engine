package cadruntime

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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
	"parametron/internal/engine/adapter/freecad"
	"parametron/internal/engine/runtimecap"
	"parametron/internal/engine/verification"
)

func validFreeCADObservationRequest(t *testing.T) adapter.CADRuntimeOrchestrationRequest {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "host", "Widget.FCStd")
	return adapter.CADRuntimeOrchestrationRequest{
		JobID: "job-widget", ProductKey: "widget", ProductDir: filepath.Join(root, "product"), StepID: "2", Attempt: 1,
		CSV: planner.WriteCSVPayload{ProductKey: "widget", Filename: "widget.csv", Headers: []string{"width"}, Values: []any{10.0}},
		Manifest: planner.WriteExportManifestPayload{
			ProductKey: "widget", ManifestFilename: "runtime_manifest.json", Adapter: runtimecap.FreeCADAdapterID,
			SchemaVersion: planner.ExportManifestSchemaVersion, PlanHash: "plan-hash-widget",
			Product: planner.ExportManifestProduct{ID: "widget"}, Inputs: planner.ExportManifestInputs{SourceModel: source},
			Values:               map[string]any{"width": 10.0},
			ParameterAssignments: []planner.ExportManifestParameterAssignment{{Name: "width", Target: "Body.Width", Value: 10, Type: "number", Unit: "mm"}},
			Outputs:              []planner.ExportManifestOutput{{Type: "step", Filename: "outputs/Widget.step", Object: "Body"}},
			Verification: planner.VerificationManifestIntent{
				ExpectedParameters:        []planner.VerificationExpectedParameter{{ID: "width", Name: "Width", Value: 10, Type: "number", Unit: "mm"}},
				ObservationParameterLinks: []planner.VerificationObservationParameterLink{{ID: "width", Name: "Width", GroupName: "VarSet"}},
			},
		},
		CADRuntime: planner.RunCADRuntimePayload{ProductKey: "widget", Adapter: runtimecap.FreeCADAdapterID, ManifestFilename: "runtime_manifest.json", ResultFilename: "cad_result.json"},
		Executable: runtimecap.ExecutableSelection{Adapter: runtimecap.FreeCADAdapterID, Command: "/missing/marker-runtime", Path: "/missing/marker-runtime", Source: runtimecap.ExecutableSelectionSourceConfigured},
	}
}

func cloneFreeCADObservationRequest(req adapter.CADRuntimeOrchestrationRequest) adapter.CADRuntimeOrchestrationRequest {
	clone := req
	clone.CSV.Headers = append([]string(nil), req.CSV.Headers...)
	clone.CSV.Values = append([]any(nil), req.CSV.Values...)
	clone.Manifest.Values = make(map[string]any, len(req.Manifest.Values))
	for k, v := range req.Manifest.Values {
		clone.Manifest.Values[k] = v
	}
	clone.Manifest.ParameterAssignments = append([]planner.ExportManifestParameterAssignment(nil), req.Manifest.ParameterAssignments...)
	clone.Manifest.Outputs = append([]planner.ExportManifestOutput(nil), req.Manifest.Outputs...)
	for i := range clone.Manifest.Outputs {
		clone.Manifest.Outputs[i].Columns = append([]string(nil), req.Manifest.Outputs[i].Columns...)
	}
	clone.Manifest.Verification.ExpectedParameters = append([]planner.VerificationExpectedParameter(nil), req.Manifest.Verification.ExpectedParameters...)
	clone.Manifest.Verification.ObservationParameterLinks = append([]planner.VerificationObservationParameterLink(nil), req.Manifest.Verification.ObservationParameterLinks...)
	return clone
}

func writeObservationHostSource(t *testing.T, req adapter.CADRuntimeOrchestrationRequest, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(req.Manifest.Inputs.SourceModel), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(req.Manifest.Inputs.SourceModel, data, 0o640); err != nil {
		t.Fatal(err)
	}
}

func prepareObservationSource(t *testing.T, req adapter.CADRuntimeOrchestrationRequest, data []byte) freecad.FreeCADRuntimeAttempt {
	t.Helper()
	writeObservationHostSource(t, req, data)
	attempt, err := freecad.PrepareFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	return attempt
}

func requireObservationError(t *testing.T, err error, stage string) *FreeCADRuntimeObservationRequestError {
	t.Helper()
	var got *FreeCADRuntimeObservationRequestError
	if !errors.As(err, &got) || got.Stage != stage {
		t.Fatalf("error=%T %v typed=%#v want stage %q", err, err, got, stage)
	}
	return got
}

func assertObservationAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s unexpectedly exists: %v", path, err)
	}
}

func observationTempFiles(t *testing.T, dir string) []string {
	t.Helper()
	got, err := filepath.Glob(filepath.Join(dir, ".verification-tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func observationOutputPaths(req adapter.CADRuntimeOrchestrationRequest, attempt freecad.FreeCADRuntimeAttempt) []string {
	paths := make([]string, 0, len(req.Manifest.Outputs))
	for _, output := range req.Manifest.Outputs {
		paths = append(paths, filepath.Join(attempt.Layout.WorkingCopyDir, filepath.FromSlash(output.Filename)))
	}
	return paths
}

func TestComposeFreeCADRuntimeObservationRequest_UsesCanonicalPath(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	attempt := prepareObservationSource(t, req, []byte("prepared"))
	got, err := ComposeFreeCADRuntimeObservationRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(attempt.Layout.WorkingCopyDir, FreeCADRuntimeObservationRequestFilename)
	if got.Path != want || !filepath.IsAbs(got.Path) || filepath.Clean(got.Path) != got.Path {
		t.Fatalf("path=%q want=%q", got.Path, want)
	}
	rel, _ := filepath.Rel(attempt.Layout.WorkingCopyDir, got.Path)
	if rel == "." || strings.HasPrefix(rel, "..") || strings.HasPrefix(rel, "outputs"+string(filepath.Separator)) {
		t.Fatalf("path containment=%q", rel)
	}
	for _, collision := range append([]string{attempt.Layout.SourceDocumentPath, attempt.Layout.ManifestPath, attempt.Layout.ResultPath, attempt.Layout.OutputDir}, observationOutputPaths(req, attempt)...) {
		if got.Path == collision {
			t.Fatalf("path collision %q", collision)
		}
	}
}

func TestComposeFreeCADRuntimeObservationRequest_RejectsManifestFilenameCollision(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	req.Manifest.ManifestFilename = FreeCADRuntimeObservationRequestFilename
	req.CADRuntime.ManifestFilename = FreeCADRuntimeObservationRequestFilename
	_, err := ComposeFreeCADRuntimeObservationRequest(req)
	got := requireObservationError(t, err, FreeCADRuntimeObservationRequestStageRequestPath)
	if got.Field != "CADRuntime.ManifestFilename" {
		t.Fatalf("field=%q", got.Field)
	}
	assertObservationAbsent(t, req.ProductDir)
}

func TestComposeFreeCADRuntimeObservationRequest_RejectsResultFilenameCollision(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	req.CADRuntime.ResultFilename = FreeCADRuntimeObservationRequestFilename
	_, err := ComposeFreeCADRuntimeObservationRequest(req)
	got := requireObservationError(t, err, FreeCADRuntimeObservationRequestStageRequestPath)
	if got.Field != "CADRuntime.ResultFilename" {
		t.Fatalf("field=%q", got.Field)
	}
	assertObservationAbsent(t, req.ProductDir)
}

func TestComposeFreeCADRuntimeObservationRequest_NoParameters(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	req.Manifest.ParameterAssignments = nil
	req.Manifest.Verification = planner.VerificationManifestIntent{}
	prepareObservationSource(t, req, []byte("prepared"))
	got, err := ComposeFreeCADRuntimeObservationRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	c := got.Contract
	if c.Observe.Components || c.Observe.Parameters || !c.Observe.Metadata || !c.Observe.References ||
		c.Checks.Components.Enabled || c.Checks.Parameters.Enabled || !c.Checks.Metadata.Enabled || !c.Checks.References.Enabled ||
		c.ObservationContext.Parameters == nil || len(c.ObservationContext.Parameters) != 0 {
		t.Fatalf("contract=%#v", c)
	}
}

func TestComposeFreeCADRuntimeObservationRequest_Parameterized(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	req.Manifest.ParameterAssignments = []planner.ExportManifestParameterAssignment{
		{Name: "z", Target: "Body.NotIdentity", Value: 2, Type: "number", Unit: "mm"},
		{Name: "a", Target: "Other.AlsoNotIdentity", Value: 1, Type: "number", Unit: "mm"},
	}
	req.Manifest.Verification.ExpectedParameters = []planner.VerificationExpectedParameter{
		{ID: "z-id", Name: "Z label", Value: 2, Type: "number", Unit: "mm"},
		{ID: "a-id", Name: "A label", Value: 1, Type: "number", Unit: "mm"},
	}
	req.Manifest.Verification.ObservationParameterLinks = []planner.VerificationObservationParameterLink{
		{ID: "z-id", Name: "ZProp", GroupName: "ZGroup"},
		{ID: "a-id", Name: "AProp", GroupName: "AGroup"},
	}
	prepareObservationSource(t, req, []byte("prepared"))
	got, err := ComposeFreeCADRuntimeObservationRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Contract.Observe.Parameters || !got.Contract.Checks.Parameters.Enabled ||
		got.Contract.Expected.Parameters[0].ID != "a-id" || got.Contract.ObservationContext.Parameters[0].ID != "a-id" ||
		got.Contract.ObservationContext.Parameters[0].Name != "AProp" || got.Contract.ObservationContext.Parameters[0].GroupName != "AGroup" {
		t.Fatalf("parameter contract=%#v", got.Contract)
	}
}

func TestComposeFreeCADRuntimeObservationRequest_ValidatesExpectedParameterIDs(t *testing.T) {
	tests := []struct {
		name string
		edit func(*adapter.CADRuntimeOrchestrationRequest)
	}{
		{"empty", func(r *adapter.CADRuntimeOrchestrationRequest) { r.Manifest.Verification.ExpectedParameters[0].ID = "" }},
		{"blank", func(r *adapter.CADRuntimeOrchestrationRequest) {
			r.Manifest.Verification.ExpectedParameters[0].ID = " "
		}},
		{"padded", func(r *adapter.CADRuntimeOrchestrationRequest) {
			r.Manifest.Verification.ExpectedParameters[0].ID = " width"
		}},
		{"duplicate", func(r *adapter.CADRuntimeOrchestrationRequest) {
			r.Manifest.Verification.ExpectedParameters = append(r.Manifest.Verification.ExpectedParameters, r.Manifest.Verification.ExpectedParameters[0])
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validFreeCADObservationRequest(t)
			tt.edit(&req)
			_, err := ComposeFreeCADRuntimeObservationRequest(req)
			got := requireObservationError(t, err, FreeCADRuntimeObservationRequestStageIntentValidation)
			if !strings.Contains(got.Field, "ExpectedParameters[") || !strings.HasSuffix(got.Field, ".ID") {
				t.Fatalf("field=%q", got.Field)
			}
			assertObservationAbsent(t, req.ProductDir)
		})
	}
}

func TestComposeFreeCADRuntimeObservationRequest_ValidatesExpectedParameterValues(t *testing.T) {
	tests := []struct {
		name, field string
		edit        func(*planner.VerificationExpectedParameter)
	}{
		{"type", "Type", func(p *planner.VerificationExpectedParameter) { p.Type = "integer" }},
		{"unit", "Unit", func(p *planner.VerificationExpectedParameter) { p.Unit = "cm" }},
		{"nan", "Value", func(p *planner.VerificationExpectedParameter) { p.Value = math.NaN() }},
		{"positive infinity", "Value", func(p *planner.VerificationExpectedParameter) { p.Value = math.Inf(1) }},
		{"negative infinity", "Value", func(p *planner.VerificationExpectedParameter) { p.Value = math.Inf(-1) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validFreeCADObservationRequest(t)
			tt.edit(&req.Manifest.Verification.ExpectedParameters[0])
			_, err := ComposeFreeCADRuntimeObservationRequest(req)
			got := requireObservationError(t, err, FreeCADRuntimeObservationRequestStageIntentValidation)
			if got.Field != "Manifest.Verification.ExpectedParameters[0]."+tt.field {
				t.Fatalf("field=%q", got.Field)
			}
		})
	}
}

func TestComposeFreeCADRuntimeObservationRequest_RequiresCompleteLinks(t *testing.T) {
	for _, links := range [][]planner.VerificationObservationParameterLink{nil, {}} {
		req := validFreeCADObservationRequest(t)
		req.Manifest.Verification.ObservationParameterLinks = links
		_, err := ComposeFreeCADRuntimeObservationRequest(req)
		requireObservationError(t, err, FreeCADRuntimeObservationRequestStageIntentValidation)
	}
	req := validFreeCADObservationRequest(t)
	req.Manifest.Verification.ExpectedParameters = append(req.Manifest.Verification.ExpectedParameters,
		planner.VerificationExpectedParameter{ID: "height", Name: "Height", Value: 5, Type: "number", Unit: "mm"})
	_, err := ComposeFreeCADRuntimeObservationRequest(req)
	requireObservationError(t, err, FreeCADRuntimeObservationRequestStageIntentValidation)
}

func TestComposeFreeCADRuntimeObservationRequest_RejectsExtraOrDuplicateLinks(t *testing.T) {
	for name, link := range map[string]planner.VerificationObservationParameterLink{
		"extra":     {ID: "height", Name: "Height", GroupName: "Vars"},
		"duplicate": {ID: "width", Name: "Again", GroupName: "Vars"},
	} {
		t.Run(name, func(t *testing.T) {
			req := validFreeCADObservationRequest(t)
			req.Manifest.Verification.ObservationParameterLinks = append(req.Manifest.Verification.ObservationParameterLinks, link)
			_, err := ComposeFreeCADRuntimeObservationRequest(req)
			requireObservationError(t, err, FreeCADRuntimeObservationRequestStageIntentValidation)
		})
	}
}

func TestComposeFreeCADRuntimeObservationRequest_ValidatesLinkFields(t *testing.T) {
	for _, field := range []string{"ID", "Name", "GroupName"} {
		for _, value := range []string{"", " ", " padded"} {
			t.Run(field+"_"+value, func(t *testing.T) {
				req := validFreeCADObservationRequest(t)
				link := &req.Manifest.Verification.ObservationParameterLinks[0]
				switch field {
				case "ID":
					link.ID = value
				case "Name":
					link.Name = value
				case "GroupName":
					link.GroupName = value
				}
				_, err := ComposeFreeCADRuntimeObservationRequest(req)
				got := requireObservationError(t, err, FreeCADRuntimeObservationRequestStageIntentValidation)
				if got.Field != "Manifest.Verification.ObservationParameterLinks[0]."+field {
					t.Fatalf("field=%q", got.Field)
				}
			})
		}
	}
}

func TestComposeFreeCADRuntimeObservationRequest_RejectsLinksWithoutParameters(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	req.Manifest.ParameterAssignments = nil
	req.Manifest.Verification.ExpectedParameters = nil
	_, err := ComposeFreeCADRuntimeObservationRequest(req)
	requireObservationError(t, err, FreeCADRuntimeObservationRequestStageIntentValidation)
	assertObservationAbsent(t, req.ProductDir)
}

func TestComposeFreeCADRuntimeObservationRequest_DoesNotInferIdentity(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	req.Manifest.Verification.ObservationParameterLinks = nil
	req.Manifest.ParameterAssignments[0].Name = "width"
	req.Manifest.ParameterAssignments[0].Target = "Body.Width"
	_, err := ComposeFreeCADRuntimeObservationRequest(req)
	requireObservationError(t, err, FreeCADRuntimeObservationRequestStageIntentValidation)
}

func TestComposeFreeCADRuntimeObservationRequest_UsesPreparedSource(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	hostBytes := []byte("host source")
	attempt := prepareObservationSource(t, req, hostBytes)
	writeObservationHostSource(t, req, []byte("changed host"))
	if err := os.WriteFile(filepath.Join(attempt.Layout.WorkingCopyDir, "unrelated"), []byte("noise"), 0o640); err != nil {
		t.Fatal(err)
	}
	got, err := ComposeFreeCADRuntimeObservationRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(hostBytes)
	if got.Contract.Expected.Metadata[0].Value != hex.EncodeToString(sum[:]) ||
		got.Contract.Expected.References[0].Name != attempt.Layout.WorkingCopyDir ||
		got.Contract.Expected.References[0].Name == attempt.Layout.SourceDocumentPath ||
		got.Contract.Expected.References[0].Name == req.Manifest.Inputs.SourceModel {
		t.Fatalf("prepared source not authoritative: %#v", got.Contract)
	}
}

func TestComposeFreeCADRuntimeObservationRequest_SeparatesWorkingCopyRootFromPreparedSource(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	prepared := []byte("prepared source identity")
	attempt := prepareObservationSource(t, req, prepared)

	got, err := ComposeFreeCADRuntimeObservationRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Contract.Expected.References[0] != (verification.ExpectedReference{
		Kind: "working_copy_path",
		Name: attempt.Layout.WorkingCopyDir,
	}) {
		t.Fatalf("working-copy reference=%#v", got.Contract.Expected.References)
	}
	if got.Contract.Expected.References[0].Name == attempt.Layout.SourceDocumentPath {
		t.Fatal("working-copy reference aliases prepared source path")
	}
	sum := sha256.Sum256(prepared)
	if got.Contract.Expected.Metadata[0].Key != "working_copy_sha256" ||
		got.Contract.Expected.Metadata[0].Value != hex.EncodeToString(sum[:]) {
		t.Fatalf("prepared-source digest=%#v", got.Contract.Expected.Metadata)
	}
	if got.Manifest.Manifest.SourceDocument != filepath.ToSlash(attempt.Layout.SourceDocument) {
		t.Fatalf("source-document intent=%q want=%q", got.Manifest.Manifest.SourceDocument, attempt.Layout.SourceDocument)
	}
	if !got.Contract.Observe.Metadata || !got.Contract.Observe.References ||
		!got.Contract.Checks.Metadata.Enabled || !got.Contract.Checks.References.Enabled {
		t.Fatalf("verification categories changed: %#v", got.Contract)
	}
}

func TestComposeFreeCADRuntimeObservationRequest_RequiresPreparedSource(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	manifest, err := freecad.ComposeFreeCADRuntimeManifest(req)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ComposeFreeCADRuntimeObservationRequest(req)
	requireObservationError(t, err, FreeCADRuntimeObservationRequestStageContractDerivation)
	var fileErr *verification.FileError
	if !errors.As(err, &fileErr) || !errors.Is(err, verification.ErrIO) {
		t.Fatalf("cause=%T %v", err, err)
	}
	assertObservationAbsent(t, manifest.Attempt.Layout.ManifestPath)
}

func TestComposeFreeCADRuntimeObservationRequest_IsReadOnly(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	attempt := prepareObservationSource(t, req, []byte("prepared"))
	sourceBefore, _ := os.ReadFile(attempt.Layout.SourceDocumentPath)
	if _, err := ComposeFreeCADRuntimeObservationRequest(req); err != nil {
		t.Fatal(err)
	}
	sourceAfter, _ := os.ReadFile(attempt.Layout.SourceDocumentPath)
	if !bytes.Equal(sourceBefore, sourceAfter) {
		t.Fatal("prepared source modified")
	}
	for _, path := range append([]string{attempt.Layout.ManifestPath, attempt.Layout.ResultPath, filepath.Join(attempt.Layout.WorkingCopyDir, FreeCADRuntimeObservationRequestFilename)}, observationOutputPaths(req, attempt)...) {
		assertObservationAbsent(t, path)
	}
}

func TestComposeFreeCADRuntimeObservationRequest_IsDeterministic(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	prepareObservationSource(t, req, []byte("prepared"))
	first, err := ComposeFreeCADRuntimeObservationRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ComposeFreeCADRuntimeObservationRequest(cloneFreeCADObservationRequest(req))
	if err != nil {
		t.Fatal(err)
	}
	if first.Path != second.Path || !reflect.DeepEqual(first.Contract, second.Contract) || !bytes.Equal(first.JSON, second.JSON) ||
		!bytes.HasSuffix(first.JSON, []byte("\n")) || bytes.HasSuffix(first.JSON, []byte("\n\n")) {
		t.Fatalf("nondeterministic\n%#v\n%#v", first, second)
	}
}

func TestComposeFreeCADRuntimeObservationRequest_CanonicalRoundTrip(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	prepareObservationSource(t, req, []byte("prepared"))
	got, err := ComposeFreeCADRuntimeObservationRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := verification.Parse(bytes.TrimSuffix(got.JSON, []byte("\n")))
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := verification.CanonicalJSON(parsed)
	if err != nil || !bytes.Equal(append(canonical, '\n'), got.JSON) {
		t.Fatalf("round trip: %v\n%s\n%s", err, canonical, got.JSON)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(got.JSON, &top); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(top))
	for k := range top {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	want := []string{"checks", "expected", "observationContext", "observe", "schemaVersion"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("keys=%v", keys)
	}
}

func TestComposeFreeCADRuntimeObservationRequest_DoesNotMutateRequest(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	prepareObservationSource(t, req, []byte("prepared"))
	want := cloneFreeCADObservationRequest(req)
	if _, err := ComposeFreeCADRuntimeObservationRequest(req); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(req, want) {
		t.Fatalf("request mutated\nwant=%#v\ngot=%#v", want, req)
	}
}

func TestWriteFreeCADRuntimeObservationRequest_MaterializesRequest(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	prepareObservationSource(t, req, []byte("prepared"))
	got, err := WriteFreeCADRuntimeObservationRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{got.Manifest.Attempt.Layout.SourceDocumentPath, got.Manifest.Attempt.Layout.OutputDir, got.Manifest.Attempt.Layout.ManifestPath, got.Path} {
		if info, err := os.Stat(path); err != nil || (path != got.Manifest.Attempt.Layout.OutputDir && !info.Mode().IsRegular()) {
			t.Fatalf("required path %q: %v %#v", path, err, info)
		}
	}
	persisted, _ := os.ReadFile(got.Path)
	if !bytes.Equal(persisted, got.JSON) {
		t.Fatal("persisted bytes differ")
	}
	parsed, err := verification.Parse(bytes.TrimSuffix(persisted, []byte("\n")))
	if err != nil || !reflect.DeepEqual(*parsed, got.Contract) {
		t.Fatalf("persisted contract: %v %#v", err, parsed)
	}
	for _, path := range append([]string{got.Manifest.Attempt.Layout.ResultPath, filepath.Join(got.Manifest.Attempt.Layout.WorkingCopyDir, "prm.observed.json")}, observationOutputPaths(req, got.Manifest.Attempt)...) {
		assertObservationAbsent(t, path)
	}
}

func TestWriteFreeCADRuntimeObservationRequest_PreflightFailureHasNoSideEffects(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	req.Manifest.Verification.ExpectedParameters[0].ID = ""
	_, err := WriteFreeCADRuntimeObservationRequest(req)
	requireObservationError(t, err, FreeCADRuntimeObservationRequestStageIntentValidation)
	assertObservationAbsent(t, req.ProductDir)
}

func TestWriteFreeCADRuntimeObservationRequest_ManifestFailureWritesNoRequest(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	preflight, err := freecad.ComposeFreeCADRuntimeManifest(req)
	if err != nil {
		t.Fatal(err)
	}
	_, err = WriteFreeCADRuntimeObservationRequest(req)
	requireObservationError(t, err, FreeCADRuntimeObservationRequestStageManifestMaterialization)
	var manifestErr *freecad.FreeCADRuntimeManifestError
	if !errors.As(err, &manifestErr) {
		t.Fatalf("manifest cause lost: %v", err)
	}
	assertObservationAbsent(t, filepath.Join(preflight.Attempt.Layout.WorkingCopyDir, FreeCADRuntimeObservationRequestFilename))
}

func TestWriteFreeCADRuntimeObservationRequest_DigestMatchesPreparedAttempt(t *testing.T) {
	req1 := validFreeCADObservationRequest(t)
	writeObservationHostSource(t, req1, []byte("A"))
	one, err := WriteFreeCADRuntimeObservationRequest(req1)
	if err != nil {
		t.Fatal(err)
	}
	req2 := cloneFreeCADObservationRequest(req1)
	req2.Attempt = 2
	writeObservationHostSource(t, req2, []byte("B"))
	two, err := WriteFreeCADRuntimeObservationRequest(req2)
	if err != nil {
		t.Fatal(err)
	}
	a, b := sha256.Sum256([]byte("A")), sha256.Sum256([]byte("B"))
	if one.Contract.Expected.Metadata[0].Value != hex.EncodeToString(a[:]) || two.Contract.Expected.Metadata[0].Value != hex.EncodeToString(b[:]) ||
		one.Path == two.Path || one.Contract.Expected.References[0].Name == req1.Manifest.Inputs.SourceModel || two.Contract.Expected.References[0].Name == req2.Manifest.Inputs.SourceModel {
		t.Fatalf("attempt isolation failed: %#v %#v", one, two)
	}
}

func TestWriteFreeCADRuntimeObservationRequest_RepeatedWriteIsDeterministic(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	writeObservationHostSource(t, req, []byte("prepared"))
	first, err := WriteFreeCADRuntimeObservationRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(first.Manifest.Attempt.Layout.OutputDir, "keep.txt")
	if err := os.WriteFile(unrelated, []byte("keep"), 0o640); err != nil {
		t.Fatal(err)
	}
	second, err := WriteFreeCADRuntimeObservationRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Path != second.Path || !reflect.DeepEqual(first.Contract, second.Contract) || !bytes.Equal(first.JSON, second.JSON) {
		t.Fatal("repeated write drift")
	}
	if data, _ := os.ReadFile(unrelated); string(data) != "keep" || len(observationTempFiles(t, filepath.Dir(first.Path))) != 0 {
		t.Fatal("unrelated content or temp cleanup failed")
	}
}

func TestWriteFreeCADRuntimeObservationRequest_ReplacesExistingRegularFile(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	attempt := prepareObservationSource(t, req, []byte("prepared"))
	path := filepath.Join(attempt.Layout.WorkingCopyDir, FreeCADRuntimeObservationRequestFilename)
	if err := os.WriteFile(path, []byte("sentinel"), 0o640); err != nil {
		t.Fatal(err)
	}
	got, err := WriteFreeCADRuntimeObservationRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	info, _ := os.Stat(path)
	if !bytes.Equal(data, got.JSON) || bytes.Contains(data, []byte("sentinel")) || !info.Mode().IsRegular() || len(observationTempFiles(t, filepath.Dir(path))) != 0 {
		t.Fatal("atomic replacement failed")
	}
}

func TestWriteFreeCADRuntimeObservationRequest_ReturnedJSONDoesNotAliasFile(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	writeObservationHostSource(t, req, []byte("prepared"))
	got, err := WriteFreeCADRuntimeObservationRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(got.Path)
	got.JSON[0] = 'X'
	after, _ := os.ReadFile(got.Path)
	if !bytes.Equal(before, after) {
		t.Fatal("returned bytes alias file")
	}
}

func TestWriteFreeCADRuntimeObservationRequest_DoesNotMutateRequest(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	writeObservationHostSource(t, req, []byte("prepared"))
	want := cloneFreeCADObservationRequest(req)
	if _, err := WriteFreeCADRuntimeObservationRequest(req); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(req, want) {
		t.Fatal("request mutated")
	}
}

func TestWriteFreeCADRuntimeObservationRequest_PreservesExistingFileOnPreflightFailure(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	writeObservationHostSource(t, req, []byte("prepared"))
	first, err := WriteFreeCADRuntimeObservationRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	beforeRequest, _ := os.ReadFile(first.Path)
	beforeManifest, _ := os.ReadFile(first.Manifest.Attempt.Layout.ManifestPath)
	req.Manifest.Verification.ExpectedParameters[0].ID = ""
	_, err = WriteFreeCADRuntimeObservationRequest(req)
	requireObservationError(t, err, FreeCADRuntimeObservationRequestStageIntentValidation)
	afterRequest, _ := os.ReadFile(first.Path)
	afterManifest, _ := os.ReadFile(first.Manifest.Attempt.Layout.ManifestPath)
	if !bytes.Equal(beforeRequest, afterRequest) || !bytes.Equal(beforeManifest, afterManifest) || len(observationTempFiles(t, filepath.Dir(first.Path))) != 0 {
		t.Fatal("existing Task 8/9 state changed")
	}
}

func TestWriteFreeCADRuntimeObservationRequest_CleansTempFileAfterWriteFailure(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	writeObservationHostSource(t, req, []byte("prepared"))
	manifest, err := freecad.WriteFreeCADRuntimeManifest(req)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(manifest.Attempt.Layout.WorkingCopyDir, FreeCADRuntimeObservationRequestFilename)
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = WriteFreeCADRuntimeObservationRequest(req)
	requireObservationError(t, err, FreeCADRuntimeObservationRequestStageContractWrite)
	info, statErr := os.Stat(path)
	if statErr != nil || !info.IsDir() || len(observationTempFiles(t, filepath.Dir(path))) != 0 {
		t.Fatalf("cleanup/destination=%v %#v", statErr, info)
	}
	for _, required := range []string{manifest.Attempt.Layout.SourceDocumentPath, manifest.Attempt.Layout.ManifestPath, manifest.Attempt.Layout.OutputDir} {
		if _, err := os.Stat(required); err != nil {
			t.Fatalf("Task 8 state lost: %v", err)
		}
	}
}

func TestWriteFreeCADRuntimeObservationRequest_WriteFailureCreatesNoPartialFile(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	writeObservationHostSource(t, req, []byte("prepared"))
	manifest, err := freecad.WriteFreeCADRuntimeManifest(req)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(manifest.Attempt.Layout.WorkingCopyDir, FreeCADRuntimeObservationRequestFilename)
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = WriteFreeCADRuntimeObservationRequest(req)
	requireObservationError(t, err, FreeCADRuntimeObservationRequestStageContractWrite)
	if info, _ := os.Stat(path); info == nil || info.Mode().IsRegular() {
		t.Fatal("partial regular request created")
	}
}

func TestFreeCADRuntimeObservationRequestError_ErrorAndUnwrap(t *testing.T) {
	sentinel := errors.New("sentinel")
	err := &FreeCADRuntimeObservationRequestError{Stage: "stage", Field: "field", AttemptID: "attempt", Path: "/path", Err: sentinel}
	for _, text := range []string{"stage", "field", "attempt", "/path", "sentinel"} {
		if !strings.Contains(err.Error(), text) {
			t.Fatalf("missing %q: %s", text, err)
		}
	}
	var typed *FreeCADRuntimeObservationRequestError
	if !errors.Is(err, sentinel) || !errors.As(err, &typed) || strings.Contains(err.Error(), "schemaVersion") {
		t.Fatalf("error contract=%v", err)
	}
}

func TestFreeCADRuntimeObservationRequest_ErrorStages(t *testing.T) {
	if FreeCADRuntimeObservationRequestStageContractSerialization != "contract_serialization" {
		t.Fatal("serialization stage changed")
	}
	manual := &FreeCADRuntimeObservationRequestError{Stage: FreeCADRuntimeObservationRequestStageContractSerialization, Err: errors.New("sentinel")}
	requireObservationError(t, manual, FreeCADRuntimeObservationRequestStageContractSerialization)

	req := validFreeCADObservationRequest(t)
	req.JobID = ""
	_, err := ComposeFreeCADRuntimeObservationRequest(req)
	requireObservationError(t, err, FreeCADRuntimeObservationRequestStageManifestPreflight)

	req = validFreeCADObservationRequest(t)
	req.CADRuntime.ResultFilename = FreeCADRuntimeObservationRequestFilename
	_, err = ComposeFreeCADRuntimeObservationRequest(req)
	requireObservationError(t, err, FreeCADRuntimeObservationRequestStageRequestPath)

	req = validFreeCADObservationRequest(t)
	req.Manifest.Verification.ExpectedParameters[0].ID = ""
	_, err = ComposeFreeCADRuntimeObservationRequest(req)
	requireObservationError(t, err, FreeCADRuntimeObservationRequestStageIntentValidation)

	req = validFreeCADObservationRequest(t)
	_, err = WriteFreeCADRuntimeObservationRequest(req)
	requireObservationError(t, err, FreeCADRuntimeObservationRequestStageManifestMaterialization)

	req = validFreeCADObservationRequest(t)
	_, err = ComposeFreeCADRuntimeObservationRequest(req)
	requireObservationError(t, err, FreeCADRuntimeObservationRequestStageContractDerivation)
}

func TestFreeCADRuntimeObservationRequestError_PreservesCauses(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	_, err := WriteFreeCADRuntimeObservationRequest(req)
	var manifestErr *freecad.FreeCADRuntimeManifestError
	if !errors.As(err, &manifestErr) {
		t.Fatalf("Task 8 cause lost: %v", err)
	}
	req = validFreeCADObservationRequest(t)
	_, err = ComposeFreeCADRuntimeObservationRequest(req)
	var fileErr *verification.FileError
	if !errors.As(err, &fileErr) || !errors.Is(err, verification.ErrIO) {
		t.Fatalf("verification cause lost: %v", err)
	}
	sentinel := errors.New("filesystem sentinel")
	wrapped := &FreeCADRuntimeObservationRequestError{Stage: FreeCADRuntimeObservationRequestStageContractWrite, Err: &verification.FileError{Path: "/x", Err: sentinel}}
	if !errors.Is(wrapped, sentinel) || !errors.Is(wrapped, verification.ErrIO) {
		t.Fatal("filesystem unwrap lost")
	}
	validation := &FreeCADRuntimeObservationRequestError{Stage: FreeCADRuntimeObservationRequestStageContractCompatibility, Err: &verification.ValidationError{Message: "bad"}}
	if !errors.Is(validation, verification.ErrValidation) {
		t.Fatal("validation unwrap lost")
	}
}

func TestFreeCADRuntimeObservationRequest_DoesNotExecuteRuntime(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	marker := filepath.Join(t.TempDir(), "executed")
	req.Executable.Command, req.Executable.Path = marker, marker
	writeObservationHostSource(t, req, []byte("prepared"))
	if _, err := WriteFreeCADRuntimeObservationRequest(req); err != nil {
		t.Fatal(err)
	}
	assertObservationAbsent(t, marker)
}

func TestFreeCADRuntimeObservationRequest_DoesNotProduceVerificationResult(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	writeObservationHostSource(t, req, []byte("prepared"))
	got, err := WriteFreeCADRuntimeObservationRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range [][]byte{[]byte(`"status"`), []byte(`"pass"`), []byte(`"fail"`), []byte(`"accepted"`), []byte(`"rejected"`)} {
		if bytes.Contains(got.JSON, forbidden) {
			t.Fatalf("verification decision leaked: %s", got.JSON)
		}
	}
}

func TestWriteFreeCADRuntimeObservationRequest_DoesNotMaterializeTask10Outputs(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	req.Manifest.Outputs = []planner.ExportManifestOutput{
		{Type: "step", Filename: "outputs/Widget.step", Object: "Body"},
		{Type: "pdf", Filename: "outputs/Widget.pdf"},
		{Type: "csv", Filename: "outputs/Widget.csv", Columns: []string{"name"}},
	}
	writeObservationHostSource(t, req, []byte("prepared"))
	got, err := WriteFreeCADRuntimeObservationRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(got.Manifest.Attempt.Layout.OutputDir); err != nil {
		t.Fatal(err)
	}
	for _, path := range append([]string{got.Manifest.Attempt.Layout.ResultPath, filepath.Join(got.Manifest.Attempt.Layout.WorkingCopyDir, "prm.observed.json")}, observationOutputPaths(req, got.Manifest.Attempt)...) {
		assertObservationAbsent(t, path)
	}
}

func TestFreeCADRuntimeObservationRequest_DoesNotAlterLogicalEngineeringIdentity(t *testing.T) {
	req := validFreeCADObservationRequest(t)
	prepareObservationSource(t, req, []byte("prepared"))
	want := cloneFreeCADObservationRequest(req)
	beforeAttempt, err := freecad.ComputeFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	composed, err := ComposeFreeCADRuntimeObservationRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WriteFreeCADRuntimeObservationRequest(req); err != nil {
		t.Fatal(err)
	}
	afterAttempt, err := freecad.ComputeFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(req, want) || !reflect.DeepEqual(beforeAttempt.Identity, afterAttempt.Identity) {
		t.Fatal("logical identity changed")
	}
	logical, _ := json.Marshal(struct {
		JobID, ProductKey, StepID, PlanHash string
	}{req.JobID, req.ProductKey, req.StepID, req.Manifest.PlanHash})
	for _, forbidden := range []string{composed.Path, composed.Manifest.Attempt.Layout.SourceDocumentPath, composed.Manifest.Attempt.Layout.ManifestPath, req.Executable.Path, string(composed.JSON)} {
		if forbidden != "" && bytes.Contains(logical, []byte(forbidden)) {
			t.Fatalf("operational state entered logical identity: %s", logical)
		}
	}
}

func TestComposeFreeCADRuntimeObservationRequest_PathDoesNotAlterEngineeringIdentity(t *testing.T) {
	TestFreeCADRuntimeObservationRequest_DoesNotAlterLogicalEngineeringIdentity(t)
}
