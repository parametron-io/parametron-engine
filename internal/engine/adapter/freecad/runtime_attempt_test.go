package freecad

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/adapter"
	"parametron/internal/engine/handoff"
	"parametron/internal/engine/job"
	"parametron/internal/engine/runtimecap"
)

func validCADRuntimeRequest() adapter.CADRuntimeOrchestrationRequest {
	return adapter.CADRuntimeOrchestrationRequest{
		JobID: "job-1", ProductKey: "widget", ProductDir: "/missing/products/widget", StepID: "2", Attempt: 1,
		CSV:        planner.WriteCSVPayload{ProductKey: "widget", Filename: "misleading.csv", Headers: []string{"width"}, Values: []any{10.0}},
		Manifest:   planner.WriteExportManifestPayload{ProductKey: "widget", ManifestFilename: "export_manifest_v1.json", Adapter: "freecad", Values: map[string]any{"width": 10.0}},
		CADRuntime: planner.RunCADRuntimePayload{ProductKey: "widget", Adapter: "freecad", ManifestFilename: "export_manifest_v1.json", ResultFilename: "result.json"},
		Executable: runtimecap.ExecutableSelection{Adapter: "freecad", Command: "/opt/parametron/bin/parametron-freecad", Path: "/opt/parametron/bin/parametron-freecad", Source: runtimecap.ExecutableSelectionSourceConfigured},
	}
}

func validFreeCADRuntimeAttemptRequest(t *testing.T) adapter.CADRuntimeOrchestrationRequest {
	t.Helper()
	root := t.TempDir()
	req := validCADRuntimeRequest()
	req.JobID = "job-widget"
	req.ProductKey = "widget"
	req.ProductDir = filepath.Join(root, "product")
	req.StepID = "2"
	req.Attempt = 1
	req.Manifest.ProductKey = req.ProductKey
	req.Manifest.PlanHash = "plan-hash-widget"
	req.Manifest.Inputs.SourceModel = filepath.Join(root, "host", "Widget.FCStd")
	req.Manifest.ManifestFilename = "runtime_manifest.json"
	req.Manifest.Adapter = runtimecap.FreeCADAdapterID
	req.CADRuntime.ProductKey = req.ProductKey
	req.CADRuntime.Adapter = runtimecap.FreeCADAdapterID
	req.CADRuntime.ManifestFilename = req.Manifest.ManifestFilename
	req.CADRuntime.ResultFilename = "cad_result.json"
	req.Executable = runtimecap.ExecutableSelection{
		Adapter: runtimecap.FreeCADAdapterID,
		Command: "/opt/parametron/bin/parametron-freecad",
		Path:    "/opt/parametron/bin/parametron-freecad",
		Source:  runtimecap.ExecutableSelectionSourceConfigured,
	}
	return req
}

func cloneFreeCADRuntimeAttemptRequest(req adapter.CADRuntimeOrchestrationRequest) adapter.CADRuntimeOrchestrationRequest {
	clone := req
	clone.CSV.Headers = append([]string(nil), req.CSV.Headers...)
	clone.CSV.Values = cloneAttemptAnySlice(req.CSV.Values)
	clone.Manifest.Values = cloneAttemptAnyMap(req.Manifest.Values)
	clone.Manifest.ParameterAssignments = append([]planner.ExportManifestParameterAssignment(nil), req.Manifest.ParameterAssignments...)
	clone.Manifest.AssemblyMutations = cloneAttemptMutations(req.Manifest.AssemblyMutations)
	clone.Manifest.PartMutations = cloneAttemptMutations(req.Manifest.PartMutations)
	clone.Manifest.Outputs = append([]planner.ExportManifestOutput(nil), req.Manifest.Outputs...)
	for i := range clone.Manifest.Outputs {
		clone.Manifest.Outputs[i].Columns = append([]string(nil), req.Manifest.Outputs[i].Columns...)
	}
	clone.Manifest.Verification.ExpectedParameters = append([]planner.VerificationExpectedParameter(nil), req.Manifest.Verification.ExpectedParameters...)
	clone.Manifest.Verification.ObservationParameterLinks = append([]planner.VerificationObservationParameterLink(nil), req.Manifest.Verification.ObservationParameterLinks...)
	return clone
}

func cloneAttemptMutations(in *planner.ExportManifestMutationCollection) *planner.ExportManifestMutationCollection {
	if in == nil {
		return nil
	}
	out := *in
	out.Parameters = append([]planner.ExportManifestParameterMutation(nil), in.Parameters...)
	out.Properties = append([]planner.ExportManifestPropertyMutation(nil), in.Properties...)
	for i := range out.Properties {
		out.Properties[i].Value = cloneAttemptAny(in.Properties[i].Value)
	}
	out.Suppression = append([]planner.ExportManifestSuppressionMutation(nil), in.Suppression...)
	return &out
}

func cloneAttemptAnyMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for key, value := range in {
		out[key] = cloneAttemptAny(value)
	}
	return out
}

func cloneAttemptAnySlice(in []interface{}) []interface{} {
	if in == nil {
		return nil
	}
	out := make([]interface{}, len(in))
	for i, value := range in {
		out[i] = cloneAttemptAny(value)
	}
	return out
}

func cloneAttemptAny(value interface{}) interface{} {
	switch value := value.(type) {
	case map[string]interface{}:
		return cloneAttemptAnyMap(value)
	case []interface{}:
		return cloneAttemptAnySlice(value)
	case []string:
		return append([]string(nil), value...)
	default:
		return value
	}
}

func enrichFreeCADRuntimeAttemptRequest(req *adapter.CADRuntimeOrchestrationRequest) {
	req.CSV.Headers = []string{"width", "label"}
	req.CSV.Values = []interface{}{10.0, map[string]interface{}{"nested": []interface{}{"value", 2.0}}}
	req.Manifest.SchemaVersion = planner.ExportManifestSchemaVersion
	req.Manifest.Product = planner.ExportManifestProduct{ID: req.ProductKey}
	req.Manifest.SourceDocument = "source/Widget.FCStd"
	req.Manifest.Values = map[string]interface{}{"width": 10.0, "nested": map[string]interface{}{"labels": []interface{}{"a", "b"}}}
	req.Manifest.ParameterAssignments = []planner.ExportManifestParameterAssignment{{Name: "width", Target: "Body.Width", Value: 10, Type: "number", Unit: "mm"}}
	req.Manifest.AssemblyMutations = &planner.ExportManifestMutationCollection{
		Properties:  []planner.ExportManifestPropertyMutation{{Object: "Assembly", Property: "Metadata", Value: map[string]interface{}{"label": "widget"}}},
		Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Bracket", Suppressed: true}},
	}
	req.Manifest.PartMutations = &planner.ExportManifestMutationCollection{
		Parameters: []planner.ExportManifestParameterMutation{{Object: "Body", Property: "Width", ValueParam: "width", Type: "number", Unit: "mm"}},
	}
	req.Manifest.Outputs = []planner.ExportManifestOutput{{Type: "step", Filename: "Widget.step", Object: "Body", Columns: []string{"name", "value"}}}
	req.Manifest.Verification = planner.VerificationManifestIntent{
		ExpectedParameters:        []planner.VerificationExpectedParameter{{ID: "width", Name: "width", Value: 10, Type: "number", Unit: "mm"}},
		ObservationParameterLinks: []planner.VerificationObservationParameterLink{{ID: "width", Name: "width", GroupName: "dimensions"}},
	}
}

func requireAttemptError(t *testing.T, err error, stage, field string) *FreeCADRuntimeAttemptError {
	t.Helper()
	if err == nil {
		t.Fatal("expected error")
	}
	var attemptErr *FreeCADRuntimeAttemptError
	if !errors.As(err, &attemptErr) {
		t.Fatalf("error %T %v is not FreeCADRuntimeAttemptError", err, err)
	}
	if attemptErr.Stage != stage {
		t.Fatalf("stage=%q want %q: %v", attemptErr.Stage, stage, err)
	}
	if field != "" && attemptErr.Field != field {
		t.Fatalf("field=%q want %q: %v", attemptErr.Field, field, err)
	}
	return attemptErr
}

func writeAttemptSource(t *testing.T, req adapter.CADRuntimeOrchestrationRequest, contents []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(req.Manifest.Inputs.SourceModel), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(req.Manifest.Inputs.SourceModel, contents, 0o640); err != nil {
		t.Fatal(err)
	}
}

func assertPathAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("path %q unexpectedly exists or cannot be inspected: %v", path, err)
	}
}

func TestComputeFreeCADRuntimeAttempt_ValidShape(t *testing.T) {
	req := validFreeCADRuntimeAttemptRequest(t)
	attempt, err := ComputeFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	if attempt.Identity.ID == "" || attempt.Identity.JobID != req.JobID || attempt.Identity.ProductKey != req.ProductKey || attempt.Identity.StepID != req.StepID || attempt.Identity.Attempt != req.Attempt || attempt.Identity.Adapter != req.CADRuntime.Adapter || attempt.Identity.PlanHash != req.Manifest.PlanHash {
		t.Fatalf("unexpected identity: %#v", attempt.Identity)
	}
	if attempt.SourcePath != req.Manifest.Inputs.SourceModel || attempt.Layout.WorkingCopyID != attempt.Identity.ID {
		t.Fatalf("source/layout identity mismatch: %#v", attempt)
	}
	wantRoot := filepath.Join(req.ProductDir, "_working")
	wantDir := filepath.Join(wantRoot, attempt.Identity.ID)
	wantSourceDir := filepath.Join(wantDir, "source")
	want := FreeCADRuntimeWorkingCopyLayout{
		ProductDir: req.ProductDir, WorkingRoot: wantRoot, WorkingCopyDir: wantDir, WorkingCopyID: attempt.Identity.ID,
		SourceDir: wantSourceDir, SourceDocument: "source/Widget.FCStd", SourceDocumentPath: filepath.Join(wantSourceDir, "Widget.FCStd"),
		ManifestPath: filepath.Join(wantDir, req.CADRuntime.ManifestFilename), ResultPath: filepath.Join(wantDir, req.CADRuntime.ResultFilename),
		OutputDir: filepath.Join(wantDir, "outputs"), OutputDirRelativePath: "outputs",
	}
	if !reflect.DeepEqual(attempt.Layout, want) {
		t.Fatalf("layout mismatch\n got: %#v\nwant: %#v", attempt.Layout, want)
	}
}

func TestComputeFreeCADRuntimeAttempt_IDIsSafePathSegment(t *testing.T) {
	keys := []string{
		"Widget PRO / ../ \\ punctuation!?",
		"\u00dcR\u00dcN \u00e7\u00f6\u011f\u0131\u015f / ../../host",
		strings.Repeat("Very-Long Product Prefix! ", 20),
	}
	for _, key := range keys {
		t.Run(fmt.Sprintf("key-%x", len(key)), func(t *testing.T) {
			req := validFreeCADRuntimeAttemptRequest(t)
			req.ProductKey = key
			req.CSV.ProductKey, req.Manifest.ProductKey, req.CADRuntime.ProductKey = key, key, key
			attempt, err := ComputeFreeCADRuntimeAttempt(req)
			if err != nil {
				t.Fatal(err)
			}
			id := attempt.Identity.ID
			if id != strings.ToLower(id) || !regexp.MustCompile(`^[a-z0-9-]+$`).MatchString(id) || strings.ContainsAny(id, "/\\ \t\r\n") || filepath.Base(id) != id || len(id) > 80 {
				t.Fatalf("unsafe attempt ID %q", id)
			}
			for _, segment := range strings.Split(id, "-") {
				if segment == ".." {
					t.Fatalf("traversal segment in %q", id)
				}
			}
			if !strings.HasSuffix(id, "attempt-000001") {
				t.Fatalf("attempt suffix missing: %q", id)
			}
			if strings.Contains(id, req.ProductDir) || strings.Contains(id, req.Manifest.Inputs.SourceModel) || strings.Contains(id, filepath.Dir(req.Manifest.Inputs.SourceModel)) {
				t.Fatalf("host path leaked into %q", id)
			}
		})
	}
}

func TestComputeFreeCADRuntimeAttempt_IsDeterministic(t *testing.T) {
	req := validFreeCADRuntimeAttemptRequest(t)
	first, err := ComputeFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		got, err := ComputeFreeCADRuntimeAttempt(cloneFreeCADRuntimeAttemptRequest(req))
		if err != nil || !reflect.DeepEqual(got, first) {
			t.Fatalf("iteration %d: got=%#v err=%v want=%#v", i, got, err, first)
		}
	}
}

func TestComputeFreeCADRuntimeAttempt_IsolatesAttempts(t *testing.T) {
	req := validFreeCADRuntimeAttemptRequest(t)
	var attempts []FreeCADRuntimeAttempt
	for n := 1; n <= 3; n++ {
		req.Attempt = n
		attempt, err := ComputeFreeCADRuntimeAttempt(req)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(attempt.Identity.ID, fmt.Sprintf("attempt-%06d", n)) {
			t.Fatalf("attempt %d ID=%q", n, attempt.Identity.ID)
		}
		attempts = append(attempts, attempt)
	}
	for i := range attempts {
		for j := i + 1; j < len(attempts); j++ {
			if attempts[i].Identity.ID == attempts[j].Identity.ID || attempts[i].Layout.WorkingCopyDir == attempts[j].Layout.WorkingCopyDir {
				t.Fatalf("attempts share identity/layout: %#v %#v", attempts[i], attempts[j])
			}
		}
		if attempts[i].Layout.ProductDir != req.ProductDir || attempts[i].Layout.WorkingRoot != filepath.Join(req.ProductDir, "_working") || attempts[i].Layout.SourceDocument != "source/Widget.FCStd" {
			t.Fatalf("attempt %d changed common layout: %#v", i+1, attempts[i].Layout)
		}
	}
}

func TestComputeFreeCADRuntimeAttempt_IdentityInputSensitivity(t *testing.T) {
	base := validFreeCADRuntimeAttemptRequest(t)
	baseline, err := ComputeFreeCADRuntimeAttempt(base)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*adapter.CADRuntimeOrchestrationRequest)
		check  func(FreeCADRuntimeAttemptIdentity) bool
	}{
		{"JobID", func(r *adapter.CADRuntimeOrchestrationRequest) { r.JobID = "job-other" }, func(i FreeCADRuntimeAttemptIdentity) bool { return i.JobID == "job-other" }},
		{"ProductKey", func(r *adapter.CADRuntimeOrchestrationRequest) {
			r.ProductKey, r.CSV.ProductKey, r.Manifest.ProductKey, r.CADRuntime.ProductKey = "gadget", "gadget", "gadget", "gadget"
		}, func(i FreeCADRuntimeAttemptIdentity) bool { return i.ProductKey == "gadget" }},
		{"StepID", func(r *adapter.CADRuntimeOrchestrationRequest) { r.StepID = "3" }, func(i FreeCADRuntimeAttemptIdentity) bool { return i.StepID == "3" }},
		{"PlanHash", func(r *adapter.CADRuntimeOrchestrationRequest) { r.Manifest.PlanHash = "plan-hash-other" }, func(i FreeCADRuntimeAttemptIdentity) bool { return i.PlanHash == "plan-hash-other" }},
		{"SourceModel", func(r *adapter.CADRuntimeOrchestrationRequest) {
			r.Manifest.Inputs.SourceModel = filepath.Join(filepath.Dir(r.Manifest.Inputs.SourceModel), "Other.FCStd")
		}, nil},
		{"ManifestFilename", func(r *adapter.CADRuntimeOrchestrationRequest) {
			r.Manifest.ManifestFilename, r.CADRuntime.ManifestFilename = "manifest-v2.json", "manifest-v2.json"
		}, nil},
		{"ResultFilename", func(r *adapter.CADRuntimeOrchestrationRequest) { r.CADRuntime.ResultFilename = "result-v2.json" }, nil},
		{"Attempt", func(r *adapter.CADRuntimeOrchestrationRequest) { r.Attempt = 2 }, func(i FreeCADRuntimeAttemptIdentity) bool { return i.Attempt == 2 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := cloneFreeCADRuntimeAttemptRequest(base)
			test.mutate(&req)
			got, err := ComputeFreeCADRuntimeAttempt(req)
			if err != nil {
				t.Fatal(err)
			}
			if got.Identity.ID == baseline.Identity.ID {
				t.Fatalf("%s did not change attempt ID", test.name)
			}
			if test.check != nil && !test.check(got.Identity) {
				t.Fatalf("identity did not expose changed %s: %#v", test.name, got.Identity)
			}
		})
	}

	other := cloneFreeCADRuntimeAttemptRequest(base)
	other.Manifest.Adapter, other.CADRuntime.Adapter, other.Executable.Adapter = "solidworks", "solidworks", "solidworks"
	if err := adapter.ValidateCADRuntimeOrchestrationRequest(other); err != nil {
		t.Fatalf("generic validation unexpectedly rejected another adapter: %v", err)
	}
	requireAttemptError(t, func() error { _, err := ComputeFreeCADRuntimeAttempt(other); return err }(), FreeCADRuntimeAttemptStageRequestValidation, "CADRuntime.Adapter")
}

func TestComputeFreeCADRuntimeAttempt_ExcludesPlacementAndExecutableSelection(t *testing.T) {
	base := validFreeCADRuntimeAttemptRequest(t)
	baseline, err := ComputeFreeCADRuntimeAttempt(base)
	if err != nil {
		t.Fatal(err)
	}
	variants := []struct {
		name   string
		mutate func(*adapter.CADRuntimeOrchestrationRequest)
	}{
		{"ProductDir", func(r *adapter.CADRuntimeOrchestrationRequest) {
			r.ProductDir = filepath.Join(t.TempDir(), "other-product")
		}},
		{"Command", func(r *adapter.CADRuntimeOrchestrationRequest) { r.Executable.Command = "/different/command" }},
		{"Path", func(r *adapter.CADRuntimeOrchestrationRequest) { r.Executable.Path = "/different/path" }},
		{"Source", func(r *adapter.CADRuntimeOrchestrationRequest) {
			r.Executable.Source = runtimecap.ExecutableSelectionSourceDefaultPATH
		}},
	}
	for _, test := range variants {
		t.Run(test.name, func(t *testing.T) {
			req := cloneFreeCADRuntimeAttemptRequest(base)
			test.mutate(&req)
			got, err := ComputeFreeCADRuntimeAttempt(req)
			if err != nil {
				t.Fatal(err)
			}
			if got.Identity != baseline.Identity {
				t.Fatalf("operational selection changed identity: got=%#v want=%#v", got.Identity, baseline.Identity)
			}
			if test.name == "ProductDir" && (got.Layout.ProductDir == baseline.Layout.ProductDir || got.Layout.WorkingRoot == baseline.Layout.WorkingRoot || got.Layout.WorkingCopyDir == baseline.Layout.WorkingCopyDir) {
				t.Fatalf("placement did not change root-derived layout: %#v %#v", got.Layout, baseline.Layout)
			}
		})
	}
}

func TestComputeFreeCADRuntimeAttempt_DoesNotMutateRequest(t *testing.T) {
	req := validFreeCADRuntimeAttemptRequest(t)
	enrichFreeCADRuntimeAttemptRequest(&req)
	want := cloneFreeCADRuntimeAttemptRequest(req)
	for i := 0; i < 5; i++ {
		if _, err := ComputeFreeCADRuntimeAttempt(req); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(req, want) {
		t.Fatalf("request mutated\n got: %#v\nwant: %#v", req, want)
	}
}

func TestComputeFreeCADRuntimeAttempt_HasNoFilesystemOrProcessSideEffects(t *testing.T) {
	req := validFreeCADRuntimeAttemptRequest(t)
	root := t.TempDir()
	req.ProductDir = filepath.Join(root, "absent-product")
	req.Manifest.Inputs.SourceModel = filepath.Join(root, "absent-source", "Widget.FCStd")
	marker := filepath.Join(root, "marker")
	req.Executable.Command, req.Executable.Path = filepath.Join(root, "would-write-marker"), filepath.Join(root, "would-write-marker")
	attempt, err := ComputeFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{req.ProductDir, attempt.Layout.WorkingRoot, attempt.Layout.WorkingCopyDir, attempt.Layout.SourceDocumentPath, attempt.Layout.ManifestPath, attempt.Layout.ResultPath, attempt.Layout.OutputDir, marker} {
		assertPathAbsent(t, path)
	}
}

func TestComputeFreeCADRuntimeAttempt_PreservesOrchestrationRequestValidationErrors(t *testing.T) {
	for _, test := range []struct {
		name, field string
		mutate      func(*adapter.CADRuntimeOrchestrationRequest)
	}{
		{"empty JobID", "JobID", func(r *adapter.CADRuntimeOrchestrationRequest) { r.JobID = "" }},
		{"missing command", "Executable.Command", func(r *adapter.CADRuntimeOrchestrationRequest) { r.Executable.Command = "" }},
		{"adapter mismatch", "CADRuntime.Adapter", func(r *adapter.CADRuntimeOrchestrationRequest) { r.Manifest.Adapter = "other" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := validFreeCADRuntimeAttemptRequest(t)
			test.mutate(&req)
			_, err := ComputeFreeCADRuntimeAttempt(req)
			requireAttemptError(t, err, FreeCADRuntimeAttemptStageRequestValidation, test.field)
			var requestErr *adapter.CADRuntimeOrchestrationRequestError
			if !errors.As(err, &requestErr) || requestErr.Field != test.field {
				t.Fatalf("Task 6 error lost: %#v %v", requestErr, err)
			}
			assertPathAbsent(t, req.ProductDir)
		})
	}
}

func TestComputeFreeCADRuntimeAttempt_RequiresFreeCADAdapter(t *testing.T) {
	req := validFreeCADRuntimeAttemptRequest(t)
	req.Manifest.Adapter, req.CADRuntime.Adapter, req.Executable.Adapter = "solidworks", "solidworks", "solidworks"
	if err := adapter.ValidateCADRuntimeOrchestrationRequest(req); err != nil {
		t.Fatal(err)
	}
	_, err := ComputeFreeCADRuntimeAttempt(req)
	requireAttemptError(t, err, FreeCADRuntimeAttemptStageRequestValidation, "CADRuntime.Adapter")
	assertPathAbsent(t, req.ProductDir)
}

func TestComputeFreeCADRuntimeAttempt_ValidatesProductDir(t *testing.T) {
	root := t.TempDir()
	values := []string{"", " ", "relative/path", "./relative", root + "/child/..", root + "/./child", " " + root, root + "\x00bad"}
	for i, value := range values {
		t.Run(fmt.Sprintf("case-%d", i), func(t *testing.T) {
			req := validFreeCADRuntimeAttemptRequest(t)
			req.ProductDir = value
			_, err := ComputeFreeCADRuntimeAttempt(req)
			requireAttemptError(t, err, FreeCADRuntimeAttemptStageRequestValidation, "ProductDir")
			if value != "" && !strings.ContainsRune(value, 0) && filepath.IsAbs(value) {
				assertPathAbsent(t, value)
			}
		})
	}
}

func TestComputeFreeCADRuntimeAttempt_ValidatesCanonicalStepID(t *testing.T) {
	for _, stepID := range []string{"0", "1", "2", "42"} {
		req := validFreeCADRuntimeAttemptRequest(t)
		req.StepID = stepID
		if _, err := ComputeFreeCADRuntimeAttempt(req); err != nil {
			t.Fatalf("StepID %q rejected: %v", stepID, err)
		}
	}
	for _, stepID := range []string{"", " ", "+2", "-1", "02", "2.0", "step-2", "2 "} {
		t.Run(fmt.Sprintf("invalid-%q", stepID), func(t *testing.T) {
			req := validFreeCADRuntimeAttemptRequest(t)
			req.StepID = stepID
			_, err := ComputeFreeCADRuntimeAttempt(req)
			requireAttemptError(t, err, FreeCADRuntimeAttemptStageRequestValidation, "StepID")
		})
	}
}

func TestComputeFreeCADRuntimeAttempt_ValidatesPlanHash(t *testing.T) {
	for _, value := range []string{"", " ", " hash", "hash ", "hash\nvalue", "hash\x00value"} {
		t.Run(fmt.Sprintf("value-%q", value), func(t *testing.T) {
			req := validFreeCADRuntimeAttemptRequest(t)
			req.Manifest.PlanHash = value
			_, err := ComputeFreeCADRuntimeAttempt(req)
			requireAttemptError(t, err, FreeCADRuntimeAttemptStageRequestValidation, "Manifest.PlanHash")
		})
	}
}

func TestComputeFreeCADRuntimeAttempt_ValidatesSourceModel(t *testing.T) {
	root := t.TempDir()
	for _, value := range []string{"", " ", "relative.FCStd", "./relative.FCStd", root + "/child/../model.FCStd", " " + filepath.Join(root, "model.FCStd"), filepath.Join(root, "model.FCStd") + "\x00"} {
		t.Run(fmt.Sprintf("invalid-%q", value), func(t *testing.T) {
			req := validFreeCADRuntimeAttemptRequest(t)
			req.Manifest.Inputs.SourceModel = value
			_, err := ComputeFreeCADRuntimeAttempt(req)
			requireAttemptError(t, err, FreeCADRuntimeAttemptStageRequestValidation, "Manifest.Inputs.SourceModel")
		})
	}
	req := validFreeCADRuntimeAttemptRequest(t)
	req.Manifest.Inputs.SourceModel = filepath.Join(root, "absent", "clean.FCStd")
	if _, err := ComputeFreeCADRuntimeAttempt(req); err != nil {
		t.Fatalf("clean nonexistent source rejected: %v", err)
	}
}

func TestComputeFreeCADRuntimeAttempt_UsesManifestInputSourceModel(t *testing.T) {
	req := validFreeCADRuntimeAttemptRequest(t)
	req.Manifest.Inputs.SourceModel = filepath.Join(t.TempDir(), "actual.FCStd")
	req.Manifest.SourceDocument = "source/misleading.FCStd"
	first, err := ComputeFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	if first.SourcePath != req.Manifest.Inputs.SourceModel || filepath.Base(first.Layout.SourceDocumentPath) != "actual.FCStd" || first.Layout.SourceDocument != "source/actual.FCStd" {
		t.Fatalf("wrong source authority: %#v", first)
	}
	req.Manifest.SourceDocument = "source/another-misleading.FCStd"
	second, err := ComputeFreeCADRuntimeAttempt(req)
	if err != nil || first.Identity != second.Identity || !reflect.DeepEqual(first.Layout, second.Layout) {
		t.Fatalf("SourceDocument influenced attempt: first=%#v second=%#v err=%v", first, second, err)
	}
}

func TestComputeFreeCADRuntimeAttempt_ValidatesAndMapsRuntimeFilenames(t *testing.T) {
	for _, names := range [][2]string{{"runtime_manifest.json", "cad_result.json"}, {"manifest-v2.json", "result_v2.json"}} {
		req := validFreeCADRuntimeAttemptRequest(t)
		req.Manifest.ManifestFilename, req.CADRuntime.ManifestFilename, req.CADRuntime.ResultFilename = names[0], names[0], names[1]
		attempt, err := ComputeFreeCADRuntimeAttempt(req)
		if err != nil {
			t.Fatal(err)
		}
		if attempt.Layout.ManifestPath != filepath.Join(attempt.Layout.WorkingCopyDir, names[0]) || attempt.Layout.ResultPath != filepath.Join(attempt.Layout.WorkingCopyDir, names[1]) {
			t.Fatalf("filename mapping mismatch: %#v", attempt.Layout)
		}
	}
	invalid := []string{"", " ", ".", "..", "../manifest.json", "nested/manifest.json", `nested\manifest.json`, "/absolute.json", "white space.json", "bad\x00.json"}
	for _, field := range []string{"manifest", "result"} {
		for _, value := range invalid {
			t.Run(field+fmt.Sprintf("-%q", value), func(t *testing.T) {
				req := validFreeCADRuntimeAttemptRequest(t)
				wantField := "CADRuntime.ResultFilename"
				if field == "manifest" {
					req.Manifest.ManifestFilename, req.CADRuntime.ManifestFilename = value, value
					wantField = "Manifest.ManifestFilename"
				} else {
					req.CADRuntime.ResultFilename = value
				}
				_, err := ComputeFreeCADRuntimeAttempt(req)
				attemptErr := requireAttemptError(t, err, FreeCADRuntimeAttemptStageRequestValidation, "")
				if field == "manifest" && value != "" && strings.TrimSpace(value) != "" {
					wantField = "CADRuntime.ManifestFilename"
				}
				if attemptErr.Field != wantField {
					t.Fatalf("field=%q want=%q: %v", attemptErr.Field, wantField, err)
				}
			})
		}
	}
	req := validFreeCADRuntimeAttemptRequest(t)
	req.Manifest.ManifestFilename = "other.json"
	_, err := ComputeFreeCADRuntimeAttempt(req)
	requireAttemptError(t, err, FreeCADRuntimeAttemptStageRequestValidation, "CADRuntime.ManifestFilename")
}

func TestComputeFreeCADRuntimeAttempt_LayoutIsContained(t *testing.T) {
	req := validFreeCADRuntimeAttemptRequest(t)
	attempt, err := ComputeFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	layout := attempt.Layout
	if layout.WorkingRoot != filepath.Join(req.ProductDir, "_working") || !isPathUnder(layout.WorkingRoot, layout.WorkingCopyDir) || filepath.Base(layout.WorkingCopyID) != layout.WorkingCopyID {
		t.Fatalf("working-copy containment invalid: %#v", layout)
	}
	for name, path := range map[string]string{"source dir": layout.SourceDir, "source document": layout.SourceDocumentPath, "manifest": layout.ManifestPath, "result": layout.ResultPath, "output": layout.OutputDir} {
		if !isPathUnder(layout.WorkingCopyDir, path) || path == layout.WorkingCopyDir {
			t.Fatalf("%s %q not strictly contained under %q", name, path, layout.WorkingCopyDir)
		}
	}
}

func TestComputeFreeCADRuntimeAttempt_FilenameChangesOnlyReservedRuntimePathsAndIdentity(t *testing.T) {
	base := validFreeCADRuntimeAttemptRequest(t)
	first, _ := ComputeFreeCADRuntimeAttempt(base)
	changed := cloneFreeCADRuntimeAttemptRequest(base)
	changed.Manifest.ManifestFilename, changed.CADRuntime.ManifestFilename, changed.CADRuntime.ResultFilename = "manifest-v2.json", "manifest-v2.json", "result-v2.json"
	second, err := ComputeFreeCADRuntimeAttempt(changed)
	if err != nil {
		t.Fatal(err)
	}
	if first.Identity.ID == second.Identity.ID || first.Layout.WorkingCopyDir == second.Layout.WorkingCopyDir {
		t.Fatal("declared filename changes did not isolate attempt")
	}
	if first.Layout.SourceDocument != second.Layout.SourceDocument || first.Layout.OutputDirRelativePath != "outputs" || second.Layout.OutputDirRelativePath != "outputs" {
		t.Fatalf("relative layout shape changed: %#v %#v", first.Layout, second.Layout)
	}
	if filepath.Base(second.Layout.ManifestPath) != "manifest-v2.json" || filepath.Base(second.Layout.ResultPath) != "result-v2.json" {
		t.Fatalf("filenames were not mapped as single segments: %#v", second.Layout)
	}
}

func TestPrepareFreeCADRuntimeAttempt_PreparesSourceAndOutputDirectory(t *testing.T) {
	req := validFreeCADRuntimeAttemptRequest(t)
	contents := []byte("deterministic-fcstd-source\n")
	writeAttemptSource(t, req, contents)
	before, err := os.Stat(req.Manifest.Inputs.SourceModel)
	if err != nil {
		t.Fatal(err)
	}
	beforeBytes, _ := os.ReadFile(req.Manifest.Inputs.SourceModel)
	pure, err := ComputeFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(prepared, pure) {
		t.Fatalf("prepared attempt differs from computation: %#v %#v", prepared, pure)
	}
	for _, dir := range []string{prepared.Layout.WorkingCopyDir, prepared.Layout.SourceDir, prepared.Layout.OutputDir} {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatalf("directory %q not prepared: %v", dir, err)
		}
	}
	preparedInfo, err := os.Stat(prepared.Layout.SourceDocumentPath)
	if err != nil || !preparedInfo.Mode().IsRegular() {
		t.Fatalf("prepared source invalid: %v %#v", err, preparedInfo)
	}
	got, _ := os.ReadFile(prepared.Layout.SourceDocumentPath)
	afterBytes, _ := os.ReadFile(req.Manifest.Inputs.SourceModel)
	after, _ := os.Stat(req.Manifest.Inputs.SourceModel)
	if !reflect.DeepEqual(got, contents) || !reflect.DeepEqual(afterBytes, beforeBytes) || after.Mode() != before.Mode() || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("source copy/original metadata mismatch: before=%#v after=%#v got=%q", before, after, got)
	}
	if err := ValidatePreparedFreeCADRuntimeSourceDocument(prepared.Layout); err != nil || !isPathUnder(prepared.Layout.WorkingCopyDir, prepared.Layout.SourceDocumentPath) {
		t.Fatalf("prepared source containment failed: %v", err)
	}
}

func TestPrepareFreeCADRuntimeAttempt_UsesManifestInputSourceModel(t *testing.T) {
	req := validFreeCADRuntimeAttemptRequest(t)
	writeAttemptSource(t, req, []byte("actual"))
	misleading := filepath.Join(filepath.Dir(req.Manifest.Inputs.SourceModel), "misleading.FCStd")
	if err := os.WriteFile(misleading, []byte("misleading"), 0o644); err != nil {
		t.Fatal(err)
	}
	req.Manifest.SourceDocument = misleading
	attempt, err := PrepareFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(attempt.Layout.SourceDocumentPath)
	if string(got) != "actual" {
		t.Fatalf("prepared wrong source: %q", got)
	}
}

func TestPrepareFreeCADRuntimeAttempt_RecomputesAuthoritativeLayout(t *testing.T) {
	req := validFreeCADRuntimeAttemptRequest(t)
	writeAttemptSource(t, req, []byte("authoritative"))
	computed, _ := ComputeFreeCADRuntimeAttempt(req)
	mutated := computed
	mutated.Layout.SourceDocumentPath = filepath.Join(t.TempDir(), "wrong.FCStd")
	mutated.Layout.ManifestPath = filepath.Join(t.TempDir(), "wrong-manifest.json")
	mutated.Layout.OutputDir = filepath.Join(t.TempDir(), "wrong-output")
	prepared, err := PrepareFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(prepared, computed) || reflect.DeepEqual(prepared.Layout, mutated.Layout) {
		t.Fatalf("preparation did not recompute authoritative layout: %#v", prepared)
	}
	for _, path := range []string{mutated.Layout.SourceDocumentPath, mutated.Layout.ManifestPath, mutated.Layout.OutputDir} {
		assertPathAbsent(t, path)
	}
}

func TestPrepareFreeCADRuntimeAttempt_RepeatedPreparationIsDeterministic(t *testing.T) {
	req := validFreeCADRuntimeAttemptRequest(t)
	writeAttemptSource(t, req, []byte("AAAA"))
	first, err := PrepareFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(first.Layout.OutputDir, "keep.txt")
	if err := os.WriteFile(unrelated, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(req.Manifest.Inputs.SourceModel, []byte("BBBB-complete"), 0o640); err != nil {
		t.Fatal(err)
	}
	second, err := PrepareFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(second.Layout.SourceDocumentPath)
	kept, _ := os.ReadFile(unrelated)
	temps, _ := filepath.Glob(filepath.Join(second.Layout.SourceDir, ".source-document-*.tmp"))
	if !reflect.DeepEqual(first, second) || string(got) != "BBBB-complete" || string(kept) != "keep" || len(temps) != 0 {
		t.Fatalf("repeat mismatch: got=%q kept=%q temps=%v", got, kept, temps)
	}
	assertPathAbsent(t, second.Layout.ManifestPath)
	assertPathAbsent(t, second.Layout.ResultPath)
}

func TestPrepareFreeCADRuntimeAttempt_IsolatesRetryAttempts(t *testing.T) {
	req := validFreeCADRuntimeAttemptRequest(t)
	writeAttemptSource(t, req, []byte("attempt-one"))
	first, err := PrepareFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(first.Layout.OutputDir, "only-first.txt")
	if err := os.WriteFile(marker, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(req.Manifest.Inputs.SourceModel, []byte("attempt-two"), 0o640); err != nil {
		t.Fatal(err)
	}
	req.Attempt = 2
	second, err := PrepareFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	one, _ := os.ReadFile(first.Layout.SourceDocumentPath)
	two, _ := os.ReadFile(second.Layout.SourceDocumentPath)
	if first.Identity.ID == second.Identity.ID || first.Layout.WorkingCopyDir == second.Layout.WorkingCopyDir || first.Layout.OutputDir == second.Layout.OutputDir || string(one) != "attempt-one" || string(two) != "attempt-two" {
		t.Fatalf("attempt isolation failed: first=%#v second=%#v one=%q two=%q", first, second, one, two)
	}
	assertPathAbsent(t, filepath.Join(second.Layout.OutputDir, filepath.Base(marker)))
	for _, path := range []string{first.Layout.ManifestPath, first.Layout.ResultPath, second.Layout.ManifestPath, second.Layout.ResultPath} {
		assertPathAbsent(t, path)
	}
}

func TestPrepareFreeCADRuntimeAttempt_DoesNotMutateRequest(t *testing.T) {
	req := validFreeCADRuntimeAttemptRequest(t)
	enrichFreeCADRuntimeAttemptRequest(&req)
	writeAttemptSource(t, req, []byte("immutable"))
	want := cloneFreeCADRuntimeAttemptRequest(req)
	if _, err := PrepareFreeCADRuntimeAttempt(req); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(req, want) {
		t.Fatalf("request mutated\n got: %#v\nwant: %#v", req, want)
	}
}

func TestPrepareFreeCADRuntimeAttempt_MissingSource(t *testing.T) {
	req := validFreeCADRuntimeAttemptRequest(t)
	computed, err := ComputeFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	_, err = PrepareFreeCADRuntimeAttempt(req)
	attemptErr := requireAttemptError(t, err, FreeCADRuntimeAttemptStageSourcePreparation, "")
	if attemptErr.AttemptID != computed.Identity.ID || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing source context/cause lost: %#v %v", attemptErr, err)
	}
	for _, path := range []string{computed.Layout.ManifestPath, computed.Layout.ResultPath, computed.Layout.OutputDir} {
		assertPathAbsent(t, path)
	}
}

func TestPrepareFreeCADRuntimeAttempt_RejectsSourceDirectory(t *testing.T) {
	req := validFreeCADRuntimeAttemptRequest(t)
	if err := os.MkdirAll(req.Manifest.Inputs.SourceModel, 0o755); err != nil {
		t.Fatal(err)
	}
	computed, _ := ComputeFreeCADRuntimeAttempt(req)
	_, err := PrepareFreeCADRuntimeAttempt(req)
	attemptErr := requireAttemptError(t, err, FreeCADRuntimeAttemptStageSourcePreparation, "")
	if attemptErr.AttemptID != computed.Identity.ID || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("directory source context missing: %v", err)
	}
	assertPathAbsent(t, computed.Layout.ManifestPath)
	assertPathAbsent(t, computed.Layout.ResultPath)
}

func TestPrepareFreeCADRuntimeAttempt_CleansTemporaryFilesAfterFailure(t *testing.T) {
	req := validFreeCADRuntimeAttemptRequest(t)
	writeAttemptSource(t, req, []byte("host-source"))
	computed, _ := ComputeFreeCADRuntimeAttempt(req)
	if err := os.MkdirAll(computed.Layout.SourceDocumentPath, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := PrepareFreeCADRuntimeAttempt(req)
	requireAttemptError(t, err, FreeCADRuntimeAttemptStageSourcePreparation, "")
	temps, globErr := filepath.Glob(filepath.Join(computed.Layout.SourceDir, ".source-document-*.tmp"))
	if globErr != nil || len(temps) != 0 {
		t.Fatalf("temporary files remain: %v %v", temps, globErr)
	}
	if info, err := os.Stat(computed.Layout.SourceDocumentPath); err != nil || !info.IsDir() {
		t.Fatalf("destination conflict was altered: %v %#v", err, info)
	}
	host, _ := os.ReadFile(req.Manifest.Inputs.SourceModel)
	if string(host) != "host-source" {
		t.Fatalf("host source changed: %q", host)
	}
	assertPathAbsent(t, computed.Layout.ManifestPath)
	assertPathAbsent(t, computed.Layout.ResultPath)
}

func TestPrepareFreeCADRuntimeAttempt_RequestValidationFailureHasNoSideEffects(t *testing.T) {
	req := validFreeCADRuntimeAttemptRequest(t)
	root := t.TempDir()
	req.ProductDir = filepath.Join(root, "absent-product")
	req.JobID = ""
	marker := filepath.Join(root, "marker")
	req.Executable.Command, req.Executable.Path = marker, marker
	_, err := PrepareFreeCADRuntimeAttempt(req)
	requireAttemptError(t, err, FreeCADRuntimeAttemptStageRequestValidation, "JobID")
	assertPathAbsent(t, req.ProductDir)
	assertPathAbsent(t, marker)
}

func TestPrepareFreeCADRuntimeAttempt_PreservesExistingOutputContents(t *testing.T) {
	req := validFreeCADRuntimeAttemptRequest(t)
	writeAttemptSource(t, req, []byte("source"))
	attempt, err := PrepareFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		filepath.Join(attempt.Layout.OutputDir, "one.txt"):           []byte("one"),
		filepath.Join(attempt.Layout.OutputDir, "nested", "two.bin"): {0, 1, 2, 3},
	}
	for path, data := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := PrepareFreeCADRuntimeAttempt(req); err != nil {
		t.Fatal(err)
	}
	for path, want := range files {
		got, err := os.ReadFile(path)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("output %q changed: got=%v err=%v want=%v", path, got, err, want)
		}
	}
}

func TestPrepareFreeCADRuntimeAttempt_WrapsOutputDirectoryPreparationFailure(t *testing.T) {
	req := validFreeCADRuntimeAttemptRequest(t)
	writeAttemptSource(t, req, []byte("source"))
	computed, _ := ComputeFreeCADRuntimeAttempt(req)
	if err := os.MkdirAll(computed.Layout.WorkingCopyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(computed.Layout.OutputDir, []byte("conflict"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := PrepareFreeCADRuntimeAttempt(req)
	attemptErr := requireAttemptError(t, err, FreeCADRuntimeAttemptStageSourcePreparation, "")
	if attemptErr.AttemptID != computed.Identity.ID || !strings.Contains(err.Error(), "output directory") || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("lower-level output cause lost: %#v %v", attemptErr, err)
	}
}

func TestFreeCADRuntimeAttemptError_ErrorAndUnwrap(t *testing.T) {
	sentinel := errors.New("sentinel")
	err := &FreeCADRuntimeAttemptError{Stage: FreeCADRuntimeAttemptStageWorkingCopyLayout, Field: "Layout", AttemptID: "widget-hash-attempt-000001", Err: sentinel}
	message := err.Error()
	if !strings.Contains(message, err.Stage) || !strings.Contains(message, err.Field) || !strings.Contains(message, err.AttemptID) || !errors.Is(err, sentinel) {
		t.Fatalf("error contract missing context: %q", message)
	}
	var got *FreeCADRuntimeAttemptError
	if !errors.As(err, &got) || got != err || err.Unwrap() != sentinel {
		t.Fatalf("error unwrap/as mismatch: %#v", got)
	}
}

func TestFreeCADRuntimeAttempt_ErrorStages(t *testing.T) {
	if FreeCADRuntimeAttemptStageRequestValidation != "request_validation" || FreeCADRuntimeAttemptStageAttemptIdentity != "attempt_identity" || FreeCADRuntimeAttemptStageWorkingCopyLayout != "working_copy_layout" || FreeCADRuntimeAttemptStageSourcePreparation != "source_preparation" {
		t.Fatal("exported stage constants changed")
	}
	for _, stage := range []string{FreeCADRuntimeAttemptStageAttemptIdentity, FreeCADRuntimeAttemptStageWorkingCopyLayout} {
		err := &FreeCADRuntimeAttemptError{Stage: stage, Err: errors.New("manual unreachable-stage cause")}
		if !strings.Contains(err.Error(), stage) {
			t.Fatalf("stage absent from error: %v", err)
		}
	}
	req := validFreeCADRuntimeAttemptRequest(t)
	req.JobID = ""
	_, err := ComputeFreeCADRuntimeAttempt(req)
	requireAttemptError(t, err, FreeCADRuntimeAttemptStageRequestValidation, "JobID")
	req = validFreeCADRuntimeAttemptRequest(t)
	_, err = PrepareFreeCADRuntimeAttempt(req)
	requireAttemptError(t, err, FreeCADRuntimeAttemptStageSourcePreparation, "")
}

func TestFreeCADRuntimeAttemptError_PreservesTask6ValidationCause(t *testing.T) {
	req := validFreeCADRuntimeAttemptRequest(t)
	req.Executable.Command = ""
	_, err := ComputeFreeCADRuntimeAttempt(req)
	var attemptErr *FreeCADRuntimeAttemptError
	var requestErr *adapter.CADRuntimeOrchestrationRequestError
	if !errors.As(err, &attemptErr) || !errors.As(err, &requestErr) || attemptErr.Stage != FreeCADRuntimeAttemptStageRequestValidation || requestErr.Field != "Executable.Command" {
		t.Fatalf("validation chain lost: attempt=%#v request=%#v err=%v", attemptErr, requestErr, err)
	}
}

func TestFreeCADRuntimeAttempt_DoesNotExecuteSelectedRuntime(t *testing.T) {
	req := validFreeCADRuntimeAttemptRequest(t)
	writeAttemptSource(t, req, []byte("source"))
	marker := filepath.Join(t.TempDir(), "started")
	script := filepath.Join(t.TempDir(), "runtime.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf started > \""+marker+"\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	req.Executable.Command, req.Executable.Path = script, script
	if _, err := ComputeFreeCADRuntimeAttempt(req); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareFreeCADRuntimeAttempt(req); err != nil {
		t.Fatal(err)
	}
	assertPathAbsent(t, marker)
}

func TestPrepareFreeCADRuntimeAttempt_DoesNotMaterializeLaterRuntimeFiles(t *testing.T) {
	req := validFreeCADRuntimeAttemptRequest(t)
	enrichFreeCADRuntimeAttemptRequest(&req)
	req.Manifest.Outputs = []planner.ExportManifestOutput{{Type: "step", Filename: "Widget.step", Object: "Body"}, {Type: "csv", Filename: "Widget.csv"}}
	writeAttemptSource(t, req, []byte("source"))
	attempt, err := PrepareFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(attempt.Layout.OutputDir); err != nil || !info.IsDir() {
		t.Fatalf("outputs directory missing: %v", err)
	}
	paths := []string{attempt.Layout.ManifestPath, attempt.Layout.ResultPath, filepath.Join(attempt.Layout.WorkingCopyDir, "observation_request.json"), filepath.Join(attempt.Layout.WorkingCopyDir, "observation-request.json")}
	for _, output := range req.Manifest.Outputs {
		paths = append(paths, filepath.Join(attempt.Layout.OutputDir, output.Filename))
	}
	for _, path := range paths {
		assertPathAbsent(t, path)
	}
}

func TestFreeCADRuntimeAttempt_ProductionFreeCADAdapterRemainsUnimplemented(t *testing.T) {
	if _, ok := any(NewFreeCADAdapter(t.TempDir())).(adapter.CADRuntimeOrchestrator); ok {
		t.Fatal("production FreeCAD adapter unexpectedly implements CADRuntimeOrchestrator")
	}
}

func TestFreeCADRuntimeAttempt_DoesNotAlterLogicalEngineeringIdentity(t *testing.T) {
	req := validFreeCADRuntimeAttemptRequest(t)
	enrichFreeCADRuntimeAttemptRequest(&req)
	writeAttemptSource(t, req, []byte("logical-identity-source"))
	steps := []planner.Step{
		{Type: planner.StepWriteCSV, Payload: req.CSV},
		{Type: planner.StepWriteExportManifest, Payload: req.Manifest},
		{Type: planner.StepRunCADRuntime, Payload: req.CADRuntime},
	}
	plan := &planner.ExecutionPlan{Steps: append([]planner.Step(nil), steps...)}
	planHashBefore, err := planner.ComputePlanHash(plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	cacheBefore, err := planner.ComputeStepCacheKeyContext(plan, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	logicalJob, err := job.New(req.ProductKey, steps)
	if err != nil {
		t.Fatal(err)
	}
	jobIDBefore := logicalJob.ID
	pkgBefore, err := handoff.FromJob(logicalJob)
	if err != nil {
		t.Fatal(err)
	}
	handoffBefore, err := json.Marshal(pkgBefore)
	if err != nil {
		t.Fatal(err)
	}
	requestBefore := cloneFreeCADRuntimeAttemptRequest(req)
	attempt, err := ComputeFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareFreeCADRuntimeAttempt(req); err != nil {
		t.Fatal(err)
	}
	planHashAfter, _ := planner.ComputePlanHash(plan, nil)
	cacheAfter, _ := planner.ComputeStepCacheKeyContext(plan, nil, 2)
	pkgAfter, err := handoff.FromJob(logicalJob)
	if err != nil {
		t.Fatal(err)
	}
	handoffAfter, _ := json.Marshal(pkgAfter)
	if planHashAfter != planHashBefore || !reflect.DeepEqual(cacheAfter, cacheBefore) || logicalJob.ID != jobIDBefore || !reflect.DeepEqual(handoffAfter, handoffBefore) || !reflect.DeepEqual(req, requestBefore) {
		t.Fatal("attempt computation/preparation altered a logical engineering identity surface")
	}
	for name, data := range map[string][]byte{"handoff": handoffAfter, "plan hash": []byte(planHashAfter), "job ID": []byte(logicalJob.ID), "cache context": []byte(fmt.Sprintf("%#v", cacheAfter))} {
		if strings.Contains(string(data), attempt.Identity.ID) || strings.Contains(string(data), attempt.Layout.WorkingCopyDir) || strings.Contains(string(data), req.Executable.Path) {
			t.Fatalf("operational attempt/executable leaked into %s: %s", name, data)
		}
	}
}

func TestComputeFreeCADRuntimeAttempt_IdentityIsIndependentOfProductRoot(t *testing.T) {
	firstReq := validFreeCADRuntimeAttemptRequest(t)
	secondReq := cloneFreeCADRuntimeAttemptRequest(firstReq)
	secondReq.ProductDir = filepath.Join(t.TempDir(), "other-root")
	first, _ := ComputeFreeCADRuntimeAttempt(firstReq)
	second, err := ComputeFreeCADRuntimeAttempt(secondReq)
	if err != nil {
		t.Fatal(err)
	}
	if first.Identity != second.Identity || first.Layout.WorkingRoot == second.Layout.WorkingRoot || first.Layout.WorkingCopyDir == second.Layout.WorkingCopyDir || first.Layout.SourceDocument != second.Layout.SourceDocument || first.Layout.OutputDirRelativePath != second.Layout.OutputDirRelativePath {
		t.Fatalf("cross-root determinism mismatch: %#v %#v", first, second)
	}
}

func TestComputeFreeCADRuntimeAttempt_IdentityIsIndependentOfExecutableSelection(t *testing.T) {
	base := validFreeCADRuntimeAttemptRequest(t)
	variants := []runtimecap.ExecutableSelection{
		{Adapter: "freecad", Command: "/opt/one/wrapper", Path: "/opt/one/wrapper", Source: runtimecap.ExecutableSelectionSourceConfigured},
		{Adapter: "freecad", Command: "parametron-freecad", Path: "/nix/store/default/bin/parametron-freecad", Source: runtimecap.ExecutableSelectionSourceDefaultPATH},
		{Adapter: "freecad", Command: "/Applications/FreeCAD Wrapper", Path: "/Applications/FreeCAD Wrapper", Source: runtimecap.ExecutableSelectionSourceConfigured},
	}
	var want FreeCADRuntimeAttemptIdentity
	for i, selection := range variants {
		req := cloneFreeCADRuntimeAttemptRequest(base)
		req.Executable = selection
		attempt, err := ComputeFreeCADRuntimeAttempt(req)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			want = attempt.Identity
		} else if attempt.Identity != want {
			t.Fatalf("selection changed identity: %#v want %#v", attempt.Identity, want)
		}
	}
}

func TestPrepareFreeCADRuntimeAttempt_MatchesPureComputation(t *testing.T) {
	req := validFreeCADRuntimeAttemptRequest(t)
	pure, err := ComputeFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	assertPathAbsent(t, req.Manifest.Inputs.SourceModel)
	writeAttemptSource(t, req, []byte("created-after-computation"))
	prepared, err := PrepareFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(prepared, pure) {
		t.Fatalf("preparation differs from pure computation: %#v %#v", prepared, pure)
	}
}

func TestComputeFreeCADRuntimeWorkingCopyLayout_Task7RefactorPreservesLegacyContract(t *testing.T) {
	req := FreeCADRuntimeWorkingCopyLayoutRequest{ProductDir: t.TempDir(), ProductKey: "legacy", PlanHash: "legacy-plan", SourcePath: "relative/model.FCStd"}
	first, err := ComputeFreeCADRuntimeWorkingCopyLayout(req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ComputeFreeCADRuntimeWorkingCopyLayout(req)
	if err != nil {
		t.Fatal(err)
	}
	if first.WorkingCopyID != second.WorkingCopyID || strings.Contains(first.WorkingCopyID, "attempt-") || filepath.Base(first.ManifestPath) != planner.FreeCADRuntimeExportManifestFilename || filepath.Base(first.ResultPath) != freeCADRuntimeResultFilename {
		t.Fatalf("legacy contract changed: %#v %#v", first, second)
	}
}

func TestPrepareFreeCADRuntimeAttempt_SourceMetadataCheckIsStable(t *testing.T) {
	// This guards the metadata assertions above against coarse filesystem clocks.
	req := validFreeCADRuntimeAttemptRequest(t)
	writeAttemptSource(t, req, []byte("metadata"))
	wantTime := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(req.Manifest.Inputs.SourceModel, wantTime, wantTime); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(req.Manifest.Inputs.SourceModel)
	if _, err := PrepareFreeCADRuntimeAttempt(req); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(req.Manifest.Inputs.SourceModel)
	if !before.ModTime().Equal(after.ModTime()) || before.Mode() != after.Mode() || before.Size() != after.Size() {
		t.Fatalf("source metadata changed: before=%#v after=%#v", before, after)
	}
}
