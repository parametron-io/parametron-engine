package freecad

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/authoring/planner"
)

func stepSelectorPayload(outputs ...planner.ExportManifestOutput) planner.WriteExportManifestPayload {
	return planner.WriteExportManifestPayload{SchemaVersion: "1.0", SourceDocument: "box.FCStd", Outputs: outputs}
}

func TestProjectFreeCADRuntimeExportManifest_STEPUsesExactObjectSelector(t *testing.T) {
	manifest, err := ProjectFreeCADRuntimeExportManifest(stepSelectorPayload(
		planner.ExportManifestOutput{Type: "step", Filename: "outputs/my-export.step", Object: "RootAssembly"},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := FreeCADRuntimeManifestOutput{ID: "RootAssembly", Format: "step", Path: "outputs/my-export.step"}
	if got := manifest.Outputs[0]; got != want {
		t.Fatalf("output: got %#v, want %#v", got, want)
	}
}

func TestProjectFreeCADRuntimeExportManifest_STEPSelectorIsIndependentOfPath(t *testing.T) {
	for _, outputPath := range []string{"outputs/Body.step", "outputs/archive/unrelated-name.step"} {
		manifest, err := ProjectFreeCADRuntimeExportManifest(stepSelectorPayload(
			planner.ExportManifestOutput{Type: "step", Filename: outputPath, Object: "Body"},
		))
		if err != nil {
			t.Fatalf("path %q: unexpected error: %v", outputPath, err)
		}
		if got := manifest.Outputs[0].ID; got != "Body" {
			t.Errorf("path %q: id got %q, want Body", outputPath, got)
		}
	}
}

func TestProjectFreeCADRuntimeExportManifest_AllowsRepeatedSTEPSelectors(t *testing.T) {
	manifest, err := ProjectFreeCADRuntimeExportManifest(stepSelectorPayload(
		planner.ExportManifestOutput{Type: "step", Filename: "outputs/primary.step", Object: "Body"},
		planner.ExportManifestOutput{Type: "step", Filename: "outputs/archive.step", Object: "Body"},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := []string{manifest.Outputs[0].ID, manifest.Outputs[1].ID}; !reflect.DeepEqual(got, []string{"Body", "Body"}) {
		t.Fatalf("ids: got %v, want [Body Body]", got)
	}
}

func TestProjectFreeCADRuntimeExportManifest_RejectsMissingSTEPSelector(t *testing.T) {
	for _, selector := range []string{"", "   "} {
		_, err := ProjectFreeCADRuntimeExportManifest(stepSelectorPayload(
			planner.ExportManifestOutput{Type: "step", Filename: "outputs/body.step", Object: selector},
		))
		if err == nil || !strings.Contains(err.Error(), "outputs[0].object") || !strings.Contains(err.Error(), "required") {
			t.Errorf("selector %q: got error %v, want required outputs[0].object error", selector, err)
		}
	}
}

func TestProjectFreeCADRuntimeExportManifest_RejectsMalformedSTEPSelector(t *testing.T) {
	for _, selector := range []string{"Body.Sub", "Body/Pad", `Body\Pad`, "Body Name", "Body\tName", "Body\nName", "Body\x00"} {
		_, err := ProjectFreeCADRuntimeExportManifest(stepSelectorPayload(
			planner.ExportManifestOutput{Type: "step", Filename: "outputs/body.step", Object: selector},
		))
		if err == nil || !strings.Contains(err.Error(), "outputs[0].object") || !strings.Contains(err.Error(), "malformed") {
			t.Errorf("selector %q: got error %v, want malformed outputs[0].object error", selector, err)
		}
	}

	for _, selector := range []string{"Body", "Body001", "RootAssembly", "Part_01", "Bracket-Left"} {
		manifest, err := ProjectFreeCADRuntimeExportManifest(stepSelectorPayload(
			planner.ExportManifestOutput{Type: "step", Filename: "outputs/body.step", Object: selector},
		))
		if err != nil {
			t.Errorf("selector %q: unexpected error: %v", selector, err)
			continue
		}
		if got := manifest.Outputs[0].ID; got != selector {
			t.Errorf("selector %q: id got %q", selector, got)
		}
	}
}

func TestProjectFreeCADRuntimeExportManifest_TrimsSurroundingSTEPSelectorWhitespace(t *testing.T) {
	manifest, err := ProjectFreeCADRuntimeExportManifest(stepSelectorPayload(
		planner.ExportManifestOutput{Type: "step", Filename: "outputs/body.step", Object: "  Body  "},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := manifest.Outputs[0].ID; got != "Body" {
		t.Fatalf("id: got %q, want Body", got)
	}
}

func TestProjectFreeCADRuntimeExportManifest_NonSTEPOutputIDsRemainSynthetic(t *testing.T) {
	manifest, err := ProjectFreeCADRuntimeExportManifest(stepSelectorPayload(
		planner.ExportManifestOutput{Type: "csv", Filename: "outputs/my report.csv"},
		planner.ExportManifestOutput{Type: "csv", Filename: "outputs/archive/my report.csv"},
		planner.ExportManifestOutput{Type: "pdf", Filename: "outputs/my report.pdf"},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"my_report-csv", "my_report-csv-2", "my_report-pdf"}
	for i := range want {
		if got := manifest.Outputs[i].ID; got != want[i] {
			t.Errorf("outputs[%d].id: got %q, want %q", i, got, want[i])
		}
	}
}

func TestProjectFreeCADRuntimeExportManifest_PreservesMixedOutputOrder(t *testing.T) {
	manifest, err := ProjectFreeCADRuntimeExportManifest(stepSelectorPayload(
		planner.ExportManifestOutput{Type: "pdf", Filename: "outputs/drawing.pdf"},
		planner.ExportManifestOutput{Type: "step", Filename: "outputs/body.step", Object: "Body"},
		planner.ExportManifestOutput{Type: "csv", Filename: "outputs/report.csv"},
		planner.ExportManifestOutput{Type: "step", Filename: "outputs/pad.step", Object: "Pad"},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantIDs := []string{"drawing-pdf", "Body", "report-csv", "Pad"}
	for i, want := range wantIDs {
		if got := manifest.Outputs[i].ID; got != want {
			t.Errorf("outputs[%d].id: got %q, want %q", i, got, want)
		}
	}
}

func TestProjectFreeCADRuntimeExportManifest_STEPSelectorProjectionIsDeterministic(t *testing.T) {
	payload := stepSelectorPayload(
		planner.ExportManifestOutput{Type: "step", Filename: "outputs/body.step", Object: "Body"},
		planner.ExportManifestOutput{Type: "step", Filename: "outputs/archive.step", Object: "Body"},
	)
	original := payload
	original.Outputs = append([]planner.ExportManifestOutput(nil), payload.Outputs...)
	first, err := ProjectFreeCADRuntimeExportManifest(payload)
	if err != nil {
		t.Fatalf("first projection: %v", err)
	}
	second, err := ProjectFreeCADRuntimeExportManifest(payload)
	if err != nil {
		t.Fatalf("second projection: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("projections differ: %#v vs %#v", first, second)
	}
	if !reflect.DeepEqual(payload, original) {
		t.Fatalf("projection mutated caller input: got %#v, want %#v", payload, original)
	}
}

func validProjectionPayload() planner.WriteExportManifestPayload {
	return planner.WriteExportManifestPayload{
		SchemaVersion:  "1.0",
		SourceDocument: "box.FCStd",
		ParameterAssignments: []planner.ExportManifestParameterAssignment{
			{Name: "Width", Value: 50.0, Type: "number", Unit: "mm"},
		},
		AssemblyMutations: &planner.ExportManifestMutationCollection{
			Parameters: []planner.ExportManifestParameterMutation{
				{Object: "Box", Property: "Width", ValueParam: "Width"},
			},
		},
		Outputs: []planner.ExportManifestOutput{
			{Type: "step", Filename: "outputs/box.step", Object: "Body"},
		},
	}
}

// Test 1: Valid projection.
func TestProjectFreeCADRuntimeExportManifest_ValidPayload(t *testing.T) {
	manifest, err := ProjectFreeCADRuntimeExportManifest(validProjectionPayload())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if manifest == nil {
		t.Fatal("expected non-nil manifest")
	}

	if manifest.SchemaVersion != "1.0" {
		t.Errorf("schemaVersion: got %q, want %q", manifest.SchemaVersion, "1.0")
	}
	if manifest.SourceDocument != "box.FCStd" {
		t.Errorf("sourceDocument: got %q, want %q", manifest.SourceDocument, "box.FCStd")
	}

	if len(manifest.ParameterAssignments) != 1 {
		t.Fatalf("parameterAssignments: got %d, want 1", len(manifest.ParameterAssignments))
	}
	pa := manifest.ParameterAssignments[0]
	if pa.Target != "Box.Width" {
		t.Errorf("parameterAssignments[0].target: got %q, want %q", pa.Target, "Box.Width")
	}
	if v, ok := pa.Value.(float64); !ok || v != 50.0 {
		t.Errorf("parameterAssignments[0].value: got %v (%T), want float64(50.0)", pa.Value, pa.Value)
	}
	if pa.ValueKind != "float" {
		t.Errorf("parameterAssignments[0].valueKind: got %q, want %q", pa.ValueKind, "float")
	}

	if len(manifest.Outputs) != 1 {
		t.Fatalf("outputs: got %d, want 1", len(manifest.Outputs))
	}
	out := manifest.Outputs[0]
	if out.Format != "step" {
		t.Errorf("outputs[0].format: got %q, want %q", out.Format, "step")
	}
	if out.Path != "outputs/box.step" {
		t.Errorf("outputs[0].path: got %q, want %q", out.Path, "outputs/box.step")
	}
	if out.ID != "Body" {
		t.Errorf("outputs[0].id: got %q, want %q", out.ID, "Body")
	}

	data, err := MarshalFreeCADRuntimeExportManifestJSON(manifest)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	raw := string(data)
	for _, legacyField := range []string{
		`"inputs"`, `"sourceModel"`, `"name"`, `"type"`, `"unit"`,
		`"filename"`, `"object"`, `"assemblyMutations"`, `"partMutations"`,
	} {
		if strings.Contains(raw, legacyField) {
			t.Errorf("marshaled JSON contains legacy field %s", legacyField)
		}
	}
}

// Test 2: Deterministic JSON serialization.
func TestMarshalFreeCADRuntimeExportManifestJSON_Deterministic(t *testing.T) {
	manifest := &FreeCADRuntimeExportManifest{
		SchemaVersion:  "1.0",
		SourceDocument: "box.FCStd",
		ParameterAssignments: []FreeCADRuntimeParameterAssignment{
			{Target: "Box.Width", Value: 50.0, ValueKind: "float"},
		},
		Outputs: []FreeCADRuntimeManifestOutput{
			{ID: "Body", Format: "step", Path: "outputs/box.step"},
		},
	}

	data1, err := MarshalFreeCADRuntimeExportManifestJSON(manifest)
	if err != nil {
		t.Fatalf("first marshal: %v", err)
	}
	data2, err := MarshalFreeCADRuntimeExportManifestJSON(manifest)
	if err != nil {
		t.Fatalf("second marshal: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data1, &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	s := string(data1)
	if !strings.HasSuffix(s, "\n") {
		t.Error("output does not end with newline")
	}
	if strings.HasSuffix(s, "\n\n") {
		t.Error("output ends with more than one newline")
	}
	if s != string(data2) {
		t.Error("repeated serialization of the same manifest produced different bytes")
	}

	for _, nativeField := range []string{
		`"schemaVersion"`, `"sourceDocument"`, `"parameterAssignments"`, `"outputs"`,
		`"target"`, `"value"`, `"valueKind"`, `"id"`, `"format"`, `"path"`,
	} {
		if !strings.Contains(s, nativeField) {
			t.Errorf("marshaled JSON missing native field %s", nativeField)
		}
	}
	for _, legacyField := range []string{
		`"inputs"`, `"sourceModel"`, `"name"`, `"type"`, `"unit"`,
		`"filename"`, `"object"`, `"assemblyMutations"`, `"partMutations"`,
	} {
		if strings.Contains(s, legacyField) {
			t.Errorf("marshaled JSON contains legacy field %s", legacyField)
		}
	}
}

// Test 3: Missing explicit target rejection.
func TestProjectFreeCADRuntimeExportManifest_MissingTargetRejection(t *testing.T) {
	cases := []struct {
		name        string
		payload     planner.WriteExportManifestPayload
		errContains string
	}{
		{
			name: "no matching mutation",
			payload: func() planner.WriteExportManifestPayload {
				p := validProjectionPayload()
				p.AssemblyMutations = nil
				return p
			}(),
			errContains: "missing explicit CAD target",
		},
		{
			name: "multiple matching mutations by ValueParam",
			payload: func() planner.WriteExportManifestPayload {
				p := validProjectionPayload()
				p.PartMutations = &planner.ExportManifestMutationCollection{
					Parameters: []planner.ExportManifestParameterMutation{
						{Object: "Part", Property: "Width", ValueParam: "Width"},
					},
				}
				return p
			}(),
			errContains: "ambiguous CAD target",
		},
		{
			name: "matching mutation has empty object",
			payload: func() planner.WriteExportManifestPayload {
				p := validProjectionPayload()
				p.AssemblyMutations.Parameters[0] = planner.ExportManifestParameterMutation{
					Object: "", Property: "Width", ValueParam: "Width",
				}
				return p
			}(),
			errContains: "target",
		},
		{
			name: "matching mutation has empty property",
			payload: func() planner.WriteExportManifestPayload {
				p := validProjectionPayload()
				p.AssemblyMutations.Parameters[0] = planner.ExportManifestParameterMutation{
					Object: "Box", Property: "", ValueParam: "Width",
				}
				return p
			}(),
			errContains: "target",
		},
		{
			name: "matching mutation has whitespace-only object",
			payload: func() planner.WriteExportManifestPayload {
				p := validProjectionPayload()
				p.AssemblyMutations.Parameters[0] = planner.ExportManifestParameterMutation{
					Object: "   ", Property: "Width", ValueParam: "Width",
				}
				return p
			}(),
			errContains: "target",
		},
		{
			name: "matching mutation has whitespace-only property",
			payload: func() planner.WriteExportManifestPayload {
				p := validProjectionPayload()
				p.AssemblyMutations.Parameters[0] = planner.ExportManifestParameterMutation{
					Object: "Box", Property: "\t", ValueParam: "Width",
				}
				return p
			}(),
			errContains: "target",
		},
		{
			name: "object segment contains dot",
			payload: func() planner.WriteExportManifestPayload {
				p := validProjectionPayload()
				p.AssemblyMutations.Parameters[0] = planner.ExportManifestParameterMutation{
					Object: "Box.Sub", Property: "Width", ValueParam: "Width",
				}
				return p
			}(),
			errContains: "target",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manifest, err := ProjectFreeCADRuntimeExportManifest(tc.payload)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if manifest != nil {
				t.Error("expected nil manifest on error")
			}
			if !strings.Contains(err.Error(), tc.errContains) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.errContains)
			}
		})
	}
}

// Test 4: Unsupported parameter assignment rejection.
func TestProjectFreeCADRuntimeExportManifest_UnsupportedParameterAssignment(t *testing.T) {
	cases := []struct {
		name        string
		modify      func(*planner.WriteExportManifestPayload)
		errContains string
	}{
		{
			name: "unsupported type",
			modify: func(p *planner.WriteExportManifestPayload) {
				p.ParameterAssignments[0].Type = "string"
			},
			errContains: "not supported",
		},
		{
			name: "unsupported unit",
			modify: func(p *planner.WriteExportManifestPayload) {
				p.ParameterAssignments[0].Unit = "inch"
			},
			errContains: "not supported",
		},
		{
			name: "NaN value",
			modify: func(p *planner.WriteExportManifestPayload) {
				p.ParameterAssignments[0].Value = math.NaN()
			},
			errContains: "finite",
		},
		{
			name: "+Inf value",
			modify: func(p *planner.WriteExportManifestPayload) {
				p.ParameterAssignments[0].Value = math.Inf(1)
			},
			errContains: "finite",
		},
		{
			name: "-Inf value",
			modify: func(p *planner.WriteExportManifestPayload) {
				p.ParameterAssignments[0].Value = math.Inf(-1)
			},
			errContains: "finite",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := validProjectionPayload()
			tc.modify(&payload)
			manifest, err := ProjectFreeCADRuntimeExportManifest(payload)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if manifest != nil {
				t.Error("expected nil manifest on error")
			}
			if !strings.Contains(err.Error(), tc.errContains) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.errContains)
			}
		})
	}
}

// Test 5: Source document path validation.
func TestProjectFreeCADRuntimeExportManifest_SourceDocumentPath(t *testing.T) {
	makePayload := func(sourceDocument string) planner.WriteExportManifestPayload {
		return planner.WriteExportManifestPayload{
			SchemaVersion:  "1.0",
			SourceDocument: sourceDocument,
			Outputs:        []planner.ExportManifestOutput{{Type: "step", Filename: "outputs/box.step", Object: "Body"}},
		}
	}

	accepted := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple relative path", "box.FCStd", "box.FCStd"},
		{"nested relative path", "input/box.FCStd", "input/box.FCStd"},
		{"backslash separator normalization", `input\box.FCStd`, "input/box.FCStd"},
		{"whitespace trimmed path", " input/model.FCStd ", "input/model.FCStd"},
	}
	for _, tc := range accepted {
		t.Run("accepted/"+tc.name, func(t *testing.T) {
			manifest, err := ProjectFreeCADRuntimeExportManifest(makePayload(tc.input))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if manifest.SourceDocument != tc.expected {
				t.Errorf("sourceDocument: got %q, want %q", manifest.SourceDocument, tc.expected)
			}
		})
	}

	rejected := []struct {
		name        string
		input       string
		errContains string
	}{
		{"empty path", "", "is required"},
		{"whitespace only", "   ", "is required"},
		{"dot only", ".", "is required"},
		{"null byte", "\x00model.FCStd", "null byte"},
		{"double dot", "..", "parent traversal"},
		{"parent traversal", "../box.FCStd", "parent traversal"},
		{"nested parent traversal", "input/../box.FCStd", "parent traversal"},
		{"double parent traversal", "input/../../model.FCStd", "parent traversal"},
		{"clean-back-inside traversal", "input/nested/../model.FCStd", "parent traversal"},
		{"absolute Unix path", "/box.FCStd", "must be relative"},
		{"absolute Windows path", `C:\box.FCStd`, "must be relative"},
		{"leading backslash", `\model.FCStd`, "must be relative"},
		{"UNC path", `\\server\share\model.FCStd`, "must be relative"},
	}
	for _, tc := range rejected {
		t.Run("rejected/"+tc.name, func(t *testing.T) {
			_, err := ProjectFreeCADRuntimeExportManifest(makePayload(tc.input))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.errContains) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.errContains)
			}
		})
	}
}

// Test 6: Output projection.
func TestProjectFreeCADRuntimeExportManifest_OutputProjection(t *testing.T) {
	makePayload := func(outputs []planner.ExportManifestOutput) planner.WriteExportManifestPayload {
		return planner.WriteExportManifestPayload{
			SchemaVersion:  "1.0",
			SourceDocument: "box.FCStd",
			Outputs:        outputs,
		}
	}

	t.Run("step format accepted", func(t *testing.T) {
		manifest, err := ProjectFreeCADRuntimeExportManifest(makePayload([]planner.ExportManifestOutput{
			{Type: "step", Filename: "outputs/box.step", Object: "Body"},
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if manifest.Outputs[0].Format != "step" {
			t.Errorf("format: got %q, want %q", manifest.Outputs[0].Format, "step")
		}
	})

	t.Run("csv format accepted", func(t *testing.T) {
		manifest, err := ProjectFreeCADRuntimeExportManifest(makePayload([]planner.ExportManifestOutput{
			{Type: "csv", Filename: "outputs/data.csv"},
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if manifest.Outputs[0].Format != "csv" {
			t.Errorf("format: got %q, want %q", manifest.Outputs[0].Format, "csv")
		}
	})

	t.Run("pdf format accepted", func(t *testing.T) {
		manifest, err := ProjectFreeCADRuntimeExportManifest(makePayload([]planner.ExportManifestOutput{
			{Type: "pdf", Filename: "outputs/drawing.pdf"},
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if manifest.Outputs[0].Format != "pdf" {
			t.Errorf("format: got %q, want %q", manifest.Outputs[0].Format, "pdf")
		}
	})

	t.Run("empty output path rejected", func(t *testing.T) {
		_, err := ProjectFreeCADRuntimeExportManifest(makePayload([]planner.ExportManifestOutput{
			{Type: "step", Filename: "", Object: "Body"},
		}))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "is required") {
			t.Errorf("error %q does not contain %q", err.Error(), "is required")
		}
	})

	t.Run("absolute output path rejected", func(t *testing.T) {
		_, err := ProjectFreeCADRuntimeExportManifest(makePayload([]planner.ExportManifestOutput{
			{Type: "step", Filename: "/absolute/box.step", Object: "Body"},
		}))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "must be relative") {
			t.Errorf("error %q does not contain %q", err.Error(), "must be relative")
		}
	})

	t.Run("parent traversal output path rejected", func(t *testing.T) {
		_, err := ProjectFreeCADRuntimeExportManifest(makePayload([]planner.ExportManifestOutput{
			{Type: "step", Filename: "../box.step", Object: "Body"},
		}))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "parent traversal") {
			t.Errorf("error %q does not contain %q", err.Error(), "parent traversal")
		}
	})

	t.Run("duplicate normalized output path rejected", func(t *testing.T) {
		_, err := ProjectFreeCADRuntimeExportManifest(makePayload([]planner.ExportManifestOutput{
			{Type: "step", Filename: "outputs/box.step", Object: "Body"},
			{Type: "step", Filename: "outputs/box.step", Object: "Body001"},
		}))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "duplicates") {
			t.Errorf("error %q does not contain %q", err.Error(), "duplicates")
		}
	})

	t.Run("output IDs are deterministic", func(t *testing.T) {
		payload := makePayload([]planner.ExportManifestOutput{
			{Type: "step", Filename: "outputs/box.step", Object: "Body"},
			{Type: "csv", Filename: "outputs/data.csv"},
		})
		m1, err := ProjectFreeCADRuntimeExportManifest(payload)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m2, err := ProjectFreeCADRuntimeExportManifest(payload)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for i := range m1.Outputs {
			if m1.Outputs[i].ID != m2.Outputs[i].ID {
				t.Errorf("outputs[%d].id: %q vs %q (not deterministic)", i, m1.Outputs[i].ID, m2.Outputs[i].ID)
			}
		}
	})

	t.Run("output ID collision resolved deterministically", func(t *testing.T) {
		// Repeated selectors remain exact and are not collision-suffixed.
		payload := makePayload([]planner.ExportManifestOutput{
			{Type: "step", Filename: "outputs/a/box.step", Object: "Body"},
			{Type: "step", Filename: "outputs/b/box.step", Object: "Body"},
		})
		m1, err := ProjectFreeCADRuntimeExportManifest(payload)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(m1.Outputs) != 2 {
			t.Fatalf("expected 2 outputs, got %d", len(m1.Outputs))
		}
		id0, id1 := m1.Outputs[0].ID, m1.Outputs[1].ID
		if id0 == "" || id1 == "" {
			t.Error("output IDs must not be empty")
		}
		if id0 != "Body" || id1 != "Body" {
			t.Errorf("STEP selectors changed: got %q and %q", id0, id1)
		}

		m2, err := ProjectFreeCADRuntimeExportManifest(payload)
		if err != nil {
			t.Fatalf("unexpected error on second projection: %v", err)
		}
		if m1.Outputs[0].ID != m2.Outputs[0].ID || m1.Outputs[1].ID != m2.Outputs[1].ID {
			t.Error("output ID collision resolution is not deterministic")
		}
	})
}

func TestProjectFreeCADRuntimeExportManifest_UnsupportedOutputDeclarations(t *testing.T) {
	tests := []struct {
		name       string
		outputType string
		outputPath string
	}{
		{name: "empty format", outputType: "", outputPath: "outputs/box.invalid"},
		{name: "whitespace format", outputType: "   ", outputPath: "outputs/box.invalid"},
		{name: "stl", outputType: "stl", outputPath: "outputs/box.stl"},
		{name: "dxf", outputType: "dxf", outputPath: "outputs/box.dxf"},
		{name: "svg", outputType: "svg", outputPath: "outputs/box.svg"},
		{name: "json", outputType: "json", outputPath: "outputs/box.json"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload := validProjectionPayload()
			payload.Outputs = []planner.ExportManifestOutput{
				{Type: tc.outputType, Filename: tc.outputPath},
			}

			manifest, err := ProjectFreeCADRuntimeExportManifest(payload)
			if err == nil {
				t.Fatal("expected unsupported output declaration error, got nil")
			}
			if manifest != nil {
				t.Fatalf("expected nil manifest on error, got %+v", manifest)
			}
			if !strings.Contains(err.Error(), "outputs[0].format") {
				t.Errorf("error %q does not identify outputs[0].format", err.Error())
			}
			if !strings.Contains(err.Error(), "is not supported") {
				t.Errorf("error %q does not identify unsupported format", err.Error())
			}

			repeatedManifest, repeatedErr := ProjectFreeCADRuntimeExportManifest(payload)
			if repeatedErr == nil {
				t.Fatal("expected repeated projection to fail")
			}
			if repeatedManifest != nil {
				t.Fatalf("expected nil manifest on repeated error, got %+v", repeatedManifest)
			}
			if repeatedErr.Error() != err.Error() {
				t.Fatalf("projection error is not deterministic: first %q, repeated %q", err, repeatedErr)
			}
		})
	}
}

// Test 7: Empty output list projects to a zero-length outputs slice.
//
// This projection function is the single native-manifest projector used for
// every freecad-runtime-native payload, whether or not it carries derived
// outputs (see isFreeCADRuntimeNativeManifest, which gates purely on
// adapter identity, not on requested-format content). The invariant that
// only an explicit outputs=["none"] request may reach the planner with a
// zero-length ExportManifestOutput slice is enforced upstream in the
// planner (normalizeFreeCADRuntimeOutputsForRequest); by the time a payload
// reaches this projector, an empty Outputs slice is always a legitimate
// native-only request, and projection must succeed with a non-nil empty
// slice, not synthesize or reject it.
func TestProjectFreeCADRuntimeExportManifest_EmptyOutputListProjectsToEmptySlice(t *testing.T) {
	for _, outputs := range [][]planner.ExportManifestOutput{nil, {}} {
		payload := validProjectionPayload()
		payload.Outputs = outputs

		manifest, err := ProjectFreeCADRuntimeExportManifest(payload)
		if err != nil {
			t.Fatalf("unexpected error for empty native outputs: %v", err)
		}
		if manifest == nil {
			t.Fatal("expected non-nil manifest")
		}
		if manifest.Outputs == nil {
			t.Fatal("expected a non-nil empty Outputs slice, got nil")
		}
		if len(manifest.Outputs) != 0 {
			t.Fatalf("expected zero outputs, got %+v", manifest.Outputs)
		}
		if manifest.SourceDocument == "" {
			t.Fatal("expected sourceDocument to remain populated for a native-only manifest")
		}
		if len(manifest.ParameterAssignments) == 0 {
			t.Fatal("expected parameterAssignments to remain populated for a native-only manifest")
		}
	}
}

// Test 8: Nil manifest serialization rejection.
func TestMarshalFreeCADRuntimeExportManifestJSON_RejectsNilManifest(t *testing.T) {
	data, err := MarshalFreeCADRuntimeExportManifestJSON(nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if data != nil {
		t.Error("expected nil bytes on error")
	}
	if !strings.Contains(err.Error(), "manifest") {
		t.Errorf("error %q does not mention manifest", err.Error())
	}
}

// Test 9: Native projection uses explicit assignment.Target when present.
func TestProjectFreeCADRuntimeExportManifest_UsesExplicitAssignmentTarget(t *testing.T) {
	payload := validProjectionPayload()
	payload.ParameterAssignments[0].Target = "ExplicitObject.ExplicitProperty"

	manifest, err := ProjectFreeCADRuntimeExportManifest(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if manifest.ParameterAssignments[0].Target != "ExplicitObject.ExplicitProperty" {
		t.Errorf("expected explicit target %q, got %q", "ExplicitObject.ExplicitProperty", manifest.ParameterAssignments[0].Target)
	}
}

// Test 10: Explicit target bypasses mutation fallback even when mutation would match.
func TestProjectFreeCADRuntimeExportManifest_ExplicitTargetBypassesMutationFallback(t *testing.T) {
	payload := validProjectionPayload()
	// The mutation says Box.Width but the explicit target differs.
	payload.ParameterAssignments[0].Target = "DifferentObject.DifferentProperty"

	manifest, err := ProjectFreeCADRuntimeExportManifest(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if manifest.ParameterAssignments[0].Target != "DifferentObject.DifferentProperty" {
		t.Errorf("expected explicit target %q, got %q", "DifferentObject.DifferentProperty", manifest.ParameterAssignments[0].Target)
	}
}

// Test 11: Explicit target is validated by existing strict target validation.
func TestProjectFreeCADRuntimeExportManifest_ExplicitTargetValidation(t *testing.T) {
	cases := []struct {
		name        string
		target      string
		errContains string
		expectOK    bool
	}{
		{"valid target", "Body.Width", "", true},
		{"valid Sketch.Height", "Sketch.Height", "", true},
		{"valid Pad.Length", "Pad.Length", "", true},
		{"missing dot", "BodyWidth", "malformed", false},
		{"multiple dots", "Body.Sub.Width", "malformed", false},
		{"empty object segment", ".Width", "malformed", false},
		{"empty property segment", "Body.", "malformed", false},
		{"slash in object segment", "Body/Pad.Width", "malformed", false},
		{"slash in property segment", "Body.Width/Length", "malformed", false},
		{"backslash in object segment", "Body\\Pad.Width", "malformed", false},
		{"backslash in property segment", "Body.Width\\Length", "malformed", false},
		{"space before dot", "Body .Width", "whitespace", false},
		{"space after dot", "Body. Width", "whitespace", false},
		{"tab before dot", "Body\t.Width", "whitespace", false},
		{"tab after dot", "Body.\tWidth", "whitespace", false},
		{"null byte in property segment", "Body.\x00Width", "null byte", false},
		{"null byte in object segment", "Body\x00.Width", "null byte", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := validProjectionPayload()
			payload.ParameterAssignments[0].Target = tc.target

			manifest, err := ProjectFreeCADRuntimeExportManifest(payload)
			if tc.expectOK {
				if err != nil {
					t.Fatalf("expected success, got error: %v", err)
				}
				if manifest == nil {
					t.Fatal("expected non-nil manifest")
				}
			} else {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tc.errContains)
				}
			}
		})
	}
}

// Test 12: Mutation fallback still works when assignment.Target is empty.
func TestProjectFreeCADRuntimeExportManifest_MutationFallbackWhenTargetEmpty(t *testing.T) {
	payload := validProjectionPayload()
	payload.ParameterAssignments[0].Target = ""

	manifest, err := ProjectFreeCADRuntimeExportManifest(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if manifest.ParameterAssignments[0].Target != "Box.Width" {
		t.Errorf("expected mutation fallback target %q, got %q", "Box.Width", manifest.ParameterAssignments[0].Target)
	}
}

// Test 13: Invalid explicit target fails deterministically.
func TestProjectFreeCADRuntimeExportManifest_InvalidExplicitTargetFailsDeterministically(t *testing.T) {
	payload := validProjectionPayload()
	payload.ParameterAssignments[0].Target = "invalid-no-dot"

	_, err1 := ProjectFreeCADRuntimeExportManifest(payload)
	_, err2 := ProjectFreeCADRuntimeExportManifest(payload)

	if err1 == nil || err2 == nil {
		t.Fatal("expected both invocations to return an error")
	}
	if err1.Error() != err2.Error() {
		t.Fatalf("error is not deterministic\nfirst:  %s\nsecond: %s", err1.Error(), err2.Error())
	}
	if !strings.Contains(err1.Error(), "malformed") {
		t.Errorf("expected error to mention malformed, got %q", err1.Error())
	}
}

// Test 15: Zero value is preserved in native projection and not omitted from JSON.
func TestProjectFreeCADRuntimeExportManifest_ZeroValuePreserved(t *testing.T) {
	payload := validProjectionPayload()
	payload.ParameterAssignments[0].Value = 0.0

	manifest, err := ProjectFreeCADRuntimeExportManifest(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pa := manifest.ParameterAssignments[0]
	v, ok := pa.Value.(float64)
	if !ok {
		t.Fatalf("parameterAssignments[0].value: expected float64, got %T", pa.Value)
	}
	if v != 0.0 {
		t.Errorf("parameterAssignments[0].value: got %v, want 0.0", v)
	}

	// Verify zero value is not omitted in JSON (guards against omitempty bugs).
	data, err := MarshalFreeCADRuntimeExportManifestJSON(manifest)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	assignments, ok := raw["parameterAssignments"].([]any)
	if !ok || len(assignments) == 0 {
		t.Fatal("expected non-empty parameterAssignments in JSON")
	}
	pa0, ok := assignments[0].(map[string]any)
	if !ok {
		t.Fatal("expected parameterAssignments[0] to be a JSON object")
	}
	jsonVal, exists := pa0["value"]
	if !exists {
		t.Fatal("parameterAssignments[0].value must not be omitted when the value is zero")
	}
	if jsonVal != 0.0 {
		t.Errorf("parameterAssignments[0].value in JSON: got %v, want 0.0", jsonVal)
	}
}

// Test 16: Negative and decimal finite values survive projection and JSON serialization.
func TestProjectFreeCADRuntimeExportManifest_NegativeAndDecimalValues(t *testing.T) {
	cases := []struct {
		name  string
		value float64
	}{
		{"negative", -12.5},
		{"decimal", 3.75},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := validProjectionPayload()
			payload.ParameterAssignments[0].Value = tc.value

			manifest, err := ProjectFreeCADRuntimeExportManifest(payload)
			if err != nil {
				t.Fatalf("unexpected error for %v: %v", tc.value, err)
			}
			v, ok := manifest.ParameterAssignments[0].Value.(float64)
			if !ok {
				t.Fatalf("value: expected float64, got %T", manifest.ParameterAssignments[0].Value)
			}
			if v != tc.value {
				t.Errorf("value: got %v, want %v", v, tc.value)
			}

			data, err := MarshalFreeCADRuntimeExportManifestJSON(manifest)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var raw map[string]any
			if err := json.Unmarshal(data, &raw); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			assignments, ok := raw["parameterAssignments"].([]any)
			if !ok || len(assignments) == 0 {
				t.Fatal("expected non-empty parameterAssignments in JSON")
			}
			pa, ok := assignments[0].(map[string]any)
			if !ok {
				t.Fatal("expected parameterAssignments[0] to be a JSON object")
			}
			jsonVal, exists := pa["value"]
			if !exists {
				t.Fatalf("parameterAssignments[0].value must be present for %v", tc.value)
			}
			if jsonVal != tc.value {
				t.Errorf("JSON value: got %v, want %v", jsonVal, tc.value)
			}
		})
	}
}

// Test 14: No name-only fallback: missing target and no matching mutation is rejected.
func TestProjectFreeCADRuntimeExportManifest_ExplicitTargetDoesNotIntroduceNameOnlyFallback(t *testing.T) {
	payload := planner.WriteExportManifestPayload{
		SchemaVersion:  "1.0",
		SourceDocument: "box.FCStd",
		ParameterAssignments: []planner.ExportManifestParameterAssignment{
			{Name: "Width", Value: 50.0, Type: "number", Unit: "mm"},
		},
		Outputs: []planner.ExportManifestOutput{
			{Type: "step", Filename: "outputs/box.step", Object: "Body"},
		},
	}

	_, err := ProjectFreeCADRuntimeExportManifest(payload)
	if err == nil {
		t.Fatal("expected error when no explicit target and no mutation fallback, got nil")
	}
	if !strings.Contains(err.Error(), "missing explicit CAD target") {
		t.Errorf("error %q does not contain %q", err.Error(), "missing explicit CAD target")
	}
}

// Test 17: Native output path containment — accepted cases.
func TestProjectFreeCADRuntimeOutputPath_ContainmentAccepted(t *testing.T) {
	makePayload := func(filename string) planner.WriteExportManifestPayload {
		return planner.WriteExportManifestPayload{
			SchemaVersion:  "1.0",
			SourceDocument: "box.FCStd",
			Outputs:        []planner.ExportManifestOutput{{Type: "step", Filename: filename, Object: "Body"}},
		}
	}

	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{"direct contained path", "outputs/box.step", "outputs/box.step"},
		{"nested contained path", "outputs/nested/box.step", "outputs/nested/box.step"},
		{"whitespace trimmed contained path", " outputs/box.step ", "outputs/box.step"},
		{"backslash normalized into boundary", "outputs\\nested\\box.step", "outputs/nested/box.step"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manifest, err := ProjectFreeCADRuntimeExportManifest(makePayload(tc.input))
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.input, err)
			}
			if len(manifest.Outputs) != 1 {
				t.Fatalf("expected 1 output, got %d", len(manifest.Outputs))
			}
			got := manifest.Outputs[0].Path
			if got != tc.expected {
				t.Errorf("path: got %q, want %q", got, tc.expected)
			}
			if !strings.HasPrefix(got, "outputs/") {
				t.Errorf("emitted path %q is not under outputs/", got)
			}
			if strings.HasPrefix(got, "/") {
				t.Errorf("emitted path %q must not be absolute", got)
			}
			if strings.Contains(got, "..") {
				t.Errorf("emitted path %q must not contain parent traversal", got)
			}
		})
	}
}

// Test 18: Native output path containment — rejected cases.
func TestProjectFreeCADRuntimeOutputPath_ContainmentRejected(t *testing.T) {
	makePayload := func(filename string) planner.WriteExportManifestPayload {
		return planner.WriteExportManifestPayload{
			SchemaVersion:  "1.0",
			SourceDocument: "box.FCStd",
			Outputs:        []planner.ExportManifestOutput{{Type: "step", Filename: filename, Object: "Body"}},
		}
	}

	cases := []struct {
		name        string
		input       string
		errContains string
	}{
		{"empty string", "", "is required"},
		{"whitespace only", "   ", "is required"},
		{"dot only", ".", "is required"},
		{"outputs dot only", "outputs/.", "must be contained under"},
		{"root level path", "box.step", "must be contained under"},
		{"wrong outputs prefix", "output/box.step", "must be contained under"},
		{"absolute unix path", "/tmp/box.step", "must be relative"},
		{"windows drive path", `C:\tmp\box.step`, "must be relative"},
		{"leading backslash", `\tmp\box.step`, "must be relative"},
		{"UNC path", `\\server\share\box.step`, "must be relative"},
		{"parent traversal", "../box.step", "must not contain parent traversal"},
		{"outputs parent traversal", "outputs/../box.step", "must not contain parent traversal"},
		{"outputs double parent traversal", "outputs/../../box.step", "must not contain parent traversal"},
		{"nested outputs double parent traversal", "outputs/nested/../../box.step", "must not contain parent traversal"},
		{"null byte", "outputs/\x00box.step", "null byte"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manifest, err := ProjectFreeCADRuntimeExportManifest(makePayload(tc.input))
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc.input)
			}
			if manifest != nil {
				t.Error("expected nil manifest on error")
			}
			if !strings.Contains(err.Error(), "outputs[0].path") {
				t.Errorf("error %q does not identify outputs[0].path", err.Error())
			}
			if !strings.Contains(err.Error(), tc.errContains) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.errContains)
			}
		})
	}
}

// Test 20: Valid projection with multiple assignments and multiple outputs.
func TestProjectFreeCADRuntimeExportManifest_MultipleAssignmentsAndOutputs(t *testing.T) {
	payload := planner.WriteExportManifestPayload{
		SchemaVersion:  "1.0",
		SourceDocument: "input/box.FCStd",
		ParameterAssignments: []planner.ExportManifestParameterAssignment{
			{Name: "Length", Value: 120.0, Type: "number", Unit: "mm"},
			{Name: "Width", Value: 60.0, Type: "number", Unit: "mm"},
			{Name: "Height", Value: 25.5, Type: "number", Unit: "mm"},
		},
		AssemblyMutations: &planner.ExportManifestMutationCollection{
			Parameters: []planner.ExportManifestParameterMutation{
				{Object: "Box", Property: "Length", ValueParam: "Length"},
				{Object: "Box", Property: "Width", ValueParam: "Width"},
				{Object: "Pad", Property: "Height", ValueParam: "Height"},
			},
		},
		Outputs: []planner.ExportManifestOutput{
			{Type: "step", Filename: "outputs/box.step", Object: "Body"},
			{Type: "csv", Filename: "outputs/box.csv"},
			{Type: "pdf", Filename: "outputs/report.pdf"},
		},
	}

	manifest, err := ProjectFreeCADRuntimeExportManifest(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if manifest == nil {
		t.Fatal("expected non-nil manifest")
	}

	if manifest.SchemaVersion != "1.0" {
		t.Errorf("schemaVersion: got %q, want %q", manifest.SchemaVersion, "1.0")
	}
	if manifest.SourceDocument != "input/box.FCStd" {
		t.Errorf("sourceDocument: got %q, want %q", manifest.SourceDocument, "input/box.FCStd")
	}

	if len(manifest.ParameterAssignments) != 3 {
		t.Fatalf("parameterAssignments: got %d, want 3", len(manifest.ParameterAssignments))
	}
	wantAssignments := []struct {
		target    string
		value     float64
		valueKind string
	}{
		{"Box.Length", 120.0, "float"},
		{"Box.Width", 60.0, "float"},
		{"Pad.Height", 25.5, "float"},
	}
	for i, want := range wantAssignments {
		pa := manifest.ParameterAssignments[i]
		if pa.Target != want.target {
			t.Errorf("parameterAssignments[%d].target: got %q, want %q", i, pa.Target, want.target)
		}
		v, ok := pa.Value.(float64)
		if !ok || v != want.value {
			t.Errorf("parameterAssignments[%d].value: got %v (%T), want float64(%v)", i, pa.Value, pa.Value, want.value)
		}
		if pa.ValueKind != want.valueKind {
			t.Errorf("parameterAssignments[%d].valueKind: got %q, want %q", i, pa.ValueKind, want.valueKind)
		}
	}

	if len(manifest.Outputs) != 3 {
		t.Fatalf("outputs: got %d, want 3", len(manifest.Outputs))
	}
	wantOutputs := []struct {
		format string
		path   string
	}{
		{"step", "outputs/box.step"},
		{"csv", "outputs/box.csv"},
		{"pdf", "outputs/report.pdf"},
	}
	for i, want := range wantOutputs {
		out := manifest.Outputs[i]
		if out.ID == "" {
			t.Errorf("outputs[%d].id must not be empty", i)
		}
		if out.Format != want.format {
			t.Errorf("outputs[%d].format: got %q, want %q", i, out.Format, want.format)
		}
		if out.Path != want.path {
			t.Errorf("outputs[%d].path: got %q, want %q", i, out.Path, want.path)
		}
	}
}

// Test 21: All supported output formats normalize correctly in a single projection.
func TestProjectFreeCADRuntimeExportManifest_AllSupportedOutputFormatsNormalized(t *testing.T) {
	payload := planner.WriteExportManifestPayload{
		SchemaVersion:  "1.0",
		SourceDocument: "box.FCStd",
		Outputs: []planner.ExportManifestOutput{
			{Type: "step", Filename: "outputs/model.step", Object: "Body"},
			{Type: "csv", Filename: "outputs/data.csv"},
			{Type: "pdf", Filename: "outputs/drawing.pdf"},
		},
	}

	manifest, err := ProjectFreeCADRuntimeExportManifest(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(manifest.Outputs) != 3 {
		t.Fatalf("outputs: got %d, want 3", len(manifest.Outputs))
	}

	wantFormats := []string{"step", "csv", "pdf"}
	for i, want := range wantFormats {
		out := manifest.Outputs[i]
		if out.Format != want {
			t.Errorf("outputs[%d].format: got %q, want %q", i, out.Format, want)
		}
		if out.ID == "" {
			t.Errorf("outputs[%d].id must not be empty", i)
		}
		if out.Path == "" {
			t.Errorf("outputs[%d].path must not be empty", i)
		}
	}
}

// Test 22: Output IDs are unique and deterministic when outputs share a stem across different
// formats and different directories.
func TestProjectFreeCADRuntimeExportManifest_OutputIDCollisionStemAndFormat(t *testing.T) {
	payload := planner.WriteExportManifestPayload{
		SchemaVersion:  "1.0",
		SourceDocument: "box.FCStd",
		Outputs: []planner.ExportManifestOutput{
			{Type: "step", Filename: "outputs/box.step", Object: "Body"},
			{Type: "csv", Filename: "outputs/box.csv"},
			{Type: "step", Filename: "outputs/nested/box.step", Object: "Body001"},
		},
	}

	m1, err := ProjectFreeCADRuntimeExportManifest(payload)
	if err != nil {
		t.Fatalf("unexpected error on first projection: %v", err)
	}
	if len(m1.Outputs) != 3 {
		t.Fatalf("expected 3 outputs, got %d", len(m1.Outputs))
	}

	for i, out := range m1.Outputs {
		if out.ID == "" {
			t.Errorf("outputs[%d].id must not be empty", i)
		}
	}

	ids := make(map[string]int, 3)
	for i, out := range m1.Outputs {
		if j, exists := ids[out.ID]; exists {
			t.Errorf("outputs[%d].id %q collides with outputs[%d].id", i, out.ID, j)
		}
		ids[out.ID] = i
	}

	m2, err := ProjectFreeCADRuntimeExportManifest(payload)
	if err != nil {
		t.Fatalf("unexpected error on second projection: %v", err)
	}
	for i := range m1.Outputs {
		if m1.Outputs[i].ID != m2.Outputs[i].ID {
			t.Errorf("outputs[%d].id: %q vs %q (not deterministic)", i, m1.Outputs[i].ID, m2.Outputs[i].ID)
		}
	}
}

// Test 19: Duplicate detection operates on normalized contained output paths.
func TestProjectFreeCADRuntimeOutputPath_DuplicateDetectionAfterNormalization(t *testing.T) {
	// "outputs/box.step" and "outputs\\box.step" both normalize to "outputs/box.step".
	// Duplicate detection must fire on the normalized form, not the raw input.
	payload := planner.WriteExportManifestPayload{
		SchemaVersion:  "1.0",
		SourceDocument: "box.FCStd",
		Outputs: []planner.ExportManifestOutput{
			{Type: "step", Filename: "outputs/box.step", Object: "Body"},
			{Type: "step", Filename: "outputs\\box.step", Object: "Body001"},
		},
	}

	manifest, err := ProjectFreeCADRuntimeExportManifest(payload)
	if err == nil {
		t.Fatal("expected duplicate path error after normalization, got nil")
	}
	if manifest != nil {
		t.Error("expected nil manifest on error")
	}
	if !strings.Contains(err.Error(), "duplicates") {
		t.Errorf("error %q does not contain %q", err.Error(), "duplicates")
	}
	if !strings.Contains(err.Error(), "outputs[1].path") {
		t.Errorf("error %q does not identify outputs[1].path", err.Error())
	}
}

// Test 23: Valid explicit target boundary — proves accepted syntax and full projection.
func TestProjectFreeCADRuntimeExportManifest_ValidExplicitTargetBoundary(t *testing.T) {
	for _, target := range []string{"Body.Width", "Sketch.Height", "Pad.Length"} {
		t.Run(target, func(t *testing.T) {
			payload := validProjectionPayload()
			payload.ParameterAssignments[0].Target = target

			manifest, err := ProjectFreeCADRuntimeExportManifest(payload)
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", target, err)
			}
			if manifest == nil {
				t.Fatal("expected non-nil manifest")
			}
			if len(manifest.ParameterAssignments) != 1 {
				t.Fatalf("expected 1 parameterAssignment, got %d", len(manifest.ParameterAssignments))
			}
			pa := manifest.ParameterAssignments[0]
			if pa.Target != target {
				t.Errorf("parameterAssignments[0].target: got %q, want %q", pa.Target, target)
			}
			if _, ok := pa.Value.(float64); !ok {
				t.Errorf("parameterAssignments[0].value: expected float64, got %T", pa.Value)
			}
			if pa.ValueKind != "float" {
				t.Errorf("parameterAssignments[0].valueKind: got %q, want %q", pa.ValueKind, "float")
			}
			if len(manifest.Outputs) != 1 {
				t.Fatalf("expected 1 output, got %d", len(manifest.Outputs))
			}
			if !strings.HasPrefix(manifest.Outputs[0].Path, "outputs/") {
				t.Errorf("outputs[0].path %q is not under outputs/", manifest.Outputs[0].Path)
			}
		})
	}
}

// Test 24: Whitespace beside the dot separator is rejected — regression guard for
// the implementation fix that removed pre-validation trimming of target segments.
func TestProjectFreeCADRuntimeExportManifest_WhitespaceBesideDotRejected(t *testing.T) {
	cases := []struct {
		name        string
		target      string
		errContains string
	}{
		{"space before dot", "Body .Width", "whitespace"},
		{"space after dot", "Body. Width", "whitespace"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := validProjectionPayload()
			payload.ParameterAssignments[0].Target = tc.target

			manifest, err := ProjectFreeCADRuntimeExportManifest(payload)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if manifest != nil {
				t.Error("expected nil manifest on error")
			}
			if !strings.Contains(err.Error(), tc.errContains) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.errContains)
			}
			if !strings.Contains(err.Error(), "target") {
				t.Errorf("error %q does not mention target context", err.Error())
			}
		})
	}
}

// Test 25: Mutation-backed fallback with malformed Object or Property segments fails.
func TestProjectFreeCADRuntimeExportManifest_MutationFallbackMalformedSegments(t *testing.T) {
	cases := []struct {
		name        string
		object      string
		property    string
		errContains string
	}{
		{"object segment contains slash", "Body/Part", "Width", "malformed object segment"},
		{"property segment contains slash", "Body", "Width/Length", "malformed property segment"},
		{"object segment contains backslash", "Body\\Part", "Width", "malformed object segment"},
		{"property segment contains backslash", "Body", "Width\\Length", "malformed property segment"},
		{"object segment contains internal whitespace", "Box Part", "Width", "malformed object segment"},
		{"property segment contains internal whitespace", "Body", "Width Length", "malformed property segment"},
		{"object segment contains null byte", "Body\x00Part", "Width", "malformed object segment"},
		{"property segment contains null byte", "Body", "Width\x00Length", "malformed property segment"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := planner.WriteExportManifestPayload{
				SchemaVersion:  "1.0",
				SourceDocument: "box.FCStd",
				ParameterAssignments: []planner.ExportManifestParameterAssignment{
					{Name: "Width", Value: 50.0, Type: "number", Unit: "mm"},
				},
				AssemblyMutations: &planner.ExportManifestMutationCollection{
					Parameters: []planner.ExportManifestParameterMutation{
						{Object: tc.object, Property: tc.property, ValueParam: "Width"},
					},
				},
				Outputs: []planner.ExportManifestOutput{
					{Type: "step", Filename: "outputs/box.step", Object: "Body"},
				},
			}

			manifest, err := ProjectFreeCADRuntimeExportManifest(payload)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if manifest != nil {
				t.Error("expected nil manifest on error")
			}
			if !strings.Contains(err.Error(), tc.errContains) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.errContains)
			}
			if !strings.Contains(err.Error(), "target") {
				t.Errorf("error %q does not mention target context", err.Error())
			}
		})
	}
}
