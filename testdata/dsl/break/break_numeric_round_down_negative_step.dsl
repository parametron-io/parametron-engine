dsl v1.0
profile Test { output_dir = "break" }
use profile Test

product RoundDownNegativeBreak {
    param bad: number = round_down(10, -2)
}
