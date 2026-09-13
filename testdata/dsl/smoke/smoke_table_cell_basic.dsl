dsl v1.0

product TableCellBasic {
    param sku: string = "M8x20"
    param diameter: number = table_cell("fasteners", sku, "diameter")
    param label: string = table_cell("fasteners", sku, "label")
    param coated: boolean = table_cell("fasteners", sku, "coated")
}
