package adapter

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/artifact"
)

// ExecuteWriteCSV writes the CAD-neutral WriteCSV step payload as a single-row
// CSV file (a header row followed by one value row) into outputDir, creating
// outputDir if necessary.
func ExecuteWriteCSV(outputDir string, step planner.Step) error {
	payload, ok := step.Payload.(planner.WriteCSVPayload)
	if !ok {
		return fmt.Errorf("invalid payload type for WriteCSV: %T", step.Payload)
	}

	filename := payload.Filename
	headers := payload.Headers
	values := payload.Values

	// Create output directory
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	filePath := filepath.Join(outputDir, filename)
	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	if err := writer.Write(headers); err != nil {
		return fmt.Errorf("failed to write headers: %w", err)
	}

	row := make([]string, len(values))
	for i, v := range values {
		row[i] = fmt.Sprintf("%v", v)
	}

	if err := writer.Write(row); err != nil {
		return fmt.Errorf("failed to write values: %w", err)
	}

	slog.Info("Generated CSV", "path", filePath)
	return nil
}

// MarshalGenericExportManifest encodes payload as indented JSON in the generic
// export-manifest shape, terminated by a trailing newline.
func MarshalGenericExportManifest(payload planner.WriteExportManifestPayload) ([]byte, error) {
	data, err := json.MarshalIndent(genericExportManifestPayloadFromPlanner(payload), "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to encode export manifest: %w", err)
	}
	return append(data, '\n'), nil
}

type genericExportManifestPayload struct {
	SchemaVersion        string                                      `json:"schemaVersion"`
	PlanHash             string                                      `json:"planHash"`
	Adapter              string                                      `json:"adapter,omitempty"`
	Product              planner.ExportManifestProduct               `json:"product"`
	Inputs               planner.ExportManifestInputs                `json:"inputs"`
	Values               map[string]interface{}                      `json:"values"`
	ParameterAssignments []planner.ExportManifestParameterAssignment `json:"parameterAssignments"`
	AssemblyMutations    *planner.ExportManifestMutationCollection   `json:"assemblyMutations,omitempty"`
	PartMutations        *planner.ExportManifestMutationCollection   `json:"partMutations,omitempty"`
	Outputs              []planner.ExportManifestOutput              `json:"outputs"`
}

func genericExportManifestPayloadFromPlanner(payload planner.WriteExportManifestPayload) genericExportManifestPayload {
	return genericExportManifestPayload{
		SchemaVersion:        payload.SchemaVersion,
		PlanHash:             payload.PlanHash,
		Adapter:              payload.Adapter,
		Product:              payload.Product,
		Inputs:               payload.Inputs,
		Values:               payload.Values,
		ParameterAssignments: payload.ParameterAssignments,
		AssemblyMutations:    payload.AssemblyMutations,
		PartMutations:        payload.PartMutations,
		Outputs:              payload.Outputs,
	}
}

// WriteExportManifestFile writes exact manifest bytes through a temporary file in
// outputDir, then renames it to filePath. It removes the temporary file on failure.
// An empty errorPrefix preserves generic export-manifest errors; a non-empty
// prefix supplies caller context for create, write, close, and finalize errors.
// Underlying filesystem errors remain wrapped. Directories must already exist.
func WriteExportManifestFile(outputDir, filePath string, data []byte, errorPrefix string) error {
	tmpFile, err := os.CreateTemp(outputDir, ".export-manifest-*.tmp")
	if err != nil {
		if errorPrefix != "" {
			return fmt.Errorf("%s: failed to create temp file: %w", errorPrefix, err)
		}
		return fmt.Errorf("failed to create temp export manifest file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		_ = os.Remove(tmpPath)
		if errorPrefix != "" {
			return fmt.Errorf("%s: failed to write temp file: %w", errorPrefix, err)
		}
		return fmt.Errorf("failed to write export manifest: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		if errorPrefix != "" {
			return fmt.Errorf("%s: failed to close temp file: %w", errorPrefix, err)
		}
		return fmt.Errorf("failed to close temp export manifest file: %w", err)
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		_ = os.Remove(tmpPath)
		if errorPrefix != "" {
			return fmt.Errorf("%s: failed to finalize file: %w", errorPrefix, err)
		}
		return fmt.Errorf("failed to finalize export manifest file: %w", err)
	}

	return nil
}

// ValidateGenericExportManifest validates an export-manifest payload's outputs
// and parameter assignments using the generic, CAD-neutral rules. requireStepObject
// controls whether outputs whose type requires an object (e.g. step) must supply
// a non-empty object name.
func ValidateGenericExportManifest(payload planner.WriteExportManifestPayload, requireStepObject bool) error {
	if err := validateManifestOutputs(payload.Outputs, requireStepObject); err != nil {
		return err
	}
	return validateParameterAssignments(payload.ParameterAssignments)
}

func validateParameterAssignments(assignments []planner.ExportManifestParameterAssignment) error {
	seenNames := make(map[string]int, len(assignments))
	for i, assignment := range assignments {
		normalizedName := strings.TrimSpace(assignment.Name)
		if normalizedName == "" {
			return fmt.Errorf("parameterAssignments[%d].name is required", i)
		}
		if assignment.Type != "number" {
			return fmt.Errorf("parameterAssignments[%d].type must be %q, got %q", i, "number", assignment.Type)
		}
		if assignment.Unit != "mm" {
			return fmt.Errorf("parameterAssignments[%d].unit must be %q, got %q", i, "mm", assignment.Unit)
		}
		if firstIndex, exists := seenNames[normalizedName]; exists {
			return fmt.Errorf("parameterAssignments[%d].name %q duplicates parameterAssignments[%d].name", i, normalizedName, firstIndex)
		}
		seenNames[normalizedName] = i
	}
	return nil
}

func validateManifestOutputs(outputs []planner.ExportManifestOutput, requireStepObject bool) error {
	seenFilenames := make(map[string]int, len(outputs))
	for i, output := range outputs {
		outputType := artifact.NormalizeExportOutputType(output.Type)
		spec, ok := artifact.ExportOutputSpecFor(outputType)
		if !ok {
			return fmt.Errorf("manifest outputs[%d].type %q is not supported", i, output.Type)
		}
		if strings.TrimSpace(output.Filename) == "" {
			return fmt.Errorf("manifest outputs[%d].filename must be a non-empty string", i)
		}
		if firstIndex, exists := seenFilenames[output.Filename]; exists {
			return fmt.Errorf("manifest outputs[%d].filename %q duplicates outputs[%d].filename", i, output.Filename, firstIndex)
		}
		seenFilenames[output.Filename] = i

		objectName := strings.TrimSpace(output.Object)
		switch {
		case requireStepObject && spec.RequiresObject && objectName == "":
			return fmt.Errorf("manifest outputs[%d].object must be a non-empty string for step outputs", i)
		case !spec.RequiresObject && objectName != "":
			return fmt.Errorf("manifest outputs[%d] contains unsupported fields for %s outputs: object", i, outputType)
		}

		if outputType != "csv" {
			continue
		}

		if target := strings.TrimSpace(output.Target); output.Target != "" && target == "" {
			return fmt.Errorf("manifest outputs[%d].target must be a non-empty string for csv outputs", i)
		}

		if output.Rollup != "" && output.Rollup != "flat" && output.Rollup != "assembly" {
			return fmt.Errorf("manifest outputs[%d].rollup %q is not supported for csv outputs", i, output.Rollup)
		}

		if output.Columns != nil {
			if len(output.Columns) == 0 {
				return fmt.Errorf("manifest outputs[%d].columns must contain at least one item for csv outputs", i)
			}

			seenColumns := make(map[string]int, len(output.Columns))
			for j, column := range output.Columns {
				if strings.TrimSpace(column) == "" {
					return fmt.Errorf("manifest outputs[%d].columns[%d] must be a non-empty string for csv outputs", i, j)
				}
				switch column {
				case "name", "label", "type_id", "quantity", "assembly_path":
				default:
					return fmt.Errorf("manifest outputs[%d].columns[%d] %q is not supported for csv outputs", i, j, column)
				}
				if firstIndex, exists := seenColumns[column]; exists {
					return fmt.Errorf("manifest outputs[%d].columns[%d] %q duplicates columns[%d] for csv outputs", i, j, column, firstIndex)
				}
				seenColumns[column] = j
			}
		}
	}
	return nil
}
