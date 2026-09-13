dsl v1.0
const OUT = "freecad_box_out"

profile Dev {
  output_dir = OUT
  metadata_enabled = true
}

use profile Dev

product Box {
  adapter = "freecad"
  source_model = "boxmodel"
  outputs = ["step"]

  param length: number = 35
}
