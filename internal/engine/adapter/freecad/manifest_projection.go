package freecad

import (
	"encoding/json"
	"fmt"
	"math"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/artifact"
)

var windowsAbsolutePathPattern = regexp.MustCompile(`^[A-Za-z]:[/\\]`)

const freeCADRuntimeOutputPathRoot = "outputs"

type FreeCADRuntimeExportManifest struct {
	SchemaVersion        string                              `json:"schemaVersion"`
	SourceDocument       string                              `json:"sourceDocument"`
	ParameterAssignments []FreeCADRuntimeParameterAssignment `json:"parameterAssignments"`
	AssemblyMutations    *FreeCADRuntimeMutationCollection   `json:"assemblyMutations,omitempty"`
	PartMutations        *FreeCADRuntimeMutationCollection   `json:"partMutations,omitempty"`
	Outputs              []FreeCADRuntimeManifestOutput      `json:"outputs"`
}

type FreeCADRuntimeMutationCollection struct {
	Suppression []FreeCADRuntimeSuppressionMutation `json:"suppression,omitempty"`
	Visibility  []FreeCADRuntimeVisibilityMutation  `json:"visibility,omitempty"`
	Deletion    []FreeCADRuntimeDeletionMutation    `json:"deletion,omitempty"`
}

type FreeCADRuntimeSuppressionMutation struct {
	Object     string `json:"object"`
	Suppressed bool   `json:"suppressed"`
}

type FreeCADRuntimeVisibilityMutation struct {
	Object  string `json:"object"`
	Visible bool   `json:"visible"`
}

type FreeCADRuntimeDeletionMutation struct {
	Object string `json:"object"`
}

type FreeCADRuntimeParameterAssignment struct {
	Target    string `json:"target"`
	Value     any    `json:"value"`
	ValueKind string `json:"valueKind"`
}

type FreeCADRuntimeManifestOutput struct {
	ID     string `json:"id"`
	Format string `json:"format"`
	Path   string `json:"path"`
}

type FreeCADRuntimeManifestProjectionError struct {
	Field   string
	Message string
}

func (e *FreeCADRuntimeManifestProjectionError) Error() string {
	if e == nil {
		return ""
	}
	if e.Field == "" {
		return e.Message
	}
	return e.Field + " " + e.Message
}

func ProjectFreeCADRuntimeExportManifest(payload planner.WriteExportManifestPayload) (*FreeCADRuntimeExportManifest, error) {
	schemaVersion := strings.TrimSpace(payload.SchemaVersion)
	if err := ValidateFreeCADRuntimeExportManifestSchema(payload); err != nil {
		return nil, err
	}

	sourceDocument, err := normalizeFreeCADRuntimeSourceDocumentPath(payload.SourceDocument)
	if err != nil {
		return nil, err
	}

	assignments, err := projectFreeCADRuntimeParameterAssignments(payload)
	if err != nil {
		return nil, err
	}

	outputs, err := projectFreeCADRuntimeManifestOutputs(payload.Outputs)
	if err != nil {
		return nil, err
	}

	assemblyMutations := projectFreeCADRuntimeMutationCollection(payload.AssemblyMutations)
	partMutations := projectFreeCADRuntimeMutationCollection(payload.PartMutations)

	return &FreeCADRuntimeExportManifest{
		SchemaVersion:        schemaVersion,
		SourceDocument:       sourceDocument,
		ParameterAssignments: assignments,
		AssemblyMutations:    assemblyMutations,
		PartMutations:        partMutations,
		Outputs:              outputs,
	}, nil
}

func ValidateFreeCADRuntimeExportManifestSchema(payload planner.WriteExportManifestPayload) error {
	schemaVersion := payload.SchemaVersion
	switch schemaVersion {
	case planner.ExportManifestSchemaVersion:
	case "":
		return projectionError("schemaVersion", "is required")
	default:
		return projectionErrorf("schemaVersion", "%q is not supported", schemaVersion)
	}
	return nil
}

func hasPlannerRuntimeTargetMutations(collection *planner.ExportManifestMutationCollection) bool {
	return collection != nil && (len(collection.Suppression) > 0 || len(collection.Visibility) > 0 || len(collection.Deletion) > 0)
}

func projectFreeCADRuntimeMutationCollection(collection *planner.ExportManifestMutationCollection) *FreeCADRuntimeMutationCollection {
	if !hasPlannerRuntimeTargetMutations(collection) {
		return nil
	}
	projected := &FreeCADRuntimeMutationCollection{
		Suppression: make([]FreeCADRuntimeSuppressionMutation, 0, len(collection.Suppression)),
		Visibility:  make([]FreeCADRuntimeVisibilityMutation, 0, len(collection.Visibility)),
		Deletion:    make([]FreeCADRuntimeDeletionMutation, 0, len(collection.Deletion)),
	}
	for _, mutation := range collection.Suppression {
		projected.Suppression = append(projected.Suppression, FreeCADRuntimeSuppressionMutation{Object: mutation.Object, Suppressed: mutation.Suppressed})
	}
	for _, mutation := range collection.Visibility {
		projected.Visibility = append(projected.Visibility, FreeCADRuntimeVisibilityMutation{Object: mutation.Object, Visible: mutation.Visible})
	}
	for _, mutation := range collection.Deletion {
		projected.Deletion = append(projected.Deletion, FreeCADRuntimeDeletionMutation{Object: mutation.Object})
	}
	return projected
}

func MarshalFreeCADRuntimeExportManifestJSON(manifest *FreeCADRuntimeExportManifest) ([]byte, error) {
	if manifest == nil {
		return nil, projectionError("manifest", "is required")
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal FreeCAD runtime export manifest: %w", err)
	}
	return append(data, '\n'), nil
}

func projectFreeCADRuntimeParameterAssignments(payload planner.WriteExportManifestPayload) ([]FreeCADRuntimeParameterAssignment, error) {
	assignments := make([]FreeCADRuntimeParameterAssignment, 0, len(payload.ParameterAssignments))
	for i, assignment := range payload.ParameterAssignments {
		field := fmt.Sprintf("parameterAssignments[%d]", i)
		name := strings.TrimSpace(assignment.Name)
		if name == "" {
			return nil, projectionError(field+".name", "is required")
		}
		if assignment.Type != "number" {
			return nil, projectionErrorf(field+".type", "%q is not supported", assignment.Type)
		}
		if assignment.Unit != "mm" {
			return nil, projectionErrorf(field+".unit", "%q is not supported", assignment.Unit)
		}
		if math.IsNaN(assignment.Value) || math.IsInf(assignment.Value, 0) {
			return nil, projectionError(field+".value", "must be finite")
		}

		target, err := resolveFreeCADRuntimeParameterTarget(field, name, assignment.Target, payload)
		if err != nil {
			return nil, err
		}

		assignments = append(assignments, FreeCADRuntimeParameterAssignment{
			Target:    target,
			Value:     assignment.Value,
			ValueKind: "float",
		})
	}
	return assignments, nil
}

func resolveFreeCADRuntimeParameterTarget(field, name, explicitTarget string, payload planner.WriteExportManifestPayload) (string, error) {
	if strings.TrimSpace(explicitTarget) != "" {
		return validateFreeCADRuntimeParameterTarget(field, explicitTarget)
	}

	matches := make([]planner.ExportManifestParameterMutation, 0, 2)
	if payload.AssemblyMutations != nil {
		matches = appendMatchingFreeCADRuntimeMutations(matches, payload.AssemblyMutations.Parameters, name)
	}
	if payload.PartMutations != nil {
		matches = appendMatchingFreeCADRuntimeMutations(matches, payload.PartMutations.Parameters, name)
	}

	switch len(matches) {
	case 0:
		return "", projectionErrorf(field, "missing explicit CAD target for parameter %q", name)
	case 1:
	default:
		return "", projectionErrorf(field, "ambiguous CAD target for parameter %q", name)
	}

	mutation := matches[0]
	target, err := buildFreeCADRuntimeParameterTarget(field, mutation.Object, mutation.Property)
	if err != nil {
		return "", err
	}
	return target, nil
}

func validateFreeCADRuntimeParameterTarget(field, rawTarget string) (string, error) {
	target := strings.TrimSpace(rawTarget)
	if strings.Count(target, ".") != 1 {
		return "", projectionErrorf(field+".target", "%q is malformed", target)
	}

	segments := strings.Split(target, ".")
	return buildFreeCADRuntimeParameterTarget(field, segments[0], segments[1])
}

func appendMatchingFreeCADRuntimeMutations(dst []planner.ExportManifestParameterMutation, mutations []planner.ExportManifestParameterMutation, name string) []planner.ExportManifestParameterMutation {
	for _, mutation := range mutations {
		if mutation.ValueParam == name {
			dst = append(dst, mutation)
		}
	}
	return dst
}

func buildFreeCADRuntimeParameterTarget(field, object, property string) (string, error) {
	if err := validateFreeCADRuntimeTargetSegment(object); err != nil {
		return "", projectionErrorf(field+".target", "has malformed object segment: %v", err)
	}
	if err := validateFreeCADRuntimeTargetSegment(property); err != nil {
		return "", projectionErrorf(field+".target", "has malformed property segment: %v", err)
	}

	target := object + "." + property
	if strings.Count(target, ".") != 1 {
		return "", projectionErrorf(field+".target", "%q is malformed", target)
	}
	return target, nil
}

func validateFreeCADRuntimeTargetSegment(segment string) error {
	if segment == "" {
		return fmt.Errorf("segment is required")
	}
	if strings.ContainsAny(segment, ".\\/") {
		return fmt.Errorf("segment contains an unsupported separator")
	}
	if strings.Contains(segment, "\x00") {
		return fmt.Errorf("segment contains null byte")
	}
	for _, r := range segment {
		if unicode.IsSpace(r) {
			return fmt.Errorf("segment contains whitespace")
		}
	}
	return nil
}

func projectFreeCADRuntimeManifestOutputs(outputs []planner.ExportManifestOutput) ([]FreeCADRuntimeManifestOutput, error) {
	if len(outputs) == 0 {
		return []FreeCADRuntimeManifestOutput{}, nil
	}

	projected := make([]FreeCADRuntimeManifestOutput, 0, len(outputs))
	seenPaths := make(map[string]int, len(outputs))
	seenIDs := make(map[string]int, len(outputs))

	for i, output := range outputs {
		field := fmt.Sprintf("outputs[%d]", i)
		format := artifact.NormalizeExportOutputType(output.Type)
		if !artifact.IsSupportedExportOutputType(format) {
			return nil, projectionErrorf(field+".format", "%q is not supported", output.Type)
		}

		outputPath, err := normalizeFreeCADRuntimeOutputPath(field+".path", output.Filename)
		if err != nil {
			return nil, err
		}
		if firstIndex, exists := seenPaths[outputPath]; exists {
			return nil, projectionErrorf(field+".path", "%q duplicates outputs[%d].path", outputPath, firstIndex)
		}
		seenPaths[outputPath] = i

		id, err := freeCADRuntimeManifestOutputID(field, output, outputPath, format, seenIDs)
		if err != nil {
			return nil, err
		}

		projected = append(projected, FreeCADRuntimeManifestOutput{
			ID:     id,
			Format: format,
			Path:   outputPath,
		})
	}
	return projected, nil
}

// freeCADRuntimeManifestOutputID projects the aligned outputs[].id.
//
// For STEP outputs, id is the exact FreeCAD internal object-name selector from
// ExportManifestOutput.Object. It is not derived from the path, sanitized, or
// collision-suffixed. Duplicate STEP selectors with distinct paths are valid.
//
// Non-STEP outputs keep synthetic path-derived IDs.
func freeCADRuntimeManifestOutputID(field string, output planner.ExportManifestOutput, outputPath, format string, seenIDs map[string]int) (string, error) {
	if format == artifact.ExportOutputTypeSTEP {
		return validateFreeCADRuntimeSTEPOutputSelector(field+".object", output.Object)
	}
	return freeCADRuntimeOutputID(outputPath, format, seenIDs), nil
}

func validateFreeCADRuntimeSTEPOutputSelector(field, rawObject string) (string, error) {
	selector := strings.TrimSpace(rawObject)
	if selector == "" {
		return "", projectionError(field, "is required for STEP output selector")
	}
	if err := validateFreeCADRuntimeTargetSegment(selector); err != nil {
		return "", projectionError(field, "has malformed STEP output selector")
	}
	return selector, nil
}

func freeCADRuntimeOutputID(outputPath, format string, seen map[string]int) string {
	base := path.Base(outputPath)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	id := sanitizeFreeCADRuntimeOutputID(stem + "-" + format)
	if id == "" {
		id = format
	}

	count := seen[id] + 1
	seen[id] = count
	if count == 1 {
		return id
	}

	for {
		candidate := fmt.Sprintf("%s-%d", id, count)
		if seen[candidate] == 0 {
			seen[candidate] = 1
			return candidate
		}
		count++
		seen[id] = count
	}
}

func sanitizeFreeCADRuntimeOutputID(value string) string {
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '_' || r == '-' || r == '.':
			builder.WriteRune(r)
		default:
			builder.WriteRune('_')
		}
	}
	return strings.Trim(builder.String(), "_.-")
}

func normalizeFreeCADRuntimeSourceDocumentPath(rawPath string) (string, error) {
	return normalizeFreeCADRuntimeRelativePath("sourceDocument", rawPath)
}

func normalizeFreeCADRuntimeOutputPath(field, rawPath string) (string, error) {
	outputPath, err := normalizeFreeCADRuntimeRelativePath(field, rawPath)
	if err != nil {
		return "", err
	}

	// Native FreeCAD runtime outputs are confined to the manifest-declared
	// artifact subtree, separate from sourceDocument and other working-copy files.
	outputRoot := freeCADRuntimeOutputPathRoot + "/"
	if outputPath == freeCADRuntimeOutputPathRoot || !strings.HasPrefix(outputPath, outputRoot) {
		return "", projectionErrorf(field, "must be contained under %q", outputRoot)
	}
	return outputPath, nil
}

func normalizeFreeCADRuntimeRelativePath(field, rawPath string) (string, error) {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" {
		return "", projectionError(field, "is required")
	}
	if strings.Contains(trimmed, "\x00") {
		return "", projectionError(field, "contains null byte")
	}
	if filepath.IsAbs(trimmed) || path.IsAbs(trimmed) || strings.HasPrefix(trimmed, `\`) || windowsAbsolutePathPattern.MatchString(trimmed) {
		return "", projectionError(field, "must be relative")
	}

	slashPath := strings.ReplaceAll(trimmed, "\\", "/")
	for _, segment := range strings.Split(slashPath, "/") {
		if segment == ".." {
			return "", projectionError(field, "must not contain parent traversal")
		}
	}

	cleaned := path.Clean(slashPath)
	switch {
	case cleaned == ".":
		return "", projectionError(field, "is required")
	case cleaned == ".." || strings.HasPrefix(cleaned, "../"):
		return "", projectionError(field, "must not contain parent traversal")
	case path.IsAbs(cleaned):
		return "", projectionError(field, "must be relative")
	}
	return cleaned, nil
}

func projectionError(field, message string) error {
	return &FreeCADRuntimeManifestProjectionError{Field: field, Message: message}
}

func projectionErrorf(field, format string, args ...any) error {
	return projectionError(field, fmt.Sprintf(format, args...))
}
