package adapter

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/authoring/planner"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := ensureDir(filepath.Dir(path)); err != nil {
		t.Fatalf("failed to create parent directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
}

func ensureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

func manifestTempFiles(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".export-manifest-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

// writeGenericManifest exercises the generic parent-package manifest boundary
// (validate, marshal, atomic write) the way a CAD-neutral caller would.
func writeGenericManifest(t *testing.T, outputDir string, payload planner.WriteExportManifestPayload) {
	t.Helper()
	if err := ValidateGenericExportManifest(payload, false); err != nil {
		t.Fatalf("ValidateGenericExportManifest returned error: %v", err)
	}
	data, err := MarshalGenericExportManifest(payload)
	if err != nil {
		t.Fatalf("MarshalGenericExportManifest returned error: %v", err)
	}
	filePath := filepath.Join(outputDir, payload.ManifestFilename)
	if err := WriteExportManifestFile(outputDir, filePath, data, ""); err != nil {
		t.Fatalf("WriteExportManifestFile returned error: %v", err)
	}
}

func TestWriteExportManifest_WritesExpectedJSON(t *testing.T) {
	outputDir := t.TempDir()

	payload := planner.WriteExportManifestPayload{
		ProductKey:       "widget",
		ManifestFilename: planner.ExportManifestFilename,
		SchemaVersion:    planner.ExportManifestSchemaVersion,
		PlanHash:         "abc123",
		Product:          planner.ExportManifestProduct{ID: "widget"},
		Inputs:           planner.ExportManifestInputs{},
		Values:           map[string]interface{}{"width": 120.0},
		ParameterAssignments: []planner.ExportManifestParameterAssignment{
			{Name: "width", Value: 120.0, Type: "number", Unit: "mm"},
		},
		Outputs: []planner.ExportManifestOutput{
			{Type: "step", Filename: "widget.step"},
		},
	}

	writeGenericManifest(t, outputDir, payload)

	manifestPath := filepath.Join(outputDir, planner.ExportManifestFilename)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("failed to read manifest: %v", err)
	}

	var decoded planner.WriteExportManifestPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to decode manifest JSON: %v", err)
	}
	if decoded.SchemaVersion != planner.ExportManifestSchemaVersion {
		t.Fatalf("unexpected schemaVersion: %q", decoded.SchemaVersion)
	}
	if decoded.PlanHash != "abc123" {
		t.Fatalf("unexpected planHash: %q", decoded.PlanHash)
	}
	if decoded.Product.ID != "widget" {
		t.Fatalf("unexpected product.id: %q", decoded.Product.ID)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to decode raw manifest JSON: %v", err)
	}
	inputs, ok := raw["inputs"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected inputs object in manifest JSON, got %#v", raw["inputs"])
	}
	if _, exists := inputs["csv"]; exists {
		t.Fatalf("did not expect inputs.csv in manifest JSON: %+v", inputs)
	}
	if got := decoded.Values["width"]; got != 120.0 {
		t.Fatalf("unexpected values.width: %v", got)
	}
	if len(decoded.ParameterAssignments) != 1 || decoded.ParameterAssignments[0] != (planner.ExportManifestParameterAssignment{Name: "width", Value: 120.0, Type: "number", Unit: "mm"}) {
		t.Fatalf("unexpected parameterAssignments: %+v", decoded.ParameterAssignments)
	}
	if len(decoded.Outputs) != 1 || decoded.Outputs[0].Filename != "widget.step" {
		t.Fatalf("unexpected outputs: %+v", decoded.Outputs)
	}
	if _, exists := raw["bindings"]; exists {
		t.Fatalf("did not expect legacy bindings field in manifest JSON: %+v", raw)
	}
}

func TestWriteExportManifest_WritesMutationCollections(t *testing.T) {
	outputDir := t.TempDir()

	payload := planner.WriteExportManifestPayload{
		ProductKey:       "widget",
		ManifestFilename: planner.ExportManifestFilename,
		SchemaVersion:    planner.ExportManifestSchemaVersion,
		PlanHash:         "abc123",
		Product:          planner.ExportManifestProduct{ID: "widget"},
		Inputs:           planner.ExportManifestInputs{},
		Values:           map[string]interface{}{"width": 120.0},
		ParameterAssignments: []planner.ExportManifestParameterAssignment{
			{Name: "width", Value: 120.0, Type: "number", Unit: "mm"},
		},
		AssemblyMutations: &planner.ExportManifestMutationCollection{
			Parameters: []planner.ExportManifestParameterMutation{
				{Object: "Assembly", Property: "Length", ValueParam: "width", Type: "number", Unit: "mm"},
			},
			Properties: []planner.ExportManifestPropertyMutation{
				{Object: "Assembly", Property: "PartNumber", Value: "W-120"},
				{Object: "Assembly", Property: "Mass", Value: 12.5},
				{Object: "Assembly", Property: "Visible", Value: true},
			},
		},
		PartMutations: &planner.ExportManifestMutationCollection{
			Properties: []planner.ExportManifestPropertyMutation{
				{Object: "Pad", Property: "Label", Value: "PAD-120"},
				{Object: "Pad", Property: "Visible", Value: false},
			},
			Suppression: []planner.ExportManifestSuppressionMutation{
				{Object: "Pocket", Suppressed: true},
			},
		},
		Outputs: []planner.ExportManifestOutput{
			{Type: "step", Filename: "widget.step"},
		},
	}

	writeGenericManifest(t, outputDir, payload)

	data, err := os.ReadFile(filepath.Join(outputDir, planner.ExportManifestFilename))
	if err != nil {
		t.Fatalf("failed to read manifest: %v", err)
	}

	var decoded planner.WriteExportManifestPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to decode manifest JSON: %v", err)
	}

	if decoded.AssemblyMutations == nil {
		t.Fatal("expected assemblyMutations to be present")
	}
	if decoded.PartMutations == nil {
		t.Fatal("expected partMutations to be present")
	}
	if got := decoded.AssemblyMutations.Parameters[0]; got != (planner.ExportManifestParameterMutation{Object: "Assembly", Property: "Length", ValueParam: "width", Type: "number", Unit: "mm"}) {
		t.Fatalf("unexpected assembly parameter mutation: %+v", got)
	}
	wantAssemblyProperties := []planner.ExportManifestPropertyMutation{
		{Object: "Assembly", Property: "PartNumber", Value: "W-120"},
		{Object: "Assembly", Property: "Mass", Value: 12.5},
		{Object: "Assembly", Property: "Visible", Value: true},
	}
	if len(decoded.AssemblyMutations.Properties) != len(wantAssemblyProperties) {
		t.Fatalf("unexpected assembly property count: %+v", decoded.AssemblyMutations.Properties)
	}
	for i, want := range wantAssemblyProperties {
		if got := decoded.AssemblyMutations.Properties[i]; got != want {
			t.Fatalf("unexpected assembly property mutation at %d: %+v", i, got)
		}
	}
	wantPartProperties := []planner.ExportManifestPropertyMutation{
		{Object: "Pad", Property: "Label", Value: "PAD-120"},
		{Object: "Pad", Property: "Visible", Value: false},
	}
	if len(decoded.PartMutations.Properties) != len(wantPartProperties) {
		t.Fatalf("unexpected part property count: %+v", decoded.PartMutations.Properties)
	}
	for i, want := range wantPartProperties {
		if got := decoded.PartMutations.Properties[i]; got != want {
			t.Fatalf("unexpected part property mutation at %d: %+v", i, got)
		}
	}
	if got := decoded.PartMutations.Suppression[0]; got != (planner.ExportManifestSuppressionMutation{Object: "Pocket", Suppressed: true}) {
		t.Fatalf("unexpected part suppression mutation: %+v", got)
	}
}

func TestWriteExportManifest_PreservesOutputTypesWithoutImplicitCrossConversion(t *testing.T) {
	outputDir := t.TempDir()

	payload := planner.WriteExportManifestPayload{
		ProductKey:       "widget",
		ManifestFilename: planner.ExportManifestFilename,
		SchemaVersion:    planner.ExportManifestSchemaVersion,
		PlanHash:         "abc123",
		Product:          planner.ExportManifestProduct{ID: "widget"},
		Inputs:           planner.ExportManifestInputs{},
		Values:           map[string]interface{}{"width": 120.0},
		ParameterAssignments: []planner.ExportManifestParameterAssignment{
			{Name: "width", Value: 120.0, Type: "number", Unit: "mm"},
		},
		Outputs: []planner.ExportManifestOutput{
			{Type: "csv", Filename: "reports/bom.step"},
			{Type: "step", Filename: "exports/widget.csv", Object: "Body"},
		},
	}

	writeGenericManifest(t, outputDir, payload)

	manifestPath := filepath.Join(outputDir, planner.ExportManifestFilename)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("failed to read manifest: %v", err)
	}

	var decoded planner.WriteExportManifestPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to decode manifest JSON: %v", err)
	}
	if len(decoded.Outputs) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(decoded.Outputs))
	}
	if !reflect.DeepEqual(decoded.Outputs[0], planner.ExportManifestOutput{Type: "csv", Filename: "reports/bom.step"}) {
		t.Fatalf("unexpected first output: %+v", decoded.Outputs[0])
	}
	if !reflect.DeepEqual(decoded.Outputs[1], planner.ExportManifestOutput{Type: "step", Filename: "exports/widget.csv", Object: "Body"}) {
		t.Fatalf("unexpected second output: %+v", decoded.Outputs[1])
	}
}

func TestWriteExportManifest_RejectsInvalidParameterAssignments(t *testing.T) {
	payload := planner.WriteExportManifestPayload{
		ProductKey:       "widget",
		ManifestFilename: planner.ExportManifestFilename,
		SchemaVersion:    planner.ExportManifestSchemaVersion,
		PlanHash:         "abc123",
		Product:          planner.ExportManifestProduct{ID: "widget"},
		Inputs:           planner.ExportManifestInputs{},
		Values:           map[string]interface{}{"width": 120.0},
		ParameterAssignments: []planner.ExportManifestParameterAssignment{
			{Name: "width", Value: 120.0, Type: "integer", Unit: "mm"},
		},
		Outputs: []planner.ExportManifestOutput{
			{Type: "step", Filename: "widget.step"},
		},
	}

	err := ValidateGenericExportManifest(payload, false)
	if err == nil {
		t.Fatal("expected invalid parameterAssignments to fail")
	}
	if !strings.Contains(err.Error(), `parameterAssignments[0].type must be "number", got "integer"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteExportManifest_RejectsDuplicateParameterAssignmentNames(t *testing.T) {
	payload := planner.WriteExportManifestPayload{
		ProductKey:       "widget",
		ManifestFilename: planner.ExportManifestFilename,
		SchemaVersion:    planner.ExportManifestSchemaVersion,
		PlanHash:         "abc123",
		Product:          planner.ExportManifestProduct{ID: "widget"},
		Inputs:           planner.ExportManifestInputs{},
		Values:           map[string]interface{}{"width": 120.0},
		ParameterAssignments: []planner.ExportManifestParameterAssignment{
			{Name: "width", Value: 120.0, Type: "number", Unit: "mm"},
			{Name: " width ", Value: 121.0, Type: "number", Unit: "mm"},
		},
		Outputs: []planner.ExportManifestOutput{
			{Type: "step", Filename: "widget.step"},
		},
	}

	err := ValidateGenericExportManifest(payload, false)
	if err == nil {
		t.Fatal("expected duplicate parameterAssignments to fail")
	}
	if !strings.Contains(err.Error(), `parameterAssignments[1].name "width" duplicates parameterAssignments[0].name`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteExportManifest_RejectsUnsupportedLegacyOutputType(t *testing.T) {
	payload := planner.WriteExportManifestPayload{
		ProductKey:       "widget",
		ManifestFilename: planner.ExportManifestFilename,
		SchemaVersion:    planner.ExportManifestSchemaVersion,
		PlanHash:         "abc123",
		Product:          planner.ExportManifestProduct{ID: "widget"},
		Outputs: []planner.ExportManifestOutput{
			{Type: "stl", Filename: "widget.stl"},
		},
	}

	err := ValidateGenericExportManifest(payload, false)
	if err == nil {
		t.Fatal("expected unsupported output type to fail")
	}
	if !strings.Contains(err.Error(), `manifest outputs[0].type "stl" is not supported`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteWriteCSV_PreservesBytes(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "nested")
	step := planner.Step{Type: planner.StepWriteCSV, Payload: planner.WriteCSVPayload{
		Filename: "values.csv",
		Headers:  []string{"z", "quoted,header", "a", "empty", "nil"},
		Values:   []interface{}{12.5, "a,\"b\"\nline", true, "", nil},
	}}
	if err := ExecuteWriteCSV(outputDir, step); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(outputDir, "values.csv"))
	if err != nil {
		t.Fatal(err)
	}
	want := "z,\"quoted,header\",a,empty,nil\n12.5,\"a,\"\"b\"\"\nline\",true,,<nil>\n"
	if string(got) != want {
		t.Fatalf("CSV bytes = %q, want %q", got, want)
	}
}

func TestWriteExportManifestFile_ReplacesExactBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	writeFile(t, path, "old content")
	want := []byte("{\n  \"value\": 1\n}\n")
	if err := WriteExportManifestFile(dir, path, want, ""); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("manifest bytes = %q, want %q", got, want)
	}
	if files := manifestTempFiles(t, dir); len(files) != 0 {
		t.Fatalf("temporary files remain: %v", files)
	}
}

func TestWriteExportManifestFile_FailureCleanupAndChaining(t *testing.T) {
	for _, prefix := range []string{"", "caller manifest"} {
		t.Run(prefix, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "manifest.json")
			if err := os.Mkdir(path, 0755); err != nil {
				t.Fatal(err)
			}
			err := WriteExportManifestFile(dir, path, []byte("new content"), prefix)
			var cause *os.LinkError
			if !errors.As(err, &cause) {
				t.Fatalf("filesystem cause lost: %v", err)
			}
			want := "failed to finalize export manifest file: " + cause.Error()
			if prefix != "" {
				want = prefix + ": failed to finalize file: " + cause.Error()
			}
			if err.Error() != want {
				t.Fatalf("error = %q, want %q", err, want)
			}
			if info, err := os.Stat(path); err != nil || !info.IsDir() {
				t.Fatalf("destination changed: %v", err)
			}
			if files := manifestTempFiles(t, dir); len(files) != 0 {
				t.Fatalf("temporary files remain: %v", files)
			}

			err = WriteExportManifestFile(filepath.Join(dir, "missing"), path, nil, prefix)
			var pathErr *os.PathError
			if !errors.As(err, &pathErr) {
				t.Fatalf("filesystem cause lost: %v", err)
			}
			want = "failed to create temp export manifest file: " + pathErr.Error()
			if prefix != "" {
				want = prefix + ": failed to create temp file: " + pathErr.Error()
			}
			if err.Error() != want {
				t.Fatalf("error = %q, want %q", err, want)
			}
		})
	}
}
