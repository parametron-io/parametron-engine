dsl v1.0
// Calls a function name that is not in the DSL stdlib.
// Expected: validate error — "unknown function: 'calculate'"
profile Test { output_dir = "break" }
use profile Test

product Widget {
    param x: number = calculate(100)
}
