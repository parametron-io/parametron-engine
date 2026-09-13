dsl v1.0
// Looks up a row key that does not exist.
// Expected: plan error.
product BreakTableCellMissingRow {
    param diameter: number = table_cell("fasteners", "M8x99", "diameter")
}
