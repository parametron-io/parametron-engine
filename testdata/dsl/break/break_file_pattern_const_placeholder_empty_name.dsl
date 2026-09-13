dsl v1.0
profile Dev {
    file_pattern = "{const:}"
}

use profile Dev

product A {
    param x: number = 1
}
