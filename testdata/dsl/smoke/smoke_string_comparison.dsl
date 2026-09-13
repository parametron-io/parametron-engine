dsl v1.0
const DEFAULT_FINISH = "raw"

profile StringTest {
    output_dir = "string_test"
    metadata_enabled = true
}

use profile StringTest

product Panel {
    param material: string = "oak"
    param finish: string = "varnished"
    param is_premium: boolean = finish == "varnished"
    param is_raw: boolean = finish != DEFAULT_FINISH
    param label: string = is_premium ? "PREMIUM" : "STANDARD"
}