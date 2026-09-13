dsl v1.0
// Product declares an outputs format that is not in the allowed set for freecad.
// Expected: validate error — "outputs[0] has unsupported format"
profile Dev {
    output_dir = "break"
}

use profile Dev

product Widget {
    adapter = "freecad"
    source_model = "model/part.FCStd"
    outputs = ["dxf"]

    param x: number = 1
}
