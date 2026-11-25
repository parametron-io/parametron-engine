dsl v1.0

// Capture acceptance fixture: two embedded technical drawings, one sheet each.

profile DrawingCapture {
  output_dir = "drawing"
}

use profile DrawingCapture

product DrawingCaptureModel {
  adapter      = "freecad"
  source_model = "drawing_multiple_drawings_model"
  outputs      = ["none"]
}
