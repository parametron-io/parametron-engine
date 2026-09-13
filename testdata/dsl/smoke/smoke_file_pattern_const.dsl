dsl v1.0
const FILE_PREFIX = "acme"

profile Dev {
    output_dir = "smoke_const"
    file_pattern = "{const:FILE_PREFIX}_{product}_{param:x}_{plan_hash}"
}

use profile Dev

product ProductA {
    param x: number = 42
}
