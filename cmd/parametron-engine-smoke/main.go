package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/handoff"
	"parametron/internal/engine/job"
	"parametron/internal/engine/metadata"
)

type submissionRequest struct {
	Handoff *handoffPackagePayload `json:"handoff"`
}

type handoffPackagePayload struct {
	JobID      string                        `json:"jobID"`
	ProductKey string                        `json:"productKey"`
	CSV        planner.WriteCSVPayload       `json:"csv"`
	Manifest   manifestPayload               `json:"manifest"`
	CADRuntime *planner.RunCADRuntimePayload `json:"cadRuntime,omitempty"`
	Tables     []metadata.TableInputMetadata `json:"tables,omitempty"`
	Steps      []stepSnapshotPayload         `json:"steps"`
}

type stepSnapshotPayload struct {
	Type       planner.StepType              `json:"type"`
	CSV        *planner.WriteCSVPayload      `json:"csv,omitempty"`
	Manifest   *manifestPayload              `json:"manifest,omitempty"`
	CADRuntime *planner.RunCADRuntimePayload `json:"cadRuntime,omitempty"`
}

type manifestPayload struct {
	ProductKey             string                                      `json:"productKey,omitempty"`
	ManifestFilename       string                                      `json:"manifestFilename"`
	ManifestProjectionMode planner.ExportManifestProjectionMode        `json:"manifestProjectionMode,omitempty"`
	SchemaVersion          string                                      `json:"schemaVersion"`
	PlanHash               string                                      `json:"planHash"`
	Adapter                string                                      `json:"adapter,omitempty"`
	Product                planner.ExportManifestProduct               `json:"product"`
	SourceDocument         string                                      `json:"sourceDocument,omitempty"`
	Inputs                 planner.ExportManifestInputs                `json:"inputs"`
	Values                 map[string]interface{}                      `json:"values"`
	ParameterAssignments   []planner.ExportManifestParameterAssignment `json:"parameterAssignments"`
	Outputs                []planner.ExportManifestOutput              `json:"outputs"`
}

func main() {
	productKey := flag.String("product", "widget", "product key to use in the sample submission")
	jobIDOnly := flag.Bool("job-id-only", false, "print only the deterministic job ID for the sample submission")
	sourceModel := flag.String("source-model", "", "absolute FCStd source model for the aligned submission")
	flag.Parse()

	payload, err := buildSubmission(*productKey, *sourceModel)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if *jobIDOnly {
		fmt.Fprintln(os.Stdout, payload.Handoff.JobID)
		return
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func buildSubmission(productKey, sourceModel string) (*submissionRequest, error) {
	if !filepath.IsAbs(sourceModel) || filepath.Clean(sourceModel) != sourceModel {
		return nil, fmt.Errorf("source-model must be a canonical absolute path")
	}
	const planHash = "task14-aligned-proof-plan"
	steps := []planner.Step{
		{Type: planner.StepWriteCSV, Payload: planner.WriteCSVPayload{
			ProductKey: productKey, Filename: productKey + ".csv",
			Headers: []string{"width"}, Values: []interface{}{42},
		}},
		{Type: planner.StepWriteExportManifest, Payload: planner.WriteExportManifestPayload{
			ProductKey: productKey, ManifestFilename: planner.ExportManifestFilename,
			ManifestProjectionMode: planner.ExportManifestProjectionModeFreeCADRuntimeNative,
			SchemaVersion:          planner.ExportManifestSchemaVersion, PlanHash: planHash, Adapter: "freecad",
			Product: planner.ExportManifestProduct{ID: productKey}, SourceDocument: "source/" + filepath.Base(sourceModel),
			Inputs: planner.ExportManifestInputs{SourceModel: sourceModel}, Values: map[string]interface{}{"width": 42},
			Outputs: []planner.ExportManifestOutput{{Type: "step", Filename: "outputs/" + productKey + ".step", Object: "Body"}},
		}},
		{Type: planner.StepRunCADRuntime, Payload: planner.RunCADRuntimePayload{
			ProductKey: productKey, Adapter: "freecad", ManifestFilename: planner.ExportManifestFilename, ResultFilename: planner.FreeCADRuntimeResultFilename,
		}},
	}
	j, err := job.New(productKey, steps)
	if err != nil {
		return nil, fmt.Errorf("build aligned sample job: %w", err)
	}
	pkg, err := handoff.FromJob(j)
	if err != nil {
		return nil, fmt.Errorf("build aligned handoff package: %w", err)
	}
	return &submissionRequest{Handoff: packageEnvelopeFromPackage(pkg)}, nil
}

func packageEnvelopeFromPackage(pkg *handoff.Package) *handoffPackagePayload {
	if pkg == nil {
		return nil
	}

	req := &handoffPackagePayload{
		JobID:      pkg.JobID,
		ProductKey: pkg.ProductKey,
		CSV:        pkg.CSV,
		Manifest: manifestPayload{
			ProductKey:             pkg.Manifest.ProductKey,
			ManifestFilename:       pkg.Manifest.ManifestFilename,
			ManifestProjectionMode: pkg.Manifest.ManifestProjectionMode,
			SchemaVersion:          pkg.Manifest.SchemaVersion,
			PlanHash:               pkg.Manifest.PlanHash,
			Adapter:                pkg.Manifest.Adapter,
			Product:                pkg.Manifest.Product,
			SourceDocument:         pkg.Manifest.SourceDocument,
			Inputs:                 pkg.Manifest.Inputs,
			Values:                 pkg.Manifest.Values,
			ParameterAssignments:   pkg.Manifest.ParameterAssignments,
			Outputs:                pkg.Manifest.Outputs,
		},
		CADRuntime: pkg.CADRuntime,
		Tables:     handoff.CloneTableInputs(pkg.Tables),
		Steps:      make([]stepSnapshotPayload, 0, len(pkg.Steps)),
	}

	for _, step := range pkg.Steps {
		snapshot := stepSnapshotPayload{Type: step.Type}
		if step.CSV != nil {
			csv := *step.CSV
			snapshot.CSV = &csv
		}
		if step.Manifest != nil {
			manifest := manifestPayload{
				ProductKey:             step.Manifest.ProductKey,
				ManifestFilename:       step.Manifest.ManifestFilename,
				ManifestProjectionMode: step.Manifest.ManifestProjectionMode,
				SchemaVersion:          step.Manifest.SchemaVersion,
				PlanHash:               step.Manifest.PlanHash,
				Adapter:                step.Manifest.Adapter,
				Product:                step.Manifest.Product,
				SourceDocument:         step.Manifest.SourceDocument,
				Inputs:                 step.Manifest.Inputs,
				Values:                 step.Manifest.Values,
				ParameterAssignments:   step.Manifest.ParameterAssignments,
				Outputs:                step.Manifest.Outputs,
			}
			snapshot.Manifest = &manifest
		}
		if step.CADRuntime != nil {
			runtime := *step.CADRuntime
			snapshot.CADRuntime = &runtime
		}
		req.Steps = append(req.Steps, snapshot)
	}

	return req
}
