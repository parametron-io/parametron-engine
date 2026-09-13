package ir

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/authoring/dsl"
	"parametron/internal/authoring/planner"
	"parametron/internal/engine/table"
)

func TestCreatePlanFromIRWithTables_TableCellParity(t *testing.T) {
	dslContent := `
product Fastener {
    param sku: string = "M8x20"
    param diameter: number = table_cell("fasteners", sku, "diameter")
    param label: string = table_cell("fasteners", sku, "label")
    param coated: boolean = table_cell("fasteners", sku, "coated")
}`

	tables := irTestTables()
	ast := parseAndValidateWithTablesForIR(t, dslContent, tables)
	astPlan, err := planner.CreatePlanWithTables(ast, nil, tables)
	if err != nil {
		t.Fatalf("CreatePlanWithTables failed: %v", err)
	}

	program, err := ConvertAST(ast)
	if err != nil {
		t.Fatalf("ConvertAST failed: %v", err)
	}
	irPlan, err := CreatePlanFromIRWithTables(program, nil, tables)
	if err != nil {
		t.Fatalf("CreatePlanFromIRWithTables failed: %v", err)
	}

	if !reflect.DeepEqual(astPlan, irPlan) {
		t.Fatalf("expected AST and IR planner outputs to match\nAST: %#v\nIR: %#v", astPlan, irPlan)
	}
}

func TestCreatePlanFromIRWithTables_TableCellFailureParity(t *testing.T) {
	dslContent := `
product Fastener {
    param diameter: number = table_cell("fasteners", "M8x99", "diameter")
}`

	tables := irTestTables()
	ast := parseAndValidateWithTablesForIR(t, dslContent, tables)

	_, astErr := planner.CreatePlanWithTables(ast, nil, tables)
	if astErr == nil {
		t.Fatal("expected AST planner to fail")
	}

	program, err := ConvertAST(ast)
	if err != nil {
		t.Fatalf("ConvertAST failed: %v", err)
	}
	_, irErr := CreatePlanFromIRWithTables(program, nil, tables)
	if irErr == nil {
		t.Fatal("expected IR planner to fail")
	}

	if !strings.Contains(astErr.Error(), "row not found") {
		t.Fatalf("expected AST planner error to mention row not found, got %q", astErr.Error())
	}
	if !strings.Contains(irErr.Error(), "row not found") {
		t.Fatalf("expected IR planner error to mention row not found, got %q", irErr.Error())
	}
}

func TestCreatePlanFromIRWithTables_TableCellNestedRowKeyParity(t *testing.T) {
	dslContent := `
product Fastener {
    param keyPrefix: string = "M8"
    param diameter: number = table_cell("fasteners", keyPrefix + "x20", "diameter")
}`

	tables := irTestTables()
	ast := parseAndValidateWithTablesForIR(t, dslContent, tables)

	astPlan, err := planner.CreatePlanWithTables(ast, nil, tables)
	if err != nil {
		t.Fatalf("CreatePlanWithTables failed: %v", err)
	}

	program, err := ConvertAST(ast)
	if err != nil {
		t.Fatalf("ConvertAST failed: %v", err)
	}
	irPlan, err := CreatePlanFromIRWithTables(program, nil, tables)
	if err != nil {
		t.Fatalf("CreatePlanFromIRWithTables failed: %v", err)
	}

	if !reflect.DeepEqual(astPlan, irPlan) {
		t.Fatalf("expected AST and IR planner outputs to match for nested row key\nAST: %#v\nIR: %#v", astPlan, irPlan)
	}

	payload := astPlan.Steps[0].Payload.(planner.WriteCSVPayload)
	if payload.Values[0] != "M8" || payload.Values[1] != 8.0 {
		t.Fatalf("unexpected nested row-key values: %v", payload.Values)
	}
}

func parseAndValidateWithTablesForIR(t *testing.T, content string, tables map[string]*table.Table) *dsl.AST {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "ir_table_lookup_*.dsl")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write([]byte(withDSLVersionHeader(content))); err != nil {
		t.Fatalf("failed to write temp DSL: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("failed to close temp DSL: %v", err)
	}

	ast, err := dsl.Parse(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to parse DSL: %v", err)
	}
	if err := dsl.ValidateWithTables(ast, tables); err != nil {
		t.Fatalf("ValidateWithTables failed: %v", err)
	}
	return ast
}

func irTestTables() map[string]*table.Table {
	return map[string]*table.Table{
		"fasteners": {
			SchemaVersion: table.SchemaVersion,
			Name:          "fasteners",
			KeyColumn:     "code",
			Columns: []table.Column{
				{Name: "code", Type: table.ColumnTypeString, Required: true},
				{Name: "diameter", Type: table.ColumnTypeNumber, Required: true},
				{Name: "label", Type: table.ColumnTypeString, Required: true},
				{Name: "coated", Type: table.ColumnTypeBoolean, Required: true},
			},
			Rows: []table.Row{
				{Values: map[string]table.Value{
					"code":     {Type: table.ColumnTypeString, String: "M8x20"},
					"diameter": {Type: table.ColumnTypeNumber, Number: json.Number("8")},
					"label":    {Type: table.ColumnTypeString, String: "Hex bolt"},
					"coated":   {Type: table.ColumnTypeBoolean, Boolean: true},
				}},
			},
		},
	}
}
