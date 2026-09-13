dsl v1.0
product UnaryNotSmoke {
    param a: boolean = true
    param b: boolean = false

    param not_a: boolean = !a
    param double_not_a: boolean = !!a
    param not_a_and_b: boolean = !a && b
    param a_or_not_b: boolean = a || !b
    param not_grouped: boolean = !(a && b)
    param not_equal_check: boolean = a != b
}
