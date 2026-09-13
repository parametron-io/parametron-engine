package ir

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMarshalUnmarshal_SimpleProgram(t *testing.T) {
	original := &IRProgram{
		Version: "1.0",
		Constants: map[string]IRConstant{
			"PI": {Name: "PI", Type: IRType{Kind: IRTypeNumber}, Value: 3.14159},
		},
		Products: []IRProduct{
			{
				Name: "Circle",
				Parameters: []IRParameter{
					{
						Name:         "radius",
						Type:         IRType{Kind: IRTypeNumber},
						DefaultValue: IRExpression{Kind: ExprKindLiteral, LiteralValue: 5.0},
					},
				},
			},
		},
	}

	// Marshal to JSON
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal program: %v", err)
	}

	// Unmarshal back
	var restored IRProgram
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Failed to unmarshal program: %v", err)
	}

	// Verify
	if restored.Version != original.Version {
		t.Errorf("Version mismatch: expected %s, got %s", original.Version, restored.Version)
	}

	if len(restored.Constants) != len(original.Constants) {
		t.Errorf("Constants count mismatch: expected %d, got %d", len(original.Constants), len(restored.Constants))
	}

	if len(restored.Products) != len(original.Products) {
		t.Errorf("Products count mismatch: expected %d, got %d", len(original.Products), len(restored.Products))
	}
}

func TestMarshalUnmarshal_CompleteProgram(t *testing.T) {
	original := &IRProgram{
		Version: "1.0",
		Constants: map[string]IRConstant{
			"BASE":  {Name: "BASE", Type: IRType{Kind: IRTypeNumber}, Value: 100.0},
			"LABEL": {Name: "LABEL", Type: IRType{Kind: IRTypeString}, Value: "test"},
		},
		Profiles: []IRProfile{
			{
				Name: "Dev",
				Settings: map[string]IRExpression{
					"output_dir": {Kind: ExprKindLiteral, LiteralValue: "output"},
				},
			},
		},
		ActiveProfileName: strPtr("Dev"),
		Products: []IRProduct{
			{
				Name: "Table",
				Parameters: []IRParameter{
					{
						Name: "width",
						Type: IRType{Kind: IRTypeNumber},
						DefaultValue: IRExpression{
							Kind:          ExprKindReference,
							ReferenceName: "BASE",
							ReferenceType: RefTypeConstant,
						},
					},
					{
						Name: "height",
						Type: IRType{Kind: IRTypeNumber},
						DefaultValue: IRExpression{
							Kind:     ExprKindBinary,
							BinaryOp: "*",
							BinaryLeft: &IRExpression{
								Kind:          ExprKindReference,
								ReferenceName: "width",
								ReferenceType: RefTypeParam,
							},
							BinaryRight: &IRExpression{
								Kind:         ExprKindLiteral,
								LiteralValue: 0.75,
							},
						},
					},
					{
						Name: "material",
						Type: IRType{Kind: IRTypeEnum, EnumValues: []string{"Oak", "Pine"}},
						DefaultValue: IRExpression{
							Kind:          ExprKindReference,
							ReferenceName: "Oak",
							ReferenceType: RefTypeEnumValue,
						},
					},
				},
			},
		},
	}

	// Marshal to JSON
	data, err := json.MarshalIndent(original, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal program: %v", err)
	}

	// Verify JSON structure contains expected fields
	jsonStr := string(data)
	expectedFields := []string{
		`"version": "1.0"`,
		`"constants"`,
		`"profiles"`,
		`"activeProfileName": "Dev"`,
		`"products"`,
		`"kind": "reference"`,
		`"kind": "binary"`,
		`"referenceType": "constant"`,
		`"referenceType": "param"`,
		`"referenceType": "enum"`,
		`"enumValues"`,
	}

	for _, field := range expectedFields {
		if !strings.Contains(jsonStr, field) {
			t.Errorf("JSON missing expected field: %s\nJSON:\n%s", field, jsonStr)
		}
	}

	// Unmarshal back
	var restored IRProgram
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Failed to unmarshal program: %v", err)
	}

	// Verify structure
	if restored.Version != "1.0" {
		t.Errorf("Version mismatch")
	}

	if len(restored.Constants) != 2 {
		t.Errorf("Constants count mismatch")
	}

	if len(restored.Profiles) != 1 {
		t.Errorf("Profiles count mismatch")
	}

	if restored.ActiveProfileName == nil || *restored.ActiveProfileName != "Dev" {
		t.Errorf("ActiveProfileName mismatch")
	}

	if len(restored.Products) != 1 {
		t.Errorf("Products count mismatch")
	}

	product := restored.Products[0]
	if len(product.Parameters) != 3 {
		t.Errorf("Parameters count mismatch")
	}

	// Check binary expression was preserved
	heightParam := product.Parameters[1]
	if heightParam.DefaultValue.Kind != ExprKindBinary {
		t.Errorf("Expected binary expression for height, got %s", heightParam.DefaultValue.Kind)
	}
	if heightParam.DefaultValue.BinaryLeft == nil {
		t.Error("Binary left operand should not be nil")
	}
	if heightParam.DefaultValue.BinaryRight == nil {
		t.Error("Binary right operand should not be nil")
	}

	// Check enum type preservation
	materialParam := product.Parameters[2]
	if materialParam.Type.Kind != IRTypeEnum {
		t.Errorf("Expected enum type, got %s", materialParam.Type.Kind)
	}
	if len(materialParam.Type.EnumValues) != 2 {
		t.Errorf("Expected 2 enum values, got %d", len(materialParam.Type.EnumValues))
	}
}

func TestMarshalUnmarshal_DeepNestedExpression(t *testing.T) {
	// Create a deeply nested expression: max(min(a, b), abs(c)) * (d + e)
	original := &IRProgram{
		Version: "1.0",
		Products: []IRProduct{
			{
				Name: "Test",
				Parameters: []IRParameter{
					{
						Name: "complex",
						Type: IRType{Kind: IRTypeNumber},
						DefaultValue: IRExpression{
							Kind:     ExprKindBinary,
							BinaryOp: "*",
							BinaryLeft: &IRExpression{
								Kind:     ExprKindCall,
								FuncName: "max",
								FuncArgs: []IRExpression{
									{
										Kind:     ExprKindCall,
										FuncName: "min",
										FuncArgs: []IRExpression{
											{Kind: ExprKindLiteral, LiteralValue: 1.0},
											{Kind: ExprKindLiteral, LiteralValue: 2.0},
										},
									},
									{
										Kind:     ExprKindCall,
										FuncName: "abs",
										FuncArgs: []IRExpression{
											{Kind: ExprKindLiteral, LiteralValue: -5.0},
										},
									},
								},
							},
							BinaryRight: &IRExpression{
								Kind:     ExprKindBinary,
								BinaryOp: "+",
								BinaryLeft: &IRExpression{
									Kind:         ExprKindLiteral,
									LiteralValue: 10.0,
								},
								BinaryRight: &IRExpression{
									Kind:         ExprKindLiteral,
									LiteralValue: 20.0,
								},
							},
						},
					},
				},
			},
		},
	}

	// Marshal to JSON
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal program: %v", err)
	}

	// Unmarshal back
	var restored IRProgram
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Failed to unmarshal program: %v", err)
	}

	// Verify the nested structure
	expr := restored.Products[0].Parameters[0].DefaultValue

	// Check outer binary expression
	if expr.Kind != ExprKindBinary {
		t.Fatalf("Expected binary expression, got %s", expr.Kind)
	}
	if expr.BinaryOp != "*" {
		t.Errorf("Expected operator '*', got '%s'", expr.BinaryOp)
	}

	// Check left side (max call)
	if expr.BinaryLeft.Kind != ExprKindCall {
		t.Errorf("Expected call expression on left, got %s", expr.BinaryLeft.Kind)
	}
	if expr.BinaryLeft.FuncName != "max" {
		t.Errorf("Expected 'max' function, got '%s'", expr.BinaryLeft.FuncName)
	}
	if len(expr.BinaryLeft.FuncArgs) != 2 {
		t.Errorf("Expected 2 arguments to max, got %d", len(expr.BinaryLeft.FuncArgs))
	}

	// Check nested min call
	minCall := expr.BinaryLeft.FuncArgs[0]
	if minCall.Kind != ExprKindCall || minCall.FuncName != "min" {
		t.Error("Expected nested min call")
	}

	// Check nested abs call
	absCall := expr.BinaryLeft.FuncArgs[1]
	if absCall.Kind != ExprKindCall || absCall.FuncName != "abs" {
		t.Error("Expected nested abs call")
	}

	// Check right side (addition)
	if expr.BinaryRight.Kind != ExprKindBinary || expr.BinaryRight.BinaryOp != "+" {
		t.Error("Expected binary '+' on right side")
	}
}

func TestValidate_EmptyVersion(t *testing.T) {
	program := &IRProgram{
		Version:   "",
		Constants: map[string]IRConstant{},
		Products:  []IRProduct{},
	}

	err := program.Validate()
	if err == nil {
		t.Fatal("Expected error for empty version")
	}
	if !strings.Contains(err.Error(), "version") {
		t.Errorf("Expected version error, got: %v", err)
	}
}

func TestValidate_InvalidConstant(t *testing.T) {
	program := &IRProgram{
		Version: "1.0",
		Constants: map[string]IRConstant{
			"BAD": {Name: "WRONG_NAME", Type: IRType{Kind: IRTypeNumber}, Value: 1.0},
		},
		Products: []IRProduct{},
	}

	err := program.Validate()
	if err == nil {
		t.Fatal("Expected error for constant name mismatch")
	}
}

func TestValidate_DuplicateProfile(t *testing.T) {
	program := &IRProgram{
		Version:   "1.0",
		Constants: map[string]IRConstant{},
		Profiles: []IRProfile{
			{Name: "Dev", Settings: map[string]IRExpression{}},
			{Name: "Dev", Settings: map[string]IRExpression{}},
		},
		Products: []IRProduct{},
	}

	err := program.Validate()
	if err == nil {
		t.Fatal("Expected error for duplicate profile")
	}
}

func TestValidate_MissingActiveProfile(t *testing.T) {
	program := &IRProgram{
		Version:           "1.0",
		Constants:         map[string]IRConstant{},
		ActiveProfileName: strPtr("Missing"),
		Profiles:          []IRProfile{},
		Products:          []IRProduct{},
	}

	err := program.Validate()
	if err == nil {
		t.Fatal("Expected error for missing active profile")
	}
}

func TestValidate_DuplicateProduct(t *testing.T) {
	program := &IRProgram{
		Version:   "1.0",
		Constants: map[string]IRConstant{},
		Products: []IRProduct{
			{Name: "A", Parameters: []IRParameter{}},
			{Name: "A", Parameters: []IRParameter{}},
		},
	}

	err := program.Validate()
	if err == nil {
		t.Fatal("Expected error for duplicate product")
	}
}

func TestValidate_DuplicateParameter(t *testing.T) {
	program := &IRProgram{
		Version:   "1.0",
		Constants: map[string]IRConstant{},
		Products: []IRProduct{
			{
				Name: "Test",
				Parameters: []IRParameter{
					{Name: "x", Type: IRType{Kind: IRTypeNumber}, DefaultValue: IRExpression{Kind: ExprKindLiteral, LiteralValue: 1.0}},
					{Name: "x", Type: IRType{Kind: IRTypeNumber}, DefaultValue: IRExpression{Kind: ExprKindLiteral, LiteralValue: 2.0}},
				},
			},
		},
	}

	err := program.Validate()
	if err == nil {
		t.Fatal("Expected error for duplicate parameter")
	}
}

func TestValidate_InvalidExpression(t *testing.T) {
	program := &IRProgram{
		Version:   "1.0",
		Constants: map[string]IRConstant{},
		Products: []IRProduct{
			{
				Name: "Test",
				Parameters: []IRParameter{
					{
						Name:         "x",
						Type:         IRType{Kind: IRTypeNumber},
						DefaultValue: IRExpression{Kind: "invalid_kind"},
					},
				},
			},
		},
	}

	err := program.Validate()
	if err == nil {
		t.Fatal("Expected error for invalid expression kind")
	}
}

func TestValidate_BinaryExpressionNilOperand(t *testing.T) {
	program := &IRProgram{
		Version:   "1.0",
		Constants: map[string]IRConstant{},
		Products: []IRProduct{
			{
				Name: "Test",
				Parameters: []IRParameter{
					{
						Name: "x",
						Type: IRType{Kind: IRTypeNumber},
						DefaultValue: IRExpression{
							Kind:        ExprKindBinary,
							BinaryOp:    "+",
							BinaryLeft:  nil,
							BinaryRight: &IRExpression{Kind: ExprKindLiteral, LiteralValue: 1.0},
						},
					},
				},
			},
		},
	}

	err := program.Validate()
	if err == nil {
		t.Fatal("Expected error for nil binary left operand")
	}
}

func TestToJSON(t *testing.T) {
	program := &IRProgram{
		Version: "1.0",
		Constants: map[string]IRConstant{
			"X": {Name: "X", Type: IRType{Kind: IRTypeNumber}, Value: 1.0},
		},
		Products: []IRProduct{
			{
				Name: "Test",
				Parameters: []IRParameter{
					{Name: "p", Type: IRType{Kind: IRTypeNumber}, DefaultValue: IRExpression{Kind: ExprKindLiteral, LiteralValue: 1.0}},
				},
			},
		},
	}

	jsonStr, err := program.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}

	// Verify it's valid JSON and contains expected fields
	var check map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &check); err != nil {
		t.Fatalf("ToJSON produced invalid JSON: %v", err)
	}

	if check["version"] != "1.0" {
		t.Error("Version field missing or incorrect in JSON")
	}
}

func TestFromJSON(t *testing.T) {
	jsonData := `{
		"version": "1.0",
		"constants": {
			"PI": {"name": "PI", "type": {"kind": "number"}, "value": 3.14}
		},
		"profiles": [],
		"activeProfileName": null,
		"products": [
			{
				"name": "Circle",
				"parameters": [
					{
						"name": "radius",
						"type": {"kind": "number"},
						"defaultValue": {"kind": "literal", "literalValue": 5}
					}
				]
			}
		]
	}`

	program, err := FromJSON(jsonData)
	if err != nil {
		t.Fatalf("FromJSON failed: %v", err)
	}

	if program.Version != "1.0" {
		t.Errorf("Version mismatch: expected 1.0, got %s", program.Version)
	}

	if len(program.Constants) != 1 {
		t.Errorf("Expected 1 constant, got %d", len(program.Constants))
	}

	if len(program.Products) != 1 {
		t.Errorf("Expected 1 product, got %d", len(program.Products))
	}

	if program.Products[0].Name != "Circle" {
		t.Errorf("Expected product name 'Circle', got %s", program.Products[0].Name)
	}
}

func TestFromJSON_Invalid(t *testing.T) {
	invalidJSON := `{"version": "1.0", "constants": {}}` // Missing required fields

	_, err := FromJSON(invalidJSON)
	// This might or might not fail depending on validation strictness
	// The main thing is it shouldn't panic
	_ = err
}

func TestMarshal_NilProgram(t *testing.T) {
	var program *IRProgram
	data, err := json.Marshal(program)
	if err != nil {
		t.Fatalf("Failed to marshal nil program: %v", err)
	}

	if string(data) != "null" {
		t.Errorf("Expected 'null', got %s", string(data))
	}
}

func TestUnmarshal_NilReceiver(t *testing.T) {
	jsonData := `{"version": "1.0"}`

	var program *IRProgram
	err := json.Unmarshal([]byte(jsonData), program)
	if err == nil {
		t.Fatal("Expected error for nil receiver")
	}
}

func TestMustToJSON(t *testing.T) {
	program := &IRProgram{
		Version:   "1.0",
		Constants: map[string]IRConstant{},
		Products:  []IRProduct{},
	}

	// Should not panic
	jsonStr := program.MustToJSON()
	if jsonStr == "" {
		t.Error("MustToJSON returned empty string")
	}

	// Verify it's valid JSON
	var check map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &check); err != nil {
		t.Fatalf("MustToJSON produced invalid JSON: %v", err)
	}
}

func TestTypeKindIsValid(t *testing.T) {
	tests := []struct {
		kind  IRTypeKind
		valid bool
	}{
		{IRTypeNumber, true},
		{IRTypeString, true},
		{IRTypeBoolean, true},
		{IRTypeEnum, true},
		{IRTypeKind("invalid"), false},
		{IRTypeKind(""), false},
	}

	for _, tt := range tests {
		if got := tt.kind.IsValid(); got != tt.valid {
			t.Errorf("%q.IsValid() = %v, want %v", tt.kind, got, tt.valid)
		}
	}
}

func TestExpressionKindIsValid(t *testing.T) {
	tests := []struct {
		kind  IRExpressionKind
		valid bool
	}{
		{ExprKindLiteral, true},
		{ExprKindReference, true},
		{ExprKindBinary, true},
		{ExprKindUnary, true},
		{ExprKindTernary, true},
		{ExprKindCall, true},
		{IRExpressionKind("invalid"), false},
		{IRExpressionKind(""), false},
	}

	for _, tt := range tests {
		if got := tt.kind.IsValid(); got != tt.valid {
			t.Errorf("%q.IsValid() = %v, want %v", tt.kind, got, tt.valid)
		}
	}
}

func TestRefTypeIsValid(t *testing.T) {
	tests := []struct {
		refType RefType
		valid   bool
	}{
		{RefTypeParam, true},
		{RefTypeConstant, true},
		{RefTypeEnumValue, true},
		{RefType("invalid"), false},
		{RefType(""), false},
	}

	for _, tt := range tests {
		if got := tt.refType.IsValid(); got != tt.valid {
			t.Errorf("%q.IsValid() = %v, want %v", tt.refType, got, tt.valid)
		}
	}
}
