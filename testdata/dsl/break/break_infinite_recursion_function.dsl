dsl v1.0
profile Test { output_dir = "break" }
use profile Test

product Recursion {
    param x: number = self.x + 1
}