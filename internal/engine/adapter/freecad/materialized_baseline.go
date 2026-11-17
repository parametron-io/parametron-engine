package freecad

import (
	"errors"
	"os"
	"sort"

	"parametron/internal/engine/integrity"
	"parametron/internal/engine/resourcebinding"
)

// FreeCADRuntimeMaterializedBaseline is one declared resource as materialized in
// the prepared attempt workspace. Path is operational access context only; the
// attempt-relative coordinate and the content digest are the stable identity.
type FreeCADRuntimeMaterializedBaseline struct {
	Binding    resourcebinding.Binding
	Coordinate string
	Path       string
	Digest     integrity.Digest
}

// VerifyFreeCADRuntimeMaterializedBaselines independently hashes every declared
// resource that was materialized into the prepared attempt workspace and proves
// byte-preserving equality with the captured source content identity.
//
// The returned baselines are ordered by declared logical resource identity. When
// a resource is missing or its materialized bytes differ from the accepted
// source bytes, the returned error carries an Engine-owned integrity failure and
// describes only the attempt-relative coordinate, never a host absolute path.
func VerifyFreeCADRuntimeMaterializedBaselines(attempt FreeCADRuntimeAttempt) ([]FreeCADRuntimeMaterializedBaseline, error) {
	bindings := attempt.Resources.Bindings()
	sort.SliceStable(bindings, func(i, j int) bool { return bindings[i].LogicalID < bindings[j].LogicalID })

	baselines := make([]FreeCADRuntimeMaterializedBaseline, 0, len(bindings))
	for _, binding := range bindings {
		coordinate := binding.MaterializedCoordinate
		if coordinate == "" {
			return baselines, &integrity.Failure{
				Code:      integrity.CodeMaterializationMissing,
				LogicalID: binding.LogicalID,
				Message:   "declared resource has no completed materialization",
			}
		}

		path, err := attempt.Layout.ResourcePath(coordinate)
		if err != nil {
			return baselines, &integrity.Failure{
				Code:      integrity.CodeMaterializationMissing,
				LogicalID: binding.LogicalID, Coordinate: coordinate, Err: err,
				Message: "materialized coordinate is not usable",
			}
		}

		info, err := os.Stat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return baselines, &integrity.Failure{
					Code:      integrity.CodeMaterializationMissing,
					LogicalID: binding.LogicalID, Coordinate: coordinate,
					Message: "materialized resource is missing",
				}
			}
			return baselines, &integrity.Failure{
				Code:      integrity.CodeMaterializationMissing,
				LogicalID: binding.LogicalID, Coordinate: coordinate, Err: err,
				Message: "materialized resource is not accessible",
			}
		}
		if !info.Mode().IsRegular() {
			return baselines, &integrity.Failure{
				Code:      integrity.CodeMaterializationMissing,
				LogicalID: binding.LogicalID, Coordinate: coordinate,
				Message: "materialized resource is not a regular file",
			}
		}

		digest, err := integrity.HashFile(path)
		if err != nil {
			return baselines, &integrity.Failure{
				Code:      integrity.CodeMaterializationMissing,
				LogicalID: binding.LogicalID, Coordinate: coordinate, Err: err,
				Message: "materialized resource cannot be hashed",
			}
		}

		baselines = append(baselines, FreeCADRuntimeMaterializedBaseline{
			Binding:    binding,
			Coordinate: coordinate,
			Path:       path,
			Digest:     digest,
		})

		if binding.SourceDigest == "" {
			continue
		}
		expected, err := integrity.ParseDigest(binding.SourceDigest)
		if err != nil {
			return baselines, &integrity.Failure{
				Code:      integrity.CodeProvenanceConflict,
				LogicalID: binding.LogicalID, Coordinate: coordinate, Err: err,
				Message: "declared source identity is not canonical",
			}
		}
		if digest != expected {
			return baselines, &integrity.Failure{
				Code:           integrity.CodeMaterializationMismatch,
				LogicalID:      binding.LogicalID,
				Coordinate:     coordinate,
				ExpectedDigest: expected,
				ActualDigest:   digest,
				Message:        "materialized baseline bytes differ from the accepted source bytes",
			}
		}
	}

	return baselines, nil
}
