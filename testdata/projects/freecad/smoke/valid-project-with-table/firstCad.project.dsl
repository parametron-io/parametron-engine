dsl v1.0
const OUT = "freecad_box_out"

profile Dev {
  output_dir = OUT
  metadata_enabled = true
}

use profile Dev

product Box {
  adapter = "freecad"
  source_model = "box_model"
  outputs = ["step"]

  param sku: string = "M8x20"
  param label: string = table_cell("fasteners", sku, "label")
}
