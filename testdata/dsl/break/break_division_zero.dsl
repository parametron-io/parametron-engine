dsl v1.0
const ZERO = 0
profile Test { output_dir = "break" }
use profile Test

product DivZero {
    param a: number = 100 / ZERO
    param b: number = 100 / (50 - 50)
}