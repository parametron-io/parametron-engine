package executor

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/handoff"
	"parametron/internal/engine/metadata"
)

func TestExecutionPackage_TablesPreservedThroughExecutorBoundary(t *testing.T) {
	pkg := &handoff.Package{
		JobID:      "job-1",
		ProductKey: "widget",
		CSV: planner.WriteCSVPayload{
			ProductKey: "widget",
			Filename:   "widget.csv",
			Headers:    []string{"width"},
			Values:     []any{42},
		},
		Manifest: planner.WriteExportManifestPayload{
			ProductKey:       "widget",
			ManifestFilename: planner.ExportManifestFilename,
			SchemaVersion:    planner.ExportManifestSchemaVersion,
			PlanHash:         "plan-widget",
			Product:          planner.ExportManifestProduct{ID: "widget"},
			Values: map[string]any{
				"width": 42,
			},
			Outputs: []planner.ExportManifestOutput{
				{Type: "step", Filename: "widget.step", Object: "Body"},
			},
		},
		Tables: []metadata.TableInputMetadata{
			{LogicalID: "labels", Name: "label_catalog", Fingerprint: "bbb"},
			{LogicalID: "fasteners", Name: "fastener_catalog", Fingerprint: "aaa"},
		},
		Steps: []handoff.StepSnapshot{
			{
				Type: planner.StepWriteCSV,
				CSV: &planner.WriteCSVPayload{
					ProductKey: "widget",
					Filename:   "widget.csv",
					Headers:    []string{"width"},
					Values:     []any{42},
				},
			},
			{
				Type: planner.StepWriteExportManifest,
				Manifest: &planner.WriteExportManifestPayload{
					ProductKey:       "widget",
					ManifestFilename: planner.ExportManifestFilename,
					SchemaVersion:    planner.ExportManifestSchemaVersion,
					PlanHash:         "plan-widget",
					Product:          planner.ExportManifestProduct{ID: "widget"},
					Values: map[string]any{
						"width": 42,
					},
					Outputs: []planner.ExportManifestOutput{
						{Type: "step", Filename: "widget.step", Object: "Body"},
					},
				},
			},
		},
	}

	exec := New(&recordingAdapter{})
	if err := exec.ExecutePackage(context.Background(), pkg); err != nil {
		t.Fatalf("ExecutePackage returned error: %v", err)
	}

	gotPkg := exec.Package()
	if gotPkg == nil {
		t.Fatal("expected preserved handoff package")
	}

	wantTables := []metadata.TableInputMetadata{
		{LogicalID: "fasteners", Name: "fastener_catalog", Fingerprint: "aaa"},
		{LogicalID: "labels", Name: "label_catalog", Fingerprint: "bbb"},
	}
	if !reflect.DeepEqual(gotPkg.Tables, wantTables) {
		t.Fatalf("preserved tables mismatch:\ngot=%#v\nwant=%#v", gotPkg.Tables, wantTables)
	}

	if !reflect.DeepEqual(pkg.Tables, []metadata.TableInputMetadata{
		{LogicalID: "labels", Name: "label_catalog", Fingerprint: "bbb"},
		{LogicalID: "fasteners", Name: "fastener_catalog", Fingerprint: "aaa"},
	}) {
		t.Fatalf("input package tables were mutated: %#v", pkg.Tables)
	}

	runMetadata := metadata.Build(metadata.BuildInput{
		PlanHash:        "plan-widget",
		DSLHash:         "dsl-widget",
		Tables:          gotPkg.Tables,
		StartedAt:       time.Date(2026, 4, 14, 9, 0, 0, 0, time.UTC),
		EndedAt:         time.Date(2026, 4, 14, 9, 0, 1, 0, time.UTC),
		WorkerCount:     1,
		MaxRetries:      DefaultMaxRetries,
		StepTimeoutSecs: int(DefaultStepTimeout / time.Second),
	})
	if !reflect.DeepEqual(runMetadata.Tables, wantTables) {
		t.Fatalf("metadata tables mismatch:\ngot=%#v\nwant=%#v", runMetadata.Tables, wantTables)
	}

	firstJSON, err := json.Marshal(runMetadata)
	if err != nil {
		t.Fatalf("marshal first metadata: %v", err)
	}
	secondJSON, err := json.Marshal(metadata.Build(metadata.BuildInput{
		PlanHash:        "plan-widget",
		DSLHash:         "dsl-widget",
		Tables:          exec.Package().Tables,
		StartedAt:       time.Date(2026, 4, 14, 9, 0, 0, 0, time.UTC),
		EndedAt:         time.Date(2026, 4, 14, 9, 0, 1, 0, time.UTC),
		WorkerCount:     1,
		MaxRetries:      DefaultMaxRetries,
		StepTimeoutSecs: int(DefaultStepTimeout / time.Second),
	}))
	if err != nil {
		t.Fatalf("marshal second metadata: %v", err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("metadata JSON changed across repeated runs:\nfirst=%s\nsecond=%s", firstJSON, secondJSON)
	}
}
