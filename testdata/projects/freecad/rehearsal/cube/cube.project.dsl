dsl v1.0

// Phase 5 cube rehearsal: parameter-only, capture-backed, table-driven.
// The five real FreeCAD VarSet dimensions come from cube_variants.

profile CubeRehearsal {
  output_dir = "cube"
  metadata_enabled = true
}

use profile CubeRehearsal

product CubeBox {
  adapter      = "freecad"
  source_model = "cube_rehearsal_model"
  outputs      = ["none"]

  /*
   * variant selects one deterministic cube configuration.
   * The default small row matches the committed FreeCAD fixture.
   */
  param variant: string = "small"

  param boxLength:  number = table_cell("cube_variants", variant, "boxLength")
  param boxWidth:   number = table_cell("cube_variants", variant, "boxWidth")
  param boxHeight:  number = table_cell("cube_variants", variant, "boxHeight")
  param holeDia:    number = table_cell("cube_variants", variant, "holeDia")
  param boxChamfer: number = table_cell("cube_variants", variant, "boxChamfer")
}
