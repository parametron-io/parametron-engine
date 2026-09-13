package freecad

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"parametron/internal/engine/adapter"
)

const (
	FreeCADRuntimeManifestStageAttemptComputation    = "attempt_computation"
	FreeCADRuntimeManifestStageManifestValidation    = "manifest_validation"
	FreeCADRuntimeManifestStageManifestProjection    = "manifest_projection"
	FreeCADRuntimeManifestStageManifestSerialization = "manifest_serialization"
	FreeCADRuntimeManifestStageAttemptPreparation    = "attempt_preparation"
	FreeCADRuntimeManifestStageManifestWrite         = "manifest_write"
)

// FreeCADRuntimeManifestMaterialization retains the authoritative attempt,
// projected native manifest, and exact bytes used for runtime materialization.
type FreeCADRuntimeManifestMaterialization struct {
	Attempt  FreeCADRuntimeAttempt
	Manifest FreeCADRuntimeExportManifest
	JSON     []byte
}

// FreeCADRuntimeManifestError is a typed manifest composition or write failure
// with stable stage and optional field, attempt, and path context.
type FreeCADRuntimeManifestError struct {
	Stage     string
	Field     string
	AttemptID string
	Path      string
	Err       error
}

func (e *FreeCADRuntimeManifestError) Error() string {
	if e == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("freecad runtime manifest")
	if e.Stage != "" {
		b.WriteString(": ")
		b.WriteString(e.Stage)
	}
	if e.Field != "" {
		b.WriteString(": ")
		b.WriteString(e.Field)
	}
	if e.AttemptID != "" {
		b.WriteString(": attempt=")
		b.WriteString(e.AttemptID)
	}
	if e.Path != "" {
		b.WriteString(": path=")
		b.WriteString(e.Path)
	}
	if e.Err != nil {
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

func (e *FreeCADRuntimeManifestError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ComposeFreeCADRuntimeManifest computes the authoritative attempt and exact
// FreeCAD-native manifest bytes without filesystem or process side effects.
func ComposeFreeCADRuntimeManifest(req adapter.CADRuntimeOrchestrationRequest) (FreeCADRuntimeManifestMaterialization, error) {
	attempt, err := ComputeFreeCADRuntimeAttempt(req)
	if err != nil {
		return FreeCADRuntimeManifestMaterialization{}, &FreeCADRuntimeManifestError{
			Stage: FreeCADRuntimeManifestStageAttemptComputation,
			Err:   err,
		}
	}

	if err := ValidateFreeCADRuntimeExportManifestSchema(req.Manifest); err != nil {
		return FreeCADRuntimeManifestMaterialization{}, &FreeCADRuntimeManifestError{
			Stage:     FreeCADRuntimeManifestStageManifestValidation,
			Field:     "Manifest.SchemaVersion",
			AttemptID: attempt.Identity.ID,
			Path:      attempt.Layout.ManifestPath,
			Err:       err,
		}
	}

	payload := req.Manifest
	payload.SourceDocument = attempt.Layout.SourceDocument
	manifest, err := ProjectFreeCADRuntimeExportManifest(payload)
	if err != nil {
		projectionField := ""
		var projectionError *FreeCADRuntimeManifestProjectionError
		if errors.As(err, &projectionError) {
			projectionField = projectionError.Field
		}
		return FreeCADRuntimeManifestMaterialization{}, &FreeCADRuntimeManifestError{
			Stage:     FreeCADRuntimeManifestStageManifestProjection,
			Field:     projectionField,
			AttemptID: attempt.Identity.ID,
			Path:      attempt.Layout.ManifestPath,
			Err:       err,
		}
	}

	data, err := MarshalFreeCADRuntimeExportManifestJSON(manifest)
	if err != nil {
		return FreeCADRuntimeManifestMaterialization{}, &FreeCADRuntimeManifestError{
			Stage:     FreeCADRuntimeManifestStageManifestSerialization,
			AttemptID: attempt.Identity.ID,
			Path:      attempt.Layout.ManifestPath,
			Err:       err,
		}
	}

	return FreeCADRuntimeManifestMaterialization{
		Attempt:  attempt,
		Manifest: *manifest,
		JSON:     data,
	}, nil
}

// WriteFreeCADRuntimeManifest composes before preparing the authoritative
// attempt, then atomically writes the exact composed bytes to its manifest path.
func WriteFreeCADRuntimeManifest(req adapter.CADRuntimeOrchestrationRequest) (FreeCADRuntimeManifestMaterialization, error) {
	materialization, err := ComposeFreeCADRuntimeManifest(req)
	if err != nil {
		return FreeCADRuntimeManifestMaterialization{}, err
	}

	prepared, err := PrepareFreeCADRuntimeAttempt(req)
	if err != nil {
		return FreeCADRuntimeManifestMaterialization{}, &FreeCADRuntimeManifestError{
			Stage:     FreeCADRuntimeManifestStageAttemptPreparation,
			AttemptID: materialization.Attempt.Identity.ID,
			Path:      materialization.Attempt.Layout.ManifestPath,
			Err:       err,
		}
	}
	if !reflect.DeepEqual(prepared, materialization.Attempt) {
		return FreeCADRuntimeManifestMaterialization{}, &FreeCADRuntimeManifestError{
			Stage:     FreeCADRuntimeManifestStageAttemptPreparation,
			AttemptID: materialization.Attempt.Identity.ID,
			Path:      materialization.Attempt.Layout.ManifestPath,
			Err:       fmt.Errorf("prepared attempt does not match composed attempt"),
		}
	}

	if err := adapter.WriteExportManifestFile(
		prepared.Layout.WorkingCopyDir,
		prepared.Layout.ManifestPath,
		materialization.JSON,
		"write FreeCAD runtime export manifest",
	); err != nil {
		return FreeCADRuntimeManifestMaterialization{}, &FreeCADRuntimeManifestError{
			Stage:     FreeCADRuntimeManifestStageManifestWrite,
			AttemptID: prepared.Identity.ID,
			Path:      prepared.Layout.ManifestPath,
			Err:       err,
		}
	}

	materialization.Attempt = prepared
	return materialization, nil
}
