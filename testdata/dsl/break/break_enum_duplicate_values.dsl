dsl v1.0
// Enum parameter declares the same value twice.
// Expected: validate error — "duplicate enum value 'Oak'"
profile Test { output_dir = "break" }
use profile Test

product DuplicateEnum {
    param finish: enum { Oak, Oak } = Oak
}
