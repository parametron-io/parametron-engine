package adapter

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/runtimecap"
)

// CADRuntimeOrchestrator is the secondary adapter capability for aligned
// RunCADRuntime package execution. Adapters that only implement Adapter remain
// valid for legacy plans; aligned CAD-runtime plans require this interface.
//
// A nil error means the complete RunCADRuntime step succeeded.
type CADRuntimeOrchestrator interface {
	OrchestrateCADRuntime(context.Context, CADRuntimeOrchestrationRequest) error
}

// CADRuntimeOrchestrationRequest is the adapter-neutral Executor→Adapter
// handoff for one RunCADRuntime attempt. Logical payloads stay separate from
// operational executable selection.
type CADRuntimeOrchestrationRequest struct {
	JobID      string
	ProductKey string
	ProductDir string
	StepID     string
	Attempt    int

	CSV        planner.WriteCSVPayload
	Manifest   planner.WriteExportManifestPayload
	CADRuntime planner.RunCADRuntimePayload
	Executable runtimecap.ExecutableSelection

	// ExternalTargets carries Engine-authoritative canonical identities for
	// external reference coordinates. The runtime must never derive these
	// identities from resolved working-copy paths.
	ExternalTargets []ReferenceTraversalExternalTarget
}

// ReferenceTraversalExternalTarget maps one runtime-observable reference
// coordinate to an Engine-owned canonical external target identity.
type ReferenceTraversalExternalTarget struct {
	SourceObjectName   string
	SourceProperty     string
	ReferenceMechanism string
	TargetObjectName   string
	TargetDocumentPath string
}

// CADRuntimeOrchestrationRequestError is a typed, field-specific request
// validation failure. Validation is side-effect-free.
type CADRuntimeOrchestrationRequestError struct {
	Field string
	Err   error
}

func (e *CADRuntimeOrchestrationRequestError) Error() string {
	if e == nil {
		return ""
	}
	if e.Field == "" {
		if e.Err == nil {
			return "cad runtime orchestration request invalid"
		}
		return e.Err.Error()
	}
	if e.Err == nil {
		return fmt.Sprintf("cad runtime orchestration request invalid: %s", e.Field)
	}
	return fmt.Sprintf("cad runtime orchestration request invalid: %s: %v", e.Field, e.Err)
}

func (e *CADRuntimeOrchestrationRequestError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ValidateCADRuntimeOrchestrationRequest checks request completeness and
// internal consistency without filesystem, process, or config side effects.
func ValidateCADRuntimeOrchestrationRequest(req CADRuntimeOrchestrationRequest) error {
	if strings.TrimSpace(req.JobID) == "" {
		return &CADRuntimeOrchestrationRequestError{
			Field: "JobID",
			Err:   fmt.Errorf("must be non-empty"),
		}
	}
	if strings.TrimSpace(req.ProductKey) == "" {
		return &CADRuntimeOrchestrationRequestError{
			Field: "ProductKey",
			Err:   fmt.Errorf("must be non-empty"),
		}
	}
	if strings.TrimSpace(req.ProductDir) == "" {
		return &CADRuntimeOrchestrationRequestError{
			Field: "ProductDir",
			Err:   fmt.Errorf("must be non-empty"),
		}
	}
	if strings.TrimSpace(req.StepID) == "" {
		return &CADRuntimeOrchestrationRequestError{
			Field: "StepID",
			Err:   fmt.Errorf("must be non-empty"),
		}
	}
	if req.Attempt < 1 {
		return &CADRuntimeOrchestrationRequestError{
			Field: "Attempt",
			Err:   fmt.Errorf("must be >= 1"),
		}
	}
	if strings.TrimSpace(req.CSV.Filename) == "" {
		return &CADRuntimeOrchestrationRequestError{
			Field: "CSV.Filename",
			Err:   fmt.Errorf("must be non-empty"),
		}
	}
	if strings.TrimSpace(req.Manifest.ManifestFilename) == "" {
		return &CADRuntimeOrchestrationRequestError{
			Field: "Manifest.ManifestFilename",
			Err:   fmt.Errorf("must be non-empty"),
		}
	}
	if strings.TrimSpace(req.CADRuntime.Adapter) == "" {
		return &CADRuntimeOrchestrationRequestError{
			Field: "CADRuntime.Adapter",
			Err:   fmt.Errorf("must be non-empty"),
		}
	}
	if strings.TrimSpace(req.CADRuntime.ManifestFilename) == "" {
		return &CADRuntimeOrchestrationRequestError{
			Field: "CADRuntime.ManifestFilename",
			Err:   fmt.Errorf("must be non-empty"),
		}
	}
	if strings.TrimSpace(req.Executable.Adapter) == "" {
		return &CADRuntimeOrchestrationRequestError{
			Field: "Executable.Adapter",
			Err:   fmt.Errorf("must be non-empty"),
		}
	}
	if strings.TrimSpace(req.Executable.Command) == "" {
		return &CADRuntimeOrchestrationRequestError{
			Field: "Executable.Command",
			Err:   fmt.Errorf("must be non-empty"),
		}
	}
	if strings.TrimSpace(req.Executable.Path) == "" {
		return &CADRuntimeOrchestrationRequestError{
			Field: "Executable.Path",
			Err:   fmt.Errorf("must be non-empty"),
		}
	}
	if !filepath.IsAbs(req.Executable.Path) {
		return &CADRuntimeOrchestrationRequestError{
			Field: "Executable.Path",
			Err:   fmt.Errorf("must be absolute"),
		}
	}
	if filepath.Clean(req.Executable.Path) != req.Executable.Path {
		return &CADRuntimeOrchestrationRequestError{
			Field: "Executable.Path",
			Err:   fmt.Errorf("must be clean"),
		}
	}
	switch req.Executable.Source {
	case runtimecap.ExecutableSelectionSourceConfigured, runtimecap.ExecutableSelectionSourceDefaultPATH:
	default:
		return &CADRuntimeOrchestrationRequestError{
			Field: "Executable.Source",
			Err:   fmt.Errorf("unsupported selection source %q", req.Executable.Source),
		}
	}

	if req.CADRuntime.Adapter != req.Manifest.Adapter {
		return &CADRuntimeOrchestrationRequestError{
			Field: "CADRuntime.Adapter",
			Err: fmt.Errorf(
				"must match Manifest.Adapter: got=%q want=%q",
				req.CADRuntime.Adapter,
				req.Manifest.Adapter,
			),
		}
	}
	if req.CADRuntime.Adapter != req.Executable.Adapter {
		return &CADRuntimeOrchestrationRequestError{
			Field: "CADRuntime.Adapter",
			Err: fmt.Errorf(
				"must match Executable.Adapter: got=%q want=%q",
				req.CADRuntime.Adapter,
				req.Executable.Adapter,
			),
		}
	}
	if req.CADRuntime.ManifestFilename != req.Manifest.ManifestFilename {
		return &CADRuntimeOrchestrationRequestError{
			Field: "CADRuntime.ManifestFilename",
			Err: fmt.Errorf(
				"must match Manifest.ManifestFilename: got=%q want=%q",
				req.CADRuntime.ManifestFilename,
				req.Manifest.ManifestFilename,
			),
		}
	}

	if err := validateOptionalProductKey("CSV.ProductKey", req.CSV.ProductKey, req.ProductKey); err != nil {
		return err
	}
	if err := validateOptionalProductKey("Manifest.ProductKey", req.Manifest.ProductKey, req.ProductKey); err != nil {
		return err
	}
	if err := validateOptionalProductKey("CADRuntime.ProductKey", req.CADRuntime.ProductKey, req.ProductKey); err != nil {
		return err
	}

	return nil
}

func validateOptionalProductKey(field, payloadKey, requestKey string) error {
	if payloadKey == "" {
		return nil
	}
	if payloadKey != requestKey {
		return &CADRuntimeOrchestrationRequestError{
			Field: field,
			Err: fmt.Errorf(
				"must be empty or match ProductKey: got=%q want=%q",
				payloadKey,
				requestKey,
			),
		}
	}
	return nil
}
