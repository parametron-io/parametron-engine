dsl v1.0
// Uses a non-literal table name.
// Expected: validate error.
product BreakTableCellNonLiteralTable {
    param table_name: string = "fasteners"
    param diameter: number = table_cell(table_name, "M8x20", "diameter")
}
