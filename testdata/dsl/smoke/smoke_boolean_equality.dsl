dsl v1.0
profile BoolEq {
    output_dir = "bool_eq"
}

use profile BoolEq

product BoolComparison {
    param enabled: boolean = true
    param experimental: boolean = false
    param should_build: boolean = enabled != experimental
}
