package cad

import (
	"fmt"
	"slices"
	"strings"

	"parametron/internal/authoring/dsl"
)

// ValidateAuthoringReferences enforces currently expressible DSL references
// against the loaded capture contract. Surfaces that are not yet expressible in
// the DSL are intentionally left unvalidated here rather than guessed.
func ValidateAuthoringReferences(ast *dsl.AST, contract *CADContract) error {
	problems := validateAuthoringReferences(ast, contract)
	if len(problems) == 0 {
		return nil
	}
	return &AuthoringValidationError{Problems: problems}
}

func validateAuthoringReferences(ast *dsl.AST, contract *CADContract) []AuthoringDiagnostic {
	if ast == nil || contract == nil {
		return nil
	}

	var problems []AuthoringDiagnostic
	for _, product := range ast.Products {
		if product == nil {
			continue
		}

		adapter, err := resolveAuthoringString(product.Adapter, ast.ResolvedConstants)
		if err != nil {
			problems = append(problems, newAuthoringDiagnostic(
				AuthoringDiagnosticCategoryInvalidDSLReference,
				"adapter_resolution_failed",
				fmt.Sprintf("product %q adapter: %v", product.Name, err),
			))
			continue
		}
		if !isCaptureBackedAdapter(adapter) {
			continue
		}

		sourceModel, err := resolveAuthoringString(product.SourceModel, ast.ResolvedConstants)
		if err != nil {
			problems = append(problems, newAuthoringDiagnostic(
				AuthoringDiagnosticCategoryInvalidDSLReference,
				"source_model_resolution_failed",
				fmt.Sprintf("product %q source_model: %v", product.Name, err),
			))
			continue
		}
		if sourceModel != "" && contract.SourceDocument.LogicalID != "" && sourceModel != contract.SourceDocument.LogicalID {
			problems = append(problems, newAuthoringDiagnostic(
				AuthoringDiagnosticCategorySourceModelReferenceMismatch,
				AuthoringDiagnosticCodeSourceModelLogicalIDMismatch,
				fmt.Sprintf("product %q source_model %q does not match capture sourceDocument.logicalId %q", product.Name, sourceModel, contract.SourceDocument.LogicalID),
			))
		}

		// The current DSL only expresses output formats, not capture-backed output
		// targets or scopes, so there is nothing additional to resolve here yet.
		for _, param := range product.Parameters {
			if param == nil || param.Type.Kind != dsl.ParamTypeNumber {
				continue
			}

			matches := resolveCapturedParametersByExactName(contract, param.Name)
			switch len(matches) {
			case 0:
				problems = append(problems, newAuthoringDiagnostic(
					AuthoringDiagnosticCategoryInvalidDSLReference,
					AuthoringDiagnosticCodeCapturedParameterReferenceNotFound,
					fmt.Sprintf("product %q parameter %q does not resolve to a captured parameter by exact name", product.Name, param.Name),
				))
			case 1:
				continue
			default:
				ids := make([]string, 0, len(matches))
				for _, match := range matches {
					ids = append(ids, match.ID)
				}
				slices.Sort(ids)
				problems = append(problems, newAuthoringDiagnostic(
					AuthoringDiagnosticCategoryAmbiguousDSLReference,
					AuthoringDiagnosticCodeCapturedParameterReferenceAmbiguous,
					fmt.Sprintf("product %q parameter %q is ambiguous across captured parameters %q", product.Name, param.Name, ids),
				))
			}
		}

		// No current DSL syntax carries explicit capture group, metadata, mutation
		// target, or output scope references, so those surfaces remain deliberately
		// unimplemented at this boundary.
	}

	return problems
}

func newAuthoringDiagnostic(category, code, message string) AuthoringDiagnostic {
	return AuthoringDiagnostic{
		Category: category,
		Code:     code,
		Message:  message,
	}
}

func resolveCapturedParametersByExactName(contract *CADContract, name string) []Parameter {
	if contract == nil {
		return nil
	}

	matches := make([]Parameter, 0)
	for _, parameter := range contract.Entities.Parameters {
		if parameter.Name == name {
			matches = append(matches, parameter)
		}
	}
	return matches
}

func resolveAuthoringString(expr dsl.ExpressionNode, consts map[string]dsl.ConstantValue) (string, error) {
	if expr == nil {
		return "", nil
	}

	switch e := expr.(type) {
	case *dsl.LiteralExpression:
		value, ok := e.Value.(string)
		if !ok {
			return "", fmt.Errorf("must resolve to string, got %T", e.Value)
		}
		return strings.TrimSpace(value), nil
	case *dsl.IdentifierExpression:
		value, exists := consts[e.Name]
		if !exists {
			return "", fmt.Errorf("undefined constant: %s", e.Name)
		}
		text, ok := value.Value.(string)
		if !ok {
			return "", fmt.Errorf("constant %q must resolve to string, got %T", e.Name, value.Value)
		}
		return strings.TrimSpace(text), nil
	default:
		return "", fmt.Errorf("must be a string literal or string constant")
	}
}

func isCaptureBackedAdapter(adapter string) bool {
	return strings.EqualFold(strings.TrimSpace(adapter), "freecad")
}
