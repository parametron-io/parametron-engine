package cadruntime

import (
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"strings"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/adapter"
	"parametron/internal/engine/adapter/freecad"
	"parametron/internal/engine/verification"
)

const (
	FreeCADRuntimeObservationRequestFilename = "prm.verification.json"

	FreeCADRuntimeObservationRequestStageManifestPreflight       = "manifest_preflight"
	FreeCADRuntimeObservationRequestStageRequestPath             = "request_path"
	FreeCADRuntimeObservationRequestStageIntentValidation        = "intent_validation"
	FreeCADRuntimeObservationRequestStageManifestMaterialization = "manifest_materialization"
	FreeCADRuntimeObservationRequestStageContractDerivation      = "contract_derivation"
	FreeCADRuntimeObservationRequestStageContractCompatibility   = "contract_compatibility"
	FreeCADRuntimeObservationRequestStageContractSerialization   = "contract_serialization"
	FreeCADRuntimeObservationRequestStageContractWrite           = "contract_write"
)

// FreeCADRuntimeObservationRequestMaterialization retains the Task 8
// materialization and the exact Engine verification request prepared for the
// aligned FreeCAD runtime.
type FreeCADRuntimeObservationRequestMaterialization struct {
	Manifest freecad.FreeCADRuntimeManifestMaterialization
	Contract verification.Contract
	JSON     []byte
	Path     string
}

// FreeCADRuntimeObservationRequestError is a typed observation-request
// composition or write failure with stable stage and contextual details.
type FreeCADRuntimeObservationRequestError struct {
	Stage     string
	Field     string
	AttemptID string
	Path      string
	Err       error
}

func (e *FreeCADRuntimeObservationRequestError) Error() string {
	if e == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("freecad runtime observation request")
	for _, part := range []string{e.Stage, e.Field} {
		if part != "" {
			b.WriteString(": ")
			b.WriteString(part)
		}
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

func (e *FreeCADRuntimeObservationRequestError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ComposeFreeCADRuntimeObservationRequest composes the exact request bytes from
// an already-prepared Task 7 source without filesystem or process side effects.
func ComposeFreeCADRuntimeObservationRequest(req adapter.CADRuntimeOrchestrationRequest) (FreeCADRuntimeObservationRequestMaterialization, error) {
	manifest, path, err := preflightFreeCADRuntimeObservationRequest(req)
	if err != nil {
		return FreeCADRuntimeObservationRequestMaterialization{}, err
	}
	return composeFreeCADRuntimeObservationRequest(manifest, req.Manifest, path)
}

// WriteFreeCADRuntimeObservationRequest performs side-effect-free preflight,
// materializes Task 8, then atomically writes the Engine verification request.
func WriteFreeCADRuntimeObservationRequest(req adapter.CADRuntimeOrchestrationRequest) (FreeCADRuntimeObservationRequestMaterialization, error) {
	preflight, path, err := preflightFreeCADRuntimeObservationRequest(req)
	if err != nil {
		return FreeCADRuntimeObservationRequestMaterialization{}, err
	}

	written, err := freecad.WriteFreeCADRuntimeManifest(req)
	if err != nil {
		return FreeCADRuntimeObservationRequestMaterialization{}, observationRequestError(
			FreeCADRuntimeObservationRequestStageManifestMaterialization, "", preflight, path, err,
		)
	}
	if !reflect.DeepEqual(written.Attempt, preflight.Attempt) {
		return FreeCADRuntimeObservationRequestMaterialization{}, observationRequestError(
			FreeCADRuntimeObservationRequestStageManifestMaterialization, "", preflight, path,
			fmt.Errorf("materialized attempt does not match preflight attempt"),
		)
	}

	materialization, err := composeFreeCADRuntimeObservationRequest(written, req.Manifest, path)
	if err != nil {
		return FreeCADRuntimeObservationRequestMaterialization{}, err
	}
	if err := verification.WriteFile(path, &materialization.Contract); err != nil {
		return FreeCADRuntimeObservationRequestMaterialization{}, observationRequestError(
			FreeCADRuntimeObservationRequestStageContractWrite, "", written, path, err,
		)
	}
	return materialization, nil
}

func preflightFreeCADRuntimeObservationRequest(req adapter.CADRuntimeOrchestrationRequest) (freecad.FreeCADRuntimeManifestMaterialization, string, error) {
	manifest, err := freecad.ComposeFreeCADRuntimeManifest(req)
	if err != nil {
		return freecad.FreeCADRuntimeManifestMaterialization{}, "", &FreeCADRuntimeObservationRequestError{
			Stage: FreeCADRuntimeObservationRequestStageManifestPreflight,
			Err:   err,
		}
	}
	path, field, err := deriveFreeCADRuntimeObservationRequestPath(manifest)
	if err != nil {
		return freecad.FreeCADRuntimeManifestMaterialization{}, "", observationRequestError(
			FreeCADRuntimeObservationRequestStageRequestPath, field, manifest, path, err,
		)
	}
	if field, err := validateFreeCADVerificationIntent(req.Manifest.Verification); err != nil {
		return freecad.FreeCADRuntimeManifestMaterialization{}, "", observationRequestError(
			FreeCADRuntimeObservationRequestStageIntentValidation, field, manifest, path, err,
		)
	}
	return manifest, path, nil
}

func composeFreeCADRuntimeObservationRequest(
	manifest freecad.FreeCADRuntimeManifestMaterialization,
	payload planner.WriteExportManifestPayload,
	path string,
) (FreeCADRuntimeObservationRequestMaterialization, error) {
	contract, err := verification.DeriveFromManifestAndWorkingCopy(
		payload,
		manifest.Attempt.Layout.SourceDocumentPath,
	)
	if err != nil {
		return FreeCADRuntimeObservationRequestMaterialization{}, observationRequestError(
			FreeCADRuntimeObservationRequestStageContractDerivation, "", manifest, path, err,
		)
	}
	// The prepared source path supplies the bytes used for
	// working_copy_sha256. The working_copy_path reference instead identifies
	// the exact Task 7 attempt root supplied to the runtime through
	// --working-copy.
	contract.Expected.References[0].Name = manifest.Attempt.Layout.WorkingCopyDir
	if field, err := validateFreeCADVerificationContract(contract); err != nil {
		return FreeCADRuntimeObservationRequestMaterialization{}, observationRequestError(
			FreeCADRuntimeObservationRequestStageContractCompatibility, field, manifest, path, err,
		)
	}
	data, err := verification.CanonicalJSON(contract)
	if err != nil {
		return FreeCADRuntimeObservationRequestMaterialization{}, observationRequestError(
			FreeCADRuntimeObservationRequestStageContractSerialization, "", manifest, path, err,
		)
	}
	data = append(data, '\n')
	return FreeCADRuntimeObservationRequestMaterialization{
		Manifest: manifest,
		Contract: *contract,
		JSON:     data,
		Path:     path,
	}, nil
}

func deriveFreeCADRuntimeObservationRequestPath(manifest freecad.FreeCADRuntimeManifestMaterialization) (string, string, error) {
	layout := manifest.Attempt.Layout
	path := filepath.Join(layout.WorkingCopyDir, FreeCADRuntimeObservationRequestFilename)
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return path, "Path", fmt.Errorf("must be canonical, absolute, and clean")
	}
	relative, err := filepath.Rel(layout.WorkingCopyDir, path)
	if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return path, "Path", fmt.Errorf("must be strictly contained under working-copy directory")
	}
	collisions := []struct {
		field string
		path  string
	}{
		{"CADRuntime.ManifestFilename", layout.ManifestPath},
		{"CADRuntime.ResultFilename", layout.ResultPath},
		{"SourceDocumentPath", layout.SourceDocumentPath},
		{"OutputDir", layout.OutputDir},
	}
	for _, collision := range collisions {
		if path == collision.path {
			return path, collision.field, fmt.Errorf("collides with observation-request path")
		}
	}
	outputRelative, err := filepath.Rel(layout.OutputDir, path)
	if err == nil && outputRelative != ".." && !strings.HasPrefix(outputRelative, ".."+string(filepath.Separator)) {
		return path, "Path", fmt.Errorf("must not be nested under output directory")
	}
	return path, "", nil
}

func validateFreeCADVerificationIntent(intent planner.VerificationManifestIntent) (string, error) {
	expectedIDs := make(map[string]int, len(intent.ExpectedParameters))
	for index, parameter := range intent.ExpectedParameters {
		prefix := fmt.Sprintf("Manifest.Verification.ExpectedParameters[%d]", index)
		if field, err := validateExactNonEmptyString(prefix+".ID", parameter.ID); err != nil {
			return field, err
		}
		if previous, exists := expectedIDs[parameter.ID]; exists {
			return prefix + ".ID", fmt.Errorf("duplicates ExpectedParameters[%d].ID", previous)
		}
		expectedIDs[parameter.ID] = index
		if parameter.Type != "number" {
			return prefix + ".Type", fmt.Errorf("must equal %q", "number")
		}
		if parameter.Unit != "mm" {
			return prefix + ".Unit", fmt.Errorf("must equal %q", "mm")
		}
		if math.IsNaN(parameter.Value) || math.IsInf(parameter.Value, 0) {
			return prefix + ".Value", fmt.Errorf("must be finite")
		}
	}

	if len(expectedIDs) == 0 {
		if len(intent.ObservationParameterLinks) != 0 {
			return "Manifest.Verification.ObservationParameterLinks", fmt.Errorf("must be empty when no expected parameters are present")
		}
		return "", nil
	}
	if len(intent.ObservationParameterLinks) == 0 {
		return "Manifest.Verification.ObservationParameterLinks", fmt.Errorf("must contain one explicit link for every expected parameter")
	}

	linkIDs := make(map[string]int, len(intent.ObservationParameterLinks))
	for index, link := range intent.ObservationParameterLinks {
		prefix := fmt.Sprintf("Manifest.Verification.ObservationParameterLinks[%d]", index)
		for _, value := range []struct {
			field string
			value string
		}{{prefix + ".ID", link.ID}, {prefix + ".Name", link.Name}, {prefix + ".GroupName", link.GroupName}} {
			if field, err := validateExactNonEmptyString(value.field, value.value); err != nil {
				return field, err
			}
		}
		if previous, exists := linkIDs[link.ID]; exists {
			return prefix + ".ID", fmt.Errorf("duplicates ObservationParameterLinks[%d].ID", previous)
		}
		linkIDs[link.ID] = index
		if _, exists := expectedIDs[link.ID]; !exists {
			return prefix + ".ID", fmt.Errorf("has no matching expected parameter")
		}
	}
	for id, index := range expectedIDs {
		if _, exists := linkIDs[id]; !exists {
			return fmt.Sprintf("Manifest.Verification.ExpectedParameters[%d].ID", index), fmt.Errorf("has no observation parameter link")
		}
	}
	return "", nil
}

func validateFreeCADVerificationContract(contract *verification.Contract) (string, error) {
	if err := verification.Validate(contract); err != nil {
		return "", err
	}
	if contract.Observe.Components || contract.Checks.Components.Enabled || len(contract.Expected.Components) != 0 {
		return "components", fmt.Errorf("component observation is not supported")
	}
	if !contract.Observe.Metadata || !contract.Checks.Metadata.Enabled ||
		len(contract.Expected.Metadata) != 1 ||
		contract.Expected.Metadata[0].Key != "working_copy_sha256" {
		return "metadata", fmt.Errorf("must request exactly working_copy_sha256 metadata")
	}
	if !contract.Observe.References || !contract.Checks.References.Enabled ||
		len(contract.Expected.References) != 1 ||
		contract.Expected.References[0].Kind != "working_copy_path" {
		return "references", fmt.Errorf("must request exactly working_copy_path reference")
	}
	if len(contract.Expected.Parameters) == 0 {
		if contract.Observe.Parameters || contract.Checks.Parameters.Enabled || len(contract.ObservationContext.Parameters) != 0 {
			return "parameters", fmt.Errorf("empty expected parameters require disabled observation/checks and empty bindings")
		}
		return "", nil
	}
	if !contract.Observe.Parameters || !contract.Checks.Parameters.Enabled {
		return "parameters", fmt.Errorf("expected parameters require enabled observation and checks")
	}
	expected := make(map[string]struct{}, len(contract.Expected.Parameters))
	for _, parameter := range contract.Expected.Parameters {
		expected[parameter.ID] = struct{}{}
	}
	if len(contract.ObservationContext.Parameters) != len(expected) {
		return "observationContext.parameters", fmt.Errorf("must bind every expected parameter exactly once")
	}
	for index, binding := range contract.ObservationContext.Parameters {
		if _, exists := expected[binding.ID]; !exists || binding.Name == "" || binding.GroupName == "" {
			return fmt.Sprintf("observationContext.parameters[%d]", index), fmt.Errorf("must explicitly bind id, name, and groupName")
		}
	}
	return "", nil
}

func validateExactNonEmptyString(field, value string) (string, error) {
	if value == "" || strings.TrimSpace(value) == "" {
		return field, fmt.Errorf("must be non-empty")
	}
	if strings.TrimSpace(value) != value {
		return field, fmt.Errorf("must not have surrounding whitespace")
	}
	return "", nil
}

func observationRequestError(stage, field string, manifest freecad.FreeCADRuntimeManifestMaterialization, path string, err error) error {
	return &FreeCADRuntimeObservationRequestError{
		Stage:     stage,
		Field:     field,
		AttemptID: manifest.Attempt.Identity.ID,
		Path:      path,
		Err:       err,
	}
}
