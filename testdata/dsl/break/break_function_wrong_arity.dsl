dsl v1.0
// Calls 'max' with one argument instead of the required two.
// Expected: validate error — "function 'max' expects 2 arguments, but got 1"
profile Test { output_dir = "break" }
use profile Test

product Widget {
    param x: number = max(100)
}
