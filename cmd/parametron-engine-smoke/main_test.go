package main

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/handoff"
	"parametron/internal/engine/job"
	"parametron/internal/engine/metadata"
)

func TestBuildSubmission_IsCanonicalAndJSONRoundTrips(t *testing.T) {
	source := filepath.Join(t.TempDir(), "box.FCStd")
	got, err := buildSubmission("widget", source)
	if err != nil {
		t.Fatal(err)
	}
	h := got.Handoff
	if h.CADRuntime == nil || h.CADRuntime.Adapter != "freecad" {
		t.Fatalf("unexpected runtime shape: %#v", h)
	}
	if h.Manifest.ManifestProjectionMode != planner.ExportManifestProjectionModeFreeCADRuntimeNative ||
		h.Manifest.Inputs.SourceModel != source || h.Manifest.SourceDocument != "source/box.FCStd" ||
		h.Manifest.Outputs[0].Object != "Body" {
		t.Fatalf("unexpected aligned manifest: %#v", h.Manifest)
	}
	j, err := job.New(h.ProductKey, (&handoff.Package{
		JobID: h.JobID, ProductKey: h.ProductKey, CSV: h.CSV,
		Manifest: planner.WriteExportManifestPayload{
			ProductKey: h.Manifest.ProductKey, ManifestFilename: h.Manifest.ManifestFilename,
			ManifestProjectionMode: h.Manifest.ManifestProjectionMode, SchemaVersion: h.Manifest.SchemaVersion,
			PlanHash: h.Manifest.PlanHash, Adapter: h.Manifest.Adapter, Product: h.Manifest.Product,
			SourceDocument: h.Manifest.SourceDocument, Inputs: h.Manifest.Inputs, Values: h.Manifest.Values,
			ParameterAssignments: h.Manifest.ParameterAssignments, Outputs: h.Manifest.Outputs,
		},
		CADRuntime: h.CADRuntime,
		Steps: []handoff.StepSnapshot{
			{Type: planner.StepWriteCSV, CSV: &h.CSV},
			{Type: planner.StepWriteExportManifest, Manifest: &planner.WriteExportManifestPayload{
				ProductKey: h.Manifest.ProductKey, ManifestFilename: h.Manifest.ManifestFilename,
				ManifestProjectionMode: h.Manifest.ManifestProjectionMode, SchemaVersion: h.Manifest.SchemaVersion,
				PlanHash: h.Manifest.PlanHash, Adapter: h.Manifest.Adapter, Product: h.Manifest.Product,
				SourceDocument: h.Manifest.SourceDocument, Inputs: h.Manifest.Inputs, Values: h.Manifest.Values,
				ParameterAssignments: h.Manifest.ParameterAssignments, Outputs: h.Manifest.Outputs,
			}},
			{Type: planner.StepRunCADRuntime, CADRuntime: h.CADRuntime},
		},
	}).Plan().Steps)
	if err != nil {
		t.Fatal(err)
	}
	if h.JobID != j.ID {
		t.Fatalf("job ID=%q want=%q", h.JobID, j.ID)
	}
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var decoded submissionRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Handoff.JobID != h.JobID {
		t.Fatalf("round-trip job ID=%q want=%q", decoded.Handoff.JobID, h.JobID)
	}
}

func TestPackageEnvelopeFromPackage_PreservesTables(t *testing.T) {
	pkg := &handoff.Package{
		JobID:      "job-1",
		ProductKey: "widget",
		CSV: planner.WriteCSVPayload{
			ProductKey: "widget",
			Filename:   "widget.csv",
		},
		Manifest: planner.WriteExportManifestPayload{
			ProductKey:       "widget",
			ManifestFilename: planner.ExportManifestFilename,
			SchemaVersion:    planner.ExportManifestSchemaVersion,
			Product:          planner.ExportManifestProduct{ID: "widget"},
			Outputs: []planner.ExportManifestOutput{
				{Type: "step", Filename: "widget.step", Object: "Body"},
			},
		},
		Tables: []metadata.TableInputMetadata{
			{LogicalID: "labels", Name: "label_catalog", Fingerprint: "bbb"},
			{LogicalID: "fasteners", Name: "fastener_catalog", Fingerprint: "aaa"},
		},
	}

	env := packageEnvelopeFromPackage(pkg)

	if len(env.Tables) != 2 {
		t.Fatalf("expected 2 tables, got %d", len(env.Tables))
	}
	if env.Tables[0].LogicalID != "fasteners" || env.Tables[1].LogicalID != "labels" {
		t.Fatalf("unexpected table ordering: %#v", env.Tables)
	}
}
