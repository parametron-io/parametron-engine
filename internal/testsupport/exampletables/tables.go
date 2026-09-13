package exampletables

import (
	"encoding/json"

	"parametron/internal/engine/table"
)

// SmokeBreakTables returns the planner-visible tables used by repository-level
// smoke and break examples.
func SmokeBreakTables() map[string]*table.Table {
	return map[string]*table.Table{
		"fasteners": {
			SchemaVersion: table.SchemaVersion,
			Name:          "fasteners",
			KeyColumn:     "sku",
			Columns: []table.Column{
				{Name: "sku", Type: table.ColumnTypeString, Required: true},
				{Name: "diameter", Type: table.ColumnTypeNumber, Required: true},
				{Name: "label", Type: table.ColumnTypeString, Required: true},
				{Name: "coated", Type: table.ColumnTypeBoolean, Required: true},
				{Name: "note", Type: table.ColumnTypeString, Required: false},
			},
			Rows: []table.Row{
				{Values: map[string]table.Value{
					"sku":      {Type: table.ColumnTypeString, String: "M8x20"},
					"diameter": {Type: table.ColumnTypeNumber, Number: json.Number("8")},
					"label":    {Type: table.ColumnTypeString, String: "Hex bolt"},
					"coated":   {Type: table.ColumnTypeBoolean, Boolean: true},
					"note":     {Type: table.ColumnTypeString, String: "zinc"},
				}},
				{Values: map[string]table.Value{
					"sku":      {Type: table.ColumnTypeString, String: "M8x30"},
					"diameter": {Type: table.ColumnTypeNumber, Number: json.Number("8")},
					"label":    {Type: table.ColumnTypeString, String: "Hex bolt long"},
					"coated":   {Type: table.ColumnTypeBoolean, Boolean: false},
				}},
			},
		},
		"exact_fasteners": {
			SchemaVersion: table.SchemaVersion,
			Name:          "exact_fasteners",
			KeyColumn:     "sku",
			Columns: []table.Column{
				{Name: "sku", Type: table.ColumnTypeString, Required: true},
				{Name: "diameter", Type: table.ColumnTypeNumber, Required: true},
			},
			Rows: []table.Row{
				{Values: map[string]table.Value{
					"sku":      {Type: table.ColumnTypeString, String: "m8"},
					"diameter": {Type: table.ColumnTypeNumber, Number: json.Number("8")},
				}},
				{Values: map[string]table.Value{
					"sku":      {Type: table.ColumnTypeString, String: " M8 "},
					"diameter": {Type: table.ColumnTypeNumber, Number: json.Number("8")},
				}},
			},
		},
	}
}
