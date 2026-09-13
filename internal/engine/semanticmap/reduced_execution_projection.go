package semanticmap

import (
	"encoding/json"
	"slices"
)

// ReducedExecutionProjection is the semanticmap boundary artifact for
// manifest-facing execution intent. It must remain free of semantic, capture,
// and mapping metadata.
type ReducedExecutionProjection struct {
	ParameterAssignments []ManifestParameterAssignment `json:"parameterAssignments,omitempty"`
	AssemblyMutations    *ManifestMutationCollection   `json:"assemblyMutations,omitempty"`
	PartMutations        *ManifestMutationCollection   `json:"partMutations,omitempty"`
	Outputs              []ManifestOutput              `json:"outputs,omitempty"`
}

func MarshalReducedExecutionProjectionJSON(assignments []ManifestParameterAssignment, outputs []ManifestOutput, projected *ManifestMutationProjection) ([]byte, error) {
	return json.Marshal(newReducedExecutionProjection(assignments, outputs, projected))
}

func newReducedExecutionProjection(assignments []ManifestParameterAssignment, outputs []ManifestOutput, projected *ManifestMutationProjection) ReducedExecutionProjection {
	reduced := ReducedExecutionProjection{
		ParameterAssignments: append([]ManifestParameterAssignment(nil), assignments...),
		Outputs:              append([]ManifestOutput(nil), outputs...),
	}

	slices.SortFunc(reduced.ParameterAssignments, compareManifestParameterAssignments)
	slices.SortFunc(reduced.Outputs, compareManifestOutputs)

	if projected == nil {
		return reduced
	}

	if projected.Assembly != nil {
		reduced.AssemblyMutations = cloneManifestMutationCollection(projected.Assembly)
	}
	if projected.Part != nil {
		reduced.PartMutations = cloneManifestMutationCollection(projected.Part)
	}
	return reduced
}

func cloneManifestMutationCollection(collection *ManifestMutationCollection) *ManifestMutationCollection {
	if collection == nil {
		return nil
	}

	cloned := &ManifestMutationCollection{
		Parameters:  append([]ManifestParameterMutation(nil), collection.Parameters...),
		Properties:  append([]ManifestPropertyMutation(nil), collection.Properties...),
		Suppression: append([]ManifestSuppressionMutation(nil), collection.Suppression...),
		Visibility:  append([]ManifestVisibilityMutation(nil), collection.Visibility...),
		Deletion:    append([]ManifestDeletionMutation(nil), collection.Deletion...),
	}
	slices.SortFunc(cloned.Parameters, compareManifestParameterMutations)
	slices.SortFunc(cloned.Properties, compareManifestPropertyMutations)
	slices.SortFunc(cloned.Suppression, compareManifestSuppressionMutations)
	slices.SortFunc(cloned.Visibility, compareManifestVisibilityMutations)
	slices.SortFunc(cloned.Deletion, compareManifestDeletionMutations)

	if len(cloned.Parameters) == 0 && len(cloned.Properties) == 0 && len(cloned.Suppression) == 0 && len(cloned.Visibility) == 0 && len(cloned.Deletion) == 0 {
		return nil
	}
	return cloned
}

func compareManifestParameterAssignments(a, b ManifestParameterAssignment) int {
	if a.Name != b.Name {
		return compareStrings(a.Name, b.Name)
	}
	if a.Value != b.Value {
		if a.Value < b.Value {
			return -1
		}
		return 1
	}
	if a.Type != b.Type {
		return compareStrings(a.Type, b.Type)
	}
	return compareStrings(a.Unit, b.Unit)
}
