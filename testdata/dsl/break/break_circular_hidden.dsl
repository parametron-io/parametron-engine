dsl v1.0
profile Test { output_dir = "break" }
use profile Test

product Sneaky {
    param a: number = b + 1
    param b: number = c + 1  
    param c: number = a + 1
}