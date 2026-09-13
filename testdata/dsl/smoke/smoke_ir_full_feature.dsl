dsl v1.0
const BASE_WIDTH = 120
const BASE_HEIGHT = 80
const THICKNESS = 2
const SCALE = 1.25
const PI = 3.14159
const NOTE_PREFIX = "part"
const OUT_DIR = "smoke_ir"

profile Dev {
    output_dir = OUT_DIR
    file_pattern = "{product}_{param:variant}_{param:quality}"
    metadata_enabled = true
}

use profile Dev

product Bracket {
    param variant: enum { Lite, Standard, Pro } = Standard
    param quality: enum { Draft, Final } = Final

    param width: number = BASE_WIDTH * SCALE
    param height: number = BASE_HEIGHT + THICKNESS * 5
    param area: number = width * height
    param diag: number = round(max(width, height) * PI)

    param is_pro: boolean = variant == Pro
    param is_final: boolean = quality == Final
    param should_export: boolean = !(variant == Lite) && is_final

    param pocket_count: number = is_pro ? 4 : 2
    param label: string = should_export ? "export_ready" : "preview_only"
    param tag: string = NOTE_PREFIX
}

product Plate {
    param variant: enum { Lite, Standard, Pro } = Pro
    param quality: enum { Draft, Final } = Draft

    param base: number = BASE_WIDTH + BASE_HEIGHT
    param adjusted: number = base + abs(-THICKNESS)
    param score: number = max(adjusted, round(PI * 10))

    param eq_check: boolean = variant == Pro
    param neq_check: boolean = quality != Final
    param ready: boolean = eq_check && neq_check

    param lane: number = ready ? 3 : 1
    param note: string = ready ? "ok" : "wait"
}
