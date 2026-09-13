package planner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"parametron/internal/authoring/dsl"
	"parametron/internal/authoring/runtime"
	"parametron/internal/engine/semantic"
	"parametron/internal/engine/semanticmap"
)

func TestCreatePlan(t *testing.T) {
	tests := []struct {
		name                  string
		dslContent            string
		overrides             map[string]string
		validateErrorContains string
		expectError           bool
		errorContains         string
		expectedValues        map[string]interface{}
	}{
		{
			name: "Simple Parameter Resolution",
			dslContent: `
product Test {
    param width: number = 100
    param height: number = 200
}`,
			expectedValues: map[string]interface{}{
				"width":  100.0,
				"height": 200.0,
			},
		},
		{
			name: "Dependency Resolution",
			dslContent: `
product Test {
    param width: number = 100
    param area: number = width * 2
}`,
			expectedValues: map[string]interface{}{
				"width": 100.0,
				"area":  200.0,
			},
		},
		{
			name: "Override Semantics",
			dslContent: `
product Test {
    param width: number = 100
    param area: number = width * 2
}`,
			overrides: map[string]string{"width": "50"},
			expectedValues: map[string]interface{}{
				"width": 50.0,
				"area":  100.0,
			},
		},
		{
			name: "Let Participates In Evaluation",
			dslContent: `
product Test {
    let base = 5
    let next = base + 1
    param result: number = next + 1
}`,
			expectedValues: map[string]interface{}{
				"result": 7.0,
			},
		},
		{
			name: "Mixed Let Param Graph",
			dslContent: `
product Test {
    param a: number = 10
    let b = a + 1
    param c: number = b + 1
}`,
			expectedValues: map[string]interface{}{
				"a": 10.0,
				"c": 12.0,
			},
		},
		{
			name: "Param Let Param Chain",
			dslContent: `
product Test {
    param a: number = 10
    let b = a + 1
    param c: number = b + 1
    let d = c + 1
    param e: number = d + 1
}`,
			expectedValues: map[string]interface{}{
				"a": 10.0,
				"c": 12.0,
				"e": 14.0,
			},
		},
		{
			name: "Constant Resolution",
			dslContent: `
const BASE = 50
product Test {
    param width: number = BASE
    param area: number = width * 2
}`,
			expectedValues: map[string]interface{}{
				"width": 50.0,
				"area":  100.0,
			},
		},
		{
			name: "Cannot Override Constant",
			dslContent: `
const BASE = 50
product Test {
    param width: number = BASE
}`,
			overrides:     map[string]string{"BASE": "99"},
			expectError:   true,
			errorContains: "cannot override constant",
		},
		{
			name: "Cannot Override Let",
			dslContent: `
product Test {
    let width = 100
    param area: number = width * 2
}`,
			overrides:     map[string]string{"width": "99"},
			expectError:   true,
			errorContains: "override target 'width' is not an exported parameter",
		},
		{
			name: "Circular Dependency Detection",
			dslContent: `
product Test {
    param a: number = b
    param b: number = a
}`,
			validateErrorContains: "circular dependency detected in product 'Test': a → b → a",
		},
		{
			name: "Override Cannot Break Circular Dependency At Validation",
			dslContent: `
product Test {
    param a: number = b
    param b: number = a
}`,
			overrides:             map[string]string{"a": "10"},
			validateErrorContains: "circular dependency detected in product 'Test': a → b → a",
		},
		{
			name: "Complex Expression Resolution",
			dslContent: `
product Test {
    param a: number = 10
    param b: number = 20
    param c: boolean = a < b
    param d: string = c ? "yes" : "no"
}`,
			expectedValues: map[string]interface{}{
				"a": 10.0,
				"b": 20.0,
				"c": true,
				"d": "yes",
			},
		},
		{
			name: "Function Call Evaluation - max",
			dslContent: `
product Test {
    param a: number = 150
    param b: number = max(a, 100)
}`,
			expectedValues: map[string]interface{}{
				"a": 150.0,
				"b": 150.0,
			},
		},
		{
			name: "Function Call Evaluation - abs/round",
			dslContent: `
product Test {
    param a: number = abs(10 - 20.6)
    param b: number = round(a)
}`,
			expectedValues: map[string]interface{}{
				"a": 10.6,
				"b": 11.0,
			},
		},
		{
			name: "Nested Function Call Evaluation",
			dslContent: `
product Test {
    param res: number = max(min(10, 100), min(50, 60))
}`,
			expectedValues: map[string]interface{}{
				"res": 50.0, // max(10, 50)
			},
		},
		{
			name: "Function Call Evaluation - Numeric Expansion",
			dslContent: `
product Test {
    param width: number = 13
    param floor_val: number = floor(12.9)
    param ceil_val: number = ceil(12.1)
    param clamp_hi: number = clamp(15, 0, 10)
    param clamp_ok: number = clamp(5, 0, 10)
    param up_val: number = round_up(width, 5)
    param down_val: number = round_down(width, 5)
    param nested: number = max(floor(8.9), 3)
    param chained: number = round_up(clamp(width, 0, 100), 5)
}`,
			expectedValues: map[string]interface{}{
				"floor_val": 12.0,
				"ceil_val":  13.0,
				"clamp_hi":  10.0,
				"clamp_ok":  5.0,
				"up_val":    15.0,
				"down_val":  10.0,
				"nested":    8.0,
				"chained":   15.0,
			},
		},
		{
			name: "Function Call Evaluation - Clamp Bounds Invalid At Plan Time",
			dslContent: `
product Test {
    param lo: number = 20
    param hi: number = 5
    param v: number = clamp(10, lo, hi)
}`,
			expectError:   true,
			errorContains: "clamp requires lo <= hi",
		},
		{
			name: "Function Call Evaluation - Round Up Zero Step At Plan Time",
			dslContent: `
product Test {
    param step: number = 0
    param v: number = round_up(10, step)
}`,
			expectError:   true,
			errorContains: "round_up requires step > 0",
		},
		{
			name: "Function Call Evaluation - Round Down Negative Step At Plan Time",
			dslContent: `
product Test {
    param step: number = -2
    param v: number = round_down(10, step)
}`,
			expectError:   true,
			errorContains: "round_down requires step > 0",
		},
		{
			name: "Enum Resolves To String",
			dslContent: `
product Test {
    param finish: enum { Oak, Pine } = Oak
}`,
			expectedValues: map[string]interface{}{
				"finish": "Oak",
			},
		},
		{
			name: "Enum Reference Resolution",
			dslContent: `
product Test {
    param base_finish: enum { Oak, Pine } = Oak
    param finish: enum { Oak, Pine } = base_finish
}`,
			expectedValues: map[string]interface{}{
				"base_finish": "Oak",
				"finish":      "Oak",
			},
		},
		{
			name: "Enum Ternary Resolution",
			dslContent: `
product Test {
    param use_oak: boolean = true
    param finish: enum { Oak, Pine } = use_oak ? Oak : Pine
}`,
			expectedValues: map[string]interface{}{
				"use_oak": true,
				"finish":  "Oak",
			},
		},
		{
			name: "Enum Override Valid",
			dslContent: `
product Test {
    param finish: enum { Oak, Pine } = Oak
}`,
			overrides: map[string]string{"finish": "Pine"},
			expectedValues: map[string]interface{}{
				"finish": "Pine",
			},
		},
		{
			name: "Enum Override Quoted",
			dslContent: `
product Test {
    param finish: enum { Oak, Pine } = Oak
}`,
			overrides: map[string]string{"finish": "\"Pine\""},
			expectedValues: map[string]interface{}{
				"finish": "Pine",
			},
		},
		{
			name: "Enum Override Whitespace",
			dslContent: `
product Test {
    param finish: enum { Oak, Pine } = Oak
}`,
			overrides: map[string]string{"finish": " Pine "},
			expectedValues: map[string]interface{}{
				"finish": "Pine",
			},
		},
		{
			name: "Enum Override Invalid Value",
			dslContent: `
product Test {
    param finish: enum { Oak, Pine } = Oak
}`,
			overrides:     map[string]string{"finish": "Maple"},
			expectError:   true,
			errorContains: "failed to apply override for 'finish': invalid enum override 'Maple', allowed values are [Oak Pine]",
		},
		{
			name: "Enum Override Wrong Case",
			dslContent: `
product Test {
    param finish: enum { Oak, Pine } = Oak
}`,
			overrides:     map[string]string{"finish": "pine"},
			expectError:   true,
			errorContains: "failed to apply override for 'finish': invalid enum override 'pine', allowed values are [Oak Pine]",
		},
		{
			name: "Enum Comparison Equal True",
			dslContent: `
product Test {
    param finish: enum { Oak, Pine } = Oak
    param is_oak: boolean = finish == Oak
}`,
			expectedValues: map[string]interface{}{
				"finish": "Oak",
				"is_oak": true,
			},
		},
		{
			name: "Enum Comparison Equal False",
			dslContent: `
product Test {
    param finish: enum { Oak, Pine } = Oak
    param is_pine: boolean = finish == Pine
}`,
			expectedValues: map[string]interface{}{
				"finish":  "Oak",
				"is_pine": false,
			},
		},
		{
			name: "Enum Comparison Not Equal True",
			dslContent: `
product Test {
    param finish: enum { Oak, Pine } = Oak
    param not_pine: boolean = finish != Pine
}`,
			expectedValues: map[string]interface{}{
				"finish":   "Oak",
				"not_pine": true,
			},
		},
		{
			name: "Enum Comparison In Ternary",
			dslContent: `
product Test {
    param finish: enum { Oak, Pine } = Oak
    param label: string = finish == Oak ? "premium" : "standard"
}`,
			expectedValues: map[string]interface{}{
				"finish": "Oak",
				"label":  "premium",
			},
		},
		{
			name: "Boolean Comparison Equal",
			dslContent: `
product Test {
    param a: boolean = true
    param b: boolean = true
    param result: boolean = a == b
}`,
			expectedValues: map[string]interface{}{
				"a":      true,
				"b":      true,
				"result": true,
			},
		},
		{
			name: "Boolean Comparison Not Equal",
			dslContent: `
product Test {
    param a: boolean = true
    param b: boolean = false
    param result: boolean = a != b
}`,
			expectedValues: map[string]interface{}{
				"a":      true,
				"b":      false,
				"result": true,
			},
		},
		{
			name: "Boolean Comparison Literals Matrix",
			dslContent: `
product Test {
    param eq_false: boolean = true == false
    param ne_true: boolean = true != false
    param eq_true: boolean = true == true
    param ne_false: boolean = true != true
}`,
			expectedValues: map[string]interface{}{
				"eq_false": false,
				"ne_true":  true,
				"eq_true":  true,
				"ne_false": false,
			},
		},
	}

	for _, tt := range tests {
		tt := tt // Capture range variable
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create a temporary file for the DSL content
			tmpFile, err := os.CreateTemp("", "test_*.dsl")
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}
			defer os.Remove(tmpFile.Name())

			if _, err := tmpFile.Write([]byte(withDSLVersionHeader(tt.dslContent))); err != nil {
				t.Fatalf("Failed to write to temp file: %v", err)
			}
			if err := tmpFile.Close(); err != nil {
				t.Fatalf("Failed to close temp file: %v", err)
			}

			// Parse the DSL
			ast, err := dsl.Parse(tmpFile.Name())
			if err != nil {
				t.Fatalf("Failed to parse DSL: %v", err)
			}

			// Validate the AST
			err = dsl.Validate(ast)
			if tt.validateErrorContains != "" {
				if err == nil {
					t.Fatalf("Expected validation error containing '%s', got nil", tt.validateErrorContains)
				}
				if !strings.Contains(err.Error(), tt.validateErrorContains) {
					t.Fatalf("Expected validation error containing '%s', got '%s'", tt.validateErrorContains, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("Failed to validate AST: %v", err)
			}

			// Create the execution plan
			plan, err := CreatePlan(ast, tt.overrides)

			// Check error expectations
			if tt.expectError {
				if err == nil {
					t.Error("Expected error, but got nil")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errorContains, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error creating plan: %v", err)
			}

			// Verify resolved values in the plan
			foundCSV := false
			for _, step := range plan.Steps {
				if step.Type == StepWriteCSV {
					foundCSV = true
					payload, ok := step.Payload.(WriteCSVPayload)
					if !ok {
						t.Fatalf("Expected WriteCSVPayload, got %T", step.Payload)
					}
					headers := payload.Headers
					values := payload.Values

					if len(headers) != len(values) {
						t.Fatalf("Headers count (%d) does not match values count (%d)", len(headers), len(values))
					}

					resultMap := make(map[string]interface{})
					for i, h := range headers {
						resultMap[h] = values[i]
					}

					for k, expectedV := range tt.expectedValues {
						actualV, ok := resultMap[k]
						if !ok {
							t.Errorf("Parameter '%s' missing in output", k)
							continue
						}
						switch v := actualV.(type) {
						case float64:
							ev := expectedV.(float64)
							if math.Abs(v-ev) > 1e-9 {
								t.Errorf("Parameter '%s': expected %v, got %v", k, expectedV, actualV)
							}
						default:
							if actualV != expectedV {
								t.Errorf("Parameter '%s': expected %v, got %v", k, expectedV, actualV)
							}
						}
					}
				}
			}

			if !foundCSV {
				t.Error("Plan did not contain a WriteCSV step")
			}
		})
	}
}

func TestEvaluateExpressionShortCircuitLogicalOperators(t *testing.T) {
	panicFnName := "panic_if_called_in_short_circuit_test"
	originalFn, hadOriginal := runtime.Builtins[panicFnName]
	runtime.Builtins[panicFnName] = func(args []interface{}) (interface{}, error) {
		panic("right operand must not be evaluated")
	}
	t.Cleanup(func() {
		if hadOriginal {
			runtime.Builtins[panicFnName] = originalFn
		} else {
			delete(runtime.Builtins, panicFnName)
		}
	})

	tests := []struct {
		name string
		expr dsl.ExpressionNode
		want bool
	}{
		{
			name: "AND short-circuits when left is false",
			expr: &dsl.BinaryExpression{
				Left:     &dsl.LiteralExpression{Value: false},
				Operator: "&&",
				Right:    &dsl.FunctionCallExpression{Name: panicFnName, Args: []dsl.ExpressionNode{}},
			},
			want: false,
		},
		{
			name: "OR short-circuits when left is true",
			expr: &dsl.BinaryExpression{
				Left:     &dsl.LiteralExpression{Value: true},
				Operator: "||",
				Right:    &dsl.FunctionCallExpression{Name: panicFnName, Args: []dsl.ExpressionNode{}},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := evaluateExpression(
				tt.expr,
				map[string]interface{}{},
				map[string]struct{}{},
				map[string]*dsl.ParameterNode{},
				map[string]dsl.ConstantValue{},
				nil,
			)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			gotBool, ok := got.(bool)
			if !ok {
				t.Fatalf("expected boolean result, got %T", got)
			}
			if gotBool != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, gotBool)
			}
		})
	}
}

func TestEvaluateExpression_RejectsUnknownIdentifierWithoutEnumContext(t *testing.T) {
	_, err := evaluateExpression(
		&dsl.IdentifierExpression{Name: "UNKNOWN"},
		map[string]interface{}{},
		map[string]struct{}{},
		map[string]*dsl.ParameterNode{},
		map[string]dsl.ConstantValue{},
		nil,
	)
	if err == nil {
		t.Fatal("expected undefined identifier error")
	}
	if !strings.Contains(err.Error(), "undefined identifier 'UNKNOWN'") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecutionPlanDeterminismWithEnumOverrides(t *testing.T) {
	dslContent := `
product Edge {
    param finish: enum { Oak, Pine } = Oak
}`

	createPlan := func(overrides map[string]string) (*ExecutionPlan, error) {
		tmpFile, err := os.CreateTemp("", "determinism_*.dsl")
		if err != nil {
			return nil, err
		}
		defer os.Remove(tmpFile.Name())

		if _, err := tmpFile.Write([]byte(withDSLVersionHeader(dslContent))); err != nil {
			return nil, err
		}
		if err := tmpFile.Close(); err != nil {
			return nil, err
		}

		ast, err := dsl.Parse(tmpFile.Name())
		if err != nil {
			return nil, err
		}
		if err := dsl.Validate(ast); err != nil {
			return nil, err
		}

		return CreatePlan(ast, overrides)
	}

	hashPlan := func(plan *ExecutionPlan) (string, string, error) {
		data, err := json.Marshal(plan)
		if err != nil {
			return "", "", err
		}
		sum := sha256.Sum256(data)
		return string(data), hex.EncodeToString(sum[:]), nil
	}

	plan1, err := createPlan(map[string]string{"finish": "Pine"})
	if err != nil {
		t.Fatalf("failed to create first plan: %v", err)
	}
	plan2, err := createPlan(map[string]string{"finish": "Pine"})
	if err != nil {
		t.Fatalf("failed to create second plan: %v", err)
	}
	plan3, err := createPlan(map[string]string{"finish": "Oak"})
	if err != nil {
		t.Fatalf("failed to create third plan: %v", err)
	}

	if !reflect.DeepEqual(plan1, plan2) {
		t.Fatalf("plans with same DSL and same override should be identical")
	}

	json1, hash1, err := hashPlan(plan1)
	if err != nil {
		t.Fatalf("failed to hash first plan: %v", err)
	}
	json2, hash2, err := hashPlan(plan2)
	if err != nil {
		t.Fatalf("failed to hash second plan: %v", err)
	}
	_, hash3, err := hashPlan(plan3)
	if err != nil {
		t.Fatalf("failed to hash third plan: %v", err)
	}

	if json1 != json2 {
		t.Fatalf("plan JSON serialization mismatch for same DSL and override")
	}
	if hash1 != hash2 {
		t.Fatalf("plan hash mismatch for same DSL and override")
	}
	if hash1 == hash3 {
		t.Fatalf("plan hash should change when enum override changes")
	}
}

func TestCreatePlan_UsesFilePatternFromProfile(t *testing.T) {
	dslContent := `
profile Prod {
    file_pattern = "{product}_{profile}_{param:width}"
}
use profile Prod

product Widget {
    param width: number = 120
}
`

	ast, plan := parseValidateAndPlan(t, dslContent)
	if ast.ActiveProfileName == nil || *ast.ActiveProfileName != "Prod" {
		t.Fatalf("expected active profile Prod, got %+v", ast.ActiveProfileName)
	}
	if len(plan.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(plan.Steps))
	}

	writePayload, ok := plan.Steps[0].Payload.(WriteCSVPayload)
	if !ok {
		t.Fatalf("expected WriteCSVPayload, got %T", plan.Steps[0].Payload)
	}
	if writePayload.Filename != "Widget_Prod_120.csv" {
		t.Fatalf("unexpected csv filename: %q", writePayload.Filename)
	}

	manifestPayload, ok := plan.Steps[1].Payload.(WriteExportManifestPayload)
	if !ok {
		t.Fatalf("expected WriteExportManifestPayload, got %T", plan.Steps[1].Payload)
	}
	if manifestPayload.SchemaVersion != ExportManifestSchemaVersion {
		t.Fatalf("unexpected schema version: %q", manifestPayload.SchemaVersion)
	}
	if manifestPayload.PlanHash == "" {
		t.Fatal("expected manifest plan hash to be set")
	}
	if got := manifestPayload.Values["width"]; got != 120.0 {
		t.Fatalf("unexpected manifest value width: %v", got)
	}
	if want := []ExportManifestParameterAssignment{{Name: "width", Value: 120.0, Type: "number", Unit: "mm"}}; !reflect.DeepEqual(manifestPayload.ParameterAssignments, want) {
		t.Fatalf("unexpected parameter assignments: %+v", manifestPayload.ParameterAssignments)
	}
	if len(manifestPayload.Outputs) != 1 || manifestPayload.Outputs[0].Type != "step" || manifestPayload.Outputs[0].Filename != "Widget_Prod_120.step" {
		t.Fatalf("unexpected manifest outputs: %+v", manifestPayload.Outputs)
	}
}

func TestCreatePlan_ExporterManifestStepOrderAndPayload(t *testing.T) {
	dslContent := `
product Widget {
    param width: number = 120
}
`

	_, plan := parseValidateAndPlan(t, dslContent)
	if len(plan.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(plan.Steps))
	}
	if plan.Steps[0].Type != StepWriteCSV {
		t.Fatalf("expected step 0 to be %s, got %s", StepWriteCSV, plan.Steps[0].Type)
	}
	if plan.Steps[1].Type != StepWriteExportManifest {
		t.Fatalf("expected step 1 to be %s, got %s", StepWriteExportManifest, plan.Steps[1].Type)
	}

	manifestPayload, ok := plan.Steps[1].Payload.(WriteExportManifestPayload)
	if !ok {
		t.Fatalf("expected WriteExportManifestPayload, got %T", plan.Steps[1].Payload)
	}
	if manifestPayload.SchemaVersion != ExportManifestSchemaVersion {
		t.Fatalf("expected schema version %q, got %q", ExportManifestSchemaVersion, manifestPayload.SchemaVersion)
	}
	if manifestPayload.PlanHash == "" {
		t.Fatal("expected non-empty manifest plan hash")
	}
	if manifestPayload.Product.ID != "Widget" {
		t.Fatalf("expected product id Widget, got %q", manifestPayload.Product.ID)
	}
	if got := manifestPayload.Values["width"]; got != 120.0 {
		t.Fatalf("expected values.width to be 120, got %v", got)
	}
	if want := []ExportManifestParameterAssignment{{Name: "width", Value: 120.0, Type: "number", Unit: "mm"}}; !reflect.DeepEqual(manifestPayload.ParameterAssignments, want) {
		t.Fatalf("unexpected parameter assignments: %+v", manifestPayload.ParameterAssignments)
	}
	if len(manifestPayload.Outputs) != 1 {
		t.Fatalf("expected a single output, got %d", len(manifestPayload.Outputs))
	}
	if manifestPayload.Outputs[0].Type != "step" {
		t.Fatalf("expected output type step, got %q", manifestPayload.Outputs[0].Type)
	}
	if manifestPayload.Outputs[0].Filename != "Widget.step" {
		t.Fatalf("expected output filename Widget.step, got %q", manifestPayload.Outputs[0].Filename)
	}
}

func TestCreatePlan_ProductOutputsFlowToExportManifest(t *testing.T) {
	dslContent := `
product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = ["step", "csv", "pdf"]

    param label: string = "box"
}
`

	_, plan := parseValidateAndPlan(t, dslContent)
	manifestPayload, ok := plan.Steps[1].Payload.(WriteExportManifestPayload)
	if !ok {
		t.Fatalf("expected WriteExportManifestPayload, got %T", plan.Steps[1].Payload)
	}

	expectedOutputs := []ExportManifestOutput{
		{Type: "step", Filename: "outputs/Box.step", Object: freeCADExportObjectName},
		{Type: "csv", Filename: "outputs/Box.csv"},
		{Type: "pdf", Filename: "outputs/Box.pdf"},
	}
	if !reflect.DeepEqual(manifestPayload.Outputs, expectedOutputs) {
		t.Fatalf("unexpected manifest outputs: %+v", manifestPayload.Outputs)
	}
}

func TestCreatePlan_ProductOutputsAppliedPerProduct(t *testing.T) {
	dslContent := `
product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = ["step", "pdf"]

    param label: string = "box"
}
product Bracket {
    adapter = "freecad"
    source_model = "input/bracket.FCStd"
    outputs = ["step"]

    param label: string = "bracket"
}
`

	_, plan := parseValidateAndPlan(t, dslContent)
	if len(plan.Steps) != 6 {
		t.Fatalf("expected 6 steps, got %d", len(plan.Steps))
	}

	firstManifest, ok := plan.Steps[1].Payload.(WriteExportManifestPayload)
	if !ok {
		t.Fatalf("expected first manifest payload, got %T", plan.Steps[1].Payload)
	}
	secondManifest, ok := plan.Steps[4].Payload.(WriteExportManifestPayload)
	if !ok {
		t.Fatalf("expected second manifest payload, got %T", plan.Steps[3].Payload)
	}

	expectedFirst := []ExportManifestOutput{
		{Type: "step", Filename: "outputs/Box.step", Object: freeCADExportObjectName},
		{Type: "pdf", Filename: "outputs/Box.pdf"},
	}
	expectedSecond := []ExportManifestOutput{
		{Type: "step", Filename: "outputs/Bracket.step", Object: freeCADExportObjectName},
	}
	if !reflect.DeepEqual(firstManifest.Outputs, expectedFirst) {
		t.Fatalf("unexpected first product outputs: %+v", firstManifest.Outputs)
	}
	if !reflect.DeepEqual(secondManifest.Outputs, expectedSecond) {
		t.Fatalf("unexpected second product outputs: %+v", secondManifest.Outputs)
	}
}

func TestCreatePlan_FreeCADManifestExecutionContract(t *testing.T) {
	dslContent := `
product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"

    param label: string = "box"
}
`

	ast, plan := parseValidateAndPlan(t, dslContent)
	if len(plan.Steps) != 3 {
		t.Fatalf("expected 3 steps for freecad adapter, got %d", len(plan.Steps))
	}
	manifestPayload, ok := plan.Steps[1].Payload.(WriteExportManifestPayload)
	if !ok {
		t.Fatalf("expected WriteExportManifestPayload, got %T", plan.Steps[1].Payload)
	}

	if manifestPayload.Adapter != "freecad" {
		t.Fatalf("expected adapter freecad, got %q", manifestPayload.Adapter)
	}

	expectedSourceModel := filepath.Join(filepath.Dir(ast.SourcePath), "input", "box.FCStd")
	expectedSourceModel, err := filepath.Abs(expectedSourceModel)
	if err != nil {
		t.Fatalf("failed to compute expected source model path: %v", err)
	}
	if manifestPayload.Inputs.SourceModel != expectedSourceModel {
		t.Fatalf("expected sourceModel %q, got %q", expectedSourceModel, manifestPayload.Inputs.SourceModel)
	}

	expectedValues := map[string]interface{}{
		"label": "box",
	}
	if !reflect.DeepEqual(manifestPayload.Values, expectedValues) {
		t.Fatalf("unexpected values: %+v", manifestPayload.Values)
	}
	if len(manifestPayload.ParameterAssignments) != 0 {
		t.Fatalf("expected no parameter assignments for string-only params, got: %+v", manifestPayload.ParameterAssignments)
	}

	if len(manifestPayload.Outputs) != 1 {
		t.Fatalf("expected 1 output, got %d", len(manifestPayload.Outputs))
	}
	if manifestPayload.Outputs[0].Object != freeCADExportObjectName {
		t.Fatalf("expected output object %q, got %q", freeCADExportObjectName, manifestPayload.Outputs[0].Object)
	}
	if plan.Steps[2].Type != StepRunCADRuntime {
		t.Fatalf("expected step 2 to be %s, got %s", StepRunCADRuntime, plan.Steps[2].Type)
	}
	runPayload, ok := plan.Steps[2].Payload.(RunCADRuntimePayload)
	if !ok {
		t.Fatalf("expected RunCADRuntimePayload, got %T", plan.Steps[2].Payload)
	}
	if runPayload.Adapter != "freecad" || runPayload.ResultFilename != FreeCADRuntimeResultFilename {
		t.Fatalf("unexpected runtime payload: %+v", runPayload)
	}
}

func TestCreatePlan_FreeCADProductOutputsUseImplementedBaseline(t *testing.T) {
	dslContent := `
product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = ["step", "csv", "pdf"]

    param label: string = "box"
}
`

	_, plan := parseValidateAndPlan(t, dslContent)
	if len(plan.Steps) != 3 {
		t.Fatalf("expected 3 steps for freecad adapter, got %d", len(plan.Steps))
	}

	manifestPayload, ok := plan.Steps[1].Payload.(WriteExportManifestPayload)
	if !ok {
		t.Fatalf("expected WriteExportManifestPayload, got %T", plan.Steps[1].Payload)
	}

	expectedOutputs := []ExportManifestOutput{
		{Type: "step", Filename: "outputs/Box.step", Object: freeCADExportObjectName},
		{Type: "csv", Filename: "outputs/Box.csv"},
		{Type: "pdf", Filename: "outputs/Box.pdf"},
	}
	if !reflect.DeepEqual(manifestPayload.Outputs, expectedOutputs) {
		t.Fatalf("unexpected manifest outputs: %+v", manifestPayload.Outputs)
	}
}

func TestBuildExportManifestOutputs_FreeCADAddsObjectOnlyToStepOutputs(t *testing.T) {
	outputs, err := buildExportManifestOutputs("Widget", []string{"step", "csv", "pdf"}, true)
	if err != nil {
		t.Fatalf("buildExportManifestOutputs returned error: %v", err)
	}

	expected := []ExportManifestOutput{
		{Type: "step", Filename: "Widget.step", Object: freeCADExportObjectName},
		{Type: "csv", Filename: "Widget.csv"},
		{Type: "pdf", Filename: "Widget.pdf"},
	}
	if !reflect.DeepEqual(outputs, expected) {
		t.Fatalf("unexpected outputs: %+v", outputs)
	}
}

func TestBuildExportManifestOutputs_RejectsUnsupportedLegacyFormat(t *testing.T) {
	_, err := buildExportManifestOutputs("Widget", []string{"step", "stl"}, true)
	if err == nil {
		t.Fatal("expected unsupported legacy output to fail")
	}
	if !strings.Contains(err.Error(), `unsupported output format "stl"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreatePlan_AdapterNoneSkipsCADRunner(t *testing.T) {
	dslContent := `
product Widget {
    adapter = "none"

    param width: number = 120
}
`

	_, plan := parseValidateAndPlan(t, dslContent)
	if len(plan.Steps) != 2 {
		t.Fatalf("expected 2 steps when adapter is none, got %d", len(plan.Steps))
	}
	manifestPayload, ok := plan.Steps[1].Payload.(WriteExportManifestPayload)
	if !ok {
		t.Fatalf("expected WriteExportManifestPayload, got %T", plan.Steps[1].Payload)
	}
	if len(manifestPayload.Outputs) != 0 {
		t.Fatalf("expected no manifest outputs when adapter is none, got %+v", manifestPayload.Outputs)
	}
}

func TestCreatePlan_ProductExecutionDeclarationsDoNotCrossContaminate(t *testing.T) {
	dslContent := `
product CADBox {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = ["step", "pdf"]

    param label: string = "box"
}

product PlanningOnly {
    adapter = "none"
    outputs = []

    param width: number = 50
}
`

	ast, plan := parseValidateAndPlan(t, dslContent)
	if len(plan.Steps) != 5 {
		t.Fatalf("expected 5 steps, got %d", len(plan.Steps))
	}

	firstManifest, ok := plan.Steps[1].Payload.(WriteExportManifestPayload)
	if !ok {
		t.Fatalf("expected first manifest payload, got %T", plan.Steps[1].Payload)
	}
	secondManifest, ok := plan.Steps[4].Payload.(WriteExportManifestPayload)
	if !ok {
		t.Fatalf("expected second manifest payload, got %T", plan.Steps[4].Payload)
	}

	expectedSourceModel := filepath.Join(filepath.Dir(ast.SourcePath), "input", "box.FCStd")
	expectedSourceModel, err := filepath.Abs(expectedSourceModel)
	if err != nil {
		t.Fatalf("failed to compute expected source model path: %v", err)
	}

	if firstManifest.Adapter != "freecad" || firstManifest.Inputs.SourceModel != expectedSourceModel {
		t.Fatalf("unexpected first manifest contract: %+v", firstManifest)
	}
	if !reflect.DeepEqual(firstManifest.Outputs, []ExportManifestOutput{
		{Type: "step", Filename: "outputs/CADBox.step", Object: freeCADExportObjectName},
		{Type: "pdf", Filename: "outputs/CADBox.pdf"},
	}) {
		t.Fatalf("unexpected first manifest outputs: %+v", firstManifest.Outputs)
	}

	if secondManifest.Adapter != "none" {
		t.Fatalf("expected adapter none for second manifest, got %q", secondManifest.Adapter)
	}
	if secondManifest.Inputs.SourceModel != "" {
		t.Fatalf("expected empty source model for second manifest, got %q", secondManifest.Inputs.SourceModel)
	}
	if len(secondManifest.Outputs) != 0 {
		t.Fatalf("expected no outputs for second manifest, got %+v", secondManifest.Outputs)
	}
}

func TestCreatePlan_FreeCADRequiresSourceModel(t *testing.T) {
	dslContent := `
product Box {
    adapter = "freecad"

    param length: number = 10
}
`

	tmpFile, err := os.CreateTemp("", "freecad_missing_source_model_*.dsl")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write([]byte(withDSLVersionHeader(dslContent))); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("failed to close temp file: %v", err)
	}

	ast, err := dsl.Parse(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to parse dsl: %v", err)
	}
	err = dsl.Validate(ast)
	if err == nil {
		t.Fatal("expected validation to fail when freecad adapter is missing source_model")
	}
	if !strings.Contains(err.Error(), `source_model is required when adapter is "freecad"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreatePlan_ExporterManifestBackwardCompatibleWithoutContractFields(t *testing.T) {
	dslContent := `
product Widget {
    param width: number = 120
}
`

	_, plan := parseValidateAndPlan(t, dslContent)
	manifestPayload, ok := plan.Steps[1].Payload.(WriteExportManifestPayload)
	if !ok {
		t.Fatalf("expected WriteExportManifestPayload, got %T", plan.Steps[1].Payload)
	}

	if manifestPayload.Adapter != "" {
		t.Fatalf("expected empty adapter for backward compatibility, got %q", manifestPayload.Adapter)
	}
	if manifestPayload.Inputs.SourceModel != "" {
		t.Fatalf("expected empty sourceModel for backward compatibility, got %q", manifestPayload.Inputs.SourceModel)
	}
	if got := manifestPayload.Values["width"]; got != 120.0 {
		t.Fatalf("expected values.width to be 120, got %v", got)
	}
	if len(manifestPayload.ParameterAssignments) != 1 {
		t.Fatalf("expected one parameter assignment, got %+v", manifestPayload.ParameterAssignments)
	}
	if manifestPayload.Outputs[0].Object != "" {
		t.Fatalf("expected no output object for backward compatibility, got %q", manifestPayload.Outputs[0].Object)
	}
}

func TestWriteExportManifestPayload_JSONSerializationStable(t *testing.T) {
	payload := WriteExportManifestPayload{
		SchemaVersion: ExportManifestSchemaVersion,
		PlanHash:      "abc123",
		Adapter:       "freecad",
		Product:       ExportManifestProduct{ID: "Box"},
		Inputs: ExportManifestInputs{
			SourceModel: "/tmp/box.FCStd",
		},
		Values: map[string]interface{}{
			"length": 35.0,
			"width":  53.0,
			"height": 42.0,
		},
		ParameterAssignments: []ExportManifestParameterAssignment{
			{Name: "length", Value: 35.0, Type: "number", Unit: "mm"},
		},
		AssemblyMutations: &ExportManifestMutationCollection{
			Parameters: []ExportManifestParameterMutation{
				{Object: "Assembly", Property: "Length", ValueParam: "length", Type: "number", Unit: "mm"},
			},
			Properties: []ExportManifestPropertyMutation{
				{Object: "Assembly", Property: "PartNumber", Value: "BOX-35"},
			},
			Suppression: []ExportManifestSuppressionMutation{
				{Object: "Bracket-1", Suppressed: true},
			},
		},
		PartMutations: &ExportManifestMutationCollection{
			Parameters: []ExportManifestParameterMutation{
				{Object: "Pad", Property: "Length", ValueParam: "length", Type: "number", Unit: "mm"},
			},
		},
		Outputs: []ExportManifestOutput{
			{Type: "step", Filename: "Box.step", Object: "Body"},
		},
	}

	first, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("first marshal failed: %v", err)
	}
	second, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("second marshal failed: %v", err)
	}

	if string(first) != string(second) {
		t.Fatalf("manifest payload serialization is not stable")
	}
	if !strings.Contains(string(first), `"schemaVersion": "1.0"`) {
		t.Fatalf("serialized payload missing schemaVersion: %s", string(first))
	}
	if !strings.Contains(string(first), `"adapter": "freecad"`) {
		t.Fatalf("serialized payload missing adapter: %s", string(first))
	}
	if !strings.Contains(string(first), `"sourceModel": "/tmp/box.FCStd"`) {
		t.Fatalf("serialized payload missing sourceModel: %s", string(first))
	}
	if strings.Contains(string(first), `"csv":`) {
		t.Fatalf("serialized payload must not include inputs.csv: %s", string(first))
	}
	if !strings.Contains(string(first), `"values"`) {
		t.Fatalf("serialized payload missing values: %s", string(first))
	}
	if strings.Contains(string(first), `"bindings"`) {
		t.Fatalf("serialized payload must not include bindings: %s", string(first))
	}
	if !strings.Contains(string(first), `"parameterAssignments"`) {
		t.Fatalf("serialized payload missing parameterAssignments: %s", string(first))
	}
	if !strings.Contains(string(first), `"assemblyMutations"`) {
		t.Fatalf("serialized payload missing assemblyMutations: %s", string(first))
	}
	if !strings.Contains(string(first), `"partMutations"`) {
		t.Fatalf("serialized payload missing partMutations: %s", string(first))
	}
	if !strings.Contains(string(first), `"valueParam": "length"`) {
		t.Fatalf("serialized payload missing valueParam: %s", string(first))
	}
	if !strings.Contains(string(first), `"suppressed": true`) {
		t.Fatalf("serialized payload missing suppression mutation: %s", string(first))
	}
	if strings.Contains(string(first), `VarSet.`) {
		t.Fatalf("serialized payload must not include adapter-specific targets: %s", string(first))
	}
	if !strings.Contains(string(first), `"object": "Body"`) {
		t.Fatalf("serialized payload missing output object: %s", string(first))
	}
}

func TestWriteExportManifestPayload_JSONSerializationOmitsAbsentMutations(t *testing.T) {
	payload := WriteExportManifestPayload{
		SchemaVersion: ExportManifestSchemaVersion,
		PlanHash:      "abc123",
		Product:       ExportManifestProduct{ID: "Box"},
		Inputs:        ExportManifestInputs{},
		Values:        map[string]interface{}{"length": 35.0},
		ParameterAssignments: []ExportManifestParameterAssignment{
			{Name: "length", Value: 35.0, Type: "number", Unit: "mm"},
		},
		Outputs: []ExportManifestOutput{
			{Type: "step", Filename: "Box.step"},
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	if strings.Contains(string(data), `"assemblyMutations"`) {
		t.Fatalf("expected assemblyMutations to be omitted when absent: %s", string(data))
	}
	if strings.Contains(string(data), `"partMutations"`) {
		t.Fatalf("expected partMutations to be omitted when absent: %s", string(data))
	}
}

func TestCreatePlan_ManifestMutationsOmittedWithoutSourceData(t *testing.T) {
	dslContent := `
product Box {
    adapter = "freecad"
    source_model = "models/box.FCStd"

    param label: string = "box"
}`

	_, plan := parseValidateAndPlan(t, dslContent)
	manifestPayload, ok := plan.Steps[1].Payload.(WriteExportManifestPayload)
	if !ok {
		t.Fatalf("expected WriteExportManifestPayload, got %T", plan.Steps[1].Payload)
	}

	if manifestPayload.AssemblyMutations != nil {
		t.Fatalf("expected assemblyMutations to be omitted without source data, got %+v", manifestPayload.AssemblyMutations)
	}
	if manifestPayload.PartMutations != nil {
		t.Fatalf("expected partMutations to be omitted without source data, got %+v", manifestPayload.PartMutations)
	}
}

func TestBuildDeclaredExportManifestParameterAssignments_IgnoresNonNumberParameters(t *testing.T) {
	params := []*dsl.ParameterNode{
		{
			Name: "label",
			Type: dsl.ParameterType{Kind: dsl.ParamTypeString},
		},
	}

	assignments, err := buildDeclaredExportManifestParameterAssignments(params, map[string]any{"label": "box"})
	if err != nil {
		t.Fatalf("expected non-number parameter assignment to be ignored, got error: %v", err)
	}
	if len(assignments) != 0 {
		t.Fatalf("expected no parameter assignments for non-number parameters, got %+v", assignments)
	}
}

func TestValidateFreeCADCADTargetMappings(t *testing.T) {
	singleAssemblyMatch := &ExportManifestMutationCollection{
		Parameters: []ExportManifestParameterMutation{
			{Object: "Body", Property: "Width", ValueParam: "width"},
		},
	}
	twoMatchesInAssembly := &ExportManifestMutationCollection{
		Parameters: []ExportManifestParameterMutation{
			{Object: "Body", Property: "Width", ValueParam: "width"},
			{Object: "Part", Property: "Width", ValueParam: "width"},
		},
	}

	t.Run("explicit target bypasses mutation requirement", func(t *testing.T) {
		err := ValidateFreeCADCADTargetMappings(
			[]ExportManifestParameterAssignment{{Name: "width", Target: "Body.Width", Value: 50.0, Type: "number", Unit: "mm"}},
			nil, nil,
		)
		if err != nil {
			t.Fatalf("expected nil for explicit target, got: %v", err)
		}
	})

	t.Run("exactly one mutation fallback accepted", func(t *testing.T) {
		err := ValidateFreeCADCADTargetMappings(
			[]ExportManifestParameterAssignment{{Name: "width", Value: 50.0, Type: "number", Unit: "mm"}},
			singleAssemblyMatch, nil,
		)
		if err != nil {
			t.Fatalf("expected nil for exactly-one mutation, got: %v", err)
		}
	})

	t.Run("zero mutation matches rejected as missing", func(t *testing.T) {
		err := ValidateFreeCADCADTargetMappings(
			[]ExportManifestParameterAssignment{{Name: "width", Value: 50.0, Type: "number", Unit: "mm"}},
			nil, nil,
		)
		if err == nil {
			t.Fatal("expected error for missing mutation, got nil")
		}
		if !strings.Contains(err.Error(), "missing") {
			t.Errorf("error %q does not indicate missing target", err.Error())
		}
		if !strings.Contains(err.Error(), "parameterAssignments[0]") {
			t.Errorf("error %q does not identify parameterAssignments[0]", err.Error())
		}
		if !strings.Contains(err.Error(), `"width"`) {
			t.Errorf("error %q does not identify parameter name", err.Error())
		}
	})

	t.Run("two mutations in same collection rejected as ambiguous", func(t *testing.T) {
		err := ValidateFreeCADCADTargetMappings(
			[]ExportManifestParameterAssignment{{Name: "width", Value: 50.0, Type: "number", Unit: "mm"}},
			twoMatchesInAssembly, nil,
		)
		if err == nil {
			t.Fatal("expected error for ambiguous mutations, got nil")
		}
		if !strings.Contains(err.Error(), "ambiguous") {
			t.Errorf("error %q does not indicate ambiguity", err.Error())
		}
		if !strings.Contains(err.Error(), "parameterAssignments[0]") {
			t.Errorf("error %q does not identify parameterAssignments[0]", err.Error())
		}
	})

	t.Run("one match in assembly plus one in part rejected as ambiguous", func(t *testing.T) {
		assemblyMatch := &ExportManifestMutationCollection{
			Parameters: []ExportManifestParameterMutation{
				{Object: "Assembly", Property: "Width", ValueParam: "width"},
			},
		}
		partMatch := &ExportManifestMutationCollection{
			Parameters: []ExportManifestParameterMutation{
				{Object: "Part", Property: "Width", ValueParam: "width"},
			},
		}
		err := ValidateFreeCADCADTargetMappings(
			[]ExportManifestParameterAssignment{{Name: "width", Value: 50.0, Type: "number", Unit: "mm"}},
			assemblyMatch, partMatch,
		)
		if err == nil {
			t.Fatal("expected ambiguous error when match spans assembly and part, got nil")
		}
		if !strings.Contains(err.Error(), "ambiguous") {
			t.Errorf("error %q does not indicate ambiguity", err.Error())
		}
	})

	t.Run("empty assignments always pass", func(t *testing.T) {
		err := ValidateFreeCADCADTargetMappings(nil, nil, nil)
		if err != nil {
			t.Fatalf("expected nil for empty assignments, got: %v", err)
		}
	})
}

func TestProjectSemanticManifestIntent_ReturnsCompleteManifestProjection(t *testing.T) {
	model := &semantic.Model{
		SchemaVersion:           semantic.SchemaVersion,
		SourceDocumentLogicalID: "box_model",
		RootComponentID:         "cmp.root",
		Components: []semantic.Component{
			{
				ID:   "cmp.root",
				Kind: "assembly",
				Name: "RootAssembly",
			},
			{
				ID:       "cmp.part",
				Kind:     "part",
				Name:     "Leg",
				ParentID: "cmp.root",
			},
		},
		Parameters: []semantic.Parameter{
			{
				ID:          "par.root.length",
				OwnerKind:   semantic.OwnerKindComponent,
				OwnerID:     "cmp.root",
				ComponentID: "cmp.root",
				Name:        "length",
				NativeType:  "Length",
			},
		},
		Metadata: []semantic.Metadata{
			{
				ID:          "meta.part.finish",
				OwnerKind:   semantic.OwnerKindComponent,
				OwnerID:     "cmp.part",
				ComponentID: "cmp.part",
				Key:         "finish",
				NativeType:  "String",
				ValueType:   "string",
			},
		},
	}
	intent := &semantic.ProductIntent{
		Name: "Box",
		ExportedParameters: []semantic.ExportedParameterIntent{
			{DSLParameterName: "length", SemanticParameterID: "par.root.length"},
		},
		Outputs: []semantic.OutputIntent{
			{OutputType: "step", TargetEntityKind: "assembly", TargetSemanticID: "cmp.root", TargetNameSource: "identity_linkage"},
			{OutputType: "csv", TargetEntityKind: "assembly", TargetSemanticID: "cmp.root", TargetNameSource: "identity_linkage", Scope: "assembly"},
		},
		Mutations: []semantic.MutationIntent{
			{
				OperationKind:    "set_parameter",
				TargetEntityKind: "parameter",
				TargetSemanticID: "par.root.length",
				TargetField:      "length",
				Scope:            "assembly",
				ValueSource: semantic.MutationValueSource{
					Kind:                "dsl_parameter",
					DSLParameterName:    "length",
					SemanticParameterID: "par.root.length",
				},
			},
			{
				OperationKind:    "set_property",
				TargetEntityKind: "metadata",
				TargetSemanticID: "meta.part.finish",
				TargetField:      "finish",
				Scope:            "part",
				ValueSource: semantic.MutationValueSource{
					Kind:             "dsl_parameter",
					DSLParameterName: "finish",
				},
			},
		},
	}

	result, err := projectSemanticManifestIntent(&semanticManifestProjectionRequest{
		BaseName: "Box",
		Model:    model,
		Intent:   intent,
		ResolvedValues: map[string]any{
			"length": 35.0,
			"finish": "Oak",
		},
		Contract: testProjectionContract(),
	})
	if err != nil {
		t.Fatalf("expected semantic manifest projection result, got error: %v", err)
	}

	want := &semanticManifestProjectionResult{
		ParameterAssignments: []ExportManifestParameterAssignment{
			{Name: "length", Target: "RootAssembly.length", Value: 35.0, Type: "number", Unit: "mm"},
		},
		Outputs: []ExportManifestOutput{
			{Type: "csv", Filename: "Box.csv", Target: "RootAssembly", Rollup: "assembly"},
			{Type: "step", Filename: "Box.step", Object: "RootAssembly"},
		},
		AssemblyMutations: &ExportManifestMutationCollection{
			Parameters: []ExportManifestParameterMutation{
				{Object: "RootAssembly", Property: "length", ValueParam: "length", Type: "number", Unit: "mm"},
			},
		},
		PartMutations: &ExportManifestMutationCollection{
			Properties: []ExportManifestPropertyMutation{
				{Object: "Leg", Property: "finish", Value: "Oak"},
			},
		},
		Verification: VerificationManifestIntent{
			ExpectedParameters: []VerificationExpectedParameter{
				{ID: "par.root.length", Name: "length", Value: 35.0, Type: "number", Unit: "mm"},
			},
			ObservationParameterLinks: []VerificationObservationParameterLink{
				{ID: "par.root.length", Name: "length", GroupName: ""},
			},
		},
	}
	if !reflect.DeepEqual(result, want) {
		t.Fatalf("unexpected semantic manifest projection\nwant: %#v\ngot:  %#v", want, result)
	}

	second, err := projectSemanticManifestIntent(&semanticManifestProjectionRequest{
		BaseName: "Box",
		Model:    model,
		Intent:   intent,
		ResolvedValues: map[string]any{
			"length": 35.0,
			"finish": "Oak",
		},
		Contract: testProjectionContract(),
	})
	if err != nil {
		t.Fatalf("second semantic manifest projection failed: %v", err)
	}
	if !reflect.DeepEqual(result, second) {
		t.Fatalf("semantic manifest projection changed across repeated runs\nfirst:  %#v\nsecond: %#v", result, second)
	}
}

func TestProjectSemanticManifestIntent_FailsWithoutSemanticModel(t *testing.T) {
	_, err := projectSemanticManifestIntent(&semanticManifestProjectionRequest{
		BaseName: "Box",
		Intent: &semantic.ProductIntent{
			Name: "Box",
		},
		ResolvedValues: map[string]any{},
		Contract:       testProjectionContract(),
	})
	if err == nil {
		t.Fatal("expected semantic manifest projection to fail without a semantic model")
	}
	if !strings.Contains(err.Error(), "semantic model must not be nil") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProjectSemanticManifestIntent_FailsWithoutIdentityLinkage(t *testing.T) {
	contract := testProjectionContract()
	contract.CaptureToSemantic.Components = []semanticmap.ComponentMapping{
		{
			CaptureKind: "component",
			SemanticKinds: map[string]string{
				"part": "part",
			},
			Identity:      "capture.id",
			Targetability: "capture.targetability",
		},
	}

	_, err := projectSemanticManifestIntent(&semanticManifestProjectionRequest{
		BaseName: "Box",
		Model: &semantic.Model{
			SchemaVersion:           semantic.SchemaVersion,
			SourceDocumentLogicalID: "box_model",
			RootComponentID:         "cmp.root",
			Components: []semantic.Component{
				{ID: "cmp.root", Kind: "assembly", Name: "RootAssembly"},
			},
		},
		Intent: &semantic.ProductIntent{
			Name: "Box",
			Outputs: []semantic.OutputIntent{
				{OutputType: "step", TargetEntityKind: "assembly", TargetSemanticID: "cmp.root", TargetNameSource: "identity_linkage"},
			},
		},
		ResolvedValues: map[string]any{},
		Contract:       contract,
	})
	if err == nil {
		t.Fatal("expected semantic manifest projection to fail when identity linkage is unavailable")
	}
	if !strings.Contains(err.Error(), "cannot be linked through captureToSemantic.components") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func testProjectionContract() *semanticmap.SemanticMap {
	return &semanticmap.SemanticMap{
		SchemaVersion: "1.0",
		MappingID:     "test-freecad-default-v1",
		Adapter:       "freecad",
		CADSystem: &semanticmap.CADSystem{
			Name: "FreeCAD",
		},
		SemanticTypes: &semanticmap.SemanticTypes{
			ParameterTypes: map[string]semanticmap.ParameterType{
				"length": {
					ValueKind:   "number",
					DefaultUnit: "mm",
					Coercion:    semantic.CoercionRuleNumberToLength,
				},
			},
			ResolutionPolicy: &semanticmap.ResolutionPolicy{
				Precedence:           []string{"explicit_parameter_mapping", "capture_native_type_mapping"},
				AllowEngineDefault:   boolPtr(false),
				AllowEngineInference: boolPtr(false),
			},
		},
		CaptureToSemantic: &semanticmap.CaptureToSemantic{
			ParameterGroups: []semanticmap.ParameterGroupMapping{
				{CaptureKind: "parameter_group", CADContainerType: "VarSet", SemanticKind: "parameter_group"},
			},
			Parameters: []semanticmap.ParameterMapping{
				{
					CaptureKind:  "parameter",
					SemanticKind: "parameter",
					Identity:     "capture.id",
					Name:         "capture.name",
					Group:        "capture.groupId",
					SemanticType: semanticmap.ParameterSemanticTypeRef{
						From: "capture.nativeType",
						Map: map[string]string{
							"Length": "length",
						},
					},
				},
			},
			Components: []semanticmap.ComponentMapping{
				{
					CaptureKind: "component",
					SemanticKinds: map[string]string{
						"assembly": "assembly",
						"part":     "part",
					},
					Identity:      "capture.id",
					Targetability: "capture.targetability",
				},
			},
			Metadata: []semanticmap.MetadataMapping{
				{
					CaptureKind:  "metadata",
					SemanticKind: "metadata",
					Identity:     "capture.id",
					Key:          "capture.key",
					ValueKind:    "capture.nativeType",
				},
			},
		},
		SemanticToManifest: &semanticmap.SemanticToManifest{
			Parameters: &semanticmap.ParameterManifestMapping{
				Target: "parameterAssignments",
				Name:   "semantic.parameter.name",
				Value:  "resolved.value",
				Type:   "number",
				Unit:   "resolved.unit",
			},
			Outputs: map[string]semanticmap.OutputManifestMapping{
				"csv":  {ManifestType: "csv", TargetField: "target", TargetSource: "semantic.output.target.manifestName", ScopeField: "rollup", ScopeMap: map[string]string{"flat": "flat", "assembly": "assembly", "subtree": "assembly"}},
				"pdf":  {ManifestType: "pdf"},
				"step": {ManifestType: "step", TargetField: "object", TargetSource: "semantic.output.target.manifestName"},
			},
			Mutations: map[string]semanticmap.MutationMapping{
				"set_parameter": {ManifestCollection: "partMutations.parameters", TargetField: "object", PropertyField: "property", ValueField: "valueParam"},
				"set_property":  {ManifestCollection: "partMutations.properties", TargetField: "object", PropertyField: "property", ValueField: "value"},
				"suppress":      {ManifestCollection: "partMutations.suppression", TargetField: "object", ValueField: "suppressed", Value: boolPtr(true)},
			},
		},
		OverridePolicy: &semanticmap.OverridePolicy{
			AllowAdapterOverrides: boolPtr(false),
			AllowProjectOverrides: boolPtr(false),
			AllowUserOverrides:    boolPtr(false),
		},
		OperationCapabilities: map[string]semanticmap.Capability{
			"write_parameter": {
				RequiresWritable:       true,
				AllowedSemanticTargets: []string{"parameter"},
			},
		},
		Determinism: &semanticmap.Determinism{
			AllowImplicitFallback:     boolPtr(false),
			AllowCaseInsensitiveMatch: boolPtr(false),
			AllowDisplayNameIdentity:  boolPtr(false),
			AllowAdapterInference:     boolPtr(false),
		},
	}
}

func boolPtr(v bool) *bool {
	return &v
}

func parseValidateAndPlanCaptureBacked(t *testing.T, dslContent string, model *semantic.Model, contract *semanticmap.SemanticMap) (*dsl.AST, *ExecutionPlan) {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "planner_capture_backed_*.dsl")
	if err != nil {
		t.Fatalf("failed to create temp DSL: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write([]byte(withDSLVersionHeader(dslContent))); err != nil {
		t.Fatalf("failed to write temp DSL: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("failed to close temp DSL: %v", err)
	}

	ast, err := dsl.Parse(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to parse DSL: %v", err)
	}
	if err := dsl.Validate(ast); err != nil {
		t.Fatalf("failed to validate DSL: %v", err)
	}

	intentModel, err := semantic.InjectDSLIntent(semantic.Clone(model), ast)
	if err != nil {
		t.Fatalf("failed to inject DSL intent: %v", err)
	}

	plan, err := CreatePlanWithTablesAndSemanticModel(ast, map[string]string{}, nil, intentModel, contract)
	if err != nil {
		t.Fatalf("failed to create capture-backed plan: %v", err)
	}

	return ast, plan
}

func parseValidateAndPlanCaptureBackedError(t *testing.T, dslContent string, model *semantic.Model, contract *semanticmap.SemanticMap, injectIntent bool) error {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "planner_capture_backed_error_*.dsl")
	if err != nil {
		t.Fatalf("failed to create temp DSL: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write([]byte(withDSLVersionHeader(dslContent))); err != nil {
		t.Fatalf("failed to write temp DSL: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("failed to close temp DSL: %v", err)
	}

	ast, err := dsl.Parse(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to parse DSL: %v", err)
	}
	if err := dsl.Validate(ast); err != nil {
		t.Fatalf("failed to validate DSL: %v", err)
	}

	planningModel := semantic.Clone(model)
	if injectIntent {
		planningModel, err = semantic.InjectDSLIntent(planningModel, ast)
		if err != nil {
			t.Fatalf("failed to inject DSL intent: %v", err)
		}
	}

	_, err = CreatePlanWithTablesAndSemanticModel(ast, map[string]string{}, nil, planningModel, contract)
	return err
}

func manifestPayloadFromPlan(t *testing.T, plan *ExecutionPlan) WriteExportManifestPayload {
	t.Helper()

	if plan == nil || len(plan.Steps) < 2 {
		t.Fatalf("expected manifest step, got %#v", plan)
	}

	payload, ok := plan.Steps[1].Payload.(WriteExportManifestPayload)
	if !ok {
		t.Fatalf("expected WriteExportManifestPayload, got %T", plan.Steps[1].Payload)
	}
	return payload
}

func manifestPayloadJSONFromPlan(t *testing.T, plan *ExecutionPlan) string {
	t.Helper()

	payload := manifestPayloadFromPlan(t, plan)
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal manifest payload: %v", err)
	}
	return string(data)
}

func assertExecutionOnlyManifestPayload(t *testing.T, payload WriteExportManifestPayload, forbiddenSentinels ...string) {
	t.Helper()

	data, problems := collectExecutionOnlyManifestPayloadProblems(payload, forbiddenSentinels...)
	if len(problems) == 0 {
		return
	}

	t.Fatalf("expected execution-only manifest payload, found leakage:\n%s\nmanifest JSON: %s", strings.Join(problems, "\n"), data)
}

func collectExecutionOnlyManifestPayloadProblems(payload WriteExportManifestPayload, forbiddenSentinels ...string) (string, []string) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", []string{fmt.Sprintf("$.<marshal>: failed to marshal manifest payload: %v", err)}
	}

	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return string(data), []string{fmt.Sprintf("$.<unmarshal>: failed to unmarshal manifest payload JSON: %v", err)}
	}

	problems := []string{}
	inspectExecutionOnlyManifestJSON(decoded, "$", "$", forbiddenSentinels, &problems)
	sort.Strings(problems)
	return string(data), problems
}

func inspectExecutionOnlyManifestJSON(node any, schemaPath, displayPath string, forbiddenSentinels []string, problems *[]string) {
	switch value := node.(type) {
	case map[string]any:
		allowedKeys, allowDynamicKeys, ok := allowedManifestKeysForSchemaPath(schemaPath)
		if !ok {
			*problems = append(*problems, fmt.Sprintf("%s: unexpected object at schema path %s", displayPath, schemaPath))
			return
		}

		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for _, key := range keys {
			childSchemaPath := schemaPath + "." + key
			if schemaPath == "$" {
				childSchemaPath = "$." + key
			}
			childDisplayPath := displayPath + "." + key
			if displayPath == "$" {
				childDisplayPath = "$." + key
			}

			if isForbiddenManifestKeyName(key) {
				*problems = append(*problems, fmt.Sprintf("%s: forbidden key %q", childDisplayPath, key))
			}
			for _, sentinel := range forbiddenSentinels {
				if sentinel != "" && strings.Contains(key, sentinel) {
					*problems = append(*problems, fmt.Sprintf("%s: key %q contains forbidden sentinel %q", childDisplayPath, key, sentinel))
				}
			}
			if !allowDynamicKeys {
				if _, allowed := allowedKeys[key]; !allowed {
					*problems = append(*problems, fmt.Sprintf("%s: unexpected key %q", childDisplayPath, key))
				}
			}

			nextSchemaPath := childSchemaPath
			if schemaPath == "$.values" || strings.HasPrefix(schemaPath, "$.values.") {
				nextSchemaPath = "$.values"
			}
			inspectExecutionOnlyManifestJSON(value[key], nextSchemaPath, childDisplayPath, forbiddenSentinels, problems)
		}
	case []any:
		for i, item := range value {
			childSchemaPath := schemaPath + "[]"
			childDisplayPath := fmt.Sprintf("%s[%d]", displayPath, i)
			if schemaPath == "$.values" || strings.HasPrefix(schemaPath, "$.values.") {
				childSchemaPath = "$.values"
			}
			inspectExecutionOnlyManifestJSON(item, childSchemaPath, childDisplayPath, forbiddenSentinels, problems)
		}
	case string:
		for _, sentinel := range forbiddenSentinels {
			if sentinel != "" && strings.Contains(value, sentinel) {
				*problems = append(*problems, fmt.Sprintf("%s: string value %q contains forbidden sentinel %q", displayPath, value, sentinel))
			}
		}
	}
}

func allowedManifestKeysForSchemaPath(schemaPath string) (map[string]struct{}, bool, bool) {
	if schemaPath == "$.values" || strings.HasPrefix(schemaPath, "$.values.") {
		return nil, true, true
	}

	allowed := map[string]map[string]struct{}{
		"$": {
			"schemaVersion":        {},
			"planHash":             {},
			"adapter":              {},
			"product":              {},
			"inputs":               {},
			"sourceDocument":       {},
			"values":               {},
			"parameterAssignments": {},
			"outputs":              {},
			"assemblyMutations":    {},
			"partMutations":        {},
		},
		"$.product": {
			"id": {},
		},
		"$.inputs": {
			"sourceModel": {},
		},
		"$.parameterAssignments[]": {
			"name":   {},
			"target": {},
			"value":  {},
			"type":   {},
			"unit":   {},
		},
		"$.outputs[]": {
			"type":     {},
			"filename": {},
			"object":   {},
			"target":   {},
			"rollup":   {},
			"columns":  {},
		},
		"$.assemblyMutations": {
			"parameters":  {},
			"properties":  {},
			"suppression": {},
		},
		"$.partMutations": {
			"parameters":  {},
			"properties":  {},
			"suppression": {},
		},
		"$.assemblyMutations.parameters[]": {
			"object":     {},
			"property":   {},
			"valueParam": {},
			"type":       {},
			"unit":       {},
		},
		"$.partMutations.parameters[]": {
			"object":     {},
			"property":   {},
			"valueParam": {},
			"type":       {},
			"unit":       {},
		},
		"$.assemblyMutations.properties[]": {
			"object":   {},
			"property": {},
			"value":    {},
		},
		"$.partMutations.properties[]": {
			"object":   {},
			"property": {},
			"value":    {},
		},
		"$.assemblyMutations.suppression[]": {
			"object":     {},
			"suppressed": {},
		},
		"$.partMutations.suppression[]": {
			"object":     {},
			"suppressed": {},
		},
	}

	keys, ok := allowed[schemaPath]
	return keys, false, ok
}

func isForbiddenManifestKeyName(key string) bool {
	forbiddenKeys := []string{
		"captureId",
		"captureName",
		"category",
		"code",
		"diagnostic",
		"diagnostics",
		"displayName",
		"fallbackHint",
		"fallbackHints",
		"identityLinkage",
		"mappingId",
		"message",
		"nativeName",
		"nativeType",
		"normalization",
		"normalizationHistory",
		"precedence",
		"resolutionOrigin",
		"semanticId",
		"semanticIds",
		"semanticType",
		"semanticTypeName",
		"semanticTypes",
		"severity",
	}
	for _, forbidden := range forbiddenKeys {
		if strings.EqualFold(key, forbidden) {
			return true
		}
	}
	return false
}

func TestCreatePlanWithTablesAndSemanticModel_CaptureBackedMissingSemanticProductIntentFailsDeterministically(t *testing.T) {
	dslContent := `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param length: number = 35
}
`

	baseModel := &semantic.Model{
		SchemaVersion:           semantic.SchemaVersion,
		SourceDocumentLogicalID: "box_model",
		RootComponentID:         "cmp.root",
		Components: []semantic.Component{
			{ID: "cmp.root", Kind: "assembly", Name: "RootAssembly"},
		},
		Parameters: []semantic.Parameter{
			{ID: "par.root.length", OwnerKind: semantic.OwnerKindComponent, OwnerID: "cmp.root", ComponentID: "cmp.root", Name: "length", NativeType: "Length"},
		},
	}

	firstErr := parseValidateAndPlanCaptureBackedError(t, dslContent, baseModel, testProjectionContract(), false)
	secondErr := parseValidateAndPlanCaptureBackedError(t, dslContent, baseModel, testProjectionContract(), false)
	if firstErr == nil || secondErr == nil {
		t.Fatalf("expected capture-backed planning to fail without semantic product intent, got first=%v second=%v", firstErr, secondErr)
	}
	if firstErr.Error() != secondErr.Error() {
		t.Fatalf("expected deterministic error for missing semantic product intent\nfirst:  %s\nsecond: %s", firstErr.Error(), secondErr.Error())
	}
	if !strings.Contains(firstErr.Error(), "capture-backed manifest generation requires semantic product intent") {
		t.Fatalf("unexpected error: %v", firstErr)
	}
}

func TestCreatePlanWithTablesAndSemanticModel_CaptureBackedNilSemanticMapFailsDeterministically(t *testing.T) {
	dslContent := `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param length: number = 35
}
`

	baseModel := &semantic.Model{
		SchemaVersion:           semantic.SchemaVersion,
		SourceDocumentLogicalID: "box_model",
		RootComponentID:         "cmp.root",
		Components: []semantic.Component{
			{ID: "cmp.root", Kind: "assembly", Name: "RootAssembly"},
		},
		Parameters: []semantic.Parameter{
			{ID: "par.root.length", OwnerKind: semantic.OwnerKindComponent, OwnerID: "cmp.root", ComponentID: "cmp.root", Name: "length", NativeType: "Length"},
		},
	}

	firstErr := parseValidateAndPlanCaptureBackedError(t, dslContent, baseModel, nil, true)
	secondErr := parseValidateAndPlanCaptureBackedError(t, dslContent, baseModel, nil, true)
	if firstErr == nil || secondErr == nil {
		t.Fatalf("expected capture-backed planning to fail without semantic map, got first=%v second=%v", firstErr, secondErr)
	}
	if firstErr.Error() != secondErr.Error() {
		t.Fatalf("expected deterministic error for missing semantic map\nfirst:  %s\nsecond: %s", firstErr.Error(), secondErr.Error())
	}
	if !strings.Contains(firstErr.Error(), "capture-backed manifest generation requires semantic map") {
		t.Fatalf("unexpected error: %v", firstErr)
	}
}

func TestCreatePlan_FreeCADStandaloneNameOnlyParameterAssignmentRejected(t *testing.T) {
	dslContent := `
product Widget {
    adapter = "freecad"
    source_model = "input/widget.FCStd"
    outputs = ["step"]

    param width: number = 120
}
`

	tmpFile, err := os.CreateTemp("", "freecad_standalone_*.dsl")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write([]byte(withDSLVersionHeader(dslContent))); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("failed to close temp file: %v", err)
	}

	ast, err := dsl.Parse(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to parse DSL: %v", err)
	}
	if err := dsl.Validate(ast); err != nil {
		t.Fatalf("DSL validation failed unexpectedly: %v", err)
	}

	_, planErr := CreatePlan(ast, nil)
	if planErr == nil {
		t.Fatal("expected FreeCAD standalone name-only parameter assignment to be rejected")
	}
	if !strings.Contains(planErr.Error(), `"width"`) {
		t.Fatalf("expected rejection error to identify parameter name 'width', got: %v", planErr)
	}
	if !strings.Contains(planErr.Error(), "name-only parameter assignments are not supported") {
		t.Fatalf("expected rejection error to state name-only is not supported, got: %v", planErr)
	}
}

func TestCreatePlanWithTablesAndSemanticModel_CaptureBackedSemanticProjectionRemainsProjected(t *testing.T) {
	dslContent := `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step", "csv"]

    param width: number = 12
    param length: number = 35
    param depth: number = 7
}
`

	baseModel := &semantic.Model{
		SchemaVersion:           semantic.SchemaVersion,
		SourceDocumentLogicalID: "box_model",
		RootComponentID:         "cmp.root",
		Components: []semantic.Component{
			{ID: "cmp.root", Kind: "assembly", Name: "RootAssembly", DisplayName: "Root Assembly"},
		},
		Parameters: []semantic.Parameter{
			{ID: "par.root.width", OwnerKind: semantic.OwnerKindComponent, OwnerID: "cmp.root", ComponentID: "cmp.root", Name: "width", NativeType: "Length"},
			{ID: "par.root.length", OwnerKind: semantic.OwnerKindComponent, OwnerID: "cmp.root", ComponentID: "cmp.root", Name: "length", NativeType: "Length"},
			{ID: "par.root.depth", OwnerKind: semantic.OwnerKindComponent, OwnerID: "cmp.root", ComponentID: "cmp.root", Name: "depth", NativeType: "Length"},
		},
	}

	_, plan := parseValidateAndPlanCaptureBacked(t, dslContent, baseModel, testProjectionContract())
	manifest := manifestPayloadFromPlan(t, plan)

	if len(manifest.ParameterAssignments) == 0 {
		t.Fatal("expected capture-backed manifest parameter assignments")
	}
	if len(manifest.Outputs) == 0 {
		t.Fatal("expected capture-backed manifest outputs")
	}
	if manifest.Outputs[0].Object != "RootAssembly" && manifest.Outputs[0].Target != "RootAssembly" {
		t.Fatalf("expected semantic projection to preserve projected output targeting, got %+v", manifest.Outputs)
	}
}

func TestCreatePlanWithTablesAndSemanticModel_CaptureBackedManifestPayloadContainsExecutionIntentOnly(t *testing.T) {
	dslContent := `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step", "csv"]

    param width: number = 12
    param length: number = 35
    param depth: number = 7
    param finish: string = "Oak"
    param suppress_Keyway: boolean = true
}
`

	contract := testProjectionContract()
	contract.MappingID = "mapping-leak-id"
	contract.SemanticTypes.ParameterTypes["sem-leak-length-type"] = semanticmap.ParameterType{
		ValueKind:   "number",
		DefaultUnit: "mm",
		Coercion:    semantic.CoercionRuleNumberToLength,
	}
	contract.SemanticTypes.ResolutionPolicy.Precedence = []string{"explicit_parameter_mapping", "capture_native_type_mapping"}
	contract.CaptureToSemantic.Parameters[0].SemanticType.Map = map[string]string{
		"LengthNativeLeak": "sem-leak-length-type",
	}

	model := &semantic.Model{
		SchemaVersion:           semantic.SchemaVersion,
		SourceDocumentLogicalID: "box_model",
		RootComponentID:         "sem-leak-root-id",
		Components: []semantic.Component{
			{ID: "sem-leak-root-id", Kind: "assembly", Name: "RootAssembly", DisplayName: "displayName sem-leak root precedence"},
			{ID: "sem-leak-part-id", Kind: "part", Name: "Leg", DisplayName: "displayName sem-leak leg resolution_origin", ParentID: "sem-leak-root-id"},
		},
		Features: []semantic.Feature{
			{ID: "sem-leak-feature-id", ComponentID: "sem-leak-part-id", Name: "Keyway", DisplayName: "feature displayName sem leak", NativeType: "nativeType-leak-feature"},
		},
		Parameters: []semantic.Parameter{
			{ID: "sem-leak-param-id-width", OwnerKind: semantic.OwnerKindComponent, OwnerID: "sem-leak-root-id", ComponentID: "sem-leak-root-id", Name: "width", DisplayName: "width displayName leak", NativeType: "LengthNativeLeak"},
			{ID: "sem-leak-param-id-length", OwnerKind: semantic.OwnerKindComponent, OwnerID: "sem-leak-root-id", ComponentID: "sem-leak-root-id", Name: "length", DisplayName: "length displayName leak", NativeType: "LengthNativeLeak"},
			{ID: "sem-leak-param-id-depth", OwnerKind: semantic.OwnerKindComponent, OwnerID: "sem-leak-root-id", ComponentID: "sem-leak-root-id", Name: "depth", DisplayName: "depth displayName leak", NativeType: "LengthNativeLeak"},
		},
		Metadata: []semantic.Metadata{
			{ID: "sem-leak-metadata-id", OwnerKind: semantic.OwnerKindComponent, OwnerID: "sem-leak-part-id", ComponentID: "sem-leak-part-id", Key: "finish", DisplayName: "metadata displayName leak", NativeType: "nativeType-leak-metadata", ValueType: "string"},
		},
	}

	_, plan := parseValidateAndPlanCaptureBacked(t, dslContent, model, contract)
	manifest := manifestPayloadFromPlan(t, plan)
	assertExecutionOnlyManifestPayload(t, manifest,
		"sem-leak-",
		"cap-leak-",
		"mapping-leak-id",
		"sem-leak-length-type",
		"LengthNativeLeak",
		"displayName",
		"nativeType-leak",
		"coercion_exact_integer_normalized",
		"resolution_origin",
		"precedence",
		"capture.id",
		"capture.name",
		"capture.groupId",
		"capture.nativeType",
		"capture.key",
		"capture.targetability",
		"identity_linkage",
		"fallback",
		"adapterInference",
	)

	foundExecutionFacingOutput := false
	for _, output := range manifest.Outputs {
		if output.Object == "RootAssembly" || output.Target == "RootAssembly" {
			foundExecutionFacingOutput = true
			break
		}
	}
	if !foundExecutionFacingOutput {
		t.Fatalf("expected projected outputs to remain execution-facing, got %+v", manifest.Outputs)
	}
	parameterMutationCount := 0
	propertyMutationCount := 0
	suppressionMutationCount := 0
	if manifest.AssemblyMutations != nil {
		parameterMutationCount += len(manifest.AssemblyMutations.Parameters)
		propertyMutationCount += len(manifest.AssemblyMutations.Properties)
		suppressionMutationCount += len(manifest.AssemblyMutations.Suppression)
	}
	if manifest.PartMutations != nil {
		parameterMutationCount += len(manifest.PartMutations.Parameters)
		propertyMutationCount += len(manifest.PartMutations.Properties)
		suppressionMutationCount += len(manifest.PartMutations.Suppression)
	}
	if parameterMutationCount == 0 || propertyMutationCount == 0 {
		t.Fatalf("expected projected parameter/property mutations in capture-backed manifest, got assembly=%+v part=%+v", manifest.AssemblyMutations, manifest.PartMutations)
	}
	// The retired suppress_ authoring prefix (Task 1) must not project a
	// suppression mutation for an ordinary, unmapped boolean parameter name.
	if suppressionMutationCount != 0 {
		t.Fatalf("expected retired suppress_ prefix parameter to add no suppression mutation, got assembly=%+v part=%+v", manifest.AssemblyMutations, manifest.PartMutations)
	}
}

func TestCreatePlanWithTablesAndSemanticModel_CaptureBackedManifestPayloadJSONIsByteStable(t *testing.T) {
	dslContent := `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step", "csv"]

    param width: number = 12
    param length: number = 35
    param depth: number = 7
}
`

	model := &semantic.Model{
		SchemaVersion:           semantic.SchemaVersion,
		SourceDocumentLogicalID: "box_model",
		RootComponentID:         "cmp.root",
		Components: []semantic.Component{
			{ID: "cmp.root", Kind: "assembly", Name: "RootAssembly", DisplayName: "Root Assembly"},
		},
		Parameters: []semantic.Parameter{
			{ID: "par.root.width", OwnerKind: semantic.OwnerKindComponent, OwnerID: "cmp.root", ComponentID: "cmp.root", Name: "width", NativeType: "Length"},
			{ID: "par.root.length", OwnerKind: semantic.OwnerKindComponent, OwnerID: "cmp.root", ComponentID: "cmp.root", Name: "length", NativeType: "Length"},
			{ID: "par.root.depth", OwnerKind: semantic.OwnerKindComponent, OwnerID: "cmp.root", ComponentID: "cmp.root", Name: "depth", NativeType: "Length"},
		},
	}

	_, firstPlan := parseValidateAndPlanCaptureBacked(t, dslContent, model, testProjectionContract())
	_, secondPlan := parseValidateAndPlanCaptureBacked(t, dslContent, model, testProjectionContract())

	firstJSON := manifestPayloadJSONFromPlan(t, firstPlan)
	secondJSON := manifestPayloadJSONFromPlan(t, secondPlan)
	if firstJSON != secondJSON {
		t.Fatalf("expected byte-identical manifest JSON across repeated capture-backed planning\nfirst:  %s\nsecond: %s", firstJSON, secondJSON)
	}
}

func TestCreatePlanWithTablesAndSemanticModel_CaptureBackedManifestPayloadDoesNotLeakNormalizationDiagnostics(t *testing.T) {
	dslContent := `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param count: number = 35
}
`

	contract := testProjectionContract()
	contract.SemanticTypes.ParameterTypes["sem-leak-count-type"] = semanticmap.ParameterType{
		ValueKind:   "number",
		DefaultUnit: "mm",
		Coercion:    semantic.CoercionRuleNumberToIntegerExact,
	}
	contract.CaptureToSemantic.Parameters[0].SemanticType.Map = map[string]string{
		"CountNativeLeak": "sem-leak-count-type",
	}

	model := &semantic.Model{
		SchemaVersion:           semantic.SchemaVersion,
		SourceDocumentLogicalID: "box_model",
		RootComponentID:         "sem-leak-root-id",
		Components: []semantic.Component{
			{ID: "sem-leak-root-id", Kind: "assembly", Name: "RootAssembly", DisplayName: "displayName count leak"},
		},
		Parameters: []semantic.Parameter{
			{ID: "sem-leak-count-id", OwnerKind: semantic.OwnerKindComponent, OwnerID: "sem-leak-root-id", ComponentID: "sem-leak-root-id", Name: "count", DisplayName: "count displayName leak", NativeType: "CountNativeLeak"},
		},
	}

	_, plan := parseValidateAndPlanCaptureBacked(t, dslContent, model, contract)
	manifest := manifestPayloadFromPlan(t, plan)
	assertExecutionOnlyManifestPayload(t, manifest,
		"coercion_exact_integer_normalized",
		"number_to_integer_exact",
		"diagnostic",
		"severity",
		"message",
		"rule",
		"normalization",
		"displayName",
		"sem-leak-",
		"CountNativeLeak",
	)

	if len(manifest.ParameterAssignments) != 1 {
		t.Fatalf("expected exactly one projected parameter assignment, got %+v", manifest.ParameterAssignments)
	}
	if manifest.ParameterAssignments[0].Value != 35 {
		t.Fatalf("expected normalized execution value to remain projectable, got %+v", manifest.ParameterAssignments[0])
	}
}

func TestCaptureBackedManifest_RejectsOrStripsSemanticFieldInjection(t *testing.T) {
	dslContent := `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step", "csv"]

    param width: number = 12
    param finish: string = "Oak"
}
`

	contract := testProjectionContract()
	contract.MappingID = "injected-mapping-id"
	contract.SemanticTypes.ParameterTypes["injected-semantic-type-length"] = semanticmap.ParameterType{
		ValueKind:   "number",
		DefaultUnit: "mm",
		Coercion:    semantic.CoercionRuleNumberToLength,
	}
	contract.SemanticTypes.ResolutionPolicy.Precedence = []string{"explicit_parameter_mapping", "capture_native_type_mapping"}
	contract.CaptureToSemantic.Parameters[0].SemanticType.Map = map[string]string{
		"InjectedNativeLength": "injected-semantic-type-length",
	}

	model := &semantic.Model{
		SchemaVersion:           semantic.SchemaVersion,
		SourceDocumentLogicalID: "box_model",
		RootComponentID:         "injected-semantic-root-id",
		Components: []semantic.Component{
			{ID: "injected-semantic-root-id", Kind: "assembly", Name: "RootAssembly", DisplayName: "injected-display-name"},
			{ID: "injected-precedence-trace", Kind: "part", Name: "Leg", DisplayName: "injected-resolution-origin", ParentID: "injected-semantic-root-id"},
		},
		Features: []semantic.Feature{
			{ID: "injected-capture-id", ComponentID: "injected-precedence-trace", Name: "Keyway", DisplayName: "injected-adapter-inference", NativeType: "injected-native-type"},
		},
		Parameters: []semantic.Parameter{
			{ID: "injected-semantic-id", OwnerKind: semantic.OwnerKindComponent, OwnerID: "injected-semantic-root-id", ComponentID: "injected-semantic-root-id", Name: "width", DisplayName: "injected-fallback-hint", NativeType: "InjectedNativeLength"},
		},
		Metadata: []semantic.Metadata{
			{ID: "injected-normalization-history", OwnerKind: semantic.OwnerKindComponent, OwnerID: "injected-precedence-trace", ComponentID: "injected-precedence-trace", Key: "finish", DisplayName: "injected-coercion-diagnostic", NativeType: "injected-native-type", ValueType: "string"},
		},
	}

	_, plan := parseValidateAndPlanCaptureBacked(t, dslContent, model, contract)
	manifest := manifestPayloadFromPlan(t, plan)
	manifestJSON := manifestPayloadJSONFromPlan(t, plan)

	assertExecutionOnlyManifestPayload(t, manifest,
		"injected-semantic-id",
		"injected-capture-id",
		"injected-mapping-id",
		"injected-semantic-type-length",
		"injected-display-name",
		"injected-native-type",
		"injected-coercion-diagnostic",
		"injected-normalization-history",
		"injected-resolution-origin",
		"injected-precedence-trace",
		"injected-adapter-inference",
		"injected-fallback-hint",
	)

	for _, forbidden := range []string{
		"injected-semantic-id",
		"injected-capture-id",
		"injected-mapping-id",
		"injected-semantic-type-length",
		"injected-display-name",
		"injected-native-type",
		"injected-coercion-diagnostic",
		"injected-normalization-history",
		"injected-resolution-origin",
		"injected-precedence-trace",
		"injected-adapter-inference",
		"injected-fallback-hint",
	} {
		if strings.Contains(manifestJSON, forbidden) {
			t.Fatalf("expected manifest JSON to reject or strip injected semantic metadata %q, got %s", forbidden, manifestJSON)
		}
	}
}

func TestCollectExecutionOnlyManifestPayloadProblems_IsDeterministic(t *testing.T) {
	firstPayload := WriteExportManifestPayload{
		SchemaVersion: ExportManifestSchemaVersion,
		PlanHash:      "plan-hash",
		Adapter:       "freecad",
		Product:       ExportManifestProduct{ID: "Box"},
		Inputs:        ExportManifestInputs{SourceModel: "box_model"},
		Values: map[string]any{
			"width": map[string]any{
				"displayName": "displayName sem leak",
				"metaB":       "sem-leak-value-b",
			},
			"depth": map[string]any{
				"metaA": "sem-leak-value-a",
			},
		},
		ParameterAssignments: []ExportManifestParameterAssignment{
			{Name: "width", Value: 12, Type: "number", Unit: "mm"},
		},
		Outputs: []ExportManifestOutput{
			{Type: "step", Filename: "Box.step", Object: "RootAssembly"},
		},
	}
	secondPayload := WriteExportManifestPayload{
		SchemaVersion: ExportManifestSchemaVersion,
		PlanHash:      "plan-hash",
		Adapter:       "freecad",
		Product:       ExportManifestProduct{ID: "Box"},
		Inputs:        ExportManifestInputs{SourceModel: "box_model"},
		Values: map[string]any{
			"depth": map[string]any{
				"metaA": "sem-leak-value-a",
			},
			"width": map[string]any{
				"metaB":       "sem-leak-value-b",
				"displayName": "displayName sem leak",
			},
		},
		ParameterAssignments: []ExportManifestParameterAssignment{
			{Name: "width", Value: 12, Type: "number", Unit: "mm"},
		},
		Outputs: []ExportManifestOutput{
			{Type: "step", Filename: "Box.step", Object: "RootAssembly"},
		},
	}

	_, firstProblems := collectExecutionOnlyManifestPayloadProblems(firstPayload, "displayName", "sem-leak-")
	_, secondProblems := collectExecutionOnlyManifestPayloadProblems(secondPayload, "displayName", "sem-leak-")

	if !reflect.DeepEqual(firstProblems, secondProblems) {
		t.Fatalf("expected deterministic leakage diagnostics\nfirst:  %#v\nsecond: %#v", firstProblems, secondProblems)
	}

	wantProblems := []string{
		"$.values.depth.metaA: string value \"sem-leak-value-a\" contains forbidden sentinel \"sem-leak-\"",
		"$.values.width.displayName: forbidden key \"displayName\"",
		"$.values.width.displayName: key \"displayName\" contains forbidden sentinel \"displayName\"",
		"$.values.width.displayName: string value \"displayName sem leak\" contains forbidden sentinel \"displayName\"",
		"$.values.width.metaB: string value \"sem-leak-value-b\" contains forbidden sentinel \"sem-leak-\"",
	}
	if !reflect.DeepEqual(firstProblems, wantProblems) {
		t.Fatalf("unexpected deterministic leakage diagnostics\nwant: %#v\ngot:  %#v", wantProblems, firstProblems)
	}
}

func TestCreatePlanWithTablesAndSemanticModel_CaptureBackedManifestPayloadIgnoresSemanticInputOrderAndMapConstructionOrder(t *testing.T) {
	dslContent := `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step", "csv"]

    param width: number = 12
    param length: number = 35
    param depth: number = 7
}
`

	firstContract := testProjectionContract()
	secondContract := testProjectionContract()
	secondContract.SemanticTypes.ParameterTypes = map[string]semanticmap.ParameterType{}
	secondContract.SemanticTypes.ParameterTypes["length"] = semanticmap.ParameterType{
		ValueKind:   "number",
		DefaultUnit: "mm",
		Coercion:    semantic.CoercionRuleNumberToLength,
	}
	secondContract.SemanticToManifest.Outputs = map[string]semanticmap.OutputManifestMapping{}
	secondContract.SemanticToManifest.Outputs["step"] = semanticmap.OutputManifestMapping{ManifestType: "step", TargetField: "object", TargetSource: "semantic.output.target.manifestName"}
	secondContract.SemanticToManifest.Outputs["pdf"] = semanticmap.OutputManifestMapping{ManifestType: "pdf"}
	secondContract.SemanticToManifest.Outputs["csv"] = semanticmap.OutputManifestMapping{ManifestType: "csv", TargetField: "target", TargetSource: "semantic.output.target.manifestName", ScopeField: "rollup", ScopeMap: map[string]string{"subtree": "assembly", "assembly": "assembly", "flat": "flat"}}
	secondContract.SemanticToManifest.Mutations = map[string]semanticmap.MutationMapping{}
	secondContract.SemanticToManifest.Mutations["suppress"] = semanticmap.MutationMapping{ManifestCollection: "partMutations.suppression", TargetField: "object", ValueField: "suppressed", Value: boolPtr(true)}
	secondContract.SemanticToManifest.Mutations["set_property"] = semanticmap.MutationMapping{ManifestCollection: "partMutations.properties", TargetField: "object", PropertyField: "property", ValueField: "value"}
	secondContract.SemanticToManifest.Mutations["set_parameter"] = semanticmap.MutationMapping{ManifestCollection: "partMutations.parameters", TargetField: "object", PropertyField: "property", ValueField: "valueParam"}

	firstModel := &semantic.Model{
		SchemaVersion:           semantic.SchemaVersion,
		SourceDocumentLogicalID: "box_model",
		RootComponentID:         "cmp.root",
		Components: []semantic.Component{
			{ID: "cmp.root", Kind: "assembly", Name: "RootAssembly", DisplayName: "Root Assembly"},
		},
		Parameters: []semantic.Parameter{
			{ID: "par.root.width", OwnerKind: semantic.OwnerKindComponent, OwnerID: "cmp.root", ComponentID: "cmp.root", Name: "width", NativeType: "Length"},
			{ID: "par.root.length", OwnerKind: semantic.OwnerKindComponent, OwnerID: "cmp.root", ComponentID: "cmp.root", Name: "length", NativeType: "Length"},
			{ID: "par.root.depth", OwnerKind: semantic.OwnerKindComponent, OwnerID: "cmp.root", ComponentID: "cmp.root", Name: "depth", NativeType: "Length"},
		},
	}
	secondModel := &semantic.Model{
		SchemaVersion:           semantic.SchemaVersion,
		SourceDocumentLogicalID: "box_model",
		RootComponentID:         "cmp.root",
		Components: []semantic.Component{
			{ID: "cmp.root", Kind: "assembly", Name: "RootAssembly", DisplayName: "Root Assembly"},
		},
		Parameters: []semantic.Parameter{
			{ID: "par.root.depth", OwnerKind: semantic.OwnerKindComponent, OwnerID: "cmp.root", ComponentID: "cmp.root", Name: "depth", NativeType: "Length"},
			{ID: "par.root.width", OwnerKind: semantic.OwnerKindComponent, OwnerID: "cmp.root", ComponentID: "cmp.root", Name: "width", NativeType: "Length"},
			{ID: "par.root.length", OwnerKind: semantic.OwnerKindComponent, OwnerID: "cmp.root", ComponentID: "cmp.root", Name: "length", NativeType: "Length"},
		},
	}

	_, firstPlan := parseValidateAndPlanCaptureBacked(t, dslContent, firstModel, firstContract)
	_, secondPlan := parseValidateAndPlanCaptureBacked(t, dslContent, secondModel, secondContract)

	firstJSON := manifestPayloadJSONFromPlan(t, firstPlan)
	secondJSON := manifestPayloadJSONFromPlan(t, secondPlan)
	if firstJSON != secondJSON {
		t.Fatalf("expected identical manifest JSON across semantic input/map order changes\nfirst:  %s\nsecond: %s", firstJSON, secondJSON)
	}
}

func TestCreatePlan_FilePatternInvalidPlaceholder(t *testing.T) {
	dslContent := `
profile Prod {
    file_pattern = "{unknown}"
}
use profile Prod

product Widget {
    param width: number = 120
}
`

	tmpFile, err := os.CreateTemp("", "file_pattern_invalid_*.dsl")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write([]byte(withDSLVersionHeader(dslContent))); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	ast, err := dsl.Parse(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to parse DSL: %v", err)
	}
	if err := dsl.Validate(ast); err != nil {
		t.Fatalf("Failed to validate AST: %v", err)
	}

	_, err = CreatePlan(ast, nil)
	if err == nil {
		t.Fatal("expected CreatePlan to fail for invalid placeholder")
	}
	if !strings.Contains(err.Error(), "unknown placeholder") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreatePlan_FilePatternMissingParam(t *testing.T) {
	dslContent := `
profile Prod {
    file_pattern = "{product}_{param:missing}"
}
use profile Prod

product Widget {
    param width: number = 120
}
`

	tmpFile, err := os.CreateTemp("", "file_pattern_missing_param_*.dsl")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write([]byte(withDSLVersionHeader(dslContent))); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	ast, err := dsl.Parse(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to parse DSL: %v", err)
	}
	if err := dsl.Validate(ast); err != nil {
		t.Fatalf("Failed to validate AST: %v", err)
	}

	_, err = CreatePlan(ast, nil)
	if err == nil {
		t.Fatal("expected CreatePlan to fail for missing param in file_pattern")
	}
	if !strings.Contains(err.Error(), "unknown param 'missing'") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreatePlan_FilePatternConstPlaceholder_ResolvedAtPlannerStage(t *testing.T) {
	dslContent := `
const TAG = "r5"

profile Prod {
    file_pattern = "{profile}_{product}_{const:TAG}_{param:width}"
}
use profile Prod

product Widget {
    param width: number = 120
}
`

	_, plan := parseValidateAndPlan(t, dslContent)
	if len(plan.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(plan.Steps))
	}

	writePayload, ok := plan.Steps[0].Payload.(WriteCSVPayload)
	if !ok {
		t.Fatalf("expected WriteCSVPayload, got %T", plan.Steps[0].Payload)
	}
	manifestPayload, ok := plan.Steps[1].Payload.(WriteExportManifestPayload)
	if !ok {
		t.Fatalf("expected WriteExportManifestPayload, got %T", plan.Steps[1].Payload)
	}
	if writePayload.Filename != "Prod_Widget_r5_120.csv" {
		t.Fatalf("unexpected csv filename: %q", writePayload.Filename)
	}
	if strings.Contains(writePayload.Filename, "{const:") {
		t.Fatalf("expected resolved filename, got %q", writePayload.Filename)
	}
	if len(manifestPayload.Outputs) != 1 || manifestPayload.Outputs[0].Filename != "Prod_Widget_r5_120.step" {
		t.Fatalf("unexpected manifest output filename: %#v", manifestPayload.Outputs)
	}
}

func TestCreatePlan_FilePatternConstPlaceholder_MissingConst(t *testing.T) {
	dslContent := `
profile Prod {
    file_pattern = "{product}_{const:MISSING}"
}
use profile Prod

product Widget {
    param width: number = 120
}
`

	tmpFile, err := os.CreateTemp("", "file_pattern_missing_const_*.dsl")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write([]byte(withDSLVersionHeader(dslContent))); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	ast, err := dsl.Parse(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to parse DSL: %v", err)
	}
	if err := dsl.Validate(ast); err != nil {
		t.Fatalf("Failed to validate AST: %v", err)
	}

	_, err = CreatePlan(ast, nil)
	if err == nil {
		t.Fatal("expected CreatePlan to fail for missing const in file_pattern")
	}
	if !strings.Contains(err.Error(), "unknown const 'MISSING'") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreatePlan_FilePatternConstPlaceholder_CaseSensitive(t *testing.T) {
	dslContent := `
const FILE_PREFIX = "x"

profile Prod {
    file_pattern = "{const:file_prefix}_{product}"
}
use profile Prod

product Widget {
    param width: number = 120
}
`

	tmpFile, err := os.CreateTemp("", "file_pattern_const_case_sensitive_*.dsl")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write([]byte(withDSLVersionHeader(dslContent))); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	ast, err := dsl.Parse(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to parse DSL: %v", err)
	}
	if err := dsl.Validate(ast); err != nil {
		t.Fatalf("Failed to validate AST: %v", err)
	}

	_, err = CreatePlan(ast, nil)
	if err == nil {
		t.Fatal("expected CreatePlan to fail for case-mismatched const in file_pattern")
	}
	if !strings.Contains(err.Error(), "unknown const 'file_prefix'") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreatePlan_FilePatternConstPlaceholder_NonStringConst(t *testing.T) {
	dslContent := `
const COUNT = 7

profile Prod {
    file_pattern = "{product}_{const:COUNT}"
}
use profile Prod

product Widget {
    param width: number = 120
}
`

	tmpFile, err := os.CreateTemp("", "file_pattern_non_string_const_*.dsl")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write([]byte(withDSLVersionHeader(dslContent))); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	ast, err := dsl.Parse(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to parse DSL: %v", err)
	}
	if err := dsl.Validate(ast); err != nil {
		t.Fatalf("Failed to validate AST: %v", err)
	}

	_, err = CreatePlan(ast, nil)
	if err == nil {
		t.Fatal("expected CreatePlan to fail for non-string const in file_pattern")
	}
	if !strings.Contains(err.Error(), "must resolve to string") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreatePlan_FilePatternConstPlaceholder_EmptyStringConstant(t *testing.T) {
	dslContent := `
const PREFIX = ""

profile Dev {
    file_pattern = "{const:PREFIX}_{product}"
}
use profile Dev

product ProductA {
    param x: number = 1
}

product ProductB {
    param x: number = 2
}
`

	_, plan := parseValidateAndPlan(t, dslContent)
	if len(plan.Steps) != 4 {
		t.Fatalf("expected 4 steps, got %d", len(plan.Steps))
	}

	w0, ok := plan.Steps[0].Payload.(WriteCSVPayload)
	if !ok {
		t.Fatalf("expected WriteCSVPayload at step 0, got %T", plan.Steps[0].Payload)
	}
	w1, ok := plan.Steps[2].Payload.(WriteCSVPayload)
	if !ok {
		t.Fatalf("expected WriteCSVPayload at step 2, got %T", plan.Steps[2].Payload)
	}

	if w0.Filename != "_ProductA.csv" {
		t.Fatalf("unexpected ProductA filename: %q", w0.Filename)
	}
	if w1.Filename != "_ProductB.csv" {
		t.Fatalf("unexpected ProductB filename: %q", w1.Filename)
	}
	if w0.Filename == w1.Filename {
		t.Fatalf("expected distinct filenames, got %q", w0.Filename)
	}
}

func TestCreatePlan_FilePatternMustBeStringLiteralOrStringConstant(t *testing.T) {
	tests := []struct {
		name                   string
		dslContent             string
		validateErrorContains  string
		createPlanErrorContain string
	}{
		{
			name: "number literal rejected",
			dslContent: `
profile Prod {
    file_pattern = 42
}
use profile Prod

product Widget {
    param width: number = 120
}
`,
			createPlanErrorContain: "file_pattern",
		},
		{
			name: "enum-like identifier rejected",
			dslContent: `
profile Prod {
    file_pattern = SymbolicName
}
use profile Prod

product Widget {
    param width: number = 120
}
`,
			validateErrorContains: "undefined constant: SymbolicName",
		},
		{
			name: "non-string constant rejected",
			dslContent: `
const PATTERN = 42

profile Prod {
    file_pattern = PATTERN
}
use profile Prod

product Widget {
    param width: number = 120
}
`,
			createPlanErrorContain: "file_pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile, err := os.CreateTemp("", "file_pattern_string_rule_*.dsl")
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}
			defer os.Remove(tmpFile.Name())
			if _, err := tmpFile.Write([]byte(withDSLVersionHeader(tt.dslContent))); err != nil {
				t.Fatalf("Failed to write temp file: %v", err)
			}
			if err := tmpFile.Close(); err != nil {
				t.Fatalf("Failed to close temp file: %v", err)
			}

			ast, err := dsl.Parse(tmpFile.Name())
			if err != nil {
				t.Fatalf("Failed to parse DSL: %v", err)
			}

			err = dsl.Validate(ast)
			if tt.validateErrorContains != "" {
				if err == nil {
					t.Fatal("expected validation to fail")
				}
				if !strings.Contains(err.Error(), tt.validateErrorContains) {
					t.Fatalf("unexpected validation error: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Failed to validate AST: %v", err)
			}

			_, err = CreatePlan(ast, nil)
			if err == nil {
				t.Fatal("expected CreatePlan to fail")
			}
			if !strings.Contains(err.Error(), tt.createPlanErrorContain) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestCreatePlan_FilePattern_PathSafety(t *testing.T) {
	tests := []struct {
		name         string
		pattern      string
		errorContain string
	}{
		{
			name:         "rejects traversal",
			pattern:      "../pwned_{product}",
			errorContain: "must not contain path separators",
		},
		{
			name:         "rejects subdir separator",
			pattern:      "subdir/{product}",
			errorContain: "must not contain path separators",
		},
		{
			name:         "rejects absolute path",
			pattern:      "/tmp/{product}",
			errorContain: "must be relative",
		},
		{
			name:         "rejects windows separator literal",
			pattern:      "A\\\\B.csv",
			errorContain: "must not contain path separators",
		},
		{
			name:         "rejects windows separator placeholder path",
			pattern:      "{product}\\\\{plan_hash}.csv",
			errorContain: "must not contain path separators",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dslContent := `
profile Prod {
    file_pattern = "` + tt.pattern + `"
}
use profile Prod

product Widget {
    param width: number = 120
}
`

			tmpFile, err := os.CreateTemp("", "file_pattern_path_safety_*.dsl")
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}
			defer os.Remove(tmpFile.Name())
			if _, err := tmpFile.Write([]byte(withDSLVersionHeader(dslContent))); err != nil {
				t.Fatalf("Failed to write temp file: %v", err)
			}
			if err := tmpFile.Close(); err != nil {
				t.Fatalf("Failed to close temp file: %v", err)
			}

			ast, err := dsl.Parse(tmpFile.Name())
			if err != nil {
				t.Fatalf("Failed to parse DSL: %v", err)
			}
			if err := dsl.Validate(ast); err != nil {
				t.Fatalf("Failed to validate AST: %v", err)
			}

			_, err = CreatePlan(ast, nil)
			if err == nil {
				t.Fatal("expected CreatePlan to fail for unsafe file_pattern output")
			}
			if !strings.Contains(err.Error(), tt.errorContain) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestCreatePlan_FilePatternPlanHashPlaceholder_Deterministic(t *testing.T) {
	dslContent := `
profile Prod {
    file_pattern = "{plan_hash}_{product}"
}
use profile Prod

product Widget {
    param width: number = 120
}
`

	ast1, plan1 := parseValidateAndPlan(t, dslContent)
	ast2, plan2 := parseValidateAndPlan(t, dslContent)

	if ast1.ActiveProfileName == nil || ast2.ActiveProfileName == nil {
		t.Fatalf("expected active profile for both ASTs")
	}

	if len(plan1.Steps) != 2 || len(plan2.Steps) != 2 {
		t.Fatalf("expected two steps in each plan")
	}

	p1, ok := plan1.Steps[0].Payload.(WriteCSVPayload)
	if !ok {
		t.Fatalf("expected first plan step payload type WriteCSVPayload, got %T", plan1.Steps[0].Payload)
	}
	p2, ok := plan2.Steps[0].Payload.(WriteCSVPayload)
	if !ok {
		t.Fatalf("expected second plan step payload type WriteCSVPayload, got %T", plan2.Steps[0].Payload)
	}

	if p1.Filename != p2.Filename {
		t.Fatalf("expected deterministic filename across repeated runs, got %q and %q", p1.Filename, p2.Filename)
	}

	if !strings.HasSuffix(p1.Filename, "_Widget.csv") {
		t.Fatalf("expected plan-hash-prefixed filename ending with _Widget.csv, got %q", p1.Filename)
	}
}

func TestPlanner_ParameterAssignments_DeterministicOrdering(t *testing.T) {
	dslContent := `
product Widget {
    param width: number = 120
    param label: string = "box"
    param height: number = width / 2
    param enabled: boolean = true
    param depth: number = height + 5
}
`

	_, plan1 := parseValidateAndPlan(t, dslContent)
	_, plan2 := parseValidateAndPlan(t, dslContent)

	if len(plan1.Steps) < 2 || len(plan2.Steps) < 2 {
		t.Fatalf("expected manifest step in both plans")
	}

	manifest1, ok := plan1.Steps[1].Payload.(WriteExportManifestPayload)
	if !ok {
		t.Fatalf("expected first manifest payload, got %T", plan1.Steps[1].Payload)
	}
	manifest2, ok := plan2.Steps[1].Payload.(WriteExportManifestPayload)
	if !ok {
		t.Fatalf("expected second manifest payload, got %T", plan2.Steps[1].Payload)
	}

	assignmentsJSON1, err := json.Marshal(manifest1.ParameterAssignments)
	if err != nil {
		t.Fatalf("failed to marshal first parameterAssignments: %v", err)
	}
	assignmentsJSON2, err := json.Marshal(manifest2.ParameterAssignments)
	if err != nil {
		t.Fatalf("failed to marshal second parameterAssignments: %v", err)
	}

	if !reflect.DeepEqual(manifest1.ParameterAssignments, manifest2.ParameterAssignments) {
		t.Fatalf("parameterAssignments changed across repeated planning runs\nfirst:  %+v\nsecond: %+v", manifest1.ParameterAssignments, manifest2.ParameterAssignments)
	}
	if string(assignmentsJSON1) != string(assignmentsJSON2) {
		t.Fatalf("parameterAssignments JSON changed across repeated planning runs\nfirst:  %s\nsecond: %s", assignmentsJSON1, assignmentsJSON2)
	}

	expected := []ExportManifestParameterAssignment{
		{Name: "width", Value: 120.0, Type: "number", Unit: "mm"},
		{Name: "height", Value: 60.0, Type: "number", Unit: "mm"},
		{Name: "depth", Value: 65.0, Type: "number", Unit: "mm"},
	}
	if !reflect.DeepEqual(manifest1.ParameterAssignments, expected) {
		t.Fatalf("unexpected parameterAssignments ordering/content: %+v", manifest1.ParameterAssignments)
	}
}

func TestCaptureBackedProjection_ByteStableManifestParameterAssignments(t *testing.T) {
	dslContent := `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param width: number = 12
    param length: number = 35
    param depth: number = 7
}
`

	baseModel := &semantic.Model{
		SchemaVersion:           semantic.SchemaVersion,
		SourceDocumentLogicalID: "box_model",
		RootComponentID:         "cmp.root",
		Components: []semantic.Component{
			{
				ID:          "cmp.root",
				Kind:        "assembly",
				Name:        "RootAssembly",
				DisplayName: "Root Assembly",
			},
		},
		Parameters: []semantic.Parameter{
			{ID: "par.root.width", OwnerKind: semantic.OwnerKindComponent, OwnerID: "cmp.root", ComponentID: "cmp.root", Name: "width", NativeType: "Length"},
			{ID: "par.root.length", OwnerKind: semantic.OwnerKindComponent, OwnerID: "cmp.root", ComponentID: "cmp.root", Name: "length", NativeType: "Length"},
			{ID: "par.root.depth", OwnerKind: semantic.OwnerKindComponent, OwnerID: "cmp.root", ComponentID: "cmp.root", Name: "depth", NativeType: "Length"},
		},
	}

	_, firstPlan := parseValidateAndPlanCaptureBacked(t, dslContent, baseModel, testProjectionContract())
	_, secondPlan := parseValidateAndPlanCaptureBacked(t, dslContent, baseModel, testProjectionContract())

	firstManifest := manifestPayloadFromPlan(t, firstPlan)
	secondManifest := manifestPayloadFromPlan(t, secondPlan)

	firstJSON, err := json.Marshal(firstManifest.ParameterAssignments)
	if err != nil {
		t.Fatalf("failed to marshal first parameterAssignments: %v", err)
	}
	secondJSON, err := json.Marshal(secondManifest.ParameterAssignments)
	if err != nil {
		t.Fatalf("failed to marshal second parameterAssignments: %v", err)
	}

	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("expected byte-stable capture-backed parameterAssignments JSON\nfirst:  %s\nsecond: %s", firstJSON, secondJSON)
	}

	want := []ExportManifestParameterAssignment{
		{Name: "depth", Target: "RootAssembly.depth", Value: 7.0, Type: "number", Unit: "mm"},
		{Name: "length", Target: "RootAssembly.length", Value: 35.0, Type: "number", Unit: "mm"},
		{Name: "width", Target: "RootAssembly.width", Value: 12.0, Type: "number", Unit: "mm"},
	}
	if !reflect.DeepEqual(firstManifest.ParameterAssignments, want) {
		t.Fatalf("unexpected capture-backed parameterAssignments ordering/content: %+v", firstManifest.ParameterAssignments)
	}

	if firstManifest.ParameterAssignments[0].Name == "width" {
		t.Fatalf("capture-backed parameterAssignments followed DSL declaration order instead of projection ordering: %+v", firstManifest.ParameterAssignments)
	}
}

func TestCaptureBackedProjection_CarriesVerificationParameterIDsWithoutLeakingToManifestJSON(t *testing.T) {
	dslContent := `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param width: number = 12
    param length: number = 35
}
`

	model := &semantic.Model{
		SchemaVersion:           semantic.SchemaVersion,
		SourceDocumentLogicalID: "box_model",
		RootComponentID:         "cmp.root",
		Components: []semantic.Component{
			{ID: "cmp.root", Kind: "assembly", Name: "RootAssembly", DisplayName: "Root Assembly"},
		},
		ParameterGroups: []semantic.ParameterGroup{
			{ID: "grp.root.dimensions", OwnerComponentID: "cmp.root", Name: "Dimensions", DisplayName: "Dimensions", GroupKind: "parameter_group", NativeType: "App::VarSet", Observable: true, Writable: true},
		},
		Parameters: []semantic.Parameter{
			{ID: "par.root.width", OwnerKind: semantic.OwnerKindGroup, OwnerID: "grp.root.dimensions", ComponentID: "cmp.root", GroupID: "grp.root.dimensions", Name: "width", NativeType: "Length"},
			{ID: "par.root.length", OwnerKind: semantic.OwnerKindGroup, OwnerID: "grp.root.dimensions", ComponentID: "cmp.root", GroupID: "grp.root.dimensions", Name: "length", NativeType: "Length"},
		},
	}

	_, plan := parseValidateAndPlanCaptureBacked(t, dslContent, model, testProjectionContract())
	manifest := manifestPayloadFromPlan(t, plan)

	wantExpected := []VerificationExpectedParameter{
		{ID: "par.root.length", Name: "length", Value: 35, Type: "number", Unit: "mm"},
		{ID: "par.root.width", Name: "width", Value: 12, Type: "number", Unit: "mm"},
	}
	if !reflect.DeepEqual(manifest.Verification.ExpectedParameters, wantExpected) {
		t.Fatalf("unexpected verification expected parameters: %+v", manifest.Verification.ExpectedParameters)
	}

	wantBindings := []VerificationObservationParameterLink{
		{ID: "par.root.length", Name: "length", GroupName: "Dimensions"},
		{ID: "par.root.width", Name: "width", GroupName: "Dimensions"},
	}
	if !reflect.DeepEqual(manifest.Verification.ObservationParameterLinks, wantBindings) {
		t.Fatalf("unexpected verification parameter bindings: %+v", manifest.Verification.ObservationParameterLinks)
	}

	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("failed to marshal manifest: %v", err)
	}
	text := string(data)
	for _, forbidden := range []string{"par.root.length", "par.root.width"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("manifest JSON leaked verification-only identity %q: %s", forbidden, text)
		}
	}
}

func TestCreatePlan_ExportedPublicSurfacesExcludeLetBindings(t *testing.T) {
	dslContent := `
product Demo {
    let base = 10
    let doubled = base * 2
    param width: number = doubled
    param height: number = width + 5
}
`

	_, plan := parseValidateAndPlan(t, dslContent)
	if len(plan.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(plan.Steps))
	}

	writePayload, ok := plan.Steps[0].Payload.(WriteCSVPayload)
	if !ok {
		t.Fatalf("expected WriteCSVPayload, got %T", plan.Steps[0].Payload)
	}
	if want := []string{"width", "height"}; !reflect.DeepEqual(writePayload.Headers, want) {
		t.Fatalf("unexpected CSV headers: %+v", writePayload.Headers)
	}
	if want := []interface{}{20.0, 25.0}; !reflect.DeepEqual(writePayload.Values, want) {
		t.Fatalf("unexpected CSV values: %+v", writePayload.Values)
	}

	manifestPayload, ok := plan.Steps[1].Payload.(WriteExportManifestPayload)
	if !ok {
		t.Fatalf("expected WriteExportManifestPayload, got %T", plan.Steps[1].Payload)
	}

	expectedValues := map[string]interface{}{
		"width":  20.0,
		"height": 25.0,
	}
	if !reflect.DeepEqual(manifestPayload.Values, expectedValues) {
		t.Fatalf("unexpected manifest values: %+v", manifestPayload.Values)
	}
	if _, ok := manifestPayload.Values["base"]; ok {
		t.Fatalf("manifest values unexpectedly included let binding 'base': %+v", manifestPayload.Values)
	}
	if _, ok := manifestPayload.Values["doubled"]; ok {
		t.Fatalf("manifest values unexpectedly included let binding 'doubled': %+v", manifestPayload.Values)
	}

	expectedAssignments := []ExportManifestParameterAssignment{
		{Name: "width", Value: 20.0, Type: "number", Unit: "mm"},
		{Name: "height", Value: 25.0, Type: "number", Unit: "mm"},
	}
	if !reflect.DeepEqual(manifestPayload.ParameterAssignments, expectedAssignments) {
		t.Fatalf("unexpected parameter assignments: %+v", manifestPayload.ParameterAssignments)
	}
	for _, assignment := range manifestPayload.ParameterAssignments {
		if assignment.Name == "base" || assignment.Name == "doubled" {
			t.Fatalf("parameterAssignments unexpectedly included let binding: %+v", manifestPayload.ParameterAssignments)
		}
	}
}

func TestCreatePlan_ExportedPublicSurfacesRemainDeterministicWithLetBindings(t *testing.T) {
	dslContent := `
product Demo {
    let base = 10
    let doubled = base * 2
    param width: number = doubled
    param height: number = width + 5
}
`

	_, firstPlan := parseValidateAndPlan(t, dslContent)
	_, secondPlan := parseValidateAndPlan(t, dslContent)

	firstCSV, ok := firstPlan.Steps[0].Payload.(WriteCSVPayload)
	if !ok {
		t.Fatalf("expected first CSV payload, got %T", firstPlan.Steps[0].Payload)
	}
	secondCSV, ok := secondPlan.Steps[0].Payload.(WriteCSVPayload)
	if !ok {
		t.Fatalf("expected second CSV payload, got %T", secondPlan.Steps[0].Payload)
	}
	if !reflect.DeepEqual(firstCSV, secondCSV) {
		t.Fatalf("exported CSV payload changed across repeated planning runs\nfirst:  %+v\nsecond: %+v", firstCSV, secondCSV)
	}

	firstManifest, ok := firstPlan.Steps[1].Payload.(WriteExportManifestPayload)
	if !ok {
		t.Fatalf("expected first manifest payload, got %T", firstPlan.Steps[1].Payload)
	}
	secondManifest, ok := secondPlan.Steps[1].Payload.(WriteExportManifestPayload)
	if !ok {
		t.Fatalf("expected second manifest payload, got %T", secondPlan.Steps[1].Payload)
	}
	if !reflect.DeepEqual(firstManifest.Values, secondManifest.Values) {
		t.Fatalf("manifest values changed across repeated planning runs\nfirst:  %+v\nsecond: %+v", firstManifest.Values, secondManifest.Values)
	}
	if !reflect.DeepEqual(firstManifest.ParameterAssignments, secondManifest.ParameterAssignments) {
		t.Fatalf("parameterAssignments changed across repeated planning runs\nfirst:  %+v\nsecond: %+v", firstManifest.ParameterAssignments, secondManifest.ParameterAssignments)
	}
}

func TestCreatePlan_FilePattern_DoesNotDuplicateExtensions(t *testing.T) {
	tests := []struct {
		name         string
		pattern      string
		wantFilename string
	}{
		{
			name:         "csv suffix in pattern is normalized",
			pattern:      "{profile}_{product}_{param:x}.csv",
			wantFilename: "Dev_A_1.csv",
		},
		{
			name:         "step suffix in pattern is normalized",
			pattern:      "{profile}_{product}_{param:x}.step",
			wantFilename: "Dev_A_1.csv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dslContent := `
profile Dev {
    file_pattern = "` + tt.pattern + `"
}
use profile Dev

product A {
    param x: number = 1
}
`
			ast, plan := parseValidateAndPlan(t, dslContent)
			if ast.ActiveProfileName == nil || *ast.ActiveProfileName != "Dev" {
				t.Fatalf("expected active profile Dev")
			}
			if len(plan.Steps) != 2 {
				t.Fatalf("expected 2 steps, got %d", len(plan.Steps))
			}

			writePayload, ok := plan.Steps[0].Payload.(WriteCSVPayload)
			if !ok {
				t.Fatalf("expected WriteCSVPayload, got %T", plan.Steps[0].Payload)
			}
			if writePayload.Filename != tt.wantFilename {
				t.Fatalf("unexpected filename: got %q want %q", writePayload.Filename, tt.wantFilename)
			}
		})
	}
}

func TestCreatePlan_FilePattern_DoesNotChangeProductKey(t *testing.T) {
	dslContent := `
profile Dev {
    file_pattern = "{profile}_{product}_{plan_hash}_{param:x}.csv"
}
use profile Dev

product A {
    param x: number = 1
}

product B {
    param x: number = 2
}
`

	_, plan := parseValidateAndPlan(t, dslContent)
	if len(plan.Steps) != 4 {
		t.Fatalf("expected 4 steps, got %d", len(plan.Steps))
	}

	w0, ok := plan.Steps[0].Payload.(WriteCSVPayload)
	if !ok {
		t.Fatalf("expected WriteCSVPayload at step 0, got %T", plan.Steps[0].Payload)
	}
	m1, ok := plan.Steps[1].Payload.(WriteExportManifestPayload)
	if !ok {
		t.Fatalf("expected WriteExportManifestPayload at step 1, got %T", plan.Steps[1].Payload)
	}
	w2, ok := plan.Steps[2].Payload.(WriteCSVPayload)
	if !ok {
		t.Fatalf("expected WriteCSVPayload at step 2, got %T", plan.Steps[2].Payload)
	}
	m3, ok := plan.Steps[3].Payload.(WriteExportManifestPayload)
	if !ok {
		t.Fatalf("expected WriteExportManifestPayload at step 3, got %T", plan.Steps[3].Payload)
	}

	if w0.ProductKey != "A" || m1.ProductKey != "A" {
		t.Fatalf("expected first product key to stay 'A', got write=%q manifest=%q", w0.ProductKey, m1.ProductKey)
	}
	if w2.ProductKey != "B" || m3.ProductKey != "B" {
		t.Fatalf("expected second product key to stay 'B', got write=%q manifest=%q", w2.ProductKey, m3.ProductKey)
	}
}

func TestCreatePlan_FilePattern_CollisionDetected(t *testing.T) {
	dslContent := `
profile Dev {
    file_pattern = "{plan_hash}"
}
use profile Dev

product A {
    param x: number = 1
}

product B {
    param x: number = 2
}
`
	tmpFile, err := os.CreateTemp("", "file_pattern_collision_*.dsl")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write([]byte(withDSLVersionHeader(dslContent))); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	ast, err := dsl.Parse(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to parse DSL: %v", err)
	}
	if err := dsl.Validate(ast); err != nil {
		t.Fatalf("Failed to validate AST: %v", err)
	}

	_, err = CreatePlan(ast, nil)
	if err == nil {
		t.Fatal("expected file_pattern collision error")
	}
	if !strings.Contains(err.Error(), "file_pattern collision") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreatePlan_FilePattern_CollisionAfterConstSubstitution(t *testing.T) {
	dslContent := `
const SHARED = "same_name"

profile Dev {
    file_pattern = "{const:SHARED}"
}
use profile Dev

product A {
    param x: number = 1
}

product B {
    param x: number = 2
}
`
	tmpFile, err := os.CreateTemp("", "file_pattern_const_collision_*.dsl")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write([]byte(withDSLVersionHeader(dslContent))); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	ast, err := dsl.Parse(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to parse DSL: %v", err)
	}
	if err := dsl.Validate(ast); err != nil {
		t.Fatalf("Failed to validate AST: %v", err)
	}

	_, err = CreatePlan(ast, nil)
	if err == nil {
		t.Fatal("expected file_pattern collision error")
	}
	if !strings.Contains(err.Error(), "file_pattern collision") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreatePlan_FilePattern_NoProductPlaceholder_ProductKeyStable(t *testing.T) {
	dslContent := `
profile Dev {
    file_pattern = "{profile}_{plan_hash}_{param:x}.csv"
}
use profile Dev

product A {
    param x: number = 1
}

product B {
    param x: number = 2
}
`

	_, plan := parseValidateAndPlan(t, dslContent)
	if len(plan.Steps) != 4 {
		t.Fatalf("expected 4 steps, got %d", len(plan.Steps))
	}

	w0 := plan.Steps[0].Payload.(WriteCSVPayload)
	m1 := plan.Steps[1].Payload.(WriteExportManifestPayload)
	w2 := plan.Steps[2].Payload.(WriteCSVPayload)
	m3 := plan.Steps[3].Payload.(WriteExportManifestPayload)

	if w0.ProductKey != "A" || m1.ProductKey != "A" {
		t.Fatalf("expected first product key to remain A, got write=%q manifest=%q", w0.ProductKey, m1.ProductKey)
	}
	if w2.ProductKey != "B" || m3.ProductKey != "B" {
		t.Fatalf("expected second product key to remain B, got write=%q manifest=%q", w2.ProductKey, m3.ProductKey)
	}
	if w0.Filename == w2.Filename {
		t.Fatalf("expected unique filenames for A/B in this test, both got %q", w0.Filename)
	}
}

func TestCreatePlan_FilePattern_NoProductPlaceholder_CollisionDetected(t *testing.T) {
	dslContent := `
profile Dev {
    file_pattern = "{profile}_{plan_hash}.csv"
}
use profile Dev

product A {
    param x: number = 1
}

product B {
    param x: number = 2
}
`

	tmpFile, err := os.CreateTemp("", "file_pattern_no_product_collision_*.dsl")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write([]byte(withDSLVersionHeader(dslContent))); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	ast, err := dsl.Parse(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to parse DSL: %v", err)
	}
	if err := dsl.Validate(ast); err != nil {
		t.Fatalf("Failed to validate AST: %v", err)
	}

	_, err = CreatePlan(ast, nil)
	if err == nil {
		t.Fatal("expected file_pattern collision error")
	}
	if !strings.Contains(err.Error(), "file_pattern collision") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreatePlan_FilePattern_MissingParamPlaceholderFails(t *testing.T) {
	dslContent := `
profile Dev {
    file_pattern = "{product}_{param:missing}.csv"
}
use profile Dev

product A {
    param x: number = 1
}
`

	tmpFile, err := os.CreateTemp("", "file_pattern_missing_param_contract_*.dsl")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write([]byte(withDSLVersionHeader(dslContent))); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	ast, err := dsl.Parse(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to parse DSL: %v", err)
	}
	if err := dsl.Validate(ast); err != nil {
		t.Fatalf("Failed to validate AST: %v", err)
	}

	if _, err := CreatePlan(ast, nil); err == nil {
		t.Fatal("expected CreatePlan to fail for missing param placeholder")
	}
}

func TestExplicitParameterAssignmentTarget_SingleMutationEmitsTarget(t *testing.T) {
	assemblyMutations := &ExportManifestMutationCollection{
		Parameters: []ExportManifestParameterMutation{
			{Object: "Body", Property: "Width", ValueParam: "width", Type: "number", Unit: "mm"},
		},
	}

	result := explicitParameterAssignmentTarget("width", assemblyMutations, nil)
	if result != "Body.Width" {
		t.Fatalf("expected explicit target %q, got %q", "Body.Width", result)
	}
}

func TestExplicitParameterAssignmentTarget_FormatIsObjectDotProperty(t *testing.T) {
	assemblyMutations := &ExportManifestMutationCollection{
		Parameters: []ExportManifestParameterMutation{
			{Object: "MyObject", Property: "MyProperty", ValueParam: "myParam", Type: "number", Unit: "mm"},
		},
	}

	result := explicitParameterAssignmentTarget("myParam", assemblyMutations, nil)
	if result != "MyObject.MyProperty" {
		t.Fatalf("expected <Object>.<Property> format, got %q", result)
	}
	parts := strings.SplitN(result, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		t.Fatalf("expected exactly one non-empty dot-separated pair, got %q", result)
	}
	if parts[0] != "MyObject" {
		t.Fatalf("expected object segment %q, got %q", "MyObject", parts[0])
	}
	if parts[1] != "MyProperty" {
		t.Fatalf("expected property segment %q, got %q", "MyProperty", parts[1])
	}
}

func TestExplicitParameterAssignmentTarget_MissingMutationOmitsTarget(t *testing.T) {
	assemblyMutations := &ExportManifestMutationCollection{
		Parameters: []ExportManifestParameterMutation{
			{Object: "Body", Property: "Height", ValueParam: "height", Type: "number", Unit: "mm"},
		},
	}

	result := explicitParameterAssignmentTarget("width", assemblyMutations, nil)
	if result != "" {
		t.Fatalf("expected empty target when no mutation matches, got %q", result)
	}
}

func TestExplicitParameterAssignmentTarget_AmbiguousMutationOmitsTarget(t *testing.T) {
	assemblyMutations := &ExportManifestMutationCollection{
		Parameters: []ExportManifestParameterMutation{
			{Object: "Body", Property: "Width", ValueParam: "width", Type: "number", Unit: "mm"},
		},
	}
	partMutations := &ExportManifestMutationCollection{
		Parameters: []ExportManifestParameterMutation{
			{Object: "Part", Property: "Width", ValueParam: "width", Type: "number", Unit: "mm"},
		},
	}

	result := explicitParameterAssignmentTarget("width", assemblyMutations, partMutations)
	if result != "" {
		t.Fatalf("expected empty target when mutation is ambiguous, got %q", result)
	}
}

func TestExplicitParameterAssignmentTarget_NilMutationsOmitTarget(t *testing.T) {
	result := explicitParameterAssignmentTarget("width", nil, nil)
	if result != "" {
		t.Fatalf("expected empty target when no mutations, got %q", result)
	}
}

func TestValidateFreeCADCADTargetMappings_Missing_RejectsWithParameterAssignmentIndex(t *testing.T) {
	assignments := []ExportManifestParameterAssignment{
		{Name: "width", Value: 120.0, Type: "number", Unit: "mm"},
	}

	err := ValidateFreeCADCADTargetMappings(assignments, nil, nil)
	if err == nil {
		t.Fatal("expected missing CAD target mapping to be rejected")
	}
	if !strings.Contains(err.Error(), "parameterAssignments[0]") {
		t.Fatalf("expected error to contain 'parameterAssignments[0]', got: %v", err)
	}
	if !strings.Contains(err.Error(), `"width"`) {
		t.Fatalf("expected error to identify parameter name 'width', got: %v", err)
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected error to indicate missing CAD target mapping, got: %v", err)
	}
	if strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected error not to incorrectly indicate ambiguity, got: %v", err)
	}
}

func TestValidateFreeCADCADTargetMappings_Ambiguous_RejectsWithParameterAssignmentIndex(t *testing.T) {
	assignments := []ExportManifestParameterAssignment{
		{Name: "width", Value: 120.0, Type: "number", Unit: "mm"},
	}
	assemblyMutations := &ExportManifestMutationCollection{
		Parameters: []ExportManifestParameterMutation{
			{Object: "Body", Property: "Width", ValueParam: "width"},
		},
	}
	partMutations := &ExportManifestMutationCollection{
		Parameters: []ExportManifestParameterMutation{
			{Object: "Part", Property: "Width", ValueParam: "width"},
		},
	}

	err := ValidateFreeCADCADTargetMappings(assignments, assemblyMutations, partMutations)
	if err == nil {
		t.Fatal("expected ambiguous CAD target mappings to be rejected")
	}
	if !strings.Contains(err.Error(), "parameterAssignments[0]") {
		t.Fatalf("expected error to contain 'parameterAssignments[0]', got: %v", err)
	}
	if !strings.Contains(err.Error(), `"width"`) {
		t.Fatalf("expected error to identify parameter name 'width', got: %v", err)
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected error to indicate ambiguous CAD target mappings, got: %v", err)
	}
	if strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected error not to collapse into missing-target report, got: %v", err)
	}
}

func TestValidateFreeCADCADTargetMappings_TwoAssemblyMutations_RejectsAsAmbiguous(t *testing.T) {
	assignments := []ExportManifestParameterAssignment{
		{Name: "width", Value: 120.0, Type: "number", Unit: "mm"},
	}
	assemblyMutations := &ExportManifestMutationCollection{
		Parameters: []ExportManifestParameterMutation{
			{Object: "Body", Property: "Width", ValueParam: "width"},
			{Object: "Cap", Property: "Width", ValueParam: "width"},
		},
	}

	err := ValidateFreeCADCADTargetMappings(assignments, assemblyMutations, nil)
	if err == nil {
		t.Fatal("expected two assembly mutations for same parameter to be rejected as ambiguous")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous error, got: %v", err)
	}
}

func TestValidateFreeCADCADTargetMappings_ExplicitTarget_Accepted(t *testing.T) {
	assignments := []ExportManifestParameterAssignment{
		{Name: "width", Target: "Body.Width", Value: 120.0, Type: "number", Unit: "mm"},
	}

	if err := ValidateFreeCADCADTargetMappings(assignments, nil, nil); err != nil {
		t.Fatalf("expected explicit target to be accepted without mutations, got error: %v", err)
	}
}

func TestValidateFreeCADCADTargetMappings_ExactlyOneMutation_Accepted(t *testing.T) {
	assignments := []ExportManifestParameterAssignment{
		{Name: "width", Value: 120.0, Type: "number", Unit: "mm"},
	}
	assemblyMutations := &ExportManifestMutationCollection{
		Parameters: []ExportManifestParameterMutation{
			{Object: "Body", Property: "Width", ValueParam: "width"},
		},
	}

	if err := ValidateFreeCADCADTargetMappings(assignments, assemblyMutations, nil); err != nil {
		t.Fatalf("expected exactly-one mutation fallback to be accepted, got error: %v", err)
	}
}

func TestProjectSemanticManifestIntent_ParameterAssignmentRetainsGenericFields(t *testing.T) {
	model := &semantic.Model{
		SchemaVersion:           semantic.SchemaVersion,
		SourceDocumentLogicalID: "box_model",
		RootComponentID:         "cmp.root",
		Components: []semantic.Component{
			{ID: "cmp.root", Kind: "assembly", Name: "RootAssembly"},
		},
		Parameters: []semantic.Parameter{
			{ID: "par.root.width", OwnerKind: semantic.OwnerKindComponent, OwnerID: "cmp.root", ComponentID: "cmp.root", Name: "width", NativeType: "Length"},
		},
	}
	intent := &semantic.ProductIntent{
		Name: "Box",
		ExportedParameters: []semantic.ExportedParameterIntent{
			{DSLParameterName: "width", SemanticParameterID: "par.root.width"},
		},
		Outputs: []semantic.OutputIntent{
			{OutputType: "step", TargetEntityKind: "assembly", TargetSemanticID: "cmp.root", TargetNameSource: "identity_linkage"},
		},
		Mutations: []semantic.MutationIntent{
			{
				OperationKind:    "set_parameter",
				TargetEntityKind: "parameter",
				TargetSemanticID: "par.root.width",
				TargetField:      "width",
				Scope:            "assembly",
				ValueSource: semantic.MutationValueSource{
					Kind:                "dsl_parameter",
					DSLParameterName:    "width",
					SemanticParameterID: "par.root.width",
				},
			},
		},
	}

	result, err := projectSemanticManifestIntent(&semanticManifestProjectionRequest{
		BaseName:       "Box",
		Model:          model,
		Intent:         intent,
		ResolvedValues: map[string]any{"width": 100.0},
		Contract:       testProjectionContract(),
	})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if len(result.ParameterAssignments) != 1 {
		t.Fatalf("expected 1 assignment, got %d", len(result.ParameterAssignments))
	}
	assignment := result.ParameterAssignments[0]
	if assignment.Name != "width" {
		t.Errorf("expected name %q, got %q", "width", assignment.Name)
	}
	if assignment.Value != 100.0 {
		t.Errorf("expected value 100.0, got %v", assignment.Value)
	}
	if assignment.Type != "number" {
		t.Errorf("expected type %q, got %q", "number", assignment.Type)
	}
	if assignment.Unit != "mm" {
		t.Errorf("expected unit %q, got %q", "mm", assignment.Unit)
	}
	if assignment.Target != "RootAssembly.width" {
		t.Errorf("expected target %q, got %q", "RootAssembly.width", assignment.Target)
	}
}

func TestProjectSemanticManifestIntent_AssignmentOrderingIsDeterministic(t *testing.T) {
	model := &semantic.Model{
		SchemaVersion:           semantic.SchemaVersion,
		SourceDocumentLogicalID: "box_model",
		RootComponentID:         "cmp.root",
		Components: []semantic.Component{
			{ID: "cmp.root", Kind: "assembly", Name: "RootAssembly"},
		},
		Parameters: []semantic.Parameter{
			{ID: "par.root.z", OwnerKind: semantic.OwnerKindComponent, OwnerID: "cmp.root", ComponentID: "cmp.root", Name: "z", NativeType: "Length"},
			{ID: "par.root.a", OwnerKind: semantic.OwnerKindComponent, OwnerID: "cmp.root", ComponentID: "cmp.root", Name: "a", NativeType: "Length"},
			{ID: "par.root.m", OwnerKind: semantic.OwnerKindComponent, OwnerID: "cmp.root", ComponentID: "cmp.root", Name: "m", NativeType: "Length"},
		},
	}
	intent := &semantic.ProductIntent{
		Name: "Box",
		ExportedParameters: []semantic.ExportedParameterIntent{
			{DSLParameterName: "z", SemanticParameterID: "par.root.z"},
			{DSLParameterName: "a", SemanticParameterID: "par.root.a"},
			{DSLParameterName: "m", SemanticParameterID: "par.root.m"},
		},
		Outputs: []semantic.OutputIntent{
			{OutputType: "step", TargetEntityKind: "assembly", TargetSemanticID: "cmp.root", TargetNameSource: "identity_linkage"},
		},
		Mutations: []semantic.MutationIntent{
			{
				OperationKind: "set_parameter", TargetEntityKind: "parameter",
				TargetSemanticID: "par.root.z", TargetField: "z", Scope: "assembly",
				ValueSource: semantic.MutationValueSource{Kind: "dsl_parameter", DSLParameterName: "z", SemanticParameterID: "par.root.z"},
			},
			{
				OperationKind: "set_parameter", TargetEntityKind: "parameter",
				TargetSemanticID: "par.root.a", TargetField: "a", Scope: "assembly",
				ValueSource: semantic.MutationValueSource{Kind: "dsl_parameter", DSLParameterName: "a", SemanticParameterID: "par.root.a"},
			},
			{
				OperationKind: "set_parameter", TargetEntityKind: "parameter",
				TargetSemanticID: "par.root.m", TargetField: "m", Scope: "assembly",
				ValueSource: semantic.MutationValueSource{Kind: "dsl_parameter", DSLParameterName: "m", SemanticParameterID: "par.root.m"},
			},
		},
	}

	first, err := projectSemanticManifestIntent(&semanticManifestProjectionRequest{
		BaseName:       "Box",
		Model:          model,
		Intent:         intent,
		ResolvedValues: map[string]any{"z": 1.0, "a": 2.0, "m": 3.0},
		Contract:       testProjectionContract(),
	})
	if err != nil {
		t.Fatalf("first projection error: %v", err)
	}
	second, err := projectSemanticManifestIntent(&semanticManifestProjectionRequest{
		BaseName:       "Box",
		Model:          model,
		Intent:         intent,
		ResolvedValues: map[string]any{"z": 1.0, "a": 2.0, "m": 3.0},
		Contract:       testProjectionContract(),
	})
	if err != nil {
		t.Fatalf("second projection error: %v", err)
	}
	if !reflect.DeepEqual(first.ParameterAssignments, second.ParameterAssignments) {
		t.Fatalf("assignment ordering changed across repeated projections\nfirst:  %+v\nsecond: %+v", first.ParameterAssignments, second.ParameterAssignments)
	}
	for _, assignment := range first.ParameterAssignments {
		if assignment.Target == "" {
			t.Errorf("expected non-empty target for assignment %q", assignment.Name)
		}
	}
}

func TestCreatePlan_FreeCADNameOnlyParameterAssignmentRejected(t *testing.T) {
	dslContent := `
product Widget {
    adapter = "freecad"
    source_model = "input/widget.FCStd"

    param width: number = 120
}
`

	tmpFile, err := os.CreateTemp("", "freecad_name_only_*.dsl")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write([]byte(withDSLVersionHeader(dslContent))); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("failed to close temp file: %v", err)
	}

	ast, err := dsl.Parse(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to parse DSL: %v", err)
	}
	if err := dsl.Validate(ast); err != nil {
		t.Fatalf("DSL validation failed unexpectedly: %v", err)
	}

	_, planErr := CreatePlan(ast, nil)
	if planErr == nil {
		t.Fatal("expected FreeCAD name-only parameter assignment to be rejected")
	}
	if !strings.Contains(planErr.Error(), `"width"`) {
		t.Fatalf("expected rejection error to identify parameter name 'width', got: %v", planErr)
	}
	if !strings.Contains(planErr.Error(), "name-only parameter assignments are not supported") {
		t.Fatalf("expected rejection error to state name-only is not supported, got: %v", planErr)
	}
}
