package planner

import (
	"fmt"
	"slices"
	"strings"

	"parametron/internal/authoring/dsl"
	"parametron/internal/engine/semantic"
	"parametron/internal/engine/semanticmap"
	"parametron/internal/engine/table"
)

// plannedMetadataMutation is planner-private target-scoped metadata intent. It
// retains semantic provenance for later verification/record work while carrying
// the exact native owner, property, value, and value kind needed for runtime
// projection.
type plannedMetadataMutation struct {
	SemanticTarget string
	MetadataID     string
	MetadataKey    string
	NativeObject   string
	NativeProperty string
	Value          any
	ValueKind      string
	Scope          string
}

func evaluateProductTargetMetadata(
	product *dsl.ProductNode,
	resolvedValues map[string]interface{},
	productBindingNames map[string]struct{},
	productBindingScope map[string]*dsl.ParameterNode,
	constants map[string]dsl.ConstantValue,
	tables map[string]*table.Table,
	model *semantic.Model,
	contract *semanticmap.SemanticMap,
) ([]plannedMetadataMutation, error) {
	if product == nil || len(product.TargetMetadata) == 0 {
		return nil, nil
	}
	if model == nil {
		return nil, fmt.Errorf("target metadata requires a semantic model")
	}
	if contract == nil {
		return nil, fmt.Errorf("target metadata requires a semantic map")
	}

	linkage, err := semanticmap.LinkCaptureIdentities(contract, model)
	if err != nil {
		return nil, err
	}

	mutations := make([]plannedMetadataMutation, 0, len(product.TargetMetadata))
	semanticSeen := make(map[string]string, len(product.TargetMetadata))
	nativeSeen := make(map[string]string, len(product.TargetMetadata))
	for _, declaration := range product.TargetMetadata {
		if declaration == nil {
			continue
		}
		location := fmt.Sprintf("%s.%s", declaration.SemanticTarget, declaration.Key)
		if len(declaration.Settings) > 0 {
			return nil, fmt.Errorf("target metadata '%s': metadata settings are not supported in DSL version %s", location, dsl.SupportedDSLVersion)
		}

		resolved, err := semanticmap.ResolveTargetMetadata(&semanticmap.TargetMetadataResolutionRequest{
			Model:           model,
			IdentityLinkage: linkage,
			Contract:        contract,
			TargetName:      declaration.SemanticTarget,
			MetadataKey:     declaration.Key,
		})
		if err != nil {
			return nil, fmt.Errorf("target metadata '%s': %w", location, err)
		}
		if resolved.Target.Scope != "assembly" && resolved.Target.Scope != "part" {
			return nil, fmt.Errorf("target metadata '%s': resolved target scope %q is not assembly or part", location, resolved.Target.Scope)
		}

		expectedType, err := metadataExpectedDSLType(resolved.Metadata.ValueType)
		if err != nil {
			return nil, fmt.Errorf("target metadata '%s': %w", location, err)
		}
		value, err := evaluateExpressionWithContext(
			declaration.Value,
			resolvedValues,
			productBindingNames,
			productBindingScope,
			constants,
			expectedType,
			plannerEvalContext{tables: tables},
		)
		if err != nil {
			return nil, fmt.Errorf("target metadata '%s': %w", location, err)
		}
		if err := semanticmap.ValidateResolvedMetadataValue(resolved.ValueKind, value); err != nil {
			return nil, fmt.Errorf("target metadata '%s': %w", location, err)
		}

		semanticKey := resolved.Target.SemanticID + "\x00" + resolved.Metadata.Key
		if previous, exists := semanticSeen[semanticKey]; exists {
			return nil, fmt.Errorf("target metadata '%s' conflicts with target metadata '%s' for the same resolved semantic target and metadata field", location, previous)
		}
		semanticSeen[semanticKey] = location

		nativeKey := resolved.NativeObject + "\x00" + resolved.NativeProperty
		if previous, exists := nativeSeen[nativeKey]; exists {
			return nil, fmt.Errorf("target metadata '%s' conflicts with target metadata '%s' on native property '%s.%s'", location, previous, resolved.NativeObject, resolved.NativeProperty)
		}
		nativeSeen[nativeKey] = location

		mutations = append(mutations, plannedMetadataMutation{
			SemanticTarget: resolved.Target.SemanticID,
			MetadataID:     resolved.Metadata.ID,
			MetadataKey:    resolved.Metadata.Key,
			NativeObject:   resolved.NativeObject,
			NativeProperty: resolved.NativeProperty,
			Value:          value,
			ValueKind:      resolved.ValueKind,
			Scope:          resolved.Target.Scope,
		})
	}
	return mutations, nil
}

func metadataExpectedDSLType(valueType string) (*dsl.ParameterType, error) {
	switch valueType {
	case "string":
		return &dsl.ParameterType{Kind: dsl.ParamTypeString}, nil
	case "boolean":
		return &dsl.ParameterType{Kind: dsl.ParamTypeBoolean}, nil
	case "number":
		return &dsl.ParameterType{Kind: dsl.ParamTypeNumber}, nil
	default:
		return nil, fmt.Errorf("unsupported captured metadata valueType %q", valueType)
	}
}

func appendPlannedMetadataMutations(assembly, part *ExportManifestMutationCollection, mutations []plannedMetadataMutation) (*ExportManifestMutationCollection, *ExportManifestMutationCollection) {
	for _, mutation := range mutations {
		entry := ExportManifestMetadataMutation{
			Object:    mutation.NativeObject,
			Property:  mutation.NativeProperty,
			Value:     mutation.Value,
			ValueKind: mutation.ValueKind,
		}
		if mutation.Scope == "assembly" {
			if assembly == nil {
				assembly = &ExportManifestMutationCollection{}
			}
			assembly.Metadata = append(assembly.Metadata, entry)
			continue
		}
		if part == nil {
			part = &ExportManifestMutationCollection{}
		}
		part.Metadata = append(part.Metadata, entry)
	}
	return assembly, part
}

func sortExportManifestMetadata(collection *ExportManifestMutationCollection) {
	if collection == nil {
		return
	}
	slices.SortFunc(collection.Metadata, func(a, b ExportManifestMetadataMutation) int {
		if a.Object != b.Object {
			return strings.Compare(a.Object, b.Object)
		}
		if a.Property != b.Property {
			return strings.Compare(a.Property, b.Property)
		}
		return strings.Compare(a.ValueKind, b.ValueKind)
	})
}
