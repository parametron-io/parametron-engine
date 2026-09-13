dsl v1.0
profile Test { output_dir = "break" }
use profile Test

product RoundUpZeroBreak {
    param bad: number = round_up(10, 0)
}
