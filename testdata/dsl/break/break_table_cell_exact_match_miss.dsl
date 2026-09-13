dsl v1.0
// Exact-match lookup only: "M8" must not match "m8" or " M8 ".
// Expected: plan error.
product BreakTableCellExactMatchMiss {
    param diameter: number = table_cell("exact_fasteners", "M8", "diameter")
}
