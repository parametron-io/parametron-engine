package planner

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/authoring/dsl"
	"parametron/internal/engine/table"
)

func TestCreatePlanWithTables_TableCellSuccessAndDeterminism(t *testing.T) {
	dslContent := `
product Fastener {
    param sku: string = "M8x20"
    param diameter: number = table_cell("fasteners", sku, "diameter")
    param label: string = table_cell("fasteners", sku, "label")
    param coated: boolean = table_cell("fasteners", sku, "coated")
}`

	ast := parseAndValidateWithTables(t, dslContent, plannerTestTables())
	plan1, err := CreatePlanWithTables(ast, nil, plannerTestTables())
	if err != nil {
		t.Fatalf("CreatePlanWithTables failed: %v", err)
	}
	plan2, err := CreatePlanWithTables(ast, nil, plannerTestTables())
	if err != nil {
		t.Fatalf("second CreatePlanWithTables failed: %v", err)
	}

	if !reflect.DeepEqual(plan1, plan2) {
		t.Fatal("expected repeated planning with same table inputs to be deterministic")
	}

	payload := plan1.Steps[0].Payload.(WriteCSVPayload)
	expected := []interface{}{"M8x20", 8.0, "Hex bolt", true}
	if !reflect.DeepEqual(payload.Values, expected) {
		t.Fatalf("unexpected resolved values: got %v want %v", payload.Values, expected)
	}
}

func TestCreatePlanWithTables_TableCellFailures(t *testing.T) {
	tests := []struct {
		name          string
		dslContent    string
		tables        map[string]*table.Table
		errorContains string
	}{
		{
			name: "Missing Table",
			dslContent: `
product Fastener {
    param diameter: number = table_cell("missing", "M8x20", "diameter")
}`,
			tables:        plannerTestTables(),
			errorContains: "unknown table 'missing'",
		},
		{
			name: "Missing Row",
			dslContent: `
product Fastener {
    param diameter: number = table_cell("fasteners", "M8x99", "diameter")
}`,
			tables:        plannerTestTables(),
			errorContains: "row not found",
		},
		{
			name: "Missing Column",
			dslContent: `
product Fastener {
    param diameter: number = table_cell("fasteners", "M8x20", "missing")
}`,
			tables:        plannerTestTables(),
			errorContains: "unknown column 'missing'",
		},
		{
			name: "Optional Column Missing In Row",
			dslContent: `
product Fastener {
    param note: string = table_cell("fasteners", "M8x30", "note")
}`,
			tables:        plannerTestTables(),
			errorContains: "optional column value is missing",
		},
		{
			name: "Invalid Underlying Table",
			dslContent: `
product Fastener {
    param diameter: number = table_cell("fasteners", "M8x20", "diameter")
}`,
			tables:        plannerInvalidTables(),
			errorContains: "table validation error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast := parseDSLForPlannerTest(t, tt.dslContent)
			_, err := CreatePlanWithTables(ast, nil, tt.tables)
			if err == nil {
				t.Fatal("expected CreatePlanWithTables to fail")
			}
			if !strings.Contains(err.Error(), tt.errorContains) {
				t.Fatalf("expected error containing %q, got %q", tt.errorContains, err.Error())
			}
		})
	}
}

func TestCreatePlanWithTables_TableCellRejectsDuplicateKeyTables(t *testing.T) {
	ast := parseDSLForPlannerTest(t, `
product Fastener {
    param diameter: number = table_cell("fasteners", "M8x20", "diameter")
}`)

	_, err := CreatePlanWithTables(ast, nil, plannerDuplicateKeyTables())
	if err == nil {
		t.Fatal("expected CreatePlanWithTables to fail for duplicate-key table")
	}
	if !errors.Is(err, table.ErrValidation) {
		t.Fatalf("expected duplicate-key failure to remain a validation error, got %v", err)
	}
	if strings.Contains(err.Error(), "row not found") || strings.Contains(err.Error(), "unknown column") {
		t.Fatalf("expected duplicate-key failure to stay in schema validation path, got %q", err.Error())
	}
}

func TestCreatePlanWithTables_TableCellExactMatchSemantics(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{name: "CaseSensitive", key: "M8"},
		{name: "NoTrimStoredWhitespace", key: "M8"},
		{name: "NoTrimTrailingWhitespace", key: "M8"},
	}

	tables := plannerExactMatchTables()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast := parseDSLForPlannerTest(t, `
product Fastener {
    param diameter: number = table_cell("fasteners", "`+tt.key+`", "diameter")
}`)

			_, err := CreatePlanWithTables(ast, nil, tables)
			if err == nil {
				t.Fatal("expected exact-match planner lookup to fail")
			}
			if !strings.Contains(err.Error(), "row not found") {
				t.Fatalf("expected exact-match row lookup failure, got %q", err.Error())
			}
		})
	}
}

func TestCreatePlanWithTables_TableCellRepeatedLookupDeterministicWithinProduct(t *testing.T) {
	ast := parseAndValidateWithTables(t, `
product Fastener {
    param sku: string = "M8x20"
    param diameter_a: number = table_cell("fasteners", sku, "diameter")
    param diameter_b: number = table_cell("fasteners", "M8x20", "diameter")
    param label: string = table_cell("fasteners", sku, "label")
}`, plannerTestTables())

	plan1, err := CreatePlanWithTables(ast, nil, plannerTestTables())
	if err != nil {
		t.Fatalf("CreatePlanWithTables failed: %v", err)
	}
	plan2, err := CreatePlanWithTables(ast, nil, plannerTestTables())
	if err != nil {
		t.Fatalf("second CreatePlanWithTables failed: %v", err)
	}

	if !reflect.DeepEqual(plan1, plan2) {
		t.Fatal("expected repeated in-product lookups to remain deterministic")
	}

	payload := plan1.Steps[0].Payload.(WriteCSVPayload)
	if payload.Values[1] != 8.0 || payload.Values[2] != 8.0 {
		t.Fatalf("expected both repeated lookups to resolve to 8.0, got %v and %v", payload.Values[1], payload.Values[2])
	}
	if payload.Values[3] != "Hex bolt" {
		t.Fatalf("expected later lookup to remain unaffected, got %v", payload.Values[3])
	}
}

func parseAndValidateWithTables(t *testing.T, content string, tables map[string]*table.Table) *dsl.AST {
	t.Helper()

	ast := parseDSLForPlannerTest(t, content)
	if err := dsl.ValidateWithTables(ast, tables); err != nil {
		t.Fatalf("ValidateWithTables failed: %v", err)
	}
	return ast
}

func parseDSLForPlannerTest(t *testing.T, content string) *dsl.AST {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "planner_table_lookup_*.dsl")
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
	return ast
}

func plannerTestTables() map[string]*table.Table {
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
				{Name: "note", Type: table.ColumnTypeString, Required: false},
			},
			Rows: []table.Row{
				{Values: map[string]table.Value{
					"code":     {Type: table.ColumnTypeString, String: "M8x20"},
					"diameter": {Type: table.ColumnTypeNumber, Number: json.Number("8")},
					"label":    {Type: table.ColumnTypeString, String: "Hex bolt"},
					"coated":   {Type: table.ColumnTypeBoolean, Boolean: true},
					"note":     {Type: table.ColumnTypeString, String: "zinc"},
				}},
				{Values: map[string]table.Value{
					"code":     {Type: table.ColumnTypeString, String: "M8x30"},
					"diameter": {Type: table.ColumnTypeNumber, Number: json.Number("8")},
					"label":    {Type: table.ColumnTypeString, String: "Hex bolt long"},
					"coated":   {Type: table.ColumnTypeBoolean, Boolean: false},
				}},
			},
		},
	}
}

func plannerInvalidTables() map[string]*table.Table {
	tables := plannerTestTables()
	tables["fasteners"].Rows[0].Values["diameter"] = table.Value{Type: table.ColumnTypeString, String: "bad"}
	return tables
}

func plannerDuplicateKeyTables() map[string]*table.Table {
	tables := plannerTestTables()
	tables["fasteners"].Rows = append(tables["fasteners"].Rows, table.Row{
		Values: map[string]table.Value{
			"code":     {Type: table.ColumnTypeString, String: "M8x20"},
			"diameter": {Type: table.ColumnTypeNumber, Number: json.Number("10")},
			"label":    {Type: table.ColumnTypeString, String: "Duplicate"},
			"coated":   {Type: table.ColumnTypeBoolean, Boolean: false},
		},
	})
	return tables
}

func plannerExactMatchTables() map[string]*table.Table {
	return map[string]*table.Table{
		"fasteners": {
			SchemaVersion: table.SchemaVersion,
			Name:          "fasteners",
			KeyColumn:     "code",
			Columns: []table.Column{
				{Name: "code", Type: table.ColumnTypeString, Required: true},
				{Name: "diameter", Type: table.ColumnTypeNumber, Required: true},
			},
			Rows: []table.Row{
				{Values: map[string]table.Value{
					"code":     {Type: table.ColumnTypeString, String: "m8"},
					"diameter": {Type: table.ColumnTypeNumber, Number: json.Number("8")},
				}},
				{Values: map[string]table.Value{
					"code":     {Type: table.ColumnTypeString, String: " M8 "},
					"diameter": {Type: table.ColumnTypeNumber, Number: json.Number("8")},
				}},
				{Values: map[string]table.Value{
					"code":     {Type: table.ColumnTypeString, String: "M8 "},
					"diameter": {Type: table.ColumnTypeNumber, Number: json.Number("8")},
				}},
			},
		},
	}
}
