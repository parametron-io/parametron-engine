dsl v1.0
product Early {
    param x: number = 10
}

const LATE = x

profile Test { output_dir = "break" }
use profile Test

product Late { param y: number = LATE }