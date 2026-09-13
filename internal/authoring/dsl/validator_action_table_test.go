package dsl

import (
	"strings"
	"testing"

	"parametron/internal/engine/table"
)

// actionTableTestTables returns the permanent Task 5 in-memory "variants"
// table: an ordinary string-backed table with no action-specific schema or
// metadata. Its "chamferAction" column is consumed contextually as an
// action only through expected-type threading in validateTableCellExpression;
// the column itself remains ColumnTypeString like any other string column.
func actionTableTestTables() map[string]*table.Table {
	return map[string]*table.Table{
		"variants": {
			SchemaVersion: table.SchemaVersion,
			Name:          "variants",
			KeyColumn:     "variant",
			Columns: []table.Column{
				{Name: "variant", Type: table.ColumnTypeString, Required: true},
				{Name: "chamferAction", Type: table.ColumnTypeString, Required: true},
			},
			Rows: []table.Row{
				{Values: map[string]table.Value{
					"variant":       {Type: table.ColumnTypeString, String: "A"},
					"chamferAction": {Type: table.ColumnTypeString, String: "suppress"},
				}},
				{Values: map[string]table.Value{
					"variant":       {Type: table.ColumnTypeString, String: "B"},
					"chamferAction": {Type: table.ColumnTypeString, String: "hide"},
				}},
				{Values: map[string]table.Value{
					"variant":       {Type: table.ColumnTypeString, String: "C"},
					"chamferAction": {Type: table.ColumnTypeString, String: "delete"},
				}},
				{Values: map[string]table.Value{
					"variant":       {Type: table.ColumnTypeString, String: "D"},
					"chamferAction": {Type: table.ColumnTypeString, String: "keep"},
				}},
				{Values: map[string]table.Value{
					"variant":       {Type: table.ColumnTypeString, String: "E"},
					"chamferAction": {Type: table.ColumnTypeString, String: "unsuppress"},
				}},
				{Values: map[string]table.Value{
					"variant":       {Type: table.ColumnTypeString, String: "F"},
					"chamferAction": {Type: table.ColumnTypeString, String: "unhide"},
				}},
				{Values: map[string]table.Value{
					"variant":       {Type: table.ColumnTypeString, String: "BAD"},
					"chamferAction": {Type: table.ColumnTypeString, String: "explode"},
				}},
				{Values: map[string]table.Value{
					"variant":       {Type: table.ColumnTypeString, String: "CASE"},
					"chamferAction": {Type: table.ColumnTypeString, String: "Suppress"},
				}},
				{Values: map[string]table.Value{
					"variant":       {Type: table.ColumnTypeString, String: "SPACE"},
					"chamferAction": {Type: table.ColumnTypeString, String: "suppress "},
				}},
			},
		},
	}
}

// TestValidateWithTables_TargetAction_TableSchemaNeutrality permanently
// proves Task 5 introduces no action-specific table schema or column type:
// the same ordinary string column is consumed both as a plain string and,
// contextually, as an action, with no table metadata distinguishing the two
// uses.
func TestValidateWithTables_TargetAction_TableSchemaNeutrality(t *testing.T) {
	tables := actionTableTestTables()

	t.Run("consumed as ordinary string", func(t *testing.T) {
		ast := parseDSLForValidatorTableTest(t, `
product Demo {
    param chosen: string = table_cell("variants", "A", "chamferAction")
}`)
		if err := ValidateWithTables(ast, tables); err != nil {
			t.Fatalf("ValidateWithTables: unexpected error consuming action column as string: %v", err)
		}
	})

	t.Run("consumed contextually as action", func(t *testing.T) {
		ast := parseDSLForValidatorTableTest(t, `
product Demo {
    target Chamfer: action = table_cell("variants", "A", "chamferAction")
}`)
		if err := ValidateWithTables(ast, tables); err != nil {
			t.Fatalf("ValidateWithTables: unexpected error consuming string column as action: %v", err)
		}
	})
}

// TestValidateWithTables_TargetAction_CanonicalSelectedValueMatrix
// permanently locks that all six canonical action values, when statically
// selected via a literal table row key, Parse PASS and ValidateWithTables
// PASS.
func TestValidateWithTables_TargetAction_CanonicalSelectedValueMatrix(t *testing.T) {
	tables := actionTableTestTables()
	matrix := []struct {
		row    string
		action string
	}{
		{"A", "suppress"},
		{"B", "hide"},
		{"C", "delete"},
		{"D", "keep"},
		{"E", "unsuppress"},
		{"F", "unhide"},
	}

	for _, tt := range matrix {
		t.Run(tt.row+"_"+tt.action, func(t *testing.T) {
			ast := parseDSLForValidatorTableTest(t, `
product Demo {
    target Chamfer: action = table_cell("variants", "`+tt.row+`", "chamferAction")
}`)
			if err := ValidateWithTables(ast, tables); err != nil {
				t.Fatalf("ValidateWithTables: unexpected error for row %q selecting canonical action %q: %v", tt.row, tt.action, err)
			}
		})
	}
}

// TestValidateWithTables_TargetAction_StaticInvalidSelectedValueMatrix
// permanently locks that statically selected noncanonical table values fail
// validation: an out-of-domain value ("explode"), a case-mismatched value
// ("Suppress"), and a whitespace-noncanonical value ("suppress "). No case
// folding, trimming, or aliasing is performed.
func TestValidateWithTables_TargetAction_StaticInvalidSelectedValueMatrix(t *testing.T) {
	tables := actionTableTestTables()
	matrix := []struct {
		name          string
		row           string
		errorContains []string
	}{
		{
			name: "out of domain",
			row:  "BAD",
			errorContains: []string{
				"selected invalid action value 'explode'",
				`from row "BAD"`,
				"allowed values are [keep suppress unsuppress hide unhide delete]",
			},
		},
		{
			name: "case mismatch",
			row:  "CASE",
			errorContains: []string{
				"selected invalid action value 'Suppress'",
				`from row "CASE"`,
			},
		},
		{
			name: "whitespace noncanonical",
			row:  "SPACE",
			errorContains: []string{
				"selected invalid action value 'suppress '",
				`from row "SPACE"`,
			},
		},
	}

	for _, tt := range matrix {
		t.Run(tt.name, func(t *testing.T) {
			ast := parseDSLForValidatorTableTest(t, `
product Demo {
    target Chamfer: action = table_cell("variants", "`+tt.row+`", "chamferAction")
}`)
			err := ValidateWithTables(ast, tables)
			if err == nil {
				t.Fatalf("ValidateWithTables: expected error for row %q", tt.row)
			}
			for _, want := range tt.errorContains {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("expected error containing %q, got %q", want, err.Error())
				}
			}
		})
	}
}

// TestValidateWithTables_TargetAction_InvalidSelectedValueDiagnosticExact
// locks the exact, full diagnostic shape produced when a statically known
// row key selects a noncanonical action value, matching production output
// verbatim.
func TestValidateWithTables_TargetAction_InvalidSelectedValueDiagnosticExact(t *testing.T) {
	ast := parseDSLForValidatorTableTest(t, `
product Demo {
    target Chamfer: action = table_cell("variants", "BAD", "chamferAction")
}`)
	err := ValidateWithTables(ast, actionTableTestTables())
	if err == nil {
		t.Fatal("ValidateWithTables: expected error")
	}
	want := `validation error in product 'Demo': in target action 'Chamfer': function 'table_cell' returns a string from table 'variants' column 'chamferAction', but expected type is 'action': selected invalid action value 'explode' from row "BAD"; allowed values are [keep suppress unsuppress hide unhide delete]`
	if err.Error() != want {
		t.Fatalf("expected exact error:\n%q\ngot:\n%q", want, err.Error())
	}
}

// TestValidateWithTables_TargetAction_DynamicRowKeyDoesNotFreezeDefault
// proves a dynamic (parameter-bound) row key validates as an action
// expression without the validator freezing the selection to the
// parameter's default row: static analysis only confirms the table, column,
// and row-key expression type, deferring the actual selected value to
// planner evaluation.
func TestValidateWithTables_TargetAction_DynamicRowKeyDoesNotFreezeDefault(t *testing.T) {
	ast := parseDSLForValidatorTableTest(t, `
product Demo {
    param variant: string = "A"

    target Chamfer:
        action = table_cell("variants", variant, "chamferAction")
}`)
	if err := ValidateWithTables(ast, actionTableTestTables()); err != nil {
		t.Fatalf("ValidateWithTables: unexpected error for dynamic row-key action expression: %v", err)
	}
}

// TestValidateWithTables_TargetAction_WrongColumnTypeMatrix permanently
// proves number-backed and boolean-backed table columns remain
// action-invalid: Task 5 does not introduce number->action or
// boolean->action coercion. Reuses the existing validatorTestTables()
// "fasteners" fixture rather than introducing a new table.
func TestValidateWithTables_TargetAction_WrongColumnTypeMatrix(t *testing.T) {
	tables := validatorTestTables()

	tests := []struct {
		name          string
		column        string
		errorContains string
	}{
		{name: "number column", column: "diameter", errorContains: "returns a number"},
		{name: "boolean column", column: "coated", errorContains: "returns a boolean"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast := parseDSLForValidatorTableTest(t, `
product Fastener {
    target Pad: action = table_cell("fasteners", "M8x20", "`+tt.column+`")
}`)
			err := ValidateWithTables(ast, tables)
			if err == nil {
				t.Fatalf("ValidateWithTables: expected error for %s used as action", tt.name)
			}
			if !strings.Contains(err.Error(), tt.errorContains) {
				t.Fatalf("expected error containing %q, got %q", tt.errorContains, err.Error())
			}
			if !strings.Contains(err.Error(), "expected type is 'action'") {
				t.Fatalf("expected 'action' expected-type diagnostic, got %q", err.Error())
			}
		})
	}
}

// TestValidateWithTables_TargetAction_DirectStringRemainsInvalid preserves
// the Task 4 boundary in table-aware validation: a direct string literal
// still does not coerce to action, even though Task 5 permits string-backed
// table results contextually.
func TestValidateWithTables_TargetAction_DirectStringRemainsInvalid(t *testing.T) {
	ast := parseDSLForValidatorTableTest(t, `
product Demo {
    target Pad: action = "suppress"
}`)
	err := ValidateWithTables(ast, actionTableTestTables())
	if err == nil {
		t.Fatal("ValidateWithTables: expected error for direct string literal in action context")
	}
	if !strings.Contains(err.Error(), "expected action literal, but got string literal") {
		t.Fatalf("expected string-literal rejection, got %q", err.Error())
	}
}

// TestValidateWithTables_TargetAction_StringBindingRemainsInvalid proves a
// string-typed parameter binding does not coerce to action, even when
// planner-visible tables are present.
func TestValidateWithTables_TargetAction_StringBindingRemainsInvalid(t *testing.T) {
	ast := parseDSLForValidatorTableTest(t, `
product Demo {
    param desired: string = "suppress"
    target Pad: action = desired
}`)
	err := ValidateWithTables(ast, actionTableTestTables())
	if err == nil {
		t.Fatal("ValidateWithTables: expected error for string parameter binding in action context")
	}
	if !strings.Contains(err.Error(), "binding reference 'desired' has type 'string', but expected 'action'") {
		t.Fatalf("expected binding type-mismatch diagnostic, got %q", err.Error())
	}
}

// TestValidateWithTables_TargetAction_LetFromTableDoesNotBecomeAction proves
// Task 5 does not introduce action-valued lets: a let bound to a
// table_cell lookup infers as an ordinary string (per the existing
// binding-type inference model, which does not thread an expected action
// type through let inference), so referencing it in action context fails.
func TestValidateWithTables_TargetAction_LetFromTableDoesNotBecomeAction(t *testing.T) {
	ast := parseDSLForValidatorTableTest(t, `
product Demo {
    param variant: string = "A"

    let desired =
        table_cell("variants", variant, "chamferAction")

    target Pad: action = desired
}`)
	err := ValidateWithTables(ast, actionTableTestTables())
	if err == nil {
		t.Fatal("ValidateWithTables: action-valued let via table_cell must not validate as an action binding")
	}
	if !strings.Contains(err.Error(), "binding reference 'desired' has type 'string', but expected 'action'") {
		t.Fatalf("expected binding type-mismatch diagnostic, got %q", err.Error())
	}
}
