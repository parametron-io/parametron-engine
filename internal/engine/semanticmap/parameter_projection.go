package semanticmap

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"parametron/internal/engine/semantic"
)

var (
	ErrProjection                        = errors.New("semantic map projection error")
	ErrProjectionMissingSemanticMap      = errors.New("semantic map projection missing semantic map")
	ErrProjectionInvalidSemanticMap      = errors.New("semantic map projection invalid semantic map")
	ErrProjectionInvalidModel            = errors.New("semantic map projection invalid semantic model")
	ErrProjectionMissingIdentityLinkage  = errors.New("semantic map projection missing identity linkage")
	ErrProjectionMissingSemanticIntent   = errors.New("semantic map projection missing semantic intent")
	ErrProjectionContractNotReady        = errors.New("semantic map projection contract not ready")
	ErrProjectionUnsupportedSemanticType = errors.New("semantic map projection unsupported semantic type")
	ErrProjectionUnsupportedValueKind    = errors.New("semantic map projection unsupported value kind")
	ErrProjectionUnsupportedUnit         = errors.New("semantic map projection unsupported unit")
	ErrProjectionCoercionFailure         = errors.New("semantic map projection coercion failure")
	ErrProjectionMissingFinalValue       = errors.New("semantic map projection missing final value")
	ErrProjectionNonProjectableParameter = errors.New("semantic map projection non-projectable parameter")
	ErrProjectionNonProjectableOutput    = errors.New("semantic map projection non-projectable output")
	ErrProjectionNonProjectableMutation  = errors.New("semantic map projection non-projectable mutation")
)

const (
	projectionCodeMissingSemanticMap      = "projection_missing_semantic_map"
	projectionCodeInvalidSemanticMap      = "projection_invalid_semantic_map"
	projectionCodeInvalidModel            = "projection_invalid_semantic_model"
	projectionCodeMissingIdentityLinkage  = "projection_missing_identity_linkage"
	projectionCodeMissingSemanticIntent   = "projection_missing_semantic_intent"
	projectionCodeContractNotReady        = "projection_contract_not_ready"
	projectionCodeUnsupportedSemanticType = "projection_unsupported_semantic_type"
	projectionCodeUnsupportedValueKind    = "projection_unsupported_value_kind"
	projectionCodeUnsupportedUnit         = "projection_unsupported_unit"
	projectionCodeCoercionFailure         = "projection_coercion_failure"
	projectionCodeMissingFinalValue       = "projection_missing_final_value"
	projectionCodeNonProjectableParameter = "projection_non_projectable_parameter"
	projectionCodeNonProjectableOutput    = "projection_non_projectable_output"
	projectionCodeNonProjectableMutation  = "projection_non_projectable_mutation"
)

type ManifestParameterAssignment struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
	Type  string  `json:"type"`
	Unit  string  `json:"unit"`
}

type ParameterProjectionRequest struct {
	Model          *semantic.Model
	Intent         *semantic.ProductIntent
	ResolvedValues map[string]any
	Contract       *SemanticMap
}

type ProjectionError struct {
	Code                string
	Message             string
	ParameterName       string
	SemanticParameterID string
	SemanticType        string
	Problems            []string
	Err                 error
}

func (e *ProjectionError) Error() string {
	if e == nil || strings.TrimSpace(e.Message) == "" {
		return ErrProjection.Error()
	}
	return fmt.Sprintf("%s: %s", ErrProjection, e.Message)
}

func (e *ProjectionError) Unwrap() []error {
	if e == nil {
		return []error{ErrProjection}
	}

	out := []error{ErrProjection}
	switch e.Code {
	case projectionCodeMissingSemanticMap:
		out = append(out, ErrProjectionMissingSemanticMap)
	case projectionCodeInvalidSemanticMap:
		out = append(out, ErrProjectionInvalidSemanticMap)
	case projectionCodeInvalidModel:
		out = append(out, ErrProjectionInvalidModel)
	case projectionCodeMissingIdentityLinkage:
		out = append(out, ErrProjectionMissingIdentityLinkage)
	case projectionCodeMissingSemanticIntent:
		out = append(out, ErrProjectionMissingSemanticIntent)
	case projectionCodeContractNotReady:
		out = append(out, ErrProjectionContractNotReady)
	case projectionCodeUnsupportedSemanticType:
		out = append(out, ErrProjectionUnsupportedSemanticType)
	case projectionCodeUnsupportedValueKind:
		out = append(out, ErrProjectionUnsupportedValueKind)
	case projectionCodeUnsupportedUnit:
		out = append(out, ErrProjectionUnsupportedUnit)
	case projectionCodeCoercionFailure:
		out = append(out, ErrProjectionCoercionFailure)
	case projectionCodeMissingFinalValue:
		out = append(out, ErrProjectionMissingFinalValue)
	case projectionCodeNonProjectableParameter:
		out = append(out, ErrProjectionNonProjectableParameter)
	case projectionCodeNonProjectableOutput:
		out = append(out, ErrProjectionNonProjectableOutput)
	case projectionCodeNonProjectableMutation:
		out = append(out, ErrProjectionNonProjectableMutation)
	}
	if e.Err != nil {
		out = append(out, e.Err)
	}
	return out
}

func (e *ProjectionError) ProblemList() []string {
	if e == nil {
		return nil
	}
	return append([]string(nil), e.Problems...)
}

func ProjectParameterAssignments(request *ParameterProjectionRequest) ([]ManifestParameterAssignment, error) {
	if request == nil || request.Contract == nil {
		return nil, &ProjectionError{
			Code:    projectionCodeMissingSemanticMap,
			Message: "semantic map must not be nil",
		}
	}

	if err := validateProjectionLinkageContract(request.Contract); err != nil {
		problems := projectionProblems(err)
		return nil, &ProjectionError{
			Code:     projectionCodeInvalidSemanticMap,
			Message:  strings.Join(problems, "; "),
			Problems: problems,
			Err:      err,
		}
	}

	review := ReviewProjectionContract(request.Contract)
	if !review.Ready {
		return nil, &ProjectionError{
			Code:     projectionCodeContractNotReady,
			Message:  strings.Join(review.Problems, "; "),
			Problems: append([]string(nil), review.Problems...),
		}
	}
	parameterMapping := request.Contract.SemanticToManifest.Parameters

	if request.Intent == nil || len(request.Intent.ExportedParameters) == 0 {
		return []ManifestParameterAssignment{}, nil
	}
	if request.Model == nil {
		return nil, &ProjectionError{
			Code:    projectionCodeNonProjectableParameter,
			Message: "semantic model must not be nil when projecting exported parameters",
		}
	}

	parametersByID := make(map[string]semantic.Parameter, len(request.Model.Parameters))
	for _, parameter := range request.Model.CanonicalParameters() {
		parametersByID[parameter.ID] = parameter
	}

	exported := append([]semantic.ExportedParameterIntent(nil), request.Intent.ExportedParameters...)
	slices.SortFunc(exported, func(a, b semantic.ExportedParameterIntent) int {
		if a.DSLParameterName != b.DSLParameterName {
			return compareStrings(a.DSLParameterName, b.DSLParameterName)
		}
		if a.SemanticParameterID != b.SemanticParameterID {
			return compareStrings(a.SemanticParameterID, b.SemanticParameterID)
		}
		return compareStrings(a.Type, b.Type)
	})

	assignments := make([]ManifestParameterAssignment, 0, len(exported))
	for _, exportedParam := range exported {
		resolvedValue, ok := request.ResolvedValues[exportedParam.DSLParameterName]
		if !ok {
			return nil, &ProjectionError{
				Code:                projectionCodeMissingFinalValue,
				Message:             fmt.Sprintf("exported parameter %q is missing a resolved final value", exportedParam.DSLParameterName),
				ParameterName:       exportedParam.DSLParameterName,
				SemanticParameterID: exportedParam.SemanticParameterID,
			}
		}

		semanticParameter, ok := parametersByID[exportedParam.SemanticParameterID]
		if !ok {
			return nil, &ProjectionError{
				Code:                projectionCodeNonProjectableParameter,
				Message:             fmt.Sprintf("exported parameter %q references unknown semantic parameter %q", exportedParam.DSLParameterName, exportedParam.SemanticParameterID),
				ParameterName:       exportedParam.DSLParameterName,
				SemanticParameterID: exportedParam.SemanticParameterID,
			}
		}

		semanticTypeName, semanticType, err := resolveParameterSemanticType(request.Contract, semanticParameter)
		if err != nil {
			return nil, err
		}

		rawScalar, err := scalarValueFromResolvedValue(resolvedValue)
		if err != nil {
			return nil, &ProjectionError{
				Code:                projectionCodeNonProjectableParameter,
				Message:             fmt.Sprintf("exported parameter %q resolved to unsupported value kind %T", exportedParam.DSLParameterName, resolvedValue),
				ParameterName:       exportedParam.DSLParameterName,
				SemanticParameterID: exportedParam.SemanticParameterID,
				SemanticType:        semanticTypeName,
				Err:                 err,
			}
		}

		coerced, err := semantic.CoerceValue(&semantic.CoercionRequest{
			ParameterName:       exportedParam.DSLParameterName,
			SemanticParameterID: exportedParam.SemanticParameterID,
			Rule:                semanticType.Coercion,
			Value:               rawScalar,
		})
		if err != nil {
			return nil, &ProjectionError{
				Code:                projectionCodeCoercionFailure,
				Message:             err.Error(),
				ParameterName:       exportedParam.DSLParameterName,
				SemanticParameterID: exportedParam.SemanticParameterID,
				SemanticType:        semanticTypeName,
				Err:                 err,
			}
		}

		if semanticType.ValueKind != "number" {
			return nil, &ProjectionError{
				Code:                projectionCodeUnsupportedValueKind,
				Message:             fmt.Sprintf("exported parameter %q semantic type %q valueKind %q is not projectable to manifest parameterAssignments", exportedParam.DSLParameterName, semanticTypeName, semanticType.ValueKind),
				ParameterName:       exportedParam.DSLParameterName,
				SemanticParameterID: exportedParam.SemanticParameterID,
				SemanticType:        semanticTypeName,
			}
		}
		if semanticType.DefaultUnit != "mm" {
			return nil, &ProjectionError{
				Code:                projectionCodeUnsupportedUnit,
				Message:             fmt.Sprintf("exported parameter %q semantic type %q unit %q is not projectable to manifest parameterAssignments", exportedParam.DSLParameterName, semanticTypeName, semanticType.DefaultUnit),
				ParameterName:       exportedParam.DSLParameterName,
				SemanticParameterID: exportedParam.SemanticParameterID,
				SemanticType:        semanticTypeName,
			}
		}
		if coerced == nil || coerced.Value.IsZero() {
			return nil, &ProjectionError{
				Code:                projectionCodeMissingFinalValue,
				Message:             fmt.Sprintf("exported parameter %q is missing a projected final value", exportedParam.DSLParameterName),
				ParameterName:       exportedParam.DSLParameterName,
				SemanticParameterID: exportedParam.SemanticParameterID,
				SemanticType:        semanticTypeName,
			}
		}

		value, err := decodeProjectedNumber(coerced.Value)
		if err != nil {
			return nil, &ProjectionError{
				Code:                projectionCodeNonProjectableParameter,
				Message:             fmt.Sprintf("exported parameter %q projected value is not JSON-number compatible", exportedParam.DSLParameterName),
				ParameterName:       exportedParam.DSLParameterName,
				SemanticParameterID: exportedParam.SemanticParameterID,
				SemanticType:        semanticTypeName,
				Err:                 err,
			}
		}

		assignments = append(assignments, ManifestParameterAssignment{
			Name:  exportedParam.DSLParameterName,
			Value: value,
			Type:  parameterMapping.Type,
			Unit:  "mm",
		})
	}

	return assignments, nil
}

func resolveParameterSemanticType(contract *SemanticMap, parameter semantic.Parameter) (string, ParameterType, error) {
	for _, mapping := range contract.CaptureToSemantic.Parameters {
		if mapping.CaptureKind != "parameter" || mapping.SemanticKind != "parameter" {
			continue
		}
		semanticTypeName, ok := mapping.SemanticType.Map[parameter.NativeType]
		if !ok {
			continue
		}
		semanticType, ok := contract.SemanticTypes.ParameterTypes[semanticTypeName]
		if !ok {
			break
		}
		return semanticTypeName, semanticType, nil
	}

	return "", ParameterType{}, &ProjectionError{
		Code:                projectionCodeUnsupportedSemanticType,
		Message:             fmt.Sprintf("semantic parameter %q native type %q does not resolve to a projectable semantic type", parameter.ID, parameter.NativeType),
		ParameterName:       parameter.Name,
		SemanticParameterID: parameter.ID,
	}
}

func scalarValueFromResolvedValue(value any) (semantic.ScalarValue, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return semantic.ScalarValue{}, err
	}
	return semantic.NewScalarValue(data)
}

func decodeProjectedNumber(value semantic.ScalarValue) (float64, error) {
	decoder := json.NewDecoder(bytes.NewReader(value.Raw()))
	decoder.UseNumber()

	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return 0, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != nil && err != io.EOF {
		return 0, err
	} else if err == nil {
		return 0, fmt.Errorf("unexpected trailing JSON content")
	}

	number, ok := decoded.(json.Number)
	if !ok {
		return 0, fmt.Errorf("value is not numeric")
	}
	return number.Float64()
}

func projectionProblems(err error) []string {
	var validationErr *ValidationError
	if errors.As(err, &validationErr) {
		return validationErr.Messages()
	}
	if err == nil {
		return nil
	}
	return []string{err.Error()}
}
