package projectinput

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"slices"
)

type ResourceKind string

const (
	ResourceKindDSL   ResourceKind = "dsl"
	ResourceKindModel ResourceKind = "model"
	ResourceKindTable ResourceKind = "table"
)

type CapturedResource struct {
	LogicalID    string       `json:"logicalId,omitempty"`
	ResolvedPath string       `json:"resolvedPath"`
	Signature    string       `json:"signature"`
	Kind         ResourceKind `json:"kind"`
}

type CapturedResources struct {
	DSL    CapturedResource   `json:"dsl"`
	Models []CapturedResource `json:"models,omitempty"`
	Tables []CapturedResource `json:"tables,omitempty"`
}

func CaptureResources(resolved *Resolved) (*CapturedResources, error) {
	if resolved == nil {
		return nil, nil
	}

	dslHash, err := computeFileSHA256(resolved.DSLPath)
	if err != nil {
		return nil, fmt.Errorf("hash DSL %q: %w", resolved.DSLPath, err)
	}

	captured := &CapturedResources{
		DSL: CapturedResource{
			ResolvedPath: resolved.DSLPath,
			Signature:    dslHash,
			Kind:         ResourceKindDSL,
		},
	}

	if len(resolved.ModelPaths) > 0 {
		modelIDs := sortedLogicalIDs(resolved.ModelPaths)
		captured.Models = make([]CapturedResource, 0, len(modelIDs))
		for _, logicalID := range modelIDs {
			modelPath := resolved.ModelPaths[logicalID]
			modelHash, err := computeFileSHA256(modelPath)
			if err != nil {
				return nil, fmt.Errorf("hash model %q at %q: %w", logicalID, modelPath, err)
			}
			captured.Models = append(captured.Models, CapturedResource{
				LogicalID:    logicalID,
				ResolvedPath: modelPath,
				Signature:    modelHash,
				Kind:         ResourceKindModel,
			})
		}
	}

	if len(resolved.TablePaths) > 0 {
		projectTables, err := LoadProjectTables(resolved)
		if err != nil {
			return nil, err
		}

		tableIDs := sortedLogicalIDs(resolved.TablePaths)
		captured.Tables = make([]CapturedResource, 0, len(tableIDs))
		for _, logicalID := range tableIDs {
			tablePath := resolved.TablePaths[logicalID]
			loadedTable, ok := projectTables[logicalID]
			if !ok {
				return nil, fmt.Errorf("table %q was resolved at %q but not loaded", logicalID, tablePath)
			}
			fingerprint, err := loadedTable.Fingerprint()
			if err != nil {
				return nil, fmt.Errorf("fingerprint table %q at %q: %w", logicalID, tablePath, err)
			}
			captured.Tables = append(captured.Tables, CapturedResource{
				LogicalID:    logicalID,
				ResolvedPath: tablePath,
				Signature:    fingerprint,
				Kind:         ResourceKindTable,
			})
		}
	}

	return captured, nil
}

func CombinedModelSignature(captured *CapturedResources) (string, error) {
	if captured == nil || len(captured.Models) == 0 {
		return "", nil
	}

	models := cloneCapturedResourceList(captured.Models)
	data, err := json.Marshal(models)
	if err != nil {
		return "", fmt.Errorf("marshal model resources: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func CloneCapturedResources(in *CapturedResources) *CapturedResources {
	if in == nil {
		return nil
	}

	return &CapturedResources{
		DSL:    in.DSL,
		Models: cloneCapturedResourceList(in.Models),
		Tables: cloneCapturedResourceList(in.Tables),
	}
}

func cloneCapturedResourceList(in []CapturedResource) []CapturedResource {
	if len(in) == 0 {
		return nil
	}
	out := make([]CapturedResource, len(in))
	copy(out, in)
	slices.SortFunc(out, compareCapturedResource)
	return out
}

func compareCapturedResource(a, b CapturedResource) int {
	if a.LogicalID != b.LogicalID {
		return compareStrings(a.LogicalID, b.LogicalID)
	}
	if a.ResolvedPath != b.ResolvedPath {
		return compareStrings(a.ResolvedPath, b.ResolvedPath)
	}
	if a.Signature != b.Signature {
		return compareStrings(a.Signature, b.Signature)
	}
	return compareStrings(string(a.Kind), string(b.Kind))
}

func sortedLogicalIDs(paths map[string]string) []string {
	logicalIDs := make([]string, 0, len(paths))
	for logicalID := range paths {
		logicalIDs = append(logicalIDs, logicalID)
	}
	slices.Sort(logicalIDs)
	return logicalIDs
}

func compareStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func computeFileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
