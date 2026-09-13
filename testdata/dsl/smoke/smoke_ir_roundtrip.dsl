dsl v1.0
const COMPANY = "ACME \"R&D\""
const PATH_HINT = "C:\\tmp\\parts"
const MULTILINE = "line1\\nline2\\tend"

profile Roundtrip {
    output_dir = "roundtrip_out"
    file_pattern = "{product}_{param:name}_{param:material}"
    metadata_enabled = true
}

use profile Roundtrip

product Escapes {
    param name: string = "helloWorld"
    param material: enum { Oak, Pine, Steel } = Oak

    param title: string = COMPANY
    param path: string = PATH_HINT
    param text: string = MULTILINE

    param width: number = 42
    param signed: number = -width
    param is_oak: boolean = material == Oak
    param is_not_pine: boolean = material != Pine
    param state: string = is_oak ? "primary" : "secondary"
    param final_name: string = is_not_pine ? name : "fallback"
}
