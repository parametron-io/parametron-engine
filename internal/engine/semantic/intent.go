package semantic

import (
	"fmt"
	"slices"
	"strings"

	"parametron/internal/authoring/dsl"
	"parametron/internal/engine/cad"
)

func InjectDSLIntent(model *Model, ast *dsl.AST) (*Model, error) {
	if err := Validate(model); err != nil {
		return nil, err
	}

	intents, diagnostics := buildDSLIntents(model, ast)
	if len(diagnostics) > 0 {
		return nil, &DSLIntentError{Problems: diagnostics}
	}

	out := Clone(model)
	out.ProductIntents = intents
	return Canonicalize(out)
}

func buildDSLIntents(model *Model, ast *dsl.AST) ([]ProductIntent, []DSLIntentDiagnostic) {
	if ast == nil {
		return nil, nil
	}

	intents := make([]ProductIntent, 0, len(ast.Products))
	var diagnostics []DSLIntentDiagnostic
	for _, product := range ast.Products {
		if product == nil {
			continue
		}

		intent, problems := buildProductIntent(model, product, ast.ResolvedConstants)
		if len(problems) > 0 {
			diagnostics = append(diagnostics, problems...)
			continue
		}
		intents = append(intents, intent)
	}

	return intents, diagnostics
}

func buildProductIntent(model *Model, product *dsl.ProductNode, consts map[string]dsl.ConstantValue) (ProductIntent, []DSLIntentDiagnostic) {
	adapter, err := resolveIntentString(product.Adapter, consts)
	if err != nil {
		return ProductIntent{}, []DSLIntentDiagnostic{newDSLIntentDiagnostic(
			DSLIntentDiagnosticCategoryInvalidDSLReference,
			DSLIntentDiagnosticCodeDSLIntentResolutionFailed,
			product.Name,
			"",
			fmt.Sprintf("product %q adapter: %v", product.Name, err),
			nil,
		)}
	}

	sourceModel, err := resolveIntentString(product.SourceModel, consts)
	if err != nil {
		return ProductIntent{}, []DSLIntentDiagnostic{newDSLIntentDiagnostic(
			DSLIntentDiagnosticCategoryInvalidDSLReference,
			DSLIntentDiagnosticCodeDSLIntentResolutionFailed,
			product.Name,
			"",
			fmt.Sprintf("product %q source_model: %v", product.Name, err),
			nil,
		)}
	}

	outputs, _, err := resolveIntentStringArray(product.Outputs, consts)
	if err != nil {
		return ProductIntent{}, []DSLIntentDiagnostic{newDSLIntentDiagnostic(
			DSLIntentDiagnosticCategoryInvalidDSLReference,
			DSLIntentDiagnosticCodeDSLIntentResolutionFailed,
			product.Name,
			"",
			fmt.Sprintf("product %q outputs: %v", product.Name, err),
			nil,
		)}
	}

	intent := ProductIntent{
		Name:                   product.Name,
		Adapter:                normalizeIntentAdapter(adapter),
		SourceModelLogicalID:   sourceModel,
		RequestedOutputFormats: outputs,
		Outputs:                []OutputIntent{},
		ExportedParameters:     []ExportedParameterIntent{},
		Mutations:              []MutationIntent{},
	}
	if !isSemanticCaptureBackedAdapter(intent.Adapter) {
		return intent, nil
	}

	var diagnostics []DSLIntentDiagnostic
	if intent.SourceModelLogicalID != model.SourceDocumentLogicalID {
		diagnostics = append(diagnostics, newDSLIntentDiagnostic(
			DSLIntentDiagnosticCategorySourceModelReferenceMismatch,
			DSLIntentDiagnosticCodeSourceModelLogicalIDMismatch,
			product.Name,
			"",
			fmt.Sprintf(
				"product %q source_model %q does not match semantic sourceDocumentLogicalId %q",
				product.Name,
				intent.SourceModelLogicalID,
				model.SourceDocumentLogicalID,
			),
			nil,
		))
	}

	outputIntents, outputDiagnostics := buildOutputIntents(model, product.Name, outputs)
	if len(outputDiagnostics) > 0 {
		diagnostics = append(diagnostics, outputDiagnostics...)
	}
	intent.Outputs = outputIntents

	for _, param := range product.Parameters {
		if param == nil {
			continue
		}

		switch param.Type.Kind {
		case dsl.ParamTypeNumber:
			matches := resolveSemanticParametersByExactName(model, param.Name)
			switch len(matches) {
			case 0:
				diagnostics = append(diagnostics, newDSLIntentDiagnostic(
					DSLIntentDiagnosticCategoryInvalidDSLReference,
					DSLIntentDiagnosticCodeSemanticParameterReferenceNotFound,
					product.Name,
					param.Name,
					fmt.Sprintf("product %q parameter %q does not resolve to a semantic parameter by exact name", product.Name, param.Name),
					nil,
				))
			case 1:
				intent.ExportedParameters = append(intent.ExportedParameters, ExportedParameterIntent{
					DSLParameterName:    param.Name,
					SemanticParameterID: matches[0].ID,
					Type:                string(param.Type.Kind),
				})
				intent.Mutations = append(intent.Mutations, MutationIntent{
					OperationKind:    "set_parameter",
					TargetEntityKind: "parameter",
					TargetSemanticID: matches[0].ID,
					TargetField:      matches[0].Name,
					Scope:            mutationScopeForComponent(model, matches[0].ComponentID),
					ValueSource: MutationValueSource{
						Kind:                "dsl_parameter",
						DSLParameterName:    param.Name,
						SemanticParameterID: matches[0].ID,
					},
				})
			default:
				ids := collectParameterIDs(matches)
				diagnostics = append(diagnostics, newDSLIntentDiagnostic(
					DSLIntentDiagnosticCategoryAmbiguousDSLReference,
					DSLIntentDiagnosticCodeSemanticParameterReferenceAmbiguous,
					product.Name,
					param.Name,
					fmt.Sprintf("product %q parameter %q is ambiguous across semantic parameters %q", product.Name, param.Name, ids),
					ids,
				))
			}
		case dsl.ParamTypeString, dsl.ParamTypeBoolean:
			propertyIntent, problems := buildPropertyMutationIntent(model, product.Name, param)
			if len(problems) > 0 {
				diagnostics = append(diagnostics, problems...)
				continue
			}
			if propertyIntent != nil {
				intent.Mutations = append(intent.Mutations, *propertyIntent)
			}
		}
	}

	if len(diagnostics) > 0 {
		return ProductIntent{}, diagnostics
	}
	return intent, nil
}

func buildOutputIntents(model *Model, productName string, formats []string) ([]OutputIntent, []DSLIntentDiagnostic) {
	if len(formats) == 0 {
		return []OutputIntent{}, nil
	}

	outputs := make([]OutputIntent, 0, len(formats))
	root := resolveRootOutputComponent(model)
	for _, format := range formats {
		switch format {
		case "none":
			continue
		case "pdf":
			outputs = append(outputs, OutputIntent{OutputType: "pdf"})
		case "step":
			if root == nil {
				return nil, []DSLIntentDiagnostic{newDSLIntentDiagnostic(
					DSLIntentDiagnosticCategoryInvalidDSLReference,
					DSLIntentDiagnosticCodeSemanticOutputTargetNotProjectable,
					productName,
					"",
					fmt.Sprintf("product %q output %q requires a semantic root component target", productName, format),
					nil,
				)}
			}
			outputs = append(outputs, OutputIntent{
				OutputType:       "step",
				TargetEntityKind: root.Kind,
				TargetSemanticID: root.ID,
				TargetNameSource: "identity_linkage",
			})
		case "csv":
			if root == nil {
				return nil, []DSLIntentDiagnostic{newDSLIntentDiagnostic(
					DSLIntentDiagnosticCategoryInvalidDSLReference,
					DSLIntentDiagnosticCodeSemanticOutputTargetNotProjectable,
					productName,
					"",
					fmt.Sprintf("product %q output %q requires a semantic root component target", productName, format),
					nil,
				)}
			}
			outputs = append(outputs, OutputIntent{
				OutputType:       "csv",
				TargetEntityKind: root.Kind,
				TargetSemanticID: root.ID,
				TargetNameSource: "identity_linkage",
				Scope:            "assembly",
			})
		default:
			outputs = append(outputs, OutputIntent{OutputType: format})
		}
	}
	return outputs, nil
}

func buildPropertyMutationIntent(model *Model, productName string, param *dsl.ParameterNode) (*MutationIntent, []DSLIntentDiagnostic) {
	if param == nil {
		return nil, nil
	}

	matches := resolveMetadataByExactKey(model, param.Name)
	switch len(matches) {
	case 0:
		return nil, nil
	case 1:
		metadata := matches[0]
		return &MutationIntent{
			OperationKind:    "set_property",
			TargetEntityKind: "metadata",
			TargetSemanticID: metadata.ID,
			TargetField:      metadata.Key,
			Scope:            mutationScopeForComponent(model, metadata.ComponentID),
			ValueSource: MutationValueSource{
				Kind:             "dsl_parameter",
				DSLParameterName: param.Name,
			},
		}, nil
	default:
		ids := collectMetadataIDs(matches)
		return nil, []DSLIntentDiagnostic{newDSLIntentDiagnostic(
			DSLIntentDiagnosticCategoryAmbiguousDSLReference,
			DSLIntentDiagnosticCodeSemanticMetadataReferenceAmbiguous,
			productName,
			param.Name,
			fmt.Sprintf("product %q parameter %q is ambiguous across semantic metadata %q", productName, param.Name, ids),
			ids,
		)}
	}
}

// ResolvedSemanticTarget identifies one semantic Feature or Component selected
// by an authored exact Name. It is transient resolver output and is not part of
// the serialized semantic model.
type ResolvedSemanticTarget struct {
	SemanticEntityKind string
	TargetEntityKind   string
	SemanticID         string
	Scope              string
	Targetability      cad.Targetability
}

// LowerTargetActionMutationIntent lowers one capability-approved canonical
// target action into semantic mutation intent. Keep is an explicit no-op and
// therefore produces no mutation record.
func LowerTargetActionMutationIntent(target ResolvedSemanticTarget, action string) (*MutationIntent, error) {
	switch action {
	case "keep":
		return nil, nil
	case "suppress", "unsuppress", "hide", "unhide", "delete":
		return &MutationIntent{
			OperationKind:    action,
			TargetEntityKind: target.TargetEntityKind,
			TargetSemanticID: target.SemanticID,
			Scope:            target.Scope,
			ValueSource: MutationValueSource{
				Kind: "target_action",
			},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported target action %q for semantic mutation lowering", action)
	}
}

// ValidateTargetActionCapability checks whether the captured targetability of
// one resolved semantic target permits the canonical authored action.
func ValidateTargetActionCapability(target ResolvedSemanticTarget, action string) error {
	var allowed bool
	var requiredField string

	switch action {
	case "keep":
		return nil
	case "suppress":
		allowed = target.Targetability.Suppress
		requiredField = "Targetability.Suppress"
	case "unsuppress":
		allowed = target.Targetability.Unsuppress
		requiredField = "Targetability.Unsuppress"
	case "hide":
		allowed = target.Targetability.Hide
		requiredField = "Targetability.Hide"
	case "unhide":
		allowed = target.Targetability.Unhide
		requiredField = "Targetability.Unhide"
	case "delete":
		allowed = target.Targetability.Delete
		requiredField = "Targetability.Delete"
	default:
		return fmt.Errorf("unsupported target action %q for capability validation", action)
	}

	if !allowed {
		return fmt.Errorf(
			"action %q requires captured %s=true for semantic %s %q",
			action,
			requiredField,
			target.SemanticEntityKind,
			target.SemanticID,
		)
	}
	return nil
}

// ResolveSemanticTargetByExactName resolves name across the combined semantic
// Feature and Component candidate space using exact, case-sensitive Name
// matching.
func ResolveSemanticTargetByExactName(model *Model, name string) (ResolvedSemanticTarget, error) {
	matches := resolveSemanticMutationTargetsByExactName(model, name)
	switch len(matches) {
	case 0:
		return ResolvedSemanticTarget{}, fmt.Errorf("semantic target %q does not resolve to a Feature or Component by exact Name", name)
	case 1:
		return matches[0], nil
	default:
		descriptors := make([]string, 0, len(matches))
		for _, match := range matches {
			descriptors = append(descriptors, match.SemanticEntityKind+":"+match.SemanticID)
		}
		return ResolvedSemanticTarget{}, fmt.Errorf("semantic target %q is ambiguous across candidates [%s]", name, strings.Join(descriptors, " "))
	}
}

func resolveSemanticMutationTargetsByExactName(model *Model, name string) []ResolvedSemanticTarget {
	featureMatches := resolveFeaturesByExactName(model, name)
	componentMatches := resolveComponentsByExactName(model, name)
	matches := make([]ResolvedSemanticTarget, 0, len(featureMatches)+len(componentMatches))
	for _, feature := range featureMatches {
		matches = append(matches, ResolvedSemanticTarget{
			SemanticEntityKind: "feature",
			TargetEntityKind:   "feature",
			SemanticID:         feature.ID,
			Scope:              mutationScopeForComponent(model, feature.ComponentID),
			Targetability:      feature.Targetability,
		})
	}
	for _, component := range componentMatches {
		matches = append(matches, ResolvedSemanticTarget{
			SemanticEntityKind: "component",
			TargetEntityKind:   component.Kind,
			SemanticID:         component.ID,
			Scope:              mutationScopeForComponent(model, component.ID),
			Targetability:      component.Targetability,
		})
	}
	slices.SortFunc(matches, func(a, b ResolvedSemanticTarget) int {
		if result := strings.Compare(a.SemanticEntityKind, b.SemanticEntityKind); result != 0 {
			return result
		}
		return strings.Compare(a.SemanticID, b.SemanticID)
	})
	return matches
}

func newDSLIntentDiagnostic(category, code, productName, parameterName, message string, matchingIDs []string) DSLIntentDiagnostic {
	diagnostic := DSLIntentDiagnostic{
		Category:      category,
		Code:          code,
		Message:       message,
		ProductName:   productName,
		ParameterName: parameterName,
	}
	if len(matchingIDs) > 0 {
		diagnostic.MatchingParameterIDs = append([]string(nil), matchingIDs...)
	}
	return diagnostic
}

func resolveSemanticParametersByExactName(model *Model, name string) []Parameter {
	if model == nil {
		return nil
	}

	matches := make([]Parameter, 0)
	for _, parameter := range model.Parameters {
		if parameter.Name == name {
			matches = append(matches, parameter)
		}
	}
	return matches
}

func resolveMetadataByExactKey(model *Model, key string) []Metadata {
	if model == nil {
		return nil
	}
	matches := make([]Metadata, 0)
	for _, entry := range model.Metadata {
		if entry.Key == key {
			matches = append(matches, entry)
		}
	}
	return matches
}

func resolveFeaturesByExactName(model *Model, name string) []Feature {
	if model == nil {
		return nil
	}
	matches := make([]Feature, 0)
	for _, feature := range model.Features {
		if feature.Name == name {
			matches = append(matches, feature)
		}
	}
	return matches
}

func resolveComponentsByExactName(model *Model, name string) []Component {
	if model == nil {
		return nil
	}
	matches := make([]Component, 0)
	for _, component := range model.Components {
		if component.Name == name {
			matches = append(matches, component)
		}
	}
	return matches
}

func resolveRootOutputComponent(model *Model) *Component {
	if model == nil {
		return nil
	}
	for _, component := range model.Components {
		if component.ID == model.RootComponentID {
			componentCopy := component
			return &componentCopy
		}
	}
	return nil
}

func mutationScopeForComponent(model *Model, componentID string) string {
	if model == nil {
		return ""
	}
	for _, component := range model.Components {
		if component.ID != componentID {
			continue
		}
		if component.Kind == "assembly" {
			return "assembly"
		}
		return "part"
	}
	return ""
}

func collectParameterIDs(matches []Parameter) []string {
	ids := make([]string, 0, len(matches))
	for _, match := range matches {
		ids = append(ids, match.ID)
	}
	slices.Sort(ids)
	return ids
}

func collectMetadataIDs(matches []Metadata) []string {
	ids := make([]string, 0, len(matches))
	for _, match := range matches {
		ids = append(ids, match.ID)
	}
	slices.Sort(ids)
	return ids
}

func resolveIntentString(expr dsl.ExpressionNode, consts map[string]dsl.ConstantValue) (string, error) {
	if expr == nil {
		return "", nil
	}

	value, err := resolveIntentValue(expr, consts)
	if err != nil {
		return "", err
	}

	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("must resolve to string, got %T", value)
	}
	return strings.TrimSpace(text), nil
}

func resolveIntentStringArray(expr dsl.ExpressionNode, consts map[string]dsl.ConstantValue) ([]string, bool, error) {
	if expr == nil {
		return nil, false, nil
	}

	value, err := resolveIntentValue(expr, consts)
	if err != nil {
		return nil, true, err
	}

	items, ok := value.([]interface{})
	if !ok {
		return nil, true, fmt.Errorf("must resolve to an array of strings, got %T", value)
	}

	out := make([]string, 0, len(items))
	for idx, item := range items {
		text, ok := item.(string)
		if !ok {
			return nil, true, fmt.Errorf("item %d must resolve to string, got %T", idx, item)
		}
		format := strings.ToLower(strings.TrimSpace(text))
		if format == "" {
			return nil, true, fmt.Errorf("item %d must not be empty", idx)
		}
		out = append(out, format)
	}
	return out, true, nil
}

func resolveIntentValue(expr dsl.ExpressionNode, consts map[string]dsl.ConstantValue) (interface{}, error) {
	switch e := expr.(type) {
	case *dsl.LiteralExpression:
		return e.Value, nil
	case *dsl.IdentifierExpression:
		value, ok := consts[e.Name]
		if !ok {
			return nil, fmt.Errorf("undefined constant: %s", e.Name)
		}
		return cloneIntentResolvedValue(value.Value), nil
	case *dsl.ArrayExpression:
		items := make([]interface{}, 0, len(e.Elements))
		for _, item := range e.Elements {
			resolved, err := resolveIntentValue(item, consts)
			if err != nil {
				return nil, err
			}
			items = append(items, resolved)
		}
		return items, nil
	default:
		return nil, fmt.Errorf("must be a literal, array literal, or constant reference")
	}
}

func cloneIntentResolvedValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case []interface{}:
		out := make([]interface{}, len(typed))
		for i := range typed {
			out[i] = cloneIntentResolvedValue(typed[i])
		}
		return out
	default:
		return typed
	}
}

func normalizeIntentAdapter(adapter string) string {
	return strings.ToLower(strings.TrimSpace(adapter))
}

func isSemanticCaptureBackedAdapter(adapter string) bool {
	return normalizeIntentAdapter(adapter) == "freecad"
}
