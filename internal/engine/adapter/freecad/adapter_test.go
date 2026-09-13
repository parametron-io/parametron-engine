package freecad

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/adapter"
)

func TestNewFreeCADAdapter_InitializesRuntimeCapability(t *testing.T) {
	adp := NewFreeCADAdapter(t.TempDir())
	if adp.runtimeCapability == nil {
		t.Fatal("expected NewFreeCADAdapter to initialize runtime capability")
	}
}

func TestWriteExportManifest_FreeCADStepRequiresObjectAndCSVPDFFobidObject(t *testing.T) {
	outputDir := t.TempDir()
	adp := NewFreeCADAdapter(outputDir)

	tests := []struct {
		name    string
		outputs []planner.ExportManifestOutput
		want    string
	}{
		{
			name: "step missing object",
			outputs: []planner.ExportManifestOutput{
				{Type: "step", Filename: "widget.step"},
			},
			want: "manifest outputs[0].object must be a non-empty string for step outputs",
		},
		{
			name: "csv forbids object",
			outputs: []planner.ExportManifestOutput{
				{Type: "csv", Filename: "widget.csv", Object: "Body"},
			},
			want: "manifest outputs[0] contains unsupported fields for csv outputs: object",
		},
		{
			name: "pdf forbids object",
			outputs: []planner.ExportManifestOutput{
				{Type: "pdf", Filename: "widget.pdf", Object: "Page001"},
			},
			want: "manifest outputs[0] contains unsupported fields for pdf outputs: object",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			step := planner.Step{
				Type: planner.StepWriteExportManifest,
				Payload: planner.WriteExportManifestPayload{
					ProductKey:       "widget",
					ManifestFilename: planner.ExportManifestFilename,
					SchemaVersion:    planner.ExportManifestSchemaVersion,
					PlanHash:         "abc123",
					Adapter:          "freecad",
					Product:          planner.ExportManifestProduct{ID: "widget"},
					Outputs:          tc.outputs,
				},
			}

			err := adp.Run(context.Background(), step)
			if err == nil {
				t.Fatalf("expected %s to fail", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestWriteExportManifest_FreeCADCSVSupportsAndValidatesBOMFields(t *testing.T) {
	outputDir := t.TempDir()
	adp := NewFreeCADAdapter(outputDir)

	t.Run("preserves bom fields", func(t *testing.T) {
		step := planner.Step{
			Type: planner.StepWriteExportManifest,
			Payload: planner.WriteExportManifestPayload{
				ProductKey:       "widget",
				ManifestFilename: planner.ExportManifestFilename,
				SchemaVersion:    planner.ExportManifestSchemaVersion,
				PlanHash:         "abc123",
				Adapter:          "freecad",
				Product:          planner.ExportManifestProduct{ID: "widget"},
				Inputs:           planner.ExportManifestInputs{SourceModel: filepath.Join(outputDir, "input", "box.FCStd")},
				Outputs: []planner.ExportManifestOutput{
					{
						Type:     "csv",
						Filename: "reports/bom.csv",
						Target:   "AssemblyA",
						Rollup:   "assembly",
						Columns:  []string{"assembly_path", "name", "quantity"},
					},
				},
			},
		}

		if err := adp.Run(context.Background(), step); err != nil {
			t.Fatalf("Run returned error: %v", err)
		}

		manifestPath := filepath.Join(outputDir, planner.ExportManifestFilename)
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatalf("failed to read manifest: %v", err)
		}

		var payload planner.WriteExportManifestPayload
		if err := json.Unmarshal(data, &payload); err != nil {
			t.Fatalf("failed to decode manifest JSON: %v", err)
		}

		want := planner.ExportManifestOutput{
			Type:     "csv",
			Filename: "reports/bom.csv",
			Target:   "AssemblyA",
			Rollup:   "assembly",
			Columns:  []string{"assembly_path", "name", "quantity"},
		}
		if len(payload.Outputs) != 1 || !reflect.DeepEqual(payload.Outputs[0], want) {
			t.Fatalf("unexpected outputs: %+v", payload.Outputs)
		}
		raw := string(data)
		for _, genericField := range []string{`"inputs":`, `"sourceModel":`, `"filename":`} {
			if !strings.Contains(raw, genericField) {
				t.Fatalf("expected transitional manifest to contain generic field %s", genericField)
			}
		}
		for _, nativeField := range []string{`"sourceDocument":`, `"format":`, `"path":`} {
			if strings.Contains(raw, nativeField) {
				t.Fatalf("transitional manifest contains FreeCAD-native field %s", nativeField)
			}
		}
	})

	tests := []struct {
		name   string
		output planner.ExportManifestOutput
		want   string
	}{
		{
			name: "invalid rollup",
			output: planner.ExportManifestOutput{
				Type:     "csv",
				Filename: "reports/bom.csv",
				Rollup:   "nested",
			},
			want: `manifest outputs[0].rollup "nested" is not supported for csv outputs`,
		},
		{
			name: "empty target",
			output: planner.ExportManifestOutput{
				Type:     "csv",
				Filename: "reports/bom.csv",
				Target:   "   ",
			},
			want: "manifest outputs[0].target must be a non-empty string for csv outputs",
		},
		{
			name: "unknown column",
			output: planner.ExportManifestOutput{
				Type:     "csv",
				Filename: "reports/bom.csv",
				Columns:  []string{"name", "part_number"},
			},
			want: `manifest outputs[0].columns[1] "part_number" is not supported for csv outputs`,
		},
		{
			name: "duplicate column",
			output: planner.ExportManifestOutput{
				Type:     "csv",
				Filename: "reports/bom.csv",
				Columns:  []string{"name", "name"},
			},
			want: `manifest outputs[0].columns[1] "name" duplicates columns[0] for csv outputs`,
		},
		{
			name: "empty columns",
			output: planner.ExportManifestOutput{
				Type:     "csv",
				Filename: "reports/bom.csv",
				Columns:  []string{},
			},
			want: "manifest outputs[0].columns must contain at least one item for csv outputs",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			step := planner.Step{
				Type: planner.StepWriteExportManifest,
				Payload: planner.WriteExportManifestPayload{
					ProductKey:       "widget",
					ManifestFilename: planner.ExportManifestFilename,
					SchemaVersion:    planner.ExportManifestSchemaVersion,
					PlanHash:         "abc123",
					Adapter:          "freecad",
					Product:          planner.ExportManifestProduct{ID: "widget"},
					Outputs:          []planner.ExportManifestOutput{tc.output},
				},
			}

			err := adp.Run(context.Background(), step)
			if err == nil {
				t.Fatalf("expected %s to fail", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestWriteExportManifest_FreeCAD_EmitsFreeCADNativeShape(t *testing.T) {
	outputDir := t.TempDir()
	adp := NewFreeCADAdapter(outputDir)

	step := planner.Step{
		Type: planner.StepWriteExportManifest,
		Payload: planner.WriteExportManifestPayload{
			ProductKey:             "widget",
			ManifestFilename:       planner.ExportManifestFilename,
			ManifestProjectionMode: planner.ExportManifestProjectionModeFreeCADRuntimeNative,
			SchemaVersion:          planner.ExportManifestSchemaVersion,
			PlanHash:               "abc123",
			Adapter:                "freecad",
			Product:                planner.ExportManifestProduct{ID: "widget"},
			SourceDocument:         "input/box.FCStd",
			ParameterAssignments: []planner.ExportManifestParameterAssignment{
				{Name: "width", Value: 120.0, Type: "number", Unit: "mm"},
			},
			AssemblyMutations: &planner.ExportManifestMutationCollection{
				Parameters: []planner.ExportManifestParameterMutation{
					{Object: "Box", Property: "Width", ValueParam: "width"},
				},
			},
			Outputs: []planner.ExportManifestOutput{
				{Type: "step", Filename: "outputs/widget.step", Object: "Body"},
			},
		},
	}

	if err := adp.Run(context.Background(), step); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	manifestPath := filepath.Join(outputDir, planner.ExportManifestFilename)
	if !strings.HasSuffix(manifestPath, "export_manifest_v1.json") {
		t.Fatalf("expected manifest filename export_manifest_v1.json, got %q", filepath.Base(manifestPath))
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("failed to read manifest: %v", err)
	}

	var manifest FreeCADRuntimeExportManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("failed to decode FreeCAD manifest: %v", err)
	}

	if manifest.SchemaVersion == "" {
		t.Fatal("expected non-empty schemaVersion")
	}
	if manifest.SourceDocument == "" {
		t.Fatal("expected non-empty sourceDocument")
	}
	if len(manifest.ParameterAssignments) != 1 {
		t.Fatalf("expected 1 parameterAssignment, got %d", len(manifest.ParameterAssignments))
	}
	pa := manifest.ParameterAssignments[0]
	if pa.Target == "" {
		t.Fatal("parameterAssignments[0].target must not be empty")
	}
	if pa.Value == nil {
		t.Fatal("parameterAssignments[0].value must not be nil")
	}
	if pa.ValueKind == "" {
		t.Fatal("parameterAssignments[0].valueKind must not be empty")
	}
	if len(manifest.Outputs) != 1 {
		t.Fatalf("expected 1 output, got %d", len(manifest.Outputs))
	}
	out := manifest.Outputs[0]
	if out.ID == "" {
		t.Fatal("outputs[0].id must not be empty")
	}
	if out.Format == "" {
		t.Fatal("outputs[0].format must not be empty")
	}
	if out.Path == "" {
		t.Fatal("outputs[0].path must not be empty")
	}

	raw := string(data)
	for _, legacy := range []string{`"inputs":`, `"sourceModel":`, `"name":`, `"type":`, `"unit":`, `"filename":`} {
		if strings.Contains(raw, legacy) {
			t.Fatalf("emitted FreeCAD manifest contains legacy field %s", legacy)
		}
	}
}

func TestWriteExportManifest_GenericAdapter_PreservesLegacyShape(t *testing.T) {
	outputDir := t.TempDir()
	adp := NewFreeCADAdapter(outputDir)

	step := planner.Step{
		Type: planner.StepWriteExportManifest,
		Payload: planner.WriteExportManifestPayload{
			ProductKey:       "widget",
			ManifestFilename: planner.ExportManifestFilename,
			SchemaVersion:    planner.ExportManifestSchemaVersion,
			PlanHash:         "abc123",
			// No Adapter field — generic path must emit the Engine payload shape.
			Product: planner.ExportManifestProduct{ID: "widget"},
			Inputs:  planner.ExportManifestInputs{},
			ParameterAssignments: []planner.ExportManifestParameterAssignment{
				{Name: "width", Value: 120.0, Type: "number", Unit: "mm"},
			},
			Outputs: []planner.ExportManifestOutput{
				{Type: "step", Filename: "widget.step"},
			},
		},
	}

	if err := adp.Run(context.Background(), step); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, planner.ExportManifestFilename))
	if err != nil {
		t.Fatalf("failed to read manifest: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to decode manifest JSON: %v", err)
	}

	if _, hasInputs := raw["inputs"]; !hasInputs {
		t.Fatal("expected generic manifest to have root \"inputs\" object (legacy Engine shape)")
	}
	if _, hasSourceDoc := raw["sourceDocument"]; hasSourceDoc {
		t.Fatal("did not expect \"sourceDocument\" in generic manifest (FreeCAD-native field)")
	}

	assignments, ok := raw["parameterAssignments"].([]interface{})
	if !ok || len(assignments) == 0 {
		t.Fatal("expected non-empty parameterAssignments in generic manifest")
	}
	first, ok := assignments[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected parameterAssignments[0] to be an object")
	}
	if _, hasName := first["name"]; !hasName {
		t.Fatal("expected parameterAssignments[0].name in generic manifest (legacy Engine field)")
	}
}

func TestWriteExportManifest_FreeCAD_ProjectionFailureNoPartialFile(t *testing.T) {
	outputDir := t.TempDir()
	adp := NewFreeCADAdapter(outputDir)

	step := planner.Step{
		Type: planner.StepWriteExportManifest,
		Payload: planner.WriteExportManifestPayload{
			ProductKey:             "widget",
			ManifestFilename:       planner.ExportManifestFilename,
			ManifestProjectionMode: planner.ExportManifestProjectionModeFreeCADRuntimeNative,
			SchemaVersion:          planner.ExportManifestSchemaVersion,
			PlanHash:               "abc123",
			Adapter:                "freecad",
			Product:                planner.ExportManifestProduct{ID: "widget"},
			// SourceDocument deliberately omitted to trigger projection failure.
			Outputs: []planner.ExportManifestOutput{
				{Type: "step", Filename: "outputs/widget.step", Object: "Body"},
			},
		},
	}

	err := adp.Run(context.Background(), step)
	if err == nil {
		t.Fatal("expected projection failure, got nil error")
	}
	if !strings.Contains(err.Error(), "project FreeCAD runtime export manifest") {
		t.Fatalf("expected error to contain %q, got: %v", "project FreeCAD runtime export manifest", err)
	}
	if !strings.Contains(err.Error(), "sourceDocument") {
		t.Fatalf("expected error to mention %q, got: %v", "sourceDocument", err)
	}

	manifestPath := filepath.Join(outputDir, planner.ExportManifestFilename)
	if _, statErr := os.Stat(manifestPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected manifest file to not exist after projection failure, but os.Stat returned: %v", statErr)
	}
}

func TestWriteExportManifest_FreeCAD_RejectsNameOnlyParameterAssignment(t *testing.T) {
	outputDir := t.TempDir()
	adp := NewFreeCADAdapter(outputDir)

	step := planner.Step{
		Type: planner.StepWriteExportManifest,
		Payload: planner.WriteExportManifestPayload{
			ProductKey:       "widget",
			ManifestFilename: planner.ExportManifestFilename,
			SchemaVersion:    planner.ExportManifestSchemaVersion,
			PlanHash:         "abc123",
			Adapter:          "freecad",
			Product:          planner.ExportManifestProduct{ID: "widget"},
			Inputs:           planner.ExportManifestInputs{},
			Values:           map[string]interface{}{"width": 120.0},
			ParameterAssignments: []planner.ExportManifestParameterAssignment{
				{Name: "width", Value: 120.0, Type: "number", Unit: "mm"},
			},
			Outputs: []planner.ExportManifestOutput{
				{Type: "step", Filename: "widget.step", Object: "Body"},
			},
		},
	}

	err := adp.Run(context.Background(), step)
	if err == nil {
		t.Fatal("expected FreeCAD name-only parameter assignment to be rejected")
	}
	if !strings.Contains(err.Error(), `"width"`) {
		t.Fatalf("expected rejection error to identify parameter name 'width', got: %v", err)
	}
	if !strings.Contains(err.Error(), "name-only parameter assignments are not supported") {
		t.Fatalf("expected rejection error to state name-only is not supported, got: %v", err)
	}

	manifestPath := filepath.Join(outputDir, planner.ExportManifestFilename)
	if _, statErr := os.Stat(manifestPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected manifest file to not exist after rejection, but os.Stat returned: %v", statErr)
	}
}

func TestWriteExportManifest_GenericAdapter_AcceptsNameOnlyParameterAssignment(t *testing.T) {
	outputDir := t.TempDir()
	adp := NewFreeCADAdapter(outputDir)

	step := planner.Step{
		Type: planner.StepWriteExportManifest,
		Payload: planner.WriteExportManifestPayload{
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
		},
	}

	if err := adp.Run(context.Background(), step); err != nil {
		t.Fatalf("expected generic adapter to accept name-only parameter assignment, got error: %v", err)
	}

	manifestPath := filepath.Join(outputDir, planner.ExportManifestFilename)
	if _, statErr := os.Stat(manifestPath); statErr != nil {
		t.Fatalf("expected manifest file to exist for generic adapter, got: %v", statErr)
	}
}

func TestWriteExportManifest_FreeCAD_MissingCADTargetMapping_RejectsBeforeWrite(t *testing.T) {
	outputDir := t.TempDir()
	adp := NewFreeCADAdapter(outputDir)

	step := planner.Step{
		Type: planner.StepWriteExportManifest,
		Payload: planner.WriteExportManifestPayload{
			ProductKey:       "widget",
			ManifestFilename: planner.ExportManifestFilename,
			SchemaVersion:    planner.ExportManifestSchemaVersion,
			PlanHash:         "abc123",
			Adapter:          "freecad",
			Product:          planner.ExportManifestProduct{ID: "widget"},
			Inputs:           planner.ExportManifestInputs{},
			Values:           map[string]interface{}{"length": 35.0},
			ParameterAssignments: []planner.ExportManifestParameterAssignment{
				{Name: "length", Value: 35.0, Type: "number", Unit: "mm"},
			},
			// No AssemblyMutations or PartMutations — zero matching mutations.
			Outputs: []planner.ExportManifestOutput{
				{Type: "step", Filename: "widget.step", Object: "Body"},
			},
		},
	}

	err := adp.Run(context.Background(), step)
	if err == nil {
		t.Fatal("expected missing CAD target mapping to fail, got nil")
	}
	if !strings.Contains(err.Error(), "parameterAssignments[0]") {
		t.Fatalf("expected error to contain 'parameterAssignments[0]', got: %v", err)
	}
	if !strings.Contains(err.Error(), `"length"`) {
		t.Fatalf("expected error to identify parameter name 'length', got: %v", err)
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected error to indicate missing CAD target mapping, got: %v", err)
	}
	if strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected error not to incorrectly report ambiguity, got: %v", err)
	}

	manifestPath := filepath.Join(outputDir, planner.ExportManifestFilename)
	if _, statErr := os.Stat(manifestPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected manifest file to not exist after missing-target rejection, got: %v", statErr)
	}
}

func TestWriteExportManifest_FreeCAD_AmbiguousCADTargetMappings_RejectsBeforeWrite(t *testing.T) {
	outputDir := t.TempDir()
	adp := NewFreeCADAdapter(outputDir)

	step := planner.Step{
		Type: planner.StepWriteExportManifest,
		Payload: planner.WriteExportManifestPayload{
			ProductKey:       "widget",
			ManifestFilename: planner.ExportManifestFilename,
			SchemaVersion:    planner.ExportManifestSchemaVersion,
			PlanHash:         "abc123",
			Adapter:          "freecad",
			Product:          planner.ExportManifestProduct{ID: "widget"},
			Inputs:           planner.ExportManifestInputs{},
			Values:           map[string]interface{}{"length": 35.0},
			ParameterAssignments: []planner.ExportManifestParameterAssignment{
				{Name: "length", Value: 35.0, Type: "number", Unit: "mm"},
			},
			// Two mutations both matching "length" — ambiguous.
			AssemblyMutations: &planner.ExportManifestMutationCollection{
				Parameters: []planner.ExportManifestParameterMutation{
					{Object: "Body", Property: "Length", ValueParam: "length"},
				},
			},
			PartMutations: &planner.ExportManifestMutationCollection{
				Parameters: []planner.ExportManifestParameterMutation{
					{Object: "Part", Property: "Length", ValueParam: "length"},
				},
			},
			Outputs: []planner.ExportManifestOutput{
				{Type: "step", Filename: "widget.step", Object: "Body"},
			},
		},
	}

	err := adp.Run(context.Background(), step)
	if err == nil {
		t.Fatal("expected ambiguous CAD target mappings to fail, got nil")
	}
	if !strings.Contains(err.Error(), "parameterAssignments[0]") {
		t.Fatalf("expected error to contain 'parameterAssignments[0]', got: %v", err)
	}
	if !strings.Contains(err.Error(), `"length"`) {
		t.Fatalf("expected error to identify parameter name 'length', got: %v", err)
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected error to indicate ambiguous CAD target mappings, got: %v", err)
	}

	manifestPath := filepath.Join(outputDir, planner.ExportManifestFilename)
	if _, statErr := os.Stat(manifestPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected manifest file to not exist after ambiguous-target rejection, got: %v", statErr)
	}
}

func TestWriteExportManifest_FreeCAD_ExplicitTarget_AcceptedWithoutMutationFallback(t *testing.T) {
	outputDir := t.TempDir()
	adp := NewFreeCADAdapter(outputDir)

	step := planner.Step{
		Type: planner.StepWriteExportManifest,
		Payload: planner.WriteExportManifestPayload{
			ProductKey:       "widget",
			ManifestFilename: planner.ExportManifestFilename,
			SchemaVersion:    planner.ExportManifestSchemaVersion,
			PlanHash:         "abc123",
			Adapter:          "freecad",
			Product:          planner.ExportManifestProduct{ID: "widget"},
			Inputs:           planner.ExportManifestInputs{},
			Values:           map[string]interface{}{"length": 35.0},
			ParameterAssignments: []planner.ExportManifestParameterAssignment{
				{Name: "length", Target: "Body.Length", Value: 35.0, Type: "number", Unit: "mm"},
			},
			// No mutations — explicit Target must bypass mutation requirement.
			Outputs: []planner.ExportManifestOutput{
				{Type: "step", Filename: "widget.step", Object: "Body"},
			},
		},
	}

	if err := adp.Run(context.Background(), step); err != nil {
		t.Fatalf("expected explicit target to be accepted without mutations, got error: %v", err)
	}

	manifestPath := filepath.Join(outputDir, planner.ExportManifestFilename)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("expected manifest file to be written, got: %v", err)
	}
	if !strings.Contains(string(data), `"target": "Body.Length"`) {
		t.Fatalf("expected written manifest to preserve explicit target 'Body.Length', got: %s", string(data))
	}
}

func TestWriteExportManifest_FreeCAD_NativeSourceDocument_UnsafeValues_NoPartialFile(t *testing.T) {
	cases := []struct {
		name           string
		sourceDocument string
	}{
		{"parent traversal", "../model.FCStd"},
		{"leading backslash", `\model.FCStd`},
		{"UNC path", `\\server\share\model.FCStd`},
		{"absolute unix", "/model.FCStd"},
		{"windows drive", `C:\model.FCStd`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outputDir := t.TempDir()
			adp := NewFreeCADAdapter(outputDir)

			step := planner.Step{
				Type: planner.StepWriteExportManifest,
				Payload: planner.WriteExportManifestPayload{
					ProductKey:             "widget",
					ManifestFilename:       planner.ExportManifestFilename,
					ManifestProjectionMode: planner.ExportManifestProjectionModeFreeCADRuntimeNative,
					SchemaVersion:          planner.ExportManifestSchemaVersion,
					PlanHash:               "abc123",
					Adapter:                "freecad",
					Product:                planner.ExportManifestProduct{ID: "widget"},
					SourceDocument:         tc.sourceDocument,
					AssemblyMutations: &planner.ExportManifestMutationCollection{
						Parameters: []planner.ExportManifestParameterMutation{
							{Object: "Box", Property: "Width", ValueParam: "width"},
						},
					},
					ParameterAssignments: []planner.ExportManifestParameterAssignment{
						{Name: "width", Value: 50.0, Type: "number", Unit: "mm"},
					},
					Outputs: []planner.ExportManifestOutput{
						{Type: "step", Filename: "outputs/widget.step", Object: "Body"},
					},
				},
			}

			err := adp.Run(context.Background(), step)
			if err == nil {
				t.Fatalf("expected error for unsafe sourceDocument %q, got nil", tc.sourceDocument)
			}
			if !strings.Contains(err.Error(), "sourceDocument") {
				t.Errorf("error %q does not identify sourceDocument", err.Error())
			}
			if !strings.Contains(err.Error(), "project FreeCAD runtime export manifest") {
				t.Errorf("error %q does not contain projection prefix", err.Error())
			}

			manifestPath := filepath.Join(outputDir, planner.ExportManifestFilename)
			if _, statErr := os.Stat(manifestPath); !os.IsNotExist(statErr) {
				t.Fatalf("expected no manifest file after sourceDocument rejection, but os.Stat returned: %v", statErr)
			}
		})
	}
}

func TestWriteExportManifest_FreeCAD_ExactlyOneMutationFallback_Accepted(t *testing.T) {
	outputDir := t.TempDir()
	adp := NewFreeCADAdapter(outputDir)

	step := planner.Step{
		Type: planner.StepWriteExportManifest,
		Payload: planner.WriteExportManifestPayload{
			ProductKey:       "widget",
			ManifestFilename: planner.ExportManifestFilename,
			SchemaVersion:    planner.ExportManifestSchemaVersion,
			PlanHash:         "abc123",
			Adapter:          "freecad",
			Product:          planner.ExportManifestProduct{ID: "widget"},
			Inputs:           planner.ExportManifestInputs{},
			Values:           map[string]interface{}{"length": 35.0},
			ParameterAssignments: []planner.ExportManifestParameterAssignment{
				{Name: "length", Value: 35.0, Type: "number", Unit: "mm"},
			},
			// Exactly one matching mutation — deterministic fallback must be accepted.
			AssemblyMutations: &planner.ExportManifestMutationCollection{
				Parameters: []planner.ExportManifestParameterMutation{
					{Object: "Body", Property: "Length", ValueParam: "length"},
				},
			},
			Outputs: []planner.ExportManifestOutput{
				{Type: "step", Filename: "widget.step", Object: "Body"},
			},
		},
	}

	if err := adp.Run(context.Background(), step); err != nil {
		t.Fatalf("expected exactly-one mutation fallback to be accepted, got error: %v", err)
	}

	manifestPath := filepath.Join(outputDir, planner.ExportManifestFilename)
	if _, statErr := os.Stat(manifestPath); statErr != nil {
		t.Fatalf("expected manifest file to be written for exactly-one mutation fallback, got: %v", statErr)
	}
}

func TestWriteExportManifest_FreeCAD_NativeProjection_UnsupportedExplicitTarget_NoPartialFile(t *testing.T) {
	cases := []struct {
		name        string
		target      string
		errContains string
	}{
		{"space before dot", "Body .Width", "whitespace"},
		{"space after dot", "Body. Width", "whitespace"},
		{"slash in object segment", "Body/Part.Width", "malformed"},
		{"backslash in property segment", "Body.Width\\Length", "malformed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outputDir := t.TempDir()
			adp := NewFreeCADAdapter(outputDir)

			step := planner.Step{
				Type: planner.StepWriteExportManifest,
				Payload: planner.WriteExportManifestPayload{
					ProductKey:             "widget",
					ManifestFilename:       planner.ExportManifestFilename,
					ManifestProjectionMode: planner.ExportManifestProjectionModeFreeCADRuntimeNative,
					SchemaVersion:          planner.ExportManifestSchemaVersion,
					PlanHash:               "abc123",
					Adapter:                "freecad",
					Product:                planner.ExportManifestProduct{ID: "widget"},
					SourceDocument:         "input/box.FCStd",
					ParameterAssignments: []planner.ExportManifestParameterAssignment{
						{Name: "width", Target: tc.target, Value: 50.0, Type: "number", Unit: "mm"},
					},
					Outputs: []planner.ExportManifestOutput{
						{Type: "step", Filename: "outputs/widget.step", Object: "Body"},
					},
				},
			}

			err := adp.Run(context.Background(), step)
			if err == nil {
				t.Fatal("expected error for unsupported target syntax, got nil")
			}
			if !strings.Contains(err.Error(), "target") {
				t.Errorf("error %q does not mention target context", err.Error())
			}
			if !strings.Contains(err.Error(), tc.errContains) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.errContains)
			}

			manifestPath := filepath.Join(outputDir, planner.ExportManifestFilename)
			if _, statErr := os.Stat(manifestPath); !os.IsNotExist(statErr) {
				t.Fatalf("expected no manifest file after unsupported target rejection, but os.Stat returned: %v", statErr)
			}
		})
	}
}

func TestWriteExportManifest_FreeCAD_NativeUnsupportedOutputDeclarations_NoPartialFile(t *testing.T) {
	tests := []struct {
		name        string
		outputs     []planner.ExportManifestOutput
		errContains string
	}{
		{
			name: "unsupported format",
			outputs: []planner.ExportManifestOutput{
				{Type: "stl", Filename: "outputs/widget.stl", Object: "Body"},
			},
			errContains: "manifest outputs[0].type",
		},
		{
			name: "blank format",
			outputs: []planner.ExportManifestOutput{
				{Type: "   ", Filename: "outputs/widget.invalid", Object: "Body"},
			},
			errContains: "manifest outputs[0].type",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			outputDir := t.TempDir()
			adp := NewFreeCADAdapter(outputDir)

			step := planner.Step{
				Type: planner.StepWriteExportManifest,
				Payload: planner.WriteExportManifestPayload{
					ProductKey:             "widget",
					ManifestFilename:       planner.ExportManifestFilename,
					ManifestProjectionMode: planner.ExportManifestProjectionModeFreeCADRuntimeNative,
					SchemaVersion:          planner.ExportManifestSchemaVersion,
					PlanHash:               "abc123",
					Adapter:                "freecad",
					Product:                planner.ExportManifestProduct{ID: "widget"},
					SourceDocument:         "input/box.FCStd",
					Outputs:                tc.outputs,
				},
			}

			err := adp.Run(context.Background(), step)
			if err == nil {
				t.Fatal("expected unsupported native output declaration to fail")
			}
			if !strings.Contains(err.Error(), tc.errContains) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.errContains)
			}

			manifestPath := filepath.Join(outputDir, planner.ExportManifestFilename)
			if _, statErr := os.Stat(manifestPath); !os.IsNotExist(statErr) {
				t.Fatalf("expected no final manifest file, but os.Stat returned: %v", statErr)
			}

			entries, readErr := os.ReadDir(outputDir)
			if readErr != nil {
				t.Fatalf("read output directory: %v", readErr)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".export-manifest-") {
					t.Fatalf("unexpected partial manifest file %q", entry.Name())
				}
			}
		})
	}
}

// TestWriteExportManifest_FreeCAD_NativeEmptyOutputsWritesEmptyArrayManifest
// proves the native-only counterpart to
// TestWriteExportManifest_FreeCAD_NativeUnsupportedOutputDeclarations_NoPartialFile:
// a nil/empty Outputs slice on a freecad-runtime-native manifest payload is
// no longer an adapter-layer error (that guarantee moved to the planner,
// see TestNormalizeFreeCADRuntimeOutputsForRequest_OnlyExplicitNoneAllowsEmpty
// in the planner package). At this layer the manifest must be written
// successfully with "outputs":[] -- never null, never omitted, never a
// synthetic "none" entry -- and sourceDocument/parameterAssignments/
// schemaVersion must still be preserved.
func TestWriteExportManifest_FreeCAD_NativeEmptyOutputsWritesEmptyArrayManifest(t *testing.T) {
	outputDir := t.TempDir()
	adp := NewFreeCADAdapter(outputDir)

	step := planner.Step{
		Type: planner.StepWriteExportManifest,
		Payload: planner.WriteExportManifestPayload{
			ProductKey:             "widget",
			ManifestFilename:       planner.ExportManifestFilename,
			ManifestProjectionMode: planner.ExportManifestProjectionModeFreeCADRuntimeNative,
			SchemaVersion:          planner.ExportManifestSchemaVersion,
			PlanHash:               "abc123",
			Adapter:                "freecad",
			Product:                planner.ExportManifestProduct{ID: "widget"},
			SourceDocument:         "input/box.FCStd",
			ParameterAssignments: []planner.ExportManifestParameterAssignment{
				{Name: "width", Value: 120.0, Type: "number", Unit: "mm", Target: "Box.Width"},
			},
			Outputs: nil,
		},
	}

	if err := adp.Run(context.Background(), step); err != nil {
		t.Fatalf("Run returned error for native-only empty outputs: %v", err)
	}

	manifestPath := filepath.Join(outputDir, planner.ExportManifestFilename)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("failed to read manifest: %v", err)
	}

	if !strings.Contains(string(data), `"outputs": []`) {
		t.Fatalf("expected manifest JSON to serialize outputs as an empty array, got: %s", data)
	}

	var manifest FreeCADRuntimeExportManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("failed to decode FreeCAD manifest: %v", err)
	}
	if manifest.SchemaVersion == "" {
		t.Fatal("expected non-empty schemaVersion")
	}
	if manifest.SourceDocument == "" {
		t.Fatal("expected non-empty sourceDocument")
	}
	if manifest.Outputs == nil {
		t.Fatal("expected outputs to decode as a non-nil empty slice")
	}
	if len(manifest.Outputs) != 0 {
		t.Fatalf("expected zero outputs, got %+v", manifest.Outputs)
	}
	if len(manifest.ParameterAssignments) != 1 {
		t.Fatalf("expected parameter assignment to survive native-only manifest, got %d", len(manifest.ParameterAssignments))
	}
}

func TestWriteExportManifest_FreeCAD_NativeOutputPathContainment_NoPartialFile(t *testing.T) {
	cases := []struct {
		name        string
		outputPath  string
		errContains string
	}{
		{"root level path", "box.step", "must be contained under"},
		{"outputs parent traversal", "outputs/../box.step", "must not contain parent traversal"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outputDir := t.TempDir()
			adp := NewFreeCADAdapter(outputDir)

			step := planner.Step{
				Type: planner.StepWriteExportManifest,
				Payload: planner.WriteExportManifestPayload{
					ProductKey:             "widget",
					ManifestFilename:       planner.ExportManifestFilename,
					ManifestProjectionMode: planner.ExportManifestProjectionModeFreeCADRuntimeNative,
					SchemaVersion:          planner.ExportManifestSchemaVersion,
					PlanHash:               "abc123",
					Adapter:                "freecad",
					Product:                planner.ExportManifestProduct{ID: "widget"},
					SourceDocument:         "input/box.FCStd",
					AssemblyMutations: &planner.ExportManifestMutationCollection{
						Parameters: []planner.ExportManifestParameterMutation{
							{Object: "Box", Property: "Width", ValueParam: "width"},
						},
					},
					ParameterAssignments: []planner.ExportManifestParameterAssignment{
						{Name: "width", Value: 50.0, Type: "number", Unit: "mm"},
					},
					Outputs: []planner.ExportManifestOutput{
						{Type: "step", Filename: tc.outputPath, Object: "Body"},
					},
				},
			}

			err := adp.Run(context.Background(), step)
			if err == nil {
				t.Fatalf("expected error for output path %q, got nil", tc.outputPath)
			}
			if !strings.Contains(err.Error(), "outputs[0].path") {
				t.Errorf("error %q does not identify outputs[0].path", err.Error())
			}
			if !strings.Contains(err.Error(), tc.errContains) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.errContains)
			}

			manifestPath := filepath.Join(outputDir, planner.ExportManifestFilename)
			if _, statErr := os.Stat(manifestPath); !os.IsNotExist(statErr) {
				t.Fatalf("expected no manifest file after output path rejection, but os.Stat returned: %v", statErr)
			}
		})
	}
}

func TestFreeCADAdapter_DoesNotImplementCADRuntimeOrchestrator(t *testing.T) {
	freeCADAdapter := NewFreeCADAdapter(t.TempDir())
	if _, ok := any(freeCADAdapter).(adapter.CADRuntimeOrchestrator); ok {
		t.Fatal("production FreeCAD adapter claims aligned orchestration")
	}
}
