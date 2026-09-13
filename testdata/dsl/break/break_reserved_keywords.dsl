dsl v1.0
profile Test { output_dir = "break" }
use profile Test

product if {
    param then: number = 1
    param number: number = 2
}