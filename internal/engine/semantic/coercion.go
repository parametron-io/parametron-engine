package semantic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
)

const (
	CoercionRuleNumberToLength        = "number_to_length"
	CoercionRuleNumberToIntegerExact  = "number_to_integer_exact"
	CoercionRuleNumberToDimensionless = "number_to_dimensionless"
)

const (
	CoercionDiagnosticCategory = "coercion"
)

const (
	CoercionSeverityError   = "error"
	CoercionSeverityWarning = "warning"
)

const (
	CoercionCodeInvalidRequest         = "coercion_invalid_request"
	CoercionCodeUnsupportedRule        = "coercion_unsupported_rule"
	CoercionCodeInvalidValueKind       = "coercion_invalid_value_kind"
	CoercionCodeNonIntegralNumber      = "coercion_non_integral_number"
	CoercionCodeExactIntegerNormalized = "coercion_exact_integer_normalized"
)

type CoercionRequest struct {
	ParameterName       string
	SemanticParameterID string
	Rule                string
	Value               ScalarValue
}

type CoercionResult struct {
	ValueKind   string
	Value       ScalarValue
	Diagnostics []CoercionDiagnostic
}

type CoercionDiagnostic struct {
	Category            string
	Code                string
	Severity            string
	ParameterName       string
	SemanticParameterID string
	Rule                string
	Input               ScalarValue
	Output              ScalarValue
	Message             string
}

type CoercionError struct {
	ParameterName       string
	SemanticParameterID string
	Rule                string
	Input               ScalarValue
	Code                string
	Reason              string
	Diagnostic          CoercionDiagnostic
}

func (e *CoercionError) Error() string {
	if e == nil {
		return ErrCoercion.Error()
	}
	return fmt.Sprintf("%s: %s", ErrCoercion, coercionMessage(e.Diagnostic))
}

func (e *CoercionError) Unwrap() error {
	return ErrCoercion
}

func IsSupportedCoercionRule(rule string) bool {
	switch rule {
	case CoercionRuleNumberToLength, CoercionRuleNumberToIntegerExact, CoercionRuleNumberToDimensionless:
		return true
	default:
		return false
	}
}

func CoerceValue(request *CoercionRequest) (*CoercionResult, error) {
	if request == nil {
		return nil, newCoercionError(nil, CoercionCodeInvalidRequest, ScalarValue{})
	}
	if request.Rule == "" {
		return nil, newCoercionError(request, CoercionCodeInvalidRequest, ScalarValue{})
	}
	if !IsSupportedCoercionRule(request.Rule) {
		return nil, newCoercionError(request, CoercionCodeUnsupportedRule, ScalarValue{})
	}

	number, err := decodeNumericScalar(request.Value)
	if err != nil {
		return nil, newCoercionError(request, CoercionCodeInvalidValueKind, ScalarValue{})
	}

	switch request.Rule {
	case CoercionRuleNumberToLength, CoercionRuleNumberToDimensionless:
		return &CoercionResult{
			ValueKind: "number",
			Value:     cloneScalarValue(request.Value),
		}, nil
	case CoercionRuleNumberToIntegerExact:
		rational, ok := new(big.Rat).SetString(number.String())
		if !ok || !rational.IsInt() {
			return nil, newCoercionError(request, CoercionCodeNonIntegralNumber, ScalarValue{})
		}
		normalized, err := NewScalarValue([]byte(rational.Num().String()))
		if err != nil {
			return nil, newCoercionError(request, CoercionCodeInvalidValueKind, ScalarValue{})
		}

		result := &CoercionResult{
			ValueKind: "number",
			Value:     normalized,
		}
		if string(request.Value.Raw()) != string(normalized.Raw()) {
			result.Diagnostics = []CoercionDiagnostic{
				newCoercionDiagnostic(request, CoercionCodeExactIntegerNormalized, cloneScalarValue(request.Value), normalized),
			}
		}
		return result, nil
	default:
		return nil, newCoercionError(request, CoercionCodeUnsupportedRule, ScalarValue{})
	}
}

func decodeNumericScalar(value ScalarValue) (json.Number, error) {
	if value.IsZero() {
		return "", fmt.Errorf("value must be set")
	}

	decoder := json.NewDecoder(bytes.NewReader(value.raw))
	decoder.UseNumber()

	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return "", err
	}
	var extra any
	if err := decoder.Decode(&extra); err != nil && err != io.EOF {
		return "", err
	} else if err == nil {
		return "", fmt.Errorf("unexpected trailing JSON content")
	}

	number, ok := decoded.(json.Number)
	if !ok {
		return "", fmt.Errorf("value is not numeric")
	}
	return number, nil
}

func newCoercionError(request *CoercionRequest, code string, output ScalarValue) *CoercionError {
	diagnostic := newCoercionDiagnostic(request, code, inputScalarValue(request), output)
	return &CoercionError{
		ParameterName:       diagnostic.ParameterName,
		SemanticParameterID: diagnostic.SemanticParameterID,
		Rule:                diagnostic.Rule,
		Input:               diagnostic.Input,
		Code:                diagnostic.Code,
		Reason:              diagnostic.Code,
		Diagnostic:          diagnostic,
	}
}

func newCoercionDiagnostic(request *CoercionRequest, code string, input ScalarValue, output ScalarValue) CoercionDiagnostic {
	diagnostic := CoercionDiagnostic{
		Category: CoercionDiagnosticCategory,
		Code:     code,
		Input:    cloneScalarValue(input),
		Output:   cloneScalarValue(output),
	}
	if request != nil {
		diagnostic.ParameterName = request.ParameterName
		diagnostic.SemanticParameterID = request.SemanticParameterID
		diagnostic.Rule = request.Rule
	}
	switch code {
	case CoercionCodeInvalidRequest:
		diagnostic.Severity = CoercionSeverityError
	case CoercionCodeUnsupportedRule:
		diagnostic.Severity = CoercionSeverityError
	case CoercionCodeInvalidValueKind:
		diagnostic.Severity = CoercionSeverityError
	case CoercionCodeNonIntegralNumber:
		diagnostic.Severity = CoercionSeverityError
	case CoercionCodeExactIntegerNormalized:
		diagnostic.Severity = CoercionSeverityWarning
	default:
		diagnostic.Severity = CoercionSeverityError
	}
	diagnostic.Message = coercionMessage(diagnostic)
	return diagnostic
}

func coercionMessage(diagnostic CoercionDiagnostic) string {
	target := "semantic value"
	switch {
	case diagnostic.ParameterName != "":
		target = fmt.Sprintf("parameter %q", diagnostic.ParameterName)
	case diagnostic.SemanticParameterID != "":
		target = fmt.Sprintf("semantic parameter %q", diagnostic.SemanticParameterID)
	}

	input := "<unset>"
	if !diagnostic.Input.IsZero() {
		input = string(diagnostic.Input.Raw())
	}
	output := "<unset>"
	if !diagnostic.Output.IsZero() {
		output = string(diagnostic.Output.Raw())
	}

	switch diagnostic.Code {
	case CoercionCodeInvalidRequest:
		return fmt.Sprintf("%s coercion request is invalid", target)
	case CoercionCodeUnsupportedRule:
		return fmt.Sprintf("%s coercion %q is not supported for input %s", target, diagnostic.Rule, input)
	case CoercionCodeInvalidValueKind:
		return fmt.Sprintf("%s coercion %q requires a numeric DSL value, got %s", target, diagnostic.Rule, input)
	case CoercionCodeNonIntegralNumber:
		return fmt.Sprintf("%s coercion %q requires an exact integer numeric DSL value, got %s", target, diagnostic.Rule, input)
	case CoercionCodeExactIntegerNormalized:
		return fmt.Sprintf("%s coercion %q normalized exact integer input %s to %s", target, diagnostic.Rule, input, output)
	default:
		return fmt.Sprintf("%s coercion %q failed for input %s", target, diagnostic.Rule, input)
	}
}

func inputScalarValue(request *CoercionRequest) ScalarValue {
	if request == nil {
		return ScalarValue{}
	}
	return cloneScalarValue(request.Value)
}

func cloneScalarValue(value ScalarValue) ScalarValue {
	if value.IsZero() {
		return ScalarValue{}
	}
	cloned, err := NewScalarValue(value.Raw())
	if err != nil {
		return ScalarValue{}
	}
	return cloned
}
