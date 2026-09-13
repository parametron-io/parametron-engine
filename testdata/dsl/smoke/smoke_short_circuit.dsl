dsl v1.0
profile Test {
    output_dir = "shortcircuit_test"
}

use profile Test

product CircuitTest {
    param flag: boolean = false
    param safe_div: boolean = flag && (1/0 > 0)
    param skip_check: boolean = true || (1/0 > 0)
    
    param x: number = 10
    param y: number = 0
    param valid: boolean = y != 0 && (x/y > 5)
    param result: number = valid ? x : 0
}