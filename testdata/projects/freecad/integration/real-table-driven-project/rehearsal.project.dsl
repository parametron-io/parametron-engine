dsl v1.0

profile CouplingRehearsal {
  output_dir = "Coupling"
  metadata_enabled = true
}

use profile CouplingRehearsal

product small_plain {
  adapter      = "freecad"
  source_model = "coupling"
  outputs      = ["step"]

  param label: string = "small_plain"
}

product medium_keyed {
  adapter      = "freecad"
  source_model = "coupling"
  outputs      = ["step"]

  param label: string = "medium_keyed"
}
