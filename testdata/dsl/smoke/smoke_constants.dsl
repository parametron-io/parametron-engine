dsl v1.0
const PI = 3.14159
const FACTOR = 2
const RADIUS_BASE = 50
const DIAMETER = RADIUS_BASE * FACTOR

profile Calc {
    output_dir = "calc_test"
}

use profile Calc

product Wheel {
    param radius: number = RADIUS_BASE
    param diameter: number = DIAMETER
    param circumference: number = PI * diameter
    param area: number = PI * (radius * radius)
}