dsl v1.0
profile NumericExpansion {
    output_dir = "smoke"
}
use profile NumericExpansion

product NumericExpansion {
    param width: number = 13
    param floor_val: number = floor(12.9)
    param ceil_val: number = ceil(12.1)
    param clamp_hi: number = clamp(15, 0, 10)
    param clamp_ok: number = clamp(5, 0, 10)
    param up_val: number = round_up(13, 5)
    param down_val: number = round_down(13, 5)
    param nested: number = max(floor(8.9), 3)
    param chained: number = round_up(clamp(width, 0, 100), 5)
}
