package freecad

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/adapter"
	"parametron/internal/engine/runtimecap"
)

// FreeCADAdapter writes Engine-owned CSV and export-manifest plan artifacts.
// CAD execution is delegated through the external runtime capability contract.
type FreeCADAdapter struct {
	outputDir         string
	runtimeCapability runtimecap.Capability
}

// NewFreeCADAdapter creates a new instance of FreeCADAdapter.
func NewFreeCADAdapter(outputDir string) *FreeCADAdapter {
	return &FreeCADAdapter{
		outputDir:         outputDir,
		runtimeCapability: runtimecap.NewExternalCapability(),
	}
}

// Run executes a single plan step sequentially.
func (a *FreeCADAdapter) Run(ctx context.Context, step planner.Step) error {
	switch step.Type {
	case planner.StepWriteCSV:
		return adapter.ExecuteWriteCSV(a.outputDir, step)
	case planner.StepWriteExportManifest:
		return a.executeWriteExportManifest(step)
	default:
		return fmt.Errorf("unknown step type: %s", step.Type)
	}
}

func (a *FreeCADAdapter) runFreeCADRuntimeExecution(ctx context.Context, req FreeCADRuntimeExecutionRequest) (*FreeCADRuntimeExecutionResult, error) {
	return invokeFreeCADRuntime(a.runtimeCapability, ctx, req)
}

func (a *FreeCADAdapter) executeWriteExportManifest(step planner.Step) error {
	payload, ok := step.Payload.(planner.WriteExportManifestPayload)
	if !ok {
		return fmt.Errorf("invalid payload type for WriteExportManifest: %T", step.Payload)
	}
	if payload.ManifestFilename == "" {
		return fmt.Errorf("manifest filename is empty")
	}
	if payload.SchemaVersion == "" {
		return fmt.Errorf("schema version is empty")
	}
	if payload.PlanHash == "" {
		return fmt.Errorf("plan hash is empty")
	}
	freeCADRuntimeNative := isFreeCADRuntimeNativeManifest(payload)
	if len(payload.Outputs) == 0 && !freeCADRuntimeNative {
		return fmt.Errorf("manifest outputs must contain at least one item")
	}
	if err := adapter.ValidateGenericExportManifest(payload, strings.EqualFold(strings.TrimSpace(payload.Adapter), "freecad")); err != nil {
		return err
	}
	freeCADBacked := strings.EqualFold(strings.TrimSpace(payload.Adapter), "freecad") || freeCADRuntimeNative
	if freeCADBacked {
		if err := planner.ValidateFreeCADCADTargetMappings(payload.ParameterAssignments, payload.AssemblyMutations, payload.PartMutations); err != nil {
			return err
		}
	}

	if err := os.MkdirAll(a.outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	data, err := marshalExportManifestPayload(payload)
	if err != nil {
		return err
	}

	filePath := filepath.Join(a.outputDir, payload.ManifestFilename)
	errorPrefix := ""
	if freeCADRuntimeNative {
		errorPrefix = "write FreeCAD runtime export manifest"
	}
	if err := adapter.WriteExportManifestFile(a.outputDir, filePath, data, errorPrefix); err != nil {
		return err
	}

	slog.Info("Generated exporter manifest", "path", filePath)
	return nil
}

func marshalExportManifestPayload(payload planner.WriteExportManifestPayload) ([]byte, error) {
	if isFreeCADRuntimeNativeManifest(payload) {
		projected, err := ProjectFreeCADRuntimeExportManifest(payload)
		if err != nil {
			return nil, fmt.Errorf("project FreeCAD runtime export manifest: %w", err)
		}
		data, err := MarshalFreeCADRuntimeExportManifestJSON(projected)
		if err != nil {
			return nil, fmt.Errorf("marshal FreeCAD runtime export manifest: %w", err)
		}
		return data, nil
	}

	return adapter.MarshalGenericExportManifest(payload)
}

func isFreeCADRuntimeNativeManifest(payload planner.WriteExportManifestPayload) bool {
	return payload.ManifestProjectionMode == planner.ExportManifestProjectionModeFreeCADRuntimeNative
}
