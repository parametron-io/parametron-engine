dsl v1.0
// Uses a numeric row key expression.
// Expected: validate error.
product BreakTableCellNonStringRowKey {
    param diameter: number = table_cell("fasteners", 820, "diameter")
}
