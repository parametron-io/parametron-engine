dsl v1.0
const BIG = 1e308
const BIGGER = BIG * 100

profile Test { output_dir = "break" }
use profile Test

product Overflow {
    param inf: number = BIG
    param nan: number = 0 / 0
    param negative_inf: number = -1e309
    param int_overflow: number = 922337203685477580700000000
}