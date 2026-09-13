dsl v1.0
profile BreakEq {
    output_dir = "break"
}

use profile BreakEq

product InvalidEquality {
    param enabled: boolean = true
    param invalid: boolean = enabled == 1
}
