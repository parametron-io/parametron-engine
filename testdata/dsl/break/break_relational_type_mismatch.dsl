dsl v1.0
// Ordered comparison operator applied to operands of different types (string < number).
// Expected: validate error — type mismatch (right side of comparison '<')
product RelationalMismatch {
    param bad_lt: boolean = "a" < 1
}
