dsl v1.0
const DEFAULT_MATERIAL = Oak

profile Production {
    output_dir = "smoke_output"
    file_pattern = "{product}_{param:material}_{param:diameter}"
}

use profile Production

product Gear {
    param radius: number = 50
    param diameter: number = radius * 2
    param circumference: number = round(2 * 3.14159 * radius)
    param min_size: number = min(radius, diameter)
    param max_size: number = max(radius, diameter)
    param tolerance: number = abs(diameter - 100)
    
    param material: enum { Oak, Pine, Steel } = DEFAULT_MATERIAL
    param is_metallic: boolean = material == Steel
    param is_wood: boolean = material != Steel
}