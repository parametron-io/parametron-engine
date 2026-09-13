package handoff

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/artifact"
	"parametron/internal/engine/job"
	"parametron/internal/engine/metadata"
)

var (
	ErrNilJob                          = errors.New("handoff job is nil")
	ErrMissingWriteCSV                 = errors.New("handoff requires exactly one WriteCSV step")
	ErrMissingManifest                 = errors.New("handoff requires exactly one WriteExportManifest step")
	ErrDuplicateWriteCSV               = errors.New("handoff has duplicate WriteCSV steps")
	ErrDuplicateManifest               = errors.New("handoff has duplicate WriteExportManifest steps")
	ErrDuplicateCADRuntime             = errors.New("handoff has more than one RunCADRuntime step")
	ErrUnsupportedStep                 = errors.New("handoff contains unsupported step")
	ErrMismatchedProductKey            = errors.New("handoff step product identity does not match job product key")
	ErrManifestFilenameEmpty           = errors.New("handoff manifest filename is empty")
	ErrSchemaVersionEmpty              = errors.New("handoff manifest schemaVersion is empty")
	ErrManifestOutputsEmpty            = errors.New("handoff manifest outputs are empty")
	ErrManifestOutputInvalid           = errors.New("handoff manifest output is invalid")
	ErrCADRuntimeAdapterEmpty          = errors.New("handoff CAD runtime adapter is empty")
	ErrCADRuntimeAdapterMismatch       = errors.New("handoff CAD runtime adapter does not match manifest adapter")
	ErrCADRuntimeManifestFilenameEmpty = errors.New("handoff CAD runtime manifest filename is empty")
	ErrCADRuntimeManifestMismatch      = errors.New("handoff CAD runtime manifest filename does not match manifest filename")
	ErrCADRuntimeResultFilenameEmpty   = errors.New("handoff CAD runtime result filename is empty")
	ErrCADRuntimeResultFilenameInvalid = errors.New("handoff CAD runtime result filename is invalid")
	ErrCADRuntimeFilenameConflict      = errors.New("handoff CAD runtime result filename conflicts with another handoff filename")
	ErrTableLogicalIDEmpty             = errors.New("handoff table logicalID is empty")
	ErrTableNameEmpty                  = errors.New("handoff table name is empty")
	ErrTableFingerprintEmpty           = errors.New("handoff table fingerprint is empty")
	ErrDuplicateTableLogicalID         = errors.New("handoff has duplicate table logicalID")
)

// Package is the deterministic engine-domain handoff for a single product job.
// It intentionally remains independent from transport and persistence concerns.
type Package struct {
	JobID      string                             `json:"jobID"`
	ProductKey string                             `json:"productKey"`
	CSV        planner.WriteCSVPayload            `json:"csv"`
	Manifest   planner.WriteExportManifestPayload `json:"manifest"`
	CADRuntime *planner.RunCADRuntimePayload      `json:"cadRuntime,omitempty"`
	Tables     []metadata.TableInputMetadata      `json:"tables,omitempty"`
	Steps      []StepSnapshot                     `json:"steps"`
}

// StepSnapshot captures the original ordered steps in a JSON-friendly form.
type StepSnapshot struct {
	Type       planner.StepType                    `json:"type"`
	CSV        *planner.WriteCSVPayload            `json:"csv,omitempty"`
	Manifest   *planner.WriteExportManifestPayload `json:"manifest,omitempty"`
	CADRuntime *planner.RunCADRuntimePayload       `json:"cadRuntime,omitempty"`
}

// UnmarshalJSON restores handoff-only logical manifest identity that the
// runtime-facing manifest payload intentionally omits from its JSON shape.
func (p *Package) UnmarshalJSON(data []byte) error {
	type packageAlias Package
	var decoded packageAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*p = Package(decoded)

	if p.CADRuntime != nil {
		if p.Manifest.ProductKey == "" {
			p.Manifest.ProductKey = p.ProductKey
		}
		if p.Manifest.ManifestFilename == "" {
			p.Manifest.ManifestFilename = p.CADRuntime.ManifestFilename
		}
		for i := range p.Steps {
			snapshot := &p.Steps[i]
			if snapshot.Manifest == nil {
				continue
			}
			if snapshot.Manifest.ProductKey == "" {
				snapshot.Manifest.ProductKey = p.ProductKey
			}
			if snapshot.Manifest.ManifestFilename == "" {
				snapshot.Manifest.ManifestFilename = p.Manifest.ManifestFilename
			}
		}
	}
	return nil
}

// FromJob validates and constructs a deterministic handoff package from one job.
func FromJob(j *job.Job) (*Package, error) {
	if j == nil {
		return nil, ErrNilJob
	}

	pkg := &Package{
		JobID:      j.ID,
		ProductKey: j.ProductKey,
		Steps:      make([]StepSnapshot, 0, len(j.Steps())),
	}

	steps := j.Steps()
	var hasCSV bool
	var hasManifest bool
	for _, step := range steps {
		snapshot, err := snapshotStep(step)
		if err != nil {
			return nil, err
		}

		switch step.Type {
		case planner.StepWriteCSV:
			if hasCSV {
				return nil, ErrDuplicateWriteCSV
			}
			if err := validateStepProduct(j.ProductKey, step, snapshot.CSV.ProductKey); err != nil {
				return nil, err
			}
			pkg.CSV = *snapshot.CSV
			hasCSV = true
		case planner.StepWriteExportManifest:
			if hasManifest {
				return nil, ErrDuplicateManifest
			}
			if err := validateManifestProduct(j.ProductKey, step, *snapshot.Manifest); err != nil {
				return nil, err
			}
			pkg.Manifest = *snapshot.Manifest
			hasManifest = true
		case planner.StepRunCADRuntime:
			if pkg.CADRuntime != nil {
				return nil, ErrDuplicateCADRuntime
			}
			if err := validateStepProduct(j.ProductKey, step, snapshot.CADRuntime.ProductKey); err != nil {
				return nil, err
			}
			pkg.CADRuntime = snapshot.CADRuntime
		default:
			return nil, fmt.Errorf("%w: %q", ErrUnsupportedStep, step.Type)
		}

		pkg.Steps = append(pkg.Steps, snapshot)
	}

	if !hasCSV {
		return nil, ErrMissingWriteCSV
	}
	if !hasManifest {
		return nil, ErrMissingManifest
	}
	if err := validatePackage(pkg); err != nil {
		return nil, err
	}

	return pkg, nil
}

// Plan reconstructs the execution plan in the original step order.
func (p *Package) Plan() *planner.ExecutionPlan {
	if p == nil {
		return nil
	}

	steps := make([]planner.Step, 0, len(p.Steps))
	for _, snapshot := range p.Steps {
		switch snapshot.Type {
		case planner.StepWriteCSV:
			steps = append(steps, planner.Step{
				Type:    snapshot.Type,
				Payload: cloneWriteCSVPayload(*snapshot.CSV),
			})
		case planner.StepWriteExportManifest:
			steps = append(steps, planner.Step{
				Type:    snapshot.Type,
				Payload: cloneWriteExportManifestPayload(*snapshot.Manifest),
			})
		case planner.StepRunCADRuntime:
			steps = append(steps, planner.Step{
				Type:    snapshot.Type,
				Payload: cloneRunCADRuntimePayload(*snapshot.CADRuntime),
			})
		}
	}

	return &planner.ExecutionPlan{Steps: steps}
}

func (p *Package) RequiresCADRuntime() bool {
	return p != nil && p.CADRuntime != nil
}

func (p *Package) RuntimeResultFilename() string {
	if p == nil || p.CADRuntime == nil {
		return ""
	}
	return p.CADRuntime.ResultFilename
}

func (p *Package) ManifestFilename() string {
	if p == nil {
		return ""
	}
	return p.Manifest.ManifestFilename
}

// ExpectedArtifacts returns the deterministic filenames this handoff should produce.
func (p *Package) ExpectedArtifacts() []string {
	if p == nil {
		return nil
	}

	artifacts := make([]string, 0, 3+len(p.Manifest.Outputs))
	if p.CSV.Filename != "" {
		artifacts = append(artifacts, p.CSV.Filename)
	}
	if p.Manifest.ManifestFilename != "" {
		artifacts = append(artifacts, p.Manifest.ManifestFilename)
	}
	if p.CADRuntime != nil && p.CADRuntime.ResultFilename != "" {
		artifacts = append(artifacts, p.CADRuntime.ResultFilename)
	}
	for _, output := range p.Manifest.Outputs {
		if output.Filename != "" {
			artifacts = append(artifacts, output.Filename)
		}
	}
	return artifacts
}

func snapshotStep(step planner.Step) (StepSnapshot, error) {
	snapshot := StepSnapshot{Type: step.Type}

	switch payload := step.Payload.(type) {
	case planner.WriteCSVPayload:
		copied := cloneWriteCSVPayload(payload)
		snapshot.CSV = &copied
	case planner.WriteExportManifestPayload:
		copied := cloneWriteExportManifestPayload(payload)
		snapshot.Manifest = &copied
	case planner.RunCADRuntimePayload:
		copied := cloneRunCADRuntimePayload(payload)
		snapshot.CADRuntime = &copied
	default:
		return StepSnapshot{}, fmt.Errorf("%w: %T", ErrUnsupportedStep, step.Payload)
	}

	if err := validateSnapshotShape(snapshot); err != nil {
		return StepSnapshot{}, err
	}

	return snapshot, nil
}

func validateSnapshotShape(snapshot StepSnapshot) error {
	switch snapshot.Type {
	case planner.StepWriteCSV:
		if snapshot.CSV == nil || snapshot.Manifest != nil || snapshot.CADRuntime != nil {
			return fmt.Errorf("%w: %q payload shape is invalid", ErrUnsupportedStep, snapshot.Type)
		}
	case planner.StepWriteExportManifest:
		if snapshot.Manifest == nil || snapshot.CSV != nil || snapshot.CADRuntime != nil {
			return fmt.Errorf("%w: %q payload shape is invalid", ErrUnsupportedStep, snapshot.Type)
		}
	case planner.StepRunCADRuntime:
		if snapshot.CADRuntime == nil || snapshot.CSV != nil || snapshot.Manifest != nil {
			return fmt.Errorf("%w: %q payload shape is invalid", ErrUnsupportedStep, snapshot.Type)
		}
	default:
		return fmt.Errorf("%w: %q", ErrUnsupportedStep, snapshot.Type)
	}
	return nil
}

func validatePackage(pkg *Package) error {
	if csvKey := pkg.CSV.ProductKey; csvKey != "" && csvKey != pkg.ProductKey {
		return fmt.Errorf("%w: csv=%q job=%q", ErrMismatchedProductKey, csvKey, pkg.ProductKey)
	}

	if pkg.Manifest.ManifestFilename == "" {
		return ErrManifestFilenameEmpty
	}
	if pkg.Manifest.SchemaVersion == "" {
		return ErrSchemaVersionEmpty
	}
	if len(pkg.Manifest.Outputs) == 0 && pkg.CADRuntime == nil {
		return ErrManifestOutputsEmpty
	}
	if err := validateManifestOutputs(pkg.Manifest.Outputs, manifestRequiresObject(pkg)); err != nil {
		return err
	}
	if pkg.CADRuntime != nil {
		if err := validateCADRuntime(pkg); err != nil {
			return err
		}
	}
	if err := ValidateTableInputs(pkg.Tables); err != nil {
		return err
	}
	pkg.Tables = CloneTableInputs(pkg.Tables)

	return nil
}

func validateCADRuntime(pkg *Package) error {
	runtime := pkg.CADRuntime
	if runtime.ProductKey != "" && runtime.ProductKey != pkg.ProductKey {
		return fmt.Errorf("%w: cadRuntime=%q job=%q", ErrMismatchedProductKey, runtime.ProductKey, pkg.ProductKey)
	}

	adapter := runtime.Adapter
	if adapter == "" || strings.TrimSpace(adapter) == "" {
		return ErrCADRuntimeAdapterEmpty
	}
	if adapter != strings.TrimSpace(adapter) {
		return fmt.Errorf("%w: adapter %q is not canonical", ErrCADRuntimeAdapterEmpty, adapter)
	}

	manifestAdapter := pkg.Manifest.Adapter
	if manifestAdapter == "" || strings.TrimSpace(manifestAdapter) == "" {
		return fmt.Errorf("%w: manifest adapter is empty", ErrCADRuntimeAdapterMismatch)
	}
	if adapter != manifestAdapter {
		return fmt.Errorf("%w: got=%q want=%q", ErrCADRuntimeAdapterMismatch, adapter, manifestAdapter)
	}

	manifestFilename := runtime.ManifestFilename
	if manifestFilename == "" {
		return ErrCADRuntimeManifestFilenameEmpty
	}
	if err := validateLogicalFilename(manifestFilename); err != nil {
		return fmt.Errorf("%w: %v", ErrCADRuntimeManifestFilenameEmpty, err)
	}
	if manifestFilename != pkg.Manifest.ManifestFilename {
		return fmt.Errorf("%w: got=%q want=%q", ErrCADRuntimeManifestMismatch, manifestFilename, pkg.Manifest.ManifestFilename)
	}

	resultFilename := runtime.ResultFilename
	if resultFilename == "" {
		return ErrCADRuntimeResultFilenameEmpty
	}
	if err := validateLogicalFilename(resultFilename); err != nil {
		return fmt.Errorf("%w: %v", ErrCADRuntimeResultFilenameInvalid, err)
	}

	if resultFilename == pkg.CSV.Filename {
		return fmt.Errorf("%w: result filename %q conflicts with CSV filename", ErrCADRuntimeFilenameConflict, resultFilename)
	}
	if resultFilename == pkg.Manifest.ManifestFilename {
		return fmt.Errorf("%w: result filename %q conflicts with manifest filename", ErrCADRuntimeFilenameConflict, resultFilename)
	}
	for _, output := range pkg.Manifest.Outputs {
		if output.Filename == resultFilename {
			return fmt.Errorf("%w: result filename %q conflicts with declared output filename", ErrCADRuntimeFilenameConflict, resultFilename)
		}
	}

	return nil
}

func validateLogicalFilename(name string) error {
	if name == "" {
		return errors.New("filename is empty")
	}
	if name != strings.TrimSpace(name) {
		return fmt.Errorf("filename %q has surrounding whitespace", name)
	}
	if strings.ContainsAny(name, " \t\r\n") {
		return fmt.Errorf("filename %q contains whitespace", name)
	}
	if strings.ContainsRune(name, 0) {
		return fmt.Errorf("filename %q contains a null byte", name)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("filename %q is not a single logical filename", name)
	}
	if strings.Contains(name, "/") || strings.Contains(name, `\`) {
		return fmt.Errorf("filename %q must not contain path separators", name)
	}
	if filepath.IsAbs(name) {
		return fmt.Errorf("filename %q must not be absolute", name)
	}
	return nil
}

// CloneTableInputs returns a deterministic defensive copy of table metadata.
func CloneTableInputs(in []metadata.TableInputMetadata) []metadata.TableInputMetadata {
	if len(in) == 0 {
		return nil
	}

	out := make([]metadata.TableInputMetadata, len(in))
	copy(out, in)
	sort.Slice(out, func(i, j int) bool {
		if out[i].LogicalID != out[j].LogicalID {
			return out[i].LogicalID < out[j].LogicalID
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Fingerprint < out[j].Fingerprint
	})
	return out
}

// ValidateTableInputs rejects structurally invalid or ambiguous table metadata.
func ValidateTableInputs(in []metadata.TableInputMetadata) error {
	if len(in) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(in))
	for i, table := range in {
		if strings.TrimSpace(table.LogicalID) == "" {
			return fmt.Errorf("%w: tables[%d].logicalId", ErrTableLogicalIDEmpty, i)
		}
		if strings.TrimSpace(table.Name) == "" {
			return fmt.Errorf("%w: tables[%d].name", ErrTableNameEmpty, i)
		}
		if strings.TrimSpace(table.Fingerprint) == "" {
			return fmt.Errorf("%w: tables[%d].fingerprint", ErrTableFingerprintEmpty, i)
		}
		if _, ok := seen[table.LogicalID]; ok {
			return fmt.Errorf("%w: %q", ErrDuplicateTableLogicalID, table.LogicalID)
		}
		seen[table.LogicalID] = struct{}{}
	}

	return nil
}

func manifestRequiresObject(pkg *Package) bool {
	if pkg == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(pkg.Manifest.Adapter), "freecad")
}

func validateManifestOutputs(outputs []planner.ExportManifestOutput, requireStepObject bool) error {
	seenFilenames := make(map[string]int, len(outputs))
	for i, output := range outputs {
		outputType := artifact.NormalizeExportOutputType(output.Type)
		spec, ok := artifact.ExportOutputSpecFor(outputType)
		if !ok {
			return fmt.Errorf("%w: outputs[%d].type %q is not supported", ErrManifestOutputInvalid, i, output.Type)
		}
		filename := strings.TrimSpace(output.Filename)
		if filename == "" {
			return fmt.Errorf("%w: outputs[%d].filename must be a non-empty string", ErrManifestOutputInvalid, i)
		}
		if firstIndex, exists := seenFilenames[filename]; exists {
			return fmt.Errorf("%w: outputs[%d].filename %q duplicates outputs[%d].filename", ErrManifestOutputInvalid, i, output.Filename, firstIndex)
		}
		seenFilenames[filename] = i

		objectName := strings.TrimSpace(output.Object)
		switch {
		case requireStepObject && spec.RequiresObject && objectName == "":
			return fmt.Errorf("%w: outputs[%d].object must be a non-empty string for step outputs", ErrManifestOutputInvalid, i)
		case !spec.RequiresObject && objectName != "":
			return fmt.Errorf("%w: outputs[%d] contains unsupported fields for %s outputs: object", ErrManifestOutputInvalid, i, outputType)
		}
	}
	return nil
}

func validateStepProduct(jobProductKey string, step planner.Step, stepProductKey string) error {
	if derived := job.ProductKeyFromStep(step); derived != "" && derived != jobProductKey {
		return fmt.Errorf("%w: step=%q derived=%q job=%q", ErrMismatchedProductKey, step.Type, derived, jobProductKey)
	}
	if stepProductKey != "" && stepProductKey != jobProductKey {
		return fmt.Errorf("%w: step=%q payload=%q job=%q", ErrMismatchedProductKey, step.Type, stepProductKey, jobProductKey)
	}
	return nil
}

func validateManifestProduct(jobProductKey string, step planner.Step, payload planner.WriteExportManifestPayload) error {
	if err := validateStepProduct(jobProductKey, step, payload.ProductKey); err != nil {
		return err
	}
	if payload.Product.ID != "" && payload.Product.ID != jobProductKey {
		return fmt.Errorf("%w: step=%q manifestProduct=%q job=%q", ErrMismatchedProductKey, step.Type, payload.Product.ID, jobProductKey)
	}
	return nil
}

func manifestProductKey(payload planner.WriteExportManifestPayload) string {
	if payload.ProductKey != "" {
		return payload.ProductKey
	}
	return payload.Product.ID
}

func cloneWriteCSVPayload(payload planner.WriteCSVPayload) planner.WriteCSVPayload {
	return planner.WriteCSVPayload{
		ProductKey: payload.ProductKey,
		Filename:   payload.Filename,
		Headers:    append([]string(nil), payload.Headers...),
		Values:     cloneInterfaceSlice(payload.Values),
	}
}

func cloneRunCADRuntimePayload(payload planner.RunCADRuntimePayload) planner.RunCADRuntimePayload {
	return planner.RunCADRuntimePayload{
		ProductKey:       payload.ProductKey,
		Adapter:          payload.Adapter,
		ManifestFilename: payload.ManifestFilename,
		ResultFilename:   payload.ResultFilename,
	}
}

func cloneWriteExportManifestPayload(payload planner.WriteExportManifestPayload) planner.WriteExportManifestPayload {
	return planner.WriteExportManifestPayload{
		ProductKey:             payload.ProductKey,
		ManifestFilename:       payload.ManifestFilename,
		ManifestProjectionMode: payload.ManifestProjectionMode,
		SchemaVersion:          payload.SchemaVersion,
		PlanHash:               payload.PlanHash,
		Adapter:                payload.Adapter,
		Product:                payload.Product,
		SourceDocument:         payload.SourceDocument,
		Inputs:                 payload.Inputs,
		Values:                 cloneStringAnyMap(payload.Values),
		ParameterAssignments:   append([]planner.ExportManifestParameterAssignment(nil), payload.ParameterAssignments...),
		AssemblyMutations:      cloneMutationCollection(payload.AssemblyMutations),
		PartMutations:          cloneMutationCollection(payload.PartMutations),
		Outputs:                cloneExportManifestOutputs(payload.Outputs),
	}
}

// cloneExportManifestOutputs defensively copies Outputs while preserving
// the distinction between a nil slice and a non-nil empty slice: a
// native-only manifest's zero-length Outputs must remain non-nil so it
// continues to serialize as "outputs":[] (per the aligned runtime manifest
// contract) rather than collapsing to "outputs":null the way
// append(nil, emptySlice...) would.
func cloneExportManifestOutputs(in []planner.ExportManifestOutput) []planner.ExportManifestOutput {
	if in == nil {
		return nil
	}
	return append([]planner.ExportManifestOutput{}, in...)
}

func cloneMutationCollection(in *planner.ExportManifestMutationCollection) *planner.ExportManifestMutationCollection {
	if in == nil {
		return nil
	}

	out := &planner.ExportManifestMutationCollection{
		Parameters:  append([]planner.ExportManifestParameterMutation(nil), in.Parameters...),
		Suppression: append([]planner.ExportManifestSuppressionMutation(nil), in.Suppression...),
		Visibility:  append([]planner.ExportManifestVisibilityMutation(nil), in.Visibility...),
		Deletion:    append([]planner.ExportManifestDeletionMutation(nil), in.Deletion...),
	}
	if in.Properties != nil {
		out.Properties = make([]planner.ExportManifestPropertyMutation, len(in.Properties))
		for i, property := range in.Properties {
			out.Properties[i] = planner.ExportManifestPropertyMutation{
				Object:   property.Object,
				Property: property.Property,
				Value:    cloneInterfaceValue(property.Value),
			}
		}
	}

	return out
}

func cloneStringAnyMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for key, value := range in {
		out[key] = cloneInterfaceValue(value)
	}
	return out
}

func cloneInterfaceSlice(in []interface{}) []interface{} {
	if in == nil {
		return nil
	}
	out := make([]interface{}, len(in))
	for i, value := range in {
		out[i] = cloneInterfaceValue(value)
	}
	return out
}

func cloneInterfaceValue(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		return cloneStringAnyMap(v)
	case []interface{}:
		return cloneInterfaceSlice(v)
	default:
		return v
	}
}
