dsl v1.0
profile Test { output_dir = "break" }
use profile Test

product ClampBoundsBreak {
    param bad: number = clamp(10, 20, 5)
}
