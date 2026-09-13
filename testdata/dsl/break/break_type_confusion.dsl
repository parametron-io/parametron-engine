dsl v1.0
profile Test { output_dir = "break" }
use profile Test

product TypeConfusion {
    param x: number = 100
    param y: string = x 
    param z: number = y + 1
}