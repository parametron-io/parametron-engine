package semantic

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"parametron/internal/authoring/dsl"
	"parametron/internal/engine/cad"
)

func TestCoerceValueNumberToLengthAcceptsNumericValues(t *testing.T) {
	integerValue, err := NewScalarValue([]byte(`6`))
	if err != nil {
		t.Fatalf("NewScalarValue(integer) returned error: %v", err)
	}
	fractionalValue, err := NewScalarValue([]byte(`6.25`))
	if err != nil {
		t.Fatalf("NewScalarValue(fractional) returned error: %v", err)
	}

	tests := []struct {
		name  string
		value ScalarValue
		want  string
	}{
		{name: "integer_like", value: integerValue, want: `6`},
		{name: "fractional", value: fractionalValue, want: `6.25`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CoerceValue(&CoercionRequest{
				ParameterName: "width",
				Rule:          CoercionRuleNumberToLength,
				Value:         tt.value,
			})
			if err != nil {
				t.Fatalf("CoerceValue returned error: %v", err)
			}
			if result.ValueKind != "number" {
				t.Fatalf("unexpected value kind: %q", result.ValueKind)
			}
			if got := string(result.Value.Raw()); got != tt.want {
				t.Fatalf("unexpected coerced value: got %q want %q", got, tt.want)
			}
			if len(result.Diagnostics) != 0 {
				t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
			}
		})
	}
}

func TestCoerceValueNumberToDimensionlessAcceptsNumericValues(t *testing.T) {
	integerValue, err := NewScalarValue([]byte(`2`))
	if err != nil {
		t.Fatalf("NewScalarValue(integer) returned error: %v", err)
	}
	fractionalValue, err := NewScalarValue([]byte(`2.5`))
	if err != nil {
		t.Fatalf("NewScalarValue(fractional) returned error: %v", err)
	}

	tests := []struct {
		name  string
		value ScalarValue
		want  string
	}{
		{name: "integer_like", value: integerValue, want: `2`},
		{name: "fractional", value: fractionalValue, want: `2.5`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CoerceValue(&CoercionRequest{
				SemanticParameterID: "par.root.scale",
				Rule:                CoercionRuleNumberToDimensionless,
				Value:               tt.value,
			})
			if err != nil {
				t.Fatalf("CoerceValue returned error: %v", err)
			}
			if result.ValueKind != "number" {
				t.Fatalf("unexpected value kind: %q", result.ValueKind)
			}
			if got := string(result.Value.Raw()); got != tt.want {
				t.Fatalf("unexpected coerced value: got %q want %q", got, tt.want)
			}
			if len(result.Diagnostics) != 0 {
				t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
			}
		})
	}
}

func TestCoerceValueNumberToIntegerExactAcceptsExactIntegers(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "integer", input: `6`, want: `6`},
		{name: "zero", input: `0`, want: `0`},
		{name: "negative_integer", input: `-3`, want: `-3`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, err := NewScalarValue([]byte(tt.input))
			if err != nil {
				t.Fatalf("NewScalarValue returned error: %v", err)
			}
			result, err := CoerceValue(&CoercionRequest{
				ParameterName: "count",
				Rule:          CoercionRuleNumberToIntegerExact,
				Value:         value,
			})
			if err != nil {
				t.Fatalf("CoerceValue returned error: %v", err)
			}
			if result.ValueKind != "number" {
				t.Fatalf("unexpected value kind: %q", result.ValueKind)
			}
			if got := string(result.Value.Raw()); got != tt.want {
				t.Fatalf("unexpected coerced value: got %q want %q", got, tt.want)
			}
			if len(result.Diagnostics) != 0 {
				t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
			}
		})
	}
}

func TestCoerceValueNumberToIntegerExactNormalizesExactFloatIntegerWithWarning(t *testing.T) {
	value, err := NewScalarValue([]byte(`6.0`))
	if err != nil {
		t.Fatalf("NewScalarValue returned error: %v", err)
	}

	result, err := CoerceValue(&CoercionRequest{
		ParameterName:       "count",
		SemanticParameterID: "par.root.count",
		Rule:                CoercionRuleNumberToIntegerExact,
		Value:               value,
	})
	if err != nil {
		t.Fatalf("CoerceValue returned error: %v", err)
	}
	if got := string(result.Value.Raw()); got != `6` {
		t.Fatalf("unexpected coerced value: got %q want %q", got, `6`)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("expected exactly one diagnostic, got %#v", result.Diagnostics)
	}

	diagnostic := result.Diagnostics[0]
	if diagnostic.Category != CoercionDiagnosticCategory {
		t.Fatalf("unexpected diagnostic category: %q", diagnostic.Category)
	}
	if diagnostic.Code != CoercionCodeExactIntegerNormalized {
		t.Fatalf("unexpected diagnostic code: %q", diagnostic.Code)
	}
	if diagnostic.Severity != CoercionSeverityWarning {
		t.Fatalf("unexpected diagnostic severity: %q", diagnostic.Severity)
	}
	if diagnostic.Rule != CoercionRuleNumberToIntegerExact {
		t.Fatalf("unexpected diagnostic rule: %q", diagnostic.Rule)
	}
	if diagnostic.ParameterName != "count" {
		t.Fatalf("unexpected parameter name: %q", diagnostic.ParameterName)
	}
	if diagnostic.SemanticParameterID != "par.root.count" {
		t.Fatalf("unexpected semantic parameter ID: %q", diagnostic.SemanticParameterID)
	}
	if got := string(diagnostic.Input.Raw()); got != `6.0` {
		t.Fatalf("unexpected diagnostic input: got %q want %q", got, `6.0`)
	}
	if got := string(diagnostic.Output.Raw()); got != `6` {
		t.Fatalf("unexpected diagnostic output: got %q want %q", got, `6`)
	}

	wantMessage := `parameter "count" coercion "number_to_integer_exact" normalized exact integer input 6.0 to 6`
	if diagnostic.Message != wantMessage {
		t.Fatalf("unexpected diagnostic message: got %q want %q", diagnostic.Message, wantMessage)
	}
}

func TestCoerceValueNormalizationDiagnosticsDeterministicAcrossRepeatedRuns(t *testing.T) {
	value, err := NewScalarValue([]byte(`6.0`))
	if err != nil {
		t.Fatalf("NewScalarValue returned error: %v", err)
	}

	request := &CoercionRequest{
		ParameterName:       "count",
		SemanticParameterID: "par.root.count",
		Rule:                CoercionRuleNumberToIntegerExact,
		Value:               value,
	}

	var wantDiagnostics []CoercionDiagnostic
	var wantMessage string

	for i := 0; i < 7; i++ {
		result, err := CoerceValue(request)
		if err != nil {
			t.Fatalf("CoerceValue iteration %d returned error: %v", i, err)
		}
		if result == nil {
			t.Fatalf("CoerceValue iteration %d returned nil result", i)
		}
		if len(result.Diagnostics) != 1 {
			t.Fatalf("CoerceValue iteration %d returned unexpected diagnostics: %#v", i, result.Diagnostics)
		}
		if result.Diagnostics[0].Code != CoercionCodeExactIntegerNormalized {
			t.Fatalf("CoerceValue iteration %d returned unexpected diagnostic order/code: %#v", i, result.Diagnostics)
		}

		if i == 0 {
			wantDiagnostics = append([]CoercionDiagnostic(nil), result.Diagnostics...)
			wantMessage = result.Diagnostics[0].Message
			continue
		}

		if !reflect.DeepEqual(result.Diagnostics, wantDiagnostics) {
			t.Fatalf("diagnostics drifted at iteration %d\nwant: %#v\ngot:  %#v", i, wantDiagnostics, result.Diagnostics)
		}
		if result.Diagnostics[0].Message != wantMessage {
			t.Fatalf("diagnostic message drifted at iteration %d\nwant: %q\ngot:  %q", i, wantMessage, result.Diagnostics[0].Message)
		}
	}
}

func TestCoerceValueNumberToIntegerExactRejectsFractionalValuesDeterministically(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "positive_fractional", input: `6.2`},
		{name: "negative_fractional", input: `-3.4`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, err := NewScalarValue([]byte(tt.input))
			if err != nil {
				t.Fatalf("NewScalarValue returned error: %v", err)
			}
			request := &CoercionRequest{
				ParameterName: "count",
				Rule:          CoercionRuleNumberToIntegerExact,
				Value:         value,
			}

			first, second := assertCoercionFailure(t, request), assertCoercionFailure(t, request)

			if first.Error() != second.Error() {
				t.Fatalf("expected deterministic error text\nfirst:  %q\nsecond: %q", first.Error(), second.Error())
			}
			if first.Code != CoercionCodeNonIntegralNumber || second.Code != CoercionCodeNonIntegralNumber {
				t.Fatalf("unexpected coercion codes: first=%q second=%q", first.Code, second.Code)
			}
			if first.Rule != CoercionRuleNumberToIntegerExact || second.Rule != CoercionRuleNumberToIntegerExact {
				t.Fatalf("unexpected coercion rules: first=%q second=%q", first.Rule, second.Rule)
			}
			if first.ParameterName != "count" || second.ParameterName != "count" {
				t.Fatalf("unexpected parameter names: first=%q second=%q", first.ParameterName, second.ParameterName)
			}
			if first.Diagnostic.Severity != CoercionSeverityError || second.Diagnostic.Severity != CoercionSeverityError {
				t.Fatalf("unexpected severities: first=%q second=%q", first.Diagnostic.Severity, second.Diagnostic.Severity)
			}
			if got := string(first.Input.Raw()); got != tt.input {
				t.Fatalf("unexpected first input: got %q want %q", got, tt.input)
			}
			if got := string(second.Input.Raw()); got != tt.input {
				t.Fatalf("unexpected second input: got %q want %q", got, tt.input)
			}
			if !reflect.DeepEqual(first.Diagnostic, second.Diagnostic) {
				t.Fatalf("expected deterministic diagnostics\nfirst:  %#v\nsecond: %#v", first.Diagnostic, second.Diagnostic)
			}
		})
	}
}

func TestCoerceValueRejectsUnknownRuleDeterministically(t *testing.T) {
	value, err := NewScalarValue([]byte(`6`))
	if err != nil {
		t.Fatalf("NewScalarValue returned error: %v", err)
	}
	request := &CoercionRequest{
		SemanticParameterID: "par.root.length",
		Rule:                "number_to_magic",
		Value:               value,
	}

	first, second := assertCoercionFailure(t, request), assertCoercionFailure(t, request)
	if first.Error() != second.Error() {
		t.Fatalf("expected deterministic error text\nfirst:  %q\nsecond: %q", first.Error(), second.Error())
	}
	if first.Code != CoercionCodeUnsupportedRule {
		t.Fatalf("unexpected coercion code: %q", first.Code)
	}
	if first.SemanticParameterID != "par.root.length" {
		t.Fatalf("unexpected semantic parameter ID: %q", first.SemanticParameterID)
	}
	if first.Diagnostic.Severity != CoercionSeverityError {
		t.Fatalf("unexpected diagnostic severity: %q", first.Diagnostic.Severity)
	}
}

func TestCoerceValueRejectsInvalidValueKind(t *testing.T) {
	value, err := NewScalarValue([]byte(`"six"`))
	if err != nil {
		t.Fatalf("NewScalarValue returned error: %v", err)
	}

	errDetail := assertCoercionFailure(t, &CoercionRequest{
		ParameterName: "count",
		Rule:          CoercionRuleNumberToIntegerExact,
		Value:         value,
	})
	if errDetail.Code != CoercionCodeInvalidValueKind {
		t.Fatalf("unexpected coercion code: %q", errDetail.Code)
	}
	if got := string(errDetail.Input.Raw()); got != `"six"` {
		t.Fatalf("unexpected input: %q", got)
	}
	if errDetail.Diagnostic.Severity != CoercionSeverityError {
		t.Fatalf("unexpected diagnostic severity: %q", errDetail.Diagnostic.Severity)
	}
}

func TestCoerceValueRejectsNilAndEmptyRequests(t *testing.T) {
	nilErr := assertCoercionFailure(t, nil)
	if nilErr.Code != CoercionCodeInvalidRequest {
		t.Fatalf("unexpected nil request code: %q", nilErr.Code)
	}
	if nilErr.Diagnostic.Severity != CoercionSeverityError {
		t.Fatalf("unexpected nil request severity: %q", nilErr.Diagnostic.Severity)
	}

	emptyErr := assertCoercionFailure(t, &CoercionRequest{})
	if emptyErr.Code != CoercionCodeInvalidRequest {
		t.Fatalf("unexpected empty request code: %q", emptyErr.Code)
	}
	if emptyErr.Diagnostic.Severity != CoercionSeverityError {
		t.Fatalf("unexpected empty request severity: %q", emptyErr.Diagnostic.Severity)
	}
}

func TestCoerceValueResultAndErrorDoNotExposeMutableReferences(t *testing.T) {
	value, err := NewScalarValue([]byte(`6.0`))
	if err != nil {
		t.Fatalf("NewScalarValue returned error: %v", err)
	}

	result, err := CoerceValue(&CoercionRequest{
		Rule:  CoercionRuleNumberToIntegerExact,
		Value: value,
	})
	if err != nil {
		t.Fatalf("CoerceValue returned error: %v", err)
	}

	resultRaw := result.Value.Raw()
	resultRaw[0] = '9'
	if got := string(result.Value.Raw()); got != `6` {
		t.Fatalf("result value was unexpectedly mutable: %q", got)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("expected one diagnostic, got %#v", result.Diagnostics)
	}
	wantDiagnostics := append([]CoercionDiagnostic(nil), result.Diagnostics...)
	result.Diagnostics[0].Message = "mutated"
	result.Diagnostics[0].Code = "mutated_code"
	result.Diagnostics[0].Input = ScalarValue{}
	result.Diagnostics[0].Output = ScalarValue{}
	result.Diagnostics = append(result.Diagnostics, CoercionDiagnostic{
		Code:     "fake_diagnostic",
		Severity: CoercionSeverityWarning,
		Message:  "fake",
	})

	repeatedResult, err := CoerceValue(&CoercionRequest{
		Rule:  CoercionRuleNumberToIntegerExact,
		Value: value,
	})
	if err != nil {
		t.Fatalf("second CoerceValue returned error: %v", err)
	}
	if len(repeatedResult.Diagnostics) != 1 {
		t.Fatalf("expected one diagnostic after mutation, got %#v", repeatedResult.Diagnostics)
	}
	if repeatedResult.Diagnostics[0].Code != CoercionCodeExactIntegerNormalized {
		t.Fatalf("unexpected diagnostic order/code after mutation: %#v", repeatedResult.Diagnostics)
	}
	if !reflect.DeepEqual(repeatedResult.Diagnostics, wantDiagnostics) {
		t.Fatalf("diagnostics changed after caller mutation\nwant: %#v\ngot:  %#v", wantDiagnostics, repeatedResult.Diagnostics)
	}

	invalidValue, err := NewScalarValue([]byte(`6.2`))
	if err != nil {
		t.Fatalf("NewScalarValue(invalid) returned error: %v", err)
	}
	errDetail := assertCoercionFailure(t, &CoercionRequest{
		Rule:  CoercionRuleNumberToIntegerExact,
		Value: invalidValue,
	})
	errorRaw := errDetail.Input.Raw()
	errorRaw[0] = '9'
	if got := string(errDetail.Input.Raw()); got != `6.2` {
		t.Fatalf("error input was unexpectedly mutable: %q", got)
	}
	diagnosticRaw := errDetail.Diagnostic.Input.Raw()
	diagnosticRaw[0] = '9'
	if got := string(errDetail.Diagnostic.Input.Raw()); got != `6.2` {
		t.Fatalf("error diagnostic input was unexpectedly mutable: %q", got)
	}
}

func TestCoerceValueDeterministicNormalizationDiagnostics(t *testing.T) {
	value, err := NewScalarValue([]byte(`6.0`))
	if err != nil {
		t.Fatalf("NewScalarValue returned error: %v", err)
	}
	request := &CoercionRequest{
		ParameterName:       "count",
		SemanticParameterID: "par.root.count",
		Rule:                CoercionRuleNumberToIntegerExact,
		Value:               value,
	}

	first, err := CoerceValue(request)
	if err != nil {
		t.Fatalf("first CoerceValue returned error: %v", err)
	}
	second, err := CoerceValue(request)
	if err != nil {
		t.Fatalf("second CoerceValue returned error: %v", err)
	}

	if got, want := string(first.Value.Raw()), string(second.Value.Raw()); got != want {
		t.Fatalf("expected deterministic values: first=%q second=%q", got, want)
	}
	if len(first.Diagnostics) != 1 || len(second.Diagnostics) != 1 {
		t.Fatalf("expected exactly one diagnostic in each result\nfirst: %#v\nsecond: %#v", first.Diagnostics, second.Diagnostics)
	}
	if first.Diagnostics[0].Code != CoercionCodeExactIntegerNormalized || second.Diagnostics[0].Code != CoercionCodeExactIntegerNormalized {
		t.Fatalf("unexpected diagnostic ordering\nfirst: %#v\nsecond: %#v", first.Diagnostics, second.Diagnostics)
	}
	if !reflect.DeepEqual(first.Diagnostics, second.Diagnostics) {
		t.Fatalf("expected deterministic diagnostics\nfirst:  %#v\nsecond: %#v", first.Diagnostics, second.Diagnostics)
	}
	if first.Diagnostics[0].Message != second.Diagnostics[0].Message {
		t.Fatalf("expected deterministic diagnostic messages\nfirst:  %q\nsecond: %q", first.Diagnostics[0].Message, second.Diagnostics[0].Message)
	}
}

func assertCoercionFailure(t *testing.T, request *CoercionRequest) *CoercionError {
	t.Helper()

	_, err := CoerceValue(request)
	if err == nil {
		t.Fatal("expected CoerceValue to fail")
	}
	if !errors.Is(err, ErrCoercion) {
		t.Fatalf("expected ErrCoercion, got %v", err)
	}

	var coercionErr *CoercionError
	if !errors.As(err, &coercionErr) {
		t.Fatalf("expected CoercionError, got %T", err)
	}
	return coercionErr
}

func TestValidateMinimalModel(t *testing.T) {
	model := minimalModel(t)

	if err := Validate(model); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

func TestValidateDuplicateIDsFailDeterministically(t *testing.T) {
	model := minimalModel(t)
	model.Components = append(model.Components, Component{ID: model.Components[0].ID})
	model.ParameterGroups = append(model.ParameterGroups,
		ParameterGroup{ID: "grp.a", OwnerComponentID: "cmp.root"},
		ParameterGroup{ID: "grp.a", OwnerComponentID: "cmp.root"},
	)
	model.Parameters = append(model.Parameters,
		Parameter{ID: "par.a", OwnerKind: OwnerKindComponent, OwnerID: "cmp.root", ComponentID: "cmp.root"},
		Parameter{ID: "par.a", OwnerKind: OwnerKindComponent, OwnerID: "cmp.root", ComponentID: "cmp.root"},
	)
	model.Metadata = append(model.Metadata,
		Metadata{ID: "meta.a", OwnerKind: OwnerKindComponent, OwnerID: "cmp.root", ComponentID: "cmp.root"},
		Metadata{ID: "meta.a", OwnerKind: OwnerKindComponent, OwnerID: "cmp.root", ComponentID: "cmp.root"},
	)

	err := Validate(model)
	if err == nil {
		t.Fatal("expected Validate to fail")
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ValidationError, got %T", err)
	}

	want := []string{
		`components[1].id duplicates stable ID "cmp.root" already declared at components[0].id`,
		`metadata[2].id duplicates stable ID "meta.a" already declared at metadata[1].id`,
		`parameterGroups[2].id duplicates stable ID "grp.a" already declared at parameterGroups[1].id`,
		`parameters[2].id duplicates stable ID "par.a" already declared at parameters[1].id`,
	}
	if !reflect.DeepEqual(validationErr.Messages(), want) {
		t.Fatalf("unexpected problems\nwant: %#v\ngot:  %#v", want, validationErr.Messages())
	}
}

func TestValidateMissingReferencesFailDeterministically(t *testing.T) {
	model := minimalModel(t)
	model.RootComponentID = "cmp.missing"
	model.Components = append(model.Components, Component{
		ID:          "cmp.child",
		ParentID:    "cmp.ghost",
		ChildrenIDs: []string{"cmp.nowhere"},
	})
	model.ParameterGroups = append(model.ParameterGroups, ParameterGroup{
		ID:               "grp.ghost",
		OwnerComponentID: "cmp.ghost",
	})
	model.Parameters = append(model.Parameters, Parameter{
		ID:          "par.ghost",
		OwnerKind:   OwnerKindGroup,
		OwnerID:     "grp.ghost.missing",
		ComponentID: "cmp.ghost",
		GroupID:     "grp.ghost.missing",
	})
	model.Metadata = append(model.Metadata, Metadata{
		ID:          "meta.ghost",
		OwnerKind:   OwnerKindComponent,
		OwnerID:     "cmp.ghost.owner",
		ComponentID: "cmp.ghost",
	})

	err := Validate(model)
	if err == nil {
		t.Fatal("expected Validate to fail")
	}

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ValidationError, got %T", err)
	}

	want := []string{
		`components[1].childrenIds[0] "cmp.nowhere" must resolve to an existing component`,
		`components[1].parentId "cmp.ghost" must resolve to an existing component`,
		`metadata[1].componentId "cmp.ghost" must resolve to an existing component`,
		`metadata[1].ownerId "cmp.ghost.owner" must resolve to an existing component`,
		`parameterGroups[1].ownerComponentId "cmp.ghost" must resolve to an existing component`,
		`parameters[1].componentId "cmp.ghost" must resolve to an existing component`,
		`parameters[1].groupId "grp.ghost.missing" must resolve to an existing parameter group`,
		`parameters[1].ownerId "grp.ghost.missing" must resolve to an existing parameter group`,
		`rootComponentId "cmp.missing" must resolve to an existing component`,
	}
	if !reflect.DeepEqual(validationErr.Messages(), want) {
		t.Fatalf("unexpected problems\nwant: %#v\ngot:  %#v", want, validationErr.Messages())
	}
}

func TestValidateRepeatedInvalidInputIsStable(t *testing.T) {
	model := minimalModel(t)
	model.SchemaVersion = ""
	model.RootComponentID = "cmp.missing"

	first := Validate(model)
	second := Validate(model)
	if first == nil || second == nil {
		t.Fatal("expected repeated validation failures")
	}
	if first.Error() != second.Error() {
		t.Fatalf("expected deterministic error text\nfirst:  %q\nsecond: %q", first.Error(), second.Error())
	}

	var firstValidationErr *ValidationError
	if !errors.As(first, &firstValidationErr) {
		t.Fatalf("expected first ValidationError, got %T", first)
	}
	var secondValidationErr *ValidationError
	if !errors.As(second, &secondValidationErr) {
		t.Fatalf("expected second ValidationError, got %T", second)
	}
	if !reflect.DeepEqual(firstValidationErr.Messages(), secondValidationErr.Messages()) {
		t.Fatalf("expected identical structured problems\nfirst:  %#v\nsecond: %#v", firstValidationErr.Messages(), secondValidationErr.Messages())
	}
}

func TestSemanticModel_DuplicateIDAcrossCollections_Fails(t *testing.T) {
	model := minimalModel(t)
	model.ParameterGroups = append(model.ParameterGroups, ParameterGroup{
		ID:               "cmp.root",
		OwnerComponentID: "cmp.root",
		Name:             "Shadow Group",
		DisplayName:      "Shadow Group",
		GroupKind:        "varset",
		NativeType:       "Spreadsheet::Sheet",
		Observable:       true,
		Writable:         true,
	})
	model.Parameters = append(model.Parameters, Parameter{
		ID:          "grp.root.main",
		OwnerKind:   OwnerKindComponent,
		OwnerID:     "cmp.root",
		ComponentID: "cmp.root",
		Name:        "ShadowParameter",
		ValueType:   "number",
		NativeType:  "App::PropertyFloat",
		Observable:  true,
		Writable:    true,
	})
	model.Metadata = append(model.Metadata, Metadata{
		ID:          "par.root.main.length",
		OwnerKind:   OwnerKindComponent,
		OwnerID:     "cmp.root",
		ComponentID: "cmp.root",
		Key:         "shadow_metadata",
		ValueType:   "string",
		NativeType:  "App::PropertyString",
		Observable:  true,
		Writable:    true,
	})

	first := Validate(model)
	second := Validate(model)
	if first == nil || second == nil {
		t.Fatal("expected repeated validation failures")
	}
	if first.Error() != second.Error() {
		t.Fatalf("expected deterministic error text\nfirst:  %q\nsecond: %q", first.Error(), second.Error())
	}

	var validationErr *ValidationError
	if !errors.As(first, &validationErr) {
		t.Fatalf("expected ValidationError, got %T", first)
	}

	want := []string{
		`metadata[1].id duplicates stable ID "par.root.main.length" already declared at parameters[0].id`,
		`parameterGroups[1].id duplicates stable ID "cmp.root" already declared at components[0].id`,
		`parameters[1].id duplicates stable ID "grp.root.main" already declared at parameterGroups[0].id`,
	}
	if !reflect.DeepEqual(validationErr.Messages(), want) {
		t.Fatalf("unexpected problems\nwant: %#v\ngot:  %#v", want, validationErr.Messages())
	}
}

func TestSemanticModel_RootComponentMissing_Fails(t *testing.T) {
	model := minimalModel(t)
	model.RootComponentID = "cmp.missing"

	err := Validate(model)
	if err == nil {
		t.Fatal("expected Validate to fail")
	}

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ValidationError, got %T", err)
	}

	want := []string{
		`rootComponentId "cmp.missing" must resolve to an existing component`,
	}
	if !reflect.DeepEqual(validationErr.Messages(), want) {
		t.Fatalf("unexpected problems\nwant: %#v\ngot:  %#v", want, validationErr.Messages())
	}
}

func TestSemanticModel_OrphanComponentParent_Fails(t *testing.T) {
	model := minimalModel(t)
	model.Components = append(model.Components, Component{
		ID:          "cmp.orphan",
		ParentID:    "cmp.missing.parent",
		Quantity:    1,
		ChildrenIDs: []string{},
	})

	first := Validate(model)
	second := Validate(model)
	if first == nil || second == nil {
		t.Fatal("expected repeated validation failures")
	}
	if first.Error() != second.Error() {
		t.Fatalf("expected deterministic error text\nfirst:  %q\nsecond: %q", first.Error(), second.Error())
	}

	var validationErr *ValidationError
	if !errors.As(first, &validationErr) {
		t.Fatalf("expected ValidationError, got %T", first)
	}

	want := []string{
		`components[1].parentId "cmp.missing.parent" must resolve to an existing component`,
	}
	if !reflect.DeepEqual(validationErr.Messages(), want) {
		t.Fatalf("unexpected problems\nwant: %#v\ngot:  %#v", want, validationErr.Messages())
	}
}

func TestCanonicalOrderingIsStableAcrossInputOrder(t *testing.T) {
	model := minimalModel(t)
	model.Components = []Component{
		{ID: "cmp.b", ChildrenIDs: []string{"cmp.c", "cmp.a"}},
		{ID: "cmp.a"},
		{ID: "cmp.c"},
	}
	model.RootComponentID = "cmp.a"
	model.ParameterGroups = []ParameterGroup{{ID: "grp.b"}, {ID: "grp.a"}}
	model.Parameters = []Parameter{{ID: "par.b"}, {ID: "par.a"}}
	model.Metadata = []Metadata{{ID: "meta.b"}, {ID: "meta.a"}}

	canonical, err := Canonicalize(model)
	if err != nil {
		t.Fatalf("Canonicalize returned error: %v", err)
	}

	if got := idsFromComponents(canonical.Components); !reflect.DeepEqual(got, []string{"cmp.a", "cmp.b", "cmp.c"}) {
		t.Fatalf("unexpected component order: %#v", got)
	}
	if got := canonical.Components[1].ChildrenIDs; !reflect.DeepEqual(got, []string{"cmp.a", "cmp.c"}) {
		t.Fatalf("unexpected child order: %#v", got)
	}
	if got := idsFromGroups(canonical.ParameterGroups); !reflect.DeepEqual(got, []string{"grp.a", "grp.b"}) {
		t.Fatalf("unexpected group order: %#v", got)
	}
	if got := idsFromParameters(canonical.Parameters); !reflect.DeepEqual(got, []string{"par.a", "par.b"}) {
		t.Fatalf("unexpected parameter order: %#v", got)
	}
	if got := idsFromMetadata(canonical.Metadata); !reflect.DeepEqual(got, []string{"meta.a", "meta.b"}) {
		t.Fatalf("unexpected metadata order: %#v", got)
	}
}

func TestCanonicalJSONIsByteStableAndInputOrderIndependent(t *testing.T) {
	left := minimalModel(t)
	left.Components = []Component{{ID: "cmp.b"}, {ID: "cmp.a"}}
	left.RootComponentID = "cmp.a"
	left.ParameterGroups = []ParameterGroup{{ID: "grp.b"}, {ID: "grp.a"}}
	left.Parameters = []Parameter{{ID: "par.b"}, {ID: "par.a"}}
	left.Metadata = []Metadata{{ID: "meta.b"}, {ID: "meta.a"}}

	right := minimalModel(t)
	right.Components = []Component{{ID: "cmp.a"}, {ID: "cmp.b"}}
	right.RootComponentID = "cmp.a"
	right.ParameterGroups = []ParameterGroup{{ID: "grp.a"}, {ID: "grp.b"}}
	right.Parameters = []Parameter{{ID: "par.a"}, {ID: "par.b"}}
	right.Metadata = []Metadata{{ID: "meta.a"}, {ID: "meta.b"}}

	first, err := CanonicalJSON(left)
	if err != nil {
		t.Fatalf("CanonicalJSON(first) returned error: %v", err)
	}
	second, err := CanonicalJSON(left)
	if err != nil {
		t.Fatalf("CanonicalJSON(second) returned error: %v", err)
	}
	third, err := CanonicalJSON(right)
	if err != nil {
		t.Fatalf("CanonicalJSON(third) returned error: %v", err)
	}

	if !bytes.Equal(first, second) {
		t.Fatalf("canonical JSON was not byte-stable\nfirst:  %s\nsecond: %s", first, second)
	}
	if !bytes.Equal(first, third) {
		t.Fatalf("canonical JSON depended on input ordering\nfirst:  %s\nthird: %s", first, third)
	}
}

func TestCanonicalAccessorsAndBuildFromCaptureDefendAgainstMutation(t *testing.T) {
	model := minimalModel(t)
	model.Components = []Component{{ID: "cmp.b"}, {ID: "cmp.a"}}
	model.RootComponentID = "cmp.a"

	components := model.CanonicalComponents()
	components[0].ID = "cmp.mutated"
	if got := idsFromComponents(model.CanonicalComponents()); !reflect.DeepEqual(got, []string{"cmp.a", "cmp.b"}) {
		t.Fatalf("expected canonical components accessor to return a defensive copy, got %#v", got)
	}

	contract := loadCaptureFixture(t, "smoke", "with-parameter-group")
	built, err := BuildFromCapture(contract)
	if err != nil {
		t.Fatalf("BuildFromCapture returned error: %v", err)
	}

	contract.Entities.Components[0].ID = "cmp.changed"
	contract.Structure.Nodes[0].Children = append(contract.Structure.Nodes[0].Children, "cmp.changed")
	contract.Entities.Parameters[0].CurrentValue = cad.ScalarValue{}

	if built.Components[0].ID != "cmp.root" {
		t.Fatalf("expected built model to retain original component ID, got %q", built.Components[0].ID)
	}
	if len(built.Components[0].ChildrenIDs) != 0 {
		t.Fatalf("expected built model children to stay unchanged, got %#v", built.Components[0].ChildrenIDs)
	}
	if built.Parameters[0].CurrentValue.IsZero() {
		t.Fatal("expected built parameter current value to remain set")
	}
}

func TestBuildFromCaptureMinimalFixture(t *testing.T) {
	contract := loadCaptureFixture(t, "smoke", "minimal-valid")

	model, err := BuildFromCapture(contract)
	if err != nil {
		t.Fatalf("BuildFromCapture returned error: %v", err)
	}
	if err := Validate(model); err != nil {
		t.Fatalf("Validate(model) returned error: %v", err)
	}
	if model.SourceDocumentLogicalID != "first_model" {
		t.Fatalf("unexpected source document logical ID: %q", model.SourceDocumentLogicalID)
	}
	if model.Adapter == nil || model.Adapter.Name != contract.Adapter.Name || model.Adapter.Version != contract.Adapter.Version {
		t.Fatalf("unexpected adapter identity: %#v", model.Adapter)
	}
	if model.CADSystem == nil || model.CADSystem.Name != contract.CADSystem.Name || model.CADSystem.Version != contract.CADSystem.Version {
		t.Fatalf("unexpected CAD system identity: %#v", model.CADSystem)
	}
	if model.RootComponentID != "cmp.root" {
		t.Fatalf("unexpected root component ID: %q", model.RootComponentID)
	}
	if len(model.Components) != 1 || model.Components[0].ID != "cmp.root" {
		t.Fatalf("unexpected components: %#v", model.Components)
	}
	if model.Components[0].Kind != contract.Entities.Components[0].Kind ||
		model.Components[0].Name != contract.Entities.Components[0].Name ||
		model.Components[0].DisplayName != contract.Entities.Components[0].DisplayName ||
		model.Components[0].Material != contract.Entities.Components[0].Material ||
		model.Components[0].Quantity != contract.Entities.Components[0].Quantity ||
		!reflect.DeepEqual(model.Components[0].Targetability, contract.Entities.Components[0].Targetability) {
		t.Fatalf("unexpected mapped component: %#v", model.Components[0])
	}
}

func TestBuildFromCaptureHierarchyFixture(t *testing.T) {
	contract := loadCaptureFixture(t, "smoke", "with-hierarchy")

	model, err := BuildFromCapture(contract)
	if err != nil {
		t.Fatalf("BuildFromCapture returned error: %v", err)
	}

	byID := make(map[string]Component, len(model.Components))
	for _, component := range model.Components {
		byID[component.ID] = component
	}

	if byID["cmp.root"].ParentID != "" {
		t.Fatalf("expected root parent to be empty, got %q", byID["cmp.root"].ParentID)
	}
	if !reflect.DeepEqual(byID["cmp.root"].ChildrenIDs, []string{"cmp.sub"}) {
		t.Fatalf("unexpected root children: %#v", byID["cmp.root"].ChildrenIDs)
	}
	if byID["cmp.sub"].ParentID != "cmp.root" {
		t.Fatalf("unexpected subassembly parent: %q", byID["cmp.sub"].ParentID)
	}
	if !reflect.DeepEqual(byID["cmp.sub"].ChildrenIDs, []string{"cmp.part.leaf"}) {
		t.Fatalf("unexpected subassembly children: %#v", byID["cmp.sub"].ChildrenIDs)
	}
	if byID["cmp.part.leaf"].ParentID != "cmp.sub" {
		t.Fatalf("unexpected leaf parent: %q", byID["cmp.part.leaf"].ParentID)
	}
}

func TestBuildFromCaptureParameterGroupFixture(t *testing.T) {
	contract := loadCaptureFixture(t, "smoke", "with-parameter-group")

	model, err := BuildFromCapture(contract)
	if err != nil {
		t.Fatalf("BuildFromCapture returned error: %v", err)
	}

	if len(model.ParameterGroups) != 1 {
		t.Fatalf("unexpected parameter groups: %#v", model.ParameterGroups)
	}
	if model.ParameterGroups[0].OwnerComponentID != "cmp.root" {
		t.Fatalf("unexpected group owner: %q", model.ParameterGroups[0].OwnerComponentID)
	}
	if len(model.Parameters) != 1 {
		t.Fatalf("unexpected parameters: %#v", model.Parameters)
	}
	if model.Parameters[0].GroupID != "grp.root.main" {
		t.Fatalf("unexpected parameter group linkage: %q", model.Parameters[0].GroupID)
	}
	if model.Parameters[0].OwnerID != "grp.root.main" {
		t.Fatalf("unexpected parameter owner: %q", model.Parameters[0].OwnerID)
	}
	if len(model.Metadata) != 1 || model.Metadata[0].OwnerID != "cmp.root" {
		t.Fatalf("unexpected metadata: %#v", model.Metadata)
	}
}

func TestBuildFromCaptureSmokeFixturesProduceValidModels(t *testing.T) {
	fixtures := []string{"minimal-valid", "with-hierarchy", "with-parameter-group"}

	for _, fixture := range fixtures {
		t.Run(fixture, func(t *testing.T) {
			contract := loadCaptureFixture(t, "smoke", fixture)

			model, err := BuildFromCapture(contract)
			if err != nil {
				t.Fatalf("BuildFromCapture returned error: %v", err)
			}
			if err := Validate(model); err != nil {
				t.Fatalf("Validate(model) returned error: %v", err)
			}
		})
	}
}

func TestBuildFromCaptureCanonicalOutputIsRepeatable(t *testing.T) {
	contract := loadCaptureFixture(t, "smoke", "with-hierarchy")

	firstModel, err := BuildFromCapture(contract)
	if err != nil {
		t.Fatalf("BuildFromCapture(first) returned error: %v", err)
	}
	secondModel, err := BuildFromCapture(contract)
	if err != nil {
		t.Fatalf("BuildFromCapture(second) returned error: %v", err)
	}

	firstJSON, err := CanonicalJSON(firstModel)
	if err != nil {
		t.Fatalf("CanonicalJSON(first) returned error: %v", err)
	}
	secondJSON, err := CanonicalJSON(secondModel)
	if err != nil {
		t.Fatalf("CanonicalJSON(second) returned error: %v", err)
	}
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatalf("expected repeated BuildFromCapture canonical output to match\nfirst:  %s\nsecond: %s", firstJSON, secondJSON)
	}
}

func TestBuildFromCapture_InputOrderIndependence(t *testing.T) {
	original := loadCaptureFixture(t, "smoke", "with-hierarchy")
	reordered := loadCaptureFixture(t, "smoke", "with-hierarchy")

	reordered.Entities.Components = []cad.Component{
		reordered.Entities.Components[1],
		reordered.Entities.Components[2],
		reordered.Entities.Components[0],
	}
	reordered.Structure.Nodes = []cad.StructureNode{
		reordered.Structure.Nodes[1],
		reordered.Structure.Nodes[0],
		reordered.Structure.Nodes[2],
	}

	firstModel, err := BuildFromCapture(original)
	if err != nil {
		t.Fatalf("BuildFromCapture(original) returned error: %v", err)
	}
	secondModel, err := BuildFromCapture(reordered)
	if err != nil {
		t.Fatalf("BuildFromCapture(reordered) returned error: %v", err)
	}

	firstJSON, err := CanonicalJSON(firstModel)
	if err != nil {
		t.Fatalf("CanonicalJSON(first) returned error: %v", err)
	}
	secondJSON, err := CanonicalJSON(secondModel)
	if err != nil {
		t.Fatalf("CanonicalJSON(second) returned error: %v", err)
	}

	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatalf("expected BuildFromCapture canonical output to ignore capture input order\nfirst:  %s\nsecond: %s", firstJSON, secondJSON)
	}
}

func TestBuildFromCapture_ParameterAndMetadataValuesAreDefensivelyCopied(t *testing.T) {
	contract := loadCaptureFixture(t, "smoke", "with-parameter-group")

	model, err := BuildFromCapture(contract)
	if err != nil {
		t.Fatalf("BuildFromCapture returned error: %v", err)
	}

	parameterValueBefore := model.Parameters[0].CurrentValue.Raw()
	metadataValueBefore := model.Metadata[0].CurrentValue.Raw()

	contract.Entities.Parameters[0].CurrentValue = cad.ScalarValue{}
	contract.Entities.Metadata[0].CurrentValue = cad.ScalarValue{}

	if !bytes.Equal(model.Parameters[0].CurrentValue.Raw(), parameterValueBefore) {
		t.Fatalf("expected parameter current value to be unchanged, got %q", model.Parameters[0].CurrentValue.Raw())
	}
	if !bytes.Equal(model.Metadata[0].CurrentValue.Raw(), metadataValueBefore) {
		t.Fatalf("expected metadata current value to be unchanged, got %q", model.Metadata[0].CurrentValue.Raw())
	}
}

func TestBuildFromCapture_ComponentChildrenAreDefensivelyCopied(t *testing.T) {
	contract := loadCaptureFixture(t, "smoke", "with-hierarchy")

	model, err := BuildFromCapture(contract)
	if err != nil {
		t.Fatalf("BuildFromCapture returned error: %v", err)
	}

	rootIndex := slices.IndexFunc(contract.Structure.Nodes, func(node cad.StructureNode) bool {
		return node.ComponentID == contract.Structure.RootComponentID
	})
	if rootIndex < 0 {
		t.Fatal("expected root structure node in fixture")
	}

	componentIndex := slices.IndexFunc(model.Components, func(component Component) bool {
		return component.ID == contract.Structure.RootComponentID
	})
	if componentIndex < 0 {
		t.Fatal("expected root component in transformed model")
	}
	gotChildren := append([]string(nil), model.Components[componentIndex].ChildrenIDs...)
	contract.Structure.Nodes[rootIndex].Children[0] = "cmp.mutated"

	if !reflect.DeepEqual(model.Components[componentIndex].ChildrenIDs, gotChildren) {
		t.Fatalf("expected built component children to be unchanged, got %#v", model.Components[componentIndex].ChildrenIDs)
	}
}

func TestBuildFromCapture_InvalidCaptureFailsWithoutPartialModel(t *testing.T) {
	contract := loadCaptureFixture(t, "smoke", "minimal-valid")
	contract.Structure.RootComponentID = "cmp.missing"

	model, err := BuildFromCapture(contract)
	if err == nil {
		t.Fatal("expected BuildFromCapture to fail")
	}
	if model != nil {
		t.Fatalf("expected no partial model, got %#v", model)
	}
	if !errors.Is(err, cad.ErrValidation) {
		t.Fatalf("expected cad validation error, got %v", err)
	}
}

func TestBuildFromCapture_CrossCollectionIDCollisionsFailDeterministically(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*cad.CADContract)
	}{
		{
			name: "component-vs-parameter",
			mutate: func(contract *cad.CADContract) {
				contract.Entities.Parameters[0].ID = contract.Entities.Components[0].ID
			},
		},
		{
			name: "component-vs-metadata",
			mutate: func(contract *cad.CADContract) {
				contract.Entities.Metadata[0].ID = contract.Entities.Components[0].ID
			},
		},
		{
			name: "parameter-vs-parameter-group",
			mutate: func(contract *cad.CADContract) {
				contract.Entities.Parameters[0].ID = contract.Entities.ParameterGroups[0].ID
			},
		},
		{
			name: "metadata-vs-parameter-group",
			mutate: func(contract *cad.CADContract) {
				contract.Entities.Metadata[0].ID = contract.Entities.ParameterGroups[0].ID
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			contract := loadCaptureFixture(t, "smoke", "with-parameter-group")
			test.mutate(contract)
			assertBuildFromCaptureFailureIsDeterministic(t, contract, cad.ErrValidation)
		})
	}
}

func TestBuildFromCapture_BrokenCaptureReferencesFailDeterministically(t *testing.T) {
	tests := []struct {
		name     string
		contract func(t *testing.T) *cad.CADContract
	}{
		{
			name: "component-missing-parent-reference",
			contract: func(t *testing.T) *cad.CADContract {
				contract := loadCaptureFixture(t, "smoke", "with-hierarchy")
				removeComponent(contract, "cmp.sub")
				return contract
			},
		},
		{
			name: "parameter-missing-component",
			contract: func(t *testing.T) *cad.CADContract {
				contract := loadCaptureFixture(t, "smoke", "with-parameter-group")
				contract.Entities.Components = append(contract.Entities.Components, cad.Component{
					ID:          "cmp.extra",
					Kind:        "part",
					Name:        "ExtraPart",
					DisplayName: "Extra Part",
					CADType:     "PartDesign::Body",
					Quantity:    1,
					Material:    "Steel",
					Targetability: cad.Targetability{
						Hide:   true,
						Unhide: true,
					},
					Annotations: cad.Annotations{},
				})
				contract.Structure.Nodes = append(contract.Structure.Nodes, cad.StructureNode{
					ComponentID:       "cmp.extra",
					ParentComponentID: "cmp.root",
					Children:          []string{},
				})
				contract.Structure.Nodes[0].Children = append(contract.Structure.Nodes[0].Children, "cmp.extra")
				contract.Entities.Parameters = append(contract.Entities.Parameters, cad.Parameter{
					ID:          "par.extra.length",
					OwnerKind:   "component",
					OwnerID:     "cmp.extra",
					ComponentID: "cmp.extra",
					Name:        "Length",
					DisplayName: "Length",
					CADType:     "App::PropertyLength",
					ValueType:   "number",
					Observable:  true,
					Writable:    true,
					Annotations: cad.Annotations{},
				})
				removeComponent(contract, "cmp.extra")
				return contract
			},
		},
		{
			name: "parameter-missing-group",
			contract: func(t *testing.T) *cad.CADContract {
				contract := loadCaptureFixture(t, "smoke", "with-parameter-group")
				contract.Entities.ParameterGroups = []cad.ParameterGroup{}
				return contract
			},
		},
		{
			name: "metadata-missing-owner",
			contract: func(t *testing.T) *cad.CADContract {
				contract := loadCaptureFixture(t, "smoke", "minimal-valid")
				contract.Entities.Features = append(contract.Entities.Features, cad.Feature{
					ID:          "feat.root.pad",
					ComponentID: "cmp.root",
					Name:        "Pad",
					DisplayName: "Pad",
					CADType:     "PartDesign::Pad",
					Targetability: cad.Targetability{
						Hide:   true,
						Unhide: true,
					},
					Annotations: cad.Annotations{},
				})
				value, err := cad.NewScalarValue([]byte(`"Pad-001"`))
				if err != nil {
					t.Fatalf("cad.NewScalarValue returned error: %v", err)
				}
				contract.Entities.Metadata = append(contract.Entities.Metadata, cad.Metadata{
					ID:           "meta.root.pad.code",
					OwnerKind:    "feature",
					OwnerID:      "feat.root.pad",
					ComponentID:  "cmp.root",
					Key:          "feature_code",
					DisplayName:  "Feature Code",
					CADType:      "App::PropertyString",
					ValueType:    "string",
					Observable:   true,
					Writable:     true,
					CurrentValue: value,
					Annotations:  cad.Annotations{},
				})
				contract.Entities.Features = []cad.Feature{}
				return contract
			},
		},
		{
			name: "group-missing-component",
			contract: func(t *testing.T) *cad.CADContract {
				contract := loadCaptureFixture(t, "smoke", "with-parameter-group")
				contract.Entities.Components = []cad.Component{}
				return contract
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			contract := test.contract(t)
			assertBuildFromCaptureFailureIsDeterministic(t, contract, cad.ErrValidation)
		})
	}
}

func TestBuildFromCapture_DoesNotIntroduceIntentPlannerOrManifestFields(t *testing.T) {
	contract := loadCaptureFixture(t, "smoke", "with-parameter-group")

	model, err := BuildFromCapture(contract)
	if err != nil {
		t.Fatalf("BuildFromCapture returned error: %v", err)
	}

	data, err := json.Marshal(model)
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}

	text := string(data)
	for _, forbidden := range []string{`"intent"`, `"planner"`, `"manifest"`, `"execution"`, `"projection"`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("unexpected field %s in transformed model JSON: %s", forbidden, text)
		}
	}
}

func TestBuildFromCapture_LargeCaptureDeterminismAndOrdering(t *testing.T) {
	contract := largeSyntheticCapture(t, 180)

	firstModel, err := BuildFromCapture(contract)
	if err != nil {
		t.Fatalf("BuildFromCapture(first) returned error: %v", err)
	}
	secondModel, err := BuildFromCapture(contract)
	if err != nil {
		t.Fatalf("BuildFromCapture(second) returned error: %v", err)
	}

	firstJSON, err := CanonicalJSON(firstModel)
	if err != nil {
		t.Fatalf("CanonicalJSON(first) returned error: %v", err)
	}
	secondJSON, err := CanonicalJSON(secondModel)
	if err != nil {
		t.Fatalf("CanonicalJSON(second) returned error: %v", err)
	}
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatalf("expected large capture canonical JSON to be byte-stable\nfirst:  %s\nsecond: %s", firstJSON, secondJSON)
	}

	shuffled := largeSyntheticCapture(t, 180)
	shuffleLargeCaptureDeterministically(shuffled)

	shuffledModel, err := BuildFromCapture(shuffled)
	if err != nil {
		t.Fatalf("BuildFromCapture(shuffled) returned error: %v", err)
	}
	shuffledJSON, err := CanonicalJSON(shuffledModel)
	if err != nil {
		t.Fatalf("CanonicalJSON(shuffled) returned error: %v", err)
	}
	if !bytes.Equal(firstJSON, shuffledJSON) {
		t.Fatalf("expected large capture canonical JSON to ignore input ordering\nfirst:    %s\nshuffled: %s", firstJSON, shuffledJSON)
	}
	if len(firstModel.Components) != 180 {
		t.Fatalf("unexpected component count: %d", len(firstModel.Components))
	}
	if len(firstModel.ParameterGroups) == 0 || len(firstModel.Parameters) == 0 || len(firstModel.Metadata) == 0 {
		t.Fatalf("expected large capture to include groups, parameters, and metadata: %+v", firstModel)
	}
}

func TestBuildFromCapture_CanonicalStabilityInvariants(t *testing.T) {
	left := loadCaptureFixture(t, "smoke", "with-parameter-group")
	right := loadCaptureFixture(t, "smoke", "with-parameter-group")

	left.Structure.Nodes[0].Children = nil
	right.Structure.Nodes[0].Children = []string{}

	leftModel, err := BuildFromCapture(left)
	if err != nil {
		t.Fatalf("BuildFromCapture(left) returned error: %v", err)
	}
	rightModel, err := BuildFromCapture(right)
	if err != nil {
		t.Fatalf("BuildFromCapture(right) returned error: %v", err)
	}

	first, err := CanonicalJSON(leftModel)
	if err != nil {
		t.Fatalf("CanonicalJSON(first) returned error: %v", err)
	}
	second, err := CanonicalJSON(leftModel)
	if err != nil {
		t.Fatalf("CanonicalJSON(second) returned error: %v", err)
	}
	third, err := CanonicalJSON(rightModel)
	if err != nil {
		t.Fatalf("CanonicalJSON(third) returned error: %v", err)
	}

	if !bytes.Equal(first, second) {
		t.Fatalf("expected repeated canonical JSON calls to be stable\nfirst:  %s\nsecond: %s", first, second)
	}
	if !bytes.Equal(first, third) {
		t.Fatalf("expected nil and empty capture collections to canonicalize identically\nfirst:  %s\nthird: %s", first, third)
	}
	if len(first) == 0 || first[len(first)-1] != '\n' {
		t.Fatalf("expected canonical JSON to be newline-terminated, got %q", first)
	}
}

func TestBuildFromCapture_NoMutationLeaks(t *testing.T) {
	contract := loadCaptureFixture(t, "smoke", "with-parameter-group")

	model, err := BuildFromCapture(contract)
	if err != nil {
		t.Fatalf("BuildFromCapture returned error: %v", err)
	}

	before, err := CanonicalJSON(model)
	if err != nil {
		t.Fatalf("CanonicalJSON(before) returned error: %v", err)
	}

	contract.Entities.Components[0].Name = "Changed Root"
	contract.Entities.Parameters[0].CurrentValue = cad.ScalarValue{}
	contract.Entities.Metadata[0].CurrentValue = cad.ScalarValue{}
	contract.Structure.Nodes[0].Children = append(contract.Structure.Nodes[0].Children, "cmp.injected")

	afterCaptureMutation, err := CanonicalJSON(model)
	if err != nil {
		t.Fatalf("CanonicalJSON(after capture mutation) returned error: %v", err)
	}
	if !bytes.Equal(before, afterCaptureMutation) {
		t.Fatalf("expected capture mutations to leave built model unchanged\nbefore: %s\nafter:  %s", before, afterCaptureMutation)
	}

	components := model.CanonicalComponents()
	parameters := model.CanonicalParameters()
	metadata := model.CanonicalMetadata()
	rawParameter := model.Parameters[0].CurrentValue.Raw()
	rawMetadata := model.Metadata[0].CurrentValue.Raw()

	components[0].ChildrenIDs = append(components[0].ChildrenIDs, "cmp.mutated")
	parameters[0].CurrentValue.raw = []byte(`0`)
	metadata[0].CurrentValue.raw = []byte(`"mutated"`)
	if len(rawParameter) > 0 {
		rawParameter[0] = '9'
	}
	if len(rawMetadata) > 0 {
		rawMetadata[0] = '"'
	}

	afterAccessorMutation, err := CanonicalJSON(model)
	if err != nil {
		t.Fatalf("CanonicalJSON(after accessor mutation) returned error: %v", err)
	}
	if !bytes.Equal(before, afterAccessorMutation) {
		t.Fatalf("expected accessor mutations to leave model unchanged\nbefore: %s\nafter:  %s", before, afterAccessorMutation)
	}
}

func TestCanonicalJSON_NewlineTerminated(t *testing.T) {
	model := minimalModel(t)

	data, err := CanonicalJSON(model)
	if err != nil {
		t.Fatalf("CanonicalJSON returned error: %v", err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatalf("expected canonical JSON to be newline-terminated, got %q", data)
	}
}

func TestCanonicalJSON_NilAndEmptyCollectionsStable(t *testing.T) {
	left := minimalModel(t)
	left.Components = nil
	left.ParameterGroups = nil
	left.Parameters = nil
	left.Metadata = nil

	right := minimalModel(t)
	right.Components = []Component{}
	right.ParameterGroups = []ParameterGroup{}
	right.Parameters = []Parameter{}
	right.Metadata = []Metadata{}

	left.RootComponentID = ""
	right.RootComponentID = ""
	left.Components = []Component{{ID: "cmp.root", ChildrenIDs: nil, Quantity: 1}}
	right.Components = []Component{{ID: "cmp.root", ChildrenIDs: []string{}, Quantity: 1}}
	left.RootComponentID = "cmp.root"
	right.RootComponentID = "cmp.root"
	left.ParameterGroups = nil
	left.Parameters = nil
	left.Metadata = nil
	right.ParameterGroups = []ParameterGroup{}
	right.Parameters = []Parameter{}
	right.Metadata = []Metadata{}

	first, err := CanonicalJSON(left)
	if err != nil {
		t.Fatalf("CanonicalJSON(left) returned error: %v", err)
	}
	second, err := CanonicalJSON(right)
	if err != nil {
		t.Fatalf("CanonicalJSON(right) returned error: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("expected nil and empty collections to canonicalize identically\nfirst:  %s\nsecond: %s", first, second)
	}
}

func TestInjectDSLIntent_SuccessfulInjectionFromCaptureBackedDSL(t *testing.T) {
	model := buildSemanticAuthoringFixture(t, "parameter-by-name")
	ast := parseSemanticFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["pdf", "step"]

    param length: number = 35
    param title: string = "demo"
}
`)

	injected, err := InjectDSLIntent(model, ast)
	if err != nil {
		t.Fatalf("InjectDSLIntent returned error: %v", err)
	}
	if err := Validate(injected); err != nil {
		t.Fatalf("Validate(injected) returned error: %v", err)
	}
	if len(injected.ProductIntents) != 1 {
		t.Fatalf("unexpected product intents: %#v", injected.ProductIntents)
	}

	intent := injected.ProductIntents[0]
	if intent.Name != "Box" {
		t.Fatalf("unexpected product intent name: %q", intent.Name)
	}
	if intent.Adapter != "freecad" {
		t.Fatalf("unexpected adapter: %q", intent.Adapter)
	}
	if intent.SourceModelLogicalID != "box_model" {
		t.Fatalf("unexpected source model logical ID: %q", intent.SourceModelLogicalID)
	}
	if !reflect.DeepEqual(intent.RequestedOutputFormats, []string{"pdf", "step"}) {
		t.Fatalf("unexpected requested output formats: %#v", intent.RequestedOutputFormats)
	}
	if !reflect.DeepEqual(intent.Outputs, []OutputIntent{
		{OutputType: "pdf"},
		{OutputType: "step", TargetEntityKind: "assembly", TargetSemanticID: "cmp.root", TargetNameSource: "identity_linkage"},
	}) {
		t.Fatalf("unexpected structured outputs: %#v", intent.Outputs)
	}
	if len(intent.ExportedParameters) != 1 {
		t.Fatalf("unexpected exported parameters: %#v", intent.ExportedParameters)
	}
	if intent.ExportedParameters[0].DSLParameterName != "length" {
		t.Fatalf("unexpected DSL parameter name: %q", intent.ExportedParameters[0].DSLParameterName)
	}
	if intent.ExportedParameters[0].SemanticParameterID != "par.root.length" {
		t.Fatalf("unexpected semantic parameter ID: %q", intent.ExportedParameters[0].SemanticParameterID)
	}
	if !reflect.DeepEqual(intent.Mutations, []MutationIntent{
		{
			OperationKind:    "set_parameter",
			TargetEntityKind: "parameter",
			TargetSemanticID: "par.root.length",
			TargetField:      "length",
			Scope:            "assembly",
			ValueSource: MutationValueSource{
				Kind:                "dsl_parameter",
				DSLParameterName:    "length",
				SemanticParameterID: "par.root.length",
			},
		},
	}) {
		t.Fatalf("unexpected mutations: %#v", intent.Mutations)
	}
}

func TestInjectDSLIntent_NumericParameterLinksToSemanticStableID(t *testing.T) {
	model := buildSemanticAuthoringFixture(t, "parameter-by-name")
	ast := parseSemanticFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param length: number = 35
}
`)

	injected, err := InjectDSLIntent(model, ast)
	if err != nil {
		t.Fatalf("InjectDSLIntent returned error: %v", err)
	}

	got := injected.ProductIntents[0].ExportedParameters[0]
	want := ExportedParameterIntent{
		DSLParameterName:    "length",
		SemanticParameterID: "par.root.length",
		Type:                "number",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected exported parameter intent\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestInjectDSLIntent_UnknownParameterFailsDeterministically(t *testing.T) {
	model := buildSemanticAuthoringFixture(t, "parameter-by-name")
	ast := parseSemanticFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param width: number = 35
}
`)

	err := assertInjectDSLIntentFailureIsStable(t, model, ast)
	var intentErr *DSLIntentError
	if !errors.As(err, &intentErr) {
		t.Fatalf("expected DSLIntentError, got %T", err)
	}

	wantDiagnostics := []DSLIntentDiagnostic{{
		Category:      DSLIntentDiagnosticCategoryInvalidDSLReference,
		Code:          DSLIntentDiagnosticCodeSemanticParameterReferenceNotFound,
		Message:       `product "Box" parameter "width" does not resolve to a semantic parameter by exact name`,
		ProductName:   "Box",
		ParameterName: "width",
	}}
	if !reflect.DeepEqual(intentErr.Diagnostics(), wantDiagnostics) {
		t.Fatalf("unexpected diagnostics\nwant: %#v\ngot:  %#v", wantDiagnostics, intentErr.Diagnostics())
	}
}

func TestInjectDSLIntent_AmbiguousParameterFailsWithSortedIDs(t *testing.T) {
	model := buildSemanticAuthoringFixture(t, "duplicate-parameter-name")
	ast := parseSemanticFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param length: number = 35
}
`)

	err := assertInjectDSLIntentFailureIsStable(t, model, ast)
	var intentErr *DSLIntentError
	if !errors.As(err, &intentErr) {
		t.Fatalf("expected DSLIntentError, got %T", err)
	}

	wantDiagnostics := []DSLIntentDiagnostic{{
		Category:             DSLIntentDiagnosticCategoryAmbiguousDSLReference,
		Code:                 DSLIntentDiagnosticCodeSemanticParameterReferenceAmbiguous,
		Message:              `product "Box" parameter "length" is ambiguous across semantic parameters ["par.root.length.a" "par.root.length.b"]`,
		ProductName:          "Box",
		ParameterName:        "length",
		MatchingParameterIDs: []string{"par.root.length.a", "par.root.length.b"},
	}}
	if !reflect.DeepEqual(intentErr.Diagnostics(), wantDiagnostics) {
		t.Fatalf("unexpected diagnostics\nwant: %#v\ngot:  %#v", wantDiagnostics, intentErr.Diagnostics())
	}
}

func TestInjectDSLIntent_CaseMismatchAndDisplayNameFallbackFail(t *testing.T) {
	t.Run("case-sensitive", func(t *testing.T) {
		model := buildSemanticAuthoringFixture(t, "parameter-by-name")
		ast := parseSemanticFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param Length: number = 35
}
`)

		err := assertInjectDSLIntentFailureIsStable(t, model, ast)
		if !strings.Contains(err.Error(), `product "Box" parameter "Length" does not resolve to a semantic parameter by exact name`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("display-name-not-fallback", func(t *testing.T) {
		model := buildSemanticAuthoringFixture(t, "display-name-only")
		ast := parseSemanticFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param length: number = 35
}
`)

		err := assertInjectDSLIntentFailureIsStable(t, model, ast)
		if !strings.Contains(err.Error(), `product "Box" parameter "length" does not resolve to a semantic parameter by exact name`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestInjectDSLIntent_SourceModelMismatchFailsDeterministically(t *testing.T) {
	model := buildSemanticAuthoringFixture(t, "parameter-by-name")
	ast := parseSemanticFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "other_model"
    outputs = ["step"]

    param length: number = 35
}
`)

	err := assertInjectDSLIntentFailureIsStable(t, model, ast)
	var intentErr *DSLIntentError
	if !errors.As(err, &intentErr) {
		t.Fatalf("expected DSLIntentError, got %T", err)
	}

	wantDiagnostics := []DSLIntentDiagnostic{{
		Category:    DSLIntentDiagnosticCategorySourceModelReferenceMismatch,
		Code:        DSLIntentDiagnosticCodeSourceModelLogicalIDMismatch,
		Message:     `product "Box" source_model "other_model" does not match semantic sourceDocumentLogicalId "box_model"`,
		ProductName: "Box",
	}}
	if !reflect.DeepEqual(intentErr.Diagnostics(), wantDiagnostics) {
		t.Fatalf("unexpected diagnostics\nwant: %#v\ngot:  %#v", wantDiagnostics, intentErr.Diagnostics())
	}
}

func TestInjectDSLIntent_RepeatedValidInjectionIsByteStableAndModelOrderIndependent(t *testing.T) {
	left := buildSemanticAuthoringFixture(t, "parameter-by-name")
	right := Clone(left)
	right.Parameters = []Parameter{right.Parameters[0]}
	right.Components = []Component{right.Components[0]}

	ast := parseSemanticFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step", "pdf"]

    param length: number = 35
}
`)

	firstModel, err := InjectDSLIntent(left, ast)
	if err != nil {
		t.Fatalf("InjectDSLIntent(first) returned error: %v", err)
	}
	secondModel, err := InjectDSLIntent(left, ast)
	if err != nil {
		t.Fatalf("InjectDSLIntent(second) returned error: %v", err)
	}
	thirdModel, err := InjectDSLIntent(right, ast)
	if err != nil {
		t.Fatalf("InjectDSLIntent(third) returned error: %v", err)
	}

	firstJSON, err := CanonicalJSON(firstModel)
	if err != nil {
		t.Fatalf("CanonicalJSON(first) returned error: %v", err)
	}
	secondJSON, err := CanonicalJSON(secondModel)
	if err != nil {
		t.Fatalf("CanonicalJSON(second) returned error: %v", err)
	}
	thirdJSON, err := CanonicalJSON(thirdModel)
	if err != nil {
		t.Fatalf("CanonicalJSON(third) returned error: %v", err)
	}

	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatalf("expected repeated injection output to be byte-stable\nfirst:  %s\nsecond: %s", firstJSON, secondJSON)
	}
	if !bytes.Equal(firstJSON, thirdJSON) {
		t.Fatalf("expected canonical JSON to ignore input model ordering\nfirst:  %s\nthird: %s", firstJSON, thirdJSON)
	}
}

func TestInjectDSLIntent_DoesNotMutateInputModelAndAccessorsRemainDefensive(t *testing.T) {
	model := buildSemanticAuthoringFixture(t, "parameter-by-name")
	before, err := CanonicalJSON(model)
	if err != nil {
		t.Fatalf("CanonicalJSON(before) returned error: %v", err)
	}

	ast := parseSemanticFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step", "pdf"]

    param length: number = 35
}
`)

	injected, err := InjectDSLIntent(model, ast)
	if err != nil {
		t.Fatalf("InjectDSLIntent returned error: %v", err)
	}

	after, err := CanonicalJSON(model)
	if err != nil {
		t.Fatalf("CanonicalJSON(after) returned error: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("expected input model to remain unchanged\nbefore: %s\nafter:  %s", before, after)
	}
	if len(model.ProductIntents) != 0 {
		t.Fatalf("expected original model to have no injected intents, got %#v", model.ProductIntents)
	}

	intents := injected.CanonicalProductIntents()
	intents[0].RequestedOutputFormats[0] = "mutated"
	intents[0].ExportedParameters[0].SemanticParameterID = "par.mutated"
	if injected.ProductIntents[0].RequestedOutputFormats[0] != "pdf" {
		t.Fatalf("expected canonical accessor to return a defensive copy, got %#v", injected.ProductIntents[0].RequestedOutputFormats)
	}
	if injected.ProductIntents[0].ExportedParameters[0].SemanticParameterID != "par.root.length" {
		t.Fatalf("expected exported parameter intent to remain unchanged, got %#v", injected.ProductIntents[0].ExportedParameters)
	}

	clone := Clone(injected)
	clone.ProductIntents[0].RequestedOutputFormats[0] = "changed"
	if injected.ProductIntents[0].RequestedOutputFormats[0] != "pdf" {
		t.Fatalf("expected Clone to defensively copy product intents, got %#v", injected.ProductIntents[0].RequestedOutputFormats)
	}
}

// TestInjectDSLIntent_ExplicitNoneOutputYieldsEmptyStructuredOutputs proves
// the canonical semantic representation for authoring-only outputs=["none"]:
// RequestedOutputFormats is exactly ["none"], Outputs is empty (never an
// OutputIntent with OutputType "none"), and parameter/mutation intent
// (source model identity, exported parameters, mutations) survives
// unaffected by the absence of derived outputs.
func TestInjectDSLIntent_ExplicitNoneOutputYieldsEmptyStructuredOutputs(t *testing.T) {
	model := buildSemanticAuthoringFixture(t, "parameter-by-name")
	ast := parseSemanticFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["none"]

    param length: number = 35
    param title: string = "demo"
}
`)

	injected, err := InjectDSLIntent(model, ast)
	if err != nil {
		t.Fatalf("InjectDSLIntent returned error: %v", err)
	}
	if err := Validate(injected); err != nil {
		t.Fatalf("Validate(injected) returned error: %v", err)
	}
	if len(injected.ProductIntents) != 1 {
		t.Fatalf("unexpected product intents: %#v", injected.ProductIntents)
	}

	intent := injected.ProductIntents[0]
	if intent.SourceModelLogicalID != "box_model" {
		t.Fatalf("unexpected source model logical ID: %q", intent.SourceModelLogicalID)
	}
	if !reflect.DeepEqual(intent.RequestedOutputFormats, []string{"none"}) {
		t.Fatalf("unexpected requested output formats: %#v", intent.RequestedOutputFormats)
	}
	if len(intent.Outputs) != 0 {
		t.Fatalf("expected zero structured outputs for explicit none, got %#v", intent.Outputs)
	}
	for _, output := range intent.Outputs {
		if output.OutputType == "none" {
			t.Fatalf("\"none\" must never appear as an OutputIntent.OutputType, got %#v", output)
		}
	}
	if len(intent.ExportedParameters) != 1 || intent.ExportedParameters[0].DSLParameterName != "length" {
		t.Fatalf("expected parameter intent to survive native-only output request, got %#v", intent.ExportedParameters)
	}
	if len(intent.Mutations) != 1 {
		t.Fatalf("expected mutation intent to survive native-only output request, got %#v", intent.Mutations)
	}
}

// TestInjectDSLIntent_OmittedOutputsDifferFromExplicitNoneOutputs proves the
// critical assertion that omitted outputs and explicit outputs=["none"] are
// distinct at the semantic representation level: omitted outputs leave
// RequestedOutputFormats empty (not the single-element ["none"] list), so a
// future omitted-output planning path can never be confused with a native
// -only request.
func TestInjectDSLIntent_OmittedOutputsDifferFromExplicitNoneOutputs(t *testing.T) {
	model := buildSemanticAuthoringFixture(t, "parameter-by-name")

	omittedAST := parseSemanticFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "box_model"

    param length: number = 35
}
`)
	omittedInjected, err := InjectDSLIntent(model, omittedAST)
	if err != nil {
		t.Fatalf("InjectDSLIntent(omitted) returned error: %v", err)
	}

	explicitNoneAST := parseSemanticFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["none"]

    param length: number = 35
}
`)
	explicitNoneInjected, err := InjectDSLIntent(model, explicitNoneAST)
	if err != nil {
		t.Fatalf("InjectDSLIntent(explicit none) returned error: %v", err)
	}

	omittedFormats := omittedInjected.ProductIntents[0].RequestedOutputFormats
	explicitFormats := explicitNoneInjected.ProductIntents[0].RequestedOutputFormats
	if len(omittedFormats) != 0 {
		t.Fatalf("expected omitted outputs to produce empty RequestedOutputFormats, got %#v", omittedFormats)
	}
	if !reflect.DeepEqual(explicitFormats, []string{"none"}) {
		t.Fatalf("expected explicit none to produce RequestedOutputFormats==[\"none\"], got %#v", explicitFormats)
	}
	if reflect.DeepEqual(omittedFormats, explicitFormats) {
		t.Fatalf("omitted and explicit-none RequestedOutputFormats must not collapse to the same representation")
	}

	// Both requests structurally have zero OutputIntent entries, but that
	// alone must not make them the same request: RequestedOutputFormats is
	// the field that preserves the distinction end to end.
	if len(omittedInjected.ProductIntents[0].Outputs) != 0 || len(explicitNoneInjected.ProductIntents[0].Outputs) != 0 {
		t.Fatalf("expected both omitted and explicit-none intents to have zero structured outputs")
	}
}

// TestInjectDSLIntent_ExplicitNoneCanonicalizationAndDefensiveCopy proves
// that canonicalization/cloning preserves the explicit-none requested output
// format (rather than collapsing it to the empty-array representation used
// for omission), and that accessors return defensive copies.
func TestInjectDSLIntent_ExplicitNoneCanonicalizationAndDefensiveCopy(t *testing.T) {
	model := buildSemanticAuthoringFixture(t, "parameter-by-name")
	ast := parseSemanticFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["none"]

    param length: number = 35
}
`)

	injected, err := InjectDSLIntent(model, ast)
	if err != nil {
		t.Fatalf("InjectDSLIntent returned error: %v", err)
	}

	canonical, err := Canonicalize(injected)
	if err != nil {
		t.Fatalf("Canonicalize returned error: %v", err)
	}
	if !reflect.DeepEqual(canonical.ProductIntents[0].RequestedOutputFormats, []string{"none"}) {
		t.Fatalf("expected canonicalization to preserve explicit none, got %#v", canonical.ProductIntents[0].RequestedOutputFormats)
	}

	data, err := CanonicalJSON(injected)
	if err != nil {
		t.Fatalf("CanonicalJSON returned error: %v", err)
	}
	if !strings.Contains(string(data), `"requestedOutputFormats":["none"]`) {
		t.Fatalf("expected canonical JSON to serialize requestedOutputFormats as [\"none\"], got %s", data)
	}
	if !strings.Contains(string(data), `"outputs":[]`) {
		t.Fatalf("expected canonical JSON to serialize outputs as empty array, got %s", data)
	}

	accessorCopy := injected.CanonicalProductIntents()
	accessorCopy[0].RequestedOutputFormats[0] = "mutated"
	if injected.ProductIntents[0].RequestedOutputFormats[0] != "none" {
		t.Fatalf("expected CanonicalProductIntents to return a defensive copy, got %#v", injected.ProductIntents[0].RequestedOutputFormats)
	}

	cloned := Clone(injected)
	cloned.ProductIntents[0].RequestedOutputFormats[0] = "changed"
	if injected.ProductIntents[0].RequestedOutputFormats[0] != "none" {
		t.Fatalf("expected Clone to defensively copy RequestedOutputFormats, got %#v", injected.ProductIntents[0].RequestedOutputFormats)
	}
}

func TestInjectDSLIntent_NoPlannerOrManifestFieldsIntroduced(t *testing.T) {
	model := buildSemanticAuthoringFixture(t, "parameter-by-name")
	ast := parseSemanticFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step", "pdf"]

    param length: number = 35
}

product NoCAD {
    adapter = "none"
    outputs = []

    param label: string = "demo"
}
`)

	injected, err := InjectDSLIntent(model, ast)
	if err != nil {
		t.Fatalf("InjectDSLIntent returned error: %v", err)
	}

	data, err := json.Marshal(injected)
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}

	text := string(data)
	for _, forbidden := range []string{`"planner"`, `"manifest"`, `"execution"`, `"parameterAssignments"`, `"varSet"`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("unexpected field %s in injected semantic model JSON: %s", forbidden, text)
		}
	}
}

func TestInjectDSLIntent_BuildsStructuredOutputsAndOrdinaryMutationKinds(t *testing.T) {
	model := modelWithFeatureMutationFixtures(t)
	ast := parseSemanticFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["pdf", "step", "csv"]

    param length: number = 35
    param part_number: string = "PN-123"
}
`)

	injected, err := InjectDSLIntent(model, ast)
	if err != nil {
		t.Fatalf("InjectDSLIntent returned error: %v", err)
	}

	intent := injected.ProductIntents[0]
	if !reflect.DeepEqual(intent.Outputs, []OutputIntent{
		{OutputType: "csv", TargetEntityKind: "assembly", TargetSemanticID: "cmp.root", TargetNameSource: "identity_linkage", Scope: "assembly"},
		{OutputType: "pdf"},
		{OutputType: "step", TargetEntityKind: "assembly", TargetSemanticID: "cmp.root", TargetNameSource: "identity_linkage"},
	}) {
		t.Fatalf("unexpected structured outputs: %#v", intent.Outputs)
	}

	wantMutations := []MutationIntent{
		{
			OperationKind:    "set_parameter",
			TargetEntityKind: "parameter",
			TargetSemanticID: "par.root.length",
			TargetField:      "length",
			Scope:            "assembly",
			ValueSource: MutationValueSource{
				Kind:                "dsl_parameter",
				DSLParameterName:    "length",
				SemanticParameterID: "par.root.length",
			},
		},
		{
			OperationKind:    "set_property",
			TargetEntityKind: "metadata",
			TargetSemanticID: "meta.root.part_number",
			TargetField:      "part_number",
			Scope:            "assembly",
			ValueSource: MutationValueSource{
				Kind:             "dsl_parameter",
				DSLParameterName: "part_number",
			},
		},
	}
	if !reflect.DeepEqual(intent.Mutations, wantMutations) {
		t.Fatalf("unexpected structured mutations\nwant: %#v\ngot:  %#v", wantMutations, intent.Mutations)
	}
}

// TestInjectDSLIntent_UnmappedPrefixLikeParameterProducesNoMutationOrDiagnostic
// proves the retired suppress_/unsuppress_ authoring bridge no longer performs
// semantic target lookups: a boolean parameter named as if it targeted a
// missing feature is now an ordinary, unmapped parameter name and is silently
// dropped rather than failing with the old missing-target diagnostic.
func TestInjectDSLIntent_UnmappedPrefixLikeParameterProducesNoMutationOrDiagnostic(t *testing.T) {
	model := modelWithFeatureMutationFixtures(t)
	ast := parseSemanticFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param suppress_MissingFeature: boolean = true
}
`)

	assertInjectDSLIntentProducesNoMutations(t, model, ast)
}

// TestInjectDSLIntent_PrefixLikeParameterIgnoresFeatureComponentNameCollision
// proves that even when the semantic model contains both a Feature and a
// Component sharing the exact name referenced by a suppress_-prefixed
// parameter, no semantic target resolution occurs and no ambiguity
// diagnostic is raised: the retired bridge never looks at the name at all.
func TestInjectDSLIntent_PrefixLikeParameterIgnoresFeatureComponentNameCollision(t *testing.T) {
	model := modelWithFeatureAndComponentSharingName(t)
	ast := parseSemanticFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param suppress_Keyway: boolean = true
}
`)

	assertInjectDSLIntentProducesNoMutations(t, model, ast)
}

// TestInjectDSLIntent_PrefixLikeParametersNeverTriggerTargetResolution proves
// the stronger Task 1 invariant behind the old "fallbacks remain disabled"
// coverage: there is no target lookup to fall back from in the first place,
// so neither case mismatches nor display-name-only matches ever resolve a
// suppress_/unsuppress_-prefixed parameter to a semantic target.
func TestInjectDSLIntent_PrefixLikeParametersNeverTriggerTargetResolution(t *testing.T) {
	model := modelWithFeatureMutationFixtures(t)

	t.Run("case-mismatch", func(t *testing.T) {
		caseMismatch := parseSemanticFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param suppress_keyway: boolean = true
}
`)
		assertInjectDSLIntentProducesNoMutations(t, model, caseMismatch)
	})

	t.Run("display-name-only", func(t *testing.T) {
		displayNameOnly := Clone(model)
		displayNameOnly.Features[0].Name = "Slot01"
		displayNameOnly.Features[0].DisplayName = "Keyway"
		displayAST := parseSemanticFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param suppress_Keyway: boolean = true
}
`)
		assertInjectDSLIntentProducesNoMutations(t, displayNameOnly, displayAST)
	})
}

// TestInjectDSLIntent_LegacySuppressionPrefixesAreOrdinaryParameters is the
// dedicated Task 1 permanent retirement regression: suppress_<name> and
// unsuppress_<name> parameter names have no special authoring semantics,
// even when a real feature named "Pad" exists and even though both
// parameters are boolean (the old bridge's gating type). This test must fail
// if the legacy prefix bridge is ever reintroduced.
func TestInjectDSLIntent_LegacySuppressionPrefixesAreOrdinaryParameters(t *testing.T) {
	model := modelWithFeatureMutationFixtures(t)
	model.Features = append(model.Features, Feature{
		ID:          "feat.root.pad",
		ComponentID: "cmp.root",
		Name:        "Pad",
		DisplayName: "Pad",
		NativeType:  "PartDesign::Pad",
	})

	ast := parseSemanticFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param suppress_Pad: boolean = true
    param unsuppress_Pad: boolean = false
}
`)

	injected, err := InjectDSLIntent(model, ast)
	if err != nil {
		t.Fatalf("InjectDSLIntent returned error: %v", err)
	}

	mutations := injected.ProductIntents[0].Mutations
	if len(mutations) != 0 {
		t.Fatalf("expected legacy suppress_/unsuppress_ prefixed boolean parameters to produce no mutations, got %#v", mutations)
	}
	for _, mutation := range mutations {
		if mutation.OperationKind == "suppress" || mutation.OperationKind == "unsuppress" {
			t.Fatalf("expected no suppress/unsuppress mutation kinds from legacy prefix names, got %#v", mutation)
		}
	}
}

// TestInjectDSLIntent_LegacySuppressionPrefixNameMapsAsOrdinaryProperty
// proves suppress_/unsuppress_-prefixed names are not reserved: when a
// literal metadata key happens to match one exactly, it is mapped as an
// ordinary property mutation (OperationKind "set_property"), never as a
// suppress/unsuppress operation.
func TestInjectDSLIntent_LegacySuppressionPrefixNameMapsAsOrdinaryProperty(t *testing.T) {
	model := modelWithFeatureMutationFixtures(t)
	literalValue, err := NewScalarValue([]byte(`"literal"`))
	if err != nil {
		t.Fatalf("NewScalarValue returned error: %v", err)
	}
	model.Metadata = append(model.Metadata, Metadata{
		ID:           "meta.root.suppress_pad",
		OwnerKind:    OwnerKindComponent,
		OwnerID:      "cmp.root",
		ComponentID:  "cmp.root",
		Key:          "suppress_Pad",
		DisplayName:  "Suppress Pad Literal",
		ValueType:    "string",
		NativeType:   "App::PropertyString",
		Observable:   true,
		Writable:     true,
		CurrentValue: literalValue,
	})

	ast := parseSemanticFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param suppress_Pad: string = "literal"
}
`)

	injected, err := InjectDSLIntent(model, ast)
	if err != nil {
		t.Fatalf("InjectDSLIntent returned error: %v", err)
	}

	mutations := injected.ProductIntents[0].Mutations
	if len(mutations) != 1 {
		t.Fatalf("expected exactly one ordinary property mutation, got %#v", mutations)
	}
	mutation := mutations[0]
	if mutation.OperationKind != "set_property" {
		t.Fatalf("expected literal suppress_Pad metadata key to map as an ordinary property mutation, got OperationKind=%q", mutation.OperationKind)
	}
	if mutation.TargetSemanticID != "meta.root.suppress_pad" {
		t.Fatalf("expected mutation to target the literal suppress_Pad metadata entry, got %#v", mutation)
	}
}

func TestInjectDSLIntent_StructuredIntentOrderingIsDeterministic(t *testing.T) {
	model := modelWithFeatureMutationFixtures(t)
	ast := parseSemanticFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step", "pdf", "csv"]

    param part_number: string = "PN-123"
    param length: number = 35
}
`)

	first, err := InjectDSLIntent(model, ast)
	if err != nil {
		t.Fatalf("InjectDSLIntent(first) returned error: %v", err)
	}
	second, err := InjectDSLIntent(model, ast)
	if err != nil {
		t.Fatalf("InjectDSLIntent(second) returned error: %v", err)
	}

	firstJSON, err := CanonicalJSON(first)
	if err != nil {
		t.Fatalf("CanonicalJSON(first) returned error: %v", err)
	}
	secondJSON, err := CanonicalJSON(second)
	if err != nil {
		t.Fatalf("CanonicalJSON(second) returned error: %v", err)
	}
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatalf("expected repeated structured intent injection to be byte-stable\nfirst:  %s\nsecond: %s", firstJSON, secondJSON)
	}
}

func minimalModel(t *testing.T) *Model {
	t.Helper()
	value, err := NewScalarValue([]byte(`12.5`))
	if err != nil {
		t.Fatalf("NewScalarValue returned error: %v", err)
	}
	metadataValue, err := NewScalarValue([]byte(`"PN-001"`))
	if err != nil {
		t.Fatalf("NewScalarValue returned error: %v", err)
	}
	return &Model{
		SchemaVersion:           SchemaVersion,
		SourceDocumentLogicalID: "box_model",
		Adapter:                 &SystemIdentity{Name: "freecad"},
		CADSystem:               &SystemIdentity{Name: "FreeCAD"},
		RootComponentID:         "cmp.root",
		Components: []Component{{
			ID:          "cmp.root",
			Kind:        "assembly",
			Name:        "RootAssembly",
			DisplayName: "Root Assembly",
			ChildrenIDs: []string{},
			Quantity:    1,
		}},
		ParameterGroups: []ParameterGroup{{
			ID:               "grp.root.main",
			OwnerComponentID: "cmp.root",
			Name:             "Main",
			DisplayName:      "Main",
			GroupKind:        "varset",
			NativeType:       "Spreadsheet::Sheet",
			Observable:       true,
			Writable:         true,
		}},
		Parameters: []Parameter{{
			ID:           "par.root.main.length",
			OwnerKind:    OwnerKindGroup,
			OwnerID:      "grp.root.main",
			ComponentID:  "cmp.root",
			GroupID:      "grp.root.main",
			Name:         "D1",
			DisplayName:  "Length",
			ValueType:    "number",
			NativeType:   "App::PropertyLength",
			Unit:         "mm",
			Observable:   true,
			Writable:     true,
			CurrentValue: value,
		}},
		Metadata: []Metadata{{
			ID:           "meta.root.part_number",
			OwnerKind:    OwnerKindComponent,
			OwnerID:      "cmp.root",
			ComponentID:  "cmp.root",
			Key:          "part_number",
			DisplayName:  "Part Number",
			ValueType:    "string",
			NativeType:   "App::PropertyString",
			Observable:   true,
			Writable:     true,
			CurrentValue: metadataValue,
		}},
	}
}

func buildSemanticAuthoringFixture(t *testing.T, name string) *Model {
	t.Helper()

	path := filepath.Join("..", "cad", "testdata", "authoring", name, "parametron.cad.json")
	contract, err := cad.Load(path)
	if err != nil {
		t.Fatalf("cad.Load(%q) returned error: %v", path, err)
	}
	model, err := BuildFromCapture(contract)
	if err != nil {
		t.Fatalf("BuildFromCapture returned error: %v", err)
	}
	return model
}

func modelWithFeatureMutationFixtures(t *testing.T) *Model {
	t.Helper()

	value, err := NewScalarValue([]byte(`35`))
	if err != nil {
		t.Fatalf("NewScalarValue(length) returned error: %v", err)
	}
	partNumber, err := NewScalarValue([]byte(`"PN-123"`))
	if err != nil {
		t.Fatalf("NewScalarValue(part_number) returned error: %v", err)
	}

	return &Model{
		SchemaVersion:           SchemaVersion,
		SourceDocumentLogicalID: "box_model",
		Adapter:                 &SystemIdentity{Name: "freecad"},
		CADSystem:               &SystemIdentity{Name: "FreeCAD"},
		RootComponentID:         "cmp.root",
		Components: []Component{
			{
				ID:          "cmp.root",
				Kind:        "assembly",
				Name:        "Assembly",
				DisplayName: "Assembly",
				ChildrenIDs: []string{},
				Quantity:    1,
			},
		},
		Features: []Feature{
			{
				ID:          "feat.root.keyway",
				ComponentID: "cmp.root",
				Name:        "Keyway",
				DisplayName: "Keyway",
				NativeType:  "PartDesign::Pocket",
			},
		},
		Parameters: []Parameter{
			{
				ID:           "par.root.length",
				OwnerKind:    OwnerKindComponent,
				OwnerID:      "cmp.root",
				ComponentID:  "cmp.root",
				Name:         "length",
				DisplayName:  "Length",
				ValueType:    "number",
				NativeType:   "App::PropertyLength",
				Unit:         "mm",
				Observable:   true,
				Writable:     true,
				CurrentValue: value,
			},
		},
		Metadata: []Metadata{
			{
				ID:           "meta.root.part_number",
				OwnerKind:    OwnerKindComponent,
				OwnerID:      "cmp.root",
				ComponentID:  "cmp.root",
				Key:          "part_number",
				DisplayName:  "Part Number",
				ValueType:    "string",
				NativeType:   "App::PropertyString",
				Observable:   true,
				Writable:     true,
				CurrentValue: partNumber,
			},
		},
	}
}

func modelWithFeatureAndComponentSharingName(t *testing.T) *Model {
	t.Helper()
	model := modelWithFeatureMutationFixtures(t)
	model.Components = append(model.Components, Component{
		ID:          "cmp.keyway",
		Kind:        "part",
		Name:        "Keyway",
		DisplayName: "Keyway Part",
		ParentID:    "cmp.root",
		ChildrenIDs: []string{},
		Quantity:    1,
	})
	model.Components[0].ChildrenIDs = append(model.Components[0].ChildrenIDs, "cmp.keyway")
	return model
}

func parseSemanticFixtureDSL(t *testing.T, content string) *dsl.AST {
	t.Helper()

	path := filepath.Join(t.TempDir(), "fixture.dsl")
	if err := os.WriteFile(path, []byte(withSemanticDSLVersionHeader(content)), 0o644); err != nil {
		t.Fatalf("failed to write DSL fixture: %v", err)
	}
	ast, err := dsl.Parse(path)
	if err != nil {
		t.Fatalf("dsl.Parse returned error: %v", err)
	}
	if err := dsl.Validate(ast); err != nil {
		t.Fatalf("dsl.Validate returned error: %v", err)
	}
	return ast
}

func withSemanticDSLVersionHeader(content string) string {
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "dsl v1.0") || strings.HasPrefix(trimmed, "dsl 1.0") {
		return content
	}
	return "dsl v1.0\n" + content
}

func assertInjectDSLIntentFailureIsStable(t *testing.T, model *Model, ast *dsl.AST) error {
	t.Helper()

	firstModel, firstErr := InjectDSLIntent(model, ast)
	secondModel, secondErr := InjectDSLIntent(model, ast)

	if firstErr == nil || secondErr == nil {
		t.Fatalf("expected InjectDSLIntent to fail, got first=%v second=%v", firstErr, secondErr)
	}
	if firstModel != nil || secondModel != nil {
		t.Fatalf("expected no partial injected model, got first=%#v second=%#v", firstModel, secondModel)
	}
	if !errors.Is(firstErr, ErrDSLIntent) || !errors.Is(secondErr, ErrDSLIntent) {
		t.Fatalf("expected DSL intent error classification, got first=%v second=%v", firstErr, secondErr)
	}
	if firstErr.Error() != secondErr.Error() {
		t.Fatalf("expected deterministic error text\nfirst:  %q\nsecond: %q", firstErr.Error(), secondErr.Error())
	}

	var firstIntentErr *DSLIntentError
	if !errors.As(firstErr, &firstIntentErr) {
		t.Fatalf("expected first DSLIntentError, got %T", firstErr)
	}
	var secondIntentErr *DSLIntentError
	if !errors.As(secondErr, &secondIntentErr) {
		t.Fatalf("expected second DSLIntentError, got %T", secondErr)
	}
	if !reflect.DeepEqual(firstIntentErr.Diagnostics(), secondIntentErr.Diagnostics()) {
		t.Fatalf("expected identical diagnostics across runs\nfirst:  %#v\nsecond: %#v", firstIntentErr.Diagnostics(), secondIntentErr.Diagnostics())
	}

	return firstErr
}

// assertInjectDSLIntentProducesNoMutations proves a deterministic successful
// injection that yields zero MutationIntent entries for the sole product,
// i.e. no semantic target resolution occurred for any parameter.
func assertInjectDSLIntentProducesNoMutations(t *testing.T, model *Model, ast *dsl.AST) {
	t.Helper()

	injected, err := InjectDSLIntent(model, ast)
	if err != nil {
		t.Fatalf("InjectDSLIntent returned error: %v", err)
	}
	if len(injected.ProductIntents) != 1 {
		t.Fatalf("expected exactly one product intent, got %#v", injected.ProductIntents)
	}
	if mutations := injected.ProductIntents[0].Mutations; len(mutations) != 0 {
		t.Fatalf("expected no mutations for prefix-like parameter, got %#v", mutations)
	}
}

func loadCaptureFixture(t *testing.T, category, name string) *cad.CADContract {
	t.Helper()
	path := filepath.Join("..", "cad", "testdata", category, name, "parametron.cad.json")
	contract, err := cad.Load(path)
	if err != nil {
		t.Fatalf("cad.Load(%q) returned error: %v", path, err)
	}
	return contract
}

func idsFromComponents(items []Component) []string {
	return collectTestIDs(len(items), func(i int) string { return items[i].ID })
}

func idsFromGroups(items []ParameterGroup) []string {
	return collectTestIDs(len(items), func(i int) string { return items[i].ID })
}

func idsFromParameters(items []Parameter) []string {
	return collectTestIDs(len(items), func(i int) string { return items[i].ID })
}

func idsFromMetadata(items []Metadata) []string {
	return collectTestIDs(len(items), func(i int) string { return items[i].ID })
}

func collectTestIDs(count int, valueAt func(index int) string) []string {
	out := make([]string, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, valueAt(i))
	}
	return out
}

func assertBuildFromCaptureFailureIsDeterministic(t *testing.T, contract *cad.CADContract, want error) {
	t.Helper()

	firstModel, firstErr := BuildFromCapture(contract)
	secondModel, secondErr := BuildFromCapture(contract)

	if firstErr == nil || secondErr == nil {
		t.Fatalf("expected BuildFromCapture to fail, got first=%v second=%v", firstErr, secondErr)
	}
	if firstModel != nil || secondModel != nil {
		t.Fatalf("expected no partial model, got first=%#v second=%#v", firstModel, secondModel)
	}
	if !errors.Is(firstErr, want) || !errors.Is(secondErr, want) {
		t.Fatalf("expected error classification %v, got first=%v second=%v", want, firstErr, secondErr)
	}
	if firstErr.Error() != secondErr.Error() {
		t.Fatalf("expected deterministic error text\nfirst:  %q\nsecond: %q", firstErr.Error(), secondErr.Error())
	}
}

func removeComponent(contract *cad.CADContract, componentID string) {
	contract.Entities.Components = slices.DeleteFunc(contract.Entities.Components, func(component cad.Component) bool {
		return component.ID == componentID
	})
	contract.Structure.Nodes = slices.DeleteFunc(contract.Structure.Nodes, func(node cad.StructureNode) bool {
		return node.ComponentID == componentID
	})
}

func largeSyntheticCapture(t *testing.T, componentCount int) *cad.CADContract {
	t.Helper()

	contract := &cad.CADContract{
		SchemaVersion: cad.SchemaVersion,
		CaptureID:     "cap.synthetic.large",
		Adapter: cad.NamedVersion{
			Name:    "freecad",
			Version: "1.0",
		},
		CADSystem: cad.NamedVersion{
			Name:    "FreeCAD",
			Version: "1.0",
		},
		SourceDocument: cad.SourceDocument{
			LogicalID:   "synthetic_large",
			Path:        "input/SyntheticLarge.FCStd",
			Fingerprint: "sha256:synthetic-large",
		},
		RootProduct: cad.RootProduct{
			ID: "cmp.000",
		},
		Annotations: cad.Annotations{},
		Entities: cad.Entities{
			Components:      make([]cad.Component, 0, componentCount),
			Features:        []cad.Feature{},
			Relationships:   []cad.Relationship{},
			ParameterGroups: make([]cad.ParameterGroup, 0, componentCount/3),
			Parameters:      make([]cad.Parameter, 0, componentCount/3),
			Metadata:        make([]cad.Metadata, 0, componentCount),
		},
		Structure: cad.Structure{
			RootComponentID: "cmp.000",
			Nodes:           make([]cad.StructureNode, 0, componentCount),
		},
	}

	childrenByParent := make(map[string][]string, componentCount)

	for i := 0; i < componentCount; i++ {
		componentID := fmt.Sprintf("cmp.%03d", i)
		parentID := ""
		kind := "part"
		cadType := "PartDesign::Body"
		material := "Steel"
		if i == 0 {
			kind = "assembly"
			cadType = "App::Part"
			material = ""
		} else {
			parentIndex := (i - 1) / 3
			parentID = fmt.Sprintf("cmp.%03d", parentIndex)
			childrenByParent[parentID] = append(childrenByParent[parentID], componentID)
			if i < componentCount/3 {
				kind = "assembly"
				cadType = "App::Part"
				material = ""
			}
		}

		contract.Entities.Components = append(contract.Entities.Components, cad.Component{
			ID:          componentID,
			Kind:        kind,
			Name:        fmt.Sprintf("Component%03d", i),
			DisplayName: fmt.Sprintf("Component %03d", i),
			CADType:     cadType,
			Quantity:    1 + (i % 3),
			Material:    material,
			Targetability: cad.Targetability{
				Suppress:   kind == "assembly",
				Unsuppress: kind == "assembly",
				Hide:       true,
				Unhide:     true,
			},
			Annotations: cad.Annotations{},
		})
		contract.Structure.Nodes = append(contract.Structure.Nodes, cad.StructureNode{
			ComponentID:       componentID,
			ParentComponentID: parentID,
			Children:          []string{},
		})

		if i%3 == 0 {
			groupID := fmt.Sprintf("grp.%03d.main", i)
			parameterValue, err := cad.NewScalarValue([]byte(fmt.Sprintf("%d", 10+i)))
			if err != nil {
				t.Fatalf("cad.NewScalarValue(parameter) returned error: %v", err)
			}
			metadataValue, err := cad.NewScalarValue([]byte(fmt.Sprintf(`"PN-%03d"`, i)))
			if err != nil {
				t.Fatalf("cad.NewScalarValue(metadata) returned error: %v", err)
			}

			contract.Entities.ParameterGroups = append(contract.Entities.ParameterGroups, cad.ParameterGroup{
				ID:               groupID,
				OwnerComponentID: componentID,
				Name:             "Main",
				DisplayName:      "Main",
				GroupKind:        "varset",
				CADType:          "Spreadsheet::Sheet",
				Observable:       true,
				Writable:         true,
				Annotations:      cad.Annotations{},
			})
			contract.Entities.Parameters = append(contract.Entities.Parameters, cad.Parameter{
				ID:           fmt.Sprintf("par.%03d.length", i),
				OwnerKind:    "group",
				OwnerID:      groupID,
				ComponentID:  componentID,
				Name:         "Length",
				DisplayName:  "Length",
				CADType:      "App::PropertyLength",
				ValueType:    "number",
				Observable:   true,
				Writable:     true,
				CurrentValue: parameterValue,
				Unit:         "mm",
				Annotations:  cad.Annotations{},
			})
			contract.Entities.Metadata = append(contract.Entities.Metadata, cad.Metadata{
				ID:           fmt.Sprintf("meta.%03d.part_number", i),
				OwnerKind:    "component",
				OwnerID:      componentID,
				ComponentID:  componentID,
				Key:          "part_number",
				DisplayName:  "Part Number",
				CADType:      "App::PropertyString",
				ValueType:    "string",
				Observable:   true,
				Writable:     true,
				CurrentValue: metadataValue,
				Annotations:  cad.Annotations{},
			})
		}
	}

	for i := range contract.Structure.Nodes {
		contract.Structure.Nodes[i].Children = append([]string(nil), childrenByParent[contract.Structure.Nodes[i].ComponentID]...)
	}

	if err := cad.Validate(contract); err != nil {
		t.Fatalf("largeSyntheticCapture produced invalid contract: %v", err)
	}

	return contract
}

func shuffleLargeCaptureDeterministically(contract *cad.CADContract) {
	rng := rand.New(rand.NewSource(42))

	shuffleWithRand(rng, contract.Entities.Components)
	shuffleWithRand(rng, contract.Entities.ParameterGroups)
	shuffleWithRand(rng, contract.Entities.Parameters)
	shuffleWithRand(rng, contract.Entities.Metadata)
	shuffleWithRand(rng, contract.Structure.Nodes)
	for i := range contract.Structure.Nodes {
		shuffleWithRand(rng, contract.Structure.Nodes[i].Children)
	}
}

func shuffleWithRand[T any](rng *rand.Rand, values []T) {
	rng.Shuffle(len(values), func(i, j int) {
		values[i], values[j] = values[j], values[i]
	})
}

func TestValidationErrorFormattingIncludesSortedProblems(t *testing.T) {
	model := minimalModel(t)
	model.SchemaVersion = ""
	model.RootComponentID = "cmp.missing"

	err := Validate(model)
	if err == nil {
		t.Fatal("expected Validate to fail")
	}
	if !strings.Contains(err.Error(), "rootComponentId") || !strings.Contains(err.Error(), "schemaVersion is required") {
		t.Fatalf("unexpected error formatting: %v", err)
	}
}
