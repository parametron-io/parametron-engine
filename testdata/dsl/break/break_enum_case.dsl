dsl v1.0
profile Test { output_dir = "break" }
use profile Test

product CaseTest {
    param material: enum { Oak, Pine } = oak
    param check: boolean = material == OAK
}