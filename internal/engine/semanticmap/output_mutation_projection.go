package semanticmap

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"parametron/internal/engine/semantic"
)

type ManifestOutput struct {
	Type    string   `json:"type"`
	Object  string   `json:"object,omitempty"`
	Target  string   `json:"target,omitempty"`
	Rollup  string   `json:"rollup,omitempty"`
	Columns []string `json:"columns,omitempty"`
}

type ManifestMutationCollection struct {
	Parameters  []ManifestParameterMutation   `json:"parameters,omitempty"`
	Properties  []ManifestPropertyMutation    `json:"properties,omitempty"`
	Suppression []ManifestSuppressionMutation `json:"suppression,omitempty"`
	Visibility  []ManifestVisibilityMutation  `json:"visibility,omitempty"`
	Deletion    []ManifestDeletionMutation    `json:"deletion,omitempty"`
}

type ManifestMutationProjection struct {
	Assembly *ManifestMutationCollection `json:"assemblyMutations,omitempty"`
	Part     *ManifestMutationCollection `json:"partMutations,omitempty"`
}

type ManifestParameterMutation struct {
	Object     string `json:"object"`
	Property   string `json:"property"`
	ValueParam string `json:"valueParam"`
	Type       string `json:"type"`
	Unit       string `json:"unit"`
}

type ManifestPropertyMutation struct {
	Object   string `json:"object"`
	Property string `json:"property"`
	Value    any    `json:"value"`
}

type ManifestSuppressionMutation struct {
	Object     string `json:"object"`
	Suppressed bool   `json:"suppressed"`
}

type ManifestVisibilityMutation struct {
	Object  string `json:"object"`
	Visible bool   `json:"visible"`
}

type ManifestDeletionMutation struct {
	Object string `json:"object"`
}

type OutputProjectionRequest struct {
	Model           *semantic.Model
	Intent          *semantic.ProductIntent
	Contract        *SemanticMap
	IdentityLinkage *IdentityLinkage
}

type MutationProjectionRequest struct {
	Model           *semantic.Model
	Intent          *semantic.ProductIntent
	ResolvedValues  map[string]any
	Contract        *SemanticMap
	IdentityLinkage *IdentityLinkage
}

func ProjectOutputs(request *OutputProjectionRequest) ([]ManifestOutput, error) {
	contract, model, intent, err := prepareProjectionRequest(
		requestContract(request),
		requestModel(request),
		requestIntent(request),
	)
	if err != nil {
		return nil, err
	}
	if len(intent.Outputs) == 0 {
		return []ManifestOutput{}, nil
	}

	outputIntents := append([]semantic.OutputIntent(nil), intent.Outputs...)
	slices.SortFunc(outputIntents, compareOutputIntents)

	outputs := make([]ManifestOutput, 0, len(outputIntents))
	for _, outputIntent := range outputIntents {
		entry, ok := contract.SemanticToManifest.Outputs[outputIntent.OutputType]
		if !ok {
			return nil, &ProjectionError{
				Code:     projectionCodeNonProjectableOutput,
				Message:  fmt.Sprintf("semantic output format %q is not projectable to manifest outputs", outputIntent.OutputType),
				Problems: []string{fmt.Sprintf("semantic output format %q does not map to semanticToManifest.outputs by exact key", outputIntent.OutputType)},
			}
		}

		problems := missingOutputIntentProblems(outputIntent, entry)
		if len(problems) > 0 {
			return nil, &ProjectionError{
				Code:     projectionCodeNonProjectableOutput,
				Message:  strings.Join(problems, "; "),
				Problems: problems,
			}
		}

		projected := ManifestOutput{Type: entry.ManifestType}
		switch entry.ManifestType {
		case "pdf":
			outputs = append(outputs, projected)
		case "step":
			targetName, err := resolveOutputManifestTargetName(contract, model, request.IdentityLinkage, outputIntent)
			if err != nil {
				return nil, err
			}
			projected.Object = targetName
			outputs = append(outputs, projected)
		case "csv":
			targetName, err := resolveOutputManifestTargetName(contract, model, request.IdentityLinkage, outputIntent)
			if err != nil {
				return nil, err
			}
			rollup, ok := entry.ScopeMap[outputIntent.Scope]
			if !ok {
				return nil, &ProjectionError{
					Code:     projectionCodeNonProjectableOutput,
					Message:  fmt.Sprintf("semantic output format %q scope %q is not projectable to manifest field %q", outputIntent.OutputType, outputIntent.Scope, entry.ScopeField),
					Problems: []string{fmt.Sprintf("semantic output format %q scope %q does not map to semanticToManifest.outputs[%q].scopeMap by exact key", outputIntent.OutputType, outputIntent.Scope, outputIntent.OutputType)},
				}
			}
			projected.Target = targetName
			projected.Rollup = rollup
			outputs = append(outputs, projected)
		default:
			return nil, &ProjectionError{
				Code:     projectionCodeNonProjectableOutput,
				Message:  fmt.Sprintf("semantic output format %q maps to unsupported manifest output type %q", outputIntent.OutputType, entry.ManifestType),
				Problems: []string{fmt.Sprintf("semantic output format %q maps to unsupported manifest output type %q", outputIntent.OutputType, entry.ManifestType)},
			}
		}
	}

	slices.SortFunc(outputs, compareManifestOutputs)
	return outputs, nil
}

func ProjectMutations(request *MutationProjectionRequest) (*ManifestMutationProjection, error) {
	contract, model, intent, err := prepareProjectionRequest(
		requestContract(request),
		requestModel(request),
		requestIntent(request),
	)
	if err != nil {
		return nil, err
	}
	if len(intent.Mutations) == 0 {
		return &ManifestMutationProjection{}, nil
	}

	if request.IdentityLinkage == nil {
		return nil, &ProjectionError{
			Code:     projectionCodeMissingIdentityLinkage,
			Message:  "semantic mutations require identity linkage for manifest target projection",
			Problems: []string{"semantic mutations require identity linkage before projecting manifest target fields"},
		}
	}

	mutationIntents := append([]semantic.MutationIntent(nil), intent.Mutations...)
	slices.SortFunc(mutationIntents, compareMutationIntents)

	assembly := ManifestMutationCollection{}
	part := ManifestMutationCollection{}
	for _, mutationIntent := range mutationIntents {
		entry, ok := contract.SemanticToManifest.Mutations[mutationIntent.OperationKind]
		if !ok {
			return nil, &ProjectionError{
				Code:     projectionCodeNonProjectableMutation,
				Message:  fmt.Sprintf("semantic mutation operation %q is not projectable to manifest mutations", mutationIntent.OperationKind),
				Problems: []string{fmt.Sprintf("semantic mutation operation %q does not map to semanticToManifest.mutations by exact key", mutationIntent.OperationKind)},
			}
		}

		problems := missingMutationIntentProblems(mutationIntent, entry)
		if len(problems) > 0 {
			return nil, &ProjectionError{
				Code:     projectionCodeNonProjectableMutation,
				Message:  strings.Join(problems, "; "),
				Problems: problems,
			}
		}

		targetCollection, collectionKind, err := resolveMutationCollectionTarget(mutationIntent, entry)
		if err != nil {
			return nil, err
		}
		targetName, propertyName, err := resolveMutationManifestTarget(contract, model, request.IdentityLinkage, mutationIntent)
		if err != nil {
			return nil, err
		}

		collection := &part
		if targetCollection == "assembly" {
			collection = &assembly
		}
		switch collectionKind {
		case "parameters":
			typeName, unitName, err := resolveParameterMutationType(contract, model, mutationIntent)
			if err != nil {
				return nil, err
			}
			collection.Parameters = append(collection.Parameters, ManifestParameterMutation{
				Object:     targetName,
				Property:   propertyName,
				ValueParam: mutationIntent.ValueSource.DSLParameterName,
				Type:       typeName,
				Unit:       unitName,
			})
		case "properties":
			value, err := resolveProjectedMutationValue(request.ResolvedValues, mutationIntent, entry)
			if err != nil {
				return nil, err
			}
			collection.Properties = append(collection.Properties, ManifestPropertyMutation{
				Object:   targetName,
				Property: propertyName,
				Value:    value,
			})
		case "suppression":
			value, err := resolveProjectedMutationValue(request.ResolvedValues, mutationIntent, entry)
			if err != nil {
				return nil, err
			}
			suppressed, ok := value.(bool)
			if !ok {
				return nil, &ProjectionError{
					Code:     projectionCodeNonProjectableMutation,
					Message:  fmt.Sprintf("semantic mutation operation %q resolved unsupported suppression value type %T", mutationIntent.OperationKind, value),
					Problems: []string{fmt.Sprintf("semantic mutation operation %q must resolve a boolean suppression value", mutationIntent.OperationKind)},
				}
			}
			collection.Suppression = append(collection.Suppression, ManifestSuppressionMutation{
				Object:     targetName,
				Suppressed: suppressed,
			})
		case "visibility":
			value, err := resolveProjectedMutationValue(request.ResolvedValues, mutationIntent, entry)
			if err != nil {
				return nil, err
			}
			visible, ok := value.(bool)
			if !ok {
				return nil, &ProjectionError{
					Code:     projectionCodeNonProjectableMutation,
					Message:  fmt.Sprintf("semantic mutation operation %q resolved unsupported visibility value type %T", mutationIntent.OperationKind, value),
					Problems: []string{fmt.Sprintf("semantic mutation operation %q must resolve a boolean visibility value", mutationIntent.OperationKind)},
				}
			}
			collection.Visibility = append(collection.Visibility, ManifestVisibilityMutation{Object: targetName, Visible: visible})
		case "deletion":
			collection.Deletion = append(collection.Deletion, ManifestDeletionMutation{Object: targetName})
		default:
			return nil, &ProjectionError{
				Code:     projectionCodeNonProjectableMutation,
				Message:  fmt.Sprintf("semantic mutation operation %q maps to unsupported manifest mutation collection %q", mutationIntent.OperationKind, entry.ManifestCollection),
				Problems: []string{fmt.Sprintf("semantic mutation operation %q maps to unsupported manifest mutation collection %q", mutationIntent.OperationKind, entry.ManifestCollection)},
			}
		}
	}

	slices.SortFunc(assembly.Parameters, compareManifestParameterMutations)
	slices.SortFunc(assembly.Properties, compareManifestPropertyMutations)
	slices.SortFunc(assembly.Suppression, compareManifestSuppressionMutations)
	slices.SortFunc(assembly.Visibility, compareManifestVisibilityMutations)
	slices.SortFunc(assembly.Deletion, compareManifestDeletionMutations)
	slices.SortFunc(part.Parameters, compareManifestParameterMutations)
	slices.SortFunc(part.Properties, compareManifestPropertyMutations)
	slices.SortFunc(part.Suppression, compareManifestSuppressionMutations)
	slices.SortFunc(part.Visibility, compareManifestVisibilityMutations)
	slices.SortFunc(part.Deletion, compareManifestDeletionMutations)

	out := &ManifestMutationProjection{}
	if len(assembly.Parameters) > 0 || len(assembly.Properties) > 0 || len(assembly.Suppression) > 0 || len(assembly.Visibility) > 0 || len(assembly.Deletion) > 0 {
		assemblyCopy := assembly
		out.Assembly = &assemblyCopy
	}
	if len(part.Parameters) > 0 || len(part.Properties) > 0 || len(part.Suppression) > 0 || len(part.Visibility) > 0 || len(part.Deletion) > 0 {
		partCopy := part
		out.Part = &partCopy
	}
	return out, nil
}

func prepareProjectionRequest(contract *SemanticMap, model *semantic.Model, intent *semantic.ProductIntent) (*SemanticMap, *semantic.Model, *semantic.ProductIntent, error) {
	if contract == nil {
		return nil, nil, nil, &ProjectionError{
			Code:    projectionCodeMissingSemanticMap,
			Message: "semantic map must not be nil",
		}
	}

	if err := validateProjectionLinkageContract(contract); err != nil {
		problems := projectionProblems(err)
		return nil, nil, nil, &ProjectionError{
			Code:     projectionCodeInvalidSemanticMap,
			Message:  strings.Join(problems, "; "),
			Problems: problems,
			Err:      err,
		}
	}

	review := ReviewProjectionContract(contract)
	if !review.Ready {
		return nil, nil, nil, &ProjectionError{
			Code:     projectionCodeContractNotReady,
			Message:  strings.Join(review.Problems, "; "),
			Problems: append([]string(nil), review.Problems...),
		}
	}

	if model == nil {
		return nil, nil, nil, &ProjectionError{
			Code:     projectionCodeInvalidModel,
			Message:  "semantic model must not be nil",
			Problems: []string{"semantic model must not be nil"},
		}
	}
	if err := semantic.Validate(model); err != nil {
		problems := semanticValidationProblems(err)
		return nil, nil, nil, &ProjectionError{
			Code:     projectionCodeInvalidModel,
			Message:  strings.Join(problems, "; "),
			Problems: problems,
			Err:      err,
		}
	}

	canonicalModel, err := semantic.Canonicalize(model)
	if err != nil {
		problems := semanticValidationProblems(err)
		return nil, nil, nil, &ProjectionError{
			Code:     projectionCodeInvalidModel,
			Message:  strings.Join(problems, "; "),
			Problems: problems,
			Err:      err,
		}
	}

	if intent == nil {
		return nil, nil, nil, &ProjectionError{
			Code:     projectionCodeMissingSemanticIntent,
			Message:  "semantic product intent must not be nil",
			Problems: []string{"semantic product intent must not be nil"},
		}
	}

	intentCopy := *intent
	intentCopy.RequestedOutputFormats = append([]string(nil), intent.RequestedOutputFormats...)
	intentCopy.ExportedParameters = append([]semantic.ExportedParameterIntent(nil), intent.ExportedParameters...)
	intentCopy.Outputs = append([]semantic.OutputIntent(nil), intent.Outputs...)
	intentCopy.Mutations = append([]semantic.MutationIntent(nil), intent.Mutations...)

	return contract, canonicalModel, &intentCopy, nil
}

func missingOutputIntentProblems(output semantic.OutputIntent, entry OutputManifestMapping) []string {
	var problems []string
	format := output.OutputType
	if entry.TargetSource != "" {
		if strings.TrimSpace(output.TargetEntityKind) == "" {
			problems = append(problems, fmt.Sprintf("semantic output format %q requires semantic output target entity kind intent", format))
		}
		if strings.TrimSpace(output.TargetSemanticID) == "" {
			problems = append(problems, fmt.Sprintf("semantic output format %q requires semantic output target semantic ID intent", format))
		}
	}
	if entry.ScopeField != "" {
		if strings.TrimSpace(output.Scope) == "" {
			problems = append(problems, fmt.Sprintf("semantic output format %q requires semantic output scope intent for manifest field %q", format, entry.ScopeField))
		}
	}
	return problems
}

func missingMutationIntentProblems(mutation semantic.MutationIntent, entry MutationMapping) []string {
	var problems []string
	if entry.TargetField != "" && strings.TrimSpace(mutation.TargetSemanticID) == "" {
		problems = append(problems, fmt.Sprintf("semantic mutation operation %q requires semantic mutation target semantic ID intent for manifest field %q", mutation.OperationKind, entry.TargetField))
	}
	if strings.TrimSpace(mutation.TargetEntityKind) == "" {
		problems = append(problems, fmt.Sprintf("semantic mutation operation %q requires semantic mutation target entity kind intent", mutation.OperationKind))
	}
	if entry.PropertyField != "" && strings.TrimSpace(mutation.TargetField) == "" && mutation.OperationKind != "set_parameter" && mutation.OperationKind != "set_property" {
		problems = append(problems, fmt.Sprintf("semantic mutation operation %q requires semantic mutation property intent for manifest field %q", mutation.OperationKind, entry.PropertyField))
	}
	if entry.ValueField != "" {
		switch {
		case mutation.OperationKind == "set_parameter" && strings.TrimSpace(mutation.ValueSource.DSLParameterName) == "":
			problems = append(problems, fmt.Sprintf("semantic mutation operation %q requires semantic mutation value intent for manifest field %q", mutation.OperationKind, entry.ValueField))
		case mutation.OperationKind == "set_property" && mutationValueSourceIncomplete(mutation.ValueSource):
			problems = append(problems, fmt.Sprintf("semantic mutation operation %q requires semantic mutation value intent for manifest field %q", mutation.OperationKind, entry.ValueField))
		case mutation.OperationKind != "set_parameter" && mutation.OperationKind != "set_property" && entry.Value == nil && mutation.ValueSource.BooleanState == nil:
			problems = append(problems, fmt.Sprintf("semantic mutation operation %q requires semantic mutation value intent for manifest field %q", mutation.OperationKind, entry.ValueField))
		}
	}
	if strings.TrimSpace(mutation.Scope) == "" && mutation.TargetEntityKind != "assembly" && mutation.TargetEntityKind != "part" {
		problems = append(problems, fmt.Sprintf("semantic mutation operation %q requires semantic mutation scope intent", mutation.OperationKind))
	}
	return problems
}

func mutationValueSourceIncomplete(source semantic.MutationValueSource) bool {
	if strings.TrimSpace(source.DSLParameterName) != "" {
		return false
	}
	if !source.Scalar.IsZero() {
		return false
	}
	return source.BooleanState == nil
}

func resolveOutputManifestTargetName(_ *SemanticMap, model *semantic.Model, linkage *IdentityLinkage, output semantic.OutputIntent) (string, error) {
	if linkage == nil {
		return "", &ProjectionError{
			Code:     projectionCodeMissingIdentityLinkage,
			Message:  fmt.Sprintf("semantic output format %q requires identity linkage for manifest target projection", output.OutputType),
			Problems: []string{fmt.Sprintf("semantic output format %q requires identity linkage before projecting manifest target fields", output.OutputType)},
		}
	}
	targetName, err := resolveManifestEntityName(model, linkage, output.TargetEntityKind, output.TargetSemanticID)
	if err != nil {
		return "", wrapProjectionEntityError(err, projectionCodeNonProjectableOutput, output.OutputType, "output")
	}
	return targetName, nil
}

func resolveMutationCollectionTarget(mutation semantic.MutationIntent, entry MutationMapping) (string, string, error) {
	collectionKind := entry.ManifestCollection
	if idx := strings.LastIndex(collectionKind, "."); idx >= 0 {
		collectionKind = collectionKind[idx+1:]
	}
	switch collectionKind {
	case "parameters", "properties", "suppression", "visibility", "deletion":
	default:
		return "", "", &ProjectionError{
			Code:     projectionCodeNonProjectableMutation,
			Message:  fmt.Sprintf("semantic mutation operation %q maps to unsupported manifest mutation collection %q", mutation.OperationKind, entry.ManifestCollection),
			Problems: []string{fmt.Sprintf("semantic mutation operation %q maps to unsupported manifest mutation collection %q", mutation.OperationKind, entry.ManifestCollection)},
		}
	}

	destination, err := resolveMutationDestination(mutation)
	if err != nil {
		return "", "", &ProjectionError{
			Code:     projectionCodeNonProjectableMutation,
			Message:  fmt.Sprintf("semantic mutation operation %q scope %q is not projectable to manifest collection %q", mutation.OperationKind, mutation.Scope, entry.ManifestCollection),
			Problems: []string{fmt.Sprintf("semantic mutation operation %q scope %q is not supported for manifest collection %q", mutation.OperationKind, mutation.Scope, entry.ManifestCollection)},
		}
	}
	return destination, collectionKind, nil
}

func resolveMutationDestination(mutation semantic.MutationIntent) (string, error) {
	switch {
	case mutation.TargetEntityKind == "assembly":
		return "assembly", nil
	case mutation.TargetEntityKind == "part":
		return "part", nil
	case mutation.Scope == "assembly":
		return "assembly", nil
	case mutation.Scope == "part":
		return "part", nil
	default:
		return "", fmt.Errorf("semantic mutation operation %q has unsupported target entity kind %q and scope %q", mutation.OperationKind, mutation.TargetEntityKind, mutation.Scope)
	}
}

func resolveMutationManifestTarget(contract *SemanticMap, model *semantic.Model, linkage *IdentityLinkage, mutation semantic.MutationIntent) (string, string, error) {
	switch mutation.OperationKind {
	case "set_parameter":
		parameter, ok := findParameterByID(model, mutation.TargetSemanticID)
		if !ok {
			return "", "", &ProjectionError{
				Code:     projectionCodeNonProjectableMutation,
				Message:  fmt.Sprintf("semantic mutation operation %q references unknown semantic parameter %q", mutation.OperationKind, mutation.TargetSemanticID),
				Problems: []string{fmt.Sprintf("semantic mutation operation %q target semantic ID %q does not resolve to a semantic parameter", mutation.OperationKind, mutation.TargetSemanticID)},
			}
		}
		if _, err := requireIdentityLink(linkage, identityKindParameter, parameter.ID); err != nil {
			return "", "", wrapProjectionEntityError(err, projectionCodeNonProjectableMutation, mutation.OperationKind, "mutation")
		}
		objectName, err := resolveParameterContainerName(contract, model, linkage, parameter)
		if err != nil {
			return "", "", wrapProjectionEntityError(err, projectionCodeNonProjectableMutation, mutation.OperationKind, "mutation")
		}
		return objectName, parameter.Name, nil
	case "set_property":
		metadata, ok := findMetadataByID(model, mutation.TargetSemanticID)
		if !ok {
			return "", "", &ProjectionError{
				Code:     projectionCodeNonProjectableMutation,
				Message:  fmt.Sprintf("semantic mutation operation %q references unknown semantic metadata %q", mutation.OperationKind, mutation.TargetSemanticID),
				Problems: []string{fmt.Sprintf("semantic mutation operation %q target semantic ID %q does not resolve to semantic metadata", mutation.OperationKind, mutation.TargetSemanticID)},
			}
		}
		if _, err := requireIdentityLink(linkage, identityKindMetadata, metadata.ID); err != nil {
			return "", "", wrapProjectionEntityError(err, projectionCodeNonProjectableMutation, mutation.OperationKind, "mutation")
		}
		objectName, err := resolveOwnerManifestName(contract, model, linkage, metadata.OwnerKind, metadata.OwnerID)
		if err != nil {
			return "", "", wrapProjectionEntityError(err, projectionCodeNonProjectableMutation, mutation.OperationKind, "mutation")
		}
		return objectName, metadata.Key, nil
	case "suppress", "unsuppress", "hide", "unhide", "delete":
		objectName, err := resolveManifestEntityName(model, linkage, mutation.TargetEntityKind, mutation.TargetSemanticID)
		if err != nil {
			return "", "", wrapProjectionEntityError(err, projectionCodeNonProjectableMutation, mutation.OperationKind, "mutation")
		}
		return objectName, "", nil
	default:
		return "", "", &ProjectionError{
			Code:     projectionCodeNonProjectableMutation,
			Message:  fmt.Sprintf("semantic mutation operation %q is not projectable to manifest mutations", mutation.OperationKind),
			Problems: []string{fmt.Sprintf("semantic mutation operation %q is not supported by semantic projection", mutation.OperationKind)},
		}
	}
}

func resolveParameterMutationType(contract *SemanticMap, model *semantic.Model, mutation semantic.MutationIntent) (string, string, error) {
	parameter, ok := findParameterByID(model, mutation.TargetSemanticID)
	if !ok {
		return "", "", &ProjectionError{
			Code:     projectionCodeNonProjectableMutation,
			Message:  fmt.Sprintf("semantic mutation operation %q references unknown semantic parameter %q", mutation.OperationKind, mutation.TargetSemanticID),
			Problems: []string{fmt.Sprintf("semantic mutation operation %q target semantic ID %q does not resolve to a semantic parameter", mutation.OperationKind, mutation.TargetSemanticID)},
		}
	}
	semanticTypeName, semanticType, err := resolveParameterSemanticType(contract, parameter)
	if err != nil {
		return "", "", err
	}
	if semanticType.ValueKind != "number" {
		return "", "", &ProjectionError{
			Code:     projectionCodeUnsupportedValueKind,
			Message:  fmt.Sprintf("semantic mutation operation %q semantic type %q valueKind %q is not projectable to manifest parameter mutations", mutation.OperationKind, semanticTypeName, semanticType.ValueKind),
			Problems: []string{fmt.Sprintf("semantic mutation operation %q semantic type %q valueKind %q is not supported for manifest parameter mutations", mutation.OperationKind, semanticTypeName, semanticType.ValueKind)},
		}
	}
	if semanticType.DefaultUnit != "mm" {
		return "", "", &ProjectionError{
			Code:     projectionCodeUnsupportedUnit,
			Message:  fmt.Sprintf("semantic mutation operation %q semantic type %q unit %q is not projectable to manifest parameter mutations", mutation.OperationKind, semanticTypeName, semanticType.DefaultUnit),
			Problems: []string{fmt.Sprintf("semantic mutation operation %q semantic type %q unit %q is not supported for manifest parameter mutations", mutation.OperationKind, semanticTypeName, semanticType.DefaultUnit)},
		}
	}
	return "number", "mm", nil
}

func resolveProjectedMutationValue(resolvedValues map[string]any, mutation semantic.MutationIntent, entry MutationMapping) (any, error) {
	if entry.Value != nil {
		return *entry.Value, nil
	}
	if mutation.ValueSource.BooleanState != nil {
		return *mutation.ValueSource.BooleanState, nil
	}
	if strings.TrimSpace(mutation.ValueSource.DSLParameterName) != "" {
		value, ok := resolvedValues[mutation.ValueSource.DSLParameterName]
		if !ok {
			return nil, &ProjectionError{
				Code:     projectionCodeNonProjectableMutation,
				Message:  fmt.Sprintf("semantic mutation operation %q value source %q is missing a resolved final value", mutation.OperationKind, mutation.ValueSource.DSLParameterName),
				Problems: []string{fmt.Sprintf("semantic mutation operation %q value source %q does not resolve to a final execution value", mutation.OperationKind, mutation.ValueSource.DSLParameterName)},
			}
		}
		switch value.(type) {
		case string, bool, float64:
			return value, nil
		default:
			return nil, &ProjectionError{
				Code:     projectionCodeNonProjectableMutation,
				Message:  fmt.Sprintf("semantic mutation operation %q resolved unsupported property value type %T", mutation.OperationKind, value),
				Problems: []string{fmt.Sprintf("semantic mutation operation %q resolved value must be a string, number, or bool", mutation.OperationKind)},
			}
		}
	}
	if !mutation.ValueSource.Scalar.IsZero() {
		var value any
		if err := json.Unmarshal(mutation.ValueSource.Scalar.Raw(), &value); err != nil {
			return nil, &ProjectionError{
				Code:     projectionCodeNonProjectableMutation,
				Message:  fmt.Sprintf("semantic mutation operation %q carries an invalid scalar value", mutation.OperationKind),
				Problems: []string{fmt.Sprintf("semantic mutation operation %q scalar value could not be decoded", mutation.OperationKind)},
				Err:      err,
			}
		}
		switch value.(type) {
		case string, bool, float64:
			return value, nil
		default:
			return nil, &ProjectionError{
				Code:     projectionCodeNonProjectableMutation,
				Message:  fmt.Sprintf("semantic mutation operation %q resolved unsupported scalar value type %T", mutation.OperationKind, value),
				Problems: []string{fmt.Sprintf("semantic mutation operation %q scalar value must be a string, number, or bool", mutation.OperationKind)},
			}
		}
	}
	return nil, &ProjectionError{
		Code:     projectionCodeNonProjectableMutation,
		Message:  fmt.Sprintf("semantic mutation operation %q has incomplete value source", mutation.OperationKind),
		Problems: []string{fmt.Sprintf("semantic mutation operation %q requires a complete value source", mutation.OperationKind)},
	}
}

func resolveManifestEntityName(model *semantic.Model, linkage *IdentityLinkage, targetEntityKind, targetSemanticID string) (string, error) {
	switch targetEntityKind {
	case "assembly", "part":
		component, ok := findComponentByID(model, targetSemanticID)
		if !ok || component.Kind != targetEntityKind {
			return "", fmt.Errorf("missing target for %s %q", targetEntityKind, targetSemanticID)
		}
		if _, err := requireIdentityLink(linkage, identityKindComponent, component.ID); err != nil {
			return "", err
		}
		if strings.TrimSpace(component.Name) == "" {
			return "", fmt.Errorf("missing manifest-facing name for %s %q", targetEntityKind, targetSemanticID)
		}
		return component.Name, nil
	case "feature":
		feature, ok := findFeatureByID(model, targetSemanticID)
		if !ok {
			return "", fmt.Errorf("missing target for feature %q", targetSemanticID)
		}
		if strings.TrimSpace(feature.Name) == "" {
			return "", fmt.Errorf("missing manifest-facing name for feature %q", targetSemanticID)
		}
		return feature.Name, nil
	default:
		return "", fmt.Errorf("unsupported semantic target kind %q", targetEntityKind)
	}
}

func resolveParameterContainerName(contract *SemanticMap, model *semantic.Model, linkage *IdentityLinkage, parameter semantic.Parameter) (string, error) {
	if strings.TrimSpace(parameter.GroupID) != "" {
		group, ok := findParameterGroupByID(model, parameter.GroupID)
		if !ok {
			return "", fmt.Errorf("missing target for parameter group %q", parameter.GroupID)
		}
		if _, err := requireIdentityLink(linkage, identityKindParameterGroup, group.ID); err != nil {
			return "", err
		}
		if strings.TrimSpace(group.Name) == "" {
			return "", fmt.Errorf("missing manifest-facing name for parameter group %q", group.ID)
		}
		return group.Name, nil
	}
	return resolveOwnerManifestName(contract, model, linkage, parameter.OwnerKind, parameter.OwnerID)
}

func resolveOwnerManifestName(contract *SemanticMap, model *semantic.Model, linkage *IdentityLinkage, ownerKind, ownerID string) (string, error) {
	switch ownerKind {
	case semantic.OwnerKindComponent:
		component, ok := findComponentByID(model, ownerID)
		if !ok {
			return "", fmt.Errorf("missing target for component %q", ownerID)
		}
		if _, err := requireIdentityLink(linkage, identityKindComponent, component.ID); err != nil {
			return "", err
		}
		if strings.TrimSpace(component.Name) == "" {
			return "", fmt.Errorf("missing manifest-facing name for component %q", ownerID)
		}
		return component.Name, nil
	case semantic.OwnerKindFeature:
		feature, ok := findFeatureByID(model, ownerID)
		if !ok {
			return "", fmt.Errorf("missing target for feature %q", ownerID)
		}
		if strings.TrimSpace(feature.Name) == "" {
			return "", fmt.Errorf("missing manifest-facing name for feature %q", ownerID)
		}
		return feature.Name, nil
	case semantic.OwnerKindGroup:
		group, ok := findParameterGroupByID(model, ownerID)
		if !ok {
			return "", fmt.Errorf("missing target for parameter group %q", ownerID)
		}
		if _, err := requireIdentityLink(linkage, identityKindParameterGroup, group.ID); err != nil {
			return "", err
		}
		if strings.TrimSpace(group.Name) == "" {
			return "", fmt.Errorf("missing manifest-facing name for parameter group %q", ownerID)
		}
		return group.Name, nil
	default:
		return "", fmt.Errorf("unsupported owner kind %q", ownerKind)
	}
}

func requireIdentityLink(linkage *IdentityLinkage, entityKind, semanticID string) (IdentityLink, error) {
	if linkage == nil {
		return IdentityLink{}, fmt.Errorf("missing identity linkage for %s %q", entityKind, semanticID)
	}
	for _, entry := range linkage.Entries {
		if entry.EntityKind == entityKind && entry.SemanticID == semanticID {
			return entry, nil
		}
	}
	return IdentityLink{}, fmt.Errorf("missing identity linkage for %s %q", entityKind, semanticID)
}

func wrapProjectionEntityError(err error, code, subject, noun string) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	if strings.Contains(message, "missing identity linkage") {
		return &ProjectionError{
			Code:     projectionCodeMissingIdentityLinkage,
			Message:  fmt.Sprintf("semantic %s %q requires identity linkage for manifest target projection", noun, subject),
			Problems: []string{fmt.Sprintf("semantic %s %q requires identity linkage before projecting manifest target fields", noun, subject)},
			Err:      err,
		}
	}
	return &ProjectionError{
		Code:     code,
		Message:  fmt.Sprintf("semantic %s %q is not projectable: %s", noun, subject, message),
		Problems: []string{fmt.Sprintf("semantic %s %q is not projectable: %s", noun, subject, message)},
		Err:      err,
	}
}

func findComponentByID(model *semantic.Model, id string) (semantic.Component, bool) {
	for _, component := range model.Components {
		if component.ID == id {
			return component, true
		}
	}
	return semantic.Component{}, false
}

func findFeatureByID(model *semantic.Model, id string) (semantic.Feature, bool) {
	for _, feature := range model.Features {
		if feature.ID == id {
			return feature, true
		}
	}
	return semantic.Feature{}, false
}

func findParameterGroupByID(model *semantic.Model, id string) (semantic.ParameterGroup, bool) {
	for _, group := range model.ParameterGroups {
		if group.ID == id {
			return group, true
		}
	}
	return semantic.ParameterGroup{}, false
}

func findParameterByID(model *semantic.Model, id string) (semantic.Parameter, bool) {
	for _, parameter := range model.Parameters {
		if parameter.ID == id {
			return parameter, true
		}
	}
	return semantic.Parameter{}, false
}

func findMetadataByID(model *semantic.Model, id string) (semantic.Metadata, bool) {
	for _, metadata := range model.Metadata {
		if metadata.ID == id {
			return metadata, true
		}
	}
	return semantic.Metadata{}, false
}

func compareOutputIntents(a, b semantic.OutputIntent) int {
	if result := compareStrings(a.OutputType, b.OutputType); result != 0 {
		return result
	}
	if result := compareStrings(a.TargetEntityKind, b.TargetEntityKind); result != 0 {
		return result
	}
	if result := compareStrings(a.TargetSemanticID, b.TargetSemanticID); result != 0 {
		return result
	}
	return compareStrings(a.Scope, b.Scope)
}

func compareMutationIntents(a, b semantic.MutationIntent) int {
	if result := compareStrings(a.OperationKind, b.OperationKind); result != 0 {
		return result
	}
	if result := compareStrings(a.TargetEntityKind, b.TargetEntityKind); result != 0 {
		return result
	}
	if result := compareStrings(a.TargetSemanticID, b.TargetSemanticID); result != 0 {
		return result
	}
	if result := compareStrings(a.TargetField, b.TargetField); result != 0 {
		return result
	}
	if result := compareStrings(a.Scope, b.Scope); result != 0 {
		return result
	}
	if result := compareStrings(a.ValueSource.Kind, b.ValueSource.Kind); result != 0 {
		return result
	}
	if result := compareStrings(a.ValueSource.DSLParameterName, b.ValueSource.DSLParameterName); result != 0 {
		return result
	}
	return compareStrings(a.ValueSource.SemanticParameterID, b.ValueSource.SemanticParameterID)
}

func compareManifestOutputs(a, b ManifestOutput) int {
	if result := compareStrings(a.Type, b.Type); result != 0 {
		return result
	}
	if result := compareStrings(a.Object, b.Object); result != 0 {
		return result
	}
	if result := compareStrings(a.Target, b.Target); result != 0 {
		return result
	}
	return compareStrings(a.Rollup, b.Rollup)
}

func compareManifestParameterMutations(a, b ManifestParameterMutation) int {
	if result := compareStrings(a.Object, b.Object); result != 0 {
		return result
	}
	if result := compareStrings(a.Property, b.Property); result != 0 {
		return result
	}
	return compareStrings(a.ValueParam, b.ValueParam)
}

func compareManifestPropertyMutations(a, b ManifestPropertyMutation) int {
	if result := compareStrings(a.Object, b.Object); result != 0 {
		return result
	}
	return compareStrings(a.Property, b.Property)
}

func compareManifestSuppressionMutations(a, b ManifestSuppressionMutation) int {
	return compareStrings(a.Object, b.Object)
}

func compareManifestVisibilityMutations(a, b ManifestVisibilityMutation) int {
	return compareStrings(a.Object, b.Object)
}

func compareManifestDeletionMutations(a, b ManifestDeletionMutation) int {
	return compareStrings(a.Object, b.Object)
}

func requestContract[T interface{ getContract() *SemanticMap }](request T) *SemanticMap {
	if any(request) == nil {
		return nil
	}
	return request.getContract()
}

func requestModel[T interface{ getModel() *semantic.Model }](request T) *semantic.Model {
	if any(request) == nil {
		return nil
	}
	return request.getModel()
}

func requestIntent[T interface {
	getIntent() *semantic.ProductIntent
}](request T) *semantic.ProductIntent {
	if any(request) == nil {
		return nil
	}
	return request.getIntent()
}

func (r *OutputProjectionRequest) getContract() *SemanticMap {
	if r == nil {
		return nil
	}
	return r.Contract
}

func (r *OutputProjectionRequest) getModel() *semantic.Model {
	if r == nil {
		return nil
	}
	return r.Model
}

func (r *OutputProjectionRequest) getIntent() *semantic.ProductIntent {
	if r == nil {
		return nil
	}
	return r.Intent
}

func (r *MutationProjectionRequest) getContract() *SemanticMap {
	if r == nil {
		return nil
	}
	return r.Contract
}

func (r *MutationProjectionRequest) getModel() *semantic.Model {
	if r == nil {
		return nil
	}
	return r.Model
}

func (r *MutationProjectionRequest) getIntent() *semantic.ProductIntent {
	if r == nil {
		return nil
	}
	return r.Intent
}
