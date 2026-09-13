dsl v1.0
const NAME = "x"

profile Dev {
    file_pattern = "{const:NAME:EXTRA}"
}

use profile Dev

product A {
    param x: number = 1
}
