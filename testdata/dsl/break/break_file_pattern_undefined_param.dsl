dsl v1.0
profile Dev {
    file_pattern = "{product}_{param:nonexistent}"
}

use profile Dev

product A {
    param x: number = 1
}
