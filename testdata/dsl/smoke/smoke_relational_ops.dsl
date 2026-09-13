dsl v1.0
// Smoke test: ordered relational operators (<, <=, >, >=) on numbers and strings.

profile Test {
    output_dir = "relational_smoke"
}

use profile Test

product RelationalOps {
    param width: number = 200
    param height: number = 100
    param label: string = "beta"

    // Number comparisons
    param is_wide: boolean = width > height
    param is_narrow: boolean = width < 500
    param at_least: boolean = height >= 100
    param at_most: boolean = height <= 200

    // String comparisons
    param is_before_gamma: boolean = label < "gamma"
    param is_alpha_or_later: boolean = label >= "alpha"

    // Ordered comparison result used in ternary
    param size_class: string = width > 150 ? "large" : "small"

    // Chained: number relational in boolean expression
    param in_range: boolean = width > 100 && width < 300
}
