package ir

import (
	"math"
	"reflect"
	"testing"

	"parametron/internal/authoring/planner"
	"parametron/internal/authoring/runtime"
)

func TestCreatePlanFromIR_SimpleProduct(t *testing.T) {
	dslContent := `
product Test {
    param width: number = 100
    param height: number = 200
}`

	program := parseAndConvert(t, dslContent)
	execPlan, err := CreatePlanFromIR(program, nil)
	if err != nil {
		t.Fatalf("CreatePlanFromIR failed: %v", err)
	}

	if len(execPlan.Steps) != 2 {
		t.Fatalf("Expected 2 steps, got %d", len(execPlan.Steps))
	}

	// Check WriteCSV step
	if execPlan.Steps[0].Type != planner.StepWriteCSV {
		t.Errorf("Expected first step to be WriteCSV, got %s", execPlan.Steps[0].Type)
	}

	payload, ok := execPlan.Steps[0].Payload.(planner.WriteCSVPayload)
	if !ok {
		t.Fatalf("Expected WriteCSVPayload, got %T", execPlan.Steps[0].Payload)
	}

	if payload.ProductKey != "Test" {
		t.Errorf("Expected product key 'Test', got '%s'", payload.ProductKey)
	}

	if len(payload.Headers) != 2 {
		t.Errorf("Expected 2 headers, got %d", len(payload.Headers))
	}

	if len(payload.Values) != 2 {
		t.Errorf("Expected 2 values, got %d", len(payload.Values))
	}

	// Check values
	if payload.Values[0] != 100.0 {
		t.Errorf("Expected width 100, got %v", payload.Values[0])
	}
	if payload.Values[1] != 200.0 {
		t.Errorf("Expected height 200, got %v", payload.Values[1])
	}

	// Check exporter manifest step
	if execPlan.Steps[1].Type != planner.StepWriteExportManifest {
		t.Errorf("Expected second step to be WriteExportManifest, got %s", execPlan.Steps[1].Type)
	}
}

func TestCreatePlanFromIR_ManifestMutationsOmittedWithoutSourceData(t *testing.T) {
	dslContent := `
product Test {
    param width: number = 100
}`

	program := parseAndConvert(t, dslContent)
	execPlan, err := CreatePlanFromIR(program, nil)
	if err != nil {
		t.Fatalf("CreatePlanFromIR failed: %v", err)
	}

	manifest, ok := execPlan.Steps[1].Payload.(planner.WriteExportManifestPayload)
	if !ok {
		t.Fatalf("expected WriteExportManifestPayload, got %T", execPlan.Steps[1].Payload)
	}
	if manifest.AssemblyMutations != nil {
		t.Fatalf("expected assemblyMutations to be omitted without source data, got %+v", manifest.AssemblyMutations)
	}
	if manifest.PartMutations != nil {
		t.Fatalf("expected partMutations to be omitted without source data, got %+v", manifest.PartMutations)
	}

	ast := parseAndValidateWithTablesForIR(t, dslContent, irTestTables())
	astPlan, err := planner.CreatePlan(ast, nil)
	if err != nil {
		t.Fatalf("planner.CreatePlan failed: %v", err)
	}
	if !reflect.DeepEqual(astPlan, execPlan) {
		t.Fatalf("expected AST and IR planner outputs to match\nAST: %#v\nIR: %#v", astPlan, execPlan)
	}
}

func TestCreatePlanFromIR_ConstantResolution(t *testing.T) {
	dslContent := `
const BASE = 50

product Test {
    param width: number = BASE
    param doubled: number = width * 2
}`

	program := parseAndConvert(t, dslContent)
	execPlan, err := CreatePlanFromIR(program, nil)
	if err != nil {
		t.Fatalf("CreatePlanFromIR failed: %v", err)
	}

	payload := execPlan.Steps[0].Payload.(planner.WriteCSVPayload)

	// width should be 50 (from constant)
	if payload.Values[0] != 50.0 {
		t.Errorf("Expected width 50, got %v", payload.Values[0])
	}

	// doubled should be 100 (50 * 2)
	if payload.Values[1] != 100.0 {
		t.Errorf("Expected doubled 100, got %v", payload.Values[1])
	}
}

func TestCreatePlanFromIR_OverrideHandling(t *testing.T) {
	dslContent := `
product Test {
    param width: number = 100
    param area: number = width * 2
}`

	program := parseAndConvert(t, dslContent)
	overrides := map[string]string{"width": "50"}

	execPlan, err := CreatePlanFromIR(program, overrides)
	if err != nil {
		t.Fatalf("CreatePlanFromIR failed: %v", err)
	}

	payload := execPlan.Steps[0].Payload.(planner.WriteCSVPayload)

	// width should be overridden to 50
	if payload.Values[0] != 50.0 {
		t.Errorf("Expected width 50 (overridden), got %v", payload.Values[0])
	}

	// area should be 100 (50 * 2)
	if payload.Values[1] != 100.0 {
		t.Errorf("Expected area 100, got %v", payload.Values[1])
	}
}

func TestCreatePlanFromIR_LetEvaluationAndOverrideBoundary(t *testing.T) {
	dslContent := `
product Test {
    let a = 5
    let b = a + 1
    param c: number = b + 1
}`

	program := parseAndConvert(t, dslContent)
	execPlan, err := CreatePlanFromIR(program, nil)
	if err != nil {
		t.Fatalf("CreatePlanFromIR failed: %v", err)
	}

	payload := execPlan.Steps[0].Payload.(planner.WriteCSVPayload)
	if len(payload.Values) != 1 || payload.Values[0] != 7.0 {
		t.Fatalf("expected c to resolve to 7, got %+v", payload.Values)
	}

	_, err = CreatePlanFromIR(program, map[string]string{"b": "20"})
	if err == nil {
		t.Fatal("expected overriding let to fail")
	}
	if !contains(err.Error(), "override target 'b' is not an exported parameter") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreatePlanFromIR_MixedBindingGraph(t *testing.T) {
	dslContent := `
product Test {
    param a: number = 10
    let b = a + 1
    param c: number = b + 1
    let d = c + 1
    param e: number = d + 1
}`

	program := parseAndConvert(t, dslContent)
	execPlan, err := CreatePlanFromIR(program, nil)
	if err != nil {
		t.Fatalf("CreatePlanFromIR failed: %v", err)
	}

	payload := execPlan.Steps[0].Payload.(planner.WriteCSVPayload)
	expected := []interface{}{10.0, 12.0, 14.0}
	if !reflect.DeepEqual(payload.Values, expected) {
		t.Fatalf("unexpected values: got %+v want %+v", payload.Values, expected)
	}
}

func TestCreatePlanFromIR_CannotOverrideConstant(t *testing.T) {
	dslContent := `
const BASE = 50
product Test {
    param width: number = BASE
}`

	program := parseAndConvert(t, dslContent)
	overrides := map[string]string{"BASE": "100"}

	_, err := CreatePlanFromIR(program, overrides)
	if err == nil {
		t.Fatal("Expected error when overriding constant")
	}
	if !contains(err.Error(), "cannot override constant") {
		t.Errorf("Expected 'cannot override constant' error, got: %v", err)
	}
}

func TestCreatePlanFromIR_UnknownOverride(t *testing.T) {
	dslContent := `
product Test {
    param width: number = 100
}`

	program := parseAndConvert(t, dslContent)
	overrides := map[string]string{"unknown": "value"}

	_, err := CreatePlanFromIR(program, overrides)
	if err == nil {
		t.Fatal("Expected error for unknown override")
	}
	if !contains(err.Error(), "not defined") {
		t.Errorf("Expected 'not defined' error, got: %v", err)
	}
}

func TestCreatePlanFromIR_ExpressionEvaluation(t *testing.T) {
	dslContent := `
product Test {
    param a: number = 10
    param b: number = 20
    param sum: number = a + b
    param prod: number = a * b
    param diff: number = b - a
    param quotient: number = b / a
    param comparison: boolean = a < b
}`

	program := parseAndConvert(t, dslContent)
	execPlan, err := CreatePlanFromIR(program, nil)
	if err != nil {
		t.Fatalf("CreatePlanFromIR failed: %v", err)
	}

	payload := execPlan.Steps[0].Payload.(planner.WriteCSVPayload)

	// Values should be: a=10, b=20, sum=30, prod=200, diff=10, quotient=2, comparison=true
	expectedValues := []interface{}{10.0, 20.0, 30.0, 200.0, 10.0, 2.0, true}

	if len(payload.Values) != len(expectedValues) {
		t.Fatalf("Expected %d values, got %d", len(expectedValues), len(payload.Values))
	}

	for i, expected := range expectedValues {
		actual := payload.Values[i]
		switch ev := expected.(type) {
		case float64:
			av, ok := actual.(float64)
			if !ok {
				t.Errorf("Value %d: expected float64, got %T", i, actual)
				continue
			}
			if math.Abs(av-ev) > 1e-9 {
				t.Errorf("Value %d: expected %v, got %v", i, expected, actual)
			}
		default:
			if actual != expected {
				t.Errorf("Value %d: expected %v, got %v", i, expected, actual)
			}
		}
	}
}

func TestCreatePlanFromIR_StringConcatenationEvaluation(t *testing.T) {
	dslContent := `
const PREFIX = "pre"

product Test {
    param left: string = "L"
    param right: string = "R"
    param joined: string = left + right
    param chained: string = "a" + "b" + "c"
    param ternary_joined: string = true ? left + right : "x" + "y"
    param from_const: string = PREFIX + "_fix"
    param sum: number = 20 + 22
}`

	program := parseAndConvert(t, dslContent)
	execPlan, err := CreatePlanFromIR(program, nil)
	if err != nil {
		t.Fatalf("CreatePlanFromIR failed: %v", err)
	}

	payload := execPlan.Steps[0].Payload.(planner.WriteCSVPayload)
	expectedValues := []interface{}{"L", "R", "LR", "abc", "LR", "pre_fix", 42.0}
	if len(payload.Values) != len(expectedValues) {
		t.Fatalf("expected %d values, got %d", len(expectedValues), len(payload.Values))
	}
	for i := range expectedValues {
		if payload.Values[i] != expectedValues[i] {
			t.Fatalf("value %d mismatch: got %v want %v", i, payload.Values[i], expectedValues[i])
		}
	}
}

func TestCreatePlanFromIR_StringConcatenationDeterministic(t *testing.T) {
	dslContent := `
product Test {
    param left: string = "A"
    param right: string = "B"
    param result: string = left + right
}`

	program := parseAndConvert(t, dslContent)

	const runs = 5
	for i := 0; i < runs; i++ {
		execPlan, err := CreatePlanFromIR(program, nil)
		if err != nil {
			t.Fatalf("run %d failed: %v", i+1, err)
		}
		payload := execPlan.Steps[0].Payload.(planner.WriteCSVPayload)
		if got := payload.Values[2]; got != "AB" {
			t.Fatalf("run %d result mismatch: got %v want AB", i+1, got)
		}
	}
}

func TestCreatePlanFromIR_StringInterpolationEvaluation(t *testing.T) {
	dslContent := `
product Test {
    param first: string = "Ada"
    param last: string = "Lovelace"
    param full: string = "{param:first} {param:last}"
    param badge: string = "ENG-{param:full}"
}`

	program := parseAndConvert(t, dslContent)
	execPlan, err := CreatePlanFromIR(program, nil)
	if err != nil {
		t.Fatalf("CreatePlanFromIR failed: %v", err)
	}

	payload := execPlan.Steps[0].Payload.(planner.WriteCSVPayload)
	expected := []interface{}{"Ada", "Lovelace", "Ada Lovelace", "ENG-Ada Lovelace"}
	if len(payload.Values) != len(expected) {
		t.Fatalf("expected %d values, got %d", len(expected), len(payload.Values))
	}
	for i := range expected {
		if payload.Values[i] != expected[i] {
			t.Fatalf("value %d mismatch: got %v want %v", i, payload.Values[i], expected[i])
		}
	}
}

func TestCreatePlanFromIR_StringInterpolationDeterministic(t *testing.T) {
	dslContent := `
product Test {
    param first: string = "A"
    param second: string = "{param:first}-B"
}`

	program := parseAndConvert(t, dslContent)

	for i := 0; i < 5; i++ {
		execPlan, err := CreatePlanFromIR(program, nil)
		if err != nil {
			t.Fatalf("run %d failed: %v", i+1, err)
		}
		payload := execPlan.Steps[0].Payload.(planner.WriteCSVPayload)
		if got := payload.Values[1]; got != "A-B" {
			t.Fatalf("run %d mismatch: got %v want A-B", i+1, got)
		}
	}
}

func TestCreatePlanFromIR_ShortCircuitLogicalOperators(t *testing.T) {
	// Register a panic function to test short-circuit
	panicFnName := "panic_if_called"
	originalFn, hadOriginal := runtime.Builtins[panicFnName]
	runtime.Builtins[panicFnName] = func(args []interface{}) (interface{}, error) {
		panic("right operand must not be evaluated")
	}
	defer func() {
		if hadOriginal {
			runtime.Builtins[panicFnName] = originalFn
		} else {
			delete(runtime.Builtins, panicFnName)
		}
	}()

	// Test && short-circuit: false && X should not evaluate X
	program := &IRProgram{
		Version: "1.0",
		Products: []IRProduct{
			{
				Name: "Test",
				Parameters: []IRParameter{
					{
						Name: "short_circuit_and",
						Type: IRType{Kind: IRTypeBoolean},
						DefaultValue: IRExpression{
							Kind:     ExprKindBinary,
							BinaryOp: "&&",
							BinaryLeft: &IRExpression{
								Kind:         ExprKindLiteral,
								LiteralValue: false,
							},
							BinaryRight: &IRExpression{
								Kind:     ExprKindCall,
								FuncName: panicFnName,
								FuncArgs: []IRExpression{},
							},
						},
					},
				},
			},
		},
	}

	execPlan, err := CreatePlanFromIR(program, nil)
	if err != nil {
		t.Fatalf("CreatePlanFromIR failed: %v", err)
	}

	payload := execPlan.Steps[0].Payload.(planner.WriteCSVPayload)
	if payload.Values[0] != false {
		t.Errorf("Expected false from short-circuit &&, got %v", payload.Values[0])
	}

	// Test || short-circuit: true || X should not evaluate X
	program2 := &IRProgram{
		Version: "1.0",
		Products: []IRProduct{
			{
				Name: "Test2",
				Parameters: []IRParameter{
					{
						Name: "short_circuit_or",
						Type: IRType{Kind: IRTypeBoolean},
						DefaultValue: IRExpression{
							Kind:     ExprKindBinary,
							BinaryOp: "||",
							BinaryLeft: &IRExpression{
								Kind:         ExprKindLiteral,
								LiteralValue: true,
							},
							BinaryRight: &IRExpression{
								Kind:     ExprKindCall,
								FuncName: panicFnName,
								FuncArgs: []IRExpression{},
							},
						},
					},
				},
			},
		},
	}

	execPlan2, err := CreatePlanFromIR(program2, nil)
	if err != nil {
		t.Fatalf("CreatePlanFromIR failed: %v", err)
	}

	payload2 := execPlan2.Steps[0].Payload.(planner.WriteCSVPayload)
	if payload2.Values[0] != true {
		t.Errorf("Expected true from short-circuit ||, got %v", payload2.Values[0])
	}
}

func TestCreatePlanFromIR_TernaryShortCircuit(t *testing.T) {
	// Test that only the taken branch is evaluated
	panicFnName := "panic_in_ternary"
	originalFn, hadOriginal := runtime.Builtins[panicFnName]
	runtime.Builtins[panicFnName] = func(args []interface{}) (interface{}, error) {
		panic("unreachable branch was evaluated")
	}
	defer func() {
		if hadOriginal {
			runtime.Builtins[panicFnName] = originalFn
		} else {
			delete(runtime.Builtins, panicFnName)
		}
	}()

	// true ? 1 : panic() - should not call panic
	program := &IRProgram{
		Version: "1.0",
		Products: []IRProduct{
			{
				Name: "Test",
				Parameters: []IRParameter{
					{
						Name: "ternary_true",
						Type: IRType{Kind: IRTypeNumber},
						DefaultValue: IRExpression{
							Kind: ExprKindTernary,
							TernaryCond: &IRExpression{
								Kind:         ExprKindLiteral,
								LiteralValue: true,
							},
							TernaryTrue: &IRExpression{
								Kind:         ExprKindLiteral,
								LiteralValue: 1.0,
							},
							TernaryFalse: &IRExpression{
								Kind:     ExprKindCall,
								FuncName: panicFnName,
								FuncArgs: []IRExpression{},
							},
						},
					},
				},
			},
		},
	}

	execPlan, err := CreatePlanFromIR(program, nil)
	if err != nil {
		t.Fatalf("CreatePlanFromIR failed: %v", err)
	}

	payload := execPlan.Steps[0].Payload.(planner.WriteCSVPayload)
	if payload.Values[0] != 1.0 {
		t.Errorf("Expected 1 from ternary, got %v", payload.Values[0])
	}

	// false ? panic() : 0 - should not call panic
	program2 := &IRProgram{
		Version: "1.0",
		Products: []IRProduct{
			{
				Name: "Test2",
				Parameters: []IRParameter{
					{
						Name: "ternary_false",
						Type: IRType{Kind: IRTypeNumber},
						DefaultValue: IRExpression{
							Kind: ExprKindTernary,
							TernaryCond: &IRExpression{
								Kind:         ExprKindLiteral,
								LiteralValue: false,
							},
							TernaryTrue: &IRExpression{
								Kind:     ExprKindCall,
								FuncName: panicFnName,
								FuncArgs: []IRExpression{},
							},
							TernaryFalse: &IRExpression{
								Kind:         ExprKindLiteral,
								LiteralValue: 0.0,
							},
						},
					},
				},
			},
		},
	}

	execPlan2, err := CreatePlanFromIR(program2, nil)
	if err != nil {
		t.Fatalf("CreatePlanFromIR failed: %v", err)
	}

	payload2 := execPlan2.Steps[0].Payload.(planner.WriteCSVPayload)
	if payload2.Values[0] != 0.0 {
		t.Errorf("Expected 0 from ternary, got %v", payload2.Values[0])
	}
}

func TestCreatePlanFromIR_EnumResolution(t *testing.T) {
	dslContent := `
product Test {
    param material: enum { Oak, Pine } = Oak
    param is_oak: boolean = material == Oak
}`

	program := parseAndConvert(t, dslContent)
	execPlan, err := CreatePlanFromIR(program, nil)
	if err != nil {
		t.Fatalf("CreatePlanFromIR failed: %v", err)
	}

	payload := execPlan.Steps[0].Payload.(planner.WriteCSVPayload)

	// material should be "Oak" (enum resolves to string)
	if payload.Values[0] != "Oak" {
		t.Errorf("Expected material 'Oak', got %v (type %T)", payload.Values[0], payload.Values[0])
	}

	// is_oak should be true
	if payload.Values[1] != true {
		t.Errorf("Expected is_oak true, got %v (type %T)", payload.Values[1], payload.Values[1])
	}
}

func TestCreatePlanFromIR_FunctionCalls(t *testing.T) {
	dslContent := `
product Test {
    param a: number = 10
    param b: number = 20
    param max_val: number = max(a, b)
    param min_val: number = min(a, b)
    param abs_val: number = abs(-5)
    param round_val: number = round(3.7)
    param floor_val: number = floor(12.9)
    param ceil_val: number = ceil(12.1)
    param clamp_hi: number = clamp(15, 0, 10)
    param clamp_ok: number = clamp(5, 0, 10)
    param up_val: number = round_up(13, 5)
    param down_val: number = round_down(13, 5)
    param nested: number = max(floor(8.9), 3)
    param chained: number = round_up(clamp(a, 0, 100), 5)
}`

	program := parseAndConvert(t, dslContent)
	execPlan, err := CreatePlanFromIR(program, nil)
	if err != nil {
		t.Fatalf("CreatePlanFromIR failed: %v", err)
	}

	payload := execPlan.Steps[0].Payload.(planner.WriteCSVPayload)

	// Values are in declaration order.
	expected := []float64{10.0, 20.0, 20.0, 10.0, 5.0, 4.0, 12.0, 13.0, 10.0, 5.0, 15.0, 10.0, 8.0, 10.0}

	for i, exp := range expected {
		actual, ok := payload.Values[i].(float64)
		if !ok {
			t.Errorf("Value %d: expected float64, got %T", i, payload.Values[i])
			continue
		}
		if math.Abs(actual-exp) > 1e-9 {
			t.Errorf("Value %d: expected %v, got %v", i, exp, actual)
		}
	}
}

func TestCreatePlanFromIR_FunctionCalls_InvalidNumericExpansion(t *testing.T) {
	tests := []struct {
		name          string
		dslContent    string
		errorContains string
	}{
		{
			name: "Clamp Invalid Bounds",
			dslContent: `
product Test {
    param lo: number = 20
    param hi: number = 5
    param bad: number = clamp(10, lo, hi)
}`,
			errorContains: "clamp requires lo <= hi",
		},
		{
			name: "Round Up Zero Step",
			dslContent: `
product Test {
    param step: number = 0
    param bad: number = round_up(10, step)
}`,
			errorContains: "round_up requires step > 0",
		},
		{
			name: "Round Down Negative Step",
			dslContent: `
product Test {
    param step: number = -2
    param bad: number = round_down(10, step)
}`,
			errorContains: "round_down requires step > 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			program := parseAndConvert(t, tt.dslContent)
			_, err := CreatePlanFromIR(program, nil)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !contains(err.Error(), tt.errorContains) {
				t.Fatalf("expected error containing %q, got %q", tt.errorContains, err.Error())
			}
		})
	}
}

func TestCreatePlanFromIR_NilProgram(t *testing.T) {
	_, err := CreatePlanFromIR(nil, nil)
	if err == nil {
		t.Fatal("Expected error for nil program")
	}
	if !contains(err.Error(), "nil") {
		t.Errorf("Expected nil error, got: %v", err)
	}
}

func TestCreatePlanFromIR_StringParameter(t *testing.T) {
	dslContent := `
const PREFIX = "Item"

product Test {
    param name: string = "TestItem"
    param label: string = PREFIX
}`

	program := parseAndConvert(t, dslContent)
	execPlan, err := CreatePlanFromIR(program, nil)
	if err != nil {
		t.Fatalf("CreatePlanFromIR failed: %v", err)
	}

	payload := execPlan.Steps[0].Payload.(planner.WriteCSVPayload)

	if payload.Values[0] != "TestItem" {
		t.Errorf("Expected name 'TestItem', got %v", payload.Values[0])
	}
	if payload.Values[1] != "Item" {
		t.Errorf("Expected label 'Item', got %v", payload.Values[1])
	}
}

func TestCreatePlanFromIR_BooleanParameter(t *testing.T) {
	dslContent := `
product Test {
    param flag: boolean = true
    param other: boolean = false
    param combined: boolean = flag && other
    param comparison: boolean = 1 < 2
    param logical: boolean = true && false
}`

	program := parseAndConvert(t, dslContent)
	execPlan, err := CreatePlanFromIR(program, nil)
	if err != nil {
		t.Fatalf("CreatePlanFromIR failed: %v", err)
	}

	payload := execPlan.Steps[0].Payload.(planner.WriteCSVPayload)

	// flag = true
	if payload.Values[0] != true {
		t.Errorf("Expected flag true, got %v", payload.Values[0])
	}
	// other = false
	if payload.Values[1] != false {
		t.Errorf("Expected other false, got %v", payload.Values[1])
	}
	// combined = true && false = false
	if payload.Values[2] != false {
		t.Errorf("Expected combined false, got %v", payload.Values[2])
	}
	// comparison = 1 < 2 = true
	if payload.Values[3] != true {
		t.Errorf("Expected comparison true, got %v", payload.Values[3])
	}
	// logical = true && false = false
	if payload.Values[4] != false {
		t.Errorf("Expected logical false, got %v", payload.Values[4])
	}
}

func TestCreatePlanFromIR_ComparisonOperators(t *testing.T) {
	dslContent := `
product Test {
    param a: number = 10
    param b: number = 20
    param eq: boolean = a == b
    param neq: boolean = a != b
    param lt: boolean = a < b
    param lte: boolean = a <= b
    param gt: boolean = a > b
    param gte: boolean = a >= b
}`

	program := parseAndConvert(t, dslContent)
	execPlan, err := CreatePlanFromIR(program, nil)
	if err != nil {
		t.Fatalf("CreatePlanFromIR failed: %v", err)
	}

	payload := execPlan.Steps[0].Payload.(planner.WriteCSVPayload)

	// 10 == 20 = false
	if payload.Values[2] != false {
		t.Errorf("Expected eq false, got %v", payload.Values[2])
	}
	// 10 != 20 = true
	if payload.Values[3] != true {
		t.Errorf("Expected neq true, got %v", payload.Values[3])
	}
	// 10 < 20 = true
	if payload.Values[4] != true {
		t.Errorf("Expected lt true, got %v", payload.Values[4])
	}
	// 10 <= 20 = true
	if payload.Values[5] != true {
		t.Errorf("Expected lte true, got %v", payload.Values[5])
	}
	// 10 > 20 = false
	if payload.Values[6] != false {
		t.Errorf("Expected gt false, got %v", payload.Values[6])
	}
	// 10 >= 20 = false
	if payload.Values[7] != false {
		t.Errorf("Expected gte false, got %v", payload.Values[7])
	}
}

func TestCreatePlanFromIR_StringComparison(t *testing.T) {
	dslContent := `
product Test {
    param a: string = "apple"
    param b: string = "banana"
    param eq: boolean = a == b
    param neq: boolean = a != b
}`

	program := parseAndConvert(t, dslContent)
	execPlan, err := CreatePlanFromIR(program, nil)
	if err != nil {
		t.Fatalf("CreatePlanFromIR failed: %v", err)
	}

	payload := execPlan.Steps[0].Payload.(planner.WriteCSVPayload)

	// "apple" == "banana" = false
	if payload.Values[2] != false {
		t.Errorf("Expected eq false, got %v", payload.Values[2])
	}
	// "apple" != "banana" = true
	if payload.Values[3] != true {
		t.Errorf("Expected neq true, got %v", payload.Values[3])
	}
}

func TestCreatePlanFromIR_EnumOverride(t *testing.T) {
	dslContent := `
product Test {
    param finish: enum { Oak, Pine } = Oak
}`

	program := parseAndConvert(t, dslContent)
	overrides := map[string]string{"finish": "Pine"}

	execPlan, err := CreatePlanFromIR(program, overrides)
	if err != nil {
		t.Fatalf("CreatePlanFromIR failed: %v", err)
	}

	payload := execPlan.Steps[0].Payload.(planner.WriteCSVPayload)

	if payload.Values[0] != "Pine" {
		t.Errorf("Expected finish 'Pine' (overridden), got %v", payload.Values[0])
	}
}

func TestCreatePlanFromIR_InvalidEnumOverride(t *testing.T) {
	dslContent := `
product Test {
    param finish: enum { Oak, Pine } = Oak
}`

	program := parseAndConvert(t, dslContent)
	overrides := map[string]string{"finish": "Maple"}

	_, err := CreatePlanFromIR(program, overrides)
	if err == nil {
		t.Fatal("Expected error for invalid enum override")
	}
	if !contains(err.Error(), "invalid enum") {
		t.Errorf("Expected 'invalid enum' error, got: %v", err)
	}
}

func TestCreatePlanFromIR_FilePatternConstPlaceholder_ResolvedAtPlannerStage(t *testing.T) {
	dslContent := `
const TAG = "irv1"

profile Dev {
    file_pattern = "{profile}_{product}_{const:TAG}_{param:x}"
}
use profile Dev

product Item {
    param x: number = 9
}`

	program := parseAndConvert(t, dslContent)
	execPlan, err := CreatePlanFromIR(program, nil)
	if err != nil {
		t.Fatalf("CreatePlanFromIR failed: %v", err)
	}
	if len(execPlan.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(execPlan.Steps))
	}

	writePayload, ok := execPlan.Steps[0].Payload.(planner.WriteCSVPayload)
	if !ok {
		t.Fatalf("expected WriteCSVPayload, got %T", execPlan.Steps[0].Payload)
	}
	if writePayload.Filename != "Dev_Item_irv1_9.csv" {
		t.Fatalf("unexpected csv filename: %q", writePayload.Filename)
	}
	if contains(writePayload.Filename, "{const:") {
		t.Fatalf("expected resolved filename, got %q", writePayload.Filename)
	}
}

// TestCreatePlanFromIR_WriteExportManifestFilenameUsesCurrentAlias proves that the IR
// planner delegates ManifestFilename to planner.ExportManifestFilename, which resolves
// to the canonical shared contract filename "prm.export-manifest.json".
func TestCreatePlanFromIR_WriteExportManifestFilenameUsesCurrentAlias(t *testing.T) {
	dslContent := `
product Widget {
    param width: number = 100
}
`
	program := parseAndConvert(t, dslContent)
	execPlan, err := CreatePlanFromIR(program, nil)
	if err != nil {
		t.Fatalf("CreatePlanFromIR failed: %v", err)
	}

	if len(execPlan.Steps) < 2 {
		t.Fatalf("expected at least 2 steps, got %d", len(execPlan.Steps))
	}

	manifestPayload, ok := execPlan.Steps[1].Payload.(planner.WriteExportManifestPayload)
	if !ok {
		t.Fatalf("expected WriteExportManifestPayload at step 1, got %T", execPlan.Steps[1].Payload)
	}

	if manifestPayload.ManifestFilename != planner.ExportManifestFilename {
		t.Errorf("ManifestFilename = %q, want planner.ExportManifestFilename %q", manifestPayload.ManifestFilename, planner.ExportManifestFilename)
	}
	if manifestPayload.ManifestFilename != "prm.export-manifest.json" {
		t.Errorf("ManifestFilename = %q, want canonical shared contract filename %q", manifestPayload.ManifestFilename, "prm.export-manifest.json")
	}
}

// Helper function to parse DSL and convert to IR
func parseAndConvert(t *testing.T, content string) *IRProgram {
	t.Helper()
	ast := parseAndValidate(t, content)
	program, err := ConvertAST(ast)
	if err != nil {
		t.Fatalf("ConvertAST failed: %v", err)
	}
	return program
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsInternal(s, substr))
}

func containsInternal(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
