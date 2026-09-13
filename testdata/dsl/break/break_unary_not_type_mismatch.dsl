dsl v1.0
product UnaryNotBreak {
    param n: number = 1
    param s: string = "x"
    param e: enum { Oak, Pine } = Oak

    param bad_number: boolean = !n
    param bad_string: boolean = !s
    param bad_enum: boolean = !e
}
