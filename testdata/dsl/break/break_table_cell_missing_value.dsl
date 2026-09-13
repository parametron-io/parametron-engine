dsl v1.0
// The row exists, the column exists, but this row omits the cell value.
// Expected: plan error.
product BreakTableCellMissingValue {
    param note: string = table_cell("fasteners", "M8x30", "note")
}
