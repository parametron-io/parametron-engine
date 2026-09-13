dsl v1.0
const PREFIX = ""

profile Dev {
    output_dir = "smoke_const_empty"
    file_pattern = "{const:PREFIX}_{product}"
}

use profile Dev

product ProductA {
    param x: number = 1
}

product ProductB {
    param x: number = 2
}
