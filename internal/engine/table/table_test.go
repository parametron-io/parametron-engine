package table

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParse_ValidTable(t *testing.T) {
	table, err := Parse([]byte(validTableJSON))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if table.SchemaVersion != SchemaVersion {
		t.Fatalf("expected schema version %q, got %q", SchemaVersion, table.SchemaVersion)
	}
	if table.Name != "fastener_catalog" {
		t.Fatalf("unexpected table name: %q", table.Name)
	}
	if table.KeyColumn != "code" {
		t.Fatalf("unexpected key column: %q", table.KeyColumn)
	}
	if len(table.Columns) != 4 {
		t.Fatalf("expected 4 columns, got %d", len(table.Columns))
	}
	if table.Columns[0].Name != "code" || table.Columns[1].Name != "diameter" || table.Columns[2].Name != "length" || table.Columns[3].Name != "coated" {
		t.Fatalf("column order was not preserved: %+v", table.Columns)
	}
	if len(table.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(table.Rows))
	}
	if table.Rows[0].Values["code"].String != "M8x20" {
		t.Fatalf("unexpected first row key value: %+v", table.Rows[0].Values["code"])
	}
	if table.Rows[1].Values["code"].String != "M8x30" {
		t.Fatalf("unexpected second row key value: %+v", table.Rows[1].Values["code"])
	}
	if _, ok := table.Rows[1].Values["coated"]; ok {
		t.Fatal("optional omitted field should not be present in parsed row")
	}
}

func TestParse_EmptyRowsAllowed(t *testing.T) {
	table, err := Parse([]byte(`{
		"schemaVersion": "1.0",
		"name": "fastener_catalog",
		"keyColumn": "code",
		"columns": [
			{ "name": "code", "type": "string", "required": true }
		],
		"rows": []
	}`))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(table.Rows) != 0 {
		t.Fatalf("expected empty rows, got %d", len(table.Rows))
	}
}

func TestValidate_ValidProgrammaticTable(t *testing.T) {
	table := validProgrammaticTable()

	if err := Validate(table); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

func TestValidate_IdempotentAndNonMutating(t *testing.T) {
	table := validProgrammaticTable()
	before := cloneTable(table)

	canonicalBefore, err := CanonicalJSON(table)
	if err != nil {
		t.Fatalf("CanonicalJSON(before) returned error: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := Validate(table); err != nil {
			t.Fatalf("Validate call %d returned error: %v", i+1, err)
		}
	}

	if !reflect.DeepEqual(table, before) {
		t.Fatalf("Validate mutated table\nbefore: %#v\nafter: %#v", before, table)
	}

	canonicalAfter, err := CanonicalJSON(table)
	if err != nil {
		t.Fatalf("CanonicalJSON(after) returned error: %v", err)
	}
	if string(canonicalBefore) != string(canonicalAfter) {
		t.Fatalf("expected canonical JSON to be stable, got %q and %q", canonicalBefore, canonicalAfter)
	}
}

func TestValidate_NilTableFails(t *testing.T) {
	err := Validate(nil)
	if err == nil {
		t.Fatal("expected Validate to fail")
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), "table must not be nil") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestValidate_InvalidProgrammaticTableFails(t *testing.T) {
	table := validProgrammaticTable()
	table.Rows = append(table.Rows, Row{
		Values: map[string]Value{
			"code":  {Type: ColumnTypeString, String: "M8x40"},
			"extra": {Type: ColumnTypeString, String: "nope"},
		},
	})

	err := Validate(table)
	if err == nil {
		t.Fatal("expected Validate to fail")
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), `row 2 has unknown column "extra"`) {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestCanonicalJSON_InvalidProgrammaticTableRejected(t *testing.T) {
	table := validProgrammaticTable()
	table.Name = "   "

	_, err := CanonicalJSON(table)
	if err == nil {
		t.Fatal("expected CanonicalJSON to fail")
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestComputeFingerprint_InvalidProgrammaticTableRejected(t *testing.T) {
	table := validProgrammaticTable()
	table.Rows = nil

	_, err := ComputeFingerprint(table)
	if err == nil {
		t.Fatal("expected ComputeFingerprint to fail")
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), "rows is required") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestValidate_SchemaValidationFailures(t *testing.T) {
	tests := []struct {
		name    string
		table   func() *Table
		wantMsg string
	}{
		{
			name: "unknown_column_deterministic",
			table: func() *Table {
				table := validProgrammaticTable()
				table.Rows = []Row{{
					Values: map[string]Value{
						"code": {Type: ColumnTypeString, String: "M8x20"},
						"zzz":  {Type: ColumnTypeString, String: "later"},
						"aaa":  {Type: ColumnTypeString, String: "first"},
					},
				}}
				return table
			},
			wantMsg: `row 0 has unknown column "aaa"`,
		},
		{
			name: "duplicate_key_value",
			table: func() *Table {
				table := validProgrammaticTable()
				table.Rows[1].Values["code"] = Value{Type: ColumnTypeString, String: "M8x20"}
				return table
			},
			wantMsg: `duplicate keyColumn value "M8x20"`,
		},
		{
			name: "unsupported_column_type",
			table: func() *Table {
				table := validProgrammaticTable()
				table.Columns[1].Type = ColumnType("enum")
				return table
			},
			wantMsg: `column "diameter" has unsupported type "enum"`,
		},
		{
			name: "blank_name",
			table: func() *Table {
				table := validProgrammaticTable()
				table.Name = " "
				return table
			},
			wantMsg: "name is required",
		},
		{
			name: "blank_key_column",
			table: func() *Table {
				table := validProgrammaticTable()
				table.KeyColumn = "\t"
				return table
			},
			wantMsg: "keyColumn is required",
		},
		{
			name: "blank_column_name",
			table: func() *Table {
				table := validProgrammaticTable()
				table.Columns[0].Name = " "
				return table
			},
			wantMsg: "column 0: name is required",
		},
		{
			name: "rows_nil_rejected",
			table: func() *Table {
				table := validProgrammaticTable()
				table.Rows = nil
				return table
			},
			wantMsg: "rows is required",
		},
		{
			name: "rows_empty_allowed",
			table: func() *Table {
				table := validProgrammaticTable()
				table.Rows = []Row{}
				return table
			},
			wantMsg: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.table())
			if tt.wantMsg == "" {
				if err != nil {
					t.Fatalf("expected Validate to succeed, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected Validate to fail")
			}
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("expected ErrValidation, got %v", err)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Fatalf("unexpected error message: %v", err)
			}
		})
	}
}

func TestValidate_UnknownColumnErrorDeterministic(t *testing.T) {
	table := validProgrammaticTable()
	table.Rows = []Row{{
		Values: map[string]Value{
			"code": {Type: ColumnTypeString, String: "M8x20"},
			"zzz":  {Type: ColumnTypeString, String: "last"},
			"aaa":  {Type: ColumnTypeString, String: "first"},
			"mmm":  {Type: ColumnTypeString, String: "middle"},
		},
	}}

	err := Validate(table)
	if err == nil {
		t.Fatal("expected Validate to fail")
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), `row 0 has unknown column "aaa"`) {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestValidate_KeyColumnRules(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Table)
		wantMsg string
	}{
		{
			name: "key_column_wrong_type",
			mutate: func(table *Table) {
				table.KeyColumn = "diameter"
			},
			wantMsg: `keyColumn "diameter" must have type "string"`,
		},
		{
			name: "key_column_not_required",
			mutate: func(table *Table) {
				table.Columns[0].Required = false
			},
			wantMsg: `keyColumn "code" must be required`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			table := validProgrammaticTable()
			tt.mutate(table)

			err := Validate(table)
			if err == nil {
				t.Fatal("expected Validate to fail")
			}
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("expected ErrValidation, got %v", err)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Fatalf("unexpected error message: %v", err)
			}
		})
	}
}

func TestValidate_SchemaVersionStrict(t *testing.T) {
	tests := []struct {
		name    string
		version string
		wantErr bool
		wantMsg string
	}{
		{name: "empty", version: "", wantErr: true, wantMsg: `schemaVersion must be "1.0"`},
		{name: "whitespace", version: "   ", wantErr: true, wantMsg: `schemaVersion must be "1.0"`},
		{name: "unsupported_v2", version: "v2.0", wantErr: true, wantMsg: `schemaVersion must be "1.0"`},
		{name: "unsupported_numeric", version: "2.0", wantErr: true, wantMsg: `schemaVersion must be "1.0"`},
		{name: "correct", version: SchemaVersion, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			table := validProgrammaticTable()
			table.SchemaVersion = tt.version

			err := Validate(table)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected Validate to fail")
				}
				if !errors.Is(err, ErrValidation) {
					t.Fatalf("expected ErrValidation, got %v", err)
				}
				if !strings.Contains(err.Error(), tt.wantMsg) {
					t.Fatalf("unexpected error message: %v", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("expected Validate to succeed, got %v", err)
			}
		})
	}
}

func TestValidate_RequiredNonKeyColumnMissing(t *testing.T) {
	table := validProgrammaticTable()
	delete(table.Rows[0].Values, "diameter")

	err := Validate(table)
	if err == nil {
		t.Fatal("expected Validate to fail")
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), `row 0 is missing required column "diameter"`) {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestValidate_TypeMismatchMatrix(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Table)
		wantErr bool
		wantMsg string
	}{
		{
			name: "string_column_with_number_value",
			mutate: func(table *Table) {
				table.Rows[0].Values["code"] = Value{Type: ColumnTypeNumber, Number: json.Number("42")}
			},
			wantErr: true,
			wantMsg: `row 0 column "code" must have type "string"`,
		},
		{
			name: "number_column_with_string_value",
			mutate: func(table *Table) {
				table.Rows[0].Values["diameter"] = Value{Type: ColumnTypeString, String: "8"}
			},
			wantErr: true,
			wantMsg: `row 0 column "diameter" must have type "number"`,
		},
		{
			name: "boolean_column_with_string_value",
			mutate: func(table *Table) {
				table.Rows[0].Values["coated"] = Value{Type: ColumnTypeString, String: "true"}
			},
			wantErr: true,
			wantMsg: `row 0 column "coated" must have type "boolean"`,
		},
		{
			name: "valid_value_types",
			mutate: func(table *Table) {
				table.Rows[0].Values["code"] = Value{Type: ColumnTypeString, String: "M8x20"}
				table.Rows[0].Values["diameter"] = Value{Type: ColumnTypeNumber, Number: json.Number("8")}
				table.Rows[0].Values["coated"] = Value{Type: ColumnTypeBoolean, Boolean: true}
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			table := validProgrammaticTable()
			tt.mutate(table)

			err := Validate(table)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected Validate to fail")
				}
				if !errors.Is(err, ErrValidation) {
					t.Fatalf("expected ErrValidation, got %v", err)
				}
				if !strings.Contains(err.Error(), tt.wantMsg) {
					t.Fatalf("unexpected error message: %v", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("expected Validate to succeed, got %v", err)
			}
		})
	}
}

func TestCanonicalAndFingerprintStableAfterValidate(t *testing.T) {
	table := validProgrammaticTable()

	canonicalBefore, err := CanonicalJSON(table)
	if err != nil {
		t.Fatalf("CanonicalJSON(before) returned error: %v", err)
	}
	fingerprintBefore, err := ComputeFingerprint(table)
	if err != nil {
		t.Fatalf("ComputeFingerprint(before) returned error: %v", err)
	}

	if err := Validate(table); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}

	canonicalAfter, err := CanonicalJSON(table)
	if err != nil {
		t.Fatalf("CanonicalJSON(after) returned error: %v", err)
	}
	fingerprintAfter, err := ComputeFingerprint(table)
	if err != nil {
		t.Fatalf("ComputeFingerprint(after) returned error: %v", err)
	}

	if string(canonicalBefore) != string(canonicalAfter) {
		t.Fatalf("expected canonical JSON to remain stable, got %q and %q", canonicalBefore, canonicalAfter)
	}
	if fingerprintBefore != fingerprintAfter {
		t.Fatalf("expected fingerprint to remain stable, got %q and %q", fingerprintBefore, fingerprintAfter)
	}
}

func TestComputeFingerprint_RepeatedCallsStable(t *testing.T) {
	table, err := Parse([]byte(validTableJSON))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	first, err := ComputeFingerprint(table)
	if err != nil {
		t.Fatalf("first ComputeFingerprint returned error: %v", err)
	}
	second, err := ComputeFingerprint(table)
	if err != nil {
		t.Fatalf("second ComputeFingerprint returned error: %v", err)
	}

	if first != second {
		t.Fatalf("expected stable fingerprint, got %q and %q", first, second)
	}
}

func TestParse_ValidationFailures(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantMsg string
	}{
		{
			name: "missing_schema_version",
			input: `{
				"name": "fastener_catalog",
				"keyColumn": "code",
				"columns": [{ "name": "code", "type": "string", "required": true }],
				"rows": []
			}`,
			wantMsg: "schemaVersion is required",
		},
		{
			name: "unsupported_schema_version",
			input: `{
				"schemaVersion": "2.0",
				"name": "fastener_catalog",
				"keyColumn": "code",
				"columns": [{ "name": "code", "type": "string", "required": true }],
				"rows": []
			}`,
			wantMsg: `schemaVersion must be "1.0"`,
		},
		{
			name: "missing_name",
			input: `{
				"schemaVersion": "1.0",
				"keyColumn": "code",
				"columns": [{ "name": "code", "type": "string", "required": true }],
				"rows": []
			}`,
			wantMsg: "name is required",
		},
		{
			name: "missing_key_column",
			input: `{
				"schemaVersion": "1.0",
				"name": "fastener_catalog",
				"columns": [{ "name": "code", "type": "string", "required": true }],
				"rows": []
			}`,
			wantMsg: "keyColumn is required",
		},
		{
			name: "empty_columns",
			input: `{
				"schemaVersion": "1.0",
				"name": "fastener_catalog",
				"keyColumn": "code",
				"columns": [],
				"rows": []
			}`,
			wantMsg: "columns must be non-empty",
		},
		{
			name: "duplicate_column_names",
			input: `{
				"schemaVersion": "1.0",
				"name": "fastener_catalog",
				"keyColumn": "code",
				"columns": [
					{ "name": "code", "type": "string", "required": true },
					{ "name": "code", "type": "string", "required": true }
				],
				"rows": []
			}`,
			wantMsg: `column names must be unique: "code"`,
		},
		{
			name: "unsupported_column_type",
			input: `{
				"schemaVersion": "1.0",
				"name": "fastener_catalog",
				"keyColumn": "code",
				"columns": [
					{ "name": "code", "type": "enum", "required": true }
				],
				"rows": []
			}`,
			wantMsg: `column "code" has unsupported type "enum"`,
		},
		{
			name: "key_column_not_found",
			input: `{
				"schemaVersion": "1.0",
				"name": "fastener_catalog",
				"keyColumn": "code",
				"columns": [
					{ "name": "sku", "type": "string", "required": true }
				],
				"rows": []
			}`,
			wantMsg: `keyColumn "code" must exist in columns`,
		},
		{
			name: "key_column_not_required",
			input: `{
				"schemaVersion": "1.0",
				"name": "fastener_catalog",
				"keyColumn": "code",
				"columns": [
					{ "name": "code", "type": "string", "required": false }
				],
				"rows": []
			}`,
			wantMsg: `keyColumn "code" must be required`,
		},
		{
			name: "key_column_must_be_string",
			input: `{
				"schemaVersion": "1.0",
				"name": "fastener_catalog",
				"keyColumn": "id",
				"columns": [
					{ "name": "id", "type": "number", "required": true }
				],
				"rows": []
			}`,
			wantMsg: `keyColumn "id" must have type "string"`,
		},
		{
			name: "row_missing_required_field",
			input: `{
				"schemaVersion": "1.0",
				"name": "fastener_catalog",
				"keyColumn": "code",
				"columns": [
					{ "name": "code", "type": "string", "required": true },
					{ "name": "diameter", "type": "number", "required": true }
				],
				"rows": [
					{ "code": "M8x20" }
				]
			}`,
			wantMsg: `row 0 is missing required column "diameter"`,
		},
		{
			name: "row_has_unknown_extra_field",
			input: `{
				"schemaVersion": "1.0",
				"name": "fastener_catalog",
				"keyColumn": "code",
				"columns": [
					{ "name": "code", "type": "string", "required": true }
				],
				"rows": [
					{ "code": "M8x20", "extra": "nope" }
				]
			}`,
			wantMsg: `row 0 has unknown column "extra"`,
		},
		{
			name: "row_type_mismatch_string",
			input: `{
				"schemaVersion": "1.0",
				"name": "fastener_catalog",
				"keyColumn": "code",
				"columns": [
					{ "name": "code", "type": "string", "required": true }
				],
				"rows": [
					{ "code": 42 }
				]
			}`,
			wantMsg: `row 0 column "code" must be a string`,
		},
		{
			name: "row_type_mismatch_number",
			input: `{
				"schemaVersion": "1.0",
				"name": "fastener_catalog",
				"keyColumn": "code",
				"columns": [
					{ "name": "code", "type": "string", "required": true },
					{ "name": "diameter", "type": "number", "required": true }
				],
				"rows": [
					{ "code": "M8x20", "diameter": "8" }
				]
			}`,
			wantMsg: `row 0 column "diameter" must be a number`,
		},
		{
			name: "row_type_mismatch_boolean",
			input: `{
				"schemaVersion": "1.0",
				"name": "fastener_catalog",
				"keyColumn": "code",
				"columns": [
					{ "name": "code", "type": "string", "required": true },
					{ "name": "coated", "type": "boolean", "required": true }
				],
				"rows": [
					{ "code": "M8x20", "coated": "true" }
				]
			}`,
			wantMsg: `row 0 column "coated" must be a boolean`,
		},
		{
			name: "null_rejected",
			input: `{
				"schemaVersion": "1.0",
				"name": "fastener_catalog",
				"keyColumn": "code",
				"columns": [
					{ "name": "code", "type": "string", "required": true }
				],
				"rows": [
					{ "code": null }
				]
			}`,
			wantMsg: `row 0 column "code" must not be null`,
		},
		{
			name: "duplicate_key_value",
			input: `{
				"schemaVersion": "1.0",
				"name": "fastener_catalog",
				"keyColumn": "code",
				"columns": [
					{ "name": "code", "type": "string", "required": true }
				],
				"rows": [
					{ "code": "M8x20" },
					{ "code": "M8x20" }
				]
			}`,
			wantMsg: `duplicate keyColumn value "M8x20"`,
		},
		{
			name: "empty_string_key_rejected",
			input: `{
				"schemaVersion": "1.0",
				"name": "fastener_catalog",
				"keyColumn": "code",
				"columns": [
					{ "name": "code", "type": "string", "required": true }
				],
				"rows": [
					{ "code": "" }
				]
			}`,
			wantMsg: `row 0 keyColumn "code" must be a non-empty string`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.input))
			if err == nil {
				t.Fatal("expected Parse to fail")
			}
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("expected ErrValidation, got %v", err)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Fatalf("unexpected error message: %v", err)
			}
		})
	}
}

func TestParse_SameLogicalContentDifferentRowFieldOrderSameFingerprint(t *testing.T) {
	left, err := Parse([]byte(validTableJSON))
	if err != nil {
		t.Fatalf("Parse(left) returned error: %v", err)
	}
	right, err := Parse([]byte(`{
		"schemaVersion": "1.0",
		"name": "fastener_catalog",
		"keyColumn": "code",
		"columns": [
			{ "name": "code", "type": "string", "required": true },
			{ "name": "diameter", "type": "number", "required": true },
			{ "name": "length", "type": "number", "required": true },
			{ "name": "coated", "type": "boolean", "required": false }
		],
		"rows": [
			{ "length": 20, "coated": true, "diameter": 8, "code": "M8x20" },
			{ "length": 30, "diameter": 8, "code": "M8x30" }
		]
	}`))
	if err != nil {
		t.Fatalf("Parse(right) returned error: %v", err)
	}

	leftFingerprint, err := ComputeFingerprint(left)
	if err != nil {
		t.Fatalf("ComputeFingerprint(left) returned error: %v", err)
	}
	rightFingerprint, err := ComputeFingerprint(right)
	if err != nil {
		t.Fatalf("ComputeFingerprint(right) returned error: %v", err)
	}

	if leftFingerprint != rightFingerprint {
		t.Fatalf("expected equal fingerprints, got %q and %q", leftFingerprint, rightFingerprint)
	}
}

func TestComputeFingerprint_ChangesWhenValueChanges(t *testing.T) {
	left, err := Parse([]byte(validTableJSON))
	if err != nil {
		t.Fatalf("Parse(left) returned error: %v", err)
	}
	right, err := Parse([]byte(strings.Replace(validTableJSON, `"length": 30`, `"length": 35`, 1)))
	if err != nil {
		t.Fatalf("Parse(right) returned error: %v", err)
	}

	assertDifferentFingerprint(t, left, right)
}

func TestComputeFingerprint_NumberLexemeChangesFingerprint(t *testing.T) {
	left, err := Parse([]byte(`{
		"schemaVersion": "1.0",
		"name": "numeric_table",
		"keyColumn": "code",
		"columns": [
			{ "name": "code", "type": "string", "required": true },
			{ "name": "diameter", "type": "number", "required": true }
		],
		"rows": [
			{ "code": "A", "diameter": 1 }
		]
	}`))
	if err != nil {
		t.Fatalf("Parse(left) returned error: %v", err)
	}
	right, err := Parse([]byte(`{
		"schemaVersion": "1.0",
		"name": "numeric_table",
		"keyColumn": "code",
		"columns": [
			{ "name": "code", "type": "string", "required": true },
			{ "name": "diameter", "type": "number", "required": true }
		],
		"rows": [
			{ "code": "A", "diameter": 1.0 }
		]
	}`))
	if err != nil {
		t.Fatalf("Parse(right) returned error: %v", err)
	}

	leftCanonical, err := CanonicalJSON(left)
	if err != nil {
		t.Fatalf("CanonicalJSON(left) returned error: %v", err)
	}
	rightCanonical, err := CanonicalJSON(right)
	if err != nil {
		t.Fatalf("CanonicalJSON(right) returned error: %v", err)
	}

	if string(leftCanonical) != `{"schemaVersion":"1.0","name":"numeric_table","keyColumn":"code","columns":[{"name":"code","type":"string","required":true},{"name":"diameter","type":"number","required":true}],"rows":[{"code":"A","diameter":1}]}` {
		t.Fatalf("unexpected left canonical JSON: %s", leftCanonical)
	}
	if string(rightCanonical) != `{"schemaVersion":"1.0","name":"numeric_table","keyColumn":"code","columns":[{"name":"code","type":"string","required":true},{"name":"diameter","type":"number","required":true}],"rows":[{"code":"A","diameter":1.0}]}` {
		t.Fatalf("unexpected right canonical JSON: %s", rightCanonical)
	}

	assertDifferentFingerprint(t, left, right)
}

func TestComputeFingerprint_ChangesWhenColumnOrderChanges(t *testing.T) {
	left, err := Parse([]byte(validTableJSON))
	if err != nil {
		t.Fatalf("Parse(left) returned error: %v", err)
	}
	right, err := Parse([]byte(`{
		"schemaVersion": "1.0",
		"name": "fastener_catalog",
		"keyColumn": "code",
		"columns": [
			{ "name": "code", "type": "string", "required": true },
			{ "name": "length", "type": "number", "required": true },
			{ "name": "diameter", "type": "number", "required": true },
			{ "name": "coated", "type": "boolean", "required": false }
		],
		"rows": [
			{ "code": "M8x20", "diameter": 8, "length": 20, "coated": true },
			{ "code": "M8x30", "diameter": 8, "length": 30 }
		]
	}`))
	if err != nil {
		t.Fatalf("Parse(right) returned error: %v", err)
	}

	assertDifferentFingerprint(t, left, right)
}

func TestComputeFingerprint_ChangesWhenRowOrderChanges(t *testing.T) {
	left, err := Parse([]byte(validTableJSON))
	if err != nil {
		t.Fatalf("Parse(left) returned error: %v", err)
	}
	right, err := Parse([]byte(`{
		"schemaVersion": "1.0",
		"name": "fastener_catalog",
		"keyColumn": "code",
		"columns": [
			{ "name": "code", "type": "string", "required": true },
			{ "name": "diameter", "type": "number", "required": true },
			{ "name": "length", "type": "number", "required": true },
			{ "name": "coated", "type": "boolean", "required": false }
		],
		"rows": [
			{ "code": "M8x30", "diameter": 8, "length": 30 },
			{ "code": "M8x20", "diameter": 8, "length": 20, "coated": true }
		]
	}`))
	if err != nil {
		t.Fatalf("Parse(right) returned error: %v", err)
	}

	assertDifferentFingerprint(t, left, right)
}

func TestComputeFingerprint_DoesNotDependOnMapInsertionOrder(t *testing.T) {
	left := &Table{
		SchemaVersion: SchemaVersion,
		Name:          "fastener_catalog",
		KeyColumn:     "code",
		Columns: []Column{
			{Name: "code", Type: ColumnTypeString, Required: true},
			{Name: "diameter", Type: ColumnTypeNumber, Required: true},
			{Name: "length", Type: ColumnTypeNumber, Required: true},
			{Name: "coated", Type: ColumnTypeBoolean, Required: false},
		},
		Rows: []Row{
			{Values: map[string]Value{
				"code":     {Type: ColumnTypeString, String: "M8x20"},
				"diameter": {Type: ColumnTypeNumber, Number: json.Number("8")},
				"length":   {Type: ColumnTypeNumber, Number: json.Number("20")},
				"coated":   {Type: ColumnTypeBoolean, Boolean: true},
			}},
		},
	}
	right := &Table{
		SchemaVersion: SchemaVersion,
		Name:          "fastener_catalog",
		KeyColumn:     "code",
		Columns:       append([]Column(nil), left.Columns...),
		Rows: []Row{
			{Values: map[string]Value{
				"coated":   {Type: ColumnTypeBoolean, Boolean: true},
				"length":   {Type: ColumnTypeNumber, Number: json.Number("20")},
				"diameter": {Type: ColumnTypeNumber, Number: json.Number("8")},
				"code":     {Type: ColumnTypeString, String: "M8x20"},
			}},
		},
	}

	leftFingerprint, err := ComputeFingerprint(left)
	if err != nil {
		t.Fatalf("ComputeFingerprint(left) returned error: %v", err)
	}
	rightFingerprint, err := ComputeFingerprint(right)
	if err != nil {
		t.Fatalf("ComputeFingerprint(right) returned error: %v", err)
	}

	if leftFingerprint != rightFingerprint {
		t.Fatalf("expected equal fingerprints, got %q and %q", leftFingerprint, rightFingerprint)
	}
}

func TestLoadFile_NonexistentFileWrapsIOError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")

	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected LoadFile to fail")
	}
	if !errors.Is(err, ErrIO) {
		t.Fatalf("expected ErrIO, got %v", err)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got %v", err)
	}

	var fileErr *FileError
	if !errors.As(err, &fileErr) {
		t.Fatalf("expected FileError, got %T", err)
	}
	if fileErr.Path != path {
		t.Fatalf("expected path %q, got %q", path, fileErr.Path)
	}
}

func TestParse_MalformedJSONWrapsDecodeError(t *testing.T) {
	_, err := Parse([]byte(`{"schemaVersion":"1.0"`))
	if err == nil {
		t.Fatal("expected Parse to fail")
	}
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("expected ErrDecode, got %v", err)
	}

	var decodeErr *DecodeError
	if !errors.As(err, &decodeErr) {
		t.Fatalf("expected DecodeError, got %T", err)
	}
}

func TestParse_TrailingJSONWrapsDecodeError(t *testing.T) {
	_, err := Parse([]byte(`{"schemaVersion":"1.0","name":"fastener_catalog","keyColumn":"code","columns":[{"name":"code","type":"string","required":true}],"rows":[]}{"extra":true}`))
	if err == nil {
		t.Fatal("expected Parse to fail")
	}
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("expected ErrDecode, got %v", err)
	}
	if errors.Is(err, ErrValidation) {
		t.Fatalf("expected trailing JSON to remain a decode error, got %v", err)
	}
}

func TestLoadFile_ValidTable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "table.json")
	if err := os.WriteFile(path, []byte(validTableJSON), 0644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	table, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile returned error: %v", err)
	}
	if table.Name != "fastener_catalog" {
		t.Fatalf("unexpected table name: %q", table.Name)
	}
}

func TestCanonicalJSON_UsesDeclaredColumnOrder(t *testing.T) {
	table, err := Parse([]byte(`{
		"schemaVersion": "1.0",
		"name": "ordered_table",
		"keyColumn": "code",
		"columns": [
			{ "name": "code", "type": "string", "required": true },
			{ "name": "b", "type": "number", "required": false },
			{ "name": "a", "type": "number", "required": false }
		],
		"rows": [
			{ "a": 1, "code": "x", "b": 2 }
		]
	}`))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	data, err := CanonicalJSON(table)
	if err != nil {
		t.Fatalf("CanonicalJSON returned error: %v", err)
	}

	got := string(data)
	if !strings.Contains(got, `"rows":[{"code":"x","b":2,"a":1}]`) {
		t.Fatalf("canonical row order did not follow columns: %s", got)
	}
}

func TestRowByKey_MissingKeyReturnsRuntimeError(t *testing.T) {
	table := validProgrammaticTable()

	_, err := table.RowByKey("M8x99")
	if err == nil {
		t.Fatal("expected RowByKey to fail")
	}
	if !errors.Is(err, ErrRuntime) {
		t.Fatalf("expected ErrRuntime, got %v", err)
	}
	if errors.Is(err, ErrValidation) {
		t.Fatalf("runtime lookup failure must not classify as validation error, got %v", err)
	}

	var runtimeErr *RuntimeError
	if !errors.As(err, &runtimeErr) {
		t.Fatalf("expected RuntimeError, got %T", err)
	}
	if runtimeErr.TableName != "fastener_catalog" {
		t.Fatalf("expected table name %q, got %q", "fastener_catalog", runtimeErr.TableName)
	}
	if runtimeErr.Operation != "row lookup" {
		t.Fatalf("expected operation %q, got %q", "row lookup", runtimeErr.Operation)
	}
	if runtimeErr.KeyColumn != "code" {
		t.Fatalf("expected key column %q, got %q", "code", runtimeErr.KeyColumn)
	}
	if runtimeErr.KeyValue != "M8x99" {
		t.Fatalf("expected key value %q, got %q", "M8x99", runtimeErr.KeyValue)
	}
	if runtimeErr.Message != "row not found" {
		t.Fatalf("expected message %q, got %q", "row not found", runtimeErr.Message)
	}

	want := `table runtime error: table "fastener_catalog": row lookup: code="M8x99": row not found`
	if err.Error() != want {
		t.Fatalf("unexpected error message:\nwant: %s\ngot:  %s", want, err.Error())
	}
}

func TestRowByKey_MissingKeyDeterministic(t *testing.T) {
	table := validProgrammaticTable()

	firstErr := rowByKeyError(t, table, "M8x99")
	secondErr := rowByKeyError(t, table, "M8x99")

	if !errors.Is(firstErr, ErrRuntime) || !errors.Is(secondErr, ErrRuntime) {
		t.Fatalf("expected both errors to classify as ErrRuntime, got %v and %v", firstErr, secondErr)
	}
	if firstErr.Error() != secondErr.Error() {
		t.Fatalf("expected deterministic runtime error message, got %q and %q", firstErr.Error(), secondErr.Error())
	}
}

func TestRowByKey_MissingKeyRuntimeErrorSupportsIsAndAs(t *testing.T) {
	table := validProgrammaticTable()

	err := rowByKeyError(t, table, "M8x99")

	if !errors.Is(err, ErrRuntime) {
		t.Fatalf("expected errors.Is(err, ErrRuntime) to succeed, got %v", err)
	}
	if errors.Is(err, ErrValidation) {
		t.Fatalf("expected missing-key lookup to remain a runtime error, got %v", err)
	}

	var runtimeErr *RuntimeError
	if !errors.As(err, &runtimeErr) {
		t.Fatalf("expected errors.As(err, *RuntimeError) to succeed, got %T", err)
	}
	if runtimeErr.TableName != "fastener_catalog" {
		t.Fatalf("expected table name %q, got %q", "fastener_catalog", runtimeErr.TableName)
	}
	if runtimeErr.Operation != "row lookup" {
		t.Fatalf("expected operation %q, got %q", "row lookup", runtimeErr.Operation)
	}
	if runtimeErr.KeyColumn != "code" {
		t.Fatalf("expected key column %q, got %q", "code", runtimeErr.KeyColumn)
	}
	if runtimeErr.KeyValue != "M8x99" {
		t.Fatalf("expected key value %q, got %q", "M8x99", runtimeErr.KeyValue)
	}
	if runtimeErr.Column != "" {
		t.Fatalf("expected column context to be empty, got %q", runtimeErr.Column)
	}
	if runtimeErr.Message != "row not found" {
		t.Fatalf("expected message %q, got %q", "row not found", runtimeErr.Message)
	}
	if runtimeErr.Err != nil {
		t.Fatalf("expected wrapped inner error to be nil, got %v", runtimeErr.Err)
	}

	want := `table runtime error: table "fastener_catalog": row lookup: code="M8x99": row not found`
	if err.Error() != want {
		t.Fatalf("unexpected error message:\nwant: %s\ngot:  %s", want, err.Error())
	}
}

func TestRowByKey_FoundRowDeterministicAcrossRepeatedCalls(t *testing.T) {
	table := validProgrammaticTable()

	first, err := table.RowByKey("M8x20")
	if err != nil {
		t.Fatalf("first RowByKey returned error: %v", err)
	}
	second, err := table.RowByKey("M8x20")
	if err != nil {
		t.Fatalf("second RowByKey returned error: %v", err)
	}

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("expected deterministic row content\nfirst:  %#v\nsecond: %#v", first, second)
	}
	if &first == &second {
		t.Fatal("expected separate row values for repeated lookups")
	}
	if reflect.ValueOf(first.Values).Pointer() == reflect.ValueOf(second.Values).Pointer() {
		t.Fatal("expected repeated lookups to return independent row maps")
	}
}

func TestRowByKey_ReturnedRowsAreIndependentAcrossCalls(t *testing.T) {
	table := validProgrammaticTable()
	before := cloneTable(table)

	r1, err := table.RowByKey("M8x20")
	if err != nil {
		t.Fatalf("first RowByKey returned error: %v", err)
	}
	r1.Values["length"] = Value{Type: ColumnTypeNumber, Number: json.Number("999")}
	r1.Values["code"] = Value{Type: ColumnTypeString, String: "CORRUPTED"}

	r2, err := table.RowByKey("M8x20")
	if err != nil {
		t.Fatalf("second RowByKey returned error: %v", err)
	}

	if r2.Values["length"].Number != json.Number("20") {
		t.Fatalf("expected second lookup to return clean length value, got %+v", r2.Values["length"])
	}
	if r2.Values["code"].String != "M8x20" {
		t.Fatalf("expected second lookup to return clean key value, got %+v", r2.Values["code"])
	}
	if reflect.ValueOf(r1.Values).Pointer() == reflect.ValueOf(r2.Values).Pointer() {
		t.Fatal("expected each lookup call to return a fresh row map clone")
	}
	if !reflect.DeepEqual(table, before) {
		t.Fatalf("expected stored table data to remain unchanged\nbefore: %#v\nafter:  %#v", before, table)
	}
}

func TestRowByKey_FoundRow(t *testing.T) {
	table := validProgrammaticTable()

	row, err := table.RowByKey("M8x20")
	if err != nil {
		t.Fatalf("RowByKey returned error: %v", err)
	}
	if row.Values["length"].Number != json.Number("20") {
		t.Fatalf("unexpected row returned: %+v", row)
	}
}

func TestRowByKey_ExactMatchSemantics(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{name: "exact_match", key: "M8x20", want: "M8x20"},
		{name: "leading_whitespace_not_trimmed", key: " M8x20"},
		{name: "trailing_whitespace_not_trimmed", key: "M8x20 "},
		{name: "case_not_folded", key: "m8x20"},
		{name: "different_punctuation_not_normalized", key: "M8X20"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			table := validProgrammaticTable()

			row, err := table.RowByKey(tt.key)
			if tt.want == "" {
				if err == nil {
					t.Fatal("expected RowByKey to fail")
				}
				if !errors.Is(err, ErrRuntime) {
					t.Fatalf("expected ErrRuntime, got %v", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("expected RowByKey to succeed, got %v", err)
			}
			if got := row.Values["code"].String; got != tt.want {
				t.Fatalf("expected key %q, got %q", tt.want, got)
			}
		})
	}
}

func TestRowByKey_DoesNotMutateCanonicalJSONOrFingerprint(t *testing.T) {
	table := validProgrammaticTable()

	canonicalBefore, err := CanonicalJSON(table)
	if err != nil {
		t.Fatalf("CanonicalJSON(before) returned error: %v", err)
	}
	fingerprintBefore, err := ComputeFingerprint(table)
	if err != nil {
		t.Fatalf("ComputeFingerprint(before) returned error: %v", err)
	}

	for i := 0; i < 3; i++ {
		if _, err := table.RowByKey("M8x20"); err != nil {
			t.Fatalf("RowByKey call %d returned error: %v", i+1, err)
		}
	}
	if _, err := table.RowByKey("M8x99"); err == nil {
		t.Fatal("expected missing-key lookup to fail")
	}

	canonicalAfter, err := CanonicalJSON(table)
	if err != nil {
		t.Fatalf("CanonicalJSON(after) returned error: %v", err)
	}
	fingerprintAfter, err := ComputeFingerprint(table)
	if err != nil {
		t.Fatalf("ComputeFingerprint(after) returned error: %v", err)
	}

	if string(canonicalBefore) != string(canonicalAfter) {
		t.Fatalf("expected canonical JSON to remain stable, got %q and %q", canonicalBefore, canonicalAfter)
	}
	if fingerprintBefore != fingerprintAfter {
		t.Fatalf("expected fingerprint to remain stable, got %q and %q", fingerprintBefore, fingerprintAfter)
	}
}

func TestRowByKey_ReturnedRowMutationDoesNotAffectStoredTableData(t *testing.T) {
	table := validProgrammaticTable()
	before := cloneTable(table)

	row, err := table.RowByKey("M8x20")
	if err != nil {
		t.Fatalf("RowByKey returned error: %v", err)
	}

	row.Values["length"] = Value{Type: ColumnTypeNumber, Number: json.Number("999")}
	row.Values["added"] = Value{Type: ColumnTypeString, String: "caller-owned"}
	delete(row.Values, "coated")

	again, err := table.RowByKey("M8x20")
	if err != nil {
		t.Fatalf("second RowByKey returned error: %v", err)
	}

	if !reflect.DeepEqual(table, before) {
		t.Fatalf("expected stored table data to remain unchanged\nbefore: %#v\nafter:  %#v", before, table)
	}
	if again.Values["length"].Number != json.Number("20") {
		t.Fatalf("expected stored row length to remain 20, got %+v", again.Values["length"])
	}
	if _, ok := again.Values["added"]; ok {
		t.Fatalf("unexpected leaked added column in stored row: %+v", again)
	}
	if _, ok := again.Values["coated"]; !ok {
		t.Fatalf("expected stored optional column to remain present: %+v", again)
	}
}

func TestRowByKey_OptionalColumnsRemainDeterministic(t *testing.T) {
	table := validProgrammaticTable()

	first, err := table.RowByKey("M8x30")
	if err != nil {
		t.Fatalf("first RowByKey returned error: %v", err)
	}
	second, err := table.RowByKey("M8x30")
	if err != nil {
		t.Fatalf("second RowByKey returned error: %v", err)
	}

	if _, ok := first.Values["coated"]; ok {
		t.Fatalf("optional omitted column should remain absent, got %+v", first)
	}
	if _, ok := second.Values["coated"]; ok {
		t.Fatalf("optional omitted column should remain absent, got %+v", second)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("expected deterministic rows for optional columns\nfirst:  %#v\nsecond: %#v", first, second)
	}
}

func TestRowByKey_DoesNotDependOnRowMapInsertionOrder(t *testing.T) {
	left := &Table{
		SchemaVersion: SchemaVersion,
		Name:          "fastener_catalog",
		KeyColumn:     "code",
		Columns: []Column{
			{Name: "code", Type: ColumnTypeString, Required: true},
			{Name: "diameter", Type: ColumnTypeNumber, Required: true},
			{Name: "length", Type: ColumnTypeNumber, Required: true},
			{Name: "coated", Type: ColumnTypeBoolean, Required: false},
		},
		Rows: []Row{
			{Values: map[string]Value{
				"code":     {Type: ColumnTypeString, String: "M8x20"},
				"diameter": {Type: ColumnTypeNumber, Number: json.Number("8")},
				"length":   {Type: ColumnTypeNumber, Number: json.Number("20")},
				"coated":   {Type: ColumnTypeBoolean, Boolean: true},
			}},
			{Values: map[string]Value{
				"code":     {Type: ColumnTypeString, String: "M8x30"},
				"diameter": {Type: ColumnTypeNumber, Number: json.Number("8")},
				"length":   {Type: ColumnTypeNumber, Number: json.Number("30")},
			}},
		},
	}
	right := &Table{
		SchemaVersion: SchemaVersion,
		Name:          "fastener_catalog",
		KeyColumn:     "code",
		Columns:       append([]Column(nil), left.Columns...),
		Rows: []Row{
			{Values: map[string]Value{
				"coated":   {Type: ColumnTypeBoolean, Boolean: true},
				"length":   {Type: ColumnTypeNumber, Number: json.Number("20")},
				"diameter": {Type: ColumnTypeNumber, Number: json.Number("8")},
				"code":     {Type: ColumnTypeString, String: "M8x20"},
			}},
			{Values: map[string]Value{
				"length":   {Type: ColumnTypeNumber, Number: json.Number("30")},
				"diameter": {Type: ColumnTypeNumber, Number: json.Number("8")},
				"code":     {Type: ColumnTypeString, String: "M8x30"},
			}},
		},
	}

	leftRow, err := left.RowByKey("M8x20")
	if err != nil {
		t.Fatalf("left RowByKey returned error: %v", err)
	}
	rightRow, err := right.RowByKey("M8x20")
	if err != nil {
		t.Fatalf("right RowByKey returned error: %v", err)
	}
	if !reflect.DeepEqual(leftRow, rightRow) {
		t.Fatalf("expected identical lookup result despite insertion order\nleft:  %#v\nright: %#v", leftRow, rightRow)
	}

	leftErr := rowByKeyError(t, left, "missing")
	rightErr := rowByKeyError(t, right, "missing")
	if leftErr.Error() != rightErr.Error() {
		t.Fatalf("expected identical runtime error despite insertion order, got %q and %q", leftErr.Error(), rightErr.Error())
	}
}

func TestRowByKey_InvalidTableStillReturnsValidationError(t *testing.T) {
	table := validProgrammaticTable()
	table.Name = " "

	_, err := table.RowByKey("M8x20")
	if err == nil {
		t.Fatal("expected RowByKey to fail")
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if errors.Is(err, ErrRuntime) {
		t.Fatalf("expected invalid table to stay a validation error, got %v", err)
	}
}

func TestRowByKey_DuplicateKeyTableReturnsValidationError(t *testing.T) {
	table := validProgrammaticTable()
	table.Rows[1].Values["code"] = Value{Type: ColumnTypeString, String: "M8x20"}

	validateErr := Validate(table)
	if validateErr == nil {
		t.Fatal("expected Validate to fail")
	}
	if !errors.Is(validateErr, ErrValidation) {
		t.Fatalf("expected duplicate keys to classify as ErrValidation, got %v", validateErr)
	}
	if errors.Is(validateErr, ErrRuntime) {
		t.Fatalf("expected duplicate keys to avoid runtime classification, got %v", validateErr)
	}

	_, lookupErr := table.RowByKey("M8x20")
	if lookupErr == nil {
		t.Fatal("expected RowByKey to fail")
	}
	if !errors.Is(lookupErr, ErrValidation) {
		t.Fatalf("expected RowByKey duplicate-key failure to classify as ErrValidation, got %v", lookupErr)
	}
	if errors.Is(lookupErr, ErrRuntime) {
		t.Fatalf("expected RowByKey duplicate-key failure to avoid ErrRuntime, got %v", lookupErr)
	}
	if validateErr.Error() != lookupErr.Error() {
		t.Fatalf("expected stable duplicate-key validation message across Validate and RowByKey, got %q and %q", validateErr.Error(), lookupErr.Error())
	}
}

func assertDifferentFingerprint(t *testing.T, left *Table, right *Table) {
	t.Helper()

	leftFingerprint, err := ComputeFingerprint(left)
	if err != nil {
		t.Fatalf("ComputeFingerprint(left) returned error: %v", err)
	}
	rightFingerprint, err := ComputeFingerprint(right)
	if err != nil {
		t.Fatalf("ComputeFingerprint(right) returned error: %v", err)
	}
	if leftFingerprint == rightFingerprint {
		t.Fatalf("expected different fingerprints, both were %q", leftFingerprint)
	}
}

func rowByKeyError(t *testing.T, table *Table, key string) error {
	t.Helper()

	_, err := table.RowByKey(key)
	if err == nil {
		t.Fatal("expected RowByKey to fail")
	}
	return err
}

func validProgrammaticTable() *Table {
	return &Table{
		SchemaVersion: SchemaVersion,
		Name:          "fastener_catalog",
		KeyColumn:     "code",
		Columns: []Column{
			{Name: "code", Type: ColumnTypeString, Required: true},
			{Name: "diameter", Type: ColumnTypeNumber, Required: true},
			{Name: "length", Type: ColumnTypeNumber, Required: true},
			{Name: "coated", Type: ColumnTypeBoolean, Required: false},
		},
		Rows: []Row{
			{Values: map[string]Value{
				"code":     {Type: ColumnTypeString, String: "M8x20"},
				"diameter": {Type: ColumnTypeNumber, Number: json.Number("8")},
				"length":   {Type: ColumnTypeNumber, Number: json.Number("20")},
				"coated":   {Type: ColumnTypeBoolean, Boolean: true},
			}},
			{Values: map[string]Value{
				"code":     {Type: ColumnTypeString, String: "M8x30"},
				"diameter": {Type: ColumnTypeNumber, Number: json.Number("8")},
				"length":   {Type: ColumnTypeNumber, Number: json.Number("30")},
			}},
		},
	}
}

func cloneTable(table *Table) *Table {
	if table == nil {
		return nil
	}

	clone := &Table{
		SchemaVersion: table.SchemaVersion,
		Name:          table.Name,
		KeyColumn:     table.KeyColumn,
		Columns:       append([]Column(nil), table.Columns...),
		Rows:          make([]Row, len(table.Rows)),
	}

	for i, row := range table.Rows {
		if row.Values == nil {
			continue
		}
		values := make(map[string]Value, len(row.Values))
		for key, value := range row.Values {
			values[key] = value
		}
		clone.Rows[i] = Row{Values: values}
	}

	return clone
}

const validTableJSON = `{
	"schemaVersion": "1.0",
	"name": "fastener_catalog",
	"keyColumn": "code",
	"columns": [
		{ "name": "code", "type": "string", "required": true },
		{ "name": "diameter", "type": "number", "required": true },
		{ "name": "length", "type": "number", "required": true },
		{ "name": "coated", "type": "boolean", "required": false }
	],
	"rows": [
		{ "code": "M8x20", "diameter": 8, "length": 20, "coated": true },
		{ "code": "M8x30", "diameter": 8, "length": 30 }
	]
}`
