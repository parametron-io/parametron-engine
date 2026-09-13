package semanticmap

import "fmt"

type ProjectionReview struct {
	Ready                        bool
	ParameterTarget              string
	SupportedOutputTypes         []string
	SupportedMutationCollections []string
	OverridesAllowed             bool
	AllowImplicitFallback        bool
	AllowAdapterInference        bool
	AllowDisplayNameIdentity     bool
	AllowCaseInsensitiveMatch    bool
	Problems                     []string
}

func ReviewProjectionContract(contract *SemanticMap) ProjectionReview {
	review := ProjectionReview{
		SupportedOutputTypes:         supportedProjectionOutputTypes(),
		SupportedMutationCollections: supportedProjectionMutationCollections(),
	}

	if contract == nil {
		review.Problems = []string{"semantic map must not be nil"}
		return review
	}

	if contract.SemanticToManifest != nil && contract.SemanticToManifest.Parameters != nil {
		review.ParameterTarget = contract.SemanticToManifest.Parameters.Target
	}
	if contract.OverridePolicy != nil {
		review.OverridesAllowed = contract.OverridePolicy.OverridesAllowed()
	}
	if contract.Determinism != nil {
		review.AllowImplicitFallback = boolValue(contract.Determinism.AllowImplicitFallback)
		review.AllowAdapterInference = boolValue(contract.Determinism.AllowAdapterInference)
		review.AllowDisplayNameIdentity = boolValue(contract.Determinism.AllowDisplayNameIdentity)
		review.AllowCaseInsensitiveMatch = boolValue(contract.Determinism.AllowCaseInsensitiveMatch)
	}

	problems := append([]string(nil), validateSemanticToManifest(contract.SemanticToManifest)...)
	problems = append(problems, validateOverridePolicy(contract.OverridePolicy)...)
	problems = append(problems, validateDeterminismPolicy(contract)...)

	if contract.SemanticToManifest == nil {
		review.Problems = problems
		return review
	}

	if contract.SemanticToManifest.Parameters != nil && contract.SemanticToManifest.Parameters.Target != "parameterAssignments" {
		problems = append(problems, `semanticToManifest.parameters.target must map to "parameterAssignments"`)
	}

	outputsByType := map[string]bool{}
	for _, key := range sortedKeys(contract.SemanticToManifest.Outputs) {
		entry := contract.SemanticToManifest.Outputs[key]
		if entry.ManifestType != "" {
			outputsByType[entry.ManifestType] = true
		}
	}
	for _, manifestType := range supportedProjectionOutputTypes() {
		if !outputsByType[manifestType] {
			problems = append(problems, fmt.Sprintf("semanticToManifest.outputs must include manifestType %q", manifestType))
		}
	}

	mutationsByCollection := map[string]bool{}
	for _, key := range sortedKeys(contract.SemanticToManifest.Mutations) {
		entry := contract.SemanticToManifest.Mutations[key]
		if entry.ManifestCollection != "" {
			mutationsByCollection[entry.ManifestCollection] = true
		}
	}
	for _, collection := range supportedProjectionMutationCollections() {
		if !mutationsByCollection[collection] {
			problems = append(problems, fmt.Sprintf("semanticToManifest.mutations must include manifestCollection %q", collection))
		}
	}

	review.Problems = problems
	review.Ready = len(problems) == 0
	return review
}

func ValidateProjectionContract(contract *SemanticMap) error {
	review := ReviewProjectionContract(contract)
	if len(review.Problems) == 0 {
		return nil
	}
	return &ValidationError{Problems: review.Problems}
}

func validateProjectionLinkageContract(contract *SemanticMap) error {
	if err := Validate(contract); err != nil {
		return err
	}
	return enforceDeterminismPolicy(contract)
}

func supportedProjectionOutputTypes() []string {
	return []string{"csv", "pdf", "step"}
}

func supportedProjectionMutationCollections() []string {
	return []string{
		"partMutations.parameters",
		"partMutations.properties",
		"partMutations.suppression",
	}
}

func boolValue(value *bool) bool {
	return value != nil && *value
}
