dsl v1.0

// Capture acceptance fixture: one embedded technical drawing with two sheets.

profile DrawingCapture {
  output_dir = "drawing"
}

use profile DrawingCapture

product DrawingCaptureModel {
  adapter      = "freecad"
  source_model = "drawing_multi_sheet_model"
  outputs      = ["none"]
}
