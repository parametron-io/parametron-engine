package ir

import (
	"encoding/json"
	"os"
	"testing"

	"parametron/internal/authoring/dsl"
	"parametron/internal/authoring/planner"
)

// TestFullRoundtrip tests the complete roundtrip:
// DSL → AST → IR → DSL → AST → IR
// The two IR programs should be equivalent.
func TestFullRoundtrip(t *testing.T) {
	testCases := []struct {
		name string
		dsl  string
	}{
		{
			name: "Simple product",
			dsl: `product Table {
    param width: number = 100
    param height: number = 200
}`,
		},
		{
			name: "Constants and expressions",
			dsl: `const BASE = 100
const SCALE = 0.5

product Table {
    param width: number = BASE
    param height: number = width * SCALE
    param area: number = width * height
}`,
		},
		{
			name: "Profile with settings",
			dsl: `profile Dev {
    output_dir = "test_output"
}

use profile Dev

product Widget {
    param size: number = 50
}`,
		},
		{
			name: "Enum types",
			dsl: `product Cabinet {
    param material: enum { Oak, Pine, Maple } = Oak
    param finish: enum { Glossy, Matte } = Matte
}`,
		},
		{
			name: "Complex expressions",
			dsl: `product Complex {
    param a: number = 10
    param b: number = 20
    param max_val: number = max(a, b)
    param is_large: boolean = a > 5 && b > 15
    param conditional: number = is_large ? 100 : 0
}`,
		},
		{
			name: "Lets",
			dsl: `product WithLets {
    let base = 10
    let next = base + 1
    param total: number = next + 1
}`,
		},
		{
			name: "String parameters",
			dsl: `const PREFIX = "Item"

product Labeled {
    param name: string = "Default"
    param full_name: string = PREFIX
}`,
		},
		{
			name: "Boolean logic",
			dsl: `product Logic {
    param a: boolean = true
    param b: boolean = false
    param and_result: boolean = a && b
    param or_result: boolean = a || b
}`,
		},
		{
			name: "Unary operators",
			dsl: `product Unary {
    param positive: number = 10
    param negative: number = -positive
}`,
		},
		{
			name: "Comparison operators",
			dsl: `product Compare {
    param a: number = 10
    param b: number = 20
    param eq: boolean = a == b
    param neq: boolean = a != b
    param lt: boolean = a < b
    param lte: boolean = a <= b
    param gt: boolean = a > b
    param gte: boolean = a >= b
}`,
		},
		{
			name: "Nested function calls",
			dsl: `product Nested {
    param result: number = max(min(10, 50), abs(-20))
}`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// First pass: DSL → AST → IR
			ast1 := parseAndValidate(t, tc.dsl)
			ir1, err := ConvertAST(ast1)
			if err != nil {
				t.Fatalf("First ConvertAST failed: %v", err)
			}

			// IR → DSL
			generatedDSL := GenerateDSL(ir1)

			// Second pass: Generated DSL → AST → IR
			ast2 := parseAndValidateFromString(t, generatedDSL)
			ir2, err := ConvertAST(ast2)
			if err != nil {
				t.Fatalf("Second ConvertAST failed: %v\nGenerated DSL:\n%s", err, generatedDSL)
			}

			// Compare the two IR programs
			if !programsEqual(ir1, ir2) {
				t.Errorf("IR programs are not equal after roundtrip\nOriginal DSL:\n%s\nGenerated DSL:\n%s", tc.dsl, generatedDSL)
			}
		})
	}
}

// TestIRExecution tests that IR-based execution produces the same results as AST-based execution.
func TestIRExecution(t *testing.T) {
	dslContent := `
const SCALE = 0.5

profile Dev {
    output_dir = "test"
}

use profile Dev

product Table {
    param width: number = 100
    param height: number = width * SCALE
    param area: number = width * height
    param is_large: boolean = area > 1000
    param material: enum { Oak, Pine } = Oak
}`

	// Create plan using AST
	ast := parseAndValidate(t, dslContent)
	astPlan, err := planner.CreatePlan(ast, nil)
	if err != nil {
		t.Fatalf("CreatePlan (AST) failed: %v", err)
	}

	// Create plan using IR
	irProgram, err := ConvertAST(ast)
	if err != nil {
		t.Fatalf("ConvertAST failed: %v", err)
	}

	irPlan, err := CreatePlanFromIR(irProgram, nil)
	if err != nil {
		t.Fatalf("CreatePlanFromIR failed: %v", err)
	}

	// Compare plans
	if len(astPlan.Steps) != len(irPlan.Steps) {
		t.Fatalf("Step count mismatch: AST=%d, IR=%d", len(astPlan.Steps), len(irPlan.Steps))
	}

	for i := range astPlan.Steps {
		if astPlan.Steps[i].Type != irPlan.Steps[i].Type {
			t.Errorf("Step %d type mismatch: AST=%s, IR=%s", i, astPlan.Steps[i].Type, irPlan.Steps[i].Type)
		}

		// Compare payloads
		switch astPayload := astPlan.Steps[i].Payload.(type) {
		case planner.WriteCSVPayload:
			irPayload, ok := irPlan.Steps[i].Payload.(planner.WriteCSVPayload)
			if !ok {
				t.Errorf("Step %d payload type mismatch", i)
				continue
			}
			if astPayload.Filename != irPayload.Filename {
				t.Errorf("Step %d filename mismatch: %s vs %s", i, astPayload.Filename, irPayload.Filename)
			}
			if len(astPayload.Headers) != len(irPayload.Headers) {
				t.Errorf("Step %d headers count mismatch", i)
				continue
			}
			if len(astPayload.Values) != len(irPayload.Values) {
				t.Errorf("Step %d values count mismatch", i)
				continue
			}
			for j := range astPayload.Values {
				if astPayload.Values[j] != irPayload.Values[j] {
					t.Errorf("Step %d value %d mismatch: %v vs %v", i, j, astPayload.Values[j], irPayload.Values[j])
				}
			}
		case planner.RunCADRuntimePayload:
			irPayload, ok := irPlan.Steps[i].Payload.(planner.RunCADRuntimePayload)
			if !ok {
				t.Errorf("Step %d payload type mismatch", i)
				continue
			}
			if astPayload.Adapter != irPayload.Adapter {
				t.Errorf("Step %d adapter mismatch: %s vs %s", i, astPayload.Adapter, irPayload.Adapter)
			}
			if astPayload.ManifestFilename != irPayload.ManifestFilename {
				t.Errorf("Step %d manifest filename mismatch", i)
			}
			if astPayload.ResultFilename != irPayload.ResultFilename {
				t.Errorf("Step %d result filename mismatch", i)
			}
		}
	}
}

// TestIRPlanningUsesAlignedCADRuntime proves that IR lowering emits the
// current aligned RunCADRuntime step (never the retired RunPython step)
// when the active profile requires the FreeCAD adapter, and that the
// lowering is deterministic across equivalent inputs.
//
// The IR program is constructed directly rather than parsed from DSL text:
// the current DSL validator rejects profile-level "adapter" settings
// (execution declarations now live in product blocks), but IR is a
// standalone serializable format that Designer/API callers may construct
// without going through DSL text, so its own adapter-gated lowering is
// still an IR capability worth proving directly.
func TestIRPlanningUsesAlignedCADRuntime(t *testing.T) {
	activeProfile := "Dev"
	irProgram := &IRProgram{
		Version:           "1.0",
		Constants:         map[string]IRConstant{},
		ActiveProfileName: &activeProfile,
		Profiles: []IRProfile{
			{
				Name: "Dev",
				Settings: map[string]IRExpression{
					"adapter": {Kind: ExprKindLiteral, LiteralValue: "freecad"},
				},
			},
		},
		Products: []IRProduct{
			{
				Name: "Table",
				Parameters: []IRParameter{
					{
						Name:         "width",
						Type:         IRType{Kind: IRTypeNumber},
						DefaultValue: IRExpression{Kind: ExprKindLiteral, LiteralValue: 100.0},
					},
					{
						Name:         "height",
						Type:         IRType{Kind: IRTypeNumber},
						DefaultValue: IRExpression{Kind: ExprKindLiteral, LiteralValue: 200.0},
					},
				},
			},
		},
	}

	buildPlan := func() *planner.ExecutionPlan {
		plan, err := CreatePlanFromIR(irProgram, nil)
		if err != nil {
			t.Fatalf("CreatePlanFromIR failed: %v", err)
		}
		return plan
	}

	plan := buildPlan()

	wantTypes := []planner.StepType{
		planner.StepWriteCSV,
		planner.StepWriteExportManifest,
		planner.StepRunCADRuntime,
	}
	if len(plan.Steps) != len(wantTypes) {
		t.Fatalf("step count mismatch: got %d, want %d", len(plan.Steps), len(wantTypes))
	}
	for i, wantType := range wantTypes {
		if plan.Steps[i].Type != wantType {
			t.Errorf("step %d type = %s, want %s", i, plan.Steps[i].Type, wantType)
		}
		if string(plan.Steps[i].Type) == "RunPython" {
			t.Errorf("step %d: IR must never emit the retired RunPython step type", i)
		}
	}

	cadPayload, ok := plan.Steps[2].Payload.(planner.RunCADRuntimePayload)
	if !ok {
		t.Fatalf("step 2 payload type = %T, want planner.RunCADRuntimePayload", plan.Steps[2].Payload)
	}
	if cadPayload.Adapter != "freecad" {
		t.Errorf("cadPayload.Adapter = %q, want %q", cadPayload.Adapter, "freecad")
	}
	if cadPayload.ManifestFilename != planner.ExportManifestFilename {
		t.Errorf("cadPayload.ManifestFilename = %q, want %q", cadPayload.ManifestFilename, planner.ExportManifestFilename)
	}
	if cadPayload.ResultFilename != planner.FreeCADRuntimeResultFilename {
		t.Errorf("cadPayload.ResultFilename = %q, want %q", cadPayload.ResultFilename, planner.FreeCADRuntimeResultFilename)
	}

	// planner.RunCADRuntimePayload has no field capable of carrying a
	// script path, so its exclusive use here is itself the proof that the
	// retired CAD-runner script coupling cannot resurface from IR lowering.
	if _, ok := plan.Steps[1].Payload.(planner.WriteExportManifestPayload); !ok {
		t.Fatalf("step 1 payload type = %T, want planner.WriteExportManifestPayload", plan.Steps[1].Payload)
	}

	// Determinism: rebuilding the plan from the same IR program must
	// produce an identical RunCADRuntime payload.
	again := buildPlan()
	againPayload, ok := again.Steps[2].Payload.(planner.RunCADRuntimePayload)
	if !ok {
		t.Fatalf("second build: step 2 payload type = %T, want planner.RunCADRuntimePayload", again.Steps[2].Payload)
	}
	if againPayload != cadPayload {
		t.Errorf("repeated planning of equivalent IR produced different RunCADRuntime payloads: %+v vs %+v", cadPayload, againPayload)
	}
}

// TestJSONRoundtrip tests that JSON serialization preserves all data.
func TestJSONRoundtrip(t *testing.T) {
	dslContent := `
const BASE = 100

profile Dev {
    output_dir = "test"
}

use profile Dev

product Table {
    param width: number = BASE
    param height: number = width * 0.5
    param material: enum { Oak, Pine } = Oak
    param label: string = "Table"
    param is_valid: boolean = width > 0
}`

	ast := parseAndValidate(t, dslContent)
	ir1, err := ConvertAST(ast)
	if err != nil {
		t.Fatalf("ConvertAST failed: %v", err)
	}

	// Serialize to JSON
	jsonData, err := ir1.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}

	// Deserialize from JSON
	ir2, err := FromJSON(jsonData)
	if err != nil {
		t.Fatalf("FromJSON failed: %v", err)
	}

	// Create plans from both IR programs
	plan1, err := CreatePlanFromIR(ir1, nil)
	if err != nil {
		t.Fatalf("CreatePlanFromIR (ir1) failed: %v", err)
	}

	plan2, err := CreatePlanFromIR(ir2, nil)
	if err != nil {
		t.Fatalf("CreatePlanFromIR (ir2) failed: %v", err)
	}

	// Compare plans
	if len(plan1.Steps) != len(plan2.Steps) {
		t.Fatalf("Step count mismatch after JSON roundtrip")
	}

	for i := range plan1.Steps {
		payload1, ok1 := plan1.Steps[i].Payload.(planner.WriteCSVPayload)
		payload2, ok2 := plan2.Steps[i].Payload.(planner.WriteCSVPayload)

		if ok1 && ok2 {
			if len(payload1.Values) != len(payload2.Values) {
				t.Errorf("Step %d value count mismatch", i)
				continue
			}
			for j := range payload1.Values {
				if payload1.Values[j] != payload2.Values[j] {
					t.Errorf("Step %d value %d mismatch: %v vs %v", i, j, payload1.Values[j], payload2.Values[j])
				}
			}
		}
	}
}

// TestOverrideHandling tests that overrides work correctly through the IR layer.
func TestOverrideHandling(t *testing.T) {
	dslContent := `
const BASE = 100

product Table {
    param width: number = BASE
    param height: number = width * 0.5
    param area: number = width * height
}`

	ast := parseAndValidate(t, dslContent)
	ir, err := ConvertAST(ast)
	if err != nil {
		t.Fatalf("ConvertAST failed: %v", err)
	}

	// Test without override
	plan1, err := CreatePlanFromIR(ir, nil)
	if err != nil {
		t.Fatalf("CreatePlanFromIR failed: %v", err)
	}

	payload1 := plan1.Steps[0].Payload.(planner.WriteCSVPayload)
	// width = 100, height = 50, area = 5000
	if payload1.Values[0] != 100.0 || payload1.Values[1] != 50.0 || payload1.Values[2] != 5000.0 {
		t.Errorf("Unexpected values without override: %v", payload1.Values)
	}

	// Test with override
	plan2, err := CreatePlanFromIR(ir, map[string]string{"width": "200"})
	if err != nil {
		t.Fatalf("CreatePlanFromIR with override failed: %v", err)
	}

	payload2 := plan2.Steps[0].Payload.(planner.WriteCSVPayload)
	// width = 200 (overridden), height = 100, area = 20000
	if payload2.Values[0] != 200.0 {
		t.Errorf("Expected width 200 (overridden), got %v", payload2.Values[0])
	}
	if payload2.Values[1] != 100.0 {
		t.Errorf("Expected height 100, got %v", payload2.Values[1])
	}
	if payload2.Values[2] != 20000.0 {
		t.Errorf("Expected area 20000, got %v", payload2.Values[2])
	}
}

// TestNilSafety tests that the IR package handles nil inputs gracefully.
func TestNilSafety(t *testing.T) {
	// Test nil AST conversion
	_, err := ConvertAST(nil)
	if err == nil {
		t.Error("Expected error for nil AST")
	}

	// Test nil program DSL generation
	result := GenerateDSL(nil)
	if result != "" {
		t.Errorf("Expected empty string for nil program, got: %s", result)
	}

	// Test nil program execution
	_, err = CreatePlanFromIR(nil, nil)
	if err == nil {
		t.Error("Expected error for nil program execution")
	}

	// Test nil program JSON
	var nilProgram *IRProgram
	data, err := json.Marshal(nilProgram)
	if err != nil {
		t.Errorf("Failed to marshal nil program: %v", err)
	}
	if string(data) != "null" {
		t.Errorf("Expected 'null', got: %s", string(data))
	}
}

// TestDeterminism tests that the same DSL produces identical IR and output.
func TestDeterminism(t *testing.T) {
	dslContent := `
const Z = 1
const A = 2
const M = 3

profile Zulu {
    output_dir = "z"
}

profile Alpha {
    output_dir = "a"
}

use profile Alpha

product Zebra {
    param z: number = 1
}

product Apple {
    param a: number = 2
}

product Mango {
    param m: number = 3
}`

	// Parse multiple times
	var programs []*IRProgram
	for i := 0; i < 3; i++ {
		ast := parseAndValidate(t, dslContent)
		ir, err := ConvertAST(ast)
		if err != nil {
			t.Fatalf("ConvertAST failed: %v", err)
		}
		programs = append(programs, ir)
	}

	// All should be equal
	for i := 1; i < len(programs); i++ {
		if !programsEqual(programs[0], programs[i]) {
			t.Errorf("Programs %d and %d are not equal", 0, i)
		}
	}

	// Generated DSL should be identical
	var dslOutputs []string
	for _, ir := range programs {
		dslOutputs = append(dslOutputs, GenerateDSL(ir))
	}

	for i := 1; i < len(dslOutputs); i++ {
		if dslOutputs[0] != dslOutputs[i] {
			t.Errorf("DSL output %d differs from output 0\nOutput 0:\n%s\nOutput %d:\n%s", i, dslOutputs[0], i, dslOutputs[i])
		}
	}
}

// Helper function to parse and validate DSL from a string
func parseAndValidateFromString(t *testing.T, content string) *dsl.AST {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "test_*.dsl")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write([]byte(withDSLVersionHeader(content))); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	ast, err := dsl.Parse(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to parse DSL: %v\nContent:\n%s", err, content)
	}

	if err := dsl.Validate(ast); err != nil {
		t.Fatalf("Failed to validate AST: %v\nContent:\n%s", err, content)
	}

	return ast
}
