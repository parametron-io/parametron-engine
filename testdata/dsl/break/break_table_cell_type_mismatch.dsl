dsl v1.0
// Looks up a number into a boolean parameter.
// Expected: validate error.
product BreakTableCellTypeMismatch {
    param coated: boolean = table_cell("fasteners", "M8x20", "diameter")
}
