package cadruntime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"parametron/internal/engine/adapter/freecad"
	"parametron/internal/engine/integrity"
	"parametron/internal/engine/resourcebinding"
)

const (
	// FreeCADIntegrityRequestFilename is the Engine-authored per-resource
	// integrity request read by the runtime before mutation.
	FreeCADIntegrityRequestFilename = "prm.integrity-request.json"

	// FreeCADIntegrityRequestSchemaVersion is the single supported schema version
	// of the integrity request contract.
	FreeCADIntegrityRequestSchemaVersion = "1.0"

	// FreeCADIntegrityRequestDirectoryName is the deterministic attempt-scoped
	// directory under the orchestration root that holds the request document.
	FreeCADIntegrityRequestDirectoryName = "integrity"

	// Evidence resource status values. "observed" is the implicit default for a
	// reported resource; the others let a runtime state that it could not
	// establish a declared resource's baseline without claiming content identity.
	FreeCADIntegrityStatusObserved    = "observed"
	FreeCADIntegrityStatusUnavailable = "unavailable"
	FreeCADIntegrityStatusMissing     = "missing"
	FreeCADIntegrityStatusUnsupported = "unsupported"
)

// FreeCADIntegrityRequest is the Engine-authored expectation for the declared
// resources of one attempt. It states which resources the runtime may report, the
// attempt-relative coordinate Engine materialized for each, the expected
// pre-mutation baseline content identity, and whether a persisted configured
// result must be reported.
type FreeCADIntegrityRequest struct {
	SchemaVersion string                            `json:"schemaVersion"`
	Resources     []FreeCADIntegrityRequestResource `json:"resources"`
}

// FreeCADIntegrityRequestResource is one requested declared resource.
type FreeCADIntegrityRequestResource struct {
	LogicalID              string `json:"logicalId"`
	Kind                   string `json:"kind"`
	MaterializedCoordinate string `json:"materializedCoordinate"`
	BaselineSHA256         string `json:"baselineSha256"`
	ExpectsPersistedResult bool   `json:"expectsPersistedResult"`
}

// ComposeFreeCADIntegrityRequest builds the request value and its exact bytes
// from verified materialized baselines. Resources are ordered by declared
// logical resource identity and carry no host paths, so equivalent inputs yield
// byte-identical documents.
func ComposeFreeCADIntegrityRequest(
	baselines []freecad.FreeCADRuntimeMaterializedBaseline,
	primaryCoordinate string,
) (FreeCADIntegrityRequest, []byte, error) {
	resources := make([]FreeCADIntegrityRequestResource, 0, len(baselines))
	seen := make(map[string]struct{}, len(baselines))
	for _, baseline := range baselines {
		logicalID := baseline.Binding.LogicalID
		if logicalID == "" {
			return FreeCADIntegrityRequest{}, nil, fmt.Errorf("integrity request resource requires a logical ID")
		}
		if _, exists := seen[logicalID]; exists {
			return FreeCADIntegrityRequest{}, nil, fmt.Errorf("integrity request contains duplicate logical ID %q", logicalID)
		}
		seen[logicalID] = struct{}{}
		resources = append(resources, FreeCADIntegrityRequestResource{
			LogicalID:              logicalID,
			Kind:                   string(baseline.Binding.Kind),
			MaterializedCoordinate: baseline.Coordinate,
			BaselineSHA256:         baseline.Digest.String(),
			ExpectsPersistedResult: baseline.Coordinate == primaryCoordinate,
		})
	}
	sort.SliceStable(resources, func(i, j int) bool { return resources[i].LogicalID < resources[j].LogicalID })

	request := FreeCADIntegrityRequest{
		SchemaVersion: FreeCADIntegrityRequestSchemaVersion,
		Resources:     resources,
	}
	data, err := marshalFreeCADIntegrityJSON(request)
	if err != nil {
		return FreeCADIntegrityRequest{}, nil, err
	}
	if _, err := ParseFreeCADIntegrityRequest(data); err != nil {
		return FreeCADIntegrityRequest{}, nil, err
	}
	return request, data, nil
}

// ParseFreeCADIntegrityRequest strictly decodes and validates an integrity
// request document.
func ParseFreeCADIntegrityRequest(data []byte) (*FreeCADIntegrityRequest, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var request FreeCADIntegrityRequest
	if err := decoder.Decode(&request); err != nil {
		return nil, &integrity.Failure{Code: integrity.CodeEvidenceInvalid, Err: err,
			Message: "integrity request is not valid JSON for the supported contract"}
	}
	if err := ensureNoTrailingFreeCADIntegrityJSON(decoder); err != nil {
		return nil, &integrity.Failure{Code: integrity.CodeEvidenceInvalid, Err: err,
			Message: "integrity request contains trailing content"}
	}
	if request.SchemaVersion != FreeCADIntegrityRequestSchemaVersion {
		return nil, &integrity.Failure{Code: integrity.CodeEvidenceSchemaUnsupported,
			Message: fmt.Sprintf("integrity request schema version %q is not supported", request.SchemaVersion)}
	}
	if len(request.Resources) == 0 {
		return nil, &integrity.Failure{Code: integrity.CodeEvidenceInvalid,
			Message: "integrity request must declare at least one resource"}
	}

	persistedResults := 0
	for index := range request.Resources {
		resource := &request.Resources[index]
		if resource.LogicalID == "" || resource.LogicalID != strings.TrimSpace(resource.LogicalID) {
			return nil, &integrity.Failure{Code: integrity.CodeEvidenceInvalid, LogicalID: resource.LogicalID,
				Message: fmt.Sprintf("integrity request resources[%d].logicalId must be a non-empty unspaced string", index)}
		}
		if resource.Kind != string(resourcebinding.ResourceKindModel) {
			return nil, &integrity.Failure{Code: integrity.CodeEvidenceInvalid, LogicalID: resource.LogicalID,
				Message: fmt.Sprintf("integrity request resources[%d].kind %q is not a supported CAD resource kind", index, resource.Kind)}
		}
		if err := resourcebinding.ValidateCoordinate(resource.MaterializedCoordinate); err != nil {
			return nil, &integrity.Failure{Code: integrity.CodeEvidenceInvalid, LogicalID: resource.LogicalID, Err: err,
				Message: fmt.Sprintf("integrity request resources[%d].materializedCoordinate is not canonical", index)}
		}
		if err := integrity.ValidateDigest(resource.BaselineSHA256); err != nil {
			return nil, &integrity.Failure{Code: integrity.CodeEvidenceInvalid, LogicalID: resource.LogicalID, Err: err,
				Message: fmt.Sprintf("integrity request resources[%d].baselineSha256 is not canonical", index)}
		}
		if resource.ExpectsPersistedResult {
			persistedResults++
		}
		if index > 0 && request.Resources[index-1].LogicalID >= resource.LogicalID {
			return nil, &integrity.Failure{Code: integrity.CodeEvidenceInvalid, LogicalID: resource.LogicalID,
				Message: "integrity request resources must be unique and ordered by logical ID"}
		}
	}
	if persistedResults != 1 {
		return nil, &integrity.Failure{Code: integrity.CodeEvidenceInvalid,
			Message: fmt.Sprintf("integrity request must expect exactly one persisted result, got %d", persistedResults)}
	}

	return &request, nil
}

// FreeCADIntegrityRequestPath returns the deterministic attempt-scoped path of
// the integrity request document under the orchestration root.
func FreeCADIntegrityRequestPath(root, attemptID string) (string, error) {
	if err := requireCanonicalAbsolutePath("IntegrityRequestRoot", root); err != nil {
		return "", err
	}
	if err := validateIntegrityRequestAttemptID(attemptID); err != nil {
		return "", err
	}
	return filepath.Join(root, FreeCADIntegrityRequestDirectoryName, attemptID, FreeCADIntegrityRequestFilename), nil
}

// WriteFreeCADIntegrityRequest writes the integrity request bytes for one attempt
// into the Engine-owned orchestration root and returns their absolute path.
func WriteFreeCADIntegrityRequest(root, attemptID string, data []byte) (string, error) {
	path, err := FreeCADIntegrityRequestPath(root, attemptID)
	if err != nil {
		return "", err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", fmt.Errorf("create integrity request directory: %w", err)
	}

	tmpFile, err := os.CreateTemp(directory, "."+FreeCADIntegrityRequestFilename+".tmp-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return "", err
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	return path, nil
}

func validateIntegrityRequestAttemptID(attemptID string) error {
	if attemptID == "" || strings.TrimSpace(attemptID) != attemptID {
		return fmt.Errorf("integrity request attempt ID must be non-empty and unspaced")
	}
	if strings.ContainsAny(attemptID, "/\\\x00") || attemptID == "." || attemptID == ".." {
		return fmt.Errorf("integrity request attempt ID %q is not a single path segment", attemptID)
	}
	if filepath.Base(attemptID) != attemptID || filepath.IsAbs(attemptID) {
		return fmt.Errorf("integrity request attempt ID %q is not a single path segment", attemptID)
	}
	return nil
}
