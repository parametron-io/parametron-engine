dsl v1.0

const KEY_PREFIX = "M8"

product TableCellExpressionKey {
    param suffix: string = "x20"
    param lookup_key: string = KEY_PREFIX + suffix
    param diameter: number = table_cell("fasteners", lookup_key, "diameter")
}
