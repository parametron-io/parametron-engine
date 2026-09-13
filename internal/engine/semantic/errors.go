package semantic

import (
	"errors"
	"fmt"
	"strings"
)

var ErrValidation = errors.New("semantic model validation error")
var ErrDSLIntent = errors.New("semantic DSL intent resolution error")
var ErrCoercion = errors.New("semantic coercion error")

const (
	DSLIntentDiagnosticCategoryInvalidDSLReference          = "invalid_dsl_reference"
	DSLIntentDiagnosticCategoryAmbiguousDSLReference        = "ambiguous_dsl_reference"
	DSLIntentDiagnosticCategorySourceModelReferenceMismatch = "source_model_reference_mismatch"

	DSLIntentDiagnosticCodeSourceModelLogicalIDMismatch        = "source_model_logical_id_mismatch"
	DSLIntentDiagnosticCodeSemanticParameterReferenceNotFound  = "semantic_parameter_reference_not_found"
	DSLIntentDiagnosticCodeSemanticParameterReferenceAmbiguous = "semantic_parameter_reference_ambiguous"
	DSLIntentDiagnosticCodeSemanticMetadataReferenceNotFound   = "semantic_metadata_reference_not_found"
	DSLIntentDiagnosticCodeSemanticMetadataReferenceAmbiguous  = "semantic_metadata_reference_ambiguous"
	DSLIntentDiagnosticCodeSemanticFeatureReferenceNotFound    = "semantic_feature_reference_not_found"
	DSLIntentDiagnosticCodeSemanticFeatureReferenceAmbiguous   = "semantic_feature_reference_ambiguous"
	DSLIntentDiagnosticCodeSemanticComponentReferenceNotFound  = "semantic_component_reference_not_found"
	DSLIntentDiagnosticCodeSemanticComponentReferenceAmbiguous = "semantic_component_reference_ambiguous"
	DSLIntentDiagnosticCodeSemanticOutputTargetNotProjectable  = "semantic_output_target_not_projectable"
	DSLIntentDiagnosticCodeDSLIntentResolutionFailed           = "dsl_intent_resolution_failed"
)

type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	if e == nil || len(e.Problems) == 0 {
		return ErrValidation.Error()
	}
	return fmt.Sprintf("%s: %s", ErrValidation, strings.Join(e.Problems, "; "))
}

func (e *ValidationError) Unwrap() error {
	return ErrValidation
}

func (e *ValidationError) Messages() []string {
	if e == nil {
		return nil
	}
	out := make([]string, len(e.Problems))
	copy(out, e.Problems)
	return out
}

type DSLIntentDiagnostic struct {
	Category             string
	Code                 string
	Message              string
	ProductName          string
	ParameterName        string
	MatchingParameterIDs []string
}

type DSLIntentError struct {
	Problems []DSLIntentDiagnostic
}

func (e *DSLIntentError) Error() string {
	if e == nil || len(e.Problems) == 0 {
		return ErrDSLIntent.Error()
	}
	return fmt.Sprintf("%s: %s", ErrDSLIntent, strings.Join(e.Messages(), "; "))
}

func (e *DSLIntentError) Unwrap() error {
	return ErrDSLIntent
}

func (e *DSLIntentError) Messages() []string {
	if e == nil {
		return nil
	}
	out := make([]string, 0, len(e.Problems))
	for _, problem := range e.Problems {
		out = append(out, problem.Message)
	}
	return out
}

func (e *DSLIntentError) Diagnostics() []DSLIntentDiagnostic {
	if e == nil {
		return nil
	}
	out := make([]DSLIntentDiagnostic, len(e.Problems))
	copy(out, e.Problems)
	for i := range out {
		out[i].MatchingParameterIDs = append([]string(nil), out[i].MatchingParameterIDs...)
	}
	return out
}
