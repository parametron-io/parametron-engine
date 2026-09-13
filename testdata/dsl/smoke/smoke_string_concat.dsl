dsl v1.0

const PREFIX = "box"

product StringConcat {
    param left: string = "A"
    param right: string = "B"
    param name: string = PREFIX + "_" + left + right
}
