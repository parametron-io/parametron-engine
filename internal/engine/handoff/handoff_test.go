package handoff

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/job"
	"parametron/internal/engine/metadata"
)

func TestFromJob_Success(t *testing.T) {
	tests := []struct {
		name           string
		steps          []planner.Step
		wantCADRuntime bool
		wantArtifacts  []string
		wantStepTypes  []planner.StepType
		wantProductKey string
	}{
		{
			name: "freecad style job",
			steps: []planner.Step{
				{
					Type: planner.StepWriteCSV,
					Payload: planner.WriteCSVPayload{
						ProductKey: "widget",
						Filename:   "widget.csv",
						Headers:    []string{"width", "height"},
						Values:     []interface{}{42, 12},
					},
				},
				{
					Type: planner.StepWriteExportManifest,
					Payload: planner.WriteExportManifestPayload{
						ProductKey:       "widget",
						ManifestFilename: planner.ExportManifestFilename,
						SchemaVersion:    planner.ExportManifestSchemaVersion,
						PlanHash:         "plan-widget",
						Adapter:          "freecad",
						Product:          planner.ExportManifestProduct{ID: "widget"},
						Inputs:           planner.ExportManifestInputs{SourceModel: "widget.FCStd"},
						Values: map[string]interface{}{
							"width": 42,
						},
						Outputs: []planner.ExportManifestOutput{
							{Type: "step", Filename: "widget.step", Object: "Body"},
						},
					},
				},
				{
					Type: planner.StepRunCADRuntime,
					Payload: planner.RunCADRuntimePayload{
						ProductKey:       "widget",
						Adapter:          "freecad",
						ManifestFilename: planner.ExportManifestFilename,
						ResultFilename:   "result.json",
					},
				},
			},
			wantCADRuntime: true,
			wantArtifacts:  []string{"widget.csv", planner.ExportManifestFilename, "result.json", "widget.step"},
			wantStepTypes:  []planner.StepType{planner.StepWriteCSV, planner.StepWriteExportManifest, planner.StepRunCADRuntime},
			wantProductKey: "widget",
		},
		{
			name: "manifest only execution",
			steps: []planner.Step{
				{
					Type: planner.StepWriteCSV,
					Payload: planner.WriteCSVPayload{
						ProductKey: "plate",
						Filename:   "plate.csv",
					},
				},
				{
					Type: planner.StepWriteExportManifest,
					Payload: planner.WriteExportManifestPayload{
						ProductKey:       "plate",
						ManifestFilename: planner.ExportManifestFilename,
						SchemaVersion:    planner.ExportManifestSchemaVersion,
						PlanHash:         "plan-plate",
						Product:          planner.ExportManifestProduct{ID: "plate"},
						Values: map[string]interface{}{
							"thickness": 3,
						},
						Outputs: []planner.ExportManifestOutput{
							{Type: "pdf", Filename: "plate.pdf"},
						},
					},
				},
			},
			wantCADRuntime: false,
			wantArtifacts:  []string{"plate.csv", planner.ExportManifestFilename, "plate.pdf"},
			wantStepTypes:  []planner.StepType{planner.StepWriteCSV, planner.StepWriteExportManifest},
			wantProductKey: "plate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := mustJob(t, tt.wantProductKey, tt.steps)

			pkg, err := FromJob(j)
			if err != nil {
				t.Fatalf("FromJob returned error: %v", err)
			}

			if pkg.JobID != j.ID {
				t.Fatalf("job id mismatch: got %q want %q", pkg.JobID, j.ID)
			}
			if pkg.ProductKey != tt.wantProductKey {
				t.Fatalf("product key mismatch: got %q want %q", pkg.ProductKey, tt.wantProductKey)
			}
			if pkg.RequiresCADRuntime() != tt.wantCADRuntime {
				t.Fatalf("RequiresCADRuntime mismatch: got %v want %v", pkg.RequiresCADRuntime(), tt.wantCADRuntime)
			}
			if pkg.ManifestFilename() != planner.ExportManifestFilename {
				t.Fatalf("ManifestFilename mismatch: got %q want %q", pkg.ManifestFilename(), planner.ExportManifestFilename)
			}
			if !reflect.DeepEqual(pkg.ExpectedArtifacts(), tt.wantArtifacts) {
				t.Fatalf("ExpectedArtifacts mismatch:\ngot=%#v\nwant=%#v", pkg.ExpectedArtifacts(), tt.wantArtifacts)
			}

			gotTypes := make([]planner.StepType, 0, len(pkg.Steps))
			for _, step := range pkg.Steps {
				gotTypes = append(gotTypes, step.Type)
			}
			if !reflect.DeepEqual(gotTypes, tt.wantStepTypes) {
				t.Fatalf("step types mismatch:\ngot=%#v\nwant=%#v", gotTypes, tt.wantStepTypes)
			}
		})
	}
}

func TestFromJob_ValidationFailures(t *testing.T) {
	tests := []struct {
		name    string
		job     *job.Job
		wantErr error
	}{
		{
			name:    "nil job",
			job:     nil,
			wantErr: ErrNilJob,
		},
		{
			name: "missing csv step",
			job: mustJob(t, "widget", []planner.Step{
				manifestStep("widget", planner.ExportManifestFilename, planner.ExportManifestSchemaVersion, "widget.step"),
			}),
			wantErr: ErrMissingWriteCSV,
		},
		{
			name: "missing manifest step",
			job: mustJob(t, "widget", []planner.Step{
				csvStep("widget", "widget.csv"),
			}),
			wantErr: ErrMissingManifest,
		},
		{
			name: "duplicate manifest step",
			job: mustJob(t, "widget", []planner.Step{
				csvStep("widget", "widget.csv"),
				manifestStep("widget", "first.json", planner.ExportManifestSchemaVersion, "widget.step"),
				manifestStep("widget", "second.json", planner.ExportManifestSchemaVersion, "widget.step"),
			}),
			wantErr: ErrDuplicateManifest,
		},
		{
			name: "duplicate csv step",
			job: mustJob(t, "widget", []planner.Step{
				csvStep("widget", "one.csv"),
				csvStep("widget", "two.csv"),
				manifestStep("widget", planner.ExportManifestFilename, planner.ExportManifestSchemaVersion, "widget.step"),
			}),
			wantErr: ErrDuplicateWriteCSV,
		},
		{
			name: "mismatched manifest product identity",
			job: mustJob(t, "widget", []planner.Step{
				csvStep("widget", "widget.csv"),
				{
					Type: planner.StepWriteExportManifest,
					Payload: planner.WriteExportManifestPayload{
						ProductKey:       "widget",
						ManifestFilename: planner.ExportManifestFilename,
						SchemaVersion:    planner.ExportManifestSchemaVersion,
						Product:          planner.ExportManifestProduct{ID: "other"},
						Outputs: []planner.ExportManifestOutput{
							{Type: "step", Filename: "widget.step", Object: "Body"},
						},
					},
				},
			}),
			wantErr: ErrMismatchedProductKey,
		},
		{
			name: "empty manifest filename",
			job: mustJob(t, "widget", []planner.Step{
				csvStep("widget", "widget.csv"),
				manifestStep("widget", "", planner.ExportManifestSchemaVersion, "widget.step"),
			}),
			wantErr: ErrManifestFilenameEmpty,
		},
		{
			name: "empty manifest schema version",
			job: mustJob(t, "widget", []planner.Step{
				csvStep("widget", "widget.csv"),
				manifestStep("widget", planner.ExportManifestFilename, "", "widget.step"),
			}),
			wantErr: ErrSchemaVersionEmpty,
		},
		{
			name: "manifest outputs missing",
			job: mustJob(t, "widget", []planner.Step{
				csvStep("widget", "widget.csv"),
				{
					Type: planner.StepWriteExportManifest,
					Payload: planner.WriteExportManifestPayload{
						ProductKey:       "widget",
						ManifestFilename: planner.ExportManifestFilename,
						SchemaVersion:    planner.ExportManifestSchemaVersion,
						Product:          planner.ExportManifestProduct{ID: "widget"},
					},
				},
			}),
			wantErr: ErrManifestOutputsEmpty,
		},
		{
			name: "manifest outputs reject unsupported legacy type",
			job: mustJob(t, "widget", []planner.Step{
				csvStep("widget", "widget.csv"),
				{
					Type: planner.StepWriteExportManifest,
					Payload: planner.WriteExportManifestPayload{
						ProductKey:       "widget",
						ManifestFilename: planner.ExportManifestFilename,
						SchemaVersion:    planner.ExportManifestSchemaVersion,
						Product:          planner.ExportManifestProduct{ID: "widget"},
						Outputs: []planner.ExportManifestOutput{
							{Type: "stl", Filename: "widget.stl"},
						},
					},
				},
			}),
			wantErr: ErrManifestOutputInvalid,
		},
		{
			name: "unsupported step shape",
			job: mustJob(t, "widget", []planner.Step{
				{
					Type: planner.StepWriteCSV,
					Payload: planner.RunCADRuntimePayload{
						ProductKey:       "widget",
						Adapter:          "freecad",
						ManifestFilename: planner.ExportManifestFilename,
						ResultFilename:   "result.json",
					},
				},
				manifestStep("widget", planner.ExportManifestFilename, planner.ExportManifestSchemaVersion, "widget.step"),
			}),
			wantErr: ErrUnsupportedStep,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := FromJob(tt.job)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestFromJob_RejectsUnknownStepType(t *testing.T) {
	j := mustJob(t, "widget", []planner.Step{
		csvStep("widget", "widget.csv"),
		{
			Type: "UnknownStep",
			Payload: planner.WriteCSVPayload{
				ProductKey: "widget",
				Filename:   "widget.csv",
			},
		},
		manifestStep("widget", planner.ExportManifestFilename, planner.ExportManifestSchemaVersion, "widget.step"),
	})

	_, err := FromJob(j)
	if !errors.Is(err, ErrUnsupportedStep) {
		t.Fatalf("expected ErrUnsupportedStep, got %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "UnknownStep") {
		t.Fatalf("expected unknown step type in error text, got %v", err)
	}
}

func TestFromJob_Determinism(t *testing.T) {
	steps := []planner.Step{
		{
			Type: planner.StepWriteCSV,
			Payload: planner.WriteCSVPayload{
				ProductKey: "widget",
				Filename:   "widget.csv",
				Headers:    []string{"alpha", "beta"},
				Values:     []interface{}{1, map[string]interface{}{"x": true, "y": "yes"}},
			},
		},
		{
			Type: planner.StepWriteExportManifest,
			Payload: planner.WriteExportManifestPayload{
				ProductKey:       "widget",
				ManifestFilename: planner.ExportManifestFilename,
				SchemaVersion:    planner.ExportManifestSchemaVersion,
				PlanHash:         "plan-widget",
				Adapter:          "freecad",
				Product:          planner.ExportManifestProduct{ID: "widget"},
				Values: map[string]interface{}{
					"alpha": 1,
					"beta": map[string]interface{}{
						"x": true,
						"y": "yes",
					},
				},
				Outputs: []planner.ExportManifestOutput{
					{Type: "step", Filename: "widget.step", Object: "Body"},
				},
			},
		},
		{
			Type: planner.StepRunCADRuntime,
			Payload: planner.RunCADRuntimePayload{
				ProductKey:       "widget",
				Adapter:          "freecad",
				ManifestFilename: planner.ExportManifestFilename,
				ResultFilename:   "result.json",
			},
		},
	}

	first := mustPackage(t, mustJob(t, "widget", steps))
	second := mustPackage(t, mustJob(t, "widget", steps))
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("packages are not equal:\nfirst=%#v\nsecond=%#v", first, second)
	}

	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal first package: %v", err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("marshal second package: %v", err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("marshal output mismatch:\nfirst=%s\nsecond=%s", firstJSON, secondJSON)
	}

	mapOrderA := mustPackage(t, mustJob(t, "widget", []planner.Step{
		csvStep("widget", "widget.csv"),
		{
			Type: planner.StepWriteExportManifest,
			Payload: planner.WriteExportManifestPayload{
				ProductKey:       "widget",
				ManifestFilename: planner.ExportManifestFilename,
				SchemaVersion:    planner.ExportManifestSchemaVersion,
				Product:          planner.ExportManifestProduct{ID: "widget"},
				Values: map[string]interface{}{
					"alpha": 1,
					"beta": map[string]interface{}{
						"x": true,
						"y": "yes",
					},
				},
				Outputs: []planner.ExportManifestOutput{
					{Type: "step", Filename: "widget.step", Object: "Body"},
				},
			},
		},
	}))
	mapOrderB := mustPackage(t, mustJob(t, "widget", []planner.Step{
		csvStep("widget", "widget.csv"),
		{
			Type: planner.StepWriteExportManifest,
			Payload: planner.WriteExportManifestPayload{
				ProductKey:       "widget",
				ManifestFilename: planner.ExportManifestFilename,
				SchemaVersion:    planner.ExportManifestSchemaVersion,
				Product:          planner.ExportManifestProduct{ID: "widget"},
				Values: map[string]interface{}{
					"beta": map[string]interface{}{
						"y": "yes",
						"x": true,
					},
					"alpha": 1,
				},
				Outputs: []planner.ExportManifestOutput{
					{Type: "step", Filename: "widget.step", Object: "Body"},
				},
			},
		},
	}))
	mapJSONA, err := json.Marshal(mapOrderA)
	if err != nil {
		t.Fatalf("marshal mapOrderA: %v", err)
	}
	mapJSONB, err := json.Marshal(mapOrderB)
	if err != nil {
		t.Fatalf("marshal mapOrderB: %v", err)
	}
	if string(mapJSONA) != string(mapJSONB) {
		t.Fatalf("map insertion order changed JSON:\na=%s\nb=%s", mapJSONA, mapJSONB)
	}
}

func TestPackagePlan_ReconstructsOriginalOrderAndPayloads(t *testing.T) {
	steps := []planner.Step{
		{
			Type: planner.StepWriteCSV,
			Payload: planner.WriteCSVPayload{
				ProductKey: "widget",
				Filename:   "01-widget.csv",
				Headers:    []string{"first"},
				Values:     []interface{}{1},
			},
		},
		{
			Type: planner.StepWriteExportManifest,
			Payload: planner.WriteExportManifestPayload{
				ProductKey:       "widget",
				ManifestFilename: "02-widget.json",
				SchemaVersion:    planner.ExportManifestSchemaVersion,
				PlanHash:         "abc123",
				Adapter:          "freecad",
				Product:          planner.ExportManifestProduct{ID: "widget"},
				Values: map[string]interface{}{
					"first": 1,
				},
				AssemblyMutations: &planner.ExportManifestMutationCollection{
					Parameters: []planner.ExportManifestParameterMutation{
						{Object: "Assembly", Property: "Length", ValueParam: "first", Type: "number", Unit: "mm"},
					},
					Properties: []planner.ExportManifestPropertyMutation{
						{Object: "Assembly", Property: "Metadata", Value: map[string]interface{}{"label": "primary"}},
					},
				},
				PartMutations: &planner.ExportManifestMutationCollection{
					Suppression: []planner.ExportManifestSuppressionMutation{
						{Object: "Pocket", Suppressed: true},
					},
				},
				Outputs: []planner.ExportManifestOutput{
					{Type: "step", Filename: "03-widget.step", Object: "Body"},
				},
			},
		},
		{
			Type: planner.StepRunCADRuntime,
			Payload: planner.RunCADRuntimePayload{
				ProductKey:       "widget",
				Adapter:          "freecad",
				ManifestFilename: "02-widget.json",
				ResultFilename:   "result.json",
			},
		},
	}

	pkg := mustPackage(t, mustJob(t, "widget", steps))
	plan := pkg.Plan()
	if plan == nil {
		t.Fatal("Plan returned nil")
	}
	if !reflect.DeepEqual(plan.Steps, steps) {
		t.Fatalf("reconstructed plan mismatch:\ngot=%#v\nwant=%#v", plan.Steps, steps)
	}
}

func TestPackageWithTables_DeterministicJSONAndPlanCompatibility(t *testing.T) {
	pkgA := mustPackage(t, mustJob(t, "widget", []planner.Step{
		csvStep("widget", "widget.csv"),
		manifestStep("widget", planner.ExportManifestFilename, planner.ExportManifestSchemaVersion, "widget.step"),
		runStep("widget", "widget.csv", planner.ExportManifestFilename),
	}))
	pkgA.Tables = CloneTableInputs([]metadata.TableInputMetadata{
		{LogicalID: "labels", Name: "label_catalog", Fingerprint: "bbb"},
		{LogicalID: "fasteners", Name: "fastener_catalog", Fingerprint: "aaa"},
	})

	pkgB := mustPackage(t, mustJob(t, "widget", []planner.Step{
		csvStep("widget", "widget.csv"),
		manifestStep("widget", planner.ExportManifestFilename, planner.ExportManifestSchemaVersion, "widget.step"),
		runStep("widget", "widget.csv", planner.ExportManifestFilename),
	}))
	pkgB.Tables = CloneTableInputs([]metadata.TableInputMetadata{
		{LogicalID: "fasteners", Name: "fastener_catalog", Fingerprint: "aaa"},
		{LogicalID: "labels", Name: "label_catalog", Fingerprint: "bbb"},
	})

	if !reflect.DeepEqual(pkgA.Tables, pkgB.Tables) {
		t.Fatalf("normalized tables mismatch:\na=%#v\nb=%#v", pkgA.Tables, pkgB.Tables)
	}

	jsonA, err := json.Marshal(pkgA)
	if err != nil {
		t.Fatalf("marshal pkgA: %v", err)
	}
	jsonB, err := json.Marshal(pkgB)
	if err != nil {
		t.Fatalf("marshal pkgB: %v", err)
	}
	if string(jsonA) != string(jsonB) {
		t.Fatalf("table ordering changed JSON:\na=%s\nb=%s", jsonA, jsonB)
	}

	plan := pkgA.Plan()
	if plan == nil || len(plan.Steps) != 3 {
		t.Fatalf("unexpected plan reconstruction: %#v", plan)
	}
	if !reflect.DeepEqual(plan.Steps, mustJob(t, "widget", []planner.Step{
		csvStep("widget", "widget.csv"),
		manifestStep("widget", planner.ExportManifestFilename, planner.ExportManifestSchemaVersion, "widget.step"),
		runStep("widget", "widget.csv", planner.ExportManifestFilename),
	}).Steps()) {
		t.Fatal("plan reconstruction changed when tables were present")
	}
}

func TestCloneTableInputs_DeterministicAndDefensive(t *testing.T) {
	source := []metadata.TableInputMetadata{
		{LogicalID: "labels", Name: "label_catalog", Fingerprint: "bbb"},
		{LogicalID: "fasteners", Name: "fastener_catalog", Fingerprint: "aaa"},
	}

	cloned := CloneTableInputs(source)
	want := []metadata.TableInputMetadata{
		{LogicalID: "fasteners", Name: "fastener_catalog", Fingerprint: "aaa"},
		{LogicalID: "labels", Name: "label_catalog", Fingerprint: "bbb"},
	}
	if !reflect.DeepEqual(cloned, want) {
		t.Fatalf("CloneTableInputs mismatch:\ngot=%#v\nwant=%#v", cloned, want)
	}

	source[0].Fingerprint = "mutated"
	if reflect.DeepEqual(cloned, source) {
		t.Fatalf("CloneTableInputs did not make a defensive copy: %#v", cloned)
	}

	if got := CloneTableInputs(nil); got != nil {
		t.Fatalf("expected nil clone for nil input, got %#v", got)
	}
	if got := CloneTableInputs([]metadata.TableInputMetadata{}); got != nil {
		t.Fatalf("expected nil clone for empty input, got %#v", got)
	}
}

func TestValidateTableInputs(t *testing.T) {
	tests := []struct {
		name    string
		tables  []metadata.TableInputMetadata
		wantErr error
	}{
		{
			name: "valid tables",
			tables: []metadata.TableInputMetadata{
				{LogicalID: "fasteners", Name: "fastener_catalog", Fingerprint: "aaa"},
				{LogicalID: "labels", Name: "label_catalog", Fingerprint: "bbb"},
			},
		},
		{
			name: "empty logical id",
			tables: []metadata.TableInputMetadata{
				{Name: "fastener_catalog", Fingerprint: "aaa"},
			},
			wantErr: ErrTableLogicalIDEmpty,
		},
		{
			name: "empty name",
			tables: []metadata.TableInputMetadata{
				{LogicalID: "fasteners", Fingerprint: "aaa"},
			},
			wantErr: ErrTableNameEmpty,
		},
		{
			name: "empty fingerprint",
			tables: []metadata.TableInputMetadata{
				{LogicalID: "fasteners", Name: "fastener_catalog"},
			},
			wantErr: ErrTableFingerprintEmpty,
		},
		{
			name: "duplicate logical id",
			tables: []metadata.TableInputMetadata{
				{LogicalID: "fasteners", Name: "fastener_catalog", Fingerprint: "aaa"},
				{LogicalID: "fasteners", Name: "other_catalog", Fingerprint: "bbb"},
			},
			wantErr: ErrDuplicateTableLogicalID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTableInputs(tt.tables)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ValidateTableInputs error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestPackageWithoutTables_OmitsTablesFromJSON(t *testing.T) {
	pkg := mustPackage(t, mustJob(t, "widget", []planner.Step{
		csvStep("widget", "widget.csv"),
		manifestStep("widget", planner.ExportManifestFilename, planner.ExportManifestSchemaVersion, "widget.step"),
	}))

	data, err := json.Marshal(pkg)
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}
	if strings.Contains(string(data), `"tables"`) {
		t.Fatalf("expected tables to be omitted, got %s", data)
	}
}

func TestFromJob_ManifestMutationsRoundTripThroughJSON(t *testing.T) {
	steps := []planner.Step{
		csvStep("widget", "widget.csv"),
		{
			Type: planner.StepWriteExportManifest,
			Payload: planner.WriteExportManifestPayload{
				ProductKey:       "widget",
				ManifestFilename: planner.ExportManifestFilename,
				SchemaVersion:    planner.ExportManifestSchemaVersion,
				PlanHash:         "plan-widget",
				Adapter:          "freecad",
				Product:          planner.ExportManifestProduct{ID: "widget"},
				Values:           map[string]interface{}{"width": 42.0},
				AssemblyMutations: &planner.ExportManifestMutationCollection{
					Parameters: []planner.ExportManifestParameterMutation{
						{Object: "Assembly", Property: "Length", ValueParam: "width", Type: "number", Unit: "mm"},
					},
					Properties: []planner.ExportManifestPropertyMutation{
						{Object: "Assembly", Property: "Metadata", Value: map[string]interface{}{"kind": "fixture"}},
					},
				},
				PartMutations: &planner.ExportManifestMutationCollection{
					Suppression: []planner.ExportManifestSuppressionMutation{
						{Object: "Pocket", Suppressed: true},
					},
				},
				Outputs: []planner.ExportManifestOutput{
					{Type: "step", Filename: "widget.step", Object: "Body"},
				},
			},
		},
	}

	pkg := mustPackage(t, mustJob(t, "widget", steps))
	data, err := json.Marshal(pkg)
	if err != nil {
		t.Fatalf("marshal package: %v", err)
	}

	var roundTrip Package
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("unmarshal package: %v", err)
	}

	if roundTrip.JobID != pkg.JobID {
		t.Fatalf("job id mismatch: got %q want %q", roundTrip.JobID, pkg.JobID)
	}
	if roundTrip.ProductKey != pkg.ProductKey {
		t.Fatalf("product key mismatch: got %q want %q", roundTrip.ProductKey, pkg.ProductKey)
	}
	if !reflect.DeepEqual(roundTrip.Manifest.AssemblyMutations, pkg.Manifest.AssemblyMutations) {
		t.Fatalf("assemblyMutations mismatch after JSON round trip:\ngot=%#v\nwant=%#v", roundTrip.Manifest.AssemblyMutations, pkg.Manifest.AssemblyMutations)
	}
	if !reflect.DeepEqual(roundTrip.Manifest.PartMutations, pkg.Manifest.PartMutations) {
		t.Fatalf("partMutations mismatch after JSON round trip:\ngot=%#v\nwant=%#v", roundTrip.Manifest.PartMutations, pkg.Manifest.PartMutations)
	}
	if roundTrip.Manifest.ManifestFilename != "" {
		t.Fatalf("expected manifestFilename to be omitted from JSON payload, got %q", roundTrip.Manifest.ManifestFilename)
	}
	if roundTrip.Manifest.ProductKey != "" {
		t.Fatalf("expected productKey to be omitted from JSON payload, got %q", roundTrip.Manifest.ProductKey)
	}
}

func mustPackage(t *testing.T, j *job.Job) *Package {
	t.Helper()

	pkg, err := FromJob(j)
	if err != nil {
		t.Fatalf("FromJob returned error: %v", err)
	}
	return pkg
}

func mustJob(t *testing.T, productKey string, steps []planner.Step) *job.Job {
	t.Helper()

	j, err := job.New(productKey, steps)
	if err != nil {
		t.Fatalf("job.New returned error: %v", err)
	}
	return j
}

func csvStep(productKey, filename string) planner.Step {
	return planner.Step{
		Type: planner.StepWriteCSV,
		Payload: planner.WriteCSVPayload{
			ProductKey: productKey,
			Filename:   filename,
		},
	}
}

func manifestStep(productKey, filename, schemaVersion, outputFilename string) planner.Step {
	return planner.Step{
		Type: planner.StepWriteExportManifest,
		Payload: planner.WriteExportManifestPayload{
			ProductKey:       productKey,
			ManifestFilename: filename,
			SchemaVersion:    schemaVersion,
			PlanHash:         "plan-" + productKey,
			Adapter:          "freecad",
			Product:          planner.ExportManifestProduct{ID: productKey},
			Values: map[string]interface{}{
				"width": 42,
			},
			Outputs: []planner.ExportManifestOutput{
				{Type: "step", Filename: outputFilename, Object: "Body"},
			},
		},
	}
}

func runStep(productKey, _, manifestFilename string) planner.Step {
	return planner.Step{
		Type: planner.StepRunCADRuntime,
		Payload: planner.RunCADRuntimePayload{
			ProductKey:       productKey,
			Adapter:          "freecad",
			ManifestFilename: manifestFilename,
			ResultFilename:   "result.json",
		},
	}
}
