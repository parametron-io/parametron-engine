package cadruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"parametron/internal/engine/adapter/freecad"
	"parametron/internal/engine/integrity"
	"parametron/internal/engine/resourcebinding"
)

const (
	// FreeCADIntegrityEvidenceFilename is the runtime-authored per-resource
	// baseline and persisted-result evidence read by Engine after persistence.
	FreeCADIntegrityEvidenceFilename = "prm.integrity-evidence.json"

	// FreeCADIntegritySchemaVersion is the single supported schema version of the
	// integrity evidence contract.
	FreeCADIntegritySchemaVersion = "1.0"
)

// FreeCADIntegrityEvidence is the runtime-authored report of observed baseline
// and persisted configured-result identity for declared resources.
type FreeCADIntegrityEvidence struct {
	SchemaVersion string                             `json:"schemaVersion"`
	Resources     []FreeCADIntegrityEvidenceResource `json:"resources"`
}

// FreeCADIntegrityEvidenceResource is one reported resource. LogicalID is
// optional: Engine joins the report to declared resource identity through the
// materialized coordinate it assigned, and only accepts a logical ID that agrees
// with that join. Status defaults to "observed"; the other statuses state that
// the runtime could not establish the resource's baseline.
type FreeCADIntegrityEvidenceResource struct {
	LogicalID              string                           `json:"logicalId,omitempty"`
	MaterializedCoordinate string                           `json:"materializedCoordinate"`
	Status                 string                           `json:"status,omitempty"`
	BaselineSHA256         string                           `json:"baselineSha256"`
	PersistedResult        *FreeCADIntegrityPersistedResult `json:"persistedResult"`
}

// FreeCADIntegrityPersistedResult reports post-persistence configured-result
// identity. It is never the pre-mutation baseline identity.
type FreeCADIntegrityPersistedResult struct {
	Coordinate string `json:"coordinate"`
	SHA256     string `json:"sha256"`
}

// FreeCADIntegrityEvidencePath returns the attempt-relative location of the
// runtime integrity evidence inside the output directory.
func FreeCADIntegrityEvidencePath(layout freecad.FreeCADRuntimeWorkingCopyLayout) string {
	return filepath.Join(layout.OutputDir, FreeCADIntegrityEvidenceFilename)
}

// ParseFreeCADIntegrityEvidence strictly decodes runtime integrity evidence.
func ParseFreeCADIntegrityEvidence(data []byte) (*FreeCADIntegrityEvidence, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var evidence FreeCADIntegrityEvidence
	if err := decoder.Decode(&evidence); err != nil {
		return nil, &integrity.Failure{Code: integrity.CodeEvidenceInvalid, Err: err,
			Message: "integrity evidence is not valid JSON for the supported contract"}
	}
	if err := ensureNoTrailingFreeCADIntegrityJSON(decoder); err != nil {
		return nil, &integrity.Failure{Code: integrity.CodeEvidenceInvalid, Err: err,
			Message: "integrity evidence contains trailing content"}
	}
	if evidence.SchemaVersion != FreeCADIntegritySchemaVersion {
		return nil, &integrity.Failure{Code: integrity.CodeEvidenceSchemaUnsupported,
			Message: fmt.Sprintf("integrity evidence schema version %q is not supported", evidence.SchemaVersion)}
	}
	if len(evidence.Resources) == 0 {
		return nil, &integrity.Failure{Code: integrity.CodeEvidenceInvalid,
			Message: "integrity evidence must declare at least one resource"}
	}

	for index := range evidence.Resources {
		resource := &evidence.Resources[index]
		if resource.LogicalID != strings.TrimSpace(resource.LogicalID) {
			return nil, &integrity.Failure{Code: integrity.CodeEvidenceInvalid, LogicalID: resource.LogicalID,
				Message: fmt.Sprintf("integrity evidence resources[%d].logicalId must not have surrounding whitespace", index)}
		}
		if err := resourcebinding.ValidateCoordinate(resource.MaterializedCoordinate); err != nil {
			return nil, &integrity.Failure{Code: integrity.CodeEvidenceInvalid, LogicalID: resource.LogicalID, Err: err,
				Message: fmt.Sprintf("integrity evidence resources[%d].materializedCoordinate is not canonical", index)}
		}
		if !knownIntegrityEvidenceStatus(resource.Status) {
			return nil, &integrity.Failure{Code: integrity.CodeEvidenceInvalid, LogicalID: resource.LogicalID,
				Message: fmt.Sprintf("integrity evidence resources[%d].status %q is not supported", index, resource.Status)}
		}
		if resource.Status != "" && resource.Status != FreeCADIntegrityStatusObserved {
			if resource.BaselineSHA256 != "" || resource.PersistedResult != nil {
				return nil, &integrity.Failure{Code: integrity.CodeEvidenceInvalid, LogicalID: resource.LogicalID,
					Message: fmt.Sprintf("integrity evidence resources[%d] must not report content identity for status %q", index, resource.Status)}
			}
		}
		if resource.BaselineSHA256 != "" {
			if err := integrity.ValidateDigest(resource.BaselineSHA256); err != nil {
				return nil, &integrity.Failure{Code: integrity.CodeRuntimeBaselineMalformed, LogicalID: resource.LogicalID, Err: err,
					Message: "runtime baseline digest is not canonical"}
			}
		}
		if resource.PersistedResult != nil {
			if err := resourcebinding.ValidateCoordinate(resource.PersistedResult.Coordinate); err != nil {
				return nil, &integrity.Failure{Code: integrity.CodeEvidenceInvalid, LogicalID: resource.LogicalID, Err: err,
					Message: fmt.Sprintf("integrity evidence resources[%d].persistedResult.coordinate is not canonical", index)}
			}
			if resource.PersistedResult.SHA256 != "" {
				if err := integrity.ValidateDigest(resource.PersistedResult.SHA256); err != nil {
					return nil, &integrity.Failure{Code: integrity.CodeResultDigestMalformed, LogicalID: resource.LogicalID, Err: err,
						Message: "persisted-result digest is not canonical"}
				}
			}
		}
	}

	sort.SliceStable(evidence.Resources, func(i, j int) bool {
		return evidence.Resources[i].MaterializedCoordinate < evidence.Resources[j].MaterializedCoordinate
	})
	for index := 1; index < len(evidence.Resources); index++ {
		if evidence.Resources[index-1].MaterializedCoordinate == evidence.Resources[index].MaterializedCoordinate {
			return nil, &integrity.Failure{Code: integrity.CodeProvenanceConflict,
				Coordinate: evidence.Resources[index].MaterializedCoordinate,
				Message:    "integrity evidence declares the same materialized coordinate more than once"}
		}
	}

	return &evidence, nil
}

// knownIntegrityEvidenceStatus reports whether a reported resource status is
// supported by the integrity evidence contract.
func knownIntegrityEvidenceStatus(status string) bool {
	switch status {
	case "", FreeCADIntegrityStatusObserved, FreeCADIntegrityStatusUnavailable,
		FreeCADIntegrityStatusMissing, FreeCADIntegrityStatusUnsupported:
		return true
	default:
		return false
	}
}

// LoadFreeCADIntegrityEvidence reads optional runtime integrity evidence.
// A missing file is reported as absent evidence, not as a failure.
func LoadFreeCADIntegrityEvidence(workingCopyDir, evidencePath string) (*FreeCADIntegrityEvidence, []byte, error) {
	content, err := ReadOptionalFreeCADRuntimeEvidence(workingCopyDir, evidencePath)
	if err != nil {
		return nil, nil, &integrity.Failure{Code: integrity.CodeEvidenceInvalid, Err: err,
			Message: "integrity evidence is not readable"}
	}
	if len(content) == 0 {
		return nil, nil, nil
	}
	evidence, err := ParseFreeCADIntegrityEvidence(content)
	if err != nil {
		return nil, content, err
	}
	return evidence, content, nil
}

// EvaluateFreeCADRuntimeIntegrity validates runtime-reported evidence against the
// Engine-authored integrity request and independently hashes accessible persisted
// result bytes.
//
// The returned operation provenance is always populated for every requested
// resource, including when validation fails, so a failed run still records the
// identity chain it reached. Evidence is optional: when the runtime provides no
// integrity evidence, requested baselines are still recorded and runtime/result
// stages remain explicitly not reported.
func EvaluateFreeCADRuntimeIntegrity(
	attempt freecad.FreeCADRuntimeAttempt,
	request *FreeCADIntegrityRequest,
	evidence *FreeCADIntegrityEvidence,
) (*integrity.OperationIntegrity, error) {
	if request == nil {
		return nil, fmt.Errorf("integrity request is required")
	}

	operations := make(map[string]*integrity.ResourceOperation, len(request.Resources))
	byCoordinate := make(map[string]string, len(request.Resources))
	persistedRequired := make(map[string]bool, len(request.Resources))
	primaryLogicalID := ""
	for _, requested := range request.Resources {
		baseline, err := integrity.ParseDigest(requested.BaselineSHA256)
		if err != nil {
			return nil, &integrity.Failure{
				Code: integrity.CodeEvidenceInvalid, LogicalID: requested.LogicalID, Err: err,
				Message: "integrity request baseline digest is not canonical",
			}
		}
		operations[requested.LogicalID] = &integrity.ResourceOperation{
			LogicalID:              requested.LogicalID,
			MaterializedCoordinate: requested.MaterializedCoordinate,
			MaterializedDigest:     baseline,
			MaterializationStatus:  integrity.StatusVerified,
			RuntimeBaselineStatus:  integrity.StatusNotReported,
			ResultStatus:           integrity.StatusNotReported,
		}
		byCoordinate[requested.MaterializedCoordinate] = requested.LogicalID
		if requested.ExpectsPersistedResult {
			persistedRequired[requested.LogicalID] = true
			primaryLogicalID = requested.LogicalID
		}
	}

	snapshot := func() *integrity.OperationIntegrity {
		logicalIDs := make([]string, 0, len(operations))
		for logicalID := range operations {
			logicalIDs = append(logicalIDs, logicalID)
		}
		sort.Strings(logicalIDs)
		resources := make([]integrity.ResourceOperation, 0, len(logicalIDs))
		for _, logicalID := range logicalIDs {
			resources = append(resources, *operations[logicalID])
		}
		return &integrity.OperationIntegrity{Resources: resources}
	}

	if evidence == nil {
		return snapshot(), nil
	}

	// Reported evidence is joined to declared resource identity through the
	// requested materialized coordinate. A runtime is not required to enumerate
	// resources it never accessed, and evidence outside the request is rejected.
	observed := make(map[string]bool, len(evidence.Resources))
	for _, reported := range evidence.Resources {
		logicalID, ok := byCoordinate[reported.MaterializedCoordinate]
		if !ok {
			return snapshot(), &integrity.Failure{
				Code:       integrity.CodeEvidenceUnrequested,
				Coordinate: reported.MaterializedCoordinate,
				Message:    "integrity evidence names a coordinate the integrity request did not request",
			}
		}
		if reported.LogicalID != "" && reported.LogicalID != logicalID {
			return snapshot(), &integrity.Failure{
				Code:       integrity.CodeProvenanceConflict,
				LogicalID:  reported.LogicalID,
				Coordinate: reported.MaterializedCoordinate,
				Message:    "integrity evidence logical ID disagrees with the requested resource for its materialized coordinate",
			}
		}
		operation := operations[logicalID]
		observed[logicalID] = true

		switch reported.Status {
		case FreeCADIntegrityStatusUnavailable:
			operation.RuntimeBaselineStatus = integrity.StatusUnavailable
		case FreeCADIntegrityStatusMissing:
			operation.RuntimeBaselineStatus = integrity.StatusMissing
		case FreeCADIntegrityStatusUnsupported:
			operation.RuntimeBaselineStatus = integrity.StatusUnsupported
		default:
			if reported.BaselineSHA256 == "" {
				operation.RuntimeBaselineStatus = integrity.StatusMissing
				return snapshot(), &integrity.Failure{
					Code:       integrity.CodeRuntimeBaselineMissing,
					LogicalID:  logicalID,
					Coordinate: operation.MaterializedCoordinate,
					Message:    "runtime did not report the pre-mutation baseline digest",
				}
			}
			runtimeBaseline, err := integrity.ParseDigest(reported.BaselineSHA256)
			if err != nil {
				operation.RuntimeBaselineStatus = integrity.StatusMalformed
				return snapshot(), &integrity.Failure{
					Code:       integrity.CodeRuntimeBaselineMalformed,
					LogicalID:  logicalID,
					Coordinate: operation.MaterializedCoordinate,
					Err:        err,
					Message:    "runtime baseline digest is not canonical",
				}
			}
			operation.RuntimeBaselineDigest = runtimeBaseline
			if runtimeBaseline != operation.MaterializedDigest {
				operation.RuntimeBaselineStatus = integrity.StatusMismatch
				return snapshot(), &integrity.Failure{
					Code:           integrity.CodeRuntimeBaselineMismatch,
					LogicalID:      logicalID,
					Coordinate:     operation.MaterializedCoordinate,
					ExpectedDigest: operation.MaterializedDigest,
					ActualDigest:   runtimeBaseline,
					Message:        "runtime-observed baseline differs from the Engine materialized baseline",
				}
			}
			operation.RuntimeBaselineStatus = integrity.StatusVerified
		}

		if _, isPrimary := persistedRequired[logicalID]; isPrimary && reported.Status != FreeCADIntegrityStatusObserved && reported.Status != "" {
			return snapshot(), &integrity.Failure{
				Code:       integrity.CodeRuntimeBaselineMissing,
				LogicalID:  logicalID,
				Coordinate: operation.MaterializedCoordinate,
				Message:    fmt.Sprintf("primary source document cannot be reported as %q", reported.Status),
			}
		}
	}

	if primaryLogicalID != "" && !observed[primaryLogicalID] {
		operations[primaryLogicalID].RuntimeBaselineStatus = integrity.StatusMissing
		return snapshot(), &integrity.Failure{
			Code:       integrity.CodeRuntimeBaselineMissing,
			LogicalID:  primaryLogicalID,
			Coordinate: operations[primaryLogicalID].MaterializedCoordinate,
			Message:    "integrity evidence omits the primary source document",
		}
	}

	for _, reported := range evidence.Resources {
		if reported.Status != "" && reported.Status != FreeCADIntegrityStatusObserved {
			continue
		}
		operation := operations[byCoordinate[reported.MaterializedCoordinate]]
		logicalID := operation.LogicalID
		expectedCoordinate := operation.MaterializedCoordinate
		required := persistedRequired[logicalID]
		if reported.PersistedResult == nil {
			if !required {
				continue
			}
			operation.ResultStatus = integrity.StatusMissing
			return snapshot(), &integrity.Failure{
				Code:       integrity.CodeResultDigestMissing,
				LogicalID:  logicalID,
				Coordinate: expectedCoordinate,
				Message:    "runtime did not report the persisted configured-result identity",
			}
		}
		if reported.PersistedResult.Coordinate != expectedCoordinate {
			return snapshot(), &integrity.Failure{
				Code:       integrity.CodeProvenanceConflict,
				LogicalID:  logicalID,
				Coordinate: reported.PersistedResult.Coordinate,
				Message:    "persisted-result coordinate does not match the Engine persisted-result coordinate",
			}
		}
		operation.PersistedResultCoordinate = reported.PersistedResult.Coordinate
		if reported.PersistedResult.SHA256 == "" {
			operation.ResultStatus = integrity.StatusMissing
			return snapshot(), &integrity.Failure{
				Code:       integrity.CodeResultDigestMissing,
				LogicalID:  logicalID,
				Coordinate: expectedCoordinate,
				Message:    "runtime did not report the persisted-result digest",
			}
		}
		reportedDigest, err := integrity.ParseDigest(reported.PersistedResult.SHA256)
		if err != nil {
			operation.ResultStatus = integrity.StatusMalformed
			return snapshot(), &integrity.Failure{
				Code:       integrity.CodeResultDigestMalformed,
				LogicalID:  logicalID,
				Coordinate: expectedCoordinate,
				Err:        err,
				Message:    "persisted-result digest is not canonical",
			}
		}
		operation.PersistedResultDigest = reportedDigest

		computed, status, failure := hashPersistedFreeCADResult(attempt, expectedCoordinate, logicalID)
		if failure != nil {
			operation.ResultStatus = status
			return snapshot(), failure
		}
		operation.ResultStatus = integrity.StatusVerified
		if computed != reportedDigest {
			operation.ResultStatus = integrity.StatusMismatch
			return snapshot(), &integrity.Failure{
				Code:           integrity.CodeResultMismatch,
				LogicalID:      logicalID,
				Coordinate:     expectedCoordinate,
				ExpectedDigest: reportedDigest,
				ActualDigest:   computed,
				Message:        "runtime-reported persisted-result digest disagrees with the Engine-computed digest",
			}
		}
	}

	return snapshot(), nil
}

// hashPersistedFreeCADResult independently hashes accessible persisted-result
// bytes for one attempt-relative coordinate.
func hashPersistedFreeCADResult(
	attempt freecad.FreeCADRuntimeAttempt,
	coordinate string,
	logicalID string,
) (integrity.Digest, integrity.Status, *integrity.Failure) {
	resolved, err := attempt.Layout.ResourcePath(coordinate)
	if err != nil {
		return "", integrity.StatusInaccessible, &integrity.Failure{
			Code: integrity.CodeResultFileInaccessible, LogicalID: logicalID, Coordinate: coordinate, Err: err,
			Message: "persisted-result coordinate is not usable",
		}
	}
	if err := requireNoSymlinkPathComponents(attempt.Layout.WorkingCopyDir, resolved); err != nil {
		return "", integrity.StatusInaccessible, &integrity.Failure{
			Code: integrity.CodeResultFileInaccessible, LogicalID: logicalID, Coordinate: coordinate, Err: err,
			Message: "persisted result is not a confined regular file",
		}
	}
	if err := requireRegularNonSymlinkFile(resolved); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", integrity.StatusMissing, &integrity.Failure{
				Code: integrity.CodeResultFileMissing, LogicalID: logicalID, Coordinate: coordinate,
				Message: "persisted result is missing",
			}
		}
		return "", integrity.StatusInaccessible, &integrity.Failure{
			Code: integrity.CodeResultFileInaccessible, LogicalID: logicalID, Coordinate: coordinate, Err: err,
			Message: "persisted result is not accessible",
		}
	}
	digest, err := integrity.HashFile(resolved)
	if err != nil {
		return "", integrity.StatusInaccessible, &integrity.Failure{
			Code: integrity.CodeResultFileInaccessible, LogicalID: logicalID, Coordinate: coordinate, Err: err,
			Message: "persisted result cannot be hashed",
		}
	}
	return digest, integrity.StatusVerified, nil
}

// operationIntegrityFromBaselines records verified materialized baselines before
// any runtime evidence is available.
func operationIntegrityFromBaselines(baselines []freecad.FreeCADRuntimeMaterializedBaseline) *integrity.OperationIntegrity {
	if len(baselines) == 0 {
		return nil
	}
	resources := make([]integrity.ResourceOperation, 0, len(baselines))
	for _, baseline := range baselines {
		resources = append(resources, integrity.ResourceOperation{
			LogicalID:              baseline.Binding.LogicalID,
			MaterializedCoordinate: baseline.Coordinate,
			MaterializedDigest:     baseline.Digest,
			MaterializationStatus:  integrity.StatusVerified,
			RuntimeBaselineStatus:  integrity.StatusNotPerformed,
			ResultStatus:           integrity.StatusNotPerformed,
		})
	}
	return (&integrity.OperationIntegrity{Resources: resources}).Copy()
}

// operationIntegrityForMaterializationFailure records the identity chain reached
// when materialized baseline verification fails, so a failed run still reports
// what Engine proved and what it rejected.
func operationIntegrityForMaterializationFailure(
	baselines []freecad.FreeCADRuntimeMaterializedBaseline,
	failure *integrity.Failure,
) *integrity.OperationIntegrity {
	resources := make([]integrity.ResourceOperation, 0, len(baselines)+1)
	for _, baseline := range baselines {
		resources = append(resources, integrity.ResourceOperation{
			LogicalID:              baseline.Binding.LogicalID,
			MaterializedCoordinate: baseline.Coordinate,
			MaterializedDigest:     baseline.Digest,
			MaterializationStatus:  integrity.StatusVerified,
			RuntimeBaselineStatus:  integrity.StatusNotPerformed,
			ResultStatus:           integrity.StatusNotPerformed,
		})
	}
	if failure != nil && failure.LogicalID != "" && !hasIntegrityResource(resources, failure.LogicalID) {
		status := integrity.StatusMissing
		if failure.Code == integrity.CodeMaterializationMismatch {
			status = integrity.StatusMismatch
		}
		resources = append(resources, integrity.ResourceOperation{
			LogicalID:              failure.LogicalID,
			MaterializedCoordinate: failure.Coordinate,
			MaterializedDigest:     failure.ActualDigest,
			MaterializationStatus:  status,
			RuntimeBaselineStatus:  integrity.StatusNotPerformed,
			ResultStatus:           integrity.StatusNotPerformed,
		})
	}
	return (&integrity.OperationIntegrity{Resources: resources}).Copy()
}

func hasIntegrityResource(resources []integrity.ResourceOperation, logicalID string) bool {
	for _, resource := range resources {
		if resource.LogicalID == logicalID {
			return true
		}
	}
	return false
}

func marshalFreeCADIntegrityJSON(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func writeCanonicalFreeCADIntegrityFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func ensureNoTrailingFreeCADIntegrityJSON(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}
