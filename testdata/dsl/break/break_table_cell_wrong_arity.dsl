dsl v1.0
// Calls table_cell with two arguments instead of the required three.
// Expected: validate error.
product BreakTableCellWrongArity {
    param diameter: number = table_cell("fasteners", "M8x20")
}
