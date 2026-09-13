dsl v1.0
const FILE_PREFIX = "x"

profile Dev {
    file_pattern = "{const:file_prefix}_{product}"
}

use profile Dev

product A {
    param x: number = 1
}
