dsl v1.0
const COMPANY = "ACME"
const PI = 3.14159
const VERSION = "v2"

profile Release {
    output_dir = "release"
    file_pattern = "{product}_{param:id}"
    metadata_enabled = true
}

use profile Release

product Mount {
    param id: number = 100
    param company: string = COMPANY
    param version: string = VERSION
    
    param width: number = 200
    param height: number = 100
    param depth: number = 50
    
    param volume: number = width * height * depth
    param max_dim: number = max(width, max(height, depth))
    param min_dim: number = min(width, min(height, depth))
    param rounded_vol: number = round(volume / 1000)
    
    param material: enum { Steel, Aluminum, Plastic } = Steel
    param is_metal: boolean = material == Steel || material == Aluminum
    param is_heavy: boolean = is_metal && (volume > 500000)
    
    param category: string = is_heavy ? "heavy_duty" : "standard"
    param valid: boolean = width > 0 && height > 0 && depth > 0 && (material != Plastic)
}