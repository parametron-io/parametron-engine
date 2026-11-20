package dsl

import (
	"encoding/json"
	"strings"
	"testing"

	"parametron/internal/engine/table"
)

func metadataValidatorTables() map[string]*table.Table {
	return map[string]*table.Table{
		"materials": {
			SchemaVersion: table.SchemaVersion,
			Name:          "materials",
			KeyColumn:     "grade",
			Columns: []table.Column{
				{Name: "grade", Type: table.ColumnTypeString, Required: true},
				{Name: "name", Type: table.ColumnTypeString, Required: true},
				{Name: "density", Type: table.ColumnTypeNumber, Required: true},
				{Name: "approved", Type: table.ColumnTypeBoolean, Required: true},
			},
			Rows: []table.Row{
				{Values: map[string]table.Value{
					"grade":    {Type: table.ColumnTypeString, String: "S235"},
					"name":     {Type: table.ColumnTypeString, String: "S235JR"},
					"density":  {Type: table.ColumnTypeNumber, Number: json.Number("7.85")},
					"approved": {Type: table.ColumnTypeBoolean, Boolean: true},
				}},
			},
		},
	}
}

func TestValidate_TargetMetadata_SupportedScalarExpressions(t *testing.T) {
	tests := []struct {
		name string
		dsl  string
	}{
		{
			name: "string literal",
			dsl: `
product Demo {
    target Pad: metadata material = "S235JR"
}`,
		},
		{
			name: "number literal",
			dsl: `
product Demo {
    target Pad: metadata count = 5
}`,
		},
		{
			name: "boolean literal",
			dsl: `
product Demo {
    target Pad: metadata active = true
}`,
		},
		{
			name: "let binding",
			dsl: `
product Demo {
    let base = "S235JR"
    target Pad: metadata material = base
}`,
		},
		{
			name: "param binding",
			dsl: `
product Demo {
    param base: string = "S235JR"
    target Pad: metadata material = base
}`,
		},
		{
			name: "ternary",
			dsl: `
product Demo {
    param heavy: boolean = true
    target Pad: metadata material = heavy ? "S235JR" : "S355"
}`,
		},
		{
			name: "enum condition ternary",
			dsl: `
product Demo {
    param variant: enum { standard, heavy } = heavy
    target Pad: metadata count = variant == heavy ? 1 : 2
}`,
		},
		{
			name: "action and metadata together",
			dsl: `
product Demo {
    target Pad:
        action = suppress
        metadata material = "S235JR"
}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, err := parseDSLForValidatorActionTest(t, tt.dsl)
			if err != nil {
				t.Fatalf("Parse: unexpected error: %v", err)
			}
			if err := Validate(ast); err != nil {
				t.Fatalf("Validate: unexpected error: %v", err)
			}
		})
	}
}

func TestValidate_TargetMetadata_TableLookupTypes(t *testing.T) {
	tables := metadataValidatorTables()
	tests := []struct {
		name   string
		column string
	}{
		{name: "string column", column: "name"},
		{name: "number column", column: "density"},
		{name: "boolean column", column: "approved"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, err := parseDSLForValidatorActionTest(t, `
product Demo {
    target Pad: metadata value = table_cell("materials", "S235", "`+tt.column+`")
}`)
			if err != nil {
				t.Fatalf("Parse: unexpected error: %v", err)
			}
			if err := ValidateWithTables(ast, tables); err != nil {
				t.Fatalf("ValidateWithTables: unexpected error: %v", err)
			}
		})
	}
}

func TestValidate_TargetMetadata_NonEmptySettingsRejected(t *testing.T) {
	ast, err := parseDSLForValidatorActionTest(t, `
product Demo {
    target Pad: metadata material = "S235JR" {
        source = "supplier"
    }
}`)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}

	err = Validate(ast)
	if err == nil {
		t.Fatal("Validate: expected non-empty metadata settings to fail")
	}
	want := "in target metadata 'Pad.material': metadata settings are not supported in DSL version 1.0"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected error containing %q, got %q", want, err.Error())
	}
}

func TestValidate_TargetMetadata_InvalidExpressions(t *testing.T) {
	tests := []struct {
		name string
		expr string
	}{
		{name: "undefined binding", expr: "unknown"},
		{name: "mixed arithmetic", expr: "1 + true"},
		{name: "action value", expr: "suppress"},
		{name: "array value", expr: "[1, 2]"},
		{name: "mismatched ternary", expr: `true ? 1 : "two"`},
	}

	want := "metadata value must resolve to a supported scalar type (number, string, or boolean)"
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, err := parseDSLForValidatorActionTest(t, `
product Demo {
    target Pad: metadata material = `+tt.expr+`
}`)
			if err != nil {
				t.Fatalf("Parse: unexpected error: %v", err)
			}
			err = Validate(ast)
			if err == nil {
				t.Fatalf("Validate: expected error for expression %q", tt.expr)
			}
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("expected error containing %q, got %q", want, err.Error())
			}
		})
	}
}

func TestValidate_TargetMetadata_EmptySettingsAccepted(t *testing.T) {
	ast, err := parseDSLForValidatorActionTest(t, `
product Demo {
    target Pad: metadata material = "S235JR" { }
}`)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if err := Validate(ast); err != nil {
		t.Fatalf("Validate: unexpected error for empty settings block: %v", err)
	}
}
