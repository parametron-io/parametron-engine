dsl v1.0
// Smoke test: adapter-present path.
// With adapter = "freecad" declared the planner must emit:
//   WriteCSV -> WriteExportManifest -> RunCADRuntime
// FreeCAD does not need to be installed for plan generation to succeed.

profile Dev {
    output_dir = "smoke_adapter"
}

use profile Dev

product Part {
    adapter = "freecad"
    source_model = "model/part.FCStd"
    outputs = ["step"]

    param label: string = "part"
}
