package dsl

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"parametron/internal/engine/table"
)

func TestValidateWithTables_TableCellValidation(t *testing.T) {
	tests := []struct {
		name          string
		dslContent    string
		tables        map[string]*table.Table
		errorContains string
	}{
		{
			name: "Wrong Arity",
			dslContent: `
product Fastener {
    param diameter: number = table_cell("fasteners", "M8x20")
}`,
			tables:        validatorTestTables(),
			errorContains: "expects 3 arguments",
		},
		{
			name: "Row Key Must Be String Expression",
			dslContent: `
product Fastener {
    param diameter: number = table_cell("fasteners", 123, "diameter")
}`,
			tables:        validatorTestTables(),
			errorContains: "argument 2 of 'table_cell'",
		},
		{
			name: "First Argument Must Be String Literal",
			dslContent: `
product Fastener {
    param table_name: string = "fasteners"
    param diameter: number = table_cell(table_name, "M8x20", "diameter")
}`,
			tables:        validatorTestTables(),
			errorContains: "argument 1 of 'table_cell' must be a string literal",
		},
		{
			name: "Third Argument Must Be String Literal",
			dslContent: `
product Fastener {
    param column_name: string = "diameter"
    param diameter: number = table_cell("fasteners", "M8x20", column_name)
}`,
			tables:        validatorTestTables(),
			errorContains: "argument 3 of 'table_cell' must be a string literal",
		},
		{
			name: "Type Mismatch",
			dslContent: `
product Fastener {
    param coated: boolean = table_cell("fasteners", "M8x20", "diameter")
}`,
			tables:        validatorTestTables(),
			errorContains: "returns a number",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast := parseDSLForValidatorTableTest(t, tt.dslContent)
			err := ValidateWithTables(ast, tt.tables)
			if err == nil {
				t.Fatal("expected ValidateWithTables to fail")
			}
			if !strings.Contains(err.Error(), tt.errorContains) {
				t.Fatalf("expected error containing %q, got %q", tt.errorContains, err.Error())
			}
		})
	}
}

func TestValidateWithTables_TableCellAdditionalValidationCases(t *testing.T) {
	tests := []struct {
		name          string
		dslContent    string
		tables        map[string]*table.Table
		errorContains string
	}{
		{
			name: "Number Literal",
			dslContent: `
product Fastener {
    param diameter: number = table_cell("fasteners", 123, "diameter")
}`,
			tables:        validatorTestTables(),
			errorContains: "argument 2 of 'table_cell'",
		},
		{
			name: "Boolean Literal",
			dslContent: `
product Fastener {
    param diameter: number = table_cell("fasteners", true, "diameter")
}`,
			tables:        validatorTestTables(),
			errorContains: "argument 2 of 'table_cell'",
		},
		{
			name: "Duplicate Key Table",
			dslContent: `
product Fastener {
    param diameter: number = table_cell("fasteners", "M8x20", "diameter")
}`,
			tables:        validatorDuplicateKeyTables(),
			errorContains: "table validation error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast := parseDSLForValidatorTableTest(t, tt.dslContent)
			err := ValidateWithTables(ast, tt.tables)
			if err == nil {
				t.Fatal("expected ValidateWithTables to fail")
			}
			if !strings.Contains(err.Error(), tt.errorContains) {
				t.Fatalf("expected error containing %q, got %q", tt.errorContains, err.Error())
			}
			if tt.name == "Duplicate Key Table" && !strings.Contains(err.Error(), "duplicate") {
				t.Fatalf("expected duplicate-key validation error, got %q", err.Error())
			}
		})
	}
}

func TestValidateWithTables_BuiltinsUnaffected(t *testing.T) {
	ast := parseDSLForValidatorTableTest(t, `
product Test {
    param width: number = 13
    param rounded: number = round_up(width, 5)
}`)
	if err := ValidateWithTables(ast, validatorTestTables()); err != nil {
		t.Fatalf("ValidateWithTables unexpectedly failed for numeric builtin: %v", err)
	}
}

func parseDSLForValidatorTableTest(t *testing.T, content string) *AST {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "validator_table_lookup_*.dsl")
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

	ast, err := Parse(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to parse DSL: %v", err)
	}
	return ast
}

func validatorTestTables() map[string]*table.Table {
	return map[string]*table.Table{
		"fasteners": {
			SchemaVersion: table.SchemaVersion,
			Name:          "fasteners",
			KeyColumn:     "code",
			Columns: []table.Column{
				{Name: "code", Type: table.ColumnTypeString, Required: true},
				{Name: "diameter", Type: table.ColumnTypeNumber, Required: true},
				{Name: "coated", Type: table.ColumnTypeBoolean, Required: true},
			},
			Rows: []table.Row{
				{Values: map[string]table.Value{
					"code":     {Type: table.ColumnTypeString, String: "M8x20"},
					"diameter": {Type: table.ColumnTypeNumber, Number: json.Number("8")},
					"coated":   {Type: table.ColumnTypeBoolean, Boolean: true},
				}},
			},
		},
	}
}

func validatorDuplicateKeyTables() map[string]*table.Table {
	tables := validatorTestTables()
	tables["fasteners"].Rows = append(tables["fasteners"].Rows, table.Row{
		Values: map[string]table.Value{
			"code":     {Type: table.ColumnTypeString, String: "M8x20"},
			"diameter": {Type: table.ColumnTypeNumber, Number: json.Number("10")},
			"coated":   {Type: table.ColumnTypeBoolean, Boolean: false},
		},
	})
	return tables
}
