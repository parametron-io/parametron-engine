dsl v1.0
// Uses a non-literal column name.
// Expected: validate error.
product BreakTableCellNonLiteralColumn {
    param column_name: string = "diameter"
    param diameter: number = table_cell("fasteners", "M8x20", column_name)
}
